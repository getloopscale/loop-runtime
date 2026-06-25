#!/usr/bin/env bash
# Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T4.1 — hermetic structural test
# for infra/tests/e2e/dev-public-info-disclosure-scrub_test.sh (Sprint 7
# T1.2 LIVE script, closes loopzero-cloud#35 info-disclosure CWE-209). Completes
# the canonical 3-script triplet debt left behind when Sprint 7 shipped
# LIVE-only. Pure grep + bash -n; NO network, NO kubectl.
set -euo pipefail

LIVE_SCRIPT="${LIVE_SCRIPT:-infra/tests/e2e/dev-public-info-disclosure-scrub_test.sh}"
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

# B — all 7 scenarios A-G documented + dispatched
assert "scenario A (trigger error response) function" "grep -qE 'scenario_a' '$LIVE_SCRIPT'"
assert "scenarios B/C/D/E/F use assert_no_term helper or per-scenario function" "grep -qE 'assert_no_term|scenario_b|scenario_c|scenario_d|scenario_e|scenario_f' '$LIVE_SCRIPT'"
assert "scenario G (negative-direction regression-protector) function" "grep -qE 'scenario_g' '$LIVE_SCRIPT'"

# C — Cross-Project Rule 9 forbidden terms denylist coverage (the 5
# forbidden terms the scrub MUST extend per Sprint 7).
assert "forbidden term 'argo' tested" "grep -qE \"['\\\"\\(]argo['\\\"\\)]|term.*argo\" '$LIVE_SCRIPT'"
assert "forbidden term 'argoproj' tested" "grep -q 'argoproj' '$LIVE_SCRIPT'"
assert "forbidden term 'serviceaccount' tested" "grep -qi 'serviceaccount' '$LIVE_SCRIPT'"
assert "forbidden term 'namespace' tested" "grep -qi 'namespace' '$LIVE_SCRIPT'"
assert "forbidden filesystem path '/tmp/loopzero-source-' tested" "grep -qE '/tmp/loopzero-source-' '$LIVE_SCRIPT'"

# D — Scenario G negative-direction regression-protector
# (user-supplied terms like 'my-argo-test' as a project name MUST survive
# the scrub — otherwise the scrub is too aggressive and breaks UX).
assert "scenario G asserts user-supplied terms survive" "grep -qE 'my-argo-test|negative|survive' '$LIVE_SCRIPT'"

# E — operator HARD STOP per cleanroom mandate
assert "OPERATOR HARD STOP header" "grep -qE 'OPERATOR HARD STOP|cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "Sprint 7 plan slug cited" "grep -q '$SPRINT7_SLUG' '$LIVE_SCRIPT'"
assert "issue #35 cited" "grep -qE '#35|loopzero-cloud#35' '$LIVE_SCRIPT'"
assert "CWE-209 cited" "grep -qE 'CWE-?209' '$LIVE_SCRIPT'"

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

# I — allowlist cross-check
assert "LIVE script registered in allowlist" "grep -qE 'dev-public-info-disclosure-scrub_test\\.sh' '$ALLOWLIST'"
assert "hermetic test registered in allowlist" "grep -qE 'check-dev-public-info-disclosure-scrub-syntax_test\\.sh' '$ALLOWLIST'"
assert "Sprint 8 retroactive completion note" "grep -qE '$SPRINT8_SLUG' '$ALLOWLIST'"

echo "$(basename "$0"): PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
