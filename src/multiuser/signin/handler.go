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

	// A waitlisted number hears back at most once an hour, however often it
	// asks: its place does not change faster than that.
	defaultWaitlistMax    = 1
	defaultWaitlistWindow = time.Hour
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

	// Waitlist records an unknown number that may not sign up now. Nil keeps
	// the plain "isn't open for sign-ups yet" reply.
	Waitlist Waitlist
	// Full reports whether the server has reached its cap on linked numbers.
	// With SignupOpen, a new number joins the waitlist while it is full. Nil
	// means never full.
	Full func(ctx context.Context) bool
	// DefaultMaxDevices is a new user's own limit. 0 means the column default.
	DefaultMaxDevices int
	// WaitlistLimiter caps waitlist replies per number, on top of Limiter.
	// Nil means 1 per hour.
	WaitlistLimiter *Limiter

	once        sync.Once
	defSender   *Limiter
	defGlobal   *Limiter
	defWaitlist *Limiter
	inflight    chan struct{}
}

// Waitlist is the part of the store the handler needs. JoinWaitlist is
// idempotent per phone; joined is false for a declined number.
type Waitlist interface {
	JoinWaitlist(ctx context.Context, phone string) (joined bool, position int, err error)
}

func (h *Handler) init() {
	h.once.Do(func() {
		h.defSender = NewLimiter(defaultPerSenderMax, defaultPerSenderWindow)
		h.defGlobal = NewLimiter(defaultGlobalMax, defaultGlobalWindow)
		h.defWaitlist = NewLimiter(defaultWaitlistMax, defaultWaitlistWindow)
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

func (h *Handler) waitlistLimiter() *Limiter {
	if h.WaitlistLimiter != nil {
		return h.WaitlistLimiter
	}
	return h.defWaitlist
}

func (h *Handler) full(ctx context.Context) bool {
	return h.Full != nil && h.Full(ctx)
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
	if !ok || m == nil {
		return // not a message event at all - fires constantly for every other event type on every device, so never logged
	}
	if inst == nil || h.ServiceDeviceID == nil {
		// Should never happen in production: handler() in event_handler.go
		// never calls EventHook with a nil instance, and NewRuntime always
		// wires ServiceDeviceID. If this fires, the hook is wired wrong.
		logrus.Warn("[WTF] sign-in: outcome=misconfigured (no device instance or no ServiceDeviceID resolver)")
		return
	}
	// Cheap checks first: every message on every linked phone passes here.
	reason := h.ignorable(m)
	matches := reason == "" && h.isSignInRequest(m.Message)
	if reason != "" || !matches {
		h.logNearMiss(messageText(m.Message), reason)
		return
	}
	if id := h.ServiceDeviceID(ctx); id == "" || inst.ID() != id {
		// The phrase matched - reason == "" above proves it - but the live
		// ServiceDeviceID lookup disagrees with which device actually
		// received it. Both are internal device ids, never a phone number
		// or message text, so logging them is safe and is the one piece of
		// information that can actually explain this particular silence:
		// a correctly-typed phrase that still produces nothing, with no
		// near-miss and no outcome logged either, because Handle() is never
		// reached at all.
		logrus.Infof("[WTF] sign-in: a matching phrase arrived on device %q but the configured service device is %q", inst.ID(), id)
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
	if evt == nil {
		return // a nil pointer, never produced by OnEvent's own dispatch
	}
	if h.Store == nil {
		logrus.Warn("[WTF] sign-in: outcome=store_unconfigured")
		return
	}
	if reason := h.ignorable(evt); reason != "" || !h.isSignInRequest(evt.Message) {
		// Should never happen in production: OnEvent only ever dispatches to
		// Handle after proving this exact message already matches. If this
		// fires, OnEvent and Handle have gone out of sync with each other.
		logrus.Warnf("[WTF] sign-in: outcome=handle_guard_unexpected (ignorable=%q)", reason)
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
			h.logOutcome("lid_unresolved_rate_limited")
			return
		}
		h.reply(ctx, inst, info.Chat, "Sorry, we couldn't read your number from this chat. Please update WhatsApp and try again.")
		h.logOutcome("lid_unresolved")
		return
	}
	phone, err := store.NormalizePhone(pn.User)
	if err != nil {
		h.logOutcome("invalid_phone")
		return
	}
	if !h.allow(phone, now) {
		h.logOutcome("rate_limited") // silent to the sender: never amplify
		return
	}

	user, err := h.Store.UserByPhone(ctx, phone)
	switch {
	case errors.Is(err, store.ErrNotFound):
		superadmin := h.IsSuperadmin != nil && h.IsSuperadmin(phone)
		if !superadmin && (!h.SignupOpen || h.full(ctx)) {
			h.notYet(ctx, inst, info.Chat, phone, now)
			return
		}
		created, cerr := h.createUser(ctx, phone)
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
		h.logOutcome("disabled")
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
	h.logOutcome("sign_in_link_sent")
}

// logOutcome names what Handle() (or notYet) actually did with a recognised
// sign-in request - which reply went out, or which silent limit caught it -
// never the phone number or any text. Handle's own first guard (not a
// recognised request at all) is OnEvent's logNearMiss instead; this covers
// everything a reply is guaranteed for once that much is true, success
// included, so a real report can be matched to an outcome without guessing.
func (h *Handler) logOutcome(outcome string) {
	logrus.Infof("[WTF] sign-in: outcome=%s", outcome)
}

func (h *Handler) createUser(ctx context.Context, phone string) (*store.User, error) {
	if h.DefaultMaxDevices > 0 {
		return h.Store.CreateUserWithLimit(ctx, phone, h.DefaultMaxDevices)
	}
	return h.Store.CreateUser(ctx, phone)
}

// notYet answers an unknown number that may not sign up now: sign-up is
// closed, or open but the server is full. It joins the waitlist and is told
// its place, at most once an hour. A declined number gets the plain closed
// reply. The reply is built from the product name and a number only, never
// from anything the sender wrote or their profile, and it goes to the chat
// they opened.
func (h *Handler) notYet(ctx context.Context, inst *whatsapp.DeviceInstance, chat types.JID, phone string, now time.Time) {
	closed := fmt.Sprintf("%s isn't open for sign-ups yet. We'll let you know when it is.", h.ProductName)
	if h.Waitlist == nil {
		h.reply(ctx, inst, chat, closed)
		h.logOutcome("closed_no_waitlist")
		return
	}
	joined, position, err := h.Waitlist.JoinWaitlist(ctx, phone)
	if err != nil {
		logrus.WithError(err).Error("[WTF] sign-in: waitlist join failed")
		return // never promise a place we did not record
	}
	if !h.waitlistLimiter().Allow(phone, now) {
		h.logOutcome("waitlist_rate_limited") // silent to the sender: never amplify
		return
	}
	if !joined {
		h.reply(ctx, inst, chat, closed)
		h.logOutcome("declined")
		return
	}
	_ = h.Store.Audit(ctx, store.AuditEvent{Action: "waitlist_joined"})
	h.reply(ctx, inst, chat, fmt.Sprintf("You're on the %s waitlist (number %d). We'll message you here when there's room.", h.ProductName, position))
	h.logOutcome("waitlisted")
}

// allow charges one reply to key and to the global budget. Both must pass.
func (h *Handler) allow(key string, now time.Time) bool {
	return h.perSender().Allow(key, now) && h.global().Allow(globalKey, now)
}

// ignorable reports why evt is not a fresh one-to-one message from another
// person - our own messages, groups, status and broadcast lists,
// newsletters, bots, edits and backlog - or "" when it might be one. The
// reason names which check matched and nothing else, so logNearMiss
// can report it without ever touching the message or its sender.
func (h *Handler) ignorable(evt *events.Message) string {
	info := evt.Info
	switch {
	case info.IsFromMe:
		return "from_me"
	case info.IsGroup:
		return "group"
	case info.Chat.IsEmpty():
		return "empty_chat"
	case info.IsIncomingBroadcast():
		return "broadcast"
	}
	switch info.Chat.Server {
	case types.GroupServer, types.BroadcastServer, types.NewsletterServer:
		return "chat_server"
	}
	switch info.Sender.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
	default:
		return "sender_server"
	}
	if info.Sender.IsBot() {
		return "bot_sender"
	}
	if info.Edit != "" {
		return "edit"
	}
	if !info.Timestamp.IsZero() && h.now().Sub(info.Timestamp) > maxMessageAge {
		return "too_old"
	}
	return ""
}

// logNearMiss is a diagnostic trace for a feature that is otherwise
// completely silent by design (an unrecognised request from a stranger must
// never be amplified back to them). It costs nothing for ordinary chatter: it
// is pure text comparison, the same cost class as isSignInRequest's own
// normalizeText that already runs for every message, with no store or
// settings lookup - TestOnEventSkipsChatterWithoutTouchingTheStore pins that
// ordinary traffic must never call ServiceDeviceID, so this cannot scope
// itself to the service device the way the real match does.
//
// It only fires when the text itself already resembles an attempt - contains
// both "sign" and this product's name as whole words - so it stays quiet for
// real conversation while still catching the two shapes a report like "I
// sent the phrase and got nothing back" needs distinguished: the phrase was
// close but not exact (reason defaults to "phrase_mismatch"), or it was
// exact but something else about the message disqualified it (the ignorable
// reason). It never logs the message text, the phrase comparison, or any
// phone number.
func (h *Handler) logNearMiss(text, reason string) {
	if reason == "from_me" || !looksLikeASignInAttempt(text, h.ProductName) {
		return
	}
	if reason == "" {
		reason = "phrase_mismatch"
	}
	logrus.Infof("[WTF] sign-in: a near-miss sign-in attempt did not trigger (reason=%s)", reason)
}

// looksLikeASignInAttempt is deliberately looser than isSignInRequest: it
// only decides whether a near miss is worth a diagnostic log line, never
// whether to treat it as a real sign-in request.
func looksLikeASignInAttempt(text, product string) bool {
	product = strings.ToLower(strings.TrimSpace(product))
	if text == "" || product == "" {
		return false
	}
	hasSign, hasProduct := false, false
	for _, w := range strings.Fields(normalizeText(text)) {
		switch w {
		case "sign":
			hasSign = true
		case product:
			hasProduct = true
		}
	}
	return hasSign && hasProduct
}

// messageText extracts the plain text isSignInRequest and looksLikeASignInAttempt
// both compare against: a Conversation, or an ExtendedTextMessage's text.
func messageText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	text := m.GetConversation()
	if text == "" {
		text = m.GetExtendedTextMessage().GetText()
	}
	return text
}

// isSignInRequest matches the prefilled wa.me text, "Sign me in to <product>",
// tolerating case, spacing and trailing punctuation. Nothing looser: the
// service number does not reply to ordinary chatter.
func (h *Handler) isSignInRequest(m *waE2E.Message) bool {
	product := strings.TrimSpace(h.ProductName)
	if product == "" {
		return false
	}
	text := messageText(m)
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
