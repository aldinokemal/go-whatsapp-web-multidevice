#!/bin/sh
set -e

# Bind mounts are often root-owned on the host; the app runs as gowauser and SQLite
# needs write access (DB + WAL). Fix ownership at start (requires container root).
for d in /app/storages /app/statics /app/statics/qrcode /app/statics/senditems /app/statics/media; do
	[ -d "$d" ] || mkdir -p "$d"
	chown -R gowauser:gowa "$d" 2>/dev/null || true
done

exec su-exec gowauser /app/whatsapp "$@"
