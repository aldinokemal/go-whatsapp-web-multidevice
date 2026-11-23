// Package signin turns "Sign me in to WTF" messages on the service number into
// single-use sign-in links sent back in the same chat.
//
// The service number is reachable by anyone on WhatsApp, so every path here is
// written to be abused: replies are rate limited per sender and globally (a
// flood must never turn the number into a spam source and get it banned),
// concurrent work is capped, panics are contained, and message text, tokens
// and links are never logged.
package signin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const Phrase = "Sign me in to"

const (
	tokenTTL = 15 * time.Minute

	// handleTimeout bounds one message's work (DB plus one send).
	handleTimeout = 20 * time.Second

	// maxInflight caps concurrent sign-in work started from the event hook.
	// Messages past it are dropped, never queued: a flood cannot pile up
	// goroutines or DB connections.
	maxInflight = 8

	// maxMessageAge drops messages queued while the service number was
	// offline. Answering a backlog all at once looks like spam to WhatsApp,
	// and a link for a request made long ago is not what anyone is waiting for.
	maxMessageAge = 10 * time.Minute

	// maxTextLen: the phrase is short; anything longer is not a sign-in request.
	maxTextLen = 200

	// Defaults used when the Handler leaves a limiter nil.
	defaultPerSenderMax    = 3
	defaultPerSenderWindow = time.Hour
	defaultGlobalMax       = 20
	defaultGlobalWindow    = time.Minute
)

const globalKey = "*"

type Sender interface {
	SendText(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) error
}

// ClientSender is the real Sender: it sends through the device's whatsmeow client.
type ClientSender struct{}

func (ClientSender) SendText(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) error {
	if inst == nil {
		return errors.New("no service device")
	}
	client := inst.GetClient()
	if client == nil {
		return errors.New("service number not connected")
	}
	_, err := client.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(text)})
	return err
}

type Handler struct {
	Store       *store.Store
	Send        Sender // nil means ClientSender
	ProductName string
	PublicURL   string
	SignupOpen  bool
	// IsSuperadmin reports whether an E.164 phone is on the superadmin allowlist.
	// Nil means nobody is.
	IsSuperadmin func(phone string) bool
	// Limiter caps replies per sender (phone, or LID when unresolved).
	// Nil means 3 per hour.
	Limiter *Limiter
	// Global caps replies across all senders, so the service number never
	// sends in bulk. Nil means 20 per minute.
	Global *Limiter
	// ResolvePN maps a LID to a phone-number JID. Nil means DefaultResolvePN.
	ResolvePN func(ctx context.Context, inst *whatsapp.DeviceInstance, jid types.JID) types.JID
	// ServiceDeviceID returns the device id of the service number, or "" when
	// none is set. OnEvent answers only on that device.
	ServiceDeviceID func(ctx context.Context) string
	Now             func() time.Time // nil means time.Now

	once      sync.Once
	defSender *Limiter
	defGlobal *Limiter
	inflight  chan struct{}
}

func (h *Handler) init() {
	h.once.Do(func() {
		h.defSender = NewLimiter(defaultPerSenderMax, defaultPerSenderWindow)
		h.defGlobal = NewLimiter(defaultGlobalMax, defaultGlobalWindow)
		h.inflight = make(chan struct{}, maxInflight)
	})
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Handler) perSender() *Limiter {
	if h.Limiter != nil {
		return h.Limiter
	}
	return h.defSender
}

func (h *Handler) global() *Limiter {
	if h.Global != nil {
		return h.Global
	}
	return h.defGlobal
}

func (h *Handler) sender() Sender {
	if h.Send != nil {
		return h.Send
	}
	return ClientSender{}
}

// DefaultResolvePN looks the LID up in the device's LID store. It returns the
// JID unchanged when there is no mapping.
func DefaultResolvePN(ctx context.Context, inst *whatsapp.DeviceInstance, jid types.JID) types.JID {
	if inst == nil {
		return jid
	}
	return whatsapp.NormalizeJIDFromLID(ctx, jid, inst.GetClient())
}

// OnEvent is the whatsapp.EventHook entry point. It runs on the event loop of
// every device, so it filters cheaply and synchronously, then hands real work
// to a bounded goroutine. It never panics and never blocks.
func (h *Handler) OnEvent(ctx context.Context, inst *whatsapp.DeviceInstance, evt any) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("[WTF] sign-in: recovered from panic in event hook\n%s", debug.Stack())
		}
	}()
	m, ok := evt.(*events.Message)
	if !ok || m == nil || inst == nil || h.ServiceDeviceID == nil {
		return
	}
	// Cheap checks first: every message on every linked phone passes here.
	if h.ignorable(m) || !h.isSignInRequest(m.Message) {
		return
	}
	if id := h.ServiceDeviceID(ctx); id == "" || inst.ID() != id {
		return
	}
	h.init()
	select {
	case h.inflight <- struct{}{}:
	default:
		logrus.Warn("[WTF] sign-in: busy, dropped a request")
		return
	}
	go func() {
		defer func() { <-h.inflight }()
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), handleTimeout)
		defer cancel()
		h.Handle(c, inst, m)
	}()
}

