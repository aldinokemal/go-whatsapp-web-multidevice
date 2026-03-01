# infrastructure/chatstorage

SQLite-based chat and message persistence. Single implementation file with migration system.

## KEY FILES
- `sqlite_repository.go` (1169 lines) — Full `IChatStorageRepository` implementation
- `device_repository.go` — Wrapper that injects device_id into all calls (mirrors `chatstorage_wrapper.go` in `whatsapp/`)

## MIGRATION SYSTEM
Migrations are sequential, numbered from 1. Add new ones at the end of `getMigrations()`. Each migration runs in a transaction with version tracking in `schema_version` table.

Current: 14 migrations (latest: `idx_chats_archived` index).

## CONVENTIONS
- All chat queries must be device-scoped (use `device_id` column)
- Filter methods (e.g., `GetFilteredChatCount`) must mirror `GetChats` filter logic exactly
- New interface methods → add to `device_repository.go` AND `whatsapp/chatstorage_wrapper.go`

## ANTI-PATTERNS
- Never add a migration in the middle — always append
- Never query chats without device_id scoping in multi-device context
