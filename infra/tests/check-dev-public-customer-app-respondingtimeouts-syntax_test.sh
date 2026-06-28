#!/usr/bin/env bash
# Plan red-team-77-107-closure v1.0 T1.3 — Hermetic structural guard for the
# LIVE bash regression-protector at infra/tests/e2e/dev-public-customer-app-
# respondingtimeouts_test.sh (T1.2). Locks the canonical shape against drift:
# bash -n syntax, strict mode, 3 scenarios A/B/C, 3 flags, plan slug citation,
# issue #107 citation, no Co-Authored-By, no --no-verify, allowlist entry.
#
# Pure grep + bash -n on the LIVE script source. NO network, NO kubectl, NO
# curl. Wired in `task quality:gates` via the canonical pattern of every
# sibling hermetic test under infra/tests/check-*-syntax_test.sh.
set -euo pipefail

LIVE_SCRIPT="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/tests/e2e/dev-public-customer-app-respondingtimeouts_test.sh"
ALLOWLIST="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/.claude/rules/shell-scripts-allowlist.md"
SELF="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}/infra/tests/check-dev-public-customer-app-respondingtimeouts-syntax_test.sh"

PASS=0
FAIL=0
FAILED_ASSERTIONS=()

assert() {
    local desc="$1" cmd="$2"
    if eval "$cmd" >/dev/null 2>&1; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        FAILED_ASSERTIONS+=("$desc")
    fi
}

# A: file exists
assert "LIVE script exists" "[[ -f '$LIVE_SCRIPT' ]]"
[[ -f "$LIVE_SCRIPT" ]] || {
    echo "FAIL: LIVE script not found at $LIVE_SCRIPT" >&2
    echo "PASS=$PASS FAIL=$FAIL" >&2
    exit 1
}

# B: bash -n syntax clean
assert "bash -n syntax clean" "bash -n '$LIVE_SCRIPT'"

# C: strict mode
assert "strict mode set -euo pipefail" "grep -qE '^set -euo pipefail$' '$LIVE_SCRIPT'"

# D: plan slug declared
assert "plan slug PLAN_SLUG declared" "grep -qE 'PLAN_SLUG=\"red-team-77-107-closure\"' '$LIVE_SCRIPT'"

# E: issue ref declared
assert "ISSUE_REF cites loopzero-cloud#107" "grep -qE 'ISSUE_REF=\"loopzero-cloud#107\"' '$LIVE_SCRIPT'"

# F: 3 scenario functions declared
assert "scenario_a_http_slow function declared" "grep -qE '^scenario_a_http_slow\\(\\) \\{$' '$LIVE_SCRIPT'"
assert "scenario_b_sse_stream function declared" "grep -qE '^scenario_b_sse_stream\\(\\) \\{$' '$LIVE_SCRIPT'"
assert "scenario_c_preflight function declared" "grep -qE '^scenario_c_preflight\\(\\) \\{$' '$LIVE_SCRIPT'"

# G: 3 flags wired
assert "--skip-live flag wired" "grep -qE -- '--skip-live\\)' '$LIVE_SCRIPT'"
assert "--scenario= flag wired" "grep -qE -- '--scenario=\\*\\)' '$LIVE_SCRIPT'"
assert "--json flag wired" "grep -qE -- '--json\\)' '$LIVE_SCRIPT'"
assert "--help flag wired" "grep -qE -- '--help\\)' '$LIVE_SCRIPT'"

# H: hermetic mode short-circuits before HTTP
assert "hermetic --skip-live short-circuits before HTTP" "awk '/SKIP_LIVE\\\" -eq 1/,/exit 0/' '$LIVE_SCRIPT' | grep -qE 'structural-only'"

# I: each scenario respects SCENARIO filter (single-scenario selector)
assert "scenario A respects SCENARIO filter" "grep -qE '\\[\\[ -n \"\\\$SCENARIO\" && \"\\\$SCENARIO\" != \"A\" \\]\\]' '$LIVE_SCRIPT'"
assert "scenario B respects SCENARIO filter" "grep -qE '\\[\\[ -n \"\\\$SCENARIO\" && \"\\\$SCENARIO\" != \"B\" \\]\\]' '$LIVE_SCRIPT'"
assert "scenario C respects SCENARIO filter" "grep -qE '\\[\\[ -n \"\\\$SCENARIO\" && \"\\\$SCENARIO\" != \"C\" \\]\\]' '$LIVE_SCRIPT'"

# J: writeTimeout regression-protector — scenario A asserts wall-clock window
assert "scenario A asserts wall-clock window (lower_bound + upper_bound)" "grep -qE 'lower_bound=.+SLOW_SLEEP_SECONDS.+-.+10' '$LIVE_SCRIPT' && grep -qE 'upper_bound=.+SLOW_SLEEP_SECONDS.+\\+.+20' '$LIVE_SCRIPT'"

# K: scenario A asserts canonical body shape {"ok":true}
assert "scenario A asserts canonical body shape {\"ok\":true}" "grep -qE '\"ok\".*true' '$LIVE_SCRIPT'"

# L: scenario B asserts elapsed >= duration - 10 (no early reset)
assert "scenario B asserts no early reset (elapsed >= duration - 10)" "grep -qE 'STREAM_DURATION_SECONDS.+-.+10' '$LIVE_SCRIPT'"

# M: OPERATOR HARD STOP header documented + cleanroom-up-live-test-mandate cited
assert "OPERATOR HARD STOP header documented" "grep -qE 'OPERATOR HARD STOP' '$LIVE_SCRIPT'"
assert "cleanroom-up-live-test-mandate.md cited" "grep -qE 'cleanroom-up-live-test-mandate' '$LIVE_SCRIPT'"
assert "INQUEBRÁVEL mandate cited" "grep -qE 'INQUEBR' '$LIVE_SCRIPT'"

# N: INQUEBRÁVEL feedback citation per issue-fix-requires-failing-e2e rule
assert "feedback_issue_fix_requires_failing_e2e_test_2026_06_25 cited" "grep -qE 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' '$LIVE_SCRIPT'"

# O: anti-patterns absent
assert "no Co-Authored-By trailer" "! grep -q 'Co-Authored-By' '$LIVE_SCRIPT'"
assert "no --no-verify flag" "! grep -q -- '--no-verify' '$LIVE_SCRIPT'"

# P: exit code reflects fail count (not always 0)
assert "exit reflects fail_count (exit \"\$fail_count\")" "grep -qE 'exit \"\\\$fail_count\"' '$LIVE_SCRIPT'"

# Q: allowlist cross-check (entries land in T1.4)
if [[ -f "$ALLOWLIST" ]]; then
    assert "LIVE script in allowlist" "grep -qE 'infra/tests/e2e/dev-public-customer-app-respondingtimeouts_test\\.sh' '$ALLOWLIST'"
    assert "hermetic guard in allowlist" "grep -qE 'infra/tests/check-dev-public-customer-app-respondingtimeouts-syntax_test\\.sh' '$ALLOWLIST'"
fi

# Summary
if [[ "$FAIL" -eq 0 ]]; then
    echo "PASS=$PASS FAIL=$FAIL"
    exit 0
fi
echo "PASS=$PASS FAIL=$FAIL" >&2
printf '  - %s\n' "${FAILED_ASSERTIONS[@]}" >&2
exit 1
