"""
Ollama client for AI model integration (robust)
- Async httpx client with retry/backoff (+ jitter) and pooled connections
- Optional session-based context caching (Ollama "context" tokens) with async lock
- Safer logging (no basicConfig at import; redact long prompts unless DEBUG)
- TTL cache for available models
- Flexible generate/stream methods: stop tokens, format, keep_alive, extra options
- Context manager support: `async with OllamaClient(...) as cli:`
"""
from __future__ import annotations

import asyncio
import json
import logging
import random
import time
from typing import Any, AsyncGenerator, Dict, List, Optional, Tuple

import httpx
from pydantic import BaseModel, Field
from pydantic_core import ValidationError

logger = logging.getLogger(__name__)


class OllamaResponse(BaseModel):
    """Response from Ollama API (single JSON object or stream chunk)."""

    model: str = Field(..., description="Name of the model used")
    created_at: str = Field(..., description="Timestamp of response creation")
    response: str = Field("", description="Generated response text")
    done: bool = Field(False, description="Whether generation is complete")

    # Optional returns
    context: Optional[List[int]] = Field(None, description="Context tokens for continuation")
    total_duration: Optional[int] = Field(None, description="Total processing time (ns)")
    load_duration: Optional[int] = Field(None, description="Model loading time (ns)")
    prompt_eval_duration: Optional[int] = Field(None, description="Prompt evaluation time (ns)")
    eval_duration: Optional[int] = Field(None, description="Response generation time (ns)")


