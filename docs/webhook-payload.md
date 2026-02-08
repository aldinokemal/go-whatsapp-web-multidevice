# Webhook Payload Documentation

This document provides comprehensive documentation for the webhook payload structure used by the Go WhatsApp Web
Multidevice application.

## Overview

The webhook system sends HTTP POST requests to configured URLs whenever WhatsApp events occur. Each webhook request
includes event data in JSON format and security headers for verification.

## Available Webhook Events

The following events can be received via webhook:

| Event                | Description                                             |
|----------------------|---------------------------------------------------------|
| `message`            | Text, media, contact, location, and other message types |
| `message.reaction`   | Emoji reactions to messages                             |
| `message.revoked`    | Deleted/revoked messages                                |
| `message.edited`     | Edited messages                                         |
| `message.ack`        | Delivery and read receipts                              |
| `message.deleted`    | Messages deleted for the user                           |
| `group.participants` | Group member join/leave/promote/demote events           |
| `group.joined`       | You were added to a group                               |
| `newsletter.joined`  | You subscribed to a newsletter/channel                  |
| `newsletter.left`    | You unsubscribed from a newsletter                      |
| `newsletter.message` | New message(s) posted in a newsletter                   |
| `newsletter.mute`    | Newsletter mute setting changed                         |
| `call.offer`         | Incoming call received                                  |

## Event Filtering

You can configure which events are forwarded to your webhook using the `WHATSAPP_WEBHOOK_EVENTS` environment variable or
`--webhook-events` CLI flag.

### Configuration Examples

**Environment Variable:**

```bash
# Only receive message and read receipt events
WHATSAPP_WEBHOOK_EVENTS=message,message.ack

# Receive all message-related events
WHATSAPP_WEBHOOK_EVENTS=message,message.reaction,message.revoked,message.edited,message.ack,message.deleted

# Receive only group events
WHATSAPP_WEBHOOK_EVENTS=group.participants

# Receive newsletter events
WHATSAPP_WEBHOOK_EVENTS=newsletter.joined,newsletter.left,newsletter.message,newsletter.mute

# Receive call events
WHATSAPP_WEBHOOK_EVENTS=call.offer

# Receive all group and newsletter events
WHATSAPP_WEBHOOK_EVENTS=group.participants,group.joined,newsletter.joined,newsletter.left,newsletter.message
```

**CLI Flag:**

```bash
# Only receive message events
./whatsapp rest --webhook="https://yourapp.com/webhook" --webhook-events="message"

# Receive message and group events
./whatsapp rest --webhook="https://yourapp.com/webhook" --webhook-events="message,group.participants"
```

**Behavior:**

- If `WHATSAPP_WEBHOOK_EVENTS` is empty or not set, **all events** are forwarded (default behavior)
- If configured, only the specified events are forwarded to webhooks
- Event names are case-insensitive

## Security

### HMAC Signature Verification

All webhook requests include an HMAC SHA256 signature for security verification:

- **Header**: `X-Hub-Signature-256`
- **Format**: `sha256={signature}`
- **Algorithm**: HMAC SHA256
- **Default Secret**: `secret` (configurable via `--webhook-secret` or `WHATSAPP_WEBHOOK_SECRET`)

### Verification Example (Node.js)

```javascript
const crypto = require('crypto');

function verifyWebhookSignature(payload, signature, secret) {
    const expectedSignature = crypto
        .createHmac('sha256', secret)
        .update(payload, 'utf8')
        .digest('hex');

    const receivedSignature = signature.replace('sha256=', '');
    return crypto.timingSafeEqual(
        Buffer.from(expectedSignature, 'hex'),
        Buffer.from(receivedSignature, 'hex')
    );
}
```

### Verification Example (Python)

```python
import hmac
import hashlib

def verify_webhook_signature(payload, signature, secret):
    expected_signature = hmac.new(
        secret.encode('utf-8'),
        payload,
        hashlib.sha256
    ).hexdigest()
    
    received_signature = signature.replace('sha256=', '')
    return hmac.compare_digest(expected_signature, received_signature)
```

