#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-cancel-refresh.sh (Plan T3.1 D3).
#
# TDD specs from plan T3.1:
#   test_cancel_refresh_runs_in_sequence — both phases invoked in order
#   test_skips_if_no_pending_ops — idempotent on clean stack

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/drill-cancel-refresh.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_pulumi_stub() {
  local tmpdir="$1"
  local scenario="$2"  # has-stack-ok | no-stack | cancel-fails | refresh-fails

  mkdir -p "$tmpdir/bin" "$tmpdir/pulumi-drill"
  echo "$scenario" > "$tmpdir/scenario.txt"

  cat > "$tmpdir/bin/pulumi" <<'STUB'
#!/usr/bin/env bash
SCEN=$(cat "$PULUMI_STUB_SCENARIO_FILE" 2>/dev/null || echo unknown)

case "$*" in
  "stack ls --json")
    if [[ "$SCEN" == "no-stack" ]]; then
      echo '[]'
    else
      echo '[{"name":"drill","current":true}]'
    fi
    exit 0
    ;;
  "cancel --stack drill --yes")
    if [[ "$SCEN" == "cancel-fails" ]]; then
      echo "stub: cancel failed" >&2
      exit 1
    fi
    echo "cancel --stack drill --yes" >> "$PULUMI_STUB_INVOCATIONS"
    exit 0
    ;;
  "refresh --stack drill --yes --skip-preview")
    if [[ "$SCEN" == "refresh-fails" ]]; then
      echo "stub: refresh failed" >&2
      exit 1
    fi
    echo "refresh --stack drill --yes" >> "$PULUMI_STUB_INVOCATIONS"
    exit 0
    ;;
  *)
    echo "stub: unhandled: $*" >&2
    exit 0
    ;;
esac
STUB
  chmod +x "$tmpdir/bin/pulumi"
}

run_script() {
  local tmpdir="$1"
  shift
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    PULUMI_STUB_SCENARIO_FILE="$tmpdir/scenario.txt" \
    PULUMI_STUB_INVOCATIONS="$tmpdir/invocations.txt" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    DRILL_AUDIT_DIR="$tmpdir/audit" \
    bash "$SCRIPT" "$@" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
}

test_pulumi_missing_exits_1() {
  local tmpdir="$1"
  mkdir -p "$tmpdir/empty-bin" "$tmpdir/pulumi-drill"
  set +e
  PATH="$tmpdir/empty-bin:/usr/bin:/bin" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    DRILL_AUDIT_DIR="$tmpdir/audit" \
    bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 1 ]] || return 1
}

test_skips_if_no_pending_ops_no_stack() {
  # TDD spec: idempotent — no stack → exit 2 (nothing to do)
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "no-stack"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
  # Audit must say STACK_NOT_FOUND
  grep -q "STACK_NOT_FOUND" "$tmpdir/audit"/drill-cancel-refresh-*.log
}

test_cancel_refresh_runs_in_sequence() {
  # TDD spec: both phases invoked in order on healthy stack
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "has-stack-ok"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # invocations file must show cancel BEFORE refresh
  local sequence
  sequence=$(awk '{print NR": "$0}' "$tmpdir/invocations.txt")
  echo "$sequence" | grep -q "1: cancel" || {
    echo "  expected cancel as first invocation, got: $sequence" >&2
    return 1
  }
  echo "$sequence" | grep -q "2: refresh" || {
    echo "  expected refresh as second invocation, got: $sequence" >&2
    return 1
  }
}

test_cancel_fails_exits_3() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "cancel-fails"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || return 1
  # refresh should NOT have been called (early abort)
  if [[ -f "$tmpdir/invocations.txt" ]] && grep -q "refresh" "$tmpdir/invocations.txt"; then
    echo "  FAIL: refresh invoked despite cancel failure" >&2
    return 1
  fi
}

test_refresh_fails_exits_3() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "refresh-fails"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || return 1
  # cancel SHOULD have been called (came before refresh)
  grep -q "cancel" "$tmpdir/invocations.txt" || return 1
}

test_dry_run_no_invocations() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "has-stack-ok"
  set +e
  run_script "$tmpdir" --dry-run
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # No cancel/refresh invocations in dry-run
  if [[ -f "$tmpdir/invocations.txt" ]] && [[ -s "$tmpdir/invocations.txt" ]]; then
    echo "  FAIL: dry-run invoked pulumi: $(cat "$tmpdir/invocations.txt")" >&2
    return 1
  fi
}

test_unknown_flag_exits_4() {
  local tmpdir="$1"
  mkdir -p "$tmpdir/bin" "$tmpdir/pulumi-drill"
  cat > "$tmpdir/bin/pulumi" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    DRILL_AUDIT_DIR="$tmpdir/audit" \
    bash "$SCRIPT" --unknown-x 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]]
}

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-cancel-refresh-test-XXXXXX)
  if "$name" "$tmpdir"; then
    echo "  [PASS] $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  [FAIL] $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-cancel-refresh hermetic tests (T3.1 D3) ==="

run_test test_pulumi_missing_exits_1
run_test test_skips_if_no_pending_ops_no_stack
run_test test_cancel_refresh_runs_in_sequence
run_test test_cancel_fails_exits_3
run_test test_refresh_fails_exits_3
run_test test_dry_run_no_invocations
run_test test_unknown_flag_exits_4

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
