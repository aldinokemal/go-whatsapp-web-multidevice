package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOwnership(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")

	require.NoError(t, st.SetOwner(ctx, "d_a1", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_b1", b.ID))
	require.Error(t, st.SetOwner(ctx, "d_a1", b.ID), "a device has one owner; no silent takeover")

	owner, err := st.OwnerOf(ctx, "d_a1")
	require.NoError(t, err)
	require.Equal(t, a.ID, owner)
	_, err = st.OwnerOf(ctx, "d_nobody")
	require.ErrorIs(t, err, ErrNotFound)

	ids, err := st.DevicesOf(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"d_a1"}, ids)

	n, _ := st.OwnedDeviceCount(ctx)
	require.Equal(t, 2, n)
	require.NoError(t, st.RemoveOwner(ctx, "d_a1"))
	ids, _ = st.DevicesOf(ctx, a.ID)
	require.Empty(t, ids)
}

func TestReserveOwnerEnforcesLimitAtomically(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")

	ok, err := st.ReserveOwner(ctx, "d_1", a.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = st.ReserveOwner(ctx, "d_2", a.ID, 1)
	require.NoError(t, err)
	require.False(t, ok, "limit reached")
	_, err = st.OwnerOf(ctx, "d_2")
	require.ErrorIs(t, err, ErrNotFound, "nothing recorded past the limit")
	_, err = st.ReserveOwner(ctx, "d_1", a.ID, 5)
	require.Error(t, err, "a device id is never reserved twice")

	b, _ := st.CreateUser(ctx, "+27825550101")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, errs := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := st.ReserveOwner(ctx, fmt.Sprintf("d_b%d", i), b.ID, 2)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs++
			} else if ok {
				wins++
			}
		}(i)
	}
	wg.Wait()
	require.Zero(t, errs, "losers see the limit, not a lock error")
	require.Equal(t, 2, wins)
	ids, _ := st.DevicesOf(ctx, b.ID)
	require.Len(t, ids, 2)
}

func TestForeignKeysCascadeOnUserDelete(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	var fk int
	require.NoError(t, st.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk))
	require.Equal(t, 1, fk, "foreign_keys must be on")

	a, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetOwner(ctx, "d_a1", a.ID))
	_, err := st.CreateSession(ctx, a.ID, "ua", time.Hour)
	require.NoError(t, err)
	require.NoError(t, st.AddPasskey(ctx, Passkey{CredentialID: "cred1", UserID: a.ID, CredentialJSON: []byte("{}")}))
	require.Error(t, st.SetOwner(ctx, "d_x", "u_nobody"), "an owner row needs a real user")

	_, err = st.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, a.ID)
	require.NoError(t, err)
	for _, table := range []string{"device_owners", "sessions", "passkeys"} {
		var n int
		require.NoError(t, st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE user_id = ?`, a.ID).Scan(&n))
		require.Zero(t, n, table)
	}
}
