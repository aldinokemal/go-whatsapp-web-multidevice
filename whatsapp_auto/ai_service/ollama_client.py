"""
Ollama client for AI model integration
"""

import asyncio
import json
import logging
from typing import List, Optional, AsyncGenerator

import httpx
from pydantic import BaseModel, Field
from pydantic_core import ValidationError

logger = logging.getLogger(__name__)

# Configure default logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)


class OllamaResponse(BaseModel):
    """Response from Ollama API"""

    model: str = Field(..., description="Name of the model used")
    created_at: str = Field(..., description="Timestamp of response creation")
    response: str = Field(..., description="Generated response text")
    done: bool = Field(..., description="Whether generation is complete")
    context: Optional[List[int]] = Field(
        None, description="Context tokens for continuation"
    )
    total_duration: Optional[int] = Field(
        None, description="Total processing time (ns)"
    )
    load_duration: Optional[int] = Field(None, description="Model loading time (ns)")
    prompt_eval_duration: Optional[int] = Field(
        None, description="Prompt evaluation time (ns)"
    )
    eval_duration: Optional[int] = Field(
        None, description="Response generation time (ns)"
    )


class OllamaClient:
    """Client for interacting with Ollama API"""

    def __init__(
        self,
        base_url: str = "http://localhost:11434",
        timeout: float = 30.0,
        max_retries: int = 3,
        retry_backoff: float = 1.0,
    ):
        """
        Initialize Ollama client.

        Args:
            base_url: Base URL for Ollama API
            timeout: Request timeout in seconds
            max_retries: Maximum number of retries for failed requests
            retry_backoff: Backoff factor for retry delays
        """
        # Store base_url as string to avoid HttpUrl formatting issues
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.max_retries = max_retries
        self.retry_backoff = retry_backoff
        self.client = httpx.AsyncClient(
            timeout=timeout,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )
        self._available_models: Optional[List[str]] = None

    async def close(self) -> None:
        """Close the HTTP client."""
        try:
            await self.client.aclose()
            logger.debug("Ollama client closed successfully")
        except Exception as e:
            logger.error(f"Error closing Ollama client: {e}")

    async def health_check(self) -> bool:
        """Check if Ollama service is healthy."""
        try:
            response = await self.client.get(f"{self.base_url}/api/tags")
            response.raise_for_status()
            logger.debug("Ollama health check passed")
            return True
        except httpx.HTTPError as e:
            logger.error(f"Health check failed: {e}")
            return False

    async def get_available_models(self) -> List[str]:
        """Retrieve list of available models from Ollama API."""
        if self._available_models is not None:
            return self._available_models

        try:
            response = await self.client.get(f"{self.base_url}/api/tags")
            response.raise_for_status()
            data = response.json()
            self._available_models = [
                model["name"] for model in data.get("models", [])
            ]
            logger.debug(f"Retrieved available models: {self._available_models}")
            return self._available_models
        except httpx.HTTPError as e:
            logger.error(f"Failed to fetch available models: {e}")
            raise

    async def generate_response(
        self,
        prompt: str,
        model: str = "llama2",
        temperature: float = 0.7,
        max_tokens: int = 500,
        max_retries: Optional[int] = None,
    ) -> OllamaResponse:
        """
        Generate a response from Ollama with retry logic.

        Args:
            prompt: Input prompt for the model
            model: Model name to use
            temperature: Sampling temperature (0.0 to 2.0)
            max_tokens: Maximum number of tokens to generate
            max_retries: Override default max retries for this request

        Returns:
            OllamaResponse: Response from the Ollama API

        Raises:
            ValueError: If parameters are invalid
            httpx.HTTPError: If API request fails after retries
        """
        if not prompt.strip():
            raise ValueError("Prompt cannot be empty")
        if temperature < 0.0 or temperature > 2.0:
            raise ValueError("Temperature must be between 0.0 and 2.0")
        if max_tokens <= 0:
            raise ValueError("max_tokens must be positive")

        payload = {
            "model": model,
            "prompt": prompt,
            "stream": False,
            "options": {
                "temperature": temperature,
                "num_predict": max_tokens,
            },
        }

        retries = max_retries if max_retries is not None else self.max_retries
        for attempt in range(retries + 1):
            try:
                response = await self.client.post(
                    f"{self.base_url}/api/generate",
                    json=payload,
                    timeout=self.timeout,
                )
                response.raise_for_status()
                logger.debug(f"Generated response for prompt: {prompt[:50]}...")
                return OllamaResponse.model_validate(response.json())
            except (httpx.HTTPError, ValidationError) as e:
                if attempt == retries:
                    logger.error(
                        f"Failed to generate response after {retries} attempts: {e}"
                    )
                    raise
                logger.warning(f"Retry {attempt + 1}/{retries} after error: {e}")
                await asyncio.sleep(self.retry_backoff * (2**attempt))

        raise RuntimeError("Unexpected error: Retry loop exited without result")

    async def stream_response(
        self,
        prompt: str,
        model: str = "llama2",
        temperature: float = 0.7,
        max_tokens: int = 500,
    ) -> AsyncGenerator[OllamaResponse, None]:
        """
        Stream response from Ollama API.

        Args:
            prompt: Input prompt for the model
            model: Model name to use
            temperature: Sampling temperature (0.0 to 2.0)
            max_tokens: Maximum number of tokens to generate

        Yields:
            OllamaResponse: Partial response chunks from the API
        """
        if not prompt.strip():
            raise ValueError("Prompt cannot be empty")
        if temperature < 0.0 or temperature > 2.0:
            raise ValueError("Temperature must be between 0.0 and 2.0")
        if max_tokens <= 0:
            raise ValueError("max_tokens must be positive")

        payload = {
            "model": model,
            "prompt": prompt,
            "stream": True,
            "options": {
                "temperature": temperature,
                "num_predict": max_tokens,
            },
        }

        try:
            async with self.client.stream(
                "POST", f"{self.base_url}/api/generate", json=payload
            ) as response:
                response.raise_for_status()
                async for line in response.aiter_lines():
                    if line:
                        try:
                            yield OllamaResponse.model_validate(json.loads(line))
                        except ValidationError as e:
                            logger.error(f"Invalid response chunk: {e}")
                            continue
        except httpx.HTTPError as e:
            logger.error(f"Streaming error: {e}")
            raise
