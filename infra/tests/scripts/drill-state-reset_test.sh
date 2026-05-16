#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-state-reset.sh (Plan T1.2.bis EC-01 fix).
#
# TDD specs from plan T1.2.bis:
#   test_dry_run_lists_no_destruction()      — default mode emits preview only
#   test_force_invokes_pulumi_stack_rm()      — --force triggers actual ops
#   test_force_idempotent_safe_to_re-run()    — second invocation safe (exit 0 or 2)
#
# Additional coverage:
#   test_pulumi_missing_exits_1
#   test_help_flag_shows_usage_exits_0
#   test_unknown_flag_exits_4
#   test_no_legacy_state_exits_0_clean
#   test_legacy_singular_detected_dry_run_default
#   test_audit_file_emitted
#
# Mocking: PATH-shadow `pulumi` with controllable stubs that simulate
# pulumi stack ls / output / rm / init responses.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/drill-state-reset.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Build a pulumi stub script that simulates `pulumi stack ls/output/rm/init`
# based on scenario fixtures placed at $TMPDIR/scenario.txt.
#
# Scenarios:
#   no-stack       — `pulumi stack ls` returns empty (no drill stack)
#   legacy-state   — outputs include v0 legacy keys (cluster, clusterKubeconfig)
#   v1-cluster-map — outputs include v1 `outputs` object key (cluster map)
#   clean-v2       — outputs are flat named exports (no legacy)
#   rm-fails       — pulumi stack rm exits 1
#   init-fails     — pulumi stack init exits 1
build_pulumi_stub() {
  local tmpdir="$1"
  local scenario="$2"

  mkdir -p "$tmpdir/bin" "$tmpdir/pulumi-drill"

  echo "$scenario" > "$tmpdir/scenario.txt"

  cat > "$tmpdir/bin/pulumi" <<'STUB'
#!/usr/bin/env bash
# Stub pulumi for drill-state-reset_test.sh
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
  "stack output --stack drill --json")
    case "$SCEN" in
      legacy-state)
        # v0 single-cluster legacy keys
        echo '{"cluster":"abc-123","clusterKubeconfig":"null","bucket":"old-bucket"}'
        ;;
      v1-cluster-map)
        # v1 object-literal wrapping
        echo '{"outputs":{"mgmtCluster":{"id":"x"}}}'
        ;;
      clean-v2|*)
        # v2 flat named exports — no legacy
        echo '{"mgmtClusterId":"x","mgmtClusterKubeconfig":"yes","pgHost":"h"}'
        ;;
    esac
    exit 0
    ;;
  "stack rm drill --yes")
    if [[ "$SCEN" == "rm-fails" ]]; then
      echo "stub: rm failed" >&2
      exit 1
    fi
    echo "stub: stack rm drill OK" >&2
    # Mark this invocation in shared file for assertions
    echo "stack rm drill --yes" >> "$PULUMI_STUB_INVOCATIONS"
    exit 0
    ;;
  "stack init drill --secrets-provider passphrase")
    if [[ "$SCEN" == "init-fails" ]]; then
      echo "stub: init failed" >&2
      exit 1
    fi
    echo "stub: stack init drill OK" >&2
    echo "stack init drill" >> "$PULUMI_STUB_INVOCATIONS"
    exit 0
    ;;
  *)
    echo "stub: unhandled args: $*" >&2
    exit 0
    ;;
esac
STUB
  chmod +x "$tmpdir/bin/pulumi"
}

# Run script with PATH shadow + isolated audit dir + tmp Pulumi dir
run_script() {
  local tmpdir="$1"
  shift

  PATH="$tmpdir/bin:$PATH" \
    PULUMI_STUB_SCENARIO_FILE="$tmpdir/scenario.txt" \
    PULUMI_STUB_INVOCATIONS="$tmpdir/invocations.txt" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    DRILL_AUDIT_DIR="$tmpdir/audit" \
    bash "$SCRIPT" "$@" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
}

assert_exit() {
  local expected="$1"
  local actual="$2"
  local name="$3"
  if [[ "$actual" -ne "$expected" ]]; then
    echo "  ASSERT FAIL: $name — expected exit $expected, got $actual" >&2
    cat "$4/stderr.log" >&2 2>/dev/null || true
    return 1
  fi
  return 0
}

assert_file_contains() {
  local file="$1"
  local pattern="$2"
  local name="$3"
  if ! grep -qE "$pattern" "$file" 2>/dev/null; then
    echo "  ASSERT FAIL: $name — file $file missing pattern: $pattern" >&2
    return 1
  fi
  return 0
}

# ---- Tests ----

test_pulumi_missing_exits_1() {
  local tmpdir="$1"
  mkdir -p "$tmpdir/empty-bin" "$tmpdir/pulumi-drill"
  # PATH lacks pulumi but retains /usr/bin:/bin (bash, grep, etc.)
  set +e
  PATH="$tmpdir/empty-bin:/usr/bin:/bin" \
      PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
      DRILL_AUDIT_DIR="$tmpdir/audit" \
      bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 1 ]] || return 1
  # Also assert error message mentions pulumi CLI
  grep -q "pulumi CLI not installed" "$tmpdir/stderr.log" || return 1
}

