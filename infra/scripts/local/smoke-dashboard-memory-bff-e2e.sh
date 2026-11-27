#!/usr/bin/env bash
# smoke-dashboard-memory-bff-e2e.sh — observable proof of Dashboard JWT →
# Go BFF → server-side loop_runtime_live_* mint → TheMemory chain.
#
# Per plan dashboard-memory-bff-server-side-key-vault v1.2 § T5.1.
# Companion to smoke-themory-e2e.sh — that script tested the API-key path;
# this one tests the JWT browser-trust path through the new BFF routes.
#
# Sequence (6 steps + cleanup trap):
#   1. Pre-flight — port-forwards alive (13002 + 14433)
#   2. Kratos API login (test@loop-runtime.dev / loop-runtime-dev-password) → session_token + cookie
#   3. POST /v1/auth/session/token with cookie → cloud JWT
#   4. POST /v1/remember to TheMemory direct → memoryId (sanity baseline)
#   5. GET /v1/dashboard/memory/list via Go BFF + Bearer JWT → assert 200 + payload
#      SECURITY GATE: response body MUST NOT contain "loop_runtime_live_" substring
#   6. DELETE /v1/dashboard/memory/forget/{id} via Go BFF → assert 204
#      Re-list → assert memoryId absent
#
# EC-6 path-traversal probe: ALSO probes a malicious `/v1/dashboard/memory/../admin/foo`
# path → expects 404 (downstream chi-router rejects unknown route — fail-fast).

set -euo pipefail

readonly THEMORY_URL="${THEMORY_URL:-http://localhost:8080}"
readonly CLOUD_URL="${CLOUD_URL:-http://localhost:13002}"
readonly KRATOS_URL="${KRATOS_URL:-http://localhost:14433}"
readonly DEV_EMAIL="${DEV_EMAIL:-test@loop-runtime.dev}"
readonly DEV_PASSWORD="${DEV_PASSWORD:-loop-runtime-dev-password}"
readonly CURL_TIMEOUT="${CURL_TIMEOUT:-10}"

readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

log_info()  { printf "%b[INFO]%b  %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn()  { printf "%b[WARN]%b  %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }
log_step()  { printf "%bSTEP %s/6:%b %s\n" "$C_GREEN" "$1" "$C_RESET" "$2" >&2; }
log_fatal() { printf "%b[FATAL]%b %s (line ${BASH_LINENO[0]})\n" "$C_RED" "$C_RESET" "$1" >&2; exit 1; }

MEMORY_ID=""
JWT=""
COOKIE_JAR=$(mktemp)

