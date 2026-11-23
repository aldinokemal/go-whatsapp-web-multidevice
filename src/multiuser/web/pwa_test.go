package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func get(t *testing.T, e *env, path string) (*http.Response, string) {
	t.Helper()
	return e.do(t, httptest.NewRequest(http.MethodGet, path, nil))
}

func TestManifestIsPublicValidAndUsesProductName(t *testing.T) {
	e := newEnvWith(t, "Acme Chat")
	resp, body := get(t, e, "/manifest.webmanifest")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/manifest+json")

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &raw))
	require.Equal(t, "Acme Chat", raw["name"])
	require.Equal(t, "Acme Chat", raw["short_name"])
	require.Equal(t, "/", raw["start_url"])
	require.Equal(t, "/", raw["scope"])
	require.Equal(t, "/", raw["id"])
	require.Equal(t, "standalone", raw["display"])
	require.Regexp(t, `^#[0-9A-Fa-f]{6}$`, raw["background_color"])
	require.Regexp(t, `^#[0-9A-Fa-f]{6}$`, raw["theme_color"])
	require.NotEmpty(t, raw["description"])
	require.NotEmpty(t, raw["categories"])

	icons, _ := raw["icons"].([]any)
	var has192, has512, hasMaskable bool
	for _, i := range icons {
		ic := i.(map[string]any)
		src := ic["src"].(string)
		sizes, _ := ic["sizes"].(string)
		purpose, _ := ic["purpose"].(string)
		has192 = has192 || (sizes == "192x192" && purpose != "maskable")
		has512 = has512 || (sizes == "512x512" && purpose != "maskable")
		hasMaskable = hasMaskable || purpose == "maskable"
		r, _ := get(t, e, src)
		require.Equal(t, http.StatusOK, r.StatusCode, src)
		require.Equal(t, "image/png", r.Header.Get("Content-Type"), src)
	}
	require.True(t, has192 && has512 && hasMaskable, "icons: %v", icons)
}

func newEnvWith(t *testing.T, name string) *env {
	t.Helper()
	e := newEnv(t)
	e.d.ProductName = name
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	app.Use(e.d.Auth.RequireAPI())
	e.app = app
	return e
}

func TestManifestDefaultsProductNameToWTF(t *testing.T) {
	e := newEnv(t)
	_, body := get(t, e, "/manifest.webmanifest")
	require.Contains(t, body, `"name":"WTF"`)
}

func TestServiceWorkerIsPublicJSAndNeverCached(t *testing.T) {
	e := newEnv(t)
	resp, body := get(t, e, "/sw.js")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "javascript")
	require.Contains(t, resp.Header.Get("Cache-Control"), "no-cache")
	require.Contains(t, body, "CACHE")
	require.Contains(t, body, "/offline")
	require.Contains(t, body, "/ui/static/app.css", "precache list is built from the embedded static dir")
	require.Contains(t, body, "/ui/static/brand/icon-192.png")
	// authenticated and API paths must never be cached
	for _, p := range []string{"/auth/", "/signin", "/ui/devices/"} {
		require.Contains(t, body, p)
	}
}

func TestOfflinePageIsPublicAndOnBrand(t *testing.T) {
	e := newEnv(t)
	resp, body := get(t, e, "/offline")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Contains(t, body, "page page-centered")
	require.Contains(t, body, "card card-auth")
	require.Contains(t, body, "/ui/static/app.css")
	require.Contains(t, body, "WTF")
	require.NotContains(t, body, "<script>")
}

func TestRootFaviconRobotsAndAppleIconArePublic(t *testing.T) {
	e := newEnv(t)
	resp, body := get(t, e, "/favicon.ico")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/x-icon", resp.Header.Get("Content-Type"))
	require.Equal(t, []byte{0, 0, 1, 0}, []byte(body[:4]), "ICO header")

	resp, body = get(t, e, "/robots.txt")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
	require.Contains(t, body, "Disallow: /")

	resp, _ = get(t, e, "/apple-touch-icon.png")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
}

func TestStaticContentTypes(t *testing.T) {
	e := newEnv(t)
	for path, want := range map[string]string{
		"/ui/static/brand/mark.svg":     "image/svg+xml",
		"/ui/static/brand/favicon.ico":  "image/x-icon",
		"/ui/static/brand/icon-512.png": "image/png",
		"/ui/static/app.css":            "text/css",
		"/ui/static/app.js":             "javascript",
		"/ui/static/sw-register.js":     "javascript",
	} {
		resp, _ := get(t, e, path)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		require.Contains(t, resp.Header.Get("Content-Type"), want, path)
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"), path)
	}
	require.Equal(t, "font/woff2", staticType("fonts/x.woff2"))
	require.Equal(t, "application/manifest+json", staticType("a.webmanifest"))
	require.Contains(t, staticType("a.txt"), "text/plain")
	resp, _ := get(t, e, "/ui/static/brand/mark.svg")
	require.Contains(t, resp.Header.Get("Cache-Control"), "max-age=604800")
}

func TestPWARoutesAreNotMachinePathsAndExistingRoutesUnaffected(t *testing.T) {
	for _, p := range []string{"/manifest.webmanifest", "/sw.js", "/offline", "/favicon.ico", "/robots.txt", "/apple-touch-icon.png"} {
		require.False(t, IsMachinePath(p), p)
	}
	e := newEnv(t)
	resp, _ := get(t, e, "/signin")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, _ = get(t, e, "/ui/static/htmx.min.js")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, _ = get(t, e, "/nope-nothing")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode) // gated API catch-all, as before
}
