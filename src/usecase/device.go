package usecase

import (
	"context"
	"fmt"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

type serviceDevice struct {
	manager *whatsapp.DeviceManager
	app     domainApp.IAppUsecase
}

func NewDeviceService(manager *whatsapp.DeviceManager, app domainApp.IAppUsecase) domainDevice.IDeviceUsecase {
	return &serviceDevice{
		manager: manager,
		app:     app,
	}
}

func (s *serviceDevice) ListDevices(_ context.Context) ([]domainDevice.Device, error) {
	if s.manager == nil {
		return []domainDevice.Device{}, nil
	}

	var result []domainDevice.Device
	for _, inst := range s.manager.ListDevices() {
		inst.UpdateStateFromClient()
		result = append(result, convertInstance(inst))
	}
	return result, nil
}

func (s *serviceDevice) GetDevice(_ context.Context, deviceID string) (*domainDevice.Device, error) {
	if s.manager == nil {
		return nil, fmt.Errorf("device manager not initialized")
	}
	if inst, ok := s.manager.GetDevice(deviceID); ok {
		device := convertInstance(inst)
		return &device, nil
	}
	return nil, fmt.Errorf("device %s not found", deviceID)
}

func (s *serviceDevice) AddDevice(ctx context.Context, deviceID string, webhook *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	if s.manager == nil {
		return nil, fmt.Errorf("device manager not initialized")
	}

	inst, err := s.manager.CreateDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	// Apply the device-specific webhook configuration if provided. Failures are
	// surfaced instead of logged away: the caller was promised the webhook config,
	// so a silent partial success would leave the API reporting a webhook that was
	// never persisted.
	if webhook != nil {
		storage := s.manager.GetStorage()
		if storage == nil {
			return nil, fmt.Errorf("device %s created but storage is unavailable to save webhook config", deviceID)
		}
		if err := storage.SetDeviceWebhookConfig(deviceID, webhook); err != nil {
			return nil, fmt.Errorf("device %s created but webhook config could not be saved: %w", deviceID, err)
		}
	}

	device := convertInstance(inst)
	return &device, nil
}

func (s *serviceDevice) RemoveDevice(ctx context.Context, deviceID string) error {
	if err := validations.ValidateDeviceID(ctx, deviceID); err != nil {
		return err
	}
	if s.manager == nil {
		return fmt.Errorf("device manager not initialized")
	}
	// Deleting a device fully purges it: logs it out of WhatsApp, clears its session
	// and chat data, then removes the slot from the registry. (Logout, in contrast,
	// keeps the slot.) The WhatsApp unlink itself is best-effort inside PurgeDevice
	// (an already-dead session must not block the delete), but failures of the LOCAL
	// cleanup (chatstorage / store / keys rows) are surfaced: DELETE promises a real
	// purge, so we propagate the error instead of masking it as success.
	if err := s.manager.PurgeDevice(ctx, deviceID); err != nil {
		logrus.WithError(err).Warnf("[DEVICE] purge for %s failed", deviceID)
		return fmt.Errorf("purge device %s: %w", deviceID, err)
	}
	return nil
}

func (s *serviceDevice) LoginDevice(ctx context.Context, deviceID string) (domainApp.LoginResponse, error) {
	if s.app == nil {
		return domainApp.LoginResponse{}, fmt.Errorf("app usecase not initialized")
	}
	return s.app.Login(ctx, deviceID)
}

func (s *serviceDevice) LoginDeviceWithCode(ctx context.Context, deviceID string, phone string) (string, error) {
	if s.app == nil {
		return "", fmt.Errorf("app usecase not initialized")
	}
	return s.app.LoginWithCode(ctx, deviceID, phone)
}

func (s *serviceDevice) LogoutDevice(ctx context.Context, deviceID string) error {
	if err := validations.ValidateDeviceID(ctx, deviceID); err != nil {
		return err
	}
	if s.manager == nil {
		return fmt.Errorf("device manager not initialized")
	}

	if err := s.manager.LogoutDeviceKeepSlot(ctx, deviceID); err != nil {
		return err
	}

	// Broadcast the logout so UI clients can refresh. The device slot is kept, so it
	// still appears in the list (disconnected) and can be re-paired under the same id.
	var devices []domainDevice.Device
	if s.manager != nil {
		for _, inst := range s.manager.ListDevices() {
			inst.UpdateStateFromClient()
			devices = append(devices, convertInstance(inst))
		}
	}

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "DEVICE_LOGGED_OUT",
		Message: fmt.Sprintf("Device %s logged out (slot kept)", deviceID),
		Result: map[string]any{
			"device_id": deviceID,
			"devices":   devices,
		},
	}

	return nil
}

func (s *serviceDevice) ReconnectDevice(_ context.Context, deviceID string) error {
	if s.manager == nil {
		return fmt.Errorf("device manager not initialized")
	}
	if inst, ok := s.manager.GetDevice(deviceID); ok {
		client := inst.GetClient()
		if client == nil {
			return fmt.Errorf("device %s client not initialized", deviceID)
		}

		if client.Store == nil || client.Store.ID == nil {
			return fmt.Errorf("device %s is not logged in (session deleted)", deviceID)
		}

		client.Disconnect()
		return client.Connect()
	}
	return fmt.Errorf("device %s not found", deviceID)
}

