package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const adminToken = "test-admin-token-0123456789abcdef"

func adminConfig() Config {
	c := testConfig()
	c.AdminToken = adminToken
	return c
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{0, 128, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// adminReq sends a request to the settings API as GOWA's proxy would.
func adminReq(t *testing.T, b *Bridge, method, path string, body io.Reader, ctype string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("X-CS-AI-Token", adminToken)
	req.Header.Set("X-CS-AI-User", "stage")
	req.Header.Set("X-CS-AI-Request", "1")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	b.Routes().ServeHTTP(rec, req)
	return rec
}

func uploadImage(t *testing.T, b *Bridge, code, desc, caption string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("code", code)
	_ = mw.WriteField("description", desc)
	_ = mw.WriteField("caption", caption)
	fw, _ := mw.CreateFormFile("file", "upload.bin")
	_, _ = fw.Write(data)
	_ = mw.Close()
	return adminReq(t, b, http.MethodPost, "/cs-ai/api/media", &buf, mw.FormDataContentType())
}

func TestRenderReplyMarkers(t *testing.T) {
	s := defaultSettings(testConfig())
	s.Media = []MediaItem{{Code: "brosur", File: "brosur.png"}, {Code: "harga", File: "harga.png"}, {Code: "qr", File: "qr.png"}}

	r := renderReply("Siap, Kak 😊\n[[FORM]]", s)
	if !strings.Contains(r.Text, "* Nama:") || !strings.Contains(r.Text, "Syukron 🙏🏻😊") || r.PaymentSent {
		t.Fatalf("form not pasted: %q", r.Text)
	}

	r = renderReply("Berikut info pembayarannya ✅\n[[REKENING:2x]]", s)
	for _, want := range []string{"1110012352", "Ruang Sanad Indonesia", "Bank Muamalat", "*Rp199.000*", "*2× sepekan*"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("payment text lacks %q: %q", want, r.Text)
		}
	}
	if !r.PaymentSent || strings.Contains(r.Text, "{NOMINAL}") {
		t.Fatalf("payment not rendered: %+v", r)
	}

	r = renderReply("[[REKENING:9x]]", s)
	if !strings.Contains(r.Text, "akan dikonfirmasi admin") || len(r.Warnings) == 0 {
		t.Fatalf("unknown package must not invent a price: %+v", r)
	}

	r = renderReply("Ini brosurnya 📖 [[GAMBAR:Brosur]] [[GAMBAR:brosur]] [[GAMBAR:harga]] [[GAMBAR:qr]] [[GAMBAR:nope]] [[TOOL:x]]", s)
	if len(r.Images) != 2 || r.Images[0].Code != "brosur" || r.Images[1].Code != "harga" {
		t.Fatalf("images = %+v", r.Images)
	}
	if strings.Contains(r.Text, "[[") || r.Text != "Ini brosurnya 📖" {
		t.Fatalf("markers left in text: %q", r.Text)
	}

	r = renderReply("Terima kasih, Kak 🙏🏻\n[[HANDOFF: bukti transfer | Fulan, akhwat, 1x]]", s)
	if !r.Handoff || r.Reason != "bukti transfer | Fulan, akhwat, 1x" || r.Text != "Terima kasih, Kak 🙏🏻" {
		t.Fatalf("handoff = %+v", r)
	}
}

func TestClosingFlowPaymentThenTransferProof(t *testing.T) {
	agent := &fakeAgent{reply: "Datanya sudah benar ya, Kak 😊\n[[REKENING:1x]]"}
	b, snd, store := newTestBridge(t, testConfig(), agent)
	post(t, b, msg("P1", user, "iya sudah benar", false))
	b.Wait()
	got := snd.all()
	if len(got) != 1 || !strings.Contains(got[0].text, "1110012352") || !strings.Contains(got[0].text, "Rp149.000") {
		t.Fatalf("payment reply = %+v", got)
	}
	if !store.PaymentSentRecently(user, time.Now()) {
		t.Fatal("payment not recorded")
	}

	// The lead sends a screenshot: the AI is told it is likely a transfer proof.
	agent.reply = "Terima kasih, Kak 🙏🏻 Bukti transfernya kami teruskan ke admin untuk diverifikasi.\n[[HANDOFF: bukti transfer | 1x, Rp149.000]]"
	ev := msg("P2", user, "", false)
	ev["payload"].(map[string]any)["image"] = "statics/media/proof.jpg"
	post(t, b, ev)
	b.Wait()
	calls := agent.all()
	last := calls[len(calls)-1]
	if !strings.Contains(last.user, "BUKTI TRANSFER") || !strings.Contains(last.system, "SUDAH pernah dikirim") {
		t.Fatalf("AI not told about the proof: user=%q", last.user)
	}
	got = snd.all()
	if !strings.Contains(got[len(got)-1].text, "bukti transfer") || got[len(got)-1].to != "628111@s.whatsapp.net" {
		t.Fatalf("admin not notified about the proof: %+v", got)
	}
	if _, paused := store.PausedUntil(user, time.Now()); !paused {
		t.Fatal("chat should be paused for the admin to verify")
	}
}

func TestAdminAPIAuthAndCSRF(t *testing.T) {
	b, _, _ := newTestBridge(t, adminConfig(), &fakeAgent{reply: "x"})

	noToken := adminReq(t, b, http.MethodGet, "/cs-ai/api/settings", nil, "", func(r *http.Request) { r.Header.Del("X-CS-AI-Token") })
	badToken := adminReq(t, b, http.MethodGet, "/cs-ai/api/settings", nil, "", func(r *http.Request) { r.Header.Set("X-CS-AI-Token", "wrong") })
	if noToken.Code != http.StatusForbidden || badToken.Code != http.StatusForbidden {
		t.Fatalf("token check: %d %d", noToken.Code, badToken.Code)
	}
	body := `{"enabled":false}`
	noHeader := adminReq(t, b, http.MethodPut, "/cs-ai/api/settings", strings.NewReader(body), "application/json",
		func(r *http.Request) { r.Header.Del("X-CS-AI-Request") })
	crossSite := adminReq(t, b, http.MethodPut, "/cs-ai/api/settings", strings.NewReader(body), "application/json",
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if noHeader.Code != http.StatusForbidden || crossSite.Code != http.StatusForbidden {
		t.Fatalf("csrf guard: %d %d", noHeader.Code, crossSite.Code)
	}
	if !b.settings.Get().Enabled {
		t.Fatal("rejected requests must not change settings")
	}

	page := adminReq(t, b, http.MethodGet, "/cs-ai/", nil, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Pengaturan CS AI") {
		t.Fatalf("page: %d", page.Code)
	}
}

func TestAdminPageDisabledWithoutToken(t *testing.T) {
	b, _, _ := newTestBridge(t, testConfig(), &fakeAgent{reply: "x"})
	rec := adminReq(t, b, http.MethodGet, "/cs-ai/api/settings", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 when ADMIN_TOKEN is empty, got %d", rec.Code)
	}
}

func TestAdminSettingsUpdateTakesEffect(t *testing.T) {
	agent := &fakeAgent{reply: "jawaban"}
	b, snd, _ := newTestBridge(t, adminConfig(), agent)

	rec := adminReq(t, b, http.MethodGet, "/cs-ai/api/settings", nil, "")
	var s Settings
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	// Invalid: payment text without {NOMINAL}.
	bad := s
	bad.PaymentText = "Transfer ke rekening kami"
	raw, _ := json.Marshal(bad)
	if rec := adminReq(t, b, http.MethodPut, "/cs-ai/api/settings", bytes.NewReader(raw), "application/json"); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "{NOMINAL}") {
		t.Fatalf("invalid settings accepted: %d %s", rec.Code, rec.Body)
	}
	// Unknown fields are rejected so typos do not silently drop settings.
	if rec := adminReq(t, b, http.MethodPut, "/cs-ai/api/settings", strings.NewReader(`{"enabeld":true}`), "application/json"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field accepted: %d", rec.Code)
	}

	// Valid: new price, new admin, AI switched off.
	s.Packages[0].Price = "Rp159.000"
	s.AdminNumbers = []string{"0812-0000-1111"}
	s.Enabled = false
	raw, _ = json.Marshal(s)
	rec = adminReq(t, b, http.MethodPut, "/cs-ai/api/settings", bytes.NewReader(raw), "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", rec.Code, rec.Body)
	}
	cur := b.settings.Get()
	if cur.UpdatedBy != "stage" || cur.AdminNumbers[0] != "6281200001111" || cur.Packages[0].Price != "Rp159.000" {
		t.Fatalf("settings not stored/normalised: %+v", cur)
	}

	post(t, b, msg("D1", user, "halo", false))
	b.Wait()
	if len(agent.all()) != 0 || len(snd.all()) != 0 {
		t.Fatal("AI must stay silent when disabled")
	}
	// Admin commands still work while the AI is off (new admin number).
	post(t, b, msg("D2", "6281200001111@s.whatsapp.net", "AI STATUS", false))
	b.Wait()
	if len(snd.all()) != 1 {
		t.Fatal("admin command should still be answered")
	}

	// Switch back on: the new price is in the prompt the AI receives.
	cur.Enabled = true
	raw, _ = json.Marshal(cur)
	adminReq(t, b, http.MethodPut, "/cs-ai/api/settings", bytes.NewReader(raw), "application/json")
	post(t, b, msg("D3", user, "harganya?", false))
	b.Wait()
	if c := agent.all(); len(c) != 1 || !strings.Contains(c[0].system, "Rp159.000") {
		t.Fatalf("new price not in prompt: %+v", c)
	}

	preview := adminReq(t, b, http.MethodGet, "/cs-ai/api/prompt-preview", nil, "")
	if !strings.Contains(preview.Body.String(), "Rp159.000") || !strings.Contains(preview.Body.String(), "ATURAN TETAP") {
		t.Fatal("prompt preview incomplete")
	}
}

