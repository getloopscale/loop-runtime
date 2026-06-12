#!/usr/bin/env bash
# check-chaos-c12-syntax_test.sh
#
# T4.2 hermetic structural test for the C12 chaos scenario script of plan
# 2026-06-08-pre-release-test-rigor v1.2.
#
# C12 = 2 simultaneous PRs during network-partition (mgmt cluster ↔ build
# cluster). The LIVE test requires chaos-mesh + gh CLI + admin session;
# this hermetic test verifies structural shape only.
#
# Hermetic: NO network, NO kubectl, NO chaos-mesh.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="${REPO_ROOT}/infra/scripts/local/chaos-scenarios/c12-simul-prs-during-partition.sh"
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

assert "C12 script exists"      test -f "$SCRIPT"
assert "C12 script executable"  test -x "$SCRIPT"
assert "C12 script bash -n ok"  bash -n "$SCRIPT"

# --- Shebang + strict mode --------------------------------------------------

assert_grep "shebang is bash"                "$SCRIPT" '^#!/usr/bin/env bash'
assert_grep "strict mode: set -euo pipefail" "$SCRIPT" '^set -euo pipefail'

# --- EC-11 preflight (chaos-mesh health) -----------------------------------

assert_grep "(EC-11) preflight: chaos-mesh namespace health check" \
  "$SCRIPT" '(CHAOS_NAMESPACE|chaos-mesh|chaos_mesh)'
assert_grep "(EC-11) preflight: pods Running" \
  "$SCRIPT" '(Running|status\.phase|grep Running)'

# --- NetworkChaos applied (T4.2 core) --------------------------------------

assert_grep "applies NetworkChaos via kubectl apply" \
  "$SCRIPT" 'NetworkChaos|kubectl apply'
assert_grep "partition action declared (iptables DROP / partition)" \
  "$SCRIPT" '(partition|drop|netem|action: partition)'

# --- EC-6 blast-radius scoping (selector.namespaces explicit) -------------

assert_grep "(EC-6) selector.namespaces is explicit" \
  "$SCRIPT" 'selector\.namespaces|selector:.*namespaces|namespaces:.*\[?'
assert_grep "(EC-6) selector.namespaces contains loopzero-system" \
  "$SCRIPT" 'loopzero-system'
assert_grep "(EC-6) does NOT use mode: all" \
  "$SCRIPT" '(mode: one|mode: fixed-percent|mode: all-targeted)|mode:\s*all\s*\#?'
assert_not_grep "(EC-6) mode: all is forbidden (over-broad blast radius)" \
  "$SCRIPT" 'mode:\s*all\s*$'

# --- EC-6 cleanup trap on EXIT INT TERM ------------------------------------

assert_grep "(EC-6) trap EXIT INT TERM declared" \
  "$SCRIPT" 'trap.+EXIT.+INT.+TERM'
assert_grep "(EC-6) cleanup deletes networkchaos resources" \
  "$SCRIPT" 'kubectl delete.*networkchaos'

# --- 2 PR simultaneous dispatch --------------------------------------------

assert_grep "dispatches 2 PRs concurrently"   "$SCRIPT" '(&\s*$|2.*PR|simul|parallel)'
assert_grep "gh api dispatch (or push-event)" "$SCRIPT" '(gh api.*dispatch|gh api.*refs/heads)'

# --- LoopZero-design.md invariants verified ------------------------------------

assert_grep "withEnvironmentLock OR 1×202+1×409 invariant cited" \
  "$SCRIPT" '(withEnvironmentLock|pg_advisory|202.*409|409.*202|exactly one|exactly 1)'

# --- Post-heal assertions --------------------------------------------------

assert_grep "asserts at least 1 release reaches ACTIVE post-heal" \
  "$SCRIPT" '(ACTIVE|release.*ACTIVE|at least 1)'
assert_grep "asserts no duplicate releases for same commit" \
  "$SCRIPT" '(duplicate|no 2 ACTIVE|commit_sha)'

# --- Flags + modes ---------------------------------------------------------

assert_grep "--skip-live flag declared"  "$SCRIPT" '\-\-skip-live'
assert_grep "--json flag declared"       "$SCRIPT" '\-\-json'

# --- Anti-patterns forbidden ------------------------------------------------

assert_not_grep "NO Co-Authored-By"        "$SCRIPT" 'Co-Authored-By'
assert_not_grep "NO --no-verify"           "$SCRIPT" '\-\-no-verify'

# --- chaos-test SKILL.md updated (C12 registered) --------------------------

assert "SKILL.md exists" test -f "$SKILL"
assert_grep "(SKILL) C12 row registered in cenários table" \
  "$SKILL" '^\| (\*\*)?C12(\*\*)?'
assert_grep "(SKILL) C12 description mentions network-partition + 2 PRs" \
  "$SKILL" 'C12.*(partition|network|simul|2.PR|2 PR)'

# --- Allowlist cross-check (D5) ---------------------------------------------

assert "allowlist exists" test -f "$ALLOWLIST"
assert_grep "(D5) c12-simul-prs-during-partition.sh allowlisted" \
  "$ALLOWLIST" 'c12-simul-prs-during-partition\.sh'
assert_grep "(D5) check-chaos-c12-syntax_test.sh allowlisted" \
  "$ALLOWLIST" 'check-chaos-c12-syntax_test\.sh'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
