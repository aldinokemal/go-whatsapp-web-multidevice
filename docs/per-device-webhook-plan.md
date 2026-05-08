# Per-Device Webhook Implementation Plan

## Status: IMPLEMENTED

## Overview

Add per-device webhook support where each device can have its own webhook URL. If a device has no custom webhook URL, it falls back to the global webhook.

## Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         WHATSAPP EVENT RECEIVED                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  event_handler.go :: handler()                                             │
│  - Routes event to appropriate handler (message, receipt, etc.)             │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Event-specific handler (e.g., event_message_handler.go)                   │
│  - Creates webhook payload with device_id                                   │
│  - Calls forwardMessageToWebhook()                                          │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  webhook_forward.go :: forwardPayloadToConfiguredWebhooks()                 │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  STEP 1: Extract device JID from payload["device_id"]                │  │
│  │  STEP 2: Call getWebhookURLsForDevice(deviceJID)                      │  │
│  │          - Looks up device record by JID                              │  │
│  │          - If device has custom webhook_url, return it only           │  │
│  │          - Otherwise return global config.WhatsappWebhook              │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                    ┌───────────────┴───────────────┐
                    │                               │
                    ▼                               ▼
         ┌──────────────────┐           ┌────────────────────────┐
         │ Device has custom │           │ Device webhook is NULL │
         │ webhook_url?      │           │ or empty?               │
         └──────────────────────┘           └─────────────────────────┘
                    │                               │
              YES   │                               │ YES
                    ▼                               ▼
    ┌────────────────────────┐           ┌─────────────────────────┐
    │ Use device webhook URL │           │ Use global config       │
    │ (override global)     │           │ WhatsappWebhook[]       │
    └────────────────────────┘           └─────────────────────────┘
                    │                               │
                    └───────────────┬───────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  webhook_forward.go :: forwardToWebhooks()                                  │
│  - Iterates webhook URLs                                                    │
│  - For each URL: submitWebhook() with HMAC signature                         │
│  - 5 retries with exponential backoff                                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                    ┌───────────────┴───────────────┐
                    │                               │
                    ▼                               ▼
         ┌──────────────────┐           ┌────────────────────────┐
         │ All webhooks     │           │ Some webhooks failed   │
         │ succeeded        │           │ (partial failure OK)   │
         └──────────────────┘           └────────────────────────┘
```

## API Flow: Set Device Webhook

```
Client                    REST API                    UseCase                  Repository
  │                          │                           │                          │
  │ PATCH /devices/:id/webhook│                          │                          │
  │ { "webhook_url": "..." } │                           │                          │
  │─────────────────────────>│                           │                          │
  │                          │ SetDeviceWebhook()         │                          │
  │                          │──────────────────────────>│                          │
  │                          │                           │ SetDeviceWebhookURL()    │
  │                          │                           │────────────────────────>│
  │                          │                           │                          │
  │                          │                           │                          │
  │ 200 OK                   │                           │                          │
  │<─────────────────────────│                           │                          │
  │                          │                           │                          │
```

## Files Modified

| File | Change |
|------|--------|
| `domains/chatstorage/chatstorage.go` | Added `WebhookURL *string` to `DeviceRecord` struct |
| `domains/chatstorage/interfaces.go` | Added `GetDeviceRecordByJID`, `SetDeviceWebhookURL`, `GetDeviceWebhookURL` |
| `infrastructure/chatstorage/sqlite_repository.go` | Added migration #17, updated queries, implemented new methods |
| `infrastructure/whatsapp/chatstorage_wrapper.go` | Added wrapper methods for new interface methods |
| `infrastructure/whatsapp/webhook_forward.go` | Added `getWebhookURLsForDevice()`, updated forwarding logic |
| `infrastructure/whatsapp/device_manager.go` | Added `GetStorage()` method |
| `domains/device/interfaces.go` | Added `SetDeviceWebhook`, `GetDeviceWebhook` to `IDeviceUsecase` |
| `usecase/device.go` | Implemented `SetDeviceWebhook`, `GetDeviceWebhook` |
| `ui/rest/device.go` | Added `PATCH /devices/:device_id/webhook`, `GET /devices/:device_id/webhook` |
| `infrastructure/whatsapp/webhook_forward_test.go` | Added per-device webhook tests |
| `usecase/device_test.go` | Added device service tests |
| `docs/openapi.yaml` | Added webhook API documentation |

## Database Changes

**Migration #17** adds `webhook_url TEXT DEFAULT ''` column to `devices` table.

## API Endpoints

- `PATCH /devices/{device_id}/webhook` - Set device-specific webhook URL
  - Body: `{ "webhook_url": "https://example.com/webhook" }`
  - Set to empty string `""` to clear and use global webhook

- `GET /devices/{device_id}/webhook` - Get device-specific webhook URL
  - Returns empty string if not set (use global)

## Testing

### Unit Tests

**`infrastructure/whatsapp/webhook_forward_test.go`:**
- `TestGetWebhookURLsForDevice_NoDeviceJID` - Returns global webhook when device JID is empty
- `TestGetWebhookURLsForDevice_DeviceNotFound` - Falls back to global when device not found
- `TestGetWebhookURLsForDevice_FallbackToGlobal` - Falls back to global when device has no custom webhook
- `TestForwardPayloadToConfiguredWebhooks_WithDeviceSpecificWebhook` - Uses device-specific webhook when set
- `TestForwardPayloadToConfiguredWebhooks_DeviceWebhookCleared_FallsBackToGlobal` - Falls back to global when device webhook is cleared

**`usecase/device_test.go`:**
- `TestDeviceServiceInterface` - Verifies interface implementation
- `TestSetDeviceWebhook_InvalidManager` - Error handling for nil manager
- `TestGetDeviceWebhook_InvalidManager` - Error handling for nil manager
- `TestGetDeviceWebhook_DeviceNotFound` - Error handling for missing device
- `TestSetDeviceWebhook_DeviceNotFound` - Error handling for missing device

## OpenAPI Documentation

Added to `docs/openapi.yaml`:

- `GET /devices/{device_id}/webhook` - Get device webhook URL
- `PATCH /devices/{device_id}/webhook` - Set device webhook URL
