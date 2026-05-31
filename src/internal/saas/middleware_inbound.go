package saas

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"
)

// InboundAuthMiddleware gates the bot's /send/* endpoints against the
// shared `X-Saas-Token` secret. The SaaS sets the header on every
// outbound call; mismatches return 401 BEFORE any whatsmeow work.
//
// When SaaS is not configured, the middleware is a no-op so generic
// gowa users keep their existing auth pipeline (basic auth, etc).
//
// Constant-time compare avoids the timing-oracle vulnerability that
// substr or `==` checks would expose.
func InboundAuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		cfg := Load()
		if cfg.InboundSecret == "" {
			return c.Next()
		}

		given := c.Get("X-Saas-Token")
		if given == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"ok":    false,
				"error": "saas_token_missing",
			})
		}
		if subtle.ConstantTimeCompare([]byte(given), []byte(cfg.InboundSecret)) != 1 {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"ok":    false,
				"error": "saas_token_invalid",
			})
		}
		return c.Next()
	}
}
