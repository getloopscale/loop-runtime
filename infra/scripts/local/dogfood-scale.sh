#!/usr/bin/env bash
# T1.3 of plan 2026-06-15-code-review-49-findings-remediation — J-20 Scale
# dogfood aggregator. Closes R1-F1 BLOCKER (J-20 Scale missing canonical
# 3-script triplet vs J-21 + J-11 + J-19 + J-22 + J-25).
#
# Aggregates the J-20 test suites into one canonical entry-point:
#   1. T1.2 hermetic syntax test for the LIVE bash test
#   2. Go enforcement tests for pkg/enforcement.AssertKEDAAllowed +
#      AssertHPATargetBounds + ComputeMonthlyCost (EC-2 + EC-3 + EC-4)
#   3. vitest hermetic for J-20 scale components + hooks
#   4. T1.1 LIVE bash dev-public-scale-flow_test.sh (OPERATOR HARD STOP
#      per ADR D5 + cleanroom-up-live-test-mandate.md INQUEBRÁVEL)
#
# Usage:
#   bash infra/scripts/local/dogfood-scale.sh [--hermetic-only] [--live-only] [--json]
#
# Default: hermetic + LIVE both run. --hermetic-only suppresses step 4 for
# CI (no LIVE infra). --live-only suppresses 1+2+3 for the operator slot.

set -euo pipefail

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_OUTPUT=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1 ;;
    --live-only) LIVE_ONLY=1 ;;
    --json) JSON_OUTPUT=1 ;;
    --help|-h)
      cat <<EOF
Usage: $(basename "$0") [--hermetic-only] [--live-only] [--json]

J-20 Scale dogfood aggregator. Per ADR D5 + cleanroom-up-live-test-mandate.md
INQUEBRÁVEL, the LIVE step is an operator HARD STOP — runs only when
--live-only is passed OR when the default mode is invoked from a sponsor
JUNTOS session AFTER Cleanroom TOTAL + UP + dev-public canonical chain GREEN.
EOF
      exit 0 ;;
    *) echo "Unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0
RESULTS=()

run_suite() {
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    PASS=$((PASS+1))
    RESULTS+=("PASS: $label")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "✓ $label"
  else
    FAIL=$((FAIL+1))
    RESULTS+=("FAIL: $label")
    [[ $JSON_OUTPUT -eq 0 ]] && echo "✗ $label"
  fi
}

if [[ $LIVE_ONLY -eq 0 ]]; then
  # Step 1 — hermetic syntax test for the LIVE bash test
  run_suite "T1.2 hermetic syntax test" \
    bash infra/tests/check-dev-public-scale-syntax_test.sh

  # Step 2 — Go enforcement tests (EC-2 + EC-3 + EC-4 absorption)
  run_suite "Go pkg/enforcement scale (KEDA + HPA + cost)" \
    go test -count=1 ./pkg/enforcement/...

  # Step 3 — vitest hermetic for J-20 scale components + hooks
  run_suite "vitest J-20 scale components + hooks" \
    bash -c "cd cloud/dashboard && npx vitest run src/components/scale/ src/hooks/use-scale.test.tsx src/hooks/use-scale-cost.test.tsx"
fi

if [[ $HERMETIC_ONLY -eq 0 ]]; then
  # Step 4 — LIVE bash test (OPERATOR HARD STOP per ADR D5 + cleanroom-up-live-test-mandate)
  # When called from CI: --hermetic-only suppresses this. Operator session
  # invokes the wrapper without flags OR with --live-only to skip 1+2+3.
  run_suite "T1.1 LIVE bash dev-public-scale-flow (operator only)" \
    bash infra/tests/e2e/dev-public-scale-flow_test.sh
fi

echo
if [[ $JSON_OUTPUT -eq 1 ]]; then
  jq -n \
    --argjson pass "$PASS" \
    --argjson fail "$FAIL" \
    --arg verdict "$([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)" \
    --argjson results "$(printf '%s\n' "${RESULTS[@]}" | jq -R . | jq -s .)" \
    '{pass:$pass,fail:$fail,verdict:$verdict,results:$results}'
else
  echo "=== summary: $PASS pass / $FAIL fail ==="
fi

exit $(( FAIL > 0 ? 1 : 0 ))
