package main

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

// defaultSystemPrompt is the editable part of the CS instructions (style, flow,
// program facts). It combines the whatsapp-cs-closing skill with the Ruang Sanad
// closing guide and 30-day script (sources in knowledge/). Admins can change it
// from the /cs-ai page; it seeds settings.json on first start.
//
//go:embed prompts/ruang-sanad-cs.md
var defaultSystemPrompt string

// lockedPrompt holds the rules the /cs-ai page cannot change: marker syntax,
// payment safety, handoff triggers and prompt-injection resistance.
//
//go:embed prompts/locked.md
var lockedPrompt string

// buildSystemPrompt assembles what Hermes receives on every CS turn:
// editable instructions + data from settings + locked rules + sender context.
func buildSystemPrompt(s Settings, senderName string, now time.Time, paymentSent bool) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(s.Instructions))
	b.WriteString("\n\n## Data dari pengaturan (sumber kebenaran, menang atas teks lain)\n\n")
	b.WriteString("Paket yang tersedia (kode untuk [[REKENING:<kode>]]):\n")
	for _, p := range s.Packages {
		detail := ""
		if p.Detail != "" {
			detail = " (" + p.Detail + ")"
		}
		fmt.Fprintf(&b, "- kode `%s`: *%s* — %s%s\n", p.Code, p.Name, p.Price, detail)
	}
	b.WriteString("\nForm pendaftaran: kirim dengan [[FORM]]. Isinya meminta:\n")
	for _, line := range strings.Split(s.FormText, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "*") || strings.HasPrefix(t, "-") || strings.HasPrefix(t, "•") {
			fmt.Fprintf(&b, "  %s\n", t)
		}
	}
	b.WriteString("\nGaleri gambar (kirim dengan [[GAMBAR:<kode>]]):\n")
	if len(s.Media) == 0 {
		b.WriteString("- belum ada gambar; jangan memakai [[GAMBAR:...]]\n")
	}
	for _, m := range s.Media {
		fmt.Fprintf(&b, "- kode `%s`: %s\n", m.Code, orDash(m.Description))
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(lockedPrompt))
	fmt.Fprintf(&b, "\n\n## Konteks chat ini\nNama pengirim di WhatsApp: %s\nWaktu sekarang: %s\n",
		orDash(senderName), now.Format("Monday, 2 January 2006 15:04 MST"))
	if paymentSent {
		b.WriteString("Info pembayaran ([[REKENING]]) SUDAH pernah dikirim ke pengirim ini.\n")
	}
	return b.String()
}
