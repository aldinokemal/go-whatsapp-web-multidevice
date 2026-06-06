package whatsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

// TestSyncPayloadToChatwootFailFast verifies the forward path skips silently
// (no error, no retry) when the device resolves to no Chatwoot config, and
// propagates a transient resolution error so the caller can retry.
func TestSyncPayloadToChatwootFailFast(t *testing.T) {
	orig := getChatwootClientFn
	t.Cleanup(func() { getChatwootClientFn = orig })

	payload := map[string]any{"payload": map[string]any{"id": "wa-1", "chat_id": "628@s.whatsapp.net"}}

	// No config for this device -> skip, return nil.
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) { return nil, nil }
	if err := syncPayloadToChatwoot(context.Background(), payload, "message", "dev-unmapped", nil); err != nil {
		t.Fatalf("unmapped device should skip with nil, got %v", err)
	}

	// Transient resolution error -> propagate (retryable).
	wantErr := errors.New("storage down")
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) { return nil, wantErr }
	if err := syncPayloadToChatwoot(context.Background(), payload, "message", "dev", nil); !errors.Is(err, wantErr) {
		t.Fatalf("resolution error should propagate, got %v", err)
	}
}

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

func TestBuildChatwootForwardMessageLink(t *testing.T) {
	data := map[string]any{
		"id":         "wa-live-1",
		"chat_id":    "628123456789@s.whatsapp.net",
		"is_from_me": false,
	}

	link := buildChatwootForwardMessageLink(
		"device-a@s.whatsapp.net",
		42, // configID
		3,  // accountID
		data,
		chatwoot.MessageOptions{SourceID: "WAID:wa-live-1"},
		&chatwootSyncResult{MessageID: 123, ConversationID: 456, InboxID: 789},
	)

	if link == nil {
		t.Fatal("expected chatwoot message link")
	}
	if link.DeviceID != "device-a@s.whatsapp.net" {
		t.Fatalf("DeviceID = %q", link.DeviceID)
	}
	if link.WhatsAppMessageID != "wa-live-1" || link.SourceID != "WAID:wa-live-1" {
		t.Fatalf("unexpected source mapping: %+v", link)
	}
	if link.ChatwootMessageID != 123 || link.ChatwootConversationID != 456 || link.ChatwootInboxID != 789 {
		t.Fatalf("unexpected chatwoot ids: %+v", link)
	}
	if link.ChatwootConfigID != 42 || link.ChatwootAccountID != 3 {
		t.Fatalf("unexpected scope ids: config=%d account=%d", link.ChatwootConfigID, link.ChatwootAccountID)
	}
	if link.Direction != "incoming" {
		t.Fatalf("Direction = %q, want incoming", link.Direction)
	}
}

