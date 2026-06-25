#!/usr/bin/env bash
# Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T3.1 — hermetic structural test
# for infra/tests/e2e/dev-public-build-rbac_test.sh (Sprint 7 T1.1 LIVE
# script, closes loopzero-cloud#36). Completes the canonical 3-script triplet
# debt left behind when Sprint 7 shipped LIVE-only. Pure grep + bash -n;
# NO network, NO kubectl.
set -euo pipefail

LIVE_SCRIPT="${LIVE_SCRIPT:-infra/tests/e2e/dev-public-build-rbac_test.sh}"
SPRINT7_SLUG="qa-sprint7-cleanup-2026-06-25"
SPRINT8_SLUG="qa-sprint8-cleanup-2026-06-25"
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

# B — all 4 scenarios A-D documented + dispatched
assert "scenario A (SA claim sub == argo/loopzero-workflow-submitter) function" "grep -qE 'scenario_a' '$LIVE_SCRIPT'"
assert "scenario B (argo SA can-i create workflows) function" "grep -qE 'scenario_b' '$LIVE_SCRIPT'"
assert "scenario C (POST /builds returns 202 not BUILD_SUBMIT_FAILED) function" "grep -qE 'scenario_c' '$LIVE_SCRIPT'"
assert "scenario D (verbatim Unauthorized text absent) function" "grep -qE 'scenario_d' '$LIVE_SCRIPT'"

# C — Sprint 7 RBAC contract is named in the script
assert "argo namespace cited" "grep -qE '\\bargo\\b' '$LIVE_SCRIPT'"
assert "loopzero-workflow-submitter SA cited" "grep -q 'loopzero-workflow-submitter' '$LIVE_SCRIPT'"
assert "scenario A asserts canonical SA claim" "grep -qE 'system:serviceaccount:argo:loopzero-workflow-submitter' '$LIVE_SCRIPT'"

# D — Scenario D regression-protector against the legacy verbatim text
# (the #36 sponsor catch: "argo: create workflow: Unauthorized")
assert "scenario D asserts Unauthorized text NEVER appears" "grep -qE 'argo.*create workflow.*Unauthorized|Unauthorized' '$LIVE_SCRIPT'"

# E — operator HARD STOP per cleanroom mandate
assert "OPERATOR HARD STOP header" "grep -qE 'OPERATOR HARD STOP|cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "Sprint 7 plan slug cited" "grep -q '$SPRINT7_SLUG' '$LIVE_SCRIPT'"
assert "issue #36 cited" "grep -qE '#36|loopzero-cloud#36' '$LIVE_SCRIPT'"
assert "INQUEBRÁVEL feedback citation" "grep -qE 'feedback_issue_fix_requires_failing_e2e_test' '$LIVE_SCRIPT'"

# F — flags
assert "--skip-live flag" "grep -qE '\\-\\-skip-live' '$LIVE_SCRIPT'"
assert "--scenario= flag" "grep -qE '\\-\\-scenario=' '$LIVE_SCRIPT'"
assert "--json flag" "grep -qE '\\-\\-json' '$LIVE_SCRIPT'"
assert "--help flag" "grep -qE '\\-\\-help' '$LIVE_SCRIPT'"

# G — env contract per check-no-cloud-dev-default gate
assert "LOOPZERO_CLOUD_URL env required (no hardcoded default)" "grep -qE 'LOOPZERO_CLOUD_URL.*:[?]' '$LIVE_SCRIPT'"

# H — anti-patterns
assert "no Co-Authored-By trailer" "! grep -qi 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "no --no-verify flag invocation" "! grep -qE '\\-\\-no-verify' '$LIVE_SCRIPT'"

# I — allowlist cross-check (Sprint 7 LIVE + Sprint 8 retroactive hermetic)
assert "LIVE script registered in allowlist" "grep -qE 'dev-public-build-rbac_test\\.sh' '$ALLOWLIST'"
assert "hermetic test registered in allowlist" "grep -qE 'check-dev-public-build-rbac-syntax_test\\.sh' '$ALLOWLIST'"
assert "Sprint 8 retroactive completion note" "grep -qE '$SPRINT8_SLUG' '$ALLOWLIST'"

echo "$(basename "$0"): PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