## Payload Structure

All webhook payloads follow a consistent top-level structure:

```json
{
  "event": "message",
  "device_id": "628123456789@s.whatsapp.net",
  "payload": {
    // Event-specific fields
  }
}
```

### Top-Level Fields

| **Field**   | **Type** | **Description**                                                                                                     |
|-------------|----------|---------------------------------------------------------------------------------------------------------------------|
| `event`     | string   | Event type: `message`, `message.reaction`, `message.revoked`, `message.edited`, `message.ack`, `message.deleted`, `group.participants`, `group.joined`, `newsletter.joined`, `newsletter.left`, `newsletter.message`, `newsletter.mute`, `call.offer` |
| `device_id` | string   | JID of the device that received this event (e.g., `628123456789@s.whatsapp.net`)                                    |
| `payload`   | object   | Event-specific payload data                                                                                         |

### Common Payload Fields

Fields commonly found inside the `payload` object:

| **Field**   | **Type** | **Description**                                                               |
|-------------|----------|-------------------------------------------------------------------------------|
| `id`        | string   | Message ID                                                                    |
| `chat_id`   | string   | Chat JID (e.g., `628987654321@s.whatsapp.net` or `120363...@g.us` for groups) |
| `from`      | string   | Full JID of the sender (e.g., `628123456789@s.whatsapp.net`)                  |
| `from_lid`  | string   | LID (Linked ID) of the sender if available                                    |
| `from_name` | string   | Display name (pushname) of the sender                                         |
| `timestamp` | string   | RFC3339 formatted timestamp (e.g., `2023-10-15T10:30:00Z`)                    |
| `is_from_me` | boolean | Whether the message was sent by the current user                              |

## Message Events

### Text Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A1",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_lid": "251556368777322@lid",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T10:30:00Z",
    "is_from_me": false,
    "body": "Hello, how are you?"
  }
}
```

### Reply Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A2",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T10:35:00Z",
    "is_from_me": false,
    "body": "I'm doing great, thanks!",
    "replied_to_id": "3EB0C127D7BACC83D6A1",
    "quoted_body": "Hello, how are you?"
  }
}
```

### Reaction Message

```json
{
  "event": "message.reaction",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "88760C69D1F35FEB239102699AE9XXXX",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T10:40:00Z",
    "is_from_me": false,
    "reaction": "👍",
    "reacted_message_id": "3EB0C127D7BACC83D6A1"
  }
}
```

## Receipt Events

Receipt events are triggered when messages receive acknowledgments such as delivery confirmations and read receipts.
These events use the `message.ack` event type and provide information about message status changes.

### Message Delivered

Triggered when a message is successfully delivered to the recipient's device.

```json
{
  "event": "message.ack",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2025-07-18T22:44:20Z",
  "payload": {
    "ids": [
      "3EB00106E8BE0F407E88EC"
    ],
    "chat_id": "120363402106XXXXX@g.us",
    "from": "6289685XXXXXX@s.whatsapp.net",
    "from_lid": "251556368777322@lid",
    "receipt_type": "delivered",
    "receipt_type_description": "means the message was delivered to the device (but the user might not have noticed)."
  }
}
```

### Message Read

Triggered when a message is read by the recipient (they opened the chat and saw the message).

```json
{
  "event": "message.ack",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2025-07-18T22:44:44Z",
  "payload": {
    "ids": [
      "3EB00106E8BE0F407E88EC"
    ],
    "chat_id": "120363402106XXXXX@g.us",
    "from": "6289685XXXXXX@s.whatsapp.net",
    "receipt_type": "read",
    "receipt_type_description": "the user opened the chat and saw the message."
  }
}
```

### Receipt Event Fields

