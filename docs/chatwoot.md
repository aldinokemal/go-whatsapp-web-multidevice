# Chatwoot Integration

This document provides comprehensive documentation for integrating Go WhatsApp Web Multidevice with Chatwoot for customer support.

## Overview

The Chatwoot integration allows you to:
- Receive WhatsApp messages in your Chatwoot inbox
- Reply to WhatsApp messages directly from Chatwoot
- Support text messages, images, audio, video, and file attachments
- Handle both individual chats and group conversations

## Prerequisites

Before setting up the integration, ensure you have:

1. **Go WhatsApp Web Multidevice** running and accessible via a public URL
2. **Chatwoot** instance (self-hosted or cloud) with admin access
3. **API Channel** inbox created in Chatwoot
4. At least one WhatsApp device connected and logged in

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `CHATWOOT_ENABLED` | Yes | `false` | Enable Chatwoot integration |
| `CHATWOOT_URL` | Yes | - | Your Chatwoot instance URL (e.g., `https://app.chatwoot.com`) |
| `CHATWOOT_API_TOKEN` | Yes | - | API access token from Chatwoot |
| `CHATWOOT_ACCOUNT_ID` | Yes | - | Your Chatwoot account ID |
| `CHATWOOT_INBOX_ID` | Yes | - | The inbox ID for WhatsApp messages |
| `CHATWOOT_DEVICE_ID` | No | - | Specific device ID for outbound messages (required for multi-device setups) |
| `CHATWOOT_IMPORT_MESSAGES` | No | `false` | Enable message history sync to Chatwoot |
| `CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES` | No | `3` | Number of days of history to import |

### Configuration Examples

**Environment File (.env):**
```bash
CHATWOOT_ENABLED=true
CHATWOOT_URL=https://app.chatwoot.com
CHATWOOT_API_TOKEN=your_api_token_here
CHATWOOT_ACCOUNT_ID=12345
CHATWOOT_INBOX_ID=67890
CHATWOOT_DEVICE_ID=my-whatsapp-device

# Optional: History sync settings
CHATWOOT_IMPORT_MESSAGES=true
CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7
```

**CLI Flags:**
```bash
./whatsapp rest \
  --chatwoot-enabled=true \
  --chatwoot-device-id="my-device-id"
```

**Docker Compose:**
```yaml
services:
  whatsapp-api:
    image: aldinokemal2104/go-whatsapp-web-multidevice:latest
    environment:
      - CHATWOOT_ENABLED=true
      - CHATWOOT_URL=https://app.chatwoot.com
      - CHATWOOT_API_TOKEN=your_api_token
      - CHATWOOT_ACCOUNT_ID=12345
      - CHATWOOT_INBOX_ID=67890
      - CHATWOOT_DEVICE_ID=my-device
    command: rest
```

## Chatwoot Setup

### Step 1: Create an API Channel Inbox