// Handle answers one message synchronously. OnEvent is the production entry
// point; tests call Handle directly.
func (h *Handler) Handle(ctx context.Context, inst *whatsapp.DeviceInstance, evt *events.Message) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("[WTF] sign-in: recovered from panic\n%s", debug.Stack())
		}
	}()
	if evt == nil || h.Store == nil || h.ignorable(evt) || !h.isSignInRequest(evt.Message) {
		return
	}
	h.init()
	info := evt.Info
	now := h.now()

	sender := info.Sender
	if sender.Server == types.HiddenUserServer && info.SenderAlt.Server == types.DefaultUserServer && info.SenderAlt.User != "" {
		sender = info.SenderAlt // the server told us the phone number
	}
	resolve := h.ResolvePN
	if resolve == nil {
		resolve = DefaultResolvePN
	}
	pn := sender
	if pn.Server != types.DefaultUserServer {
		pn = resolve(ctx, inst, sender)
	}

	if pn.Server != types.DefaultUserServer || pn.User == "" {
		// A LID with no phone mapping. Never create an account under a LID.
		if !h.allow("lid:"+sender.User, now) {
			return
		}
		h.reply(ctx, inst, info.Chat, "Sorry, we couldn't read your number from this chat. Please update WhatsApp and try again.")
		return
	}
	phone, err := store.NormalizePhone(pn.User)
	if err != nil {
		return
	}
	if !h.allow(phone, now) {
		return // silent: never amplify
	}

	user, err := h.Store.UserByPhone(ctx, phone)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if !h.SignupOpen && (h.IsSuperadmin == nil || !h.IsSuperadmin(phone)) {
			h.reply(ctx, inst, info.Chat, fmt.Sprintf("%s isn't open for sign-ups yet. We'll let you know when it is.", h.ProductName))
			return
		}
		created, cerr := h.Store.CreateUser(ctx, phone)
		if cerr != nil {
			// Most likely a concurrent first message from the same number won
			// the insert. Use its row; anything else is a real failure.
			if user, err = h.Store.UserByPhone(ctx, phone); err != nil {
				logrus.WithError(cerr).Error("[WTF] sign-in: create user failed")
				return
			}
		} else {
			user = created
			_ = h.Store.Audit(ctx, store.AuditEvent{UserID: user.ID, Action: "signup"})
		}
	case err != nil:
		logrus.WithError(err).Error("[WTF] sign-in: user lookup failed")
		return
	}
	if user.Status != store.StatusActive {
		h.reply(ctx, inst, info.Chat, fmt.Sprintf("This %s account is disabled.", h.ProductName))
		return
	}

	raw, err := h.Store.CreateLoginToken(ctx, phone, tokenTTL)
	if err != nil {
		logrus.WithError(err).Error("[WTF] sign-in: token failed")
		return
	}
	_ = h.Store.Audit(ctx, store.AuditEvent{UserID: user.ID, Action: "signin_link_sent"})
	link := h.PublicURL + "/signin/verify?t=" + url.QueryEscape(raw)
	h.reply(ctx, inst, info.Chat, fmt.Sprintf("Here's your %s sign-in link. It works once, for 15 minutes:\n\n%s\n\nIf you didn't ask for this, ignore this message.", h.ProductName, link))
}

// allow charges one reply to key and to the global budget. Both must pass.
func (h *Handler) allow(key string, now time.Time) bool {
	return h.perSender().Allow(key, now) && h.global().Allow(globalKey, now)
}

// ignorable drops everything that is not a fresh one-to-one message from
// another person: our own messages, groups, status and broadcast lists,
// newsletters, bots, edits and backlog.
func (h *Handler) ignorable(evt *events.Message) bool {
	info := evt.Info
	if info.IsFromMe || info.IsGroup || info.Chat.IsEmpty() || info.IsIncomingBroadcast() {
		return true
	}
	switch info.Chat.Server {
	case types.GroupServer, types.BroadcastServer, types.NewsletterServer:
		return true
	}
	switch info.Sender.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
	default:
		return true
	}
	if info.Sender.IsBot() || info.Edit != "" {
		return true
	}
	if !info.Timestamp.IsZero() && h.now().Sub(info.Timestamp) > maxMessageAge {
		return true
	}
	return false
}

// isSignInRequest matches the prefilled wa.me text, "Sign me in to <product>",
// tolerating case, spacing and trailing punctuation. Nothing looser: the
// service number does not reply to ordinary chatter.
func (h *Handler) isSignInRequest(m *waE2E.Message) bool {
	product := strings.TrimSpace(h.ProductName)
	if m == nil || product == "" {
		return false
	}
	text := m.GetConversation()
	if text == "" {
		text = m.GetExtendedTextMessage().GetText()
	}
	if text == "" || len(text) > maxTextLen {
		return false
	}
	return normalizeText(text) == normalizeText(Phrase+" "+product)
}

func normalizeText(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.TrimSpace(strings.TrimRight(s, ".!? "))
}

func (h *Handler) reply(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) {
	if err := h.sender().SendText(ctx, inst, to, text); err != nil {
		logrus.WithError(err).Warn("[WTF] sign-in reply failed")
	}
}
