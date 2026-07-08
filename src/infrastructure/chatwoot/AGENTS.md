# CHATWOOT INFRASTRUCTURE

Generated: 2026-06-06

## OVERVIEW

This package handles Chatwoot API calls, live sync, historical import, echo-loop suppression, and optional direct Postgres writes for Chatwoot history.

## STRUCTURE

```text
chatwoot/
|-- client.go       # Chatwoot REST API client: contacts, conversations (find/reopen),
|                   #   messages (source_id + reply attrs), inbox list/create, echo-loop cache
|-- provision.go    # EnsureInbox: startup auto-create/reuse of the API inbox
|-- sync.go         # Sync service, message import flow, direct-import orchestration
|-- sync_types.go   # Sync DTOs/state
|-- types.go        # Chatwoot API payloads (incl. Inbox, MessageOptions fields)
`-- pgimport/       # Direct PostgreSQL importer for historical messages; see child AGENTS
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| REST API behavior | `client.go` | HTTP calls to Chatwoot, contact lookup, sent-message cache. |
| Live/history sync | `sync.go` | Uses app/send usecases, storage, and Chatwoot settings. |
| Direct DB import | `pgimport/` | SQL writer and identity mapping with sqlmock tests; see child AGENTS. |
| Message links/read/delete | `sync.go`, `../../ui/rest/chatwoot.go`, `../../domains/chatstorage/` | Store mappings for idempotency, read state, webhook replies, and deletes. |
| Forward retry worker | `../whatsapp/webhook_forward.go`, `../chatstorage/sqlite_repository.go` | Live WhatsApp-to-Chatwoot retries use persistent queue storage. |
| REST surface | `../../ui/rest/chatwoot.go` | Public webhook plus authenticated manual sync/status endpoints. |
| Config | `../../config/settings.go`, `../../cmd/root.go` | `CHATWOOT_*` env/flags. |

## CONVENTIONS

- Chatwoot webhook route is public relative to app Basic Auth and is registered before the Basic Auth middleware.
- Webhook handling accepts only outgoing Chatwoot messages for WhatsApp send-back and skips IDs marked as sent by this app.
- Outbound Chatwoot messages require a configured/selected device; preserve device context when calling send usecases.
- Direct import is controlled separately from live REST forwarding.
- Direct import owns historical non-media rows. Media history may pre-pass through REST only when `CHATWOOT_IMPORT_MEDIA_WITH_REST` is enabled.
- Mirrored WhatsApp rows use `source_id` values like `WAID:<message_id>` and persistent links for idempotency and echo suppression.
- `SyncService.Close()` owns closing the direct-Postgres importer pool during REST shutdown.
- Chatwoot webhook can be public relative to Basic Auth and still require `CHATWOOT_WEBHOOK_SECRET`.
- Groups and `@lid` contacts use identifier/custom attributes; private chats use normalized phone lookup.
- Tests include regular sync tests and `pgimport` sqlmock tests.

## ANTI-PATTERNS

- Do not mix direct Postgres historical import with live REST forwarding semantics.
- Do not treat Chatwoot API inbox identifiers as numeric account or inbox IDs.
- Do not remove the sent-message cache unless another echo-loop guard exists.
- Do not log API tokens or raw secrets from Chatwoot config.
- Do not assume imported media is always downloadable; placeholder behavior is configurable.
