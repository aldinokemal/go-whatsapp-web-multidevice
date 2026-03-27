# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-01

## OVERVIEW

Go-based WhatsApp Web API server supporting REST API and MCP (Model Context Protocol) modes. Multi-device management via whatsmeow library.

## STRUCTURE
```
go-whatsapp-web-multidevice/
├── src/                    # All source code
│   ├── cmd/                # Cobra CLI commands (rest, mcp)
│   ├── domains/            # Business domain contracts (interfaces + DTOs)
│   ├── infrastructure/     # External integrations
│   │   ├── whatsapp/       # WhatsApp protocol (whatsmeow) — 28 files
│   │   ├── chatstorage/    # SQLite chat/message persistence
│   │   └── chatwoot/       # Chatwoot CRM integration
│   ├── ui/                 # Transport layers (REST, MCP, WebSocket)
│   ├── usecase/            # Application logic (bridges domain ↔ infra)
│   ├── validations/        # ozzo-validation input checks + tests
│   ├── views/              # Vue.js 3 components (Semantic UI)
│   ├── pkg/                # Shared utilities
│   ├── config/             # Viper config binding
│   └── storages/           # Runtime DB files (SQLite)
├── docs/                   # OpenAPI spec (openapi.yaml)
└── gallery/                # Screenshot examples
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add new message type | `domains/send/`, `usecase/send.go`, `validations/send_validation.go` | 3-file pattern |
| Add new API endpoint | `ui/rest/`, `usecase/`, `domains/` | Handler → usecase → domain |
| Handle WhatsApp event | `infrastructure/whatsapp/event_*.go` | Register in `event_handler.go` switch |
| Add DB migration | `infrastructure/chatstorage/sqlite_repository.go` → `getMigrations()` | Append only, never insert |
| Add MCP tool | `ui/mcp/` | Mirrors REST endpoints |
| Add Vue component | `views/components/` | Plain JS, no .vue SFC |
| Device management | `infrastructure/whatsapp/device_manager.go` | 602-line central orchestrator |

## COMMANDS
```bash
cd src && go run . rest          # Run REST API mode
cd src && go run . mcp           # Run MCP server mode
cd src && go build -o whatsapp   # Build binary
cd src && go test ./...          # Run all tests
cd src && go vet ./...           # Static analysis
cd src && go fmt ./...           # Format code
go mod tidy                      # Update dependencies
```

## CRITICAL: Device ID vs JID

Two distinct identifiers — confusing them causes silent data bugs:

- **Device ID** (`instance.ID()`): User alias or UUID (e.g., `"my-device"`)
- **JID** (`instance.JID()`): WhatsApp JID (e.g., `"6289605618749@s.whatsapp.net"`)

**The `chats`/`messages` tables store device_id as the JID** (without device number). For DB queries:
```go
deviceID := client.Store.ID.ToNonAD().String()  // ✅ "6289605618749@s.whatsapp.net"
// NOT instance.ID()  // ❌ may return "6289605618749:11@s.whatsapp.net" or alias
```

## CONVENTIONS

- **Clean Architecture**: `domains/` → `usecase/` → `ui/` (never reverse)
- **1:1 mapping**: Each domain has matching usecase, validation, and UI handler files
- **Device scoping**: All chat/message queries must include `device_id`
- **LID normalization**: WhatsApp sends `@lid` JIDs — always call `NormalizeJIDFromLID()` before DB ops
- **Optional booleans**: Use `*bool` for optional filter params (nil = not set)
- **Config priority**: CLI flags > env vars > `.env` file

## ANTI-PATTERNS

- **Never** query chats/messages without device_id scoping
- **Never** use raw event JIDs for DB lookups without `NormalizeJIDFromLID`
- **Never** add `IChatStorageRepository` methods without updating both wrapper files (`chatstorage_wrapper.go` + `device_repository.go`)
- **Never** insert migrations in the middle — always append to `getMigrations()`
- **Never** put business logic in domain packages — they define contracts only

## KEY DEPENDENCIES

| Package | Role |
|---------|------|
| `go.mau.fi/whatsmeow` | WhatsApp Web protocol |
| `github.com/gofiber/fiber/v2` | REST web framework |
| `github.com/mark3labs/mcp-go` | MCP server |
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Config management |
| `github.com/ozzo/ozzo-validation` | Input validation |

## NOTES

- **Go 1.25.0+** required (see `src/go.mod`)
- REST and MCP modes cannot run simultaneously (whatsmeow limitation)
- Media files stored in `src/statics/media/`
- FFmpeg required for media processing
- HTML/JS assets embedded in binary via Go's `embed`
- Database: SQLite default, PostgreSQL supported via `DB_URI`
