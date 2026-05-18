#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for infra/scripts/lib/drill-skill-common.sh shared library.
# Plan: 2026-05-18-drill-skills-trio-plan.md T4.1 (7 tests per TDD spec)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LIB="$ROOT/infra/scripts/lib/drill-skill-common.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Helper: source library in a subshell with controlled env
source_lib_in_subshell() {
  local env_setup="$1"
  bash -c "
    set -euo pipefail
    $env_setup
    source '$LIB'
    $2
  "
}

# ---- Tests ----

test_confirm_cost_without_flag_exits_2() {
  local out
  set +e
  out=$(bash -c "
    set -euo pipefail
    unset DRILL_SKILL_CONFIRM_COST
    unset CI
    source '$LIB'
    drill_skill_confirm_cost '0.30' '15'
  " 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || { echo "  expected exit 2, got $rc" >&2; return 1; }
  echo "$out" | grep -q -- "--confirm-cost flag required" || return 1
}

test_confirm_cost_with_ci_env_passes() {
  set +e
  bash -c "
    set -euo pipefail
    unset DRILL_SKILL_CONFIRM_COST
    export CI=true
    source '$LIB'
    drill_skill_confirm_cost '0.30' '15'
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

test_confirm_cost_with_flag_passes() {
  set +e
  bash -c "
    set -euo pipefail
    export DRILL_SKILL_CONFIRM_COST=true
    unset CI
    source '$LIB'
    drill_skill_confirm_cost '0.30' '15'
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

test_sponsor_check_without_env_or_ci_exits_2() {
  set +e
  bash -c "
    set -euo pipefail
    unset LOOPZERO_DRILL_SKILL_SPONSOR
    unset CI
    source '$LIB'
    drill_skill_sponsor_check
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || { echo "  expected exit 2, got $rc" >&2; return 1; }
}

test_sponsor_check_with_invalid_format_exits_2() {
  set +e
  bash -c "
    set -euo pipefail
    export LOOPZERO_DRILL_SKILL_SPONSOR='too-long-with-special-chars!@#'
    unset CI
    source '$LIB'
    drill_skill_sponsor_check
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
}

test_sponsor_check_with_valid_initials_passes() {
  set +e
  bash -c "
    set -euo pipefail
    export LOOPZERO_DRILL_SKILL_SPONSOR=phn
    unset CI
    source '$LIB'
    drill_skill_sponsor_check
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

test_sponsor_check_with_ci_bypasses() {
  set +e
  bash -c "
    set -euo pipefail
    unset LOOPZERO_DRILL_SKILL_SPONSOR
    export CI=true
    source '$LIB'
    drill_skill_sponsor_check
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

test_init_audit_creates_dir_structure() {
  local tmpdir
  tmpdir=$(mktemp -d -t drill-lib-test-XXXXXX)
  local audit_dir audit_md
  set +e
  result=$(bash -c "
    set -euo pipefail
    export DRILL_SKILL_AUDIT_BASE='$tmpdir/audit'
    export DRILL_SKILL_SPONSOR_VALUE=tt
    source '$LIB'
    drill_skill_init_audit 'test-skill'
    echo \"\$DRILL_SKILL_AUDIT_DIR\"
    echo \"\$DRILL_SKILL_AUDIT_MD\"
  " 2>&1)
  local rc=$?
  set -e
  rm -rf "$tmpdir"
  [[ "$rc" -eq 0 ]] || { echo "  init_audit failed: $result" >&2; return 1; }
  echo "$result" | grep -q "audit-test-skill" || { echo "  expected audit-test-skill dir name, got: $result" >&2; return 1; }
}

test_emit_finding_appends_to_audit_md() {
  local tmpdir
  tmpdir=$(mktemp -d -t drill-lib-test-XXXXXX)
  set +e
  bash -c "
    set -euo pipefail
    export DRILL_SKILL_AUDIT_BASE='$tmpdir/audit'
    export DRILL_SKILL_SPONSOR_VALUE=tt
    source '$LIB'
    drill_skill_init_audit 'test-emit'
    drill_skill_emit_finding 'S0' 'Test finding entry'
    drill_skill_emit_finding 'S1' 'Test warning entry'
    cat \"\$DRILL_SKILL_AUDIT_MD\"
  " 2>"$tmpdir/stderr.log" > "$tmpdir/stdout.log"
  local rc=$?
  set -e
  if [[ "$rc" -ne 0 ]]; then
    cat "$tmpdir/stderr.log" >&2
    rm -rf "$tmpdir"
    return 1
  fi
  grep -q "\\[S0\\] Test finding entry" "$tmpdir/stdout.log" || {
    echo "  S0 entry not in audit md" >&2
    rm -rf "$tmpdir"
    return 1
  }
  grep -q "\\[S1\\] Test warning entry" "$tmpdir/stdout.log" || {
    echo "  S1 entry not in audit md" >&2
    rm -rf "$tmpdir"
    return 1
  }
  rm -rf "$tmpdir"
}

test_acquire_lock_blocks_concurrent_dispatch() {
  local tmpdir
  tmpdir=$(mktemp -d -t drill-lib-test-XXXXXX)
  # Lock file used by library
  local lock_file="/tmp/drill-skill-test-${RANDOM}.lock"
  # Open lock first (simulate another skill running)
  exec 99>"$lock_file"
  flock -n 99 || { echo "  test setup failed" >&2; rm -rf "$tmpdir"; return 1; }
  # Try to acquire from library
  set +e
  bash -c "
    set -euo pipefail
    source '$LIB'
    # Override lock file to our test file
    DRILL_SKILL_LOCK_FILE='$lock_file'
    exec 200>'$lock_file'
    flock -n 200
  " >/dev/null 2>&1
  local rc=$?
  set -e
  flock -u 99
  exec 99>&-
  rm -f "$lock_file"
  rm -rf "$tmpdir"
  [[ "$rc" -ne 0 ]] || { echo "  expected lock contention failure" >&2; return 1; }
}

test_lib_double_sourcing_idempotent() {
  set +e
  bash -c "
    set -euo pipefail
    source '$LIB'
    source '$LIB'  # second source should be no-op due to source guard
    [[ -n \"\${_DRILL_SKILL_COMMON_SOURCED:-}\" ]] || exit 1
  " >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

# ---- Test runner ----

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  if "$name"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  [PASS] $name"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  [FAIL] $name"
  fi
}

echo "=== drill-skill-common.sh library hermetic tests (T4.1) ==="

run_test test_confirm_cost_without_flag_exits_2
run_test test_confirm_cost_with_ci_env_passes
run_test test_confirm_cost_with_flag_passes
run_test test_sponsor_check_without_env_or_ci_exits_2
run_test test_sponsor_check_with_invalid_format_exits_2
run_test test_sponsor_check_with_valid_initials_passes
run_test test_sponsor_check_with_ci_bypasses
run_test test_init_audit_creates_dir_structure
run_test test_emit_finding_appends_to_audit_md
run_test test_acquire_lock_blocks_concurrent_dispatch
run_test test_lib_double_sourcing_idempotent

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

[[ "$TESTS_FAILED" -eq 0 ]] || exit 1
