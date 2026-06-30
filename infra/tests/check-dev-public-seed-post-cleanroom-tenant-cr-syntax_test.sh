#!/usr/bin/env bash
# check-dev-public-seed-post-cleanroom-tenant-cr-syntax_test.sh
#
# T1.1 of plan seed-post-cleanroom-tenant-cr-provisioning v1.0.
# Hermetic structural guard for the sibling LIVE bash. NO network, NO kubectl —
# pure grep + bash -n on LIVE script source.
#
# Wired in `task quality:gates` via the bash-test runner.

set -euo pipefail

LIVE="infra/tests/e2e/dev-public-seed-post-cleanroom-tenant-cr_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"

pass=0
fail=0
fails=()

assert() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then
    pass=$((pass + 1))
    printf '  PASS  %s\n' "$name"
  else
    fail=$((fail + 1))
    fails+=("$name")
    printf '  FAIL  %s\n' "$name"
  fi
}

cd "$(git rev-parse --show-toplevel 2>/dev/null || dirname "$(dirname "$(readlink -f "$0")")" )" 2>/dev/null || true

# 1. File exists
assert "file_exists" test -f "$LIVE"

# Stop early if missing — every other assertion would noise-fail
if [ ! -f "$LIVE" ]; then
  printf '\nRESULT: 1 file missing — other 16 assertions skipped\n' >&2
  printf 'Expected: %s\n' "$LIVE" >&2
  exit 1
fi

# 2. bash syntax clean
assert "bash_syntax" bash -n "$LIVE"

# 3. Strict mode
assert "strict_mode" grep -q '^set -euo pipefail' "$LIVE"

# 4. OPERATOR HARD STOP header
assert "operator_hard_stop" grep -q 'OPERATOR HARD STOP' "$LIVE"

# 5. Cleanroom mandate cited
assert "cleanroom_mandate_cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE"

# 6. Feedback rule cited (INQUEBRÁVEL)
assert "feedback_e2e_test_cited" grep -q 'feedback_issue_fix_requires_failing_e2e_test' "$LIVE"

# 7-10. Flags present
assert "flag_skip_live" grep -q -- '--skip-live' "$LIVE"
assert "flag_help"      grep -q -- '--help' "$LIVE"
assert "flag_json"      grep -q -- '--json' "$LIVE"
assert "flag_scenario"  grep -q -- '--scenario=' "$LIVE"

# 11. kubectl with mgmt context (not workload — CRDs live in mgmt)
assert "kubectl_mgmt_context" grep -q 'kubectl --context.*KUBE_CONTEXT\|kubectl --context k3d-loopzero-mgmt' "$LIVE"
assert "default_context_is_mgmt" grep -q 'KUBE_CONTEXT:-k3d-loopzero-mgmt' "$LIVE"

# 12. Correct apiVersion group — loopzero.io, NOT loopzero.sh
assert "apiversion_loopzero_io" grep -q 'tenants\.loopzero\.io' "$LIVE"
assert "no_loopzero_sh_typo"    bash -c "! grep -q 'loopzero\.sh' '$LIVE'"

# 13. Canonical tenant_ids probed
assert "admin_tenant_probe"    grep -q 'tn-78d5c2d4f678b774' "$LIVE"
assert "redteam_tenant_probe"  grep -q 'tn-79611071399a2e5e' "$LIVE"

# 14. Issue reference
assert "issue_reference"  grep -q 'loopzero-cloud#118' "$LIVE"

# 15. Forbidden trailers absent (Co-Authored-By per cycle-review hard gate)
assert "no_co_authored"  bash -c "! grep -q 'Co-Authored-By' '$LIVE'"

# 16. No --no-verify in script (would skip hooks — per global Rule 4)
assert "no_no_verify"    bash -c "! grep -q -- '--no-verify' '$LIVE'"

# 17. Allowlist entry for the LIVE script
if [ -f "$ALLOWLIST" ]; then
  assert "allowlist_live_entry" grep -q "dev-public-seed-post-cleanroom-tenant-cr_test.sh" "$ALLOWLIST"
else
  fail=$((fail + 1))
  fails+=("allowlist_live_entry (allowlist file missing)")
  printf '  FAIL  allowlist_live_entry (file missing: %s)\n' "$ALLOWLIST"
fi

printf '\n============================================================\n'
printf 'RESULT: %d PASS, %d FAIL\n' "$pass" "$fail"
if [ "${#fails[@]}" -gt 0 ]; then
  printf 'Failed:\n'
  printf '  - %s\n' "${fails[@]}"
fi
printf '============================================================\n'

[ "$fail" -eq 0 ]
