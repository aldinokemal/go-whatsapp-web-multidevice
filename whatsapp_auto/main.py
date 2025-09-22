"""
Main FastAPI application for AI WhatsApp Bot Service (refactored)
- Safer logging driven by settings
- Request ID middleware + structured logs context
- API key auth applied to /api/* routes via router dependency
- Thread-safe conversation store with asyncio.Lock
- Robust error handling & validation
- Graceful background task cancellation
- Configurable CORS origins via env (comma-separated)
"""
from __future__ import annotations

import asyncio
import logging
import os
from contextlib import asynccontextmanager
from datetime import datetime, timezone, timedelta
from typing import Any, Dict, List, Optional
from uuid import uuid4

import uvicorn
from fastapi import APIRouter, Depends, FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.security import APIKeyHeader
from pydantic import BaseModel, ValidationError
from pydantic_settings import BaseSettings

from ai_service.models import AIConfig, AIResponse, Conversation, Message
from ai_service.ollama_client import OllamaClient
from ai_service.ai_service import AIService


# ------------------------- Settings -------------------------
class Settings(BaseSettings):
    ollama_url: str = "http://localhost:11434"
    ollama_model: str = "llama3.2:3b-text-q4_K_M"
    ai_temperature: float = 0.7
    ai_max_tokens: int = 500
    ai_personality: str = "helpful"
    ai_enable_questions: bool = True
    auto_reply_threshold: float = 0.8

    app_host: str = "0.0.0.0"
    app_port: int = 8000
    log_level: str = "INFO"  # DEBUG, INFO, WARNING, ERROR

    api_key: Optional[str] = None

    cleanup_interval_hours: float = 1.0
    conversation_timeout_hours: float = 24.0

    # Comma-separated list of origins
    cors_allow_origins: str = "http://localhost"

    # Optional assistant name triggers for group mentions (comma-separated)
    assistant_names: Optional[str] = "Jason"

    class Config:
        env_file = ".env"
        env_file_encoding = "utf-8"


settings = Settings()

# Configure logging once using settings
logging.getLogger().setLevel(getattr(logging, settings.log_level.upper(), logging.INFO))
logger = logging.getLogger("ai-bot.main")


# ------------------------- Globals -------------------------
ollama_client: Optional[OllamaClient] = None
ai_service: Optional[AIService] = None
conversations: Dict[str, Conversation] = {}
conversations_lock = asyncio.Lock()
cleanup_task: Optional[asyncio.Task] = None

# API key security
api_key_header = APIKeyHeader(name="X-API-Key", auto_error=False)


async def verify_api_key(api_key: str = Depends(api_key_header)) -> None:
    """Verify API key if configured."""
    if settings.api_key and api_key != settings.api_key:
        raise HTTPException(status_code=401, detail="Invalid API key")


# ------------------------- Lifespan -------------------------
@asynccontextmanager
async def lifespan(app: FastAPI):
    global ollama_client, ai_service, cleanup_task

    logger.info("Starting AI WhatsApp Bot Service…")

    # Initialize Ollama client
    ollama_client = OllamaClient(
        base_url=settings.ollama_url,
        timeout=120.0,  # Increased from 30.0 to handle slow model responses
        max_retries=3,
        retry_backoff=1.0,
    )

    # Non-blocking health check
    try:
        healthy = await ollama_client.health_check()
        logger.log(logging.INFO if healthy else logging.WARNING, "Ollama health: %s", healthy)
    except Exception as e:
        logger.warning("Ollama health check failed at startup: %s", e)

    # Initialize AI service
    config = AIConfig(
        model_name=settings.ollama_model,
        temperature=settings.ai_temperature,
        max_tokens=settings.ai_max_tokens,
        personality=settings.ai_personality,
        enable_questions=settings.ai_enable_questions,
        response_delay=1.0,
        auto_reply_threshold=settings.auto_reply_threshold,
        assistant_names=_split_csv(settings.assistant_names) if settings.assistant_names else ["Jason"],
    )
    ai_service = AIService(config, ollama_client)

    # Start cleanup task
    cleanup_task = asyncio.create_task(cleanup_old_conversations())
    logger.info("Background cleanup task started")

    try:
        yield
    finally:
        logger.info("Shutting down AI WhatsApp Bot Service…")
        if cleanup_task:
            cleanup_task.cancel()
            try:
                await cleanup_task
            except asyncio.CancelledError:
                pass
        if ollama_client:
            await ollama_client.close()
            logger.info("Ollama client closed")


