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
  # B3 (iter 3): adds exit code 35 for phase_rbac_patch failure.
  for code in 0 1 10 20 25 30 35 38 40 45 50 60; do
    if ! grep -qE "^#   ${code}[[:space:]]+—" "$SCRIPT"; then
      missing_in_header+=("$code")
    fi
  done

  # Body check: each non-zero failure code must appear in a `return N` or
  # `exit N` statement somewhere in the script body.
  for code in 1 10 20 25 30 35 38 40 45 50 60; do
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

# ---------------------------------------------------------------------------
# Test 5 (regression for B1, iter 1, 2026-05-05): LOG_FILE must NOT live
# under REPORT_DIR (.test-reports/), because Phase 1 destroy invokes
# `task clean:all` -> `task clean` -> `rm -rf .test-reports/`, which would
# orphan the tee FD and lose all output.
# ---------------------------------------------------------------------------
test_log_file_outside_report_dir() {
  # Extract LOG_DIR and REPORT_DIR assignments from the script source.
  local log_dir_decl report_dir_decl
  log_dir_decl=$(grep -E '^LOG_DIR=' "$SCRIPT" | head -1)
  report_dir_decl=$(grep -E '^REPORT_DIR=' "$SCRIPT" | head -1)

  if [[ -z "$log_dir_decl" ]] || [[ -z "$report_dir_decl" ]]; then
    printf '    expected both LOG_DIR= and REPORT_DIR= declarations in source\n' >&2
    return 1
  fi

  # Static check: LOG_DIR and REPORT_DIR must resolve to different paths.
  # (LOG_DIR=.test-reports-cleanroom; REPORT_DIR=.test-reports — distinct dirs)
  if [[ "$log_dir_decl" == "${log_dir_decl/.test-reports-cleanroom/}" ]]; then
    printf '    LOG_DIR must include .test-reports-cleanroom (sibling of .test-reports, safe from `task clean`)\n' >&2
    printf '    got: %s\n' "$log_dir_decl" >&2
    return 1
  fi

  # Verify LOG_FILE references LOG_DIR, not REPORT_DIR.
  local log_file_decl
  log_file_decl=$(grep -E '^LOG_FILE=' "$SCRIPT" | head -1)
  if ! grep -q '${LOG_DIR}' <<<"$log_file_decl"; then
    printf '    LOG_FILE must use ${LOG_DIR}, got: %s\n' "$log_file_decl" >&2
    return 1
  fi

  return 0
}

# ---------------------------------------------------------------------------
# Test 6 (regression for B3, iter 3, 2026-05-05): phase_rbac_patch must
# exist AND be invoked between phase_setup and phase_tilt_up. Without the
# patch, Tilt's narrow ClusterRole leaves loopzero-api SA without `secrets`
# and `clustercredentials.loopzero.io` perms — pod CrashLoopBackOff and tilt ci
# hits 600s timeout. The patch is additive (different ClusterRole name) so
# it does not conflict with Tilt's apply.
# ---------------------------------------------------------------------------
test_rbac_patch_phase_exists_and_ordered() {
  if ! grep -qE '^phase_rbac_patch\(\)' "$SCRIPT"; then
    printf '    expected `phase_rbac_patch()` function in script\n' >&2
    return 1
  fi
  if ! grep -q 'loopzero-api-cleanroom-extra' "$SCRIPT"; then
    printf '    phase_rbac_patch must reference ClusterRole `loopzero-api-cleanroom-extra`\n' >&2
    return 1
  fi
  if ! grep -q 'clustercredentials' "$SCRIPT"; then
    printf '    phase_rbac_patch must grant `clustercredentials` perms\n' >&2
    return 1
  fi
  # Ordering: phase_setup → phase_rbac_patch → phase_heal → phase_tilt_up.
  # B4 (iter 4→5): heal MUST run before tilt so ClusterCredentials exist
  # when loopzero-api boots. Look at the final main-sequence block specifically
  # (tail), not docstrings.
  local seq
  seq=$(grep -E '^phase_(setup|rbac_patch|heal|tilt_up)\b' "$SCRIPT" | tail -4 | tr '\n' '|')
  case "$seq" in
    'phase_setup'*'phase_rbac_patch'*'phase_heal'*'phase_tilt_up'*)
      ;;
    *)
      printf '    phase ordering wrong; expected setup → rbac_patch → heal → tilt_up; got:\n%s\n' "$seq" >&2
      return 1
      ;;
  esac
  return 0
}

# ---------------------------------------------------------------------------
# Test 7 (regression for B5, iter 5, 2026-05-05): phase_dogfood must call
# full-loop.sh TWICE — once with --reset (clears state), once without
# (actually runs journeys). full-loop.sh's --reset path exits immediately
# after re-initialising state.
# ---------------------------------------------------------------------------
test_dogfood_phase_calls_full_loop_twice() {
  # Extract phase_dogfood body and check it has both invocations.
  local body
  body=$(awk '/^phase_dogfood\(\)/,/^}/' "$SCRIPT")
  if [[ -z "$body" ]]; then
    printf '    expected `phase_dogfood()` function in script\n' >&2
    return 1
  fi
  local reset_count run_count
  reset_count=$(grep -cE 'bash.*--reset' <<<"$body" || true)
  # Match `bash "$DOGFOOD_FULL_LOOP"` NOT followed by --reset on same line.
  # This catches the no-flag invocation regardless of trailing `; then`.
  run_count=$(grep -E 'bash[[:space:]]+"\$DOGFOOD_FULL_LOOP"' <<<"$body" \
    | grep -cv -- '--reset' || true)
  if [[ "$reset_count" -lt 1 ]]; then
    printf '    phase_dogfood must call `bash $DOGFOOD_FULL_LOOP --reset`\n' >&2
    return 1
  fi
  if [[ "$run_count" -lt 1 ]]; then
    printf '    phase_dogfood must also call `bash $DOGFOOD_FULL_LOOP` (no flags) to actually run journeys\n' >&2
    return 1
  fi
  return 0
}

run_test test_unknown_arg_returns_1
run_test test_help_flag_exits_zero
run_test test_dry_run_does_not_invoke_destroy
run_test test_exit_code_map_complete
run_test test_log_file_outside_report_dir
run_test test_rbac_patch_phase_exists_and_ordered
run_test test_dogfood_phase_calls_full_loop_twice

printf '\n%d/%d tests passed\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  printf 'FAILED tests: %s\n' "${FAILED_NAMES[*]}"
  exit 1
fi
exit 0
