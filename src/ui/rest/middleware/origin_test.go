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

	t.Run("request host is trusted under https behind a TLS proxy", func(t *testing.T) {
		for _, target := range []struct {
			method string
			path   string
			header string
			value  string
			want   int
		}{
			{fiber.MethodPost, "/send/message", "Origin", "https://gowa.example", fiber.StatusOK},
			{fiber.MethodGet, "/app/logout", "Referer", "https://gowa.example/", fiber.StatusOK},
			{fiber.MethodPost, "/send/message", "Origin", "https://gowa.example:8443", fiber.StatusForbidden},
			{fiber.MethodPost, "/send/message", "Origin", "https://evil.gowa.example", fiber.StatusForbidden},
		} {
			req := httptest.NewRequest(target.method, "http://gowa.example"+target.path, nil)
			req.Host = "gowa.example"
			req.Header.Set(target.header, target.value)
			resp, err := newApp().Test(req)
			require.NoError(t, err)
			require.Equal(t, target.want, resp.StatusCode, target.value)
			require.NoError(t, resp.Body.Close())
		}
	})

	t.Run("configured app origin is matched as written", func(t *testing.T) {
		prev := config.AppTrustedOrigins
		config.AppTrustedOrigins = []string{"Capacitor://LocalHost/", "chrome-extension://abcdefgh", "null"}
		t.Cleanup(func() { config.AppTrustedOrigins = prev })

		for origin, want := range map[string]int{
			"capacitor://localhost":       fiber.StatusOK,
			"chrome-extension://abcdefgh": fiber.StatusOK,
			"capacitor://evil":            fiber.StatusForbidden,
			"chrome-extension://zzzzzzzz": fiber.StatusForbidden,
			"null":                        fiber.StatusForbidden,
		} {
			req := httptest.NewRequest(fiber.MethodPost, "http://gowa.example/send/message", nil)
			req.Host = "gowa.example"
			req.Header.Set("Origin", origin)
			resp, err := newApp().Test(req)
			require.NoError(t, err)
			require.Equal(t, want, resp.StatusCode, origin)
			require.NoError(t, resp.Body.Close())
		}
	})
}

