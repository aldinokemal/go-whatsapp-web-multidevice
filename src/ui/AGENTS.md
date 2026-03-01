# ui

User interface layer. Three transport modes: REST API, MCP server, WebSocket.

## STRUCTURE
```
ui/
├── rest/            # Fiber HTTP handlers (9 files, 1:1 with domains)
│   ├── helpers/     # Response formatting, device resolution
│   └── middleware/  # Auth, logging, CORS
├── mcp/             # MCP (Model Context Protocol) tools for AI agents
│   └── helpers/     # MCP-specific utilities
└── websocket/       # Real-time WebSocket events
```

## REST HANDLER PATTERN
Each handler file maps to one domain. Pattern:
1. Parse request (query params / body)
2. Build domain request struct
3. Call `usecase.Method(ctx, request)`
4. Return JSON response via `helpers.ResponseSuccess`

Device resolution: `helpers.ResolveDevice(c)` extracts from `X-Device-Id` header.

## CONVENTIONS
- Query param parsing: use `c.Query("param")` for strings, `strconv` for booleans
- Boolean query params: parse to `*bool` (nil = not provided)
- All REST handlers are in `rest/` directory, one file per domain
- MCP tools mirror REST endpoints but with different transport

## ANTI-PATTERNS
- Never put business logic in handlers — delegate to usecase layer
- Never access infrastructure directly from UI layer
