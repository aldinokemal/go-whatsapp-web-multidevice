package auth

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// Fix round 1 (adversarial review of b3aed9f).

const testSecret = "0123456789abcdef0123456789abcdef"

func csrfFor(t *testing.T, app *fiber.App, ck *http.Cookie) string {
	t.Helper()
	b, _ := io.ReadAll(get(t, app, "/csrf", ck).Body)
	return string(b)
}

func do(t *testing.T, app *fiber.App, method, path string, ck *http.Cookie, hdr map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if ck != nil {
		req.AddCookie(ck)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

// 1. The upstream REST API has GET routes with side effects (logout, reconnect,
// login). SameSite=Lax still sends the cookie on top-level cross-site GETs.
func TestRequireSameOriginAPI(t *testing.T) {
	app, m, st := setup(t)
	api := []fiber.Handler{m.RequireAPI(), m.RequireSameOriginAPI(), m.VerifyCSRF()}
	app.Get("/app/logout", api[0], api[1], api[2], func(c fiber.Ctx) error { return c.SendString("out") })
	app.Post("/send", api[0], api[1], api[2], func(c fiber.Ctx) error { return c.SendString("sent") })
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	tok := csrfFor(t, app, ck)

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"cross-site even with token", map[string]string{"Sec-Fetch-Site": "cross-site", "X-CSRF-Token": tok}, http.StatusForbidden},
		{"same-site", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"none (typed URL)", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		{"no header, no token", nil, http.StatusForbidden},
		{"no header, wrong token", map[string]string{"X-CSRF-Token": "nope"}, http.StatusForbidden},
		{"no header, valid token", map[string]string{"X-CSRF-Token": tok}, http.StatusOK},
	}
	for _, tc := range cases {
		resp := do(t, app, http.MethodGet, "/app/logout", ck, tc.hdr)
		require.Equal(t, tc.want, resp.StatusCode, tc.name)
		if tc.want == http.StatusForbidden {
			require.Contains(t, resp.Header.Get("Content-Type"), "application/json", tc.name)
		}
	}
	require.Equal(t, http.StatusForbidden,
		do(t, app, http.MethodPost, "/send", ck, map[string]string{"Sec-Fetch-Site": "cross-site", "X-CSRF-Token": tok}).StatusCode)
	require.Equal(t, http.StatusOK,
		do(t, app, http.MethodPost, "/send", ck, map[string]string{"Sec-Fetch-Site": "same-origin", "X-CSRF-Token": tok}).StatusCode)
	// Page GETs are unaffected: no Sec-Fetch-Site, no token, still 200.
	require.Equal(t, http.StatusOK, get(t, app, "/page", ck).StatusCode)
}

// 2. next must only ever be a local path.
func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/ok?x=1":          "/ok?x=1",
		"/":                "/",
		"/devices/abc":     "/devices/abc",
		"":                 "/",
		"//evil.com":       "/",
		"/\\evil.com":      "/",
		"\\\\evil.com":     "/",
		"https://evil.com": "/",
		"javascript:alert": "/",
		"/%09/evil.com":    "/",
		"/\t/evil.com":     "/",
		"/%2F/evil.com":    "/",
		"/%5Cevil.com":     "/",
		"/a\x7fb":          "/",
		"/a\nb":            "/",
		"relative":         "/",
		"//":               "/",
		"/%zz":             "/",
	} {
		require.Equal(t, want, SafeNext(in), "SafeNext(%q)", in)
	}
}

func TestRequirePageNextIsSafe(t *testing.T) {
	app, _, _ := setup(t)
	resp := get(t, app, "/page?x=1", nil)
	require.Equal(t, "/signin?next=%2Fpage%3Fx%3D1", resp.Header.Get("Location"))
}

func TestHTMXUnauthenticatedGetsHXRedirect(t *testing.T) {
	app, _, _ := setup(t)
	resp := do(t, app, http.MethodGet, "/page", nil, map[string]string{"HX-Request": "true"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "/signin?next=%2Fpage", resp.Header.Get("HX-Redirect"))
	require.Empty(t, resp.Header.Get("Location"))
}

// 3. For unauthenticated state-changing POSTs (sign-in verify, passkey finish).
func TestRequireSameOrigin(t *testing.T) {
	app, m, _ := setup(t)
	app.Post("/signin/verify", m.RequireSameOrigin(), func(c fiber.Ctx) error { return c.SendString("ok") })
	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"foreign origin", map[string]string{"Origin": "https://evil.com"}, http.StatusForbidden},
		{"lookalike origin", map[string]string{"Origin": "https://example.com.evil.com"}, http.StatusForbidden},
		{"null origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"same origin", map[string]string{"Origin": "https://example.com", "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"neither header (non-browser)", nil, http.StatusOK},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, do(t, app, http.MethodPost, "/signin/verify", nil, tc.hdr).StatusCode, tc.name)
	}
}

