# PROJECT KNOWLEDGE BASE

Generated: 2026-05-24
Commit: 438d0a1
Branch: feat/daily-presence-pulse

## OVERVIEW

Go WhatsApp Web Multi-Device is a Go 1.25.5 WhatsApp Web API server with REST and MCP SSE modes. It uses whatsmeow for multi-device WhatsApp sessions, Fiber for REST, plain Vue 3 modules for the embedded UI, and SQLite-backed chat/session storage by default.

## STRUCTURE

```text
go-whatsapp-web-multidevice/
├── src/                         # Go module root; run Go commands here
│   ├── main.go                  # go:embed views, then cmd.Execute
│   ├── cmd/                     # Cobra root, rest, mcp, global app wiring
│   ├── config/                  # Mutable package globals bound from flags/env
│   ├── domains/                 # Interfaces and DTOs; see child AGENTS
│   ├── usecase/                 # Business orchestration; see child AGENTS
│   ├── validations/             # ozzo-validation plus table tests
│   ├── ui/                      # REST, MCP, websocket adapters
│   ├── infrastructure/
│   │   ├── whatsapp/            # Device manager, events, presence pulse, JID utilities
│   │   ├── chatstorage/         # chat/message/device SQL repository
│   │   └── chatwoot/            # Chatwoot REST sync and direct PG import
│   ├── views/                   # Embedded Vue 3 plain JS UI
│   ├── statics/                 # Runtime media, QR codes, send items
│   └── storages/                # Runtime SQLite DBs and history dumps
├── docs/                        # OpenAPI, webhook payload docs, SDK config
├── docker/                      # Multi-stage Alpine image and entrypoint
└── .github/workflows/           # Docker publish, release, latest promotion
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add message type | `src/domains/send/`, `src/usecase/send.go`, `src/validations/send_validation.go`, `src/ui/rest/send.go` | MCP support is optional and currently partial. |
| Add REST endpoint | `src/ui/rest/`, `src/usecase/`, `src/domains/` | Handler parses request, usecase validates/executes, domain owns DTO/interface. |
| Add MCP tool | `src/ui/mcp/` | Register in `Add*Tools`; resolve a device with `helpers.ContextWithDefaultDevice`. |
| Handle WhatsApp event | `src/infrastructure/whatsapp/event_*.go` | Register the concrete event in `event_handler.go`. |
| Presence behavior | `src/infrastructure/whatsapp/event_handler.go`, `src/infrastructure/whatsapp/presence_pulse.go`, `src/cmd/helpers.go` | Connect-time presence and scheduled available/unavailable pulses. |
| Add chat storage method | `src/domains/chatstorage/interfaces.go`, `src/infrastructure/chatstorage/sqlite_repository.go`, `src/infrastructure/whatsapp/chatstorage_wrapper.go` | Keep wrapper behavior in sync with the interface. |
| Add DB migration | `src/infrastructure/chatstorage/sqlite_repository.go` `getMigrations()` | Append only. Current list has 22 migrations. |
| Add UI component | `src/views/components/`, `src/views/index.html` | Plain JS modules, no `.vue` single-file components. |
| Device management | `src/infrastructure/whatsapp/device_manager.go` | Central registry and purge/load/create logic. |
| Chatwoot integration | `src/infrastructure/chatwoot/` and `src/ui/rest/chatwoot.go` | REST sync plus optional direct Postgres import. |
| CLI flags / config | `src/cmd/root.go`, `src/config/settings.go`, `src/.env.example` | Flags and env mutate config package globals. |
| Shared helpers | `src/pkg/utils/`, `src/pkg/error/`, `src/pkg/sqlite/` | Utilities, aliased package errors, and CGO/purego SQLite driver selection. |
| Docker/release | `docker/golang.Dockerfile`, `.github/workflows/*.yaml` | Multi-arch Docker, GitHub Actions, and generated GoReleaser configs. |

## CODE MAP

| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `cmd.Execute` | function | `src/cmd/root.go` | Stores embedded views and runs Cobra root command. |
| `initApp` | function | `src/cmd/root.go` | Creates folders, DBs, WhatsApp client, device manager, repositories, and usecases. |
| `DeviceManager` | struct | `src/infrastructure/whatsapp/device_manager.go` | Owns active device registry and persisted device records. |
| `DeviceInstance` | struct | `src/infrastructure/whatsapp/device_instance.go` | Wraps per-device ID, JID, client, state, and storage. |
| `IChatStorageRepository` | interface | `src/domains/chatstorage/interfaces.go` | Storage contract for chats, messages, calls, stats, schema, and device records. |
| `SQLiteRepository` | struct | `src/infrastructure/chatstorage/sqlite_repository.go` | Implements chat storage and inline migrations. |
| `deviceChatStorage` | wrapper | `src/infrastructure/whatsapp/chatstorage_wrapper.go` | Injects or enforces device scoping for event-side storage access. |
| `StartPresencePulseScheduler` | function | `src/infrastructure/whatsapp/presence_pulse.go` | Periodically marks connected devices available, then unavailable. |
| `NormalizeJIDFromLID` | function | `src/infrastructure/whatsapp/jid_utils.go` | Converts `@lid` JIDs to phone JIDs where whatsmeow can resolve them. |
| `DeviceMiddleware` | middleware | `src/ui/rest/middleware/device.go` | Resolves `X-Device-Id` or `device_id` and injects device context. |
| `ContextWithDefaultDevice` | helper | `src/ui/mcp/helpers/context.go` | MCP equivalent of REST device middleware for default/only device. |

## CONVENTIONS

- Go commands run from `src/`; the repo root is not the Go module root.
- Config priority is Cobra flags, then env/Viper, then `.env` loaded from `src/`.
- REST and MCP share the same app initialization and cannot safely run together against the same whatsmeow state in one process.
- Process-wide helpers in `cmd/helpers.go` guard auto-reconnect and presence-pulse startup for both REST and MCP.
- `domains/` defines DTOs and interfaces. Current contracts expose some whatsmeow and multipart types; follow existing contracts but do not add executable business logic there.
- Usecases validate first, then obtain the device/client from context, then call whatsmeow/storage.
- Device-scoped REST routes must pass `whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))`.
- MCP handlers do not receive `X-Device-Id`; they resolve the default/only device via `ContextWithDefaultDevice`.
- Optional boolean filters use `*bool` so nil means "not provided".
- Tests are colocated as `*_test.go`, mostly table-driven with `testify/assert` and occasional `testify/suite`.

## ANTI-PATTERNS

- Do not query chats or messages without `device_id` scoping for user-facing/device-scoped flows.
- Do not use `instance.ID()` as the chat/message table `device_id` after login; use `client.Store.ID.ToNonAD().String()` when deriving the WhatsApp storage identity.
- Do not use raw `@lid` event JIDs for DB lookups; normalize with `NormalizeJIDFromLID()` first.
- Do not add `IChatStorageRepository` methods without updating `chatstorage_wrapper.go` and the concrete repository.
- Do not insert migrations in the middle of `getMigrations()`; append new entries only.
- Do not remove the `evt.Sender.Device != 0` receipt check; it prevents duplicate webhook deliveries from linked devices.
- Do not put generated/runtime media, QR codes, SQLite DBs, or history dumps into source-oriented docs or commits unless explicitly requested.

## UNIQUE STYLES

- Cobra subcommands are registered by `init()` side effects in `cmd/rest.go` and `cmd/mcp.go`.
- The app uses mutable package globals for config, clients, repositories, and usecases instead of dependency injection from `main`.
- REST has wider coverage than MCP: REST send has 12 routes, MCP send currently has 6 tools.
- Chat storage migrations are Go string literals in the repository, not external migration files.
- The embedded UI uses Vue 3 from CDN, Fomantic UI modals/toasts, and custom delimiters `[[`, `]]`.
- Release workflows generate GoReleaser YAML into `/tmp`; there is no committed `.goreleaser.yml`.
- `src/pkg/error` declares package name `error`; import it with aliases such as `pkgError`.

## COMMANDS

```bash
cd src && go run . rest
cd src && go run . mcp
cd src && go build -o whatsapp
cd src && go test ./...
cd src && go vet ./...
cd src && go mod tidy
docker compose up --build
```

## NOTES

- Docker builds use `docker/golang.Dockerfile`, Go `1.25-alpine3.23`, CGO, and a final non-root `gowauser` process after the entrypoint fixes volume ownership.
- Docker publish is tag/manual driven. Arch-specific `-amd`, `-arm`, and `-armv7` images are merged into a versioned manifest; `latest` is promoted by a separate manual workflow.
- `release.yml` declares `workflow_dispatch`, but jobs are guarded to tag pushes, so manual dispatch currently skips release jobs.
- `AppVersion` is hard-coded in `src/config/settings.go`; release workflows do not inject it with ldflags.
- `status@broadcast` chat names intentionally resolve to `Status`.
- `src/.air.toml` excludes `statics` and `storages`; keep hot reload from watching runtime data.
