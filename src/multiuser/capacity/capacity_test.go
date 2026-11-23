package capacity

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSnapshotCountsEverything(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	_, _ = st.CreateUser(ctx, "+27825550101")
	require.NoError(t, st.SetOwner(ctx, "d_1", a.ID)) // reserved, maybe never scanned: still counts
	require.NoError(t, st.SetOwner(ctx, "d_2", a.ID))
	_, _, _ = st.JoinWaitlist(ctx, "+27825550102")
	_, _, _ = st.JoinWaitlist(ctx, "+27825550103")
	_, _, _ = st.JoinWaitlist(ctx, "+27825550104")
	require.NoError(t, st.DeclineWaitlist(ctx, "+27825550104", "u_admin"))

	c, err := Service{Store: st, MaxPhones: 5}.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, Capacity{Linked: 2, Max: 5, Users: 2, Waiting: 2}, c, "waitlisted people are not users and hold no capacity")
}

func TestFull(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetOwner(ctx, "d_1", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_2", a.ID))

	require.False(t, Service{Store: st, MaxPhones: 0}.Full(ctx), "0 means no cap")
	require.False(t, Service{Store: st, MaxPhones: 3}.Full(ctx))
	require.True(t, Service{Store: st, MaxPhones: 2}.Full(ctx))
	require.True(t, Service{Store: st, MaxPhones: 1}.Full(ctx), "over the cap (it was lowered) is full")
}

func TestFullFailsClosed(t *testing.T) {
	st := openStore(t)
	require.NoError(t, st.Close())
	require.True(t, Service{Store: st, MaxPhones: 2}.Full(context.Background()),
		"when the count cannot be read, assume full: nobody gets an account that can link nothing")
	require.False(t, Service{Store: st, MaxPhones: 0}.Full(context.Background()), "no cap needs no count")
}
