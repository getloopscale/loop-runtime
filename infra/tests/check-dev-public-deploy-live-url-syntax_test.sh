#!/usr/bin/env bash
# Plan m37-tenant-cr-project-sync-canonical-fix v1.0 T3.2 — hermetic
# structural test for infra/tests/e2e/dev-public-deploy-live-url_test.sh
# (T3.1 LIVE script). Closes the cenário 4 BUILD → RELEASE → URL coverage
# gap that Sprint 8 #37 closure missed. Pure grep + bash -n; NO network,
# NO kubectl.
set -euo pipefail

LIVE_SCRIPT="${LIVE_SCRIPT:-infra/tests/e2e/dev-public-deploy-live-url_test.sh}"
PLAN_SLUG="m37-tenant-cr-project-sync-canonical-fix"
ALLOWLIST="${ALLOWLIST:-.claude/rules/shell-scripts-allowlist.md}"

PASS=0
FAIL=0

assert() {
    local desc="$1" expr="$2"
    if eval "$expr" >/dev/null 2>&1; then
        PASS=$((PASS + 1))
    else
        echo "FAIL: $desc"
        FAIL=$((FAIL + 1))
    fi
}

# A — file presence + syntax
assert "LIVE script present" "[ -f '$LIVE_SCRIPT' ]"
assert "bash -n syntax clean" "bash -n '$LIVE_SCRIPT'"
assert "strict mode set" "grep -q 'set -euo pipefail' '$LIVE_SCRIPT'"

# B — all 4 scenarios A-D documented + dispatched + preflight present
assert "scenario A (Tenant CR tuple extended) function" "grep -qE 'scenario_a' '$LIVE_SCRIPT'"
assert "scenario B (AppProject destinations extended) function" "grep -qE 'scenario_b' '$LIVE_SCRIPT'"
assert "scenario C (release ACTIVE with URL) function" "grep -qE 'scenario_c' '$LIVE_SCRIPT'"
assert "scenario D (Live URL HTTP 2xx/3xx) function" "grep -qE 'scenario_d' '$LIVE_SCRIPT'"
assert "preflight function" "grep -qE 'preflight' '$LIVE_SCRIPT'"

# C — m37 fix artifacts cited in the script (per-(project, env) tuple semantic)
assert "per-(project, env) tuple semantic cited" "grep -qE 'per-\\(project, env\\) tuple|tuple-aware|m37' '$LIVE_SCRIPT'"
assert "Tenant CR spec.environments target cited" "grep -qE 'spec.environments|spec\\[..environments..\\]|environments' '$LIVE_SCRIPT'"
assert "AppProject destinations target cited" "grep -qE 'AppProject|appproject|destinations' '$LIVE_SCRIPT'"

# D — canonical 3-segment namespace pattern enforcement
assert "canonical namespace shape loopzero-<tenant>-<project>-<env>" "grep -qE 'loopzero-.*-.*-' '$LIVE_SCRIPT'"

# E — OPERATOR HARD STOP per cleanroom mandate
assert "OPERATOR HARD STOP header" "grep -qE 'OPERATOR HARD STOP|cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "plan slug cited" "grep -q '$PLAN_SLUG' '$LIVE_SCRIPT'"
assert "issue #37 cited" "grep -qE '#37|loopzero-cloud#37' '$LIVE_SCRIPT'"
assert "INQUEBRÁVEL feedback citation" "grep -qE 'feedback_issue_fix_requires_failing_e2e_test' '$LIVE_SCRIPT'"

# F — flags
assert "--skip-live flag" "grep -qE '\\-\\-skip-live' '$LIVE_SCRIPT'"
assert "--scenario= flag" "grep -qE '\\-\\-scenario=' '$LIVE_SCRIPT'"
assert "--json flag" "grep -qE '\\-\\-json' '$LIVE_SCRIPT'"
assert "--help flag" "grep -qE '\\-\\-help' '$LIVE_SCRIPT'"

# G — env contract per check-no-cloud-dev-default gate
assert "LOOPZERO_CLOUD_URL env required (no hardcoded default)" "grep -qE 'LOOPZERO_CLOUD_URL.*:[?]' '$LIVE_SCRIPT'"

# H — scenario chain integrity: C captures URL → D probes it
assert "scenario C writes Live URL to /tmp marker" "grep -qE '/tmp/m37-live-url\\.txt' '$LIVE_SCRIPT'"

# I — 30min CLI ceiling honored (Sprint 8 #38 contract)
assert "30min wait budget honored (1800s default)" "grep -qE 'WAIT_BUDGET.*1800|1800.*WAIT_BUDGET' '$LIVE_SCRIPT'"

# J — anti-patterns
assert "no Co-Authored-By trailer" "! grep -qi 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "no --no-verify flag invocation" "! grep -qE '\\-\\-no-verify' '$LIVE_SCRIPT'"

# K — allowlist cross-check
assert "LIVE script registered in allowlist" "grep -qE 'dev-public-deploy-live-url_test\\.sh' '$ALLOWLIST'"
assert "hermetic test registered in allowlist" "grep -qE 'check-dev-public-deploy-live-url-syntax_test\\.sh' '$ALLOWLIST'"

echo "$(basename "$0"): PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
