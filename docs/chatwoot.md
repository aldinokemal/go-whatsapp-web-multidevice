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
| `CHATWOOT_WEBHOOK_URL` | Reply webhook | - | Public URL Chatwoot should call for agent replies: `https://your-gowa.example.com/chatwoot/webhook`. Used when creating an API inbox automatically, and useful as the value to place in an existing API channel's `webhook_url`. Include `?secret=...` when using `CHATWOOT_WEBHOOK_SECRET` and Chatwoot cannot send custom headers. |
| `CHATWOOT_WEBHOOK_SECRET` | Recommended | - | Shared secret expected by GOWA for incoming Chatwoot webhook calls. If empty, webhooks are accepted without this extra check. If set, the request must send the value via `X-Chatwoot-Webhook-Secret`, `X-Gowa-Chatwoot-Secret`, `?secret=...`, or an `X-Hub-Signature-256` HMAC signature. |
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
CHATWOOT_WEBHOOK_SECRET=strong-shared-secret
CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook?secret=strong-shared-secret

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

# Agent reply callback from Chatwoot to GOWA.
CHATWOOT_WEBHOOK_SECRET=strong-shared-secret
CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook?secret=strong-shared-secret

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
      - CHATWOOT_WEBHOOK_SECRET=strong-shared-secret
      - CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook?secret=strong-shared-secret
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
8. Set `CHATWOOT_WEBHOOK_SECRET` and send the same value through the webhook URL query string or a supported header.
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
   https://your-gowa.example.com/chatwoot/webhook?secret=strong-shared-secret
   ```
4. Save the inbox.

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
CHATWOOT_WEBHOOK_SECRET=super-secret-key           # optional, but recommended
CHATWOOT_WEBHOOK_URL=https://your-api.com/chatwoot/webhook?secret=super-secret-key
```

On startup the app will:

1. Look for an inbox named `CHATWOOT_INBOX_NAME` in the account and **reuse it** if present (so restarts never create duplicates).
2. Otherwise **create** a new API-channel inbox with that name, wired to `CHATWOOT_WEBHOOK_URL`.
3. Resolve `CHATWOOT_INBOX_ID` automatically for the rest of the integration.

Notes:

- If `CHATWOOT_INBOX_ID` is already set, it takes precedence and auto-create is skipped (your explicit value wins).
- If `CHATWOOT_WEBHOOK_URL` is empty, the inbox is still created but **agent replies won't reach WhatsApp** until you set the webhook — a warning is logged.
- Provisioning failures are non-fatal; the app keeps running and you can fall back to manual setup.

## Webhook Secret

`CHATWOOT_WEBHOOK_SECRET` protects GOWA's public `/chatwoot/webhook` endpoint. It is the value GOWA expects to receive from Chatwoot on every agent-reply webhook.

When `CHATWOOT_WEBHOOK_SECRET` is empty, GOWA accepts Chatwoot webhooks without this extra check. When it is set, GOWA accepts the request only if the same secret arrives in one of these forms:

| Method | Example | When to use |
|--------|---------|-------------|
| Header | `X-Chatwoot-Webhook-Secret: strong-shared-secret` | Best when a proxy or webhook sender can set custom headers. |
| Header | `X-Gowa-Chatwoot-Secret: strong-shared-secret` | Same as above; provided as a GOWA-specific header name. |
| Query string | `/chatwoot/webhook?secret=strong-shared-secret` | Practical for Chatwoot API-channel inbox webhook URLs because the inbox stores a URL, not custom headers. |
| HMAC signature | `X-Hub-Signature-256: sha256=<digest>` | Use when an intermediate system can sign the raw request body with the shared secret. |

For a plain Chatwoot API-channel inbox, use:

```bash
CHATWOOT_WEBHOOK_SECRET=strong-shared-secret
CHATWOOT_WEBHOOK_URL=https://your-gowa.example.com/chatwoot/webhook?secret=strong-shared-secret
```

The query-string form is less ideal than a header because URLs may appear in logs. If that is a concern, put a reverse proxy in front of GOWA: let Chatwoot call a secret-free URL on the proxy, then have the proxy add `X-Chatwoot-Webhook-Secret` before forwarding to GOWA.

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

