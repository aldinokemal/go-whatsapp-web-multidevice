# Working on GOWA

GOWA is a Go WhatsApp API server using whatsmeow, Fiber, and SQLite. The Go module
and local runtime directory are `src/`. `rest` also mounts MCP at `/mcp` when
enabled; both transports share the same device manager and usecases.

## Working scope and completion

- Use the applicable scoped `AGENTS.md` and task-relevant source. The entry points
  below are routing hints; follow the ones needed for the affected behavior.
- For multi-step work, identify the requested outcome and how to verify it. Continue
  through implementation, relevant checks, and fixes caused by the change. When the
  request includes running or inspecting the result, include that in completion.
  If blocked, report what remains and the specific missing input or access.
- Preserve unrelated working-tree changes. Keep `.env`, SQLite databases, session
  data, QR codes, generated media, and history dumps out of commits.
- Local tests using temporary databases, mocks, and fake clients can be run and
  repaired within the task without repeated approval. Running the app against saved
  sessions can reconnect real WhatsApp devices; use that runtime only within the
  user's authorized scope.

## Task entry points

- DTOs and interfaces: [domains](src/domains/AGENTS.md). Business orchestration:
  [usecases](src/usecase/AGENTS.md). Input rules: [validation](src/validations/AGENTS.md).
- REST, MCP, and websocket transport: [UI adapters](src/ui/AGENTS.md). For MCP
  authentication, use [the OAuth guide](docs/mcp-oauth.md).
- Device lifecycle, events, JIDs, and presence: [WhatsApp infrastructure](src/infrastructure/whatsapp/AGENTS.md).
  For webhook payload changes, consult [the payload contract](docs/webhook-payload.md).
- SQL queries, migrations, and cleanup: [chat storage](src/infrastructure/chatstorage/AGENTS.md).
- Chatwoot routing, live sync, and history: [Chatwoot infrastructure](src/infrastructure/chatwoot/AGENTS.md).
  Setup or routing-mode changes: [configuration](docs/chatwoot.md).
  Direct Postgres changes: [scoped guide](src/infrastructure/chatwoot/pgimport/AGENTS.md).
- Embedded browser UI: `src/views/components/` and `src/views/index.html`. These are
  plain Vue 3 modules loaded from CDN, with Fomantic UI and `[[`, `]]` delimiters.
- Startup and configuration: `src/cmd/root.go`, `src/cmd/rest.go`,
  `src/cmd/helpers.go`, `src/config/settings.go`, and `src/.env.example`. Configuration
  and service wiring use package globals; flags override env/Viper and `.env` defaults.
- Packaging: `docker/golang.Dockerfile`, `docker/entrypoint.sh`, and `.github/workflows/`.
  Read toolchain versions from those files and `src/go.mod`. `AppVersion` is a source
  constant in `src/config/settings.go`; release builds do not inject it with ldflags.
  Release jobs require a pushed tag and generate GoReleaser configuration in `/tmp`;
  there is no committed `.goreleaser.yml`. Docker image publication is tag/manual driven.
- Publishing a stable release: use [new-release](.agents/skills/new-release/SKILL.md)
  only when explicitly invoked. For an existing release's notes, use
  [update-release-note](.agents/skills/update-release-note/SKILL.md).

## Cross-layer contracts

- Keep transport parsing in `ui/`, orchestration in `usecase/`, and DTOs/interfaces
  in `domains/`. Preserve JSON/form fields used by REST, MCP, and browser clients.
- User-facing chat/message access must carry device scope. After login, storage
  uses `client.Store.ID.ToNonAD().String()`, not the user-facing device alias.
- Repository interface changes must reach all three layers:
  `src/domains/chatstorage/interfaces.go`, `src/infrastructure/chatstorage/sqlite_repository.go`,
  and `src/infrastructure/whatsapp/chatstorage_wrapper.go`.
- Tests that mutate config, package globals, or scheduler state stay serial and
  restore that state. Use existing colocated test helpers and stubs.

## Local commands and validation

Run Go commands from `src/`; select checks for the affected behavior:

```sh
go test ./path/to/affected/package/...
go test ./...
go vet ./...
go build -o whatsapp .
```

Choose checks for the changed behavior. Use the full suite for shared contracts,
startup, or broad changes. After relevant checks pass, repeat or broaden them only
for new edits, failures, or unresolved risks. For Markdown-only changes, check the
diff and references; application tests are unnecessary. Report checks actually run
and any unverified behavior.
Default SQLite builds use CGO; `-tags purego` selects `modernc.org/sqlite` when that
build variant is relevant. Run `go mod tidy` when changing dependencies.

For an authorized runtime check, `go run . rest` starts the server. Runtime paths
are relative to the process directory. Direct runs use `src/storages`
and `src/statics`; Docker Compose mounts the root-level directories into `/app`.
Keep these paths excluded from hot reload in `src/.air.toml`. The Docker entrypoint
fixes volume ownership before dropping to `gowauser`.
