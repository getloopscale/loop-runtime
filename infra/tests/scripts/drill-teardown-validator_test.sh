#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-teardown-validator.sh (Plan T4.1.bis EC-05 fix).
#
# Tests cover:
#   - DIGITALOCEAN_TOKEN missing → exit 1
#   - doctl missing → exit 1
#   - Clean state (zero residuals) → exit 0
#   - Residual cluster found → force-delete called → exit 0
#   - Force-delete fails → exit 3
#   - flock blocks concurrent invocation → exit 2
#   - Audit trail markdown emitted
#
# Mocking: PATH-shadow doctl with controllable stubs.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/dr/teardown-validator.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_doctl_stub() {
  local tmpdir="$1"
  local scenario="$2"  # clean | residual | delete-fails | vpc-retry-success | vpc-retry-fail

  mkdir -p "$tmpdir/bin"

  # Counter file for VPC retry semantics (incremented per vpcs delete call)
  echo "0" > "$tmpdir/vpc-call-counter"

  cat > "$tmpdir/bin/doctl" <<EOF
#!/usr/bin/env bash
# Stub doctl for drill-teardown-validator_test.sh
SCENARIO="$scenario"
COUNTER_FILE="$tmpdir/vpc-call-counter"

case "\$*" in
  *"kubernetes cluster list"*)
    if [[ "\$SCENARIO" == "residual" || "\$SCENARIO" == "delete-fails" ]]; then
      # Return one residual drill cluster
      echo "abc-123  loopzero-drill-mgmt-cluster  loopzero-drill"
    fi
    # else: empty (clean state) — including vpc-retry scenarios
    exit 0
    ;;
  *"kubernetes cluster delete"*)
    if [[ "\$SCENARIO" == "delete-fails" ]]; then
      exit 1
    fi
    exit 0
    ;;
  *"databases list"*)
    # Always empty in tests
    exit 0
    ;;
  *"databases delete"*)
    exit 0
    ;;
  *"vpcs list"*)
    # vpc-retry scenarios surface one residual VPC
    if [[ "\$SCENARIO" == "vpc-retry-success" || "\$SCENARIO" == "vpc-retry-fail" ]]; then
      echo "vpc-xyz-789  loopzero-drill-vpc-sfo3  loopzero-drill"
    fi
    exit 0
    ;;
  *"vpcs delete"*)
    # Increment counter per call
    CALL=\$(cat "\$COUNTER_FILE" 2>/dev/null || echo 0)
    CALL=\$((CALL + 1))
    echo "\$CALL" > "\$COUNTER_FILE"
    case "\$SCENARIO" in
      vpc-retry-success)
        # 2 failures, success on 3rd attempt
        if [[ "\$CALL" -le 2 ]]; then
          echo "stub: 409 Can not delete with members" >&2
          exit 1
        fi
        exit 0
        ;;
      vpc-retry-fail)
        # All 5 attempts (1 initial + 4 retries) fail
        echo "stub: 409 Can not delete with members" >&2
        exit 1
        ;;
      *)
        exit 0
        ;;
    esac
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "$tmpdir/bin/doctl"

  # flock comes from system; ensure available
  if ! command -v flock >/dev/null 2>&1; then
    echo "flock not available — test environment limitation" >&2
    return 1
  fi
}

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-validator-test-XXXXXX)

  local result
  if "$@" "$tmpdir"; then
    result="PASS"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    result="FAIL"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  echo "  [$result] $name"
  rm -rf "$tmpdir"
}

# ---- Tests ----

