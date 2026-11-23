package owners

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

type removeRecorder struct {
	mu      sync.Mutex
	removed []string
}

func (r *removeRecorder) RemoveDevice(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, id)
	if id == "d_gone" {
		return pkgError.ErrDeviceNotFound
	}
	return nil
}

func staleStore(t *testing.T) (*store.Store, time.Time) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	base := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return base }
	return st, base
}

// Behaviour: an hour-old slot nobody scanned is freed (owner row gone, device
// purged, audited against its owner); a slot signed out from the phone, a
// connected one and a young one are kept.
func TestSweeperFreesOnlyNeverScannedSlots(t *testing.T) {
	st, base := staleStore(t)
	ctx := context.Background()
	alice, err := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	bob, err := st.CreateUser(ctx, "+27825550102")
	require.NoError(t, err)
	require.NoError(t, st.SetOwner(ctx, "d_alice_stale", alice.ID))
	require.NoError(t, st.SetOwner(ctx, "d_bob_stale", bob.ID))
	require.NoError(t, st.SetOwner(ctx, "d_out", alice.ID))
	require.NoError(t, st.MarkSignedOut(ctx, "d_out", "27825550101", ""))
	require.NoError(t, st.SetOwner(ctx, "d_live", alice.ID)) // linked, identity not recorded yet
	st.Now = func() time.Time { return base.Add(59 * time.Minute) }
	require.NoError(t, st.SetOwner(ctx, "d_young", bob.ID))

	rec := &removeRecorder{}
	sw := &StaleSlotSweeper{Store: st, Devices: rec,
		Live: func() []PairedDevice {
			return []PairedDevice{{ID: "d_live", JID: "27825550103@s.whatsapp.net"}, {ID: "d_alice_stale"}}
		},
		Now: func() time.Time { return base.Add(61 * time.Minute) }}
	n, err := sw.SweepOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.ElementsMatch(t, []string{"d_alice_stale", "d_bob_stale"}, rec.removed)

	aliceIDs, _ := st.DevicesOf(ctx, alice.ID)
	require.ElementsMatch(t, []string{"d_out", "d_live"}, aliceIDs)
	bobIDs, _ := st.DevicesOf(ctx, bob.ID)
	require.Equal(t, []string{"d_young"}, bobIDs)

	rows, _, err := st.ListAudit(ctx, store.AuditQuery{Limit: 50})
	require.NoError(t, err)
	owners := map[string]string{}
	for _, r := range rows {
		if r.Action == "device_slot_expired" {
			owners[r.DeviceID] = r.UserID
		}
	}
	require.Equal(t, map[string]string{"d_alice_stale": alice.ID, "d_bob_stale": bob.ID}, owners)

	// A second sweep has nothing left to do.
	n, err = sw.SweepOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

// Behaviour: a device the manager no longer knows still frees its place.
func TestSweeperToleratesAGhostSlot(t *testing.T) {
	st, base := staleStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	require.NoError(t, st.SetOwner(ctx, "d_gone", u.ID))
	sw := &StaleSlotSweeper{Store: st, Devices: &removeRecorder{}, Now: func() time.Time { return base.Add(2 * time.Hour) }}
	n, err := sw.SweepOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = st.OwnedDeviceCount(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

// Behaviour: freeing a stale slot makes room under the server cap.
func TestSweptSlotFreesCapacity(t *testing.T) {
	st, base := staleStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	r, err := st.ReserveOwner(ctx, "d_abandoned", u.ID, 5, 1)
	require.NoError(t, err)
	require.Equal(t, store.Reserved, r)
	r, err = st.ReserveOwner(ctx, "d_next", u.ID, 5, 1)
	require.NoError(t, err)
	require.Equal(t, store.ServerFull, r)
	sw := &StaleSlotSweeper{Store: st, Devices: &removeRecorder{}, Now: func() time.Time { return base.Add(2 * time.Hour) }}
	_, err = sw.SweepOnce(ctx)
	require.NoError(t, err)
	r, err = st.ReserveOwner(ctx, "d_next", u.ID, 5, 1)
	require.NoError(t, err)
	require.Equal(t, store.Reserved, r)
}
