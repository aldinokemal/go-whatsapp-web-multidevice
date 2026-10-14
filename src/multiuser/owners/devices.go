package owners

import (
	"context"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// ScopedDevices wraps upstream's device usecase so the unchanged
// /devices REST handlers only ever see the caller's own devices.
type ScopedDevices struct {
	inner domainDevice.IDeviceUsecase
	st    *store.Store
}

func NewScopedDevices(inner domainDevice.IDeviceUsecase, st *store.Store) domainDevice.IDeviceUsecase {
	return &ScopedDevices{inner: inner, st: st}
}

func (s *ScopedDevices) own(ctx context.Context, deviceID string) error {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return pkgError.ErrDeviceNotFound
	}
	owner, err := s.st.OwnerOf(ctx, deviceID)
	if err != nil || owner != u.ID {
		return pkgError.ErrDeviceNotFound
	}
	return nil
}

func (s *ScopedDevices) ListDevices(ctx context.Context) ([]domainDevice.Device, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return []domainDevice.Device{}, nil
	}
	ids, err := s.st.DevicesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	mine := make(map[string]bool, len(ids))
	for _, id := range ids {
		mine[id] = true
	}
	all, err := s.inner.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := []domainDevice.Device{}
	for _, d := range all {
		if mine[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *ScopedDevices) AddDevice(ctx context.Context, _ string, webhook *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, pkgError.ErrDeviceNotFound
	}
	ids, err := s.st.DevicesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(ids) >= u.MaxDevices {
		return nil, pkgError.ValidationError("you have reached your limit of linked WhatsApp accounts")
	}
	id := NewDeviceID()
	dev, err := s.inner.AddDevice(ctx, id, webhook)
	if err != nil {
		return nil, err
	}
	if err := s.st.SetOwner(ctx, id, u.ID); err != nil {
		_ = s.inner.RemoveDevice(ctx, id) // never leave an ownerless device behind
		return nil, err
	}
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "device_add", DeviceID: id})
	return dev, nil
}

func (s *ScopedDevices) RemoveDevice(ctx context.Context, deviceID string) error {
	if err := s.own(ctx, deviceID); err != nil {
		return err
	}
	if err := s.inner.RemoveDevice(ctx, deviceID); err != nil {
		return err
	}
	u, _ := auth.UserFrom(ctx)
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "device_remove", DeviceID: deviceID})
	return s.st.RemoveOwner(ctx, deviceID)
}

func (s *ScopedDevices) GetDevice(ctx context.Context, id string) (*domainDevice.Device, error) {
	if err := s.own(ctx, id); err != nil {
		return nil, err
	}
	return s.inner.GetDevice(ctx, id)
}

func (s *ScopedDevices) LoginDevice(ctx context.Context, id string) (domainApp.LoginResponse, error) {
	if err := s.own(ctx, id); err != nil {
		return domainApp.LoginResponse{}, err
	}
	return s.inner.LoginDevice(ctx, id)
}

func (s *ScopedDevices) LoginDeviceWithCode(ctx context.Context, id, phone string) (string, error) {
	if err := s.own(ctx, id); err != nil {
		return "", err
	}
	return s.inner.LoginDeviceWithCode(ctx, id, phone)
}

func (s *ScopedDevices) LogoutDevice(ctx context.Context, id string) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.LogoutDevice(ctx, id)
}

func (s *ScopedDevices) ReconnectDevice(ctx context.Context, id string) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.ReconnectDevice(ctx, id)
}

func (s *ScopedDevices) GetStatus(ctx context.Context, id string) (bool, bool, error) {
	if err := s.own(ctx, id); err != nil {
		return false, false, err
	}
	return s.inner.GetStatus(ctx, id)
}

func (s *ScopedDevices) SetDeviceWebhook(ctx context.Context, id, url string) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.SetDeviceWebhook(ctx, id, url)
}

func (s *ScopedDevices) GetDeviceWebhook(ctx context.Context, id string) (string, error) {
	if err := s.own(ctx, id); err != nil {
		return "", err
	}
	return s.inner.GetDeviceWebhook(ctx, id)
}

func (s *ScopedDevices) SetDeviceWebhookConfig(ctx context.Context, id string, cfg *chatstorage.DeviceWebhookConfig) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.SetDeviceWebhookConfig(ctx, id, cfg)
}

func (s *ScopedDevices) GetDeviceWebhookConfig(ctx context.Context, id string) (*chatstorage.DeviceWebhookConfig, error) {
	if err := s.own(ctx, id); err != nil {
		return nil, err
	}
	return s.inner.GetDeviceWebhookConfig(ctx, id)
}
