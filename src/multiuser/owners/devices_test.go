package owners

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
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

// syncInner is safe for concurrent AddDevice calls.
type syncInner struct {
	*innerStub
	mu sync.Mutex
}

func (s *syncInner) AddDevice(ctx context.Context, id string, w *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	time.Sleep(10 * time.Millisecond) // creating a device is slow; a check-then-insert limit loses this race
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.innerStub.AddDevice(ctx, id, w)
}

func TestAddDeviceLimitHoldsUnderConcurrency(t *testing.T) {
	st, _, _, _ := fixture(t)
	c, _ := st.CreateUser(context.Background(), "+27825550102") // MaxDevices 2
	inner := &syncInner{innerStub: &innerStub{}}
	svc := NewScopedDevices(inner, st)
	ctxC := auth.WithUser(context.Background(), c)

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, limited := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.AddDevice(ctxC, "", nil)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if errors.Is(err, ErrDeviceLimitReached) {
				limited++
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 2, wins)
	require.Equal(t, 14, limited, "every loser is told about the limit")
	ids, err := st.DevicesOf(context.Background(), c.ID)
	require.NoError(t, err)
	require.Len(t, ids, 2)
	require.Len(t, inner.added, 2, "the inner usecase only created the two reserved devices")
}

// failingInner creates the slot, then fails (as upstream does when the webhook
// config can't be saved).
type failingInner struct {
	*innerStub
	removeErr error
}

func (f *failingInner) AddDevice(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	f.added = append(f.added, id)
	return nil, errors.New("device created but webhook config could not be saved")
}
func (f *failingInner) RemoveDevice(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return f.removeErr
}

func TestAddDeviceRollsBackWhenInnerFails(t *testing.T) {
	for name, removeErr := range map[string]error{
		"slot removed":       nil,
		"slot already gone":  pkgError.ErrDeviceNotFound,
		"slot removal fails": errors.New("disk full"),
	} {
		t.Run(name, func(t *testing.T) {
			st, _, _, _ := fixture(t)
			c, _ := st.CreateUser(context.Background(), "+27825550102")
			inner := &failingInner{innerStub: &innerStub{}, removeErr: removeErr}
			svc := NewScopedDevices(inner, st)
			ctxC := auth.WithUser(context.Background(), c)

			_, err := svc.AddDevice(ctxC, "", nil)
			require.Error(t, err)
			require.Len(t, inner.added, 1)
			require.Equal(t, inner.added, inner.removed, "the half-made slot is removed")
			ids, _ := st.DevicesOf(context.Background(), c.ID)
			require.Empty(t, ids, "the reservation is released, so it does not use up the limit")
		})
	}
}

// ghostInner has no manager entry for any device.
type ghostInner struct{ *innerStub }

func (g *ghostInner) RemoveDevice(_ context.Context, id string) error {
	g.removed = append(g.removed, id)
	return pkgError.ErrDeviceNotFound
}

func TestRemoveGhostDeviceClearsOwnership(t *testing.T) {
	st, _, a, b := fixture(t)
	inner := &ghostInner{innerStub: &innerStub{}}
	svc := NewScopedDevices(inner, st)

	require.NoError(t, svc.RemoveDevice(auth.WithUser(context.Background(), a), "d_a1"))
	_, err := st.OwnerOf(context.Background(), "d_a1")
	require.ErrorIs(t, err, store.ErrNotFound, "a ghost row would use up the limit forever")

	// The ghost path never lets someone else's row be cleared.
	require.ErrorIs(t, svc.RemoveDevice(auth.WithUser(context.Background(), a), "d_b1"), pkgError.ErrDeviceNotFound)
	owner, err := st.OwnerOf(context.Background(), "d_b1")
	require.NoError(t, err)
	require.Equal(t, b.ID, owner)
}
