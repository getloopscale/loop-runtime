#!/usr/bin/env bash
# Hermetic structural guard for Red Team loopzero-cloud#42 closure
# (billing seats counter sourced from tenant_members instead of
# append-only usage_records).

set -euo pipefail

LIVE_TEST="$(cd "$(dirname "$0")/.." && pwd)/tests/e2e/dev-public-loopzero-cloud-billing-seats-from-tenant-members_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
DOGFOOD_WRAPPER="archive/canonical-tests-v1/scripts/local/dogfood-loopzero-cloud-billing-seats-from-tenant-members.sh"

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

echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] ============================================================"
echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] LIVE test: $LIVE_TEST"
echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] ============================================================"

# Structural
assert "LIVE script exists on disk" test -f "$LIVE_TEST"
assert "bash -n parses LIVE script (syntactically clean)" bash -n "$LIVE_TEST"
assert "strict mode 'set -euo pipefail' declared" grep -qF 'set -euo pipefail' "$LIVE_TEST"

# Audit-trail metadata
assert "issue ref 'loopzero-cloud#42' cited in header" grep -qF 'loopzero-cloud#42' "$LIVE_TEST"
assert "slug 'loopzero-cloud-billing-seats-from-tenant-members' present" \
    grep -qF 'loopzero-cloud-billing-seats-from-tenant-members' "$LIVE_TEST"
assert "INQUEBRÁVEL feedback citation" grep -qF 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE_TEST"

# 3 scenario function blocks
assert "Scenario A block present" grep -qE 'want_scenario A' "$LIVE_TEST"
assert "Scenario B block present" grep -qE 'want_scenario B' "$LIVE_TEST"
assert "Scenario C block present" grep -qE 'want_scenario C' "$LIVE_TEST"

# Scenario-specific probes
assert "Scenario A asserts h.countSeats(r.Context(), tenantID) wire-up" \
    grep -qF 'h\.countSeats\(r\.Context\(\), tenantID\)' "$LIVE_TEST"
assert "Scenario B asserts 'SELECT COUNT(*) FROM tenant_members WHERE tenant_id = \$1'" \
    grep -qF 'SELECT COUNT(*) FROM tenant_members WHERE tenant_id = $1' "$LIVE_TEST"
assert "Scenario C runs the pre-existing #42 closure test" \
    grep -qF 'TestGetBilling_Seats_FromTenantMembers_RedTeam42' "$LIVE_TEST"
assert "Scenario C invokes 'go test -count=1'" \
    grep -qF 'go test -count=1' "$LIVE_TEST"

# Env override + exit contract
assert "LOOPZERO_CLOUD_DIR env override pattern present" grep -qF 'LOOPZERO_CLOUD_DIR:-' "$LIVE_TEST"
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
    grep -qF 'dev-public-loopzero-cloud-billing-seats-from-tenant-members_test.sh' "$ALLOWLIST"
assert "hermetic syntax test registered in shell-scripts-allowlist.md" \
    grep -qF 'check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax_test.sh' "$ALLOWLIST"
assert "dogfood wrapper registered in shell-scripts-allowlist.md" \
    grep -qF "$(basename "$DOGFOOD_WRAPPER")" "$ALLOWLIST"

echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] ============================================================"
echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] Result: $PASS PASS, $FAIL FAIL"
echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] ============================================================"

if [ $FAIL -gt 0 ] ; then
    echo "[check-dev-public-loopzero-cloud-billing-seats-from-tenant-members-syntax] Failed assertions:$FAILED_LIST"
    exit 1
fi
exit 0
