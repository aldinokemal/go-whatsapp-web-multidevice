# Troubleshooting Guide

This guide helps you resolve common issues with the WhatsApp Web Multidevice API.

## Quick Diagnosis

Run the troubleshooting script to automatically check for common issues:

```bash
cd src
./scripts/troubleshoot.sh
```

## Common Issues and Solutions

### 1. Application Won't Start

**Symptoms:**
- Application crashes on startup
- "Failed to initialize" errors
- Database connection errors

**Solutions:**
1. Check if you're in the correct directory:
   ```bash
   cd src
   ```

2. Update dependencies:
   ```bash
   go mod tidy
   go mod download
   ```

3. Clear old database files:
   ```bash
   rm -f storages/*.db
   ```

4. Ensure required directories exist:
   ```bash
   mkdir -p statics/qrcode statics/senditems statics/media storages
   ```

### 2. WhatsApp Connection Issues

**Symptoms:**
- QR code doesn't appear
- Connection fails after scanning QR
- "No device found" errors

**Solutions:**
1. Ensure your WhatsApp account supports multi-device
2. Check your phone's internet connection
3. Remove old QR codes:
   ```bash
   rm statics/qrcode/scan-*
   ```

4. Clear WhatsApp session:
   ```bash
   rm storages/whatsapp.db
   ```

5. Restart the application and scan QR code again

### 3. API Endpoints Not Working

**Symptoms:**
- 404 errors on API calls
- "WhatsApp client not available" errors
- Connection timeout errors

**Solutions:**
1. Check if WhatsApp is connected:
   ```bash
   curl http://localhost:3000/health
   ```

2. Ensure the application is running:
   ```bash
   go run . rest
   ```

3. Check logs for error messages

### 4. Database Issues

**Symptoms:**
- "Database initialization error"
- Foreign key constraint errors
- Database locked errors

**Solutions:**
1. Stop the application
2. Remove database files:
   ```bash
   rm storages/*.db
   ```
3. Restart the application

### 5. Port Already in Use

**Symptoms:**
- "Failed to start: address already in use"
- Can't access the web interface

**Solutions:**
1. Find what's using port 3000:
   ```bash
   lsof -i :3000
   ```

2. Kill the process or change the port:
   ```bash
   # Change port in environment
   export APP_PORT=3001
   go run . rest
   ```

## Health Check Endpoint

Use the health check endpoint to diagnose connection issues:

```bash
curl http://localhost:3000/health
```

Expected response:
```json
{
  "status": "ok",
  "whatsapp": {
    "connected": true,
    "logged_in": true,
    "device_id": "your-device-id",
    "client_available": true
  },
  "timestamp": 1234567890
}
```

## Debug Mode

Enable debug mode for more detailed logs:

```bash
export APP_DEBUG=true
go run . rest
```

## Environment Variables

Common environment variables for troubleshooting:

```bash
# Debug mode
export APP_DEBUG=true

# Change port
export APP_PORT=3001

# Database settings
export DB_URI="file:storages/whatsapp.db?_foreign_keys=on"
export DB_KEYS_URI="file:storages/keys.db?_foreign_keys=on"

# WhatsApp settings
export WHATSAPP_LOG_LEVEL=DEBUG
export WHATSAPP_AUTO_MARK_READ=true
```

## Getting Help

If you're still experiencing issues:

1. Check the logs for specific error messages
2. Run the troubleshooting script
3. Try the health check endpoint
4. Enable debug mode and check logs
5. Create an issue on GitHub with:
   - Error messages
   - Steps to reproduce
   - Environment details
   - Health check response

## Version Information

Current version: v7.3.1
WhatsMeow version: v0.0.0-20241219175558-4d0bd735b871