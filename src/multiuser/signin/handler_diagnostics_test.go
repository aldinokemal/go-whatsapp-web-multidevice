// src/multiuser/signin/handler_diagnostics_test.go
package signin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

// captureLogs points the package logger at an in-memory hook for the
// duration of one test and restores it afterwards.
func captureLogs(t *testing.T) *logrustest.Hook {
	t.Helper()
	hook := logrustest.NewLocal(logrus.StandardLogger())
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks)) })
	return hook
}

func logLines(hook *logrustest.Hook) []string {
	lines := make([]string, len(hook.AllEntries()))
	for i, e := range hook.AllEntries() {
		lines[i] = e.Message
	}
	return lines
}

func anyContains(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// Caught the real bug this pins: a real voice-note-shaped near miss ("Sign
// me into WTF" - one word, not "Sign me in to WTF") produced zero signal
// anywhere - no reply, no waitlist entry, no log line - making a completely
// silent-by-design feature impossible to debug from a live report alone.
// This is the first thing that makes that observable, without giving
// ServiceDeviceID (a settings/store lookup in production) a new per-message
// call that TestOnEventSkipsChatterWithoutTouchingTheStore forbids: it is
// pure text comparison, scoped by resemblance to the phrase rather than by
// which device the message landed on.
func TestOnEventLogsAMismatchedPhrase(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return svc.ID() }
	hook := captureLogs(t)

	from := types.NewJID("27825550199", types.DefaultUserServer)
	h.OnEvent(context.Background(), svc, msg(from, "Sign me into WTF"))
	require.Eventually(t, func() bool { return len(hook.AllEntries()) > 0 }, time.Second, time.Millisecond)

	lines := logLines(hook)
	assert.True(t, anyContains(lines, "near-miss sign-in attempt did not trigger"), "lines: %v", lines)
	assert.True(t, anyContains(lines, "reason=phrase_mismatch"), "lines: %v", lines)
	for _, l := range lines {
		assert.NotContains(t, l, "Sign me into WTF", "the logged line must never carry the message text itself")
	}
}

func TestOnEventLogsASurprisingIgnorableReasonForAnOtherwiseExactPhrase(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return svc.ID() }
	hook := captureLogs(t)

	from := types.NewJID("27825550199", types.DefaultUserServer)
	stale := msg(from, "Sign me in to WTF")
	stale.Info.Timestamp = h.now().Add(-maxMessageAge - time.Minute)
	h.OnEvent(context.Background(), svc, stale)
	require.Eventually(t, func() bool { return len(hook.AllEntries()) > 0 }, time.Second, time.Millisecond)

	assert.True(t, anyContains(logLines(hook), "reason=too_old"))
}

// A near miss is logged wherever it lands, not only on the configured
// service device: "right phrase, wrong (linked) number" is itself a real
// failure shape worth surfacing, and scoping by device would need the exact
// per-message settings lookup the cost contract above forbids.
func TestOnEventLogsANearMissOnAnyLinkedDevice(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return svc.ID() }
	hook := captureLogs(t)

	other := whatsapp.NewDeviceInstance("d_other", nil, nil)
	from := types.NewJID("27825550199", types.DefaultUserServer)
	h.OnEvent(context.Background(), other, msg(from, "Sign me into WTF"))
	require.Eventually(t, func() bool { return len(hook.AllEntries()) > 0 }, time.Second, time.Millisecond)

	assert.True(t, anyContains(logLines(hook), "reason=phrase_mismatch"))
}

// A matching phrase that still produces nothing, with no near-miss logged
// (reason == "" proves the phrase itself was fine), can only mean Handle()
// was never reached at all - the one remaining silent branch #20 and #21
// both missed, because it only exists once the phrase has already matched.
func TestOnEventLogsAMatchingPhraseOnTheWrongDevice(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return "d_service" }
	hook := captureLogs(t)

	userPhone := whatsapp.NewDeviceInstance("d_users_own_phone", nil, nil)
	from := types.NewJID("27825550199", types.DefaultUserServer)
	h.OnEvent(context.Background(), userPhone, msg(from, "Sign me in to WTF"))
	require.Eventually(t, func() bool { return len(hook.AllEntries()) > 0 }, time.Second, time.Millisecond)

	lines := logLines(hook)
	assert.True(t, anyContains(lines, "d_users_own_phone"), "lines: %v", lines)
	assert.True(t, anyContains(lines, "d_service"), "lines: %v", lines)
	for _, l := range lines {
		assert.NotContains(t, l, "Sign me in to WTF")
	}
}

func TestOnEventStaysQuietForOrdinaryChatter(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return svc.ID() }
	hook := captureLogs(t)

	from := types.NewJID("27825550199", types.DefaultUserServer)
	h.OnEvent(context.Background(), svc, msg(from, "Kk built a castle"))
	h.OnEvent(context.Background(), svc, msg(from, "hello there, how are you"))
	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, hook.AllEntries(), "ordinary chatter mentions neither 'sign' nor the product name")
}

// The service number's own sent messages would otherwise dominate this log
// the moment anyone discusses the phrase itself over WhatsApp (exactly what
// this bug report's own conversation did).
func TestOnEventStaysQuietForTheOwnSentMessagesEvenWhenTheyNameThePhrase(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return svc.ID() }
	hook := captureLogs(t)

	from := types.NewJID("27825550199", types.DefaultUserServer)
	mine := msg(from, "Sign me into WTF") // would otherwise match looksLikeASignInAttempt
	mine.Info.IsFromMe = true
	h.OnEvent(context.Background(), svc, mine)
	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, hook.AllEntries(), "the service number's own sent messages must never be logged")
}

func TestLooksLikeASignInAttempt(t *testing.T) {
	assert.True(t, looksLikeASignInAttempt("Sign me into WTF", "WTF"))
	assert.True(t, looksLikeASignInAttempt("please SIGN me in to wtf!", "WTF"))
	assert.False(t, looksLikeASignInAttempt("hello there", "WTF"))
	assert.False(t, looksLikeASignInAttempt("wtf is going on here", "WTF"), "mentions the product but never 'sign'")
	assert.False(t, looksLikeASignInAttempt("please sign this form", "WTF"), "mentions 'sign' but never the product")
	assert.False(t, looksLikeASignInAttempt("", "WTF"))
	assert.False(t, looksLikeASignInAttempt("sign me in to wtf", ""))
}
