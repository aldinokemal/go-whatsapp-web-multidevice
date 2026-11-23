package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open("file:" + filepath.Join(t.TempDir(), "accounts.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOpenIsIdempotent(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "accounts.db")
	st, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	st, err = Open(path) // migrations must be safe to re-run
	require.NoError(t, err)
	require.NoError(t, st.Close())
}

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{
		"+27 82 555 0100":   "+27825550100",
		"27825550100":       "+27825550100",
		"(+27) 82-555-0100": "+27825550100",
	} {
		got, err := NormalizePhone(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "abc", "+12", "+1234567890123456"} {
		_, err := NormalizePhone(bad)
		require.Error(t, err, bad)
	}
}

func TestTokensAreRandomAndHashed(t *testing.T) {
	a, b := NewToken(), NewToken()
	require.NotEqual(t, a, b)
	require.Len(t, a, 43)
	require.Len(t, HashToken(a), 64)
	require.NotContains(t, HashToken(a), a)
}

func TestSettingsRoundTrip(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	_, ok, err := st.Setting(ctx, SettingServiceDeviceID)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, st.SetSetting(ctx, SettingServiceDeviceID, "d_1", "u1"))
	require.NoError(t, st.SetSetting(ctx, SettingServiceDeviceID, "d_2", "u1"))
	v, ok, err := st.Setting(ctx, SettingServiceDeviceID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "d_2", v)
}

func TestAuditWrites(t *testing.T) {
	st := openTest(t)
	st.Now = func() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }
	require.NoError(t, st.Audit(context.Background(), AuditEvent{UserID: "u1", Action: "signin", IP: "1.2.3.4"}))
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='signin'`).Scan(&n))
	require.Equal(t, 1, n)
}
