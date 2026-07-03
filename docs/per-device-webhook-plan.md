# Per-Device Webhook Implementation Plan

## Status: IMPLEMENTED

## Overview

Add per-device webhook support where each device can have its own webhook URL. If a device has no custom webhook URL, it falls back to the global webhook.

## Flow Diagram

```text
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
│  │  STEP 2: Call getWebhookConfigForDevice(deviceJID)                    │  │
│  │          - Looks up device record by JID                              │  │
│  │          - If device has custom webhook_url, return device config      │  │
│  │          - Otherwise return global config.WhatsappWebhook settings     │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                    ┌───────────────┴───────────────┐
                    │                               │
                    ▼                               ▼
         ┌──────────────────┐           ┌────────────────────────┐
         │ Device has custom │           │ Device webhook URL is  │
         │ webhook config?   │           │ NULL or empty?          │
         └──────────────────────┘           └─────────────────────────┘
                    │                               │
              YES   │                               │ YES
                    ▼                               ▼
    ┌────────────────────────┐           ┌─────────────────────────┐
    │ Use device webhook    │           │ Use global config       │
    │ config (override)     │           │ WhatsappWebhook[]       │
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

```text
Client                    REST API                    UseCase                  Repository
  │                          │                           │                          │
  │ PATCH /devices/:id/webhook│                          │                          │
  │ { "webhook_url": "...",  │                           │                          │
  │   "webhook_secret": "..."}                           │                          │
  │─────────────────────────>│                           │                          │
  │                          │ SetDeviceWebhookConfig()   │                          │
  │                          │──────────────────────────>│                          │
  │                          │                           │ SetDeviceWebhookConfig() │
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
| `domains/chatstorage/chatstorage.go` | Added webhook URL, secret, event whitelist, and TLS-skip fields plus `DeviceWebhookConfig` |
| `domains/chatstorage/interfaces.go` | Added `GetDeviceRecordByJID`, URL-only helpers, and full webhook config helpers |
| `infrastructure/chatstorage/sqlite_repository.go` | Added migrations #31-#34, updated queries, implemented new methods |
| `infrastructure/whatsapp/chatstorage_wrapper.go` | Added wrapper methods for new interface methods |
| `infrastructure/whatsapp/webhook_forward.go` | Added `getWebhookConfigForDevice()`, device event filtering, and updated forwarding logic |
| `infrastructure/whatsapp/device_manager.go` | Added `GetStorage()` method |
| `domains/device/interfaces.go` | Added URL-only and full-config webhook methods to `IDeviceUsecase` |
| `usecase/device.go` | Implemented URL-only and full-config webhook methods |
| `ui/rest/device.go` | Added `PATCH /devices/:device_id/webhook`, `GET /devices/:device_id/webhook` |
| `infrastructure/whatsapp/webhook_forward_test.go` | Added per-device webhook tests |
| `usecase/device_test.go` | Added device service tests |
| `docs/openapi.yaml` | Added webhook API documentation |

## Database Changes

Migrations #31-#34 add the per-device webhook fields to the `devices` table:

- `webhook_url TEXT DEFAULT NULL`
- `webhook_secret TEXT DEFAULT ''`
- `webhook_events TEXT DEFAULT ''`
- `webhook_insecure_skip_verify BOOLEAN DEFAULT FALSE`

## API Endpoints

- `PATCH /devices/{device_id}/webhook` - Set device-specific webhook configuration
  - Body: `{ "webhook_url": "https://example.com/webhook", "webhook_secret": "secret", "webhook_events": "message,message.ack", "webhook_insecure_skip_verify": false }`
  - Set to empty string `""` to clear and use global webhook

- `GET /devices/{device_id}/webhook` - Get device-specific webhook configuration
  - Returns empty values if not set (use global)

## Testing

### Unit Tests

**`infrastructure/whatsapp/webhook_forward_test.go`:**
- `TestGetWebhookConfigForDevice_NoDeviceID` - Returns global webhook config when device JID is empty
- `TestGetWebhookConfigForDevice_DeviceNotFound` - Falls back to global when device not found
- `TestGetWebhookConfigForDevice_FallbackToGlobal` - Falls back to global when device has no custom webhook
- `TestGetWebhookConfigForDevice_DeviceSpecificOverride` - Uses the full device-specific webhook config when set
- `TestForwardPayloadToConfiguredWebhooks_WithDeviceSpecificWebhook` - Uses device-specific webhook when set
- `TestForwardPayloadToConfiguredWebhooks_DeviceWebhookCleared_FallsBackToGlobal` - Falls back to global when device webhook is cleared
- `TestForwardPayloadToConfiguredWebhooks_DeviceWebhookOnly_NoGlobal` - Uses device-specific webhook with no global webhook
- `TestSQLiteRepositoryGetsDeviceWebhookConfigByJID` - Persists and resolves the full device webhook config
- `TestAddDevice_ForwardsFullWebhookConfig` - Accepts full webhook config when creating a device

**`usecase/device_test.go`:**
- `TestDeviceServiceInterface` - Verifies interface implementation
- `TestSetDeviceWebhook_InvalidManager` - Error handling for nil manager
- `TestGetDeviceWebhook_InvalidManager` - Error handling for nil manager

## OpenAPI Documentation

Added to `docs/openapi.yaml`:

- `GET /devices/{device_id}/webhook` - Get device webhook configuration
- `PATCH /devices/{device_id}/webhook` - Set device webhook configuration
