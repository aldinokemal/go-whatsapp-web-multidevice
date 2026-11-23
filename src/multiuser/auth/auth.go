// Package auth is WTF's browser session layer: cookie sessions, the page/API
// gates, the superadmin allowlist, CSRF and same-origin checks.
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
	"strings"
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

// localsKey types the request Locals this package sets, so no other package
// (and no form or template field) can collide with or spoof them.
//
// The raw session id lives in Locals. NEVER enable fiber's PassLocalsToViews:
// it would hand the raw session id (and the user) to every template.
type localsKey int

const (
	keyRaw     localsKey = iota // raw session id (string)
	keyUser                     // *store.User
	keyAuthErr                  // bool: the accounts store failed while loading the session
)

// ErrInactiveUser is returned by StartSession for a user who is not active.
var ErrInactiveUser = errors.New("auth: user is not active")

// ErrStoreFault is returned by StartSession when the session lookup for this
// request failed: we refuse to mint a new session rather than guess.
var ErrStoreFault = errors.New("auth: accounts store unavailable")

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

// New copies the secret and the superadmin allowlist, so a caller mutating
// either afterwards cannot change who is a superadmin or forge CSRF tokens.
func New(cfg Config, st *store.Store) *Manager {
	if len(cfg.Secret) < 32 {
		panic("auth: secret must be at least 32 bytes")
	}
	if cfg.SessionTTL <= 0 {
		panic("auth: SessionTTL must be positive")
	}
	cfg.Secret = append([]byte(nil), cfg.Secret...)
	admins := make(map[string]bool, len(cfg.Superadmins))
	for phone, ok := range cfg.Superadmins {
		if !ok {
			continue
		}
		if p, err := store.NormalizePhone(phone); err == nil {
			admins[p] = true
		}
	}
	cfg.Superadmins = admins
	return &Manager{cfg: cfg, st: st}
}

// Secure reports whether cookies are issued with the Secure flag (HTTPS).
func (m *Manager) Secure() bool { return m.cfg.Secure }

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
			c.Locals(keyAuthErr, true)
			return c.Next()
		}
		if err != nil || u == nil || u.Status != store.StatusActive {
			m.clearCookie(c)
			return c.Next()
		}
		// Re-issue the cookie so the browser's expiry slides with the server's.
		m.setCookie(c, raw)
		c.Locals(keyRaw, raw)
		c.Locals(keyUser, u)
		c.SetContext(WithUser(c.Context(), u))
		return c.Next()
	}
}

func storeFailed(c fiber.Ctx) bool {
	failed, _ := c.Locals(keyAuthErr).(bool)
	return failed
}

func errStorePage() error {
	return fiber.NewError(fiber.StatusInternalServerError, "Something went wrong on our side. Please try again.")
}

// RequirePage sends anonymous browsers to sign-in. An htmx request gets a
// 401 with HX-Redirect instead of a 303, so htmx navigates the whole page
// rather than swapping the sign-in page into a fragment.
func (m *Manager) RequirePage() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := UserFrom(c.Context()); ok {
			return c.Next()
		}
		if storeFailed(c) {
			return errStorePage()
		}
		next := c.Path()
		if q := string(c.Request().URI().QueryString()); q != "" {
			next += "?" + q
		}
		target := "/signin?next=" + url.QueryEscape(SafeNext(next))
		if c.Get("HX-Request") == "true" {
			c.Set("HX-Redirect", target)
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To(target)
	}
}

// SafeNext returns next when it is a local path on this site, else "/".
// It refuses control characters and backslashes anywhere (browsers treat "\"
// as "/" and strip tabs/newlines, turning "/\evil.com" or "/\t/evil.com" into
// "//evil.com"), anything with a scheme or host, and paths that do not start
// with exactly one "/" (before or after percent-decoding).
func SafeNext(next string) string {
	if !localPath(next) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || !localPath(u.Path) {
		return "/" // u.Path is percent-decoded: catches "/%2F/evil.com", "/%09/..." and "/%5C..."
	}
	return next
}

// localPath: starts with exactly one "/" and holds no control char or backslash.
func localPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !hasUnsafeChar(p)
}

func hasUnsafeChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b == 0x7f || b == '\\' {
			return true
		}
	}
	return false
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

// RequireSameOriginAPI guards the cookie-authenticated machine API (the
// upstream REST routes). Upstream has GET routes with side effects (logout,
// reconnect, login), and SameSite=Lax still sends our cookie on a top-level
// cross-site GET, so VerifyCSRF alone (unsafe methods only) is not enough.
// Mount it on the REST API only, after RequireAPI and before VerifyCSRF.
//
// The rule, for every method:
//   - Sec-Fetch-Site "cross-site" or "same-site": 403, token or not.
//   - Sec-Fetch-Site "same-origin" or "none" (typed into the address bar):
//     allowed here; unsafe methods still need the CSRF token (VerifyCSRF).
//   - No Sec-Fetch-Site at all (older browsers, scripts) on a request that
//     carries a session: GET/HEAD need a valid X-CSRF-Token header, else 403.
//     Unsafe methods are left to VerifyCSRF, which requires the token anyway.
//   - Requests without a session are not judged here; RequireAPI refuses them.
//
// Page routes do not use this: a plain link to a page must keep working.
func (m *Manager) RequireSameOriginAPI() fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Get(fiber.HeaderSecFetchSite) {
		case "cross-site", "same-site":
			return forbiddenJSON(c)
		case "same-origin", "none":
			return c.Next()
		case "":
			want := m.CSRFToken(c)
			if want == "" {
				return c.Next()
			}
			switch c.Method() {
			case fiber.MethodGet, fiber.MethodHead:
				got := c.Get("X-CSRF-Token")
				if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
					return forbiddenJSON(c)
				}
			}
			return c.Next()
		default: // an unknown value: fail closed
			return forbiddenJSON(c)
		}
	}
}