func TestBrowserOriginGuardWithoutOriginOrReferer(t *testing.T) {
	app := fiber.New()
	app.Use(BrowserOriginGuard())
	ok := func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) }
	app.Post("/send/message", ok)
	app.Get("/app/logout", ok)
	app.Get("/app/status", ok)

	const browser = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{"curl logout", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": "curl/8.7.1"}, fiber.StatusOK},
		{"n8n logout", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": "axios/1.7.9"}, fiber.StatusOK},
		{"browser logout with no referrer", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": browser}, fiber.StatusForbidden},
		{"browser head logout with no referrer", fiber.MethodHead, "/app/logout", map[string]string{"User-Agent": browser}, fiber.StatusForbidden},
		{"same-site logout", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": browser, "Sec-Fetch-Site": "same-site"}, fiber.StatusForbidden},
		{"same-site post", fiber.MethodPost, "/send/message", map[string]string{"User-Agent": browser, "Sec-Fetch-Site": "same-site"}, fiber.StatusForbidden},
		{"same-origin referer logout", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": browser, "Referer": "http://gowa.example/"}, fiber.StatusOK},
		{"typed logout url", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": browser, "Sec-Fetch-Site": "none"}, fiber.StatusOK},
		{"same-origin fetch metadata logout", fiber.MethodGet, "/app/logout", map[string]string{"User-Agent": browser, "Sec-Fetch-Site": "same-origin"}, fiber.StatusOK},
		{"browser post with no origin", fiber.MethodPost, "/send/message", map[string]string{"User-Agent": browser}, fiber.StatusOK},
		{"browser read with no referrer", fiber.MethodGet, "/app/status", map[string]string{"User-Agent": browser}, fiber.StatusOK},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://gowa.example"+tt.path, nil)
			req.Host = "gowa.example"
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			resp, err := app.Test(req)
			require.NoError(t, err)
			require.Equal(t, tt.want, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestMutatingGETAliasesAreRejected(t *testing.T) {
	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(BrowserOriginGuard())
		ok := func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) }
		app.Get("/app/logout", ok)
		app.Get("/app/reconnect", ok)
		app.Get("/group/invite-link", ok)
		app.Get("/app/login", ok)
		app.Get("/app/login-with-code", ok)
		app.Get("/devices/:device_id", ok)
		app.Get("/devices/:device_id/login", ok)
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
		{name: "logout two slashes", method: fiber.MethodGet, target: "http://gowa.example/app/logout//"},
		{name: "logout three slashes", method: fiber.MethodGet, target: "http://gowa.example/app/logout///"},
		{name: "logout case and two slashes", method: fiber.MethodGet, target: "http://gowa.example/APP/LOGOUT//"},
		{name: "logout head two slashes", method: fiber.MethodHead, target: "http://gowa.example/app/logout//"},
		{name: "reconnect two slashes", method: fiber.MethodGet, target: "http://gowa.example/app/reconnect//"},
		{name: "invite two slashes", method: fiber.MethodGet, target: "http://gowa.example/group/invite-link//?reset=true"},
		{name: "invite head two slashes", method: fiber.MethodHead, target: "http://gowa.example/group/invite-link//?reset=true"},
		{name: "login", method: fiber.MethodGet, target: "http://gowa.example/app/login"},
		{name: "login head", method: fiber.MethodHead, target: "http://gowa.example/app/login"},
		{name: "login case and slashes", method: fiber.MethodGet, target: "http://gowa.example/APP/LOGIN//"},
		{name: "login with code", method: fiber.MethodGet, target: "http://gowa.example/app/login-with-code?phone=628111"},
		{name: "login with code head and slash", method: fiber.MethodHead, target: "http://gowa.example/App/Login-With-Code/?phone=628111"},
		{name: "device login", method: fiber.MethodGet, target: "http://gowa.example/devices/dev-1/login"},
		{name: "device login head", method: fiber.MethodHead, target: "http://gowa.example/devices/dev-1/login"},
		{name: "device login case and slashes", method: fiber.MethodGet, target: "http://gowa.example/DEVICES/DEV-1/LOGIN//"},
		{name: "device login escaped id", method: fiber.MethodGet, target: "http://gowa.example/devices/a%2Fb/login"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := newApp().Test(evil(tt.method, tt.target))
			require.NoError(t, err)
			require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}

	t.Run("device read is not guarded", func(t *testing.T) {
		resp, err := newApp().Test(evil(fiber.MethodGet, "http://gowa.example/devices/login"))
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})

	t.Run("non-canonical logout without origin is allowed", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/APP/LOGOUT", nil)
		req.Host = "gowa.example"
		resp, err := newApp().Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})

	t.Run("double slash logout without origin is allowed", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/app/logout//", nil)
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
		app.Get("/gowa/group/invite-link", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
		for _, target := range []struct {
			method string
			url    string
		}{
			{fiber.MethodGet, "http://gowa.example/gowa/app/logout/"},
			{fiber.MethodGet, "http://gowa.example/gowa/app/logout//"},
			{fiber.MethodGet, "http://gowa.example/GOWA/APP/LOGOUT//"},
			{fiber.MethodHead, "http://gowa.example/gowa/app/logout//"},
			{fiber.MethodGet, "http://gowa.example/gowa/group/invite-link//?reset=true"},
		} {
			resp, err := app.Test(evil(target.method, target.url))
			require.NoError(t, err)
			require.Equal(t, fiber.StatusForbidden, resp.StatusCode, target.url)
			require.NoError(t, resp.Body.Close())
		}
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
		"http://gowa.example/group/invite-link?reset=TRUE",
		"http://gowa.example/group/invite-link?reset=t",
		"http://gowa.example/group/invite-link?reset=1",
		"http://gowa.example/group/invite-link?reset[]=true",
		"http://gowa.example/group/invite-link?reset[]=on",
		"http://gowa.example/group/invite-link?Reset[]=true",
		"http://gowa.example/group/invite-link?reset%5B%5D=true",
		"http://gowa.example/group/invite-link?reset=false&reset[]=true",
		"http://gowa.example/group/invite-link?[]reset=true",
		"http://gowa.example/group/invite-link?re[]set=true",
		"http://gowa.example/group/invite-link?[]reset[]=true",
		"http://gowa.example/group/invite-link?r[]e[]s[]e[]t=true",
		"http://gowa.example/group/invite-link?reset=false&[]reset=true",
		"http://gowa.example/group/invite-link?[]Reset=true",
		"http://gowa.example/group/invite-link?%5B%5Dreset=true",
	} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodGet, target, nil)
			req.Host = "gowa.example"
			req.Header.Set("Origin", "https://evil.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			resp, err := app.Test(req)
			require.NoError(t, err)
			require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}

	t.Run("indexed reset does not set the field", func(t *testing.T) {
		var reset bool
		var ran bool
		indexed := fiber.New()
		indexed.Use(BrowserOriginGuard())
		indexed.Get("/group/invite-link", func(c fiber.Ctx) error {
			ran = true
			var request struct {
				Reset bool `query:"reset"`
			}
			if err := c.Bind().Query(&request); err != nil {
				return err
			}
			reset = request.Reset
			return c.SendStatus(fiber.StatusOK)
		})
		req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/group/invite-link?reset[0]=true", nil)
		req.Host = "gowa.example"
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		resp, err := indexed.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
		require.True(t, ran)
		require.False(t, reset)
	})

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
	require.Equal(t, "capacitor://localhost", config.NormalizeOrigin("Capacitor://LocalHost/"))
	require.Empty(t, config.NormalizeOrigin("file://"))
}
