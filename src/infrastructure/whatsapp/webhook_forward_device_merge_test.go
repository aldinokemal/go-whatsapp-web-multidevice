package whatsapp

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

type mergeDelivery struct {
	url    string
	secret string
}

// runDeviceMergeForward runs the forwarder for a device that has its own webhook
// (deviceURL / deviceEvents) while the global config holds globalURLs, and reports
// every delivery attempted together with the secret each one was signed with.
func runDeviceMergeForward(t *testing.T, merge bool, globalURLs []string, globalEvents []string, deviceURL, deviceEvents, eventName string, failURLs map[string]bool) ([]mergeDelivery, error) {
	t.Helper()

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	originalIgnore := config.WhatsappWebhookIgnoreJids
	originalMerge := config.WhatsappWebhookDeviceMergeGlobal
	config.WhatsappWebhook = globalURLs
	config.WhatsappWebhookEvents = globalEvents
	config.WhatsappWebhookIgnoreJids = nil
	config.WhatsappWebhookDeviceMergeGlobal = merge
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
		config.WhatsappWebhookIgnoreJids = originalIgnore
		config.WhatsappWebhookDeviceMergeGlobal = originalMerge
	}()

	originalStorage := webhookStorageForTest
	webhookStorageForTest = func(deviceJID string) (*domainChatStorage.DeviceRecord, error) {
		url := deviceURL
		return &domainChatStorage.DeviceRecord{
			DeviceID:      deviceJID,
			WebhookURL:    &url,
			WebhookSecret: "device-secret",
			WebhookEvents: deviceEvents,
		}, nil
	}
	defer func() { webhookStorageForTest = originalStorage }()

	var deliveries []mergeDelivery
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, cfg *domainChatStorage.DeviceWebhookConfig) error {
		secret := "global-secret"
		if cfg != nil {
			secret = cfg.WebhookSecret
		}
		deliveries = append(deliveries, mergeDelivery{url: url, secret: secret})
		if failURLs[url] {
			return errors.New("boom")
		}
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	payload := ignoreJidPayload("628111@s.whatsapp.net", "628111@s.whatsapp.net")
	payload["event"] = eventName
	err := forwardPayloadToConfiguredWebhooks(context.Background(), payload, eventName)
	sort.Slice(deliveries, func(i, j int) bool { return deliveries[i].url < deliveries[j].url })
	return deliveries, err
}

func TestWebhookDeviceMerge_DisabledKeepsDeviceOnlyDelivery(t *testing.T) {
	deliveries, err := runDeviceMergeForward(t, false, []string{"https://global-a", "https://global-b"}, nil, "https://device", "", "message", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].url != "https://device" || deliveries[0].secret != "device-secret" {
		t.Fatalf("expected a single device delivery signed with the device secret, got %+v", deliveries)
	}
}

func TestWebhookDeviceMerge_EnabledDeliversToDeviceAndGlobalWithOwnSecrets(t *testing.T) {
	deliveries, err := runDeviceMergeForward(t, true, []string{"https://global-a", "https://global-b"}, nil, "https://device", "", "message", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []mergeDelivery{
		{url: "https://device", secret: "device-secret"},
		{url: "https://global-a", secret: "global-secret"},
		{url: "https://global-b", secret: "global-secret"},
	}
	if len(deliveries) != len(want) {
		t.Fatalf("expected %d deliveries, got %+v", len(want), deliveries)
	}
	for i := range want {
		if deliveries[i] != want[i] {
			t.Fatalf("delivery %d: expected %+v, got %+v", i, want[i], deliveries[i])
		}
	}
}

func TestWebhookDeviceMerge_GlobalLegUsesGlobalEventWhitelist(t *testing.T) {
	// Device only subscribed to "message"; global whitelist allows everything, so a
	// reaction still reaches the global targets but not the device URL.
	deliveries, err := runDeviceMergeForward(t, true, []string{"https://global-a"}, nil, "https://device", "message", "message.reaction", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].url != "https://global-a" {
		t.Fatalf("expected only the global delivery, got %+v", deliveries)
	}

	// And the other way round: global whitelist excludes the event, device explicitly
	// allows it (a device with no webhook_events of its own inherits the global list).
	deliveries, err = runDeviceMergeForward(t, true, []string{"https://global-a"}, []string{"message.ack"}, "https://device", "message", "message", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].url != "https://device" {
		t.Fatalf("expected only the device delivery, got %+v", deliveries)
	}
}

func TestWebhookDeviceMerge_SkipsGlobalURLAlreadyCoveredByDevice(t *testing.T) {
	deliveries, err := runDeviceMergeForward(t, true, []string{"https://device", "https://global-b"}, nil, "https://device", "", "message", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deliveries) != 2 || deliveries[0].url != "https://device" || deliveries[1].url != "https://global-b" {
		t.Fatalf("expected the shared URL to be delivered once, got %+v", deliveries)
	}
}

func TestWebhookDeviceMerge_ErrorOnlyWhenEveryLegFails(t *testing.T) {
	_, err := runDeviceMergeForward(t, true, []string{"https://global-a"}, nil, "https://device", "", "message", map[string]bool{"https://device": true})
	if err != nil {
		t.Fatalf("a surviving global delivery must not surface an error, got %v", err)
	}
	_, err = runDeviceMergeForward(t, true, []string{"https://global-a"}, nil, "https://device", "", "message", map[string]bool{"https://device": true, "https://global-a": true})
	if err == nil {
		t.Fatal("expected an error when both the device and global deliveries fail")
	}
}