# ------------------------- App & Middleware -------------------------
app = FastAPI(
    title="AI WhatsApp Bot Service",
    description="AI-powered WhatsApp bot using Ollama",
    version="1.0.1",
    lifespan=lifespan,
)


@app.middleware("http")
async def request_id_middleware(request: Request, call_next):
    request_id = request.headers.get("X-Request-ID") or str(uuid4())
    request.state.request_id = request_id
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


# CORS — parse CSV env
allow_origins = [o.strip() for o in settings.cors_allow_origins.split(",") if o.strip()]
app.add_middleware(
    CORSMiddleware,
    allow_origins=allow_origins,
    allow_credentials=True,
    allow_methods=["GET", "POST", "DELETE"],
    allow_headers=["X-API-Key", "Content-Type", "X-Request-ID"],
)


# ------------------------- Schemas -------------------------
class ProcessMessageRequest(BaseModel):
    message: Message
    user_profile: Optional[Dict[str, Any]] = None
    group_context: Optional[Dict[str, Any]] = None


class ProcessMessageResponse(BaseModel):
    success: bool
    ai_response: Optional[AIResponse] = None
    conversation: Optional[Conversation] = None
    error: Optional[str] = None


class HealthResponse(BaseModel):
    status: str
    ollama_healthy: bool
    service_version: str
    active_conversations: int
    available_models: List[str]


# ------------------------- Public Endpoints -------------------------
@app.get("/", response_model=Dict[str, str])
async def root():
    return {"service": "AI WhatsApp Bot Service", "version": app.version, "status": "running"}


@app.get("/health", response_model=HealthResponse)
async def health_check():
    ollama_healthy = False
    models: List[str] = []
    if ollama_client:
        try:
            ollama_healthy = await ollama_client.health_check()
        except Exception as e:
            logger.error("Ollama health check error: %s", e)
        try:
            models = await ollama_client.get_available_models()
        except Exception as e:
            logger.error("Failed to fetch models: %s", e)
    return HealthResponse(
        status="healthy" if ollama_healthy else "unhealthy",
        ollama_healthy=ollama_healthy,
        service_version=app.version,
        active_conversations=len(conversations),
        available_models=models,
    )


# ------------------------- Secured API Router -------------------------
api = APIRouter(prefix="/api", dependencies=[Depends(verify_api_key)])


@api.post("/process-message", response_model=ProcessMessageResponse)
async def process_message(request: ProcessMessageRequest, http_request: Request):
    """Process an incoming message and generate AI response."""
    request_id = getattr(http_request.state, "request_id", "-")
    chat_id = request.message.chat_id or ""

    try:
        logger.info("[%s] /process-message chat_id=%s sender=%s", request_id, chat_id, request.message.sender_id)

        if not chat_id:
            raise ValueError("Message chat_id cannot be empty")

        # Get or create conversation (thread-safe)
        async with conversations_lock:
            conversation = conversations.get(chat_id)
            if conversation is None:
                conversation = Conversation(
                    chat_id=chat_id,
                    is_group=bool(request.message.is_group),
                    group_name=request.message.group_name,
                    participants=[request.message.sender_id] if request.message.sender_id else [],
                )
                # Set an initial timestamp to now if missing
                if not conversation.last_updated:
                    conversation.last_updated = datetime.now(timezone.utc)
                conversations[chat_id] = conversation

        # Process via AI service
        if ai_service is None:
            raise RuntimeError("AI service not initialized")

        ai_resp = await ai_service.process_message(
            message=request.message,
            conversation=conversation,
            user_profile=request.user_profile,
            group_context=request.group_context,
        )

        logger.info("[%s] AI responded (len=%d, should_reply=%s)", request_id, len(ai_resp.text or ""), ai_resp.should_reply)
        return ProcessMessageResponse(success=True, ai_response=ai_resp, conversation=conversation)

    except ValidationError as e:
        logger.warning("[%s] Validation error: %s", request_id, e)
        raise HTTPException(status_code=422, detail=e.errors())
    except Exception as e:
        logger.exception("[%s] Error processing message", request_id)
        # Return current conversation if exists
        async with conversations_lock:
            conv = conversations.get(chat_id)
        return ProcessMessageResponse(success=False, conversation=conv, error=str(e))