### Edits & deletions

- **Edits**: when a WhatsApp user edits a message, the new text is mirrored into Chatwoot as a threaded `✏️ **Edited:** …` note (controlled by `CHATWOOT_FORWARD_EDITS`, default on).
- **Deletions**: a delete-for-everyone is mirrored as a threaded `🗑️ This message was deleted.` note (controlled by `CHATWOOT_FORWARD_DELETES`, default on).
- **Linked deletes**: with `CHATWOOT_MESSAGE_DELETE=true`, WhatsApp delete-for-everyone events delete the linked Chatwoot message when a durable link exists. Chatwoot `message_updated` delete webhooks revoke linked outgoing WhatsApp messages; inbound customer messages are not revoked because this device did not send them.

Both are threaded onto the original message via Chatwoot's `in_reply_to_external_id` using the shared `WAID:<id>` source-id convention, so they appear as replies to the message they affect.

### Read sync

With `CHATWOOT_MESSAGE_READ=true`, WhatsApp read receipts update Chatwoot `last_seen` for the linked conversation, and successful Chatwoot agent replies mark the latest unread inbound WhatsApp message in that chat as read. This depends on the local durable message-link table populated by live forwarding, REST history sync, and direct-DB import.

### Replies & reactions

Inbound replies and reactions are threaded onto the message they reference (again via the `WAID:` source-id), so a reply or 👍 lands attached to the right message in the Chatwoot conversation.

### Ignoring chats

`CHATWOOT_IGNORE_JIDS` lets you exclude chats from Chatwoot entirely, on top of the always-ignored system JIDs (`status@broadcast`, `0@s.whatsapp.net`). It accepts a comma-separated list of exact JIDs or the wildcards `@g.us` (all groups), `@s.whatsapp.net` (all DMs), or `@lid`:

```bash
# Ignore all groups and one specific contact
CHATWOOT_IGNORE_JIDS=@g.us,628123456789@s.whatsapp.net
```

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
| `401 Unauthorized` on `/chatwoot/webhook` | `CHATWOOT_WEBHOOK_SECRET` is set but the request did not send the expected secret | Add `?secret=<value>` to the API-channel webhook URL, send `X-Chatwoot-Webhook-Secret`, send `X-Gowa-Chatwoot-Secret`, or send a valid `X-Hub-Signature-256` |
| `CHATWOOT_NOT_CONFIGURED` | Missing URL, token, account ID, or inbox ID | Set `CHATWOOT_URL`, `CHATWOOT_API_TOKEN`, numeric `CHATWOOT_ACCOUNT_ID`, and numeric `CHATWOOT_INBOX_ID`, or enable auto-provisioning |

### Verifying Webhook Connectivity

Test your webhook endpoint. If `CHATWOOT_WEBHOOK_SECRET` is set, include it in the query string or a supported header:

```bash
curl -X POST "https://your-api.com/chatwoot/webhook?secret=strong-shared-secret" \
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
- Set `CHATWOOT_WEBHOOK_SECRET` for the unauthenticated webhook route. Prefer headers or HMAC signatures when possible; use `?secret=...` for Chatwoot API-channel inboxes when no custom header injection is available.

## API Reference

### Webhook Endpoint

**URL:** `POST /chatwoot/webhook`

**Headers:**
- `Content-Type: application/json`
- Optional when `CHATWOOT_WEBHOOK_SECRET` is set: `X-Chatwoot-Webhook-Secret`, `X-Gowa-Chatwoot-Secret`, or `X-Hub-Signature-256`

**Query Parameters:**
- Optional when `CHATWOOT_WEBHOOK_SECRET` is set: `secret=<CHATWOOT_WEBHOOK_SECRET>`

**Request Body:** Standard Chatwoot webhook payload

**Response Codes:**
- `200 OK` - Message processed (or skipped)
- `401 Unauthorized` - Webhook secret or signature is missing/invalid
- `503 Service Unavailable` - No device available

### Related Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/devices` | GET | List all registered devices |
| `/devices/{id}` | GET | Get device details |
| `/devices/{id}/status` | GET | Check device connection status |
