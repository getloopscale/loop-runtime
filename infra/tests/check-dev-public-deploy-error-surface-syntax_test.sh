#!/usr/bin/env bash
# Sprint 5 plan qa-sprint5-cleanup-2026-06-25 T3.1 — hermetic structural test
# for infra/tests/e2e/dev-public-deploy-error-surface_test.sh (T3.2 LIVE script).
# Closes loopzero-cloud#33. Pure grep + bash -n; NO network, NO kubectl.
set -euo pipefail

LIVE_SCRIPT="${LIVE_SCRIPT:-infra/tests/e2e/dev-public-deploy-error-surface_test.sh}"
PLAN_SLUG="qa-sprint5-cleanup-2026-06-25"
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

# B — 4 scenarios documented + dispatched
assert "scenario A function" "grep -qE 'scenario_a' '$LIVE_SCRIPT'"
assert "scenario B function" "grep -qE 'scenario_b' '$LIVE_SCRIPT'"
assert "scenario C function" "grep -qE 'scenario_c' '$LIVE_SCRIPT'"
assert "scenario D function (#33 verbatim repro)" "grep -qE 'scenario_d' '$LIVE_SCRIPT'"
assert "preflight function" "grep -qE 'preflight' '$LIVE_SCRIPT'"

# C — operator HARD STOP per cleanroom mandate
assert "OPERATOR HARD STOP header" "grep -qE 'OPERATOR HARD STOP|cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "plan slug cited" "grep -q '$PLAN_SLUG' '$LIVE_SCRIPT'"
assert "issue #33 cited" "grep -qE '#33|loopzero-cloud#33' '$LIVE_SCRIPT'"

# D — flags
assert "--skip-live flag" "grep -qE '\\-\\-skip-live' '$LIVE_SCRIPT'"
assert "--scenario= flag" "grep -qE '\\-\\-scenario=' '$LIVE_SCRIPT'"
assert "--json flag" "grep -qE '\\-\\-json' '$LIVE_SCRIPT'"
assert "--help flag" "grep -qE '\\-\\-help' '$LIVE_SCRIPT'"

# E — anti-patterns
assert "no Co-Authored-By trailer" "! grep -qi 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "no --no-verify flag invocation" "! grep -qE '\\-\\-no-verify' '$LIVE_SCRIPT'"

# F — allowlist cross-check
assert "LIVE script registered in allowlist" "grep -qE 'dev-public-deploy-error-surface_test\\.sh' '$ALLOWLIST'"
assert "hermetic test registered in allowlist" "grep -qE 'check-dev-public-deploy-error-surface-syntax_test\\.sh' '$ALLOWLIST'"

# G — scenario A asserts reason != RemoteRejectedPush on fresh name (the #33 root)
assert "scenario A asserts non-RemoteRejectedPush on fresh name" "grep -qE 'RemoteRejectedPush' '$LIVE_SCRIPT'"

echo "$(basename "$0"): PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
