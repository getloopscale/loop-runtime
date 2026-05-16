#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-helmfile-bootstrap.sh (Plan T2.1 + T2.1.bis).
#
# TDD specs from plan T2.1:
#   test_missing_DRILL_KUBECONFIG_dir_exits_2
#   test_missing_pulumi_outputs_exits_2
#   test_mgmt_first_then_build               — verify cluster iteration order
#   test_pg_host_injected_from_pulumi_output
#   test_health_check_pre_exit               — verify kubectl wait invoked
#   test_strict_mode_fails_on_partial
#   test_permissive_mode_continues_on_partial
#   test_partial_helmfile_recovery_per_cluster (EC-05) — --from-cluster
#
# Additional T2.1.bis coverage:
#   test_pg_sslmode_require_set_for_drill
#
# Mocking: PATH-shadow pulumi + helmfile + kubectl with controllable stubs.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/drill-helmfile-bootstrap.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Build stubs for pulumi/helmfile/kubectl. Scenarios driven by env file.
build_stubs() {
  local tmpdir="$1"
  local pulumi_scenario="${2:-ok}"     # ok | empty-pg
  local helmfile_scenario="${3:-all-ok}"  # all-ok | runtime-fails | all-fail

  mkdir -p "$tmpdir/bin" "$tmpdir/pulumi-drill" "$tmpdir/helmfile" "$tmpdir/drill-kc"

  # 4 kubeconfig files (per cluster)
  for c in mgmt build runtime runtime-2; do
    printf 'apiVersion: v1\nkind: Config\ncurrent-context: %s\n' "$c" > "$tmpdir/drill-kc/$c.yaml"
  done

  # helmfile.yaml (placeholder)
  touch "$tmpdir/helmfile/helmfile.yaml"

  echo "$pulumi_scenario" > "$tmpdir/pulumi-scenario.txt"
  echo "$helmfile_scenario" > "$tmpdir/helmfile-scenario.txt"

  # --- pulumi stub ---
  cat > "$tmpdir/bin/pulumi" <<EOF
#!/usr/bin/env bash
SCEN=\$(cat "$tmpdir/pulumi-scenario.txt" 2>/dev/null || echo ok)
# Parse: pulumi stack output --stack drill --cwd <dir> NAME [--show-secrets]
KEY=""
for arg in "\$@"; do
  case "\$arg" in
    pulumi|stack|output|--stack|drill|--cwd|--show-secrets) ;;
    "$tmpdir/pulumi-drill") ;;
    *) KEY="\$arg" ;;
  esac
done
if [[ "\$SCEN" == "empty-pg" ]]; then
  # Simulate missing required outputs
  exit 0
fi
case "\$KEY" in
  pgHost) echo "drill-pg.host.example.com" ;;
  pgPort) echo "25060" ;;
  pgDatabase) echo "defaultdb" ;;
  mgmtClusterEndpoint) echo "https://mgmt.k8s.ondigitalocean.com" ;;
  *) ;;
esac
exit 0
EOF
  chmod +x "$tmpdir/bin/pulumi"

  # --- helmfile stub ---
  cat > "$tmpdir/bin/helmfile" <<EOF
#!/usr/bin/env bash
SCEN=\$(cat "$tmpdir/helmfile-scenario.txt" 2>/dev/null || echo all-ok)
# Detect which cluster via KUBECONFIG env (set per cluster by orchestrator)
CLUSTER_FROM_KC=\$(basename "\${KUBECONFIG:-unknown}" .yaml)
echo "\$CLUSTER_FROM_KC" >> "$tmpdir/cluster-order.txt"

# Record env vars for assertions
{
  echo "cluster=\$CLUSTER_FROM_KC PG_SSLMODE=\${PG_SSLMODE:-} PG_HOST=\${PG_HOST:-}"
} >> "$tmpdir/helmfile-env.log"

case "\$SCEN" in
  all-ok) exit 0 ;;
  runtime-fails)
    if [[ "\$CLUSTER_FROM_KC" == "runtime" ]]; then
      exit 1
    fi
    exit 0 ;;
  all-fail) exit 1 ;;
esac
exit 0
EOF
  chmod +x "$tmpdir/bin/helmfile"

  # --- kubectl stub (for Phase 4 health check) ---
  cat > "$tmpdir/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
# Stub kubectl wait — always succeeds for tests (health check tested separately)
case "$*" in
  *"wait --for=condition=Available"*) exit 0 ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/kubectl"
}

run_script() {
  local tmpdir="$1"
  shift

  PATH="$tmpdir/bin:/usr/bin:/bin" \
    DRILL_KUBECONFIG_DIR="$tmpdir/drill-kc" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    HELMFILE_DIR="$tmpdir/helmfile" \
    HELMFILE_ENV=drill \
    HEALTH_WAIT_TIMEOUT=5s \
    bash "$SCRIPT" "$@" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
}

# ---- Tests ----

test_missing_DRILL_KUBECONFIG_dir_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  rm -rf "$tmpdir/drill-kc"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
  grep -q "DRILL_KUBECONFIG_DIR not found" "$tmpdir/stderr.log" || return 1
}

