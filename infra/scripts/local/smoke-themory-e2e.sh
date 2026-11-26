#!/usr/bin/env bash
# smoke-themory-e2e.sh — observable proof of the Dashboard → Go proxy → TheMemory chain.
#
# Per plan v1.3 § Phase 5 / T5.1 of loopruntime-cloud-themory-e2e-finalize-plan.md.
# Companion: create-test-api-key.sh (T5.2) mints the bearer.
#
# Sequence (5 steps + cleanup trap):
#   1. POST /v1/remember to TheMemory direct (sanity baseline)
#   2. GET /v1/memory/list via Go proxy with loopruntime_live_* Bearer + owner_user_id query
#   3. Assert remembered fact id appears in list (10s poll, 500ms interval)
#   4. POST /v1/memory/forget/{memoryID} via Go proxy
#   5. List again — assert id absent
#
# Cleanup (trap EXIT + SIGINT/SIGTERM): best-effort forget of the captured
# memoryID. EC-10: guards against empty memoryID (would 405/404 noise log).
#
# EC-6 GRAVE: LOOPRUNTIME_TEST_BEARER MUST be a loopruntime_live_* with memory:read|write|delete
# scopes (mint via create-test-api-key.sh T5.2). JWT session tokens 401 against
# requireScope middleware.

set -euo pipefail

readonly THEMORY_URL="${THEMORY_URL:-http://localhost:8080}"
readonly CLOUD_URL="${CLOUD_URL:-http://localhost:13002}"
readonly BEARER_FILE="${BEARER_FILE:-${HOME}/.loopruntime/local-test-memory-bearer.txt}"

# TEST_USER MUST match the `subject` field of the loopruntime_api_keys row backing
# LOOPRUNTIME_TEST_BEARER. Cloud proxy enforces D1 invariant: userID upstream comes
# from authenticated subject, NOT from any query param. Override via env to
# match your bearer's subject (defaults to smoke-user-${RANDOM} for
# operator-minted bearers via create-test-api-key.sh whose subject is the
# session's authenticated user).
readonly TEST_USER="${TEST_USER:-smoke-user-${RANDOM}}"
readonly TEST_FACT="Smoke test fact at $(date -Iseconds)"
readonly CURL_TIMEOUT="${CURL_TIMEOUT:-10}"
readonly POLL_TIMEOUT="${POLL_TIMEOUT:-10}"

readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

log_info()  { printf "%b[INFO]%b  %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn()  { printf "%b[WARN]%b  %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }
log_step()  { printf "%bSTEP %s/5:%b %s\n" "$C_GREEN" "$1" "$C_RESET" "$2" >&2; }
log_fatal() { printf "%b[FATAL]%b %s (line ${BASH_LINENO[0]})\n" "$C_RED" "$C_RESET" "$1" >&2; exit 1; }

MEMORY_ID=""

