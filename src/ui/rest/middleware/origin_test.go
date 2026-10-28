package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestBrowserOriginGuard(t *testing.T) {
	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(BrowserOriginGuard())
		ok := func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) }
		app.Post("/send/message", ok)
		app.Get("/app/logout", ok)
		app.Get("/app/status", ok)
		app.Get("/group/invite-link", ok)
		return app
	}

	t.Run("cross-site origin is rejected on a mutating route", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodPost, "http://gowa.example/send/message", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://evil.example")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "CROSS_SITE_REQUEST_BLOCKED")
	})

	t.Run("missing origin is allowed", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodPost, "http://gowa.example/send/message", nil)
		req.Host = "gowa.example"
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("trusted same-host origin is allowed", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodPost, "http://gowa.example/send/message", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "http://gowa.example")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("configured origin is allowed", func(t *testing.T) {
		prev := config.AppTrustedOrigins
		config.AppTrustedOrigins = []string{"https://ui.example"}
		t.Cleanup(func() { config.AppTrustedOrigins = prev })

		req := httptest.NewRequest(fiber.MethodPost, "http://gowa.example/send/message", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://ui.example")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("logout get is rejected for a foreign origin", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/app/logout", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://evil.example")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("invite-link reset is rejected for a foreign referer", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/group/invite-link?group_id=1&reset=true", nil)
		req.Host = "gowa.example"
		req.Header.Set("Referer", "https://evil.example/phish")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("invite-link read is not treated as a mutation", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/group/invite-link?group_id=1", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://evil.example")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("cross-site fetch metadata without origin is rejected", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/app/logout", nil)
		req.Host = "gowa.example"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})
}

func TestMutatingGETAliasesAreRejected(t *testing.T) {
	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(BrowserOriginGuard())
		ok := func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) }
		app.Get("/app/logout", ok)
		app.Get("/app/reconnect", ok)
		app.Get("/group/invite-link", ok)
		return app
	}
	evil := func(method, target string) *http.Request {
		req := httptest.NewRequest(method, target, nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		return req
	}

	cases := []struct {
		name   string
		method string
		target string
	}{
		{name: "logout trailing slash", method: fiber.MethodGet, target: "http://gowa.example/app/logout/"},
		{name: "logout different case", method: fiber.MethodGet, target: "http://gowa.example/APP/LOGOUT"},
		{name: "logout head", method: fiber.MethodHead, target: "http://gowa.example/app/logout"},
		{name: "reconnect trailing slash", method: fiber.MethodGet, target: "http://gowa.example/app/reconnect/"},
		{name: "reconnect head", method: fiber.MethodHead, target: "http://gowa.example/app/reconnect"},
		{name: "invite trailing slash", method: fiber.MethodGet, target: "http://gowa.example/group/invite-link/?reset=true"},
		{name: "invite different case", method: fiber.MethodGet, target: "http://gowa.example/GROUP/INVITE-LINK?reset=true"},
		{name: "invite head", method: fiber.MethodHead, target: "http://gowa.example/group/invite-link?reset=true"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := newApp().Test(evil(tt.method, tt.target))
			require.NoError(t, err)
			require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}

	t.Run("non-canonical logout without origin is allowed", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/APP/LOGOUT", nil)
		req.Host = "gowa.example"
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})

	t.Run("base path trailing slash", func(t *testing.T) {
		prev := config.AppBasePath
		config.AppBasePath = "/gowa"
		t.Cleanup(func() { config.AppBasePath = prev })

		app := fiber.New()
		app.Use(BrowserOriginGuard())
		app.Get("/gowa/app/logout", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
		resp, err := app.Test(evil(fiber.MethodGet, "http://gowa.example/gowa/app/logout/"))
		require.NoError(t, err)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})
}

func TestInviteLinkResetMatchesBinder(t *testing.T) {
	app := fiber.New()
	app.Use(BrowserOriginGuard())
	app.Get("/group/invite-link", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, target := range []string{
		"http://gowa.example/group/invite-link?reset=on",
		"http://gowa.example/group/invite-link?Reset=true",
		"http://gowa.example/group/invite-link?reset=false&reset=true",
		"http://gowa.example/group/invite-link?reset=false&reset=on",
		"http://gowa.example/group/invite-link?reset=true&reset=false",
	} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodGet, target, nil)
			req.Host = "gowa.example"
			req.Header.Set("Origin", "https://evil.example")
			resp, err := app.Test(req)
			require.NoError(t, err)
			require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}

	t.Run("final false only is not a reset", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/group/invite-link?reset=false", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://evil.example")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})
}

func TestNormalizeOrigin(t *testing.T) {
	require.Equal(t, "https://ui.example", config.NormalizeOrigin("https://ui.example/dashboard"))
	require.Equal(t, "http://localhost:3000", config.NormalizeOrigin("http://localhost:3000"))
	require.Equal(t, "https://ui.example", config.NormalizeOrigin("https://ui.example:443"))
	require.Empty(t, config.NormalizeOrigin("null"))
	require.Empty(t, config.NormalizeOrigin("*"))
}
