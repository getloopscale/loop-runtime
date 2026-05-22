#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-cleanroom-orchestrator.sh.
#
# Plan: 2026-05-18-drill-skills-trio-plan.md T1.2
# Tests: 8 per T1.1 TDD spec.
#
# Stubs: kubectl, task, npx, dig, drill-preflight.sh, drill-helmfile-bootstrap.sh,
#        drill-evidence-capture.sh, drill-teardown-validator.sh

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck disable=SC2034
SCRIPT="$ROOT/infra/scripts/dr/cleanroom-orchestrator.sh"  # reference only — tests invoke via $tmpdir copy

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# ---- Stub builder ----

build_stubs() {
  local tmpdir="$1"
  local preflight_exit="${2:-0}"
  local provision_exit="${3:-0}"
  local bootstrap_exit="${4:-0}"
  local cell_connected="${5:-true}"
  local pg_host_managed="${6:-true}"

  mkdir -p "$tmpdir/bin" "$tmpdir/infra/scripts" "$tmpdir/infra/scripts/dr" "$tmpdir/infra/pulumi-drill"

  # Stub: task (drill:up + drill:down)
  cat > "$tmpdir/bin/task" <<EOF
#!/usr/bin/env bash
case "\$1" in
  drill:up) exit $provision_exit ;;
  drill:down) exit 0 ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/task"

  # Stub: npx (pulumi stack output)
  cat > "$tmpdir/bin/npx" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"pulumi stack output"*) echo "stub-kubeconfig-content" ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/npx"

  # Stub: kubectl
  if [[ "$pg_host_managed" == "true" ]]; then
    pg_host_b64=$(printf '%s' "loopzero-drill-pg-do-user-123.db.ondigitalocean.com" | base64 -w0)
  else
    pg_host_b64=$(printf '%s' "postgresql.loopzero-system.svc.cluster.local" | base64 -w0)
  fi
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"wait --for=condition=Available"*"loopzero-api"*) exit 0 ;;
  *"wait --for=condition=Available"*"argocd-server"*) exit 0 ;;
  *"wait --for=condition=Available"*"argo-workflows-server"*) exit 0 ;;
  *"clustercredential"*"jsonpath={.status.connected}"*)
    echo "$cell_connected"
    exit 0 ;;
  *"secret loopzero-db-credentials"*"data.host"*)
    echo "$pg_host_b64"
    exit 0 ;;
  *"get pod -l loopzero.io/app-type=server"*)
    echo "registry.digitalocean.com/loopzero-system/loopzero-api:dev"
    exit 0 ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Stub: dig
  cat > "$tmpdir/bin/dig" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *NS*) echo "mina.ns.cloudflare.com." ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/dig"

  # Stub: drill-preflight (per arg exit)
  cat > "$tmpdir/infra/scripts/dr/preflight.sh" <<EOF
#!/usr/bin/env bash
exit $preflight_exit
EOF
  chmod +x "$tmpdir/infra/scripts/dr/preflight.sh"

  # Stub: drill-helmfile-bootstrap
  cat > "$tmpdir/infra/scripts/drill-helmfile-bootstrap.sh" <<EOF
#!/usr/bin/env bash
exit $bootstrap_exit
EOF
  chmod +x "$tmpdir/infra/scripts/drill-helmfile-bootstrap.sh"

  # Stub: drill-teardown-validator
  cat > "$tmpdir/infra/scripts/dr/teardown-validator.sh" <<EOF
#!/usr/bin/env bash
echo "stub teardown validator OK"
exit 0
EOF
  chmod +x "$tmpdir/infra/scripts/dr/teardown-validator.sh"

  # Stub: drill-evidence-capture
  cat > "$tmpdir/infra/scripts/dr/evidence-capture.sh" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/infra/scripts/dr/evidence-capture.sh"

  # Copy real lib + orchestrator + invoke with stub PATH
  mkdir -p "$tmpdir/infra/scripts/lib"
  cp "$ROOT/infra/scripts/lib/drill-skill-common.sh" "$tmpdir/infra/scripts/lib/"
  cp "$ROOT/infra/scripts/dr/cleanroom-orchestrator.sh" "$tmpdir/infra/scripts/dr/"
  chmod +x "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh"

  # Stub Taskfile (orchestrator runs from tmpdir/infra/scripts/.. = tmpdir)
  cat > "$tmpdir/Taskfile.yml" <<EOF
