# UI ADAPTERS

Generated: 2026-06-05

## OVERVIEW

`ui/` adapts HTTP REST, MCP SSE tools, and websockets to domain usecases. It should parse transport payloads and delegate behavior.

## STRUCTURE

```text
ui/
|-- rest/          # Fiber handlers, helpers, middleware
|-- mcp/           # MCP tools and default-device context helper
`-- websocket/     # Browser device/status broadcast hub
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add REST route | `rest/<domain>.go` | `InitRest*` registers paths and stores the domain service. |
| Add REST middleware | `rest/middleware/` | Use `fiber.Test()` for middleware tests. |
| Add MCP tool | `mcp/<domain>.go` | Define `tool*`, `handle*`, and register in `Add*Tools`. |
| Device context | `rest/middleware/device.go`, `mcp/helpers/context.go` | REST uses header/query; MCP uses default/only device. |
| Send transport fields | `rest/send.go`, `mcp/send.go` | REST receives full send DTOs; MCP send is a smaller tool subset with manual args. |
| Websocket changes | `websocket/websocket.go`, `../views/index.html` | Browser connects with `?device_id=`. |

## CONVENTIONS

- REST handlers parse request bodies with Fiber, add uploaded files from `FormFile`, sanitize phones where existing handlers do, then call usecases.
- REST success payloads use `utils.ResponseData{Status: 200, Code: "SUCCESS", Message: ..., Results: ...}`.
- Device management routes are registered outside `DeviceMiddleware`; most operational routes are wrapped by it.
- Chatwoot webhook is registered before basic auth so Chatwoot can POST without the app's Basic Auth header.
- MCP handlers validate argument types manually and return `mcp.NewToolResultText(...)`.
- MCP handlers must call `ContextWithDefaultDevice` before device-bound usecase calls.
- MCP coverage is intentionally selective: send has 6 tools, query 5, app 5, and group 13.

## ANTI-PATTERNS

- Do not put WhatsApp business logic in REST/MCP handlers.
- Do not assume MCP receives a device header or device ID argument; use `ContextWithDefaultDevice`.
- Do not register device-scoped REST operations outside `DeviceMiddleware`.
- Do not expose new unauthenticated REST paths unless they are health checks or explicitly public webhooks.
