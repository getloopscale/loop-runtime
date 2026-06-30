#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-health-truth_test.sh (Red Team loopzero-cloud#124).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #124 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-health-truth_test.sh"
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

echo "== check-dev-public-dashboard-health-truth-syntax_test.sh =="

# A1 — file exists
assert "live script exists at $LIVE_TEST" test -f "$LIVE_TEST"

# A2 — bash -n parses (no syntax error)
assert "bash -n syntax clean" bash -n "$LIVE_TEST"

# A3 — strict mode
assert "strict mode set -euo pipefail" grep -qE '^set -euo pipefail' "$LIVE_TEST"

# A4 — issue + slug cited
assert "issue loopzero-cloud#124 cited"        grep -q 'loopzero-cloud#124' "$LIVE_TEST"
assert "slug dashboard-health-truth cited" grep -q 'dashboard-health-truth' "$LIVE_TEST"

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

# A8 — Scenario B probes the project-status chunk literal
assert "Scenario B probes project-status chunk"          grep -q 'project-status-' "$LIVE_TEST"
assert 'Scenario B asserts "Unknown" verdict literal'    grep -q '"Unknown"' "$LIVE_TEST"

# A9 — Scenario C asserts the ground-truth shape (ACTIVE + synced)
assert 'Scenario C asserts active.status=ACTIVE'  grep -q 'active_status.*ACTIVE\|"ACTIVE"' "$LIVE_TEST"
assert 'Scenario C asserts sync_status=synced'    grep -q 'sync_status.*synced\|"synced"' "$LIVE_TEST"

# A10 — exit reflects FAIL count
assert "exit code reflects FAIL count" grep -q 'exit \$(\[ \$FAIL_COUNT' "$LIVE_TEST"

# A11 — no Co-Authored-By trailer in headers
assert "no Co-Authored-By trailer" bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"

# A12 — no --no-verify
assert "no --no-verify usage" bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

# A13 — allowlist entries present (both files)
ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-dashboard-health-truth_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-dashboard-health-truth-syntax_test.sh' "$ALLOWLIST"

echo "== Result: $PASS PASS, $FAIL FAIL =="
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