func (s *serviceDevice) GetStatus(_ context.Context, deviceID string) (bool, bool, error) {
	if s.manager == nil {
		return false, false, fmt.Errorf("device manager not initialized")
	}
	if inst, ok := s.manager.GetDevice(deviceID); ok {
		inst.UpdateStateFromClient()
		client := inst.GetClient()
		if client == nil {
			return false, false, nil
		}

		if client.Store == nil || client.Store.ID == nil {
			return false, false, nil
		}

		// Update state snapshot based on live client flags
		state := deriveState(inst)
		_ = state
		return client.IsConnected(), client.IsLoggedIn(), nil
	}
	return false, false, fmt.Errorf("device %s not found", deviceID)
}

// SetDeviceWebhook sets the webhook URL for a specific device.
func (s *serviceDevice) SetDeviceWebhook(ctx context.Context, deviceID string, webhookURL string) error {
	if s.manager == nil {
		return fmt.Errorf("device manager not initialized")
	}

	_, ok := s.manager.GetDevice(deviceID)
	if !ok {
		return pkgError.ErrDeviceNotFound
	}

	storage := s.manager.GetStorage()
	if storage == nil {
		return fmt.Errorf("storage not available")
	}

	var urlPtr *string
	if webhookURL != "" {
		urlPtr = &webhookURL
	}

	// Use deviceID (the actual device identifier) for webhook persistence
	if err := storage.SetDeviceWebhookURL(deviceID, urlPtr); err != nil {
		return fmt.Errorf("failed to set device webhook: %w", err)
	}

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "DEVICE_WEBHOOK_UPDATED",
		Message: fmt.Sprintf("Device %s webhook updated", deviceID),
		Result: map[string]any{
			"device_id":   deviceID,
			"webhook_url": webhookURL,
		},
	}

	return nil
}

// GetDeviceWebhook retrieves the webhook URL for a specific device.
func (s *serviceDevice) GetDeviceWebhook(ctx context.Context, deviceID string) (string, error) {
	if s.manager == nil {
		return "", fmt.Errorf("device manager not initialized")
	}

	_, ok := s.manager.GetDevice(deviceID)
	if !ok {
		return "", fmt.Errorf("device %s not found", deviceID)
	}

	storage := s.manager.GetStorage()
	if storage == nil {
		return "", fmt.Errorf("storage not available")
	}

	// Use deviceID (the actual device identifier) for webhook retrieval
	webhookURL, err := storage.GetDeviceWebhookURL(deviceID)
	if err != nil {
		return "", fmt.Errorf("failed to get device webhook: %w", err)
	}

	if webhookURL == nil {
		return "", nil
	}

	return *webhookURL, nil
}

// SetDeviceWebhookConfig sets the complete webhook configuration for a specific device.
func (s *serviceDevice) SetDeviceWebhookConfig(ctx context.Context, deviceID string, config *chatstorage.DeviceWebhookConfig) error {
	if s.manager == nil {
		return fmt.Errorf("device manager not initialized")
	}

	_, ok := s.manager.GetDevice(deviceID)
	if !ok {
		return pkgError.ErrDeviceNotFound
	}

	storage := s.manager.GetStorage()
	if storage == nil {
		return fmt.Errorf("storage not available")
	}

	if err := storage.SetDeviceWebhookConfig(deviceID, config); err != nil {
		return fmt.Errorf("failed to set device webhook config: %w", err)
	}

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "DEVICE_WEBHOOK_CONFIG_UPDATED",
		Message: fmt.Sprintf("Device %s webhook config updated", deviceID),
		Result: map[string]any{
			"device_id": deviceID,
		},
	}

	return nil
}

// GetDeviceWebhookConfig retrieves the complete webhook configuration for a specific device.
func (s *serviceDevice) GetDeviceWebhookConfig(ctx context.Context, deviceID string) (*chatstorage.DeviceWebhookConfig, error) {
	if s.manager == nil {
		return nil, fmt.Errorf("device manager not initialized")
	}

	_, ok := s.manager.GetDevice(deviceID)
	if !ok {
		return nil, pkgError.ErrDeviceNotFound
	}

	storage := s.manager.GetStorage()
	if storage == nil {
		return nil, fmt.Errorf("storage not available")
	}

	config, err := storage.GetDeviceWebhookConfig(deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get device webhook config: %w", err)
	}

	return config, nil
}

func convertInstance(inst *whatsapp.DeviceInstance) domainDevice.Device {
	if inst == nil {
		return domainDevice.Device{}
	}

	state := deriveState(inst)

	return domainDevice.Device{
		ID:          inst.ID(),
		PhoneNumber: inst.PhoneNumber(),
		DisplayName: inst.DisplayName(),
		State:       state,
		JID:         inst.JID(),
		CreatedAt:   inst.CreatedAt(),
	}
}

func deriveState(inst *whatsapp.DeviceInstance) domainDevice.DeviceState {
	if inst == nil {
		return domainDevice.DeviceStateDisconnected
	}

	client := inst.GetClient()
	state := inst.State()
	if client != nil {
		if client.IsLoggedIn() {
			state = domainDevice.DeviceStateLoggedIn
		} else if client.IsConnected() {
			state = domainDevice.DeviceStateConnected
		} else {
			state = domainDevice.DeviceStateDisconnected
		}
		inst.SetState(state)
	}

	return state
}
