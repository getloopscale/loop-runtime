#!/usr/bin/env bash
# Hermetic structural guard for Red Team loopzero-cloud#50 closure
# (defer JWT exchange off the LCP critical path).

set -euo pipefail

LIVE_TEST="$(cd "$(dirname "$0")/.." && pwd)/tests/e2e/dev-public-dashboard-defer-jwt-exchange_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
DOGFOOD_WRAPPER="archive/canonical-tests-v1/scripts/local/dogfood-dashboard-defer-jwt-exchange.sh"

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

echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] ============================================================"
echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] LIVE test: $LIVE_TEST"
echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] ============================================================"

# Structural
assert "LIVE script exists on disk" test -f "$LIVE_TEST"
assert "bash -n parses LIVE script (syntactically clean)" bash -n "$LIVE_TEST"
assert "strict mode 'set -euo pipefail' declared" grep -qF 'set -euo pipefail' "$LIVE_TEST"

# Audit-trail metadata
assert "issue ref 'loopzero-cloud#50' cited in header" grep -qF 'loopzero-cloud#50' "$LIVE_TEST"
assert "slug 'dashboard-defer-jwt-exchange' present" grep -qF 'dashboard-defer-jwt-exchange' "$LIVE_TEST"
assert "OPERATOR HARD STOP header present" grep -qF 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited" grep -qF 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "INQUEBRÁVEL feedback citation" grep -qF 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE_TEST"
assert "follow-on #110 fallback documented" grep -qF '#110' "$LIVE_TEST"

# 3 scenario function blocks
assert "Scenario A block present" grep -qE 'want_scenario A' "$LIVE_TEST"
assert "Scenario B block present" grep -qE 'want_scenario B' "$LIVE_TEST"
assert "Scenario C block present" grep -qE 'want_scenario C' "$LIVE_TEST"

# Scenario-specific probes
assert "Scenario A asserts deferred-exchange minified shape" \
    grep -qF '\.then\([a-z]=>\{[a-z]\|\|[a-z]\("/login",\{replace:!0\}\)\}' "$LIVE_TEST"
assert "Scenario B asserts '/v1/auth/session/token' endpoint literal" \
    grep -qF '/v1/auth/session/token' "$LIVE_TEST"
assert "Scenario C asserts Kratos '/sessions/whoami' endpoint literal" \
    grep -qF '/sessions/whoami' "$LIVE_TEST"

# Chunk locator + entry probe
assert "locate_entry_chunk helper defined" grep -qF 'locate_entry_chunk()' "$LIVE_TEST"
assert "entry index chunk probe present" grep -qF 'index-' "$LIVE_TEST"

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
    grep -qF 'dev-public-dashboard-defer-jwt-exchange_test.sh' "$ALLOWLIST"
assert "hermetic syntax test registered in shell-scripts-allowlist.md" \
    grep -qF 'check-dev-public-dashboard-defer-jwt-exchange-syntax_test.sh' "$ALLOWLIST"
assert "dogfood wrapper registered in shell-scripts-allowlist.md" \
    grep -qF "$(basename "$DOGFOOD_WRAPPER")" "$ALLOWLIST"

echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] ============================================================"
echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] Result: $PASS PASS, $FAIL FAIL"
echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] ============================================================"

if [ $FAIL -gt 0 ] ; then
    echo "[check-dev-public-dashboard-defer-jwt-exchange-syntax] Failed assertions:$FAILED_LIST"
    exit 1
fi
exit 0
