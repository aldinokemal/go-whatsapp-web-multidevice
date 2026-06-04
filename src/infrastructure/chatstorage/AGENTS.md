# CHAT STORAGE

Generated: 2026-06-05

## OVERVIEW

`SQLiteRepository` implements chat, message, edit-history, call, reaction, statistic, schema, and device-record storage behind `domains/chatstorage.IChatStorageRepository`.

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Repository contract | `../../domains/chatstorage/interfaces.go` | Any method addition must be implemented here and in WhatsApp wrapper. |
| SQL implementation | `sqlite_repository.go` | Single large repository file. |
| Migrations | `sqlite_repository.go` `getMigrations()` | Append-only list, currently 22 migrations. |
| Message edit history | `sqlite_repository.go`, `sqlite_repository_edit_test.go` | `message_edits` is append-only history while original message content updates. |
| Tests | `sqlite_repository_test.go`, `sqlite_repository_edit_test.go` | Add coverage for schema/data isolation changes. |

## CONVENTIONS

- Default chat storage URI is `file:storages/chatstorage.db`; connection setup is in `cmd/root.go`.
- `chats` primary key is `(jid, device_id)`; `messages` primary key is `(id, chat_jid, device_id)`.
- `GetMessages` and `SearchMessages` fail fast if device ID is missing.
- Use `GetMessageByIDAndDevice` for device-scoped ID lookups such as quoted replies.
- Use `GetChatByDevice`, `DeleteChatByDevice`, `DeleteMessageByDevice`, and count-by-device variants for scoped flows.
- `CreateMessage` and sent-message storage derive the current device identity from the whatsmeow client context.
- `status@broadcast` must always produce display name `Status`.
- Storage tests use real SQLite drivers, including temp DB and in-memory variants.

## ANTI-PATTERNS

- Do not add new user-facing query paths that use `GetChat`, `DeleteChat`, or `GetMessageByID` without confirming device isolation.
- Do not reorder or edit old migrations for a live DB; append a new migration.
- Do not build SQL with untrusted string interpolation. Existing dynamic clauses use fixed fragments plus args.
- Do not forget the device registry operations when changing purge/load behavior.
