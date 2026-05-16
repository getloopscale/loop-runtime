#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-evidence-capture.sh (Plan T4.7.bis EC-03).
#
# TDD specs from plan T4.7.bis:
#   test_evidence_capture_with_kubectl_failure_persists_partial
#   test_evidence_capture_with_audit_doc_write_failure_persists_partial
#   test_evidence_capture_clean_path_full_evidence
#   test_evidence_capture_idempotent_re_run

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/drill-evidence-capture.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_stubs() {
  local tmpdir="$1"
  local pulumi_scenario="${2:-ok}"     # ok | fails
  local kubectl_scenario="${3:-ok}"     # ok | fails

  mkdir -p "$tmpdir/bin" "$tmpdir/pulumi-drill" "$tmpdir/drill-kc" "$tmpdir/audit-docs"

  # 4 kubeconfig files (per cluster)
  for c in mgmt build runtime runtime-2; do
    printf 'apiVersion: v1\nkind: Config\ncurrent-context: %s\n' "$c" > "$tmpdir/drill-kc/$c.yaml"
  done

  # Init local git repo (needed for commit step)
  (cd "$tmpdir" && git init -q && git config user.email "test@test" && git config user.name "test")

  echo "$pulumi_scenario" > "$tmpdir/pulumi-scen.txt"
  echo "$kubectl_scenario" > "$tmpdir/kubectl-scen.txt"

  cat > "$tmpdir/bin/pulumi" <<EOF
#!/usr/bin/env bash
SCEN=\$(cat "$tmpdir/pulumi-scen.txt")
if [[ "\$SCEN" == "fails" ]]; then
  echo "stub: pulumi failed" >&2
  exit 1
fi
# Emit a fake outputs JSON
echo '{"mgmtClusterEndpoint":"https://fake.k8s.example","meta":{"expiresInHours":8}}'
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"

  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
SCEN=\$(cat "$tmpdir/kubectl-scen.txt")
if [[ "\$SCEN" == "fails" ]]; then
  echo "stub: kubectl failed" >&2
  exit 1
fi
# Emit fake pods/metrics output
case "\$*" in
  *"get pods"*)
    echo "apiVersion: v1"
    echo "kind: List"
    echo "items: []"
    ;;
  *"exec"*)
    # Inside exec, emit fake metrics
    echo "loopzero_build_total 42"
    echo "loopzero_deploy_duration_seconds_bucket 7"
    ;;
esac
exit 0
EOF
  chmod +x "$tmpdir/bin/kubectl"
}

run_script() {
  local tmpdir="$1"
  shift
  local run_id="$1"
  shift
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    DRILL_KUBECONFIG_DIR="$tmpdir/drill-kc" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    EVIDENCE_DIR="$tmpdir/evidence-$run_id" \
    AUDIT_DOC_DIR="$tmpdir/audit-docs" \
    bash -c "cd '$tmpdir' && exec bash '$SCRIPT' '$run_id' $*" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
}

# ---- Tests ----

