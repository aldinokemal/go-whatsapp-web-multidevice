package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

const muTestPublicURL = "https://wtf.test"

// newMultiUserApp builds the app the way restServer does in multi-user mode:
// Setup, then Install (session, pages, OAuth + /mcp, the API gate, /files),
// then the upstream REST routes, device group last. Devices d_alice and d_bob
// exist; nobody owns them yet. mcpDeps are the usecases behind the MCP tools.
func newMultiUserApp(t *testing.T, mcpDeps uimcp.Deps) (*fiber.App, *multiuser.Runtime, *whatsapp.DeviceManager) {
	return newMultiUserAppWith(t, mcpDeps, nil)
}

func newMultiUserAppWith(t *testing.T, mcpDeps uimcp.Deps, mod func(*multiuser.Settings)) (*fiber.App, *multiuser.Runtime, *whatsapp.DeviceManager) {
	t.Helper()
	savedMU, savedDB, savedMedia := config.MultiUserEnabled, config.McpOAuthDBURI, config.PathMedia
	t.Cleanup(func() { config.MultiUserEnabled, config.McpOAuthDBURI, config.PathMedia = savedMU, savedDB, savedMedia })
	config.MultiUserEnabled = true
	config.McpOAuthDBURI = "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "oauth.db"))
	config.PathMedia = filepath.Join(t.TempDir(), "media")

	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	for _, id := range []string{"d_alice", "d_bob"} {
		_, err := dm.CreateDevice(context.Background(), id)
		require.NoError(t, err)
	}
	s := multiuser.Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: muTestPublicURL, SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{},
		MCPEnabled: true, MCPClientHosts: []string{"claude.ai"}}
	if mod != nil {
		mod(&s)
	}
	rt, err := multiuser.Setup(s, dm, usecase.NewDeviceService(dm, nil), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	app := fiber.New(fiber.Config{ErrorHandler: web.ErrorHandler(rt.Web)})
	app.Use(middleware.Recovery())
	rt.Install(app, dm, mcpDeps)
	rest.InitRestDevice(app, rt.Devices)
	rest.InitRestAppInfo(app)
	registerDeviceScopedRoutes(app.Group("", middleware.DeviceMiddleware(dm)))
	return app, rt, dm
}
