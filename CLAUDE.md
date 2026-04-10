# Go WhatsApp Web Multi-Device

Go-based WhatsApp Web API server with REST and MCP modes. Multi-device management via whatsmeow.

## Structure

```
src/
├── main.go                    # Entry point; embeds views/ via go:embed
├── cmd/                       # Cobra CLI: rest, mcp subcommands + root config (407 lines)
├── config/settings.go         # All config vars (Viper-bound)
├── domains/                   # Contracts only: interfaces + DTOs (10 packages)
├── infrastructure/
│   ├── whatsapp/              # WhatsApp protocol layer (29 files, 4845 lines)
│   ├── chatstorage/           # SQLite/PostgreSQL persistence (1263-line repo)
│   └── chatwoot/              # Chatwoot CRM bidirectional sync
├── usecase/                   # Business logic (1:1 with domains)
├── validations/               # ozzo-validation input checks + tests
├── ui/
│   ├── rest/                  # Fiber HTTP handlers + middleware
│   └── mcp/                   # MCP server handlers
├── views/                     # Vue.js 3 components (Semantic UI, plain JS)
├── pkg/
│   ├── utils/                 # Shared helpers: JID, media, phone formatting
│   └── error/                 # 4 error types: app, generic, validation, whatsapp
├── statics/                   # Runtime media + QR codes
└── storages/                  # Runtime SQLite DBs
```

## Commands

```bash
cd src && go run . rest          # Run REST API (port 3000)
cd src && go run . mcp           # Run MCP server (port 8080)
cd src && go build -o whatsapp   # Build binary
cd src && go test ./...          # Run all tests
cd src && go vet ./...           # Static analysis
go mod tidy                      # Update dependencies (run from src/)
```

## Where to Look

| Task | Location | Notes |
|------|----------|-------|
| Add message type | `domains/send/`, `usecase/send.go`, `validations/send_validation.go` | 3-file pattern |
| Add API endpoint | `ui/rest/`, `usecase/`, `domains/` | Handler → usecase → domain |
| Add MCP tool | `ui/mcp/` | Mirrors REST; `query.go` for read ops |
| Handle WA event | `infrastructure/whatsapp/event_*.go` | Register in `event_handler.go` switch |
| Add DB migration | `infrastructure/chatstorage/sqlite_repository.go` → `getMigrations()` | Append only — 15 migrations |
| Add Vue component | `views/components/` | Plain JS, no .vue SFC |
| Device management | `infrastructure/whatsapp/device_manager.go` | 615-line central orchestrator |
| Chatwoot integration | `infrastructure/chatwoot/` | `client.go` (API) + `sync.go` (sync) |
| CLI flags / config | `cmd/root.go` | Viper+Cobra, `.env` loading |
| Shared helpers | `pkg/utils/whatsapp.go`, `general.go` | JID, media, phone formatting |

## Critical: Device ID vs JID

Two distinct identifiers — confusing them causes silent data bugs:

- **Device ID** (`instance.ID()`): User alias or UUID (e.g., `"my-device"`)
- **JID** (`instance.JID()`): WhatsApp JID (e.g., `"6289605618749@s.whatsapp.net"`)

The `chats`/`messages` tables store device_id as the **JID without device number**:
```go
deviceID := client.Store.ID.ToNonAD().String()  // ✅ "6289605618749@s.whatsapp.net"
// NOT instance.ID()  // ❌ may return alias or "6289605618749:11@s.whatsapp.net"
```

## Conventions

- **Clean Architecture**: `domains/` → `usecase/` → `ui/` (never reverse)
- **1:1 mapping**: Each domain has matching usecase, validation, and UI handler
- **Device scoping**: All chat/message DB queries must include `device_id`
- **LID normalization**: WhatsApp sends `@lid` JIDs — call `NormalizeJIDFromLID()` before DB ops
- **Optional booleans**: Use `*bool` for optional filter params (nil = not set)
- **Config priority**: CLI flags > env vars > `.env` file
- **Wrapper pattern**: `IChatStorageRepository` has two wrappers that inject device_id:
  - `infrastructure/whatsapp/chatstorage_wrapper.go` (for event handlers)
  - `infrastructure/chatstorage/device_repository.go` (for usecase layer)

## Anti-Patterns

- **Never** query chats/messages without device_id scoping
- **Never** use raw event JIDs for DB lookups without `NormalizeJIDFromLID`
- **Never** add `IChatStorageRepository` methods without updating both wrapper files
- **Never** insert migrations in the middle — always append to `getMigrations()`
- **Never** put business logic in domain packages — they define contracts only
- **Never** remove the Device == 0 check in receipt forwarding (prevents duplicate webhooks)

## Testing

- Standard Go testing + `testify/assert` + `testify/suite`
- Table-driven tests throughout
- `httptest` for HTTP testing, `fiber.Test()` for middleware
- Tests colocated with source: `*_test.go` next to implementation
- Run: `cd src && go test ./...`

## Key Dependencies

| Package | Role |
|---------|------|
| `go.mau.fi/whatsmeow` | WhatsApp Web protocol |
| `github.com/gofiber/fiber/v2` | REST framework |
| `github.com/mark3labs/mcp-go` | MCP server (v0.45.0) |
| `github.com/spf13/cobra` + `viper` | CLI + config |
| `github.com/go-ozzo/ozzo-validation/v4` | Input validation |
| `github.com/mattn/go-sqlite3` / `github.com/lib/pq` | SQLite / PostgreSQL |

## Notes

- **Go 1.25.0+** required (`src/go.mod`)
- REST and MCP modes cannot run simultaneously (whatsmeow limitation)
- FFmpeg required for media processing (`ConvertToJPEG`, `ConvertToMP4`)
- HTML/JS assets embedded in binary via `go:embed`
- Database: SQLite default, PostgreSQL via `DB_URI` env var
- Docker: multi-stage alpine build, non-root user `gowauser` (uid 20001)
- CI: GitHub Actions → GoReleaser (Linux/Windows/macOS) + multi-arch Docker (GHCR + Docker Hub)
- Hot reload: `.air.toml` configured (excludes `statics/`, `storages/`)
- `status@broadcast` chat always returns name "Status" regardless of other naming
