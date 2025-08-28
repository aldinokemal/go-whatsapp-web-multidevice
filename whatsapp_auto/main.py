"""
Main FastAPI application for AI WhatsApp Bot Service
"""

import asyncio
import logging
from contextlib import asynccontextmanager
from typing import Dict, Any, Optional, List
from datetime import datetime, timezone

import uvicorn
from fastapi import FastAPI, HTTPException, BackgroundTasks, Depends
from fastapi.middleware.cors import CORSMiddleware
from fastapi.security import APIKeyHeader
from pydantic import BaseModel, ValidationError
from pydantic_settings import BaseSettings

from ai_service.models import Message, Conversation, AIResponse, AIConfig
from ai_service.ollama_client import OllamaClient
from ai_service.ai_service import AIService

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


# Configuration settings
class Settings(BaseSettings):
    ollama_url: str = "http://localhost:11434"
    ollama_model: str = "llama3.2:3b-text-q4_K_M"
    ai_temperature: float = 0.7
    ai_max_tokens: int = 500
    ai_personality: str = "helpful"
    ai_enable_questions: bool = True
    app_host: str = "0.0.0.0"
    app_port: int = 8000
    api_key: Optional[str] = None
    cleanup_interval_hours: float = 1.0
    conversation_timeout_hours: float = 24.0
    log_level: str = "INFO"

    class Config:
        env_file = ".env"
        env_file_encoding = "utf-8"


settings = Settings()

# Global variables
ollama_client: Optional[OllamaClient] = None
ai_service: Optional[AIService] = None
conversations: Dict[str, Conversation] = {}

# API key security
api_key_header = APIKeyHeader(name="X-API-Key", auto_error=False)


