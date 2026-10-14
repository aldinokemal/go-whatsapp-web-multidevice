package signin

// Adversarial tests beyond the plan: the service number must not be usable to
// spam replies, mint links for strangers, or crash the event loop.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func pn(n string) types.JID { return types.NewJID(n, types.DefaultUserServer) }

func TestPhraseMustNameThisProduct(t *testing.T) {
	h, rec, st := newHandler(t, true)
	for _, text := range []string{
		"Sign me in to OtherApp",
		"please sign me in to WTF now",
		"Sign me in to",
		"Sign me in to WTFX",
	} {
		h.Handle(context.Background(), svc, msg(pn("27825550101"), text))
	}
	require.Zero(t, rec.count())
	users, _ := st.ListUsers(context.Background())
	require.Empty(t, users)

	h.Handle(context.Background(), svc, msg(pn("27825550101"), "  Sign   me in to WTF. \n"))
	require.Equal(t, 1, rec.count(), "spacing, case and trailing punctuation are tolerated")
	require.Contains(t, rec.msgs[0].text, "/signin/verify?t=")
}

func TestExtendedTextIsAccepted(t *testing.T) {
	h, rec, _ := newHandler(t, true)
	m := msg(pn("27825550101"), "")
	m.Message = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("Sign me in to WTF")}}
	h.Handle(context.Background(), svc, m)
	require.Equal(t, 1, rec.count())
}

func TestNonTextAndBroadcastsAreIgnored(t *testing.T) {
	h, rec, st := newHandler(t, true)
	from := pn("27825550101")

	img := msg(from, "")
	img.Message = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("Sign me in to WTF")}}
	h.Handle(context.Background(), svc, img)

	status := msg(from, "Sign me in to WTF")
	status.Info.Chat = types.StatusBroadcastJID
	h.Handle(context.Background(), svc, status)

	list := msg(from, "Sign me in to WTF")
	list.Info.Chat = types.NewJID("1234567890", types.BroadcastServer)
	h.Handle(context.Background(), svc, list)

	news := msg(from, "Sign me in to WTF")
	news.Info.Chat = types.NewJID("120363000000000001", types.NewsletterServer)
	h.Handle(context.Background(), svc, news)

	bot := msg(types.NewJID("27825550101", types.BotServer), "Sign me in to WTF")
	h.Handle(context.Background(), svc, bot)

	h.Handle(context.Background(), svc, nil)
	h.Handle(context.Background(), svc, &events.Message{})

	require.Zero(t, rec.count())
	users, _ := st.ListUsers(context.Background())
	require.Empty(t, users)
}

func TestStaleMessagesAreIgnored(t *testing.T) {
	h, rec, st := newHandler(t, true)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	old := msg(pn("27825550101"), "Sign me in to WTF")
	old.Info.Timestamp = h.Now().Add(-1 * time.Hour) // queued while the service number was offline
	h.Handle(context.Background(), svc, old)
	require.Zero(t, rec.count())

	fresh := msg(pn("27825550101"), "Sign me in to WTF")
	fresh.Info.Timestamp = h.Now().Add(-1 * time.Minute)
	h.Handle(context.Background(), svc, fresh)
	require.Equal(t, 1, rec.count())
}

func TestLIDSenderWithServerSuppliedPhoneIsServed(t *testing.T) {
	h, rec, st := newHandler(t, false)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	m := msg(lid, "Sign me in to WTF")
	m.Info.SenderAlt = pn("27825550101")
	h.Handle(context.Background(), svc, m)
	require.Equal(t, 1, rec.count())
	require.Equal(t, lid, rec.msgs[0].to, "reply goes back to the chat it came from")
	require.Contains(t, rec.msgs[0].text, "/signin/verify?t=")
}

func TestUnresolvableLIDRepliesAreRateLimited(t *testing.T) {
	h, rec, st := newHandler(t, true)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	for i := 0; i < 6; i++ {
		h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF"))
	}
	require.Equal(t, 3, rec.count())
	users, _ := st.ListUsers(context.Background())
	require.Empty(t, users)
}

func TestClosedSignupRepliesAreRateLimited(t *testing.T) {
	h, rec, _ := newHandler(t, false)
	for i := 0; i < 6; i++ {
		h.Handle(context.Background(), svc, msg(pn("27825550199"), "Sign me in to WTF"))
	}
	require.Equal(t, 3, rec.count())
}

func TestGlobalLimitCapsAllReplies(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.Global = NewLimiter(2, time.Minute)
	for i := 0; i < 5; i++ {
		h.Handle(context.Background(), svc, msg(pn(fmt.Sprintf("2782555020%d", i)), "Sign me in to WTF"))
	}
	require.Equal(t, 2, rec.count())
	users, _ := st.ListUsers(context.Background())
	require.Len(t, users, 2, "senders past the global cap get no account either")
}

func TestPhoneIsStoredNormalised(t *testing.T) {
	h, _, st := newHandler(t, true)
	h.Handle(context.Background(), svc, msg(pn("27825550199"), "Sign me in to WTF"))
	u, err := st.UserByPhone(context.Background(), "+27825550199")
	require.NoError(t, err)
	require.Equal(t, "+27825550199", u.Phone)
}

