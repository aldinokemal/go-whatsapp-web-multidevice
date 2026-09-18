# Chat storage

`sqlite_repository.go` implements `domains/chatstorage.IChatStorageRepository`.
For a contract change, update the domain interface and WhatsApp wrapper together.

## Identity and queries

- `chats` uses `(jid, device_id)`; `messages` uses `(id, chat_jid, device_id)`.
  Use device-scoped chat, message, delete, and count methods for user-facing flows.
- `GetMessages` and `SearchMessages` require device IDs. Quote lookups use
  `GetMessageByIDAndDevice`; use `GetMessageByIDChatAndDevice` when chat identity
  is part of the request. Do not copy legacy global lookups into scoped flows.
- Event/sent-message storage derives device identity from the whatsmeow client
  context. `status@broadcast` must display as `Status`.
- Parameterize values in SQL; dynamic clauses use fixed fragments plus arguments.
- `message_edits` retains append-only history while the current message updates.

## Schema and cleanup

- Append migrations to `getMigrations()`; do not reorder or rewrite applied entries.
  Read the current schema there instead of relying on a documented migration count.
- `chatwoot_message_links` uses `(device_id, wa_message_id)`; reverse conversation
  lookup also needs account/config scope. Legacy account `0` is allowed only in
  legacy single-account mode, never as a wildcard for per-device routing.
- `chatwoot_forward_queue` uniqueness is `(device_id, event_name, wa_message_id)`.
  Device deletion/truncation must clean up related links and retry jobs. Config
  deletion removes its owned links so stale rows cannot route a rebound destination.
- Preserve device registry operations during purge/load changes. Poll definitions
  use `(device_id, chat_jid, poll_message_id)` and must retain that scope.

Storage tests use real SQLite with temporary or in-memory databases. For schema,
isolation, or cleanup changes, cover the affected query and deletion behavior with
those fixtures; exercise CGO/purego variants when driver behavior is involved.
