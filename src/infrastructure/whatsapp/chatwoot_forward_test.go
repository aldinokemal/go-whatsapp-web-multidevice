package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
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
			// The target id is not repeated in the text: when reacted_message_id
			// is present, syncPayloadToChatwoot already threads this note onto
			// the reacted-to message via in_reply_to_external_id, so Chatwoot
			// nests it under that message's bubble.
			name: "reaction with sender name and target id",
			payload: map[string]any{
				"reaction":           "👍",
				"reacted_message_id": "wamid-123",
			},
			fromName: "Alice",
			expected: "Alice reacted 👍",
		},
		{
			name: "reaction falls back to phone",
			payload: map[string]any{
				"reaction":           "🔥",
				"reacted_message_id": "wamid-456",
				"from":               "628123456789@s.whatsapp.net",
			},
			fromName: "",
			expected: "628123456789 reacted 🔥",
		},
		{
			name: "reaction removal",
			payload: map[string]any{
				"reaction":           "",
				"reacted_message_id": "wamid-789",
			},
			fromName: "Bob",
			expected: "Bob removed a reaction",
		},
		{
			name: "reaction falls back to sender jid when pushname missing",
			payload: map[string]any{
				"reaction":           "😂",
				"reacted_message_id": "wamid-999",
				"from":               "628777000111@s.whatsapp.net",
			},
			fromName: "",
			expected: "628777000111 reacted 😂",
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

type replyAttrsTestRepo struct {
	domainChatStorage.IChatStorageRepository
	links map[string]*domainChatStorage.ChatwootMessageLink
}

func (r *replyAttrsTestRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*domainChatStorage.ChatwootMessageLink, error) {
	return r.links[deviceID+"|"+waMessageID], nil
}

func TestResolveChatwootReplyTarget(t *testing.T) {
	repo := &replyAttrsTestRepo{
		links: map[string]*domainChatStorage.ChatwootMessageLink{
			"device-1|wamid-linked":         {ChatwootMessageID: 77, ChatwootConversationID: 10},
			"device-1|wamid-zero-link":      {ChatwootMessageID: 0},
			"device-1|wamid-other-config":   {ChatwootMessageID: 78, ChatwootConfigID: 9},
			"device-1|wamid-other-account":  {ChatwootMessageID: 79, ChatwootAccountID: 2},
			"device-1|wamid-current-config": {ChatwootMessageID: 80, ChatwootConfigID: 5},
		},
	}
	legacy := &chatwoot.Client{AccountID: 1}

	usable := []struct {
		id       string
		configID int64
		wantID   int
	}{
		{"wamid-linked", 0, 77},
		{"wamid-current-config", 5, 80},
	}
	for _, tc := range usable {
		target := resolveChatwootReplyTarget(legacy, tc.configID, "device-1", repo, tc.id, "")
		if target.link == nil || target.link.ChatwootMessageID != tc.wantID {
			t.Fatalf("%s: expected link %d, got %+v", tc.id, tc.wantID, target.link)
		}
	}

	// No link, a zero id, a link written for another Chatwoot destination
	// (its message id belongs to that server/account), no repository or no
	// device: nothing to thread natively.
	unusable := []struct {
		name     string
		repo     domainChatStorage.IChatStorageRepository
		device   string
		id       string
		configID int64
	}{
		{"unknown", repo, "device-1", "wamid-unknown", 0},
		{"zero chatwoot id", repo, "device-1", "wamid-zero-link", 0},
		{"per-device client, other config", repo, "device-1", "wamid-other-config", 5},
		{"legacy client, other account", repo, "device-1", "wamid-other-account", 0},
		{"legacy client, per-device link", repo, "device-1", "wamid-current-config", 0},
		{"nil repo", nil, "device-1", "wamid-linked", 0},
		{"empty device", repo, "", "wamid-linked", 0},
	}
	for _, tc := range unusable {
		target := resolveChatwootReplyTarget(legacy, tc.configID, tc.device, tc.repo, tc.id, "Hello\nthere")
		if target.link != nil {
			t.Fatalf("%s: expected no link, got %+v", tc.name, target.link)
		}
		if target.excerpt != "> Hello there" {
			t.Fatalf("%s: excerpt = %q", tc.name, target.excerpt)
		}
	}

	if target := resolveChatwootReplyTarget(legacy, 0, "device-1", repo, "wamid-linked", "  "); target.excerpt != "" {
		t.Fatalf("blank quoted body must not produce an excerpt, got %q", target.excerpt)
	}
}

func TestChatwootReplyTargetApply(t *testing.T) {
	link := &domainChatStorage.ChatwootMessageLink{ChatwootMessageID: 77, ChatwootConversationID: 10}
	newOpts := func() chatwoot.MessageOptions {
		return chatwoot.MessageOptions{ContentAttributes: map[string]any{"in_reply_to_external_id": "WAID:x"}}
	}

	tests := []struct {
		name           string
		target         *chatwootReplyTarget
		conversationID int
		content        string
		wantContent    string
		wantInReplyTo  any // nil: must not be set
	}{
		{"link in this conversation", &chatwootReplyTarget{link: link, excerpt: "> hi"}, 10, "ok", "ok", 77},
		// A returning contact can get a new conversation
		// (CHATWOOT_REOPEN_CONVERSATION=false); Chatwoot rejects in_reply_to
		// across conversations, so fall back to the excerpt.
		{"link in another conversation", &chatwootReplyTarget{link: link, excerpt: "> hi"}, 11, "ok", "> hi\n\nok", nil},
		{"reaction link in another conversation", &chatwootReplyTarget{link: link}, 11, "Alice reacted 👍", "Alice reacted 👍", nil},
		{"no link, excerpt only", &chatwootReplyTarget{excerpt: "> hi"}, 10, "ok", "> hi\n\nok", nil},
		{"no link, empty content", &chatwootReplyTarget{excerpt: "> hi"}, 10, "", "> hi", nil},
		{"nil target", nil, 10, "ok", "ok", nil},
	}
	for _, tt := range tests {
		opts := newOpts()
		got := tt.target.apply(tt.conversationID, tt.content, &opts)
		if got != tt.wantContent {
			t.Fatalf("%s: content = %q, want %q", tt.name, got, tt.wantContent)
		}
		inReplyTo, set := opts.ContentAttributes["in_reply_to"]
		if tt.wantInReplyTo == nil && set {
			t.Fatalf("%s: in_reply_to must not be set, got %v", tt.name, inReplyTo)
		}
		if tt.wantInReplyTo != nil && inReplyTo != tt.wantInReplyTo {
			t.Fatalf("%s: in_reply_to = %v, want %v", tt.name, inReplyTo, tt.wantInReplyTo)
		}
	}
}

func TestOneLineExcerpt(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "collapses newlines and spaces", in: "  olá,\n  como   vai? ", max: 80, want: "olá, como vai?"},
		{name: "truncates with ellipsis", in: strings.Repeat("b", 90), max: 80, want: strings.Repeat("b", 80) + "…"},
		{name: "short string unchanged", in: "oi", max: 80, want: "oi"},
		{name: "empty string", in: "", max: 80, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := oneLineExcerpt(tt.in, tt.max); got != tt.want {
				t.Fatalf("oneLineExcerpt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSyncPayloadToChatwoot_ReactionEchoStampsSourceID(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			response = map[string]any{"payload": []map[string]any{{"id": 7, "name": "Alice", "phone_number": r.URL.Query().Get("q")}}}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/7/conversations":
			response = map[string]any{"payload": []map[string]any{{"id": 42, "inbox_id": 1, "status": "open"}}}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/42/messages":
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Errorf("decode message body: %v", err)
			}
			response = map[string]any{"id": 999}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	cw := &chatwoot.Client{BaseURL: server.URL, APIToken: "t", AccountID: 1, InboxID: 1, HTTPClient: server.Client()}
	orig := getChatwootClientFn
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{Client: cw}, nil
	}
	t.Cleanup(func() { getChatwootClientFn = orig })

	payload := map[string]any{"payload": map[string]any{
		"id":                 "REACTION-ID-1",
		"reacted_message_id": "ORIGINAL-ID-1",
		"reaction":           "👍",
		"from":               "628123456789@s.whatsapp.net",
		"chat_id":            "628123456789@s.whatsapp.net",
		"from_name":          "Alice",
		"is_from_me":         true,
	}}
	if err := syncPayloadToChatwoot(context.Background(), payload, "message.reaction", "", nil); err != nil {
		t.Fatalf("syncPayloadToChatwoot: %v", err)
	}
	if capturedBody == nil {
		t.Fatal("message-create endpoint was never called")
	}
	if got := capturedBody["source_id"]; got != "WAID:REACTION-ID-1" {
		t.Errorf("source_id = %v, want WAID:REACTION-ID-1", got)
	}
	if got := capturedBody["message_type"]; got != "outgoing" {
		t.Errorf("message_type = %v, want outgoing", got)
	}
	if got := capturedBody["private"]; got != false {
		t.Errorf("private = %v, want false", got)
	}
	attributes, _ := capturedBody["content_attributes"].(map[string]any)
	if got := attributes["in_reply_to_external_id"]; got != "WAID:ORIGINAL-ID-1" {
		t.Errorf("in_reply_to_external_id = %v, want WAID:ORIGINAL-ID-1", got)
	}
}
