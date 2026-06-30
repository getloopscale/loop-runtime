#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-build-error-canonical-manifest_test.sh (Red Team loopzero-cloud#123).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-build-error-canonical-manifest_test.sh"
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

echo "== check-dev-public-build-error-canonical-manifest-syntax_test.sh =="

assert "live script exists at $LIVE_TEST" test -f "$LIVE_TEST"
assert "bash -n syntax clean" bash -n "$LIVE_TEST"
assert "strict mode set -euo pipefail" grep -qE '^set -euo pipefail' "$LIVE_TEST"
assert "issue loopzero-cloud#123 cited"               grep -q 'loopzero-cloud#123' "$LIVE_TEST"
assert "slug build-error-canonical-manifest cited" grep -q 'build-error-canonical-manifest' "$LIVE_TEST"
assert "OPERATOR HARD STOP header"                grep -q 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited"     grep -q 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "feedback_issue_fix_requires_failing_e2e cited" grep -q 'feedback_issue_fix_requires_failing_e2e' "$LIVE_TEST"
assert "Scenario A function" grep -q 'want_scenario A' "$LIVE_TEST"
assert "Scenario B function" grep -q 'want_scenario B' "$LIVE_TEST"
assert "--skip-live flag"  grep -q -- '--skip-live'  "$LIVE_TEST"
assert "--scenario= flag"  grep -q -- '--scenario='  "$LIVE_TEST"
assert "--json flag"       grep -q -- '--json'       "$LIVE_TEST"
assert "--help flag"       grep -q -- '--help'       "$LIVE_TEST"
assert "Scenario A asserts canonical .loopzero/services.json"  grep -q '\.loopzero/services\\\.json' "$LIVE_TEST"
assert "Scenario A asserts canonical services:"            grep -q "'services:'" "$LIVE_TEST"
assert "Scenario A asserts MISSING_FIELD"                  grep -q 'MISSING_FIELD' "$LIVE_TEST"
assert "Scenario B asserts legacy loopzero.yaml absent"        grep -q 'legacy loopzero.yaml' "$LIVE_TEST"
assert "exit code reflects FAIL count" grep -q 'exit \$(\[ \$FAIL_COUNT' "$LIVE_TEST"
assert "no Co-Authored-By trailer" bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"
assert "no --no-verify usage" bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-build-error-canonical-manifest_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-build-error-canonical-manifest-syntax_test.sh' "$ALLOWLIST"

echo "== Result: $PASS PASS, $FAIL FAIL =="
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
