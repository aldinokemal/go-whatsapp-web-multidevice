package middleware

import (
	"strconv"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

// BrowserOriginGuard rejects cross-site browser requests on state-changing
// routes, including GET handlers that mutate (logout, reconnect, and
// invite-link reset). API clients that omit Origin and Referer are allowed
// so curl and server integrations keep working. When either header is
// present it must match the trusted-origin allowlist. A browser Sec-Fetch-Site
// of cross-site with neither header is also rejected.
func BrowserOriginGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !stateChangingRequest(c) || browserSourceAllowed(c) {
			return c.Next()
		}
		return c.Status(fiber.StatusForbidden).JSON(utils.ResponseData{
			Status:  fiber.StatusForbidden,
			Code:    "CROSS_SITE_REQUEST_BLOCKED",
			Message: "browser request origin is not allowed",
		})
	}
}

func browserSourceAllowed(c fiber.Ctx) bool {
	return config.BrowserSourceAllowed(c.Scheme(), c.Host(), c.Get(fiber.HeaderOrigin), c.Get(fiber.HeaderReferer), c.Get("Sec-Fetch-Site"))
}

func stateChangingRequest(c fiber.Ctx) bool {
	switch strings.ToUpper(c.Method()) {
	case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
		return true
	case fiber.MethodGet:
		return mutatingGET(c)
	default:
		return false
	}
}

func mutatingGET(c fiber.Ctx) bool {
	switch requestPath(c) {
	case "/app/logout", "/app/reconnect":
		return true
	case "/group/invite-link":
		reset, err := strconv.ParseBool(strings.TrimSpace(c.Query("reset")))
		return err == nil && reset
	default:
		return false
	}
}

func requestPath(c fiber.Ctx) string {
	path := c.Path()
	base := strings.TrimRight(strings.TrimSpace(config.AppBasePath), "/")
	if base != "" && (path == base || strings.HasPrefix(path, base+"/")) {
		path = strings.TrimPrefix(path, base)
	}
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