test_evidence_capture_clean_path_full_evidence() {
  # TDD spec: happy path — all fragments captured
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "ok"
  set +e
  run_script "$tmpdir" "drill-l2-test-001"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || {
    echo "  expected exit 0, got $rc" >&2
    cat "$tmpdir/stderr.log" >&2 | head -10
    return 1
  }
  # All evidence fragments exist
  [[ -f "$tmpdir/evidence-drill-l2-test-001/pulumi-outputs.json" ]] || return 1
  [[ -f "$tmpdir/evidence-drill-l2-test-001/mgmt-pods.yaml" ]] || return 1
  [[ -f "$tmpdir/evidence-drill-l2-test-001/build-pods.yaml" ]] || return 1
  [[ -f "$tmpdir/evidence-drill-l2-test-001/runtime-pods.yaml" ]] || return 1
  [[ -f "$tmpdir/evidence-drill-l2-test-001/runtime-2-pods.yaml" ]] || return 1
  [[ -f "$tmpdir/evidence-drill-l2-test-001/loopzero-metrics.txt" ]] || return 1
  # Audit doc exists + has "in-progress" status
  local audit_file
  audit_file=$(ls "$tmpdir/audit-docs"/*-drill-l-run-drill-l2-test-001.md 2>/dev/null | head -1)
  [[ -n "$audit_file" ]] || return 1
  grep -q "status: in-progress" "$audit_file"
}

test_evidence_capture_with_kubectl_failure_persists_partial() {
  # TDD spec: kubectl failure → partial evidence still committed
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "fails"
  set +e
  run_script "$tmpdir" "drill-l2-test-002"
  local rc=$?
  set -e
  # Should still exit 0 (partial > zero)
  [[ "$rc" -eq 0 ]] || {
    echo "  expected exit 0 (partial > zero), got $rc" >&2
    return 1
  }
  # Pulumi fragment exists (pulumi works)
  [[ -f "$tmpdir/evidence-drill-l2-test-002/pulumi-outputs.json" ]] || return 1
  # kubectl fragments exist but contain error indicator
  [[ -f "$tmpdir/evidence-drill-l2-test-002/mgmt-pods.yaml" ]] || return 1
  grep -q "stub: kubectl failed" "$tmpdir/evidence-drill-l2-test-002/mgmt-pods.yaml" || {
    echo "  expected kubectl error preserved in fragment" >&2
    return 1
  }
  # Audit doc shows ✗ for kubectl steps
  local audit_file
  audit_file=$(ls "$tmpdir/audit-docs"/*-drill-l-run-drill-l2-test-002.md 2>/dev/null | head -1)
  grep -q "✗ mgmt cluster pods" "$audit_file" || {
    echo "  expected ✗ marker for failed kubectl step" >&2
    return 1
  }
}

test_evidence_capture_idempotent_re_run() {
  # TDD spec: second invocation overwrites partial but preserves git
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "ok"
  set +e
  run_script "$tmpdir" "drill-l2-test-003"
  local rc1=$?
  # Second run with SAME run_id (overwrite fragments)
  run_script "$tmpdir" "drill-l2-test-003"
  local rc2=$?
  set -e
  [[ "$rc1" -eq 0 ]] || return 1
  # Second run might exit 0 (no new commit needed) OR 0 (committed delta)
  [[ "$rc2" -eq 0 ]] || {
    echo "  second invocation: expected exit 0, got $rc2" >&2
    return 1
  }
  # Evidence fragments still present
  [[ -f "$tmpdir/evidence-drill-l2-test-003/pulumi-outputs.json" ]] || return 1
  # Git log has at least one commit for this run
  (cd "$tmpdir" && git log --oneline 2>/dev/null | grep -q "drill-l2-test-003") || {
    echo "  expected git commit for drill-l2-test-003" >&2
    return 1
  }
}

test_missing_run_id_exits_1() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    bash -c "cd '$tmpdir' && exec bash '$SCRIPT'" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 1 ]] || return 1
}

test_unsafe_run_id_exits_4() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    bash -c "cd '$tmpdir' && exec bash '$SCRIPT' 'evil/../../path'" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]] || return 1
}

test_dry_run_no_commit() {
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "ok"
  set +e
  run_script "$tmpdir" "drill-l2-test-dry" --dry-run
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # Audit doc exists (skeleton always written)
  ls "$tmpdir/audit-docs"/*-drill-l-run-drill-l2-test-dry.md > /dev/null
  # No git commit happened (run --dry-run)
  (cd "$tmpdir" && git log --oneline 2>/dev/null | grep -q "drill-l2-test-dry") && {
    echo "  FAIL: dry-run created git commit" >&2
    return 1
  }
  return 0
}

test_unknown_flag_exits_4() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    bash -c "cd '$tmpdir' && exec bash '$SCRIPT' --unknown" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]] || return 1
}

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-evidence-capture-test-XXXXXX)
  if "$name" "$tmpdir"; then
    echo "  [PASS] $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  [FAIL] $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-evidence-capture hermetic tests (T4.7.bis EC-03) ==="

run_test test_evidence_capture_clean_path_full_evidence
run_test test_evidence_capture_with_kubectl_failure_persists_partial
run_test test_evidence_capture_idempotent_re_run
run_test test_missing_run_id_exits_1
run_test test_unsafe_run_id_exits_4
run_test test_dry_run_no_commit
run_test test_unknown_flag_exits_4

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
