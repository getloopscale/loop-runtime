#!/usr/bin/env bash
# Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T2.2 — hermetic structural test
# for infra/tests/e2e/dev-public-build-trigger-error-surface_test.sh (T2.1 LIVE
# script). Closes retroactively loopzero-cloud#34 (Sprint 6 closed engineering-side
# but lacked the LIVE triplet mandated by feedback_issue_fix_requires_failing
# _e2e_test_2026_06_25). Pure grep + bash -n; NO network, NO kubectl.
set -euo pipefail

LIVE_SCRIPT="${LIVE_SCRIPT:-infra/tests/e2e/dev-public-build-trigger-error-surface_test.sh}"
PLAN_SLUG="qa-sprint8-cleanup-2026-06-25"
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

# B — all 6 scenarios A-F documented + dispatched (the 5 SubmitBuildErrorReason
# discriminators + Scenario F regression-protector for the legacy
# "Something went wrong" text)
assert "scenario A (workflow submit) function" "grep -qE 'scenario_a' '$LIVE_SCRIPT'"
assert "scenario B (project ensure) function" "grep -qE 'scenario_b' '$LIVE_SCRIPT'"
assert "scenario C (DB write) function" "grep -qE 'scenario_c' '$LIVE_SCRIPT'"
assert "scenario D (app normalize) function" "grep -qE 'scenario_d' '$LIVE_SCRIPT'"
assert "scenario E (unknown catchall) function" "grep -qE 'scenario_e' '$LIVE_SCRIPT'"
assert "scenario F (regression-protector) function" "grep -qE 'scenario_f' '$LIVE_SCRIPT'"

# C — five SubmitBuildErrorReason discriminators are named in the script
# (locks the assertion that the LIVE test actually exercises the 5 reasons
# the Sprint 6 fix introduced — not just a single happy-path probe).
assert "BuildSubmitWorkflowFailed reason cited" "grep -q 'BuildSubmitWorkflowFailed' '$LIVE_SCRIPT'"
assert "BuildSubmitProjectEnsureFailed reason cited" "grep -q 'BuildSubmitProjectEnsureFailed' '$LIVE_SCRIPT'"
assert "BuildSubmitDBWriteFailed reason cited" "grep -q 'BuildSubmitDBWriteFailed' '$LIVE_SCRIPT'"
assert "BuildSubmitAppNormalizeFailed reason cited" "grep -q 'BuildSubmitAppNormalizeFailed' '$LIVE_SCRIPT'"
assert "BuildSubmitUnknown reason cited" "grep -q 'BuildSubmitUnknown' '$LIVE_SCRIPT'"

# D — Scenario F regression-protector against the legacy verbatim text
# (the #34 sponsor catch: "Something went wrong. Try again, or check loopzero status
# for details")
assert "scenario F asserts legacy text NEVER appears" "grep -qE 'Something went wrong' '$LIVE_SCRIPT'"
assert "scenario F + scenario E reject INTERNAL_ERROR catchall" "grep -qE 'INTERNAL_ERROR' '$LIVE_SCRIPT'"

# E — operator HARD STOP per cleanroom mandate
assert "OPERATOR HARD STOP header" "grep -qE 'OPERATOR HARD STOP|cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "plan slug cited" "grep -q '$PLAN_SLUG' '$LIVE_SCRIPT'"
assert "issue #34 cited" "grep -qE '#34|loopzero-cloud#34' '$LIVE_SCRIPT'"
assert "INQUEBRÁVEL feedback citation" "grep -qE 'feedback_issue_fix_requires_failing_e2e_test' '$LIVE_SCRIPT'"

# F — flags
assert "--skip-live flag" "grep -qE '\\-\\-skip-live' '$LIVE_SCRIPT'"
assert "--scenario= flag" "grep -qE '\\-\\-scenario=' '$LIVE_SCRIPT'"
assert "--json flag" "grep -qE '\\-\\-json' '$LIVE_SCRIPT'"
assert "--help flag" "grep -qE '\\-\\-help' '$LIVE_SCRIPT'"

# G — env contract per check-no-cloud-dev-default gate (T1.1 of plan
# m1-commit-canonical-30-fixes 2026-06-23). The cloud base URL must come from
# LOOPZERO_CLOUD_URL env, NOT a hardcoded default to cloud-dev.loopzero.example.
assert "LOOPZERO_CLOUD_URL env required (no hardcoded default)" "grep -qE 'LOOPZERO_CLOUD_URL.*:[?]' '$LIVE_SCRIPT'"

# H — anti-patterns
assert "no Co-Authored-By trailer" "! grep -qi 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "no --no-verify flag invocation" "! grep -qE '\\-\\-no-verify' '$LIVE_SCRIPT'"

# I — allowlist cross-check
assert "LIVE script registered in allowlist" "grep -qE 'dev-public-build-trigger-error-surface_test\\.sh' '$ALLOWLIST'"
assert "hermetic test registered in allowlist" "grep -qE 'check-dev-public-build-trigger-error-surface-syntax_test\\.sh' '$ALLOWLIST'"

echo "$(basename "$0"): PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
