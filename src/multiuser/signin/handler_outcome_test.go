// src/multiuser/signin/handler_outcome_test.go
package signin

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

// Caught the gap PR #20 left open: a live report ("Sign me in to WTF" sent to
// the correct, connected, correctly-configured service number, matched the
// phrase, produced zero near-miss log either) proved the message was
// reaching Handle() itself - but Handle()'s own business logic (phone
// resolution, rate limiting, the actual reply) had several silent exit
// points of its own that #20 never touched, so there was still no way to
// tell which one, or whether the happy path even logs anything at all when
// it works. These tests pin an outcome log at every one of Handle()'s exits,
// success included, with the same privacy rule as #20: never the phone
// number, never the message text.

func TestHandleLogsSignInLinkSentOnSuccess(t *testing.T) {
	h, _, st := newHandler(t, false)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	hook := captureLogs(t)
	h.Handle(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	assert.True(t, anyContains(logLines(hook), "outcome=sign_in_link_sent"))
}

func TestHandleLogsLIDUnresolved(t *testing.T) {
	h, _, _ := newHandler(t, true)
	hook := captureLogs(t)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF"))
	assert.True(t, anyContains(logLines(hook), "outcome=lid_unresolved"))
}

func TestHandleLogsLIDUnresolvedRateLimited(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.Limiter = NewLimiter(1, time.Hour) // the lid: key shares this limiter too
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF")) // consumes the one slot
	hook := captureLogs(t)
	h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF")) // now silently capped
	assert.True(t, anyContains(logLines(hook), "outcome=lid_unresolved_rate_limited"))
}

func TestHandleLogsRateLimited(t *testing.T) {
	h, _, st := newHandler(t, true)
	h.Limiter = NewLimiter(1, time.Hour)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	from := pn("27825550101")
	h.Handle(context.Background(), svc, msg(from, "Sign me in to WTF")) // consumes the one slot
	hook := captureLogs(t)
	h.Handle(context.Background(), svc, msg(from, "Sign me in to WTF")) // now silently capped
	assert.True(t, anyContains(logLines(hook), "outcome=rate_limited"))
}

func TestHandleLogsDisabled(t *testing.T) {
	h, _, st := newHandler(t, true)
	u, _ := st.CreateUser(context.Background(), "+27825550101")
	_ = st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled)
	hook := captureLogs(t)
	h.Handle(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	assert.True(t, anyContains(logLines(hook), "outcome=disabled"))
}

func TestHandleLogsClosedWithNoWaitlistConfigured(t *testing.T) {
	h, _, _ := newHandler(t, false) // Waitlist left nil, same as the package default
	hook := captureLogs(t)
	h.Handle(context.Background(), svc, msg(pn(stranger), "Sign me in to WTF"))
	assert.True(t, anyContains(logLines(hook), "outcome=closed_no_waitlist"))
}

func TestHandleLogsWaitlisted(t *testing.T) {
	h, _, _, _ := waitlistHandler(t, false)
	hook := captureLogs(t)
	ask(h, stranger)
	assert.True(t, anyContains(logLines(hook), "outcome=waitlisted"))
}

func TestHandleLogsDeclined(t *testing.T) {
	h, _, st, _ := waitlistHandler(t, false)
	_, _, _ = st.JoinWaitlist(context.Background(), "+"+stranger)
	require.NoError(t, st.DeclineWaitlist(context.Background(), "+"+stranger, "u_admin"))
	hook := captureLogs(t)
	ask(h, stranger)
	assert.True(t, anyContains(logLines(hook), "outcome=declined"))
}

// None of the above may ever carry the phone number or the message text.
func TestHandleOutcomeLogsNeverCarryPrivateData(t *testing.T) {
	h, _, st := newHandler(t, false)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	hook := captureLogs(t)
	h.Handle(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	for _, l := range logLines(hook) {
		assert.NotContains(t, l, "27825550101")
		assert.NotContains(t, l, "Sign me in to WTF")
	}
}