@api.get("/conversations/{chat_id}", response_model=Conversation)
async def get_conversation(chat_id: str):
    async with conversations_lock:
        conv = conversations.get(chat_id)
    if not conv:
        raise HTTPException(status_code=404, detail="Conversation not found")
    return conv


@api.get("/conversations", response_model=Dict[str, Conversation])
async def list_conversations():
    async with conversations_lock:
        # return a shallow copy to avoid mutation during iteration
        return dict(conversations)


@api.delete("/conversations/{chat_id}", response_model=Dict[str, Any])
async def delete_conversation(chat_id: str):
    async with conversations_lock:
        if chat_id not in conversations:
            raise HTTPException(status_code=404, detail="Conversation not found")
        del conversations[chat_id]
    logger.info("Deleted conversation: %s", chat_id)
    return {"success": True, "message": f"Conversation {chat_id} deleted"}


@api.post("/conversations/{chat_id}/clear", response_model=Dict[str, Any])
async def clear_conversation_context(chat_id: str):
    async with conversations_lock:
        conv = conversations.get(chat_id)
        if not conv:
            raise HTTPException(status_code=404, detail="Conversation not found")
        conv.context = ""
        conv.messages = []
        conv.last_updated = datetime.now(timezone.utc)
    logger.info("Cleared context for conversation: %s", chat_id)
    return {"success": True, "message": f"Context cleared for conversation {chat_id}"}


@api.get("/models", response_model=Dict[str, List[str]])
async def list_models():
    if not ollama_client:
        raise HTTPException(status_code=503, detail="Ollama client not initialized")
    try:
        models = await ollama_client.get_available_models()
        return {"models": models}
    except Exception as e:
        logger.error("Error listing models: %s", e)
        raise HTTPException(status_code=500, detail=f"Error listing models: {e}")


app.include_router(api)


# ------------------------- Background Cleanup -------------------------
async def cleanup_old_conversations():
    """Periodically evict stale conversations to limit memory usage."""
    interval = max(0.1, settings.cleanup_interval_hours) * 3600
    timeout = max(0.5, settings.conversation_timeout_hours)

    while True:
        try:
            now = datetime.now(timezone.utc)
            to_remove: List[str] = []
            async with conversations_lock:
                for chat_id, conv in list(conversations.items()):
                    last = conv.last_updated or (now - timedelta(hours=timeout + 1))
                    hours = (now - last).total_seconds() / 3600.0
                    # Only remove empty conversations if they've been idle a while (avoid race right after creation)
                    if hours > timeout or (hours > 0.25 and not conv.messages):
                        to_remove.append(chat_id)
                for cid in to_remove:
                    conversations.pop(cid, None)
            for cid in to_remove:
                logger.info("Cleaned up conversation: %s", cid)
        except asyncio.CancelledError:
            break
        except Exception as e:
            logger.error("Error in cleanup task: %s", e)
        finally:
            await asyncio.sleep(interval)


# ------------------------- Utils -------------------------

def _split_csv(csv: Optional[str]) -> List[str]:
    """Split comma-separated string into list, handling edge cases."""
    if not csv:
        return ["Jason"]  # Default fallback
    try:
        items = [x.strip() for x in csv.split(',') if x.strip()]
        return items if items else ["Jason"]  # Ensure we always have at least one item
    except Exception:
        return ["Jason"]  # Fallback on any error


# ------------------------- Entrypoint -------------------------
if __name__ == "__main__":
    # Respect LOG_LEVEL & UVICORN_LOG_LEVEL if set
    uvicorn_log_level = os.getenv("UVICORN_LOG_LEVEL", settings.log_level.lower())
    reload_flag = os.getenv("DEV", "false").lower() in {"1", "true", "yes"}

    uvicorn.run(
        "main:app",
        host=settings.app_host,
        port=settings.app_port,
        log_level=uvicorn_log_level,
        reload=reload_flag,
    )
