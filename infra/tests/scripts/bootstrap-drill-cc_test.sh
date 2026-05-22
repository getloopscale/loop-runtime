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
# The 3 per-cell bootstrap-cell-*-cc-drill.sh scripts were consolidated into one
# parametrized function in commit 1a9f1f77 (Phase 4 T4.2). The test now sources
# the lib and calls the function. Per-cell (CELL_ID, PULUMI_KEY) mapping (D3):
#   cell-default → runtimeCluster · cell-1 → runtimeCluster · cell-2 → runtimeCluster2
LIB="$ROOT/infra/scripts/lib/bootstrap-cell-cc-drill-common.sh"

# run_cc CELL_ID PULUMI_KEY [--dry-run] — runs the consolidated function in a
# subshell so env/PATH overrides apply and the return code propagates as $?.
run_cc() { ( source "$LIB"; bootstrap_cell_cc_drill "$@" ); }

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

  # Mock npx (pulumi) — v2 FLAT NAMED EXPORTS schema (post L2-unlock T1.2 D1)
  # New invocation pattern: pulumi stack output --stack drill NAME returns
  # the raw value (NOT jq-traversed JSON). Each named export accessed individually.
  cat > "$tmpdir/bin/npx" <<EOF
#!/usr/bin/env bash
# Stub npx for pulumi stack output NAME (v2 named-exports schema)
HR=\$(cat "$tmpdir/has_runtime" 2>/dev/null || echo "true")
HR2=\$(cat "$tmpdir/has_runtime2" 2>/dev/null || echo "true")

# Expected invocation: npx pulumi stack output --stack drill NAME [--show-secrets]
# Extract the key (last non-flag arg or arg before --show-secrets).
KEY=""
for arg in "\$@"; do
  case "\$arg" in
    npx|pulumi|stack|output|--stack|drill|--show-secrets) ;;
    *) KEY="\$arg" ;;
  esac
done

case "\$KEY" in
  runtimeClusterEndpoint)
    if [[ "\$HR" == "true" ]]; then
      printf '%s\n' "https://aaa.k8s.ondigitalocean.com"
    fi
    ;;
  runtimeClusterKubeconfig)
    if [[ "\$HR" == "true" ]]; then
      printf '%s\n' "apiVersion: v1
kind: Config
fake-runtime-kubeconfig"
    fi
    ;;
  runtimeCluster2Endpoint)
    if [[ "\$HR2" == "true" ]]; then
      printf '%s\n' "https://bbb.k8s.ondigitalocean.com"
    fi
    ;;
  runtimeCluster2Kubeconfig)
    if [[ "\$HR2" == "true" ]]; then
      printf '%s\n' "apiVersion: v1
kind: Config
fake-runtime2-kubeconfig"
    fi
    ;;
  *)
    # Empty stdout for unknown keys (mirrors real pulumi behavior)
    ;;
esac
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

  # Post L2-unlock T1.3: lib reads `pulumi stack output NAME` directly (NOT json+jq)
  # so jq stub is no longer needed for the normal path. MOCK_PULUMI_JSON path is
  # not exercised by these tests (would require jq, but tests use npx stub instead).
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
  ( unset DRILL_KUBECONFIG; export PATH="$tmpdir/bin:$PATH"; \
    run_cc cell-default runtimeCluster ) > /dev/null 2>&1
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
    run_cc cell-default runtimeCluster --dry-run 2>/dev/null || true)
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
    run_cc cell-default runtimeCluster --dry-run 2>/dev/null || true)
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
    run_cc cell-1 runtimeCluster --dry-run 2>/dev/null || true)
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
    run_cc cell-2 runtimeCluster2 --dry-run 2>/dev/null || true)
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
    run_cc cell-default runtimeCluster > /dev/null 2>&1
  local first_run_size
  first_run_size=$(wc -c < "$tmpdir/kubectl.log")
  # Run again
  DRILL_KUBECONFIG="$tmpdir/fake-kc.yaml" \
    DRILL_PULUMI_DIR="$tmpdir" \
    PATH="$tmpdir/bin:$PATH" \
    run_cc cell-default runtimeCluster > /dev/null 2>&1
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
    run_cc cell-default runtimeCluster > /dev/null 2>&1
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
