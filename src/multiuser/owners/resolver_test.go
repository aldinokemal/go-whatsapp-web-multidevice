package owners

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

type fakeDM map[string]*whatsapp.DeviceInstance

func (f fakeDM) GetDevice(id string) (*whatsapp.DeviceInstance, bool) { d, ok := f[id]; return d, ok }

func fixture(t *testing.T) (*store.Store, fakeDM, *store.User, *store.User) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")
	dm := fakeDM{}
	for _, id := range []string{"d_a1", "d_a2", "d_b1", "d_orphan"} {
		dm[id] = whatsapp.NewDeviceInstance(id, nil, nil)
	}
	require.NoError(t, st.SetOwner(ctx, "d_a1", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_a2", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_b1", b.ID))
	return st, dm, a, b
}

func TestResolveOnlyOwnDevices(t *testing.T) {
	st, dm, a, b := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	ctxB := auth.WithUser(context.Background(), b)

	inst, id, err := r.Resolve(ctxA, "d_a1")
	require.NoError(t, err)
	require.Equal(t, "d_a1", id)
	require.Equal(t, "d_a1", inst.ID())

	_, id, err = r.Resolve(ctxA, "d_b1")
	require.Error(t, err, "another user's device")
	require.Equal(t, "d_b1", id, "reported like a missing device, so the middleware answers 404")

	_, _, err = r.Resolve(ctxA, "d_orphan")
	require.Error(t, err, "a device nobody owns is invisible")

	_, _, err = r.Resolve(ctxA, "")
	require.ErrorIs(t, err, ErrDeviceIDRequired, "two devices and no id: ask, never guess")

	inst, id, err = r.Resolve(ctxB, "")
	require.NoError(t, err, "one device: it is the default")
	require.Equal(t, "d_b1", id)
	require.Equal(t, "d_b1", inst.ID())

	_, _, err = r.Resolve(context.Background(), "d_a1")
	require.Error(t, err, "no user in context")
}

func TestResolveUserWithNoDevices(t *testing.T) {
	st, dm, _, _ := fixture(t)
	c, _ := st.CreateUser(context.Background(), "+27825550102")
	_, id, err := NewResolver(dm, st).Resolve(auth.WithUser(context.Background(), c), "")
	require.ErrorIs(t, err, ErrNoDeviceLinked)
	require.Empty(t, id)
}

func TestResolveNeverFallsBackToAnotherDevice(t *testing.T) {
	st, dm, a, _ := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	for _, forged := range []string{"d_b1 ", " d_b1", "d_missing", "D_A1"} {
		inst, _, err := r.Resolve(ctxA, forged)
		require.Error(t, err, forged)
		require.Nil(t, inst, forged)
	}
	// Owned but not loaded in the device manager: 404 with the id, never a substitute.
	require.NoError(t, st.SetOwner(context.Background(), "d_gone", a.ID))
	inst, id, err := r.Resolve(ctxA, "d_gone")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound)
	require.Equal(t, "d_gone", id)
	require.Nil(t, inst)
}

func TestNewDeviceID(t *testing.T) {
	a, b := NewDeviceID(), NewDeviceID()
	require.Regexp(t, `^d_[0-9a-f]{32}$`, a)
	require.NotEqual(t, a, b)
}
