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

| Variable | Required | Default | Function |
|----------|----------|---------|----------|
| `CHATWOOT_ENABLED` | Yes | `false` | Master switch. When `true`, the app registers the public `POST /chatwoot/webhook` route before basic auth, starts the Chatwoot live-forward retry worker, registers the authenticated `/chatwoot/sync` routes, and forwards WhatsApp messages to Chatwoot. If `false`, Chatwoot webhooks fall through to normal auth/routing and agent replies will not work. |
| `CHATWOOT_URL` | Yes | - | Base URL of the Chatwoot instance, without a trailing slash, for REST API calls. Example: `https://chatwoot.example.com`. |
| `CHATWOOT_API_TOKEN` | Yes | - | Chatwoot user API access token. The token is sent as `api_access_token` to create contacts, conversations, messages, inboxes, and attachments. Direct DB import also uses this token to resolve the Chatwoot user that should own imported outgoing messages. |
| `CHATWOOT_ACCOUNT_ID` | Yes | - | Numeric Chatwoot account ID. This is the number in `/app/accounts/<id>/...`, not an inbox identifier, hmac token, or random API-channel string. |
| `CHATWOOT_INBOX_ID` | Yes\* | - | Numeric Chatwoot inbox ID for the API-channel inbox. Optional only when `CHATWOOT_AUTO_CREATE=true`, because startup can resolve it automatically. If set, it always wins and auto-provisioning is skipped. |
| `CHATWOOT_DEVICE_ID` | Multi-device | - | WhatsApp device that should send Chatwoot agent replies. Use either the local device ID or the device JID. Required when more than one device is registered. If empty and exactly one device exists, that device is used automatically. |
| `CHATWOOT_IMPORT_MESSAGES` | No | `false` | Auto-start history sync when a WhatsApp device connects. Live message forwarding works even when this is `false`. |
| `CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES` | No | `3` | Number of days of stored WhatsApp history to import during auto-sync or when a manual sync omits `days_limit`. |
| `CHATWOOT_IMPORT_DB_URI` | No | - | PostgreSQL connection URI for direct Chatwoot DB import. When set, history sync writes directly to Chatwoot tables to preserve original timestamps. When empty, history sync uses Chatwoot REST. Live forwarding always uses REST. |
| `CHATWOOT_IMPORT_PLACEHOLDER_MEDIA_MESSAGE` | No | `true` | Direct-DB import behavior for media that cannot be downloaded. `true` inserts a text placeholder such as `[image]`; `false` leaves the message body empty. |
| `CHATWOOT_IMPORT_MEDIA_WITH_REST` | No | `false` | Direct-DB import media mode. `true` sends downloadable media rows through Chatwoot REST so ActiveStorage attachments are created; non-media rows still use direct DB. Media uploaded this way gets Chatwoot's upload timestamp. |
| `CHATWOOT_AUTO_CREATE` | No | `false` | Startup provisioning mode. When `true`, the app lists inboxes, reuses an API-channel inbox named `CHATWOOT_INBOX_NAME`, or creates one using `CHATWOOT_WEBHOOK_URL`; then it fills `CHATWOOT_INBOX_ID` in memory. |
| `CHATWOOT_INBOX_NAME` | No | `WhatsApp` | Inbox name used only by auto-provisioning. The app reuses a same-name `Channel::Api` inbox, but ignores same-name non-API inboxes because they cannot receive API-channel reply webhooks. |
| `CHATWOOT_WEBHOOK_URL` | Reply webhook | - | Public URL Chatwoot should call for agent replies: `https://your-gowa.example.com/chatwoot/webhook`. Used when creating an API inbox automatically, and useful as the value to place in an existing API channel's `webhook_url`. Keep the secret out of this URL when Chatwoot signs its webhooks; see [Webhook Secret](#webhook-secret). |
| `CHATWOOT_WEBHOOK_SECRET` | Recommended | - | Secret that authenticates incoming Chatwoot webhook calls. An empty effective secret rejects the call. Set `CHATWOOT_WEBHOOK_ALLOW_UNSIGNED=true` only on a trusted network to restore unsigned delivery. When a secret is set, the request must carry Chatwoot's own signature (`X-Chatwoot-Signature` + `X-Chatwoot-Timestamp`, recommended) or present the secret through `X-Chatwoot-Webhook-Secret`, `X-Gowa-Chatwoot-Secret`, `?secret=...`, or an `X-Hub-Signature-256` HMAC signature. |
| `CHATWOOT_WEBHOOK_SIGNATURE_TOLERANCE` | No | `5m` | Maximum age (and clock difference) of the `X-Chatwoot-Timestamp` accepted with Chatwoot's signature. Older or further-ahead requests are rejected, which limits replay of a captured request. |
| `CHATWOOT_REOPEN_CONVERSATION` | No | `true` | When a contact has a resolved conversation and messages again, reopen/reuse that conversation instead of starting a new thread. Keeps REST and direct-DB behavior aligned. |
| `CHATWOOT_CONVERSATION_PENDING` | No | `false` | Create new conversations with status `pending` instead of `open`, so they land in the unassigned queue. Applies to REST and direct-DB paths. |
| `CHATWOOT_IGNORE_JIDS` | No | - | Comma-separated exact JIDs or wildcards to never mirror to Chatwoot. Supports exact values plus `@g.us`, `@s.whatsapp.net`, and `@lid`. System JIDs such as `status@broadcast` are already ignored. |
| `CHATWOOT_SIGN_MSG` | No | `false` | Prefix Chatwoot agent replies sent to WhatsApp with the agent name. Example: `*Jane*` plus the delimiter and message body. |
| `CHATWOOT_SIGN_DELIMITER` | No | `\n\n` | Delimiter inserted between the agent signature and reply body. Literal `\n` sequences are expanded to real newlines. |
| `CHATWOOT_FORWARD_EDITS` | No | `true` | Mirror WhatsApp message edits into Chatwoot as private/threaded notes attached to the original message. |
| `CHATWOOT_FORWARD_DELETES` | No | `true` | Mirror WhatsApp delete-for-everyone events into Chatwoot as private/threaded notes attached to the original message. |
| `CHATWOOT_MESSAGE_READ` | No | `false` | Evolution-compatible read sync. Updates Chatwoot last-seen from WhatsApp receipts and marks WhatsApp messages read after agent replies when durable message links exist. |
| `CHATWOOT_MESSAGE_DELETE` | No | `false` | Evolution-compatible delete sync. Deletes/revokes the linked message on the opposite side when durable message links exist. Inbound customer messages are not revoked from WhatsApp because this device did not send them. |

