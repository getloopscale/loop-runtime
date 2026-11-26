#!/usr/bin/env bash
# create-test-api-key.sh — mint a memory-scoped loop_runtime_live_* API key for E2E smoke bearer.
#
# Per plan v1.3 § Phase 5 / T5.2 (EC-6 absorption) of loop-console-themory-e2e-finalize-plan.md.
#
# EC-6 GRAVE FIX context: the smoke-themory-e2e.sh bearer MUST be a loop_runtime_live_*
# key with memory:read|write|delete scopes (recognized by the Go proxy's
# requireScope middleware), NOT a JWT session cookie from local-dogfood-token.txt
# (which would 401 against scoped middleware).
#
# Implementation diverges from plan v1.3 § T5.2 step 3: cloud's POST /v1/api-keys
# returns the secret in field `.secret` (NOT `.token` as plan documented).
# Documented in the same commit; will be reconciled into the plan if a future
# cycle re-audits.
#
# Sequence:
#   1. Pre-flight: require ~/.loop-runtime/local-dogfood-token.txt (admin JWT for the call itself)
#   2. POST localhost:13002/v1/api-keys with Bearer JWT + body { name, scopes }
#   3. Parse .secret via jq → write to ~/.loop-runtime/local-test-memory-bearer.txt chmod 600
#   4. Print short confirmation (NEVER print the secret to stdout/stderr)

set -euo pipefail

readonly ADMIN_JWT_FILE="${ADMIN_JWT_FILE:-${HOME}/.loop-runtime/local-dogfood-token.txt}"
readonly OUTPUT_FILE="${OUTPUT_FILE:-${HOME}/.loop-runtime/local-test-memory-bearer.txt}"
readonly CLOUD_BASE_URL="${CLOUD_BASE_URL:-http://localhost:13002}"
readonly KEY_NAME_DEFAULT="${KEY_NAME:-local-test-memory-$(date +%s)}"

readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

log_info()  { printf "%b[INFO]%b  %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn()  { printf "%b[WARN]%b  %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }
log_fatal() { printf "%b[FATAL]%b %s\n" "$C_RED" "$C_RESET" "$1" >&2; exit 1; }

ensure_jwt_present() {
  [ -f "$ADMIN_JWT_FILE" ] \
    || log_fatal "Admin JWT not found at $ADMIN_JWT_FILE — run dogfood session bootstrap first OR set ADMIN_JWT_FILE env"
  local jwt
  jwt="$(cat "$ADMIN_JWT_FILE")"
  [ -n "$jwt" ] || log_fatal "Admin JWT file is empty"
  echo "$jwt"
}

ensure_jq_present() {
  command -v jq >/dev/null \
    || log_fatal "jq not found — install via 'apt install jq' / 'brew install jq'"
}

ensure_output_dir() {
  mkdir -p "$(dirname "$OUTPUT_FILE")"
}

mint_key() {
  local admin_jwt="$1"
  local body
  body=$(printf '{"name":"%s","scopes":["memory:read","memory:write","memory:delete"]}' "$KEY_NAME_DEFAULT")
  log_info "POST $CLOUD_BASE_URL/v1/api-keys (name=$KEY_NAME_DEFAULT, scopes=memory:r/w/d)"
  local response
  response="$(curl -fsS -X POST "$CLOUD_BASE_URL/v1/api-keys" \
    -H "Authorization: Bearer $admin_jwt" \
    -H "Content-Type: application/json" \
    -d "$body" 2>&1)" \
    || log_fatal "POST /v1/api-keys FAILED — response: $response"
  local secret
  secret="$(echo "$response" | jq -r '.secret // empty')"
  [ -n "$secret" ] \
    || log_fatal "Response missing .secret field — raw: $response"
  echo "$secret"
}

main() {
  printf "create-test-api-key.sh — mint memory-scoped loop_runtime_live_* key for smoke bearer\n\n"
  ensure_jq_present
  ensure_output_dir
  local admin_jwt
  admin_jwt="$(ensure_jwt_present)"
  local secret
  secret="$(mint_key "$admin_jwt")"
  printf "%s\n" "$secret" > "$OUTPUT_FILE"
  chmod 600 "$OUTPUT_FILE"
  log_info "Test bearer saved at $OUTPUT_FILE (chmod 600, prefix=${secret:0:11})"
  printf "\n%b✓ Memory-scoped loop_runtime_live_* key ready%b\n" "$C_GREEN" "$C_RESET"
  printf "Next: bash infra/scripts/local/smoke-themory-e2e.sh\n"
}

main "$@"
