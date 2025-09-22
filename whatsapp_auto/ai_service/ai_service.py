"""
Main AI service for WhatsApp bot (improved)
- Safer logging (no basicConfig at import)
- Correct use of recent_messages vs. context in templates
- Robust normalization of model output (handles JSON string literals, code fences, empties)
- Tighter prompt construction with truncation to avoid context bloat
- Clearer should_reply logic with configurable assistant mentions
- More precise typing + docstrings
"""

from __future__ import annotations

import logging
import re
import textwrap
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

from .models import AIConfig, AIResponse, Conversation, Message
from .ollama_client import OllamaClient

logger = logging.getLogger(__name__)

# ---------- Tunables ----------
MAX_CONTEXT_CHARS = 4000  # hard cap for context fed to the model
MAX_MSG_SNIPPET = 240  # per-message truncation when building "recent messages"
RECENT_COUNT = 6  # how many recent messages to include
QUESTION_WORDS = {
    "what",
    "how",
    "why",
    "when",
    "where",
    "who",
    "can",
    "would",
    "could",
    "which",
    "do",
    "does",
    "did",
    "is",
    "are",
    "am",
    "should",
    "will",
}


class AIService:
    """Main AI service that integrates with Ollama for WhatsApp bot functionality."""

    def __init__(self, config: AIConfig, ollama_client: OllamaClient):
        """Initialize AI service with configuration and Ollama client.

        Args:
            config: AI configuration settings
            ollama_client: Client for interacting with Ollama API
        """
        self.config = config
        self.ollama = ollama_client
        self.conversation_templates = self._load_conversation_templates()

        # Optional: list of names/aliases that should trigger replies in groups
        self.assistant_names = {
            n.lower()
            for n in getattr(config, "assistant_names", ["Jason"])
            if isinstance(n, str)
        }
        if not self.assistant_names:
            self.assistant_names = {"Jason"}

        logger.info("AIService initialized with model: %s", config.model_name)

    # ---------- Templates ----------
    def _load_conversation_templates(self) -> Dict[str, str]:
        """Load conversation templates for different scenarios."""
        return {
            "default": textwrap.dedent(
                """
                You are Jason, a helpful WhatsApp AI assistant. Be concise, friendly, and conversational.
                IMPORTANT: Respond ONLY with your message content. Do NOT include any instructions, context, or formatting.

                Context:
                {context}

                Recent messages:
                {recent_messages}

                User message: {user_message}

                Your response (just the message, no formatting):
                """
            ).strip(),
            "group": textwrap.dedent(
                """
                You are Jason, a group assistant. Only respond when you're mentioned or help is requested.
                Keep replies under 2 sentences unless clarification is needed.
                IMPORTANT: Respond ONLY with your message content. Do NOT include any instructions, context, or formatting.

                Group: {group_name}

                Context:
                {context}

                Recent messages:
                {recent_messages}

                User message: {user_message}

                Your response (just the message, no formatting):
                """
            ).strip(),
            "question": textwrap.dedent(
                """
                You are Jason. Answer questions clearly and concisely. If needed, ask one short follow-up.
                IMPORTANT: Respond ONLY with your answer. Do NOT include any instructions, context, or formatting.

                Context:
                {context}

                Recent messages:
                {recent_messages}

                Question: {user_message}

                Your answer (just the answer, no formatting):
                """
            ).strip(),
        }

    # ---------- Public API ----------
    async def process_message(
        self,
        message: Message,
        conversation: Conversation,
        user_profile: Optional[Dict[str, Any]] = None,
        group_context: Optional[Dict[str, Any]] = None,
    ) -> AIResponse:
        """Process an incoming message and generate an AI response.

        Raises:
            ValueError: If message or conversation is invalid
        """
        if not message or not conversation:
            logger.error("Invalid input: message or conversation is None")
            raise ValueError("Message and conversation must not be None")

        try:
            logger.info(
                "Processing message id=%s type=%s", message.id, message.message_type
            )
            logger.debug("Message text: %r", message.text)

            # Update rolling conversation
            conversation.add_message(message)

            # Choose template
            template_key = self._select_template_key(message, conversation)
            logger.debug("Selected template: %s", template_key)

            # Build context + recent separately (bugfix: don't reuse the same value)
            context_str, recent_str = self._build_context_and_recent(
                conversation, message, user_profile, group_context
            )

            # Generate response
            ai_response = await self._generate_ai_response(
                message=message,
                conversation=conversation,
                context=context_str,
                recent_messages=recent_str,
                template_key=template_key,
            )

            # Update conversation metadata
            conversation.context = ai_response.context
            conversation.last_updated = datetime.now(timezone.utc)

            logger.info(
                "Generated response for message id=%s (confidence=%.2f; should_reply=%s)",
                message.id,
                ai_response.confidence,
                ai_response.should_reply,
            )
            logger.debug("AI Response text: %r", ai_response.text)
            return ai_response

        except Exception as e:
            logger.exception(
                "Error processing message id=%s", getattr(message, "id", "<unknown>")
            )
            return AIResponse(
                text="Sorry, I'm having trouble processing your message. Please try again.",
                should_reply=True,
                confidence=0.0,
                context=conversation.context,
                reasoning=f"Error occurred: {e}",
            )

    # ---------- Helpers ----------
    def _select_template_key(self, message: Message, conversation: Conversation) -> str:
        """Select appropriate conversation template based on message and context."""
        if conversation.is_group:
            return "group"
        text = (message.text or "").lower()
        if "?" in text or any(w in text.split() for w in QUESTION_WORDS):
            return "question"
        return "default"

    def _build_context_and_recent(
        self,
        conversation: Conversation,
        message: Message,
        user_profile: Optional[Dict[str, Any]] = None,
        group_context: Optional[Dict[str, Any]] = None,
    ) -> Tuple[str, str]:
        """Compose the long-lived context and a short recent-messages block.

        Returns:
            Tuple[str, str]: (context, recent_messages)
        """
        parts: List[str] = []

        # Long-lived context
        if conversation.context:
            parts.append(
                _truncate(
                    f"Previous context: {conversation.context}", MAX_CONTEXT_CHARS // 2
                )
            )

        # User profile
        if user_profile and user_profile.get("name"):
            language = user_profile.get("language", "en")
            parts.append(f"User: {user_profile['name']} (Language: {language})")

        # Group info
        if group_context and conversation.is_group:
            group_name = group_context.get("name", conversation.group_name or "Unknown")
            parts.append(f"Group: {group_name}")
            topic = group_context.get("topic")
            if topic:
                parts.append(_truncate(f"Topic: {topic}", MAX_CONTEXT_CHARS // 4))

        # Join the long-lived context
        context_str = _hard_cap("\n".join(parts), MAX_CONTEXT_CHARS)

        # Recent messages (last N)
        recent_msgs = conversation.get_recent_messages(count=RECENT_COUNT)
        recent_lines: List[str] = []
        for m in recent_msgs:
            sender = m.sender_id
            if m.is_group and m.group_name:
                sender = f"{sender} in {m.group_name}"
            snippet = _one_line(_truncate(m.text or "", MAX_MSG_SNIPPET))
            ts = (
                m.timestamp.astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S %Z")
                if m.timestamp
                else ""
            )
            recent_lines.append(f"{sender} ({ts}): {snippet}")
        recent_str = "\n".join(recent_lines) or "(no recent messages)"

        return context_str, recent_str

    async def _generate_ai_response(
        self,
        message: Message,
        conversation: Conversation,
        context: str,
        recent_messages: str,
        template_key: str,
    ) -> AIResponse:
        """Generate AI response using Ollama."""
        template = self.conversation_templates.get(
            template_key, self.conversation_templates["default"]
        )
        prompt = template.format(
            context=context,
            recent_messages=recent_messages,
            user_message=message.text,
            group_name=conversation.group_name or "Unknown",
            group_context=conversation.context or "",
        )

        try:
            ollama_response = await self.ollama.generate_response(
                prompt=prompt,
                model=self.config.model_name,
                temperature=self.config.temperature,
                max_tokens=self.config.max_tokens,
                session_id=conversation.chat_id,
                keep_alive="5m",
                stop=[
                    "\nUser:",
                    "\nContext:",
                    "\nRecent messages:",
                    "\nYour response",
                    "\nYour answer",
                    "IMPORTANT:",
                    "Do NOT include",
                ],
            )

            raw_text = getattr(ollama_response, "response", "") or ""
            response_text = _normalize_model_output(raw_text)

            # Derive metadata
            questions = self._extract_questions(response_text)
            should_reply = self._should_reply(response_text, message, conversation)
            confidence = self._calculate_confidence(response_text, message)

            # If empty after normalization, decide whether to reply
            if not response_text:
                # fall back to a minimal acknowledgment only in DMs; skip in groups
                if not conversation.is_group:
                    response_text = "I understand your message."
                    should_reply = True
                    confidence = min(confidence, 0.4)
                else:
                    should_reply = False

            # Final cap on response length (hard safety)
            response_text = _hard_cap(response_text, self.config.max_tokens * 4)

            return AIResponse(
                text=response_text,
                should_reply=should_reply,
                questions=questions,
                context=context,
                confidence=confidence,
                reasoning=f"Generated using {self.config.model_name} (temp={self.config.temperature})",
            )

        except Exception as e:
            logger.exception("Failed to generate AI response for message %s", message.id)
            
            # Handle specific timeout errors
            if "timeout" in str(e).lower() or "deadline" in str(e).lower():
                logger.warning("Ollama request timed out for message %s, using fallback response", message.id)
                return AIResponse(
                    text="I'm processing your message, but it's taking longer than expected. Please wait a moment.",
                    should_reply=True,
                    questions=[],
                    context=context,
                    confidence=0.3,
                    reasoning=f"Timeout fallback response for {self.config.model_name}",
                )
            
            # Handle other errors
            logger.error("Unexpected error in AI generation: %s", e)
            raise

    # ---------- Post-processing ----------
    def _extract_questions(self, text: str) -> List[str]:
        """Extract up to 3 questions from AI response if enabled in config."""
        if not self.config.enable_questions or not text:
            return []
        qs: List[str] = []
        for line in text.splitlines():
            s = line.strip()
            if s.endswith("?") and any(w in s.lower() for w in QUESTION_WORDS):
                qs.append(s)
            if len(qs) == 3:
                break
        return qs

    def _should_reply(
        self, response: str, message: Message, conversation: Conversation
    ) -> bool:
        """Decide whether the bot should reply to this message."""
        # Don't reply to self
        if getattr(message, "sender_id", None) == "self":
            logger.debug("Skipping reply to own message")
            return False

        # AI toggled off
        if not conversation.ai_enabled:
            logger.debug("AI disabled for conversation %s", conversation.chat_id)
            return False

        # Confidence gate
        confidence = self._calculate_confidence(response or "", message)
        if confidence < getattr(self.config, "auto_reply_threshold", 0.0):
            logger.debug(
                "Confidence %.2f below threshold %.2f",
                confidence,
                self.config.auto_reply_threshold,
            )
            return False

        # Group-specific rules
        if conversation.is_group:
            text = (message.text or "").lower()
            mentioned = any(name in text for name in self.assistant_names)
            is_question = ("?" in text) or any(
                w in text.split() for w in QUESTION_WORDS
            )
            wants_help = any(w in text for w in ("help", "assist", "support"))
            return bool(mentioned or is_question or wants_help)

        return True

    def _calculate_confidence(self, response: str, message: Message) -> float:
        """Heuristic confidence score between 0.0 and 1.0."""
        if not response:
            return 0.0

        confidence = 0.75  # base

        # Length-based adjustments
        n = len(response)
        if n > 50:
            confidence += 0.08
        elif n < 10:
            confidence -= 0.25

        # Overlap heuristic (very light)
        msg_words = set(re.findall(r"[\w']+", (message.text or "").lower()))
        resp_words = set(re.findall(r"[\w']+", response.lower()))
        if msg_words and (len(msg_words & resp_words) / max(1, len(msg_words))) >= 0.1:
            confidence += 0.07

        # Questions parity
        if "?" in (message.text or "") and "?" in response:
            confidence += 0.03

        return max(0.0, min(1.0, confidence))


# ---------- Utility functions ----------


def _truncate(s: str, limit: int) -> str:
    if len(s) <= limit:
        return s
    return s[: max(0, limit - 1)] + "…"


def _hard_cap(s: str, limit: int) -> str:
    """Strictly cap a string; avoids huge payloads to the model."""
    if limit <= 0:
        return ""
    return s if len(s) <= limit else s[:limit]


def _one_line(s: str) -> str:
    return re.sub(r"\s+", " ", s).strip()


def _strip_code_fences(s: str) -> str:
    """Remove triple backtick fences and optional language tags."""
    s = s.strip()
    if s.startswith("```"):
        # remove opening fence with optional lang
        s = re.sub(r"^```[a-zA-Z0-9_+-]*\n?", "", s, count=1)
        # remove closing fence if present
        s = re.sub(r"\n?```\s*$", "", s, count=1)
    return s.strip()


def _unquote_json_string_literal(s: str) -> str:
    """If the model returned a JSON string literal (e.g., "\"hello\"" or "\"\""), unquote it."""
    s = s.strip()
    if len(s) >= 2 and s[0] == '"' and s[-1] == '"':
        try:
            import json as _json

            return _json.loads(s)
        except Exception:
            pass
    return s


def _normalize_model_output(raw: str) -> str:
    """Normalize model output to a clean, plain text string.

    Handles:
    - JSON string literal outputs (returns inner text)
    - Code fences (```lang ... ```)
    - Empty or whitespace-only responses
    - Prompt artifacts and instructions
    """
    if not raw:
        return ""
    s = _strip_code_fences(raw)
    s = _unquote_json_string_literal(s)
    s = s.replace("\r", "").strip()

    # Occasionally the model returns an empty JSON string ("")
    if s == '""':
        s = ""

    # Remove common prompt artifacts and instructions
    prompt_artifacts = [
        "IMPORTANT:",
        "Do NOT include",
        "Your response",
        "Your answer",
        "Context:",
        "Recent messages:",
        "User message:",
        "Question:",
        "Group:",
        "Response:",
        "Answer:",
        "Message:",
    ]

    for artifact in prompt_artifacts:
        # Remove the artifact and everything after it
        if artifact in s:
            parts = s.split(artifact)
            s = parts[0].strip()
            break

    # Remove any remaining prompt-like patterns
    s = re.sub(r"^.*?(?=\w)", "", s, flags=re.DOTALL)  # Remove leading non-word content
    s = re.sub(
        r"\n\s*(?:Context|Recent|User|Group|Response|Answer|Message):.*$",
        "",
        s,
        flags=re.DOTALL,
    )

    # Collapse excessive blank lines
    s = re.sub(r"\n{3,}", "\n\n", s)

    # Final cleanup
    s = s.strip()

    # If we end up with nothing meaningful, return a default response
    if not s or len(s.strip()) < 2:
        return "I understand your message."

    return s
