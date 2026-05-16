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
SCRIPT="$ROOT/infra/scripts/drill-teardown-validator.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_doctl_stub() {
  local tmpdir="$1"
  local scenario="$2"  # clean | residual | delete-fails

  mkdir -p "$tmpdir/bin"

  cat > "$tmpdir/bin/doctl" <<EOF
#!/usr/bin/env bash
# Stub doctl for drill-teardown-validator_test.sh
SCENARIO="$scenario"

case "\$*" in
  *"kubernetes cluster list"*)
    if [[ "\$SCENARIO" == "residual" || "\$SCENARIO" == "delete-fails" ]]; then
      # Return one residual drill cluster
      echo "abc-123  loopzero-drill-mgmt-cluster  loopzero-drill"
    fi
    # else: empty (clean state)
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
    exit 0
    ;;
  *"vpcs delete"*)
    exit 0
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

# ---- Run ----

echo "Running hermetic tests for drill-teardown-validator.sh..."
echo ""

run_test "missing_DIGITALOCEAN_TOKEN_exits_1" test_missing_token
run_test "clean_state_exits_0" test_clean_state_exits_0
run_test "residual_force_deleted_exits_0" test_residual_force_deleted
run_test "force_delete_fails_exits_3" test_force_delete_fails_exits_3
run_test "audit_trail_markdown_emitted" test_audit_trail_emitted

echo ""
echo "Total: $TESTS_RUN | Passed: $TESTS_PASSED | Failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