cleanup() {
  local exit_code=$?
  # EC-10: guard empty memoryID (no curl noise + no false 404 log)
  if [ -n "$MEMORY_ID" ] && [ -n "${LOOPRUNTIME_TEST_BEARER:-}" ]; then
    log_warn "Cleanup: best-effort forget of $MEMORY_ID"
    curl -fsS -X DELETE "${CLOUD_URL}/v1/memory/forget/${MEMORY_ID}" \
      -H "Authorization: Bearer ${LOOPRUNTIME_TEST_BEARER}" \
      --max-time "$CURL_TIMEOUT" >/dev/null 2>&1 || true
  fi
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

ensure_jq_present() {
  command -v jq >/dev/null \
    || log_fatal "jq not found — install via 'apt install jq' / 'brew install jq'"
}

ensure_bearer() {
  if [ -z "${LOOPRUNTIME_TEST_BEARER:-}" ]; then
    [ -f "$BEARER_FILE" ] \
      || log_fatal "LOOPRUNTIME_TEST_BEARER unset AND $BEARER_FILE absent — run create-test-api-key.sh (T5.2) first"
    LOOPRUNTIME_TEST_BEARER="$(cat "$BEARER_FILE")"
    export LOOPRUNTIME_TEST_BEARER
  fi
  # EC-6 GRAVE: bearer MUST be loopruntime_live_* — JWT (eyJ...) would 401 vs requireScope middleware
  if [[ ! "$LOOPRUNTIME_TEST_BEARER" =~ ^loopruntime_live_ ]]; then
    log_fatal "LOOPRUNTIME_TEST_BEARER does not match ^loopruntime_live_ — must be a scoped API key (NOT a JWT). Run create-test-api-key.sh OR check Dashboard /settings/tokens."
  fi
  log_info "Bearer loaded (prefix=${LOOPRUNTIME_TEST_BEARER:0:11}, length=${#LOOPRUNTIME_TEST_BEARER})"
}

step1_remember_direct() {
  log_step 1 "POST /v1/remember to TheMemory direct (sanity)"
  local body
  body=$(printf '{"userId":"%s","text":"%s","infer":false}' "$TEST_USER" "$TEST_FACT")
  local response
  response="$(curl -fsS -X POST "${THEMORY_URL}/v1/remember" \
    -H "Content-Type: application/json" \
    -d "$body" \
    --max-time "$CURL_TIMEOUT" 2>&1)" \
    || log_fatal "step 1: TheMemory /v1/remember FAILED — response: $response"
  MEMORY_ID="$(echo "$response" | jq -r '.items[0].id // empty')"
  [ -n "$MEMORY_ID" ] \
    || log_fatal "step 1: response missing .items[0].id — raw: $response"
  log_info "step 1 OK — memoryId=$MEMORY_ID"
}

step2_3_list_via_proxy() {
  log_step 2 "GET /v1/memory/list via Go proxy + assert fact present"
  local elapsed=0
  while [ "$elapsed" -lt "$POLL_TIMEOUT" ]; do
    local response
    response="$(curl -fsS "${CLOUD_URL}/v1/memory/list?owner_user_id=${TEST_USER}" \
      -H "Authorization: Bearer ${LOOPRUNTIME_TEST_BEARER}" \
      --max-time "$CURL_TIMEOUT" 2>&1)" || response=""
    if echo "$response" | jq -e ".memories[] | select(.id == \"$MEMORY_ID\")" >/dev/null 2>&1; then
      log_info "step 2 OK — fact appeared in proxy list"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  log_fatal "step 2: memoryID $MEMORY_ID did NOT appear in proxy list within ${POLL_TIMEOUT}s"
}

step4_forget_via_proxy() {
  log_step 4 "POST /v1/memory/forget/{id} via Go proxy"
  curl -fsS -X DELETE "${CLOUD_URL}/v1/memory/forget/${MEMORY_ID}" \
    -H "Authorization: Bearer ${LOOPRUNTIME_TEST_BEARER}" \
    --max-time "$CURL_TIMEOUT" >/dev/null 2>&1 \
    || log_fatal "step 4: proxy forget FAILED for memoryID=$MEMORY_ID"
  log_info "step 4 OK — forget request accepted"
}

step5_assert_absent() {
  log_step 5 "GET /v1/memory/list again — assert fact absent"
  sleep 1  # eventual consistency window
  local response
  response="$(curl -fsS "${CLOUD_URL}/v1/memory/list?owner_user_id=${TEST_USER}" \
    -H "Authorization: Bearer ${LOOPRUNTIME_TEST_BEARER}" \
    --max-time "$CURL_TIMEOUT" 2>&1)" \
    || log_fatal "step 5: proxy list FAILED — response: $response"
  if echo "$response" | jq -e ".memories[] | select(.id == \"$MEMORY_ID\")" >/dev/null 2>&1; then
    log_fatal "step 5: memoryID $MEMORY_ID STILL present after forget"
  fi
  log_info "step 5 OK — fact correctly absent post-forget"
  MEMORY_ID=""  # disarm cleanup trap (already forgotten)
}

main() {
  printf "smoke-themory-e2e.sh — Dashboard → Go proxy → TheMemory chain\n\n"
  ensure_jq_present
  ensure_bearer

  step1_remember_direct
  step2_3_list_via_proxy
  step4_forget_via_proxy
  step5_assert_absent

  printf "\n%b✓ ALL 5 STEPS GREEN — chain wired end-to-end%b\n" "$C_GREEN" "$C_RESET"
}

main "$@"
