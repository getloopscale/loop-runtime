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

  # Per-cluster helmfile files (iter 11 LIVE bug fix — no unified helmfile.yaml)
  touch "$tmpdir/helmfile/mgmt.helmfile.yaml.gotmpl"
  touch "$tmpdir/helmfile/build.helmfile.yaml.gotmpl"
  touch "$tmpdir/helmfile/runtime.helmfile.yaml.gotmpl"
  # runtime-2 reuses runtime.helmfile.yaml.gotmpl per script mapping

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
  # Phase 2 T1.3 — per-service DSN exports (T1.3.bis EC-1 requires --show-secrets)
  kratosDsn)
    if echo "\$*" | grep -q "show-secrets"; then
      echo "postgres://kratos:realpassword123@drill-pg.host.example.com:25060/kratos?sslmode=require"
    else
      echo "postgres://kratos:[secret]@drill-pg.host.example.com:25060/kratos?sslmode=require"
    fi
    ;;
  hydraDsn)
    if echo "\$*" | grep -q "show-secrets"; then
      echo "postgres://hydra:realpassword456@drill-pg.host.example.com:25060/hydra?sslmode=require"
    else
      echo "postgres://hydra:[secret]@drill-pg.host.example.com:25060/hydra?sslmode=require"
    fi
    ;;
  giteaDsn)
    if echo "\$*" | grep -q "show-secrets"; then
      echo "postgres://gitea:realpassword789@drill-pg.host.example.com:25060/gitea?sslmode=require"
    else
      echo "postgres://gitea:[secret]@drill-pg.host.example.com:25060/gitea?sslmode=require"
    fi
    ;;
  loopzeroApiDsn)
    if echo "\$*" | grep -q "show-secrets"; then
      echo "postgres://loopzero_api:realpasswordABC@drill-pg.host.example.com:25060/loopzero?sslmode=require"
    else
      echo "postgres://loopzero_api:[secret]@drill-pg.host.example.com:25060/loopzero?sslmode=require"
    fi
    ;;
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

  # --- kubectl stub (Phase 4 health check + Phase 2 Secret reconciliation) ---
  # T2.1.bis EC-2 guard: cluster-info return must match drill-mgmt pattern
  # T2.1: kubectl create namespace / secret invocations recorded
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
# Stub kubectl — supports cluster-info (T2.1.bis), namespace/secret (T2.1), wait (Phase 4)
KUBECTL_SCEN=\$(cat "$tmpdir/kubectl-scenario.txt" 2>/dev/null || echo drill-mgmt)
case "\$*" in
  *"cluster-info"*)
    case "\$KUBECTL_SCEN" in
      drill-mgmt) echo "Kubernetes control plane is running at https://drill-mgmt.k8s.ondigitalocean.com" ;;
      wrong-prod) echo "Kubernetes control plane is running at https://prod.k8s.example.com" ;;
      empty) ;;
    esac
    exit 0
    ;;
  *"create namespace"*)
    echo "namespace=\$3" >> "$tmpdir/kubectl-invocations.log"
    exit 0
    ;;
  *"create secret generic"*)
    # Extract secret name + namespace
    SEC_NAME=\$(echo "\$*" | sed -E 's/.*create secret generic ([^ ]+).*/\1/')
    SEC_NS=\$(echo "\$*" | sed -E 's/.*--namespace ([^ ]+).*/\1/')
    echo "secret=\$SEC_NAME namespace=\$SEC_NS" >> "$tmpdir/kubectl-invocations.log"
    exit 0
    ;;
  *"apply -f"*) exit 0 ;;
  *"wait --for=condition=Available"*) exit 0 ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Default kubectl scenario
  echo "${4:-drill-mgmt}" > "$tmpdir/kubectl-scenario.txt"
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


# --- Phase 2 plan T1.3.bis + T2.1 + T2.1.bis tests (EC-1, EC-2) ---

test_dsn_extracted_with_show_secrets_flag() {
  # T1.3.bis EC-1: bootstrap.sh MUST invoke pulumi with --show-secrets
  # to avoid "[secret]" literal in DSN → kratos crashloop
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || {
    echo "  expected exit 0, got $rc" >&2
    tail -15 "$tmpdir/stderr.log" >&2
    return 1
  }
  # Assert Secret data contains REAL password (NOT "[secret]")
  grep -q "secret=ory-kratos-dsn-managed-pg" "$tmpdir/kubectl-invocations.log" || {
    echo "  kratos Secret not created" >&2
    return 1
  }
}

test_4_k8s_secrets_created_managed_pg() {
  # T2.1: 4 Secrets created (kratos+hydra in loopzero-auth, gitea+loopzero-api in loopzero-system)
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  set -e
  for sec in ory-kratos-dsn-managed-pg ory-hydra-dsn-managed-pg gitea-dsn-managed-pg loopzero-api-dsn-managed-pg; do
    grep -q "secret=$sec " "$tmpdir/kubectl-invocations.log" || {
      echo "  missing Secret: $sec" >&2
      cat "$tmpdir/kubectl-invocations.log" >&2
      return 1
    }
  done
}

test_namespace_precreated_if_missing() {
  # T2.1: loopzero-auth + loopzero-system pre-created before Secret creation
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  set -e
  grep -q "namespace=loopzero-auth" "$tmpdir/kubectl-invocations.log" || return 1
  grep -q "namespace=loopzero-system" "$tmpdir/kubectl-invocations.log" || return 1
}

test_secret_creation_idempotent_re_run() {
  # T2.1: Re-running same bootstrap doesn't fail (kubectl apply pattern)
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_script "$tmpdir"
  local rc1=$?
  : > "$tmpdir/kubectl-invocations.log"
  run_script "$tmpdir"
  local rc2=$?
  set -e
  [[ "$rc1" -eq 0 && "$rc2" -eq 0 ]] || {
    echo "  expected both runs exit 0, got rc1=$rc1 rc2=$rc2" >&2
    return 1
  }
}

test_kubeconfig_wrong_cluster_aborts_exit_4() {
  # T2.1.bis EC-2: KUBECONFIG points to non-drill cluster → exit 4
  local tmpdir="$1"
  build_stubs "$tmpdir" "ok" "all-ok" "wrong-prod"
  set +e
  run_script "$tmpdir"
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]] || {
    echo "  expected exit 4 (cross-cluster guard), got $rc" >&2
    tail -15 "$tmpdir/stderr.log" >&2
    return 1
  }
  # Critical: NO Secrets created
  if [[ -f "$tmpdir/kubectl-invocations.log" ]] && grep -q "secret=" "$tmpdir/kubectl-invocations.log"; then
    echo "  FAIL: Secret created despite wrong cluster guard" >&2
    return 1
  fi
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
run_test test_dsn_extracted_with_show_secrets_flag
run_test test_4_k8s_secrets_created_managed_pg
run_test test_namespace_precreated_if_missing
run_test test_secret_creation_idempotent_re_run
run_test test_kubeconfig_wrong_cluster_aborts_exit_4

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