async def verify_api_key(api_key: str = Depends(api_key_header)) -> None:
    """Verify API key if configured."""
    if settings.api_key and api_key != settings.api_key:
        raise HTTPException(status_code=401, detail="Invalid API key")


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan manager."""
    global ollama_client, ai_service

    # Startup
    logger.info("Starting AI WhatsApp Bot Service...")

    try:
        # Initialize Ollama client
        ollama_client = OllamaClient(
            base_url=settings.ollama_url,
            timeout=30.0,
            max_retries=3,
            retry_backoff=1.0,
        )

        # Check Ollama health
        if not await ollama_client.health_check():
            logger.error("Ollama service is not available")
            raise RuntimeError("Ollama service is not available")

        logger.info("Ollama service is healthy")

        # Initialize AI service
        config = AIConfig(
            model_name=settings.ollama_model,
            temperature=settings.ai_temperature,
            max_tokens=settings.ai_max_tokens,
            personality=settings.ai_personality,
            enable_questions=settings.ai_enable_questions,
            response_delay=1.0,
            auto_reply_threshold=0.8,
        )

        ai_service = AIService(config, ollama_client)
        logger.info("AI Service initialized with model: %s", config.model_name)

        # Start background cleanup task
        logger.info("Starting background cleanup task")
        asyncio.create_task(cleanup_old_conversations())

        yield

    finally:
        # Shutdown
        logger.info("Shutting down AI WhatsApp Bot Service...")
        if ollama_client:
            await ollama_client.close()
            logger.info("Ollama client closed")


# Create FastAPI app
app = FastAPI(
    title="AI WhatsApp Bot Service",
    description="AI-powered WhatsApp bot using Ollama",
    version="1.0.0",
    lifespan=lifespan,
)

# Add CORS middleware (restrict for production)
app.add_middleware(
    CORSMiddleware,
    allow_origins=[
        "http://localhost",
        "https://your-production-domain.com",
    ],  # Update for production
    allow_credentials=True,
    allow_methods=["GET", "POST", "DELETE"],
    allow_headers=["X-API-Key", "Content-Type"],
)


# API Models
class ProcessMessageRequest(BaseModel):
    """Request to process a message."""

    message: Message
    user_profile: Optional[Dict[str, Any]] = None
    group_context: Optional[Dict[str, Any]] = None


class ProcessMessageResponse(BaseModel):
    """Response from message processing."""

    success: bool
    ai_response: Optional[AIResponse] = None
    conversation: Optional[Conversation] = None
    error: Optional[str] = None


class HealthResponse(BaseModel):
    """Health check response."""

    status: str
    ollama_healthy: bool
    service_version: str
    active_conversations: int
    available_models: List[str]


# API Endpoints
@app.get("/", response_model=Dict[str, str])
async def root():
    """Root endpoint."""
    return {
        "service": "AI WhatsApp Bot Service",
        "version": "1.0.0",
        "status": "running",
    }


@app.get("/health", response_model=HealthResponse)
async def health_check():
    """Health check endpoint."""
    ollama_healthy = False
    available_models = []

    if ollama_client:
        ollama_healthy = await ollama_client.health_check()
        try:
            available_models = await ollama_client.get_available_models()
        except Exception as e:
            logger.error("Failed to fetch available models: %s", e)

    status = "healthy" if ollama_healthy else "unhealthy"
    return HealthResponse(
        status=status,
        ollama_healthy=ollama_healthy,
        service_version="1.0.0",
        active_conversations=len(conversations),
        available_models=available_models,
    )


@app.post("/api/process-message", response_model=ProcessMessageResponse)
async def process_message(request: ProcessMessageRequest):
    """Process an incoming message and generate AI response."""
    try:
        # Validate input
        if not request.message.chat_id:
            raise ValueError("Message chat_id cannot be empty")

        # Get or create conversation
        chat_id = request.message.chat_id
        if chat_id not in conversations:
            conversations[chat_id] = Conversation(
                chat_id=chat_id,
                is_group=request.message.is_group,
                group_name=request.message.group_name,
                participants=[request.message.sender_id]
                if request.message.sender_id
                else [],
            )

        conversation = conversations[chat_id]

        # Process message with AI
        ai_response = await ai_service.process_message(
            message=request.message,
            conversation=conversation,
            user_profile=request.user_profile,
            group_context=request.group_context,
        )

        logger.info(
            "Processed message for chat %s from sender %s",
            chat_id,
            request.message.sender_id,
        )
        return ProcessMessageResponse(
            success=True,
            ai_response=ai_response,
            conversation=conversation,
        )

    except ValidationError as e:
        logger.error("Validation error processing message: %s", e)
        raise HTTPException(status_code=422, detail=str(e))
    except Exception as e:
        logger.error("Error processing message: %s", e)
        return ProcessMessageResponse(
            success=False,
            ai_response=None,
            conversation=conversations.get(chat_id),
            error=str(e),
        )


@app.get("/api/conversations/{chat_id}", response_model=Conversation)
async def get_conversation(chat_id: str):
    """Get conversation by chat ID."""
    if chat_id not in conversations:
        raise HTTPException(status_code=404, detail="Conversation not found")
    return conversations[chat_id]


@app.get("/api/conversations", response_model=Dict[str, Conversation])
async def list_conversations():
    """List all conversations."""
    return conversations


@app.delete("/api/conversations/{chat_id}", response_model=Dict[str, Any])
async def delete_conversation(chat_id: str):
    """Delete a conversation."""
    if chat_id not in conversations:
        raise HTTPException(status_code=404, detail="Conversation not found")

    del conversations[chat_id]
    logger.info("Deleted conversation: %s", chat_id)
    return {"success": True, "message": f"Conversation {chat_id} deleted"}


@app.post("/api/conversations/{chat_id}/clear", response_model=Dict[str, Any])
async def clear_conversation_context(chat_id: str):
    """Clear conversation context."""
    if chat_id not in conversations:
        raise HTTPException(status_code=404, detail="Conversation not found")

    conversations[chat_id].context = ""
    conversations[chat_id].messages = []
    logger.info("Cleared context for conversation: %s", chat_id)
    return {"success": True, "message": f"Context cleared for conversation {chat_id}"}


@app.get("/api/models", response_model=Dict[str, List[str]])
async def list_models():
    """List available Ollama models."""
    if not ollama_client:
        raise HTTPException(status_code=503, detail="Ollama client not initialized")

    try:
        models = await ollama_client.get_available_models()
        return {"models": models}
    except Exception as e:
        logger.error("Error listing models: %s", e)
        raise HTTPException(status_code=500, detail=f"Error listing models: {str(e)}")


async def cleanup_old_conversations():
    """Clean up old conversations to prevent memory leaks."""
    while True:
        try:
            current_time = datetime.now(timezone.utc)
            conversations_to_remove = []

            for chat_id, conversation in conversations.items():
                time_diff = (
                    current_time - conversation.last_updated
                ).total_seconds() / 3600
                if (
                    time_diff > settings.conversation_timeout_hours
                    or len(conversation.messages) == 0
                ):
                    conversations_to_remove.append(chat_id)

            for chat_id in conversations_to_remove:
                del conversations[chat_id]
                logger.info("Cleaned up conversation: %s", chat_id)

            await asyncio.sleep(settings.cleanup_interval_hours * 3600)

        except Exception as e:
            logger.error("Error in cleanup task: %s", e)
            await asyncio.sleep(settings.cleanup_interval_hours * 3600)


if __name__ == "__main__":
    uvicorn.run(
        "main:app",
        host=settings.app_host,
        port=settings.app_port,
        log_level="info",
        reload=settings.app_host == "0.0.0.0",  # Enable reload only for local dev
    )
