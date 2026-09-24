# Transport adapters

`rest/`, `mcp/`, and `websocket/` parse transport inputs and delegate to domain
usecases. REST and MCP share the `rest` process and device manager.

## REST and websockets

- `rest/<domain>.go` registers routes with `InitRest*`. Parse bodies/files and use
  the existing `utils.ResponseData` success envelope (`Status: 200`, `Code: "SUCCESS"`).
- Device operations belong behind `DeviceMiddleware`; management routes sit
  outside it. Pass `whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c))`
  to device-bound usecases. Browser websockets select a device with `?device_id=`.
- Route/auth order lives in `../cmd/rest.go`. Chatwoot webhooks are mounted before
  Basic Auth but enforce `CHATWOOT_WEBHOOK_SECRET` when configured. Keep manual
  sync/config routes authenticated and preserve webhook device routing and echo guards.
  New public routes need an explicit public contract, such as a health check or webhook.

## MCP

- `mcp/<domain>.go` defines tools/handlers and registers them via `Add*Tools`.
  The consolidated `whatsapp_send`, `whatsapp_schedule`, `whatsapp_message`,
  `whatsapp_chat`, `whatsapp_group`, and `whatsapp_app` tools dispatch on `type` or `action`.
  MCP send arguments cover a subset of REST DTOs; validate types and preserve result formats.
- Resolve device-bound calls with `resolveDeviceContext` in `mcp/device.go`:
  explicit `device_id` overrides the device injected by `mcp/route.go` from
  `X-Device-Id`. An empty header may resolve the default/only device at the HTTP
  layer. If no device reaches the tool, resolution returns an error.
- Keep streaming disabled in the Fiber adaptor: an idle GET SSE connection can
  retain a goroutine/connection until shutdown. `mcp/route.go` mounts POST and DELETE.
- For authentication changes, read [MCP OAuth](../../docs/mcp-oauth.md) and
  `../cmd/mcp_oauth.go`. OAuth-protected MCP mounts before global Basic Auth;
  when OAuth is disabled, MCP uses the ordinary authenticated router.

Use the relevant handler tests, Fiber's `app.Test`, or MCP tool tests for changed
routing, auth, parsing, and device resolution behavior.
