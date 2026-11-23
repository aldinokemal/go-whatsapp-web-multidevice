package oauth

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// Session-subject mode. With Config.Session set, the authorize endpoint no
// longer asks for a Basic Auth username and password: it asks the
// application who the browser is signed in as, sends an anonymous browser to
// sign in first, and shows a consent page. The subject on codes and tokens is
// the application's stable user id. Nothing here changes Basic mode.

// activeCheck returns the predicate token issue must pass, or nil in Basic
// mode (where every subject is a configured Basic Auth account).
func (s *Server) activeCheck(ctx context.Context) func(string) bool {
	if s.session == nil {
		return nil
	}
	return func(subject string) bool { return s.session.Active(ctx, subject) }
}

// sessionAuthorizeGET runs after the request validated: sign in first, then consent.
func (s *Server) sessionAuthorizeGET(c fiber.Ctx, req authorizationRequest, client Client) error {
	if _, ok := s.session.Subject(c); !ok {
		s.setNoStore(c)
		return c.Redirect().Status(fiber.StatusSeeOther).To(s.session.SignInURL(s.authorizeLocalURL(req)))
	}
	return s.renderConsent(c, req, client)
}

// sessionAuthorizePOST is the consent form's submission. The order matters:
// the request is re-validated (done by the caller), then the form must carry
// this session's CSRF token, then the browser must still be signed in as an
// active user, and only then is the decision read. A missing or unknown
// decision is an error, never consent.
func (s *Server) sessionAuthorizePOST(c fiber.Ctx, req authorizationRequest, client Client) error {
	if !s.session.VerifyCSRF(c) {
		return s.browserError(c, fiber.StatusForbidden, "invalid_request", "the consent form has expired; start connecting again",
			"This page has expired. Start connecting again from the app.")
	}
	subject, ok := s.session.Subject(c)
	if !ok {
		s.setNoStore(c)
		return c.Redirect().Status(fiber.StatusSeeOther).To(s.session.SignInURL(s.authorizeLocalURL(req)))
	}
	switch req.Decision {
	case "approve":
		return s.issueCodeAndRedirect(c, req, client, subject)
	case "deny":
		return s.authorizationError(c, req, "access_denied", "the user declined the request")
	default:
		return s.browserError(c, fiber.StatusBadRequest, "invalid_request", "decision must be approve or deny",
			"Choose Allow or Cancel to continue.")
	}
}

// browserError answers an authorize request that cannot go back to the
// client: the application's error page in session mode when it draws one,
// else the JSON OAuth error.
func (s *Server) browserError(c fiber.Ctx, status int, code, description, message string) error {
	if r, ok := s.session.(SessionErrorRenderer); ok && s.session != nil {
		s.setNoStore(c)
		return r.RenderAuthorizeError(c, status, message)
	}
	return oauthError(c, status, code, description)
}

func (s *Server) renderConsent(c fiber.Ctx, req authorizationRequest, client Client) error {
	s.setNoStore(c)
	view := ConsentView{
		ClientName:   client.Name,
		RedirectHost: redirectHost(req.RedirectURI),
		LoopbackOnly: allLoopback(client.RedirectURIs),
		Action:       s.issuerEndpointPath("/oauth/authorize"),
		CSRF:         s.session.CSRFToken(c),
		Hidden: map[string]string{
			"response_type":         req.ResponseType,
			"client_id":             req.ClientID,
			"redirect_uri":          req.RedirectURI,
			"code_challenge":        req.CodeChallenge,
			"code_challenge_method": req.CodeChallengeMethod,
			"resource":              req.Resource,
			"scope":                 req.Scope,
			"state":                 req.State,
		},
	}
	if client.ApplicationType == applicationTypeCIMD {
		view.ClientHost = clientDisplayHost(client.ID, nil)
	}
	// req.RedirectURI has passed redirect matching, so its origin is one the
	// client declared (for a loopback redirect, only the port came from the
	// request). It is the one origin the consent POST may redirect to.
	return s.session.RenderConsent(c, view, redirectOrigin(req.RedirectURI))
}

// authorizeLocalURL is the authorize URL (path and query) to return to after
// sign-in. It is local: issuer and site share an origin.
func (s *Server) authorizeLocalURL(req authorizationRequest) string {
	q := url.Values{}
	for k, v := range map[string]string{
		"response_type": req.ResponseType, "client_id": req.ClientID, "redirect_uri": req.RedirectURI,
		"code_challenge": req.CodeChallenge, "code_challenge_method": req.CodeChallengeMethod,
		"resource": req.Resource, "scope": req.Scope, "state": req.State,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return s.issuerEndpointPath("/oauth/authorize") + "?" + q.Encode()
}

// ValidateAccessToken checks a bearer access token against this server's
// resource and, in session mode, that its subject is still active.
func (s *Server) ValidateAccessToken(ctx context.Context, raw string) (Principal, error) {
	p, err := s.store.validateAccessToken(ctx, raw, s.resource.String(), s.now())
	if err != nil {
		return Principal{}, err
	}
	if s.session != nil && !s.session.Active(ctx, p.Subject) {
		return Principal{}, ErrInvalidToken
	}
	return p, nil
}

// Unauthorized answers 401 with this server's Bearer challenge (pointing at
// the protected-resource metadata) and no Basic advertisement.
func (s *Server) Unauthorized(c fiber.Ctx, invalidToken bool) error {
	return s.mcpUnauthorized(c, false, invalidToken)
}

// ListGrants lists the subject's connected clients, one per token family.
func (s *Server) ListGrants(ctx context.Context, subject string) ([]Grant, error) {
	return s.store.listGrants(ctx, subject, s.now())
}

// RevokeGrant disconnects one of the subject's clients at once.
// ErrInvalidGrant when the family is not the subject's.
func (s *Server) RevokeGrant(ctx context.Context, subject, familyID string) error {
	return s.store.revokeGrant(ctx, subject, familyID, s.now())
}

// RevokeSubject revokes every token and unused code the subject holds.
func (s *Server) RevokeSubject(ctx context.Context, subject string) error {
	return s.store.revokeSubject(ctx, subject, s.now())
}

// clientDisplayHost names a client the way a person can check it: the host
// of a CIMD client_id URL, else the host of its first redirect URI (a
// registered client's id is opaque and its name is self-asserted).
func clientDisplayHost(clientID string, redirects []string) string {
	if u, err := url.Parse(clientID); err == nil && u.Scheme == "https" && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	for _, r := range redirects {
		if u, err := url.Parse(r); err == nil && u.Hostname() != "" {
			return strings.ToLower(u.Hostname())
		}
	}
	return "an app"
}

func redirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// isLoopbackHost: the names a native client's local callback can use.
func isLoopbackHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func allLoopback(redirects []string) bool {
	if len(redirects) == 0 {
		return false
	}
	for _, r := range redirects {
		u, err := url.Parse(r)
		if err != nil || !isLoopbackHost(u.Hostname()) {
			return false
		}
	}
	return true
}
