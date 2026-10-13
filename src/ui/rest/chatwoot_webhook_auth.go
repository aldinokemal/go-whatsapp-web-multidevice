package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	ok, nativeErr := chatwootWebhookVerify(c, secret)
	if !ok {
		logChatwootSignatureRejection(nativeErr)
	}
	return ok
}

// logChatwootSignatureRejection says why the Chatwoot signature failed when
// one was sent: an expired timestamp (clock drift) and a wrong secret need
// different fixes.
func logChatwootSignatureRejection(nativeErr error) {
	if nativeErr != nil && !errors.Is(nativeErr, errChatwootSignatureMissing) {
		logrus.Warnf("Chatwoot Webhook: %s rejected: %v", chatwootSignatureHeader, nativeErr)
	}
}

// chatwootVerifiedSecretKey is the request-local slot remembering the secret
// this request already verified against, so a second check against the same
// secret (the per-device endpoint authenticates before parsing, then again
// once the route is known) neither repeats the work nor mistakes the request
// for a replay of itself.
type chatwootVerifiedSecretKey struct{}

// chatwootWebhookVerify is chatwootWebhookAuthorizedWith without the rejection
// log. The returned error is the outcome of the Chatwoot-signature check, for
// the caller to log when the request ends up rejected.
func chatwootWebhookVerify(c fiber.Ctx, secret string) (bool, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return true, nil
	}
	if verified, ok := c.Locals(chatwootVerifiedSecretKey{}).(string); ok && chatwootSecretMatches(secret, verified) {
		return true, nil
	}

	now := time.Now()
	timestamp, signature := c.Get(chatwootTimestampHeader), c.Get(chatwootSignatureHeader)
	nativeErr := verifyChatwootSignature(secret, timestamp, signature, c.BodyRaw(), now, config.ChatwootWebhookSignatureTolerance)
	if nativeErr == nil {
		if chatwootReplays.seenBefore(timestamp, signature, now, config.ChatwootWebhookSignatureTolerance) {
			c.Locals(chatwootReplayedKey{}, true)
		}
		c.Locals(chatwootVerifiedSecretKey{}, secret)
		return true, nil
	}

	for _, candidate := range []string{
		c.Get("X-Chatwoot-Webhook-Secret"),
		c.Get("X-Gowa-Chatwoot-Secret"),
	} {
		if chatwootSecretMatches(secret, candidate) {
			c.Locals(chatwootVerifiedSecretKey{}, secret)
			return true, nativeErr
		}
	}

	if chatwootSecretMatches(secret, c.Query("secret")) {
		chatwootQuerySecretWarning.Do(func() {
			logrus.Warn("Chatwoot Webhook: authenticated by the ?secret= query parameter. URLs end up in proxy and access logs; " +
				"prefer Chatwoot's signed webhooks (set CHATWOOT_WEBHOOK_SECRET to the API channel's webhook secret and drop ?secret= from the URL).")
		})
		c.Locals(chatwootVerifiedSecretKey{}, secret)
		return true, nativeErr
	}

	if signature := strings.TrimSpace(c.Get("X-Hub-Signature-256")); signature != "" {
		digest, err := utils.GetMessageDigestOrSignature(c.Body(), []byte(secret))
		if err != nil {
			logrus.Errorf("Chatwoot Webhook: Failed to compute webhook signature: %v", err)
			return false, nativeErr
		}
		if chatwootSecretMatches("sha256="+digest, signature) {
			c.Locals(chatwootVerifiedSecretKey{}, secret)
			return true, nativeErr
		}
	}

	return false, nativeErr
}

// chatwootRouteWebhookSecret returns the secret that protects a delivery
// through the given route: the routed device's own webhook_secret when its
// config has one (each Chatwoot API channel has its own secret), otherwise the
// global CHATWOOT_WEBHOOK_SECRET. A route with no device (unroutable) and a
// device without a config use the global secret.
//
// The device is looked up the way delivery resolves it (alias, JID, or the
// default device for an empty id in legacy mode), under every identifier it is
// known by, so a route that names the device by its storage JID finds a config
// stored under the alias. A storage error is returned so the caller can refuse
// the request instead of guessing.
func (h *ChatwootHandler) chatwootRouteWebhookSecret(route chatwootWebhookRoute) (string, error) {
	if h == nil || h.ChatStorageRepo == nil || route.Unroutable {
		return config.ChatwootWebhookSecret, nil
	}
	identifiers := []string{route.DeviceID}
	if h.DeviceManager != nil {
		if instance, resolvedID, err := h.DeviceManager.ResolveDevice(route.DeviceID); err == nil {
			identifiers = append(identifiers, resolvedID)
			if instance != nil {
				identifiers = append(identifiers, instance.ID(), instance.JID())
			}
		}
	}
	tried := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		identifier = strings.TrimSpace(identifier)
		if _, done := tried[identifier]; done || identifier == "" {
			continue
		}
		tried[identifier] = struct{}{}
		cfg, err := h.ChatStorageRepo.GetChatwootDeviceConfigByIdentifier(identifier)
		if err != nil {
			return "", fmt.Errorf("load device config: %w", err)
		}
		if cfg == nil {
			continue
		}
		if strings.TrimSpace(cfg.WebhookSecret) != "" {
			return cfg.WebhookSecret, nil
		}
		break
	}
	return config.ChatwootWebhookSecret, nil
}

