// Package saas adds SaaS_Construction integration on top of the upstream
// go-whatsapp-web-multidevice. It signs outbound webhooks, gates the
// /send/* endpoints with a shared secret, exposes /healthz, and drops
// group messages before they ever hit the SaaS webhook.
//
// All code is gated on env-var presence: when SAAS_WEBHOOK_URL is empty
// the upstream behaviour is preserved unchanged. This keeps the fork
// usable as a generic gowa instance in environments without the SaaS.
package saas

import (
	"os"
	"sync"
	"time"
)

// Config holds the SaaS integration knobs, loaded once from env on first
// access. Reading is concurrency-safe.
type Config struct {
	// OrgID is the SaaS organization UUID this bot container serves.
	// Stamped as the `X-Saas-Org-Id` header on every outbound webhook so
	// the SaaS doesn't have to infer it from the WhatsApp number.
	OrgID string
	// WebhookURL is the SaaS endpoint the bot POSTs inbound events to.
	// When empty, SaaS integration is fully disabled and upstream
	// forwarding behaviour is preserved.
	WebhookURL string
	// WebhookSecret is the shared secret used to HMAC-sign outbound
	// webhooks (`X-Bot-Token-Hmac` header).
	WebhookSecret string
	// InboundSecret is the shared secret the SaaS sends on every
	// /send/* request (`X-Saas-Token` header). Used to gate the
	// bot's send endpoints.
	InboundSecret string
	// PairedAt is set once the bot has reported active to the SaaS;
	// surfaced in /healthz.
	PairedAt time.Time
}

var (
	cfgOnce sync.Once
	cfg     Config
)

// Load returns the lazily-loaded config snapshot. Safe to call from any
// goroutine.
func Load() Config {
	cfgOnce.Do(func() {
		cfg = Config{
			OrgID:         os.Getenv("ORG_ID"),
			WebhookURL:    os.Getenv("SAAS_WEBHOOK_URL"),
			WebhookSecret: os.Getenv("SAAS_WEBHOOK_SECRET"),
			InboundSecret: os.Getenv("SAAS_INBOUND_SECRET"),
		}
	})
	return cfg
}

// Enabled reports whether SaaS integration is configured. Used as the
// kill-switch so the binary stays a vanilla gowa instance when the env
// vars are unset.
func Enabled() bool {
	c := Load()
	return c.WebhookURL != "" && c.WebhookSecret != ""
}

// MarkPaired records the time we first observed pairing — surfaced via
// /healthz. Best-effort, no persistence (server restart resets it).
func MarkPaired() {
	cfg.PairedAt = time.Now()
}
