#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for drill-dogfood-orchestrator.sh.
# Plan: 2026-05-18-drill-skills-trio-plan.md T2.2

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck disable=SC2034
SCRIPT_PATH="$ROOT/infra/scripts/dr/dogfood-orchestrator.sh"  # reference only — tests invoke via $tmpdir copy

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

build_stubs() {
  local tmpdir="$1"
  local cleanroom_exit="${2:-0}"
  local jwt_output="${3:-eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0aGVvIn0.fake}"
  local curl_code="${4:-200}"
  local invariant_pass="${5:-true}"

  mkdir -p "$tmpdir/bin" "$tmpdir/infra/scripts" "$tmpdir/infra/scripts/lib" "$tmpdir/infra/tests/e2e/fixtures/hello-server"

  # Stub: cleanroom-drill (delegated foundation step)
  cat > "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh" <<EOF
#!/usr/bin/env bash
exit $cleanroom_exit
EOF
  chmod +x "$tmpdir/infra/scripts/dr/cleanroom-orchestrator.sh"

  # Stub: drill-mint-jwt (returns JWT on stdout)
  cat > "$tmpdir/infra/scripts/drill-mint-jwt.sh" <<EOF
#!/usr/bin/env bash
echo "$jwt_output"
exit 0
EOF
  chmod +x "$tmpdir/infra/scripts/drill-mint-jwt.sh"

  # Stub: drill-teardown-validator
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

  # Stub: task (drill:down)
  cat > "$tmpdir/bin/task" <<EOF
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$tmpdir/bin/task"

  # Stub: curl (configurable HTTP code)
  cat > "$tmpdir/bin/curl" <<EOF
#!/usr/bin/env bash
# Parse -w arg
for arg in "\$@"; do
  case "\$arg" in
    "%{http_code}")
      printf '%s' "$curl_code"
      exit 0 ;;
  esac
done
exit 0
EOF
  chmod +x "$tmpdir/bin/curl"

  # Stub: kubectl (for invariants)
  if [[ "$invariant_pass" == "true" ]]; then
    image_value="registry.digitalocean.com/loopzero-system/loopzero-api:dev"
    pg_host_b64=$(printf '%s' "loopzero-drill-pg-do-user-123.db.ondigitalocean.com" | base64 -w0)
    cert_issuer="letsencrypt-prod"
  else
    image_value="k3d-loopzero-registry:5000/loopzero-api:dev"
    pg_host_b64=$(printf '%s' "postgresql.loopzero-system.svc.cluster.local" | base64 -w0)
    cert_issuer="loopzero-ca-issuer"
  fi
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"get pod"*"loopzero.io/app-type=server"*)
    echo "$image_value"
    exit 0 ;;
  *"controller.cert-manager.io/fao=true"*)
    echo "$cert_issuer"
    exit 0 ;;
  *"secret loopzero-db-credentials"*"data.host"*)
    echo "$pg_host_b64"
    exit 0 ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Stub: dig (CF nameservers)
  cat > "$tmpdir/bin/dig" <<EOF
#!/usr/bin/env bash
echo "mina.ns.cloudflare.com."
exit 0
EOF
  chmod +x "$tmpdir/bin/dig"

  # Fixture file (avoid orchestrator failing on --data-binary @path/missing)
  touch "$tmpdir/infra/tests/e2e/fixtures/hello-server/source.bundle"

  # Copy real lib + orchestrator
  cp "$ROOT/infra/scripts/lib/drill-skill-common.sh" "$tmpdir/infra/scripts/lib/"
  cp "$ROOT/infra/scripts/dr/dogfood-orchestrator.sh" "$tmpdir/infra/scripts/"
  chmod +x "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh"

  # Fake runtime kubeconfig (file exists for invariants step)
  mkdir -p /tmp/drill-kc
  echo "fake-kubeconfig" > /tmp/drill-kc/runtime.yaml
  echo "fake-kubeconfig" > /tmp/drill-mgmt-kc.yaml
}

run_orchestrator() {
  local tmpdir="$1"
  shift
  cd "$tmpdir"
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    CI=false \
    LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    bash "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh" "$@" 2>&1
}

# ---- Tests ----

test_missing_confirm_cost_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" CI=false LOOPZERO_DRILL_SKILL_SPONSOR=tt \
    bash "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh" >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || { echo "  got $rc, expected 2" >&2; return 1; }
}