func TestExtractReceiptMessageIDs(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want []string
	}{
		{name: "string slice", in: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "any slice", in: []any{"a", "b", 3}, want: []string{"a", "b"}},
		{name: "single string", in: "a", want: []string{"a"}},
		{name: "empty", in: nil, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractReceiptMessageIDs(map[string]any{"ids": tc.in})
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ids = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestDeleteTargetMessageID(t *testing.T) {
	cases := []struct {
		event string
		data  map[string]any
		want  string
	}{
		{"message.revoked", map[string]any{"revoked_message_id": "wa-1"}, "wa-1"},
		{"message.deleted", map[string]any{"deleted_message_id": "wa-2"}, "wa-2"},
		{"message.deleted", map[string]any{"id": "wa-current"}, "wa-current"},
		{"message", map[string]any{"id": "wa-current"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			if got := deleteTargetMessageID(tc.event, tc.data); got != tc.want {
				t.Fatalf("deleteTargetMessageID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShouldForwardEventToChatwoot(t *testing.T) {
	// message and message.reaction are always forwarded; edits and deletes are
	// gated by their config toggles. Save/restore the globals so other tests in
	// the package see the defaults.
	origEdits := config.ChatwootForwardEdits
	origDeletes := config.ChatwootForwardDeletes
	origRead := config.ChatwootMessageRead
	origMsgDelete := config.ChatwootMessageDelete
	defer func() {
		config.ChatwootForwardEdits = origEdits
		config.ChatwootForwardDeletes = origDeletes
		config.ChatwootMessageRead = origRead
		config.ChatwootMessageDelete = origMsgDelete
	}()

	t.Run("always-on and unrelated events", func(t *testing.T) {
		config.ChatwootMessageRead = false
		cases := map[string]bool{
			"message":          true,
			"message.reaction": true,
			"message.ack":      false,
			"chat_presence":    false,
		}
		for event, want := range cases {
			if got := shouldForwardEventToChatwoot(event); got != want {
				t.Errorf("shouldForwardEventToChatwoot(%q) = %v, want %v", event, got, want)
			}
		}
	})

	t.Run("read receipts follow toggle", func(t *testing.T) {
		config.ChatwootMessageRead = true
		if !shouldForwardEventToChatwoot("message.ack") {
			t.Fatal("message.ack should forward when ChatwootMessageRead is enabled")
		}
		config.ChatwootMessageRead = false
		if shouldForwardEventToChatwoot("message.ack") {
			t.Fatal("message.ack should not forward when ChatwootMessageRead is disabled")
		}
	})

	t.Run("edits and deletes follow their toggles", func(t *testing.T) {
		config.ChatwootMessageDelete = false
		config.ChatwootForwardEdits = true
		config.ChatwootForwardDeletes = true
		for _, e := range []string{"message.edited", "message.revoked", "message.deleted"} {
			if !shouldForwardEventToChatwoot(e) {
				t.Errorf("%q should forward when toggles enabled", e)
			}
		}

		config.ChatwootForwardEdits = false
		config.ChatwootForwardDeletes = false
		for _, e := range []string{"message.edited", "message.revoked", "message.deleted"} {
			if shouldForwardEventToChatwoot(e) {
				t.Errorf("%q should NOT forward when toggles disabled", e)
			}
		}
	})

	t.Run("deletes forward when only ChatwootMessageDelete is enabled", func(t *testing.T) {
		// The hard-delete feature must reach the forwarder even when tombstone-note
		// forwarding (ChatwootForwardDeletes) is off, so the two are independent.
		// Edits remain gated solely on ChatwootForwardEdits.
		config.ChatwootForwardEdits = false
		config.ChatwootForwardDeletes = false
		config.ChatwootMessageDelete = true
		for _, e := range []string{"message.revoked", "message.deleted"} {
			if !shouldForwardEventToChatwoot(e) {
				t.Errorf("%q should forward when ChatwootMessageDelete is enabled", e)
			}
		}
		if shouldForwardEventToChatwoot("message.edited") {
			t.Error("message.edited should NOT forward on ChatwootMessageDelete alone")
		}
	})
}

func TestIsRetryableChatwootForwardEvent(t *testing.T) {
	// Base messages and their sub-events carry a unique WhatsApp id and are
	// retry-eligible; read receipts are best-effort and must not be queued.
	for _, e := range []string{"message", "message.edited", "message.revoked", "message.deleted", "message.reaction"} {
		if !isRetryableChatwootForwardEvent(e) {
			t.Errorf("%q should be retry-eligible", e)
		}
	}
	for _, e := range []string{"message.ack", "chat_presence", ""} {
		if isRetryableChatwootForwardEvent(e) {
			t.Errorf("%q should NOT be retry-eligible", e)
		}
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

func TestExtractMediaPath(t *testing.T) {
	// extractMediaPath handles the two shapes buildAutoDownloadPayload emits
	// for image/video/document fields. The map branch is the regression-prone
	// one: prior to its introduction, captioned images silently dropped their
	// attachment because the string assertion failed and the loop moved on.
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "plain string path (audio/sticker shape)",
			in:   "/tmp/whatsapp/audio-123.oga",
			want: "/tmp/whatsapp/audio-123.oga",
		},
		{
			name: "map with path key (captioned image/video/document shape)",
			in:   map[string]any{"path": "/tmp/whatsapp/img-1.jpg", "caption": "look at this"},
			want: "/tmp/whatsapp/img-1.jpg",
		},
		{
			name: "map with only url (auto-download disabled) yields empty",
			in:   map[string]any{"url": "https://wa.example/blob.jpg", "caption": "x"},
			want: "",
		},
		{
			name: "nil yields empty",
			in:   nil,
			want: "",
		},
		{
			name: "unrelated type yields empty",
			in:   42,
			want: "",
		},
		{
			name: "empty string returns empty (caller treats as no-op)",
			in:   "",
			want: "",
		},
		{
			name: "map with non-string path yields empty",
			in:   map[string]any{"path": 12345},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractMediaPath(tc.in); got != tc.want {
				t.Fatalf("extractMediaPath(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestChatwootIdentifierForJID(t *testing.T) {
	// chatwootIdentifierForJID guards the boundary into the Chatwoot REST
	// client. The client picks its identifier-vs-phone search path by
	// checking HasSuffix(identifier, "@lid"); stripping that suffix here
	// would silently downgrade @lid contacts to phone-normalized garbage.
	// Each case below pins a specific JID server / suffix so a regression
	// (e.g. someone reintroducing ExtractPhoneFromJID at the call site)
	// surfaces as a focused test failure rather than a downstream search miss.
	tests := []struct {
		name string
		jid  string
		want string
	}{
		{
			name: "ordinary phone JID is stripped to digits",
			jid:  "628123456789@s.whatsapp.net",
			want: "628123456789",
		},
		{
			name: "lid JID is preserved end-to-end so chatwoot client takes identifier path",
			jid:  "1234567890abcd@lid",
			want: "1234567890abcd@lid",
		},
		{
			name: "raw digits without @ pass through untouched",
			jid:  "628123456789",
			want: "628123456789",
		},
		{
			name: "empty string yields empty",
			jid:  "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatwootIdentifierForJID(tc.jid); got != tc.want {
				t.Fatalf("chatwootIdentifierForJID(%q) = %q, want %q", tc.jid, got, tc.want)
			}
		})
	}
}

func TestBuildReactionChatwootContent(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		fromName string
		expected string
	}{
		{
			name: "reaction with sender name and target id",
			payload: map[string]any{
				"reaction":           "👍",
				"reacted_message_id": "wamid-123",
			},
			fromName: "Alice",
			expected: "Alice reacted 👍 to message wamid-123",
		},
		{
			name: "reaction falls back to phone",
			payload: map[string]any{
				"reaction":           "🔥",
				"reacted_message_id": "wamid-456",
				"from":               "628123456789@s.whatsapp.net",
			},
			fromName: "",
			expected: "628123456789 reacted 🔥 to message wamid-456",
		},
		{
			name: "reaction removal",
			payload: map[string]any{
				"reaction":           "",
				"reacted_message_id": "wamid-789",
			},
			fromName: "Bob",
			expected: "Bob removed a reaction from message wamid-789",
		},
		{
			name: "reaction falls back to sender jid when pushname missing",
			payload: map[string]any{
				"reaction":           "😂",
				"reacted_message_id": "wamid-999",
				"from":               "628777000111@s.whatsapp.net",
			},
			fromName: "",
			expected: "628777000111 reacted 😂 to message wamid-999",
		},
		{
			name: "missing target id still produces readable text",
			payload: map[string]any{
				"reaction": "❤️",
			},
			fromName: "Carol",
			expected: "Carol reacted ❤️",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildReactionChatwootContent(tc.payload, tc.fromName); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
