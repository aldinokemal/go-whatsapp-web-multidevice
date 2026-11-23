package mcpauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

const (
	testPublic   = "https://wtf.test"
	testResource = "https://wtf.test/mcp"
)

type harness struct {
	t     *testing.T
	st    *store.Store
	am    *auth.Manager
	authn *Authenticator
	srv   *oauth.Server
	app   *fiber.App
}

// newHarness builds the OAuth server in session mode over a real accounts
// store, mounted the way the runtime mounts it: the session middleware first.
func newHarness(t *testing.T, cfg oauth.Config) *harness {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	am := auth.New(auth.Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour, Secure: true}, st)
	authn := &Authenticator{Auth: am, Store: st, Web: web.Deps{Store: st, Auth: am, ProductName: "WTF", PublicURL: testPublic}}
	cfg.IssuerURL, cfg.ResourceURL = testPublic, testResource
	cfg.StorageURI = "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "oauth.db"))
	cfg.Session = authn
	srv, err := oauth.New(cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
	app := fiber.New()
	app.Use(am.Middleware())
	srv.RegisterPublic(app)
	return &harness{t: t, st: st, am: am, authn: authn, srv: srv, app: app}
}

// user creates an active user and returns them with a signed-in cookie.
func (h *harness) user(phone string) (*store.User, *http.Cookie) {
	h.t.Helper()
	u, err := h.st.CreateUser(context.Background(), phone)
	require.NoError(h.t, err)
	raw, err := h.st.CreateSession(context.Background(), u.ID, "", time.Hour)
	require.NoError(h.t, err)
	return u, &http.Cookie{Name: h.am.CookieName(), Value: raw}
}

func (h *harness) do(req *http.Request) (*http.Response, string) {
	h.t.Helper()
	resp, err := h.app.Test(req)
	require.NoError(h.t, err)
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (h *harness) register(redirect string) string {
	h.t.Helper()
	body := `{"client_name":"Test","redirect_uris":["` + redirect + `"],"application_type":"web","token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, raw := h.do(req)
	require.Equal(h.t, fiber.StatusCreated, resp.StatusCode, raw)
	var p struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(h.t, json.Unmarshal([]byte(raw), &p))
	return p.ClientID
}

func authorizeQuery(clientID, redirect string) (url.Values, string) {
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	return url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {testResource}, "scope": {"mcp"}, "state": {"s"},
	}, verifier
}

// csrfOf reads the consent form's csrf value out of a rendered page.
func csrfOf(t *testing.T, page string) string {
	t.Helper()
	const marker = `name="csrf" value="`
	i := strings.Index(page, marker)
	require.GreaterOrEqual(t, i, 0, "consent page has a csrf field")
	rest := page[i+len(marker):]
	return rest[:strings.IndexByte(rest, '"')]
}

// tokensFor runs the whole browser flow for a signed-in user and returns
// the token pair: consent page, Allow, code exchange.
func (h *harness) tokensFor(ck *http.Cookie, clientID, redirect string) (string, string) {
	h.t.Helper()
	q, verifier := authorizeQuery(clientID, redirect)
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, page := h.do(req)
	require.Equal(h.t, fiber.StatusOK, resp.StatusCode, page)
	form := url.Values{}
	for k, v := range q {
		form[k] = v
	}
	form.Set("csrf", csrfOf(h.t, page))
	form.Set("decision", "approve")
	req = httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, body := h.do(req)
	require.Equal(h.t, fiber.StatusFound, resp.StatusCode, body)
	cb, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(h.t, err)
	tok := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {cb.Query().Get("code")},
		"redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {testResource}}
	req = httptest.NewRequest("POST", "/oauth/token", strings.NewReader(tok.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body = h.do(req)
	require.Equal(h.t, fiber.StatusOK, resp.StatusCode, body)
	var pair struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(h.t, json.Unmarshal([]byte(body), &pair))
	return pair.AccessToken, pair.RefreshToken
}
