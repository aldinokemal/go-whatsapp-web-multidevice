package device

import "context"

// IDeviceUsecase defines device lifecycle operations.
type IDeviceUsecase interface {
	ListDevices(ctx context.Context) ([]Device, error)
	GetDevice(ctx context.Context, deviceID string) (*Device, error)
	AddDevice(ctx context.Context, deviceID string) (*Device, error)
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
}