func TestMediaUploadAndSend(t *testing.T) {
	agent := &fakeAgent{reply: "Ini brosur kelasnya, Kak 📖\n[[GAMBAR:brosur]]"}
	b, snd, _ := newTestBridge(t, adminConfig(), agent)

	if rec := uploadImage(t, b, "brosur", "", "", tinyPNG(t)); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing description accepted: %d", rec.Code)
	}
	if rec := uploadImage(t, b, "Bad Code!", "x", "", tinyPNG(t)); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad code accepted: %d", rec.Code)
	}
	if rec := uploadImage(t, b, "teks", "x", "", []byte("just some text, not an image")); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-image accepted: %d", rec.Code)
	}
	if rec := uploadImage(t, b, "besar", "x", "", append(tinyPNG(t), make([]byte, maxMediaBytes)...)); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized file accepted: %d", rec.Code)
	}
	rec := uploadImage(t, b, "brosur", "Saat minta info program", "Info Kelas Tahsin 📖", tinyPNG(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body)
	}
	if prev := adminReq(t, b, http.MethodGet, "/cs-ai/media/brosur", nil, ""); prev.Code != http.StatusOK || prev.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("preview: %d %q", prev.Code, prev.Header().Get("Content-Type"))
	}

	post(t, b, msg("M1", user, "minta brosurnya", false))
	b.Wait()
	if c := agent.all(); !strings.Contains(c[0].system, "kode `brosur`: Saat minta info program") {
		t.Fatal("gallery not described to the AI")
	}
	got := snd.all()
	if len(got) != 2 || got[0].text != "Ini brosur kelasnya, Kak 📖" || got[1].text != "IMAGE:brosur.png|image/png|Info Kelas Tahsin 📖" {
		t.Fatalf("sent = %+v", got)
	}

	if rec := adminReq(t, b, http.MethodDelete, "/cs-ai/api/media/brosur", nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, ok := b.settings.Get().MediaByCode("brosur"); ok {
		t.Fatal("image still listed after delete")
	}
}

