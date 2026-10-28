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
	case fiber.MethodGet, fiber.MethodHead:
		// Fiber copies GET handlers onto HEAD and matches paths
		// case-insensitively with trailing slashes when strict routing is off.
		return mutatingGET(c)
	default:
		return false
	}
}

func mutatingGET(c fiber.Ctx) bool {
	switch canonicalRequestPath(c.Path(), config.AppBasePath) {
	case "/app/logout", "/app/reconnect":
		return true
	case "/group/invite-link":
		return inviteLinkReset(c)
	default:
		return false
	}
}

// canonicalRequestPath strips the configured base path, one trailing slash,
// and letter case so the guard sees the same route Fiber will run.
func canonicalRequestPath(path, base string) string {
	if path == "" {
		path = "/"
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base != "" && len(path) >= len(base) && strings.EqualFold(path[:len(base)], base) {
		rest := path[len(base):]
		if rest == "" || strings.HasPrefix(rest, "/") {
			path = rest
		}
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}
	path = strings.ToLower(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// inviteLinkReset reports whether any reset query value would make
// GetGroupInviteLink regenerate the link. gofiber/schema convertBool treats
// the exact string "on" and strconv.ParseBool truths as true, and matches the
// key case-insensitively. Any truthy value fails closed, including when a
// later value is false, so a repeated key cannot hide the reset.
func inviteLinkReset(c fiber.Ctx) bool {
	reset := false
	c.RequestCtx().QueryArgs().VisitAll(func(key, value []byte) {
		if !strings.EqualFold(string(key), "reset") {
			return
		}
		if schemaBoolTrue(string(value)) {
			reset = true
		}
	})
	return reset
}

// schemaBoolTrue matches gofiber/schema convertBool's true results:
// the exact string "on", plus 1, t, T, true, TRUE, and True.
func schemaBoolTrue(value string) bool {
	if value == "on" {
		return true
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}
