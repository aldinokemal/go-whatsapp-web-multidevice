package owners

import (
	"context"
	"errors"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/sirupsen/logrus"
)

// ScopedDevices wraps upstream's device usecase so the unchanged
// /devices REST handlers only ever see the caller's own devices.
type ScopedDevices struct {
	inner domainDevice.IDeviceUsecase
	st    *store.Store
}

var ErrDeviceLimitReached = pkgError.ValidationError("you have reached your limit of linked WhatsApp accounts")

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

// AddDevice ignores the requested id: the server picks it. Ownership is
// reserved before the device is created, in one statement that also enforces
// the user's limit, so concurrent adds cannot exceed it and a device never
// exists without an owner.
func (s *ScopedDevices) AddDevice(ctx context.Context, _ string, webhook *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, pkgError.ErrDeviceNotFound
	}
	if err := validateWebhookConfig(ctx, webhook); err != nil {
		return nil, err
	}
	id := NewDeviceID()
	reserved, err := s.st.ReserveOwner(ctx, id, u.ID, u.MaxDevices)
	if err != nil {
		return nil, err
	}
	if !reserved {
		return nil, ErrDeviceLimitReached
	}
	dev, err := s.inner.AddDevice(ctx, id, webhook)
	if err != nil {
		s.rollbackAdd(ctx, id)
		return nil, err
	}
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "device_add", DeviceID: id})
	return dev, nil
}

// rollbackAdd undoes a failed AddDevice. Upstream can create the slot and
// then fail (for example when the webhook config can't be saved), so the slot
// is removed as well as the reservation. It runs even if the request was
// cancelled. Failures are logged by device id only.
func (s *ScopedDevices) rollbackAdd(ctx context.Context, id string) {
	ctx = context.WithoutCancel(ctx)
	if err := s.inner.RemoveDevice(ctx, id); err != nil && !errors.Is(err, pkgError.ErrDeviceNotFound) {
		logrus.WithError(err).Warnf("[WTF] rollback: could not remove half-created device %s", id)
	}
	if err := s.st.RemoveOwner(ctx, id); err != nil {
		logrus.WithError(err).Warnf("[WTF] rollback: could not release reservation for device %s", id)
	}
}

// RemoveDevice also clears a "ghost": an owner row whose device the manager no
// longer has. Without that, the row would count against the limit forever.
func (s *ScopedDevices) RemoveDevice(ctx context.Context, deviceID string) error {
	if err := s.own(ctx, deviceID); err != nil {
		return err
	}
	if err := s.inner.RemoveDevice(ctx, deviceID); err != nil && !errors.Is(err, pkgError.ErrDeviceNotFound) {
		return err
	}
	u, _ := auth.UserFrom(ctx)
	number := "" // kept in the audit line: the slot row that holds it is about to go
	if sl, err := s.st.SlotOf(ctx, deviceID); err == nil {
		number = sl.LastPhone
	}
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, ActorID: actorID(ctx), Action: "device_remove", DeviceID: deviceID, Detail: number})
	return s.st.RemoveOwner(ctx, deviceID)
}

func actorID(ctx context.Context) string {
	if a, ok := auth.ActorFrom(ctx); ok {
		return a.ID
	}
	return ""
}

// RemoveDeviceAsAdmin unlinks one of target's numbers on an admin's behalf.
// It runs the very same owner-checked RemoveDevice the user's own Unlink
// uses, with target as the acting user, so a device that is not target's is
// ErrDeviceNotFound. The audit line names admin as the actor. The caller must
// already have checked that admin is a superadmin.
func RemoveDeviceAsAdmin(ctx context.Context, devices domainDevice.IDeviceUsecase, admin, target *store.User, deviceID string) error {
	return devices.RemoveDevice(asAdmin(ctx, admin, target), deviceID)
}

// RemoveAllDevicesAsAdmin unlinks every number target owns, the same way.
// It returns how many were removed and stops at the first failure.
func RemoveAllDevicesAsAdmin(ctx context.Context, devices domainDevice.IDeviceUsecase, st *store.Store, admin, target *store.User) (int, error) {
	ids, err := st.DevicesOf(ctx, target.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if err := RemoveDeviceAsAdmin(ctx, devices, admin, target, id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func asAdmin(ctx context.Context, admin, target *store.User) context.Context {
	return auth.WithActor(auth.WithUser(ctx, target), admin)
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
	if err := ValidateWebhookURL(ctx, url); err != nil {
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
	if err := validateWebhookConfig(ctx, cfg); err != nil {
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

// ListDevicesAsAdmin lists target's numbers for an admin, through the same
// owner-scoped ListDevices the user's own page uses.
func ListDevicesAsAdmin(ctx context.Context, devices domainDevice.IDeviceUsecase, admin, target *store.User) ([]domainDevice.Device, error) {
	return devices.ListDevices(asAdmin(ctx, admin, target))
}