1. Log in to your Chatwoot dashboard
2. Navigate to **Settings** > **Inboxes**
3. Click **Add Inbox**
4. Select **API** as the channel type
5. Configure the inbox:
   - **Name**: WhatsApp (or any descriptive name)
   - **Webhook URL**: Leave empty for now (we'll configure this in Step 4)
6. Click **Create Inbox**
7. Note down the **Inbox ID** (visible in the URL or inbox settings)

### Step 2: Get Your API Token

1. Navigate to **Settings** > **Profile Settings**
2. Scroll to **Access Token** section
3. Copy your API access token

### Step 3: Find Your Account ID

Your account ID is visible in the URL when logged into Chatwoot:
```
https://app.chatwoot.com/app/accounts/[ACCOUNT_ID]/dashboard
```

### Step 4: Configure the Webhook

1. Navigate to **Settings** > **Integrations** > **Webhooks**
2. Click **Add new webhook**
3. Configure:
   - **URL**: `https://your-whatsapp-api.com/chatwoot/webhook`
   - **Events**: Select `message_created`
4. Click **Create**

> **Important:** The webhook URL must be publicly accessible. If you're running locally, use a tunneling service like ngrok.

## Multi-Device Setup

When running with multiple WhatsApp devices, you **must** specify which device should handle Chatwoot outbound messages using `CHATWOOT_DEVICE_ID`.

### Finding Your Device ID

1. Access your WhatsApp API at `http://your-api:3000`
2. Navigate to the device management section
3. Note the Device ID of the device you want to use for Chatwoot

Alternatively, use the API:
```bash
curl http://your-api:3000/devices
```

### Configuration

```bash
# Using JID format
CHATWOOT_DEVICE_ID=628123456789@s.whatsapp.net

# Or using custom device ID
CHATWOOT_DEVICE_ID=my-support-device
```

### Important Notes

- If `CHATWOOT_DEVICE_ID` is **not set** and **only one device** is registered, that device will be used automatically
- If `CHATWOOT_DEVICE_ID` is **not set** and **multiple devices** exist, outbound messages will **fail**
- If the specified device is not found or not connected, outbound messages will fail with a `DEVICE_NOT_AVAILABLE` error

## Message History Sync

The history sync feature allows you to import existing WhatsApp message history into Chatwoot. This is useful when you want to have context from past conversations when starting to use Chatwoot.

### Enabling Auto-Sync

To automatically sync history when a device connects:

```bash
CHATWOOT_IMPORT_MESSAGES=true
CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7  # Sync last 7 days
```

### Manual Sync via API

You can trigger a sync manually using the REST API:

**Start Sync:**
```bash
curl -X POST "http://your-api:3000/chatwoot/sync" \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "my-device-id",
    "days_limit": 7,
    "include_media": true,
    "include_groups": true
  }'
```

**Check Sync Status:**
```bash
curl "http://your-api:3000/chatwoot/sync/status?device_id=my-device-id"
```

### Sync Options

| Option | Default | Description |
|--------|---------|-------------|
| `days_limit` | 3 | Number of days of history to import |
| `include_media` | true | Download and sync media attachments |
| `include_groups` | true | Include group chat messages |

### How It Works

1. **Reads stored messages** from the local chat storage database
2. **Creates contacts** in Chatwoot for each chat participant
3. **Creates conversations** for each chat
4. **Imports messages** with timestamps, preserving chronological order
5. **Downloads and attaches media** (if enabled and media is still available)

### Notes

- Messages are prefixed with their original timestamp for context: `[2024-01-15 14:30] Hello!`
- Group messages include the sender name: `[2024-01-15 14:30] John: Hello!`
- Media older than ~2 weeks may be unavailable on WhatsApp servers
- The sync runs in the background and can be monitored via the status endpoint
- Only one sync can run per device at a time

## Supported Features

### Incoming Messages (WhatsApp → Chatwoot)

| Message Type | Supported | Notes |
|--------------|-----------|-------|
| Text | ✅ | Full text content preserved |
| Images | ✅ | Displayed as attachments |
| Audio | ✅ | Displayed as attachments |
| Video | ✅ | Displayed as attachments |
| Documents | ✅ | Displayed as attachments |
| Stickers | ✅ | Displayed as image attachments |
| Location | ✅ | Shown as text with coordinates |
| Contacts | ✅ | vCard information preserved |

**Outgoing messages (sent from your own WhatsApp device)** are automatically forwarded to Chatwoot as `outgoing` messages.

### Outgoing Messages (Chatwoot → WhatsApp)

| Message Type | Supported | Notes |
|--------------|-----------|-------|
| Text | ✅ | - |
| Images | ✅ | Sent with optional caption |
| Audio | ✅ | Sent as voice note (PTT) |
| Video | ✅ | - |
| Files | ✅ | Any file type supported |

### Group Support

- Groups are automatically detected by JID format (`@g.us`)
- Group name is used as contact name in Chatwoot
- Replies go to the correct group chat
- Group messages include sender name prefix

## Architecture

```
┌─────────────────────┐         ┌──────────────────────┐
│   WhatsApp User     │         │      Chatwoot        │
│                     │         │                      │
└─────────┬───────────┘         └───────────┬──────────┘
          │                                 │
          │ Incoming                        │ Outgoing
          │ Message                         │ Message
          ▼                                 ▼
┌─────────────────────────────────────────────────────────┐
│            Go WhatsApp Web Multidevice                  │
│                                                         │
│  ┌─────────────────┐     ┌─────────────────────────┐   │
│  │ Event Handler   │     │  /chatwoot/webhook      │   │
│  │ (Message Event) │     │  (POST endpoint)        │   │
│  └────────┬────────┘     └───────────┬─────────────┘   │
│           │                          │                  │
│           │ Forward to               │ Resolve Device   │
│           │ Chatwoot API             │ & Send Message   │
│           ▼                          ▼                  │
│  ┌─────────────────┐     ┌─────────────────────────┐   │
│  │ Chatwoot Client │     │  Device Manager         │   │
│  │ (Create Message)│     │  (CHATWOOT_DEVICE_ID)   │   │
│  └─────────────────┘     └─────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### Message Flow

1. **Incoming (WhatsApp → Chatwoot)**:
   - WhatsApp message received by connected device
   - Event handler processes the message
   - Message forwarded to Chatwoot API
   - Contact/conversation created if needed
   - Message appears in Chatwoot inbox

2. **Outgoing (Chatwoot → WhatsApp)**:
   - Agent replies in Chatwoot
   - Chatwoot sends webhook to `/chatwoot/webhook`
   - Handler resolves device from `CHATWOOT_DEVICE_ID` or default
   - Message sent via WhatsApp
   - Delivery confirmed

## Troubleshooting

### Outbound Messages Not Sending

**Symptoms:** Messages typed in Chatwoot are not delivered to WhatsApp

**Possible Causes & Solutions:**

1. **Device not specified or not found**
   - Check logs for "Failed to resolve device" errors
   - Ensure `CHATWOOT_DEVICE_ID` is set correctly
   - Verify the device is registered: `curl http://your-api:3000/devices`

2. **Webhook not configured**
   - Verify the webhook URL in Chatwoot settings
   - Ensure the URL is publicly accessible
   - Check that `message_created` event is selected

3. **Device not logged in**
   - Check device status: `curl http://your-api:3000/devices/{device_id}/status`
   - Reconnect the device if disconnected

4. **CHATWOOT_ENABLED not set**
   - Verify `CHATWOOT_ENABLED=true` in your configuration

### Incoming Messages Not Appearing in Chatwoot

**Symptoms:** WhatsApp messages are not showing in Chatwoot inbox

**Possible Causes & Solutions:**

1. **Chatwoot not enabled**
   - Verify `CHATWOOT_ENABLED=true` in configuration

2. **Invalid API credentials**
   - Double-check `CHATWOOT_API_TOKEN`
   - Verify `CHATWOOT_ACCOUNT_ID` and `CHATWOOT_INBOX_ID`

3. **Contact/Conversation issues**
   - Check API logs for contact creation errors
   - Verify Chatwoot inbox is properly configured

4. **Network connectivity**
   - Ensure the API server can reach Chatwoot URL
   - Check for firewall rules blocking outbound connections

### Debug Logging

Enable debug mode to see detailed Chatwoot integration logs:

```bash
./whatsapp rest --debug=true
```

Or via environment variable:
```bash
APP_DEBUG=true ./whatsapp rest
```

Look for log entries starting with:
- `Chatwoot Webhook:` - Webhook processing
- `Chatwoot:` - API operations (contact/conversation/message creation)

### Common Error Messages

| Error | Meaning | Solution |
|-------|---------|----------|
| `DEVICE_NOT_AVAILABLE` | No device found for sending | Set `CHATWOOT_DEVICE_ID` or ensure one device is registered |
| `device not found` | Specified device ID doesn't exist | Check device ID spelling and registration |
| `device id is required` | No device ID and multiple devices exist | Set `CHATWOOT_DEVICE_ID` |
| `failed to create contact` | Chatwoot API error | Verify API token and account permissions |
| `Invalid payload` | Malformed webhook request | Check Chatwoot webhook configuration |

### Verifying Webhook Connectivity

Test your webhook endpoint:

```bash
curl -X POST https://your-api.com/chatwoot/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "event": "message_created",
    "message_type": "outgoing",
    "content": "Test message",
    "private": false,
    "conversation": {
      "id": 1,
      "meta": {
        "sender": {
          "id": 1,
          "phone_number": "+1234567890"
        }
      }
    }
  }'
```

Expected response: `200 OK` or error with details

## Best Practices

1. **Use a dedicated device** for Chatwoot integration in production
2. **Set up monitoring** for device connection status
3. **Configure auto-reconnect** to maintain service availability
4. **Test webhook connectivity** before going live
5. **Use HTTPS** for webhook URLs in production
6. **Set `CHATWOOT_DEVICE_ID`** explicitly in multi-device environments
7. **Monitor logs** for failed message deliveries

## Security Considerations

- Keep `CHATWOOT_API_TOKEN` secure and rotate periodically
- Use HTTPS for all webhook communications
- Consider network-level restrictions on the webhook endpoint
- Monitor for unusual activity in Chatwoot logs
- Use strong authentication for the WhatsApp API (`APP_BASIC_AUTH`)
- **Note:** The `/chatwoot/webhook` endpoint is excluded from basic auth to allow Chatwoot to send webhooks without credentials. The `/chatwoot/sync` endpoints require authentication.

## API Reference

### Webhook Endpoint

**URL:** `POST /chatwoot/webhook`

**Headers:**
- `Content-Type: application/json`

**Request Body:** Standard Chatwoot webhook payload

**Response Codes:**
- `200 OK` - Message processed (or skipped)
- `503 Service Unavailable` - No device available

### Related Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/devices` | GET | List all registered devices |
| `/devices/{id}` | GET | Get device details |
| `/devices/{id}/status` | GET | Check device connection status |
