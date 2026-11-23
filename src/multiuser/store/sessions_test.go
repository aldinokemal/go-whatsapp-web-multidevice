package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionLifecycle(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	u, _ := st.CreateUser(ctx, "+27825550100")
	ttl := 30 * 24 * time.Hour

	raw, err := st.CreateSession(ctx, u.ID, "Safari on iOS", ttl)
	require.NoError(t, err)

	got, err := st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)

	// 20 days on: less than half left, so the expiry slides forward.
	now = now.Add(20 * 24 * time.Hour)
	_, err = st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)
	now = now.Add(20 * 24 * time.Hour) // 40 days after creation, still alive thanks to the slide
	_, err = st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)

	now = now.Add(31 * 24 * time.Hour)
	_, err = st.SessionUser(ctx, raw, ttl)
	require.ErrorIs(t, err, ErrNotFound, "expired")

	raw2, _ := st.CreateSession(ctx, u.ID, "", ttl)
	raw3, _ := st.CreateSession(ctx, u.ID, "", ttl)
	require.NoError(t, st.DeleteSession(ctx, raw2))
	_, err = st.SessionUser(ctx, raw2, ttl)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, st.DeleteUserSessions(ctx, u.ID))
	_, err = st.SessionUser(ctx, raw3, ttl)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSessionNotSlidWhenMoreThanHalfLeft(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	u, _ := st.CreateUser(ctx, "+27825550100")
	ttl := 30 * 24 * time.Hour
	raw, _ := st.CreateSession(ctx, u.ID, "", ttl)
	var before string
	require.NoError(t, st.db.QueryRow(`SELECT expires_at FROM sessions`).Scan(&before))
	now = now.Add(5 * 24 * time.Hour)
	_, err := st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)
	var after string
	require.NoError(t, st.db.QueryRow(`SELECT expires_at FROM sessions`).Scan(&after))
	require.Equal(t, before, after)
}

func TestSessionReturnsDisabledUser(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	raw, _ := st.CreateSession(ctx, u.ID, "", time.Hour)
	require.NoError(t, st.SetUserStatus(ctx, u.ID, StatusDisabled))
	got, err := st.SessionUser(ctx, raw, time.Hour)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, got.Status)
}

func TestSessionUserSurfacesDBError(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	raw, _ := st.CreateSession(ctx, u.ID, "", time.Hour)
	require.NoError(t, st.db.Close())
	_, err := st.SessionUser(ctx, raw, time.Hour)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)
}