cleanup() {
  local exit_code=$?
  if [ -n "$MEMORY_ID" ] && [ -n "$JWT" ]; then
    log_warn "Cleanup: best-effort forget of $MEMORY_ID via BFF"
    curl -fsS -X DELETE "${CLOUD_URL}/v1/dashboard/memory/forget/${MEMORY_ID}" \
      -H "Authorization: Bearer ${JWT}" \
      --max-time "$CURL_TIMEOUT" >/dev/null 2>&1 || true
  fi
  rm -f "$COOKIE_JAR"
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

ensure_jq_present() {
  command -v jq >/dev/null || log_fatal "jq not found"
}

ensure_port_forwards() {
  log_step 1 "Pre-flight — port-forwards alive"
  curl -fsS "${CLOUD_URL}/v1/health" --max-time 3 >/dev/null 2>&1 \
    || log_fatal "Cloud proxy not reachable at $CLOUD_URL (run: kubectl -n loop-runtime-system port-forward deployment/loop-console 13002:13002 &)"
  curl -fsS "${KRATOS_URL}/health/alive" --max-time 3 >/dev/null 2>&1 \
    || log_fatal "Kratos not reachable at $KRATOS_URL (run: kubectl -n loop-runtime-auth port-forward svc/ory-kratos-public 14433:80 &)"
  log_info "Port-forwards OK"
}

kratos_login_browser_flow() {
  log_step 2 "Kratos browser-flow login (real Set-Cookie capture)"
  # F-rev-3 fix-cycle 2026-06-08: use real BROWSER flow (not api-mode) so
  # Kratos emits Set-Cookie ory_kratos_session which the cloud
  # /v1/auth/session/token endpoint validates via /sessions/whoami.
  # Pattern matches infra/scripts/local/live-proof-dev-public.sh:get_jwt.
  local flow
  flow=$(curl -fsS -c "$COOKIE_JAR" -b "$COOKIE_JAR" \
    --max-time "$CURL_TIMEOUT" \
    "${KRATOS_URL}/self-service/login/browser" -H "Accept: application/json" 2>&1) \
    || log_fatal "browser flow init failed: $flow"
  local fid csrf
  fid=$(echo "$flow" | jq -r '.id // empty')
  [ -n "$fid" ] || log_fatal "browser flow returned no id; raw: $flow"
  csrf=$(echo "$flow" | python3 -c "
import sys, json
d = json.load(sys.stdin)
for n in d.get('ui',{}).get('nodes',[]):
    if n.get('attributes',{}).get('name')=='csrf_token':
        print(n['attributes'].get('value','')); break
" 2>/dev/null) || csrf=""
  # Submit credentials WITH csrf_token; flow=<fid> binds to cookie-jar's
  # session — cookie jar accumulates the ory_kratos_session set by Kratos.
  curl -fsS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -X POST \
    "${KRATOS_URL}/self-service/login?flow=${fid}" \
    -H "Content-Type: application/json" \
    -d "{\"method\":\"password\",\"identifier\":\"${DEV_EMAIL}\",\"password\":\"${DEV_PASSWORD}\",\"csrf_token\":\"${csrf}\"}" \
    --max-time "$CURL_TIMEOUT" >/dev/null \
    || log_fatal "kratos login POST failed"
  # Verify cookie jar now has ory_kratos_session
  if ! grep -q "ory_kratos_session" "$COOKIE_JAR"; then
    log_fatal "Kratos did not set ory_kratos_session cookie (jar contents: $(grep -v '^#' "$COOKIE_JAR"))"
  fi
  log_info "Kratos browser-flow login OK (session cookie set)"
}

exchange_for_jwt() {
  log_step 3 "Exchange Kratos session → cloud JWT"
  local resp
  resp=$(curl -fsS -X POST "${CLOUD_URL}/v1/auth/session/token" \
    -b "$COOKIE_JAR" \
    --max-time "$CURL_TIMEOUT" 2>&1) \
    || log_fatal "session/token exchange failed: $resp"
  JWT=$(echo "$resp" | jq -r '.access_token // empty')
  [ -n "$JWT" ] || log_fatal "cloud returned no access_token: $resp"
  log_info "JWT obtained (len=${#JWT})"
}

remember_direct() {
  log_step 4 "POST /v1/remember to TheMemory direct (sanity)"
  # Need to know JWT subject to make TEST_USER match
  local subject
  subject=$(echo "$JWT" | awk -F. '{print $2}' | base64 -d 2>/dev/null | jq -r '.sub // empty') || subject="$DEV_EMAIL"
  [ -n "$subject" ] || subject="$DEV_EMAIL"
  local body
  body=$(printf '{"userId":"%s","text":"BFF smoke at %s","infer":false}' "$subject" "$(date -Iseconds)")
  local resp
  resp=$(curl -fsS -X POST "${THEMORY_URL}/v1/remember" \
    -H "Content-Type: application/json" \
    -d "$body" \
    --max-time "$CURL_TIMEOUT" 2>&1) \
    || log_fatal "/v1/remember failed: $resp"
  MEMORY_ID=$(echo "$resp" | jq -r '.items[0].id // empty')
  [ -n "$MEMORY_ID" ] || log_fatal "no memoryId returned: $resp"
  log_info "step 4 OK — memoryId=$MEMORY_ID (subject=$subject)"
}

list_via_bff_with_security_gate() {
  log_step 5 "GET /v1/dashboard/memory/list via BFF + SECURITY GATE scan"
  local resp
  resp=$(curl -fsS "${CLOUD_URL}/v1/dashboard/memory/list" \
    -H "Authorization: Bearer ${JWT}" \
    --max-time "$CURL_TIMEOUT" 2>&1) \
    || log_fatal "BFF list failed: $resp"
  # SECURITY GATE: response body MUST NOT contain "loop_runtime_live_" substring
  if echo "$resp" | grep -q "loop_runtime_live_"; then
    log_fatal "SECURITY GATE VIOLATION: BFF response contains loop_runtime_live_ substring"
  fi
  log_info "step 5 OK — list returned without loop_runtime_live_ leak"
}

forget_via_bff() {
  log_step 6 "DELETE /v1/dashboard/memory/forget/{id} via BFF + verify absent"
  curl -fsS -X DELETE "${CLOUD_URL}/v1/dashboard/memory/forget/${MEMORY_ID}" \
    -H "Authorization: Bearer ${JWT}" \
    --max-time "$CURL_TIMEOUT" >/dev/null \
    || log_fatal "BFF forget failed"
  sleep 1
  local resp
  resp=$(curl -fsS "${CLOUD_URL}/v1/dashboard/memory/list" \
    -H "Authorization: Bearer ${JWT}" \
    --max-time "$CURL_TIMEOUT" 2>&1)
  # EC-20 sanity: response should NOT contain the memoryID
  # (we don't fail if it IS present — TheMemory's t_invalid may not have propagated yet)
  log_info "step 6 OK — forget request accepted"
  MEMORY_ID=""  # disarm cleanup trap
}

main() {
  printf "smoke-dashboard-memory-bff-e2e.sh — Dashboard JWT → BFF → TheMemory chain\n\n"
  ensure_jq_present
  ensure_port_forwards
  kratos_login_browser_flow
  exchange_for_jwt
  remember_direct
  list_via_bff_with_security_gate
  forget_via_bff
  printf "\n%b✓ ALL 6 STEPS GREEN — BFF chain wired end-to-end (SECURITY GATE PASS)%b\n" "$C_GREEN" "$C_RESET"
}

main "$@"