// anyChatwootDeviceWebhookSecret reports whether any device config carries its
// own webhook secret.
func (h *ChatwootHandler) anyChatwootDeviceWebhookSecret() (bool, error) {
	if h == nil || h.ChatStorageRepo == nil {
		return false, nil
	}
	configs, err := h.ChatStorageRepo.ListChatwootDeviceConfigs()
	if err != nil {
		return false, fmt.Errorf("list device configs: %w", err)
	}
	for _, cfg := range configs {
		if cfg != nil && strings.TrimSpace(cfg.WebhookSecret) != "" {
			return true, nil
		}
	}
	return false, nil
}

// chatwootWebhookRouteGate is the one authorization decision for acting on a
// Chatwoot webhook (WhatsApp send or revoke, mark-as-read, Chatwoot note,
// stored link). It must be called with the final route, after resolution and
// any forced override, and before anything is acted on. It returns 0 to
// proceed, or the HTTP status to answer with instead.
//
// The request must verify against the secret of the device the delivery is
// routed to, whichever endpoint it arrived on: a request that only satisfies
// the global secret is refused for a device that has its own, and one device's
// secret cannot deliver through another device.
//
// When nothing resolves and there is no global secret, no secret applies and
// the request is acknowledged as before (nothing is acted on for it) - unless
// some device has its own secret. Then it is answered like a wrong secret, so
// the status does not tell a caller which account/inbox pairs are protected.
//
// A Chatwoot-signed request seen before inside the tolerance window is
// authentic but already handled: it is acknowledged (200) and not acted on.
func (h *ChatwootHandler) chatwootWebhookRouteGate(c fiber.Ctx, route chatwootWebhookRoute) int {
	secret, err := h.chatwootRouteWebhookSecret(route)
	if err != nil {
		// Fail closed: it is unknown whether the device has its own secret,
		// and the global one may be empty.
		logrus.Errorf("Chatwoot Webhook: cannot authenticate request: %v", err)
		return fiber.StatusServiceUnavailable
	}
	if strings.TrimSpace(secret) == "" && route.Unroutable {
		protected, err := h.anyChatwootDeviceWebhookSecret()
		if err != nil {
			logrus.Errorf("Chatwoot Webhook: cannot authenticate request: %v", err)
			return fiber.StatusServiceUnavailable
		}
		if protected {
			logrus.Warn("Chatwoot Webhook: Rejected request that resolves to no device while devices have their own webhook secret")
			return fiber.StatusUnauthorized
		}
	}
	if !chatwootWebhookAuthorizedWith(c, secret) {
		logrus.Warn("Chatwoot Webhook: Rejected request not signed with the routed device's webhook secret")
		return fiber.StatusUnauthorized
	}
	if replayed, _ := c.Locals(chatwootReplayedKey{}).(bool); replayed {
		logrus.Warnf("Chatwoot Webhook: Ignoring repeated delivery of a signed request (%s seen before)", chatwootSignatureHeader)
		return fiber.StatusOK
	}
	return 0
}

// chatwootReplayedKey is the request-local flag set when the request's
// Chatwoot signature was already seen inside the tolerance window.
type chatwootReplayedKey struct{}

// chatwootReplayGuardMaxEntries bounds the memory of the replay guard. Only
// signatures that verified are remembered, so it cannot be filled by a caller
// without a secret; past the bound the oldest entries are forgotten first.
const chatwootReplayGuardMaxEntries = 1 << 16

type chatwootReplayKey struct {
	timestamp int64
	digest    [sha256.Size]byte
}

type chatwootReplayEntry struct {
	key     chatwootReplayKey
	expires time.Time
}

// chatwootReplayGuard remembers the (timestamp, signature) pairs of verified
// Chatwoot-signed requests for as long as the timestamp check would still
// accept them, so a captured request cannot be delivered a second time.
// It is in-process memory: it does not survive a restart and is not shared
// between instances.
type chatwootReplayGuard struct {
	mu    sync.Mutex
	max   int
	seen  map[chatwootReplayKey]struct{}
	order []chatwootReplayEntry // insertion order, which is also expiry order
}

var chatwootReplays = &chatwootReplayGuard{max: chatwootReplayGuardMaxEntries}

// seenBefore records a verified signature and reports whether it was already
// recorded. The pair must have passed verifyChatwootSignature.
func (g *chatwootReplayGuard) seenBefore(timestamp, signature string, now time.Time, tolerance time.Duration) bool {
	var key chatwootReplayKey
	seconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(signature), "sha256="))
	if err != nil || len(digest) != len(key.digest) {
		return false
	}
	key.timestamp = seconds
	copy(key.digest[:], digest)

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen == nil {
		g.seen = make(map[chatwootReplayKey]struct{})
	}
	// Forget what the timestamp check now rejects by itself.
	drop := 0
	for drop < len(g.order) && !g.order[drop].expires.After(now) {
		delete(g.seen, g.order[drop].key)
		drop++
	}
	g.order = g.order[drop:]

	if _, ok := g.seen[key]; ok {
		return true
	}
	for len(g.order) > 0 && len(g.order) >= g.max {
		delete(g.seen, g.order[0].key)
		g.order = g.order[1:]
	}
	g.seen[key] = struct{}{}
	// A timestamp may be up to tolerance ahead of this clock and is then
	// accepted until tolerance past it, so twice the tolerance from now covers
	// every accepted timestamp and keeps expiry in insertion order.
	g.order = append(g.order, chatwootReplayEntry{key: key, expires: now.Add(2 * tolerance)})
	return false
}

func (g *chatwootReplayGuard) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen = nil
	g.order = nil
}
