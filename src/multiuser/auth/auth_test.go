package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (*fiber.App, *Manager, *store.Store) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	m := New(Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: 30 * 24 * time.Hour,
		Superadmins: map[string]bool{"+27825550100": true}, Secure: true}, st)
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/page", m.RequirePage(), func(c fiber.Ctx) error {
		u, _ := UserFrom(c.Context())
		return c.SendString("hello " + u.Phone)
	})
	app.Post("/act", m.RequirePage(), m.VerifyCSRF(), func(c fiber.Ctx) error { return c.SendString("done") })
	app.Get("/api/thing", m.RequireAPI(), func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/admin", m.RequirePage(), m.RequireSuperadmin(), func(c fiber.Ctx) error { return c.SendString("admin") })
	app.Get("/login-as/:id", func(c fiber.Ctx) error {
		u, err := st.UserByID(c.Context(), c.Params("id"))
		require.NoError(t, err)
		return m.StartSession(c, u)
	})
	app.Get("/csrf", m.RequirePage(), func(c fiber.Ctx) error { return c.SendString(m.CSRFToken(c)) })
	return app, m, st
}

func login(t *testing.T, app *fiber.App, userID string) *http.Cookie {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/login-as/"+userID, nil))
	require.NoError(t, err)
	for _, c := range resp.Cookies() {
		if c.Name == CookieName {
			require.True(t, c.HttpOnly)
			require.True(t, c.Secure)
			require.Equal(t, http.SameSiteLaxMode, c.SameSite)
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func get(t *testing.T, app *fiber.App, path string, ck *http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func TestAnonymousPageRedirectsAndAPIGets401(t *testing.T) {
	app, _, _ := setup(t)
	resp := get(t, app, "/page", nil)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/signin?next=%2Fpage", resp.Header.Get("Location"))

	resp = get(t, app, "/api/thing", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

func TestSessionGrantsAccessAndDisabledUserIsRefusedAtOnce(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)

	resp := get(t, app, "/page", ck)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "hello +27825550199", string(body))

	require.NoError(t, st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled))
	resp = get(t, app, "/page", ck)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "disabled user is signed out on the next request")
	resp = get(t, app, "/api/thing", ck)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSuperadminComesFromAllowlistOnly(t *testing.T) {
	app, _, st := setup(t)
	admin, _ := st.CreateUser(context.Background(), "+27825550100")
	plain, _ := st.CreateUser(context.Background(), "+27825550101")
	require.Equal(t, http.StatusOK, get(t, app, "/admin", login(t, app, admin.ID)).StatusCode)
	require.Equal(t, http.StatusNotFound, get(t, app, "/admin", login(t, app, plain.ID)).StatusCode)
}

func TestCSRFRequiredOnUnsafeMethods(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)

	post := func(form url.Values, header string) int {
		req := httptest.NewRequest(http.MethodPost, "/act", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if header != "" {
			req.Header.Set("X-CSRF-Token", header)
		}
		req.AddCookie(ck)
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}
	require.Equal(t, http.StatusForbidden, post(url.Values{}, ""))
	require.Equal(t, http.StatusForbidden, post(url.Values{"csrf": {"wrong"}}, ""))

	tokResp := get(t, app, "/csrf", ck)
	tok, _ := io.ReadAll(tokResp.Body)
	require.Equal(t, http.StatusOK, post(url.Values{"csrf": {string(tok)}}, ""))
	require.Equal(t, http.StatusOK, post(url.Values{}, string(tok)))
}

// --- Hardening tests beyond the brief (see task-3-report.md). ---

func TestInsecureManagerDoesNotRenameSecureCookie(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "b.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	dev := New(Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour}, st)
	require.Equal(t, "wtf_session", dev.CookieName())
	require.Equal(t, "__Host-wtf_session", CookieName, "a dev manager must not change the global name")

	_, m, _ := setup(t)
	require.Equal(t, "__Host-wtf_session", m.CookieName())
}

func TestSessionCookieFlagsAndSlidingRefresh(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	require.Equal(t, "/", ck.Path)
	require.Equal(t, 30*24*3600, ck.MaxAge)
	require.Empty(t, ck.Domain, "__Host- cookies must not carry a Domain")

	resp := get(t, app, "/page", ck)
	var refreshed *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == CookieName {
			refreshed = c
		}
	}
	require.NotNil(t, refreshed, "an authenticated request re-issues the cookie so expiry slides")
	require.Equal(t, ck.Value, refreshed.Value)
	require.True(t, refreshed.HttpOnly)
	require.True(t, refreshed.Secure)
	require.Equal(t, 30*24*3600, refreshed.MaxAge)
}

func TestInvalidCookieIsClearedAndAnonymous(t *testing.T) {
	app, _, _ := setup(t)
	resp := get(t, app, "/api/thing", &http.Cookie{Name: CookieName, Value: "forged"})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == CookieName && c.Value == "" && c.MaxAge < 0 {
			cleared = true
		}
	}
	require.True(t, cleared)
}

func TestEndSessionDeletesServerSide(t *testing.T) {
	app, m, st := setup(t)
	app.Post("/signout", m.RequirePage(), m.VerifyCSRF(), func(c fiber.Ctx) error { return m.EndSession(c) })
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	tok, _ := io.ReadAll(get(t, app, "/csrf", ck).Body)

	req := httptest.NewRequest(http.MethodPost, "/signout", nil)
	req.Header.Set("X-CSRF-Token", string(tok))
	req.AddCookie(ck)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Replaying the old cookie value must not work: the session row is gone.
	require.Equal(t, http.StatusSeeOther, get(t, app, "/page", ck).StatusCode)
}

func TestStartSessionRotatesExistingSession(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	first := login(t, app, u.ID)
	req := httptest.NewRequest(http.MethodGet, "/login-as/"+u.ID, nil)
	req.AddCookie(first)
	resp, err := app.Test(req)
	require.NoError(t, err)
	var second *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == CookieName {
			second = c
		}
	}
	require.NotNil(t, second)
	require.NotEqual(t, first.Value, second.Value)
	require.Equal(t, http.StatusSeeOther, get(t, app, "/page", first).StatusCode, "old session is revoked on re-sign-in")
	require.Equal(t, http.StatusOK, get(t, app, "/page", second).StatusCode)
}

