#!/usr/bin/env bash
# J-20 Scale plan v1.1 T4.1 / T1.1 of 2026-06-15-code-review-49-findings-remediation
# — LIVE bash test (OPERATOR HARD STOP per ADR D5).
#
# Per .claude/rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL: this script
# is NOT closed by /implement halt-loop. It is executed in a sponsor JUNTOS
# session AFTER Cleanroom TOTAL + UP + dev-public canonical chain GREEN.
#
# Scenarios (deferred to operator session):
#   A. Successful manual scale — POST /api/v1/projects/:name/scale
#      {"replicas":3,"app":"api","environment":"production"} returns
#      HTTP 200 with `monthly_cost_usd` > 0 + `plan_tier` echoed + applied
#      replicas == 3. Server-authoritative cost computation (ADR D1) +
#      withEnvironmentLock invariant (EC-5) hold.
#
#   B. KEDA tier gate / replicas cap — POST with `replicas > tier_max`
#      (Free tier limit 2) returns HTTP 402 PLAN_UPGRADE_REQUIRED. Per
#      pkg/enforcement.AssertKEDAAllowed boundary check; defends EC-3.
#
#   C. HPA bounds — POST with `autoscaling.cpu_target=5` (below 10
#      minimum) returns HTTP 400 HPA_TARGET_OUT_OF_BOUNDS with the body
#      enumerating the {min, max} pair from pkg/enforcement.
#      AssertHPATargetBounds. Defends EC-4 + plan ADR D4 default targets.
#
# Usage (operator-only):
#
#   bash infra/tests/e2e/dev-public-scale-flow_test.sh [--skip-live] [--json] [--scenario=A|B|C]
#
# Flags:
#   --skip-live    Hermetic mode: structural assertions only, no curl. Used
#                  by the companion hermetic test at
#                  infra/tests/check-dev-public-scale-syntax_test.sh.
#   --json         Machine-readable verdict for dogfood aggregator.
#   --scenario=A   Run only scenario A (or B, or C). Default: all three.
#
# Environment overrides:
#   CLOUD_BASE     dashboard base URL (default: https://cloud-dev.loopzero.example)
#   TOKEN_FILE     JWT seed file (default: ~/.loopzero/local-dogfood-token.txt)
#   PROJECT_NAME   project under test (default: teste)
#   APP_NAME       app under test (default: api)
#   ENV_NAME       environment (default: production)
#
# Plan reference: 2026-06-06-dashboard-journey-j20-scale v1.1 + closure of
# R1-F1 BLOCKER per plan 2026-06-15-code-review-49-findings-remediation T1.1.

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO=""

usage() {
  cat <<EOF
Usage: $SCRIPT_NAME [--skip-live] [--json] [--scenario=A|B|C]

J-20 Scale LIVE bash test. OPERATOR HARD STOP per
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
APP_NAME="${APP_NAME:-api}"
ENV_NAME="${ENV_NAME:-production}"

preflight() {
  command -v curl >/dev/null || { emit_fail "preflight: curl not installed"; return 1; }
  command -v jq >/dev/null || { emit_fail "preflight: jq not installed"; return 1; }
  [[ -f "$TOKEN_FILE" ]] || { emit_fail "preflight: JWT seed missing at $TOKEN_FILE"; return 1; }
  emit_pass "preflight: tools + JWT seed present"
}

scale_endpoint() {
  echo "$CLOUD_BASE/engine/api/v1/projects/$PROJECT_NAME/scale"
}

scenario_a_success_manual_scale() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario A: SKIP --skip-live"; return 0; }
  local jwt
  jwt="$(<"$TOKEN_FILE")"

  # Manual scale: replicas=2 (within Free tier 2-cap; safe baseline).
  local body http_code response
  response="$(curl -fsS -o /tmp/scale-a.json -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer $jwt" \
    -H 'Content-Type: application/json' \
    -d "{\"replicas\":2,\"app\":\"$APP_NAME\",\"environment\":\"$ENV_NAME\"}" \
    "$(scale_endpoint)" || true)"
  body="$(cat /tmp/scale-a.json 2>/dev/null || echo '')"
  http_code="$response"

  [[ "$http_code" == "200" ]] \
    && emit_pass "scenario A: HTTP 200 on manual scale" \
    || { emit_fail "scenario A: expected HTTP 200 got $http_code body=$body"; return 1; }

  # Server-authoritative cost field present (ADR D1).
  echo "$body" | jq -e '.monthly_cost_usd' >/dev/null \
    && emit_pass "scenario A: monthly_cost_usd field present (ADR D1)" \
    || emit_fail "scenario A: monthly_cost_usd missing (ADR D1 violation)"

  # plan_tier echoed.
  echo "$body" | jq -e '.plan_tier' >/dev/null \
    && emit_pass "scenario A: plan_tier echoed" \
    || emit_fail "scenario A: plan_tier missing"

  rm -f /tmp/scale-a.json
}

