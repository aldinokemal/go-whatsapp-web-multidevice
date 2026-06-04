# CHATWOOT INFRASTRUCTURE

Generated: 2026-06-05

## OVERVIEW

This package handles Chatwoot API calls, live sync, historical import, echo-loop suppression, and optional direct Postgres writes for Chatwoot history.

## STRUCTURE

```text
chatwoot/
|-- client.go       # Chatwoot REST API client, contact lookup, echo-loop cache
|-- sync.go         # Sync service and message import flow
|-- sync_types.go   # Sync DTOs/state
|-- types.go        # Chatwoot API payloads
`-- pgimport/       # Direct PostgreSQL importer for historical messages
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| REST API behavior | `client.go` | HTTP calls to Chatwoot, contact lookup, sent-message cache. |
| Live/history sync | `sync.go` | Uses app/send usecases, storage, and Chatwoot settings. |
| Direct DB import | `pgimport/` | SQL writer and identity mapping with sqlmock tests. |
| REST surface | `../../ui/rest/chatwoot.go` | Public webhook plus authenticated manual sync/status endpoints. |
| Config | `../../config/settings.go`, `../../cmd/root.go` | `CHATWOOT_*` env/flags. |

## CONVENTIONS

- Chatwoot webhook route is public relative to app Basic Auth and is registered before the Basic Auth middleware.
- Webhook handling accepts only outgoing Chatwoot messages for WhatsApp send-back and skips IDs marked as sent by this app.
- Outbound Chatwoot messages require a configured/selected device; preserve device context when calling send usecases.
- Direct import is controlled separately from live REST forwarding.
- Groups and `@lid` contacts use identifier/custom attributes; private chats use normalized phone lookup.
- Tests include regular sync tests and `pgimport` sqlmock tests.

## ANTI-PATTERNS

- Do not mix direct Postgres historical import with live REST forwarding semantics.
- Do not remove the sent-message cache unless another echo-loop guard exists.
- Do not log API tokens or raw secrets from Chatwoot config.
- Do not assume imported media is always downloadable; placeholder behavior is configurable.
