#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-profile-name-layout-bounded_test.sh
# (Red Team loopzero-cloud#71).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #71 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-profile-name-layout-bounded_test.sh"
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

echo "== check-dev-public-dashboard-profile-name-layout-bounded-syntax_test.sh =="

assert "live script exists at $LIVE_TEST"           test -f "$LIVE_TEST"
assert "bash -n syntax clean"                       bash -n "$LIVE_TEST"
assert "strict mode set -euo pipefail"              grep -qE '^set -euo pipefail' "$LIVE_TEST"

assert "issue loopzero-cloud#71 cited"                          grep -q 'loopzero-cloud#71' "$LIVE_TEST"
assert "slug dashboard-profile-name-layout-bounded cited"   grep -q 'dashboard-profile-name-layout-bounded' "$LIVE_TEST"

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

assert "Scenario A asserts max-w-[28ch] literal"     grep -qF 'max-w-[28ch]' "$LIVE_TEST"
assert "Scenario B asserts overflowWrap literal"     grep -qF 'overflowWrap' "$LIVE_TEST"
assert "Scenario C asserts profile-name testid"      grep -qF 'profile-name' "$LIVE_TEST"
assert "shared profile-chunk locator helper"         grep -q 'locate_profile_chunk' "$LIVE_TEST"
assert "probes profile-*.js page chunk"              grep -q 'profile-' "$LIVE_TEST"

assert "exit code reflects FAIL count" grep -qE 'exit 1|FAIL_COUNT.*-eq 0' "$LIVE_TEST"
assert "no Co-Authored-By trailer"     bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"
assert "no --no-verify usage"          bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-dashboard-profile-name-layout-bounded_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-dashboard-profile-name-layout-bounded-syntax_test.sh' "$ALLOWLIST"

echo "------------------------------------------------------------"
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ] || exit 1
exit 0
