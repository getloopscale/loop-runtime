#!/usr/bin/env bash
# T1.2 of plan 2026-06-15-code-review-49-findings-remediation — hermetic
# structural test for the LIVE bash at
# infra/tests/e2e/dev-public-scale-flow_test.sh.
#
# v1.1 update (M4 plan m4-scale-flow-tier-mismatch-resolution v1.1 SHIPPABLE 94):
#   Scenario B now asserts the EC-3 dual-gate per scale.go:283-298 —
#   accepts HTTP 400 (cluster KEDA gate) OR HTTP 402 (tier gate per
#   AssertKEDAAllowed). Edge-case absorption: EC-1 preserve positive 402
#   substring grep AND new negative-bracket regression-lock; EC-2 shebang
#   assertion; EC-3 empty-body guard literal asserted; EC-4 stale "409
#   also acceptable" comment must NOT be present; EC-5 emit_pass symmetry
#   between 400 and 402 branches (excl. SKIP early-return).
#
# Validates without execution that:
#   - bash -n passes
#   - all 3 scenarios A + B + C are defined
#   - --skip-live + --json + --scenario= flags wired
#   - operator HARD STOP per ADR D5 + cleanroom-up-live-test-mandate cited
#   - EC-3 (KEDA dual-gate: tier + cluster) + EC-4 (HPA bounds) cross-references present
#   - allowlist cross-check passes (.claude/rules/shell-scripts-allowlist.md)

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-scale-flow_test.sh"
PASS=0
FAIL=0

assert() {
  local label="$1"; shift
  if "$@"; then
    PASS=$((PASS+1))
    echo "✓ $label"
  else
    FAIL=$((FAIL+1))
    echo "✗ $label"
  fi
}

assert "file exists" test -f "$SCRIPT"
assert "bash -n syntax" bash -n "$SCRIPT"
assert "set -euo pipefail strict mode" grep -q '^set -euo pipefail' "$SCRIPT"
assert "scenario A function defined" grep -q "^scenario_a_success_manual_scale" "$SCRIPT"
assert "scenario B function defined" grep -q "^scenario_b_keda_tier_gate" "$SCRIPT"
assert "scenario C function defined" grep -q "^scenario_c_hpa_bounds" "$SCRIPT"
assert "preflight function defined" grep -q "^preflight" "$SCRIPT"
assert "--skip-live flag wired" grep -q -- "--skip-live" "$SCRIPT"
assert "--json flag wired" grep -q -- "--json" "$SCRIPT"
assert "--scenario= flag wired" grep -q -- "--scenario=" "$SCRIPT"
assert "scenario A probes monthly_cost_usd (ADR D1)" \
  grep -q 'monthly_cost_usd' "$SCRIPT"
assert "scenario A probes plan_tier" grep -q 'plan_tier' "$SCRIPT"
assert "scenario B probes HTTP 402 PLAN_UPGRADE_REQUIRED (EC-1 positive 402 substring preserved)" \
  grep -q '402' "$SCRIPT"
assert "scenario B references EC-3 / AssertKEDAAllowed" \
  grep -qE 'AssertKEDAAllowed|EC-3' "$SCRIPT"

# M4 v1.1 absorptions — EC-3 dual-gate canonical assertions (NEW)
assert "EC-2 shebang is bash" \
  bash -c "head -1 '$SCRIPT' | grep -q '^#!/usr/bin/env bash'"
assert "M4 v1.1: case dispatch on http_code present" \
  grep -Fq 'case "$http_code"' "$SCRIPT"
assert "M4 v1.1: 400 cluster KEDA gate branch present" \
  grep -q 'cluster KEDA gate' "$SCRIPT"
assert "M4 v1.1: scale.go dual-gate citation (any of 283-298 / 290-292 / 293-295)" \
  grep -qE 'scale\.go:(283-298|290-292|293-295)' "$SCRIPT"
assert "M4 v1.1: default branch cites dual-gate" \
  grep -q 'expected HTTP 400 or 402 per EC-3 dual-gate' "$SCRIPT"
assert "M4 v1.1: EC-3 empty-body guard before jq parse" \
  grep -q 'empty body — curl network error' "$SCRIPT"
# EC-1 negative-bracket anti-regression (distinct from EC-1 positive substring above)
assert "M4 v1.1: regression-lock — rigid [[ http_code == 402 ]] NOT present" \
  bash -c "! grep -F '[[ \"\$http_code\" == \"402\" \]]' '$SCRIPT'"
# EC-4: stale "409 also acceptable" comment must NOT be present
assert "M4 v1.1: EC-4 stale 409 comment dropped" \
  bash -c "! grep -q '409 also acceptable' '$SCRIPT'"
# EC-5: emit_pass symmetry between 400 and 402 branches (excluding SKIP early-return)
# Extract scenario_b body, filter out the SKIP line, count emit_pass = 4 (2 per branch × 2)
assert "M4 v1.1: emit_pass symmetry 400 branch (2 PASS lines)" \
  bash -c "
    awk '/^scenario_b_keda_tier_gate\\(\\)/,/^}/' '$SCRIPT' \\
      | grep -v 'SKIP --skip-live' \\
      | awk '/case \"\\\$http_code\"/,/esac/' \\
      | awk '/^[[:space:]]*400\\)/,/^[[:space:]]*;;/' \\
      | grep -c 'emit_pass.*scenario B' \\
      | grep -qx '2'
  "
assert "M4 v1.1: emit_pass symmetry 402 branch (2 PASS lines)" \
  bash -c "
    awk '/^scenario_b_keda_tier_gate\\(\\)/,/^}/' '$SCRIPT' \\
      | grep -v 'SKIP --skip-live' \\
      | awk '/case \"\\\$http_code\"/,/esac/' \\
      | awk '/^[[:space:]]*402\\)/,/^[[:space:]]*;;/' \\
      | grep -c 'emit_pass.*scenario B' \\
      | grep -qx '2'
  "
# M4 plan slug audit
assert "M4 v1.1: plan slug cited in scenario B comment" \
  grep -q 'm4-scale-flow-tier-mismatch-resolution' "$SCRIPT"
assert "scenario C probes HPA_TARGET_OUT_OF_BOUNDS" \
  grep -q 'HPA_TARGET_OUT_OF_BOUNDS' "$SCRIPT"
assert "scenario C references EC-4 / AssertHPATargetBounds" \
  grep -qE 'AssertHPATargetBounds|EC-4' "$SCRIPT"
assert "operator HARD STOP documented" grep -q "OPERATOR HARD STOP" "$SCRIPT"
assert "ADR D5 referenced" grep -q "ADR D5" "$SCRIPT"
assert "cleanroom-up-live-test-mandate cited" \
  grep -q "cleanroom-up-live-test-mandate" "$SCRIPT"
assert "Plan v1.1 J-20 reference present" \
  grep -q "2026-06-06-dashboard-journey-j20-scale" "$SCRIPT"
assert "exit code reflects FAIL count" grep -q 'exit \$(( FAIL_COUNT > 0' "$SCRIPT"
assert "JWT seed env var documented" grep -q "TOKEN_FILE" "$SCRIPT"
assert "allowlist entry present" \
  grep -q "infra/tests/e2e/dev-public-scale-flow_test.sh" \
  .claude/rules/shell-scripts-allowlist.md
assert "tmp cleanup uses rm -f (idempotent)" \
  grep -q 'rm -f /tmp/scale-' "$SCRIPT"

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
