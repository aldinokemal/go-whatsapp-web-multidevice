# Health Check Endpoint Guide for Uptime Kuma

## Overview
This guide explains how to use the WhatsApp API health check endpoints with Uptime Kuma or other monitoring tools.

## Available Endpoints

### 1. `/app/health` (Recommended for Monitoring)
**Purpose:** Comprehensive health check endpoint designed specifically for monitoring tools like Uptime Kuma.

**Method:** GET

**Response Codes:**
- `200 OK` - WhatsApp is connected AND logged in (healthy)
- `503 Service Unavailable` - WhatsApp is disconnected OR not logged in (unhealthy)

**Example Response (Healthy):**
```json
{
  "status": 200,
  "code": "SUCCESS",
  "message": "WhatsApp client is connected and logged in",
  "results": {
    "status": "healthy",
    "is_connected": true,
    "is_logged_in": true,
    "device_id": "XXXXX:X::XXXXXXXXX",
    "timestamp": 1734567890
  }
}
```

**Example Response (Unhealthy):**
```json
{
  "status": 503,
  "code": "SERVICE_UNAVAILABLE",
  "message": "WhatsApp client is disconnected and not logged in - requires login",
  "results": {
    "status": "unhealthy",
    "is_connected": false,
    "is_logged_in": false,
    "device_id": "",
    "timestamp": 1734567890
  }
}
```

### 2. `/app/status` (Legacy Endpoint)
**Purpose:** Basic status check that always returns 200 OK regardless of connection state.

**Method:** GET

**Response:** Always returns 200 OK with connection details
```json
{
  "status": 200,
  "code": "SUCCESS",
  "message": "Connection status retrieved",
  "results": {
    "is_connected": true,
    "is_logged_in": true,
    "device_id": "XXXXX:X::XXXXXXXXX"
  }
}
```

## Uptime Kuma Configuration

### Recommended Setup

1. **Monitor Type:** HTTP(s)
2. **URL:** `http://your-server:3000/app/health`
3. **Method:** GET
4. **Expected Status Code:** 200
5. **Heartbeat Interval:** 60 seconds (adjust as needed)
6. **Retries:** 2-3 (WhatsApp may temporarily disconnect)
7. **Authentication:** Add Basic Auth if configured

### Configuration Example:
```
Name: WhatsApp API Health
URL: http://localhost:3000/app/health
Method: GET
Expected Status Code: 200
Heartbeat Interval: 60
Retries: 3
```

### Advanced Configuration with Keywords:
You can also check for specific keywords in the response:
- **Keyword Type:** JSON Path
- **JSON Path:** `$.results.status`
- **Expected Value:** `healthy`

## Status Conditions

The health check considers the service healthy only when BOTH conditions are met:
1. **Connected:** The WhatsApp client is connected to WhatsApp servers
2. **Logged In:** A valid WhatsApp account session exists

### Common Scenarios:

| Scenario | is_connected | is_logged_in | HTTP Status | Description |
|----------|--------------|--------------|-------------|-------------|
| Fully Operational | true | true | 200 | Service is healthy and ready |
| Just Started | false | false | 503 | Service needs login via QR code |
| Connection Lost | false | true | 503 | Temporary network issue, will auto-reconnect |
| Session Expired | true | false | 503 | User logged out from phone, needs re-login |

## Troubleshooting

### Service Shows Unhealthy in Uptime Kuma

1. **Check the detailed message** in the health endpoint response for specific issues
2. **Common causes:**
   - WhatsApp session expired (user logged out from phone)
   - Network connectivity issues
   - Service just started and needs initial login

3. **Resolution steps:**
   - For login issues: Access the web UI and scan QR code
   - For connection issues: Use `/app/reconnect` endpoint
   - For persistent issues: Check service logs

### Auto-Recovery

The service includes automatic reconnection logic:
- Attempts to reconnect every 5 minutes if disconnected
- Maintains session across service restarts
- Only requires manual intervention for re-login after logout

## Integration with Alerts

You can configure Uptime Kuma to send alerts when the WhatsApp service becomes unhealthy:

1. Set up notification channels (Email, Webhook, Discord, etc.)
2. Configure alert triggers:
   - Down for X minutes
   - Multiple consecutive failures
3. Include the health check message in alerts for quick diagnosis

## Best Practices

1. **Don't set interval too low** - 60 seconds is recommended to avoid unnecessary load
2. **Allow for brief disconnections** - Set retries to 2-3 as WhatsApp may briefly disconnect
3. **Monitor trends** - Track uptime percentage to identify patterns
4. **Set up alerts** - Get notified when manual intervention is needed (re-login)
5. **Use with other monitors** - Combine with endpoint-specific monitors for comprehensive coverage

## API Rate Limits

The health check endpoint has no rate limits and is designed for frequent polling by monitoring tools. However, we recommend:
- Minimum interval: 30 seconds
- Recommended interval: 60 seconds
- Maximum reasonable interval: 5 minutes