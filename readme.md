<!-- markdownlint-disable MD041 -->
<!-- markdownlint-disable-next-line MD033 -->
<div align="center">
  <!-- markdownlint-disable-next-line MD033 -->
  <img src="gallery/gowa.svg" alt="GoWA Logo" width="200" height="200">

## Go WhatsApp — Built for Efficient Memory Use

</div>

[![Patreon](https://img.shields.io/badge/Support%20on-Patreon-orange.svg)](https://www.patreon.com/c/aldinokemal)

**If you're using this tool to generate income, consider supporting its development by becoming a Patreon member!**

Your support helps ensure the project stays maintained and receives regular updates!

---

![release version](https://img.shields.io/github/v/release/aldinokemal/go-whatsapp-web-multidevice)
![Build Image](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/build-docker-image.yaml/badge.svg)
![Binary Release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/release.yml/badge.svg)

## ARM, AMD64, and MCP Support

Download:

- [Release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases/latest)
- [Docker Hub](https://hub.docker.com/r/aldinokemal2104/go-whatsapp-web-multidevice/tags)
- [GitHub Container Registry](https://github.com/aldinokemal/go-whatsapp-web-multidevice/pkgs/container/go-whatsapp-web-multidevice)

## n8n Community Node

- [n8n package](https://www.npmjs.com/package/@aldinokemal2104/n8n-nodes-gowa)
- Go to **Settings → Community Nodes**, enter `@aldinokemal2104/n8n-nodes-gowa`, and select **Install**.

## Breaking Changes

- `v6`
  - REST mode requires `<binary> rest` instead of `<binary>`.
    - Example: `./whatsapp rest` instead of `./whatsapp`.
  - MCP mode required `<binary> mcp`.
    - Example: `./whatsapp mcp`.
- `v7`
  - Starting with version 7.x, binaries are built with GoReleaser and can be downloaded from the
    [latest release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases/latest).
- `v8`
  - **Multi-device support**: You can now connect and manage multiple WhatsApp accounts simultaneously in a single
    server instance.
  - **New Device Management API**: New endpoints under `/devices` manage multiple devices.
  - **Device scoping required**: All device-scoped REST API calls now require either:
    - `X-Device-Id` header, or
    - `device_id` query parameter.
    - If only one device is registered, it is used as the default.
  - **WebSocket device scoping**: Connect to `/ws?device_id=<id>` to scope the WebSocket connection to a specific device.
  - **Remote UI support**: CORS allows the `Authorization` and `X-Device-Id` headers, so a standalone web UI
    (for example, [gowa-ui](https://github.com/aldinokemal/gowa-ui)) hosted on another origin can call the API directly.
    `GET /app/info` exposes the version and media size limits. Because browsers cannot set headers on WebSocket
    connections, pass `/ws?device_id=<id>&authorization=<base64(user:pass)>` when Basic Auth is enabled
    (use TLS—the credential is visible in the URL).
  - **Webhook payload changes**: All webhook payloads now include a top-level `device_id` field identifying which
    device received the event:

    ```json
    {
      "event": "message",
      "device_id": "628123456789@s.whatsapp.net",
      "payload": { ... }
    }
    ```

- `v9`
  - **MCP and API are unified under `rest`**: MCP is no longer a separate mode or process. Run
    `./whatsapp rest` to serve both the REST API and MCP; MCP is available at `/mcp` (no standalone `mcp`
    subcommand). See [MCP Server (Model Context Protocol)](#mcp-server-model-context-protocol) for migration
    details.
  - **UI moved to a separate repository**: The web dashboard is no longer bundled in this repo. It now lives at
    [aldinokemal/gowa-ui](https://github.com/aldinokemal/gowa-ui) and ships as a single self-contained
    `gowa-ui.html`. The server downloads the latest dashboard release at startup, verifies its SHA-256 digest,
    caches it under `storages/ui/`, and serves it at `/`.
    See [Web dashboard (gowa-ui)](#web-dashboard-gowa-ui) for the `APP_UI_*` settings, supply-chain pinning,
    and air-gapped deployment.

## Features

- Send WhatsApp messages through the HTTP API. See [docs/openapi.yaml](./docs/openapi.yaml) for details.
- **MCP (Model Context Protocol) server support** — Integrate with AI agents and tools using a standardized protocol.
- **Optional MCP OAuth 2.1** — Connect remote MCP clients that cannot supply a Basic Auth header. See
  [MCP OAuth](./docs/mcp-oauth.md).
- Mention users:
  - `@phoneNumber`
  - Example: `Hello @628974812XXXX, @628974812XXXX`
- **Ghost mentions (mention all)** — Mention group participants without showing `@phone` in the message text.
  - Pass phone numbers in the `mentions` field to mention users without a visible `@` in the message.
  - Use the special keyword `@everyone` to automatically mention all group participants.
- Post WhatsApp status updates.
- Mark incoming audio messages and voice notes as played.
- **Send stickers** — Automatically convert images to WebP sticker format.
  - Supports JPG, JPEG, PNG, WebP, and GIF formats.
  - Automatically resizes images to 512×512 pixels.
  - Preserves transparency in PNG images.
  - **Animated WebP stickers** are supported but must meet WhatsApp requirements:
    - Exactly **512×512 pixels**.
    - Less than **500 KB**.
    - No more than **10 seconds** long.
    - If an animated sticker does not meet these requirements, resize it before uploading with a tool such as
      [ezgif.com](https://ezgif.com/resize).
- Compress images before sending.
- Compress videos before sending.
- Customize the OS name shown as the linked device name in WhatsApp:
  - `--os=Chrome` or `--os=MyApplication`
- Basic Auth with multiple credentials:
  - `--basic-auth=kemal:secret,toni:password,userName:secretPassword`
  - Short form: `-b=kemal:secret,toni:password,userName:secretPassword`
- Subpath deployment support:
  - `--base-path="/gowa"` allows deployment under a path such as `/gowa`.
- Customizable port and debug mode:
  - `--port 8000`
  - `--debug true`
- Automatic replies to incoming messages:
  - `--autoreply="Don't reply to this message"`
- Automatically mark incoming messages as read:
  - `--auto-mark-read=true`
- Automatically download media from incoming messages:
  - `--auto-download-media=false` disables automatic media downloads (default: `true`).
- Automatically reject incoming calls:
  - `--auto-reject-call=true` or `WHATSAPP_AUTO_REJECT_CALL=true` (see
    [Webhook Payload](./docs/webhook-payload.md#call-events) for call events).
- Configurable presence on connect:
  - `--presence-on-connect=unavailable` or `WHATSAPP_PRESENCE_ON_CONNECT=unavailable`
  - `available` — Mark the account as online (suppresses phone notifications).
  - `unavailable` — Register the push name without going online (default; preserves phone notifications).
  - `none` — Skip presence entirely (the push name is not registered, so contacts may see `-` as the name).
- Daily presence pulse:
  - `--presence-pulse-enabled=true` or `WHATSAPP_PRESENCE_PULSE_ENABLED=true` (default: `true`).
  - `--presence-pulse-interval=24h` controls how often each connected device is pulsed.
  - `--presence-pulse-duration=5m` controls how long the account stays `available` before returning to `unavailable`.
- Webhooks for received messages and other events:
  - `--webhook="http://yourwebhook.site/handler"`
  - Short form: `-w="http://yourwebhook.site/handler"`
  - See [Webhook Payload Documentation](./docs/webhook-payload.md) for details.
- **Per-device webhooks** — Each device can have its own webhook URL and event filters.
  - Set via API: `PATCH /devices/:device_id/webhook` with `{"webhook_url": "https://device-webhook.site/handler"}`.
  - Get via API: `GET /devices/:device_id/webhook`.
  - When a device has a custom webhook, events for that device are sent to the device-specific URL.
  - When no device webhook is set, events fall back to the global webhook (`--webhook`).
  - Set `webhook_url` to an empty string with `PATCH` to clear it and use the global webhook.
- **Webhook signatures** — Webhook requests include an HMAC-SHA-256 signature in the `X-Hub-Signature-256`
  header, generated with the default key `secret`.

  Change the key with:
  - `--webhook-secret="secret"`
- **Webhook payload documentation** — For detailed schemas, security implementation, and integration examples,
  see [Webhook Payload Documentation](./docs/webhook-payload.md).
- **Webhook event filtering** — Filter which events are forwarded to your webhook with:
  - `--webhook-events="message,message.ack"` (a comma-separated list), or
  - `WHATSAPP_WEBHOOK_EVENTS=message,message.ack`.

  **Available Webhook Events:**

  | Event                | Description                                   |
  |----------------------|-----------------------------------------------|
  | `message`            | Text, media, contact, location messages       |
  | `message.reaction`   | Emoji reactions to messages                   |
  | `message.revoked`    | Deleted/revoked messages                      |
  | `message.edited`     | Edited messages                               |
  | `message.ack`        | Delivery and read receipts                    |
  | `message.deleted`    | Messages deleted for the user                 |
  | `chat_presence`      | Typing and recording indicators from contacts |
  | `group.participants` | Group member join/leave/promote/demote events |
  | `group.joined`       | You were added to a group                     |
  | `label.edit`         | WhatsApp label metadata changed               |
  | `label.association`  | Label applied to or removed from a chat       |
  | `newsletter.joined`  | You subscribed to a newsletter/channel        |
  | `newsletter.left`    | You unsubscribed from a newsletter            |
  | `newsletter.message` | New message(s) posted in a newsletter         |
  | `newsletter.mute`    | Newsletter mute setting changed               |
  | `call.offer`         | Incoming call received                        |

  If this setting is empty, all events are forwarded.
- **Webhook JID filtering**

  You can skip events for specific chats or senders (for example, mute all groups) before they are forwarded:
  - `--webhook-ignore-jids="@g.us,628123456789@s.whatsapp.net"` (a comma-separated list), or
  - `WHATSAPP_WEBHOOK_IGNORE_JIDS=@g.us`.
  - Supports the `@g.us` / `@s.whatsapp.net` / `@lid` wildcards (match a whole address space) and exact JIDs.
  - This filters by conversation or sender and is independent of `--webhook-events`, which filters by event type.
    The Chatwoot integration has a separate `CHATWOOT_IGNORE_JIDS` setting.
- **Webhook TLS configuration**

  If you encounter TLS certificate verification errors when using webhooks (e.g., with Cloudflare tunnels or self-signed
  certificates):

  ```text
  tls: failed to verify certificate: x509: certificate signed by unknown authority
  ```

  You can disable TLS certificate verification with:
  - `--webhook-insecure-skip-verify=true`, or
  - `WHATSAPP_WEBHOOK_INSECURE_SKIP_VERIFY=true`.

  **Security Warning**: This option disables TLS certificate verification and should only be used in:
  - Development or testing environments.
  - Cloudflare tunnels, which provide their own security layer.
  - Internal networks with self-signed certificates.

  **For production environments**, use a valid TLS certificate (for example, from Let's Encrypt) instead of
  disabling verification.

## Configuration

Configuration is loaded in this order of priority:

1. Command-line flags (highest priority)
2. Environment variables
3. `.env` file (lowest priority)

### Environment Variables

To use environment variables:

1. From the repository root, copy the example file: `cp src/.env.example src/.env`.
2. Update the values in `src/.env` as needed.
3. Alternatively, set the same variables in the process environment.

#### Available Environment Variables

| Variable                                | Description                                                   | Default                                      | Example                                       |
|-----------------------------------------|---------------------------------------------------------------|----------------------------------------------|-----------------------------------------------|
| `APP_PORT`                              | Application port                                              | `3000`                                       | `APP_PORT=8080`                               |
| `APP_HOST`                              | Host address to bind the server                               | `0.0.0.0`                                    | `APP_HOST=127.0.0.1`                          |
| `APP_DEBUG`                             | Enable debug logging                                          | `false`                                      | `APP_DEBUG=true`                              |
| `APP_OS`                                | OS name (device name in WhatsApp)                             | `GOWA`                                       | `APP_OS=MyApp`                                |
| `APP_BASIC_AUTH`                        | Basic authentication credentials                              | -                                            | `APP_BASIC_AUTH=user1:pass1,user2:pass2`      |
| `APP_BASE_PATH`                         | Base path for subpath deployment                              | -                                            | `APP_BASE_PATH=/gowa`                         |
| `APP_TRUSTED_PROXIES`                   | Trusted proxy IP ranges for reverse proxy                     | -                                            | `APP_TRUSTED_PROXIES=0.0.0.0/0`               |
| `APP_CORS_ALLOWED_ORIGINS`              | Allowed CORS origins (any origin when empty)                  | -                                            | `APP_CORS_ALLOWED_ORIGINS=https://ui.example.com` |
| `APP_UI_ENABLED`                        | Serve the downloaded gowa-ui dashboard                        | `true`                                       | `APP_UI_ENABLED=false`                        |
| `APP_UI_AUTO_UPDATE`                    | Download and periodically refresh the latest dashboard        | `true`                                       | `APP_UI_AUTO_UPDATE=false`                    |
| `APP_UI_REPO`                           | GitHub repository containing gowa-ui releases                 | `aldinokemal/gowa-ui`                        | `APP_UI_REPO=my-org/gowa-ui`                  |
| `APP_UI_ASSET_NAME`                     | Dashboard release asset filename                              | `gowa-ui.html`                               | `APP_UI_ASSET_NAME=gowa-ui.html`              |
| `APP_UI_UPDATE_INTERVAL`                | Interval between dashboard update checks                      | `3h`                                         | `APP_UI_UPDATE_INTERVAL=6h`                   |
| `APP_UI_GITHUB_TOKEN`                   | Optional GitHub token for a higher API rate limit             | -                                            | `APP_UI_GITHUB_TOKEN=github_pat_xxx`          |
| `APP_UI_ASSET_SHA256`                   | Optional SHA-256 pin for the dashboard asset                  | -                                            | `APP_UI_ASSET_SHA256=<hex-digest>`            |
| `MCP_ENABLED`                           | Serve the streamable HTTP MCP endpoint at `/mcp`              | `true`                                       | `MCP_ENABLED=false`                           |
| `MCP_OAUTH_ENABLED`                     | Enable OAuth 2.1 authentication for MCP                       | `false`                                      | `MCP_OAUTH_ENABLED=true`                      |
| `MCP_OAUTH_ISSUER_URL`                  | Public HTTPS OAuth issuer URL                                 | -                                            | `MCP_OAUTH_ISSUER_URL=https://gowa.example.com` |
| `MCP_OAUTH_RESOURCE_URL`                | Optional canonical public MCP URL                             | Derived from issuer and base path            | `MCP_OAUTH_RESOURCE_URL=https://gowa.example.com/mcp` |
| `MCP_OAUTH_DB_URI`                      | SQLite URI for OAuth clients, codes, and token hashes         | `file:storages/oauth.db`                     | `MCP_OAUTH_DB_URI=file:storages/oauth.db`     |
| `DB_URI`                                | Database connection URI                                       | `file:storages/whatsapp.db`                  | `DB_URI=postgres://user:pass@host/db`         |
| `DB_KEYS_URI`                           | Optional database URI for encryption/session key cache. Leave blank to use `DB_URI`; avoid in-memory storage in production because restarts can lose WhatsApp session state. | - | `DB_KEYS_URI=file:storages/whatsapp-keys.db?_foreign_keys=on` |
| `CHAT_STORAGE_MAX_OPEN_CONNS`           | Maximum concurrent SQLite connections for chat storage        | `5`                                          | `CHAT_STORAGE_MAX_OPEN_CONNS=10`              |
| `WHATSAPP_AUTO_REPLY`                   | Auto-reply message                                            | -                                            | `WHATSAPP_AUTO_REPLY="Auto reply message"`    |
| `WHATSAPP_AUTO_MARK_READ`               | Auto-mark incoming messages as read                           | `false`                                      | `WHATSAPP_AUTO_MARK_READ=true`                |
| `WHATSAPP_AUTO_DOWNLOAD_MEDIA`          | Auto-download media from incoming messages                    | `true`                                       | `WHATSAPP_AUTO_DOWNLOAD_MEDIA=false`          |
| `WHATSAPP_AUTO_REJECT_CALL`             | Auto-reject incoming WhatsApp calls                           | `false`                                      | `WHATSAPP_AUTO_REJECT_CALL=true`              |
| `WHATSAPP_WEBHOOK`                      | Webhook URL(s) for events (comma-separated)                   | -                                            | `WHATSAPP_WEBHOOK=https://webhook.site/xxx`   |
| `WHATSAPP_WEBHOOK_SECRET`               | Webhook secret for validation                                 | `secret`                                     | `WHATSAPP_WEBHOOK_SECRET=super-secret-key`    |
| `WHATSAPP_WEBHOOK_INSECURE_SKIP_VERIFY` | Skip TLS verification for webhooks (insecure)                 | `false`                                      | `WHATSAPP_WEBHOOK_INSECURE_SKIP_VERIFY=true`  |
| `WHATSAPP_WEBHOOK_EVENTS`               | Whitelist of events to forward (comma-separated, empty = all) | -                                            | `WHATSAPP_WEBHOOK_EVENTS=message,message.ack` |
| `WHATSAPP_WEBHOOK_IGNORE_JIDS`          | JIDs/wildcards to skip when forwarding (comma-separated)      | -                                            | `WHATSAPP_WEBHOOK_IGNORE_JIDS=@g.us`          |
| `WHATSAPP_ACCOUNT_VALIDATION`           | Enable account validation                                     | `true`                                       | `WHATSAPP_ACCOUNT_VALIDATION=false`           |
| `WHATSAPP_PRESENCE_ON_CONNECT`          | Presence on connect: `available`, `unavailable`, or `none`    | `unavailable`                                | `WHATSAPP_PRESENCE_ON_CONNECT=unavailable`    |
| `WHATSAPP_PROXY`                        | Outbound proxy for the WhatsApp WebSocket (SOCKS5/HTTP/HTTPS) | -                                            | `WHATSAPP_PROXY=socks5://user:pass@host:1080` |
| `WHATSAPP_PRESENCE_PULSE_ENABLED`       | Enable daily available/unavailable presence pulse             | `true`                                       | `WHATSAPP_PRESENCE_PULSE_ENABLED=false`       |
| `WHATSAPP_PRESENCE_PULSE_INTERVAL`      | Interval between presence pulses                              | `24h`                                        | `WHATSAPP_PRESENCE_PULSE_INTERVAL=24h`        |
| `WHATSAPP_PRESENCE_PULSE_DURATION`      | Duration to stay available during each pulse                  | `5m`                                         | `WHATSAPP_PRESENCE_PULSE_DURATION=5m`         |
| `CHATWOOT_ENABLED`                      | Enable Chatwoot integration                                   | `false`                                      | `CHATWOOT_ENABLED=true`                       |
| `CHATWOOT_URL`                          | Chatwoot instance URL                                         | -                                            | `CHATWOOT_URL=https://app.chatwoot.com`       |
| `CHATWOOT_API_TOKEN`                    | Chatwoot API access token                                     | -                                            | `CHATWOOT_API_TOKEN=your-api-token`           |
| `CHATWOOT_ACCOUNT_ID`                   | Chatwoot account ID                                           | -                                            | `CHATWOOT_ACCOUNT_ID=12345`                   |
| `CHATWOOT_INBOX_ID`                     | Chatwoot inbox ID                                             | -                                            | `CHATWOOT_INBOX_ID=67890`                     |
| `CHATWOOT_DEVICE_ID`                    | WhatsApp device ID for Chatwoot (single-device/env fallback)  | -                                            | `CHATWOOT_DEVICE_ID=628xxx@s.whatsapp.net`    |
| `CHATWOOT_ALLOWED_HOSTS`                | Allowlist of Chatwoot hosts for per-device configs (SSRF guard) | -                                          | `CHATWOOT_ALLOWED_HOSTS=app.chatwoot.com,chat.example.com` |
| `CHATWOOT_IMPORT_MESSAGES`              | Enable message history sync to Chatwoot                       | `false`                                      | `CHATWOOT_IMPORT_MESSAGES=true`               |
| `CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES`   | Days of history to import                                     | `3`                                          | `CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7`       |
| `CHATWOOT_IMPORT_DB_URI`                | Direct Chatwoot PostgreSQL URI for history sync               | -                                            | `CHATWOOT_IMPORT_DB_URI=postgresql://user:pass@host:5432/chatwoot_production?sslmode=disable` |
| `CHATWOOT_IMPORT_PLACEHOLDER_MEDIA_MESSAGE` | Insert text placeholders for media rows during direct DB import | `true`                                    | `CHATWOOT_IMPORT_PLACEHOLDER_MEDIA_MESSAGE=true` |
| `CHATWOOT_IMPORT_MEDIA_WITH_REST`       | Upload direct-DB import media rows through Chatwoot REST       | `false`                                      | `CHATWOOT_IMPORT_MEDIA_WITH_REST=true`        |
| `CHATWOOT_AUTO_CREATE`                  | Auto-create or reuse the Chatwoot API inbox at startup        | `false`                                      | `CHATWOOT_AUTO_CREATE=true`                   |
| `CHATWOOT_INBOX_NAME`                   | Inbox name used when auto-create is enabled                   | `WhatsApp`                                   | `CHATWOOT_INBOX_NAME=WhatsApp Support`        |
| `CHATWOOT_WEBHOOK_URL`                  | Public GOWA Chatwoot reply webhook URL                        | -                                            | `CHATWOOT_WEBHOOK_URL=https://api.example.com/chatwoot/webhook?secret=shared` |
| `CHATWOOT_WEBHOOK_SECRET`               | Shared secret required for incoming Chatwoot webhooks          | -                                            | `CHATWOOT_WEBHOOK_SECRET=shared`              |
| `CHATWOOT_REOPEN_CONVERSATION`          | Reopen resolved Chatwoot conversations for returning contacts  | `true`                                       | `CHATWOOT_REOPEN_CONVERSATION=false`          |
| `CHATWOOT_CONVERSATION_PENDING`         | Create new Chatwoot conversations as pending                  | `false`                                      | `CHATWOOT_CONVERSATION_PENDING=true`          |
| `CHATWOOT_IGNORE_JIDS`                  | JIDs or wildcards to exclude from Chatwoot forwarding          | -                                            | `CHATWOOT_IGNORE_JIDS=@g.us,628123@s.whatsapp.net` |
| `CHATWOOT_SIGN_MSG`                     | Prefix Chatwoot agent replies with the agent name             | `false`                                      | `CHATWOOT_SIGN_MSG=true`                      |
| `CHATWOOT_SIGN_DELIMITER`               | Delimiter between Chatwoot agent signature and message body   | `\n\n`                                       | `CHATWOOT_SIGN_DELIMITER=" - "`               |
| `CHATWOOT_FORWARD_EDITS`                | Mirror WhatsApp edits into Chatwoot threaded notes            | `true`                                       | `CHATWOOT_FORWARD_EDITS=false`                |
| `CHATWOOT_FORWARD_DELETES`              | Mirror WhatsApp delete-for-everyone events into Chatwoot notes | `true`                                      | `CHATWOOT_FORWARD_DELETES=false`              |
| `CHATWOOT_MESSAGE_READ`                 | Sync read state for linked WhatsApp/Chatwoot messages         | `false`                                      | `CHATWOOT_MESSAGE_READ=true`                  |
| `CHATWOOT_MESSAGE_DELETE`               | Delete linked opposite-side messages when deletion is reported | `false`                                     | `CHATWOOT_MESSAGE_DELETE=true`                |

**Documentation:**

- For detailed webhook payload schemas, security implementation, and integration examples, see
  [Webhook Payload Documentation](./docs/webhook-payload.md).
- For the comprehensive Chatwoot integration guide, see
  [Chatwoot Integration Documentation](./docs/chatwoot.md).
- For OAuth deployment and security details, see [MCP OAuth](./docs/mcp-oauth.md).

Run `./whatsapp --help` to see all command-line flags.

## Requirements

### System Requirements

- **Go 1.26.0 or later** (when building from source)
- **FFmpeg** (for media processing)

### Supported Platforms

- Linux (x86_64, ARM64)
- macOS (Intel, Apple Silicon)
- Windows (x86_64; WSL recommended)

### Dependencies (without Docker)

- macOS:
  - `brew install ffmpeg webp`
  - `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
- Linux:
  - `sudo apt update`
  - `sudo apt install ffmpeg webp`
- Windows (WSL is recommended; see [Install WSL](https://docs.microsoft.com/en-us/windows/wsl/install)):
  - Install [FFmpeg](https://www.ffmpeg.org/download.html#build-windows).
  - Install [libwebp](https://developers.google.com/speed/webp/download), then extract it and add its `bin` directory
    to `PATH`.

> **Note**: The `webp` package provides `cwebp` (encoder), `dwebp` (decoder), and `webpmux` (frame extractor) tools.
> FFmpeg is required for media processing. The libwebp tools (`webpmux` + `dwebp`) are used for animated WebP sticker support.

## How to use

### Basic

1. Clone the repository: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`.
2. Open the cloned directory in a terminal.
3. Run `cd src`.
4. Run `go run . rest`.
5. Open `http://localhost:3000`.

### Docker

Docker avoids the need to install Go, FFmpeg, and libwebp directly on the host.

1. Clone the repository: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`.
2. Open the cloned directory in a terminal.
3. Copy the environment file: `cp src/.env.example src/.env`.
4. Run `docker compose up -d --build`.
5. Open `http://localhost:3000`.

### Build your own binary

1. Clone the repository: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`.
2. Open the cloned directory in a terminal.
3. Run `cd src`.
4. Build the binary:
   - Linux and macOS: `go build -o whatsapp`
   - Windows (Command Prompt or PowerShell): `go build -o whatsapp.exe`
5. Start the server:
   - Linux and macOS: `./whatsapp rest`
   - Windows: `.\whatsapp.exe rest`
6. Open `http://localhost:3000` in a browser.

Run `./whatsapp --help` (or `.\whatsapp.exe --help` on Windows) to see all flags.

### Cross-compile for Raspberry Pi (ARM)

To build for a Raspberry Pi or another ARM device without a C toolchain (CGO), use the `purego` build tag. This
selects a pure-Go SQLite implementation.

1. Clone the repository: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`.
2. Open the cloned directory in a terminal.
3. Run `cd src`.
4. **Build for Raspberry Pi Zero / 1 (ARMv6):**

   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -tags purego -o whatsapp-armv6
   ```

5. **Build for Raspberry Pi 2 / 3 / 4 (ARMv7 32-bit):**

   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -tags purego -o whatsapp-armv7
   ```

6. Transfer the binary to your Pi, give it execution permission (`chmod +x`), and run it:
   - If you built ARMv6: `./whatsapp-armv6 rest`
   - If you built ARMv7: `./whatsapp-armv7 rest`

### MCP Server (Model Context Protocol)

MCP is not a separate mode or process — it's served by the REST server itself. Whenever `./whatsapp rest` is
running, the MCP endpoint is available at `http://<host>:<port><base-path>/mcp` (default
`http://localhost:3000/mcp`) using the streamable HTTP transport. Disable it with `MCP_ENABLED=false` or
`--mcp-enabled=false` (default: enabled).

#### Available MCP Tools

There are five consolidated tools; agents choose behavior through a `type`/`action` argument instead of one tool per
operation:

| Tool               | `type` / `action` values                                                                                                                                     |
|--------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `whatsapp_send`    | `text`, `image`, `video`, `audio`, `document`, `sticker`, `location`, `contact`, `poll`, `link`, `forward`                                                    |
| `whatsapp_message` | `react`, `edit`, `revoke`, `delete`, `mark_read`, `mark_played`, `star`, `unstar`, `download_media`                                                           |
| `whatsapp_chat`    | `list_chats`, `list_contacts`, `get_messages`, `archive`                                                                                                      |
| `whatsapp_group`   | `create`, `join_with_link`, `leave`, `info`, `participants`, `add_participants`, `remove_participants`, `promote`, `demote`, `invite_link`, `set_name`, `set_topic`, `set_settings`, `join_requests`, `manage_join_requests` |
| `whatsapp_app`     | `status`, `login_qr`, `login_code`, `logout`, `reconnect`                                                                                                     |

#### Device selection

For multi-device deployments, the `X-Device-Id` header on the MCP client connection selects the device used by
every tool call on that connection. If omitted, it falls back to the default device, just like REST. Any individual
call can override it with an optional `device_id` argument.

### MCP Configuration

Point your MCP client at the `/mcp` endpoint. It inherits the REST server's Basic Auth, so include the same
`Authorization` header your REST calls use:

```json
{
  "mcpServers": {
    "whatsapp": {
      "url": "http://localhost:3000/mcp",
      "headers": {
        "Authorization": "Basic dXNlcjpzZWNyZXQ=",
        "X-Device-Id": "628123456789"
      }
    }
  }
}
```

`headers` is optional: include `Authorization` only when Basic Auth is configured, and `X-Device-Id` only for
multi-device setups.

#### OAuth for remote MCP clients

OAuth 2.1 is available for remote clients that cannot attach a Basic Auth header. It is disabled by default. A minimal
configuration is:

```env
APP_BASIC_AUTH=admin:replace-with-a-strong-password
MCP_ENABLED=true
MCP_OAUTH_ENABLED=true
MCP_OAUTH_ISSUER_URL=https://gowa.example.com
```

When OAuth is enabled, `/mcp` accepts either a Bearer token or the configured Basic Auth credentials. OAuth does not
authenticate REST or UI routes. See [MCP OAuth](./docs/mcp-oauth.md) for client setup, reverse-proxy requirements,
subpath behavior, and the security model.

#### Migrating from the standalone MCP mode

- `./whatsapp mcp` → `./whatsapp rest` (MCP is now included automatically).
- `http://localhost:8080/sse` → `http://localhost:3000/mcp`.
- 40 granular tools → 5 consolidated tools (agents choose actions through the `type`/`action` field).

### Production REST Server (Docker)

Using Docker Hub:

```bash
docker volume create whatsapp-storages
docker volume create whatsapp-statics
docker run --detach \
  --publish 3000:3000 \
  --name whatsapp \
  --restart always \
  --volume whatsapp-storages:/app/storages \
  --volume whatsapp-statics:/app/statics \
  aldinokemal2104/go-whatsapp-web-multidevice \
  rest --autoreply="Don't reply to this message, please"
```

Using GitHub Container Registry:

```bash
docker volume create whatsapp-storages
docker volume create whatsapp-statics
docker run --detach \
  --publish 3000:3000 \
  --name whatsapp \
  --restart always \
  --volume whatsapp-storages:/app/storages \
  --volume whatsapp-statics:/app/statics \
  ghcr.io/aldinokemal/go-whatsapp-web-multidevice \
  rest --autoreply="Don't reply to this message, please"
```

### Production REST Server (Docker Compose)

Create a `docker-compose.yml` file with one of the following configurations.

Using Docker Hub:

```yml
services:
  whatsapp:
    image: aldinokemal2104/go-whatsapp-web-multidevice
    container_name: whatsapp
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - whatsapp_storages:/app/storages
      - whatsapp_statics:/app/statics
    command:
      - rest
      - --basic-auth=admin:admin
      - --port=3000
      - --debug=true
      - --os=Chrome
      - --account-validation=false

volumes:
  whatsapp_storages:
  whatsapp_statics:
```

Using GitHub Container Registry:

```yml
services:
  whatsapp:
    image: ghcr.io/aldinokemal/go-whatsapp-web-multidevice
    container_name: whatsapp
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - whatsapp_storages:/app/storages
      - whatsapp_statics:/app/statics
    command:
      - rest
      - --basic-auth=admin:admin
      - --port=3000
      - --debug=true
      - --os=Chrome
      - --account-validation=false

volumes:
  whatsapp_storages:
  whatsapp_statics:
```

Using environment variables with Docker Hub:

```yml
services:
  whatsapp:
    image: aldinokemal2104/go-whatsapp-web-multidevice
    container_name: whatsapp
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - whatsapp_storages:/app/storages
      - whatsapp_statics:/app/statics
    environment:
      - APP_BASIC_AUTH=admin:admin
      - APP_PORT=3000
      - APP_DEBUG=true
      - APP_OS=Chrome
      - WHATSAPP_ACCOUNT_VALIDATION=false

volumes:
  whatsapp_storages:
  whatsapp_statics:
```

Using environment variables with GitHub Container Registry:

```yml
services:
  whatsapp:
    image: ghcr.io/aldinokemal/go-whatsapp-web-multidevice
    container_name: whatsapp
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - whatsapp_storages:/app/storages
      - whatsapp_statics:/app/statics
    environment:
      - APP_BASIC_AUTH=admin:admin
      - APP_PORT=3000
      - APP_DEBUG=true
      - APP_OS=Chrome
      - WHATSAPP_ACCOUNT_VALIDATION=false

volumes:
  whatsapp_storages:
  whatsapp_statics:
```

Start the selected stack with `docker compose up -d`.

### Production Server (Binary)

Download a binary from the [releases page](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases), then
run it with the `rest` subcommand.

You may also fork or modify the source code.

## Current API

### MCP (Model Context Protocol) API

- Served at `/mcp` by the REST server using streamable HTTP whenever `MCP_ENABLED` is true. With `APP_BASE_PATH`
  set, the route is `<base-path>/mcp`.
- Available tools are listed in the "Available MCP Tools" section above.
- Compatible with MCP-enabled AI tools and agents.

### HTTP REST API

- Check [docs/openapi.yaml](./docs/openapi.yaml) for detailed API specifications.
- Use [Swagger Editor](https://editor.swagger.io) to visualize the API.
- Generate HTTP clients using [openapi-generator](https://openapi-generator.tech/#try).

| Status   | Operation                              | Method | URL                                 |
|----------|----------------------------------------|--------|-------------------------------------|
| ✅       | Health Check                           | GET    | /health                             |
| ✅       | List Devices                           | GET    | /devices                            |
| ✅       | Add Device                             | POST   | /devices                            |
| ✅       | Get Device Info                        | GET    | /devices/:device_id                 |
| ✅       | Remove Device                          | DELETE | /devices/:device_id                 |
| ✅       | Login Device (QR)                      | GET    | /devices/:device_id/login           |
| ✅       | Login Device (Code)                    | POST   | /devices/:device_id/login/code      |
| ✅       | Logout Device                          | POST   | /devices/:device_id/logout          |
| ✅       | Reconnect Device                       | POST   | /devices/:device_id/reconnect       |
| ✅       | Get Device Status                      | GET    | /devices/:device_id/status          |
| ✅       | Get Device Webhook                     | GET    | /devices/:device_id/webhook         |
| ✅       | Set Device Webhook                     | PATCH  | /devices/:device_id/webhook         |
| ✅       | Log In with QR Code                    | GET    | /app/login                          |
| ✅       | Log In with Pairing Code               | GET    | /app/login-with-code                |
| ✅       | Passkey Pairing Status                 | GET    | /app/passkey                        |
| ✅       | Passkey Pairing Response               | POST   | /app/passkey/response               |
| ✅       | Confirm Passkey Pairing                | POST   | /app/passkey/confirm                |
| ✅       | Logout                                 | GET    | /app/logout                         |
| ✅       | Reconnect                              | GET    | /app/reconnect                      |
| ✅       | Devices                                | GET    | /app/devices                        |
| ✅       | Connection Status                      | GET    | /app/status                         |
| ✅       | App Info (version, limits)             | GET    | /app/info                           |
| ✅       | User Info                              | GET    | /user/info                          |
| ✅       | User Avatar                            | GET    | /user/avatar                        |
| ✅       | Change User Avatar                     | POST   | /user/avatar                        |
| ✅       | Change User Push Name                  | POST   | /user/pushname                      |
| ✅       | List My Groups*                        | GET    | /user/my/groups                     |
| ✅       | List My Newsletters                    | GET    | /user/my/newsletters                |
| ✅       | Get My Privacy Settings                | GET    | /user/my/privacy                    |
| ✅       | List My Contacts                       | GET    | /user/my/contacts                   |
| ✅       | Check WhatsApp User                    | GET    | /user/check                         |
| ✅       | Get Business Profile                   | GET    | /user/business-profile              |
| ✅       | Send Message                           | POST   | /send/message                       |
| ✅       | Send Image                             | POST   | /send/image                         |
| ✅       | Send Audio                             | POST   | /send/audio                         |
| ✅       | Send File                              | POST   | /send/file                          |
| ✅       | Send Video                             | POST   | /send/video                         |
| ✅       | Send Sticker                           | POST   | /send/sticker                       |
| ✅       | Send Contact                           | POST   | /send/contact                       |
| ✅       | Send Link                              | POST   | /send/link                          |
| ✅       | Send Location                          | POST   | /send/location                      |
| ✅       | Send Poll / Vote                       | POST   | /send/poll                          |
| ✅       | Send Presence                          | POST   | /send/presence                      |
| ✅       | Send Chat Presence (Typing Indicator)  | POST   | /send/chat-presence                 |
| ✅       | Revoke Message                         | POST   | /message/:message_id/revoke         |
| ✅       | React Message                          | POST   | /message/:message_id/reaction       |
| ✅       | Delete Message                         | POST   | /message/:message_id/delete         |
| ✅       | Edit Message                           | POST   | /message/:message_id/update         |
| ✅       | Mark Message as Read                   | POST   | /message/:message_id/read           |
| ✅       | Mark Audio Message as Played           | POST   | /message/:message_id/played         |
| ✅       | Star Message                           | POST   | /message/:message_id/star           |
| ✅       | Unstar Message                         | POST   | /message/:message_id/unstar         |
| ✅       | Forward Message                        | POST   | /message/:message_id/forward        |
| ✅       | Download Message Media                 | GET    | /message/:message_id/download       |
| ✅       | Reject Call                            | POST   | /call/reject                        |
| ✅       | Join Group with Link                   | POST   | /group/join-with-link               |
| ✅       | Get Group Info from Link               | GET    | /group/info-from-link               |
| ✅       | Get Group Info                         | GET    | /group/info                         |
| ✅       | Leave Group                            | POST   | /group/leave                        |
| ✅       | Create Group                           | POST   | /group                              |
| ✅       | List Group Participants                | GET    | /group/participants                 |
| ✅       | Add Group Participants                 | POST   | /group/participants                 |
| ✅       | Remove Group Participants              | POST   | /group/participants/remove          |
| ✅       | Promote Group Participants             | POST   | /group/participants/promote         |
| ✅       | Demote Group Participants              | POST   | /group/participants/demote          |
| ✅       | Export Group Participants (CSV)        | GET    | /group/participants/export          |
| ✅       | List Group Join Requests               | GET    | /group/participant-requests         |
| ✅       | Approve Group Join Requests            | POST   | /group/participant-requests/approve |
| ✅       | Reject Group Join Requests             | POST   | /group/participant-requests/reject  |
| ✅       | Set Group Photo                        | POST   | /group/photo                        |
| ✅       | Set Group Name                         | POST   | /group/name                         |
| ✅       | Lock or Unlock Group Settings          | POST   | /group/locked                       |
| ✅       | Set Group Announcement Mode            | POST   | /group/announce                     |
| ✅       | Set Group Topic                        | POST   | /group/topic                        |
| ✅       | Get Group Invite Link                  | GET    | /group/invite-link                  |
| ✅       | Unfollow Newsletter                    | POST   | /newsletter/unfollow                |
| ✅       | Get Newsletter Messages                | GET    | /newsletter/messages                |
| ✅       | Download Newsletter Message Media      | GET    | /newsletter/messages/{server_id}/download |
| ✅       | Get Chat List                          | GET    | /chats                              |
| ✅       | Get Chat Messages                      | GET    | /chat/:chat_jid/messages            |
| ✅       | Pin Chat                               | POST   | /chat/:chat_jid/pin                 |
| ✅       | Archive Chat                           | POST   | /chat/:chat_jid/archive             |
| ✅       | Set Disappearing Messages              | POST   | /chat/:chat_jid/disappearing        |
| ✅       | Chatwoot Sync History                  | POST   | /chatwoot/sync                      |
| ✅       | Chatwoot Sync Status                   | GET    | /chatwoot/sync/status               |
| ✅       | List Chatwoot Configurations           | GET    | /chatwoot/configs                   |
| ✅       | Get Device Chatwoot Configuration      | GET    | /devices/:device_id/chatwoot/config |
| ✅       | Set Device Chatwoot Configuration      | PUT    | /devices/:device_id/chatwoot/config |
| ✅       | Delete Device Chatwoot Configuration   | DELETE | /devices/:device_id/chatwoot/config |
| ✅       | Chatwoot Reply Webhook                 | POST   | /chatwoot/webhook                   |
| ✅       | Device Chatwoot Reply Webhook          | POST   | /chatwoot/webhook/:device_id        |

`✅` = available. `*` = has known limitations; see the notes below.

**Notes:**

- `*List My Groups`: Returns a maximum of 500 groups because of a WhatsApp protocol limitation. WhatsApp's servers,
  not this API, enforce the limit. See the [whatsmeow source](https://github.com/tulir/whatsmeow/blob/main/group.go)
  for details.
- `/health` is public and always registered at the root path, even when `APP_BASE_PATH` is set.
- Chatwoot routes are registered only when `CHATWOOT_ENABLED=true`.

## User Interface

### MCP UI

- Set up MCP (tested in Cursor)
  ![Setup MCP](https://i.ibb.co/vCg4zNWt/mcpsetup.png)
- Test MCP
  ![Test MCP](https://i.ibb.co/B2LX38DW/mcptest.png)
- Successful MCP setup
  ![Success MCP](https://i.ibb.co/1fCx0Myc/mcpsuccess.png)

### Web dashboard (gowa-ui)

The dashboard lives in its own repository: [aldinokemal/gowa-ui](https://github.com/aldinokemal/gowa-ui). Each
gowa-ui release publishes a single self-contained `gowa-ui.html`; the server downloads the latest release at
startup (and every `APP_UI_UPDATE_INTERVAL`, which defaults to 3h), verifies its SHA-256 digest, caches it under
`storages/ui/`, and serves it at `/` behind Basic Auth.

| Setting                  | Default              | Purpose                                                             |
|--------------------------|----------------------|---------------------------------------------------------------------|
| `APP_UI_ENABLED`         | `true`               | Serve the dashboard at `/`; `false` returns a JSON banner (API-only) |
| `APP_UI_AUTO_UPDATE`     | `true`               | Download/refresh from GitHub; disable for air-gapped deployments     |
| `APP_UI_REPO`            | `aldinokemal/gowa-ui` | Repository the updater follows—always its latest release, not a version pin |
| `APP_UI_ASSET_NAME`      | `gowa-ui.html`       | Release asset filename to download                                   |
| `APP_UI_UPDATE_INTERVAL` | `3h`                 | How often to check `releases/latest`                                 |
| `APP_UI_GITHUB_TOKEN`    | (empty)              | Optional token to raise the GitHub API rate limit                    |
| `APP_UI_ASSET_SHA256`    | (empty)              | Supply-chain pin: refuse any dashboard whose SHA-256 differs         |

Trust model: the release digest proves the download matches what GitHub advertises, not who published it.
Operators who audit a specific build can pin it with `APP_UI_ASSET_SHA256` (each release ships a `.sha256`
asset—this is the only setting that pins an exact build), point `APP_UI_REPO` at a fork they control
(the updater still tracks that repo's latest release), or pre-seed the cache and disable auto-update entirely.

Air-gapped servers: place a downloaded `gowa-ui.html` at `storages/ui/index.html` and set
`APP_UI_AUTO_UPDATE=false`. The dashboard can also be self-hosted anywhere static and pointed at this
server's URL (see the gowa-ui README).

### macOS Note

If you see `invalid flag in pkg-config --cflags: -Xpreprocessor`, run:

```bash
export CGO_CFLAGS_ALLOW="-Xpreprocessor"
```

## Important

- This project is unofficial and not affiliated with WhatsApp.
- Use the official WhatsApp Business Platform when you require a supported, production-grade integration.
