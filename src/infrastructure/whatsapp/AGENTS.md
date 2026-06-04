# WHATSAPP INFRASTRUCTURE

Generated: 2026-06-05

## OVERVIEW

This package owns whatsmeow clients, multi-device lifecycle, JID normalization, events, send retry helpers,
presence pulse scheduling, webhook forwarding, and the event-side chatstorage wrapper.

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Device lifecycle | `device_manager.go`, `device_instance.go`, `client_lifecycle.go` | Create/load/purge/reconnect and persisted registry behavior. |
| Event routing | `event_handler.go`, `event_*.go` | Add new event types to the central switch. |
| Message webhooks | `event_message.go`, `webhook_forward.go` | Payload construction, media fields, signatures, event filters. |
| Send retry / 463 | `send_retry.go`, `key_cache.go` | Reachout timelock retry and privacy-token store wiring. |
| Presence behavior | `event_handler.go`, `event_chat_presence.go`, `presence_pulse.go` | Connect-time presence, chat presence webhooks, scheduled daily pulses. |
| History import | `history_sync.go` | Stores chats/messages for history sync batches. |
| JID conversion | `jid_utils.go`, `context_device.go` | LID normalization and request/device context. |
| Storage wrapper | `chatstorage_wrapper.go` | Injects device ID into chat/message repository operations. |

## CONVENTIONS

- `DeviceManager` keys the active registry by requested device ID or alias, but logged-in storage identity may become the WhatsApp JID.
- Chat/message table `device_id` should be the WhatsApp JID without device number: `client.Store.ID.ToNonAD().String()`.
- Event handlers call `ContextWithDevice(ctx, instance)` before downstream logic.
- Presence pulse only targets connected and logged-in devices, then returns them to unavailable after the configured duration.
- Normalize `@lid` JIDs with `NormalizeJIDFromLID(ctx, jid, client)` before DB lookup/storage when a phone JID is needed.
- Use `ToNonAD()` when persisting or emitting stable non-device JIDs.
- Webhook forwarding uses goroutines and bounded contexts in selected handlers; keep failures logged without blocking the event loop.
- `chatstorage_wrapper.go` should provide device defaults for scoped repository methods, including message edit and device-scoped ID lookups.

## ANTI-PATTERNS

- Do not store `instance.ID()` as chat/message `device_id` after a real WhatsApp JID is available.
- Do not bypass `chatstorage_wrapper.go` for event-side chat/message access.
- Do not add an `IChatStorageRepository` method without implementing the wrapper method.
- Do not remove the receipt `Device == 0` filter; linked devices produce duplicate receipts.
- Do not start a second presence pulse loop; `cmd/helpers.go` uses `sync.Once` for process-wide startup.
- Do not move privacy tokens to a volatile keys DB; long-lived sessions need them in durable primary WhatsApp storage.
- Do not assume `client`, `client.Store`, or `client.Store.LIDs` is non-nil during LID resolution.

## TESTING

- Tests in this package often exercise unexported helpers directly from package `whatsapp`.
- Webhook tests replace package-level functions/globals and restore them with `defer`; preserve cleanup on new tests.
- Presence pulse tests use fake clients, injected clocks/sleeps, channels, and timeouts; avoid `t.Parallel` around shared scheduler state.
