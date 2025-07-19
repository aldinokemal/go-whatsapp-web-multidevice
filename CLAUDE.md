# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common Development Commands

### Building and Running

- **Build binary**: `cd src && go build -o whatsapp` (Linux/macOS) or `go build -o whatsapp.exe` (Windows)
- **Run REST API mode**: `cd src && go run . rest` or `./whatsapp rest`
- **Run MCP server mode**: `cd src && go run . mcp` or `./whatsapp mcp`
- **Run with Docker**: `docker-compose up -d --build`

### Testing

- **Run all tests**: `cd src && go test ./...`
- **Run specific package tests**: `cd src && go test ./validations`
- **Run tests with coverage**: `cd src && go test -cover ./...`

### Development

- **Format code**: `cd src && go fmt ./...`
- **Get dependencies**: `cd src && go mod tidy`
- **Check for issues**: `cd src && go vet ./...`

## Project Architecture

This is a Go-based WhatsApp Web API server supporting both REST API and MCP (Model Context Protocol) modes.

### Core Architecture Pattern

- **Domain-Driven Design**: Business logic separated into domain packages (`domains/`)
- **Clean Architecture**: Clear separation between UI, use cases, and infrastructure layers
- **Cobra CLI**: Command pattern with separate commands for `rest` and `mcp` modes

### Key Directories

- `src/`: Main source code directory
- `src/cmd/`: CLI commands (root, rest, mcp)
- `src/domains/`: Business domain logic (app, chat, group, message, send, user, newsletter)
- `src/infrastructure/`: External integrations (WhatsApp, database)
- `src/ui/`: User interface layers (REST API, MCP server, WebSocket)
- `src/usecase/`: Application use cases bridging domains and UI
- `src/validations/`: Input validation logic
- `src/pkg/`: Shared utilities and helpers

### Configuration

- **Environment Variables**: See `.env.example` for all available options
- **Command Line Flags**: All env vars can be overridden with CLI flags
- **Config Priority**: CLI flags > Environment variables > `.env` file

### Database

- **Main DB**: WhatsApp connection data (SQLite by default, supports PostgreSQL)
- **Chat Storage**: Separate SQLite database for chat history (`storages/chatstorage.db`)
- **Database URIs**: Configurable via `DB_URI` and `DB_KEYS_URI` environment variables

### Mode-Specific Architecture

- **REST Mode**: Fiber web server with HTML templates, WebSocket support, middleware stack
- **MCP Mode**: Model Context Protocol server with SSE transport for AI agent integration

### Key Dependencies

- `go.mau.fi/whatsmeow`: WhatsApp Web protocol implementation
- `github.com/gofiber/fiber/v2`: Web framework for REST API
- `github.com/mark3labs/mcp-go`: MCP server implementation
- `github.com/spf13/cobra`: CLI framework
- `github.com/spf13/viper`: Configuration management

### WhatsApp Integration

- Uses whatsmeow library for WhatsApp Web protocol
- Supports multi-device WhatsApp accounts
- Auto-reconnection and connection monitoring
- Media compression and webhook support

## Important Notes

- The application cannot run both REST and MCP modes simultaneously (limitation from whatsmeow library)
- All source code must be in the `src/` directory
- Media files are stored in `src/statics/media/` and `src/storages/`
- HTML templates and assets are embedded in the binary using Go's embed feature
- FFmpeg is required for media processing (installation varies by platform)
