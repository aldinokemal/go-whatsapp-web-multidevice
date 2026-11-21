# Direct Chatwoot history import

This package writes historical content/metadata to Chatwoot Postgres. Live sync
uses REST; optional history attachments are orchestrated through REST in `../sync.go`.

- `conn.go` verifies required tables, account, and inbox before opening an importer.
  Preserve this verification and idempotent `Importer.Close()` cleanup through the
  parent sync service.
- `writer.go` imports each chat transactionally. Per-message savepoints let a bad
  row be counted without losing the whole chat; a broken transaction/savepoint
  must still fail the import rather than look like an isolated row failure.
- Idempotency uses `source_id = WAID:<whatsapp_message_id>` scoped by `inbox_id`,
  not conversation: a reopened/resolved conversation may change where rows belong.
- Preserve WhatsApp message timestamps and anchor new conversations to the first
  valid message time. Reuse the latest conversation and respect pending/reopen config.
- Private contacts use phone fallback; groups and unresolved `@lid` use identifiers.
  Preserve existing individual contact names; group names may refresh from the subject.
- `identity.go` and `buildContent` handle content mapping. Incoming group messages
  retain the sender prefix. Skip messages whose content and media placeholder are
  both empty.

Use helper tests for identity/content changes and transaction tests for SQL/flow
changes. Keep sqlmock expectations specific enough to catch schema and scoping
regressions, including savepoint failure behavior when that path changes.
