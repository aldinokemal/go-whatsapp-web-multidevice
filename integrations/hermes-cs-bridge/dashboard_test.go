package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSink struct {
	mu  sync.Mutex
	evs []DashboardEvent
}

func (f *fakeSink) Emit(ev DashboardEvent) { f.mu.Lock(); f.evs = append(f.evs, ev); f.mu.Unlock() }
func (f *fakeSink) types() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.evs {
		out = append(out, e.Type)
	}
	return out
}
func (f *fakeSink) find(typ string) []DashboardEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []DashboardEvent
	for _, e := range f.evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestLeadJourneyReachesDashboard(t *testing.T) {
	agent := &fakeAgent{reply: "Silakan isi dulu ya, Kak.\n[[FORM]]"}
	b, _, _ := newTestBridge(t, testConfig(), agent)
	sink := &fakeSink{}
	b.events = sink

	ev := msg("M1", user, "Assalamu'alaikum, mau daftar tahsin", false)
	ev["payload"].(map[string]any)["timestamp"] = "2026-10-24T03:12:45Z"
	post(t, b, ev)
	b.Wait()

	in := sink.find("message.in")
	if len(in) != 1 {
		t.Fatalf("message.in events = %+v", sink.types())
	}
	if in[0].EventID != "evt_in_M1" || in[0].ChatID != user || in[0].Phone != "6281234" || in[0].WaName != "Fulan" ||
		in[0].OccurredAt != "2026-10-24T03:12:45Z" || in[0].Data["body"] != "Assalamu'alaikum, mau daftar tahsin" {
		t.Fatalf("message.in = %+v", in[0])
	}
	out := sink.find("message.out")
	if len(out) != 1 || out[0].Data["sender"] != "ai" || !strings.Contains(out[0].Data["body"].(string), "Nama:") ||
		!strings.HasPrefix(out[0].EventID, "evt_out_") {
		t.Fatalf("message.out = %+v", out)
	}
	if len(sink.find("form.sent")) != 1 {
		t.Fatalf("form.sent missing: %v", sink.types())
	}

	// Payment info: package code and price come from settings.
	agent.reply = "Baik, Kak.\n[[REKENING:1x]]"
	post(t, b, msg("M2", user, "Nama: Fulanah ... 1x sepekan", false))
	b.Wait()
	pay := sink.find("payment_info.sent")
	if len(pay) != 1 || pay[0].Data["paket_kode"] != "1x" || pay[0].Data["nominal_text"] != "Rp149.000" {
		t.Fatalf("payment_info.sent = %+v", pay)
	}

	// An image after payment info is a likely transfer proof.
	agent.reply = "Terima kasih, Kak.\n[[HANDOFF: bukti transfer | 1x sepekan]]"
	img := msg("M3", user, "", false)
	img["payload"].(map[string]any)["image"] = "statics/media/proof.jpg"
	post(t, b, img)
	b.Wait()
	proof := sink.find("proof.received")
	if len(proof) != 1 || proof[0].Data["media_type"] != "gambar" {
		t.Fatalf("proof.received = %+v (all %v)", proof, sink.types())
	}
	h := sink.find("handoff")
	if len(h) != 1 || h[0].Data["reason"] != "bukti transfer" || h[0].Data["summary"] != "1x sepekan" {
		t.Fatalf("handoff = %+v", h)
	}
	if p := sink.find("ai.paused"); len(p) != 1 || p[0].Data["until"] == "" {
		t.Fatalf("ai.paused = %+v", p)
	}
	// While paused the lead's messages are still recorded.
	post(t, b, msg("M4", user, "sudah ya kak", false))
	b.Wait()
	if n := len(sink.find("message.in")); n != 4 {
		t.Fatalf("message.in while paused: got %d of 4", n)
	}
}

func TestAdminPhoneReplyAndCommandsReachDashboard(t *testing.T) {
	b, _, _ := newTestBridge(t, testConfig(), &fakeAgent{reply: "ok"})
	sink := &fakeSink{}
	b.events = sink

	post(t, b, msg("A1", user, "Baik kak, saya bantu", true))
	b.Wait()
	out := sink.find("message.out")
	if len(out) != 1 || out[0].Data["sender"] != "admin" || out[0].EventID != "evt_out_A1" {
		t.Fatalf("admin reply = %+v", out)
	}
	if len(sink.find("ai.paused")) != 1 {
		t.Fatalf("want ai.paused, got %v", sink.types())
	}

	// Messages from admin numbers are not leads.
	post(t, b, msg("A2", "628111@s.whatsapp.net", "AI ON 6281234", false))
	b.Wait()
	if n := len(sink.find("message.in")); n != 0 {
		t.Fatalf("admin message recorded as lead: %v", sink.types())
	}
	if r := sink.find("ai.resumed"); len(r) != 1 || r[0].ChatID != user {
		t.Fatalf("ai.resumed = %+v", r)
	}
	// Typing to an admin chat from the CS phone records nothing.
	before := len(sink.types())
	post(t, b, msg("A3", "628111@s.whatsapp.net", "noted", true))
	b.Wait()
	if len(sink.types()) != before {
		t.Fatalf("admin chat recorded: %v", sink.types())
	}
}

