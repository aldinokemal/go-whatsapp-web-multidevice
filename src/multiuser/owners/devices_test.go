package owners

import (
	"context"
	"testing"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

// innerStub records calls; every method succeeds.
type innerStub struct {
	added, removed, touched []string
	all                     []domainDevice.Device
}

func (s *innerStub) ListDevices(context.Context) ([]domainDevice.Device, error) { return s.all, nil }
func (s *innerStub) GetDevice(_ context.Context, id string) (*domainDevice.Device, error) {
	s.touched = append(s.touched, id)
	return &domainDevice.Device{ID: id}, nil
}
func (s *innerStub) AddDevice(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	s.added = append(s.added, id)
	return &domainDevice.Device{ID: id}, nil
}
func (s *innerStub) RemoveDevice(_ context.Context, id string) error {
	s.removed = append(s.removed, id)
	return nil
}
func (s *innerStub) LoginDevice(_ context.Context, id string) (domainApp.LoginResponse, error) {
	s.touched = append(s.touched, id)
	return domainApp.LoginResponse{}, nil
}
func (s *innerStub) LoginDeviceWithCode(_ context.Context, id, _ string) (string, error) {
	s.touched = append(s.touched, id)
	return "", nil
}
func (s *innerStub) LogoutDevice(_ context.Context, id string) error {
	s.touched = append(s.touched, id)
	return nil
}
func (s *innerStub) ReconnectDevice(_ context.Context, id string) error {
	s.touched = append(s.touched, id)
	return nil
}
func (s *innerStub) GetStatus(_ context.Context, id string) (bool, bool, error) {
	s.touched = append(s.touched, id)
	return true, true, nil
}
func (s *innerStub) SetDeviceWebhook(_ context.Context, id, _ string) error {
	s.touched = append(s.touched, id)
	return nil
}
func (s *innerStub) GetDeviceWebhook(_ context.Context, id string) (string, error) {
	s.touched = append(s.touched, id)
	return "", nil
}
func (s *innerStub) SetDeviceWebhookConfig(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) error {
	s.touched = append(s.touched, id)
	return nil
}
func (s *innerStub) GetDeviceWebhookConfig(_ context.Context, id string) (*chatstorage.DeviceWebhookConfig, error) {
	s.touched = append(s.touched, id)
	return nil, nil
}

func TestScopedDevicesIsolation(t *testing.T) {
	st, _, a, b := fixture(t)
	inner := &innerStub{all: []domainDevice.Device{{ID: "d_a1"}, {ID: "d_a2"}, {ID: "d_b1"}, {ID: "d_orphan"}}}
	svc := NewScopedDevices(inner, st)
	ctxA := auth.WithUser(context.Background(), a)

	list, err := svc.ListDevices(ctxA)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"d_a1", "d_a2"}, []string{list[0].ID, list[1].ID})
	require.Len(t, list, 2)

	calls := map[string]func() error{
		"GetDevice":              func() error { _, err := svc.GetDevice(ctxA, "d_b1"); return err },
		"RemoveDevice":           func() error { return svc.RemoveDevice(ctxA, "d_b1") },
		"LoginDevice":            func() error { _, err := svc.LoginDevice(ctxA, "d_b1"); return err },
		"LoginDeviceWithCode":    func() error { _, err := svc.LoginDeviceWithCode(ctxA, "d_b1", "123"); return err },
		"LogoutDevice":           func() error { return svc.LogoutDevice(ctxA, "d_b1") },
		"ReconnectDevice":        func() error { return svc.ReconnectDevice(ctxA, "d_b1") },
		"GetStatus":              func() error { _, _, err := svc.GetStatus(ctxA, "d_b1"); return err },
		"SetDeviceWebhook":       func() error { return svc.SetDeviceWebhook(ctxA, "d_b1", "https://x") },
		"GetDeviceWebhook":       func() error { _, err := svc.GetDeviceWebhook(ctxA, "d_b1"); return err },
		"SetDeviceWebhookConfig": func() error { return svc.SetDeviceWebhookConfig(ctxA, "d_b1", nil) },
		"GetDeviceWebhookConfig": func() error { _, err := svc.GetDeviceWebhookConfig(ctxA, "d_b1"); return err },
	}
	for name, call := range calls {
		require.ErrorIs(t, call(), pkgError.ErrDeviceNotFound, name)
	}
	require.Empty(t, inner.touched, "the inner usecase never saw b's device")
	require.Empty(t, inner.removed)
	_ = b
}

func TestAddDeviceGeneratesIDOwnsItAndEnforcesLimit(t *testing.T) {
	st, _, a, _ := fixture(t) // a already owns 2 devices; MaxDevices defaults to 2
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	_, err := svc.AddDevice(auth.WithUser(context.Background(), a), "chosen-by-user", nil)
	require.Error(t, err, "limit reached")
	require.Empty(t, inner.added)

	c, _ := st.CreateUser(context.Background(), "+27825550102")
	dev, err := svc.AddDevice(auth.WithUser(context.Background(), c), "chosen-by-user", nil)
	require.NoError(t, err)
	require.Regexp(t, `^d_[0-9a-f]{32}$`, dev.ID, "server picks the id; the requested id is ignored")
	owner, err := st.OwnerOf(context.Background(), dev.ID)
	require.NoError(t, err)
	require.Equal(t, c.ID, owner)

	require.NoError(t, svc.RemoveDevice(auth.WithUser(context.Background(), c), dev.ID))
	_, err = st.OwnerOf(context.Background(), dev.ID)
	require.Error(t, err, "ownership row removed with the device")
}

func TestScopedDevicesWithoutUserSeesNothing(t *testing.T) {
	st, _, _, _ := fixture(t)
	inner := &innerStub{all: []domainDevice.Device{{ID: "d_a1"}, {ID: "d_b1"}, {ID: "d_orphan"}}}
	svc := NewScopedDevices(inner, st)
	ctx := context.Background()

	list, err := svc.ListDevices(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
	_, err = svc.GetDevice(ctx, "d_a1")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound)
	_, err = svc.AddDevice(ctx, "x", nil)
	require.Error(t, err)
	require.Empty(t, inner.touched)
	require.Empty(t, inner.added)
}

func TestScopedDevicesOrphanIsInvisible(t *testing.T) {
	st, _, a, _ := fixture(t)
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	_, err := svc.GetDevice(auth.WithUser(context.Background(), a), "d_orphan")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound)
	require.Empty(t, inner.touched)
}
