// Package mcpauth connects WTF's users to the MCP OAuth server and the /mcp
// endpoint: WTF users are the OAuth subjects (their stable user id, never a
// phone number), the browser's WTF session signs them in to the consent page,
// and /mcp accepts an OAuth access token or a personal API key, nothing else.
package mcpauth

import (
	"context"
	"crypto/subtle"
	"net/url"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
)

// Authenticator is oauth.SessionAuthenticator over WTF's cookie sessions.
type Authenticator struct {
	Auth  *auth.Manager
	Store *store.Store
	Web   web.Deps
}

var _ oauth.SessionAuthenticator = (*Authenticator)(nil)

// Subject is the id of the user the browser's session cookie names. Only a
// cookie session counts: the global session middleware has already run, and
// an API key never signs anyone in to the consent page.
func (a *Authenticator) Subject(c fiber.Ctx) (string, bool) {
	if a.Auth.AuthenticatedBy(c) != "session" {
		return "", false
	}
	u, ok := auth.UserFrom(c.Context())
	if !ok || u.Status != store.StatusActive {
		return "", false
	}
	return u.ID, true
}

// SignInURL sends an anonymous browser to sign in and back to next, which
// must be a local path (issuer and site share an origin).
func (a *Authenticator) SignInURL(next string) string {
	return "/signin?next=" + url.QueryEscape(auth.SafeNext(next))
}

func (a *Authenticator) CSRFToken(c fiber.Ctx) string { return a.Auth.CSRFToken(c) }

// VerifyCSRF compares the form's csrf field with this session's token.
func (a *Authenticator) VerifyCSRF(c fiber.Ctx) bool {
	want := a.Auth.CSRFToken(c)
	if want == "" {
		return false
	}
	got := strings.Clone(c.FormValue("csrf"))
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// RenderConsent draws consent.html with form-action widened to the validated
// redirect origin only.
func (a *Authenticator) RenderConsent(c fiber.Ctx, v oauth.ConsentView, formActionOrigin string) error {
	return web.RenderStandalone(c, a.Web, "consent.html", "Allow access", v, formActionOrigin)
}

// Active re-reads the user: anything but an active account is refused, so
// disabling a person ends their tokens on the next request.
func (a *Authenticator) Active(ctx context.Context, subject string) bool {
	if subject == "" {
		return false
	}
	u, err := a.Store.UserByID(ctx, subject)
	return err == nil && u != nil && u.Status == store.StatusActive
}
