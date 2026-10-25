package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Bridge receives GOWA webhooks for the CS device, asks Hermes for a reply and
// sends it back through GOWA. Admins take over by replying from the CS phone
// (the chat is then paused) or by sending "AI ON/OFF <number>" to the CS number.
// Behaviour that admins tune lives in settings (editable at /cs-ai).
type Bridge struct {
	cfg      Config
	gowa     Sender
	agent    Agent
	store    *Store
	settings *SettingsStore
	events   EventSink // lead events for the Dashboard CRM; nil = off
	log      *slog.Logger
	now      func() time.Time
	ctx      context.Context
	wg       sync.WaitGroup
	mu       sync.Mutex
	chats    map[string]*chat
	seen     map[string]time.Time // processed message ids (dedupe of webhook retries)
	sentID   map[string]time.Time // ids of messages this bridge sent
}

type chat struct {
	work     sync.Mutex // serialises AI turns of one chat
	pending  []string
	name     string
	phone    string
	timer    *time.Timer
	firstAt  time.Time
	hits     []time.Time
	notified bool // rate-limit notice already sent in this window
}

// webhookEvent is the subset of the GOWA webhook payload the bridge needs.
// See docs/webhook-payload.md in this repository.
type webhookEvent struct {
	Event     string         `json:"event"`
	SessionID string         `json:"session_id"`
	Payload   map[string]any `json:"payload"`
}

var handoffRe = regexp.MustCompile(`(?is)\[\[\s*HANDOFF\s*(?::\s*(.*?))?\]\]`)
var adminCmdRe = regexp.MustCompile(`(?i)^\s*AI\s+(ON|OFF|STATUS)\s*([+0-9 \-]*)\s*$`)

func NewBridge(ctx context.Context, cfg Config, gowa Sender, agent Agent, store *Store, settings *SettingsStore, log *slog.Logger) *Bridge {
	return &Bridge{cfg: cfg, gowa: gowa, agent: agent, store: store, settings: settings, log: log, now: time.Now, ctx: ctx,
		chats: map[string]*chat{}, seen: map[string]time.Time{}, sentID: map[string]time.Time{}}
}

// Wait blocks until all in-flight AI turns finished (used on shutdown and in tests).
func (b *Bridge) Wait() { b.wg.Wait() }

func (b *Bridge) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", b.handleWebhook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := map[string]any{"status": "ok", "paused_chats": len(b.store.ActivePauses(b.now()))}
		if o, ok := b.events.(*DashboardOutbox); ok && o != nil {
			out["dashboard_pending"] = o.Pending()
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	b.adminRoutes(mux)
	return mux
}

