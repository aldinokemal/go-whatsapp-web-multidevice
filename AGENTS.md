# PROJECT KNOWLEDGE BASE

Generated: 2026-06-06
Commit: 8c4ea8f
Branch: fix/chatwoot-postgres

## OVERVIEW

Go WhatsApp Web Multi-Device is a Go 1.25.5 WhatsApp Web API server with REST and MCP SSE modes.
It uses whatsmeow sessions, Fiber, plain Vue 3 modules, and SQLite-backed chat/session storage by default.

## STRUCTURE

```text
go-whatsapp-web-multidevice/
|-- src/                         # Go module root; run Go commands here
|   |-- main.go                  # go:embed views, then cmd.Execute
|   |-- cmd/                     # Cobra root, rest, mcp, global app wiring
|   |-- config/                  # Mutable package globals bound from flags/env
|   |-- domains/                 # Interfaces and DTOs; see child AGENTS
|   |-- usecase/                 # Business orchestration; see child AGENTS
|   |-- validations/             # ozzo-validation plus table tests
|   |-- ui/                      # REST, MCP, websocket adapters
|   |-- infrastructure/
|   |   |-- whatsapp/            # Device manager, events, presence pulse, JID utilities
|   |   |-- chatstorage/         # chat/message/device SQL repository
|   |   `-- chatwoot/            # Chatwoot REST sync and direct PG import
|   |       `-- pgimport/        # Direct Chatwoot Postgres importer; see child AGENTS
|   |-- views/                   # Embedded Vue 3 plain JS UI
|   |-- statics/                 # Runtime media, QR codes, send items
|   `-- storages/                # Runtime SQLite DBs and history dumps
|-- docs/                        # OpenAPI, webhook payload docs, SDK config
|-- docker/                      # Multi-stage Alpine image and entrypoint
|-- gallery/                     # Static screenshots and project images
`-- .github/workflows/           # Docker publish, release, latest promotion
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add message type | `src/domains/send/`, `src/usecase/send.go`, `src/ui/rest/send.go` | REST is primary; MCP support is selective. |
| Add quoted reply support | `src/domains/send/`, `src/usecase/send.go`, `src/views/components/Send*.js` | Optional `reply_message_id`; use device-scoped quote lookup. |
| Add REST endpoint | `src/ui/rest/`, `src/usecase/`, `src/domains/` | Handler parses request, usecase validates/executes, domain owns DTO/interface. |
| Add MCP tool | `src/ui/mcp/` | Register in `Add*Tools`; resolve a device with `helpers.ContextWithDefaultDevice`. |
| Handle WhatsApp event | `src/infrastructure/whatsapp/event_*.go` | Register the concrete event in `event_handler.go`. |
| Presence behavior | `src/infrastructure/whatsapp/event_handler.go`, `presence_pulse.go`, `src/cmd/helpers.go` | Connect-time and scheduled pulse presence. |
| Add chat storage method | `src/domains/chatstorage/interfaces.go`, `sqlite_repository.go`, `chatstorage_wrapper.go` | Update domain, repository, and wrapper together. |
| Add DB migration | `src/infrastructure/chatstorage/sqlite_repository.go` `getMigrations()` | Append only. Current list has 29 migrations. |
| Add UI component | `src/views/components/`, `src/views/index.html` | Plain JS modules, no `.vue` single-file components. |
| Device management | `src/infrastructure/whatsapp/device_manager.go` | Central registry and purge/load/create logic. |
| Chatwoot integration | `src/infrastructure/chatwoot/` and `src/ui/rest/chatwoot.go` | REST sync, public webhook, optional direct Postgres import. |
| Direct Chatwoot import | `src/infrastructure/chatwoot/pgimport/` | Direct Chatwoot schema writes; see child AGENTS. |
| Chatwoot link/retry state | `src/infrastructure/chatstorage/sqlite_repository.go`, `src/infrastructure/whatsapp/webhook_forward.go` | Message links, read/delete sync, and persistent forward retries. |
| CLI flags / config | `src/cmd/root.go`, `src/config/settings.go`, `src/.env.example` | Flags and env mutate config package globals. |
| Shared helpers | `src/pkg/utils/`, `src/pkg/error/`, `src/pkg/sqlite/` | Utilities, aliased package errors, and CGO/purego SQLite driver selection. |
| Docker/release | `docker/golang.Dockerfile`, `.github/workflows/*.yaml` | Multi-arch Docker, tag/manual workflows, generated GoReleaser configs. |

## CODE MAP

| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `cmd.Execute` | function | `src/cmd/root.go` | Stores embedded views and runs Cobra root command. |
| `initApp` | function | `src/cmd/root.go` | Creates folders, DBs, WhatsApp client, device manager, repositories, and usecases. |
| `DeviceManager` | struct | `src/infrastructure/whatsapp/device_manager.go` | Owns active device registry and persisted device records. |
| `DeviceInstance` | struct | `src/infrastructure/whatsapp/device_instance.go` | Wraps per-device ID, JID, client, state, and storage. |
| `IChatStorageRepository` | interface | `src/domains/chatstorage/interfaces.go` | Storage contract for chats, messages, edits, calls, stats, schema, and device records. |
| `SQLiteRepository` | struct | `src/infrastructure/chatstorage/sqlite_repository.go` | Implements chat storage, Chatwoot link/retry state, and inline migrations. |
| `deviceChatStorage` | wrapper | `src/infrastructure/whatsapp/chatstorage_wrapper.go` | Injects or enforces device scoping for event-side storage access. |
| `StartPresencePulseScheduler` | function | `src/infrastructure/whatsapp/presence_pulse.go` | Periodically marks connected devices available, then unavailable. |
| `StartChatwootForwardRetryWorker` | function | `src/infrastructure/whatsapp/webhook_forward.go` | Replays queued WhatsApp-to-Chatwoot forward failures. |
| `NormalizeJIDFromLID` | function | `src/infrastructure/whatsapp/jid_utils.go` | Converts `@lid` JIDs to phone JIDs where whatsmeow can resolve them. |
| `pgimport.Importer` | struct | `src/infrastructure/chatwoot/pgimport/conn.go` | Direct Chatwoot Postgres importer for historical messages. |
| `DeviceMiddleware` | middleware | `src/ui/rest/middleware/device.go` | Resolves `X-Device-Id` or `device_id` and injects device context. |
| `ContextWithDefaultDevice` | helper | `src/ui/mcp/helpers/context.go` | MCP equivalent of REST device middleware for default/only device. |

## CONVENTIONS

- Go commands run from `src/`; the repo root is not the Go module root.
- Local runtime paths are relative to process cwd, so direct local runs should start from `src/`.
- Config priority is Cobra flags, then env/Viper, then `.env` loaded from `src/`.
- REST and MCP share the same app initialization and cannot safely run together against the same whatsmeow state in one process.
- Process-wide helpers in `cmd/helpers.go` guard auto-reconnect and presence-pulse startup for both REST and MCP.
- `domains/` defines DTOs and interfaces. Current contracts expose some whatsmeow and multipart types; follow existing contracts but do not add executable business logic there.
- Usecases validate first, then obtain the device/client from context, then call whatsmeow/storage.
- Device-scoped REST routes must pass `whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))`.
- MCP handlers do not receive `X-Device-Id`; they resolve the default/only device via `ContextWithDefaultDevice`.
- Optional boolean filters use `*bool` so nil means "not provided".
- Tests are colocated as `*_test.go`, mostly table-driven with `testify/assert` and occasional `testify/suite`.
- Tests that mutate config, package globals, or background worker state should stay serial and restore state with `defer`.
- Chatwoot direct Postgres import is for history. Live forwarding still uses REST plus link/retry storage.

## ANTI-PATTERNS

- Do not query chats or messages without `device_id` scoping for user-facing/device-scoped flows.
- Do not use `instance.ID()` as the chat/message table `device_id` after login; use `client.Store.ID.ToNonAD().String()` when deriving the WhatsApp storage identity.
- Do not use raw `@lid` event JIDs for DB lookups; normalize with `NormalizeJIDFromLID()` first.
- Do not add `IChatStorageRepository` methods without updating `chatstorage_wrapper.go` and the concrete repository.
- Do not insert migrations in the middle of `getMigrations()`; append new entries only.
- Do not remove the `evt.Sender.Device != 0` receipt check; it prevents duplicate webhook deliveries from linked devices.
- Do not put generated/runtime media, QR codes, SQLite DBs, `.env`, or history dumps into source-oriented docs or commits unless explicitly requested.
- Do not treat Chatwoot direct Postgres import as the live forwarding path; keep REST media handling and direct DB idempotency separate.

## UNIQUE STYLES

- Cobra subcommands are registered by `init()` side effects in `cmd/rest.go` and `cmd/mcp.go`.
- The app uses mutable package globals for config, clients, repositories, and usecases instead of dependency injection from `main`.
- REST has wider coverage than MCP: REST send has 12 routes; MCP exposes send, query, app, and group tool subsets.
- Chat storage migrations are Go string literals in the repository, not external migration files.
- The embedded UI uses Vue 3 from CDN, Fomantic UI modals/toasts, and custom delimiters `[[`, `]]`.
- Release workflows generate GoReleaser YAML into `/tmp`; there is no committed `.goreleaser.yml`.
- `AppVersion` is hard-coded as `v8.6.0` in `src/config/settings.go`; release workflows do not inject it with ldflags.
- `src/pkg/error` declares package name `error`; import it with aliases such as `pkgError`.
- Default SQLite builds use CGO `github.com/mattn/go-sqlite3`; `-tags purego` switches to `modernc.org/sqlite`.

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
- Docker Compose mounts root-level `./storages` and `./statics` into `/app`; direct local runs from `src/` use `src/storages` and `src/statics`.
- Docker publish is tag/manual driven. Arch-specific `-amd`, `-arm`, and `-armv7` images are merged into a versioned manifest; `latest` is also promoted by workflows.
- `release.yml` declares `workflow_dispatch`, but release jobs are still guarded to tag refs.
- GitHub workflows are release/publish oriented; there is no PR workflow that runs `go test`, `go vet`, or lint.
- `DBKeysURI` defaults to the main DB URI when empty; avoid in-memory keys storage in production because privacy tokens must survive long-lived sessions.
- `status@broadcast` chat names intentionally resolve to `Status`.
- `src/.air.toml` excludes `statics` and `storages`; keep hot reload from watching runtime data.
