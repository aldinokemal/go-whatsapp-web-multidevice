package saas

import (
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
)

// lastMessageAtUnix is updated whenever a message-bearing webhook
// payload passes through `RecordInbound`. Stored as a Unix-seconds int64
// so we can update lock-free with atomic store.
var lastMessageAtUnix int64

// RecordInbound is called from the upstream webhook fan-out for every
// payload we forward to the SaaS. It updates the timestamp surfaced
// via /healthz so operators can detect a stuck WhatsApp session.
func RecordInbound() {
	atomic.StoreInt64(&lastMessageAtUnix, time.Now().Unix())
}

// HealthHandler returns the SaaS-specific /healthz payload. Distinct
// from the upstream /health endpoint (which is "is the WhatsApp client
// connected"); /healthz answers "is this bot integrated with the SaaS
// and recently active".
func HealthHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		cfg := Load()

		body := fiber.Map{
			"status":         "ok",
			"saas_enabled":   Enabled(),
			"org_id":         cfg.OrgID,
			"paired_at":      formatTime(cfg.PairedAt),
			"last_message":   formatTimestamp(atomic.LoadInt64(&lastMessageAtUnix)),
		}
		return c.JSON(body)
	}
}

func formatTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTimestamp(unix int64) any {
	if unix == 0 {
		return nil
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}
