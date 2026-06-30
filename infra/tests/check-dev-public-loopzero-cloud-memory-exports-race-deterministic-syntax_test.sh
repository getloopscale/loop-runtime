#!/usr/bin/env bash
# Hermetic structural guard for the LIVE bash regression-protector that
# closes Red Team loopzero-cloud#104 (TestMemoryExports_CreateIgnoresClientFilename
# flaky under `-race`).
#
# Pure grep + bash -n on the LIVE script source. NO network, NO go, NO
# kubectl. Wired in `task quality:gates` so the LIVE script's contract
# can't silently drift (e.g. someone deletes a scenario, drops a flag,
# adds a Co-Authored-By, removes the issue ref).

set -euo pipefail

LIVE_TEST="$(cd "$(dirname "$0")/.." && pwd)/tests/e2e/dev-public-loopzero-cloud-memory-exports-race-deterministic_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
DOGFOOD_WRAPPER="archive/canonical-tests-v1/scripts/local/dogfood-loopzero-cloud-memory-exports-race-deterministic.sh"

PASS=0
FAIL=0
FAILED_LIST=""

assert() {
    local description="$1"
    shift
    if "$@" ; then
        PASS=$((PASS + 1))
        printf '  PASS  %s\n' "$description"
    else
        FAIL=$((FAIL + 1))
        FAILED_LIST+=$'\n    '"$description"
        printf '  FAIL  %s\n' "$description"
    fi
}

assert_not() {
    local description="$1"
    shift
    if ! "$@" ; then
        PASS=$((PASS + 1))
        printf '  PASS  %s\n' "$description"
    else
        FAIL=$((FAIL + 1))
        FAILED_LIST+=$'\n    '"$description"
        printf '  FAIL  %s\n' "$description"
    fi
}

echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] ============================================================"
echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] LIVE test: $LIVE_TEST"
echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] ============================================================"

# ---------------------------------------------------------------------------
# A — Structural integrity
# ---------------------------------------------------------------------------
assert "LIVE script exists on disk" \
    test -f "$LIVE_TEST"
assert "bash -n parses LIVE script (syntactically clean)" \
    bash -n "$LIVE_TEST"
assert "strict mode 'set -euo pipefail' declared" \
    grep -qF 'set -euo pipefail' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# B — Audit-trail metadata (issue + slug + canonical references)
# ---------------------------------------------------------------------------
assert "issue ref 'loopzero-cloud#104' cited in header" \
    grep -qF 'loopzero-cloud#104' "$LIVE_TEST"
assert "slug 'loopzero-cloud-memory-exports-race-deterministic' present" \
    grep -qF 'loopzero-cloud-memory-exports-race-deterministic' "$LIVE_TEST"
assert "INQUEBRÁVEL feedback citation" \
    grep -qF 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# C — All 3 scenario function blocks present
# ---------------------------------------------------------------------------
assert "Scenario A block present" \
    grep -qE 'want_scenario A' "$LIVE_TEST"
assert "Scenario B block present" \
    grep -qE 'want_scenario B' "$LIVE_TEST"
assert "Scenario C block present" \
    grep -qE 'want_scenario C' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# D — Scenario-specific probe asserts (the canonical fix shape proofs)
# ---------------------------------------------------------------------------
assert "Scenario A asserts 'workers[[:space:]]+sync\\.WaitGroup' field probe" \
    grep -qF 'workers[[:space:]]+sync\.WaitGroup' "$LIVE_TEST"
assert "Scenario A asserts 'WaitForWorkers' method probe" \
    grep -qF 'WaitForWorkers(' "$LIVE_TEST"
assert "Scenario B asserts 't.Cleanup(h.WaitForWorkers)' registration" \
    grep -qF 't.Cleanup(h.WaitForWorkers)' "$LIVE_TEST"
assert "Scenario B asserts 't.Cleanup(func() { _ = db.Close() })' registration" \
    grep -qF 't.Cleanup(func() { _ = db.Close() })' "$LIVE_TEST"
assert "Scenario C runs 'go test -race -count=1'" \
    grep -qF 'go test -race -count=1' "$LIVE_TEST"
assert "Scenario C scopes to '-run TestMemoryExports_CreateIgnoresClientFilename'" \
    grep -qF 'TestMemoryExports_CreateIgnoresClientFilename' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# E — Flag wiring (4 canonical flags)
# ---------------------------------------------------------------------------
assert "--skip-live flag declared in case" \
    grep -qE '\-\-skip-live\)' "$LIVE_TEST"
assert "--scenario= flag declared in case" \
    grep -qE '\-\-scenario=\*\)' "$LIVE_TEST"
assert "--json flag declared in case" \
    grep -qE '\-\-json\)' "$LIVE_TEST"
assert "--help|-h flag declared in case" \
    grep -qE '\-\-help\|-h\)' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# F — Env override for loopzero-cloud dir (no hardcoded absolute path)
# ---------------------------------------------------------------------------
assert "LOOPZERO_CLOUD_DIR env override pattern present" \
    grep -qF 'LOOPZERO_CLOUD_DIR:-' "$LIVE_TEST"
assert "RACE_ITERATIONS env override pattern present" \
    grep -qF 'RACE_ITERATIONS:-' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# G — Verdict + exit code contract
# ---------------------------------------------------------------------------
assert "exit code reflects FAIL_COUNT" \
    grep -qE '\[ \$FAIL_COUNT -eq 0 \] \|\| exit 1' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# H — Anti-patterns (Co-Authored-By, --no-verify, hardcoded secrets)
# ---------------------------------------------------------------------------
assert_not "no Co-Authored-By trailer in LIVE script" \
    grep -qF 'Co-Authored-By' "$LIVE_TEST"
assert_not "no --no-verify flag use in LIVE script" \
    grep -qF -- '--no-verify' "$LIVE_TEST"

# ---------------------------------------------------------------------------
# I — Allowlist cross-check (both LIVE + hermetic + dogfood wrapper)
# ---------------------------------------------------------------------------
assert "LIVE script registered in shell-scripts-allowlist.md" \
    grep -qF 'dev-public-loopzero-cloud-memory-exports-race-deterministic_test.sh' "$ALLOWLIST"
assert "hermetic syntax test registered in shell-scripts-allowlist.md" \
    grep -qF 'check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax_test.sh' "$ALLOWLIST"
assert "dogfood wrapper registered in shell-scripts-allowlist.md" \
    grep -qF "$(basename "$DOGFOOD_WRAPPER")" "$ALLOWLIST"

echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] ============================================================"
echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] Result: $PASS PASS, $FAIL FAIL"
echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] ============================================================"

if [ $FAIL -gt 0 ] ; then
    echo "[check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax] Failed assertions:$FAILED_LIST"
    exit 1
fi
exit 0