test_missing_sponsor_exits_2() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" CI=false \
    env -u LOOPZERO_DRILL_SKILL_SPONSOR \
    bash "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh" --confirm-cost >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 2 ]] || return 1
}

test_ci_bypass_dry_run() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  PATH="$tmpdir/bin:/usr/bin:/bin" CI=true \
    bash "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh" --dry-run >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

test_dry_run_lists_12_journeys() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(run_orchestrator "$tmpdir" --confirm-cost --dry-run 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  local count
  count=$(echo "$out" | grep -cE "\[DRY\] J[0-9]+")
  [[ "$count" -eq 12 ]] || { echo "  expected 12 journeys, got $count" >&2; return 1; }
}

test_cleanroom_failure_aborts_dogfood() {
  local tmpdir="$1"
  build_stubs "$tmpdir" 3
  set +e
  run_orchestrator "$tmpdir" --confirm-cost >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 3 ]] || { echo "  expected exit 3, got $rc" >&2; return 1; }
}

test_skip_cleanroom_flag() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(run_orchestrator "$tmpdir" --confirm-cost --skip-cleanroom --dry-run 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
}

test_help_flag() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  out=$(bash "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh" --help 2>&1)
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || return 1
  echo "$out" | grep -q "Journeys (12)" || { echo "  missing 'Journeys (12)' in help" >&2; return 1; }
  echo "$out" | grep -q "Invariants (5)" || { echo "  missing 'Invariants (5)' in help" >&2; return 1; }
}

test_unknown_arg_exits_1() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  set +e
  run_orchestrator "$tmpdir" --bogus-flag >/dev/null 2>&1
  local rc=$?
  set -e
  [[ "$rc" -eq 1 ]] || return 1
}

test_concurrent_dispatch_blocked_by_flock() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  # Acquire lock manually first
  exec 99>/tmp/drill-skill.lock
  flock -n 99 || { echo "  test setup failed — couldn't acquire lock" >&2; return 1; }
  set +e
  run_orchestrator "$tmpdir" --confirm-cost --dry-run >/dev/null 2>&1
  local rc=$?
  set -e
  flock -u 99
  exec 99>&-
  rm -f /tmp/drill-skill.lock
  [[ "$rc" -eq 2 ]] || { echo "  expected exit 2 (lock contention), got $rc" >&2; return 1; }
}

test_audit_dir_created() {
  local tmpdir="$1"
  build_stubs "$tmpdir"
  rm -rf /tmp/drill-audit-dogfood-drill-* 2>/dev/null || true
  set +e
  run_orchestrator "$tmpdir" --confirm-cost --dry-run >/dev/null 2>&1
  local rc=$?
  set -e
  ls -d /tmp/drill-audit-dogfood-drill-* >/dev/null 2>&1 || {
    echo "  audit dir not created" >&2
    return 1
  }
  rm -rf /tmp/drill-audit-dogfood-drill-* 2>/dev/null || true
}

test_invariants_run_against_runtime_cluster() {
  # This test validates the orchestrator references RUNTIME_KC = /tmp/drill-kc/runtime.yaml
  local tmpdir="$1"
  build_stubs "$tmpdir"
  grep -q "RUNTIME_KC=/tmp/drill-kc/runtime.yaml" "$tmpdir/infra/scripts/dr/dogfood-orchestrator.sh" || {
    echo "  RUNTIME_KC not referenced correctly" >&2
    return 1
  }
}

# ---- Test runner ----

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t drill-dogfood-test-XXXXXX)
  if "$name" "$tmpdir"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  [PASS] $name"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  [FAIL] $name"
  fi
  rm -rf "$tmpdir"
}

echo "=== drill-dogfood-orchestrator hermetic tests (T2.2) ==="

run_test test_missing_confirm_cost_exits_2
run_test test_missing_sponsor_exits_2
run_test test_ci_bypass_dry_run
run_test test_dry_run_lists_12_journeys
run_test test_cleanroom_failure_aborts_dogfood
run_test test_skip_cleanroom_flag
run_test test_help_flag
run_test test_unknown_arg_exits_1
run_test test_concurrent_dispatch_blocked_by_flock
run_test test_audit_dir_created
run_test test_invariants_run_against_runtime_cluster

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

[[ "$TESTS_FAILED" -eq 0 ]] || exit 1
