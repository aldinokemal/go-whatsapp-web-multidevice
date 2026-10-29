package config

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// PublishedWhatsappWebhookSecret is the historical default documented in
// older releases. Receivers that still use it cannot tell a real delivery
// from a forged body, so it is never used as an HMAC key.
const PublishedWhatsappWebhookSecret = "secret"

// PublishedExampleWebhookSecret is the example key older .env.example files
// shipped. It is public for the same reason and is never used as an HMAC key.
const PublishedExampleWebhookSecret = "super-secret-key"

// PublishedPlaceholderWebhookSecret is a placeholder the README once showed.
// It is public for the same reason and is never used as an HMAC key.
const PublishedPlaceholderWebhookSecret = "your-private-key"

// WebhookSecretUnusable reports whether secret is empty or a published
// value. Callers must not sign webhook bodies with those values.
func WebhookSecretUnusable(secret string) bool {
	switch strings.TrimSpace(secret) {
	case "", PublishedWhatsappWebhookSecret, PublishedExampleWebhookSecret, PublishedPlaceholderWebhookSecret:
		return true
	default:
		return false
	}
}

// UseEphemeralWebhookSecret replaces an unusable WhatsappWebhookSecret with
// a random per-process key. A configured secret is left unchanged. The
// replacement is not persisted and changes the next time the process starts.
func UseEphemeralWebhookSecret() (replaced bool, err error) {
	if !WebhookSecretUnusable(WhatsappWebhookSecret) {
		return false, nil
	}
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return false, err
	}
	WhatsappWebhookSecret = hex.EncodeToString(buf)
	return true, nil
}
