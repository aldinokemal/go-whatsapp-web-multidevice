# MCP Server (stdio)

Node.js MCP (Model Context Protocol) server for GoWA, enabling AI assistants like Windsurf, Cursor, and Claude Desktop to interact with WhatsApp through natural language.

## Features

- **60+ tools** covering the complete GoWA REST API
- **Dual transport**: stdio (local) and HTTP/SSE (remote)
- **Docker support**: containerized deployment
- **Zero config**: works out of the box with sensible defaults

### Tool Categories

| Category | Examples |
|----------|----------|
| **App** | login, logout, reconnect, status |
| **Devices** | add, remove, list, per-device login |
| **Send** | text, image, video, audio, file, sticker, contact, location, link, poll |
| **Contacts** | check number, user info, avatar, business profile |
| **Messages** | revoke, delete, react, edit, star, download media |
| **Chats** | list, messages, pin, archive, label, disappearing timer |
| **Groups** | create, leave, participants, promote, demote, settings, invite links |

## Quick Start

### Option 1: Node.js (stdio)

```bash
cd mcp-stdio
npm install
npm run build

# Run
GOWA_URL=http://localhost:3000 node dist/index.js
```

### Option 2: Docker (HTTP/SSE)

```bash
cd mcp-stdio
docker-compose up -d

# MCP endpoint: http://localhost:8090/mcp
```

## AI Assistant Configuration

### Windsurf / Cursor

Add to your MCP configuration:

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "node",
      "args": ["/path/to/mcp-stdio/dist/index.js"],
      "env": {
        "GOWA_URL": "http://localhost:3000"
      }
    }
  }
}
```

### Claude Desktop

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "node",
      "args": ["/path/to/mcp-stdio/dist/index.js"],
      "env": {
        "GOWA_URL": "http://localhost:3000"
      }
    }
  }
}
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GOWA_URL` | `http://localhost:3000` | GoWA REST API URL |
| `GOWA_DEVICE_ID` | (auto) | Device ID (optional, auto-detected if single device) |
| `MCP_HTTP_PORT` | `8090` | HTTP server port (HTTP mode only) |
| `MCP_HTTP_HOST` | `0.0.0.0` | HTTP server bind address |

## Usage Examples

Once configured, interact with your AI assistant:

- "Send a WhatsApp message to +1234567890 saying Hello"
- "Check if +1234567890 has WhatsApp"
- "List all my WhatsApp groups"
- "Get the last 10 messages from chat 1234567890@s.whatsapp.net"
- "Create a group called Team with +111, +222, +333"
- "React with 👍 to message ID xyz in chat abc"

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  AI Assistant   │────▶│   MCP Server    │────▶│   GoWA REST     │
│  (Windsurf,     │     │   (Node.js)     │     │   API           │
│   Cursor, etc)  │◀────│                 │◀────│   (Go)          │
└─────────────────┘     └─────────────────┘     └─────────────────┘
       stdio                  HTTP                   HTTP
```

## Development

```bash
# Install dependencies
npm install

# Build
npm run build

# Run in development
npm run dev
```

## Requirements

- Node.js 22+ (LTS)
- GoWA server running and accessible
- npm or yarn

## License

MIT
