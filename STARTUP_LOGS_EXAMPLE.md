# Startup Logs with Version Information

## Example Output

When you start the application, you'll now see the version clearly displayed at the top of the logs:

### REST Mode

```bash
$ ./whatsapp rest
```

**Output:**
```
time="2025-11-23 01:00:00" level=info msg="========================================"
time="2025-11-23 01:00:00" level=info msg="  WhatsApp Web Multidevice"
time="2025-11-23 01:00:00" level=info msg="  Version: v7.9.0"
time="2025-11-23 01:00:00" level=info msg="========================================"
time="2025-11-23 01:00:00" level=info msg="🚀 Starting REST API mode..."
time="2025-11-23 01:00:01" level=info msg="Initialized MinIO storage with endpoint: s3s.larahq.com, bucket: wa1, region: us-east-1, SSL: true"
time="2025-11-23 01:00:01" level=info msg="🌐 Using direct public bucket URLs"
time="2025-11-23 01:00:01" level=info msg="Initialized S3-compatible storage (endpoint: https://s3s.larahq.com, bucket: wa1)"
...
```

### MCP Mode

```bash
$ ./whatsapp mcp
```

**Output:**
```
time="2025-11-23 01:00:00" level=info msg="========================================"
time="2025-11-23 01:00:00" level=info msg="  WhatsApp Web Multidevice"
time="2025-11-23 01:00:00" level=info msg="  Version: v7.9.0"
time="2025-11-23 01:00:00" level=info msg="========================================"
time="2025-11-23 01:00:00" level=info msg="🚀 Starting MCP server mode..."
...
```

### Docker Logs

When running in Docker, you'll see:

```bash
$ docker-compose logs -f whatsapp_go
```

**Output:**
```
gowa-1  | time="2025-11-23T01:00:00Z" level=info msg="========================================"
gowa-1  | time="2025-11-23T01:00:00Z" level=info msg="  WhatsApp Web Multidevice"
gowa-1  | time="2025-11-23T01:00:00Z" level=info msg="  Version: v7.9.0"
gowa-1  | time="2025-11-23T01:00:00Z" level=info msg="========================================"
gowa-1  | time="2025-11-23T01:00:00Z" level=info msg="🚀 Starting REST API mode..."
gowa-1  | time="2025-11-23T01:00:01Z" level=info msg="Initialized MinIO storage with endpoint: s3s.larahq.com, bucket: wa1, region: us-east-1, SSL: true"
gowa-1  | time="2025-11-23T01:00:01Z" level=info msg="🌐 Using direct public bucket URLs"
gowa-1  | time="2025-11-23T01:00:01Z" level=info msg="Initialized S3-compatible storage (endpoint: https://s3s.larahq.com, bucket: wa1)"
```

## Version in Help Text

The version is also displayed in the help text:

```bash
$ ./whatsapp --help
```

**Output:**
```
WhatsApp Web Multidevice - Version v7.9.0

This application is from clone https://github.com/aldinokemal/go-whatsapp-web-multidevice,
you can send whatsapp over http api but your whatsapp account have to be multi device version

Usage:
  whatsapp [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  mcp         Start as MCP server
  rest        Send whatsapp API over http

...
```

## Benefits

1. **Quick Identification**: Immediately know which version is running
2. **Troubleshooting**: Easy to verify if the correct version is deployed
3. **Log Analysis**: Version info is included in all logs for debugging
4. **Docker Monitoring**: Clear version visibility in container logs

## Version Update Workflow

When you update the version in `src/config/settings.go`:

```go
AppVersion = "v8.0.0"  // Changed from v7.9.0
```

The new version will automatically appear in:
- All startup logs
- Help text
- Docker image tags (when using `make docker-release`)
