package whatsapp

import (
	"context"
	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"testing"
)

var HandleDeviceEventForTest = handler

func CaptureDeviceEventsForTest(t *testing.T, repo domain.IChatStorageRepository, receive func(map[string]any, string, *domain.DeviceWebhookConfig)) {
	t.Helper()
	oldLookup, oldSubmit := webhookStorageForTest, submitWebhookFn
	webhookStorageForTest = repo.GetDeviceRecordByJID
	submitWebhookFn = func(_ context.Context, body map[string]any, url string, cfg *domain.DeviceWebhookConfig) error {
		receive(body, url, cfg)
		return nil
	}
	t.Cleanup(func() { webhookStorageForTest, submitWebhookFn = oldLookup, oldSubmit })
}
