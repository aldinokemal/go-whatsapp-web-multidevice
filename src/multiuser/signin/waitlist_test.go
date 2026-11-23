package signin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

const stranger = "27825550199"

// waitlistHandler is newHandler with the waitlist wired to the real store
// and a capacity switch the test controls.
func waitlistHandler(t *testing.T, open bool) (*Handler, *recorder, *store.Store, *bool) {
	t.Helper()
	h, rec, st := newHandler(t, open)
	full := false
	h.Waitlist = st
	h.Full = func(context.Context) bool { return full }
	h.DefaultMaxDevices = 3
	return h, rec, st, &full
}

func ask(h *Handler, from string) {
	h.Handle(context.Background(), svc, msg(pn(from), "Sign me in to WTF"))
}

func waiting(t *testing.T, st *store.Store) []store.WaitlistEntry {
	t.Helper()
	_, items, err := st.WaitlistPage(context.Background(), store.WaitlistWaiting, 0, 50)
	require.NoError(t, err)
	return items
}

// Behaviour.
func TestClosedSignupJoinsTheWaitlist(t *testing.T) {
	h, rec, st, _ := waitlistHandler(t, false)
	ask(h, "27825550198")
	ask(h, stranger)
	require.Equal(t, 2, rec.count())
	require.Equal(t, pn(stranger), rec.msgs[1].to, "the reply goes to the chat they opened")
	require.Equal(t, "You're on the WTF waitlist (number 2). We'll message you here when there's room.", rec.msgs[1].text)
	_, err := st.UserByPhone(context.Background(), "+"+stranger)
	require.ErrorIs(t, err, store.ErrNotFound, "the waitlist is not an account")
	require.Len(t, waiting(t, st), 2)
}

func TestOpenSignupCreatesWhileThereIsRoom(t *testing.T) {
	h, rec, st, _ := waitlistHandler(t, true)
	ask(h, stranger)
	require.Contains(t, rec.msgs[0].text, "/signin/verify?t=")
	u, err := st.UserByPhone(context.Background(), "+"+stranger)
	require.NoError(t, err)
	require.Equal(t, 3, u.MaxDevices, "new users get the configured default limit")
	require.Empty(t, waiting(t, st))
}

// Review Focus 4: open sign-up while full puts people on the waitlist
// instead of creating accounts that can link nothing.
func TestOpenSignupWhenFullJoinsTheWaitlist(t *testing.T) {
	h, rec, st, full := waitlistHandler(t, true)
	*full = true
	ask(h, stranger)
	require.Equal(t, 1, rec.count())
	require.Contains(t, rec.msgs[0].text, "waitlist (number 1)")
	require.NotContains(t, rec.msgs[0].text, "/signin/verify")
	_, err := st.UserByPhone(context.Background(), "+"+stranger)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Len(t, waiting(t, st), 1)
}

func TestSuperadminNeverWaitlisted(t *testing.T) {
	for _, open := range []bool{false, true} {
		h, rec, st, full := waitlistHandler(t, open)
		*full = true
		ask(h, "27825550100") // the superadmin in newHandler
		require.Contains(t, rec.msgs[0].text, "/signin/verify?t=")
		require.Empty(t, waiting(t, st))
	}
}

func TestDeclinedGetsTheClosedReply(t *testing.T) {
	h, rec, st, _ := waitlistHandler(t, false)
	_, _, _ = st.JoinWaitlist(context.Background(), "+"+stranger)
	require.NoError(t, st.DeclineWaitlist(context.Background(), "+"+stranger, "u_admin"))
	ask(h, stranger)
	require.Equal(t, 1, rec.count())
	require.Equal(t, "WTF isn't open for sign-ups yet. We'll let you know when it is.", rec.msgs[0].text)
}

