// src/multiuser/signin/handler_test.go
package signin

import (
	"context"
	"path/filepath"
	"strings"
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

type sent struct {
	to   types.JID
	text string
}
type recorder struct {
	mu   sync.Mutex
	msgs []sent
}

func (r *recorder) SendText(_ context.Context, _ *whatsapp.DeviceInstance, to types.JID, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, sent{to, text})
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

func newHandler(t *testing.T, open bool) (*Handler, *recorder, *store.Store) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	rec := &recorder{}
	return &Handler{
		Store: st, Send: rec, ProductName: "WTF", PublicURL: "https://wtf.onrender.com", SignupOpen: open,
		IsSuperadmin: func(p string) bool { return p == "+27825550100" },
		Limiter:      NewLimiter(3, time.Hour),
		ResolvePN:    func(_ context.Context, _ *whatsapp.DeviceInstance, j types.JID) types.JID { return j },
		Now:          func() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) },
	}, rec, st
}

func msg(sender types.JID, text string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: sender, Sender: sender},
			ID:            "M1",
		},
		Message: &waE2E.Message{Conversation: proto.String(text)},
	}
}

var svc = whatsapp.NewDeviceInstance("d_service", nil, nil)

func TestKnownUserGetsALink(t *testing.T) {
	h, rec, st := newHandler(t, false)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	from := types.NewJID("27825550101", types.DefaultUserServer)
	h.Handle(context.Background(), svc, msg(from, "Sign me in to WTF"))
	require.Len(t, rec.msgs, 1)
	require.Equal(t, from, rec.msgs[0].to)
	require.Contains(t, rec.msgs[0].text, "https://wtf.onrender.com/signin/verify?t=")
	raw := rec.msgs[0].text[strings.Index(rec.msgs[0].text, "t=")+2:]
	raw = strings.Fields(raw)[0]
	phone, err := st.ConsumeLoginToken(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "+27825550101", phone)
}

func TestClosedSignupRefusesStrangersButAdmitsSuperadmin(t *testing.T) {
	h, rec, st := newHandler(t, false)
	h.Handle(context.Background(), svc, msg(types.NewJID("27825550199", types.DefaultUserServer), "sign me in to wtf"))
	require.Len(t, rec.msgs, 1)
	require.NotContains(t, rec.msgs[0].text, "/signin/verify")
	_, err := st.UserByPhone(context.Background(), "+27825550199")
	require.ErrorIs(t, err, store.ErrNotFound, "no account created")

	h.Handle(context.Background(), svc, msg(types.NewJID("27825550100", types.DefaultUserServer), "Sign me in to WTF"))
	require.Len(t, rec.msgs, 2)
	require.Contains(t, rec.msgs[1].text, "/signin/verify?t=")
	_, err = st.UserByPhone(context.Background(), "+27825550100")
	require.NoError(t, err, "superadmin created on first contact")
}

func TestOpenSignupCreatesUser(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.Handle(context.Background(), svc, msg(types.NewJID("27825550199", types.DefaultUserServer), "Sign me in to WTF"))
	require.Contains(t, rec.msgs[0].text, "/signin/verify?t=")
	_, err := st.UserByPhone(context.Background(), "+27825550199")
	require.NoError(t, err)
}

func TestIgnoredMessages(t *testing.T) {
	h, rec, st := newHandler(t, true)
	from := types.NewJID("27825550101", types.DefaultUserServer)
	h.Handle(context.Background(), svc, msg(from, "hello there")) // not the phrase
	group := msg(from, "Sign me in to WTF")
	group.Info.IsGroup = true
	group.Info.Chat = types.NewJID("120363000000000000", types.GroupServer)
	h.Handle(context.Background(), svc, group)
	mine := msg(from, "Sign me in to WTF")
	mine.Info.IsFromMe = true
	h.Handle(context.Background(), svc, mine)
	require.Empty(t, rec.msgs)
	n, _ := st.ListUsers(context.Background())
	require.Empty(t, n)
}

func TestUnresolvableLIDIsRefusedWithoutStoringAnything(t *testing.T) {
	h, rec, st := newHandler(t, true)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF"))
	require.Len(t, rec.msgs, 1)
	require.Contains(t, rec.msgs[0].text, "couldn't read your number")
	users, _ := st.ListUsers(context.Background())
	require.Empty(t, users)
}

func TestDisabledUserIsTold(t *testing.T) {
	h, rec, st := newHandler(t, true)
	u, _ := st.CreateUser(context.Background(), "+27825550101")
	_ = st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled)
	h.Handle(context.Background(), svc, msg(types.NewJID("27825550101", types.DefaultUserServer), "Sign me in to WTF"))
	require.Contains(t, rec.msgs[0].text, "disabled")
	require.NotContains(t, rec.msgs[0].text, "/signin/verify")
}

func TestRateLimitIsSilent(t *testing.T) {
	h, rec, st := newHandler(t, true)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	from := types.NewJID("27825550101", types.DefaultUserServer)
	for i := 0; i < 5; i++ {
		h.Handle(context.Background(), svc, msg(from, "Sign me in to WTF"))
	}
	require.Len(t, rec.msgs, 3, "no reply past the limit, so we never amplify spam")
}

func TestOnEventOnlyServesTheServiceDevice(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return "d_service" }
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	userPhone := whatsapp.NewDeviceInstance("d_users_own_phone", nil, nil)
	h.OnEvent(context.Background(), userPhone, msg(types.NewJID("27825550101", types.DefaultUserServer), "Sign me in to WTF"))
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, rec.count(), "a user's own linked phone never answers sign-in messages")
}
