package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
)

// DefaultRequestTimeout is the default timeout for API requests.
// This prevents indefinite blocking when WhatsApp servers are slow or unresponsive.
const DefaultRequestTimeout = 45 * time.Second

// RequestTimeout adds a deadline to all incoming HTTP requests.
// If the handler doesn't complete within the timeout, the context is cancelled
// and whatsmeow SDK calls will return context.DeadlineExceeded.
func RequestTimeout(timeout time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), timeout)
		defer cancel()
		c.SetContext(ctx)
		return c.Next()
	}
}
