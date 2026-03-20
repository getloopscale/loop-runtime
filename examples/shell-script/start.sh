#!/bin/bash
set -euo pipefail

PORT="${PORT:-8080}"

echo "Starting service on port ${PORT}..."
echo "Press Ctrl+C to stop."

# Simple HTTP server using bash and netcat (for demonstration)
while true; do
    RESPONSE="HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n{\"status\":\"running\",\"pid\":$$}"
    echo -e "${RESPONSE}" | nc -l -p "${PORT}" -q 1 > /dev/null 2>&1 || true
done
