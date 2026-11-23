package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// countRows counts rows in table where col = val. Table and column names are
// fixed strings from the tests below, never input.
func countRows(t *testing.T, st *Store, table, col, val string) int {
	t.Helper()
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+col+` = ?`, val).Scan(&n))
	return n
}

// seedUser gives a user one of everything that hangs off the users row.
func seedUser(t *testing.T, st *Store, phone, device string) *User {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, phone)
	require.NoError(t, err)
	_, err = st.CreateSession(ctx, u.ID, "ua", time.Hour)
	require.NoError(t, err)
	require.NoError(t, st.AddPasskey(ctx, Passkey{CredentialID: "cred_" + device, UserID: u.ID, CredentialJSON: []byte(`{}`)}))
	require.NoError(t, st.SetOwner(ctx, device, u.ID))
	_, _, err = st.CreateAPIKey(ctx, u.ID, "k")
	require.NoError(t, err)
	require.NoError(t, st.Audit(ctx, AuditEvent{UserID: u.ID, Action: "signin"}))
	return u
}

// Behaviour: deleting the user row takes every row tied to it, and only those.
// The audit log is configuration we keep, so it survives the user.
func TestDeleteUserCascades(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := seedUser(t, st, "+27825550100", "d_alice")
	bob := seedUser(t, st, "+27825550101", "d_bob")

	require.NoError(t, st.DeleteUser(ctx, alice.ID))

	_, err := st.UserByID(ctx, alice.ID)
	require.ErrorIs(t, err, ErrNotFound)
	for _, tc := range []struct{ table, col string }{
		{"sessions", "user_id"}, {"passkeys", "user_id"}, {"device_owners", "user_id"}, {"api_keys", "user_id"},
	} {
		require.Zero(t, countRows(t, st, tc.table, tc.col, alice.ID), "alice's %s", tc.table)
		require.Equal(t, 1, countRows(t, st, tc.table, tc.col, bob.ID), "bob's %s untouched", tc.table)
	}
	require.Equal(t, 1, countRows(t, st, "audit_log", "user_id", alice.ID), "the audit log is kept")
	_, err = st.UserByID(ctx, bob.ID)
	require.NoError(t, err)

	require.ErrorIs(t, st.DeleteUser(ctx, alice.ID), ErrNotFound, "a second delete finds nothing")
}

// Behaviour: the deleting status is one way. Once an account is being deleted
// nothing (an admin re-enabling it, say) can bring it back to active.
func TestDeletingIsOneWay(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetUserStatus(ctx, u.ID, StatusDeleting))
	require.NoError(t, st.SetUserStatus(ctx, u.ID, StatusDeleting), "setting it again is idempotent")
	require.ErrorIs(t, st.SetUserStatus(ctx, u.ID, StatusActive), ErrDeleting)
	require.ErrorIs(t, st.SetUserStatus(ctx, u.ID, StatusDisabled), ErrDeleting)
	got, _ := st.UserByID(ctx, u.ID)
	require.Equal(t, StatusDeleting, got.Status)
	require.ErrorIs(t, st.SetUserStatus(ctx, "u_nobody", StatusDeleting), ErrNotFound)
}

func TestUsersWithStatus(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")
	_, _ = st.CreateUser(ctx, "+27825550102")
	require.NoError(t, st.SetUserStatus(ctx, a.ID, StatusDeleting))
	require.NoError(t, st.SetUserStatus(ctx, b.ID, StatusDisabled))

	got, err := st.UsersWithStatus(ctx, StatusDeleting)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, a.ID, got[0].ID)

	none, err := st.UsersWithStatus(ctx, "nonsense")
	require.NoError(t, err)
	require.Empty(t, none)
}

// Behaviour and security: a label is set on your own device only.
func TestSetLabelOnAnotherUsersDeviceIsNotFound(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice, _ := st.CreateUser(ctx, "+27825550100")
	bob, _ := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, st.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, st.SetOwner(ctx, "d_alice2", alice.ID))
	require.NoError(t, st.SetOwner(ctx, "d_bob", bob.ID))

	require.NoError(t, st.SetLabel(ctx, alice.ID, "d_alice", "Work phone"))
	require.ErrorIs(t, st.SetLabel(ctx, alice.ID, "d_bob", "mine now"), ErrNotFound)
	require.ErrorIs(t, st.SetLabel(ctx, alice.ID, "d_nobody", "x"), ErrNotFound)

	labels, err := st.LabelsOf(ctx, alice.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"d_alice": "Work phone", "d_alice2": ""}, labels)
	bobs, err := st.LabelsOf(ctx, bob.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"d_bob": ""}, bobs, "bob's label was not changed")

	require.NoError(t, st.SetLabel(ctx, alice.ID, "d_alice", ""), "empty clears the name")
	labels, _ = st.LabelsOf(ctx, alice.ID)
	require.Equal(t, "", labels["d_alice"])
}
