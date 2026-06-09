package whatsapp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

type chatwootForwardQueueTestRepo struct {
	domainChatStorage.IChatStorageRepository
	events []*domainChatStorage.ChatwootForwardEvent
}

func (r *chatwootForwardQueueTestRepo) EnqueueChatwootForwardEvent(event *domainChatStorage.ChatwootForwardEvent) error {
	cloned := *event
	r.events = append(r.events, &cloned)
	return nil
}

func TestForwardPayloadToConfiguredWebhooks_NoWebhooksConfigured(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = nil
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalSubmit := submitWebhookFn
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		t.Fatal("submitWebhookFn should not be invoked when no webhooks are configured")
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "test"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestForwardPayloadToConfiguredWebhooks_PartialFailure(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://success", "https://fail", "https://success2"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalSubmit := submitWebhookFn
	var attempts []string
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string) error {
		attempts = append(attempts, url)
		if strings.Contains(url, "fail") {
			return errors.New("boom")
		}
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "test"); err != nil {
		t.Fatalf("expected partial failure to return nil, got %v", err)
	}

	if len(attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(attempts))
	}
}

func TestForwardPayloadToConfiguredWebhooks_AllFail(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://fail1", "https://fail2"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string) error {
		return errors.New("failure for " + url)
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "test"); err == nil {
		t.Fatalf("expected error when all webhooks fail")
	}
}

func TestForwardPayloadToConfiguredWebhooks_EventWhitelist_FilteredOut(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	config.WhatsappWebhook = []string{"https://test.com"}
	config.WhatsappWebhookEvents = []string{"message"}
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
	}()

	called := false
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		called = true
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message.ack"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called {
		t.Fatal("message.ack should be filtered by whitelist when only 'message' is allowed")
	}
}

func TestForwardPayloadToConfiguredWebhooks_EventWhitelist_Allowed(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	config.WhatsappWebhook = []string{"https://test.com"}
	config.WhatsappWebhookEvents = []string{"message", "message.ack"}
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
	}()

	called := false
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		called = true
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("message should be forwarded when in whitelist")
	}
}

func TestForwardPayloadToConfiguredWebhooks_EmptyWhitelist_AllowsAll(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	config.WhatsappWebhook = []string{"https://test.com"}
	config.WhatsappWebhookEvents = []string{}
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
	}()

	called := false
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		called = true
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "any.event"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("any event should be forwarded when whitelist is empty")
	}
}

func TestForwardPayloadToConfiguredWebhooks_WhitelistCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{"foo": "bar"}

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	config.WhatsappWebhook = []string{"https://test.com"}
	config.WhatsappWebhookEvents = []string{"MESSAGE", "Message.Ack"}
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
	}()

	called := 0
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		called++
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message.ack"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called != 2 {
		t.Fatalf("expected 2 calls (case-insensitive match), got %d", called)
	}
}

func TestEnqueueChatwootForwardRetryStoresTransientMessageFailure(t *testing.T) {
	repo := &chatwootForwardQueueTestRepo{}
	payload := map[string]any{
		"payload": map[string]any{
			"id":      "wa-queue-1",
			"chat_id": "628123456789@s.whatsapp.net",
		},
	}

	queued := enqueueChatwootForwardRetry(repo, "device-a@s.whatsapp.net", "message", payload, &chatwoot.HTTPStatusError{
		StatusCode: http.StatusInternalServerError,
		Op:         "create message",
		Body:       "unavailable",
	})
	if !queued {
		t.Fatal("expected transient Chatwoot failure to be queued")
	}
	if len(repo.events) != 1 {
		t.Fatalf("queued events = %d, want 1", len(repo.events))
	}
	event := repo.events[0]
	if event.DeviceID != "device-a@s.whatsapp.net" || event.EventName != "message" || event.WhatsAppMessageID != "wa-queue-1" {
		t.Fatalf("unexpected queued event: %+v", event)
	}
	if !strings.Contains(event.PayloadJSON, "wa-queue-1") || event.NextAttemptAt.IsZero() {
		t.Fatalf("queued event missing payload/next attempt: %+v", event)
	}
}

func TestEnqueueChatwootForwardRetrySkipsPermanentFailure(t *testing.T) {
	repo := &chatwootForwardQueueTestRepo{}
	payload := map[string]any{
		"payload": map[string]any{"id": "wa-permanent"},
	}

	queued := enqueueChatwootForwardRetry(repo, "device-a@s.whatsapp.net", "message", payload, &chatwoot.HTTPStatusError{
		StatusCode: http.StatusBadRequest,
		Op:         "create message",
		Body:       "bad payload",
	})
	if queued {
		t.Fatal("permanent Chatwoot failure should not be queued")
	}
	if len(repo.events) != 0 {
		t.Fatalf("queued events = %d, want 0", len(repo.events))
	}
}

