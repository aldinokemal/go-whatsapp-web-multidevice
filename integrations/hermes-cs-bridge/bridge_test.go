package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const secret = "test-secret-0123456789"

type sent struct{ to, text string }

type fakeSender struct {
	mu     sync.Mutex
	msgs   []sent
	typing int
	n      int
}

func (f *fakeSender) SendText(_ context.Context, phone, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.msgs = append(f.msgs, sent{phone, text})
	return "OUT" + string(rune('A'+f.n)), nil
}
func (f *fakeSender) SendImage(_ context.Context, phone, caption, path, ctype string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.msgs = append(f.msgs, sent{phone, "IMAGE:" + filepath.Base(path) + "|" + ctype + "|" + caption})
	return "IMG" + string(rune('A'+f.n)), nil
}
func (f *fakeSender) Typing(context.Context, string, bool) { f.mu.Lock(); f.typing++; f.mu.Unlock() }
func (f *fakeSender) all() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.msgs...)
}

type call struct{ session, system, user string }

type fakeAgent struct {
	mu    sync.Mutex
	calls []call
	reply string
	err   error
}

func (a *fakeAgent) Reply(_ context.Context, session, system, user string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, call{session, system, user})
	return a.reply, a.err
}
func (a *fakeAgent) all() []call {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]call(nil), a.calls...)
}

func testConfig() Config {
	return Config{
		WebhookSecret: secret, GowaDeviceID: "cs-ai", SessionPrefix: "wa-cs-",
		SystemPrompt: "SYSTEM", MaxMessageChars: 3000, AdminNumbers: []string{"628111"},
		PauseDuration: time.Hour, Debounce: 30 * time.Millisecond, DebounceMaxWait: 200 * time.Millisecond,
		RateLimitPerHr: 30, HermesTimeout: time.Second, Typing: true,
		FallbackMessage: "FALLBACK", HandoffMessage: "HANDOFF_DEFAULT", RateLimitMessage: "SLOW_DOWN",
	}
}

func newTestBridge(t *testing.T, cfg Config, agent *fakeAgent) (*Bridge, *fakeSender, *Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := LoadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings(filepath.Join(dir, "settings.json"), filepath.Join(dir, "media"), defaultSettings(cfg))
	if err != nil {
		t.Fatal(err)
	}
	snd := &fakeSender{}
	b := NewBridge(context.Background(), cfg, snd, agent, store, settings, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return b, snd, store
}

func sign(body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func post(t *testing.T, b *Bridge, ev map[string]any) int {
	t.Helper()
	body, _ := json.Marshal(ev)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(body))
	rec := httptest.NewRecorder()
	b.Routes().ServeHTTP(rec, req)
	return rec.Code
}

func msg(id, from, body string, fromMe bool) map[string]any {
	return map[string]any{"event": "message", "session_id": "cs-ai", "payload": map[string]any{
		"id": id, "chat_id": from, "from": from, "body": body, "is_from_me": fromMe,
		"sender_display_name": "Fulan",
	}}
}

const user = "6281234@s.whatsapp.net"

func TestRejectsBadSignature(t *testing.T) {
	b, _, _ := newTestBridge(t, testConfig(), &fakeAgent{reply: "x"})
	body := []byte(`{"event":"message"}`)
	for _, h := range []string{"", "sha256=00", "md5=abc", sign([]byte("other"))} {
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", h)
		rec := httptest.NewRecorder()
		b.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header %q: got %d, want 401", h, rec.Code)
		}
	}
}

