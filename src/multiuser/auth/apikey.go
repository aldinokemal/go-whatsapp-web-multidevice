package auth

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

const viaAPIKey = "api_key"

// APIKey loads the user named by an "Authorization: Bearer wtfk_..." header.
// Mount it at the head of the REST API gate chain only (before RequireAPI,
// RequireSameOriginAPI and VerifyCSRF), never on page routes: a key must not
// sign anyone in to a page.
//
//   - No Authorization header, another scheme, or a bearer without the wtfk_
//     prefix: passed through untouched (the cookie, if any, decides; an OAuth
//     bearer is only meaningful on /mcp, which has its own guard).
//   - A wtfk_ bearer that is unknown, revoked, or whose user is not active: a
//     JSON 401, even when a good session cookie rides along.
//   - A good key: the cookie principal is dropped (and its refreshed cookie is
//     not sent back), the key's user is loaded instead, and the request is
//     marked as key-authenticated. Because the raw session id is then empty,
//     RequireSameOriginAPI and VerifyCSRF, which judge cookie sessions only,
//     let it through: a key is not something a browser sends by itself.
//
// The raw key is never logged or kept; only its hash is looked up.
func (m *Manager) APIKey() fiber.Handler {
	return func(c fiber.Ctx) error {
		raw, ok := bearerAPIKey(c.Get(fiber.HeaderAuthorization))
		if !ok {
			return c.Next()
		}
		u, keyID, err := m.st.UserByAPIKey(c.Context(), raw)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"status": fiber.StatusInternalServerError, "code": "INTERNAL_ERROR", "message": "could not check your API key",
			})
		}
		if err != nil || u == nil || u.Status != store.StatusActive {
			c.Set(fiber.HeaderWWWAuthenticate, `Bearer realm="wtf"`)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"status": fiber.StatusUnauthorized, "code": "INVALID_API_KEY", "message": "this API key is not valid",
			})
		}
		// One principal per request: forget the cookie session entirely.
		c.Response().Header.DelCookie(m.CookieName())
		c.Locals(keyAuthErr, false)
		c.Locals(keyRaw, "")
		c.Locals(keyUser, u)
		c.Locals(keyVia, viaAPIKey)
		c.SetContext(WithUser(c.Context(), u))
		_ = m.st.TouchAPIKey(c.Context(), keyID) // throttled in the store; a failure must not refuse the call
		return c.Next()
	}
}

// bearerAPIKey returns the key from "Bearer wtfk_..." (scheme case-insensitive).
func bearerAPIKey(h string) (string, bool) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(h), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, store.APIKeyPrefix) {
		return "", false
	}
	return rest, true
}

// AuthenticatedBy says how this request's user was loaded: "api_key",
// "session" (the cookie), or "" (anonymous).
func (m *Manager) AuthenticatedBy(c fiber.Ctx) string {
	if via, _ := c.Locals(keyVia).(string); via == viaAPIKey {
		return viaAPIKey
	}
	if raw, _ := c.Locals(keyRaw).(string); raw != "" {
		return "session"
	}
	return ""
}

// ForgetSession drops whatever the session middleware loaded from the cookie
// (the user, the raw session id) and does not send the refreshed cookie back.
// /mcp calls it first: that endpoint is authenticated by a bearer token only,
// so a cookie must never count there (no CSRF exposure on a JSON-RPC route a
// browser could be tricked into posting to).
func (m *Manager) ForgetSession(c fiber.Ctx) {
	c.Response().Header.DelCookie(m.CookieName())
	c.Locals(keyAuthErr, false)
	c.Locals(keyRaw, "")
	c.Locals(keyUser, nil)
	c.Locals(keyVia, "")
	c.SetContext(WithUser(c.Context(), nil))
}

// UseAPIKeyUser makes u, loaded from an API key outside APIKey() (the /mcp
// guard), this request's only principal.
func (m *Manager) UseAPIKeyUser(c fiber.Ctx, u *store.User) {
	m.ForgetSession(c)
	c.Locals(keyUser, u)
	c.Locals(keyVia, viaAPIKey)
	c.SetContext(WithUser(c.Context(), u))
}

// BearerAPIKey returns the raw key from an "Authorization: Bearer wtfk_..."
// header value, or ok=false when the header holds no WTF key.
func BearerAPIKey(header string) (string, bool) { return bearerAPIKey(header) }
