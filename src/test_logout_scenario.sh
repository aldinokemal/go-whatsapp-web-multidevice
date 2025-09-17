#!/bin/bash

echo "=== Testing Logout Scenario ==="
echo "1. Starting WhatsApp server..."
./whatsapp rest &
SERVER_PID=$!

echo "2. Waiting for server to initialize (5 seconds)..."
sleep 5

echo "3. Server is running with PID: $SERVER_PID"
echo "4. Calling login endpoint to generate QR code..."
curl -s -u user1:pass1 http://localhost:3000/app/login > /dev/null 2>&1

echo "5. Waiting 10 seconds then calling logout..."
sleep 10

echo "6. Calling logout endpoint to simulate user logout..."
curl -s -u user1:pass1 http://localhost:3000/app/logout > /dev/null 2>&1

echo "7. Now monitoring if timeout triggers after logout..."
echo "   Expected: Server should exit after 2 minutes from logout"

# Monitor the process
START_TIME=$(date +%s)
while kill -0 $SERVER_PID 2>/dev/null; do
    CURRENT_TIME=$(date +%s)
    ELAPSED=$((CURRENT_TIME - START_TIME))
    echo "   Server still running... (${ELAPSED}s elapsed)"
    sleep 10
done

echo "8. Server has exited!"
echo "=== Test Complete ==="