func TestReplyAndDebounce(t *testing.T) {
	agent := &fakeAgent{reply: "Wa'alaikumussalam, **silakan**."}
	b, snd, _ := newTestBridge(t, testConfig(), agent)
	if code := post(t, b, msg("M1", user, "Assalamu'alaikum", false)); code != 200 {
		t.Fatalf("status %d", code)
	}
	post(t, b, msg("M2", user, "mau tanya program", false))
	post(t, b, msg("M2", user, "mau tanya program", false)) // webhook retry: duplicate id
	b.Wait()

	calls := agent.all()
	if len(calls) != 1 {
		t.Fatalf("want 1 combined AI call, got %d", len(calls))
	}
	if calls[0].user != "Assalamu'alaikum\nmau tanya program" {
		t.Fatalf("combined text = %q", calls[0].user)
	}
	if calls[0].session != "wa-cs-6281234" {
		t.Fatalf("session = %q", calls[0].session)
	}
	if !strings.HasPrefix(calls[0].system, "SYSTEM") || !strings.Contains(calls[0].system, "Fulan") {
		t.Fatalf("system prompt missing parts: %q", calls[0].system)
	}
	got := snd.all()
	if len(got) != 1 || got[0].to != user || got[0].text != "Wa'alaikumussalam, *silakan*." {
		t.Fatalf("sent = %+v", got)
	}
	if snd.typing != 2 {
		t.Fatalf("typing calls = %d, want start+stop", snd.typing)
	}
}

func TestIgnoresGroupsStatusOtherDevicesAndNonMessages(t *testing.T) {
	agent := &fakeAgent{reply: "x"}
	b, snd, _ := newTestBridge(t, testConfig(), agent)
	post(t, b, msg("G1", "1203630@g.us", "halo grup", false))
	post(t, b, msg("S1", "status@broadcast", "status", false))
	other := msg("O1", user, "dari device lain", false)
	other["session_id"] = "keuangan"
	post(t, b, other)
	post(t, b, map[string]any{"event": "message.ack", "payload": map[string]any{"chat_id": user}})
	post(t, b, msg("E1", user, "", false)) // no text, no media
	b.Wait()
	if len(agent.all()) != 0 || len(snd.all()) != 0 {
		t.Fatalf("expected no activity, got calls=%d sent=%d", len(agent.all()), len(snd.all()))
	}
}

func TestMediaWithoutTextReachesAI(t *testing.T) {
	agent := &fakeAgent{reply: "ok"}
	b, _, _ := newTestBridge(t, testConfig(), agent)
	ev := msg("IMG", user, "", false)
	ev["payload"].(map[string]any)["image"] = "statics/media/x.jpg"
	post(t, b, ev)
	b.Wait()
	if c := agent.all(); len(c) != 1 || !strings.Contains(c[0].user, "gambar") {
		t.Fatalf("calls = %+v", c)
	}
}

func TestAdminReplyFromPhonePausesAI(t *testing.T) {
	agent := &fakeAgent{reply: "x"}
	b, snd, store := newTestBridge(t, testConfig(), agent)
	post(t, b, msg("A1", user, "dibalas admin", true)) // typed on the CS phone
	post(t, b, msg("U1", user, "terima kasih", false))
	b.Wait()
	if len(agent.all()) != 0 || len(snd.all()) != 0 {
		t.Fatalf("AI must stay silent after admin reply")
	}
	if _, ok := store.PausedUntil(user, time.Now()); !ok {
		t.Fatal("chat not paused")
	}
}

func TestAdminReplyCancelsPendingTurn(t *testing.T) {
	agent := &fakeAgent{reply: "x"}
	cfg := testConfig()
	cfg.Debounce = 150 * time.Millisecond
	b, _, _ := newTestBridge(t, cfg, agent)
	post(t, b, msg("U1", user, "pertanyaan", false))
	post(t, b, msg("A1", user, "saya bantu", true)) // admin answers within the debounce window
	b.Wait()
	if len(agent.all()) != 0 {
		t.Fatalf("pending AI turn should be cancelled")
	}
}