func TestExtractStructuredMessageContentWithContactPayload(t *testing.T) {
	payload := map[string]any{
		"contact": webhookContactPayload{
			DisplayName: "Alice",
			PhoneNumber: "+62 812 3456 7890",
		},
	}

	got := extractStructuredMessageContent(payload)
	want := "Contact: Alice (+62 812 3456 7890)"
	if got != want {
		t.Fatalf("extractStructuredMessageContent() = %q, want %q", got, want)
	}
}

func TestExtractStructuredMessageContentWithContactsArrayPayload(t *testing.T) {
	payload := map[string]any{
		"contacts_array": []webhookContactPayload{
			{
				DisplayName: "Alice",
				PhoneNumber: "+62 812 3456 7890",
			},
			{
				DisplayName: "Bob",
				PhoneNumber: "+62 813 9876 5432",
			},
		},
	}

	got := extractStructuredMessageContent(payload)
	want := "Contacts: Alice (+62 812 3456 7890)"
	if got != want {
		t.Fatalf("extractStructuredMessageContent() = %q, want %q", got, want)
	}
}

// TestAddWebhookSessionID pins the issue #578 enrichment: webhook payloads gain a
// session_id derived from their device_id (JID) so multi-tenant consumers can map
// an event back to the session they registered, while device_id stays the JID.
func TestAddWebhookSessionID(t *testing.T) {
	orig := sessionIDForJIDFn
	defer func() { sessionIDForJIDFn = orig }()

	t.Run("injects session_id resolved from device_id", func(t *testing.T) {
		sessionIDForJIDFn = func(jid string) string {
			if jid == "556283088170@s.whatsapp.net" {
				return "org_2"
			}
			return ""
		}
		payload := map[string]any{"device_id": "556283088170@s.whatsapp.net"}
		addWebhookSessionID(payload)
		if payload["session_id"] != "org_2" {
			t.Fatalf("expected session_id=org_2, got %v", payload["session_id"])
		}
		// device_id must remain the JID (backward-compatible).
		if payload["device_id"] != "556283088170@s.whatsapp.net" {
			t.Fatalf("device_id must be unchanged, got %v", payload["device_id"])
		}
	})

	t.Run("no session_id when JID is unmapped", func(t *testing.T) {
		sessionIDForJIDFn = func(string) string { return "" }
		payload := map[string]any{"device_id": "unknown@s.whatsapp.net"}
		addWebhookSessionID(payload)
		if _, ok := payload["session_id"]; ok {
			t.Fatalf("expected no session_id for unmapped JID, got %v", payload["session_id"])
		}
	})

	t.Run("does not overwrite an existing session_id", func(t *testing.T) {
		sessionIDForJIDFn = func(string) string { return "resolved" }
		payload := map[string]any{"device_id": "x@s.whatsapp.net", "session_id": "preset"}
		addWebhookSessionID(payload)
		if payload["session_id"] != "preset" {
			t.Fatalf("expected existing session_id to be preserved, got %v", payload["session_id"])
		}
	})
}

// TestSessionIDForJIDEmpty pins the empty-JID guard: it must short-circuit to ""
// before touching the device manager, regardless of global manager state.
func TestSessionIDForJIDEmpty(t *testing.T) {
	if got := sessionIDForJID(""); got != "" {
		t.Fatalf("expected empty session id for empty jid, got %q", got)
	}
}

// TestForwardPayloadInjectsSessionID verifies the session id is enriched in the
// real forward path before reaching the webhook submitter.
func TestForwardPayloadInjectsSessionID(t *testing.T) {
	ctx := context.Background()

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://test.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	origResolve := sessionIDForJIDFn
	sessionIDForJIDFn = func(jid string) string {
		if jid == "556283088170@s.whatsapp.net" {
			return "org_2"
		}
		return ""
	}
	defer func() { sessionIDForJIDFn = origResolve }()

	var captured map[string]any
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string) error {
		captured = payload
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	payload := map[string]any{"event": "message", "device_id": "556283088170@s.whatsapp.net"}
	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured == nil {
		t.Fatal("expected submitWebhookFn to be invoked")
	}
	if captured["session_id"] != "org_2" {
		t.Fatalf("expected forwarded payload session_id=org_2, got %v", captured["session_id"])
	}
}
