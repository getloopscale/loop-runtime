#!/usr/bin/env bash
# check-connect-canonical-fixture_test.sh
#
# T2.1 hermetic structural test for `infra/scripts/local/connect-canonical-fixture.sh`
# of plan 2026-06-08-pre-release-test-rigor v1.2.
#
# The script is LIVE-only (requires gh auth + LoopZero CLI + real GitHub App + admin
# session), so this hermetic test verifies structural shape and absorption of
# EC-10 (orphan installation handling) + EC-12 (gh auth preflight). The LIVE
# proof is the operator-run script itself with --verify flag.
#
# Hermetic: NO network, NO kubectl, NO gh CLI invocation.
set -u

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="${REPO_ROOT}/infra/scripts/local/connect-canonical-fixture.sh"
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
    printf '[FAIL] %s (forbidden pattern present: %s)\n' "$msg" "$pattern"
    FAIL=$((FAIL + 1))
  else
    printf '[PASS] %s\n' "$msg" ; PASS=$((PASS + 1))
  fi
}

# --- Script existence + syntax ---------------------------------------------

assert "script file exists"     test -f "$SCRIPT"
assert "script is executable"   test -x "$SCRIPT"
assert "script bash -n syntax"  bash -n "$SCRIPT"

# --- Shebang + strict mode --------------------------------------------------

assert_grep "shebang is bash"                "$SCRIPT" '^#!/usr/bin/env bash'
assert_grep "strict mode: set -euo pipefail" "$SCRIPT" '^set -euo pipefail'

# --- Canonical targets ------------------------------------------------------

assert_grep "canonical fixture repo getloopscale/loopzero-repotest" \
  "$SCRIPT" 'getloopscale/loopzero-repotest'
assert_grep "canonical project name 'teste'" \
  "$SCRIPT" '\bteste\b'
assert_grep "consumes admin@loopzero.example session from T0.2" \
  "$SCRIPT" 'admin-loopzero-dev-session\.txt|admin@loopzero\.dev'

# --- EC-12 (preflight gh auth + rate-limit) ---------------------------------

assert_grep "(EC-12) preflight: gh auth status check" \
  "$SCRIPT" 'gh auth status'
assert_grep "(EC-12) preflight: gh rate_limit check" \
  "$SCRIPT" 'rate_limit|X-RateLimit'
assert_grep "(EC-12) preflight: aborts with clear gh auth message" \
  "$SCRIPT" '(gh auth login|authenticate.*gh)'

# --- EC-10 (orphan installation handling) -----------------------------------

assert_grep "(EC-10) detects installation state" \
  "$SCRIPT" '(installation_id|installation\.id|installations)'
assert_grep "(EC-10) re-install path for orphan" \
  "$SCRIPT" '(orphan|uninstalled|status.*active|reinstall|re-install)'
assert_grep "(EC-10) idempotent — no 'already installed' loop" \
  "$SCRIPT" '(idempot|already.*install|conflict)'

# --- GitHub App install via gh API ------------------------------------------

assert_grep "uses gh api for GitHub App operations" \
  "$SCRIPT" 'gh api'
assert_grep "checks installation via /repos/{owner}/{repo}/installation" \
  "$SCRIPT" '/repos/.+/installation'

# --- LoopZero CLI for project creation ------------------------------------------

assert_grep "invokes loopzero CLI for project create (CLI-First per dogfood SKILL)" \
  "$SCRIPT" '(\$LOOPZERO|loopzero project create|loopzero\s+project)'

# --- Idempotency + flags ----------------------------------------------------

assert_grep "--verify flag declared (LIVE proof on second invocation)" \
  "$SCRIPT" '\-\-verify'
assert_grep "--dry-run flag declared" \
  "$SCRIPT" '\-\-dry-run'
assert_grep "idempotency: explicit comment OR test of existing state" \
  "$SCRIPT" '(Idempot|idempot|already exists|already present|skip if exists)'

# --- Exit codes per plan convention ----------------------------------------

assert_grep "exit 1 path (preflight failure)" "$SCRIPT" 'exit 1'
assert_grep "exit 2 path (LIVE infrastructure unreachable)" "$SCRIPT" 'exit 2'

# --- Anti-patterns forbidden ------------------------------------------------

assert_not_grep "NO Co-Authored-By trailer leak"      "$SCRIPT" 'Co-Authored-By'
assert_not_grep "NO --no-verify in script body"       "$SCRIPT" '\-\-no-verify'
assert_not_grep "NO hardcoded GitHub PAT placeholder" "$SCRIPT" '(ghp_[A-Za-z0-9]+|ghs_[A-Za-z0-9]+|REPLACE_VIA_GITHUB_TOKEN)'

# --- Allowlist cross-check (D5) ---------------------------------------------

assert "allowlist file exists" test -f "$ALLOWLIST"
assert_grep "(D5) connect-canonical-fixture.sh allowlisted" \
  "$ALLOWLIST" 'connect-canonical-fixture\.sh'
assert_grep "(D5) check-connect-canonical-fixture_test.sh allowlisted" \
  "$ALLOWLIST" 'check-connect-canonical-fixture_test\.sh'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"
exit $FAIL
