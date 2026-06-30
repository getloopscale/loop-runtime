#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-deployment-detail-fail-loud_test.sh (Red Team loopzero-cloud#122).

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-deployment-detail-fail-loud_test.sh"
PASS=0
FAIL=0

assert() {
    local label="$1" ; shift
    if "$@" >/dev/null 2>&1 ; then
        PASS=$((PASS + 1)) ; echo "  PASS: $label"
    else
        FAIL=$((FAIL + 1)) ; echo "  FAIL: $label"
    fi
}

echo "== check-dev-public-deployment-detail-fail-loud-syntax_test.sh =="

assert "live script exists at $LIVE_TEST" test -f "$LIVE_TEST"
assert "bash -n syntax clean" bash -n "$LIVE_TEST"
assert "strict mode set -euo pipefail" grep -qE '^set -euo pipefail' "$LIVE_TEST"
assert "issue loopzero-cloud#122 cited"          grep -q 'loopzero-cloud#122' "$LIVE_TEST"
assert "slug deployment-detail-fail-loud cited" grep -q 'deployment-detail-fail-loud' "$LIVE_TEST"
assert "OPERATOR HARD STOP header"           grep -q 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "feedback_issue_fix_requires_failing_e2e cited" grep -q 'feedback_issue_fix_requires_failing_e2e' "$LIVE_TEST"
assert "Scenario A function" grep -q 'want_scenario A' "$LIVE_TEST"
assert "Scenario B function" grep -q 'want_scenario B' "$LIVE_TEST"
assert "--skip-live flag"  grep -q -- '--skip-live'  "$LIVE_TEST"
assert "--scenario= flag"  grep -q -- '--scenario='  "$LIVE_TEST"
assert "--json flag"       grep -q -- '--json'       "$LIVE_TEST"
assert "--help flag"       grep -q -- '--help'       "$LIVE_TEST"
assert "Scenario A asserts 'Build failed before any step ran' literal"  grep -q "'Build failed before any step ran'" "$LIVE_TEST"
assert "Scenario B asserts throwOnError literal"                        grep -q "'throwOnError'" "$LIVE_TEST"
assert "Scenario A probes build-timeline-live chunk"                    grep -q "'build-timeline-live'" "$LIVE_TEST"
assert "Scenario B probes redeploy-dialog chunk"                        grep -q "'redeploy-dialog'" "$LIVE_TEST"
assert "exit code reflects FAIL count" grep -q 'exit \$(\[ \$FAIL_COUNT' "$LIVE_TEST"
assert "no Co-Authored-By trailer" bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"
assert "no --no-verify usage" bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-deployment-detail-fail-loud_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-deployment-detail-fail-loud-syntax_test.sh' "$ALLOWLIST"

echo "== Result: $PASS PASS, $FAIL FAIL =="
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
