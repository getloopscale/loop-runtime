#!/usr/bin/env bash
# check-chaos-c11-syntax_test.sh
#
# T4.1 hermetic structural test for the C11 chaos scenario script of plan
# 2026-06-08-pre-release-test-rigor v1.2.
#
# C11 = webhook concurrent with pod-kill (loopzero-api leader). The LIVE test
# requires chaos-mesh + gh CLI + admin session; this hermetic test verifies
# structural shape only.
#
# Hermetic: NO network, NO kubectl, NO chaos-mesh.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="${REPO_ROOT}/infra/scripts/local/chaos-scenarios/c11-webhook-concurrent-podkill.sh"
SKILL="${REPO_ROOT}/.claude/skills/chaos-test/SKILL.md"
ALLOWLIST="${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"

PASS=0
FAIL=0

assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s\n' "$msg" ; FAIL=$((FAIL + 1))
  fi
}

assert_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  else
    printf '[FAIL] %s (pattern: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  fi
}

assert_not_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[FAIL] %s (forbidden: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  else
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  fi
}

# --- Script existence + syntax ---------------------------------------------

assert "C11 script exists"      test -f "$SCRIPT"
assert "C11 script executable"  test -x "$SCRIPT"
assert "C11 script bash -n ok"  bash -n "$SCRIPT"

# --- Shebang + strict mode --------------------------------------------------

assert_grep "shebang is bash"                "$SCRIPT" '^#!/usr/bin/env bash'
assert_grep "strict mode: set -euo pipefail" "$SCRIPT" '^set -euo pipefail'

# --- EC-11 preflight (chaos-mesh health) -----------------------------------

assert_grep "(EC-11) preflight: chaos-mesh namespace health check" \
  "$SCRIPT" '(CHAOS_NAMESPACE|chaos-mesh|chaos_mesh)'
assert_grep "(EC-11) preflight: chaos-mesh pods Running ≥ 4 (or equivalent)" \
  "$SCRIPT" '(Running|grep Running|status\.phase)'

# --- PodChaos manifest applied (T4.1 specific) -----------------------------

assert_grep "applies PodChaos via kubectl apply"  "$SCRIPT" 'PodChaos|kubectl apply'
assert_grep "targets loopzero-api (leader)"           "$SCRIPT" '(loopzero-api|app=loopzero-api)'
assert_grep "pod-kill action"                     "$SCRIPT" '(pod-kill|action: pod-kill|pod_kill)'

# --- Concurrent webhook dispatch -------------------------------------------

assert_grep "concurrent dispatch (& parallelism)" "$SCRIPT" '(\&\s*$|&\s+wait|\bbash.*&\b|gh api.*&)'
assert_grep "gh api dispatch (or push-event)"     "$SCRIPT" '(gh api.*dispatch|gh api.*refs/heads)'

# --- Chain completion assertion --------------------------------------------

assert_grep "chain link 9 (HTTPS 200) assertion post-recovery" \
  "$SCRIPT" '(HTTPS 200|200|curl.*-w|http_code)'
assert_grep "25 min budget documented (5 chaos + 20 webhook chain)" \
  "$SCRIPT" '(25.*min|25 min|MAX_DURATION|1500)'

# --- Cleanup trap (no orphan chaos resources) -------------------------------

assert_grep "cleanup trap EXIT INT TERM"          "$SCRIPT" 'trap.*EXIT'
assert_grep "deletes PodChaos resources on exit"  "$SCRIPT" '(kubectl.*delete.*podchaos|delete -f|delete podchaos)'

# --- SLO assertion ---------------------------------------------------------

assert_grep "SLO error rate < 1% documented"      "$SCRIPT" '(1%|error.*rate|SLO)'

# --- Flags + modes ---------------------------------------------------------

assert_grep "--skip-live flag declared"           "$SCRIPT" '\-\-skip-live'
assert_grep "--json flag declared"                "$SCRIPT" '\-\-json'

# --- Anti-patterns forbidden ------------------------------------------------

assert_not_grep "NO Co-Authored-By trailer"       "$SCRIPT" 'Co-Authored-By'
assert_not_grep "NO --no-verify in body"          "$SCRIPT" '\-\-no-verify'
assert_not_grep "NO orphan chaos-mesh resource"   "$SCRIPT" 'sleep.*infinity|exit 0.*no cleanup'

# --- chaos-test SKILL.md updated (C11 registered) --------------------------

assert "SKILL.md exists" test -f "$SKILL"
assert_grep "(SKILL) C11 row registered in cenários table" \
  "$SKILL" '^\| (\*\*)?C11(\*\*)?'
assert_grep "(SKILL) C11 description mentions webhook + pod-kill" \
  "$SKILL" 'C11.*(webhook|podkill|pod.kill|pod-kill)'

# --- Allowlist cross-check (D5) ---------------------------------------------

assert "allowlist exists" test -f "$ALLOWLIST"
assert_grep "(D5) c11-webhook-concurrent-podkill.sh allowlisted" \
  "$ALLOWLIST" 'c11-webhook-concurrent-podkill\.sh'
assert_grep "(D5) check-chaos-c11-syntax_test.sh allowlisted" \
  "$ALLOWLIST" 'check-chaos-c11-syntax_test\.sh'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
