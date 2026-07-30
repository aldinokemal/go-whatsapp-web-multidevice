# UI ADAPTERS

Generated: 2026-06-06

## OVERVIEW

`ui/` adapts HTTP REST, MCP tools (streamable HTTP, mounted at `/mcp` by `rest`), and websockets to domain usecases. It should parse transport payloads and delegate behavior.

## STRUCTURE

```text
ui/
|-- rest/          # Fiber handlers, helpers, middleware
|-- mcp/           # MCP tools, route mounting, and device resolution
`-- websocket/     # Browser device/status broadcast hub
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add REST route | `rest/<domain>.go` | `InitRest*` registers paths and stores the domain service. |
| Add REST middleware | `rest/middleware/` | Use `fiber.Test()` for middleware tests. |
| Add MCP tool | `mcp/<domain>.go` | Define `tool*`, `handle*`, and register in `Add*Tools`. |
| Device context | `rest/middleware/device.go`, `mcp/route.go`, `mcp/device.go` | REST uses header/query; MCP uses the connection's `X-Device-Id` header (set in `route.go`'s `HTTPContextFunc`), overridable per call by a `device_id` tool argument (`resolveDeviceContext`), else the default/only device. |
| Send transport fields | `rest/send.go`, `mcp/send.go` | REST receives full send DTOs; MCP send is a smaller tool subset with manual args. |
| Chatwoot webhook | `rest/chatwoot.go` | Public route, optional shared secret, echo suppression, read/delete sync. |
| Websocket changes | `websocket/websocket.go`, `../views/index.html` | Browser connects with `?device_id=`. |

## CONVENTIONS

- REST handlers parse request bodies with Fiber, add uploaded files from `FormFile`, sanitize phones where existing handlers do, then call usecases.
- REST success payloads use `utils.ResponseData{Status: 200, Code: "SUCCESS", Message: ..., Results: ...}`.
- Device management routes are registered outside `DeviceMiddleware`; most operational routes are wrapped by it.
- Chatwoot webhook is registered before basic auth so Chatwoot can POST without the app's Basic Auth header.
- If `CHATWOOT_WEBHOOK_SECRET` is set, the public Chatwoot webhook must pass the shared-secret check before sending to WhatsApp.
- MCP handlers validate argument types manually and return `mcp.NewToolResultText(...)`.
- MCP handlers must resolve the device via `resolveDeviceContext` (`mcp/device.go`) before device-bound usecase calls.
- MCP exposes 5 consolidated tools: `whatsapp_send`, `whatsapp_message`, `whatsapp_chat`, `whatsapp_group`, `whatsapp_app`; each dispatches on a `type`/`action` argument rather than being one tool per operation.

## ANTI-PATTERNS

- Do not put WhatsApp business logic in REST/MCP handlers.
- Do not assume every MCP call carries a `device_id` argument; fall through to the connection's `X-Device-Id`-derived device via `resolveDeviceContext`, and only then the default/only device.
- Do not register device-scoped REST operations outside `DeviceMiddleware`.
- Do not expose new unauthenticated REST paths unless they are health checks or explicitly public webhooks.
- Do not remove Chatwoot `source_id` and sent-message cache echo guards without a replacement loop breaker.
