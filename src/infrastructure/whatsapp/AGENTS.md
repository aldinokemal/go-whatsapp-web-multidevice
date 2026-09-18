# WhatsApp infrastructure

This package owns clients, device lifecycle, JIDs, events, presence, and forwarding.

- Lifecycle: `device_manager.go`, `device_instance.go`, `client_lifecycle.go`.
  Registry aliases differ from logged-in storage identities: use
  `client.Store.ID.ToNonAD().String()` for chat/message `device_id`, not `instance.ID()`.
- Events: register event types in `event_handler.go` and carry
  `ContextWithDevice(ctx, instance)` downstream. Keep event-side storage access
  behind `chatstorage_wrapper.go`, including new repository methods.
- JIDs: use `NormalizeJIDFromLID` in `jid_utils.go` before phone-JID lookups/storage
  and `ToNonAD()` for stable identities. LID lookup may fail or have nil client,
  store, or LID store; preserve the original JID fallback.
- Webhooks: `event_message.go` and `webhook_forward.go`; use
  [the payload contract](../../../docs/webhook-payload.md) for payload changes.
  Keep work bounded and failures observable without blocking the event loop.
  Preserve the `evt.Sender.Device != 0` receipt guard to prevent linked-device duplicates.
- Chatwoot retries: `StartChatwootForwardRetryWorker` uses durable chat-storage jobs
  keyed by device, event name, and WhatsApp message ID. Initialize the client registry
  before the retry worker; preserve device routing when replaying queued events.
- Presence: `presence_pulse.go` targets connected, logged-in devices, then returns
  them to unavailable. `../../cmd/helpers.go` guards process-wide startup with
  `sync.Once`; do not introduce a second scheduler.
- Session keys: `key_cache.go` redirects selected key stores. Leave privacy tokens
  in durable primary WhatsApp storage rather than moving them to a volatile keys DB.
- History: `history_sync.go` handles batches; preserve the same device and JID rules.

Tests commonly exercise unexported helpers and replace package globals. Restore
replacements and keep scheduler tests serial; use fake clients/clocks and bounded
waits for presence behavior.
