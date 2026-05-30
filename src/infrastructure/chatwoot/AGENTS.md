# CHATWOOT INFRASTRUCTURE

Generated: 2026-05-24

## OVERVIEW

This package handles Chatwoot API calls, live sync, historical import, and optional direct Postgres writes for Chatwoot history.

## STRUCTURE

```text
chatwoot/
├── client.go       # Chatwoot REST API client
├── sync.go         # Sync service and message import flow
├── sync_types.go   # Sync DTOs/state
├── types.go        # Chatwoot API payloads
└── pgimport/       # Direct PostgreSQL importer for historical messages
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| REST API behavior | `client.go` | HTTP calls to Chatwoot. |
| Live/history sync | `sync.go` | Uses app/send usecases, storage, and Chatwoot settings. |
| Direct DB import | `pgimport/` | SQL writer and identity mapping with sqlmock tests. |
| REST surface | `../../ui/rest/chatwoot.go` | Webhook and manual sync endpoints. |
| Config | `../../config/settings.go`, `../../cmd/root.go` | `CHATWOOT_*` env/flags. |

## CONVENTIONS

- Chatwoot webhook route is public relative to app Basic Auth and is registered before the Basic Auth middleware.
- Outbound Chatwoot messages require a configured/selected device; preserve device context when calling send usecases.
- Direct import is controlled by `ChatwootImportDBURI`; live forwarding still uses the REST API.
- Tests include regular sync tests and `pgimport` sqlmock tests.

## ANTI-PATTERNS

- Do not mix direct Postgres historical import with live REST forwarding semantics.
- Do not log API tokens or raw secrets from Chatwoot config.
- Do not assume imported media is always downloadable; placeholder behavior is configurable.
