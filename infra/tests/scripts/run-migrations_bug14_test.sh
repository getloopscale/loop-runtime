#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic regression tests for Bug #14 — run-migrations.sh disambiguation
# of "migrate up exit=1" between idempotent re-run vs real failure.
#
# Previous code logged "checking if 'no change'" but did NOT actually check —
# always fell through to success. drill DOKS first-run (empty PG) was silently
# masked as "applied", later breaking drill-mint-jwt with `relation "users"
# does not exist`. Caught LIVE on dogfood-drill iter 6 (2026-05-18).
#
# Strategy: invoke run-migrations.sh with PATH-shadowed `kubectl` that returns
# controlled `migrate up` stdout/exit + `migrate version` stdout. Verify
# script exits accordingly.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/run-migrations.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# ---- Stub builder ----

build_stubs() {
  local tmpdir="$1"
  local migrate_up_out="$2"
  local migrate_up_exit="$3"
  local migrate_version_out="${4:-no migration}"  # default: no schema_migrations
  local has_tenants="${5:-}"                       # empty = no tenants table

  mkdir -p "$tmpdir/bin"

  # Stub kubectl: routes based on args
  cat > "$tmpdir/bin/kubectl" <<EOF
#!/usr/bin/env bash
# Simulate kctl wrapper from run-migrations.sh.
# Recognized invocations:
#   kubectl get pod -l app.kubernetes.io/name=loopzero-api -o jsonpath=...
#   kubectl get pod -l app.kubernetes.io/name=postgresql -o jsonpath=...
#   kubectl exec POD -- migrate ... up
#   kubectl exec POD -- migrate ... version
#   kubectl exec POD -- migrate ... force HIGHEST
#   kubectl exec POD -- sh -c 'psql ...' (tenants probe)
case "\$*" in
  *"app=loopzero-api"*)
    echo "loopzero-api-12345"; exit 0 ;;
  *"app.kubernetes.io/name=postgresql"*)
    echo "postgresql-0"; exit 0 ;;
  *exec*sh*"-c"*"ls /migrations"*)
    # Script pipes ls | sort | tail | sed to extract numeric version.
    # We pre-compute the final output (numeric version) since stub fakes
    # the entire `sh -c '...'` invocation as a single echo.
    echo "18"; exit 0 ;;
  *exec*migrate*\ up*)
    cat <<MIGOUT
$migrate_up_out
MIGOUT
    exit $migrate_up_exit ;;
  *exec*migrate*\ version*)
    cat <<VEROUT
$migrate_version_out
VEROUT
    exit 0 ;;
  *exec*migrate*\ force*)
    echo "force OK"; exit 0 ;;
  *exec*psql*tenants*)
    echo "$has_tenants"; exit 0 ;;
  *exec*psql*schema_migrations*)
    echo "$migrate_version_out"; exit 0 ;;
  *)
    exit 0 ;;
esac
EOF
  chmod +x "$tmpdir/bin/kubectl"

  # Stub HIGHEST file count — script uses 'ls api/internal/db/migrations | grep up.sql | wc -l'
  # We don't need to stub this since script reads from real repo dir.
}

run_script() {
  local tmpdir="$1"
  PATH="$tmpdir/bin:/usr/bin:/bin" \
    PG_HOST=stub-host PG_PORT=5432 PG_USER=stub PG_PASS=stub PG_DB=loopzero \
    PG_SSLMODE=require \
    NAMESPACE=loopzero-system \
    MGMT_CONTEXT=stub \
    bash "$SCRIPT" 2>&1
  return $?
}

# ---- Tests ----

test_no_change_message_treated_as_idempotent() {
  local tmpdir="$1"
  build_stubs "$tmpdir" "no change" 1 "5" "1"
  set +e
  out=$(run_script "$tmpdir")
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || { echo "  expected exit 0 (idempotent), got $rc" >&2; echo "$out" | tail -5 >&2; return 1; }
  echo "$out" | grep -q "Disambiguation: 'no change'" || { echo "  expected 'no change' disambiguation marker" >&2; return 1; }
}

test_version_matches_highest_treated_as_idempotent() {
  local tmpdir="$1"
  # migrate up output empty + exit 1, but version reports a number >= highest.
  # Stub hardcodes HIGHEST=18 (matches stub's "ls /migrations" output of 18).
  build_stubs "$tmpdir" "no-op" 1 "18" "1"
  set +e
  out=$(run_script "$tmpdir")
  local rc=$?
  set -e
  [[ "$rc" -eq 0 ]] || { echo "  expected exit 0 (version >= highest), got $rc" >&2; echo "$out" | tail -5 >&2; return 1; }
  echo "$out" | grep -q "version=18" || { echo "  expected 'version=18' disambiguation marker" >&2; return 1; }
}

test_real_failure_exits_nonzero() {
  local tmpdir="$1"
  # Bug #14 LIVE scenario: migrate up exit=1 with NO "no change" message,
  # AND migrate version returns "no migration" (schema_migrations absent),
  # AND tenants table doesn't exist (empty PG first-run scenario).
  build_stubs "$tmpdir" "ERROR: connection refused" 1 "no migration" ""
  set +e
  out=$(run_script "$tmpdir")
  local rc=$?
  set -e
  # Previously this returned 0 (false-positive); now must return non-zero
  [[ "$rc" -ne 0 ]] || { echo "  Bug #14 regression — expected non-zero exit on real failure, got 0" >&2; echo "$out" | tail -10 >&2; return 1; }
  echo "$out" | grep -q "REAL FAILURE" || { echo "  expected REAL FAILURE marker" >&2; return 1; }
}

# ---- Test runner ----

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t bug14-test-XXXXXX)
  if "$name" "$tmpdir"; then
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo "  [PASS] $name"
  else
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo "  [FAIL] $name"
  fi
  rm -rf "$tmpdir"
}

echo "=== run-migrations.sh Bug #14 disambiguation tests ==="

run_test test_no_change_message_treated_as_idempotent
run_test test_version_matches_highest_treated_as_idempotent
run_test test_real_failure_exits_nonzero

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

[[ "$TESTS_FAILED" -eq 0 ]] || exit 1
