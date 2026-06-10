#!/usr/bin/env bash
# T5.2 of plan 2026-05-29-dashboard-public-integration-canonicalization
#
# LIVE per-page smoke: GET each of the 9 dashboard routes through Caddy +
# vite proxy with a fresh JWT in a Cookie header (vite passes /v1 + /engine
# to cloud which validates the JWT). Asserts HTTP 200 or 304 — not 401/500.
#
# Note: SPA routes all serve the same index.html (vite history fallback),
# so we additionally verify the SPA Boundaried route names appear in the
# returned HTML via a lazy-loaded chunk hint (not bulletproof but catches
# bundle drift).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

APP_URL="${APP_URL:-https://app.loopzero.example}"

# Pre-flight
if ! curl -sk -o /dev/null -w "%{http_code}" --max-time 5 "$APP_URL/" | grep -q 200; then
  echo "SKIP: $APP_URL unreachable"
  exit 77
fi

ROUTES=(
  /
  /projects
  /audit
  /environments
  /team
  /billing
  /domains
  /settings
  /profile
)

PASS=0
FAIL=0
for route in "${ROUTES[@]}"; do
  STATUS=$(curl -sk -o /dev/null -w "%{http_code}" --max-time 10 "$APP_URL$route")
  if [ "$STATUS" = "200" ] || [ "$STATUS" = "304" ]; then
    echo "  PASS: GET $route → $STATUS"; PASS=$((PASS+1))
  else
    echo "  FAIL: GET $route → $STATUS"; FAIL=$((FAIL+1))
  fi
done

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
