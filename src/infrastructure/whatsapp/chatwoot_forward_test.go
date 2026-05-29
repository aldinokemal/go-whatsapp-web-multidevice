package whatsapp

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

func TestChatwootMessageTypeFromPayload(t *testing.T) {
	tests := []struct {
		name     string
		isFromMe any
		expected string
	}{
		{
			name:     "incoming message",
			isFromMe: false,
			expected: "incoming",
		},
		{
			name:     "outgoing message",
			isFromMe: true,
			expected: "outgoing",
		},
		{
			name:     "missing field defaults to incoming",
			isFromMe: nil,
			expected: "incoming",
		},
		{
			name:     "non-bool field defaults to incoming",
			isFromMe: "true",
			expected: "incoming",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{}
			if tc.isFromMe != nil {
				payload["is_from_me"] = tc.isFromMe
			}

			if got := chatwootMessageTypeFromPayload(payload); got != tc.expected {
				t.Fatalf("expected %s, got %s", tc.expected, got)
			}
		})
	}
}

func TestShouldForwardEventToChatwoot(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
		expected  bool
	}{
		{name: "message supported", eventName: "message", expected: true},
		{name: "message reaction supported", eventName: "message.reaction", expected: true},
		{name: "message edited unsupported", eventName: "message.edited", expected: false},
		{name: "message revoked unsupported", eventName: "message.revoked", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldForwardEventToChatwoot(tc.eventName); got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestIsEventWhitelistedForChatwoot(t *testing.T) {
	originalEvents := config.WhatsappWebhookEvents
	defer func() { config.WhatsappWebhookEvents = originalEvents }()

	t.Run("empty whitelist allows all", func(t *testing.T) {
		config.WhatsappWebhookEvents = nil
		if !isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be allowed when whitelist is empty")
		}
	})

	t.Run("explicit reaction whitelist allowed", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message.reaction"}
		if !isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be allowed when explicitly whitelisted")
		}
	})

	t.Run("message whitelist also allows reactions for chatwoot", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message"}
		if !isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be allowed for Chatwoot when message is whitelisted")
		}
	})

	t.Run("unrelated whitelist blocks reaction", func(t *testing.T) {
		config.WhatsappWebhookEvents = []string{"message.ack"}
		if isEventWhitelistedForChatwoot("message.reaction") {
			t.Fatal("expected message.reaction to be blocked for Chatwoot when not covered by whitelist")
		}
	})
}

func TestBuildReactionChatwootContent(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		isGroup  bool
		fromName string
		expected string
	}{
		{
			name: "group reaction leads with sender name (message threaded, no raw id)",
			payload: map[string]any{
				"reaction":           "👍",
				"reacted_message_id": "wamid-123",
			},
			isGroup:  true,
			fromName: "Alice",
			expected: "Alice reacted 👍",
		},
		{
			name: "direct reaction omits actor name (contact is the conversation)",
			payload: map[string]any{
				"reaction":           "🔥",
				"reacted_message_id": "wamid-456",
				"from":               "628123456789@s.whatsapp.net",
			},
			isGroup:  false,
			fromName: "",
			expected: "reacted 🔥",
		},
		{
			name: "reaction removal in group",
			payload: map[string]any{
				"reaction":           "",
				"reacted_message_id": "wamid-789",
			},
			isGroup:  true,
			fromName: "Bob",
			expected: "Bob removed a reaction",
		},
		{
			name: "group reaction falls back to sender jid when pushname missing",
			payload: map[string]any{
				"reaction":           "😂",
				"reacted_message_id": "wamid-999",
				"from":               "628777000111@s.whatsapp.net",
			},
			isGroup:  true,
			fromName: "",
			expected: "628777000111 reacted 😂",
		},
		{
			name: "missing target id still produces readable text",
			payload: map[string]any{
				"reaction": "❤️",
			},
			isGroup:  true,
			fromName: "Carol",
			expected: "Carol reacted ❤️",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildReactionChatwootContent(tc.payload, tc.isGroup, tc.fromName); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