func TestNoDashboardWhenNotConfigured(t *testing.T) {
	o, err := NewDashboardOutbox(Config{DashboardURL: "http://x", DashboardOutboxDir: t.TempDir()}, slog.Default())
	if err != nil || o != nil {
		t.Fatalf("outbox without token = %v, %v", o, err)
	}
	// The bridge works without a sink.
	b, snd, _ := newTestBridge(t, testConfig(), &fakeAgent{reply: "hai"})
	post(t, b, msg("N1", user, "halo", false))
	b.Wait()
	if len(snd.all()) != 1 {
		t.Fatal("reply missing")
	}
}

const testToken = "0123456789abcdef0123456789abcdef"

func testOutbox(t *testing.T, url, dir string) *DashboardOutbox {
	t.Helper()
	o, err := NewDashboardOutbox(Config{DashboardURL: url, DashboardToken: testToken, DashboardOutboxDir: dir},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || o == nil {
		t.Fatalf("outbox: %v", err)
	}
	return o
}

func TestOutboxSendsBatchWithToken(t *testing.T) {
	var got []DashboardEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/integrations/cs-ai/events" || r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct{ Events []DashboardEvent }
		_ = json.NewDecoder(r.Body).Decode(&req)
		got = append(got, req.Events...)
		_, _ = w.Write([]byte(`{"accepted":2,"duplicates":0,"rejected":[{"event_id":"e3","error":"type tidak dikenal"}]}`))
	}))
	defer srv.Close()

	o := testOutbox(t, srv.URL, t.TempDir())
	for _, id := range []string{"e1", "e2", "e3"} {
		o.Emit(DashboardEvent{EventID: id, Type: "message.in", ChatID: user})
	}
	n, err := o.flushOnce(context.Background())
	if err != nil || n != 3 || len(got) != 3 || o.Pending() != 0 {
		t.Fatalf("flush n=%d err=%v got=%d pending=%d", n, err, len(got), o.Pending())
	}
	if got[0].OccurredAt == "" || got[0].Data == nil {
		t.Fatalf("defaults not filled: %+v", got[0])
	}
}

func TestOutboxKeepsEventsWhileDashboardIsDown(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := int(status.Load()); s != http.StatusOK {
			w.WriteHeader(s)
			return
		}
		var req struct{ Events []DashboardEvent }
		_ = json.NewDecoder(r.Body).Decode(&req)
		received.Add(int32(len(req.Events)))
		_, _ = w.Write([]byte(`{"accepted":1}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	o := testOutbox(t, srv.URL, dir)
	o.Emit(DashboardEvent{EventID: "k1", Type: "message.in", ChatID: user})
	o.Emit(DashboardEvent{EventID: "k2", Type: "form.sent", ChatID: user})
	for _, s := range []int{http.StatusServiceUnavailable, http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden} {
		status.Store(int32(s))
		if _, err := o.flushOnce(context.Background()); err == nil || o.Pending() != 2 {
			t.Fatalf("HTTP %d: err=%v pending=%d", s, err, o.Pending())
		}
	}

	// A restart of the bridge reloads the queue from disk.
	o2 := testOutbox(t, srv.URL, dir)
	if o2.Pending() != 2 {
		t.Fatalf("after restart pending = %d", o2.Pending())
	}
	status.Store(http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o2.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for o2.Pending() > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if o2.Pending() != 0 || received.Load() != 2 {
		t.Fatalf("pending=%d received=%d", o2.Pending(), received.Load())
	}
	if o3 := testOutbox(t, srv.URL, dir); o3.Pending() != 0 {
		t.Fatal("sent events were not removed from the queue file")
	}
}

func TestOutboxSplitsLargeBatches(t *testing.T) {
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 256<<10 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var req struct{ Events []DashboardEvent }
		_ = json.Unmarshal(body, &req)
		batches = append(batches, len(req.Events))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	o := testOutbox(t, srv.URL, t.TempDir())
	long := strings.Repeat("a", 3900)
	for i := 0; i < 150; i++ {
		o.Emit(DashboardEvent{Type: "message.in", ChatID: user, Data: map[string]any{"body": long}})
	}
	for o.Pending() > 0 {
		if _, err := o.flushOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for _, n := range batches {
		if n > 100 {
			t.Fatalf("batch of %d events", n)
		}
		total += n
	}
	if total != 150 || len(batches) < 3 {
		t.Fatalf("batches = %v", batches)
	}
}