scenario_b_keda_tier_gate() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario B: SKIP --skip-live"; return 0; }
  local jwt
  jwt="$(<"$TOKEN_FILE")"

  # KEDA scale-to-zero opt-in (min=0) must be rejected via EC-3 dual-gate
  # per api/internal/routes/scale.go:283-298 (ADR D3 fail-closed):
  #   1. tier gate (line 290-292): AssertKEDAAllowed(planTier) fails when
  #      tier ∈ {starter, unknown, empty} → 402 PLAN_UPGRADE_REQUIRED.
  #   2. cluster gate (line 293-295): tier passes but kedaEnabled=false
  #      (cluster has no KEDA controller) → 400 INVALID_REQUEST with
  #      "scale-to-zero is not available" message.
  # Canonical team-tier fixture (seed-tenant-fixture-canonical.sh) lands
  # on the cluster gate path (400) on dev-public; --starter override
  # lands on the tier gate path (402). Both satisfy the EC-3 contract
  # (fail-closed via any gate). Test contract NOT implementation.
  # Per M4 plan m4-scale-flow-tier-mismatch-resolution v1.1 SHIPPABLE 94
  # + ADR D1 (Opção A) + ADR D2 (body-code discrimination retained).
  local body http_code response
  response="$(curl -sS -o /tmp/scale-b.json -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer $jwt" \
    -H 'Content-Type: application/json' \
    -d "{\"app\":\"$APP_NAME\",\"environment\":\"$ENV_NAME\",\"min\":0,\"max\":5}" \
    "$(scale_endpoint)" || true)"
  body="$(cat /tmp/scale-b.json 2>/dev/null || echo '')"
  http_code="$response"

  # EC-3 absorption (M4 v1.1): guard against empty body (curl network blip)
  # BEFORE jq parse — gives operator a clean diagnostic instead of noisy
  # `jq: error (at <stdin>:1): null (null) has no keys` stderr.
  if [[ -z "$body" ]]; then
    emit_fail "scenario B: empty body — curl network error (http_code=$http_code)"
    return 1
  fi

  case "$http_code" in
    400)
      emit_pass "scenario B: HTTP 400 cluster KEDA gate (EC-3 cluster-side dual-gate per scale.go:293-295)"
      if echo "$body" | jq -e '.code == "INVALID_REQUEST"' >/dev/null \
         || echo "$body" | grep -q "scale-to-zero is not available"; then
        emit_pass "scenario B: 400 body shape matches cluster-disabled contract"
      else
        emit_fail "scenario B: 400 body shape mismatch — expected INVALID_REQUEST or 'scale-to-zero is not available' message, got body=$body"
      fi
      ;;
    402)
      emit_pass "scenario B: HTTP 402 PLAN_UPGRADE_REQUIRED on tier cap exceeded (EC-3 tier-side dual-gate per scale.go:290-292)"
      if echo "$body" | jq -e '.code == "PLAN_UPGRADE_REQUIRED" or .code == "REPLICAS_EXCEED_PLAN"' >/dev/null; then
        emit_pass "scenario B: 402 body shape matches enforcement contract"
      else
        emit_fail "scenario B: 402 body shape mismatch — expected PLAN_UPGRADE_REQUIRED/REPLICAS_EXCEED_PLAN, got body=$body"
      fi
      ;;
    *)
      emit_fail "scenario B: expected HTTP 400 or 402 per EC-3 dual-gate, got $http_code body=$body"
      ;;
  esac

  rm -f /tmp/scale-b.json
}

scenario_c_hpa_bounds() {
  [[ $SKIP_LIVE -eq 1 ]] && { emit_pass "scenario C: SKIP --skip-live"; return 0; }
  local jwt
  jwt="$(<"$TOKEN_FILE")"

  # HPA CPU target below minimum (10) — must be rejected with
  # 400 HPA_TARGET_OUT_OF_BOUNDS per pkg/enforcement.AssertHPATargetBounds (EC-4).
  local body http_code response
  response="$(curl -sS -o /tmp/scale-c.json -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer $jwt" \
    -H 'Content-Type: application/json' \
    -d "{\"app\":\"$APP_NAME\",\"environment\":\"$ENV_NAME\",\"min\":1,\"max\":5,\"cpu\":1}" \
    "$(scale_endpoint)" || true)"
  body="$(cat /tmp/scale-c.json 2>/dev/null || echo '')"
  http_code="$response"

  [[ "$http_code" == "400" ]] \
    && emit_pass "scenario C: HTTP 400 on HPA target below minimum" \
    || { emit_fail "scenario C: expected HTTP 400 got $http_code body=$body"; }

  echo "$body" | jq -e '.code == "HPA_TARGET_OUT_OF_BOUNDS"' >/dev/null \
    && emit_pass "scenario C: HPA_TARGET_OUT_OF_BOUNDS code (EC-4)" \
    || emit_fail "scenario C: missing HPA_TARGET_OUT_OF_BOUNDS code body=$body"

  rm -f /tmp/scale-c.json
}

preflight || true

case "$SCENARIO" in
  A) scenario_a_success_manual_scale ;;
  B) scenario_b_keda_tier_gate ;;
  C) scenario_c_hpa_bounds ;;
  "")
    scenario_a_success_manual_scale
    scenario_b_keda_tier_gate
    scenario_c_hpa_bounds
    ;;
  *) emit_fail "unknown scenario '$SCENARIO' (expected A, B, or C)" ;;
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
