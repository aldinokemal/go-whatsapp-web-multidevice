#!/bin/bash

# WhatsApp Web Multidevice Troubleshooting Script
# This script helps diagnose and fix common issues

echo "🔍 WhatsApp Web Multidevice Troubleshooting Script"
echo "=================================================="

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo "❌ Error: Please run this script from the src directory"
    exit 1
fi

echo "📋 Checking system requirements..."

# Check Go version
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo "✅ Go version: $GO_VERSION"

# Check if required directories exist
echo "📁 Checking required directories..."
for dir in "statics/qrcode" "statics/senditems" "statics/media" "storages"; do
    if [ ! -d "$dir" ]; then
        echo "⚠️  Creating missing directory: $dir"
        mkdir -p "$dir"
    else
        echo "✅ Directory exists: $dir"
    fi
done

# Check database files
echo "🗄️  Checking database files..."
if [ -f "storages/whatsapp.db" ]; then
    echo "✅ WhatsApp database exists"
    DB_SIZE=$(du -h "storages/whatsapp.db" | cut -f1)
    echo "   Size: $DB_SIZE"
else
    echo "⚠️  WhatsApp database not found - will be created on first run"
fi

if [ -f "storages/chatstorage.db" ]; then
    echo "✅ Chat storage database exists"
    DB_SIZE=$(du -h "storages/chatstorage.db" | cut -f1)
    echo "   Size: $DB_SIZE"
else
    echo "⚠️  Chat storage database not found - will be created on first run"
fi

# Check for old QR codes
echo "🔍 Checking for old QR codes..."
QR_COUNT=$(find statics/qrcode -name "scan-*" 2>/dev/null | wc -l)
if [ $QR_COUNT -gt 0 ]; then
    echo "⚠️  Found $QR_COUNT old QR code files"
    echo "   Consider removing old QR codes if you're having connection issues"
    echo "   Run: rm statics/qrcode/scan-*"
fi

# Check dependencies
echo "📦 Checking Go dependencies..."
go mod tidy
if [ $? -eq 0 ]; then
    echo "✅ Dependencies are up to date"
else
    echo "❌ Error updating dependencies"
fi

# Check if port 3000 is available
echo "🌐 Checking if port 3000 is available..."
if lsof -Pi :3000 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "⚠️  Port 3000 is already in use"
    echo "   You may need to stop other services or change the port"
else
    echo "✅ Port 3000 is available"
fi

echo ""
echo "🚀 Troubleshooting complete!"
echo ""
echo "📝 Next steps:"
echo "1. Run: go run . rest"
echo "2. Open http://localhost:3000 in your browser"
echo "3. Scan the QR code with your WhatsApp"
echo "4. Check http://localhost:3000/health for connection status"
echo ""
echo "🔧 If you're still having issues:"
echo "- Check the logs for error messages"
echo "- Try removing old database files: rm storages/*.db"
echo "- Ensure your WhatsApp account supports multi-device"
echo "- Check if your phone has a stable internet connection"