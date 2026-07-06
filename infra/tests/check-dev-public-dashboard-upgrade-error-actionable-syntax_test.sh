#!/usr/bin/env bash
# Hermetic structural guard for Red Team loopzero-cloud#40 closure
# (upgrade-to-Pro 4xx surfaces actionable copy instead of misleading
# "Please try again").

set -euo pipefail

LIVE_TEST="$(cd "$(dirname "$0")/.." && pwd)/tests/e2e/dev-public-dashboard-upgrade-error-actionable_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
DOGFOOD_WRAPPER="archive/canonical-tests-v1/scripts/local/dogfood-dashboard-upgrade-error-actionable.sh"

PASS=0
FAIL=0
FAILED_LIST=""

assert() {
    local description="$1"; shift
    if "$@" ; then PASS=$((PASS + 1)); printf '  PASS  %s\n' "$description"
    else FAIL=$((FAIL + 1)); FAILED_LIST+=$'\n    '"$description"; printf '  FAIL  %s\n' "$description"; fi
}

assert_not() {
    local description="$1"; shift
    if ! "$@" ; then PASS=$((PASS + 1)); printf '  PASS  %s\n' "$description"
    else FAIL=$((FAIL + 1)); FAILED_LIST+=$'\n    '"$description"; printf '  FAIL  %s\n' "$description"; fi
}

echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] ============================================================"
echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] LIVE test: $LIVE_TEST"
echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] ============================================================"

# Structural
assert "LIVE script exists on disk" test -f "$LIVE_TEST"
assert "bash -n parses LIVE script (syntactically clean)" bash -n "$LIVE_TEST"
assert "strict mode 'set -euo pipefail' declared" grep -qF 'set -euo pipefail' "$LIVE_TEST"

# Audit-trail metadata
assert "issue ref 'loopzero-cloud#40' cited in header" grep -qF 'loopzero-cloud#40' "$LIVE_TEST"
assert "slug 'dashboard-upgrade-error-actionable' present" grep -qF 'dashboard-upgrade-error-actionable' "$LIVE_TEST"
assert "OPERATOR HARD STOP header present" grep -qF 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited" grep -qF 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "INQUEBRÁVEL feedback citation" grep -qF 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE_TEST"
assert "family of #49/#69 documented" grep -qE '#49.*#69|#49/#69' "$LIVE_TEST"

# 3 scenario function blocks
assert "Scenario A block present" grep -qE 'want_scenario A' "$LIVE_TEST"
assert "Scenario B block present" grep -qE 'want_scenario B' "$LIVE_TEST"
assert "Scenario C block present" grep -qE 'want_scenario C' "$LIVE_TEST"

# Scenario-specific probes
assert "Scenario A asserts 4xx-actionable 'Upgrade isn't available right now' literal" \
    grep -qF "Upgrade isn't available right now" "$LIVE_TEST"
assert "Scenario B asserts 'support@loopzero.example' contact literal" \
    grep -qF 'support@loopzero.example' "$LIVE_TEST"
assert "Scenario C asserts retryable 'Please try again in a moment' fallback" \
    grep -qF 'Please try again in a moment' "$LIVE_TEST"

# Chunk locator + entry probe
assert "locate_billing_chunk helper defined" grep -qF 'locate_billing_chunk()' "$LIVE_TEST"
assert "billing chunk pattern probe present" grep -qF 'billing-' "$LIVE_TEST"

# Flag wiring
assert "--skip-live flag declared in case" grep -qE '\-\-skip-live\)' "$LIVE_TEST"
assert "--scenario= flag declared in case" grep -qE '\-\-scenario=\*\)' "$LIVE_TEST"
assert "--json flag declared in case" grep -qE '\-\-json\)' "$LIVE_TEST"
assert "--help|-h flag declared in case" grep -qE '\-\-help\|-h\)' "$LIVE_TEST"

# Env + exit contract
assert "APP_URL env override pattern present" grep -qF 'APP_URL:-' "$LIVE_TEST"
assert "exit code reflects FAIL_COUNT" \
    grep -qE '\[ \$FAIL_COUNT -eq 0 \] \|\| exit 1' "$LIVE_TEST"

# Anti-patterns
assert_not "no Co-Authored-By trailer in LIVE script" grep -qF 'Co-Authored-By' "$LIVE_TEST"
assert_not "no --no-verify flag use in LIVE script" grep -qF -- '--no-verify' "$LIVE_TEST"

# Allowlist cross-check
assert "LIVE script registered in shell-scripts-allowlist.md" \
    grep -qF 'dev-public-dashboard-upgrade-error-actionable_test.sh' "$ALLOWLIST"
assert "hermetic syntax test registered in shell-scripts-allowlist.md" \
    grep -qF 'check-dev-public-dashboard-upgrade-error-actionable-syntax_test.sh' "$ALLOWLIST"
assert "dogfood wrapper registered in shell-scripts-allowlist.md" \
    grep -qF "$(basename "$DOGFOOD_WRAPPER")" "$ALLOWLIST"

echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] ============================================================"
echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] Result: $PASS PASS, $FAIL FAIL"
echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] ============================================================"

if [ $FAIL -gt 0 ] ; then
    echo "[check-dev-public-dashboard-upgrade-error-actionable-syntax] Failed assertions:$FAILED_LIST"
    exit 1
fi
exit 0
