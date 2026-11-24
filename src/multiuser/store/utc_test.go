package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// zones the store's time comparisons are checked in: east and west of UTC,
// and the extremes.
var testZones = []*time.Location{
	time.FixedZone("+02", 2*60*60),
	time.FixedZone("-05", -5*60*60),
	time.FixedZone("+14", 14*60*60),
	time.FixedZone("-12", -12*60*60),
}

const mattnLayout = "2006-01-02 15:04:05.999999999-07:00"

// storedTimes returns every non-null TIMESTAMP value in the database as text.
func storedTimes(t *testing.T, st *Store) map[string][]string {
	t.Helper()
	ctx := context.Background()
	tx, err := st.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	cols, err := timestampColumns(ctx, tx)
	require.NoError(t, err)
	require.NotEmpty(t, cols)
	out := map[string][]string{}
	for _, c := range cols {
		rows, err := tx.QueryContext(ctx, `SELECT CAST("`+c.col+`" AS TEXT) FROM "`+c.table+`" WHERE "`+c.col+`" IS NOT NULL`)
		require.NoError(t, err)
		for rows.Next() {
			var s string
			require.NoError(t, rows.Scan(&s))
			out[c.table+"."+c.col] = append(out[c.table+"."+c.col], s)
		}
		require.NoError(t, rows.Close())
	}
	return out
}

// requireUTCText fails unless v is stored text in UTC (either driver's form).
func requireUTCText(t *testing.T, col, v string) {
	t.Helper()
	tm, ok := parseStoredTime(v)
	require.True(t, ok, "%s stored %q, not a time", col, v)
	_, off := tm.Zone()
	require.Zero(t, off, "%s stored %q, not UTC", col, v)
	require.True(t, strings.Contains(v, "+00:00") || strings.Contains(v, "+0000"), "%s stored %q, not UTC", col, v)
}

// Guard: whatever zone the store's clock is in, every timestamp it writes is
// stored as UTC text, so SQL text comparisons and ORDER BYs are time order.
func TestEveryStoredTimeIsUTC(t *testing.T) {
	for _, zone := range testZones {
		t.Run(zone.String(), func(t *testing.T) {
			st := openTest(t)
			ctx := context.Background()
			st.Now = func() time.Time { return time.Now().In(zone) }
			u, err := st.CreateUser(ctx, "+27825550101")
			require.NoError(t, err)
			require.NoError(t, st.TouchLogin(ctx, u.ID))
			require.NoError(t, st.SetOwner(ctx, "d_1", u.ID))
			require.NoError(t, st.SetOwner(ctx, "d_2", u.ID))
			require.NoError(t, st.SetOwner(ctx, "d_3", u.ID))
			require.NoError(t, st.MarkSignedOut(ctx, "d_1", "27825550101", ""))
			require.NoError(t, st.RecordRefusal(ctx, "d_2", RefusalDuplicate))
			raw, err := st.CreateSession(ctx, u.ID, "ua", time.Hour)
			require.NoError(t, err)
			_, err = st.SessionUser(ctx, raw, time.Hour)
			require.NoError(t, err)
			tok, err := st.CreateLoginToken(ctx, "+27825550101", time.Minute)
			require.NoError(t, err)
			_, err = st.ConsumeLoginToken(ctx, tok)
			require.NoError(t, err)
			_, key, err := st.CreateAPIKey(ctx, u.ID, "k")
			require.NoError(t, err)
			require.NoError(t, st.TouchAPIKey(ctx, key.ID))
			_, k2, err := st.CreateAPIKey(ctx, u.ID, "k2")
			require.NoError(t, err)
			require.NoError(t, st.RevokeAPIKey(ctx, u.ID, k2.ID))
			require.NoError(t, st.AddPasskey(ctx, Passkey{CredentialID: "c1", UserID: u.ID, Name: "p", CredentialJSON: []byte("{}")}))
			require.NoError(t, st.UpdatePasskeyCredential(ctx, "c1", []byte("{}")))
			require.NoError(t, st.SetSetting(ctx, SettingServiceDeviceID, "d_1", u.ID))
			require.NoError(t, st.Audit(ctx, AuditEvent{UserID: u.ID, Action: "signin"}))
			_, _, err = st.JoinWaitlist(ctx, "+27825550102")
			require.NoError(t, err)
			require.NoError(t, st.DeclineWaitlist(ctx, "+27825550102", u.ID))

			got := storedTimes(t, st)
			for _, col := range []string{"users.created_at", "users.last_login_at", "device_owners.created_at",
				"device_owners.signed_out_at", "device_owners.last_refusal_at", "sessions.created_at",
				"sessions.expires_at", "sessions.last_seen_at", "login_tokens.expires_at", "login_tokens.used_at",
				"api_keys.created_at", "api_keys.last_used_at", "api_keys.revoked_at", "passkeys.created_at",
				"passkeys.last_used_at", "settings.updated_at", "audit_log.at", "waitlist.created_at",
				"waitlist.last_asked_at", "waitlist.decided_at"} {
				require.NotEmpty(t, got[col], "the test writes %s", col)
			}
			for col, vals := range got {
				for _, v := range vals {
					requireUTCText(t, col, v)
				}
			}
		})
	}
}