version: '3'
tasks:
  drill:up: { cmds: [exit $provision_exit] }
  drill:down: { cmds: [exit 0] }
EOF
}

run_orchestrator() {
  local tmpdir="$1"
  shift
  cd "$tmpdir"
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=false \
    LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    bash "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" "$@" 2>&1
}

# ---- Tests ----

test_missing_confirm_cost_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=false \
    LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    bash "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || { echo "  expected exit 2, got $rc" >&2; return 1; }
}

test_missing_sponsor_env_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=false \
    env -u LOOPZERO_DRILL_SKILL_SPONSOR \
    bash "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" --confirm-cost >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || { echo "  expected exit 2 (no sponsor), got $rc" >&2; return 1; }
}

test_ci_bypasses_confirm_cost_and_sponsor() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=true \
    env -u LOOPZERO_DRILL_SKILL_SPONSOR \
    bash "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" --dry-run >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || { echo "  expected exit 0 in CI dry-run, got $rc" >&2; return 1; }
}

test_preflight_fail_exits_3() {
  local tmpdir="$1"
  build_stubs "$tmpdir" 1 0 0
  set +e
  run_orchestrator "$tmpdir" --confirm-cost >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || { echo "  expected exit 3 (preflight fail), got $rc" >&2; return 1; }
}

test_provision_fail_triggers_teardown_exits_4() {
  local tmpdir="$1"
  build_stubs "$tmpdir" 0 1 0
  set +e
  run_orchestrator "$tmpdir" --confirm-cost >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 4 ]] || { echo "  expected exit 4, got $rc" >&2; return 1; }
}

test_bootstrap_fail_exits_5() {
  local tmpdir="$1"
  build_stubs "$tmpdir" 0 0 1
  set +e
  run_orchestrator "$tmpdir" --confirm-cost >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 5 ]] || { echo "  expected exit 5, got $rc" >&2; return 1; }
}

test_dry_run_skips_all_phases() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(run_orchestrator "$tmpdir" --confirm-cost --dry-run 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || { echo "  expected exit 0, got $rc" >&2; return 1; }
  echo "$out" | grep -q "DRY_RUN=true" || { echo "  expected DRY_RUN log" >&2; return 1; }
}

test_help_flag_exits_0() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(bash "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" --help 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || { echo "  expected exit 0, got $rc" >&2; return 1; }
  echo "$out" | grep -q "drill bootstrap reproducibility" || return 1
}

test_skip_teardown_env_keeps_stack_alive() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(cd "$tmpdir" && \
    PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=false \
    LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    DRILL_SKIP_TEARDOWN=true \
    bash "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" --confirm-cost 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || { echo "  expected exit 0, got $rc" >&2; echo "$out" | tail -5 >&2; return 1; }
  echo "$out" | grep -q "Teardown SKIPPED" || { echo "  expected 'Teardown SKIPPED' marker in output" >&2; return 1; }
  echo "$out" | grep -q "stack ALIVE for chained validation" || { echo "  expected ALIVE marker" >&2; return 1; }
}

# ---- Test runner ----

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-cleanroom-test-XXXXXX)
  if "$name" "$tmpdir"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  [PASS] $name"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  [FAIL] $name"
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-cleanroom-orchestrator hermetic tests (T1.2) ==="

run_test test_missing_confirm_cost_exits_2
run_test test_missing_sponsor_env_exits_2
run_test test_ci_bypasses_confirm_cost_and_sponsor
run_test test_preflight_fail_exits_3
run_test test_provision_fail_triggers_teardown_exits_4
run_test test_bootstrap_fail_exits_5
run_test test_dry_run_skips_all_phases
run_test test_help_flag_exits_0
run_test test_skip_teardown_env_keeps_stack_alive

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

[[ "$TESTS_FAILED" -eq 0 ]] || exit 1
