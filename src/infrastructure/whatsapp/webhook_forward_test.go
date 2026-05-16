package whatsapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

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

func TestGetWebhookURLsForDevice_NoDeviceJID(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	urls, err := getWebhookURLsForDevice("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://global-webhook.com" {
		t.Fatalf("expected global webhook when deviceJID is empty, got %v", urls)
	}
}

func TestGetWebhookURLsForDevice_DeviceNotFound(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	urls, err := getWebhookURLsForDevice("unknown-device-jid@s.whatsapp.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://global-webhook.com" {
		t.Fatalf("expected global webhook when device not found, got %v", urls)
	}
}

func TestGetWebhookURLsForDevice_FallbackToGlobal(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	dm := GetDeviceManager()
	if dm == nil || dm.storage == nil {
		t.Skip("DeviceManager or storage not available")
	}

	urls, err := getWebhookURLsForDevice("6289600000000@s.whatsapp.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://global-webhook.com" {
		t.Fatalf("expected global webhook when deviceJID is empty, got %v", urls)
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

	dm := GetDeviceManager()
	if dm == nil || dm.storage == nil {
		t.Skip("DeviceManager or storage not available")
	}

	deviceID := "6289600000000@s.whatsapp.net"
	prevWebhook, err := dm.storage.GetDeviceWebhookURL(deviceID)
	if err != nil {
		t.Fatalf("failed to get previous webhook: %v", err)
	}

	err = dm.storage.SetDeviceWebhookURL(deviceID, &deviceWebhookURL)
	if err != nil {
		t.Fatalf("failed to set device webhook: %v", err)
	}
	defer func() {
		var restoreErr error
		if prevWebhook != nil {
			restoreErr = dm.storage.SetDeviceWebhookURL(deviceID, prevWebhook)
		} else {
			restoreErr = dm.storage.SetDeviceWebhookURL(deviceID, nil)
		}
		if restoreErr != nil {
			t.Logf("failed to restore webhook: %v", restoreErr)
		}
	}()

	var calledURLs []string
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string) error {
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

	dm := GetDeviceManager()
	if dm == nil || dm.storage == nil {
		t.Skip("DeviceManager or storage not available")
	}

	deviceID := "6289600000000@s.whatsapp.net"
	prevWebhook, err := dm.storage.GetDeviceWebhookURL(deviceID)
	if err != nil {
		t.Fatalf("failed to get previous webhook: %v", err)
	}

	err = dm.storage.SetDeviceWebhookURL(deviceID, nil)
	if err != nil {
		t.Fatalf("failed to clear device webhook: %v", err)
	}
	defer func() {
		var restoreErr error
		if prevWebhook != nil {
			restoreErr = dm.storage.SetDeviceWebhookURL(deviceID, prevWebhook)
		} else {
			restoreErr = dm.storage.SetDeviceWebhookURL(deviceID, nil)
		}
		if restoreErr != nil {
			t.Logf("failed to restore webhook: %v", restoreErr)
		}
	}()

	var calledURLs []string
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string) error {
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
