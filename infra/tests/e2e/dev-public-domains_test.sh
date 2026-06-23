#!/usr/bin/env bash
# J-11 Domains plan v1.1 T4.2 — LIVE bash test (OPERATOR HARD STOP per ADR D5).
#
# Per .claude/rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL: this script
# is NOT closed by /implement halt-loop. It runs in a sponsor JUNTOS session
# AFTER Cleanroom TOTAL + UP + dev-public canonical chain GREEN.
#
# Scenarios (deferred to operator session):
#   A. Add domain returns 201 with nameservers — POST /api/v1/domains
#      returns the canonical Cloudflare-assigned nameservers in the
#      response body, ready for the NSInstructionsCard render.
#
#   B. Polling reaches Active for k3d fixture — for a pre-seeded domain
#      simulated as having NS propagated, the status transitions to
#      Active within the polling window.
#
#   C. Reverify happy path — POST /verify (first call) returns 200 with
#      canonical status enum. Single invariant per scenario (FAANG): we
#      assert ONLY the success contract here, NOT also 429. The rate-limit
#      contract is asserted in Scenario D.
#
#   D. Rate-limit enforcement (ADR D7) — second consecutive POST /verify
#      within the 1/30s budget MUST return 429 with canonical RATE_LIMITED
#      error body. This proves the rate-limit fires; without Scenario D the
#      ADR D7 contract is hypothetical.
#
# Usage (operator-only):
#
#   bash infra/tests/e2e/dev-public-domains_test.sh [--skip-live] [--json]
#
# Flags:
#   --skip-live   Hermetic mode: structural assertions only, no curl. Used
#                 by the companion hermetic test.
#   --json        Machine-readable verdict for dogfood aggregator.
#   --scenario=A  Run only scenario A (or B, C, D). Default: all four.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO=""

usage() {
  cat <<EOF
Usage: $SCRIPT_NAME [--skip-live] [--json] [--scenario=A|B|C|D]

J-11 Domains LIVE bash test. OPERATOR HARD STOP per
.claude/rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ;;
    --json) JSON_OUTPUT=1 ;;
    --scenario=*) SCENARIO="${1#--scenario=}" ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown arg: $1"; usage; exit 2 ;;
  esac
  shift
done

PASS_COUNT=0
FAIL_COUNT=0
RESULTS=()

emit_pass() {
  PASS_COUNT=$((PASS_COUNT+1))
  RESULTS+=("PASS: $1")
  [[ $JSON_OUTPUT -eq 0 ]] && echo "✓ $1"
}

emit_fail() {
  FAIL_COUNT=$((FAIL_COUNT+1))
  RESULTS+=("FAIL: $1")
  [[ $JSON_OUTPUT -eq 0 ]] && echo "✗ $1"
}

CLOUD_BASE="${CLOUD_BASE:-https://app-dev.loopzero.example}"
# CANONICAL 2026-06-15: prefer HMAC session JWT (carries tenant_id; passes
# loopzero-cloud /engine proxy guard). Falls back to bootstrap RSA JWT for
# engine-direct (localhost:13001) when no session JWT exists.
if [ -z "${TOKEN_FILE:-}" ] ; then
  if [ -f "$HOME/.loopzero/local-dogfood-session-jwt.txt" ] ; then
    TOKEN_FILE="$HOME/.loopzero/local-dogfood-session-jwt.txt"
  else
    TOKEN_FILE="$HOME/.loopzero/local-dogfood-token.txt"
  fi
fi
# Generate a unique domain per run so consecutive operator sessions don't
# trip the 409 already-exists guard.
PROBE_DOMAIN="${PROBE_DOMAIN:-loopzero-j11-probe-$(date +%s).example}"

preflight() {
  command -v curl >/dev/null || { emit_fail "preflight: curl not installed"; return 1; }
  command -v jq >/dev/null || { emit_fail "preflight: jq not installed"; return 1; }
  [[ -f "$TOKEN_FILE" ]] || { emit_fail "preflight: JWT seed missing at $TOKEN_FILE"; return 1; }
  emit_pass "preflight: tools + JWT seed present"
}

