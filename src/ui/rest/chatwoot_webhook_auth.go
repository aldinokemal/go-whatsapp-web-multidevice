package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

// Headers Chatwoot sets when the API channel has a webhook secret (Chatwoot
// lib/webhooks/trigger.rb): the unix time of the delivery and
// "sha256=<hex HMAC-SHA256 of '<timestamp>.<raw body>' keyed with the secret>".
const (
	chatwootSignatureHeader = "X-Chatwoot-Signature"
	chatwootTimestampHeader = "X-Chatwoot-Timestamp"
)

var (
	errChatwootSignatureMissing   = errors.New("signature or timestamp header missing")
	errChatwootSignatureTimestamp = errors.New("timestamp is not a unix time")
	errChatwootSignatureExpired   = errors.New("timestamp outside the tolerance window")
	errChatwootSignatureMalformed = errors.New("signature is not sha256=<hex>")
	errChatwootSignatureMismatch  = errors.New("signature does not match")
)

// verifyChatwootSignature checks Chatwoot's native webhook signature. The
// timestamp is part of the signed string, so it cannot be changed without
// invalidating the signature; rejecting timestamps outside tolerance (in
// either direction, to allow for clock differences) bounds how long a captured
// request can be replayed.
func verifyChatwootSignature(secret, timestamp, signature string, body []byte, now time.Time, tolerance time.Duration) error {
	timestamp = strings.TrimSpace(timestamp)
	signature = strings.TrimSpace(signature)
	if secret == "" || timestamp == "" || signature == "" {
		return errChatwootSignatureMissing
	}

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errChatwootSignatureTimestamp
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > tolerance || age < -tolerance {
		return errChatwootSignatureExpired
	}

	hexDigest, ok := strings.CutPrefix(signature, "sha256=")
	if !ok {
		return errChatwootSignatureMalformed
	}
	provided, err := hex.DecodeString(hexDigest)
	if err != nil {
		return errChatwootSignatureMalformed
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write(body)
	// hmac.Equal compares in constant time.
	if !hmac.Equal(mac.Sum(nil), provided) {
		return errChatwootSignatureMismatch
	}
	return nil
}

// chatwootQuerySecretWarning makes the query-string warning a one-off: the
// point is to tell the operator once, not to add a line per webhook.
var chatwootQuerySecretWarning sync.Once

// chatwootWebhookAuthorized authenticates an incoming Chatwoot webhook against
// the global CHATWOOT_WEBHOOK_SECRET.
func chatwootWebhookAuthorized(c fiber.Ctx) bool {
	return chatwootWebhookAuthorizedWith(c, config.ChatwootWebhookSecret)
}

// chatwootWebhookAuthorizedWith authenticates an incoming Chatwoot webhook
// with the given secret. An empty secret accepts every request (the check is
// opt-in). Otherwise the request must satisfy one of:
//
//  1. Chatwoot's own signature: X-Chatwoot-Timestamp + X-Chatwoot-Signature.
//  2. The secret itself in X-Chatwoot-Webhook-Secret or X-Gowa-Chatwoot-Secret.
//  3. The secret itself in the ?secret= query parameter.
//  4. X-Hub-Signature-256: sha256=<hex HMAC-SHA256 of the raw body>.
//
// A request that carries a Chatwoot signature which does not verify is still
// accepted when another scheme does: Chatwoot signs with the channel's own
// secret, which an existing deployment authenticating through the URL may
// never have copied into GOWA.
func chatwootWebhookAuthorizedWith(c fiber.Ctx, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return true
	}

	nativeErr := verifyChatwootSignature(secret, c.Get(chatwootTimestampHeader), c.Get(chatwootSignatureHeader),
		c.BodyRaw(), time.Now(), config.ChatwootWebhookSignatureTolerance)
	if nativeErr == nil {
		return true
	}

	for _, candidate := range []string{
		c.Get("X-Chatwoot-Webhook-Secret"),
		c.Get("X-Gowa-Chatwoot-Secret"),
	} {
		if chatwootSecretMatches(secret, candidate) {
			return true
		}
	}

	if chatwootSecretMatches(secret, c.Query("secret")) {
		chatwootQuerySecretWarning.Do(func() {
			logrus.Warn("Chatwoot Webhook: authenticated by the ?secret= query parameter. URLs end up in proxy and access logs; " +
				"prefer Chatwoot's signed webhooks (set CHATWOOT_WEBHOOK_SECRET to the API channel's webhook secret and drop ?secret= from the URL).")
		})
		return true
	}

	if signature := strings.TrimSpace(c.Get("X-Hub-Signature-256")); signature != "" {
		digest, err := utils.GetMessageDigestOrSignature(c.Body(), []byte(secret))
		if err != nil {
			logrus.Errorf("Chatwoot Webhook: Failed to compute webhook signature: %v", err)
			return false
		}
		if chatwootSecretMatches("sha256="+digest, signature) {
			return true
		}
	}

	// Say why the Chatwoot signature failed when one was sent: an expired
	// timestamp (clock drift) and a wrong secret need different fixes.
	if !errors.Is(nativeErr, errChatwootSignatureMissing) {
		logrus.Warnf("Chatwoot Webhook: %s rejected: %v", chatwootSignatureHeader, nativeErr)
	}
	return false
}
