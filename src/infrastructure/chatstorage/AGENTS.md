# infrastructure/chatstorage

SQLite/PostgreSQL chat and message persistence. Single implementation file with sequential migration system.

## KEY FILES
- `sqlite_repository.go` (1263 lines) — Full `IChatStorageRepository` implementation (39 methods)
- `device_repository.go` — Wrapper that injects device_id into all calls (mirrors `chatstorage_wrapper.go` in `whatsapp/`)

## MIGRATION SYSTEM
Migrations are sequential string slices returned by `getMigrations()`. Each runs in a transaction with version tracking in `schema_info` table.

Current: **15 migrations** (latest: `call_metadata TEXT` column on messages table).

Tables: `chats` (PK: jid+device_id), `messages` (PK: id+chat_jid+device_id), `devices` (PK: device_id), `schema_info`.

## KEY METHODS
- `CreateMessage(ctx, evt)` — Full message ingestion: resolve device → normalize JIDs → upsert chat → store message
- `StoreSentMessageWithContext(ctx, ...)` — Outbound messages with context cancellation support
- `CreateIncomingCallRecord(ctx, evt, autoRejected)` — Synthetic call rows (media_type="call")
- `GetChatNameWithPushNameByDevice(...)` — Name resolution: existing > pushName > sender > JID
- `GetFilteredChatCount(filter)` — Use this for pagination totals, NOT `GetTotalChatCount()`

## CONVENTIONS
- All chat queries must be device-scoped (use `device_id` column)
- Filter methods must mirror `GetChats` filter logic exactly via `buildChatFilterQuery`
- New interface methods → add to `device_repository.go` AND `whatsapp/chatstorage_wrapper.go`
- Cross-DB compatible SQL: avoid SQLite-specific or PostgreSQL-specific syntax in migrations

## ANTI-PATTERNS
- Never add a migration in the middle — always append
- Never query chats without device_id scoping in multi-device context