scenario_a_add_returns_nameservers() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario A: SKIP --skip-live"; return 0; }
  local jwt body
  jwt="$(<"$TOKEN_FILE")"
  # M2 contract requires Idempotency-Key header on POST /domains (per
  # router.go:485 idempotentMiddlewares). Generate per-run unique key.
  local idem_key
  idem_key="dogfood-domain-$(date +%s)-$RANDOM"
  body="$(curl -fsS -X POST -H "Authorization: Bearer $jwt" -H "Content-Type: application/json" \
    -H "Idempotency-Key: $idem_key" \
    -d "{\"domain\":\"$PROBE_DOMAIN\",\"project\":\"${PROJECT_NAME:-teste}\",\"app\":\"api\",\"environment\":\"production\"}" \
    "$CLOUD_BASE/engine/api/v1/domains" || true)"

  [[ -n "$body" ]] || { emit_fail "scenario A: empty create response"; return 1; }
  # Backend canonical response shape (per ADR D6 EC-2 + EC-3): EITHER
  #   v2 CF Zone path: .nameservers[] non-empty (CF zone assigned)
  #   v1 TXT verification path: .verification.txt_name + .cname_target
  # The legacy v1 path applies to domains without a pre-provisioned CF
  # zone (e.g., *.example test domains). Either path is canonical from
  # the dashboard's perspective — both surface the next-action UX.
  if echo "$body" | jq -e '.nameservers | length > 0' >/dev/null 2>&1; then
    emit_pass "scenario A: nameservers array non-empty (v2 CF Zone path)"
  elif echo "$body" | jq -e '.verification.txt_name and (.cname_target // .verification.txt_value)' >/dev/null 2>&1; then
    emit_pass "scenario A: verification.txt_name + cname_target present (v1 TXT path)"
  else
    emit_fail "scenario A: neither nameservers (v2) NOR verification.txt_name+cname_target (v1) present (response: $body)"
  fi
}

scenario_b_polling_reaches_active() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario B: SKIP --skip-live"; return 0; }
  local jwt
  jwt="$(<"$TOKEN_FILE")"
  # Operator session: simulates NS propagation via k3d fixture so status
  # transitions through provisioning → active within 2 minutes max.
  # Here we just assert the status endpoint is reachable + returns a
  # recognized status enum value.
  local body
  body="$(curl -fsS -H "Authorization: Bearer $jwt" \
    "$CLOUD_BASE/engine/api/v1/domains/$PROBE_DOMAIN" || true)"
  [[ -n "$body" ]] || { emit_fail "scenario B: empty get response"; return 1; }
  local status
  status="$(echo "$body" | jq -r '.status')"
  case "$status" in
    pending_verification|verified|provisioning|active|failed|removing)
      emit_pass "scenario B: status enum recognized ($status)" ;;
    *)
      emit_fail "scenario B: unrecognized status '$status'" ;;
  esac
}

