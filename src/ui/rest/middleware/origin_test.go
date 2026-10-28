package middleware

import (
	"io"
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

func TestNormalizeOrigin(t *testing.T) {
	require.Equal(t, "https://ui.example", config.NormalizeOrigin("https://ui.example/dashboard"))
	require.Equal(t, "http://localhost:3000", config.NormalizeOrigin("http://localhost:3000"))
	require.Equal(t, "https://ui.example", config.NormalizeOrigin("https://ui.example:443"))
	require.Empty(t, config.NormalizeOrigin("null"))
	require.Empty(t, config.NormalizeOrigin("*"))
}
