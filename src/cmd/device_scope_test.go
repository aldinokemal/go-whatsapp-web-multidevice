package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// fakeDevices maps aliases and JIDs to canonical device ids.
var fakeDevices = map[string]string{
	"keuangan": "keuangan", "6281111@s.whatsapp.net": "keuangan",
	"admin-kelas": "admin-kelas",
	"cs-ai":       "cs-ai", "6282222@s.whatsapp.net": "cs-ai",
}

func fakeResolve(id string) (string, bool) {
	c, ok := fakeDevices[id]
	return c, ok
}

// scopeApp mimics restServer: Basic Auth, then the scope middleware, then
// routes that echo which device header reached them.
func scopeApp(basePath string) *fiber.App {
	app := fiber.New()
	app.Use(newBasicAuthMiddleware(map[string]string{"stage": "pw", "dashboard": "pw", "cs-bridge": "pw"}))
	app.Use(newDeviceScopeMiddleware(map[string][]string{
		"dashboard": {"keuangan", "admin-kelas"},
		"cs-bridge": {"cs-ai"},
	}, basePath, fakeResolve))
	var r fiber.Router = app
	if basePath != "" {
		r = app.Group(basePath)
	}
	r.All("/*", func(c fiber.Ctx) error {
		return c.SendString("ok device=" + c.Get(middleware.DeviceIDHeader))
	})
	return app
}

type call struct {
	user, method, path, device string
}

