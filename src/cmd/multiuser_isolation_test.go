package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// publicRoutes may be reached signed out. Anything else that answers an
// anonymous request with something other than 303/401 fails this test.
//
// GET /app/info is deliberately NOT here: it is registered on apiGroup, so in
// multi-user mode it sits behind the same RequireAPI gate as the rest of the
// upstream REST API and answers 401 signed out, confirmed by a local smoke
// run. It carries no device data, so it is excluded from the step-2
// (ownership) check below via isDeviceScoped instead.
var publicRoutes = map[string]bool{
	"GET /health": true, "GET /signin": true, "GET /signin/qr.png": true, "GET /signin/verify": true,
	"POST /signin/verify": true, "GET /ui/static/*": true,
	"POST /auth/passkey/login/begin": true, "POST /auth/passkey/login/finish": true,
	// PWA and browser plumbing: no user data, must load before sign-in.
	"GET /manifest.webmanifest": true, "GET /sw.js": true, "GET /offline": true,
	"GET /favicon.ico": true, "GET /apple-touch-icon.png": true, "GET /robots.txt": true,
}

func TestEveryRouteIsGatedAndOwnerScoped(t *testing.T) {
	savedMU := config.MultiUserEnabled
	t.Cleanup(func() { config.MultiUserEnabled = savedMU })
	config.MultiUserEnabled = true

	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	for _, id := range []string{"d_alice", "d_bob"} {
		_, err := dm.CreateDevice(context.Background(), id)
		require.NoError(t, err)
	}
	s := multiuser.Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := multiuser.Setup(s, dm, usecase.NewDeviceService(dm, nil), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	app := fiber.New()
	app.Use(middleware.Recovery())
	rt.Install(app)
	rest.InitRestDevice(app, rt.Devices)
	rest.InitRestAppInfo(app)
	registerDeviceScopedRoutes(app.Group("", middleware.DeviceMiddleware(dm)))

	ctx := context.Background()
	alice, _ := rt.Store.CreateUser(ctx, "+27825550101")
	bob, _ := rt.Store.CreateUser(ctx, "+27825550102")
	require.NoError(t, rt.Store.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, rt.Store.SetOwner(ctx, "d_bob", bob.ID))
	rawAlice, _ := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	aliceCookie := &http.Cookie{Name: rt.Auth.CookieName(), Value: rawAlice}
	csrf := csrfForRaw(rt, rawAlice)

	var checked int
	for _, r := range app.GetRoutes(true) {
		if r.Method == fiber.MethodHead || r.Method == fiber.MethodOptions || r.Method == fiber.MethodConnect || r.Method == fiber.MethodTrace {
			continue
		}
		key := r.Method + " " + r.Path
		path := fill(r.Path, "d_bob")

		// 1. Signed out: refused (unless public).
		if !publicRoutes[key] {
			resp, err := app.Test(httptest.NewRequest(r.Method, path, nil))
			require.NoError(t, err, key)
			require.Contains(t, []int{http.StatusSeeOther, http.StatusUnauthorized}, resp.StatusCode, "anonymous %s", key)
		}

		// 2. Alice aiming at Bob's device: never reaches a handler.
		if !strings.Contains(r.Path, ":device_id") && !strings.Contains(r.Path, ":id") && !isDeviceScoped(r.Path) {
			continue
		}
		req := newScopedRequest(r.Method, path, key)
		req.Header.Set(middleware.DeviceIDHeader, "d_bob")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(aliceCookie)
		resp, err := app.Test(req)
		require.NoError(t, err, key)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, "alice -> bob's device via %s", key)
		checked++
	}
	require.Greater(t, checked, 60, "the route walk actually covered the upstream API (guard against an empty walk)")
}

// bodyOverrides holds the one route whose handler 400s on a required field
// before the ownership check runs, even past an empty-but-valid JSON body:
// PATCH /devices/:device_id/webhook requires webhook_url non-nil.
var bodyOverrides = map[string]string{
	"PATCH /devices/:device_id/webhook": `{"webhook_url":"https://example.test/hook"}`,
}

// newScopedRequest builds the step-2 request for key. Most handlers call
// c.Bind().Body before anything else, so a body-less PATCH/PUT/POST would be
// refused with 400 on an empty-body parse failure before ever reaching the
// ownership check this test is probing. An empty JSON object parses cleanly
// (every field keeps its zero value) and lets the request proceed to that
// check; bodyOverrides covers the one handler that still needs a field set.
func newScopedRequest(method, path, routeKey string) *http.Request {
	switch method {
	case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
		body := "{}"
		if b, ok := bodyOverrides[routeKey]; ok {
			body = b
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(fiber.HeaderContentType, "application/json")
		return req
	default:
		return httptest.NewRequest(method, path, nil)
	}
}

// fill replaces every path parameter: device params get the target device,
// others a harmless placeholder.
func fill(path, device string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		switch {
		case p == ":device_id" || p == ":id":
			parts[i] = device
		case strings.HasPrefix(p, ":"):
			parts[i] = "x"
		case p == "*":
			parts[i] = "x"
		}
	}
	return strings.Join(parts, "/")
}

// isDeviceScoped: every route registered on the device group. They are all
// routes except our own pages, /devices management (covered by :device_id)
// and the public set.
func isDeviceScoped(path string) bool {
	if path == "/ui/devices" {
		return false // POST /ui/devices (AddDevice) ignores any device id and creates one for the caller
	}
	for _, own := range []string{"/signin", "/signout", "/ui/static", "/auth/passkey", "/account", "/admin", "/health", "/devices",
		"/manifest.webmanifest", "/sw.js", "/offline", "/favicon.ico", "/apple-touch-icon.png", "/robots.txt"} {
		if strings.HasPrefix(path, own) {
			return false
		}
	}
	return path != "/" && path != "/app/info"
}

// csrfForRaw derives the CSRF token for a raw session id the same way a real
// request would: run it through the auth middleware (which validates it
// against the store and loads the session into the request) and read the
// token back off the context, rather than reaching into auth's unexported
// Locals keys from outside the package.
func csrfForRaw(rt *multiuser.Runtime, raw string) string {
	app := fiber.New()
	app.Use(rt.Auth.Middleware())
	var tok string
	app.Get("/", func(c fiber.Ctx) error { tok = rt.Auth.CSRFToken(c); return nil })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: rt.Auth.CookieName(), Value: raw})
	_, _ = app.Test(req)
	return tok
}
