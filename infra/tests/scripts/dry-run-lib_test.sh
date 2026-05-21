#!/usr/bin/env bash
# Hermetic test for infra/scripts/lib/dry-run.sh.
#
# Plan 2026-05-21-deep-review-remediation T2.2.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LIB="$REPO_ROOT/infra/scripts/lib/dry-run.sh"

FAIL=0
fail() { FAIL=$((FAIL + 1)); echo "FAIL: $*" >&2; }
pass() { echo "PASS: $*"; }

test_lib_exists() {
  [ -f "$LIB" ] && pass "test_lib_exists" || fail "test_lib_exists"
}

test_source_idempotent() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  source "$LIB"
  source "$LIB"  # second source should be a no-op
  if [[ "${_LOOPZERO_DRY_RUN_LIB_LOADED:-}" == "1" ]] && [[ "${DRY_RUN:-}" == "0" ]]; then
    pass "test_source_idempotent"
  else
    fail "test_source_idempotent"
  fi
}

test_apply_executes_when_dry_run_false() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  source "$LIB"
  out=$(apply echo "hello")
  if [[ "$out" == "hello" ]]; then
    pass "test_apply_executes_when_dry_run_false"
  else
    fail "test_apply_executes got '$out' want 'hello'"
  fi
}

test_apply_skips_when_dry_run_true() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  export DRY_RUN=1; source "$LIB"
  out=$(apply echo "should-not-run" 2>/dev/null)
  if [[ -z "$out" ]]; then
    pass "test_apply_skips_when_dry_run_true"
  else
    fail "test_apply_should_not_have_executed; got '$out'"
  fi
}

test_parse_dry_run_flag_consumes_flag() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  source "$LIB"
  parse_dry_run_flag --dry-run arg1 arg2
  if [[ "$DRY_RUN" == "1" ]] && [[ "${REMAINING_ARGS[0]}" == "arg1" ]] && [[ "${REMAINING_ARGS[1]}" == "arg2" ]]; then
    pass "test_parse_dry_run_flag_consumes_flag"
  else
    fail "test_parse: DRY_RUN=$DRY_RUN remaining=${REMAINING_ARGS[*]}"
  fi
}

test_parse_short_flag() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  source "$LIB"
  parse_dry_run_flag arg1 -n arg2
  if [[ "$DRY_RUN" == "1" ]] && [[ "${REMAINING_ARGS[0]}" == "arg1" ]] && [[ "${REMAINING_ARGS[1]}" == "arg2" ]]; then
    pass "test_parse_short_flag"
  else
    fail "test_parse_short_flag: DRY_RUN=$DRY_RUN remaining=${REMAINING_ARGS[*]}"
  fi
}

test_parse_no_flag_default_false() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  source "$LIB"
  parse_dry_run_flag arg1 arg2
  if [[ "$DRY_RUN" == "0" ]] && [[ "${REMAINING_ARGS[0]}" == "arg1" ]]; then
    pass "test_parse_no_flag_default_false"
  else
    fail "test_parse_no_flag: DRY_RUN=$DRY_RUN"
  fi
}

test_apply_pipe_dry_run() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  export DRY_RUN=1; source "$LIB"
  # In dry-run, apply_pipe should consume stdin without executing
  out=$(echo "data" | apply_pipe cat 2>/dev/null)
  if [[ -z "$out" ]]; then
    pass "test_apply_pipe_dry_run"
  else
    fail "test_apply_pipe should not output anything in dry-run; got '$out'"
  fi
}

test_apply_pipe_real_run() {
  unset _LOOPZERO_DRY_RUN_LIB_LOADED DRY_RUN 2>/dev/null || true
  source "$LIB"
  out=$(echo "data" | apply_pipe cat)
  if [[ "$out" == "data" ]]; then
    pass "test_apply_pipe_real_run"
  else
    fail "test_apply_pipe got '$out' want 'data'"
  fi
}

test_lib_exists
test_source_idempotent
test_apply_executes_when_dry_run_false
test_apply_skips_when_dry_run_true
test_parse_dry_run_flag_consumes_flag
test_parse_short_flag
test_parse_no_flag_default_false
test_apply_pipe_dry_run
test_apply_pipe_real_run

if [ "$FAIL" -gt 0 ]; then
  echo "[dry-run-lib] FAIL: $FAIL test(s)" >&2
  exit 1
fi
echo "[dry-run-lib] PASS — all 9 tests"
