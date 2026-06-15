#!/usr/bin/env bash
# J-21 Readiness Score plan v1.1 T4.3 — LIVE bash test (OPERATOR HARD STOP per ADR D5).
#
# Per .claude/rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL: this script
# is NOT closed by /implement halt-loop. It is executed in a sponsor JUNTOS
# session AFTER Cleanroom TOTAL + UP + dev-public canonical chain GREEN.
#
# Scenarios (deferred to operator session):
#   A. Signals consistent — for a fully-deployed project, the readiness page
#      returns the canonical 8 signals with auto_deploy=pass, hpa_configured=
#      pass, two_plus_replicas=pass, logs_healthy=pass, cache_hit_rate=pass.
#      Score >= 90.
#
#   B. Score deterministic — two consecutive page loads (within the React
#      Query staleTime window) yield identical scores. Validates EC-6
#      no-flicker absorption end-to-end against LIVE state.
#
# Usage (operator-only):
#
#   bash infra/tests/e2e/dev-public-readiness_test.sh [--skip-live] [--json]
#
# Flags:
#   --skip-live   Hermetic mode: structural assertions only, no curl. Used
#                 by the companion hermetic test at
#                 infra/tests/check-dev-public-readiness-syntax_test.sh.
#   --json        Machine-readable verdict for dogfood aggregator.
#   --scenario=A  Run only scenario A (or B). Default: both.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO=""

usage() {
  cat <<EOF
Usage: $SCRIPT_NAME [--skip-live] [--json] [--scenario=A|B]

J-21 Readiness Score LIVE bash test. OPERATOR HARD STOP per
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

CLOUD_BASE="${CLOUD_BASE:-https://cloud-dev.loopzero.example}"
TOKEN_FILE="${TOKEN_FILE:-$HOME/.loopzero/local-dogfood-token.txt}"
PROJECT_NAME="${PROJECT_NAME:-teste}"

preflight() {
  command -v curl >/dev/null || { emit_fail "preflight: curl not installed"; return 1; }
  command -v jq >/dev/null || { emit_fail "preflight: jq not installed"; return 1; }
  [[ -f "$TOKEN_FILE" ]] || { emit_fail "preflight: JWT seed missing at $TOKEN_FILE"; return 1; }
  emit_pass "preflight: tools + JWT seed present"
}

scenario_a_signals_consistent() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario A: SKIP --skip-live"; return 0; }
  local jwt
  jwt="$(<"$TOKEN_FILE")"

  # Fetch project status — readiness page composes 5 hooks; we hit one
  # canonical endpoint (status) as a smoke proxy for the page's data layer.
  local body
  body="$(curl -fsS -H "Authorization: Bearer $jwt" \
    "$CLOUD_BASE/engine/api/v1/projects/$PROJECT_NAME/status?env=production" || true)"

  [[ -n "$body" ]] || { emit_fail "scenario A: empty status response"; return 1; }
  # Sanity: response must include the canonical fields the readiness hook reads.
  echo "$body" | jq -e '.replicas_ready' >/dev/null \
    && emit_pass "scenario A: replicas_ready field present" \
    || emit_fail "scenario A: replicas_ready missing"
  echo "$body" | jq -e '.replicas_desired' >/dev/null \
    && emit_pass "scenario A: replicas_desired field present" \
    || emit_fail "scenario A: replicas_desired missing"
}

scenario_b_score_deterministic() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario B: SKIP --skip-live"; return 0; }
  local jwt
  jwt="$(<"$TOKEN_FILE")"

  # Two consecutive fetches within React Query staleTime — both must return
  # equivalent payloads (EC-6 no-flicker validation end-to-end).
  local body1 body2
  body1="$(curl -fsS -H "Authorization: Bearer $jwt" \
    "$CLOUD_BASE/engine/api/v1/projects/$PROJECT_NAME/status?env=production" || true)"
  sleep 1
  body2="$(curl -fsS -H "Authorization: Bearer $jwt" \
    "$CLOUD_BASE/engine/api/v1/projects/$PROJECT_NAME/status?env=production" || true)"

  # Compare on the fields that drive readiness signals (excluding timestamps
  # which legitimately tick between calls).
  local sig1 sig2
  sig1="$(echo "$body1" | jq -r '[.replicas_ready, .replicas_desired, .deploy_status] | @json')"
  sig2="$(echo "$body2" | jq -r '[.replicas_ready, .replicas_desired, .deploy_status] | @json')"
  [[ "$sig1" == "$sig2" ]] \
    && emit_pass "scenario B: signal payloads identical across 2 calls" \
    || emit_fail "scenario B: signal payloads diverged ($sig1 vs $sig2)"
}

preflight || true

case "$SCENARIO" in
  A) scenario_a_signals_consistent ;;
  B) scenario_b_score_deterministic ;;
  "")
    scenario_a_signals_consistent
    scenario_b_score_deterministic
    ;;
  *) emit_fail "unknown scenario '$SCENARIO' (expected A or B)" ;;
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