### Configuration Examples

**Minimal environment file (.env):**
```bash
CHATWOOT_ENABLED=true
CHATWOOT_URL=https://app.chatwoot.com
CHATWOOT_API_TOKEN=your_api_token_here
CHATWOOT_ACCOUNT_ID=12345
CHATWOOT_INBOX_ID=67890
CHATWOOT_DEVICE_ID=my-whatsapp-device
# The API channel's webhook secret from Chatwoot; Chatwoot signs every webhook with it.
CHATWOOT_WEBHOOK_SECRET=secret-shown-by-chatwoot
CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook

# Optional: History sync settings
CHATWOOT_IMPORT_MESSAGES=true
CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7
```

**Self-hosted Chatwoot example with direct DB history import:**
```bash
CHATWOOT_ENABLED=true
CHATWOOT_URL=https://chatwoot.example.com
CHATWOOT_API_TOKEN=your_chatwoot_user_api_token

# Numeric IDs from Chatwoot, not API-channel identifier/hmac strings.
CHATWOOT_ACCOUNT_ID=1
CHATWOOT_INBOX_ID=1

# Local GOWA device id or device JID.
CHATWOOT_DEVICE_ID=628123456789@s.whatsapp.net

# Agent reply callback from Chatwoot to GOWA, verified by Chatwoot's signature.
CHATWOOT_WEBHOOK_SECRET=secret-shown-by-chatwoot
CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook

# Optional history import.
CHATWOOT_IMPORT_MESSAGES=true
CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7
CHATWOOT_IMPORT_DB_URI=postgresql://chatwoot:password@chatwoot-db:5432/chatwoot_production?sslmode=disable
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
      - CHATWOOT_WEBHOOK_SECRET=secret-shown-by-chatwoot
      - CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook
    command: rest
```

### Setup Checklist

