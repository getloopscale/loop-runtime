#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-project-repository-route-canonical_test.sh
# (Red Team loopzero-cloud#102).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #102 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-project-repository-route-canonical_test.sh"
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

echo "== check-dev-public-dashboard-project-repository-route-canonical-syntax_test.sh =="

# A1 — file exists
assert "live script exists at $LIVE_TEST" test -f "$LIVE_TEST"

# A2 — bash -n parses (no syntax error)
assert "bash -n syntax clean" bash -n "$LIVE_TEST"

# A3 — strict mode
assert "strict mode set -euo pipefail" grep -qE '^set -euo pipefail' "$LIVE_TEST"

# A4 — issue + slug cited
assert "issue loopzero-cloud#102 cited"                                  grep -q 'loopzero-cloud#102' "$LIVE_TEST"
assert "slug dashboard-project-repository-route-canonical cited"     grep -q 'dashboard-project-repository-route-canonical' "$LIVE_TEST"

# A5 — OPERATOR HARD STOP header
assert "OPERATOR HARD STOP header"                       grep -q 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited"            grep -q 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "feedback_issue_fix_requires_failing_e2e cited"   grep -q 'feedback_issue_fix_requires_failing_e2e' "$LIVE_TEST"

# A6 — 3 scenarios present
assert "Scenario A function" grep -q 'want_scenario A' "$LIVE_TEST"
assert "Scenario B function" grep -q 'want_scenario B' "$LIVE_TEST"
assert "Scenario C function" grep -q 'want_scenario C' "$LIVE_TEST"

# A7 — 4 flags wired (--skip-live, --scenario=, --json, --help)
assert "--skip-live flag"  grep -q -- '--skip-live'  "$LIVE_TEST"
assert "--scenario= flag"  grep -q -- '--scenario='  "$LIVE_TEST"
assert "--json flag"       grep -q -- '--json'       "$LIVE_TEST"
assert "--help flag"       grep -q -- '--help'       "$LIVE_TEST"

# A8 — Scenario A probes the SPA fallback HTTP 200
assert "Scenario A asserts HTTP 200 (SPA shell)"   grep -q 'expected 200' "$LIVE_TEST"
assert "Scenario A probes /repository deep-link"   grep -q '/projects/.*/repository' "$LIVE_TEST"

# A9 — Scenario B asserts the route literal is in the entry bundle
assert "Scenario B probes index entry chunk"             grep -q 'index-' "$LIVE_TEST"
assert "Scenario B asserts ':name/repository' literal"   grep -qF ':name/repository' "$LIVE_TEST"

# A10 — Scenario C asserts the canonical /connect destination
assert "Scenario C asserts /connect destination" grep -q '/connect' "$LIVE_TEST"

# A11 — exit reflects FAIL count
assert "exit code reflects FAIL count" grep -qE 'exit 1|FAIL_COUNT.*-eq 0' "$LIVE_TEST"

# A12 — no Co-Authored-By trailer in headers
assert "no Co-Authored-By trailer" bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"

# A13 — no --no-verify
assert "no --no-verify usage" bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

# A14 — allowlist entries present (both files)
ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-dashboard-project-repository-route-canonical_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-dashboard-project-repository-route-canonical-syntax_test.sh' "$ALLOWLIST"

echo "------------------------------------------------------------"
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ] || exit 1
exit 0
