package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Behaviour: every sign-in link minted for the number goes, used or not, and
// only for that number.
func TestDeleteLoginTokensForTakesOnlyThatNumber(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	mine, err := st.CreateLoginToken(ctx, "+27825550100", time.Hour)
	require.NoError(t, err)
	theirs, err := st.CreateLoginToken(ctx, "+27825550101", time.Hour)
	require.NoError(t, err)

	require.NoError(t, st.DeleteLoginTokensFor(ctx, "+27825550100"))

	ok, err := st.LoginTokenValid(ctx, mine)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = st.LoginTokenValid(ctx, theirs)
	require.NoError(t, err)
	require.True(t, ok, "another number's link survives")
	require.NoError(t, st.DeleteLoginTokensFor(ctx, ""), "an empty number deletes nothing and is not an error")
	ok, _ = st.LoginTokenValid(ctx, theirs)
	require.True(t, ok)
}

// Behaviour: erasing a user deletes the row (and so everything that cascades
// from it), writes exactly one account_deleted line naming the user id only,
// and strips the personal detail (numbers, IPs) from the audit lines about
// that user. Another user's lines are untouched.
func TestEraseUserRecordsTheDeletionAndScrubsTheirAudit(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := seedUser(t, st, "+27825550100", "d_alice")
	bob := seedUser(t, st, "+27825550101", "d_bob")
	require.NoError(t, st.Audit(ctx, AuditEvent{UserID: alice.ID, Action: "device_remove", DeviceID: "d_alice", IP: "203.0.113.9", Detail: "27825550100"}))
	require.NoError(t, st.Audit(ctx, AuditEvent{UserID: bob.ID, ActorID: alice.ID, Action: "admin_set_limit", IP: "203.0.113.9", Detail: "2 -> 3"}))
	require.NoError(t, st.Audit(ctx, AuditEvent{UserID: bob.ID, Action: "device_remove", IP: "198.51.100.7", Detail: "27825550101"}))

	require.NoError(t, st.EraseUser(ctx, alice.ID))

	_, err := st.UserByID(ctx, alice.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.Zero(t, countRows(t, st, "device_owners", "user_id", alice.ID))

	rows, _, err := st.ListAudit(ctx, AuditQuery{Limit: 200})
	require.NoError(t, err)
	deleted := 0
	for _, r := range rows {
		if r.UserID == alice.ID {
			require.Empty(t, r.Detail, "no personal detail left about alice (%s)", r.Action)
			require.Empty(t, r.IP, "no IP left about alice (%s)", r.Action)
			if r.Action == "account_deleted" {
				deleted++
				require.Empty(t, r.ActorID)
				require.Empty(t, r.DeviceID)
			}
		}
		if r.ActorID == alice.ID {
			require.Empty(t, r.IP, "alice's IP is gone from what she did as an admin")
			require.Equal(t, "2 -> 3", r.Detail, "the detail is about bob, so it stays")
		}
		if r.UserID == bob.ID && r.ActorID == "" && r.Action == "device_remove" {
			require.Equal(t, "27825550101", r.Detail, "bob's own audit is untouched")
			require.Equal(t, "198.51.100.7", r.IP)
		}
	}
	require.Equal(t, 1, deleted, "exactly one account_deleted line")
	_, err = st.UserByID(ctx, bob.ID)
	require.NoError(t, err)

	require.ErrorIs(t, st.EraseUser(ctx, alice.ID), ErrNotFound, "a second erase finds nothing")
	rows, _, err = st.ListAudit(ctx, AuditQuery{UserID: alice.ID, Limit: 200})
	require.NoError(t, err)
	n := 0
	for _, r := range rows {
		if r.Action == "account_deleted" {
			n++
		}
	}
	require.Equal(t, 1, n, "a second erase writes no second line")
}