test_missing_pulumi_outputs_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir" "empty-pg"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
  grep -q "required Pulumi outputs missing" "$tmpdir/stderr.log" || return 1
}

test_mgmt_first_then_build() {
  # TDD spec: verify cluster iteration order via mock helmfile log
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # Order in cluster-order.txt: mgmt, build, runtime, runtime-2
  local actual
  actual=$(cat "$tmpdir/cluster-order.txt" | tr '\n' ',')
  [[ "$actual" == "mgmt,build,runtime,runtime-2," ]] || {
    echo "  expected order mgmt,build,runtime,runtime-2 got: $actual" >&2
    return 1
  }
}

test_pg_host_injected_from_pulumi_output() {
  # TDD spec: verify env var set before helmfile sync
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # helmfile-env.log should have PG_HOST=drill-pg.host.example.com per cluster
  grep -q "PG_HOST=drill-pg.host.example.com" "$tmpdir/helmfile-env.log" || return 1
}

test_pg_sslmode_require_set_for_drill() {
  # T2.1.bis EC-02 — managed PG SSL injection
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # Every helmfile invocation should see PG_SSLMODE=require
  local count_require
  count_require=$(grep -c "PG_SSLMODE=require" "$tmpdir/helmfile-env.log")
  [[ "$count_require" -ge 4 ]] || {
    echo "  expected PG_SSLMODE=require for 4 clusters, got $count_require" >&2
    return 1
  }
}

test_health_check_pre_exit() {
  # TDD spec: verify wait for loopzero-api Ready (kubectl wait invoked)
  local tmpdir="$1"
  build_stubs "$tmpdir"
  # Wrap kubectl to record invocations
  mv "$tmpdir/bin/kubectl" "$tmpdir/bin/kubectl.real"
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "$tmpdir/kubectl-invocations.log"
exec "$tmpdir/bin/kubectl.real" "\$@"
EOF
  chmod +x "$tmpdir/bin/kubectl"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  grep -q "wait --for=condition=Available" "$tmpdir/kubectl-invocations.log" || return 1
  grep -q "deployment/loopzero-api" "$tmpdir/kubectl-invocations.log" || return 1
}

test_strict_mode_fails_on_partial() {
  # TDD spec: STRICT_BOOTSTRAP=true + helmfile partial fail → exit 3
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "runtime-fails"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    DRILL_KUBECONFIG_DIR="$tmpdir/drill-kc" \
    PULUMI_DRILL_DIR="$tmpdir/pulumi-drill" \
    HELMFILE_DIR="$tmpdir/helmfile" \
    HEALTH_WAIT_TIMEOUT=5s \
    STRICT_BOOTSTRAP=true \
    bash "$SCRIPT" 2>"$tmpdir/stderr.log" >"$tmpdir/stdout.log"
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || {
    echo "  expected exit 3 (strict + partial), got $rc" >&2
    return 1
  }
}

test_permissive_mode_continues_on_partial() {
  # TDD spec: default permissive mode continues past partial — exit 3 at end
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "runtime-fails"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  # Default permissive STILL exits 3 if PARTIAL_FAILURES not empty,
  # but should have continued past the failure (all 4 clusters processed).
  [[ "$rc" -eq 3 ]] || return 1
  local count
  count=$(wc -l < "$tmpdir/cluster-order.txt")
  [[ "$count" -eq 4 ]] || {
    echo "  expected 4 clusters processed (permissive), got $count" >&2
    return 1
  }
}

test_partial_helmfile_recovery_per_cluster() {
  # EC-05: --from-cluster runtime resumes from runtime
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir" --from-cluster runtime
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # Only runtime + runtime-2 should have been processed (mgmt + build skipped)
  local actual
  actual=$(cat "$tmpdir/cluster-order.txt" | tr '\n' ',')
  [[ "$actual" == "runtime,runtime-2," ]] || {
    echo "  expected runtime,runtime-2 got: $actual" >&2
    return 1
  }
}

test_dry_run_no_helmfile_invocations() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir" --dry-run
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  # helmfile stub should NOT have been called
  [[ ! -f "$tmpdir/cluster-order.txt" ]] || {
    echo "  helmfile invoked in dry-run mode" >&2
    return 1
  }
}

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-helmfile-bootstrap-test-XXXXXX)
  if "$name" "$tmpdir"; then
    echo "  [PASS] $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  [FAIL] $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-helmfile-bootstrap hermetic tests (T2.1 + T2.1.bis) ==="

run_test test_missing_DRILL_KUBECONFIG_dir_exits_2
run_test test_missing_pulumi_outputs_exits_2
run_test test_mgmt_first_then_build
run_test test_pg_host_injected_from_pulumi_output
run_test test_pg_sslmode_require_set_for_drill
run_test test_health_check_pre_exit
run_test test_strict_mode_fails_on_partial
run_test test_permissive_mode_continues_on_partial
run_test test_partial_helmfile_recovery_per_cluster
run_test test_dry_run_no_helmfile_invocations

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