func do(t *testing.T, app *fiber.App, c call) (int, string) {
	t.Helper()
	req := httptest.NewRequest(c.method, c.path, nil)
	req.SetBasicAuth(c.user, "pw")
	if c.device != "" {
		req.Header.Set(middleware.DeviceIDHeader, c.device)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestDeviceScopeAllowsOwnDevicesOnly(t *testing.T) {
	app := scopeApp("")
	cases := []struct {
		c    call
		want int
	}{
		// Unrestricted account: unchanged behaviour.
		{call{"stage", "POST", "/send/message", "cs-ai"}, 200},
		{call{"stage", "POST", "/devices/cs-ai/logout", ""}, 200},
		{call{"stage", "GET", "/mcp", ""}, 200},

		// Own devices, by alias or JID.
		{call{"dashboard", "POST", "/send/message", "keuangan"}, 200},
		{call{"dashboard", "POST", "/send/image", "admin-kelas"}, 200},
		{call{"dashboard", "POST", "/send/message", "6281111@s.whatsapp.net"}, 200},
		{call{"dashboard", "GET", "/chat/628@s.whatsapp.net/messages", "keuangan"}, 200},
		{call{"dashboard", "GET", "/chats", "keuangan"}, 200},
		{call{"dashboard", "GET", "/user/check?phone=628", "keuangan"}, 200},
		{call{"dashboard", "GET", "/send/schedules", "keuangan"}, 200},
		{call{"dashboard", "GET", "/devices/keuangan/status", ""}, 200},
		{call{"dashboard", "GET", "/app/info", "keuangan"}, 200},
		{call{"cs-bridge", "POST", "/send/chat-presence", "cs-ai"}, 200},

		// Someone else's device.
		{call{"dashboard", "POST", "/send/message", "cs-ai"}, 403},
		{call{"dashboard", "POST", "/send/message", "6282222@s.whatsapp.net"}, 403},
		{call{"dashboard", "POST", "/send/message", "unknown-device"}, 403},
		{call{"cs-bridge", "POST", "/send/message", "keuangan"}, 403},
		{call{"dashboard", "GET", "/devices/cs-ai/status", ""}, 403},

		// Management and other surfaces are closed, even for own devices.
		{call{"dashboard", "POST", "/devices/keuangan/logout", ""}, 403},
		{call{"dashboard", "PATCH", "/devices/cs-ai/webhook", ""}, 403},
		{call{"dashboard", "GET", "/devices/keuangan/webhook", ""}, 403},
		{call{"dashboard", "DELETE", "/devices/keuangan", ""}, 403},
		{call{"dashboard", "POST", "/devices", ""}, 403},
		{call{"dashboard", "GET", "/devices", ""}, 403},
		{call{"dashboard", "GET", "/app/logout", "keuangan"}, 403},
		{call{"dashboard", "GET", "/app/login", "keuangan"}, 403},
		{call{"dashboard", "POST", "/user/pushname", "keuangan"}, 403},
		{call{"dashboard", "POST", "/mcp", "keuangan"}, 403},
		{call{"dashboard", "GET", "/ws", "keuangan"}, 403},
		{call{"dashboard", "GET", "/cs-ai/", ""}, 403},
		{call{"dashboard", "GET", "/", ""}, 403},
		{call{"dashboard", "PUT", "/devices/keuangan/chatwoot/config", ""}, 403},

		// Routing is case-insensitive; the check must be too.
		{call{"dashboard", "POST", "/SEND/message", "cs-ai"}, 403},
		{call{"dashboard", "POST", "/Devices/keuangan/Logout", ""}, 403},
		{call{"dashboard", "POST", "//send/./message", "cs-ai"}, 403},
	}
	for _, tc := range cases {
		got, body := do(t, app, tc.c)
		require.Equalf(t, tc.want, got, "%+v → %s", tc.c, body)
	}
}

func TestDeviceScopePinsSingleDeviceAndRequiresChoice(t *testing.T) {
	app := scopeApp("")
	// Single allowed device: missing header is pinned to it, never the server default.
	code, body := do(t, app, call{"cs-bridge", "POST", "/send/message", ""})
	require.Equal(t, 200, code)
	require.Equal(t, "ok device=cs-ai", body)

	// Several devices: the caller must choose.
	code, _ = do(t, app, call{"dashboard", "POST", "/send/message", ""})
	require.Equal(t, http.StatusBadRequest, code)

	// device_id query is checked too.
	req := httptest.NewRequest("POST", "/send/message?device_id=cs-ai", nil)
	req.SetBasicAuth("dashboard", "pw")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestDeviceScopeWithBasePath(t *testing.T) {
	app := scopeApp("/gowa")
	code, _ := do(t, app, call{"dashboard", "POST", "/gowa/send/message", "keuangan"})
	require.Equal(t, 200, code)
	code, _ = do(t, app, call{"dashboard", "POST", "/gowa/send/message", "cs-ai"})
	require.Equal(t, 403, code)
	code, _ = do(t, app, call{"dashboard", "POST", "/gowa/devices/keuangan/logout", ""})
	require.Equal(t, 403, code)
	code, _ = do(t, app, call{"dashboard", "POST", "/send/message", "keuangan"})
	require.Equal(t, 403, code, "paths outside the base path are not messaging endpoints")
}

func TestParseAndValidateBasicAuthDevices(t *testing.T) {
	got, err := parseBasicAuthDevices(" dashboard : keuangan | admin-kelas , cs-bridge:cs-ai ")
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"dashboard": {"keuangan", "admin-kelas"}, "cs-bridge": {"cs-ai"}}, got)

	empty, err := parseBasicAuthDevices("")
	require.NoError(t, err)
	require.Empty(t, empty)

	for _, bad := range []string{"dashboard", "dashboard:", ":keuangan", "a:x,a:y"} {
		_, err := parseBasicAuthDevices(bad)
		require.Errorf(t, err, "input %q", bad)
	}

	require.NoError(t, validateBasicAuthDevices(got, []string{"stage:x", "dashboard:y", "cs-bridge:z"}))
	err = validateBasicAuthDevices(got, []string{"stage:x", "dashboard:y"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "cs-bridge"))
	require.Error(t, validateBasicAuthDevices(got, nil))
	require.NoError(t, validateBasicAuthDevices(nil, nil))
}

// Restricted accounts must not obtain MCP access through OAuth or Basic Auth on /mcp.
func TestMcpOAuthRejectsRestrictedAccounts(t *testing.T) {
	prev := config.AppBasicAuthDevices
	t.Cleanup(func() { config.AppBasicAuthDevices = prev })
	config.AppBasicAuthDevices = map[string][]string{"dashboard": {"keuangan"}}

	validate, err := mcpOAuthCredentialValidator([]string{"stage:pw", "dashboard:pw"})
	require.NoError(t, err)
	require.True(t, validate("stage", "pw"))
	require.False(t, validate("dashboard", "pw"))
}
