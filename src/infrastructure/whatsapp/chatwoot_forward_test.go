package whatsapp

import "testing"

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

func TestBuildReactionChatwootContent(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		isGroup  bool
		fromName string
		expected string
	}{
		{
			name: "group reaction with sender name and target id",
			payload: map[string]interface{}{
				"reaction":           "👍",
				"reacted_message_id": "wamid-123",
			},
			isGroup:  true,
			fromName: "Alice",
			expected: "Alice reacted 👍 to message wamid-123",
		},
		{
			name: "direct reaction falls back to phone",
			payload: map[string]interface{}{
				"reaction":           "🔥",
				"reacted_message_id": "wamid-456",
				"from":               "628123456789@s.whatsapp.net",
			},
			isGroup:  false,
			fromName: "",
			expected: "628123456789 reacted 🔥 to message wamid-456",
		},
		{
			name: "reaction removal",
			payload: map[string]interface{}{
				"reaction":           "",
				"reacted_message_id": "wamid-789",
			},
			isGroup:  true,
			fromName: "Bob",
			expected: "Bob removed a reaction from message wamid-789",
		},
		{
			name: "missing target id still produces readable text",
			payload: map[string]interface{}{
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
