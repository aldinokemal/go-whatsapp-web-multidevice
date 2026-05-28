package whatsapp

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainWebhook "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	webhookregistry "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/webhook"
)

// fakeWebhookRepo feeds canned configs into a WebhookRegistry for tests.
type fakeWebhookRepo struct {
	all []*domainWebhook.DeviceWebhookConfig
}

func (f *fakeWebhookRepo) Migrate() error                                { return nil }
func (f *fakeWebhookRepo) Save(*domainWebhook.DeviceWebhookConfig) error { return nil }
func (f *fakeWebhookRepo) GetByID(int) (*domainWebhook.DeviceWebhookConfig, error) {
	return nil, nil
}
func (f *fakeWebhookRepo) GetByDeviceID(string) ([]*domainWebhook.DeviceWebhookConfig, error) {
	return nil, nil
}
func (f *fakeWebhookRepo) GetAll() ([]*domainWebhook.DeviceWebhookConfig, error) {
	return f.all, nil
}
func (f *fakeWebhookRepo) Delete(int) error { return nil }

// installRegistry sets a global webhook registry seeded with the given configs
// and restores the previous one on cleanup.
func installRegistry(t *testing.T, configs ...*domainWebhook.DeviceWebhookConfig) {
	t.Helper()
	prev := webhookregistry.GetGlobalRegistry()
	reg := webhookregistry.NewWebhookRegistry(&fakeWebhookRepo{all: configs})
	webhookregistry.SetGlobalRegistry(reg)
	t.Cleanup(func() { webhookregistry.SetGlobalRegistry(prev) })
}

func TestForward_PerDeviceWebhookTakesPrecedence(t *testing.T) {
	const device = "628111@s.whatsapp.net"
	installRegistry(t, &domainWebhook.DeviceWebhookConfig{
		ID:         1,
		DeviceID:   device,
		WebhookURL: "https://per-device/hook",
		Secret:     "device-secret",
		Enabled:    true,
		Headers:    map[string]string{"X-Tenant": "acme"},
	})

	originalGlobal := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global/hook"}
	defer func() { config.WhatsappWebhook = originalGlobal }()

	originalGlobalFn, originalDeviceFn := submitWebhookFn, submitDeviceWebhookFn
	defer func() { submitWebhookFn, submitDeviceWebhookFn = originalGlobalFn, originalDeviceFn }()

	globalCalled := false
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		globalCalled = true
		return nil
	}
	var gotURL, gotSecret string
	var gotHeaders map[string]string
	submitDeviceWebhookFn = func(_ context.Context, _ map[string]any, url, secret string, headers map[string]string) error {
		gotURL, gotSecret, gotHeaders = url, secret, headers
		return nil
	}

	payload := map[string]any{"device_id": device, "payload": map[string]any{}}
	if err := forwardPayloadToConfiguredWebhooks(context.Background(), payload, "message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if globalCalled {
		t.Fatal("global webhook must not be used when the device has per-device config")
	}
	if gotURL != "https://per-device/hook" || gotSecret != "device-secret" || gotHeaders["X-Tenant"] != "acme" {
		t.Fatalf("per-device webhook called with wrong args: url=%q secret=%q headers=%v", gotURL, gotSecret, gotHeaders)
	}
}

func TestForward_DeviceWithoutConfigFallsBackToGlobal(t *testing.T) {
	installRegistry(t, &domainWebhook.DeviceWebhookConfig{
		ID:         1,
		DeviceID:   "OTHER@s.whatsapp.net",
		WebhookURL: "https://per-device/hook",
		Enabled:    true,
	})

	originalGlobal := config.WhatsappWebhook
	config.WhatsappWebhook = []string{"https://global/hook"}
	defer func() { config.WhatsappWebhook = originalGlobal }()

	originalGlobalFn, originalDeviceFn := submitWebhookFn, submitDeviceWebhookFn
	defer func() { submitWebhookFn, submitDeviceWebhookFn = originalGlobalFn, originalDeviceFn }()

	globalCalled, deviceCalled := false, false
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		globalCalled = true
		return nil
	}
	submitDeviceWebhookFn = func(context.Context, map[string]any, string, string, map[string]string) error {
		deviceCalled = true
		return nil
	}

	payload := map[string]any{"device_id": "628999@s.whatsapp.net", "payload": map[string]any{}}
	if err := forwardPayloadToConfiguredWebhooks(context.Background(), payload, "message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deviceCalled {
		t.Fatal("per-device path must not be used for a device without config")
	}
	if !globalCalled {
		t.Fatal("expected fallback to the global webhook")
	}
}

func TestForward_PerDeviceEventFilterIsAuthoritative(t *testing.T) {
	const device = "628111@s.whatsapp.net"
	installRegistry(t, &domainWebhook.DeviceWebhookConfig{
		ID:         1,
		DeviceID:   device,
		WebhookURL: "https://per-device/hook",
		Events:     []string{"message"},
		Enabled:    true,
	})

	// A restrictive global whitelist must NOT suppress a per-device subscription.
	originalGlobal := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	config.WhatsappWebhook = nil
	config.WhatsappWebhookEvents = []string{"call.offer"}
	defer func() {
		config.WhatsappWebhook = originalGlobal
		config.WhatsappWebhookEvents = originalEvents
	}()

	originalDeviceFn := submitDeviceWebhookFn
	defer func() { submitDeviceWebhookFn = originalDeviceFn }()
	calls := 0
	submitDeviceWebhookFn = func(context.Context, map[string]any, string, string, map[string]string) error {
		calls++
		return nil
	}

	payload := map[string]any{"device_id": device, "payload": map[string]any{}}

	// "message" is subscribed by the device → delivered despite global whitelist.
	if err := forwardPayloadToConfiguredWebhooks(context.Background(), payload, "message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "message.ack" is NOT in the device's event filter → skipped.
	if err := forwardPayloadToConfiguredWebhooks(context.Background(), payload, "message.ack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected exactly 1 delivery (only 'message'), got %d", calls)
	}
}

func TestWebhookEventAllowed(t *testing.T) {
	if !webhookEventAllowed(nil, "anything") {
		t.Fatal("empty filter must allow all events")
	}
	if !webhookEventAllowed([]string{"MESSAGE"}, "message") {
		t.Fatal("match must be case-insensitive")
	}
	if webhookEventAllowed([]string{"message"}, "call.offer") {
		t.Fatal("unlisted event must be rejected")
	}
}
