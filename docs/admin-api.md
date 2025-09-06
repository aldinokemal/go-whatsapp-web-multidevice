# Admin API Implementation

This document provides examples and documentation for the Admin API implementation that orchestrates multi-instance GOWA with Supervisord.

## Overview

The Admin API allows you to dynamically create, manage, and delete GOWA instances through HTTP REST endpoints. Each instance runs on a different port and is managed by Supervisord for robust process supervision.

## Quick Start

### 1. Prerequisites

- Supervisord installed and running
- Required directories created:
  - `/etc/supervisor/conf.d/` (config directory)
  - `/app/instances/` (instance data directory)
  - `/var/log/supervisor/` (log directory)
  - `/tmp/` (lock directory)

### 2. Environment Variables

Set the required environment variable:

```bash
export ADMIN_TOKEN="your-secure-token-here"
```

Optional environment variables and full mappings are documented in the consolidated ADR notes: `docs/features/ADR-001/IMPLEMENTATION_SUMMARY.md`.

**Note**: The admin API supports all environment variables available in the main GOWA application. When instances are created, the admin API automatically configures them with the appropriate environment variables and CLI flags corresponding to the main application's `APP_*` and `WHATSAPP_*` environment variables.

### 3. Start the Admin Server

```bash
./whatsapp admin --port 8088
```

## API Documentation

### Authentication

All admin endpoints require Bearer token authentication:

```bash
Authorization: Bearer your-secure-token-here
```

### Endpoints

#### Create Instance

**POST** `/admin/instances`

Create a new GOWA instance on the specified port with optional configuration.

**Request:**
```json
{
  "port": 3001,
  "basic_auth": "user:password",
  "debug": true,
  "os": "MyApp",
  "account_validation": false,
  "base_path": "/api",
  "auto_reply": "Auto reply message",
  "auto_mark_read": true,
  "webhook": "https://webhook.site/xxx",
  "webhook_secret": "super-secret",
  "chat_storage": true
}
```

**Minimal Request (only port is required):**
```json
{
  "port": 3001
}
```

**Field Descriptions:**
- `port` (required): Port number for the instance (1024-65535)
- `basic_auth` (optional): Basic authentication credentials (format: "user:password")
- `debug` (optional): Enable debug logging (boolean)
- `os` (optional): OS name (device name in WhatsApp)
- `account_validation` (optional): Enable account validation (boolean)
- `base_path` (optional): Base path for subpath deployment
- `auto_reply` (optional): Auto-reply message for incoming messages
- `auto_mark_read` (optional): Auto-mark incoming messages as read (boolean)
- `webhook` (optional): Webhook URL for events
- `webhook_secret` (optional): Webhook secret for validation
- `chat_storage` (optional): Enable chat storage (boolean)

**Response (201 Created):**
```json
{
  "data": {
    "port": 3001,
    "state": "RUNNING",
    "pid": 12345,
    "uptime": "5s",
    "logs": {
      "stdout": "/var/log/supervisor/gowa_3001.out.log",
      "stderr": "/var/log/supervisor/gowa_3001.err.log"
    }
  },
  "message": "Instance created successfully",
  "request_id": "uuid-here",
  "timestamp": "2025-08-28T10:00:00Z"
}
```

**Errors:**
- `400` - Invalid port or JSON
- `409` - Instance already exists or port in use
- `500` - Internal server error

#### List Instances

**GET** `/admin/instances`

List all GOWA instances.

**Response (200 OK):**
```json
{
  "data": [
    {
      "port": 3001,
      "state": "RUNNING",
      "pid": 12345,
      "uptime": "1h23m45s",
      "logs": {
        "stdout": "/var/log/supervisor/gowa_3001.out.log",
        "stderr": "/var/log/supervisor/gowa_3001.err.log"
      }
    },
    {
      "port": 3002,
      "state": "STOPPED",
      "pid": 0,
      "uptime": "0s",
      "logs": {
        "stdout": "/var/log/supervisor/gowa_3002.out.log",
        "stderr": "/var/log/supervisor/gowa_3002.err.log"
      }
    }
  ],
  "message": "Instances retrieved successfully",
  "request_id": "uuid-here",
  "timestamp": "2025-08-28T10:00:00Z"
}
```

#### Get Instance

**GET** `/admin/instances/{port}`

Get information about a specific instance.

**Response (200 OK):**
```json
{
  "data": {
    "port": 3001,
    "state": "RUNNING",
    "pid": 12345,
    "uptime": "1h23m45s",
    "logs": {
      "stdout": "/var/log/supervisor/gowa_3001.out.log",
      "stderr": "/var/log/supervisor/gowa_3001.err.log"
    }
  },
  "message": "Instance retrieved successfully",
  "request_id": "uuid-here",
  "timestamp": "2025-08-28T10:00:00Z"
}
```

**Errors:**
- `400` - Invalid port parameter
- `404` - Instance not found

#### Delete Instance

**DELETE** `/admin/instances/{port}`

Delete an instance and clean up its resources.

**Response (200 OK):**
```json
{
  "message": "Instance deleted successfully",
  "request_id": "uuid-here",
  "timestamp": "2025-08-28T10:00:00Z"
}
```

**Errors:**
- `400` - Invalid port parameter
- `404` - Instance not found
- `500` - Deletion failed

#### Health Check

**GET** `/healthz`

Check the health status of the admin service.

