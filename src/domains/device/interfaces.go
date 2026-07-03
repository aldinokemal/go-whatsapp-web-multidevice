package device

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// IDeviceUsecase defines device lifecycle operations.
type IDeviceUsecase interface {
	ListDevices(ctx context.Context) ([]Device, error)
	GetDevice(ctx context.Context, deviceID string) (*Device, error)
	// AddDevice creates a new device. A non-nil webhook applies the full device-specific
	// webhook configuration (url, secret, events, insecure_skip_verify) at creation time.
	AddDevice(ctx context.Context, deviceID string, webhook *chatstorage.DeviceWebhookConfig) (*Device, error)
	RemoveDevice(ctx context.Context, deviceID string) error
	LoginDevice(ctx context.Context, deviceID string) error
	LoginDeviceWithCode(ctx context.Context, deviceID string, phone string) (string, error)
	LogoutDevice(ctx context.Context, deviceID string) error
	ReconnectDevice(ctx context.Context, deviceID string) error
	GetStatus(ctx context.Context, deviceID string) (isConnected bool, isLoggedIn bool, err error)
	// SetDeviceWebhook sets the webhook URL for a specific device.
	SetDeviceWebhook(ctx context.Context, deviceID string, webhookURL string) error
	// GetDeviceWebhook retrieves the webhook URL for a specific device.
	GetDeviceWebhook(ctx context.Context, deviceID string) (string, error)
	// SetDeviceWebhookConfig sets the complete webhook configuration for a specific device.
	SetDeviceWebhookConfig(ctx context.Context, deviceID string, config *chatstorage.DeviceWebhookConfig) error
	// GetDeviceWebhookConfig retrieves the complete webhook configuration for a specific device.
	GetDeviceWebhookConfig(ctx context.Context, deviceID string) (*chatstorage.DeviceWebhookConfig, error)
}
