package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Behaviour: pruning removes only rows that can never be used again (expired
// sessions; used or expired sign-in links). Live rows and the audit log stay,
// however old they are.
func TestPruneKeepsLiveRows(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return t0 }

	u, _ := st.CreateUser(ctx, "+27825550100")
	_, err := st.CreateSession(ctx, u.ID, "short", time.Hour) // expires t0+1h
	require.NoError(t, err)
	_, err = st.CreateSession(ctx, u.ID, "long", 30*24*time.Hour) // still live
	require.NoError(t, err)
	_, err = st.CreateLoginToken(ctx, u.Phone, time.Hour) // will expire
	require.NoError(t, err)
	used, err := st.CreateLoginToken(ctx, u.Phone, 365*24*time.Hour) // used, so dead
	require.NoError(t, err)
	_, err = st.ConsumeLoginToken(ctx, used)
	require.NoError(t, err)
	require.NoError(t, st.Audit(ctx, AuditEvent{UserID: u.ID, Action: "old_event"}))

	// Two hours on: the short session and the one-hour token are past expiry.
	st.Now = func() time.Time { return t0.Add(2 * time.Hour) }
	fresh, err := st.CreateLoginToken(ctx, u.Phone, time.Hour)
	require.NoError(t, err)

	counts, err := st.PruneExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, PruneCounts{Sessions: 1, LoginTokens: 2}, counts)

	sessions, _ := st.SessionsOf(ctx, u.ID)
	require.Len(t, sessions, 1, "the live session survives")
	ok, err := st.LoginTokenValid(ctx, fresh)
	require.NoError(t, err)
	require.True(t, ok, "an unused, unexpired sign-in link survives")
	require.Equal(t, 1, countRows(t, st, "audit_log", "action", "old_event"))

	// Much later still: the audit log is configuration and is never pruned.
	st.Now = func() time.Time { return t0.Add(5 * 365 * 24 * time.Hour) }
	_, err = st.PruneExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, st, "audit_log", "action", "old_event"), "the audit log is never pruned")
	_, err = st.UserByID(ctx, u.ID)
	require.NoError(t, err, "users are never pruned")
}

// Behaviour: a session exactly at its expiry instant is already dead
// (SessionUser refuses it), so pruning takes it; one a second later stays.
func TestPruneSessionBoundary(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return t0 }
	u, _ := st.CreateUser(ctx, "+27825550100")
	_, _ = st.CreateSession(ctx, u.ID, "a", time.Hour)
	_, _ = st.CreateSession(ctx, u.ID, "b", time.Hour+time.Second)
	st.Now = func() time.Time { return t0.Add(time.Hour) }
	counts, err := st.PruneExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.Sessions)
	require.Equal(t, 1, countRows(t, st, "sessions", "user_id", u.ID))
}