class OllamaClient:
    """Client for interacting with the Ollama REST API."""

    def __init__(
        self,
        base_url: str = "http://localhost:11434",
        timeout: float = 120.0,  # Increased from 30.0 to handle slow model responses
        max_retries: int = 3,
        retry_backoff: float = 1.0,
        retry_statuses: Optional[List[int]] = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.max_retries = max_retries
        self.retry_backoff = retry_backoff
        self.retry_statuses = set(retry_statuses or [408, 429, 500, 502, 503, 504])

        self.client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=timeout,
            limits=httpx.Limits(max_connections=128, max_keepalive_connections=32),
        )

        # Session contexts keyed by arbitrary session_id (e.g., chat_id)
        self._contexts: Dict[str, List[int]] = {}
        self._ctx_lock = asyncio.Lock()

        # Models cache (TTL)
        self._available_models: Optional[List[str]] = None
        self._models_cached_at: float = 0.0
        self._models_ttl: float = 60.0  # seconds

    # ------------- lifecycle -------------
    async def __aenter__(self) -> "OllamaClient":
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.close()

    async def close(self) -> None:
        try:
            await self.client.aclose()
            logger.debug("Ollama client closed")
        except Exception as e:
            logger.warning("Error closing Ollama client: %s", e)

    # ------------- health & models -------------
    async def health_check(self) -> bool:
        try:
            resp = await self.client.get("/api/tags")
            resp.raise_for_status()
            return True
        except httpx.HTTPError as e:
            logger.debug("Ollama health check failed: %s", e)
            return False

    async def get_available_models(self, force_refresh: bool = False) -> List[str]:
        if (
            not force_refresh
            and self._available_models is not None
            and (time.time() - self._models_cached_at) < self._models_ttl
        ):
            return self._available_models
        resp = await self._request("GET", "/api/tags")
        data = resp.json()
        self._available_models = [m.get("name", "") for m in data.get("models", []) if m.get("name")]
        self._models_cached_at = time.time()
        return self._available_models

    # ------------- public generation APIs -------------
    async def generate_response(
        self,
        *,
        prompt: str,
        model: str = "llama3.2:3b-text-q4_K_M",
        temperature: float = 0.7,
        max_tokens: int = 500,
        context: Optional[List[int]] = None,
        stop: Optional[List[str]] = None,
        response_format: Optional[str] = None,  # e.g., "json" for structured output
        keep_alive: Optional[str] = None,  # e.g., "5m" to keep the model in memory
        session_id: Optional[str] = None,  # if provided, context will be loaded/saved
        extra_options: Optional[Dict[str, Any]] = None,
        max_retries: Optional[int] = None,
    ) -> OllamaResponse:
        """Call /api/generate once (non-streaming) with retries and optional context caching."""
        if not prompt or not prompt.strip():
            raise ValueError("Prompt cannot be empty")
        if temperature < 0.0 or temperature > 2.0:
            raise ValueError("Temperature must be between 0.0 and 2.0")
        if max_tokens <= 0:
            raise ValueError("max_tokens must be positive")

        if session_id and context is None:
            # pull cached context
            async with self._ctx_lock:
                context = self._contexts.get(session_id)

        payload: Dict[str, Any] = {
            "model": model,
            "prompt": prompt,
            "stream": False,
            "options": {
                "temperature": temperature,
                "num_predict": max_tokens,
            },
        }
        if stop:
            payload["stop"] = stop
        if response_format:
            payload["format"] = response_format
        if keep_alive:
            payload["keep_alive"] = keep_alive
        if context:
            payload["context"] = context
        if extra_options:
            payload["options"].update(extra_options)

        _log_payload_safely(model, prompt, payload)

        retries = self.max_retries if max_retries is None else max_retries
        resp = await self._request("POST", "/api/generate", json=payload, retries=retries)
        data = resp.json()
        _debug_log_response(data)

        try:
            ollama_resp = OllamaResponse.model_validate(data)
        except ValidationError as e:
            # Fallback: coerce minimal structure if Ollama returns partial fields
            logger.warning("Validating Ollama response failed: %s; raw=%s", e, data)
            ollama_resp = OllamaResponse(
                model=str(data.get("model", model)),
                created_at=str(data.get("created_at", "")),
                response=str(data.get("response", "")),
                done=bool(data.get("done", True)),
                context=data.get("context"),
                total_duration=data.get("total_duration"),
                load_duration=data.get("load_duration"),
                prompt_eval_duration=data.get("prompt_eval_duration"),
                eval_duration=data.get("eval_duration"),
            )

        # Save updated context for the session
        if session_id and ollama_resp.context is not None:
            async with self._ctx_lock:
                self._contexts[session_id] = ollama_resp.context

        return ollama_resp

    async def stream_response(
        self,
        *,
        prompt: str,
        model: str = "llama3.2:3b-text-q4_K_M",
        temperature: float = 0.7,
        max_tokens: int = 500,
        context: Optional[List[int]] = None,
        stop: Optional[List[str]] = None,
        response_format: Optional[str] = None,
        keep_alive: Optional[str] = None,
        session_id: Optional[str] = None,
        extra_options: Optional[Dict[str, Any]] = None,
    ) -> AsyncGenerator[OllamaResponse, None]:
        """Stream /api/generate; yields OllamaResponse chunks as they arrive."""
        if not prompt or not prompt.strip():
            raise ValueError("Prompt cannot be empty")
        if temperature < 0.0 or temperature > 2.0:
            raise ValueError("Temperature must be between 0.0 and 2.0")
        if max_tokens <= 0:
            raise ValueError("max_tokens must be positive")

        if session_id and context is None:
            async with self._ctx_lock:
                context = self._contexts.get(session_id)

        payload: Dict[str, Any] = {
            "model": model,
            "prompt": prompt,
            "stream": True,
            "options": {
                "temperature": temperature,
                "num_predict": max_tokens,
            },
        }
        if stop:
            payload["stop"] = stop
        if response_format:
            payload["format"] = response_format
        if keep_alive:
            payload["keep_alive"] = keep_alive
        if context:
            payload["context"] = context
        if extra_options:
            payload["options"].update(extra_options)

        _log_payload_safely(model, prompt, payload)

        try:
            async with self.client.stream("POST", "/api/generate", json=payload) as resp:
                resp.raise_for_status()
                async for line in resp.aiter_lines():
                    if not line:
                        continue
                    try:
                        data = json.loads(line)
                        _debug_log_response(data)
                        chunk = OllamaResponse.model_validate(data)
                        # Update session context if provided in stream
                        if session_id and chunk.context is not None:
                            async with self._ctx_lock:
                                self._contexts[session_id] = chunk.context
                        yield chunk
                    except (json.JSONDecodeError, ValidationError) as e:
                        logger.debug("Skipping invalid stream chunk: %s", e)
                        continue
        except httpx.HTTPError as e:
            _log_http_error(e)
            raise

    # ------------- low-level with retries -------------
    async def _request(
        self,
        method: str,
        url: str,
        *,
        retries: Optional[int] = None,
        **kwargs: Any,
    ) -> httpx.Response:
        attempts = (self.max_retries if retries is None else retries) + 1
        for attempt in range(1, attempts + 1):
            try:
                resp = await self.client.request(method, url, **kwargs)
                if resp.status_code in self.retry_statuses:
                    raise httpx.HTTPStatusError("retryable status", request=resp.request, response=resp)
                resp.raise_for_status()
                return resp
            except (httpx.TimeoutException, httpx.TransportError, httpx.HTTPStatusError) as e:
                if attempt >= attempts:
                    _log_http_error(e)
                    raise
                delay = self._compute_backoff(attempt)
                logger.warning("HTTP error on %s %s (attempt %d/%d): %s — retrying in %.2fs", method, url, attempt, attempts, e, delay)
                await asyncio.sleep(delay)

        raise RuntimeError("Retry loop terminated unexpectedly")

    def _compute_backoff(self, attempt: int) -> float:
        base = self.retry_backoff * (2 ** (attempt - 1))
        jitter = base * 0.2 * random.random()  # up to 20% jitter
        return min(30.0, base + jitter)

    # ------------- session context utilities -------------
    async def get_session_context(self, session_id: str) -> Optional[List[int]]:
        async with self._ctx_lock:
            return self._contexts.get(session_id)

    async def set_session_context(self, session_id: str, context: Optional[List[int]]) -> None:
        async with self._ctx_lock:
            if context is None:
                self._contexts.pop(session_id, None)
            else:
                self._contexts[session_id] = context

    async def clear_contexts(self) -> None:
        async with self._ctx_lock:
            self._contexts.clear()


