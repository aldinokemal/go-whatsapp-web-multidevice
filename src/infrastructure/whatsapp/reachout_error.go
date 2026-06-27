package whatsapp

import (
	"errors"
	"strings"

	"go.mau.fi/whatsmeow"
)

// IsReachoutTimelockError reports whether err is WhatsApp server error 463
// (NackCallerReachoutTimelocked / "reach-out timelock"). This is WhatsApp's
// server-side anti-spam restriction on starting new chats: it surfaces when the
// recipient has no prior conversation with this account (no stored privacy
// token) or the sending account is temporarily restricted from reaching new
// contacts. It is enforced by WhatsApp and cannot be bypassed by the client —
// the trusted-contact token whatsmeow attaches on the outbound path can only
// originate from the recipient's side (an inbound message, a privacy_token
// notification, or history sync).
//
// whatsmeow formats this error as fmt.Errorf("%w %d", ErrServerReturnedError,
// code) → "server returned error 463". errors.Is gates on the sentinel; the
// leading-space suffix guards against false matches like "4631".
func IsReachoutTimelockError(err error) bool {
	return err != nil &&
		errors.Is(err, whatsmeow.ErrServerReturnedError) &&
		strings.HasSuffix(err.Error(), " 463")
}
