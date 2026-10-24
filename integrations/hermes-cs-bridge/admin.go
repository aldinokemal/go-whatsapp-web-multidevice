package main

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The /cs-ai settings page. It is reached through GOWA, which checks Basic Auth
// and then forwards the request with X-CS-AI-Token (shared secret) and
// X-CS-AI-User (the Basic Auth username). Requests without the token are
// rejected, so nothing else on the docker network can change the settings.

//go:embed admin.html
var adminHTML []byte

const (
	maxMediaBytes = 5 << 20
	adminPrefix   = "/cs-ai"
)

var allowedImageTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

func (b *Bridge) adminRoutes(mux *http.ServeMux) {
	if b.cfg.AdminToken == "" {
		return
	}
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, b.adminAuth(fn)) }
	h("GET "+adminPrefix, b.adminPage)
	h("GET "+adminPrefix+"/", b.adminPage)
	h("GET "+adminPrefix+"/api/settings", b.apiGetSettings)
	h("PUT "+adminPrefix+"/api/settings", b.apiPutSettings)
	h("GET "+adminPrefix+"/api/defaults", b.apiDefaults)
	h("GET "+adminPrefix+"/api/status", b.apiStatus)
	h("GET "+adminPrefix+"/api/prompt-preview", b.apiPromptPreview)
	h("DELETE "+adminPrefix+"/api/pauses/{chat}", b.apiDeletePause)
	h("POST "+adminPrefix+"/api/media", b.apiUploadMedia)
	h("DELETE "+adminPrefix+"/api/media/{code}", b.apiDeleteMedia)
	h("GET "+adminPrefix+"/media/{code}", b.apiServeMedia)
}

func (b *Bridge) adminAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-CS-AI-Token")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(b.cfg.AdminToken)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Browsers attach cached Basic Auth credentials to cross-site requests;
			// a custom header forces a CORS preflight and Sec-Fetch-Site exposes
			// cross-site origins, which together block CSRF.
			if r.Header.Get("X-CS-AI-Request") != "1" {
				jsonErr(w, http.StatusForbidden, "permintaan ditolak (header tidak lengkap)")
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				jsonErr(w, http.StatusForbidden, "permintaan lintas situs ditolak")
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next(w, r)
	})
}

func adminUser(r *http.Request) string {
	if u := strings.TrimSpace(r.Header.Get("X-CS-AI-User")); u != "" {
		return truncate(u, 64)
	}
	return "admin"
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, status int, msg string) {
	jsonOut(w, status, map[string]string{"error": msg})
}

func (b *Bridge) adminPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	_, _ = w.Write(adminHTML)
}

func (b *Bridge) apiGetSettings(w http.ResponseWriter, _ *http.Request) {
	jsonOut(w, http.StatusOK, b.settings.Get())
}

func (b *Bridge) apiPutSettings(w http.ResponseWriter, r *http.Request) {
	var in Settings
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		jsonErr(w, http.StatusBadRequest, "format data tidak valid: "+err.Error())
		return
	}
	saved, err := b.settings.Update(in, adminUser(r), b.now())
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	b.log.Info("settings updated from /cs-ai", "by", adminUser(r), "enabled", saved.Enabled)
	jsonOut(w, http.StatusOK, saved)
}

// apiDefaults returns the built-in texts so the page can offer "reset to default".
func (b *Bridge) apiDefaults(w http.ResponseWriter, _ *http.Request) {
	d := defaultSettings(b.cfg)
	jsonOut(w, http.StatusOK, map[string]string{
		"instructions": defaultSystemPrompt,
		"payment_text": d.PaymentText,
		"form_text":    d.FormText,
		"locked_rules": lockedPrompt,
	})
}

