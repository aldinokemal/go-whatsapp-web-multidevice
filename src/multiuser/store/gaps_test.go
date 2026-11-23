package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Gap A: a slot remembers the last number it held after the phone signs it out.
func TestSlotRemembersItsNumberAfterSignOutFromThePhone(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetOwner(ctx, "d1", u.ID))

	require.NoError(t, st.RecordIdentity(ctx, "d1", "27825550101", "Alice"))
	require.NoError(t, st.MarkSignedOut(ctx, "d1", "", ""))

	slots, err := st.SlotsOf(ctx, u.ID)
	require.NoError(t, err)
	s := slots["d1"]
	require.Equal(t, "27825550101", s.LastPhone, "an empty phone on sign-out keeps the last known one")
	require.Equal(t, "Alice", s.LastName)
	require.NotNil(t, s.SignedOutAt)

	// Linking again clears the signed-out mark and keeps the identity fresh.
	require.NoError(t, st.RecordIdentity(ctx, "d1", "27825550101", "Alice B"))
	s = mustSlots(t, st, u.ID)["d1"]
	require.Nil(t, s.SignedOutAt)
	require.Equal(t, "Alice B", s.LastName)
}

func TestSignOutRecordsTheIdentityItSaw(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetOwner(ctx, "d1", u.ID))
	require.NoError(t, st.MarkSignedOut(ctx, "d1", "27825550101", "Bob"))
	s := mustSlots(t, st, u.ID)["d1"]
	require.Equal(t, "27825550101", s.LastPhone)
	require.Equal(t, "Bob", s.LastName)
}

// Gap B: a refusal is kept against the slot until the next good link.
func TestRefusalStaysUntilTheNextGoodLink(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetOwner(ctx, "d1", u.ID))

	require.NoError(t, st.RecordRefusal(ctx, "d1", RefusalDuplicate))
	s := mustSlots(t, st, u.ID)["d1"]
	require.Equal(t, RefusalDuplicate, s.Refusal)
	require.NotNil(t, s.RefusalAt)

	// A reconnect's identity update does not clear it ...
	require.NoError(t, st.RecordIdentity(ctx, "d1", "27825550101", ""))
	require.Equal(t, RefusalDuplicate, mustSlots(t, st, u.ID)["d1"].Refusal)
	// ... only an accepted link does.
	require.NoError(t, st.ClearRefusal(ctx, "d1"))
	s = mustSlots(t, st, u.ID)["d1"]
	require.Empty(t, s.Refusal)
	require.Nil(t, s.RefusalAt)
}

func TestSlotsOfOnlyReturnsTheUsersOwnSlots(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550200")
	require.NoError(t, st.SetOwner(ctx, "da", a.ID))
	require.NoError(t, st.SetOwner(ctx, "db", b.ID))
	slots := mustSlots(t, st, a.ID)
	require.Contains(t, slots, "da")
	require.NotContains(t, slots, "db")
}

func mustSlots(t *testing.T, st *Store, userID string) map[string]Slot {
	t.Helper()
	m, err := st.SlotsOf(context.Background(), userID)
	require.NoError(t, err)
	return m
}

// Gap C: limits and phone changes.
func TestSetMaxDevicesValidatesTheRange(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, st.SetMaxDevices(ctx, u.ID, 0))
	require.NoError(t, st.SetMaxDevices(ctx, u.ID, 20))
	got, _ := st.UserByID(ctx, u.ID)
	require.Equal(t, 20, got.MaxDevices)
	require.ErrorIs(t, st.SetMaxDevices(ctx, u.ID, 21), ErrInvalid)
	require.ErrorIs(t, st.SetMaxDevices(ctx, u.ID, -1), ErrInvalid)
	require.ErrorIs(t, st.SetMaxDevices(ctx, "u_nobody", 3), ErrNotFound)
}

