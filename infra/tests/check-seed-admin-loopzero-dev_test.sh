#!/usr/bin/env bash
# check-seed-admin-loopzero-dev_test.sh
#
# T0.2 hermetic structural test for `infra/scripts/local/seed-admin-loopzero-dev.sh`
# of plan 2026-06-08-pre-release-test-rigor v1.2.
#
# Asserts structure ONLY — does NOT invoke LIVE Kratos/PG. The LIVE proof
# is the operator-run `bash infra/scripts/local/seed-admin-loopzero-dev.sh`
# itself (the script's --verify mode probes the session against Kratos
# whoami + engine API /projects per plan EC-3 absorption).
#
# Hermetic: NO network, NO kubectl, NO PG.
#
# Usage: bash infra/tests/check-seed-admin-loopzero-dev_test.sh
# Exit 0 PASS / non-zero FAIL count.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRIPT="${REPO_ROOT}/infra/scripts/local/seed-admin-loopzero-dev.sh"
ALLOWLIST="${REPO_ROOT}/.claude/rules/shell-scripts-allowlist.md"
RUNBOOK="${REPO_ROOT}/docs/operations/local-public-dashboard.md"

PASS=0
FAIL=0

assert() {
  local msg=$1 ; shift
  if "$@" > /dev/null 2>&1 ; then
    printf '[PASS] %s\n' "$msg"
    PASS=$((PASS + 1))
  else
    printf '[FAIL] %s\n' "$msg"
    FAIL=$((FAIL + 1))
  fi
}

assert_grep() {
  local msg=$1 file=$2 pattern=$3
  if grep -qE -- "$pattern" "$file" 2>/dev/null ; then
    printf '[PASS] %s\n' "$msg"
    PASS=$((PASS + 1))
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
    printf '[PASS] %s\n' "$msg"
    PASS=$((PASS + 1))
  fi
}

# --- Script existence + syntax ---------------------------------------------

assert "seed script file exists"     test -f "$SCRIPT"
assert "seed script is executable"   test -x "$SCRIPT"
assert "seed script bash -n syntax"  bash -n "$SCRIPT"

# --- De-costume gate (home-unhomed-logic-suspect-keeps T2.1) ----------------
NO_HEREDOC_SEED() { [ "$(grep -cE 'python3?[[:space:]]+(-[[:space:]]+)?<<|<<[[:space:]]*['\''\"]*?(PY|PYEOF|PYTHON)' "$SCRIPT")" = "0" ] ; }
assert "no python3 heredoc remains in seed-admin-loopzero-dev.sh" NO_HEREDOC_SEED
assert "seed_admin_loopzero_dev.py compiles" python3 -m py_compile "${REPO_ROOT}/infra/scripts/local/seed_admin_loopzero_dev.py"

# --- Shebang + strict mode --------------------------------------------------

assert_grep "shebang is bash"                "$SCRIPT" '^#!/usr/bin/env bash'
assert_grep "strict mode: set -euo pipefail" "$SCRIPT" '^set -euo pipefail'

# --- Canonical user + tenant lookup -----------------------------------------

assert_grep "canonical email admin@loopzero.example hard-coded" \
  "$SCRIPT" 'admin@loopzero\.dev'
assert_grep "looks up Kratos identity via admin /admin/identities" \
  "$SCRIPT" '/admin/identities'
assert_grep "uses credentials_identifier query to find identity" \
  "$SCRIPT" 'credentials_identifier'

# --- EC-3: 3-table tenant context (MUST FIX from edge-case review) ---------

assert_grep "(EC-3) populates tenant_members table" \
  "$SCRIPT" 'INSERT INTO tenant_members'
assert_grep "(EC-3) populates user_identity_map table" \
  "$SCRIPT" 'INSERT INTO user_identity_map'
assert_grep "(EC-3) sets users.active_tenant_id" \
  "$SCRIPT" 'UPDATE users SET active_tenant_id'
assert_grep "(EC-3) idempotency via ON CONFLICT DO NOTHING" \
  "$SCRIPT" 'ON CONFLICT.*DO NOTHING'

# --- Session minting via Kratos admin API ----------------------------------

assert_grep "mints session via Kratos admin /admin/identities/.../sessions" \
  "$SCRIPT" '/admin/identities/[^"]*/sessions|admin/identities/\$'
assert_grep "persists session token to ~/.loopzero/admin-loopzero-dev-session.txt" \
  "$SCRIPT" '\.loopzero/admin-loopzero-dev-session\.txt'
assert_grep "session file mode 0600 enforced" \
  "$SCRIPT" 'chmod (0)?600'

# --- EC-9: --refresh-only flag (SHOULD TEST from edge-case review) ---------

assert_grep "(EC-9) --refresh-only flag declared" \
  "$SCRIPT" '\-\-refresh-only'
assert_grep "(EC-9) refresh-only skips PG mutations" \
  "$SCRIPT" 'REFRESH_ONLY|refresh_only|refresh-only mode'

# --- Idempotency ------------------------------------------------------------

assert_grep "identity-create branch is conditional on absence" \
  "$SCRIPT" 'if .*\b[Ii]dentity'
assert_grep "transaction BEGIN/COMMIT around mutations" \
  "$SCRIPT" 'BEGIN;'
assert_grep "transaction COMMIT explicit" \
  "$SCRIPT" 'COMMIT;'

# --- Exit codes per plan ----------------------------------------------------

assert_grep "exit 1 path (user/tenant not resolved)" \
  "$SCRIPT" 'exit 1'
assert_grep "exit 2 path (PG/Kratos unreachable)" \
  "$SCRIPT" 'exit 2'

# --- Anti-patterns forbidden ------------------------------------------------

assert_not_grep "NO Co-Authored-By trailer leak"    "$SCRIPT" 'Co-Authored-By'
assert_not_grep "NO --no-verify in script body"     "$SCRIPT" '\-\-no-verify'
assert_not_grep "NO hardcoded password placeholder" "$SCRIPT" 'REPLACE_VIA|REPLACE-VIA|PLACEHOLDER'

# --- Allowlist cross-check (D5 — same-commit allowlist mandate) ------------

assert "allowlist file exists" test -f "$ALLOWLIST"
assert_grep "(D5) seed-admin-loopzero-dev.sh allowlisted" \
  "$ALLOWLIST" 'seed-admin-loopzero-dev\.sh'
assert_grep "(D5) check-seed-admin-loopzero-dev_test.sh allowlisted" \
  "$ALLOWLIST" 'check-seed-admin-loopzero-dev_test\.sh'

# --- Runbook updated --------------------------------------------------------

assert "runbook file exists" test -f "$RUNBOOK"
assert_grep "(canonical user section) runbook mentions admin@loopzero.example" \
  "$RUNBOOK" 'admin@loopzero\.dev'
assert_grep "(canonical user section) runbook references the seed script" \
  "$RUNBOOK" 'seed-admin-loopzero-dev'

# --- Summary ----------------------------------------------------------------

printf '\n=== %s ===\n' "$(basename "$0")"
printf 'Asserts: %d / %d PASS\n' "$PASS" "$((PASS + FAIL))"

exit $FAIL
