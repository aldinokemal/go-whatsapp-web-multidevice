package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
)

var (
	ErrInvalidGrant      = errors.New("invalid grant")
	ErrInvalidTarget     = errors.New("invalid target")
	ErrInvalidToken      = errors.New("invalid token")
	ErrRefreshReuse      = errors.New("refresh token reuse detected")
	ErrRegistrationLimit = errors.New("dynamic client registration limit reached")
)

type CredentialValidator func(username, password string) bool

type Config struct {
	IssuerURL   string
	ResourceURL string
	StorageURI  string
	// Session, when set, replaces the Basic Auth form: the browser's own
	// sign-in names the subject and the authorize page asks for consent.
	Session SessionAuthenticator
	// DisableDCR turns off dynamic client registration: /oauth/register is
	// not mounted, metadata omits registration_endpoint, and a client id
	// that is not a CIMD URL is refused.
	DisableDCR bool
	// CIMD, when set, accepts client ids that are HTTPS URLs of client ID
	// metadata documents on the allowed hosts.
	CIMD *CIMDConfig
}

type Client struct {
	ID                      string
	Name                    string
	RedirectURIs            []string
	ApplicationType         string
	TokenEndpointAuthMethod string
	CreatedAt               time.Time
}

type AuthorizationGrant struct {
	ClientID      string
	Subject       string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	Scope         string
	// recordClient, when set, is written to oauth_clients (if absent) in the
	// same transaction as the code. CIMD clients have no registration; the
	// row exists only because codes and tokens reference oauth_clients.
	recordClient *Client
}

type CodeExchange struct {
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	Resource     string
	// allow, when set, must accept the grant's subject before tokens issue.
	allow func(subject string) bool
}

type RefreshExchange struct {
	ClientID     string
	RefreshToken string
	Resource     string
	// allow, when set, must accept the family's subject before tokens issue.
	allow func(subject string) bool
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	Scope        string
}

type Principal struct {
	Subject  string
	ClientID string
	Scope    string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// SessionAuthenticator lets the authorization server use an application's
// own browser sign-in instead of the Basic Auth form. With it set, the subject
// on codes and tokens is whatever Subject returns (a stable user id), the
// authorize page becomes a consent page the application draws, and a subject
// that stops being Active can no longer redeem, refresh or use tokens.
type SessionAuthenticator interface {
	// Subject is the signed-in, active user's stable id, or ok=false.
	Subject(c fiber.Ctx) (subject string, ok bool)
	// SignInURL is where to send an anonymous browser; next is the local
	// authorize URL (path and query) to come back to.
	SignInURL(next string) string
	// CSRFToken and VerifyCSRF bind the consent POST to the browser session.
	CSRFToken(c fiber.Ctx) string
	VerifyCSRF(c fiber.Ctx) bool
	// RenderConsent draws the consent page. formActionOrigin is the origin of
	// the already-validated redirect URI: the page's CSP must allow it in
	// form-action, because browsers apply form-action to the redirect that
	// follows the consent POST.
	RenderConsent(c fiber.Ctx, v ConsentView, formActionOrigin string) error
	// Active reports whether subject may still use tokens. It is checked when
	// a code is redeemed, on every refresh and on every token validation.
	Active(ctx context.Context, subject string) bool
}

// ConsentView is what the consent page shows and posts back.
type ConsentView struct {
	// ClientHost is the host of a CIMD client_id URL: the relying party to
	// name. Empty for a dynamically registered client.
	ClientHost string
	// ClientName is self-asserted by the client; never show it as the
	// relying party.
	ClientName string
	// RedirectHost is where the browser goes after consent.
	RedirectHost string
	// LoopbackOnly is true when every redirect URI the client declared is on
	// this computer (any local program can claim to be such a client).
	LoopbackOnly bool
	// Action is the path the consent form posts to.
	Action string
	// Hidden are the authorization request fields the form echoes back.
	Hidden map[string]string
	CSRF   string
}

// Grant is one connected client for one subject: a token family.
type Grant struct {
	FamilyID   string
	ClientID   string
	ClientHost string // the CIMD URL's host, or a registered client's redirect host
	CreatedAt  time.Time
	LastUsedAt time.Time // when the family last obtained tokens
}