| **Field**                          | **Type** | **Description**                                           |
|------------------------------------|----------|-----------------------------------------------------------|
| `event`                            | string   | Always `"message.ack"` for receipt events                 |
| `device_id`                        | string   | JID of the device that received this event                |
| `timestamp`                        | string   | RFC3339 formatted timestamp when the receipt was received |
| `payload.ids`                      | array    | Array of message IDs that received the acknowledgment     |
| `payload.chat_id`                  | string   | Chat identifier (group or individual chat)                |
| `payload.from`                     | string   | JID of the user who triggered the receipt                 |
| `payload.from_lid`                 | string   | LID of the user (if available)                            |
| `payload.receipt_type`             | string   | Type of receipt: `"delivered"`, `"read"`, etc.            |
| `payload.receipt_type_description` | string   | Human-readable description of the receipt type            |

## Group Events

Group events are triggered when group metadata changes, including member join/leave events, admin promotions/demotions,
and group settings updates. These events use the `group.participants` event type and provide comprehensive information
about group changes.

### Group Member Join

Triggered when users join or are added to a group.

```json
{
  "event": "group.participants",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2025-07-28T10:30:00Z",
  "payload": {
    "chat_id": "120363402106XXXXX@g.us",
    "type": "join",
    "jids": [
      "6289685XXXXXX@s.whatsapp.net",
      "6289686YYYYYY@s.whatsapp.net"
    ]
  }
}
```

### Group Member Leave

Triggered when users leave or are removed from a group.

```json
{
  "event": "group.participants",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2025-07-28T10:32:00Z",
  "payload": {
    "chat_id": "120363402106XXXXX@g.us",
    "type": "leave",
    "jids": [
      "6289687ZZZZZZ@s.whatsapp.net"
    ]
  }
}
```

### Group Member Promotion

Triggered when users are promoted to admin.

```json
{
  "event": "group.participants",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2025-07-28T10:33:00Z",
  "payload": {
    "chat_id": "120363402106XXXXX@g.us",
    "type": "promote",
    "jids": [
      "6289688AAAAAA@s.whatsapp.net"
    ]
  }
}
```

### Group Member Demotion

Triggered when users are demoted from admin.

```json
{
  "event": "group.participants",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2025-07-28T10:34:00Z",
  "payload": {
    "chat_id": "120363402106XXXXX@g.us",
    "type": "demote",
    "jids": [
      "6289689BBBBBB@s.whatsapp.net"
    ]
  }
}
```

### Group Event Fields

| **Field**         | **Type** | **Description**                                              |
|-------------------|----------|--------------------------------------------------------------|
| `event`           | string   | Always `"group.participants"` for group events               |
| `device_id`       | string   | JID of the device that received this event                   |
| `timestamp`       | string   | RFC3339 formatted timestamp when the group event occurred    |
| `payload.chat_id` | string   | Group identifier (e.g., `"120363402106XXXXX@g.us"`)          |
| `payload.type`    | string   | Action type: `"join"`, `"leave"`, `"promote"`, or `"demote"` |
| `payload.jids`    | array    | Array of user JIDs affected by this action                   |

## Newsletter Events

Newsletter events are triggered when you interact with WhatsApp Channels (newsletters). These include subscribing,
unsubscribing, receiving new messages, and mute setting changes.

### Newsletter Joined

Triggered when you subscribe to a newsletter/channel.

```json
{
  "event": "newsletter.joined",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2026-01-18T12:00:00Z",
  "payload": {
    "newsletter_id": "120363123456789@newsletter",
    "name": "Tech News Daily",
    "description": "Latest tech updates and news"
  }
}
```

### Newsletter Left

Triggered when you unsubscribe from a newsletter/channel.

```json
{
  "event": "newsletter.left",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2026-01-18T12:00:00Z",
  "payload": {
    "newsletter_id": "120363123456789@newsletter",
    "role": "subscriber"
  }
}
```

### Newsletter Message

Triggered when new messages are posted in a newsletter you're subscribed to.