1. In Chatwoot, create or identify an **API channel** inbox.
2. Copy the numeric `CHATWOOT_ACCOUNT_ID` from the Chatwoot account URL.
3. Copy the numeric `CHATWOOT_INBOX_ID` from the inbox URL/settings.
4. Copy a Chatwoot user API token from profile settings.
5. Choose the WhatsApp device for agent replies and set `CHATWOOT_DEVICE_ID` when more than one device exists.
6. Make GOWA publicly reachable at `https://your-gowa.example.com/chatwoot/webhook`.
7. Set the API channel inbox webhook URL to that public GOWA webhook URL.
8. Set `CHATWOOT_WEBHOOK_SECRET` to the API channel's webhook secret so GOWA verifies Chatwoot's signature (see [Webhook Secret](#webhook-secret)). On a Chatwoot version that does not sign webhooks, send the secret through a supported header or, as a last resort, the webhook URL query string.
9. Restart GOWA after changing `.env`; the webhook and sync routes are registered at process startup.
10. Verify with `/chatwoot/sync/status` and a webhook curl before going live.

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

### Step 4: Configure the API Channel Webhook

For agent replies from Chatwoot to WhatsApp, configure the webhook URL on the **API channel inbox** itself:

1. Navigate to **Settings** > **Inboxes**.
2. Open the API-channel inbox used for WhatsApp.
3. Set the API channel webhook URL to:
   ```text
   https://your-gowa.example.com/chatwoot/webhook
   ```
4. Save the inbox.
5. Copy the inbox's webhook secret into `CHATWOOT_WEBHOOK_SECRET` so GOWA can verify that each call really comes from Chatwoot. If your Chatwoot version shows no webhook secret, see [Webhook Secret](#webhook-secret) for the alternatives.

> **Important:** The webhook URL must be publicly accessible. If GOWA runs locally, use a tunneling service such as ngrok and set the full tunnel URL, including `/chatwoot/webhook`.

Global Chatwoot webhooks under **Settings > Integrations > Webhooks** are useful for generic event delivery, but they are not the primary API-channel reply path documented here. The API-channel inbox `webhook_url` is what Chatwoot uses to send agent replies for this integration.

## Auto-Provisioning

Instead of creating the inbox by hand (Step 1) and copying its ID, you can let the integration provision it for you in one step.

Set:

```bash
CHATWOOT_ENABLED=true
CHATWOOT_URL=https://app.chatwoot.com
CHATWOOT_API_TOKEN=your_api_token
CHATWOOT_ACCOUNT_ID=12345
CHATWOOT_AUTO_CREATE=true
CHATWOOT_INBOX_NAME=WhatsApp                       # optional, defaults to "WhatsApp"
CHATWOOT_WEBHOOK_URL=https://your-api.com/chatwoot/webhook
CHATWOOT_WEBHOOK_SECRET=                           # recommended; see the note below
```

On startup the app will:

1. Look for an inbox named `CHATWOOT_INBOX_NAME` in the account and **reuse it** if present (so restarts never create duplicates).
2. Otherwise **create** a new API-channel inbox with that name, wired to `CHATWOOT_WEBHOOK_URL`.
3. Resolve `CHATWOOT_INBOX_ID` automatically for the rest of the integration.

Notes:

- If `CHATWOOT_INBOX_ID` is already set, it takes precedence and auto-create is skipped (your explicit value wins).
- If `CHATWOOT_WEBHOOK_URL` is empty, the inbox is still created but **agent replies won't reach WhatsApp** until you set the webhook — a warning is logged.
- Provisioning failures are non-fatal; the app keeps running and you can fall back to manual setup.
- Chatwoot generates the webhook secret of a new inbox, so it cannot be known before the inbox exists. After the first start, copy the secret from the created inbox into `CHATWOOT_WEBHOOK_SECRET` and restart. Until then webhook calls are rejected. See [Webhook Secret](#webhook-secret).

## Webhook Secret

`CHATWOOT_WEBHOOK_SECRET` protects GOWA's public `/chatwoot/webhook` endpoint, which sits outside basic auth so Chatwoot can reach it. When the effective secret is empty, GOWA rejects the call. Set `CHATWOOT_WEBHOOK_ALLOW_UNSIGNED=true` only when the endpoint is reachable solely on a trusted network. When a secret is set, a request is accepted only if it proves knowledge of the secret in one of these ways:

| Method | What Chatwoot (or a proxy) sends | Notes |
|--------|----------------------------------|-------|
| **Chatwoot signature** (recommended) | `X-Chatwoot-Timestamp: <unix seconds>` and `X-Chatwoot-Signature: sha256=<hex HMAC-SHA256 of "<timestamp>.<raw body>">` | Sent by Chatwoot itself (v4.x) when the API channel has a webhook secret. The secret never travels with the request, the body cannot be altered, and a captured request stops working after `CHATWOOT_WEBHOOK_SIGNATURE_TOLERANCE` (5 minutes by default). |
| HMAC of the body | `X-Hub-Signature-256: sha256=<hex HMAC-SHA256 of the raw body>` | For an intermediate system that signs the body. No timestamp, so no replay protection. |
| Header | `X-Chatwoot-Webhook-Secret: <secret>` or `X-Gowa-Chatwoot-Secret: <secret>` | For a reverse proxy that adds a header. The secret travels with every request. |
| Query string | `/chatwoot/webhook?secret=<secret>` | Works with any Chatwoot version, but **the secret is part of the URL** — see the warning below. |

### Recommended: Chatwoot's signed webhooks

1. In Chatwoot, open the API-channel inbox and copy its webhook secret. Chatwoot generates one per API channel on versions that sign webhooks.
2. Set it in GOWA and keep the webhook URL free of secrets:

   ```bash
   CHATWOOT_WEBHOOK_SECRET=secret-shown-by-chatwoot
   CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook
   ```
3. Set the inbox's webhook URL to the plain `https://your-gowa.example.com/chatwoot/webhook`.

GOWA recomputes the HMAC over the timestamp and the raw request body, compares it in constant time, and rejects timestamps more than the tolerance away from its own clock in either direction. Keep both hosts' clocks synchronized (NTP); a `401` with `timestamp outside the tolerance window` in the GOWA log means they have drifted apart.

With [per-device configs](#per-device--multi-inbox-routing), each inbox is a different API channel with its own secret. Store it on the device config as `webhook_secret`; it then protects that device instead of the global secret.

### Which secret protects a delivery

The secret that applies is the secret of the device the webhook is routed to, on both endpoints:

- The device has its own `webhook_secret`: the request must prove that secret. The global secret is not accepted for it, and neither is another device's secret.
- The device has no `webhook_secret`: the global `CHATWOOT_WEBHOOK_SECRET` applies; when that is empty too, the call is rejected unless `CHATWOOT_WEBHOOK_ALLOW_UNSIGNED=true`.
- No device resolves (unknown inbox, or an event GOWA ignores such as `conversation_updated`): the global secret applies. Nothing is sent for such a request either way.

This holds for the shared `/chatwoot/webhook` endpoint as well, which picks the device from the payload: nothing is sent, revoked, marked as read, or written before the request has been verified against the routed device's secret. A request signed with a device's secret is accepted there for that device's inbox, so several inboxes with different secrets can share the one URL. Events GOWA ignores are still checked against the global secret only, so prefer the per-device URL when devices have their own secrets and a global secret is also set.

When `CHATWOOT_WEBHOOK_SECRET` is empty and at least one device has its own secret, an agent reply that resolves to no device is answered `401`, the same as a wrong secret, so the response does not show which account/inbox pairs are mapped to a protected device. Without any device secret the call is also rejected, unless `CHATWOOT_WEBHOOK_ALLOW_UNSIGNED=true`, in which case it is acknowledged with `200` and dropped.

### Replayed requests

What a captured, valid request can do depends on how it was authenticated:

- **Chatwoot signature:** GOWA remembers the timestamp and signature of every verified request it accepted (answered `2xx`) for the tolerance window. A second copy of such a request is acknowledged with `200` and not acted on, and after the window the timestamp check rejects it. A request that was not accepted, for example one answered `503` while storage was unavailable, is not remembered, so the identical request sent again is handled. A copy that arrives while the first is still being handled waits for it: it is acknowledged as a repeat when the first is accepted, handled in its place when the first fails, and answered `503` without being acted on if the first is still not finished after 30 seconds.

  This memory is held in the GOWA process: it is lost on a restart, so a captured request can be delivered once more if GOWA restarts while the request is still inside the window, and it is not shared between several GOWA instances behind one URL. It holds up to 65,536 accepted requests. Beyond that GOWA keeps accepting webhooks and forgets the oldest request of whichever secret has the most remembered, so a forgotten request could be replayed inside its window. Only verified requests are remembered, so reaching the limit takes a valid secret, and a flood signed with one device's secret pushes out that device's own entries: each other secret keeps at least its newest 65,536 / (number of secrets in use) requests.
- **`X-Hub-Signature-256`, secret header, `?secret=`:** nothing in the request identifies one delivery, so a captured request can be sent again for as long as the secret is unchanged. The secret header and query string also hand the secret itself to whoever captures the request.

GOWA does not otherwise deduplicate agent replies: if Chatwoot delivers the same message twice in two separately signed requests, it is sent to WhatsApp twice.

### Query-string secret

> **Warning:** a secret in the URL is written wherever URLs are written: reverse-proxy and load-balancer access logs, Chatwoot's own logs and error reports, monitoring tools, and the inbox settings page that every Chatwoot administrator can open. Treat it as exposed once it has been used, and prefer the signature above.

Use `?secret=` only when your Chatwoot version does not sign webhooks and you cannot put a proxy in front of GOWA that adds a header:

```bash
CHATWOOT_WEBHOOK_SECRET=strong-shared-secret
CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook?secret=strong-shared-secret
```

GOWA logs a one-time warning when a webhook is authenticated this way. To move an existing deployment to signatures: set `CHATWOOT_WEBHOOK_SECRET` to the channel's webhook secret and restart GOWA — Chatwoot's signature is accepted from then on — then remove `?secret=...` from the inbox webhook URL and treat the old secret as exposed.

All methods stay accepted side by side: a request that carries a Chatwoot signature made with a different secret is still accepted if it also presents the configured secret in another way.

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

## Per-Device / Multi-Inbox Routing

The `CHATWOOT_*` env vars above configure a **single** Chatwoot destination shared by all
devices. To route **each WhatsApp device to its own Chatwoot inbox** (and even a different
Chatwoot account/server per device), configure per-device mappings at runtime via the REST API.

These per-device configs are stored in the `chatwoot_device_configs` table and become the single
source of truth. The `CHATWOOT_*` env vars are used as a fallback **only while no per-device
config rows exist** — once you create the first per-device config, every device must have its own
config or its messages are skipped (fail-fast) rather than misrouted to the env inbox.

### REST API (authenticated)

```bash
# List all per-device configs (api_token is masked in responses)
curl http://your-api:3000/chatwoot/configs

# Create/update a device's Chatwoot destination
curl -X PUT http://your-api:3000/devices/<device_id>/chatwoot/config \
  -H 'Content-Type: application/json' \
  -d '{
    "chatwoot_url": "https://app.chatwoot.com",
    "account_id": 12345,
    "inbox_id": 67890,
    "api_token": "device-specific-token",
    "webhook_secret": "secret-of-this-inbox-api-channel",
    "enabled": true
  }'

# Read one device's config (api_token masked)
curl http://your-api:3000/devices/<device_id>/chatwoot/config

# Remove a device's config
curl -X DELETE http://your-api:3000/devices/<device_id>/chatwoot/config
```

The PUT response includes a **`webhook_url`** — set this exact URL as the agent-reply webhook on
that device's Chatwoot inbox (it is `<public-base>/chatwoot/webhook/<device_id>`). This per-device
webhook path lets agent replies route deterministically back to the right device; it is validated
against the device's configured account/inbox, so a webhook for one device cannot be delivered
through another's path.

### Behavior & safety notes

- **api_token** is stored as-is in the local database and is masked (last 4 chars) in all API
  reads. Treat the GOWA database as a secret store.
- **webhook_secret** (optional) is the webhook secret of this inbox's API channel. When set, it
  protects every webhook routed to this device, on `/chatwoot/webhook/<device_id>` and on the
  shared `/chatwoot/webhook`, in place of the global `CHATWOOT_WEBHOOK_SECRET` and with the same
  accepted methods (see [Which secret protects a delivery](#which-secret-protects-a-delivery)).
  It is never returned: reads only report `webhook_secret_set`. Omit the field on update to keep
  the stored value; send an empty string to remove it.
- **`chatwoot_url`** is validated: only `http`/`https`, no embedded credentials, and hosts that
  resolve to private/loopback/link-local/metadata addresses are rejected (SSRF hardening). Set
  `CHATWOOT_ALLOWED_HOSTS` to a comma-separated allowlist to restrict it further.
- Changing a config's **routing identity** (`chatwoot_url`/`account_id`/`inbox_id`) is rejected
  with `409` once that config has linked conversations — delete and recreate to rebind, so
  historical conversations are never silently repointed. Rotating `api_token` or toggling
  `enabled` is always allowed. Deleting a config also deletes its message links: after a
  rebind, stale links would otherwise keep answering reverse lookups for the old destination.
- A config may be created **before the device pairs**: its WhatsApp JID is stamped onto the
  config automatically when the device connects (and re-stamped after a re-pair).
- **Direct-Postgres history import** (`CHATWOOT_IMPORT_DB_URI`) is driven by one global DSN and is
  therefore supported **only** in single-config (env) mode. Per-device configs use the REST import
  path.
- **Known limitation:** if two *separate* Chatwoot servers happen to share the same `account_id`
  **and** `inbox_id`, an agent-*initiated* conversation cannot be disambiguated from the webhook
  payload alone — use the per-device `webhook_url` (above), which is always unambiguous. Replies to
  existing conversations are always routed correctly.

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

- Group messages include the sender name: `John: Hello!`
- Media older than ~2 weeks may be unavailable on WhatsApp servers
- The sync runs in the background and can be monitored via the status endpoint
- Only one sync can run per device at a time
- For correct timestamps, use the [Direct Database Import](#direct-database-import-recommended) (the REST path cannot set `created_at` on Chatwoot messages)

## Direct Database Import (Recommended)

For **self-hosted Chatwoot** deployments, you can enable direct PostgreSQL import for message history sync. This is the recommended approach because it:

- Preserves **original WhatsApp timestamps** on messages (the REST API always stamps server time)
- Preserves **correct group names** instead of JID-based fallbacks
- Runs **significantly faster** than the REST path (one transaction per chat; no HTTP round trips)
- Is **idempotent** — safe to re-run without creating duplicates (rows are matched on `inbox_id + source_id`)

It writes only to tables that have stayed stable across Chatwoot 2.x, 3.x, and 4.x.

### Setup

1. Ensure your Go WhatsApp API service has **network access** to Chatwoot's PostgreSQL database (port 5432)

2. Set the connection URI:
   ```bash
   CHATWOOT_IMPORT_DB_URI=postgresql://postgres:password@chatwoot-db:5432/chatwoot_production?sslmode=disable
   ```

   > **Note:** if your password contains `@ : / ? #` or other URL-reserved characters, percent-encode them (e.g. `@` → `%40`). libpq will otherwise misparse the DSN and report a confusing "invalid port" error.

3. Enable history import:
   ```bash
   CHATWOOT_IMPORT_MESSAGES=true
   CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7
   ```

### Docker Compose Example

```yaml
services:
  whatsapp-api:
    image: aldinokemal2104/go-whatsapp-web-multidevice:latest
    environment:
      - CHATWOOT_ENABLED=true
      - CHATWOOT_URL=https://chatwoot.example.com
      - CHATWOOT_API_TOKEN=your_api_token
      - CHATWOOT_ACCOUNT_ID=1
      - CHATWOOT_INBOX_ID=1
      - CHATWOOT_DEVICE_ID=my-device
      - CHATWOOT_IMPORT_MESSAGES=true
      - CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7
      - CHATWOOT_IMPORT_DB_URI=postgresql://postgres:password@chatwoot-db:5432/chatwoot_production?sslmode=disable
    command: rest

  chatwoot-db:
    image: pgvector/pgvector:pg17
    # ... your existing Chatwoot Postgres config
```

### How It Works

When `CHATWOOT_IMPORT_DB_URI` is set:
- **Historical sync** (auto-sync on connect + manual `/chatwoot/sync`) writes directly to Chatwoot's PostgreSQL tables, preserving original timestamps and metadata
- **Live messages** (incoming WhatsApp messages and Chatwoot webhook replies) continue to use the REST API so Chatwoot's normal event pipeline (assignment rules, automations, agent UI sockets) fires correctly
- If the database connection, schema table checks, or configured account/inbox pair fail validation, the sync fails instead of silently falling back to the REST path
- If `CHATWOOT_IMPORT_MEDIA_WITH_REST=true`, downloadable media messages are first uploaded through REST to create Chatwoot ActiveStorage attachments; the direct DB import then skips those rows by the same `WAID:<id>` source id. Media rows uploaded this way use Chatwoot's upload time, while non-media rows keep their original WhatsApp timestamps.

When `CHATWOOT_IMPORT_DB_URI` is **not** set:
- All sync operations use the Chatwoot REST API (the previous behavior, compatible with Chatwoot Cloud)

### Configuration Options

| Variable | Default | Description |
|----------|---------|-------------|
| `CHATWOOT_IMPORT_DB_URI` | - | PostgreSQL connection string to Chatwoot's database |
| `CHATWOOT_IMPORT_PLACEHOLDER_MEDIA_MESSAGE` | `true` | When `true`, inserts `[image]`, `[video]`, etc. as message body for media messages that could not be downloaded. When `false`, leaves the body empty. |
| `CHATWOOT_IMPORT_MEDIA_WITH_REST` | `false` | When `true`, direct-DB sync uses REST for downloadable media rows so attachments exist in Chatwoot. |

### Requirements

- **Self-hosted Chatwoot only** — Chatwoot Cloud does not expose database access
- PostgreSQL network reachability from the Go API service
- The `CHATWOOT_ACCOUNT_ID` and `CHATWOOT_INBOX_ID` must match an existing account and inbox in the database; this is verified before import starts
- Chatwoot 2.x, 3.x, or 4.x (the tables used have been stable across these versions)
- The database URI must point to the Postgres database used by Chatwoot itself, not GOWA's local SQLite databases
- Current self-hosted Chatwoot images can require Postgres extensions including `vector` and `pg_stat_statements`

### Self-hosted Postgres and pgvector

Current Chatwoot releases can require the `vector` extension during `rails db:chatwoot_prepare`. A plain `postgres:<version>` image may fail with:

```text
ERROR: extension "vector" is not available
Could not open extension control file ".../extension/vector.control"
```

Use a durable Postgres image that includes pgvector, for example:

```yaml
services:
  chatwoot-db:
    image: pgvector/pgvector:pg17
```

If Chatwoot shares an existing Postgres service with other apps, prefer changing that existing service to the matching pgvector image instead of running a second Postgres server just for Chatwoot. Keep the major Postgres version the same, take a backup first, then verify existing databases after the restart. For example, move from `postgres:17` to `pgvector/pgvector:pg17`, not from `postgres:17` to a different major version.

After the pgvector-capable server is running, create or verify the Chatwoot database and extensions:

```sql
CREATE DATABASE chatwoot_production OWNER chatwoot;
\c chatwoot_production
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Then run Chatwoot's normal database preparation command for the Chatwoot container/image. GOWA only needs the final connection URI in `CHATWOOT_IMPORT_DB_URI`; it does not create or migrate the Chatwoot database itself.

### Outgoing Message Attribution

When `CHATWOOT_API_TOKEN` is set, the importer resolves the Chatwoot user that owns the token from the `access_tokens` table at startup and stamps every imported outgoing message (`IsFromMe == true`) with `sender_type='User'` (or `'AgentBot'`) and `sender_id=<owner_id>`. This makes historical replies render with the agent's name and avatar in Chatwoot's UI instead of as "Unknown sender".

If the token is missing, revoked, or the table is unreachable, outgoing messages still import — they just land with `NULL` sender; a startup warning is logged so operators can fix the token if attribution matters.

### Security

- Use TLS/SSL for the PostgreSQL connection in production (`?sslmode=require`)
- Consider SSH tunneling or VPN if the database is on a separate network
- The import connection requires `SELECT` on the `accounts`, `inboxes`, `schema_migrations`, and `access_tokens` tables, plus `INSERT`, `UPDATE`, and `SELECT` on the `contacts`, `contact_inboxes`, `conversations`, and `messages` tables

## Conversation & Message Behavior

### Conversation reopen

By default (`CHATWOOT_REOPEN_CONVERSATION=true`), when a contact who already has a **resolved** conversation messages again, their existing conversation is reopened rather than a new one being spawned — so an agent sees the full history in one thread. Both the REST and direct-DB paths behave this way.

Set it to `false` and the two paths intentionally differ. The REST path opens a fresh conversation for a contact whose only conversation is resolved, so each new session starts a clean thread. The direct-DB importer instead reuses the existing conversation regardless of status, and only creates a conversation when the contact has none yet — this keeps backfilled history in a single thread rather than splitting it, and avoids spawning duplicate threads on re-import.

With `CHATWOOT_CONVERSATION_PENDING=true`, newly-created conversations land in `pending` (the unassigned queue) instead of `open` on both paths.

### Agent signature

With `CHATWOOT_SIGN_MSG=true`, replies sent from Chatwoot are prefixed with the sending agent's name before delivery to WhatsApp, e.g. `*Jane*` followed by the body. The separator is `CHATWOOT_SIGN_DELIMITER` (default two newlines; a literal `\n` is expanded to a real newline).

### Formatting translation

Text formatting is translated in both directions so emphasis survives the bridge:

| WhatsApp | Chatwoot |
|----------|----------|
| `*bold*` | `**bold**` |
| `_italic_` | `*italic*` |
| `~strike~` | `~~strike~~` |

As in WhatsApp, a WhatsApp-side pair only counts at word boundaries and never inside links, so URLs such as `/_docs_/` or `?utm_source=wa&utm_medium=cta` and `snake_case` ids reach Chatwoot unchanged.

Chatwoot hands an API-channel inbox the agent's reply as stored Markdown, and WhatsApp has no syntax for most of it. Replies sent from Chatwoot are therefore also flattened so the customer does not read raw Markdown:

| Chatwoot reply | Delivered to WhatsApp |
|----------------|-----------------------|
| `[Our site](https://example.com/)` | `Our site: https://example.com/` |
| `[**https://example.com/**](https://example.com/)` (the label is the URL) | `https://example.com/` |
| `**https://example.com/**: https://example.com/` (how the reply box stores a canned reply holding such a link) | `https://example.com/` |
| `<https://example.com/>`, `<info@example.com>`, `<tel:+15550100>` | `https://example.com/`, `info@example.com`, `+15550100` |
| `[Write us](mailto:info@example.com)`, `[Call](tel:+15550100)` | `Write us: info@example.com`, `Call: +15550100` |
| `R\&D`, `snake\_case`, `1\.`, `https://example.com/?a=1\&b=2` | `R&D`, `snake_case`, `1.`, `https://example.com/?a=1&b=2` |
| `***bold italic***` | `*_bold italic_*` |
| `2 * 3 * 4`, `30*40*50 cm`, `* a bullet`, `Price* ... *Terms`, `****1234` | unchanged |
| a last line holding only `--` | removed |

- A delimiter only formats when it touches the text on its inner side, as in Markdown itself: `** spaced **` and arithmetic such as `2 * 3 * 4` are delivered as written. A single `*` additionally needs a word boundary on its outer side, because WhatsApp does not format inside a word: `30*40*50` stays as typed instead of becoming `30_40_50`.
- URLs are never formatted: `*`, `_` and `~` inside a link (query strings, `snake_case` paths) are delivered as written, also when the link sits inside bold or italic text.
  The markers that close bold, italic or strikethrough end the link when punctuation follows them, so `**https://example.com**!Thanks` is delivered as `*https://example.com*!Thanks`.
- A link label counts as the URL only when it is the same address: scheme and host are compared without regard to case, the rest exactly. `[https://example.com/Invoice_A](https://example.com/invoicea)` keeps its label and is delivered as `https://example.com/Invoice_A: https://example.com/invoicea`.
- An escaped delimiter (`\*`, `\~`) is delivered as the plain character and does not start formatting.
- Inline code and fenced code blocks are delivered unchanged; WhatsApp renders both.
- Lists (`- item`, `1. item`), line breaks, emoji and right-to-left text are delivered as written.
- The `--` line is the separator Chatwoot appends when an agent sends with the signature switch on but has no signature text. A separator followed by a signature is kept. When nothing but the separator is left, no text message is sent; an attachment on the same reply still goes out, without a caption.
- With `CHATWOOT_SIGN_MSG=true` the conversion applies to the agent's body; the agent name is added afterwards.

### Edits & deletions

- **Edits**: when a WhatsApp user edits a message, the new text is mirrored into Chatwoot as a threaded `✏️ **Edited:** …` note (controlled by `CHATWOOT_FORWARD_EDITS`, default on).
- **Deletions**: a delete-for-everyone is mirrored as a threaded `🗑️ This message was deleted.` note (controlled by `CHATWOOT_FORWARD_DELETES`, default on).
- **Linked deletes**: with `CHATWOOT_MESSAGE_DELETE=true`, WhatsApp delete-for-everyone events delete the linked Chatwoot message when a durable link exists. Chatwoot `message_updated` delete webhooks revoke linked outgoing WhatsApp messages; inbound customer messages are not revoked because this device did not send them.

Both are threaded onto the original message via Chatwoot's `in_reply_to_external_id` using the shared `WAID:<id>` source-id convention, so they appear as replies to the message they affect.

### Read sync

With `CHATWOOT_MESSAGE_READ=true`, WhatsApp read receipts update Chatwoot `last_seen` for the linked conversation, and successful Chatwoot agent replies mark the latest unread inbound WhatsApp message in that chat as read. This depends on the local durable message-link table populated by live forwarding, REST history sync, and direct-DB import.

### Replies & reactions

Inbound replies and reactions are threaded onto the message they reference (again via the `WAID:` source-id), so a reply or 👍 lands attached to the right message in the Chatwoot conversation.

Agent replies are quoted the other way: when an agent uses **Reply** in Chatwoot, the WhatsApp message quotes the original. The text and every attachment carry the quote, including the audio-to-file fallback; the caption stays on the first attachment only.

- A message mirrored from WhatsApp (`in_reply_to_external_id` is `WAID:<id>`) is quoted only when it is in the sending device's local message store and belongs to the same chat. An `@lid` chat matches its phone JID when the sending device knows the mapping.
- An earlier agent message has no `source_id` in Chatwoot, so only its numeric `in_reply_to` arrives. It is resolved through the link GOWA stored when it delivered that message, in the same device, conversation, account, inbox and config; the link and the WhatsApp message it points to, in the sending device's local message store, must belong to the same chat too. A message sent with several attachments is quoted by its first file. The numeric id is not used when a `WAID:` reference is present.
- A quote that cannot be resolved is not an error: the reply is sent without it, as before, and no failure note is added.

### Ignoring chats

`CHATWOOT_IGNORE_JIDS` lets you exclude chats from Chatwoot entirely, on top of the always-ignored system JIDs (`status@broadcast`, `0@s.whatsapp.net`). It accepts a comma-separated list of exact JIDs or the wildcards `@g.us` (all groups), `@s.whatsapp.net` (all DMs), or `@lid`:

```bash
# Ignore all groups and one specific contact
CHATWOOT_IGNORE_JIDS=@g.us,628123456789@s.whatsapp.net
```

An exact entry matches a chat under either of its addresses, so `111222333@lid` still excludes that contact after its LID resolves to a phone number, for live forwarding and history import alike. Wildcards match only the address a message is delivered under. Live forwarding checks the chat and the sender, so `@lid` excludes a chat whose LID has not resolved to a phone number and also a group message whose sender's LID has not resolved, although the chat is `@g.us`. History import checks only the stored chat address.

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

Live WhatsApp messages that hit a transient Chatwoot failure (network error, HTTP 429, or HTTP 5xx) are stored in the local chat storage retry queue and replayed in the background with capped exponential backoff. Permanent Chatwoot errors (most HTTP 4xx responses) are logged and not retried.

### Outgoing Messages (Chatwoot → WhatsApp)

| Message Type | Supported | Notes |
|--------------|-----------|-------|
| Text | ✅ | - |
| Images | ✅ | Sent with optional caption |
| Audio | ✅ | Sent as voice note (PTT) |
| Video | ✅ | - |
| Files | ✅ | Any file type supported |
| Quoted replies | ✅ | Quotes the original message when it can be resolved (see [Replies & reactions](#replies--reactions)) |

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
   - Both IDs must be numeric Chatwoot IDs. Do not use the API-channel `identifier`, `hmac_token`, or webhook secret as either ID.

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
| `401 Unauthorized` on `/chatwoot/webhook` | `CHATWOOT_WEBHOOK_SECRET` is set but the request did not prove it | Set `CHATWOOT_WEBHOOK_SECRET` (or the device's `webhook_secret`) to the API channel's webhook secret so Chatwoot's `X-Chatwoot-Signature` verifies. The GOWA log says why a signature was rejected: `signature does not match` is a different secret, `timestamp outside the tolerance window` is clock drift between the hosts. Otherwise send `X-Chatwoot-Webhook-Secret`, `X-Gowa-Chatwoot-Secret`, a valid `X-Hub-Signature-256`, or `?secret=<value>` |
| `CHATWOOT_NOT_CONFIGURED` | Missing URL, token, account ID, or inbox ID | Set `CHATWOOT_URL`, `CHATWOOT_API_TOKEN`, numeric `CHATWOOT_ACCOUNT_ID`, and numeric `CHATWOOT_INBOX_ID`, or enable auto-provisioning |

### Verifying Webhook Connectivity

Test your webhook endpoint. If `CHATWOOT_WEBHOOK_SECRET` is set, present it in a header for the test (to test the signed form, see the snippet below):

```bash
curl -X POST "https://your-api.com/chatwoot/webhook" \
  -H "Content-Type: application/json" \
  -H "X-Chatwoot-Webhook-Secret: strong-shared-secret" \
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

To send a request signed the way Chatwoot signs it:

```bash
SECRET='secret-shown-by-chatwoot'
BODY='{"event":"conversation_updated"}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $NF}')
curl -X POST "https://your-api.com/chatwoot/webhook" \
  -H "Content-Type: application/json" \
  -H "X-Chatwoot-Timestamp: $TS" \
  -H "X-Chatwoot-Signature: sha256=$SIG" \
  -d "$BODY"
```

Test the authenticated sync status endpoint:

```bash
curl -u user:password \
  "https://your-api.com/chatwoot/sync/status?device_id=my-device-id"
```

Expected response: `200 OK` with `status` such as `idle`, `running`, or `completed`.

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
- Set `CHATWOOT_WEBHOOK_SECRET` for the unauthenticated webhook route and let GOWA verify Chatwoot's own signature (`X-Chatwoot-Signature`). Avoid `?secret=...`: a secret in the URL leaks into access logs and into Chatwoot's inbox settings.

## API Reference

### Webhook Endpoint

**URL:** `POST /chatwoot/webhook`

**Headers:**
- `Content-Type: application/json`
- When `CHATWOOT_WEBHOOK_SECRET` is set, one of: `X-Chatwoot-Signature` with `X-Chatwoot-Timestamp` (Chatwoot's signature, recommended), `X-Hub-Signature-256`, `X-Chatwoot-Webhook-Secret`, `X-Gowa-Chatwoot-Secret`

**Query Parameters:**
- Alternative when `CHATWOOT_WEBHOOK_SECRET` is set and no header can be sent: `secret=<CHATWOOT_WEBHOOK_SECRET>` (discouraged: the secret ends up in logs)

**Request Body:** Standard Chatwoot webhook payload

**Response Codes:**
- `200 OK` - Message processed (or skipped, including a repeated copy of an already handled signed request)
- `401 Unauthorized` - Webhook secret or signature is missing/invalid for the device the request is routed to, or the signature's timestamp is outside the tolerance window
- `503 Service Unavailable` - No device available, or the routed device's config could not be read to authenticate the request, or an identical signed request is still being handled

### Related Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/devices` | GET | List all registered devices |
| `/devices/{id}` | GET | Get device details |
| `/devices/{id}/status` | GET | Check device connection status |
