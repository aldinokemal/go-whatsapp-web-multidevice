package main

import (
	"regexp"
	"strings"
)

// renderedReply is an AI reply after the bridge replaced its markers.
type renderedReply struct {
	Text        string
	Images      []MediaItem
	Handoff     bool
	Reason      string
	PaymentSent bool
	FormSent    bool
	// Package code and price of the last [[REKENING]] (for the Dashboard CRM).
	PaymentPaket, PaymentNominal string
	Warnings                     []string // unknown codes etc., for the log
}

var (
	formRe     = regexp.MustCompile(`(?i)\[\[\s*FORM\s*\]\]`)
	rekeningRe = regexp.MustCompile(`(?i)\[\[\s*REKENING\s*(?::\s*([^\]\s]*)\s*)?\]\]`)
	gambarRe   = regexp.MustCompile(`(?i)\[\[\s*GAMBAR\s*:\s*([^\]\s]+)\s*\]\]`)
	anyMarkRe  = regexp.MustCompile(`\[\[[^\]]{0,200}\]\]`)
	blankRunRe = regexp.MustCompile(`\n{3,}`)
)

const maxImagesPerReply = 2

// renderReply turns the AI text into what is actually sent: official form and
// payment text are pasted from settings (so the model never types an account
// number), images are collected, and any other [[...]] marker is removed.
func renderReply(reply string, s Settings) renderedReply {
	var r renderedReply
	clean, reason, handoff := parseHandoff(reply)
	r.Handoff, r.Reason = handoff, reason

	// Markdown cleanup first, so the official templates are not altered.
	clean = toWhatsAppMarkdown(clean)

	if formRe.MatchString(clean) {
		r.FormSent = true
		clean = formRe.ReplaceAllLiteralString(clean, "\n"+strings.TrimSpace(s.FormText)+"\n")
	}

	clean = rekeningRe.ReplaceAllStringFunc(clean, func(m string) string {
		code := ""
		if sm := rekeningRe.FindStringSubmatch(m); len(sm) > 1 {
			code = strings.TrimSpace(sm[1])
		}
		paket, nominal := "sesuai pilihan Kakak", "akan dikonfirmasi admin"
		if p, ok := s.Package(code); ok {
			paket, nominal = p.Name, p.Price
			r.PaymentPaket, r.PaymentNominal = strings.ToLower(p.Code), p.Price
		} else {
			r.Warnings = append(r.Warnings, "unknown package code in [[REKENING]]: "+code)
		}
		r.PaymentSent = true
		txt := strings.ReplaceAll(strings.TrimSpace(s.PaymentText), "{PAKET}", paket)
		return "\n" + strings.ReplaceAll(txt, "{NOMINAL}", nominal) + "\n"
	})

	seen := map[string]bool{}
	clean = gambarRe.ReplaceAllStringFunc(clean, func(m string) string {
		code := strings.ToLower(gambarRe.FindStringSubmatch(m)[1])
		item, ok := s.MediaByCode(code)
		switch {
		case !ok:
			r.Warnings = append(r.Warnings, "unknown image code: "+code)
		case seen[code]:
		case len(r.Images) >= maxImagesPerReply:
			r.Warnings = append(r.Warnings, "image limit reached, skipped: "+code)
		default:
			seen[code] = true
			r.Images = append(r.Images, item)
		}
		return ""
	})

	if leftovers := anyMarkRe.FindAllString(clean, -1); len(leftovers) > 0 {
		r.Warnings = append(r.Warnings, "removed unknown markers: "+strings.Join(leftovers, " "))
		clean = anyMarkRe.ReplaceAllString(clean, "")
	}

	clean = blankRunRe.ReplaceAllString(clean, "\n\n")
	r.Text = strings.TrimSpace(clean)
	return r
}
