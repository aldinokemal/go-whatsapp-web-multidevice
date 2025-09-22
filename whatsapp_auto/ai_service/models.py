"""
Data models for the AI WhatsApp Bot Service (improved)
- Pydantic v2 best practices (model_config, validators, serializers)
- Safer text handling: whitespace -> None; only TEXT type requires text
- Timezone safety: timestamps normalized to UTC
- Optional Media payload for non-text messages
- Conversation helpers: participants de-dup, typed recent fetch
- AIConfig aligned with service features (assistant_names, stop tokens, keep_alive, etc.)
"""
from __future__ import annotations

from datetime import datetime, timezone
from enum import Enum
from typing import Any, Dict, List, Optional, Sequence

from pydantic import BaseModel, Field, field_validator, model_validator
from pydantic.config import ConfigDict
from pydantic.functional_serializers import field_serializer


# ------------------------- Message types & media -------------------------
class MessageType(str, Enum):
    """Types of WhatsApp messages."""

    TEXT = "text"
    IMAGE = "image"
    AUDIO = "audio"
    VIDEO = "video"
    DOCUMENT = "document"
    LOCATION = "location"
    CONTACT = "contact"
    STICKER = "sticker"
    REACTION = "reaction"
    SYSTEM = "system"


class Media(BaseModel):
    """Optional media payload attached to a message."""

    url: Optional[str] = Field(None, description="Public/temporary URL to fetch the media")
    mime_type: Optional[str] = Field(None, description="Media MIME type")
    file_name: Optional[str] = Field(None, description="Original file name if known")
    sha256: Optional[str] = Field(None, description="Integrity checksum (hex)")
    size_bytes: Optional[int] = Field(None, ge=0, description="Size in bytes")
    width: Optional[int] = Field(None, ge=1, description="Image/video width in pixels")
    height: Optional[int] = Field(None, ge=1, description="Image/video height in pixels")
    duration_ms: Optional[int] = Field(None, ge=0, description="Audio/video duration in ms")

    model_config = ConfigDict(extra="ignore")