func TestAdminClearPause(t *testing.T) {
	b, _, store := newTestBridge(t, adminConfig(), &fakeAgent{reply: "x"})
	_ = store.SetPause(user, time.Now().Add(time.Hour), "uji")
	rec := adminReq(t, b, http.MethodGet, "/cs-ai/api/status", nil, "")
	if !strings.Contains(rec.Body.String(), user) {
		t.Fatalf("status lacks paused chat: %s", rec.Body)
	}
	if rec := adminReq(t, b, http.MethodDelete, "/cs-ai/api/pauses/"+user, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("clear: %d", rec.Code)
	}
	if _, paused := store.PausedUntil(user, time.Now()); paused {
		t.Fatal("pause not cleared")
	}
}

func TestSettingsPersistAndSeed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	seed := defaultSettings(testConfig())
	st, err := LoadSettings(path, filepath.Join(dir, "media"), seed)
	if err != nil {
		t.Fatal(err)
	}
	next := st.Get()
	next.FormText = "Isi data ya:\n* Nama:"
	next.AdminNumbers = []string{"6281100002222"}
	if _, err := st.Update(next, "stage", time.Now()); err != nil {
		t.Fatal(err)
	}
	// A different seed must not override what was saved.
	other := seed
	other.FormText = "SEED"
	st2, err := LoadSettings(path, filepath.Join(dir, "media"), other)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Get().FormText != "Isi data ya:\n* Nama:" {
		t.Fatalf("saved settings not reloaded: %q", st2.Get().FormText)
	}
}