type pauseView struct {
	Chat   string    `json:"chat"`
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

func (b *Bridge) apiStatus(w http.ResponseWriter, r *http.Request) {
	now := b.now()
	var pauses []pauseView
	for chatID, p := range b.store.ActivePauses(now) {
		pauses = append(pauses, pauseView{chatID, p.Until, p.Reason})
	}
	sort.Slice(pauses, func(i, j int) bool { return pauses[i].Until.Before(pauses[j].Until) })

	hermes := "tidak terjangkau"
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.cfg.HermesURL+"/health", nil); err == nil {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			hermes = fmt.Sprintf("HTTP %d", resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				hermes = "aktif"
			}
		}
	}
	s := b.settings.Get()
	jsonOut(w, http.StatusOK, map[string]any{
		"enabled": s.Enabled, "hermes": hermes, "device": b.cfg.GowaDeviceID,
		"paused": pauses, "admins": len(s.AdminNumbers), "now": now,
	})
}

func (b *Bridge) apiPromptPreview(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, buildSystemPrompt(b.settings.Get(), "Contoh Nama", b.now(), false))
}

func (b *Bridge) apiDeletePause(w http.ResponseWriter, r *http.Request) {
	chatID := r.PathValue("chat")
	if chatID == "" {
		jsonErr(w, http.StatusBadRequest, "chat kosong")
		return
	}
	if err := b.store.ClearPause(chatID); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	b.log.Info("pause cleared from /cs-ai", "chat", chatID, "by", adminUser(r))
	jsonOut(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (b *Bridge) apiUploadMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaBytes+(64<<10))
	if err := r.ParseMultipartForm(maxMediaBytes); err != nil {
		jsonErr(w, http.StatusBadRequest, "upload gagal atau file lebih dari 5 MB")
		return
	}
	code := strings.ToLower(strings.TrimSpace(r.FormValue("code")))
	if !codeRe.MatchString(code) {
		jsonErr(w, http.StatusBadRequest, "kode hanya boleh huruf kecil, angka, - atau _ (maks 32)")
		return
	}
	desc, caption := strings.TrimSpace(r.FormValue("description")), strings.TrimSpace(r.FormValue("caption"))
	if desc == "" {
		jsonErr(w, http.StatusBadRequest, "isi keterangan kapan gambar ini dikirim")
		return
	}
	if len([]rune(desc)) > 500 || len([]rune(caption)) > 1000 {
		jsonErr(w, http.StatusBadRequest, "keterangan atau caption terlalu panjang")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "pilih file gambar")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxMediaBytes+1))
	if err != nil || len(data) == 0 {
		jsonErr(w, http.StatusBadRequest, "file kosong atau tidak terbaca")
		return
	}
	if len(data) > maxMediaBytes {
		jsonErr(w, http.StatusBadRequest, "file lebih dari 5 MB")
		return
	}
	ctype := http.DetectContentType(data)
	if ctype == "application/octet-stream" && len(data) > 12 && string(data[8:12]) == "WEBP" {
		ctype = "image/webp"
	}
	ext, ok := allowedImageTypes[ctype]
	if !ok {
		jsonErr(w, http.StatusBadRequest, "format harus JPG, PNG, atau WEBP")
		return
	}
	item := MediaItem{Code: code, Description: desc, Caption: caption, File: code + ext,
		ContentType: ctype, Size: int64(len(data)), UploadedAt: b.now()}
	if err := b.settings.PutMedia(item, data, adminUser(r), b.now()); err != nil {
		jsonErr(w, http.StatusInternalServerError, "gagal menyimpan: "+err.Error())
		return
	}
	b.log.Info("image uploaded from /cs-ai", "code", code, "bytes", len(data), "by", adminUser(r))
	jsonOut(w, http.StatusOK, item)
}

func (b *Bridge) apiDeleteMedia(w http.ResponseWriter, r *http.Request) {
	ok, err := b.settings.DeleteMedia(r.PathValue("code"), adminUser(r), b.now())
	switch {
	case err != nil:
		jsonErr(w, http.StatusInternalServerError, err.Error())
	case !ok:
		jsonErr(w, http.StatusNotFound, "gambar tidak ditemukan")
	default:
		jsonOut(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (b *Bridge) apiServeMedia(w http.ResponseWriter, r *http.Request) {
	m, ok := b.settings.Get().MediaByCode(r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", m.ContentType)
	http.ServeFile(w, r, b.settings.MediaPath(m))
}
