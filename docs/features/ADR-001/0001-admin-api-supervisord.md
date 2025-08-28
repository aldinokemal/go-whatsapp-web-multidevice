# ADR-0001 — Admin API to orchestrate multi-instance GOWA with Supervisord

**Status**: **Accepted**  
**Date**: 2025-08-28  
**Repository**: `aldinokemal/go-whatsapp-web-multidevice`  
**Authors/Deciders**: Platform & Backend team (@milesibastos)  
**Consulted**: SRE/DevOps, Security, Support

---

## Context

We need to run **N isolated GOWA instances** (one per port / tenant) on a single host or container, and **create/stop/remove** them **dynamically** through an HTTP API, with basic observability and an optional web UI for manual operations.

### Key drivers
- Dynamic scaling at runtime without restarting the host process
- Robust supervision (auto-restart, status, logs)
- Simple operational UX (optional web UI), minimal moving parts
- Container-friendly, not requiring an external orchestrator

### Constraints & facts
- GOWA’s REST server starts via `whatsapp rest` and supports flags/env such as `--port`, `--basic-auth`, `--debug`, `--os` (default port 3000).
- Supervisord exposes an XML-RPC API to reload config, add/remove process groups, and start/stop processes.

---

## Decision

**Adopt an in-process Admin API (Go) that manages processes via Supervisord.**  
Use the Go client **`github.com/abrander/go-supervisord`** to call `ReloadConfig`, `AddProcessGroup`, `RemoveProcessGroup`, `StartProcess`/`StopProcess`, or `Update` when appropriate.

### Naming & layout
- Process/group name: `gowa_<PORT>` (1:1).
- Config per instance: `/etc/supervisor/conf.d/gowa-<PORT>.conf`.
- Per-instance state: `/app/instances/<PORT>/storages` (isolates DB/QR/session).
- Logs: `/var/log/supervisor/gowa_<PORT>.out.log` and `.err.log`.

### Admin API (HTTP)
- `POST /admin/instances` → body `{ "port": <int> }`
- `DELETE /admin/instances/{port}`
- `GET /admin/instances` → list filtered from Supervisord

### Security
- Admin API requires `Authorization: Bearer <ADMIN_TOKEN>`.
- Supervisord RPC bound to loopback (`127.0.0.1:9001/RPC2`) or UNIX socket, protected with Basic Auth; never expose it on the public interface.

---

## Alternatives considered

1) **systemd + D-Bus** — robust but heavier to automate in containers; no built-in web UI.  
2) **Self-spawned processes** — would re-implement supervision/restart/logging.  
3) **Compose/Swarm/Kubernetes** — raises operational bar and conflicts with the “single in-app Admin API” goal.

---

## Consequences

**Positive**
- Full runtime CRUD of instances via API
- Reuse Supervisor’s restart policy, status, logs, optional web UI
- Works both on bare-metal and inside a container

**Negative / Risks**
- Multi-process inside a single container (if containerized)
- Security risk if RPC is misconfigured/exposed
- Config drift (orphaned files) on partial failures

Mitigations: bind RPC to loopback/UNIX socket + Basic Auth + Admin token; atomic writes; idempotent flows with cleanup.

---

## Technical design (detailed)

### 1) Supervisord client
Create a client with `github.com/abrander/go-supervisord`:

- `NewClient("http://127.0.0.1:9001/RPC2", WithAuthentication(user, pass))` or `NewUnixSocketClient("/var/run/supervisor.sock", ...)`.
- Methods used: `ReloadConfig()`, `AddProcessGroup(name)`, `RemoveProcessGroup(name)`, `StartProcess(name, wait)`, `StopProcess(name, wait)`, `Update()`.

### 2) Per-instance config template
Path: `/etc/supervisor/conf.d/gowa-<PORT>.conf`

```ini
[program:gowa_<PORT>]
command=/usr/local/bin/whatsapp rest --port=<PORT> --debug=false --os=Chrome --account-validation=false --basic-auth=admin:admin
directory=/app
autostart=true
autorestart=true
startretries=3
stdout_logfile=/var/log/supervisor/gowa_<PORT>.out.log
stderr_logfile=/var/log/supervisor/gowa_<PORT>.err.log
environment=APP_PORT="<PORT>",APP_DEBUG="false",APP_OS="Chrome",APP_BASIC_AUTH="admin:admin",DB_URI="file:/app/instances/<PORT>/storages/whatsapp.db?_foreign_keys=on"
```