# ------------------------- helpers -------------------------

def _sanitize_prompt(prompt: str, max_len: int = 200) -> str:
    if logger.isEnabledFor(logging.DEBUG):
        return prompt[:max_len].replace("\n", " ")
    # If not in DEBUG, only log length
    return f"<prompt len={len(prompt)}>"


def _log_payload_safely(model: str, prompt: str, payload: Dict[str, Any]) -> None:
    safe_prompt = _sanitize_prompt(prompt)
    payload_preview = {
        "model": payload.get("model"),
        "stream": payload.get("stream"),
        "has_context": bool(payload.get("context")),
        "options": payload.get("options"),
        "format": payload.get("format"),
        "stop": payload.get("stop"),
        "keep_alive": payload.get("keep_alive"),
    }
    logger.info("Generating with model=%s, prompt=%s, payload=%s", model, safe_prompt, payload_preview)


def _debug_log_response(data: Dict[str, Any]) -> None:
    if logger.isEnabledFor(logging.DEBUG):
        logger.debug("Ollama raw response: %s", data)


def _log_http_error(e: Exception) -> None:
    if isinstance(e, httpx.HTTPStatusError) and e.response is not None:
        try:
            body = e.response.text
        except Exception:
            body = "<unavailable>"
        logger.error("HTTP %s for %s %s — body=%s", e.response.status_code, e.request.method, e.request.url, body)
    else:
        logger.error("HTTP error: %s", e)
