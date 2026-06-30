#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-overview-recent-deploys-status-env-badges_test.sh
# (Red Team loopzero-cloud#97).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #97 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25`.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-overview-recent-deploys-status-env-badges_test.sh"
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

echo "== check-dev-public-dashboard-overview-recent-deploys-status-env-badges-syntax_test.sh =="

# A1 — file exists
assert "live script exists at $LIVE_TEST" test -f "$LIVE_TEST"

# A2 — bash -n parses (no syntax error)
assert "bash -n syntax clean" bash -n "$LIVE_TEST"

# A3 — strict mode
assert "strict mode set -euo pipefail" grep -qE '^set -euo pipefail' "$LIVE_TEST"

# A4 — issue + slug cited
assert "issue loopzero-cloud#97 cited"                                                   grep -q 'loopzero-cloud#97' "$LIVE_TEST"
assert "slug dashboard-overview-recent-deploys-status-env-badges cited"              grep -q 'dashboard-overview-recent-deploys-status-env-badges' "$LIVE_TEST"

# A5 — OPERATOR HARD STOP header
assert "OPERATOR HARD STOP header"                       grep -q 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited"            grep -q 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "feedback_issue_fix_requires_failing_e2e cited"   grep -q 'feedback_issue_fix_requires_failing_e2e' "$LIVE_TEST"

# A6 — 3 scenarios present
assert "Scenario A function" grep -q 'want_scenario A' "$LIVE_TEST"
assert "Scenario B function" grep -q 'want_scenario B' "$LIVE_TEST"
assert "Scenario C function" grep -q 'want_scenario C' "$LIVE_TEST"

# A7 — 4 flags wired
assert "--skip-live flag"  grep -q -- '--skip-live'  "$LIVE_TEST"
assert "--scenario= flag"  grep -q -- '--scenario='  "$LIVE_TEST"
assert "--json flag"       grep -q -- '--json'       "$LIVE_TEST"
assert "--help flag"       grep -q -- '--help'       "$LIVE_TEST"

# A8 — Scenario A engine wire shape probes
assert "Scenario A probes /builds endpoint"          grep -q '/builds' "$LIVE_TEST"
assert "Scenario A asserts status + environment"     grep -q 'status.*environment\|status+env\|status+environment\|statuses.*envs' "$LIVE_TEST"

# A9 — Scenario B asserts BOTH Badge variant literals
assert "Scenario B asserts success Badge variant"     grep -qF '"success"' "$LIVE_TEST"
assert "Scenario B asserts destructive Badge variant" grep -qF '"destructive"' "$LIVE_TEST"
assert "Scenario B probes project-detail page chunk"  grep -q 'project-detail-' "$LIVE_TEST"

# A10 — Scenario C asserts the per-row wiring
assert "Scenario C asserts recent-deploy- testid"    grep -qF 'recent-deploy-' "$LIVE_TEST"
assert "Scenario C asserts .environment reference"   grep -q '\.environment' "$LIVE_TEST"

# A11 — exit reflects FAIL count
assert "exit code reflects FAIL count" grep -qE 'exit 1|FAIL_COUNT.*-eq 0' "$LIVE_TEST"

# A12 — no Co-Authored-By trailer
assert "no Co-Authored-By trailer" bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"

# A13 — no --no-verify
assert "no --no-verify usage" bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

# A14 — allowlist entries present (both files)
ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist"     grep -q 'dev-public-dashboard-overview-recent-deploys-status-env-badges_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" grep -q 'check-dev-public-dashboard-overview-recent-deploys-status-env-badges-syntax_test.sh' "$ALLOWLIST"

echo "------------------------------------------------------------"
echo "PASS: $PASS  FAIL: $FAIL"
[ $FAIL -eq 0 ] || exit 1
exit 0
