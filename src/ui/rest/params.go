package rest

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// pathDeviceID returns the :device_id route parameter, path-unescaped so clients
// may safely use encodeURIComponent. Fiber v3 leaves percent-escapes in Params.
func pathDeviceID(c fiber.Ctx) string {
	raw := strings.TrimSpace(c.Params("device_id"))
	if raw == "" {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}
