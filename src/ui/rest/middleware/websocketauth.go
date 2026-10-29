package middleware

import (
	"strings"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

// WebsocketQueryAuth lets browser WebSocket clients authenticate with
// ?authorization=<base64(user:pass)>, because the browser WebSocket API cannot
// set an Authorization header and userinfo in WS URLs is rejected per spec.
// The value is restored into the header before the basic-auth middleware runs.
func WebsocketQueryAuth() fiber.Handler {
	return func(c fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) && len(c.Request().Header.Peek(fiber.HeaderAuthorization)) == 0 {
			if q := c.Query("authorization"); q != "" {
				// fasthttp decodes '+' as space; base64 never contains spaces,
				// so restoring '+' is lossless.
				token := strings.ReplaceAll(q, " ", "+")
				c.Request().Header.Set(fiber.HeaderAuthorization, "Basic "+token)
			}
		}
		return c.Next()
	}
}