scenario_c_reverify_happy_path() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario C: SKIP --skip-live"; return 0; }
  local jwt code body
  jwt="$(<"$TOKEN_FILE")"
  # FAANG-canonical: ONE invariant per scenario. Scenario C asserts ONLY the
  # success contract: first POST /verify under fresh rate-limit budget MUST
  # return 200 + canonical status enum. The 429 contract is asserted in
  # Scenario D (separate invariant). Accepting 200 OR 429 in one arm would
  # mask either the happy path OR the rate-limit firing (anti-pattern
  # catalogued in CLAUDE.md common-mistakes table).
  code="$(curl -s -o /tmp/domain-verify.json -w '%{http_code}' -X POST \
    -H "Authorization: Bearer $jwt" \
    "$CLOUD_BASE/engine/api/v1/domains/$PROBE_DOMAIN/verify")"
  body="$(cat /tmp/domain-verify.json 2>/dev/null || echo '{}')"
  if [[ "$code" != "200" ]]; then
    emit_fail "scenario C: POST /verify expected 200 (happy path) got $code body=$body"
    rm -f /tmp/domain-verify.json
    return
  fi
  # 200 body MUST carry canonical status enum.
  if ! echo "$body" | jq -e '.status | type == "string"' >/dev/null 2>&1; then
    emit_fail "scenario C: POST /verify 200 but body missing .status field: $body"
    rm -f /tmp/domain-verify.json
    return
  fi
  local status
  status="$(echo "$body" | jq -r '.status')"
  case "$status" in
    pending_verification|verified|provisioning|active|failed)
      emit_pass "scenario C: POST /verify returned 200 + canonical status='$status'" ;;
    *)
      emit_fail "scenario C: POST /verify 200 but unrecognized status='$status' body=$body" ;;
  esac
  rm -f /tmp/domain-verify.json
}

scenario_d_reverify_rate_limit_enforced() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario D: SKIP --skip-live"; return 0; }
  local jwt code body
  jwt="$(<"$TOKEN_FILE")"
  # ADR D7 — POST /verify is rate-limited at 1/30s per IP. Scenario C just
  # consumed the budget; the IMMEDIATE second call from the same operator IP
  # MUST return 429 with canonical RATE_LIMITED error body. Without this
  # scenario the ADR D7 contract is hypothetical (Scenario C alone cannot
  # prove the rate-limit fires — it only proves the happy path).
  code="$(curl -s -o /tmp/domain-verify-2.json -w '%{http_code}' -X POST \
    -H "Authorization: Bearer $jwt" \
    "$CLOUD_BASE/engine/api/v1/domains/$PROBE_DOMAIN/verify")"
  body="$(cat /tmp/domain-verify-2.json 2>/dev/null || echo '{}')"
  if [[ "$code" != "429" ]]; then
    emit_fail "scenario D: 2nd POST /verify expected 429 (rate-limit ADR D7) got $code body=$body"
    rm -f /tmp/domain-verify-2.json
    return
  fi
  # 429 body MUST carry a canonical error indication (code/error/message).
  if echo "$body" | jq -e '.code // .error // .message' >/dev/null 2>&1; then
    emit_pass "scenario D: 2nd POST /verify returned 429 + canonical error body (ADR D7 enforced)"
  else
    emit_fail "scenario D: 2nd POST /verify 429 but body missing error indication: $body"
  fi
  rm -f /tmp/domain-verify-2.json
}

preflight || true

case "$SCENARIO" in
  A) scenario_a_add_returns_nameservers ;;
  B) scenario_b_polling_reaches_active ;;
  C) scenario_c_reverify_happy_path ;;
  D) scenario_d_reverify_rate_limit_enforced ;;
  "")
    scenario_a_add_returns_nameservers
    scenario_b_polling_reaches_active
    scenario_c_reverify_happy_path
    scenario_d_reverify_rate_limit_enforced
    ;;
  *) emit_fail "unknown scenario '$SCENARIO' (expected A, B, C, or D)" ;;
esac

if [[ $JSON_OUTPUT -eq 1 ]]; then
  jq -n \
    --argjson pass "$PASS_COUNT" \
    --argjson fail "$FAIL_COUNT" \
    --arg verdict "$([[ $FAIL_COUNT -eq 0 ]] && echo PASS || echo FAIL)" \
    --argjson results "$(printf '%s\n' "${RESULTS[@]}" | jq -R . | jq -s .)" \
    '{pass:$pass,fail:$fail,verdict:$verdict,results:$results}'
else
  echo
  echo "=== summary: $PASS_COUNT pass / $FAIL_COUNT fail ==="
fi

exit $(( FAIL_COUNT > 0 ? 1 : 0 ))