func (b *Bridge) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if !validSignature(body, r.Header.Get("X-Hub-Signature-256"), b.cfg.WebhookSecret) {
		b.log.Warn("rejected webhook with invalid signature", "remote", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var ev webhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	// Answer GOWA immediately; the AI turn runs in the background.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
	b.handleEvent(ev)
}

func validSignature(body []byte, header, secret string) bool {
	got, ok := strings.CutPrefix(strings.TrimSpace(header), "sha256=")
	if !ok {
		return false
	}
	gotBytes, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(gotBytes, mac.Sum(nil))
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func ignoredChat(chatID string) bool {
	return chatID == "" || strings.HasSuffix(chatID, "@g.us") || strings.HasSuffix(chatID, "@broadcast") ||
		strings.HasSuffix(chatID, "@newsletter")
}

var mediaLabels = []struct{ key, label string }{
	{"image", "gambar"}, {"video", "video"}, {"audio", "pesan suara"}, {"document", "dokumen"},
	{"sticker", "stiker"}, {"location", "lokasi"}, {"contact", "kontak"},
}

// mediaKind returns the media type label of a message, or "".
func mediaKind(p map[string]any) string {
	for _, m := range mediaLabels {
		if v, ok := p[m.key]; ok && v != nil {
			return m.label
		}
	}
	return ""
}

// mediaNote describes media to the AI. After payment info went out, an image or
// document is very likely a transfer proof, and the AI is told so.
func mediaNote(kind string, hasText, paymentSent bool) string {
	if kind == "" {
		return ""
	}
	if paymentSent && (kind == "gambar" || kind == "dokumen") {
		return fmt.Sprintf("[Pengirim mengirim %s. Info pembayaran sudah dikirim sebelumnya, jadi ini kemungkinan besar BUKTI TRANSFER. Anda tidak bisa membuka isinya.]", kind)
	}
	if hasText {
		return fmt.Sprintf("[Pesan ini disertai %s yang tidak bisa Anda buka.]", kind)
	}
	return fmt.Sprintf("[Pengirim mengirim %s tanpa teks. Anda tidak bisa membuka isinya.]", kind)
}

func (b *Bridge) handleEvent(ev webhookEvent) {
	if ev.Event != "message" || ev.Payload == nil {
		return
	}
	// A per-device webhook only carries the CS device, but guard anyway in case
	// the bridge URL is also configured as a global webhook.
	if ev.SessionID != "" && ev.SessionID != b.cfg.GowaDeviceID {
		return
	}
	p := ev.Payload
	id, chatID := str(p, "id"), str(p, "chat_id")
	if ignoredChat(chatID) {
		return
	}
	now := b.now()
	if id != "" && !b.markSeen(id, now) {
		return
	}
	s := b.settings.Get()
	text := strings.TrimSpace(str(p, "body"))
	fromMe, _ := p["is_from_me"].(bool)

	if fromMe {
		// Typed by a person on the CS phone (messages sent through the API do
		// not come back as events, and our own ids are filtered anyway).
		if b.isOwnSent(id) {
			return
		}
		ref := b.leadFor(chatID)
		// A chat with an admin number is not a lead: pause it, record nothing.
		ref.skipCRM = isAdmin(s, normalizePhone(chatID))
		b.emit(ref, "message.out", eventIDFor("out", id), messageTime(p, now),
			map[string]any{"wa_message_id": id, "body": text, "media_type": mediaKind(p), "sender": "admin"})
		b.pause(ref, now.Add(s.PauseDuration()), "admin membalas dari HP")
		b.dropPending(chatID)
		b.log.Info("admin took over chat; AI paused", "chat", chatID, "for", s.PauseDuration())
		return
	}

	from := str(p, "from")
	phone := ""
	if strings.HasSuffix(from, "@s.whatsapp.net") {
		phone = normalizePhone(from)
	} else if strings.HasSuffix(chatID, "@s.whatsapp.net") {
		phone = normalizePhone(chatID)
	}
	b.store.RememberPhone(phone, chatID)

	admin := phone != "" && isAdmin(s, phone)
	if admin {
		if m := adminCmdRe.FindStringSubmatch(text); m != nil {
			b.wg.Add(1)
			go func() { defer b.wg.Done(); b.adminCommand(chatID, strings.ToUpper(m[1]), normalizePhone(m[2])) }()
			return
		}
	}
	name := str(p, "sender_display_name")
	if name == "" {
		name = str(p, "from_name")
	}
	kind := mediaKind(p)
	paymentSent := b.store.PaymentSentRecently(chatID, now)

	// Every lead message goes to the Dashboard CRM, also while the AI is off
	// or paused. Admin numbers writing to the CS number are not leads.
	if !admin {
		ref := leadRef{chatID: chatID, phone: phone, name: name}
		at := messageTime(p, now)
		b.emit(ref, "message.in", eventIDFor("in", id), at,
			map[string]any{"wa_message_id": id, "body": text, "media_type": kind})
		if paymentSent && (kind == "gambar" || kind == "dokumen") {
			b.emit(ref, "proof.received", eventIDFor("proof", id), at, map[string]any{"media_type": kind})
		}
	}

	if !s.Enabled {
		b.log.Info("CS AI disabled in settings; message left for admins", "chat", chatID)
		return
	}
	if note := mediaNote(kind, text != "", paymentSent); note != "" {
		text = strings.TrimSpace(text + "\n" + note)
	}
	if text == "" {
		return
	}
	if _, paused := b.store.PausedUntil(chatID, now); paused {
		b.log.Info("chat paused, AI stays silent", "chat", chatID)
		return
	}
	b.enqueue(chatID, phone, name, text, now, s)
}

func (b *Bridge) markSeen(id string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, t := range b.seen {
		if now.Sub(t) > time.Hour {
			delete(b.seen, k)
		}
	}
	if _, dup := b.seen[id]; dup {
		return false
	}
	b.seen[id] = now
	return true
}

func (b *Bridge) isOwnSent(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.sentID[id]
	return ok
}

func isAdmin(s Settings, phone string) bool {
	for _, a := range s.AdminNumbers {
		if a == phone {
			return true
		}
	}
	return false
}

func (b *Bridge) dropPending(chatID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.chats[chatID]; ok {
		c.pending = nil
		b.stopTimer(c)
	}
}

// stopTimer cancels a pending debounce timer; the caller holds b.mu. A timer
// that was stopped before firing never runs its func, so its wg slot is
// released here. One that already fired releases its own slot.
func (b *Bridge) stopTimer(c *chat) {
	if c.timer != nil {
		if c.timer.Stop() {
			b.wg.Done()
		}
		c.timer = nil
	}
}

// enqueue buffers a message and (re)starts the debounce timer so several quick
// messages become one AI turn.
func (b *Bridge) enqueue(chatID, phone, name, text string, now time.Time, s Settings) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.chats[chatID]
	if !ok {
		c = &chat{}
		b.chats[chatID] = c
	}
	c.name, c.phone = name, phone

	// Rate limit per chat within a sliding hour.
	kept := c.hits[:0]
	for _, t := range c.hits {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	c.hits = kept
	if s.RateLimitPerHour > 0 && len(c.hits) >= s.RateLimitPerHour {
		if !c.notified {
			c.notified = true
			b.wg.Add(1)
			go func() {
				defer b.wg.Done()
				if s.RateLimitMessage != "" {
					b.sendLead(leadRef{chatID: chatID, phone: phone, name: name}, s.RateLimitMessage)
				}
				b.handoff(chatID, phone, name, text, fmt.Sprintf("batas %d pesan/jam terlampaui", s.RateLimitPerHour))
			}()
		}
		b.log.Warn("rate limit reached, message ignored", "chat", chatID)
		return
	}
	if len(c.hits) == 0 {
		c.notified = false
	}
	c.hits = append(c.hits, now)

	if len(c.pending) == 0 {
		c.firstAt = now
	}
	c.pending = append(c.pending, text)
	b.stopTimer(c)
	wait := s.Debounce()
	if rest := b.cfg.DebounceMaxWait - now.Sub(c.firstAt); rest < wait {
		wait = max(rest, 0)
	}
	b.wg.Add(1)
	var t *time.Timer
	t = time.AfterFunc(wait, func() {
		defer b.wg.Done()
		b.mu.Lock()
		if c.timer != t { // superseded by a newer message
			b.mu.Unlock()
			return
		}
		c.timer = nil
		b.mu.Unlock()
		b.flush(chatID, c)
	})
	c.timer = t
}

func (b *Bridge) flush(chatID string, c *chat) {
	c.work.Lock()
	defer c.work.Unlock()

	b.mu.Lock()
	texts := c.pending
	c.pending = nil
	name, phone := c.name, c.phone
	b.mu.Unlock()
	if len(texts) == 0 {
		return
	}
	now := b.now()
	if _, paused := b.store.PausedUntil(chatID, now); paused {
		return
	}
	s := b.settings.Get()
	if !s.Enabled {
		return
	}
	userText := truncate(strings.Join(texts, "\n"), b.cfg.MaxMessageChars)

	ctx, cancel := context.WithTimeout(b.ctx, b.cfg.HermesTimeout+10*time.Second)
	defer cancel()
	if s.Typing {
		b.gowa.Typing(ctx, chatID, true)
		defer b.gowa.Typing(context.WithoutCancel(ctx), chatID, false)
	}

	system := buildSystemPrompt(s, name, now, b.store.PaymentSentRecently(chatID, now))
	reply, err := b.agent.Reply(ctx, b.sessionID(chatID, phone), system, userText)
	if err != nil {
		b.log.Error("hermes failed", "chat", chatID, "err", err)
		b.sendLead(leadRef{chatID: chatID, phone: phone, name: name}, s.FallbackMessage)
		b.handoff(chatID, phone, name, userText, "AI tidak tersedia: "+truncate(err.Error(), 120))
		return
	}

	out := renderReply(reply, s)
	for _, w := range out.Warnings {
		b.log.Warn("reply rendering", "chat", chatID, "detail", w)
	}
	if out.Handoff && out.Text == "" {
		out.Text = s.HandoffMessage
	}
	if out.Text == "" && len(out.Images) == 0 {
		b.log.Warn("hermes returned empty reply", "chat", chatID)
		out.Text = s.FallbackMessage
		out.Handoff, out.Reason = true, "AI tidak memberi jawaban"
	}
	ref := leadRef{chatID: chatID, phone: phone, name: name}
	if out.Text != "" {
		b.sendLead(ref, out.Text)
	}
	for _, img := range out.Images {
		b.sendImage(ref, img)
	}
	if out.FormSent {
		b.emit(ref, "form.sent", "", b.now(), map[string]any{})
	}
	if out.PaymentSent {
		b.store.MarkPaymentSent(chatID, b.now())
		b.emit(ref, "payment_info.sent", "", b.now(),
			map[string]any{"paket_kode": out.PaymentPaket, "nominal_text": out.PaymentNominal})
	}
	if out.Handoff {
		b.handoff(chatID, phone, name, userText, out.Reason)
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func (b *Bridge) sessionID(chatID, phone string) string {
	key := phone
	if key == "" {
		key = chatID
	}
	var sb strings.Builder
	for _, r := range key {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return b.cfg.SessionPrefix + sb.String()
}

// parseHandoff strips the [[HANDOFF: reason]] marker from an AI reply.
func parseHandoff(reply string) (clean, reason string, handoff bool) {
	m := handoffRe.FindStringSubmatch(reply)
	if m == nil {
		return strings.TrimSpace(reply), "", false
	}
	reason = strings.TrimSpace(m[1])
	if reason == "" {
		reason = "AI meminta bantuan admin"
	}
	return strings.TrimSpace(handoffRe.ReplaceAllString(reply, "")), reason, true
}

var boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

// toWhatsAppMarkdown converts common Markdown from LLMs to WhatsApp formatting.
func toWhatsAppMarkdown(s string) string {
	s = boldRe.ReplaceAllString(s, "*$1*")
	return headingRe.ReplaceAllString(s, "*$1*")
}

func (b *Bridge) handoff(chatID, phone, name, lastText, reason string) {
	s := b.settings.Get()
	until := b.now().Add(s.PauseDuration())
	ref := leadRef{chatID: chatID, phone: phone, name: name}
	contact := chatID
	cmdTarget := chatID
	if phone != "" {
		contact = "https://wa.me/" + phone
		cmdTarget = phone
	}
	// The marker may carry "reason | needs summary" for the admin.
	summary := ""
	if r, sum, ok := strings.Cut(reason, "|"); ok {
		reason, summary = strings.TrimSpace(r), strings.TrimSpace(sum)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔔 *CS AI butuh admin*\nDari: %s\nKontak: %s\nAlasan: %s\n", orDash(name), contact, orDash(reason))
	if summary != "" {
		fmt.Fprintf(&sb, "Ringkasan: %s\n", summary)
	}
	fmt.Fprintf(&sb, "\nPesan terakhir:\n%s\n\nAI diam di chat ini sampai %s.\nBalas pengirim dari HP nomor CS, atau kirim \"AI ON %s\" ke nomor CS untuk mengaktifkan AI lagi.",
		truncate(lastText, 500), until.Format("02 Jan 15:04"), cmdTarget)
	msg := sb.String()
	b.emit(ref, "handoff", "", b.now(), map[string]any{"reason": reason, "summary": summary})
	b.pause(ref, until, reason)
	if len(s.AdminNumbers) == 0 {
		b.log.Warn("handoff without admin numbers; set them at /cs-ai", "chat", chatID)
	}
	for _, admin := range s.AdminNumbers {
		b.send(admin+"@s.whatsapp.net", msg)
	}
	b.log.Info("handoff to admin", "chat", chatID, "reason", reason)
}

func (b *Bridge) adminCommand(adminChat, cmd, target string) {
	now := b.now()
	pause := b.settings.Get().PauseDuration()
	switch cmd {
	case "STATUS":
		pauses := b.store.ActivePauses(now)
		if len(pauses) == 0 {
			b.send(adminChat, "AI aktif di semua chat. Tidak ada chat yang sedang dijeda.")
			return
		}
		var sb strings.Builder
		sb.WriteString("Chat yang AI-nya sedang dijeda:\n")
		for chatID, p := range pauses {
			fmt.Fprintf(&sb, "- %s sampai %s (%s)\n", chatID, p.Until.Format("02 Jan 15:04"), p.Reason)
		}
		b.send(adminChat, strings.TrimSpace(sb.String()))
	case "ON", "OFF":
		if target == "" {
			b.send(adminChat, "Format: AI ON 628xxx atau AI OFF 628xxx")
			return
		}
		chatID := b.store.ChatForPhone(target)
		ref := b.leadFor(chatID)
		if ref.phone == "" {
			ref.phone = target
		}
		if cmd == "ON" {
			_ = b.store.ClearPause(chatID)
			b.emit(ref, "ai.resumed", "", now, map[string]any{"reason": "diaktifkan oleh admin"})
			b.send(adminChat, fmt.Sprintf("AI diaktifkan lagi untuk %s.", target))
		} else {
			b.pause(ref, now.Add(pause), "dimatikan oleh admin")
			b.dropPending(chatID)
			b.send(adminChat, fmt.Sprintf("AI dijeda untuk %s sampai %s.", target, now.Add(pause).Format("02 Jan 15:04")))
		}
	}
}

func (b *Bridge) rememberSent(id string) {
	if id == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for k, t := range b.sentID {
		if now.Sub(t) > time.Hour {
			delete(b.sentID, k)
		}
	}
	b.sentID[id] = now
}

// send delivers a text and returns its WhatsApp id ("" on failure).
func (b *Bridge) send(chatID, text string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(b.ctx), 30*time.Second)
	defer cancel()
	id, err := b.gowa.SendText(ctx, chatID, text)
	if err != nil {
		b.log.Error("send failed", "chat", chatID, "err", err)
		return ""
	}
	b.rememberSent(id)
	return id
}

// sendLead sends an AI text to a lead and records it in the Dashboard CRM.
func (b *Bridge) sendLead(ref leadRef, text string) {
	id := b.send(ref.chatID, text)
	if id == "" {
		return // not delivered, nothing to record
	}
	b.emit(ref, "message.out", eventIDFor("out", id), b.now(),
		map[string]any{"wa_message_id": id, "body": text, "sender": "ai"})
}

func (b *Bridge) sendImage(ref leadRef, m MediaItem) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(b.ctx), 60*time.Second)
	defer cancel()
	id, err := b.gowa.SendImage(ctx, ref.chatID, m.Caption, b.settings.MediaPath(m), m.ContentType)
	if err != nil {
		b.log.Error("send image failed", "chat", ref.chatID, "image", m.Code, "err", err)
		return
	}
	b.rememberSent(id)
	b.emit(ref, "message.out", eventIDFor("out", id), b.now(),
		map[string]any{"wa_message_id": id, "body": m.Caption, "media_type": "gambar", "sender": "ai"})
}

// pause silences the AI in a chat and tells the Dashboard CRM.
func (b *Bridge) pause(ref leadRef, until time.Time, reason string) {
	if err := b.store.SetPause(ref.chatID, until, reason); err != nil {
		b.log.Error("save pause", "err", err)
	}
	b.emit(ref, "ai.paused", "", b.now(), map[string]any{"until": until.UTC().Format(time.RFC3339), "reason": reason})
}

// eventIDFor derives a stable event id from a WhatsApp message id, so a
// message is never recorded twice; without an id a random one is used.
func eventIDFor(kind, waMessageID string) string {
	if waMessageID == "" {
		return ""
	}
	return "evt_" + kind + "_" + truncate(waMessageID, 100)
}