func TestKnownAndDisabledUsersAreUnchanged(t *testing.T) {
	h, rec, st, full := waitlistHandler(t, false)
	*full = true
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	d, _ := st.CreateUser(context.Background(), "+27825550102")
	_ = st.SetUserStatus(context.Background(), d.ID, store.StatusDisabled)
	ask(h, "27825550101")
	ask(h, "27825550102")
	require.Contains(t, rec.msgs[0].text, "/signin/verify?t=", "a full server still signs existing users in")
	require.Contains(t, rec.msgs[1].text, "disabled")
	require.Empty(t, waiting(t, st))
}

func TestWaitlistJoinIsAudited(t *testing.T) {
	h, _, st, _ := waitlistHandler(t, false)
	ask(h, stranger)
	all, _, err := st.ListAudit(context.Background(), store.AuditQuery{})
	require.NoError(t, err)
	var rows []store.AuditRow
	for _, r := range all {
		if r.Action == "waitlist_joined" {
			rows = append(rows, r)
		}
	}
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].UserID)
	require.NotContains(t, rows[0].Detail, "5550199", "the phone stays in the waitlist table only")
}

type brokenWaitlist struct{}

func (brokenWaitlist) JoinWaitlist(context.Context, string) (bool, int, error) {
	return false, 0, errors.New("disk full")
}

func TestWaitlistFailureSendsNothing(t *testing.T) {
	h, rec, _, _ := waitlistHandler(t, false)
	h.Waitlist = brokenWaitlist{}
	ask(h, stranger)
	require.Zero(t, rec.count(), "no reply promising a place we did not record")
}

// Security.
func TestWaitlistReplyNeverEchoesTheMessage(t *testing.T) {
	h, rec, _, _ := waitlistHandler(t, false)
	m := msg(pn(stranger), "Sign me in to WTF")
	m.Info.PushName = "Mallory https://evil.example/x"
	m.Info.VerifiedName = nil
	h.Handle(context.Background(), svc, m)
	// Anything with more in it than the phrase is not a request at all.
	h.Handle(context.Background(), svc, msg(pn("27825550198"), "Sign me in to WTF https://evil.example Mallory"))
	require.Equal(t, 1, rec.count())
	for _, bad := range []string{"Mallory", "evil", "https://", "5550199"} {
		require.NotContains(t, rec.msgs[0].text, bad)
	}
}

func TestLIDIsNeverWaitlisted(t *testing.T) {
	h, rec, st, _ := waitlistHandler(t, false)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF"))
	require.Equal(t, 1, rec.count())
	require.Contains(t, rec.msgs[0].text, "couldn't read your number")
	n, err := st.WaitlistCount(context.Background(), store.WaitlistWaiting)
	require.NoError(t, err)
	require.Zero(t, n)
}

// Review Focus 2: one number asking over and over gets one reply and one row.
func TestRepeatedMessagesReplyOnce(t *testing.T) {
	h, rec, st, _ := waitlistHandler(t, false)
	base := h.Now()
	for i := 0; i < 5; i++ {
		at := base.Add(time.Duration(i) * 10 * time.Minute)
		h.Now = func() time.Time { return at }
		ask(h, stranger)
	}
	require.Equal(t, 1, rec.count(), "one waitlist reply per number per hour")
	require.Len(t, waiting(t, st), 1, "repeated asks never add rows")

	later := base.Add(61 * time.Minute)
	h.Now = func() time.Time { return later }
	ask(h, stranger)
	require.Equal(t, 2, rec.count(), "they hear back again after the hour")
	require.True(t, strings.Contains(rec.msgs[1].text, "(number 1)"))
}

func TestWaitlistRepliesShareTheGlobalBudget(t *testing.T) {
	h, rec, _, _ := waitlistHandler(t, false)
	h.Global = NewLimiter(3, time.Minute)
	for i := 0; i < 10; i++ {
		ask(h, "2782555"+string(rune('0'+i))+"000")
	}
	require.Equal(t, 3, rec.count(), "a flood of new numbers never turns into a flood of replies")
}