// 4. Never read the CSRF token from the query string; multipart body is fine.
func TestCSRFNotFromQueryButFromMultipart(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	tok := csrfFor(t, app, ck)

	require.Equal(t, http.StatusForbidden, do(t, app, http.MethodPost, "/act?csrf="+tok, ck, nil).StatusCode)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("csrf", tok))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/act", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(ck)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// 5. Store faults: VerifyCSRF answers 500, and StartSession fails closed.
func TestVerifyCSRFUnderStoreFaultIs500(t *testing.T) {
	app, m, _ := setup(t)
	fault := func(c fiber.Ctx) error { c.Locals(keyAuthErr, true); return c.Next() }
	app.Post("/faulty", fault, m.VerifyCSRF(), func(c fiber.Ctx) error { return c.SendString("done") })
	require.Equal(t, http.StatusInternalServerError, do(t, app, http.MethodPost, "/faulty", nil, nil).StatusCode)
}

func TestStartSessionUnderStoreFaultFailsClosedAndRevokesOld(t *testing.T) {
	app, m, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	fault := func(c fiber.Ctx) error { c.Locals(keyAuthErr, true); c.Locals(keyRaw, nil); return c.Next() }
	app.Get("/faulty-login", fault, func(c fiber.Ctx) error { return m.StartSession(c, u) })
	old := login(t, app, u.ID)

	resp := do(t, app, http.MethodGet, "/faulty-login", old, nil)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	for _, c := range resp.Cookies() {
		require.False(t, c.Name == CookieName && c.Value != "" && c.Value != old.Value, "no new session under a fault")
	}
	require.Equal(t, http.StatusSeeOther, get(t, app, "/page", old).StatusCode, "the old session was revoked by its raw cookie")
}

// 6. New copies its inputs.
func TestNewCopiesSecretAndSuperadmins(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "c.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	secret := []byte(testSecret)
	admins := map[string]bool{"+27825550100": true}
	m := New(Config{Secret: secret, Superadmins: admins, SessionTTL: time.Hour, Secure: true}, st)
	admin := &store.User{Phone: "+27825550100", Status: store.StatusActive}

	app := fiber.New()
	app.Get("/t", func(c fiber.Ctx) error { c.Locals(keyRaw, "fixed"); return c.SendString(m.CSRFToken(c)) })
	before, _ := io.ReadAll(get(t, app, "/t", nil).Body)

	delete(admins, "+27825550100")
	admins["+27825550101"] = true
	secret[0] = 'X'

	after, _ := io.ReadAll(get(t, app, "/t", nil).Body)
	require.Equal(t, string(before), string(after))
	require.True(t, m.IsSuperadmin(admin))
	require.False(t, m.IsSuperadmin(&store.User{Phone: "+27825550101", Status: store.StatusActive}))
}

// 8. IsSuperadmin normalises and fails closed.
func TestIsSuperadminNormalises(t *testing.T) {
	_, m, _ := setup(t)
	require.True(t, m.IsSuperadmin(&store.User{Phone: "27 82 555 0100", Status: store.StatusActive}))
	require.False(t, m.IsSuperadmin(&store.User{Phone: "garbage", Status: store.StatusActive}))
	require.False(t, m.IsSuperadmin(&store.User{Phone: "", Status: store.StatusActive}))
	require.False(t, m.IsSuperadmin(&store.User{Phone: "+27825550100", Status: store.StatusDisabled}))
	require.False(t, m.IsSuperadmin(nil))
}

// 9. Cookie naming across Secure modes.
func TestDevManagerCookieIsNotSecure(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	m := New(Config{Secret: []byte(testSecret), SessionTTL: time.Hour}, st)
	require.False(t, m.Secure())
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/in", func(c fiber.Ctx) error { return m.StartSession(c, u) })
	app.Get("/page", m.RequirePage(), func(c fiber.Ctx) error { return c.SendString("ok") })
	resp := get(t, app, "/in", nil)
	var ck *http.Cookie
	for _, c := range resp.Cookies() {
		require.NotEqual(t, CookieName, c.Name)
		if c.Name == DevCookieName {
			ck = c
		}
	}
	require.NotNil(t, ck)
	require.False(t, ck.Secure)
	require.True(t, ck.HttpOnly)
	require.Equal(t, http.StatusOK, get(t, app, "/page", ck).StatusCode)
}

func TestSecureManagerIgnoresDevCookie(t *testing.T) {
	app, m, st := setup(t)
	require.True(t, m.Secure())
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	dev := &http.Cookie{Name: DevCookieName, Value: ck.Value}
	require.Equal(t, http.StatusSeeOther, get(t, app, "/page", dev).StatusCode)
}

func TestRequirePageRefusesUnsafeNextFromThePath(t *testing.T) {
	app, m, _ := setup(t)
	app.Get("/*", m.RequirePage(), func(c fiber.Ctx) error { return c.SendString("x") })
	for _, p := range []string{"/%5Cevil.com", "/%09/evil.com"} {
		resp := get(t, app, p, nil)
		require.Equal(t, http.StatusSeeOther, resp.StatusCode, p)
		require.Equal(t, "/signin?next=%2F", resp.Header.Get("Location"), p)
	}
}
