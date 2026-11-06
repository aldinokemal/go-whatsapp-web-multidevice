package companion

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"time"
)

// RawCapturer reads a screen without the pairing classifier. Account reviews
// need it, because that classifier refuses ban and verification screens.
type RawCapturer interface {
	CaptureRaw(context.Context) (string, error)
}

// ReviewUI serves review_begin and review_request on the primary phone. It
// never navigates, types or pairs. The only input it sends is one tap on the
// official app's REQUEST REVIEW button, and only on the exact screen the
// controller approved.
type ReviewUI struct {
	Transport UITransport
	Account   Account
	Package   string
	// Settle is how long to wait after the tap before reading the result.
	Settle  time.Duration
	started bool
	seen    string
	tapped  bool
}

var visibleNumber = regexp.MustCompile(`\+[0-9][0-9 ()\-]{6,}[0-9]`)
var nonDigits = regexp.MustCompile(`[^0-9]`)

func (u *ReviewUI) capture(ctx context.Context) (string, *Screen, error) {
	raw, ok := u.Transport.(RawCapturer)
	if !ok {
		return "", nil, block("invalid_request")
	}
	xml, e := raw.CaptureRaw(ctx)
	if e != nil {
		return "", nil, e
	}
	s, e := parseRawScreen(xml, u.Package)
	if e != nil {
		return "", nil, e
	}
	return xml, s, nil
}

// identity binds the screen to the configured account. The transport has
// already checked the phone serial, Android user and app package. A phone
// number shown on screen must be the account's own; any other number fails.
func (u *ReviewUI) identity(s *Screen) (string, error) {
	source := "device_binding"
	for _, n := range s.Nodes {
		for _, value := range []string{n.get("text"), n.get("content-desc")} {
			for _, m := range visibleNumber.FindAllString(value, -1) {
				if nonDigits.ReplaceAllString(m, "") != u.Account.Phone {
					return "", block("identity_mismatch")
				}
				source = "screen_number"
			}
		}
	}
	return source, nil
}

func (u *ReviewUI) Begin(ctx context.Context) (Object, error) {
	u.started = true
	if e := u.Transport.Start(ctx); e != nil {
		return nil, e
	}
	xml, s, e := u.capture(ctx)
	if e != nil {
		return nil, e
	}
	source, e := u.identity(s)
	if e != nil {
		return nil, e
	}
	u.seen = xml
	return Object{"identity_verified": true, "identity_source": source, "xml": xml}, nil
}

// Request taps REQUEST REVIEW once. The controller must name the screen it
// classified, by SHA-256, and the current screen must still offer the same
// enabled button; otherwise the current screen is returned untouched.
func (u *ReviewUI) Request(ctx context.Context, screenSHA256 string) (Object, error) {
	if u.seen == "" || u.tapped || screenSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(u.seen))) {
		return nil, block("invalid_request")
	}
	xml, s, e := u.capture(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = u.identity(s); e != nil {
		return nil, e
	}
	status, e := ReviewScreen(xml, u.Package)
	if e != nil {
		return nil, e
	}
	if status != "review_available" {
		return Object{"xml": xml, "tapped": false}, nil
	}
	x, y, e := s.Unique("REQUEST REVIEW")
	if e != nil {
		return nil, e
	}
	u.tapped = true
	if e = u.Transport.Tap(ctx, x, y); e != nil {
		return nil, e
	}
	settle := u.Settle
	if settle == 0 {
		settle = 3 * time.Second
	}
	if e = pause(ctx, settle); e != nil {
		return nil, e
	}
	xml, _, e = u.capture(ctx)
	if e != nil {
		return nil, e
	}
	return Object{"xml": xml, "tapped": true}, nil
}

// Finish turns the screen back off if it was off. It does not navigate away
// from a review screen.
func (u *ReviewUI) Finish(ctx context.Context) error {
	if !u.started {
		return nil
	}
	u.started = false
	e := u.Transport.Restore(ctx, "")
	releaseErr := u.Transport.Release()
	if e != nil {
		return e
	}
	return releaseErr
}