```json
{
  "event": "newsletter.message",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2026-01-18T12:00:00Z",
  "payload": {
    "newsletter_id": "120363123456789@newsletter",
    "messages": [
      {
        "server_id": 123,
        "message_id": "ABC123DEF456",
        "type": "text",
        "timestamp": "2026-01-18T12:00:00Z",
        "views_count": 1500,
        "reaction_counts": {"👍": 50, "❤️": 25}
      }
    ]
  }
}
```

### Newsletter Mute

Triggered when you mute or unmute a newsletter.

```json
{
  "event": "newsletter.mute",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2026-01-18T12:00:00Z",
  "payload": {
    "newsletter_id": "120363123456789@newsletter",
    "mute": "on"
  }
}
```

### Newsletter Event Fields

| **Field**                      | **Type** | **Description**                                         |
|--------------------------------|----------|---------------------------------------------------------|
| `event`                        | string   | Event type (see table above)                            |
| `device_id`                    | string   | JID of the device that received this event              |
| `timestamp`                    | string   | RFC3339 formatted timestamp                             |
| `payload.newsletter_id`        | string   | Newsletter identifier (e.g., `120363...@newsletter`)    |
| `payload.name`                 | string   | Newsletter name (only in `newsletter.joined`)           |
| `payload.description`          | string   | Newsletter description (only in `newsletter.joined`)    |
| `payload.role`                 | string   | Your role in the newsletter (only in `newsletter.left`) |
| `payload.mute`                 | string   | Mute state: `"on"` or `"off"` (only in `newsletter.mute`)|
| `payload.messages`             | array    | Array of messages (only in `newsletter.message`)        |
| `payload.messages[].server_id` | number   | Server-assigned message ID                              |
| `payload.messages[].message_id`| string   | Message identifier                                      |
| `payload.messages[].type`      | string   | Message type (e.g., `"text"`, `"image"`)                |
| `payload.messages[].timestamp` | string   | Message timestamp                                       |
| `payload.messages[].views_count`| number  | Number of views (if available)                          |
| `payload.messages[].reaction_counts`| object | Reaction emoji counts (if available)                 |

## Call Events

Call events are triggered when you receive an incoming WhatsApp call. You can optionally auto-reject calls using the
`WHATSAPP_AUTO_REJECT_CALL` environment variable or `--auto-reject-call` CLI flag.

### Call Offer

Triggered when an incoming call is received.

```json
{
  "event": "call.offer",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2026-02-05T12:00:00Z",
  "payload": {
    "call_id": "ABC123DEF456",
    "from": "628987654321@s.whatsapp.net",
    "auto_rejected": false,
    "remote_platform": "android",
    "remote_version": "2.24.1.5"
  }
}
```

### Call Offer with Auto-Reject Enabled

When `WHATSAPP_AUTO_REJECT_CALL=true`, calls are automatically rejected and the webhook includes this status:

```json
{
  "event": "call.offer",
  "device_id": "628123456789@s.whatsapp.net",
  "timestamp": "2026-02-05T12:00:00Z",
  "payload": {
    "call_id": "ABC123DEF456",
    "from": "628987654321@s.whatsapp.net",
    "auto_rejected": true,
    "remote_platform": "android",
    "remote_version": "2.24.1.5",
    "group_jid": "120363402106XXXXX@g.us"
  }
}
```

### Call Event Fields

| **Field**                 | **Type** | **Description**                                            |
|---------------------------|----------|------------------------------------------------------------|
| `event`                   | string   | Always `"call.offer"` for call events                      |
| `device_id`               | string   | JID of the device that received this event                 |
| `timestamp`               | string   | RFC3339 formatted timestamp when the call was received     |
| `payload.call_id`         | string   | Unique identifier for the call                             |
| `payload.from`            | string   | JID of the caller                                          |
| `payload.auto_rejected`   | boolean  | Whether the call was auto-rejected                         |
| `payload.remote_platform` | string   | Platform of the caller (e.g., `"android"`, `"ios"`)        |
| `payload.remote_version`  | string   | WhatsApp version of the caller                             |
| `payload.group_jid`       | string   | Group JID if this is a group call (optional)               |

### Configuration

