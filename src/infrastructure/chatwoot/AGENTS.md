# Chatwoot integration

Use [configuration and routing docs](../../../docs/chatwoot.md) when changing setup,
routing modes, or import configuration. `client.go` owns REST calls, `provision.go` inbox setup,
`client_registry.go` destination routing, and `sync.go` history orchestration.
Direct Postgres changes have a [scoped guide](pgimport/AGENTS.md).

## Routing and live sync

- Preserve device context on Chatwoot-to-WhatsApp sends. Once per-device configs
  exist, `ClientRegistry` must not fall back to the global env inbox for an unmapped
  or disabled device. Numeric account/inbox IDs differ from API-channel identifiers.
- Webhooks in `../../ui/rest/chatwoot.go` bypass app Basic Auth and check
  `CHATWOOT_WEBHOOK_SECRET` when configured. Only outgoing messages send back to
  WhatsApp; preserve `source_id = WAID:<message_id>` and sent-message echo guards.
- Persistent links support idempotency, replies, read/delete sync, and reverse
  routing. Preserve device/account/config scope; separate servers can reuse numeric
  IDs. Per-device webhook URLs disambiguate otherwise ambiguous agent-initiated sends.
- Install the client registry before starting the persistent forward-retry worker
  in `../whatsapp/webhook_forward.go`.
- Private contacts use normalized phone lookup; groups and unresolved `@lid`
  contacts use identifiers/custom attributes. Keep API tokens and config secrets out of logs.

## History import

- Direct Postgres import is separate from live REST forwarding and is supported
  only in legacy/env mode with the global DSN. Per-device configs use REST import.
- Direct import writes historical content/metadata. With
  `CHATWOOT_IMPORT_MEDIA_WITH_REST`, downloadable media first goes through REST for
  attachments; shared source IDs prevent duplicate import. Preserve configurable
  placeholders for media that cannot be downloaded.
- `SyncService.Close()` closes its importer pool; preserve cleanup of cached sync
  services on shutdown. Use sync/registry tests for routing changes and `pgimport`
  sqlmock tests for direct SQL behavior.
