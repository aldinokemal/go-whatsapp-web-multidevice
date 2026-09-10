package mcp

import (
	"context"
	"net/http"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// Register mounts the MCP streamable-HTTP endpoint at /mcp on the given
// router (which already carries AppBasePath and the basic-auth middleware).
//
// Device scoping mirrors REST: the X-Device-Id header picks the device for
// the connection (empty resolves the default device, same as
// DeviceMiddleware); a per-call device_id tool argument overrides it (see
// resolveDeviceContext).
func Register(router fiber.Router, dm *whatsapp.DeviceManager, deps Deps) {
	// dm is typed here, but handlers take the deviceResolver interface;
	// a nil *DeviceManager must become a nil interface, not a typed nil.
	var resolver deviceResolver
	if dm != nil {
		resolver = dm
	}

	httpServer := server.NewStreamableHTTPServer(
		NewServer(deps, resolver),
		// Stateless: no server-initiated notifications or subscriptions are
		// used, and it avoids tying a session store to Fiber's shutdown.
		server.WithStateLess(true),
		// No server-initiated notifications are used, so the standalone GET
		// SSE stream is never needed. Without this, a GET reaches mcp-go's
		// "for { select { case <-writeChan: ...; case <-ctx.Done(): } }"
		// loop; ctx there is the *fasthttp.RequestCtx, whose Done() channel
		// only closes on server shutdown (not client disconnect), so the
		// goroutine, the fasthttp body-stream writer, and the connection/FD
		// would all be pinned forever through the fasthttp adaptor.
		server.WithDisableStreaming(true),
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			if dm == nil {
				return ctx
			}
			deviceID := strings.TrimSpace(r.Header.Get(middleware.DeviceIDHeader))
			inst, _, err := dm.ResolveDevice(deviceID)
			if err != nil {
				// Leave the context empty; handlers surface a tool error
				// ("device identification required") on use.
				logrus.Debugf("MCP device resolution failed for %q: %v", deviceID, err)
				return ctx
			}
			return whatsapp.ContextWithDevice(ctx, inst)
		}),
	)

	handler := adaptor.HTTPHandler(httpServer)
	// POST carries JSON-RPC calls; DELETE is part of the streamable-HTTP
	// session lifecycle. GET is required by the streamable-HTTP handshake:
	// clients such as Claude Code or Cursor perform a GET first to agree on
	// MCP-Protocol-Version before their initial POST, and drop the server on
	// any non-2xx. With streaming disabled (and the server stateless) there
	// are no server-initiated messages, so a minimal 200 is spec-compliant.
	router.Get("/mcp", func(c fiber.Ctx) error {
		c.Set("MCP-Protocol-Version", "2025-03-26")
		return c.SendString("")
	})
	router.Post("/mcp", handler)
	router.Delete("/mcp", handler)
}