**Environment Variable:**

```bash
# Auto-reject all incoming calls
WHATSAPP_AUTO_REJECT_CALL=true
```

**CLI Flag:**

```bash
# Auto-reject all incoming calls
./whatsapp rest --auto-reject-call=true
```

## Media Messages

### Image Message

When `WHATSAPP_AUTO_DOWNLOAD_MEDIA` is enabled, media is downloaded and `image` contains the file path.
When disabled, `image` contains an object with the URL.

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A3",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:05:51Z",
    "image": "statics/media/1752404751-ad9e37ac-c658-4fe5-8d25-ba4a3f4d58fd.jpeg"
  }
}
```

With auto-download disabled:

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A3",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:05:51Z",
    "image": {
      "url": "https://mmg.whatsapp.net/...",
      "caption": "Check this out!"
    }
  }
}
```

### Video Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A4",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:07:24Z",
    "video": "statics/media/1752404845-b9393cd1-8546-4df9-8a60-ee3276036aba.mp4"
  }
}
```

### Audio Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A5",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T10:55:00Z",
    "audio": "statics/media/1752404905-b9393cd1-8546-4df9-8a60-ee3276036aba.ogg"
  }
}
```

### Document Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A6",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T11:00:00Z",
    "document": "statics/media/1752404965-document.pdf"
  }
}
```

With auto-download disabled:

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A6",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T11:00:00Z",
    "document": {
      "url": "https://mmg.whatsapp.net/...",
      "filename": "report.pdf"
    }
  }
}
```

### Sticker Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "446AC2BAF2061B53E24CA526DBDFBD4E",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:09:45Z",
    "sticker": "statics/media/1752404986-ff2464a6-c54c-4e6c-afde-c4c925ce3573.webp"
  }
}
```

### Video Note Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A7",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T11:02:00Z",
    "video_note": "statics/media/1752404990-videonote.mp4"
  }
}
```

## Special Message Types

### Contact Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "56B3DFF4994284634E7AAFEEF6F1A0A2",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:10:19Z",
    "contact": {
      "displayName": "3Care",
      "vcard": "BEGIN:VCARD\nVERSION:3.0\nN:;3Care;;;\nFN:3Care\nTEL;type=Mobile:+62 132\nEND:VCARD"
    }
  }
}
```

### Location Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6A9",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T11:15:00Z",
    "location": {
      "degreesLatitude": -6.2088,
      "degreesLongitude": 106.8456,
      "name": "Jakarta, Indonesia",
      "address": "Central Jakarta, DKI Jakarta, Indonesia"
    }
  }
}
```