// Regression: the stale-slot sweeper passed a local-time cutoff, which was
// compared as text against UTC created_at: east of UTC every fresh slot looked
// stale, west of UTC no slot ever did. The cutoff may now be in any zone.
func TestStaleSlotCutoffInAnyZone(t *testing.T) {
	for _, zone := range testZones {
		t.Run(zone.String(), func(t *testing.T) {
			st := openTest(t)
			ctx := context.Background()
			now := time.Now().In(zone)
			st.Now = func() time.Time { return now.Add(-2 * time.Hour) }
			u, err := st.CreateUser(ctx, "+27825550101")
			require.NoError(t, err)
			require.NoError(t, st.SetOwner(ctx, "d_old", u.ID))
			st.Now = func() time.Time { return now }
			require.NoError(t, st.SetOwner(ctx, "d_new", u.ID))

			cutoff := now.Add(-time.Hour) // in zone, as a caller's clock gives it
			got, err := st.NeverLinkedSlotsBefore(ctx, cutoff)
			require.NoError(t, err)
			require.Equal(t, []OwnedSlot{{DeviceID: "d_old", UserID: u.ID}}, got)
			ok, err := st.ExpireNeverLinkedSlot(ctx, "d_new", cutoff)
			require.NoError(t, err)
			require.False(t, ok, "a fresh slot is kept")
			ok, err = st.ExpireNeverLinkedSlot(ctx, "d_old", cutoff)
			require.NoError(t, err)
			require.True(t, ok)
		})
	}
}

// Migration: rows written before the clock was pinned to UTC (phase-1 tables
// on the live deploy could hold them) are rewritten to UTC on open, so the
// text comparisons give the right answer for old and new rows alike.
func TestOldNonUTCRowsAreRewrittenToUTC(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "accounts.db")
	st, err := Open(path)
	require.NoError(t, err)
	ctx := context.Background()
	now := time.Now().UTC()
	u, err := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	sess, err := st.CreateSession(ctx, u.ID, "ua", 30*24*time.Hour)
	require.NoError(t, err)
	tok, err := st.CreateLoginToken(ctx, "+27825550101", time.Hour)
	require.NoError(t, err)
	require.NoError(t, st.SetOwner(ctx, "d_old", u.ID))

	// Old rows, as a local clock would have written them:
	// - a session valid for another hour, written at UTC-12 (its text reads
	//   11 hours ago, so "expires_at > now" said expired);
	// - a sign-in link that expired an hour ago, written at UTC+14 (its text
	//   reads 13 hours ahead, so it still looked valid);
	// - a slot reserved 2 hours ago, written at UTC-12 (looked 14 hours old).
	west, east := time.FixedZone("-12", -12*60*60), time.FixedZone("+14", 14*60*60)
	_, err = st.db.Exec(`UPDATE sessions SET expires_at = ?, created_at = ?`,
		now.Add(time.Hour).In(west).Format(mattnLayout), now.In(west).Format(mattnLayout))
	require.NoError(t, err)
	_, err = st.db.Exec(`UPDATE login_tokens SET expires_at = ?`, now.Add(-time.Hour).In(east).Format(mattnLayout))
	require.NoError(t, err)
	_, err = st.db.Exec(`UPDATE device_owners SET created_at = ?`, now.Add(-2*time.Hour).In(west).Format(mattnLayout))
	require.NoError(t, err)
	// A value in no known form is left alone.
	_, err = st.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('x', 'y', 'not a time')`)
	require.NoError(t, err)

	// The bug these rows cause before the rewrite.
	live, err := st.SessionsOf(ctx, u.ID)
	require.NoError(t, err)
	require.Empty(t, live, "before: a live session read as expired")
	ok, err := st.LoginTokenValid(ctx, tok)
	require.NoError(t, err)
	require.True(t, ok, "before: an expired link read as valid")

	// Re-run only the rewrite, as an existing database meets it on upgrade.
	_, err = st.db.Exec(`UPDATE wtf_schema SET version = ?`, len(migrations)-1)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	st, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	for col, vals := range storedTimes(t, st) {
		for _, v := range vals {
			if col == "settings.updated_at" {
				require.Equal(t, "not a time", v)
				continue
			}
			requireUTCText(t, col, v)
		}
	}
	live, err = st.SessionsOf(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, live, 1, "after: the live session is live")
	got, err := st.SessionUser(ctx, sess, 30*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)
	ok, err = st.LoginTokenValid(ctx, tok)
	require.NoError(t, err)
	require.False(t, ok, "after: the expired link is expired")
	stale, err := st.NeverLinkedSlotsBefore(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, []OwnedSlot{{DeviceID: "d_old", UserID: u.ID}}, stale)
	stale, err = st.NeverLinkedSlotsBefore(ctx, now.Add(-3*time.Hour))
	require.NoError(t, err)
	require.Empty(t, stale, "2 hours old, not 14")
}

func TestParseStoredTime(t *testing.T) {
	want := time.Date(2026, 11, 24, 8, 0, 0, 500, time.UTC)
	for _, s := range []string{
		"2026-11-24 10:00:00.0000005+02:00",
		"2026-11-24T03:00:00.0000005-05:00",
		"2026-11-24 10:00:00.0000005 +0200 SAST",
		"2026-11-24 10:00:00.0000005 +0200 +02 m=+0.000000001",
		"2026-11-24T08:00:00.0000005Z",
		"2026-11-24 08:00:00.0000005",
	} {
		got, ok := parseStoredTime(s)
		require.True(t, ok, s)
		require.True(t, want.Equal(got), "%s: %s", s, got)
	}
	_, ok := parseStoredTime("yesterday")
	require.False(t, ok)
}
