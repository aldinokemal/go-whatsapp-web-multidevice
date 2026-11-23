package mcpauth

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
)

// Middleware guards /mcp. Mount it on /mcp before the MCP route itself, so it
// runs before any body parsing, protocol-version check or rate limit: a
// connector only learns where to sign in from a 401 with WWW-Authenticate,
// and anything that answers earlier hides it.
//
// One principal per request, from the Authorization header only:
//   - "Bearer wtfk_...": a personal API key (Claude Code convenience).
//   - any other bearer: an OAuth access token for this MCP resource.
//   - anything else, or a credential that fails: 401 with the Bearer
//     challenge naming the protected-resource metadata (no Basic).
//
// The session cookie never counts here. In every case the user is re-read
// and must be active, so revoking a key, a grant or a person takes effect on
// the next call.
func Middleware(srv *oauth.Server, am *auth.Manager, st *store.Store) fiber.Handler {
	return func(c fiber.Ctx) error {
		am.ForgetSession(c)
		header := c.Get(fiber.HeaderAuthorization)
		if raw, ok := auth.BearerAPIKey(header); ok {
			u, keyID, err := st.UserByAPIKey(c.Context(), raw)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"status": fiber.StatusInternalServerError, "code": "INTERNAL_ERROR", "message": "could not check your API key",
				})
			}
			if err != nil || u == nil || u.Status != store.StatusActive {
				return srv.Unauthorized(c, true)
			}
			am.UseAPIKeyUser(c, u)
			_ = st.TouchAPIKey(c.Context(), keyID)
			return c.Next()
		}
		scheme, token, _ := strings.Cut(strings.TrimSpace(header), " ")
		token = strings.TrimSpace(token)
		if !strings.EqualFold(scheme, "Bearer") || token == "" {
			return srv.Unauthorized(c, false)
		}
		p, err := srv.ValidateAccessToken(c.Context(), token)
		if err != nil {
			return srv.Unauthorized(c, true)
		}
		u, err := st.UserByID(c.Context(), p.Subject)
		if err != nil || u == nil || u.Status != store.StatusActive {
			return srv.Unauthorized(c, true)
		}
		c.SetContext(auth.WithUser(c.Context(), u))
		return c.Next()
	}
}
