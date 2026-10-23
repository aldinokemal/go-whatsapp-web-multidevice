package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Package is one paid option the AI can offer and bill for.
type Package struct {
	Code   string `json:"code"`   // used in [[REKENING:<code>]]
	Name   string `json:"name"`   // e.g. "1× sepekan"
	Price  string `json:"price"`  // e.g. "Rp149.000"
	Detail string `json:"detail"` // e.g. "4 pertemuan/bulan"
}

// MediaItem is an image the AI may send with [[GAMBAR:<code>]].
type MediaItem struct {
	Code        string    `json:"code"`
	Description string    `json:"description"` // when the AI should send it
	Caption     string    `json:"caption"`
	File        string    `json:"file"` // file name inside the media dir
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

// Settings is everything an admin can change from the /cs-ai page. Secrets and
// addresses stay in the environment and are never exposed here.
type Settings struct {
	Enabled          bool        `json:"enabled"`
	AdminNumbers     []string    `json:"admin_numbers"`
	PauseMinutes     int         `json:"pause_minutes"`
	DebounceMillis   int         `json:"debounce_ms"`
	RateLimitPerHour int         `json:"rate_limit_per_hour"`
	Typing           bool        `json:"typing"`
	Packages         []Package   `json:"packages"`
	PaymentText      string      `json:"payment_text"` // {PAKET} and {NOMINAL} are filled in
	FormText         string      `json:"form_text"`
	HandoffMessage   string      `json:"handoff_message"`
	FallbackMessage  string      `json:"fallback_message"`
	RateLimitMessage string      `json:"rate_limit_message"`
	Instructions     string      `json:"instructions"`
	Media            []MediaItem `json:"media"`
	UpdatedAt        time.Time   `json:"updated_at"`
	UpdatedBy        string      `json:"updated_by"`
}

func (s Settings) PauseDuration() time.Duration { return time.Duration(s.PauseMinutes) * time.Minute }
func (s Settings) Debounce() time.Duration      { return time.Duration(s.DebounceMillis) * time.Millisecond }

func (s Settings) Package(code string) (Package, bool) {
	for _, p := range s.Packages {
		if strings.EqualFold(p.Code, code) {
			return p, true
		}
	}
	return Package{}, false
}

func (s Settings) MediaByCode(code string) (MediaItem, bool) {
	for _, m := range s.Media {
		if strings.EqualFold(m.Code, code) {
			return m, true
		}
	}
	return MediaItem{}, false
}

const defaultPaymentText = `💳 *Info Pembayaran Pendaftaran*

Paket: *{PAKET}*
Nominal: *{NOMINAL}*

🏦 *Bank Muamalat*
No. Rek: *1110012352*
Atas Nama: *Ruang Sanad Indonesia*

Setelah transfer, mohon kirimkan *foto/screenshot bukti transfer* di chat ini ya, Kak 🙏🏻
Mohon transfer hanya ke rekening resmi di atas.`

const defaultFormText = `Untuk melanjutkan pendaftaran silahkan isi list di bawah ini yaa 📝

* Nama:
* Usia:
* Domisili:
* Ikhwan/Akhwat:
* Pilihan kelas (1× / 2× sepekan):
* Waktu belajar (pagi/siang/malam):
* Apakah sebelumnya pernah belajar Tahsin/Makhorijul Huruf:
* Jika iya/pernah, belajar dimana? :

Syukron 🙏🏻😊`

// defaultSettings seeds the first settings.json from the environment.
func defaultSettings(c Config) Settings {
	return Settings{
		Enabled:          true,
		AdminNumbers:     append([]string(nil), c.AdminNumbers...),
		PauseMinutes:     int(c.PauseDuration / time.Minute),
		DebounceMillis:   int(c.Debounce / time.Millisecond),
		RateLimitPerHour: c.RateLimitPerHr,
		Typing:           c.Typing,
		Packages: []Package{
			{Code: "1x", Name: "1× sepekan", Price: "Rp149.000", Detail: "per bulan, 4 pertemuan"},
			{Code: "2x", Name: "2× sepekan", Price: "Rp199.000", Detail: "per bulan, 8 pertemuan"},
		},
		PaymentText:      defaultPaymentText,
		FormText:         defaultFormText,
		HandoffMessage:   c.HandoffMessage,
		FallbackMessage:  c.FallbackMessage,
		RateLimitMessage: c.RateLimitMessage,
		Instructions:     c.SystemPrompt,
		Media:            []MediaItem{},
	}
}

var codeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Validate normalises s in place and rejects values that would break the bridge.
func (s *Settings) Validate() error {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	var admins []string
	seen := map[string]bool{}
	for _, n := range s.AdminNumbers {
		d := normalizePhone(n)
		if d == "" {
			continue
		}
		if len(d) < 9 || len(d) > 15 {
			add("nomor admin %q tidak valid", n)
			continue
		}
		if !seen[d] {
			seen[d] = true
			admins = append(admins, d)
		}
	}
	s.AdminNumbers = admins
	if len(admins) == 0 {
		add("isi minimal satu nomor admin untuk menerima pengalihan")
	}
	if s.PauseMinutes < 5 || s.PauseMinutes > 7*24*60 {
		add("lama jeda harus 5 menit sampai 7 hari")
	}
	if s.DebounceMillis < 0 || s.DebounceMillis > 60000 {
		add("jeda penggabungan pesan harus 0–60 detik")
	}
	if s.RateLimitPerHour < 0 || s.RateLimitPerHour > 1000 {
		add("batas pesan per jam harus 0–1000")
	}

	codes := map[string]bool{}
	for i := range s.Packages {
		p := &s.Packages[i]
		p.Code = strings.ToLower(strings.TrimSpace(p.Code))
		p.Name, p.Price, p.Detail = strings.TrimSpace(p.Name), strings.TrimSpace(p.Price), strings.TrimSpace(p.Detail)
		switch {
		case !codeRe.MatchString(p.Code):
			add("kode paket %q hanya boleh huruf kecil, angka, - atau _", p.Code)
		case codes[p.Code]:
			add("kode paket %q dipakai dua kali", p.Code)
		case p.Name == "" || p.Price == "":
			add("paket %q harus punya nama dan harga", p.Code)
		}
		codes[p.Code] = true
	}
	if len(s.Packages) == 0 {
		add("isi minimal satu paket")
	}

	limits := []struct {
		name, val string
		max       int
		required  bool
	}{
		{"teks pembayaran", s.PaymentText, 4000, true},
		{"teks form pendaftaran", s.FormText, 4000, true},
		{"pesan pengalihan", s.HandoffMessage, 1000, true},
		{"pesan cadangan", s.FallbackMessage, 1000, true},
		{"pesan batas pesan", s.RateLimitMessage, 1000, false},
		{"instruksi CS", s.Instructions, 40000, true},
	}
	for _, l := range limits {
		if l.required && strings.TrimSpace(l.val) == "" {
			add("%s tidak boleh kosong", l.name)
		}
		if len([]rune(l.val)) > l.max {
			add("%s maksimal %d karakter", l.name, l.max)
		}
	}
	if !strings.Contains(s.PaymentText, "{NOMINAL}") {
		add("teks pembayaran harus memuat {NOMINAL} supaya nominal paket tercantum")
	}
	for i := range s.Media {
		m := &s.Media[i]
		m.Description, m.Caption = strings.TrimSpace(m.Description), strings.TrimSpace(m.Caption)
		if len([]rune(m.Caption)) > 1000 || len([]rune(m.Description)) > 500 {
			add("keterangan/caption gambar %q terlalu panjang", m.Code)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// SettingsStore keeps settings in memory and in a JSON file next to state.json.
type SettingsStore struct {
	mu       sync.RWMutex
	path     string
	mediaDir string
	s        Settings
}

func LoadSettings(path, mediaDir string, seed Settings) (*SettingsStore, error) {
	st := &SettingsStore{path: path, mediaDir: mediaDir}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		st.s = seed
		if err := st.s.Validate(); err != nil {
			// A seed without admin numbers is still usable; the page asks for them.
			st.s = seed
		}
		if err := st.saveLocked(); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &st.s); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if st.s.Media == nil {
			st.s.Media = []MediaItem{}
		}
	}
	if mediaDir != "" {
		if err := os.MkdirAll(mediaDir, 0o700); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// Get returns a copy that callers may read without locking.
func (st *SettingsStore) Get() Settings {
	st.mu.RLock()
	defer st.mu.RUnlock()
	c := st.s
	c.AdminNumbers = append([]string(nil), st.s.AdminNumbers...)
	c.Packages = append([]Package(nil), st.s.Packages...)
	c.Media = append([]MediaItem(nil), st.s.Media...)
	return c
}

// Update validates and stores new settings. Media files are managed separately,
// so only the caption/description of existing media is taken from next.
func (st *SettingsStore) Update(next Settings, by string, now time.Time) (Settings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	merged := make([]MediaItem, 0, len(st.s.Media))
	for _, cur := range st.s.Media {
		for _, in := range next.Media {
			if in.Code == cur.Code {
				cur.Caption, cur.Description = in.Caption, in.Description
			}
		}
		merged = append(merged, cur)
	}
	next.Media = merged
	if err := next.Validate(); err != nil {
		return st.s, err
	}
	next.UpdatedAt, next.UpdatedBy = now, by
	prev := st.s
	st.s = next
	if err := st.saveLocked(); err != nil {
		st.s = prev
		return prev, err
	}
	return st.s, nil
}

// PutMedia stores (or replaces) an image under code.
func (st *SettingsStore) PutMedia(item MediaItem, data []byte, by string, now time.Time) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.mediaDir == "" {
		return errors.New("media directory not configured")
	}
	tmp := filepath.Join(st.mediaDir, "."+item.File+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(st.mediaDir, item.File)); err != nil {
		return err
	}
	var oldFile string
	replaced := false
	for i, m := range st.s.Media {
		if m.Code == item.Code {
			oldFile = m.File
			st.s.Media[i] = item
			replaced = true
		}
	}
	if !replaced {
		st.s.Media = append(st.s.Media, item)
	}
	st.s.UpdatedAt, st.s.UpdatedBy = now, by
	if err := st.saveLocked(); err != nil {
		return err
	}
	if oldFile != "" && oldFile != item.File {
		_ = os.Remove(filepath.Join(st.mediaDir, oldFile))
	}
	return nil
}

func (st *SettingsStore) DeleteMedia(code, by string, now time.Time) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	kept := st.s.Media[:0:0]
	var removed *MediaItem
	for _, m := range st.s.Media {
		if m.Code == code {
			m := m
			removed = &m
			continue
		}
		kept = append(kept, m)
	}
	if removed == nil {
		return false, nil
	}
	st.s.Media = kept
	st.s.UpdatedAt, st.s.UpdatedBy = now, by
	if err := st.saveLocked(); err != nil {
		return true, err
	}
	_ = os.Remove(filepath.Join(st.mediaDir, removed.File))
	return true, nil
}

// MediaPath returns the absolute path of an item's file.
func (st *SettingsStore) MediaPath(m MediaItem) string {
	return filepath.Join(st.mediaDir, filepath.Base(m.File))
}

func (st *SettingsStore) saveLocked() error {
	if st.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}
