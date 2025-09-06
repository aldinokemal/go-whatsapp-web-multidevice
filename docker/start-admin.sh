#!/bin/sh

# Start supervisord in the background
echo "Starting supervisord..."
supervisord -c /etc/supervisor/supervisord.conf &

# Wait for supervisord to be ready
echo "Waiting for supervisord to start..."
sleep 5

# Check if supervisord is running and responsive
MAX_ATTEMPTS=30
ATTEMPT=0
while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    echo "Checking supervisord connection (attempt $((ATTEMPT + 1))/$MAX_ATTEMPTS)..."
    
    # Try to connect with authentication
    if curl -f -u admin:change-me-now http://127.0.0.1:9001/ >/dev/null 2>&1; then
        echo "Supervisord HTTP interface is ready!"
        break
    fi
    
    # Also check if the port is listening
    if netstat -ln | grep :9001 >/dev/null 2>&1; then
        echo "Port 9001 is listening, but authentication might be failing..."
    else
        echo "Port 9001 is not yet listening..."
    fi
    
    ATTEMPT=$((ATTEMPT + 1))
    sleep 2
done

if [ $ATTEMPT -eq $MAX_ATTEMPTS ]; then
    echo "ERROR: Supervisord did not become ready within timeout"
    echo "Supervisord logs:"
    cat /var/log/supervisor/supervisord.log 2>/dev/null || echo "No supervisord.log found"
    echo "Process list:"
    ps aux | grep supervisor
    echo "Port status:"
    netstat -ln | grep 9001 || echo "Port 9001 not found"
    exit 1
fi

echo "Supervisord is ready. Starting admin API..."

# Start the admin API
exec /app/whatsapp admin --port 8088
