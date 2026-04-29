#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP / Phase 8 — runs all offline shell-script
# regression tests. Used by `task validate:helmfile-scripts`.
#
# Each individual test sets up mocks (mock-kubectl), runs the real script,
# and asserts on captured output.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

PASS=0
FAIL=0
declare -a FAILED

run_test() {
  local name="$1" script="$2"
  printf '\n=== %s ===\n' "$name"
  if bash "$script"; then
    PASS=$((PASS+1))
  else
    FAIL=$((FAIL+1))
    FAILED+=("$name")
  fi
}

run_test "generate-secrets"          "${SCRIPT_DIR}/test-generate-secrets.sh"
run_test "generate-runtime-secrets"  "${SCRIPT_DIR}/test-generate-runtime-secrets.sh"
run_test "create-wiring-configmap"   "${SCRIPT_DIR}/test-create-wiring-configmap.sh"
run_test "secrets idempotency"       "${SCRIPT_DIR}/test-idempotency.sh"

printf '\n========================================\n'
printf '  helmfile shell-script tests: %d PASS, %d FAIL\n' "$PASS" "$FAIL"
printf '========================================\n'
if [[ "$FAIL" -gt 0 ]]; then
  printf '  Failed: %s\n' "${FAILED[*]}"
  exit 1
fi
exit 0
