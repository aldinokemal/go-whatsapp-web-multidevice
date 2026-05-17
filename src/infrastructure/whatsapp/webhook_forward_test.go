package whatsapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

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

func TestGetWebhookConfigForDevice_NoDeviceJID(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	configResult, err := getWebhookConfigForDevice("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configResult != nil {
		t.Fatalf("expected nil config when deviceJID is empty, got %v", configResult)
	}
}

func TestGetWebhookConfigForDevice_DeviceNotFound(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	configResult, err := getWebhookConfigForDevice("unknown-device-jid@s.whatsapp.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configResult != nil {
		t.Fatalf("expected nil config when device not found, got %v", configResult)
	}
}

func TestGetWebhookConfigForDevice_FallbackToGlobal(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global-webhook.com"}
	defer func() { config.WhatsappWebhook = originalWebhooks }()

	dm := GetDeviceManager()
	if dm == nil || dm.storage == nil {
		t.Skip("DeviceManager or storage not available")
	}

	configResult, err := getWebhookConfigForDevice("6289600000000@s.whatsapp.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configResult != nil {
		t.Fatalf("expected nil config when device has no webhook set, got %v", configResult)
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
	prevConfig, err := dm.storage.GetDeviceWebhookConfig(deviceID)
	if err != nil {
		t.Fatalf("failed to get previous webhook config: %v", err)
	}

	err = dm.storage.SetDeviceWebhookConfig(deviceID, &chatstorage.DeviceWebhookConfig{
		WebhookURL: &deviceWebhookURL,
	})
	if err != nil {
		t.Fatalf("failed to set device webhook: %v", err)
	}
	defer func() {
		var restoreErr error
		if prevConfig != nil {
			restoreErr = dm.storage.SetDeviceWebhookConfig(deviceID, prevConfig)
		} else {
			restoreErr = dm.storage.SetDeviceWebhookConfig(deviceID, &chatstorage.DeviceWebhookConfig{})
		}
		if restoreErr != nil {
			t.Logf("failed to restore webhook config: %v", restoreErr)
		}
	}()

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

	dm := GetDeviceManager()
	if dm == nil || dm.storage == nil {
		t.Skip("DeviceManager or storage not available")
	}

	deviceID := "6289600000000@s.whatsapp.net"
	prevConfig, err := dm.storage.GetDeviceWebhookConfig(deviceID)
	if err != nil {
		t.Fatalf("failed to get previous webhook config: %v", err)
	}

	err = dm.storage.SetDeviceWebhookConfig(deviceID, &chatstorage.DeviceWebhookConfig{})
	if err != nil {
		t.Fatalf("failed to clear device webhook config: %v", err)
	}
	defer func() {
		var restoreErr error
		if prevConfig != nil {
			restoreErr = dm.storage.SetDeviceWebhookConfig(deviceID, prevConfig)
		} else {
			restoreErr = dm.storage.SetDeviceWebhookConfig(deviceID, &chatstorage.DeviceWebhookConfig{})
		}
		if restoreErr != nil {
			t.Logf("failed to restore webhook config: %v", restoreErr)
		}
	}()

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

	dm := GetDeviceManager()
	if dm == nil || dm.storage == nil {
		t.Skip("DeviceManager or storage not available")
	}

	deviceID := "6289600000000@s.whatsapp.net"
	prevConfig, err := dm.storage.GetDeviceWebhookConfig(deviceID)
	if err != nil {
		t.Fatalf("failed to get previous webhook config: %v", err)
	}

	// Set device-specific webhook (no global webhook configured)
	err = dm.storage.SetDeviceWebhookConfig(deviceID, &chatstorage.DeviceWebhookConfig{
		WebhookURL: &deviceWebhookURL,
	})
	if err != nil {
		t.Fatalf("failed to set device webhook: %v", err)
	}
	defer func() {
		var restoreErr error
		if prevConfig != nil {
			restoreErr = dm.storage.SetDeviceWebhookConfig(deviceID, prevConfig)
		} else {
			restoreErr = dm.storage.SetDeviceWebhookConfig(deviceID, &chatstorage.DeviceWebhookConfig{})
		}
		if restoreErr != nil {
			t.Logf("failed to restore webhook config: %v", restoreErr)
		}
	}()

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
