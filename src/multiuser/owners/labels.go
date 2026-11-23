package owners

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// MaxLabelLen is the longest name a number can have, in characters (runes).
const MaxLabelLen = 40

var (
	ErrLabelTooLong = pkgError.ValidationError("a name can be at most 40 characters")
	ErrLabelInvalid = pkgError.ValidationError("a name can't contain control or text-direction characters")
)

// NormalizeLabel checks and tidies a number's name: spaces are trimmed and
// runs of whitespace become one space; at most MaxLabelLen characters; no
// control characters and no Unicode bidi embedding, override or isolate
// characters (U+202A to U+202E, U+2066 to U+2069), which could make a name
// display as something else. "" (after trimming) means "remove the name".
// Everything else, HTML included, is plain text: templates escape it.
func NormalizeLabel(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrLabelInvalid
	}
	for _, r := range s {
		if isBidiControl(r) {
			return "", ErrLabelInvalid
		}
		// Whitespace controls (tab, newline) are collapsed below, not refused.
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", ErrLabelInvalid
		}
	}
	out := strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(out) > MaxLabelLen {
		return "", ErrLabelTooLong
	}
	return out, nil
}

func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// SetDeviceLabel names (or, with "", un-names) one of the signed-in user's
// numbers and returns the stored name. A number that is not theirs, or no
// user in ctx, is pkgError.ErrDeviceNotFound, the same as one that does not
// exist. The audit line records that it was renamed, never the name.
func SetDeviceLabel(ctx context.Context, st *store.Store, deviceID, raw string) (string, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return "", pkgError.ErrDeviceNotFound
	}
	label, err := NormalizeLabel(raw)
	if err != nil {
		return "", err
	}
	if err := st.SetLabel(ctx, u.ID, deviceID, label); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", pkgError.ErrDeviceNotFound
		}
		return "", err
	}
	_ = st.Audit(ctx, store.AuditEvent{UserID: u.ID, ActorID: actorID(ctx), Action: "device_renamed", DeviceID: deviceID})
	return label, nil
}
