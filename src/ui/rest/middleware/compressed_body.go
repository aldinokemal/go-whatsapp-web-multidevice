package middleware

import (
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

// RejectCompressedBody answers 415 for any Content-Encoding other than
// identity. Mount it in front of routes that read a body before authenticating:
// Fiber would otherwise expand gzip, deflate, brotli, or zstd up to the
// application-wide BodyLimit before the handler can refuse the request.
func RejectCompressedBody() fiber.Handler {
	return func(c fiber.Ctx) error {
		for _, part := range strings.Split(c.Get(fiber.HeaderContentEncoding), ",") {
			if coding := strings.ToLower(strings.TrimSpace(part)); coding != "" && coding != "identity" {
				return c.Status(fiber.StatusUnsupportedMediaType).JSON(utils.ResponseData{
					Status:  fiber.StatusUnsupportedMediaType,
					Code:    "UNSUPPORTED_CONTENT_ENCODING",
					Message: "compressed request bodies are not supported",
				})
			}
		}
		return c.Next()
	}
}
