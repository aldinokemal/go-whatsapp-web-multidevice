# CHATWOOT PGIMPORT

Generated: 2026-06-06

## OVERVIEW

`pgimport` writes historical WhatsApp messages directly into Chatwoot's Postgres schema. It is not the live REST forwarding path.

## STRUCTURE

```text
pgimport/
|-- conn.go        # DB connection, schema/account/inbox verification, importer lifecycle
|-- identity.go    # Chat/contact identity helpers and message type/content rules
`-- writer.go      # ImportChat transaction, contact/conversation/message writes, links
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Open importer | `conn.go` | Verifies required Chatwoot tables plus configured account/inbox. |
| Import a chat | `writer.go` `ImportChat` | Transaction with contact, contact_inbox, conversation, message loop, links. |
| Contact identity | `identity.go`, `writer.go` `upsertContact` | Private chats use phone fallback; groups and `@lid` JIDs use identifiers. |
| Conversation behavior | `writer.go` `findOrCreateConversation` | Reuses latest conversation and mirrors pending/reopen config. |
| Message idempotency | `writer.go` `insertMessage` | Probe by `inbox_id` and `source_id` before insert. |
| Content mapping | `identity.go`, `writer.go` `buildContent` | Group incoming messages get sender prefix; media placeholder behavior is configurable. |
| Tests | `*_test.go` | sqlmock-heavy unit and flow coverage; update expected SQL with schema changes. |

## CONVENTIONS

- Use `source_id = WAID:<whatsapp_message_id>` for imported Chatwoot messages.
- Preserve WhatsApp timestamps in message rows and anchor new conversations to the first valid message time.
- Existing individual contact names are preserved; group names may refresh from the latest subject.
- Per-message failures use savepoints so one bad row can be counted without rolling back the whole import.
- Direct DB import inserts metadata/content only; REST attachment upload is orchestrated from parent `sync.go` when enabled.
- `Importer.Close()` is idempotent and is reached through `SyncService.Close()` on REST shutdown.

## ANTI-PATTERNS

- Do not use this package for live Chatwoot forwarding.
- Do not key idempotency only by conversation; resolved/reopened conversations can move rows.
- Do not overwrite existing one-to-one contact names when attaching a WhatsApp JID.
- Do not insert blank Chatwoot messages when both content and media placeholder are empty.
- Do not bypass account/inbox verification when opening a direct database connection.

## TESTING

- Keep sqlmock expectations close to the SQL shape; broad regexes hide schema regressions.
- Add focused unit tests for identity/content helpers before changing import flow tests.
