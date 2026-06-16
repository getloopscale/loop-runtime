#!/usr/bin/env bash
# T1.2 of plan 2026-06-15-code-review-49-findings-remediation — hermetic
# structural test for the LIVE bash at
# infra/tests/e2e/dev-public-scale-flow_test.sh.
#
# Validates without execution that:
#   - bash -n passes
#   - all 3 scenarios A + B + C are defined
#   - --skip-live + --json + --scenario= flags wired
#   - operator HARD STOP per ADR D5 + cleanroom-up-live-test-mandate cited
#   - EC-3 (KEDA tier gate) + EC-4 (HPA bounds) cross-references present
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
assert "scenario B probes HTTP 402 PLAN_UPGRADE_REQUIRED" \
  grep -q '402' "$SCRIPT"
assert "scenario B references EC-3 / AssertKEDAAllowed" \
  grep -qE 'AssertKEDAAllowed|EC-3' "$SCRIPT"
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
