#!/usr/bin/env bash
# Hermetic structural test for the LIVE bash sibling
# dev-public-dashboard-projects-badge-data-plane-aware_test.sh (Red Team
# loopzero-cloud#128 closure — dashboard /projects list badge data-plane-aware).
#
# Pure-text grep + bash -n on the LIVE script source. NO network, NO kubectl.
# Wired in task quality:gates.
#
# Closes the canonical triplet for #128 per
# `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL.

set -euo pipefail

LIVE_TEST="$(dirname "$0")/e2e/dev-public-dashboard-projects-badge-data-plane-aware_test.sh"
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

echo "== check-dev-public-dashboard-projects-badge-data-plane-aware-syntax_test.sh =="

# A1 — file exists
assert "live script exists at $LIVE_TEST" test -f "$LIVE_TEST"

# A2 — bash -n parses (no syntax error)
assert "bash -n syntax clean" bash -n "$LIVE_TEST"

# A3 — strict mode
assert "strict mode set -euo pipefail" grep -qE '^set -euo pipefail' "$LIVE_TEST"

# A4 — issue + slug cited
assert "issue loopzero-cloud#128 cited"                   grep -q 'loopzero-cloud#128' "$LIVE_TEST"
assert "slug dashboard-badge-data-plane-poller cited" grep -q 'dashboard-badge-data-plane-poller' "$LIVE_TEST"

# A5 — OPERATOR HARD STOP header + canonical rule references
assert "OPERATOR HARD STOP header"                     grep -q 'OPERATOR HARD STOP' "$LIVE_TEST"
assert "cleanroom-up-live-test-mandate cited"          grep -q 'cleanroom-up-live-test-mandate' "$LIVE_TEST"
assert "feedback_issue_fix_requires_failing_e2e cited" grep -q 'feedback_issue_fix_requires_failing_e2e' "$LIVE_TEST"

# A6 — 3 scenarios present (A engine wire / B bundle-grep / C kubectl scale)
assert "Scenario A function" grep -q 'want_scenario A' "$LIVE_TEST"
assert "Scenario B function" grep -q 'want_scenario B' "$LIVE_TEST"
assert "Scenario C function" grep -q 'want_scenario C' "$LIVE_TEST"

# A7 — 4 flags wired (--skip-live, --scenario=, --json, --help)
assert "--skip-live flag" grep -q -- '--skip-live' "$LIVE_TEST"
assert "--scenario= flag" grep -q -- '--scenario=' "$LIVE_TEST"
assert "--json flag"      grep -q -- '--json'      "$LIVE_TEST"
assert "--help flag"      grep -q -- '--help'      "$LIVE_TEST"

# A8 — Scenario A probes the canonical engine surface + snapshot field literal
assert "Scenario A probes /engine/api/v1/projects" grep -q '/engine/api/v1/projects' "$LIVE_TEST"
assert "Scenario A asserts last_replicas_ready_value literal in engine wire" \
    grep -qE 'last_replicas_ready_value' "$LIVE_TEST"

# A9 — Scenario B probes the /projects PAGE chunk (disambiguated against siblings)
assert "Scenario B probes projects page chunk"              grep -q 'projects-\[A-Za-z0-9_-\]' "$LIVE_TEST"
assert "Scenario B disambiguates against project-detail sibling"   grep -q 'project-(detail\|status\|connect\|scale\|readiness)' "$LIVE_TEST"
assert 'Scenario B asserts "Down" verdict literal'          grep -q '"Down"' "$LIVE_TEST"
assert 'Scenario B asserts "Deployed" verdict literal'      grep -q '"Deployed"' "$LIVE_TEST"
assert 'Scenario B asserts "Unknown" verdict literal'       grep -q '"Unknown"' "$LIVE_TEST"

# A10 — Scenario C invokes kubectl scale --replicas=0 (the canonical repro)
assert "Scenario C requires kubectl scale --replicas=0" grep -q 'kubectl.*scale.*--replicas=0' "$LIVE_TEST"
assert "Scenario C waits POLLER_TICK_WAIT_S for poller tick" \
    grep -qE 'POLLER_TICK_WAIT_S' "$LIVE_TEST"
assert "Scenario C asserts last_replicas_ready_value=0 post-scale" \
    grep -q 'last_replicas_ready_value.*= 0\|target_val.*= "0"\|target_val" = "0"' "$LIVE_TEST"
assert "Scenario C restores replicas post-probe (idempotent)" \
    grep -q 'restoring\|--replicas=1' "$LIVE_TEST"

# A11 — exit reflects FAIL count
assert "exit code reflects FAIL count" grep -q 'exit \$(\[ \$FAIL_COUNT' "$LIVE_TEST"

# A12 — no Co-Authored-By trailer + no --no-verify
assert "no Co-Authored-By trailer" bash -c "! grep -q 'Co-Authored-By' '$LIVE_TEST'"
assert "no --no-verify usage"      bash -c "! grep -q -- '--no-verify' '$LIVE_TEST'"

# A13 — allowlist entries present (both files) in the loopzero shell-scripts-allowlist
ALLOWLIST="$(dirname "$0")/../../.claude/rules/shell-scripts-allowlist.md"
assert "LIVE script in allowlist" \
    grep -q 'dev-public-dashboard-projects-badge-data-plane-aware_test.sh' "$ALLOWLIST"
assert "hermetic script in allowlist" \
    grep -q 'check-dev-public-dashboard-projects-badge-data-plane-aware-syntax_test.sh' "$ALLOWLIST"

echo "== Result: $PASS PASS, $FAIL FAIL =="
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
