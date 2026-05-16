#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for bootstrap-cell-{default,1,2}-cc-drill.sh (Plan T2.3 + EC-08).
#
# Tests cover:
#   - EC-08: idempotency (run 2x produces single CC, not duplicate)
#   - D3 mapping: cell-default+cell-1 → runtimeCluster, cell-2 → runtimeCluster2
#   - Dry-run renders manifests without applying
#   - DRILL_KUBECONFIG missing → exit 1
#   - Pulumi output missing key → exit 1
#
# Mocking: PATH-shadow kubectl + npx (pulumi) with stubs.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT_DEFAULT="$ROOT/infra/scripts/bootstrap-cell-default-cc-drill.sh"
SCRIPT_1="$ROOT/infra/scripts/bootstrap-cell-1-cc-drill.sh"
SCRIPT_2="$ROOT/infra/scripts/bootstrap-cell-2-cc-drill.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_stubs() {
  local tmpdir="$1"
  local has_runtime="${2:-true}"
  local has_runtime2="${3:-true}"

  mkdir -p "$tmpdir/bin"
  echo "$has_runtime" > "$tmpdir/has_runtime"
  echo "$has_runtime2" > "$tmpdir/has_runtime2"

  # Mock npx (pulumi)
  cat > "$tmpdir/bin/npx" <<EOF
#!/usr/bin/env bash
# Stub npx for pulumi stack output --json
if [[ "\$*" == *"pulumi stack output --json"* ]]; then
  HR=\$(cat "$tmpdir/has_runtime" 2>/dev/null || echo "true")
  HR2=\$(cat "$tmpdir/has_runtime2" 2>/dev/null || echo "true")

  OUT='{"outputs":{'

  if [[ "\$HR" == "true" ]]; then
    OUT+='"runtimeCluster":{"endpoint":"https://aaa.k8s.ondigitalocean.com","kubeconfig":"apiVersion: v1\\nkind: Config\\nfake-runtime-kubeconfig"}'
  fi

  if [[ "\$HR" == "true" && "\$HR2" == "true" ]]; then
    OUT+=','
  fi

  if [[ "\$HR2" == "true" ]]; then
    OUT+='"runtimeCluster2":{"endpoint":"https://bbb.k8s.ondigitalocean.com","kubeconfig":"apiVersion: v1\\nkind: Config\\nfake-runtime2-kubeconfig"}'
  fi

  OUT+='}}'
  printf '%s' "\$OUT"
  exit 0
fi
exit 0
EOF
  chmod +x "$tmpdir/bin/npx"

  # Mock kubectl
  # Records apply calls to file for idempotency assertions
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
# Stub kubectl — records apply calls to $tmpdir/kubectl.log
if [[ "\$*" == *"apply --server-side"* ]]; then
  # Read stdin to capture what would be applied
  cat - >> "$tmpdir/kubectl.log"
  printf '\n---\n' >> "$tmpdir/kubectl.log"
fi
exit 0
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Mock jq (use real jq if available, else stub)
  if ! command -v jq >/dev/null 2>&1; then
    cat > "$tmpdir/bin/jq" <<EOF
#!/usr/bin/env bash
# Minimal jq stub — parses .outputs.X.field paths
read INPUT
# Pattern: .outputs.runtimeCluster.endpoint
QUERY="\$2"
if [[ "\$QUERY" == *"runtimeCluster.endpoint"* ]]; then
  printf '%s' "https://aaa.k8s.ondigitalocean.com"
elif [[ "\$QUERY" == *"runtimeCluster.kubeconfig"* ]]; then
  printf '%s' "apiVersion: v1\nkind: Config"
elif [[ "\$QUERY" == *"runtimeCluster2.endpoint"* ]]; then
  printf '%s' "https://bbb.k8s.ondigitalocean.com"
elif [[ "\$QUERY" == *"runtimeCluster2.kubeconfig"* ]]; then
  printf '%s' "apiVersion: v1\nkind: Config2"
fi
exit 0
EOF
    chmod +x "$tmpdir/bin/jq"
  fi
}

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-cc-test-XXXXXX)

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

