package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type seenRequest struct {
	method, uri, token, user, auth, cookie, body string
}

func fakeBridge(t *testing.T) (*httptest.Server, func() seenRequest) {
	t.Helper()
	var mu sync.Mutex
	var last seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = seenRequest{r.Method, r.URL.RequestURI(), r.Header.Get("X-CS-AI-Token"), r.Header.Get("X-CS-AI-User"),
			r.Header.Get("Authorization"), r.Header.Get("Cookie"), string(b)}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() seenRequest { mu.Lock(); defer mu.Unlock(); return last }
}

// setCSAIConfig mutates package globals; tests using it must not run in parallel.
func setCSAIConfig(t *testing.T, url, token, basePath string, basicAuth []string) {
	t.Helper()
	prevURL, prevToken, prevBase, prevAuth := config.CSAIAdminURL, config.CSAIAdminToken, config.AppBasePath, config.AppBasicAuthCredential
	t.Cleanup(func() {
		config.CSAIAdminURL, config.CSAIAdminToken, config.AppBasePath, config.AppBasicAuthCredential = prevURL, prevToken, prevBase, prevAuth
	})
	config.CSAIAdminURL, config.CSAIAdminToken, config.AppBasePath, config.AppBasicAuthCredential = url, token, basePath, basicAuth
}

func csaiApp(basePath string) *fiber.App {
	app := fiber.New()
	app.Use(newBasicAuthMiddleware(map[string]string{"stage": "pw"}))
	var r fiber.Router = app
	if basePath != "" {
		r = app.Group(basePath)
	}
	registerCSAIProxy(r)
	return app
}

func TestCSAIProxyRequiresBasicAuthAndForwards(t *testing.T) {
	bridge, last := fakeBridge(t)
	setCSAIConfig(t, bridge.URL+"/", "secret-token-0123456789abcdef", "", []string{"stage:pw"})
	app := csaiApp("")

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/cs-ai/", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Empty(t, last().uri, "bridge must not be reached without Basic Auth")

	req := httptest.NewRequest(http.MethodPut, "/cs-ai/api/settings?x=1", strings.NewReader(`{"enabled":true}`))
	req.SetBasicAuth("stage", "pw")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("X-CS-AI-Token", "forged-by-client")
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.JSONEq(t, `{"ok":true}`, string(body))

	got := last()
	require.Equal(t, http.MethodPut, got.method)
	require.Equal(t, "/cs-ai/api/settings?x=1", got.uri)
	require.Equal(t, "secret-token-0123456789abcdef", got.token, "client-supplied token must be overwritten")
	require.Equal(t, "stage", got.user)
	require.Empty(t, got.auth, "the GOWA password must not reach the bridge")
	require.Empty(t, got.cookie)
	require.Equal(t, `{"enabled":true}`, got.body)

	req = httptest.NewRequest(http.MethodGet, "/cs-ai", nil)
	req.SetBasicAuth("stage", "pw")
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "/cs-ai", last().uri)
}

func TestCSAIProxyStripsBasePath(t *testing.T) {
	bridge, last := fakeBridge(t)
	setCSAIConfig(t, bridge.URL, "secret-token-0123456789abcdef", "/gowa", []string{"stage:pw"})
	app := csaiApp("/gowa")

	req := httptest.NewRequest(http.MethodDelete, "/gowa/cs-ai/api/pauses/628123@s.whatsapp.net", nil)
	req.SetBasicAuth("stage", "pw")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "/cs-ai/api/pauses/628123@s.whatsapp.net", last().uri)
}

func TestCSAIProxyNotMountedWithoutBasicAuthOrConfig(t *testing.T) {
	bridge, last := fakeBridge(t)

	t.Run("no basic auth", func(t *testing.T) {
		// Without Basic Auth the page would be public, so it must not be mounted.
		setCSAIConfig(t, bridge.URL, "secret-token-0123456789abcdef", "", nil)
		app := fiber.New()
		registerCSAIProxy(app)
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/cs-ai/", nil))
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
	t.Run("no token", func(t *testing.T) {
		setCSAIConfig(t, bridge.URL, "", "", []string{"stage:pw"})
		app := csaiApp("")
		req := httptest.NewRequest(http.MethodGet, "/cs-ai/", nil)
		req.SetBasicAuth("stage", "pw")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
	require.Empty(t, last().uri)
}

func TestCSAIProxyBridgeDown(t *testing.T) {
	setCSAIConfig(t, "http://127.0.0.1:1", "secret-token-0123456789abcdef", "", []string{"stage:pw"})
	app := csaiApp("")
	req := httptest.NewRequest(http.MethodGet, "/cs-ai/", nil)
	req.SetBasicAuth("stage", "pw")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
}
