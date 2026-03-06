# infrastructure/whatsapp

WhatsApp protocol integration layer using whatsmeow. Handles device management, event processing, chat storage wrappers, and webhook forwarding.

## STRUCTURE
```
whatsapp/
├── device_manager.go      # DeviceManager: multi-device lifecycle (602 lines)
├── device_instance.go     # DeviceInstance: ID()/JID() + client wrapper
├── event_handler.go       # Central event dispatcher (switch on event type)
├── event_message.go       # Message event → storage + webhook
├── event_archive.go       # Archive event → local DB update
├── event_receipt.go       # Read receipts
├── event_call.go          # Call events
├── event_delete.go        # Message deletion events
├── event_group.go         # Group info changes
├── event_newsletter.go    # Newsletter events
├── history_sync.go        # WhatsApp history sync to local DB
├── chatstorage_wrapper.go # deviceChatStorage: injects device_id into all repo calls
├── jid_utils.go           # NormalizeJIDFromLID: @lid → @s.whatsapp.net
├── webhook.go             # Webhook dispatch
├── webhook_forward.go     # Forward events to external webhooks
├── client_lifecycle.go    # Connect/disconnect/reconnect logic
├── context_device.go      # Context helpers for device resolution
├── auto_reply.go          # Auto-reply message handling
├── cleanup.go             # Resource cleanup
├── database.go            # DB connection setup
├── init.go                # Package initialization
└── logger.go              # Custom whatsmeow logger adapter
```

## CRITICAL PATTERNS

### JID Normalization
WhatsApp uses LID JIDs (`@lid`) internally. **Always** normalize before DB lookups:
```go
normalizedJID := NormalizeJIDFromLID(ctx, evt.JID, client)
jidStr := normalizedJID.String()
```

### Device ID for DB Lookups
DB stores device_id as JID without device number. Use `client.Store.ID.ToNonAD().String()` — **never** `instance.ID()` for chat storage queries:
```go
deviceID := client.Store.ID.ToNonAD().String()  // "6289605618749@s.whatsapp.net"
// NOT instance.ID() which returns "6289605618749:11@s.whatsapp.net"
```

### Event Handler Pattern
All event handlers follow: `func handleX(ctx, evt, chatStorageRepo, client)` → normalize JIDs → lookup/update DB → forward to webhook.

### Wrapper Pattern
`deviceChatStorage` / `DeviceRepository` wrap all `IChatStorageRepository` calls to inject device_id automatically. New interface methods must be added to BOTH wrappers.

## ANTI-PATTERNS
- **Never** use raw JID from events for DB queries without `NormalizeJIDFromLID`
- **Never** add interface methods without updating `chatstorage_wrapper.go` and `device_repository.go`
- **Never** use `instance.ID()` for chat storage — use `client.Store.ID.ToNonAD().String()`
