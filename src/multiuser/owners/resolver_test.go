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
	"go.mau.fi/whatsmeow"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
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

	// Two devices, no id, neither named: the message itself must say how to
	// get out (name one) rather than just the old unactionable refusal.
	_, _, err = r.Resolve(ctxA, "")
	require.Error(t, err, "two devices and no id: ask, never guess")
	require.NotErrorIs(t, err, ErrDeviceIDRequired, "the old static wording is intentionally gone")
	require.Contains(t, err.Error(), "wtf_rename_number", "nothing is named yet, so the way out is naming one")

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

// instanceWithPhone builds a *whatsapp.DeviceInstance that reports a phone
// number, the way a logged-in device would. The phone number is populated by
// refreshIdentityLocked, which only runs from SetClient (NewDeviceInstance's
// own constructor leaves phoneNumber unset even when given a client), so
// this goes through SetClient rather than passing the client to the
// constructor - no real whatsmeow session needed either way.
func instanceWithPhone(id, phone string) *whatsapp.DeviceInstance {
	jid := types.NewJID(phone, types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &waStore.Device{ID: &jid}}
	inst := whatsapp.NewDeviceInstance(id, nil, nil)
	inst.SetClient(client)
	return inst
}

func TestResolveByLabel(t *testing.T) {
	st, dm, a, _ := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	require.NoError(t, st.SetLabel(context.Background(), a.ID, "d_a1", "Personal"))

	for _, typed := range []string{"Personal", "personal", "PERSONAL", "  Personal  ", "PeRsOnAl "} {
		inst, id, err := r.Resolve(ctxA, typed)
		require.NoError(t, err, "typed %q", typed)
		require.Equal(t, "d_a1", id, "typed %q", typed)
		require.Equal(t, "d_a1", inst.ID(), "typed %q", typed)
	}
}

func TestResolveByLabelNoMatchIsReportedLikeNotYours(t *testing.T) {
	st, dm, a, _ := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	require.NoError(t, st.SetLabel(context.Background(), a.ID, "d_a1", "Personal"))

	inst, id, err := r.Resolve(ctxA, "Nonexistent")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound, "a label that matches nothing must not be distinguishable from a wrong id")
	require.Equal(t, "Nonexistent", id)
	require.Nil(t, inst)
}

func TestResolveByLabelAmbiguousAcrossOwnDevices(t *testing.T) {
	st, dm, a, _ := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	require.NoError(t, st.SetLabel(context.Background(), a.ID, "d_a1", "Phone"))
	require.NoError(t, st.SetLabel(context.Background(), a.ID, "d_a2", "phone"))

	inst, _, err := r.Resolve(ctxA, "Phone")
	require.Error(t, err)
	require.Nil(t, inst)
	require.NotErrorIs(t, err, pkgError.ErrDeviceNotFound, "ambiguous is a different, nameable problem, not a plain miss")
	var ve pkgError.ValidationError
	require.ErrorAs(t, err, &ve)
	require.Contains(t, err.Error(), "more than one")
}

func TestResolveByLabelNeverCrossesUsers(t *testing.T) {
	st, dm, a, b := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	require.NoError(t, st.SetLabel(context.Background(), b.ID, "d_b1", "Shared Name"))

	inst, id, err := r.Resolve(ctxA, "Shared Name")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound, "another user's label must never match, by construction")
	require.Equal(t, "Shared Name", id)
	require.Nil(t, inst)
}

func TestResolveExactIDStillWinsOverAnotherDevicesLabel(t *testing.T) {
	st, dm, a, _ := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	// d_a2 is named exactly like d_a1's own device_id string. Passing that
	// literal id must still resolve to the device the id itself names - the
	// exact-id path is unchanged, label matching only ever runs as a fallback.
	require.NoError(t, st.SetLabel(context.Background(), a.ID, "d_a2", "d_a1"))

	inst, id, err := r.Resolve(ctxA, "d_a1")
	require.NoError(t, err)
	require.Equal(t, "d_a1", id)
	require.Equal(t, "d_a1", inst.ID())
}

func TestDeviceChoiceMessageListsEveryDeviceByNameOrPhone(t *testing.T) {
	st, dm, a, _ := fixture(t)
	dm["d_a1"] = instanceWithPhone("d_a1", "27825550199")
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	require.NoError(t, st.SetLabel(context.Background(), a.ID, "d_a2", "Work"))

	_, _, err := r.Resolve(ctxA, "")
	require.Error(t, err)
	// d_a1 has no label, so its phone number stands in for it; d_a2 is named.
	require.Contains(t, err.Error(), "27825550199")
	require.Contains(t, err.Error(), "Work")
	require.Contains(t, err.Error(), "device_id")
}

func TestDeviceChoiceMessageWithNoLabelsPointsAtNamingOne(t *testing.T) {
	st, dm, a, _ := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)

	_, _, err := r.Resolve(ctxA, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "none is named yet")
	require.Contains(t, err.Error(), "wtf_rename_number")
	require.Contains(t, err.Error(), "an unnamed number", "neither d_a1 nor d_a2 has a label or a known phone number in this fixture")
}
