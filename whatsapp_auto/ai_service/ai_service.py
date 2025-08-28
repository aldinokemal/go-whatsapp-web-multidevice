"""
Main AI service for WhatsApp bot
"""

import logging
from typing import List, Dict, Any, Optional
from datetime import datetime, timezone

from .models import AIResponse, Conversation, Message, AIConfig
from .ollama_client import OllamaClient

logger = logging.getLogger(__name__)

# Configure default logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)


class AIService:
    """Main AI service that integrates with Ollama for WhatsApp bot functionality"""

    def __init__(self, config: AIConfig, ollama_client: OllamaClient):
        """
        Initialize AI service with configuration and Ollama client.

        Args:
            config: AI configuration settings
            ollama_client: Client for interacting with Ollama API
        """
        self.config = config
        self.ollama = ollama_client
        self.conversation_templates = self._load_conversation_templates()
        logger.info("AIService initialized with model: %s", config.model_name)

    def _load_conversation_templates(self) -> Dict[str, str]:
        """Load conversation templates for different scenarios."""
        return {
            "default": """You are Grok, a helpful AI assistant integrated with WhatsApp.
            Provide concise, conversational, and context-aware responses. Ask follow-up questions when appropriate.

            Current conversation context: {context}

            Recent messages:
            {recent_messages}

            User message: {user_message}

            Respond as Grok:""",

            "group": """You are Grok, an AI assistant in a WhatsApp group chat.
            Be concise and only respond when directly mentioned (@Grok) or when the message clearly requires assistance.
            Consider the group context and multiple participants.

            Group: {group_name}
            Group context: {group_context}
            Recent messages: {recent_messages}

            User message: {user_message}

            Respond appropriately:""",

            "question": """You are Grok, a helpful AI assistant.
            Provide a clear, accurate answer to the user's question. Ask follow-up questions if more information is needed.

            Context: {context}
            Question: {user_message}

            Answer:"""
        }

    async def process_message(
        self,
        message: Message,
        conversation: Conversation,
        user_profile: Optional[Dict[str, Any]] = None,
        group_context: Optional[Dict[str, Any]] = None
    ) -> AIResponse:
        """
        Process an incoming message and generate an AI response.

        Args:
            message: Incoming WhatsApp message
            conversation: Current conversation context
            user_profile: Optional user profile information
            group_context: Optional group-specific context

        Returns:
            AIResponse: Generated AI response

        Raises:
            ValueError: If message or conversation is invalid
        """
        if not message or not conversation:
            logger.error("Invalid input: message or conversation is None")
            raise ValueError("Message and conversation must not be None")

        try:
            # Update conversation with new message
            conversation.add_message(message)
            logger.debug("Processing message ID %s in chat %s", message.id, conversation.chat_id)

            # Determine conversation template
            template_key = self._select_template_key(message, conversation)

            # Build context for AI
            context = self._build_context(conversation, message, user_profile, group_context)

            # Generate AI response
            ai_response = await self._generate_ai_response(
                message, conversation, context, template_key
            )

            # Update conversation context
            conversation.context = ai_response.context
            conversation.last_updated = datetime.now(timezone.utc)

            logger.info("Generated response for message ID %s with confidence %.2f", message.id, ai_response.confidence)
            return ai_response

        except Exception as e:
            logger.error("Error processing message ID %s: %s", message.id, e)
            return AIResponse(
                text="Sorry, I'm having trouble processing your message. Please try again.",
                should_reply=True,
                confidence=0.0,
                context=conversation.context,
                reasoning=f"Error occurred: {str(e)}"
            )

    def _select_template_key(self, message: Message, conversation: Conversation) -> str:
        """Select appropriate conversation template based on message and context."""
        if conversation.is_group:
            return "group"
        if "?" in message.text or any(word in message.text.lower() for word in ["what", "how", "why", "when", "where", "who"]):
            return "question"
        return "default"

    def _build_context(
        self,
        conversation: Conversation,
        message: Message,
        user_profile: Optional[Dict[str, Any]] = None,
        group_context: Optional[Dict[str, Any]] = None
    ) -> str:
        """
        Build context string for AI processing.

        Args:
            conversation: Current conversation
            message: Incoming message
            user_profile: Optional user profile information
            group_context: Optional group-specific context

        Returns:
            str: Formatted context string
        """
        context_parts = []

        # Add conversation context
        if conversation.context:
            context_parts.append(f"Previous context: {conversation.context}")

        # Add recent messages context
        recent_messages = conversation.get_recent_messages(count=5)
        if recent_messages:
            msg_context = []
            for msg in recent_messages:
                sender = msg.sender_id
                if msg.is_group and msg.group_name:
                    sender = f"{msg.sender_id} in {msg.group_name}"
                msg_context.append(f"{sender} ({msg.timestamp:%Y-%m-%d %H:%M:%S}): {msg.text}")
            context_parts.append("Recent messages:\n" + "\n".join(msg_context))

        # Add user profile context
        if user_profile and user_profile.get("name"):
            language = user_profile.get("language", "en")
            context_parts.append(f"User: {user_profile['name']} (Language: {language})")

        # Add group context
        if group_context and conversation.is_group:
            group_name = group_context.get("name", "Unknown")
            context_parts.append(f"Group: {group_name}")
            if group_context.get("topic"):
                context_parts.append(f"Topic: {group_context['topic']}")

        return "\n".join(context_parts) or "No context available"

    async def _generate_ai_response(
        self,
        message: Message,
        conversation: Conversation,
        context: str,
        template_key: str
    ) -> AIResponse:
        """
        Generate AI response using Ollama.

        Args:
            message: Incoming message
            conversation: Current conversation
            context: Formatted context string
            template_key: Conversation template to use

        Returns:
            AIResponse: Generated response with metadata
        """
        template = self.conversation_templates.get(template_key, self.conversation_templates["default"])
        prompt = template.format(
            context=context,
            recent_messages=context,
            user_message=message.text,
            group_name=conversation.group_name or "Unknown",
            group_context=conversation.context
        )

        try:
            ollama_response = await self.ollama.generate_response(
                prompt=prompt,
                model=self.config.model_name,
                temperature=self.config.temperature,
                max_tokens=self.config.max_tokens
            )

            response_text = ollama_response.response.strip()
            questions = self._extract_questions(response_text)
            should_reply = self._should_reply(response_text, message, conversation)
            confidence = self._calculate_confidence(response_text, message)

            return AIResponse(
                text=response_text,
                should_reply=should_reply,
                questions=questions,
                context=context,
                confidence=confidence,
                reasoning=f"Generated using {self.config.model_name} with temperature {self.config.temperature}"
            )

        except Exception as e:
            logger.error("Failed to generate AI response: %s", e)
            raise

    def _extract_questions(self, text: str) -> List[str]:
        """
        Extract questions from AI response.

        Args:
            text: AI response text

        Returns:
            List[str]: List of extracted questions
        """
        if not self.config.enable_questions:
            return []

        questions = []
        lines = text.split("\n")
        question_indicators = {"what", "how", "why", "when", "where", "who", "can", "would", "could"}

        for line in lines:
            line = line.strip()
            if line.endswith("?") and any(indicator in line.lower() for indicator in question_indicators):
                questions.append(line)

        return questions[:3]  # Limit to 3 questions to avoid overwhelming users

    def _should_reply(self, response: str, message: Message, conversation: Conversation) -> bool:
        """
        Determine if AI should send a reply.

        Args:
            response: Generated AI response
            message: Incoming message
            conversation: Current conversation

        Returns:
            bool: Whether the AI should reply
        """
        # Don't reply to own messages
        if message.sender_id == "self":
            logger.debug("Skipping reply to own message")
            return False

        # Check if AI is disabled
        if not conversation.ai_enabled:
            logger.debug("AI disabled for conversation %s", conversation.chat_id)
            return False

        # Check confidence threshold
        confidence = self._calculate_confidence(response, message)
        if confidence < self.config.auto_reply_threshold:
            logger.debug("Confidence %.2f below threshold %.2f", confidence, self.config.auto_reply_threshold)
            return False

        # Group-specific logic
        if conversation.is_group:
            message_lower = message.text.lower()
            if "@grok" in message_lower or "grok" in message_lower:
                return True
            if "?" in message.text or any(word in message_lower for word in ["help", "assist", "support"]):
                return True
            logger.debug("No direct mention or question in group message")
            return False

        return True

    def _calculate_confidence(self, response: str, message: Message) -> float:
        """
        Calculate confidence score for AI response.

        Args:
            response: Generated AI response
            message: Incoming message

        Returns:
            float: Confidence score between 0.0 and 1.0
        """
        confidence = 0.8  # Base confidence

        # Adjust based on response length
        response_length = len(response)
        if response_length > 50:
            confidence += 0.1
        elif response_length < 10:
            confidence -= 0.2

        # Adjust based on relevance to message
        message_words = set(message.text.lower().split())
        response_words = set(response.lower().split())
        if message_words & response_words:  # Check for overlapping words
            confidence += 0.1

        # Adjust for question-specific responses
        if "?" in message.text and "?" in response:
            confidence += 0.05

        return min(1.0, max(0.0, confidence))