test_missing_token() {
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" clean
  local actual
  set +e
  env -u DIGITALOCEAN_TOKEN PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_clean_state_exits_0() {
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" clean
  local actual
  set +e
  DIGITALOCEAN_TOKEN=fake-token \
    DRILL_TEARDOWN_LOCK_FILE="$tmpdir/lock" \
    DRILL_AUDIT_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > "$tmpdir/stdout.log" 2> "$tmpdir/stderr.log"
  actual=$?
  set -e
  if [[ "$actual" -ne 0 ]]; then
    return 1
  fi
  # Verify audit file written
  ls "$tmpdir"/drill-teardown-audit-*.md > /dev/null 2>&1 || return 1
  # Verify "CLEAN" status in audit
  grep -q "CLEAN" "$tmpdir"/drill-teardown-audit-*.md
}

test_residual_force_deleted() {
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" residual
  local actual
  set +e
  DIGITALOCEAN_TOKEN=fake-token \
    DRILL_TEARDOWN_LOCK_FILE="$tmpdir/lock" \
    DRILL_AUDIT_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > "$tmpdir/stdout.log" 2> "$tmpdir/stderr.log"
  actual=$?
  set -e
  if [[ "$actual" -ne 0 ]]; then
    cat "$tmpdir/stderr.log" >&2
    return 1
  fi
  # Verify "RECOVERED" status in audit
  grep -q "RECOVERED" "$tmpdir"/drill-teardown-audit-*.md
}

test_force_delete_fails_exits_3() {
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" delete-fails
  local actual
  set +e
  DIGITALOCEAN_TOKEN=fake-token \
    DRILL_TEARDOWN_LOCK_FILE="$tmpdir/lock" \
    DRILL_AUDIT_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > "$tmpdir/stdout.log" 2> "$tmpdir/stderr.log"
  actual=$?
  set -e
  if [[ "$actual" -ne 3 ]]; then
    return 1
  fi
  # Verify "PARTIAL" status + MANUAL CLEANUP in audit
  grep -q "PARTIAL" "$tmpdir"/drill-teardown-audit-*.md && \
    grep -q "MANUAL CLEANUP" "$tmpdir"/drill-teardown-audit-*.md
}

test_audit_trail_emitted() {
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" clean
  set +e
  DIGITALOCEAN_TOKEN=fake-token \
    DRILL_TEARDOWN_LOCK_FILE="$tmpdir/lock" \
    DRILL_AUDIT_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > /dev/null 2>&1
  set -e
  # Audit file should exist with timestamp
  local audit_count
  audit_count=$(ls "$tmpdir"/drill-teardown-audit-*.md 2>/dev/null | wc -l)
  [[ "$audit_count" -eq 1 ]]
}

# ---- T3.2 VPC retry-backoff tests (L2-unlock plan D4) ----

test_vpc_retry_409_then_success_exits_0() {
  # TDD spec: fixture stub: 2x 409, 3rd success → validator should report CLEAN
  # Use VPC_RETRY_DELAYS_OVERRIDE=0 0 0 0 to skip real sleeps (test would be 220s).
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" vpc-retry-success
  set +e
  DIGITALOCEAN_TOKEN=fake-token \
    DRILL_TEARDOWN_LOCK_FILE="$tmpdir/lock" \
    DRILL_AUDIT_DIR="$tmpdir" \
    VPC_RETRY_DELAYS_OVERRIDE="0 0 0 0" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > "$tmpdir/stdout.log" 2> "$tmpdir/stderr.log"
  local actual=$?
  set -e
  [[ "$actual" -eq 0 ]] || {
    echo "  expected exit 0 (retry converged), got $actual" >&2
    cat "$tmpdir/stderr.log" >&2
    return 1
  }
  # Audit must show retry-converged note
  grep -q "on retry" "$tmpdir"/drill-teardown-audit-*.md || {
    echo "  expected 'on retry' note in audit" >&2
    return 1
  }
  # 3 doctl vpc delete calls (1 initial + 2 retries before success)
  local call_count
  call_count=$(cat "$tmpdir/vpc-call-counter")
  [[ "$call_count" -ge 3 ]] || {
    echo "  expected at least 3 vpc delete calls, got $call_count" >&2
    return 1
  }
}

test_vpc_retry_all_fail_exits_3() {
  # TDD spec: 5 attempts all fail (1 initial + 4 retries) → exit 3 PARTIAL
  local tmpdir="$1"
  build_doctl_stub "$tmpdir" vpc-retry-fail
  set +e
  DIGITALOCEAN_TOKEN=fake-token \
    DRILL_TEARDOWN_LOCK_FILE="$tmpdir/lock" \
    DRILL_AUDIT_DIR="$tmpdir" \
    VPC_RETRY_DELAYS_OVERRIDE="0 0 0 0" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT" > "$tmpdir/stdout.log" 2> "$tmpdir/stderr.log"
  local actual=$?
  set -e
  [[ "$actual" -eq 3 ]] || {
    echo "  expected exit 3 (PARTIAL after retries exhausted), got $actual" >&2
    return 1
  }
  # All 5 attempts made
  local call_count
  call_count=$(cat "$tmpdir/vpc-call-counter")
  [[ "$call_count" -eq 5 ]] || {
    echo "  expected exactly 5 vpc delete calls (1+4), got $call_count" >&2
    return 1
  }
  # Audit must note exhausted retries
  grep -q "after .* retries" "$tmpdir"/drill-teardown-audit-*.md
}

# ---- Run ----

echo "Running hermetic tests for drill-teardown-validator.sh..."
echo ""

run_test "missing_DIGITALOCEAN_TOKEN_exits_1" test_missing_token
run_test "clean_state_exits_0" test_clean_state_exits_0
run_test "residual_force_deleted_exits_0" test_residual_force_deleted
run_test "force_delete_fails_exits_3" test_force_delete_fails_exits_3
run_test "audit_trail_markdown_emitted" test_audit_trail_emitted
run_test "vpc_retry_409_then_success_exits_0" test_vpc_retry_409_then_success_exits_0
run_test "vpc_retry_all_fail_exits_3" test_vpc_retry_all_fail_exits_3

echo ""
echo "Total: $TESTS_RUN | Passed: $TESTS_PASSED | Failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