# ------------------------- Core message -------------------------
class Message(BaseModel):
    """Represents a WhatsApp message."""

    id: str = Field(..., description="Unique message ID")
    text: Optional[str] = Field(None, description="Message text content or caption")
    sender_id: str = Field(..., description="Sender's phone number or ID")
    chat_id: str = Field(..., description="Chat/group ID")
    timestamp: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Message timestamp (UTC)",
    )
    message_type: MessageType = Field(default=MessageType.TEXT, description="Type of message")
    is_group: bool = Field(default=False, description="Whether this is a group message")
    group_name: Optional[str] = Field(None, description="Group name if it's a group message")
    reply_to: Optional[str] = Field(None, description="ID of message being replied to")
    metadata: Dict[str, Any] = Field(default_factory=dict, description="Additional message metadata")
    media: Optional[Media] = Field(None, description="Optional media payload for non-text messages")

    # Pydantic v2 configuration
    model_config = ConfigDict(use_enum_values=True, validate_assignment=True, extra="ignore")

    # --- Validators & serializers ---
    @field_validator("timestamp", mode="before")
    @classmethod
    def _ensure_aware_utc(cls, v: datetime) -> datetime:
        """Normalize timestamps to timezone-aware UTC."""
        if v is None:
            return datetime.now(timezone.utc)
        if isinstance(v, datetime):
            if v.tzinfo is None:
                return v.replace(tzinfo=timezone.utc)
            return v.astimezone(timezone.utc)
        return datetime.now(timezone.utc)

    @field_validator("text", mode="before")
    @classmethod
    def _strip_or_none(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return None
        s = str(v).strip()
        return s or None

    @model_validator(mode="after")
    def _require_text_for_text_type(self) -> "Message":
        if self.message_type == MessageType.TEXT and (self.text is None or self.text == ""):
            raise ValueError("Text is required for TEXT message type")
        return self

    @field_serializer("timestamp")
    def _ser_timestamp(self, ts: datetime) -> str:
        return ts.astimezone(timezone.utc).isoformat()


# ------------------------- Conversation -------------------------
class Conversation(BaseModel):
    """Represents a chat conversation with context."""

    chat_id: str = Field(..., description="Unique chat identifier")
    messages: List[Message] = Field(default_factory=list, description="List of messages in conversation")
    last_updated: datetime = Field(default_factory=lambda: datetime.now(timezone.utc), description="Last activity timestamp (UTC)")
    context: str = Field(default="", description="AI-generated conversation context")
    is_group: bool = Field(default=False, description="Whether this is a group conversation")
    group_name: Optional[str] = Field(None, description="Group name if it's a group conversation")
    participants: List[str] = Field(default_factory=list, description="List of participant IDs")
    ai_enabled: bool = Field(default=True, description="Whether AI responses are enabled for this chat")
    max_context_length: int = Field(default=50, ge=1, description="Maximum number of messages to keep in context")

    model_config = ConfigDict(validate_assignment=True, extra="ignore")

    def add_message(self, message: Message) -> None:
        """Add a message and update metadata."""
        self.messages.append(message)
        self.last_updated = datetime.now(timezone.utc)
        if message.sender_id and message.sender_id not in self.participants:
            self.participants.append(message.sender_id)
        # Keep only the last N messages
        if len(self.messages) > self.max_context_length:
            self.messages = self.messages[-self.max_context_length :]

    def get_recent_messages(self, *, count: int = 10, types: Optional[Sequence[MessageType]] = None) -> List[Message]:
        """Get recent messages, optionally filtered by type."""
        if not self.messages:
            return []
        recent = self.messages[-max(1, count) :]
        if types:
            allowed = set(types)
            recent = [m for m in recent if m.message_type in allowed]
        return recent


# ------------------------- AI responses -------------------------
class AIResponse(BaseModel):
    """Represents the AI's response to a message."""

    text: str = Field(..., description="AI-generated response text")
    should_reply: bool = Field(default=True, description="Whether the AI should send a reply")
    questions: List[str] = Field(default_factory=list, description="Follow-up questions from AI")
    context: str = Field(default="", description="Updated conversation context")
    confidence: float = Field(default=1.0, ge=0.0, le=1.0, description="AI confidence in the response")
    reasoning: Optional[str] = Field(None, description="AI's reasoning for the response")
    suggested_actions: List[str] = Field(default_factory=list, description="Suggested actions for the user")

    model_config = ConfigDict(use_enum_values=True, validate_assignment=True, extra="ignore")


# ------------------------- Request wrappers -------------------------
class AIRequest(BaseModel):
    """Represents a request to the AI service."""

    message: Message = Field(..., description="Incoming message to process")
    conversation: Conversation = Field(..., description="Current conversation context")
    user_profile: Optional[Dict[str, Any]] = Field(None, description="User profile information")
    group_context: Optional[Dict[str, Any]] = Field(None, description="Group-specific context")
    ai_settings: Optional[Dict[str, Any]] = Field(None, description="AI behavior settings")

    model_config = ConfigDict(extra="ignore")


# ------------------------- Profiles & group context -------------------------
class UserProfile(BaseModel):
    """Represents user information for context."""

    user_id: str = Field(..., description="Unique user identifier")
    name: str = Field(..., description="User's display name")
    phone: str = Field(..., description="User's phone number")
    language: str = Field(default="en", description="User's preferred language")
    timezone: str = Field(default="UTC", description="User's timezone")
    preferences: Dict[str, Any] = Field(default_factory=dict, description="User preferences")
    ai_personality: Optional[str] = Field(None, description="Preferred AI personality for this user")

    model_config = ConfigDict(extra="ignore")


class GroupContext(BaseModel):
    """Represents group-specific context."""

    group_id: str = Field(..., description="Unique group identifier")
    group_name: str = Field(..., description="Group display name")
    description: Optional[str] = Field(None, description="Group description")
    participants: List[str] = Field(default_factory=list, description="List of participant IDs")
    rules: List[str] = Field(default_factory=list, description="Group rules")
    topic: Optional[str] = Field(None, description="Current group topic")
    ai_behavior: str = Field(default="helpful", description="How AI should behave in this group")

    model_config = ConfigDict(extra="ignore")


# ------------------------- AI config -------------------------
class AIConfig(BaseModel):
    """Configuration for AI behavior."""

    model_name: str = Field(default="llama3.2:3b-text-q4_K_M", description="Ollama model to use")
    temperature: float = Field(default=0.7, ge=0.0, le=2.0, description="AI creativity level")
    max_tokens: int = Field(default=500, description="Maximum response length (tokens)")
    context_window: int = Field(default=50, description="Number of messages to include in context (rolling)")
    personality: str = Field(default="helpful", description="AI personality type")
    enable_questions: bool = Field(default=True, description="Whether AI can ask follow-up questions")
    response_delay: float = Field(default=1.0, description="Delay before responding (seconds)")
    auto_reply_threshold: float = Field(default=0.8, ge=0.0, le=1.0, description="Confidence threshold for auto-replies")

    # New/optional knobs used by AIService/OllamaClient
    assistant_names: List[str] = Field(default_factory=lambda: ["grok"], description="Names/aliases that trigger replies in groups")
    stop_tokens: Optional[List[str]] = Field(default=None, description="Stop sequences for generation")
    keep_alive: Optional[str] = Field(default=None, description="Keep model loaded (e.g., '5m')")
    response_format: Optional[str] = Field(default=None, description="Ollama 'format' (e.g., 'json')")
    max_context_chars: int = Field(default=4000, ge=500, description="Hard cap on context string length sent to the model")

    model_config = ConfigDict(validate_assignment=True, extra="ignore")

    @field_validator("assistant_names", mode="before")
    @classmethod
    def _ensure_assistant_names_list(cls, v: Any) -> List[str]:
        """Ensure assistant_names is always a valid list."""
        if v is None:
            return ["Jason"]
        if isinstance(v, str):
            return [v.strip()] if v.strip() else ["Jason"]
        if isinstance(v, list):
            return [str(item).strip() for item in v if str(item).strip()] or ["Jason"]
        return ["Jason"]
