"""
Data models for the AI WhatsApp Bot Service
"""

from datetime import datetime, timezone
from typing import List, Optional, Dict, Any
from pydantic import BaseModel, Field, field_validator
from enum import Enum


class MessageType(str, Enum):
    """Types of WhatsApp messages"""

    TEXT = "text"
    IMAGE = "image"
    AUDIO = "audio"
    VIDEO = "video"
    DOCUMENT = "document"
    LOCATION = "location"
    CONTACT = "contact"
    STICKER = "sticker"
    REACTION = "reaction"


class Message(BaseModel):
    """Represents a WhatsApp message"""

    id: str = Field(..., description="Unique message ID")
    text: Optional[str] = Field(None, description="Message text content or caption")
    sender_id: str = Field(..., description="Sender's phone number or ID")
    chat_id: str = Field(..., description="Chat/group ID")
    timestamp: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Message timestamp",
    )
    message_type: MessageType = Field(
        default=MessageType.TEXT, description="Type of message"
    )
    is_group: bool = Field(default=False, description="Whether this is a group message")
    group_name: Optional[str] = Field(
        None, description="Group name if it's a group message"
    )
    reply_to: Optional[str] = Field(None, description="ID of message being replied to")
    metadata: Dict[str, Any] = Field(
        default_factory=dict, description="Additional message metadata"
    )

    @field_validator("text", mode="before")
    @classmethod
    def validate_text(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return None
        stripped = v.strip()
        if not stripped:
            raise ValueError("Message text cannot be empty or whitespace-only")
        return stripped

    @field_validator("text")
    @classmethod
    def require_text_for_text_type(cls, v: Optional[str], info) -> Optional[str]:
        message_type = info.data.get("message_type")
        if message_type == MessageType.TEXT and v is None:
            raise ValueError("Text is required for TEXT message type")
        return v


class Conversation(BaseModel):
    """Represents a chat conversation with context"""

    chat_id: str = Field(..., description="Unique chat identifier")
    messages: List[Message] = Field(
        default_factory=list, description="List of messages in conversation"
    )
    last_updated: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Last activity timestamp",
    )
    context: str = Field(default="", description="AI-generated conversation context")
    is_group: bool = Field(
        default=False, description="Whether this is a group conversation"
    )
    group_name: Optional[str] = Field(
        None, description="Group name if it's a group conversation"
    )
    participants: List[str] = Field(
        default_factory=list, description="List of participant IDs"
    )
    ai_enabled: bool = Field(
        default=True, description="Whether AI responses are enabled for this chat"
    )
    max_context_length: int = Field(
        default=50, description="Maximum number of messages to keep in context"
    )

    def add_message(self, message: Message) -> None:
        """Add a message to the conversation and update context"""
        self.messages.append(message)
        self.last_updated = datetime.now(timezone.utc)

        # Keep only the last N messages for context management
        if len(self.messages) > self.max_context_length:
            self.messages = self.messages[-self.max_context_length :]

    def get_recent_messages(self, count: int = 10) -> List[Message]:
        """Get the most recent messages for context"""
        return self.messages[-count:] if self.messages else []


class AIResponse(BaseModel):
    """Represents the AI's response to a message"""

    text: str = Field(..., description="AI-generated response text")
    should_reply: bool = Field(
        default=True, description="Whether the AI should send a reply"
    )
    questions: List[str] = Field(
        default_factory=list, description="Follow-up questions from AI"
    )
    context: str = Field(default="", description="Updated conversation context")
    confidence: float = Field(
        default=1.0, ge=0.0, le=1.0, description="AI confidence in the response"
    )
    reasoning: Optional[str] = Field(
        None, description="AI's reasoning for the response"
    )
    suggested_actions: List[str] = Field(
        default_factory=list, description="Suggested actions for the user"
    )


class AIRequest(BaseModel):
    """Represents a request to the AI service"""

    message: Message = Field(..., description="Incoming message to process")
    conversation: Conversation = Field(..., description="Current conversation context")
    user_profile: Optional[Dict[str, Any]] = Field(
        None, description="User profile information"
    )
    group_context: Optional[Dict[str, Any]] = Field(
        None, description="Group-specific context"
    )
    ai_settings: Optional[Dict[str, Any]] = Field(
        None, description="AI behavior settings"
    )


class UserProfile(BaseModel):
    """Represents user information for context"""

    user_id: str = Field(..., description="Unique user identifier")
    name: str = Field(..., description="User's display name")
    phone: str = Field(..., description="User's phone number")
    language: str = Field(default="en", description="User's preferred language")
    timezone: str = Field(default="UTC", description="User's timezone")
    preferences: Dict[str, Any] = Field(
        default_factory=dict, description="User preferences"
    )
    ai_personality: Optional[str] = Field(
        None, description="Preferred AI personality for this user"
    )


class GroupContext(BaseModel):
    """Represents group-specific context"""

    group_id: str = Field(..., description="Unique group identifier")
    group_name: str = Field(..., description="Group display name")
    description: Optional[str] = Field(None, description="Group description")
    participants: List[str] = Field(
        default_factory=list, description="List of participant IDs"
    )
    rules: List[str] = Field(default_factory=list, description="Group rules")
    topic: Optional[str] = Field(None, description="Current group topic")
    ai_behavior: str = Field(
        default="helpful", description="How AI should behave in this group"
    )


class AIConfig(BaseModel):
    """Configuration for AI behavior"""

    model_name: str = Field(default="llama2", description="Ollama model to use")
    temperature: float = Field(
        default=0.7, ge=0.0, le=2.0, description="AI creativity level"
    )
    max_tokens: int = Field(default=500, description="Maximum response length")
    context_window: int = Field(
        default=50, description="Number of messages to include in context"
    )
    personality: str = Field(default="helpful", description="AI personality type")
    enable_questions: bool = Field(
        default=True, description="Whether AI can ask follow-up questions"
    )
    response_delay: float = Field(
        default=1.0, description="Delay before responding (seconds)"
    )
    auto_reply_threshold: float = Field(
        default=0.8, description="Confidence threshold for auto-replies"
    )
