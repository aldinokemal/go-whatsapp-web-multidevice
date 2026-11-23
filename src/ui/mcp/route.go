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
			if deps.ResolveDevice != nil { // WTF: owner-checked, from the auth middleware's context
				return hookDeviceContext(ctx, r, deps.ResolveDevice)
			}
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
	if deps.ResolveDevice != nil {
		// WTF: carry the Fiber context (the signed-in user) to the handler.
		handler = adaptor.HTTPHandlerWithContext(httpServer)
	}
	// POST carries JSON-RPC calls; DELETE is part of the streamable-HTTP
	// session lifecycle. GET is intentionally not mounted: with streaming
	// disabled mcp-go would just 405 it, so Fiber's own 404 for an
	// unmounted method is equivalent and keeps the route surface narrow.
	router.Post("/mcp", handler)
	router.Delete("/mcp", handler)
}

// WTF: hookDeviceContext builds the tool context in multi-user mode: the
// request's own context, plus the values the Fiber middleware put on its
// context (the authenticated user), plus the device the X-Device-Id header
// names when the user owns it. An empty header resolves the user's only
// device; none or several leave no device, so tools ask for one. Without the
// Fiber context there is no user, and nothing resolves.
func hookDeviceContext(ctx context.Context, r *http.Request, resolve func(context.Context, string) (*whatsapp.DeviceInstance, string, error)) context.Context {
	local, ok := adaptor.LocalContextFromHTTPRequest(r)
	if !ok || local == nil {
		return ctx
	}
	ctx = mergedContext{Context: ctx, values: local}
	deviceID := strings.TrimSpace(r.Header.Get(middleware.DeviceIDHeader))
	inst, _, err := resolve(ctx, deviceID)
	if err != nil || inst == nil {
		return ctx
	}
	return whatsapp.ContextWithDevice(ctx, inst)
}

// mergedContext keeps the request context's deadline, cancellation and
// values, and falls back to values for anything it does not hold.
type mergedContext struct {
	context.Context
	values context.Context
}

func (m mergedContext) Value(key any) any {
	if v := m.Context.Value(key); v != nil {
		return v
	}
	return m.values.Value(key)
}