func TestHandoffMarker(t *testing.T) {
	agent := &fakeAgent{reply: "Baik, admin akan membalas.\n[[HANDOFF: tanya tagihan pribadi]]"}
	b, snd, store := newTestBridge(t, testConfig(), agent)
	post(t, b, msg("H1", user, "tagihan saya berapa?", false))
	b.Wait()
	got := snd.all()
	if len(got) != 2 {
		t.Fatalf("want reply + admin notice, got %+v", got)
	}
	if got[0].to != user || got[0].text != "Baik, admin akan membalas." {
		t.Fatalf("user reply = %+v", got[0])
	}
	if got[1].to != "628111@s.whatsapp.net" || !strings.Contains(got[1].text, "tanya tagihan pribadi") ||
		!strings.Contains(got[1].text, "https://wa.me/6281234") || !strings.Contains(got[1].text, "tagihan saya berapa?") {
		t.Fatalf("admin notice = %+v", got[1])
	}
	if _, ok := store.PausedUntil(user, time.Now()); !ok {
		t.Fatal("chat should be paused after handoff")
	}
	// Next message is not answered by AI.
	post(t, b, msg("H2", user, "halo?", false))
	b.Wait()
	if len(agent.all()) != 1 {
		t.Fatal("AI answered a paused chat")
	}
}

func TestAgentErrorFallsBackAndNotifiesAdmin(t *testing.T) {
	agent := &fakeAgent{err: errors.New("connection refused")}
	b, snd, _ := newTestBridge(t, testConfig(), agent)
	post(t, b, msg("F1", user, "halo", false))
	b.Wait()
	got := snd.all()
	if len(got) != 2 || got[0].text != "FALLBACK" || got[1].to != "628111@s.whatsapp.net" ||
		!strings.Contains(got[1].text, "AI tidak tersedia") {
		t.Fatalf("sent = %+v", got)
	}
}

func TestAdminCommands(t *testing.T) {
	agent := &fakeAgent{reply: "jawaban"}
	b, snd, store := newTestBridge(t, testConfig(), agent)
	admin := "628111@s.whatsapp.net"

	post(t, b, msg("U1", user, "pertanyaan", true)) // admin took over → paused
	b.Wait()
	post(t, b, msg("C1", admin, "AI ON 0812-34", false)) // 0812-34 → 6281234
	b.Wait()
	if _, ok := store.PausedUntil(user, time.Now()); ok {
		t.Fatal("AI ON should clear the pause")
	}
	post(t, b, msg("C2", admin, "ai off 6281234", false))
	b.Wait()
	if _, ok := store.PausedUntil(user, time.Now()); !ok {
		t.Fatal("AI OFF should pause the chat")
	}
	post(t, b, msg("C3", admin, "AI STATUS", false))
	b.Wait()
	if len(agent.all()) != 0 {
		t.Fatal("admin commands must not reach the AI")
	}
	got := snd.all()
	if len(got) != 3 || !strings.Contains(got[2].text, user) {
		t.Fatalf("admin replies = %+v", got)
	}

	// The same text from a non-admin is a normal question.
	post(t, b, msg("N1", "6289999@s.whatsapp.net", "AI ON 6281234", false))
	b.Wait()
	if len(agent.all()) != 1 {
		t.Fatal("non-admin command text should go to the AI")
	}
}

func TestRateLimit(t *testing.T) {
	agent := &fakeAgent{reply: "ok"}
	cfg := testConfig()
	cfg.RateLimitPerHr = 2
	b, snd, _ := newTestBridge(t, cfg, agent)
	post(t, b, msg("R0", user, "a", false))
	post(t, b, msg("R1", user, "b", false))
	b.Wait() // first burst answered as one turn
	post(t, b, msg("R2", user, "c", false))
	post(t, b, msg("R3", user, "d", false))
	b.Wait()
	c := agent.all()
	if len(c) != 1 || c[0].user != "a\nb" {
		t.Fatalf("calls = %+v", c)
	}
	notices, adminNotices := 0, 0
	for _, m := range snd.all() {
		if m.text == "SLOW_DOWN" {
			notices++
		}
		if m.to == "628111@s.whatsapp.net" && strings.Contains(m.text, "batas 2 pesan/jam") {
			adminNotices++
		}
	}
	if notices != 1 || adminNotices != 1 {
		t.Fatalf("want one lead notice and one admin notice, got %d and %d", notices, adminNotices)
	}
}

