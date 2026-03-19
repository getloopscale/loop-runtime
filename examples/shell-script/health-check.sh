#!/bin/bash
set -euo pipefail

HOST="${HOST:-localhost}"
PORT="${PORT:-8080}"

response=$(curl -s -o /dev/null -w "%{http_code}" "http://${HOST}:${PORT}/" 2>/dev/null || echo "000")

if [ "${response}" = "200" ]; then
    echo "Health check passed (HTTP ${response})"
    exit 0
else
    echo "Health check failed (HTTP ${response})"
    exit 1
fi
