package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type env struct {
	app *fiber.App
	st  *store.Store
	d   Deps
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	am := auth.New(auth.Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: 30 * 24 * time.Hour,
		Superadmins: map[string]bool{"+27825550100": true}, Secure: true}, st)
	pk, err := passkey.New("wtf.onrender.com", "https://wtf.onrender.com", "WTF", st)
	require.NoError(t, err)
	d := Deps{Store: st, Auth: am, Passkeys: pk, ProductName: "WTF", PublicURL: "https://wtf.onrender.com",
		ServicePhone: func(context.Context) string { return "27825550000" }, StorageDir: t.TempDir()}
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(d)})
	app.Use(am.Middleware())
	Mount(app, d)
	app.Use(am.APIKey(), am.RequireAPI()) // what the runtime installs after Mount
	return &env{app: app, st: st, d: d}
}

func (e *env) do(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := e.app.Test(req)
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestSigninPageOffersWhatsAppAndPasskey(t *testing.T) {
	e := newEnv(t)
	resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin", nil))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Contains(t, resp.Header.Get("Content-Security-Policy"), "script-src 'self'")
	require.Contains(t, body, "https://wa.me/27825550000?text=Sign+me+in+to+WTF")
	require.Contains(t, body, "Sign in with a passkey")
	require.NotContains(t, body, "<script>", "no inline script")
}

func TestVerifyGETNeverConsumesAndPOSTSignsIn(t *testing.T) {
	e := newEnv(t)
	_, _ = e.st.CreateUser(context.Background(), "+27825550101")
	raw, _ := e.st.CreateLoginToken(context.Background(), "+27825550101", 15*time.Minute)

	for i := 0; i < 3; i++ { // link previews and scanners
		resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin/verify?t="+url.QueryEscape(raw), nil))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Contains(t, body, `method="post"`)
	}
	ok, _ := e.st.LoginTokenValid(context.Background(), raw)
	require.True(t, ok, "GET did not consume the token")

	form := url.Values{"t": {raw}, "next": {"//evil.example"}}
	req := httptest.NewRequest(http.MethodPost, "/signin/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/", resp.Header.Get("Location"), "open redirect refused")
	var gotCookie bool
	for _, c := range resp.Cookies() {
		gotCookie = gotCookie || (c.Name == auth.CookieName && c.Value != "")
	}
	require.True(t, gotCookie)

	resp, body := e.do(t, req.Clone(context.Background()))
	_ = resp
	require.Contains(t, body, "already been used or has expired", "second POST refused politely")
}

func TestBrowser404IsHTMLAndAPI401IsJSON(t *testing.T) {
	e := newEnv(t)
	resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/admin/users", nil))
	require.Equal(t, http.StatusSeeOther, resp.StatusCode) // anonymous: to sign-in
	resp, body = e.do(t, httptest.NewRequest(http.MethodGet, "/devices", nil))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	_ = body
}

func TestNoServiceNumberExplainsInsteadOfBreaking(t *testing.T) {
	e := newEnv(t)
	e.d.ServicePhone = func(context.Context) string { return "" }
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/signin", nil))
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(b), "isn't set up yet")
	require.NotContains(t, string(b), "wa.me")
}

func TestTemplatesNeverUseBrowserDialogs(t *testing.T) {
	entries, err := templatesFS.ReadDir("templates")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, f := range entries {
		b, _ := templatesFS.ReadFile("templates/" + f.Name())
		for _, banned := range []string{"alert(", "confirm(", "hx-confirm", "onclick=", "style=\""} {
			require.NotContains(t, string(b), banned, f.Name())
		}
	}
	js, _ := staticFS.ReadFile("static/app.js")
	require.NotContains(t, string(js), "alert(")
	require.NotContains(t, string(js), "confirm(")
}