test_help_flag_shows_usage_exits_0() {
  local tmpdir="$1"
  mkdir -p "$tmpdir/bin"
  cat > "$tmpdir/bin/pulumi" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"
  mkdir -p "$tmpdir/pulumi-drill"
  if ! PATH="$tmpdir/bin:$PATH" PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
      bash "$SCRIPT" --help >"$tmpdir/stdout.log" 2>&1; then
    return 1
  fi
  grep -q "drill-state-reset.sh" "$tmpdir/stdout.log" || return 1
}

test_unknown_flag_exits_4() {
  local tmpdir="$1"
  mkdir -p "$tmpdir/bin"
  cat > "$tmpdir/bin/pulumi" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"
  mkdir -p "$tmpdir/pulumi-drill"
  set +e
  PATH="$tmpdir/bin:$PATH" PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
      bash "$SCRIPT" --unknown-flag-xyz 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]]
}

test_no_stack_exits_2_idempotent() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "no-stack"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
  # Audit file must be emitted even on idempotent no-op
  local audit_file
  audit_file=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | head -1)
  [[ -n "$audit_file" ]] || return 1
  assert_file_contains "$audit_file" "STACK_NOT_FOUND" "no-stack audit"
}

test_clean_v2_state_exits_0() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "clean-v2"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  local audit_file
  audit_file=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | head -1)
  assert_file_contains "$audit_file" "ALREADY_CLEAN" "clean-v2 audit"
}

test_dry_run_lists_no_destruction() {
  # TDD spec: default (no --force) emits preview, NEVER invokes pulumi stack rm
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "legacy-state"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # Critical: invocations file must NOT contain "stack rm"
  if [[ -f "$tmpdir/invocations.txt" ]] && grep -q "stack rm" "$tmpdir/invocations.txt"; then
    echo "  FAIL: dry-run invoked pulumi stack rm" >&2
    return 1
  fi
  # Audit must say DRY_RUN
  local audit_file
  audit_file=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | head -1)
  assert_file_contains "$audit_file" "DRY_RUN" "dry-run audit"
}

test_legacy_v1_object_literal_detected() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "v1-cluster-map"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  local audit_file
  audit_file=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | head -1)
  assert_file_contains "$audit_file" "outputs.v1-object-literal" "v1 detection"
}

test_force_invokes_pulumi_stack_rm() {
  # TDD spec: --force triggers actual pulumi stack rm + init
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "legacy-state"
  set +e
  run_script "$tmpdir" --force
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # Critical: invocations file MUST contain "stack rm" + "stack init"
  grep -q "stack rm drill" "$tmpdir/invocations.txt" || return 1
  grep -q "stack init drill" "$tmpdir/invocations.txt" || return 1
  local audit_file
  audit_file=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | head -1)
  assert_file_contains "$audit_file" "RESET_COMPLETED" "force audit"
}

test_force_skip_init_no_re_init() {
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "legacy-state"
  set +e
  run_script "$tmpdir" --force --skip-init
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  grep -q "stack rm drill" "$tmpdir/invocations.txt" || return 1
  # Must NOT invoke stack init
  if grep -q "stack init drill" "$tmpdir/invocations.txt"; then
    echo "  FAIL: --skip-init did not skip init" >&2
    return 1
  fi
  local audit_file
  audit_file=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | head -1)
  assert_file_contains "$audit_file" "RM_COMPLETED_NO_REINIT" "skip-init audit"
}

test_force_idempotent_safe_to_re_run() {
  # TDD spec: second --force invocation finds no stack (because first removed it)
  # → exits 2 (idempotent no-op path)
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "legacy-state"
  set +e
  run_script "$tmpdir" --force  # first run
  local rc1=$?
  # Simulate second invocation: stack now gone (no-stack scenario)
  echo "no-stack" > "$tmpdir/scenario.txt"
  > "$tmpdir/invocations.txt"
  run_script "$tmpdir" --force  # second run
  local rc2=$?
  set -e
  [[ "$rc1" -eq 0 ]] || return 1
  [[ "$rc2" -eq 2 ]] || return 1
  # Second run must NOT have re-invoked rm (idempotent)
  if [[ -s "$tmpdir/invocations.txt" ]]; then
    echo "  FAIL: idempotent re-run still invoked pulumi: $(cat "$tmpdir/invocations.txt")" >&2
    return 1
  fi
}

test_audit_file_emitted_on_every_invocation() {
  # Each invocation MUST emit an audit file (operator forensic trail).
  local tmpdir="$1"
  build_pulumi_stub "$tmpdir" "legacy-state"
  set +e
  run_script "$tmpdir"
  set -e
  local count
  count=$(ls "$tmpdir/audit"/drill-state-reset-audit-*.md 2>/dev/null | wc -l)
  [[ "$count" -ge 1 ]]
}

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-state-reset-test-XXXXXX)

  if "$name" "$tmpdir"; then
    echo "  [PASS] $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  [FAIL] $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-state-reset hermetic tests (T1.2.bis EC-01) ==="

run_test test_pulumi_missing_exits_1
run_test test_help_flag_shows_usage_exits_0
run_test test_unknown_flag_exits_4
run_test test_no_stack_exits_2_idempotent
run_test test_clean_v2_state_exits_0
run_test test_dry_run_lists_no_destruction
run_test test_legacy_v1_object_literal_detected
run_test test_force_invokes_pulumi_stack_rm
run_test test_force_skip_init_no_re_init
run_test test_force_idempotent_safe_to_re_run
run_test test_audit_file_emitted_on_every_invocation

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