// By default the lead gets no technical "too many messages" text; only the admin is told.
func TestRateLimitDefaultIsSilentToLead(t *testing.T) {
	agent := &fakeAgent{reply: "ok"}
	cfg := testConfig()
	cfg.RateLimitPerHr, cfg.RateLimitMessage = 1, ""
	b, snd, _ := newTestBridge(t, cfg, agent)
	post(t, b, msg("Q1", user, "a", false))
	post(t, b, msg("Q2", user, "b", false))
	b.Wait()
	for _, m := range snd.all() {
		if m.to == user && m.text != "ok" {
			t.Fatalf("lead received unexpected text %q", m.text)
		}
	}
	if got := snd.all(); got[len(got)-1].to != "628111@s.whatsapp.net" {
		t.Fatalf("admin not notified: %+v", got)
	}
}

func TestHandoffSummaryIsSeparated(t *testing.T) {
	agent := &fakeAgent{reply: "Baik, Kak.\n[[HANDOFF: siap daftar | akhwat, pemula, malam, 1× sepekan]]"}
	b, snd, _ := newTestBridge(t, testConfig(), agent)
	post(t, b, msg("S1", user, "saya mau daftar", false))
	b.Wait()
	got := snd.all()
	if len(got) != 2 || !strings.Contains(got[1].text, "Alasan: siap daftar\n") ||
		!strings.Contains(got[1].text, "Ringkasan: akhwat, pemula, malam, 1× sepekan") {
		t.Fatalf("admin notice = %+v", got)
	}
	if got[0].text != "Baik, Kak." {
		t.Fatalf("lead reply = %q", got[0].text)
	}
}

