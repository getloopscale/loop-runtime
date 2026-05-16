#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for run-migrations.sh PG_SSLMODE env handling
# (Plan T2.1.bis EC-02 MUST FIX — managed PG SSL injection).
#
# TDD specs from plan T2.1.bis:
#   test_run_migrations_default_sslmode_disable() — without PG_SSLMODE env,
#     DSN contains "sslmode=disable" (local k3d default preserved)
#   test_run_migrations_overrides_sslmode_require() — PG_SSLMODE=require →
#     DSN contains "sslmode=require" (drill managed PG path)
#
# Mocking: extract the DSN_DEFAULT line + simulate variable substitution
# without invoking real `migrate` binary or kubectl.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/run-migrations.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Compute DSN using the exact template from run-migrations.sh.
# Extracts the DSN_DEFAULT line literally, then evaluates with controlled env.
extract_dsn() {
  local env_overrides="$1"

  # Get the literal DSN_DEFAULT= line from the script (single line).
  local dsn_template
  dsn_template=$(grep '^DSN_DEFAULT=' "$SCRIPT")

  # Eval in subshell with default vars + override applied. This isolates
  # only the DSN computation logic without sourcing anything else.
  bash -c "
PG_USER=\${PG_USER:-loopzero}
PG_PASS=\${PG_PASS:-loopzero-dev-password}
PG_DB=\${PG_DB:-loopzero}
PG_HOST=\${PG_HOST:-postgresql.loopzero-system.svc.cluster.local}
PG_PORT=\${PG_PORT:-5432}
PG_SSLMODE=\${PG_SSLMODE:-disable}
$env_overrides
$dsn_template
echo \"\$DSN_DEFAULT\"
"
}

test_run_migrations_default_sslmode_disable() {
  # Without PG_SSLMODE env, DSN must contain sslmode=disable
  local tmpdir="$1"
  local dsn
  dsn=$(extract_dsn "")
  echo "$dsn" | grep -q "sslmode=disable" || {
    echo "  expected sslmode=disable in default DSN" >&2
    echo "  got: $dsn" >&2
    return 1
  }
}

test_run_migrations_overrides_sslmode_require() {
  # With PG_SSLMODE=require env, DSN must contain sslmode=require
  local tmpdir="$1"
  local dsn
  dsn=$(extract_dsn "export PG_SSLMODE=require")
  echo "$dsn" | grep -q "sslmode=require" || {
    echo "  expected sslmode=require in DSN with PG_SSLMODE=require env" >&2
    echo "  got: $dsn" >&2
    return 1
  }
  # Negative: must NOT have sslmode=disable
  if echo "$dsn" | grep -q "sslmode=disable"; then
    echo "  FAIL: sslmode=disable still present despite PG_SSLMODE=require override" >&2
    return 1
  fi
}

test_run_migrations_overrides_sslmode_verify_full() {
  # PG_SSLMODE=verify-full (future hardening) propagates correctly
  local tmpdir="$1"
  local dsn
  dsn=$(extract_dsn "export PG_SSLMODE=verify-full")
  echo "$dsn" | grep -q "sslmode=verify-full" || return 1
}

run_test() {
  local name="$1"
  TESTS_RUN=$((TESTS_RUN + 1))
  local tmpdir
  tmpdir=$(mktemp -d -t run-migrations-sslmode-test-XXXXXX)
  if "$name" "$tmpdir"; then
    echo "  [PASS] $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "  [FAIL] $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  rm -rf "$tmpdir"
}

echo "=== run-migrations PG_SSLMODE hermetic tests (T2.1.bis EC-02) ==="

run_test test_run_migrations_default_sslmode_disable
run_test test_run_migrations_overrides_sslmode_require
run_test test_run_migrations_overrides_sslmode_verify_full

echo ""
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  exit 1
fi
exit 0