func TestStartSessionRefusesDisabledUser(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	require.NoError(t, st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled))
	resp := get(t, app, "/login-as/"+u.ID, nil)
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
	for _, c := range resp.Cookies() {
		require.False(t, c.Name == CookieName && c.Value != "", "no session cookie for a disabled user")
	}
}

func TestCSRFTokenIsBoundToSessionAndChecksAllUnsafeMethods(t *testing.T) {
	app, m, st := setup(t)
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		app.Add([]string{method}, "/act", m.RequirePage(), m.VerifyCSRF(), func(c fiber.Ctx) error { return c.SendString("done") })
	}
	a, _ := st.CreateUser(context.Background(), "+27825550198")
	b, _ := st.CreateUser(context.Background(), "+27825550199")
	ckA, ckB := login(t, app, a.ID), login(t, app, b.ID)
	tokA, _ := io.ReadAll(get(t, app, "/csrf", ckA).Body)
	tokB, _ := io.ReadAll(get(t, app, "/csrf", ckB).Body)
	require.NotEqual(t, string(tokA), string(tokB))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/act", nil)
		req.Header.Set("X-CSRF-Token", string(tokA))
		req.AddCookie(ckB)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s with another session's token", method)

		req = httptest.NewRequest(method, "/act", nil)
		req.AddCookie(ckB)
		resp, err = app.Test(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s with no token", method)
	}
}

func TestAnonymousUnsafeRequestIsRefusedByGateNotCSRF(t *testing.T) {
	app, _, _ := setup(t)
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/act", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

func TestAPIUnauthenticatedBody(t *testing.T) {
	app, _, _ := setup(t)
	body, _ := io.ReadAll(get(t, app, "/api/thing", nil).Body)
	require.JSONEq(t, `{"status":401,"code":"UNAUTHENTICATED","message":"sign in required"}`, string(body))
}

func TestRedirectKeepsQueryInNext(t *testing.T) {
	app, _, _ := setup(t)
	resp := get(t, app, "/page?x=1&y=2", nil)
	require.Equal(t, "/signin?next="+url.QueryEscape("/page?x=1&y=2"), resp.Header.Get("Location"))
}

func TestStoreFailureIs500AndKeepsCookie(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	require.NoError(t, st.Close())
	_, err := st.SessionUser(context.Background(), ck.Value, time.Hour)
	require.Error(t, err)
	require.False(t, errors.Is(err, store.ErrNotFound), "precondition: a closed store is a fault, not a missing session")

	noClear := func(resp *http.Response) {
		for _, c := range resp.Cookies() {
			require.False(t, c.Name == CookieName && c.Value == "", "a store fault must not clear the session cookie")
		}
	}
	resp := get(t, app, "/page", ck)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode, "pages: 500, not a sign-in redirect")
	noClear(resp)

	resp = get(t, app, "/api/thing", ck)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	body, _ := io.ReadAll(resp.Body)
	require.JSONEq(t, `{"status":500,"code":"INTERNAL_ERROR","message":"could not check your session"}`, string(body))
	noClear(resp)
}

// Security: an account being deleted is refused on the very next request,
// with the session it already held (the first step of account deletion).
func TestDeletingUserCannotUseASession(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)
	require.Equal(t, http.StatusOK, get(t, app, "/page", ck).StatusCode)

	require.NoError(t, st.SetUserStatus(context.Background(), u.ID, store.StatusDeleting))
	require.Equal(t, http.StatusSeeOther, get(t, app, "/page", ck).StatusCode)
	require.Equal(t, http.StatusUnauthorized, get(t, app, "/api/thing", ck).StatusCode)
}
