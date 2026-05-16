#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Hermetic tests for check-drill-credentials-isolation.sh (Plan T0.1 EC-09).
#
# Tests cover:
#   - DRILL_DO_TOKEN missing → exit 2
#   - DRILL_DO_TOKEN matches prod DO_TOKEN → exit 1 CATASTROPHIC
#   - DRILL_DO_TOKEN matches prod DIGITALOCEAN_TOKEN → exit 1 CATASTROPHIC
#   - DRILL_DO_TOKEN distinct from prod → exit 0
#   - DRILL_DO_TOKEN drill-only env (no prod token) → exit 0
#   - DRILL_PULUMI_TOKEN matches prod → exit 1
#   - DRILL_CF_TOKEN matches prod → exit 1

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="$ROOT/infra/scripts/check-drill-credentials-isolation.sh"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

run_test() {
  local name="$1"
  shift
  TESTS_RUN=$((TESTS_RUN + 1))
  local result
  if "$@"; then
    result="PASS"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    result="FAIL"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
  echo "  [$result] $name"
}

test_missing_drill_token_exits_2() {
  local actual
  set +e
  env -u DRILL_DO_TOKEN -u DO_TOKEN -u DIGITALOCEAN_TOKEN \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 2 ]] && return 0 || return 1
}

test_drill_matches_prod_do_token_exits_1() {
  local actual
  set +e
  DRILL_DO_TOKEN="same-token-abc123" \
    DO_TOKEN="same-token-abc123" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_drill_matches_prod_digitalocean_token_exits_1() {
  local actual
  set +e
  DRILL_DO_TOKEN="same-token-xyz789" \
    DIGITALOCEAN_TOKEN="same-token-xyz789" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_drill_distinct_from_prod_exits_0() {
  local actual
  set +e
  DRILL_DO_TOKEN="drill-token-123" \
    DO_TOKEN="prod-token-456" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 0 ]] && return 0 || return 1
}

test_drill_only_env_exits_0() {
  local actual
  set +e
  env -u DO_TOKEN -u DIGITALOCEAN_TOKEN -u PULUMI_ACCESS_TOKEN -u CLOUDFLARE_API_TOKEN \
    DRILL_DO_TOKEN="drill-only-token" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 0 ]] && return 0 || return 1
}

test_drill_pulumi_matches_prod_exits_1() {
  local actual
  set +e
  DRILL_DO_TOKEN="drill-do-distinct" \
    DRILL_PULUMI_TOKEN="same-pulumi-token" \
    PULUMI_ACCESS_TOKEN="same-pulumi-token" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

test_drill_cf_matches_prod_exits_1() {
  local actual
  set +e
  DRILL_DO_TOKEN="drill-do-distinct" \
    DRILL_CF_TOKEN="same-cf-token" \
    CLOUDFLARE_API_TOKEN="same-cf-token" \
    "$SCRIPT" > /dev/null 2>&1
  actual=$?
  set -e
  [[ "$actual" -eq 1 ]] && return 0 || return 1
}

echo "Running hermetic tests for check-drill-credentials-isolation.sh..."
echo ""

run_test "missing_DRILL_DO_TOKEN_exits_2" test_missing_drill_token_exits_2
run_test "drill_matches_prod_DO_TOKEN_exits_1" test_drill_matches_prod_do_token_exits_1
run_test "drill_matches_prod_DIGITALOCEAN_TOKEN_exits_1" test_drill_matches_prod_digitalocean_token_exits_1
run_test "drill_distinct_from_prod_exits_0" test_drill_distinct_from_prod_exits_0
run_test "drill_only_env_no_prod_token_exits_0" test_drill_only_env_exits_0
run_test "drill_pulumi_matches_prod_exits_1" test_drill_pulumi_matches_prod_exits_1
run_test "drill_cf_matches_prod_exits_1" test_drill_cf_matches_prod_exits_1

echo ""
echo "Total: $TESTS_RUN | Passed: $TESTS_PASSED | Failed: $TESTS_FAILED"

if [[ $TESTS_FAILED -gt 0 ]]; then
  exit 1
fi