func TestSetUserPhoneRefusesATakenNumber(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	_, _ = st.CreateUser(ctx, "+27825550200")
	link, _ := st.CreateLoginToken(ctx, "+27825550100", time.Hour)
	_, err := st.SetUserPhone(ctx, a.ID, "+27825550200")
	require.ErrorIs(t, err, ErrPhoneTaken)
	_, err = st.SetUserPhone(ctx, a.ID, "27825550300")
	require.ErrorIs(t, err, ErrInvalid, "only a normalised number is stored")
	old, err := st.SetUserPhone(ctx, a.ID, "+27825550300")
	require.NoError(t, err)
	require.Equal(t, "+27825550100", old)
	got, _ := st.UserByID(ctx, a.ID)
	require.Equal(t, "+27825550300", got.Phone)
	ok, _ := st.LoginTokenValid(ctx, link)
	require.False(t, ok, "a sign-in link for the old number dies with it")
	_, err = st.SetUserPhone(ctx, "u_nobody", "+27825550400")
	require.ErrorIs(t, err, ErrNotFound)
}

// Gap D: a user's live sessions, newest first, and ending one of them.
func TestListAndEndOneSession(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550200")
	ttl := time.Hour
	raw1, _ := st.CreateSession(ctx, a.ID, "UA one", ttl)
	now = now.Add(time.Minute)
	raw2, _ := st.CreateSession(ctx, a.ID, "UA two", ttl)
	rawB, _ := st.CreateSession(ctx, b.ID, "UA bob", ttl)

	now = now.Add(5 * time.Minute)
	_, err := st.SessionUser(ctx, raw1, ttl) // a request marks it seen
	require.NoError(t, err)

	list, err := st.SessionsOf(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "UA two", list[0].UserAgent, "newest first")
	require.Equal(t, HashToken(raw2), list[0].IDHash)
	require.NotNil(t, list[1].LastSeenAt)
	require.True(t, list[1].LastSeenAt.Equal(now))

	// Ending another user's session by hash is refused and leaves it alive.
	require.ErrorIs(t, st.DeleteUserSession(ctx, a.ID, HashToken(rawB)), ErrNotFound)
	_, err = st.SessionUser(ctx, rawB, ttl)
	require.NoError(t, err)

	require.NoError(t, st.DeleteUserSession(ctx, a.ID, HashToken(raw2)))
	_, err = st.SessionUser(ctx, raw2, ttl)
	require.ErrorIs(t, err, ErrNotFound)

	// Expired sessions are not listed.
	now = now.Add(2 * time.Hour)
	list, err = st.SessionsOf(ctx, a.ID)
	require.NoError(t, err)
	require.Empty(t, list)
}

// Gap E: the audit log pages newest first, counts what matched and filters by user.
func TestAuditPagesNewestFirstWithACount(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550200")
	for i := 0; i < 60; i++ {
		require.NoError(t, st.Audit(ctx, AuditEvent{UserID: a.ID, Action: "signin"}))
	}
	require.NoError(t, st.Audit(ctx, AuditEvent{UserID: b.ID, ActorID: a.ID, Action: "user_disabled", Detail: "x"}))

	page, total, err := st.ListAudit(ctx, AuditQuery{Limit: 50})
	require.NoError(t, err)
	require.Equal(t, 61, total)
	require.Len(t, page, 50)
	require.Equal(t, "user_disabled", page[0].Action, "newest first")
	require.Equal(t, a.ID, page[0].ActorID)
	require.Equal(t, "x", page[0].Detail)

	page, total, err = st.ListAudit(ctx, AuditQuery{Limit: 50, Offset: 50})
	require.NoError(t, err)
	require.Equal(t, 61, total)
	require.Len(t, page, 11)

	page, total, err = st.ListAudit(ctx, AuditQuery{UserID: b.ID, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, page, 1)

	// Filtering by a user also finds what they did to others.
	_, total, err = st.ListAudit(ctx, AuditQuery{UserID: a.ID, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, 61, total)
}

func TestUsersByIDLooksUpSeveralAtOnce(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550200")
	m, err := st.UsersByID(ctx, []string{a.ID, b.ID, "u_gone", ""})
	require.NoError(t, err)
	require.Len(t, m, 2)
	require.Equal(t, "+27825550200", m[b.ID].Phone)
}