// Guards the embedded CS instructions against accidental edits that would
// break handoff or drop the verified facts.
func TestEmbeddedPrompt(t *testing.T) {
	cfg := testConfig()
	cfg.SystemPrompt = defaultSystemPrompt
	full := buildSystemPrompt(defaultSettings(cfg), "X", time.Now(), false)
	for _, want := range []string{"[[HANDOFF:", "[[REKENING:", "[[FORM]]", "Rp149.000", "Rp199.000", "LURC", "CLEAR",
		"JANGAN pernah menulis sendiri nomor rekening", "emoji"} {
		if !strings.Contains(full, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if strings.Contains(full, "1110012352") {
		t.Error("the account number must not be in the prompt; it is pasted by the bridge")
	}
	if len(full) > 24000 {
		t.Errorf("prompt is %d bytes; keep it compact, it is sent on every turn", len(full))
	}
}

func TestPauseExpiresAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := LoadStore(path)
	now := time.Now()
	_ = s.SetPause("x@s.whatsapp.net", now.Add(time.Minute), "r")
	s.RememberPhone("62812", "abc@lid")
	s2, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.PausedUntil("x@s.whatsapp.net", now); !ok {
		t.Fatal("pause not persisted")
	}
	if _, ok := s2.PausedUntil("x@s.whatsapp.net", now.Add(2*time.Minute)); ok {
		t.Fatal("pause should expire")
	}
	if s2.ChatForPhone("62812") != "abc@lid" || s2.ChatForPhone("62999") != "62999@s.whatsapp.net" {
		t.Fatal("phone map wrong")
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"+62 812-3456": "628123456", "08123": "628123", "6281@s.whatsapp.net": "6281", "6281:12@s.whatsapp.net": "6281", "": "",
	} {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
	clean, reason, ok := parseHandoff("Teks\n[[handoff]]")
	if !ok || clean != "Teks" || reason == "" {
		t.Errorf("parseHandoff lowercase/no reason: %q %q %v", clean, reason, ok)
	}
	if _, _, ok := parseHandoff("tanpa penanda"); ok {
		t.Error("false positive handoff")
	}
	if got := toWhatsAppMarkdown("## Judul\n**tebal** dan *miring*"); got != "*Judul*\n*tebal* dan *miring*" {
		t.Errorf("markdown = %q", got)
	}
}

// The real HTTP clients against fake GOWA and Hermes servers.
func TestHTTPClients(t *testing.T) {
	var gotHermes struct {
		auth, session string
		body          map[string]any
	}
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		gotHermes.auth, gotHermes.session = r.Header.Get("Authorization"), r.Header.Get("X-Hermes-Session-Id")
		_ = json.NewDecoder(r.Body).Decode(&gotHermes.body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"halo"},"finish_reason":"stop"}]}`))
	}))
	defer hermes.Close()

	var gotGowa struct {
		user, pass, device, path string
		body                     map[string]string
	}
	gowa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotGowa.user, gotGowa.pass, _ = r.BasicAuth()
		gotGowa.device, gotGowa.path = r.Header.Get("X-Device-Id"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotGowa.body)
		_, _ = w.Write([]byte(`{"code":"SUCCESS","results":{"message_id":"3EB0ABC","status":"ok"}}`))
	}))
	defer gowa.Close()

	cfg := testConfig()
	cfg.HermesURL, cfg.HermesAPIKey, cfg.HermesTimeout = hermes.URL, "hk", 5*time.Second
	cfg.GowaURL, cfg.GowaUser, cfg.GowaPass = gowa.URL, "bridge", "pw"

	reply, err := NewHermesClient(cfg).Reply(context.Background(), "wa-cs-1", "SYS", "tanya")
	if err != nil || reply != "halo" {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if gotHermes.auth != "Bearer hk" || gotHermes.session != "wa-cs-1" {
		t.Fatalf("hermes headers: %+v", gotHermes)
	}
	if _, hasModel := gotHermes.body["model"]; hasModel {
		t.Fatal("model must be omitted when HERMES_MODEL is empty")
	}
	msgs := gotHermes.body["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages = %v", msgs)
	}

	id, err := NewGowaClient(cfg).SendText(context.Background(), user, "teks")
	if err != nil || id != "3EB0ABC" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if gotGowa.user != "bridge" || gotGowa.pass != "pw" || gotGowa.device != "cs-ai" ||
		gotGowa.path != "/send/message" || gotGowa.body["phone"] != user || gotGowa.body["message"] != "teks" {
		t.Fatalf("gowa request: %+v", gotGowa)
	}
}

func TestHermesErrorStatuses(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"http500": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", 500) },
		"finish_error": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"partial"},"finish_reason":"error"}]}`))
		},
		"no_choices": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"choices":[]}`)) },
	} {
		srv := httptest.NewServer(h)
		cfg := testConfig()
		cfg.HermesURL, cfg.HermesAPIKey = srv.URL, "k"
		if _, err := NewHermesClient(cfg).Reply(context.Background(), "s", "sys", "u"); err == nil {
			t.Errorf("%s: expected error", name)
		}
		srv.Close()
	}
}

func TestLoadConfigValidation(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "short")
	t.Setenv("GOWA_USER", "u")
	t.Setenv("GOWA_PASS", "p")
	t.Setenv("GOWA_DEVICE_ID", "cs-ai")
	t.Setenv("HERMES_API_KEY", "k")
	t.Setenv("ADMIN_NUMBERS", "0811, +62 822")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "16") {
		t.Fatalf("want short-secret error, got %v", err)
	}
	t.Setenv("WEBHOOK_SECRET", secret)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.AdminNumbers, ",") != "62811,62822" {
		t.Fatalf("admins = %v", cfg.AdminNumbers)
	}
	// Admin numbers can now come from the /cs-ai page, so an empty env is fine.
	t.Setenv("ADMIN_NUMBERS", "")
	if _, err := loadConfig(); err != nil {
		t.Fatalf("empty ADMIN_NUMBERS should be allowed: %v", err)
	}
	t.Setenv("ADMIN_TOKEN", "short")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "ADMIN_TOKEN") {
		t.Fatalf("short ADMIN_TOKEN should fail, got %v", err)
	}
}
