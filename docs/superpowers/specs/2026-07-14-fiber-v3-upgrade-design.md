# Fiber v3 Upgrade Design

## Goal

Upgrade the REST server from `github.com/gofiber/fiber/v2` to `github.com/gofiber/fiber/v3` while preserving the existing routes, request and response contracts, authentication behavior, device scoping, embedded UI delivery, WebSocket protocol, and shutdown behavior.

## Scope

The migration covers the Go module and every Fiber-dependent production file and test. It also upgrades Fiber-coupled add-ons to versions compatible with Fiber v3:

- Fiber core and built-in middleware use `github.com/gofiber/fiber/v3`.
- Fiber utilities use `github.com/gofiber/utils/v2`.
- WebSockets use `github.com/gofiber/contrib/v3/websocket`.
- The HTML template engine remains the official GoFiber HTML engine at a Fiber v3-compatible version.

The migration does not add endpoints, alter payload schemas, change application configuration names, refactor domain or usecase behavior, or bump `config.AppVersion`.

## Migration Approach

Apply the dependency upgrade first and run the existing tests to capture the expected compile failures. Then migrate the affected APIs directly:

- Change route and middleware handler parameters from `*fiber.Ctx` to the Fiber v3 `fiber.Ctx` interface.
- Replace `BodyParser` and `QueryParser` with the corresponding `c.Bind()` methods.
- Replace removed typed query helpers with Fiber v3 generic query access while retaining existing default and optional-filter semantics.
- Move static and embedded filesystem delivery to Fiber v3's `static` middleware without changing public URL paths or embedded path prefixes.
- Update application, trusted-proxy, CORS, listen, and test configuration to their Fiber v3 forms while retaining current values.
- Replace the old Fiber WebSocket adapter with the official Fiber v3-compatible contrib package while retaining the existing `/ws` handshake and message behavior.
- Update test imports and `App.Test` calls without weakening assertions.

All edits remain in the existing transport, command, utility, usecase, and infrastructure files that directly import Fiber. No compatibility wrapper or new abstraction is introduced.

## Behavior and Error Handling

Handlers continue returning Fiber errors and the existing `utils.ResponseData` envelopes. Parsing failures keep their existing response paths and status codes. Recovery and timeout middleware continue delegating through `c.Next()`, and request-scoped authorization and device data continue using the request context/locals mechanisms supported by Fiber v3.

Static assets, embedded components, and embedded assets retain their current URL prefixes. The server continues listening on TCP and performing graceful shutdown with the existing timeout and resource cleanup.

## Verification

The existing tests are the regression contract for REST handlers and middleware. After the compile-failure checkpoint, the migration is complete only when all of the following pass from `src/`:

```bash
gofmt -w <changed-go-files>
go mod tidy
go test ./...
go vet ./...
go build -o /tmp/gowa-fiber-v3 .
```

The final review also checks that no `github.com/gofiber/fiber/v2`, `github.com/gofiber/websocket/v2`, old Fiber utility import, `*fiber.Ctx`, `BodyParser`, `QueryParser`, or removed filesystem middleware usage remains.

## Delivery

Commit the focused migration on `feature/upgrade-fiber-v3`, push it to `origin`, and open a draft pull request against the repository's default branch. The pull request documents the dependency/API migration and the exact verification commands and results.
