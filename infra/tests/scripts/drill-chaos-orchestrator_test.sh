#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-chaos-orchestrator.sh.
# Plan: 2026-05-18-drill-skills-trio-plan.md T3.2

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_stubs() {
  local tmpdir="$1"
  local cleanroom_exit="${2:-0}"
  local helm_exit="${3:-0}"
  local curl_code="${4:-200}"

  mkdir -p "$tmpdir/bin" "$tmpdir/infra/scripts" "$tmpdir/infra/scripts/lib" \
           "$tmpdir/infra/tests/chaos/manifests"

  # Stub: cleanroom-drill
  cat > "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" <<EOF
#!/usr/bin/env bash
exit $cleanroom_exit
EOF
  chmod +x "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh"

  cat > "$tmpdir/infra/scripts/dr/teardown-validator.sh" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/infra/scripts/dr/teardown-validator.sh"

  cat > "$tmpdir/infra/scripts/dr/evidence-capture.sh" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/infra/scripts/dr/evidence-capture.sh"

  # Stub: helm
  cat > "$tmpdir/bin/helm" <<EOF
#!/usr/bin/env bash
exit $helm_exit
EOF
  chmod +x "$tmpdir/bin/helm"

  # Stub: task
  cat > "$tmpdir/bin/task" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/task"

  # Stub: kubectl (chaos resources + cleanup)
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Stub: curl (sustained traffic)
  cat > "$tmpdir/bin/curl" <<EOF
#!/usr/bin/env bash
for arg in "\$@"; do
  case "\$arg" in
    "%{http_code}") printf '%s' "$curl_code"; exit 0 ;;
  esac
done
exit 0
EOF
  chmod +x "$tmpdir/bin/curl"

  # Stub: dig
  cat > "$tmpdir/bin/dig" <<EOF
#!/usr/bin/env bash
echo "mina.ns.cloudflare.com."
exit 0
EOF
  chmod +x "$tmpdir/bin/dig"

  # Create chaos manifest stubs (so file existence check passes)
  for m in pod-kill-api-leader.yaml build-cluster-network-partition.yaml \
           buildkit-io-pressure.yaml drill-pg-managed-failover.yaml \
           drill-cert-renewal-under-network-fault.yaml; do
    cat > "$tmpdir/infra/tests/chaos/manifests/$m" <<EOF
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: stub
spec: {}
EOF
  done

  # Copy real lib + orchestrator
  cp "$ROOT/infra/scripts/lib/drill-skill-common.sh" "$tmpdir/infra/scripts/lib/"
  cp "$ROOT/infra/scripts/dr/chaos-orchestrator.sh" "$tmpdir/infra/scripts/"
  chmod +x "$tmpdir/infra/scripts/dr/chaos-orchestrator.sh"

  # Fake runtime kubeconfig
  mkdir -p /tmp/drill-kc
  echo "fake-kubeconfig" > /tmp/drill-kc/runtime.yaml
}

run_orchestrator() {
  local tmpdir="$1"
  shift
  cd "$tmpdir"
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=false \
    LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    TRAFFIC_DURATION=2 \
    bash "$tmpdir/infra/scripts/dr/chaos-orchestrator.sh" "$@" 2>&1
}

# ---- Tests ----

test_missing_confirm_cost_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" CI=false LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    bash "$tmpdir/infra/scripts/dr/chaos-orchestrator.sh" >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
}

test_unknown_scenario_exits_1() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" CI=true \
    bash "$tmpdir/infra/scripts/dr/chaos-orchestrator.sh" --scenario invalid-id >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 1 ]] || { echo "  expected exit 1, got $rc" >&2; return 1; }
}

test_dry_run_lists_all_5_scenarios() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(run_orchestrator "$tmpdir" --confirm-cost --dry-run 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  local count
  count=$(echo "$out" | grep -cE "\[DRY\] (pod-kill-api|build-network|buildkit-io|pg-managed-failover|cert-renewal-fault)")
  [[ "$count" -eq 5 ]] || { echo "  expected 5 scenarios, got $count" >&2; return 1; }
}

test_dry_run_single_scenario() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(run_orchestrator "$tmpdir" --confirm-cost --scenario pg-managed-failover --dry-run 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  local count
  count=$(echo "$out" | grep -cE "\[DRY\]")
  [[ "$count" -eq 1 ]] || return 1
}

test_cleanroom_failure_aborts_chaos() {
  local tmpdir="$1"
  build_stubs "$tmpdir" 3
  set +e
  run_orchestrator "$tmpdir" --confirm-cost --scenario pod-kill-api >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || return 1
}

test_help_lists_5_scenarios() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(bash "$tmpdir/infra/scripts/dr/chaos-orchestrator.sh" --help 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  echo "$out" | grep -q "Scenarios (5)" || { echo "  missing 'Scenarios (5)'" >&2; return 1; }
  echo "$out" | grep -q "pg-managed-failover" || return 1
  echo "$out" | grep -q "cert-renewal-fault" || return 1
}

test_2_new_manifests_exist_in_repo() {
  # Test validates the chaos manifest paths used by orchestrator exist
  [[ -f "$ROOT/infra/tests/chaos/manifests/drill-pg-managed-failover.yaml" ]] || {
    echo "  missing drill-pg-managed-failover.yaml" >&2
    return 1
  }
  [[ -f "$ROOT/infra/tests/chaos/manifests/drill-cert-renewal-under-network-fault.yaml" ]] || {
    echo "  missing drill-cert-renewal-under-network-fault.yaml" >&2
    return 1
  }
}

test_chaos_manifests_have_hypothesis_annotation() {
  local manifests=(
    "$ROOT/infra/tests/chaos/manifests/drill-pg-managed-failover.yaml"
    "$ROOT/infra/tests/chaos/manifests/drill-cert-renewal-under-network-fault.yaml"
  )
  for m in "${manifests[@]}"; do
    grep -q "loopzero.io/hypothesis:" "$m" || {
      echo "  $m missing hypothesis annotation" >&2
      return 1
    }
    grep -q "loopzero.io/slo-query:" "$m" || {
      echo "  $m missing slo-query annotation" >&2
      return 1
    }
    grep -q "loopzero.io/recovery-budget-seconds:" "$m" || {
      echo "  $m missing recovery-budget annotation" >&2
      return 1
    }
  done
}

# ---- Test runner ----

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-chaos-test-XXXXXX)
  if "$name" "$tmpdir"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  [PASS] $name"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  [FAIL] $name"
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-chaos-orchestrator hermetic tests (T3.2) ==="

run_test test_missing_confirm_cost_exits_2
run_test test_unknown_scenario_exits_1
run_test test_dry_run_lists_all_5_scenarios
run_test test_dry_run_single_scenario
run_test test_cleanroom_failure_aborts_chaos
run_test test_help_lists_5_scenarios
run_test test_2_new_manifests_exist_in_repo
run_test test_chaos_manifests_have_hypothesis_annotation

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

[[ "$TESTS_FAILED" -eq 0 ]] || exit 1
