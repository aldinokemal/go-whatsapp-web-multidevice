// Package auth is WTF's browser session layer: cookie sessions, the page/API
// gates, the superadmin allowlist and CSRF.
//
// Never log a raw session id, a CSRF token or the secret from this package.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

// CookieName is the session cookie on HTTPS deployments. The __Host- prefix
// makes the browser insist on Secure, Path=/ and no Domain.
const CookieName = "__Host-wtf_session"

// DevCookieName is used only when Config.Secure is false (plain-http local
// development), because browsers refuse a __Host- cookie without Secure.
const DevCookieName = "wtf_session"

const (
	localsRaw     = "wtf_session_raw"
	localsAuthErr = "wtf_auth_err"
)

// ErrInactiveUser is returned by StartSession for a user who is not active.
var ErrInactiveUser = errors.New("auth: user is not active")

type Config struct {
	Secret      []byte
	Superadmins map[string]bool
	SessionTTL  time.Duration
	Secure      bool // false only for plain-http local development
}

type Manager struct {
	cfg Config
	st  *store.Store
}

func New(cfg Config, st *store.Store) *Manager {
	if len(cfg.Secret) < 32 {
		panic("auth: secret must be at least 32 bytes")
	}
	if cfg.SessionTTL <= 0 {
		panic("auth: SessionTTL must be positive")
	}
	return &Manager{cfg: cfg, st: st}
}

// CookieName is the session cookie name this manager reads and writes.
func (m *Manager) CookieName() string {
	if m.cfg.Secure {
		return CookieName
	}
	return DevCookieName
}

// Middleware is always mounted. It loads the user when the cookie names a live
// session of an active user; otherwise the request carries on anonymous (and a
// stale cookie is cleared). The gates below decide what anonymous may reach.
func (m *Manager) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		raw := c.Cookies(m.CookieName())
		if raw == "" {
			return c.Next()
		}
		u, err := m.st.SessionUser(c.Context(), raw, m.cfg.SessionTTL)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			// The accounts store failed: we cannot tell whether this session
			// is good. Keep the cookie, stay anonymous, and let the gates
			// answer 500 (HTML for pages, JSON for the API) rather than
			// signing the visitor out over a transient fault.
			c.Locals(localsAuthErr, true)
			return c.Next()
		}
		if err != nil || u == nil || u.Status != store.StatusActive {
			m.clearCookie(c)
			return c.Next()
		}
		// Re-issue the cookie so the browser's expiry slides with the server's.
		m.setCookie(c, raw)
		c.Locals(localsRaw, raw)
		c.Locals("wtf_user", u)
		c.SetContext(WithUser(c.Context(), u))
		return c.Next()
	}
}

// RequirePage sends anonymous browsers to sign-in. The signin handler must
// validate next as a local path before redirecting to it.
func (m *Manager) RequirePage() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := UserFrom(c.Context()); ok {
			return c.Next()
		}
		if storeFailed(c) {
			return fiber.NewError(fiber.StatusInternalServerError, "Something went wrong on our side. Please try again.")
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/signin?next=" + url.QueryEscape(c.OriginalURL()))
	}
}

// RequireAPI answers anonymous machine requests with a JSON 401.
func (m *Manager) RequireAPI() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := UserFrom(c.Context()); ok {
			return c.Next()
		}
		if storeFailed(c) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"status": fiber.StatusInternalServerError, "code": "INTERNAL_ERROR", "message": "could not check your session",
			})
		}
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"status": fiber.StatusUnauthorized, "code": "UNAUTHENTICATED", "message": "sign in required",
		})
	}
}

func storeFailed(c fiber.Ctx) bool {
	failed, _ := c.Locals(localsAuthErr).(bool)
	return failed
}

// IsSuperadmin consults the env allowlist only, never the database.
func (m *Manager) IsSuperadmin(u *store.User) bool {
	return u != nil && u.Status == store.StatusActive && m.cfg.Superadmins[u.Phone]
}

// RequireSuperadmin answers 404 to everyone else so admin pages stay hidden.
func (m *Manager) RequireSuperadmin() fiber.Handler {
	return func(c fiber.Ctx) error {
		u, _ := UserFrom(c.Context())
		if !m.IsSuperadmin(u) {
			return fiber.ErrNotFound
		}
		return c.Next()
	}
}

// StartSession signs u in. Any session the request already carried is revoked
// first, so a sign-in always yields a fresh session id.
func (m *Manager) StartSession(c fiber.Ctx, u *store.User) error {
	if u == nil || u.Status != store.StatusActive {
		return ErrInactiveUser
	}
	if old, ok := c.Locals(localsRaw).(string); ok && old != "" {
		if err := m.st.DeleteSession(c.Context(), old); err != nil {
			return err
		}
	}
	raw, err := m.st.CreateSession(c.Context(), u.ID, c.Get(fiber.HeaderUserAgent), m.cfg.SessionTTL)
	if err != nil {
		return err
	}
	_ = m.st.TouchLogin(c.Context(), u.ID)
	m.setCookie(c, raw)
	c.Locals(localsRaw, raw)
	c.Locals("wtf_user", u)
	c.SetContext(WithUser(c.Context(), u))
	return nil
}

// EndSession deletes the session server side and clears the cookie.
func (m *Manager) EndSession(c fiber.Ctx) error {
	if raw, ok := c.Locals(localsRaw).(string); ok && raw != "" {
		if err := m.st.DeleteSession(c.Context(), raw); err != nil {
			return err
		}
	}
	c.Locals(localsRaw, "")
	c.Locals("wtf_user", nil)
	c.SetContext(WithUser(c.Context(), nil))
	m.clearCookie(c)
	return nil
}

func (m *Manager) setCookie(c fiber.Ctx, raw string) {
	c.Cookie(&fiber.Cookie{
		Name: m.CookieName(), Value: raw, Path: "/", HTTPOnly: true, Secure: m.cfg.Secure,
		SameSite: fiber.CookieSameSiteLaxMode, MaxAge: int(m.cfg.SessionTTL.Seconds()),
	})
}

func (m *Manager) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{Name: m.CookieName(), Value: "", Path: "/", HTTPOnly: true, Secure: m.cfg.Secure,
		SameSite: fiber.CookieSameSiteLaxMode, MaxAge: -1})
}

// CSRFToken is HMAC(secret, session) so it needs no storage and dies with the session.
func (m *Manager) CSRFToken(c fiber.Ctx) string {
	raw, _ := c.Locals(localsRaw).(string)
	if raw == "" {
		return ""
	}
	mac := hmac.New(sha256.New, m.cfg.Secret)
	mac.Write([]byte("csrf:" + raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCSRF requires the session's token (form field csrf or header
// X-CSRF-Token) on every method other than GET, HEAD and OPTIONS. Requests
// without a session pass through: the gates refuse them.
func (m *Manager) VerifyCSRF() fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}
		want := m.CSRFToken(c)
		if want == "" { // no session: the gates decide
			return c.Next()
		}
		got := c.Get("X-CSRF-Token")
		if got == "" {
			got = c.FormValue("csrf")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			return fiber.NewError(fiber.StatusForbidden, "This form has expired. Reload the page and try again.")
		}
		return c.Next()
	}
}