### Live Location Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "94D13237B4D7F33EE4A63228BBD79EC0",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:11:22Z",
    "live_location": {
      "degreesLatitude": -7.8050297,
      "degreesLongitude": 110.4549165
    }
  }
}
```

## Protocol Messages

### Message Deleted

Triggered when a message is deleted for the current user (DeleteForMe event).

```json
{
  "event": "message.deleted",
  "device_id": "628123456789@s.whatsapp.net",
  "payload": {
    "deleted_message_id": "3EB0C127D7BACC83D6A1",
    "timestamp": "2025-07-13T11:12:00Z",
    "from": "628987654321@s.whatsapp.net",
    "chat_id": "628987654321@s.whatsapp.net",
    "original_content": "Hello, how are you?",
    "original_sender": "628987654321@s.whatsapp.net",
    "original_timestamp": "2025-07-13T10:30:00Z",
    "was_from_me": false
  }
}
```

**Fields:**

| **Field**                      | **Type** | **Description**                                       |
|--------------------------------|----------|-------------------------------------------------------|
| `payload.deleted_message_id`   | string   | ID of the deleted message                             |
| `payload.timestamp`            | string   | RFC3339 timestamp when the delete event occurred      |
| `payload.from`                 | string   | JID of the user who deleted the message               |
| `payload.chat_id`              | string   | Chat identifier where the message was deleted         |
| `payload.original_content`     | string   | Original message content (if available from storage)  |
| `payload.original_sender`      | string   | Original sender of the deleted message                |
| `payload.original_timestamp`   | string   | Original message timestamp                            |
| `payload.was_from_me`          | boolean  | Whether the deleted message was sent by current user  |
| `payload.original_media_type`  | string   | Media type if the message contained media (optional)  |
| `payload.original_filename`    | string   | Filename if the message contained media (optional)    |

### Message Revoked

```json
{
  "event": "message.revoked",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "F4062F2BBCB19B7432195AD7080DA4E2",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:13:30Z",
    "is_from_me": true,
    "revoked_message_id": "94D13237B4D7F33EE4A63228BBD79EC0",
    "revoked_from_me": true,
    "revoked_chat": "628987654321@s.whatsapp.net"
  }
}
```

### Message Edited

When a message is edited, the webhook includes the original message ID to track which message was modified.

```json
{
  "event": "message.edited",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "D6271D8223A05B4DA6AE9FE3CD632543",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2025-07-13T11:14:19Z",
    "is_from_me": false,
    "original_message_id": "94D13237B4D7F33EE4A63228BBD79EC0",
    "body": "Updated message text"
  }
}
```

**Fields:**

- `original_message_id`: The ID of the message that was edited (use this to update the correct message in your database)
- `body`: The new text content after editing
- `id`: The ID of the edit event itself (different from the original message ID)

## Special Flags

### View Once Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6B2",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T11:40:00Z",
    "image": "statics/media/1752405060-image.jpeg",
    "view_once": true
  }
}
```

### Forwarded Message

```json
{
  "event": "message",
  "device_id": "628987654321@s.whatsapp.net",
  "payload": {
    "id": "3EB0C127D7BACC83D6B3",
    "chat_id": "628987654321@s.whatsapp.net",
    "from": "628123456789@s.whatsapp.net",
    "from_name": "John Doe",
    "timestamp": "2023-10-15T11:45:00Z",
    "body": "This is a forwarded message",
    "forwarded": true
  }
}
```

## Integration Guide

### Setting Up Webhook Endpoint

1. **Configure webhook URL(s)**:

   ```bash
   ./whatsapp rest --webhook="https://yourapp.com/webhook"
   ```

2. **Set webhook secret**:

   ```bash
   ./whatsapp rest --webhook-secret="your-secret-key"
   ```

3. **Multiple webhooks**:

   ```bash
   ./whatsapp rest --webhook="https://app1.com/webhook,https://app2.com/webhook"
   ```

### Webhook Endpoint Implementation (Express.js)

