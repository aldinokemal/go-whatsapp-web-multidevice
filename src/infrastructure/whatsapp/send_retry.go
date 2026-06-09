package whatsapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// reachoutSubscribeTimeout bounds the pre-warm SubscribePresence so an
// unhealthy socket cannot stall the retry. One round-trip's worth of slack.
const reachoutSubscribeTimeout = 3 * time.Second

var (
	reachoutPrivacyTokenWait       = 2 * time.Second
	reachoutPrivacyTokenPollPeriod = 50 * time.Millisecond
)

// Seams for unit tests. Mirrors the submitWebhookFn pattern in webhook_forward.go.
var (
	sendMessageFn = func(ctx context.Context, client *whatsmeow.Client, recipient types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
		return client.SendMessage(ctx, recipient, msg)
	}
	subscribePresenceFn = func(ctx context.Context, client *whatsmeow.Client, recipient types.JID) error {
		return client.SubscribePresence(ctx, recipient)
	}
)

// IsReachoutTimelockError reports whether err is WhatsApp server error 463
// (NackCallerReachoutTimelocked) — surfaced when the recipient's privacy
// token (tctoken) is missing/expired or the recipient is a cold contact.
//
// whatsmeow formats this error as fmt.Errorf("%w %d", ErrServerReturnedError,
// code) → "server returned error 463". errors.Is gates on the sentinel; the
// leading-space suffix guards against false matches like "4631".
func IsReachoutTimelockError(err error) bool {
	return err != nil &&
		errors.Is(err, whatsmeow.ErrServerReturnedError) &&
		strings.HasSuffix(err.Error(), " 463")
}

// SendMessageWithReachoutRetry sends msg to recipient and, if the send fails
// with WhatsApp error 463 on a 1:1 user JID, performs a SubscribePresence
// pre-warm and retries the send exactly once.
//
// SubscribePresence is the only step in WA-Web's chat-open sequence that
// participates in whatsmeow's privacy-token path (it attaches any cached
// tctoken to the subscribe stanza). The retry itself does the real work:
// whatsmeow's post-send issuePrivacyTokenAndSave runs on the failed first
// attempt and lets the second succeed once a token is in place.
//
// Group/broadcast/newsletter/legacy-server JIDs skip the retry — 463 does
// not apply to them.
func SendMessageWithReachoutRetry(ctx context.Context, client *whatsmeow.Client, recipient types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	resp, err := sendMessageFn(ctx, client, recipient, msg)
	if err == nil || !IsReachoutTimelockError(err) || !isUserJID(recipient) {
		return resp, err
	}

	logrus.Warnf("Send to %s rejected with WhatsApp error 463; subscribing to presence and retrying once", recipient.String())

	// Detached context so an upstream cancel cannot abort the subscribe
	// mid-flight. Device scoping (via ContextWithDevice) is preserved.
	warmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reachoutSubscribeTimeout)
	if subscribeErr := subscribePresenceFn(warmCtx, client, recipient); subscribeErr != nil {
		logrus.Debugf("Pre-warm SubscribePresence for %s failed: %v", recipient.String(), subscribeErr)
	}
	cancel()

	if waitForReachoutPrivacyToken(ctx, client, recipient) {
		logrus.Debugf("Privacy token for %s became available before retry", recipient.String())
	} else {
		logrus.Debugf("Retrying send to %s without an observed privacy token", recipient.String())
	}

	resp, err = sendMessageFn(ctx, client, recipient, msg)
	if err != nil {
		logrus.Warnf("Retry send to %s after pre-warm still failed: %v", recipient.String(), err)
	} else {
		logrus.Infof("Retry send to %s after pre-warm succeeded", recipient.String())
	}
	return resp, err
}

// isUserJID reports whether the JID is a 1:1 user JID (regular or hidden/LID).
// Error 463 does not apply to group/broadcast/newsletter/legacy-server JIDs.
func isUserJID(jid types.JID) bool {
	return jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer
}

func waitForReachoutPrivacyToken(ctx context.Context, client *whatsmeow.Client, recipient types.JID) bool {
	if client == nil || client.Store == nil || client.Store.PrivacyTokens == nil {
		return false
	}

	waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reachoutPrivacyTokenWait)
	defer cancel()

	if hasReachoutPrivacyToken(waitCtx, client, recipient) {
		return true
	}

	ticker := time.NewTicker(reachoutPrivacyTokenPollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return false
		case <-ticker.C:
			if hasReachoutPrivacyToken(waitCtx, client, recipient) {
				return true
			}
		}
	}
}

func hasReachoutPrivacyToken(ctx context.Context, client *whatsmeow.Client, recipient types.JID) bool {
	token, err := client.Store.PrivacyTokens.GetPrivacyToken(ctx, recipient.ToNonAD())
	if err != nil {
		logrus.Debugf("Failed to check privacy token for %s before retry: %v", recipient.String(), err)
		return false
	}
	return token != nil && len(token.Token) > 0
}
