# UI ADAPTERS

Generated: 2026-05-24

## OVERVIEW

`ui/` adapts HTTP REST, MCP SSE tools, and websockets to domain usecases. It should parse transport payloads and delegate behavior.

## STRUCTURE

```text
ui/
├── rest/          # Fiber handlers, helpers, middleware
├── mcp/           # MCP tools and default-device context helper
└── websocket/     # Browser device/status broadcast hub
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add REST route | `rest/<domain>.go` | `InitRest*` registers paths and stores the domain service. |
| Add REST middleware | `rest/middleware/` | Use `fiber.Test()` for middleware tests. |
| Add MCP tool | `mcp/<domain>.go` | Define `tool*`, `handle*`, and register in `Add*Tools`. |
| Device context | `rest/middleware/device.go`, `mcp/helpers/context.go` | REST uses headers/query; MCP uses default/only device. |
| Websocket changes | `websocket/websocket.go`, `../views/index.html` | Browser connects with `?device_id=`. |

## CONVENTIONS

- REST handlers parse request bodies with Fiber, add uploaded files from `FormFile`, sanitize phones where existing handlers do, then call usecases.
- REST success payloads use `utils.ResponseData{Status: 200, Code: "SUCCESS", Message: ..., Results: ...}`.
- Device management routes are registered outside `DeviceMiddleware`; most operational routes are wrapped by it.
- Chatwoot webhook is registered before basic auth so Chatwoot can POST without the app's Basic Auth header.
- MCP handlers validate argument types manually and return `mcp.NewToolResultText(...)`.
- MCP tool coverage is intentionally smaller than REST; add only tools that are useful to agents.

## ANTI-PATTERNS

- Do not put WhatsApp business logic in REST/MCP handlers.
- Do not assume MCP receives a device header; use `ContextWithDefaultDevice`.
- Do not register device-scoped REST operations outside `DeviceMiddleware`.
- Do not expose new unauthenticated REST paths unless they are health checks or explicitly public webhooks.
