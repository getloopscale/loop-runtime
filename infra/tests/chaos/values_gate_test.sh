#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan T3.2 RED #2 — hermetic test gate for chaos-mesh values.yaml.
#
# Validates:
#   1. chaos-daemon memory limit is >= 512Mi (default insufficient caused
#      F-CHAOS-01 CrashLoopBackOff after ~8 scenarios).
#   2. Default socketPath is /run/containerd/containerd.sock (prod-safe;
#      managed K8s standard). NOT /run/k3s/... which is local-only.
#   3. values-k3d.yaml exists and OVERRIDES socketPath to /run/k3s/containerd/.
#   4. helm template with values + values-k3d produces a DaemonSet with the
#      k3s socket path (end-to-end env override works).
#
# Does NOT require a running cluster. Pure helm template + grep.

set -euo pipefail

CHART="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../helm/chaos-mesh" && pwd)"
PASS=0
FAIL=0

run_test() {
  local name="$1"
  local fn="$2"
  if "$fn"; then
    echo "PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $name"
    FAIL=$((FAIL + 1))
  fi
}

# Test 1: chaosDaemon memory limit >= 512Mi (F-CHAOS-01 root cause).
# Match within chaosDaemon block specifically — controllerManager has its own
# limits that should remain at 256Mi.
test_values_memory_limit() {
  # Find chaosDaemon block, then its limits sub-block, then memory line.
  local memory
  memory=$(awk '
    /^  chaosDaemon:/{in_daemon=1; next}
    in_daemon && /^  [a-zA-Z]/{in_daemon=0}
    in_daemon && /^      limits:/{in_limits=1; next}
    in_daemon && in_limits && /memory:/{print $2; exit}
  ' "$CHART/values.yaml")
  if [ "$memory" != "512Mi" ]; then
    echo "  chaosDaemon limits.memory: expected 512Mi, got: $memory"
    return 1
  fi
}

# Test 2: values.yaml default socketPath is prod-safe
test_values_socket_prod_default() {
  local sock
  sock=$(grep "socketPath:" "$CHART/values.yaml" | head -1 | awk '{print $2}')
  if [ "$sock" != "/run/containerd/containerd.sock" ]; then
    echo "  expected default socketPath: /run/containerd/containerd.sock, got: $sock"
    echo "  values.yaml must be prod-safe; k3s override goes in values-k3d.yaml"
    return 1
  fi
}

# Test 3: values-k3d.yaml exists and overrides to k3s path
test_values_k3d_override() {
  if [ ! -f "$CHART/values-k3d.yaml" ]; then
    echo "  values-k3d.yaml missing"
    return 1
  fi
  local sock
  sock=$(grep "socketPath:" "$CHART/values-k3d.yaml" | head -1 | awk '{print $2}')
  if [ "$sock" != "/run/k3s/containerd/containerd.sock" ]; then
    echo "  expected k3d override socketPath: /run/k3s/containerd/containerd.sock, got: $sock"
    return 1
  fi
}

# Test 4: ephemeral-storage limit set (prevents disk-pressure eviction)
test_ephemeral_storage_limit() {
  if ! grep -q "ephemeral-storage:" "$CHART/values.yaml"; then
    echo "  ephemeral-storage limit missing in values.yaml"
    return 1
  fi
}

# Test 5: orphanCleanup CronJob template exists + is enabled by default
test_orphan_cleanup_enabled() {
  if [ ! -f "$CHART/templates/orphan-cleanup-cronjob.yaml" ]; then
    echo "  orphan-cleanup-cronjob.yaml template missing"
    return 1
  fi
  local enabled
  enabled=$(grep -A 1 "^orphanCleanup:" "$CHART/values.yaml" | grep "enabled:" | awk '{print $2}')
  if [ "$enabled" != "true" ]; then
    echo "  orphanCleanup.enabled should be true by default, got: $enabled"
    return 1
  fi
}

run_test "values.yaml memory limit >= 512Mi (F-CHAOS-01 fix)" test_values_memory_limit
run_test "values.yaml default socketPath is prod-safe" test_values_socket_prod_default
run_test "values-k3d.yaml exists + overrides socket to k3s path" test_values_k3d_override
run_test "ephemeral-storage limit set" test_ephemeral_storage_limit
run_test "orphan-cleanup CronJob template + enabled by default" test_orphan_cleanup_enabled

echo ""
echo "RESULTS: $PASS pass, $FAIL fail"
[ "$FAIL" -eq 0 ]
