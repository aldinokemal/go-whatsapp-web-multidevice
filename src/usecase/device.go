package usecase

import (
	"context"
	"fmt"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/sirupsen/logrus"
)

type serviceDevice struct {
	manager *whatsapp.DeviceManager
}

func NewDeviceService(manager *whatsapp.DeviceManager) domainDevice.IDeviceUsecase {
	return &serviceDevice{
		manager: manager,
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

func (s *serviceDevice) AddDevice(ctx context.Context, deviceID string) (*domainDevice.Device, error) {
	if s.manager == nil {
		return nil, fmt.Errorf("device manager not initialized")
	}

	inst, err := s.manager.CreateDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	device := convertInstance(inst)
	return &device, nil
}

func (s *serviceDevice) RemoveDevice(ctx context.Context, deviceID string) error {
	if s.manager == nil {
		return fmt.Errorf("device manager not initialized")
	}
	// Deleting a device fully purges it: logs it out of WhatsApp, clears its session
	// and chat data, then removes the slot from the registry. (Logout, in contrast,
	// keeps the slot.) Purge removes the slot even if the WhatsApp logout reports
	// warnings (e.g. an already-dead session), so don't fail the delete on those.
	if err := s.manager.PurgeDevice(ctx, deviceID); err != nil {
		logrus.WithError(err).Warnf("[DEVICE] purge for %s completed with warnings", deviceID)
	}
	return nil
}

func (s *serviceDevice) LoginDevice(_ context.Context, _ string) error {
	return fmt.Errorf("device login per ID is not implemented yet")
}

func (s *serviceDevice) LoginDeviceWithCode(_ context.Context, _ string, _ string) (string, error) {
	return "", fmt.Errorf("device login with code is not implemented yet")
}

func (s *serviceDevice) LogoutDevice(ctx context.Context, deviceID string) error {
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
