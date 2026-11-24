package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func clockAt(st *Store, t0 time.Time) func(time.Duration) {
	now := t0
	var mu sync.Mutex
	st.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	return func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
}

var t0 = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)

func waitlistRows(t *testing.T, st *Store, phone string) int {
	t.Helper()
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM waitlist WHERE phone = ?`, phone).Scan(&n))
	return n
}

// Behaviour.
func TestJoinIsIdempotent(t *testing.T) {
	st := openTest(t)
	tick := clockAt(st, t0)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		joined, pos, err := st.JoinWaitlist(ctx, "+27825550100")
		require.NoError(t, err)
		require.True(t, joined)
		require.Equal(t, 1, pos)
		tick(time.Hour)
	}
	require.Equal(t, 1, waitlistRows(t, st, "+27825550100"))
	total, items, err := st.WaitlistPage(ctx, WaitlistWaiting, 0, 50)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.True(t, items[0].CreatedAt.Equal(t0), "created_at is the first ask")
	require.True(t, items[0].LastAskedAt.Equal(t0.Add(2*time.Hour)), "last_asked_at is bumped")
}

func TestJoinRefusesAnythingButANormalisedPhone(t *testing.T) {
	st := openTest(t)
	for _, bad := range []string{"", "123456789012345@lid", "27825550100", "+27 82 555 0100", "abc"} {
		_, _, err := st.JoinWaitlist(context.Background(), bad)
		require.ErrorIs(t, err, ErrInvalid, bad)
	}
}

func TestDeclinedStaysDeclined(t *testing.T) {
	st := openTest(t)
	clockAt(st, t0)
	ctx := context.Background()
	_, _, _ = st.JoinWaitlist(ctx, "+27825550100")
	require.NoError(t, st.DeclineWaitlist(ctx, "+27825550100", "u_admin"))
	joined, pos, err := st.JoinWaitlist(ctx, "+27825550100")
	require.NoError(t, err)
	require.False(t, joined)
	require.Zero(t, pos)
	total, _, _ := st.WaitlistPage(ctx, WaitlistDeclined, 0, 50)
	require.Equal(t, 1, total)
	total, _, _ = st.WaitlistPage(ctx, WaitlistWaiting, 0, 50)
	require.Zero(t, total)

	require.NoError(t, st.ReopenWaitlist(ctx, "+27825550100"))
	joined, pos, _ = st.JoinWaitlist(ctx, "+27825550100")
	require.True(t, joined)
	require.Equal(t, 1, pos)
}

func TestPositionCountsOnlyWaiting(t *testing.T) {
	st := openTest(t)
	tick := clockAt(st, t0)
	ctx := context.Background()
	for _, p := range []string{"+27825550101", "+27825550102", "+27825550103"} {
		_, _, err := st.JoinWaitlist(ctx, p)
		require.NoError(t, err)
		tick(time.Minute)
	}
	require.NoError(t, st.DeclineWaitlist(ctx, "+27825550101", "u_admin"))
	_, _, err := st.AdmitFromWaitlist(ctx, "+27825550102", "u_admin", 2)
	require.NoError(t, err)
	_, pos, err := st.JoinWaitlist(ctx, "+27825550103")
	require.NoError(t, err)
	require.Equal(t, 1, pos, "declined and admitted rows ahead do not count")
	_, pos, _ = st.JoinWaitlist(ctx, "+27825550104")
	require.Equal(t, 2, pos)
}

func TestWaitlistPageIsOldestFirstWithTotal(t *testing.T) {
	st := openTest(t)
	tick := clockAt(st, t0)
	ctx := context.Background()
	for _, p := range []string{"+27825550101", "+27825550102", "+27825550103"} {
		_, _, _ = st.JoinWaitlist(ctx, p)
		tick(time.Minute)
	}
	total, items, err := st.WaitlistPage(ctx, WaitlistWaiting, 1, 1)
	require.NoError(t, err)
	require.Equal(t, 3, total, "total counts every match, not the page")
	require.Len(t, items, 1)
	require.Equal(t, "+27825550102", items[0].Phone)
	n, err := st.WaitlistCount(ctx, WaitlistWaiting)
	require.NoError(t, err)
	require.Equal(t, total, n)

	_, _, err = st.WaitlistPage(ctx, "everything", 0, 50)
	require.ErrorIs(t, err, ErrInvalid, "status is an allowlist")
}

func TestAdmitCreatesOnce(t *testing.T) {
	st := openTest(t)
	clockAt(st, t0)
	ctx := context.Background()
	_, _, _ = st.JoinWaitlist(ctx, "+27825550100")
	u, created, err := st.AdmitFromWaitlist(ctx, "+27825550100", "u_admin", 3)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, 3, u.MaxDevices)
	again, created, err := st.AdmitFromWaitlist(ctx, "+27825550100", "u_admin", 3)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, u.ID, again.ID)
	total, items, _ := st.WaitlistPage(ctx, WaitlistAdmitted, 0, 50)
	require.Equal(t, 1, total)
	require.Equal(t, "u_admin", items[0].DecidedBy)
}

func TestAdmitWhenUserAlreadyExists(t *testing.T) {
	st := openTest(t)
	clockAt(st, t0)
	ctx := context.Background()
	_, _, _ = st.JoinWaitlist(ctx, "+27825550100")
	existing, err := st.CreateUser(ctx, "+27825550100") // signed up meanwhile
	require.NoError(t, err)
	u, created, err := st.AdmitFromWaitlist(ctx, "+27825550100", "u_admin", 2)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, existing.ID, u.ID)
	n, _ := st.WaitlistCount(ctx, WaitlistWaiting)
	require.Zero(t, n, "the row is marked admitted")
}

func TestAdmitNeedsAWaitlistRow(t *testing.T) {
	st := openTest(t)
	clockAt(st, t0)
	ctx := context.Background()
	_, _, err := st.AdmitFromWaitlist(ctx, "+27825550100", "u_admin", 2)
	require.ErrorIs(t, err, ErrNotFound, "only someone who asked can be let in")
	_, _, _ = st.JoinWaitlist(ctx, "+27825550101")
	require.NoError(t, st.DeclineWaitlist(ctx, "+27825550101", "u_admin"))
	_, _, err = st.AdmitFromWaitlist(ctx, "+27825550101", "u_admin", 2)
	require.ErrorIs(t, err, ErrInvalid, "a declined number is moved back first")
	_, err = st.UserByPhone(ctx, "+27825550101")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRemoveFromWaitlist(t *testing.T) {
	st := openTest(t)
	clockAt(st, t0)
	ctx := context.Background()
	_, _, _ = st.JoinWaitlist(ctx, "+27825550100")
	require.NoError(t, st.RemoveFromWaitlist(ctx, "+27825550100"))
	require.Zero(t, waitlistRows(t, st, "+27825550100"))
	require.NoError(t, st.RemoveFromWaitlist(ctx, "+27825550100"), "removing nothing is fine")
}

// Mechanism: two admins clicking "Let in" for one person at once.
func TestConcurrentAdmitsCreateOneUser(t *testing.T) {
	st := openTest(t)
	clockAt(st, t0)
	ctx := context.Background()
	_, _, _ = st.JoinWaitlist(ctx, "+27825550100")
	var wg sync.WaitGroup
	var mu sync.Mutex
	created, errs := 0, 0
	ids := map[string]bool{}
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			u, c, err := st.AdmitFromWaitlist(ctx, "+27825550100", "u_admin", 2)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs++
				return
			}
			ids[u.ID] = true
			if c {
				created++
			}
		}()
	}
	close(start)
	wg.Wait()
	require.Zero(t, errs, "losers find the user, they do not fail")
	require.Equal(t, 1, created)
	require.Len(t, ids, 1)
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM users WHERE phone = ?`, "+27825550100").Scan(&n))
	require.Equal(t, 1, n)
}
