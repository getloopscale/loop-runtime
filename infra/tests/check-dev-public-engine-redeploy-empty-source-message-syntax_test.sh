#!/usr/bin/env bash
# Hermetic structural guard for Red Team loopzero-cloud#126 closure
# (engine redeploy_of empty-source error message differentiates from
# fresh-deploy missing-apps copy).

set -euo pipefail

LIVE_TEST="$(cd "$(dirname "$0")/.." && pwd)/tests/e2e/dev-public-engine-redeploy-empty-source-message_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
DOGFOOD_WRAPPER="archive/canonical-tests-v1/scripts/local/dogfood-engine-redeploy-empty-source-message.sh"

PASS=0
FAIL=0
FAILED_LIST=""

assert() {
    local description="$1"; shift
    if "$@" ; then PASS=$((PASS + 1)); printf '  PASS  %s\n' "$description"
    else FAIL=$((FAIL + 1)); FAILED_LIST+=$'\n    '"$description"; printf '  FAIL  %s\n' "$description"; fi
}

assert_not() {
    local description="$1"; shift
    if ! "$@" ; then PASS=$((PASS + 1)); printf '  PASS  %s\n' "$description"
    else FAIL=$((FAIL + 1)); FAILED_LIST+=$'\n    '"$description"; printf '  FAIL  %s\n' "$description"; fi
}

echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] ============================================================"
echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] LIVE test: $LIVE_TEST"
echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] ============================================================"

# Structural
assert "LIVE script exists on disk" test -f "$LIVE_TEST"
assert "bash -n parses LIVE script (syntactically clean)" bash -n "$LIVE_TEST"
assert "strict mode 'set -euo pipefail' declared" grep -qF 'set -euo pipefail' "$LIVE_TEST"

# Audit-trail metadata
assert "issue ref 'loopzero-cloud#126' cited in header" grep -qF 'loopzero-cloud#126' "$LIVE_TEST"
assert "slug 'engine-redeploy-empty-source-message' present" grep -qF 'engine-redeploy-empty-source-message' "$LIVE_TEST"
assert "INQUEBRÁVEL feedback citation" grep -qF 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE_TEST"
assert "family #122/#123 documented" grep -qE '#122.*#123|#122 / #123' "$LIVE_TEST"

# 3 scenario function blocks
assert "Scenario A block present" grep -qE 'want_scenario A' "$LIVE_TEST"
assert "Scenario B block present" grep -qE 'want_scenario B' "$LIVE_TEST"
assert "Scenario C block present" grep -qE 'want_scenario C' "$LIVE_TEST"

# Scenario-specific probes
assert "Scenario A asserts 'no apps to redeploy' literal" \
    grep -qF 'no apps to redeploy' "$LIVE_TEST"
assert "Scenario B asserts 'if req.RedeployOf != \"\"' gate" \
    grep -qF 'if req.RedeployOf != ""' "$LIVE_TEST"
assert "Scenario C asserts closure unit test name" \
    grep -qF 'TestBuilds_Redeploy_SourceWithoutAppsReturnsCanonicalRedeployError' "$LIVE_TEST"
assert "Scenario C invokes 'go test -count=1'" \
    grep -qF 'go test -count=1' "$LIVE_TEST"

# Env override + exit contract
assert "ENGINE_DIR env override pattern present" grep -qF 'ENGINE_DIR:-' "$LIVE_TEST"
assert "exit code reflects FAIL_COUNT" \
    grep -qE '\[ \$FAIL_COUNT -eq 0 \] \|\| exit 1' "$LIVE_TEST"

# Flag wiring
assert "--skip-live flag declared in case" grep -qE '\-\-skip-live\)' "$LIVE_TEST"
assert "--scenario= flag declared in case" grep -qE '\-\-scenario=\*\)' "$LIVE_TEST"
assert "--json flag declared in case" grep -qE '\-\-json\)' "$LIVE_TEST"
assert "--help|-h flag declared in case" grep -qE '\-\-help\|-h\)' "$LIVE_TEST"

# Anti-patterns
assert_not "no Co-Authored-By trailer in LIVE script" grep -qF 'Co-Authored-By' "$LIVE_TEST"
assert_not "no --no-verify flag use in LIVE script" grep -qF -- '--no-verify' "$LIVE_TEST"

# Allowlist cross-check
assert "LIVE script registered in shell-scripts-allowlist.md" \
    grep -qF 'dev-public-engine-redeploy-empty-source-message_test.sh' "$ALLOWLIST"
assert "hermetic syntax test registered in shell-scripts-allowlist.md" \
    grep -qF 'check-dev-public-engine-redeploy-empty-source-message-syntax_test.sh' "$ALLOWLIST"
assert "dogfood wrapper registered in shell-scripts-allowlist.md" \
    grep -qF "$(basename "$DOGFOOD_WRAPPER")" "$ALLOWLIST"

echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] ============================================================"
echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] Result: $PASS PASS, $FAIL FAIL"
echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] ============================================================"

if [ $FAIL -gt 0 ] ; then
    echo "[check-dev-public-engine-redeploy-empty-source-message-syntax] Failed assertions:$FAILED_LIST"
    exit 1
fi
exit 0
