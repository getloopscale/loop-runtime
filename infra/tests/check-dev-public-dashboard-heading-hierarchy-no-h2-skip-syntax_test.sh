#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-heading-hierarchy-no-h2-skip_test.sh
# (Red Team loopzero-cloud#79).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #79 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-heading-hierarchy-no-h2-skip_test.sh"
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

echo "== check-dev-public-dashboard-heading-hierarchy-no-h2-skip-syntax_test.sh =="

assert "live script exists at $LIVE_TEST"   test -f "$LIVE_TEST"
assert "bash -n syntax clean"               bash -n "$LIVE_TEST"
assert "strict mode set -euo pipefail"      grep -qE '^set -euo pipefail' "$LIVE_TEST"

assert "issue loopzero-cloud#79 cited"                                grep -q 'loopzero-cloud#79' "$LIVE_TEST"
assert "slug dashboard-heading-hierarchy-no-h2-skip cited"        grep -q 'dashboard-heading-hierarchy-no-h2-skip' "$LIVE_TEST"

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

# Scenario A — jsx h2 Health on project-detail
assert "Scenario A asserts jsx(\"h2\",{children:\"Health\"})" grep -qF 'jsx\("h2",\{children:"Health"' "$LIVE_TEST"
assert "Scenario A probes project-detail page chunk"          grep -q 'project-detail' "$LIVE_TEST"
# Scenario B — jsx h2 Account on profile
assert "Scenario B asserts jsx(\"h2\",{children:\"Account\")" grep -qF 'jsx\("h2",\{children:"Account' "$LIVE_TEST"
assert "Scenario B probes profile page chunk"                 grep -qF "locate_page_chunk 'profile'" "$LIVE_TEST"
# Scenario C — no h3 in either chunk
assert "Scenario C asserts no \"h3\" element type"            grep -qF '"h3"' "$LIVE_TEST"

assert "shared locate_page_chunk helper" grep -q 'locate_page_chunk' "$LIVE_TEST"

assert "exit code reflects FAIL count" grep -qE 'exit 1|FAIL_COUNT.*-eq 0' "$LIVE_TEST"
assert "no Co-Authored-By trailer"     bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"
assert "no --no-verify usage"          bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-dashboard-heading-hierarchy-no-h2-skip_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-dashboard-heading-hierarchy-no-h2-skip-syntax_test.sh' "$ALLOWLIST"

echo "------------------------------------------------------------"
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ] || exit 1
exit 0
