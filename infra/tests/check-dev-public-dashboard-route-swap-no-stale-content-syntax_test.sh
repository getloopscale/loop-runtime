#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-route-swap-no-stale-content_test.sh
# (Red Team loopzero-cloud#73).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #73 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-route-swap-no-stale-content_test.sh"
PASS=0
FAIL=0

assert() {
    local label="$1" ; shift
    if "$@" >/dev/null 2>&1 ; then
        PASS=$((PASS + 1))
        echo "  PASS: $label"
    else
        FAIL=$((FAIL + 1))
        echo "  FAIL: $label"
    fi
}

echo "== check-dev-public-dashboard-route-swap-no-stale-content-syntax_test.sh =="

assert "live script exists at $LIVE_TEST"        test -f "$LIVE_TEST"
assert "bash -n syntax clean"                    bash -n "$LIVE_TEST"
assert "strict mode set -euo pipefail"           grep -qE '^set -euo pipefail' "$LIVE_TEST"

assert "issue loopzero-cloud#73 cited"                       grep -q 'loopzero-cloud#73' "$LIVE_TEST"
assert "slug dashboard-route-swap-no-stale-content cited" grep -q 'dashboard-route-swap-no-stale-content' "$LIVE_TEST"

assert "OPERATOR HARD STOP header"                       grep -q 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited"            grep -q 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "feedback_issue_fix_requires_failing_e2e cited"   grep -q 'feedback_issue_fix_requires_failing_e2e' "$LIVE_TEST"

assert "Scenario A function" grep -q 'want_scenario A' "$LIVE_TEST"
assert "Scenario B function" grep -q 'want_scenario B' "$LIVE_TEST"
assert "Scenario C function" grep -q 'want_scenario C' "$LIVE_TEST"

assert "--skip-live flag"  grep -q -- '--skip-live'  "$LIVE_TEST"
assert "--scenario= flag"  grep -q -- '--scenario='  "$LIVE_TEST"
assert "--json flag"       grep -q -- '--json'       "$LIVE_TEST"
assert "--help flag"       grep -q -- '--help'       "$LIVE_TEST"

# Scenario A — RouteScope keyed-div minified emission
assert "Scenario A asserts jsx(div, children, pathname) keyed-div" grep -qE 'jsx.*div.*children.*pathname' "$LIVE_TEST"
# Scenario B — RouteLoading visible text
assert "Scenario B asserts 'Loading…' literal"                     grep -qF 'Loading…' "$LIVE_TEST"
# Scenario C — AT-accessible role="status"
assert "Scenario C asserts role=\"status\" marker"                 grep -qE 'role[:=].\\\"status\\\"|role[:=]"status"' "$LIVE_TEST"

assert "shared locate_entry_js helper"      grep -q 'locate_entry_js' "$LIVE_TEST"
assert "probes /assets/index-*.js entry"    grep -q 'index-' "$LIVE_TEST"

assert "exit code reflects FAIL count" grep -qE 'exit 1|FAIL_COUNT.*-eq 0' "$LIVE_TEST"
assert "no Co-Authored-By trailer"     bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"
assert "no --no-verify usage"          bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-dashboard-route-swap-no-stale-content_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-dashboard-route-swap-no-stale-content-syntax_test.sh' "$ALLOWLIST"

echo "------------------------------------------------------------"
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ] || exit 1
exit 0
