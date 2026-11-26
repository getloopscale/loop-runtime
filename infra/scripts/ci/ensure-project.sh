#!/bin/bash
# ensure-project.sh — Creates a default project via the API if it doesn't exist.
# This triggers EnsureTenant which creates the Kubernetes Tenant CR and ArgoCD Application.
#
# Requires: API running, user + tenant seeded in DB, GitHub token.
# Idempotent — safe to run multiple times (API returns 409 if project exists).
set -euo pipefail

API_URL="${API_URL:-http://localhost:13001}"
PROJECT_NAME="${PROJECT_NAME:-acme-platform}"

# Get GitHub token from gh CLI or env
GH_TOKEN="${GH_TOKEN:-$(gh auth token 2>/dev/null || true)}"
if [ -z "$GH_TOKEN" ]; then
  echo "WARNING: No GitHub token found. Run 'gh auth login' first."
  echo "  Skipping project creation — 'loopscale deploy' will work after 'loopscale login' + manual project creation."
  exit 0
fi

# Wait for API to be ready
echo "Waiting for API at ${API_URL}..."
for i in $(seq 1 30); do
  if curl -sf "${API_URL}/api/v1/health" >/dev/null 2>&1; then
    echo "  API ready"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "  WARNING: API not ready after 60s. Skipping project creation."
    echo "  Run 'task dev:ensure-project' after 'tilt up' to create the project."
    exit 0
  fi
  sleep 2
done

# Create project (idempotent — 409 if already exists)
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${API_URL}/api/v1/projects" \
  -H "Authorization: Bearer ${GH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"${PROJECT_NAME}\",\"apps\":[{\"name\":\"api\",\"type\":\"server\",\"port\":3000},{\"name\":\"web\",\"type\":\"frontend\"},{\"name\":\"worker\",\"type\":\"worker\"}]}")

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | head -1)

case "$HTTP_CODE" in
  201) echo "✓ Project '${PROJECT_NAME}' created: ${BODY}" ;;
  409) echo "✓ Project '${PROJECT_NAME}' already exists" ;;
  403) echo "WARNING: Auth failed (403). User may not be a member of the seeded tenant." ;;
  *)   echo "WARNING: Unexpected response ${HTTP_CODE}: ${BODY}" ;;
esac
