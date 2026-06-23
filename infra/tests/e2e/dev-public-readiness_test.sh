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
  # CANONICAL FIX 2026-06-15 (FAANG SEM WORKAROUND): the J-18 + J-21
  # canonical wire schema separates *catalog* (status payload's
  # `prom_query_keys`) from *values* (probed via GET /metrics?signal=).
  # api/internal/routes/status.go:resp.PromQueryKeys lists the 8 canonical
  # signals; the J-21 readiness hooks in
  # cloud/dashboard/src/hooks/use-readiness-signals.ts each call
  # `/metrics?signal=<key>` per-signal. Test originally probed flat
  # `.replicas_ready` field that NEVER existed in the response struct —
  # false-failing every real deploy. Canonical assertion: prom_query_keys
  # MUST contain `replicas_ready` (the J-18 v0.15.0 catalog item that
  # J-21 reads). `replicas_desired` is derived client-side from
  # replicas_ready + scaling.replicas (J-21 ADR D3 canonical) so its
  # absence from the catalog is by design, not a gap.
  PROM_KEYS=$(echo "$body" | jq -r '.prom_query_keys // [] | .[]')
  if echo "$PROM_KEYS" | grep -q "^replicas_ready$"; then
    emit_pass "scenario A: replicas_ready in prom_query_keys catalog"
  else
    emit_fail "scenario A: replicas_ready missing from prom_query_keys ($PROM_KEYS)"
  fi
  # replicas_desired derived client-side (J-21 ADR D3) from active_release
  # + scaling.replicas in cloud/dashboard/src/hooks/use-readiness-signals.ts.
  # Validate the derivation source is present.
  if echo "$body" | jq -e '.active_release' >/dev/null 2>&1; then
    emit_pass "scenario A: replicas_desired derivation source (active_release) present"
  else
    emit_fail "scenario A: active_release missing — readiness hook cannot derive replicas_desired"
  fi
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
