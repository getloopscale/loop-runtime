#!/usr/bin/env bash
# J-21 Readiness Score plan v1.1 T4.4 — hermetic structural test for the
# LIVE bash test at infra/tests/e2e/dev-public-readiness_test.sh.
#
# Validates without execution that:
#   - bash -n passes
#   - both scenarios A + B are defined
#   - --skip-live + --json + --scenario= flags wired
#   - operator HARD STOP per ADR D5 documented in comment header
#   - allowlist cross-check passes (shell-scripts-allowlist.md)

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-readiness_test.sh"
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
assert "scenario A function defined" grep -q "^scenario_a_signals_consistent" "$SCRIPT"
assert "scenario B function defined" grep -q "^scenario_b_score_deterministic" "$SCRIPT"
assert "preflight function defined" grep -q "^preflight" "$SCRIPT"
assert "--skip-live flag wired" grep -q -- "--skip-live" "$SCRIPT"
assert "--json flag wired" grep -q -- "--json" "$SCRIPT"
assert "--scenario= flag wired" grep -q -- "--scenario=" "$SCRIPT"
assert "operator HARD STOP documented" grep -q "OPERATOR HARD STOP" "$SCRIPT"
assert "ADR D5 referenced" grep -q "ADR D5" "$SCRIPT"
assert "cleanroom-up-live-test-mandate cited" grep -q "cleanroom-up-live-test-mandate" "$SCRIPT"
assert "EC-6 cross-reference (no-flicker)" grep -q "EC-6" "$SCRIPT"
assert "exit code reflects FAIL count" grep -q 'exit \$(( FAIL_COUNT > 0' "$SCRIPT"
assert "JWT seed env var documented" grep -q "TOKEN_FILE" "$SCRIPT"
assert "allowlist entry present" \
  grep -q "infra/tests/e2e/dev-public-readiness_test.sh" .claude/rules/shell-scripts-allowlist.md

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
