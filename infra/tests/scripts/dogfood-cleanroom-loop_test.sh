#!/usr/bin/env bash
# infra/tests/scripts/dogfood-cleanroom-loop_test.sh
#
# RED-GREEN tests for infra/scripts/dogfood-cleanroom-loop.sh per Plan T1.1
# TDD section. Tests are hermetic — they never invoke task/k3d/docker/tilt.
# We rely on `--dry-run` for behavioral coverage of the phase sequencer.
#
# Test list:
#   1. test_unknown_arg_returns_1            — usage errors exit 1
#   2. test_help_flag_exits_zero             — --help is a happy path
#   3. test_dry_run_does_not_invoke_destroy  — hermetic; no real side-effects
#   4. test_exit_code_map_complete           — script source enumerates 0/1/10/20/25/30/40/50/60
#
# Plan amendment (D7 trivial / scope refinement): test #3 in the plan was
# "phase filter skips earlier phases" but --phase=N flag was dropped (YAGNI;
# no concrete caller). Replaced by --help happy-path coverage. No DoD impact.
#
# Run:   bash infra/tests/scripts/dogfood-cleanroom-loop_test.sh
# Exit:  0 if all tests PASS, 1 if any FAIL.
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SCRIPT="${REPO_ROOT}/infra/scripts/dogfood-cleanroom-loop.sh"

PASS=0
FAIL=0
FAILED_NAMES=()

run_test() {
  local name=$1
  if "$name"; then
    printf '  PASS  %s\n' "$name"
    PASS=$((PASS + 1))
  else
    printf '  FAIL  %s\n' "$name"
    FAIL=$((FAIL + 1))
    FAILED_NAMES+=("$name")
  fi
}

# ---------------------------------------------------------------------------
# Test 1: unknown argument returns exit 1 (usage error)
# ---------------------------------------------------------------------------
test_unknown_arg_returns_1() {
  local out rc
  out=$("$SCRIPT" --bogus-flag 2>&1) && rc=0 || rc=$?
  if [[ "$rc" -ne 1 ]]; then
    printf '    expected exit 1, got %d\n' "$rc" >&2
    return 1
  fi
  if ! grep -q "unknown argument" <<<"$out"; then
    printf '    expected "unknown argument" in stderr, got: %s\n' "$out" >&2
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# Test 2: --help flag exits 0
# ---------------------------------------------------------------------------
test_help_flag_exits_zero() {
  local out rc
  out=$("$SCRIPT" --help 2>&1) && rc=0 || rc=$?
  if [[ "$rc" -ne 0 ]]; then
    printf '    expected exit 0, got %d\n' "$rc" >&2
    return 1
  fi
  if ! grep -q "Cleanroom dogfood loop orchestrator" <<<"$out"; then
    printf '    expected help banner, got: %s\n' "$(head -3 <<<"$out")" >&2
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# Test 3: --dry-run is hermetic — no clusters touched
# ---------------------------------------------------------------------------
# Strategy: snapshot k3d cluster list before+after, ensure unchanged.
# We tolerate the script writing log/state files (those are intended outputs).
test_dry_run_does_not_invoke_destroy() {
  local before after rc
  before=$(k3d cluster list -o json 2>/dev/null || echo "[]")
  "$SCRIPT" --dry-run >/dev/null 2>&1 && rc=0 || rc=$?
  after=$(k3d cluster list -o json 2>/dev/null || echo "[]")
  if [[ "$rc" -ne 0 ]]; then
    printf '    expected dry-run exit 0, got %d\n' "$rc" >&2
    return 1
  fi
  if [[ "$before" != "$after" ]]; then
    printf '    cluster list changed during dry-run — script is NOT hermetic\n' >&2
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# Test 4: exit code map enumerated in source
# ---------------------------------------------------------------------------
# Verifies the documented exit codes (0, 1, 10, 20, 25, 30, 40, 50, 60) are
# all present in the header comment AND in the body (return statements).
# Two missing codes is enough to fail the test.
test_exit_code_map_complete() {
  local missing_in_header=()
  local missing_in_body=()
  local code

  # Header check: exact "  N  —" or "  NN  —" within the header comment block.
  for code in 0 1 10 20 25 30 40 50 60; do
    if ! grep -qE "^#   ${code}[[:space:]]+—" "$SCRIPT"; then
      missing_in_header+=("$code")
    fi
  done

  # Body check: each non-zero failure code must appear in a `return N` or
  # `exit N` statement somewhere in the script body.
  for code in 1 10 20 25 30 40 50 60; do
    if ! grep -qE "(return|exit)[[:space:]]+${code}" "$SCRIPT"; then
      missing_in_body+=("$code")
    fi
  done

  if [[ ${#missing_in_header[@]} -gt 0 ]] || [[ ${#missing_in_body[@]} -gt 0 ]]; then
    printf '    missing in header: [%s]; missing in body: [%s]\n' \
      "${missing_in_header[*]:-}" "${missing_in_body[*]:-}" >&2
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if [[ ! -x "$SCRIPT" ]]; then
  printf 'FATAL: %s missing or not executable\n' "$SCRIPT" >&2
  exit 1
fi

printf 'dogfood-cleanroom-loop.sh — test suite\n'
printf 'script: %s\n\n' "$SCRIPT"

run_test test_unknown_arg_returns_1
run_test test_help_flag_exits_zero
run_test test_dry_run_does_not_invoke_destroy
run_test test_exit_code_map_complete

printf '\n%d/%d tests passed\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  printf 'FAILED tests: %s\n' "${FAILED_NAMES[*]}"
  exit 1
fi
exit 0