func TestConcurrentFirstContactMakesOneAccount(t *testing.T) {
	h, rec, st := newHandler(t, true)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Handle(context.Background(), svc, msg(pn("27825550199"), "Sign me in to WTF"))
		}()
	}
	wg.Wait()
	users, _ := st.ListUsers(context.Background())
	require.Len(t, users, 1)
	require.Equal(t, 3, rec.count(), "the losers of the create race still get their link")
}

func TestZeroValueHandlerIsSafe(t *testing.T) {
	st, err := store.Open("file:" + t.TempDir() + "/z.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	rec := &recorder{}
	h := &Handler{Store: st, Send: rec, ProductName: "WTF", PublicURL: "https://x",
		ResolvePN: func(_ context.Context, _ *whatsapp.DeviceInstance, j types.JID) types.JID { return j }}
	// No Limiter, Global, IsSuperadmin or Now: defaults apply, closed sign-up refuses.
	for i := 0; i < 5; i++ {
		h.Handle(context.Background(), svc, msg(pn("27825550199"), "Sign me in to WTF"))
	}
	require.Equal(t, 3, rec.count())
	require.NotContains(t, rec.msgs[0].text, "/signin/verify")
}

func TestOnEventServesTheServiceDevice(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return "d_service" }
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	h.OnEvent(context.Background(), svc, "not a message")
	h.OnEvent(context.Background(), nil, msg(pn("27825550101"), "Sign me in to WTF"))
	h.OnEvent(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	require.Eventually(t, func() bool { return rec.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 1, rec.count())
}

func TestOnEventWithNoServiceDeviceDoesNothing(t *testing.T) {
	h, rec, st := newHandler(t, true)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	blank := whatsapp.NewDeviceInstance("", nil, nil)
	h.ServiceDeviceID = func(context.Context) string { return "" }
	h.OnEvent(context.Background(), blank, msg(pn("27825550101"), "Sign me in to WTF"))
	h.ServiceDeviceID = nil
	h.OnEvent(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, rec.count())
}

func TestOnEventSkipsChatterWithoutTouchingTheStore(t *testing.T) {
	h, _, _ := newHandler(t, true)
	calls := 0
	h.ServiceDeviceID = func(context.Context) string { calls++; return "d_service" }
	h.OnEvent(context.Background(), svc, msg(pn("27825550101"), "hello"))
	g := msg(pn("27825550101"), "Sign me in to WTF")
	g.Info.IsGroup = true
	h.OnEvent(context.Background(), svc, g)
	require.Zero(t, calls, "every message on every device passes the hook; ordinary chatter must be cheap")
}

type panicSender struct{ done chan struct{} }

func (p panicSender) SendText(context.Context, *whatsapp.DeviceInstance, types.JID, string) error {
	defer close(p.done)
	panic("boom")
}

func TestOnEventRecoversFromPanics(t *testing.T) {
	h, _, st := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return "d_service" }
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	ps := panicSender{done: make(chan struct{})}
	h.Send = ps
	h.OnEvent(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	<-ps.done
	time.Sleep(30 * time.Millisecond) // an unrecovered panic would kill the test binary here

	h.ServiceDeviceID = func(context.Context) string { panic("settings exploded") }
	require.NotPanics(t, func() {
		h.OnEvent(context.Background(), svc, msg(pn("27825550101"), "Sign me in to WTF"))
	})
}

type blockingSender struct {
	mu      sync.Mutex
	entered int
	release chan struct{}
	sent    int
}

func (b *blockingSender) SendText(context.Context, *whatsapp.DeviceInstance, types.JID, string) error {
	b.mu.Lock()
	b.entered++
	b.mu.Unlock()
	<-b.release
	b.mu.Lock()
	b.sent++
	b.mu.Unlock()
	return nil
}

func (b *blockingSender) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.entered, b.sent
}

func TestOnEventBoundsConcurrentWork(t *testing.T) {
	h, _, _ := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return "d_service" }
	bs := &blockingSender{release: make(chan struct{})}
	h.Send = bs
	for i := 0; i < maxInflight+4; i++ {
		h.OnEvent(context.Background(), svc, msg(pn(fmt.Sprintf("278255503%02d", i)), "Sign me in to WTF"))
	}
	require.Eventually(t, func() bool { e, _ := bs.counts(); return e == maxInflight }, 3*time.Second, 5*time.Millisecond)
	close(bs.release)
	require.Eventually(t, func() bool { _, s := bs.counts(); return s == maxInflight }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	e, _ := bs.counts()
	require.Equal(t, maxInflight, e, "a flood past the in-flight cap is dropped, not queued")
}

func TestSenderOverItsOwnLimitDoesNotDrainTheGlobalBudget(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.Global = NewLimiter(4, time.Minute)
	for i := 0; i < 10; i++ { // 3 allowed, 7 refused by the per-sender limit
		h.Handle(context.Background(), svc, msg(pn("27825550401"), "Sign me in to WTF"))
	}
	require.Equal(t, 3, rec.count())
	h.Handle(context.Background(), svc, msg(pn("27825550402"), "Sign me in to WTF"))
	require.Equal(t, 4, rec.count(), "the global budget still has room for another sender")
	h.Handle(context.Background(), svc, msg(pn("27825550403"), "Sign me in to WTF"))
	require.Equal(t, 4, rec.count(), "global budget now spent")
	_, err := st.UserByPhone(context.Background(), "+27825550403")
	require.ErrorIs(t, err, store.ErrNotFound, "no account past the global cap")
}
