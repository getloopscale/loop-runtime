#!/usr/bin/env bash
# infra/tests/scripts/dogfood-cleanroom-loop_test.sh
#
# Hermetic tests for infra/scripts/dogfood-cleanroom-loop.sh.
# After the iter-22 FAANG-grade refactor the orchestrator is a pure
# sequencer — no inline kubectl patches or YAML applies. Tests focus on:
#   1. arg parsing exit codes
#   2. --help happy path
#   3. --dry-run hermeticity (clusters untouched)
#   4. exit-code map completeness (header + body match)
#   5. log file persists outside the tree wiped by `task clean`
#   6. phase ordering: setup → heal → tilt_up
#   7. dogfood phase calls full-loop.sh twice (reset, then run)
#   8. NO inline workaround functions (no phase_rbac_patch / phase_bootstrap_extras
#      / phase_post_tilt_env — those were canonicalised to YAML / Taskfile)
#
# Run:   bash infra/tests/scripts/dogfood-cleanroom-loop_test.sh
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

test_unknown_arg_returns_1() {
  local rc
  "$SCRIPT" --bogus-flag >/dev/null 2>&1 && rc=0 || rc=$?
  [[ "$rc" -eq 1 ]]
}

test_help_flag_exits_zero() {
  local out rc
  out=$("$SCRIPT" --help 2>&1) && rc=0 || rc=$?
  [[ "$rc" -eq 0 ]] && grep -q "Cleanroom dogfood loop orchestrator" <<<"$out"
}

test_dry_run_does_not_invoke_destroy() {
  local before after
  before=$(k3d cluster list -o json 2>/dev/null || echo "[]")
  "$SCRIPT" --dry-run >/dev/null 2>&1
  after=$(k3d cluster list -o json 2>/dev/null || echo "[]")
  [[ "$before" == "$after" ]]
}

test_exit_code_map_complete() {
  local missing_in_header=() missing_in_body=()
  for code in 0 1 10 20 25 30 40 50 60; do
    grep -qE "^#   ${code}[[:space:]]+—" "$SCRIPT" || missing_in_header+=("$code")
  done
  for code in 1 10 20 25 30 40 50 60; do
    grep -qE "(return|exit)[[:space:]]+${code}" "$SCRIPT" || missing_in_body+=("$code")
  done
  if [[ ${#missing_in_header[@]} -gt 0 || ${#missing_in_body[@]} -gt 0 ]]; then
    printf '    missing in header: [%s]; missing in body: [%s]\n' \
      "${missing_in_header[*]:-}" "${missing_in_body[*]:-}" >&2
    return 1
  fi
  return 0
}

test_log_file_outside_report_dir() {
  grep -qE '^LOG_DIR=' "$SCRIPT" || { echo "    no LOG_DIR=" >&2; return 1; }
  grep -E '^LOG_DIR=' "$SCRIPT" | grep -q '.test-reports-cleanroom' || {
    echo "    LOG_DIR must include .test-reports-cleanroom" >&2; return 1
  }
  grep -E '^LOG_FILE=' "$SCRIPT" | grep -q '${LOG_DIR}' || {
    echo "    LOG_FILE must use \${LOG_DIR}" >&2; return 1
  }
  return 0
}

test_phase_ordering_setup_heal_tilt() {
  local seq
  seq=$(grep -E '^phase_(destroy|setup|heal|tilt_up|wait_doctor|dogfood)' "$SCRIPT" | tail -6 | tr '\n' '|')
  case "$seq" in
    'phase_destroy'*'phase_setup'*'phase_heal'*'phase_tilt_up'*'phase_wait_doctor'*'phase_dogfood'*)
      ;;
    *)
      printf '    ordering wrong; expected destroy → setup → heal → tilt_up → wait_doctor → dogfood; got:\n%s\n' "$seq" >&2
      return 1
      ;;
  esac
}

test_dogfood_phase_calls_full_loop_twice() {
  local body reset_count run_count
  body=$(awk '/^phase_dogfood\(\)/,/^}/' "$SCRIPT")
  [[ -n "$body" ]] || { echo "    phase_dogfood missing" >&2; return 1; }
  reset_count=$(grep -cE 'bash.*--reset' <<<"$body" || true)
  run_count=$(grep -E 'bash[[:space:]]+"\$DOGFOOD_FULL_LOOP"' <<<"$body" \
    | grep -cv -- '--reset' || true)
  [[ "$reset_count" -ge 1 ]] || { echo "    no --reset call" >&2; return 1; }
  [[ "$run_count" -ge 1 ]] || { echo "    no no-flag run call" >&2; return 1; }
  return 0
}

test_no_inline_workaround_functions() {
  # FAANG refactor: workaround phases removed. fixes are now in:
  #   - infra/tilt/api-deployment.yaml (B3 RBAC, B14 env, B22 images, B23 strategy)
  #   - Taskfile.yml `task bootstrap:standalone` (B9-B16 + B20 + B21 + B24)
  # Orchestrator MUST NOT contain phase_rbac_patch, phase_bootstrap_extras,
  # or phase_post_tilt_env — those names indicate workaround drift.
  local found=()
  for forbidden in phase_rbac_patch phase_bootstrap_extras phase_post_tilt_env; do
    grep -qE "^${forbidden}\(\)" "$SCRIPT" && found+=("$forbidden")
  done
  if [[ ${#found[@]} -gt 0 ]]; then
    printf '    workaround function(s) reintroduced: %s\n' "${found[*]}" >&2
    printf '    move the fix to its canonical layer (Tilt YAML / Taskfile / heal)\n' >&2
    return 1
  fi
  return 0
}

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
run_test test_log_file_outside_report_dir
run_test test_phase_ordering_setup_heal_tilt
run_test test_dogfood_phase_calls_full_loop_twice
run_test test_no_inline_workaround_functions

printf '\n%d/%d tests passed\n' "$PASS" "$((PASS + FAIL))"

if [[ "$FAIL" -gt 0 ]]; then
  printf 'FAILED tests: %s\n' "${FAILED_NAMES[*]}"
  exit 1
fi
exit 0