func forbiddenJSON(c fiber.Ctx) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"status": fiber.StatusForbidden, "code": "CROSS_SITE_REFUSED", "message": "request refused: not from this site",
	})
}

// RequireSameOrigin guards unauthenticated state-changing POSTs (sign-in
// verify, passkey login finish), where there is no session to bind a CSRF
// token to. It refuses Sec-Fetch-Site "cross-site"; when an Origin header is
// present its host must equal the request Host ("null" never does). A request
// with neither header (a non-browser client) is allowed. The 403 goes through
// the app's error handler, which picks HTML or JSON.
func (m *Manager) RequireSameOrigin() fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Get(fiber.HeaderSecFetchSite) == "cross-site" {
			return fiber.NewError(fiber.StatusForbidden, "This request did not come from this site.")
		}
		if origin := c.Get(fiber.HeaderOrigin); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || !strings.EqualFold(u.Host, c.Host()) {
				return fiber.NewError(fiber.StatusForbidden, "This request did not come from this site.")
			}
		}
		return c.Next()
	}
}

// IsSuperadmin consults the env allowlist only, never the database. The phone
// is normalised first; a phone that does not normalise is never a superadmin.
func (m *Manager) IsSuperadmin(u *store.User) bool {
	if u == nil || u.Status != store.StatusActive {
		return false
	}
	phone, err := store.NormalizePhone(u.Phone)
	if err != nil {
		return false
	}
	return m.cfg.Superadmins[phone]
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

// StartSession signs u in. Any session the request's cookie names is revoked
// first (by the raw cookie, even if the middleware could not load it), so a
// sign-in always yields a fresh session id. If the store failed while loading
// this request's session, it fails closed and issues nothing.
func (m *Manager) StartSession(c fiber.Ctx, u *store.User) error {
	if u == nil || u.Status != store.StatusActive {
		return ErrInactiveUser
	}
	old, _ := c.Locals(keyRaw).(string)
	if old == "" {
		old = c.Cookies(m.CookieName())
	}
	if old != "" {
		if err := m.st.DeleteSession(c.Context(), old); err != nil {
			return err
		}
		c.Locals(keyRaw, "")
	}
	if storeFailed(c) {
		return ErrStoreFault
	}
	raw, err := m.st.CreateSession(c.Context(), u.ID, c.Get(fiber.HeaderUserAgent), m.cfg.SessionTTL)
	if err != nil {
		return err
	}
	_ = m.st.TouchLogin(c.Context(), u.ID)
	m.setCookie(c, raw)
	c.Locals(keyRaw, raw)
	c.Locals(keyUser, u)
	c.SetContext(WithUser(c.Context(), u))
	return nil
}

// EndSession deletes the session server side and clears the cookie.
func (m *Manager) EndSession(c fiber.Ctx) error {
	if raw, ok := c.Locals(keyRaw).(string); ok && raw != "" {
		if err := m.st.DeleteSession(c.Context(), raw); err != nil {
			return err
		}
	}
	c.Locals(keyRaw, "")
	c.Locals(keyUser, nil)
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
	raw, _ := c.Locals(keyRaw).(string)
	if raw == "" {
		return ""
	}
	mac := hmac.New(sha256.New, m.cfg.Secret)
	mac.Write([]byte("csrf:" + raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCSRF requires the session's token on every method other than GET,
// HEAD and OPTIONS. The token is read from the X-CSRF-Token header or the
// request body (urlencoded or multipart field "csrf"), never the query string
// (URLs leak into logs, history and Referer). Requests without a session pass
// through: the gates refuse them. A store fault answers 500, like the gates.
func (m *Manager) VerifyCSRF() fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}
		if storeFailed(c) {
			return errStorePage()
		}
		want := m.CSRFToken(c)
		if want == "" { // no session: the gates decide
			return c.Next()
		}
		got := c.Get("X-CSRF-Token")
		if got == "" {
			got = bodyCSRF(c)
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			return fiber.NewError(fiber.StatusForbidden, "This form has expired. Reload the page and try again.")
		}
		return c.Next()
	}
}

func bodyCSRF(c fiber.Ctx) string {
	if c.IsMultipart() {
		mf, err := c.MultipartForm() // honours the app's BodyLimit
		if err != nil {
			return ""
		}
		if v := mf.Value["csrf"]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	return string(c.Request().PostArgs().Peek("csrf"))
}

// IsSuperadminPhone reports whether phone (any spelling) is on the superadmin
// allowlist. Used to refuse moving an account onto, or off, such a number.
func (m *Manager) IsSuperadminPhone(phone string) bool {
	p, err := store.NormalizePhone(phone)
	return err == nil && m.cfg.Superadmins[p]
}

// CurrentSessionHash is the stored hash of this request's session, or "".
func (m *Manager) CurrentSessionHash(c fiber.Ctx) string {
	raw, _ := c.Locals(keyRaw).(string)
	if raw == "" {
		return ""
	}
	return store.HashToken(raw)
}

// SessionHandle is the opaque id a page uses for one stored session: an HMAC
// of its stored hash, so neither the raw id nor the hash reaches HTML, and a
// handle cannot be turned back into either. Handles are only ever matched
// against the signed-in user's own sessions.
func (m *Manager) SessionHandle(idHash string) string {
	mac := hmac.New(sha256.New, m.cfg.Secret)
	mac.Write([]byte("session-row:" + idHash))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}
