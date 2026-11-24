package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// keyApp mounts the gates the way the runtime does: the session middleware
// for everything, page routes first, then the API-key loader at the head of
// the API gate chain, then an API route that echoes who the caller is.
func keyApp(t *testing.T) (*fiber.App, *Manager, *store.Store) {
	t.Helper()
	app, m, st := setup(t)
	app.Use(m.APIKey(), m.RequireAPI(), m.RequireSameOriginAPI(), m.VerifyCSRF())
	echo := func(c fiber.Ctx) error {
		u, _ := UserFrom(c.Context())
		return c.JSON(fiber.Map{"user": u.ID, "via": m.AuthenticatedBy(c)})
	}
	app.Get("/whoami", echo)
	app.Post("/whoami", echo)
	return app, m, st
}

type whoami struct{ User, Via string }

func keyReq(t *testing.T, app *fiber.App, method, key string, ck *http.Cookie) (*http.Response, whoami) {
	t.Helper()
	req := httptest.NewRequest(method, "/whoami", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if ck != nil {
		req.AddCookie(ck)
		req.Header.Set("Sec-Fetch-Site", "same-origin") // a browser's own request
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	var w whoami
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &w)
	return resp, w
}

func newKey(t *testing.T, st *store.Store, phone string) (*store.User, string, *store.APIKey) {
	t.Helper()
	u, err := st.CreateUser(context.Background(), phone)
	require.NoError(t, err)
	raw, k, err := st.CreateAPIKey(context.Background(), u.ID, "test")
	require.NoError(t, err)
	return u, raw, k
}

func requireKey401(t *testing.T, resp *http.Response) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.Equal(t, `Bearer realm="wtf"`, resp.Header.Get("WWW-Authenticate"))
}

func TestValidKeyAuthenticates(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, _ := newKey(t, st, "+27825550101")
	resp, w := keyReq(t, app, http.MethodGet, raw, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, alice.ID, w.User)
	require.Equal(t, "api_key", w.Via)

	keys, err := st.APIKeysOf(context.Background(), alice.ID)
	require.NoError(t, err)
	require.NotNil(t, keys[0].LastUsedAt, "use is recorded")
}

func TestSchemeIsCaseInsensitive(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, _ := newKey(t, st, "+27825550101")
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "bearer "+raw)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), alice.ID)
}

// Behaviour: a key is not a browser session, so the cookie-only checks
// (same-origin, CSRF) do not apply to it.
func TestKeyRequestNeedsNoCSRF(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, _ := newKey(t, st, "+27825550101")
	resp, w := keyReq(t, app, http.MethodPost, raw, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, alice.ID, w.User)
}

func TestRevokedKeyIs401(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, k := newKey(t, st, "+27825550101")
	require.NoError(t, st.RevokeAPIKey(context.Background(), alice.ID, k.ID))
	resp, _ := keyReq(t, app, http.MethodGet, raw, nil)
	requireKey401(t, resp)
}

// Security: disabling a user stops their keys on the very next request.
func TestDisabledUsersKeyIs401(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, _ := newKey(t, st, "+27825550101")
	resp, _ := keyReq(t, app, http.MethodGet, raw, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, st.SetUserStatus(context.Background(), alice.ID, store.StatusDisabled))
	resp, _ = keyReq(t, app, http.MethodGet, raw, nil)
	requireKey401(t, resp)
}

// Security: a wtfk_ bearer that fails is a 401 even when a good cookie rides
// along; it never falls back to the cookie.
func TestUnknownWTFKeyIs401EvenWithAGoodCookie(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, _ := newKey(t, st, "+27825550101")
	ck := login(t, app, alice.ID)
	resp, w := keyReq(t, app, http.MethodGet, "", ck)
	require.Equal(t, http.StatusOK, resp.StatusCode, "the cookie alone works")
	require.Equal(t, "session", w.Via)

	for _, bad := range []string{store.APIKeyPrefix + "nope", raw + "x", store.APIKeyPrefix} {
		resp, _ = keyReq(t, app, http.MethodGet, bad, ck)
		requireKey401(t, resp)
	}
}

// Behaviour (recorded decision): a bearer that is not a WTF key means nothing
// on the REST API; alone it is anonymous, with a cookie the cookie decides.
func TestForeignBearerIsIgnoredOnREST(t *testing.T) {
	app, _, st := keyApp(t)
	alice, _, _ := newKey(t, st, "+27825550101")
	resp, _ := keyReq(t, app, http.MethodGet, "some-oauth-token", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Empty(t, resp.Header.Get("WWW-Authenticate"))
	ck := login(t, app, alice.ID)
	resp, w := keyReq(t, app, http.MethodGet, "some-oauth-token", ck)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, alice.ID, w.User)
}

// Security: with Alice's cookie and Bob's key, the request is Bob's, and the
// response neither re-issues nor mints a session cookie.
func TestKeyWinsOverCookie(t *testing.T) {
	app, _, st := keyApp(t)
	alice, _, _ := newKey(t, st, "+27825550101")
	bob, bobKey, _ := newKey(t, st, "+27825550102")
	ck := login(t, app, alice.ID)
	resp, w := keyReq(t, app, http.MethodPost, bobKey, ck)
	require.Equal(t, http.StatusOK, resp.StatusCode, "no CSRF needed: the cookie principal was dropped")
	require.Equal(t, bob.ID, w.User)
	require.Equal(t, "api_key", w.Via)
	require.Empty(t, resp.Header.Values("Set-Cookie"))
}

// Security: a key never opens a page. Page routes are registered before the
// API gate, exactly as the runtime mounts them, and only read the cookie.
func TestKeyNeverOpensAPage(t *testing.T) {
	app, _, st := keyApp(t)
	_, raw, _ := newKey(t, st, "+27825550101")
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/signin"))
}

// Security: the raw key never reaches the logs, on success or failure.
func TestKeyIsNotLogged(t *testing.T) {
	var buf bytes.Buffer
	saved, savedLevel := logrus.StandardLogger().Out, logrus.GetLevel()
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.TraceLevel)
	t.Cleanup(func() { logrus.SetOutput(saved); logrus.SetLevel(savedLevel) })

	app, _, st := keyApp(t)
	_, raw, _ := newKey(t, st, "+27825550101")
	resp, _ := keyReq(t, app, http.MethodGet, raw, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, _ = keyReq(t, app, http.MethodGet, raw+"x", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.NotContains(t, buf.String(), raw[len(store.APIKeyPrefix):])
}

// Security: an account being deleted cannot use its API keys, even before the
// keys themselves are revoked.
func TestDeletingUsersKeyIs401(t *testing.T) {
	app, _, st := keyApp(t)
	alice, raw, _ := newKey(t, st, "+27825550101")
	resp, _ := keyReq(t, app, http.MethodGet, raw, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, st.SetUserStatus(context.Background(), alice.ID, store.StatusDeleting))
	resp, _ = keyReq(t, app, http.MethodGet, raw, nil)
	requireKey401(t, resp)
}
