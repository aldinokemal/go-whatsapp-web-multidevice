package whatsapp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

type chatwootForwardQueueTestRepo struct {
	chatstorage.IChatStorageRepository
	events []*chatstorage.ChatwootForwardEvent
}

func (r *chatwootForwardQueueTestRepo) EnqueueChatwootForwardEvent(event *chatstorage.ChatwootForwardEvent) error {
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
	submitWebhookFn = func(context.Context, map[string]any, string, *chatstorage.DeviceWebhookConfig) error {
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
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
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
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
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
	submitWebhookFn = func(context.Context, map[string]any, string, *chatstorage.DeviceWebhookConfig) error {
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
	submitWebhookFn = func(context.Context, map[string]any, string, *chatstorage.DeviceWebhookConfig) error {
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
	submitWebhookFn = func(context.Context, map[string]any, string, *chatstorage.DeviceWebhookConfig) error {
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
	submitWebhookFn = func(context.Context, map[string]any, string, *chatstorage.DeviceWebhookConfig) error {
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

// retryWorkerTestRepo records which terminal action the retry worker takes for a
// due event so a test can distinguish "rescheduled" from "marked done/deleted".
type retryWorkerTestRepo struct {
	chatstorage.IChatStorageRepository
	due       []*chatstorage.ChatwootForwardEvent
	failedIDs []int64
	doneIDs   []int64
}

func (r *retryWorkerTestRepo) ListDueChatwootForwardEvents(_ time.Time, _ int) ([]*chatstorage.ChatwootForwardEvent, error) {
	return r.due, nil
}

func (r *retryWorkerTestRepo) MarkChatwootForwardEventFailed(id int64, _ string, _ time.Time) error {
	r.failedIDs = append(r.failedIDs, id)
	return nil
}

func (r *retryWorkerTestRepo) MarkChatwootForwardEventDone(id int64) error {
	r.doneIDs = append(r.doneIDs, id)
	return nil
}

func TestProcessDueChatwootForwardRetriesReschedulesOnRegistryUnavailable(t *testing.T) {
	// Reproduces the P1 data-loss bug: when the registry is uninitialized, a due
	// retry must be rescheduled, NOT marked done (which deletes it without ever
	// delivering the message).
	orig := getChatwootClientFn
	t.Cleanup(func() { getChatwootClientFn = orig })
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return nil, chatwoot.ErrClientRegistryUnavailable
	}

	repo := &retryWorkerTestRepo{
		due: []*chatstorage.ChatwootForwardEvent{
			{ID: 7, DeviceID: "dev", EventName: "message", WhatsAppMessageID: "wa-1", PayloadJSON: `{"payload":{"id":"wa-1"}}`},
		},
	}

	processDueChatwootForwardRetries(repo)

	if len(repo.doneIDs) != 0 {
		t.Fatalf("retry job must not be marked done on nil registry, got done=%v", repo.doneIDs)
	}
	if len(repo.failedIDs) != 1 || repo.failedIDs[0] != 7 {
		t.Fatalf("retry job should be rescheduled, got failed=%v", repo.failedIDs)
	}
}

func TestEnqueueChatwootForwardRetrySkipsRegistryUnavailable(t *testing.T) {
	// An uninitialized registry is a wiring/startup condition, not a transient
	// network failure. The live path must NOT enqueue a retry that would only
	// fail the same way (and, in MCP-only deployments, accumulate forever).
	repo := &chatwootForwardQueueTestRepo{}
	payload := map[string]any{
		"payload": map[string]any{"id": "wa-no-registry"},
	}

	queued := enqueueChatwootForwardRetry(repo, "device-a@s.whatsapp.net", "message", payload, chatwoot.ErrClientRegistryUnavailable)
	if queued {
		t.Fatal("registry-unavailable failure should not be queued")
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

func TestGetWebhookConfigForDevice_NoDeviceID(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	config, err := getWebhookConfigForDevice("")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config != nil {
		t.Fatalf("expected nil config for empty deviceID, got %v", config)
	}
}

func TestGetWebhookConfigForDevice_DeviceNotFound(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return nil, nil // not found, no error
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	config, err := getWebhookConfigForDevice("unknown-device-jid@s.whatsapp.net")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config != nil {
		t.Fatalf("expected nil config when device not found, got %v", config)
	}
}

func TestGetWebhookConfigForDevice_FallbackToGlobal(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	emptyURL := ""
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{
			DeviceID:   deviceJID,
			WebhookURL: &emptyURL,
		}, nil
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	config, err := getWebhookConfigForDevice("6289600000000@s.whatsapp.net")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config != nil {
		t.Fatalf("expected nil config when device has no webhook, got %v", config)
	}
}

func TestGetWebhookConfigForDevice_DeviceSpecificOverride(t *testing.T) {
	deviceWebhookURL := "https://device-specific-webhook.com"
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{
			DeviceID:   deviceJID,
			WebhookURL: &deviceWebhookURL,
		}, nil
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	config, err := getWebhookConfigForDevice("6289600000000@s.whatsapp.net")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if config == nil {
		t.Fatal("expected non-nil config when device has webhook")
	}
	if config.WebhookURL == nil || *config.WebhookURL != deviceWebhookURL {
		t.Fatalf("expected device-specific webhook %s, got %v", deviceWebhookURL, config.WebhookURL)
	}
}

func TestForwardPayloadToConfiguredWebhooks_WithDeviceSpecificWebhook(t *testing.T) {
	ctx := context.Background()
	deviceWebhookURL := "https://device-specific-webhook.com"
	payload := map[string]any{
		"foo":       "bar",
		"device_id": "6289600000000@s.whatsapp.net",
	}

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{
			DeviceID:   deviceJID,
			WebhookURL: &deviceWebhookURL,
		}, nil
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	var calledURLs []string
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
		calledURLs = append(calledURLs, url)
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "test"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(calledURLs) != 1 {
		t.Fatalf("expected 1 webhook call (device-specific override), got %d", len(calledURLs))
	}
	if calledURLs[0] != deviceWebhookURL {
		t.Fatalf("expected device-specific webhook %s, got %s", deviceWebhookURL, calledURLs[0])
	}
}

func TestForwardPayloadToConfiguredWebhooks_DeviceWebhookCleared_FallsBackToGlobal(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{
		"foo":       "bar",
		"device_id": "6289600000000@s.whatsapp.net",
	}

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	emptyURL := ""
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{
			DeviceID:   deviceJID,
			WebhookURL: &emptyURL,
		}, nil
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	var calledURLs []string
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
		calledURLs = append(calledURLs, url)
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "test"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(calledURLs) != 1 {
		t.Fatalf("expected 1 webhook call (global fallback), got %d", len(calledURLs))
	}
	if calledURLs[0] != "https://global-webhook.com" {
		t.Fatalf("expected global webhook, got %s", calledURLs[0])
	}
}

// TestForwardPayloadToConfiguredWebhooks_DeviceLookupError_FallsBackToGlobal verifies that a
// transient storage error while resolving the device webhook config does not abort forwarding:
// the event must still be delivered using the global webhook config. The function's contract is
// to only return an error when all webhook deliveries fail — a config lookup failure is not a
// delivery failure.
func TestForwardPayloadToConfiguredWebhooks_DeviceLookupError_FallsBackToGlobal(t *testing.T) {
	ctx := context.Background()
	payload := map[string]any{
		"foo":       "bar",
		"device_id": "6289600000000@s.whatsapp.net",
	}

	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return nil, errors.New("database is locked")
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	var calledURLs []string
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
		calledURLs = append(calledURLs, url)
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message"); err != nil {
		t.Fatalf("device config lookup failure must not abort forwarding, got error: %v", err)
	}

	if len(calledURLs) != 1 {
		t.Fatalf("expected 1 webhook call (global fallback), got %d: %v", len(calledURLs), calledURLs)
	}
	if calledURLs[0] != "https://global-webhook.com" {
		t.Fatalf("expected global webhook, got %s", calledURLs[0])
	}
}

// TestForwardPayloadToConfiguredWebhooks_DeviceWebhookOnly_NoGlobal verifies that when
// no global webhook is configured but a device-specific webhook is set, events are
// forwarded to the device-specific webhook. This is the primary path aldinokemal asked
// to verify: "the feature should work when only a device webhook is set."
func TestForwardPayloadToConfiguredWebhooks_DeviceWebhookOnly_NoGlobal(t *testing.T) {
	ctx := context.Background()
	deviceWebhookURL := "https://device-only-webhook.example.com"
	payload := map[string]any{
		"foo":       "bar",
		"device_id": "6289600000000@s.whatsapp.net",
	}

	// Ensure no global webhook is configured
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = nil
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	originalStorageForTest := webhookStorageForTest
	webhookStorageForTest = func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{
			DeviceID:   deviceJID,
			WebhookURL: &deviceWebhookURL,
		}, nil
	}
	defer func() { webhookStorageForTest = originalStorageForTest }()

	var calledURLs []string
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
		calledURLs = append(calledURLs, url)
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message"); err != nil {
		t.Fatalf("expected no error when only device webhook is set, got %v", err)
	}

	if len(calledURLs) != 1 {
		t.Fatalf("expected 1 webhook call (device-specific only), got %d calls: %v", len(calledURLs), calledURLs)
	}
	if calledURLs[0] != deviceWebhookURL {
		t.Fatalf("expected device-specific webhook %s, got %s", deviceWebhookURL, calledURLs[0])
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
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string, _ *chatstorage.DeviceWebhookConfig) error {
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