**Atomic write**: write to `gowa-<PORT>.conf.tmp`, `fsync`, then `rename()` to the final path.

### 3) Admin HTTP server (new subcommand)
Add subcommand `whatsapp admin` (or embed in main), default bind `:8088`.

**Env configuration**
- `ADMIN_TOKEN` (required in prod)
- `SUPERVISOR_URL` (`http://127.0.0.1:9001/RPC2` or `unix:///var/run/supervisor.sock`)
- `SUPERVISOR_USER` / `SUPERVISOR_PASS`
- `SUPERVISOR_CONF_DIR` (default `/etc/supervisor/conf.d`)
- `INSTANCES_DIR` (default `/app/instances`)
- `SUPERVISOR_LOG_DIR` (default `/var/log/supervisor`)
- `GOWA_BIN` (default `/usr/local/bin/whatsapp`)

**Routes & flows**

- **POST /admin/instances**
  1) Validate `port` (>1024, available).
  2) Ensure `${INSTANCES_DIR}/${port}/storages` exists.
  3) Atomically write `gowa-${port}.conf` with the template.
  4) Prefer **`Update()`** to reconcile Supervisor (or `ReloadConfig()` + `AddProcessGroup("gowa_<port>")`).
  5) `StartProcess("gowa_<port>", true)` and optionally poll for `RUNNING` status.

- **DELETE /admin/instances/{port}**
  1) `StopProcess("gowa_<port>", true)`.
  2) `RemoveProcessGroup("gowa_<port>")`.
  3) Delete `gowa-${port}.conf`; optionally archive/remove instance state.

- **GET /admin/instances**
  - `GetAllProcessInfo()`, filter by name prefix `gowa_`, return `{port, state, pid, uptime, logs}`.

**Idempotency & concurrency**
- Per-port lock (flock `/run/gowa.<port>.lock` or in-process mutex) around create/delete.
- Treat “already exists/unknown group” as success where reasonable.
- Validate port is not already claimed by another running process (and not listening).

**HTTP semantics**
- `201 Created` on create; `409 Conflict` if port is already owned; `404 Not Found` on delete of unknown; `502/504` for Supervisor reachability/timeouts.
- Include `X-Request-ID`; structured JSON errors.

### 4) RPC hardening
- Prefer **UNIX socket** or bind RPC to `127.0.0.1`.
- Enable Basic Auth; keep Supervisord’s Web UI off the public interface (use SSH tunnel/proxy/TLS if needed).

### 5) Observability
- Metrics (optional): `gowa_instances_running`, `gowa_admin_api_requests_total`, `gowa_supervisor_errors_total`.
- Health: `/healthz` (admin liveness) and `/readyz` (RPC round-trip ok).
- Audit log: JSON line per create/delete.

### 6) Container/Docker notes
- Run `supervisord` (PID1 via `tini`) and start the Admin subcommand in the same container or as a sidecar.
- Mount `/app/instances` as a persistent volume.
- Do **not** expose port 9001; only expose Admin `:8088` behind TLS.

---

## Implementation tasks

1. Add dependency `github.com/abrander/go-supervisord` to `go.mod`.
2. Create package `internal/admin`:
   - `client.go` (Supervisor client builder)
   - `conf.go` (atomic writer/remover of `.conf`; directory bootstrap)
   - `api.go` (HTTP handlers `POST/GET/DELETE`, bearer auth, JSON errors)
   - `lifecycle.go` (Create/Delete orchestrations calling Supervisor, prefer `Update()`)
3. Add subcommand `whatsapp admin` (bind `:8088`).
4. Enforce `ADMIN_TOKEN` in prod; refuse to start unless explicitly allowed in dev.
5. Tests: unit (mocks), integration (container with Supervisord), idempotency & concurrency.
6. Docs: README examples (curl), sample `supervisord.conf`, container notes.

---

## Acceptance criteria

- `POST /admin/instances` creates `gowa_<PORT>` (RUNNING within timeout), config file exists, storage dir exists.
- `GET /admin/instances` lists instances with accurate state/uptime/PID.
- `DELETE /admin/instances/{port}` stops process, removes group, deletes the config file.
- RPC is not reachable externally; Admin requires a bearer token.
- Repeated create/delete are idempotent and safe under concurrent requests.

---

## Follow-ups

- Centralized logging (ship logs to ELK/OpenSearch).
- mTLS and RBAC on the Admin API via reverse proxy / service mesh.
- Migration path to “one process per container” under an external orchestrator, if/when needed.