```javascript
const express = require('express');
const crypto = require('crypto');
const app = express();

app.use(express.raw({type: 'application/json'}));

app.post('/webhook', (req, res) => {
    const signature = req.headers['x-hub-signature-256'];
    const payload = req.body;
    const secret = 'your-secret-key';

    // Verify signature
    if (!verifyWebhookSignature(payload, signature, secret)) {
        return res.status(401).send('Unauthorized');
    }

    // Parse and process webhook data
    const data = JSON.parse(payload);
    console.log('Received webhook:', data);

    // Handle different event types based on data.event
    switch (data.event) {
        case 'message':
            console.log('New message:', {
                id: data.payload.id,
                from: data.payload.from,
                body: data.payload.body,
                chat_id: data.payload.chat_id
            });
            break;

        case 'message.reaction':
            console.log('Reaction:', {
                reaction: data.payload.reaction,
                reacted_message_id: data.payload.reacted_message_id
            });
            break;

        case 'message.revoked':
            console.log('Message revoked:', data.payload.revoked_message_id);
            break;

        case 'message.edited':
            console.log('Message edited:', {
                original_id: data.payload.original_message_id,
                new_body: data.payload.body
            });
            break;

        case 'message.ack':
            console.log(`Message ${data.payload.receipt_type}:`, {
                chat_id: data.payload.chat_id,
                message_ids: data.payload.ids,
                description: data.payload.receipt_type_description
            });
            break;

        case 'group.participants':
            console.log(`Group ${data.payload.type} event:`, {
                chat_id: data.payload.chat_id,
                affected_users: data.payload.jids
            });
            break;

        case 'newsletter.joined':
            console.log('Joined newsletter:', {
                newsletter_id: data.payload.newsletter_id,
                name: data.payload.name
            });
            break;

        case 'newsletter.left':
            console.log('Left newsletter:', {
                newsletter_id: data.payload.newsletter_id,
                role: data.payload.role
            });
            break;

        case 'newsletter.message':
            console.log('Newsletter message:', {
                newsletter_id: data.payload.newsletter_id,
                messages: data.payload.messages
            });
            break;

        case 'newsletter.mute':
            console.log('Newsletter mute changed:', {
                newsletter_id: data.payload.newsletter_id,
                mute: data.payload.mute
            });
            break;

        case 'call.offer':
            console.log('Incoming call:', {
                call_id: data.payload.call_id,
                from: data.payload.from,
                auto_rejected: data.payload.auto_rejected,
                platform: data.payload.remote_platform
            });
            break;
    }

    res.status(200).send('OK');
});

function verifyWebhookSignature(payload, signature, secret) {
    const expectedSignature = crypto
        .createHmac('sha256', secret)
        .update(payload, 'utf8')
        .digest('hex');

    const receivedSignature = signature.replace('sha256=', '');
    return crypto.timingSafeEqual(
        Buffer.from(expectedSignature, 'hex'),
        Buffer.from(receivedSignature, 'hex')
    );
}

app.listen(3001, () => {
    console.log('Webhook server listening on port 3001');
});
```

### Error Handling

The webhook system includes retry logic with exponential backoff:

- **Timeout**: 10 seconds per request
- **Max Attempts**: 5 retries
- **Backoff**: Exponential (1s, 2s, 4s, 8s, 16s)

Ensure your webhook endpoint:

- Responds within 10 seconds
- Returns HTTP 2xx status for successful processing
- Handles duplicate events gracefully
- Validates signatures for security

## Configuration

### Environment Variables

```bash
# Single webhook URL
WHATSAPP_WEBHOOK=https://yourapp.com/webhook

# Multiple webhook URLs (comma-separated)
WHATSAPP_WEBHOOK=https://app1.com/webhook,https://app2.com/webhook

# Webhook secret for HMAC verification
WHATSAPP_WEBHOOK_SECRET=your-super-secret-key
```

### Command Line Flags

```bash
# Single webhook
./whatsapp rest --webhook="https://yourapp.com/webhook"

# Multiple webhooks
./whatsapp rest --webhook="https://app1.com/webhook,https://app2.com/webhook"

# Custom secret
./whatsapp rest --webhook-secret="your-secret-key"
```

## Best Practices

1. **Always verify signatures** to ensure webhook authenticity
2. **Handle duplicates** - the same event might be sent multiple times
3. **Process quickly** - respond within 10 seconds to avoid timeouts
4. **Log errors** for debugging webhook integration issues
5. **Use HTTPS** for webhook URLs to ensure secure transmission
6. **Store media files** locally if you need to process them later
7. **Implement proper error handling** for different event types

## Troubleshooting

### Common Issues

1. **Webhook not receiving events**:
    - Check webhook URL is accessible from the internet
    - Verify webhook configuration
    - Check firewall and network settings

2. **Signature verification fails**:
    - Ensure webhook secret matches configuration
    - Use raw request body for signature calculation
    - Check HMAC implementation

3. **Timeouts**:
    - Optimize webhook processing speed
    - Implement asynchronous processing
    - Return response quickly, process in background

4. **Missing media files**:
    - Check media storage path configuration
    - Ensure sufficient disk space
    - Verify file permissions

### Debug Logging

Enable debug mode to see webhook logs:

```bash
./whatsapp rest --debug=true --webhook="https://yourapp.com/webhook"
```

This will show detailed logs of webhook delivery attempts and errors.
