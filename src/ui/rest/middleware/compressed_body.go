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
		// Every header line counts, not only the first one c.Get returns.
		for _, value := range c.Request().Header.PeekAll(fiber.HeaderContentEncoding) {
			for _, part := range strings.Split(string(value), ",") {
				if coding := strings.ToLower(strings.TrimSpace(part)); coding != "" && coding != "identity" {
					return c.Status(fiber.StatusUnsupportedMediaType).JSON(utils.ResponseData{
						Status:  fiber.StatusUnsupportedMediaType,
						Code:    "UNSUPPORTED_CONTENT_ENCODING",
						Message: "compressed request bodies are not supported",
					})
				}
			}
		}
		return c.Next()
	}
}