**Response (200 OK):**
```json
{
  "status": "healthy",
  "timestamp": "2025-08-28T10:00:00Z",
  "supervisor_healthy": true,
  "version": "1.0.0"
}
```

#### Readiness Check

**GET** `/readyz`

Check if the service is ready to accept requests.

**Response (200 OK):**
```json
{
  "message": "Service is ready",
  "request_id": "uuid-here",
  "timestamp": "2025-08-28T10:00:00Z"
}
```

## Examples

### Using curl

#### Create an instance (minimal):
```bash
curl -X POST "http://localhost:8088/admin/instances" \
  -H "Authorization: Bearer your-secure-token-here" \
  -H "Content-Type: application/json" \
  -d '{"port": 3001}'
```

#### Create an instance with custom configuration:
```bash
curl -X POST "http://localhost:8088/admin/instances" \
  -H "Authorization: Bearer your-secure-token-here" \
  -H "Content-Type: application/json" \
  -d '{
    "port": 3001,
    "basic_auth": "admin:password123",
    "debug": true,
    "os": "MyCustomApp",
    "account_validation": false,
    "base_path": "/api/v1",
    "auto_reply": "This is an automated response",
    "auto_mark_read": true,
    "webhook": "https://webhook.example.com/whatsapp",
    "webhook_secret": "my-webhook-secret",
    "chat_storage": true
  }'
```

#### List instances:
```bash
curl -X GET "http://localhost:8088/admin/instances" \
  -H "Authorization: Bearer your-secure-token-here"
```

#### Get specific instance:
```bash
curl -X GET "http://localhost:8088/admin/instances/3001" \
  -H "Authorization: Bearer your-secure-token-here"
```

#### Delete instance:
```bash
curl -X DELETE "http://localhost:8088/admin/instances/3001" \
  -H "Authorization: Bearer your-secure-token-here"
```

#### Health check:
```bash
curl -X GET "http://localhost:8088/healthz"
```

### Using httpie

#### Create an instance (minimal):
```bash
http POST localhost:8088/admin/instances \
  Authorization:"Bearer your-secure-token-here" \
  port:=3001
```

#### Create an instance with custom configuration:
```bash
http POST localhost:8088/admin/instances \
  Authorization:"Bearer your-secure-token-here" \
  port:=3001 \
  basic_auth="admin:password123" \
  debug:=true \
  os="MyCustomApp" \
  account_validation:=false \
  base_path="/api/v1" \
  auto_reply="This is an automated response" \
  auto_mark_read:=true \
  webhook="https://webhook.example.com/whatsapp" \
  webhook_secret="my-webhook-secret" \
  chat_storage:=true
```

#### List instances:
```bash
http GET localhost:8088/admin/instances \
  Authorization:"Bearer your-secure-token-here"
```

## Instance States

- `RUNNING` - Instance is running normally
- `STOPPED` - Instance is stopped
- `STARTING` - Instance is in the process of starting
- `FATAL` - Instance failed to start or crashed
- `UNKNOWN` - State could not be determined

## Configuration Files

Each instance gets its own supervisord configuration file at:
`/etc/supervisor/conf.d/gowa-{port}.conf`

Example configuration:
```ini
[program:gowa_3001]
command=/usr/local/bin/whatsapp rest --port=3001 --debug=false --os=Chrome --account-validation=false --basic-auth=admin:admin --auto-mark-read=true --webhook="https://webhook.site/xxx" --webhook-secret="super-secret-key"
directory=/app
autostart=true
autorestart=true
startretries=3
stdout_logfile=/var/log/supervisor/gowa_3001.out.log
stderr_logfile=/var/log/supervisor/gowa_3001.err.log
environment=APP_PORT="3001",APP_DEBUG="false",APP_OS="Chrome",APP_BASIC_AUTH="admin:admin",DB_URI="file:/app/instances/3001/storages/whatsapp.db?_foreign_keys=on",WHATSAPP_AUTO_MARK_READ="true",WHATSAPP_WEBHOOK="https://webhook.site/xxx",WHATSAPP_WEBHOOK_SECRET="super-secret-key",WHATSAPP_ACCOUNT_VALIDATION="false",WHATSAPP_CHAT_STORAGE="true"
```

## Security Considerations

1. **Admin Token**: Always use a strong, randomly generated token for `ADMIN_TOKEN`
2. **Network Access**: Bind the admin server to localhost only in production
3. **Supervisord RPC**: Never expose supervisord's XML-RPC interface publicly
4. **File Permissions**: Ensure proper file permissions on config and data directories
5. **TLS**: Use TLS termination via reverse proxy for production deployments

## Troubleshooting

### Common Issues

1. **"ADMIN_TOKEN environment variable is required"**
   - Set the `ADMIN_TOKEN` environment variable before starting the server

2. **"Failed to connect to supervisord"**
   - Ensure supervisord is running
   - Check the `SUPERVISOR_URL` configuration
   - Verify supervisord authentication credentials

3. **"Port validation failed"**
   - Ports must be between 1024 and 65535
   - Ensure the port is not already in use

4. **"Port is currently locked by another operation"**
   - Another admin operation is in progress for that port
   - Wait for the operation to complete or check for stale lock files

### Log Files

- Admin server logs: Check the console output or configure log forwarding
- Instance logs: Located at `/var/log/supervisor/gowa_{port}.{out|err}.log`
- Supervisord logs: Located at `/var/log/supervisor/supervisord.log`

### Lock Files

Lock files are created at `/tmp/gowa.{port}.lock` during operations to prevent concurrent modifications.
