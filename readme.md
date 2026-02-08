<!-- markdownlint-disable MD041 -->
<!-- markdownlint-disable-next-line MD033 -->
<div align="center">
  <!-- markdownlint-disable-next-line MD033 -->
  <img src="src/views/assets/gowa.svg" alt="GoWA Logo" width="200" height="200">

## Golang WhatsApp - Built with Go for efficient memory use

</div>

[![Patreon](https://img.shields.io/badge/Support%20on-Patreon-orange.svg)](https://www.patreon.com/c/aldinokemal)
**If you're using this tools to generate income, consider supporting its development by becoming a Patreon member!**
Your support helps ensure the library stays maintained and receives regular updates!
___

![release version](https://img.shields.io/github/v/release/aldinokemal/go-whatsapp-web-multidevice)
![Build Image](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/build-docker-image.yaml/badge.svg)
![Binary Release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/actions/workflows/release.yml/badge.svg)

## Support for `ARM` & `AMD` Architecture along with `MCP` Support

Download:

- [Release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases/latest)
- [Docker Hub](https://hub.docker.com/r/aldinokemal2104/go-whatsapp-web-multidevice/tags)
- [GitHub Container Registry](https://github.com/aldinokemal/go-whatsapp-web-multidevice/pkgs/container/go-whatsapp-web-multidevice)

## Support n8n package (n8n.io)

- [n8n package](https://www.npmjs.com/package/@aldinokemal2104/n8n-nodes-gowa)
- Go to Settings -> Community Nodes -> Input `@aldinokemal2104/n8n-nodes-gowa` -> Install

## Breaking Changes

- `v6`
  - For REST mode, you need to run `<binary> rest` instead of `<binary>`
    - for example: `./whatsapp rest` instead of ~~./whatsapp~~
  - For MCP mode, you need to run `<binary> mcp`
    - for example: `./whatsapp mcp`
- `v7`
  - Starting version 7.x we are using goreleaser to build the binary, so you can download the binary
      from [release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases/latest)
- `v8`
  - **Multi-device support**: You can now connect and manage multiple WhatsApp accounts simultaneously in a single
      server instance
  - **New Device Management API**: New endpoints under `/devices` for managing multiple devices
  - **Device scoping required**: All device-scoped REST API calls now require either:
    - `X-Device-Id` header, or
    - `device_id` query parameter
    - If only one device is registered, it will be used as the default
  - **WebSocket device scoping**: Connect to `/ws?device_id=<id>` to scope WebSocket to a specific device
  - **Webhook payload changes**: All webhook payloads now include a top-level `device_id` field identifying which
      device received the event:

        ```json
        {
          "event": "message",
          "device_id": "628123456789@s.whatsapp.net",
          "payload": { ... }
        }
        ```

## Feature

- Send WhatsApp message via http API, [docs/openapi.yml](./docs/openapi.yaml) for more details
- **MCP (Model Context Protocol) Server Support** - Integrate with AI agents and tools using standardized protocol
- Mention someone
  - `@phoneNumber`
  - example: `Hello @628974812XXXX, @628974812XXXX`
- **Ghost Mentions (Mention All)** - Mention group participants without showing `@phone` in message text
  - Pass phone numbers in `mentions` field to mention users without visible `@` in message
  - Use special keyword `@everyone` to automatically mention ALL group participants
  - UI checkbox available in Send Message modal for groups
- Post Whatsapp Status
- **Send Stickers** - Automatically converts images to WebP sticker format
  - Supports JPG, JPEG, PNG, WebP, and GIF formats
  - Automatic resizing to 512x512 pixels
  - Preserves transparency for PNG images
  - **Animated WebP stickers** are supported but must meet WhatsApp requirements:
    - Must be exactly **512x512 pixels**
    - Must be under **500KB** file size
    - Maximum **10 seconds** duration
    - If your animated sticker doesn't meet these requirements, please resize it before uploading using tools like [ezgif.com](https://ezgif.com/resize)
- Compress image before send
- Compress video before send
- Change OS name become your app (it's the device name when connect via mobile)
  - `--os=Chrome` or `--os=MyApplication`
- Basic Auth (able to add multi credentials)
  - `--basic-auth=kemal:secret,toni:password,userName:secretPassword`, or you can simplify
  - `-b=kemal:secret,toni:password,userName:secretPassword`
- Subpath deployment support
  - `--base-path="/gowa"` (allows deployment under a specific path like `/gowa/sub/path`)
- Customizable port and debug mode
  - `--port 8000`
  - `--debug true`
- Auto reply message
  - `--autoreply="Don't reply this message"`
- Auto mark read incoming messages
  - `--auto-mark-read=true` (automatically marks incoming messages as read)
- Auto download media from incoming messages
  - `--auto-download-media=false` (disable automatic media downloads, default: `true`)
- Auto reject incoming calls
  - `--auto-reject-call=true` or `WHATSAPP_AUTO_REJECT_CALL=true` (see [Webhook Payload](./docs/webhook-payload.md#call-events) for call events)
- Webhook for received message
  - `--webhook="http://yourwebhook.site/handler"`, or you can simplify
  - `-w="http://yourwebhook.site/handler"`
  - for more detail, see [Webhook Payload Documentation](./docs/webhook-payload.md)
- Webhook Secret
  Our webhook will be sent to you with an HMAC header and a sha256 default key `secret`.

  You may modify this by using the option below:
  - `--webhook-secret="secret"`
- **Webhook Payload Documentation**
  For detailed webhook payload schemas, security implementation, and integration examples,
  see [Webhook Payload Documentation](./docs/webhook-payload.md)
- **Webhook Event Filtering**
  You can filter which events are forwarded to your webhook using:
  - `--webhook-events="message,message.ack"` (comma-separated list)
  - Or environment variable: `WHATSAPP_WEBHOOK_EVENTS=message,message.ack`

  **Available Webhook Events:**

  | Event                | Description                                   |
  |----------------------|-----------------------------------------------|
  | `message`            | Text, media, contact, location messages       |
  | `message.reaction`   | Emoji reactions to messages                   |
  | `message.revoked`    | Deleted/revoked messages                      |
  | `message.edited`     | Edited messages                               |
  | `message.ack`        | Delivery and read receipts                    |
  | `message.deleted`    | Messages deleted for the user                 |
  | `group.participants` | Group member join/leave/promote/demote events |
  | `group.joined`       | You were added to a group                     |
  | `newsletter.joined`  | You subscribed to a newsletter/channel        |
  | `newsletter.left`    | You unsubscribed from a newsletter            |
  | `newsletter.message` | New message(s) posted in a newsletter         |
  | `newsletter.mute`    | Newsletter mute setting changed               |
  | `call.offer`         | Incoming call received                        |

  If not configured (empty), all events will be forwarded.
- **Webhook TLS Configuration**

  If you encounter TLS certificate verification errors when using webhooks (e.g., with Cloudflare tunnels or self-signed
  certificates):

  ```
  tls: failed to verify certificate: x509: certificate signed by unknown authority
  ```

  You can disable TLS certificate verification using:
  - `--webhook-insecure-skip-verify=true`
  - Or environment variable: `WHATSAPP_WEBHOOK_INSECURE_SKIP_VERIFY=true`

  **Security Warning**: This option disables TLS certificate verification and should only be used in:
  - Development/testing environments
  - Cloudflare tunnels (which provide their own security layer)
  - Internal networks with self-signed certificates

  **For production environments**, it's strongly recommended to use proper SSL certificates (e.g., Let's Encrypt)
  instead of disabling verification.

## Configuration

You can configure the application using either command-line flags (shown above) or environment variables. Configuration
can be set in three ways (in order of priority):

1. Command-line flags (highest priority)
2. Environment variables
3. `.env` file (lowest priority)

### Environment Variables

You can configure the application using environment variables. Configuration can be set in three ways (in order of
priority):

1. Command-line flags (highest priority)
2. Environment variables
3. `.env` file (lowest priority)

To use environment variables:

1. Copy `.env.example` to `.env` in your project root (`cp src/.env.example src/.env`)
2. Modify the values in `.env` according to your needs
3. Or set the same variables as system environment variables

#### Available Environment Variables

| Variable                                | Description                                                   | Default                                      | Example                                       |
|-----------------------------------------|---------------------------------------------------------------|----------------------------------------------|-----------------------------------------------|
| `APP_PORT`                              | Application port                                              | `3000`                                       | `APP_PORT=8080`                               |
| `APP_HOST`                              | Host address to bind the server                               | `0.0.0.0`                                    | `APP_HOST=127.0.0.1`                          |
| `APP_DEBUG`                             | Enable debug logging                                          | `false`                                      | `APP_DEBUG=true`                              |
| `APP_OS`                                | OS name (device name in WhatsApp)                             | `Chrome`                                     | `APP_OS=MyApp`                                |
| `APP_BASIC_AUTH`                        | Basic authentication credentials                              | -                                            | `APP_BASIC_AUTH=user1:pass1,user2:pass2`      |
| `APP_BASE_PATH`                         | Base path for subpath deployment                              | -                                            | `APP_BASE_PATH=/gowa`                         |
| `APP_TRUSTED_PROXIES`                   | Trusted proxy IP ranges for reverse proxy                     | -                                            | `APP_TRUSTED_PROXIES=0.0.0.0/0`               |
| `DB_URI`                                | Database connection URI                                       | `file:storages/whatsapp.db?_foreign_keys=on` | `DB_URI=postgres://user:pass@host/db`         |
| `WHATSAPP_AUTO_REPLY`                   | Auto-reply message                                            | -                                            | `WHATSAPP_AUTO_REPLY="Auto reply message"`    |
| `WHATSAPP_AUTO_MARK_READ`               | Auto-mark incoming messages as read                           | `false`                                      | `WHATSAPP_AUTO_MARK_READ=true`                |
| `WHATSAPP_AUTO_DOWNLOAD_MEDIA`          | Auto-download media from incoming messages                    | `true`                                       | `WHATSAPP_AUTO_DOWNLOAD_MEDIA=false`          |
| `WHATSAPP_WEBHOOK`                      | Webhook URL(s) for events (comma-separated)                   | -                                            | `WHATSAPP_WEBHOOK=https://webhook.site/xxx`   |
| `WHATSAPP_WEBHOOK_SECRET`               | Webhook secret for validation                                 | `secret`                                     | `WHATSAPP_WEBHOOK_SECRET=super-secret-key`    |
| `WHATSAPP_WEBHOOK_INSECURE_SKIP_VERIFY` | Skip TLS verification for webhooks (insecure)                 | `false`                                      | `WHATSAPP_WEBHOOK_INSECURE_SKIP_VERIFY=true`  |
| `WHATSAPP_WEBHOOK_EVENTS`               | Whitelist of events to forward (comma-separated, empty = all) | -                                            | `WHATSAPP_WEBHOOK_EVENTS=message,message.ack` |
| `WHATSAPP_ACCOUNT_VALIDATION`           | Enable account validation                                     | `true`                                       | `WHATSAPP_ACCOUNT_VALIDATION=false`           |
| `CHATWOOT_ENABLED`                      | Enable Chatwoot integration                                   | `false`                                      | `CHATWOOT_ENABLED=true`                       |
| `CHATWOOT_URL`                          | Chatwoot instance URL                                         | -                                            | `CHATWOOT_URL=https://app.chatwoot.com`       |
| `CHATWOOT_API_TOKEN`                    | Chatwoot API access token                                     | -                                            | `CHATWOOT_API_TOKEN=your-api-token`           |
| `CHATWOOT_ACCOUNT_ID`                   | Chatwoot account ID                                           | -                                            | `CHATWOOT_ACCOUNT_ID=12345`                   |
| `CHATWOOT_INBOX_ID`                     | Chatwoot inbox ID                                             | -                                            | `CHATWOOT_INBOX_ID=67890`                     |
| `CHATWOOT_DEVICE_ID`                    | WhatsApp device ID for Chatwoot (multi-device setup)          | -                                            | `CHATWOOT_DEVICE_ID=628xxx@s.whatsapp.net`    |
| `CHATWOOT_IMPORT_MESSAGES`              | Enable message history sync to Chatwoot                       | `false`                                      | `CHATWOOT_IMPORT_MESSAGES=true`               |
| `CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES`   | Days of history to import                                     | `3`                                          | `CHATWOOT_DAYS_LIMIT_IMPORT_MESSAGES=7`       |

**Documentation:**

- For detailed webhook payload schemas, security implementation, and integration examples, see [Webhook Payload Documentation](./docs/webhook-payload.md)
- For comprehensive Chatwoot integration guide, see [Chatwoot Integration Documentation](./docs/chatwoot.md)

Note: Command-line flags will override any values set in environment variables or `.env` file.

- For more command `./whatsapp --help`

## Requirements

### System Requirements

- **Go 1.24.0 or higher** (for building from source)
- **FFmpeg** (for media processing)

### Platform Support

- Linux (x86_64, ARM64)
- macOS (Intel, Apple Silicon)
- Windows (x86_64) - WSL recommended

### Dependencies (without docker)

- Mac OS:
  - `brew install ffmpeg webp`
  - `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`
- Linux:
  - `sudo apt update`
  - `sudo apt install ffmpeg webp`
- Windows (not recommended, prefer using [WSL](https://docs.microsoft.com/en-us/windows/wsl/install)):
  - Install ffmpeg: [download here](https://www.ffmpeg.org/download.html#build-windows)
  - Install libwebp: [download here](https://developers.google.com/speed/webp/download) (extract and add `bin` folder to PATH)
  - Add both to [environment variable](https://www.google.com/search?q=windows+add+to+environment+path)

> **Note**: The `webp` package provides `cwebp` (encoder), `dwebp` (decoder), and `webpmux` (frame extractor) tools.
> FFmpeg is required for media processing. The libwebp tools (`webpmux` + `dwebp`) are used for animated WebP sticker support.

## How to use

### Basic

1. Clone this repo: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `cd src`
4. run `go run . rest` (for REST API mode)
5. Open `http://localhost:3000`

### Docker (you don't need to install in required)

1. Clone this repo: `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `docker-compose up -d --build`
4. open `http://localhost:3000`

### Build your own binary

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `cd src`
4. run
    1. Linux & MacOS: `go build -o whatsapp`
    2. Windows (CMD / PowerShell): `go build -o whatsapp.exe`
5. run
    1. Linux & MacOS: `./whatsapp rest` (for REST API mode)
        1. run `./whatsapp --help` for more detail flags
    2. Windows: `.\whatsapp.exe rest` (for REST API mode)
        1. run `.\whatsapp.exe --help` for more detail flags
6. open `http://localhost:3000` in browser

### MCP Server (Model Context Protocol)

This application can also run as an MCP server, allowing AI agents and tools to interact with WhatsApp through a
standardized protocol.

1. Clone this repo `git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`
2. Open the folder that was cloned via cmd/terminal.
3. run `cd src`
4. run `go run . mcp` or build the binary and run `./whatsapp mcp`
5. The MCP server will start on `http://localhost:8080` by default

#### MCP Server Options

- `--host localhost` - Set the host for MCP server (default: localhost)
- `--port 8080` - Set the port for MCP server (default: 8080)

#### Available MCP Tools

The WhatsApp MCP server provides comprehensive tools for AI agents to interact with WhatsApp through a standardized
protocol. Below is the complete list of available tools:

##### **📱 Connection Management**

- `whatsapp_connection_status` - Check whether the WhatsApp client is connected and logged in
- `whatsapp_login_qr` - Initiate QR code based login flow with image output
- `whatsapp_login_with_code` - Generate pairing code for multi-device login using phone number
- `whatsapp_logout` - Sign out the current WhatsApp session
- `whatsapp_reconnect` - Attempt to reconnect to WhatsApp using stored session

##### **💬 Messaging & Communication**

- `whatsapp_send_text` - Send text messages with reply and forwarding support
- `whatsapp_send_contact` - Send contact cards with name and phone number
- `whatsapp_send_link` - Send links with custom captions
- `whatsapp_send_location` - Send location coordinates (latitude/longitude)
- `whatsapp_send_image` - Send images with captions, compression, and view-once options
- `whatsapp_send_sticker` - Send stickers with automatic WebP conversion (supports JPG/PNG/GIF)

##### **📋 Chat & Contact Management**

- `whatsapp_list_contacts` - Retrieve all contacts in your WhatsApp account
- `whatsapp_list_chats` - Get recent chats with pagination and search filters
- `whatsapp_get_chat_messages` - Fetch messages from specific chats with time/media filtering
- `whatsapp_download_message_media` - Download images/videos from messages
- `whatsapp_archive_chat` - Archive or unarchive a chat conversation

##### **👥 Group Management**

- `whatsapp_group_create` - Create new groups with optional initial participants
- `whatsapp_group_join_via_link` - Join groups using invite links
- `whatsapp_group_leave` - Leave groups by group ID
- `whatsapp_group_participants` - List all participants in a group
- `whatsapp_group_manage_participants` - Add, remove, promote, or demote group members
- `whatsapp_group_invite_link` - Get or reset group invite links
- `whatsapp_group_info` - Get detailed group information
- `whatsapp_group_set_name` - Update group display name
- `whatsapp_group_set_topic` - Update group description/topic
- `whatsapp_group_set_locked` - Toggle admin-only group info editing
- `whatsapp_group_set_announce` - Toggle announcement-only mode
- `whatsapp_group_join_requests` - List pending join requests
- `whatsapp_group_manage_join_requests` - Approve or reject join requests

#### MCP Endpoints

- SSE endpoint: `http://localhost:8080/sse`
- Message endpoint: `http://localhost:8080/message`

### MCP Configuration

Make sure you have the MCP server running: `./whatsapp mcp`

For AI tools that support MCP with SSE (like Cursor), add this configuration:

```json
{
  "mcpServers": {
    "whatsapp": {
      "url": "http://localhost:8080/sse"
    }
  }
}
```

### Production Mode REST (docker)

Using Docker Hub:

```bash
docker run --detach --publish=3000:3000 --name=whatsapp --restart=always --volume=$(docker volume create --name=whatsapp):/app/storages aldinokemal2104/go-whatsapp-web-multidevice rest --autoreply="Dont't reply this message please"
```

Using GitHub Container Registry:

```bash
docker run --detach --publish=3000:3000 --name=whatsapp --restart=always --volume=$(docker volume create --name=whatsapp):/app/storages ghcr.io/aldinokemal/go-whatsapp-web-multidevice rest --autoreply="Dont't reply this message please"
```

### Production Mode REST (docker compose)

create `docker-compose.yml` file with the following configuration:

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
      - whatsapp:/app/storages
    command:
      - rest
      - --basic-auth=admin:admin
      - --port=3000
      - --debug=true
      - --os=Chrome
      - --account-validation=false

volumes:
  whatsapp:
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
      - whatsapp:/app/storages
    command:
      - rest
      - --basic-auth=admin:admin
      - --port=3000
      - --debug=true
      - --os=Chrome
      - --account-validation=false

volumes:
  whatsapp:
```

or with env file (Docker Hub):

```yml
services:
  whatsapp:
    image: aldinokemal2104/go-whatsapp-web-multidevice
    container_name: whatsapp
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - whatsapp:/app/storages
    environment:
      - APP_BASIC_AUTH=admin:admin
      - APP_PORT=3000
      - APP_DEBUG=true
      - APP_OS=Chrome
      - APP_ACCOUNT_VALIDATION=false

volumes:
  whatsapp:
```

or with env file (GitHub Container Registry):

```yml
services:
  whatsapp:
    image: ghcr.io/aldinokemal/go-whatsapp-web-multidevice
    container_name: whatsapp
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - whatsapp:/app/storages
    environment:
      - APP_BASIC_AUTH=admin:admin
      - APP_PORT=3000
      - APP_DEBUG=true
      - APP_OS=Chrome
      - APP_ACCOUNT_VALIDATION=false

volumes:
  whatsapp:
```

### Production Mode (binary)

- download binary from [release](https://github.com/aldinokemal/go-whatsapp-web-multidevice/releases)

You can fork or edit this source code !

## Current API

### MCP (Model Context Protocol) API

- MCP server provides standardized tools for AI agents to interact with WhatsApp
- Supports Server-Sent Events (SSE) transport
- Available tools: `whatsapp_send_text`, `whatsapp_send_contact`, `whatsapp_send_link`, `whatsapp_send_location`
- Compatible with MCP-enabled AI tools and agents

### HTTP REST API

- [API Specification Document](https://bump.sh/aldinokemal/doc/go-whatsapp-web-multidevice).
- Check [docs/openapi.yml](./docs/openapi.yaml) for detailed API specifications.
- Use [SwaggerEditor](https://editor.swagger.io) to visualize the API.
- Generate HTTP clients using [openapi-generator](https://openapi-generator.tech/#try).

| Feature  | Menu                                   | Method | URL                                 |
|----------|----------------------------------------|--------|-------------------------------------|
| ✅       | List Devices                           | GET    | /devices                            |
| ✅       | Add Device                             | POST   | /devices                            |
| ✅       | Get Device Info                        | GET    | /devices/:device_id                 |
| ✅       | Remove Device                          | DELETE | /devices/:device_id                 |
| ✅       | Login Device (QR)                      | GET    | /devices/:device_id/login           |
| ✅       | Login Device (Code)                    | POST   | /devices/:device_id/login/code      |
| ✅       | Logout Device                          | POST   | /devices/:device_id/logout          |
| ✅       | Reconnect Device                       | POST   | /devices/:device_id/reconnect       |
| ✅       | Get Device Status                      | GET    | /devices/:device_id/status          |
| ✅       | Login with Scan QR                     | GET    | /app/login                          |
| ✅       | Login With Pair Code                   | GET    | /app/login-with-code                |
| ✅       | Logout                                 | GET    | /app/logout                         |
| ✅       | Reconnect                              | GET    | /app/reconnect                      |
| ✅       | Devices                                | GET    | /app/devices                        |
| ✅       | Connection Status                      | GET    | /app/status                         |
| ✅       | User Info                              | GET    | /user/info                          |
| ✅       | User Avatar                            | GET    | /user/avatar                        |
| ✅       | User Change Avatar                     | POST   | /user/avatar                        |
| ✅       | User Change PushName                   | POST   | /user/pushname                      |
| ✅       | User My Groups*                        | GET    | /user/my/groups                     |
| ✅       | User My Newsletter                     | GET    | /user/my/newsletters                |
| ✅       | User My Privacy Setting                | GET    | /user/my/privacy                    |
| ✅       | User My Contacts                       | GET    | /user/my/contacts                   |
| ✅       | User Check                             | GET    | /user/check                         |
| ✅       | User Business Profile                  | GET    | /user/business-profile              |
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
| ✅       | Read Message (DM)                      | POST   | /message/:message_id/read           |
| ✅       | Star Message                           | POST   | /message/:message_id/star           |
| ✅       | Unstar Message                         | POST   | /message/:message_id/unstar         |
| ✅       | Download Message Media                 | GET    | /message/:message_id/download       |
| ✅       | Join Group With Link                   | POST   | /group/join-with-link               |
| ✅       | Group Info From Link                   | GET    | /group/info-from-link               |
| ✅       | Group Info                             | GET    | /group/info                         |
| ✅       | Leave Group                            | POST   | /group/leave                        |
| ✅       | Create Group                           | POST   | /group                              |
| ✅       | List Participants in Group             | GET    | /group/participants                 |
| ✅       | Add Participants in Group              | POST   | /group/participants                 |
| ✅       | Remove Participant in Group            | POST   | /group/participants/remove          |
| ✅       | Promote Participant in Group           | POST   | /group/participants/promote         |
| ✅       | Demote Participant in Group            | POST   | /group/participants/demote          |
| ✅       | Export Group Participants (CSV)        | GET    | /group/participants/export          |
| ✅       | List Requested Participants in Group   | GET    | /group/participant-requests         |
| ✅       | Approve Requested Participant in Group | POST   | /group/participant-requests/approve |
| ✅       | Reject Requested Participant in Group  | POST   | /group/participant-requests/reject  |
| ✅       | Set Group Photo                        | POST   | /group/photo                        |
| ✅       | Set Group Name                         | POST   | /group/name                         |
| ✅       | Set Group Locked                       | POST   | /group/locked                       |
| ✅       | Set Group Announce                     | POST   | /group/announce                     |
| ✅       | Set Group Topic                        | POST   | /group/topic                        |
| ✅       | Get Group Invite Link                  | GET    | /group/invite-link                  |
| ✅       | Unfollow Newsletter                    | POST   | /newsletter/unfollow                |
| ✅       | Get Chat List                          | GET    | /chats                              |
| ✅       | Get Chat Messages                      | GET    | /chat/:chat_jid/messages            |
| ✅       | Label Chat                             | POST   | /chat/:chat_jid/label               |
| ✅       | Pin Chat                               | POST   | /chat/:chat_jid/pin                 |
| ✅       | Archive Chat                           | POST   | /chat/:chat_jid/archive             |
| ✅       | Set Disappearing Messages              | POST   | /chat/:chat_jid/disappearing        |

```
✅ = Available
❌ = Not Available Yet
* = Has known limitations (see notes below)
```

**Notes:**

- `*User My Groups`: Returns a maximum of 500 groups due to WhatsApp protocol limitation. This is enforced by WhatsApp servers, not this API. See [whatsmeow source](https://github.com/tulir/whatsmeow/blob/main/group.go) for details.

## User Interface

### MCP UI

- Setup MCP (tested in cursor)
  ![Setup MCP](https://i.ibb.co/vCg4zNWt/mcpsetup.png)
- Test MCP
  ![Test MCP](https://i.ibb.co/B2LX38DW/mcptest.png)
- Successfully setup MCP
  ![Success MCP](https://i.ibb.co/1fCx0Myc/mcpsuccess.png)

### HTTP REST API UI

| Description          | Image                                                         |
|----------------------|---------------------------------------------------------------|
| Homepage             | ![Homepage](./gallery/homepage.png?v=1)                       |
| Login                | ![Login](./gallery/login.png)                                 |
| Login With Code      | ![Login With Code](./gallery/login-with-code.png)             |
| Send Message         | ![Send Message](./gallery/send-message.png)                   |
| Send Image           | ![Send Image](./gallery/send-image.png)                       |
| Send File            | ![Send File](./gallery/send-file.png)                         |
| Send Video           | ![Send Video](./gallery/send-video.png)                       |
| Send Sticker         | ![Send Sticker](./gallery/send-sticker.png)                   |
| Send Contact         | ![Send Contact](./gallery/send-contact.png)                   |
| Send Location        | ![Send Location](./gallery/send-location.png)                 |
| Send Audio           | ![Send Audio](./gallery/send-audio.png)                       |
| Send Poll            | ![Send Poll](./gallery/send-poll.png)                         |
| Send Presence        | ![Send Presence](./gallery/send-presence.png)                 |
| Send Link            | ![Send Link](./gallery/send-link.png)                         |
| My Group             | ![My Group](./gallery/group-list.png)                         |
| Group Info From Link | ![Group Info From Link](./gallery/group-info-from-link.png)   |
| Create Group         | ![Create Group](./gallery/group-create.png)                   |
| Join Group with Link | ![Join Group with Link](./gallery/group-join-link.png)        |
| Manage Participant   | ![Manage Participant](./gallery/group-manage-participant.png) |
| My Newsletter        | ![My Newsletter](./gallery/newsletter-list.png)               |
| My Contacts          | ![My Contacts](./gallery/contact-list.png)                    |
| Business Profile     | ![Business Profile](./gallery/business-profile.png)           |

### Mac OS NOTE

- Please do this if you have an error (invalid flag in pkg-config --cflags: -Xpreprocessor)
  `export CGO_CFLAGS_ALLOW="-Xpreprocessor"`

## Important

- This project is unofficial and not affiliated with WhatsApp.
- Please use official WhatsApp API to avoid any issues.
- We only able to run MCP or REST API, this is limitation from whatsmeow library. independent MCP will be available in
  the future.
