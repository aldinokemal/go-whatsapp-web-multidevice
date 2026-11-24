package multiuser

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/stretchr/testify/require"
)

// drainBroadcasts stands in for the websocket hub cmd/rest.go starts: the
// upstream RemoveDevice announces each removal on an unbuffered channel.
var drainBroadcasts sync.Once

func phase34Runtime(t *testing.T, mod func(*Settings)) (*Runtime, *whatsapp.DeviceManager) {
	t.Helper()
	drainBroadcasts.Do(func() {
		go func() {
			for range websocket.Broadcast {
			}
		}()
	})
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{},
		DefaultMaxDevices: 3}
	if mod != nil {
		mod(&s)
	}
	rt, err := Setup(s, dm, usecase.NewDeviceService(dm, nil), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })
	return rt, dm
}

// Wiring (phase 4): the sign-in handler reads the real capacity, puts people
// on the real waitlist and gives new accounts the default limit; the web
// pages get the same.
func TestSigninHandlerUsesTheRealCapacity(t *testing.T) {
	rt, _ := phase34Runtime(t, func(s *Settings) { s.MaxPhones = 1 })
	ctx := context.Background()
	require.NotNil(t, rt.Signin.Full)
	require.False(t, rt.Signin.Full(ctx))
	u, err := rt.Store.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	require.NoError(t, rt.Store.SetOwner(ctx, "d_x", u.ID))
	require.True(t, rt.Signin.Full(ctx), "Full is capacity.Service.Full over the same store")
	require.Equal(t, rt.Store, rt.Signin.Waitlist)
	require.Equal(t, 3, rt.Signin.DefaultMaxDevices)
	require.False(t, rt.Signin.SignupOpen, "sign-up stays closed unless WTF_SIGNUP_OPEN says otherwise")
	require.NotNil(t, rt.Web.Capacity)
	require.NotNil(t, rt.Web.SendWhatsApp)
	require.NotNil(t, rt.Web.DeleteAccount)
	require.Equal(t, 3, rt.Web.DefaultMaxDevices)
}

// Wiring (phase 4): linking through the scoped usecase obeys the server cap.
func TestScopedDevicesUseTheServerCap(t *testing.T) {
	rt, _ := phase34Runtime(t, func(s *Settings) { s.MaxPhones = 1 })
	ctx := context.Background()
	a, _ := rt.Store.CreateUser(ctx, "+27825550101")
	b, _ := rt.Store.CreateUser(ctx, "+27825550102")
	require.NoError(t, rt.Store.SetOwner(ctx, "d_a", a.ID))
	_, err := rt.Devices.AddDevice(auth.WithUser(ctx, b), "", nil)
	require.ErrorIs(t, err, owners.ErrServerFull)
}

// Wiring (phase 3): an account left "deleting" by a crash is finished once
// the runtime starts.
func TestPendingDeletionsResumeAtStartup(t *testing.T) {
	rt, dm := phase34Runtime(t, nil)
	ctx := context.Background()
	_, err := dm.CreateDevice(ctx, "d_half")
	require.NoError(t, err)
	u, _ := rt.Store.CreateUser(ctx, "+27825550101")
	require.NoError(t, rt.Store.SetOwner(ctx, "d_half", u.ID))
	require.NoError(t, rt.Store.SetUserStatus(ctx, u.ID, store.StatusDeleting))
	chats := &purgeRecorder{}
	rt.Start(chats)
	require.Eventually(t, func() bool {
		_, err := rt.Store.UserByID(ctx, u.ID)
		return errors.Is(err, store.ErrNotFound)
	}, 5*time.Second, 20*time.Millisecond)
	_, ok := dm.GetDevice("d_half")
	require.False(t, ok, "its number was purged")
	require.Equal(t, []string{"d_half"}, chats.ids())
}

// Wiring: Start runs the stale-slot sweeper; a slot nobody scanned for over
// an hour is freed, a fresh one is kept.
func TestStartFreesStaleSlots(t *testing.T) {
	rt, dm := phase34Runtime(t, nil)
	ctx := context.Background()
	for _, id := range []string{"d_old", "d_new"} {
		_, err := dm.CreateDevice(ctx, id)
		require.NoError(t, err)
	}
	u, _ := rt.Store.CreateUser(ctx, "+27825550101")
	rt.Store.Now = func() time.Time { return time.Now().UTC().Add(-2 * time.Hour) }
	require.NoError(t, rt.Store.SetOwner(ctx, "d_old", u.ID))
	rt.Store.Now = func() time.Time { return time.Now().UTC() }
	require.NoError(t, rt.Store.SetOwner(ctx, "d_new", u.ID))
	rt.Start(nil)
	require.Eventually(t, func() bool {
		ids, _ := rt.Store.DevicesOf(ctx, u.ID)
		return len(ids) == 1 && ids[0] == "d_new"
	}, 5*time.Second, 20*time.Millisecond)
	_, ok := dm.GetDevice("d_old")
	require.False(t, ok)
	_, ok = dm.GetDevice("d_new")
	require.True(t, ok)
	require.ErrorIs(t, rt.Deleter.Devices.RemoveDevice(ctx, "d_old"), pkgError.ErrDeviceNotFound)
}

type chatsRecorder struct {
	mu  sync.Mutex
	got []string
}

func (c *chatsRecorder) DeleteDeviceData(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, id)
	return nil
}

func (c *chatsRecorder) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got...)
}

// Wiring: an account that gets stuck "deleting" while the server runs (a
// WhatsApp error half way) is finished by the next hourly pass, not only at
// the next restart, with retention on or off.
func TestStuckDeletionsAreRetriedWhileRunning(t *testing.T) {
	for _, hours := range []int{0, 24} {
		t.Run(fmt.Sprintf("retention %dh", hours), func(t *testing.T) {
			rt, _ := phase34Runtime(t, func(s *Settings) { s.RetentionHours = hours })
			rt.ResumeDeletionsEvery = 50 * time.Millisecond
			ctx := context.Background()
			rt.Start(&purgeRecorder{})
			// Start's own resume has had its chance; this account goes stuck after it.
			time.Sleep(100 * time.Millisecond)
			u, err := rt.Store.CreateUser(ctx, "+27825550101")
			require.NoError(t, err)
			require.NoError(t, rt.Store.SetUserStatus(ctx, u.ID, store.StatusDeleting))
			require.Eventually(t, func() bool {
				_, err := rt.Store.UserByID(ctx, u.ID)
				return errors.Is(err, store.ErrNotFound)
			}, 5*time.Second, 20*time.Millisecond)
		})
	}
}

// The default retry interval is the hour retention runs on.
func TestStuckDeletionsAreRetriedHourly(t *testing.T) {
	require.Equal(t, time.Hour, resumeDeletionsEvery)
}