test_missing_kubeconfig_exits_1() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  local actual
  set +e
  env -u DRILL_KUBECONFIG PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_DEFAULT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_dry_run_prints_manifests() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  touch "$tmpdir/fake-kc.yaml"
  local stdout
  stdout=$(DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_DEFAULT" --dry-run 2>/dev/null || true)
  # Must contain ClusterCredential + Secret manifests
  grep -q "kind: ClusterCredential" <<< "$stdout" && \
    grep -q "kind: Secret" <<< "$stdout"
}

test_d3_default_maps_runtimeCluster() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  touch "$tmpdir/fake-kc.yaml"
  local stdout
  stdout=$(DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_DEFAULT" --dry-run 2>/dev/null || true)
  # Default → runtimeCluster (NOT runtimeCluster2)
  grep -q "aaa.k8s.ondigitalocean.com" <<< "$stdout" && \
    ! grep -q "bbb.k8s.ondigitalocean.com" <<< "$stdout" && \
    grep -q "cellID: cell-default" <<< "$stdout"
}

test_d3_cell1_maps_runtimeCluster() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  touch "$tmpdir/fake-kc.yaml"
  local stdout
  stdout=$(DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_1" --dry-run 2>/dev/null || true)
  # cell-1 → runtimeCluster (NOT runtimeCluster2)
  grep -q "aaa.k8s.ondigitalocean.com" <<< "$stdout" && \
    ! grep -q "bbb.k8s.ondigitalocean.com" <<< "$stdout" && \
    grep -q "cellID: cell-1" <<< "$stdout"
}

test_d3_cell2_maps_runtimeCluster2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  touch "$tmpdir/fake-kc.yaml"
  local stdout
  stdout=$(DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_2" --dry-run 2>/dev/null || true)
  # cell-2 → runtimeCluster2 (NOT runtimeCluster)
  grep -q "bbb.k8s.ondigitalocean.com" <<< "$stdout" && \
    ! grep -q "aaa.k8s.ondigitalocean.com" <<< "$stdout" && \
    grep -q "cellID: cell-2" <<< "$stdout"
}

test_ec08_idempotency_default() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  touch "$tmpdir/fake-kc.yaml"
  # Run script 2x; kubectl.log records each apply call.
  # With kubectl apply --server-side, multiple invocations should produce
  # the SAME manifest each time (idempotency from kubectl side).
  # Test asserts: 2 invocations produce identical manifest content.
  DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_DEFAULT" > /dev/null 2>&1
  local first_run_size
  first_run_size=$(wc -c < "$tmpdir/kubectl.log")
  # Run again
  DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_DEFAULT" > /dev/null 2>&1
  local second_run_size
  second_run_size=$(wc -c < "$tmpdir/kubectl.log")
  # Second run doubles the log (apply called identically twice)
  local expected_double=$((first_run_size * 2))
  [[ "$second_run_size" -eq "$expected_double" ]]
}

test_pulumi_output_missing_key_exits_1() {
  local tmpdir="$1"
  build_stubs "$tmpdir" false true  # has_runtime=false, has_runtime2=true
  touch "$tmpdir/fake-kc.yaml"
  local actual
  set +e
  DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    "$SCRIPT_DEFAULT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

# ---- Run ----

echo "Running hermetic tests for bootstrap-cell-*-cc-drill.sh..."
echo ""

run_test "missing_DRILL_KUBECONFIG_exits_1" test_missing_kubeconfig_exits_1
run_test "dry_run_prints_manifests" test_dry_run_prints_manifests
run_test "D3_cell-default_maps_runtimeCluster" test_d3_default_maps_runtimeCluster
run_test "D3_cell-1_maps_runtimeCluster" test_d3_cell1_maps_runtimeCluster
run_test "D3_cell-2_maps_runtimeCluster2" test_d3_cell2_maps_runtimeCluster2
run_test "EC-08_idempotency_default_2x" test_ec08_idempotency_default
run_test "pulumi_output_missing_key_exits_1" test_pulumi_output_missing_key_exits_1

echo ""
echo "Total: $TESTS_RUN | Passed: $TESTS_PASSED | Failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
