package web

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// RenderStandalone draws one of WTF's own templates for a page another
// package serves (the OAuth consent page). It is the usual page render, with
// one exception: when formActionOrigin is a bare http(s) origin, the page's
// form-action also allows it. The consent form posts to itself and the
// handler then redirects to the client's callback on another origin, and
// browsers apply form-action to that redirect too; without the exception,
// Approve silently does nothing. The caller must pass only an origin it has
// already validated (the matched redirect URI's), never a raw request value.
func RenderStandalone(c fiber.Ctx, d Deps, name, title string, data any, formActionOrigin string) error {
	policy := csp
	if o := cleanOrigin(formActionOrigin); o != "" {
		policy = strings.Replace(csp, "form-action 'self'", "form-action 'self' "+o, 1)
	}
	return d.renderCSP(c, fiber.StatusOK, name, title, data, policy)
}

// cleanOrigin returns raw when it is exactly scheme://host[:port] with an
// http or https scheme, else "". Anything that could add a second CSP source
// or directive (spaces, semicolons, quotes, a path) is refused.
func cleanOrigin(raw string) string {
	if raw == "" || strings.ContainsAny(raw, " ;,'\"\t\r\n") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ""
	}
	if (&url.URL{Scheme: u.Scheme, Host: u.Host}).String() != raw {
		return ""
	}
	return raw
}
