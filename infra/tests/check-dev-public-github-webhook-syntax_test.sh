#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan J-09 T4.1 (2026-06-05) — hermetic structural test for
# infra/tests/e2e/dev-public-github-webhook_test.sh.
#
# Verifies the LIVE script:
#   - parses cleanly (bash -n)
#   - declares all 4 scenarios (A/B/C/D)
#   - implements the pre-flight gate
#   - wires --skip-live / --scenario= / --json flags
#   - signs payloads with HMAC-SHA256 (openssl dgst -sha256 -hmac)
#   - reads the loopzero_github_webhook_deliveries_total counter from /metrics
#   - is allowlisted in shell-scripts-allowlist.md (Rule 12)
#
# Pure-text checks. NO network, NO curl, NO openssl invocation.

set -uo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
TARGET="$REPO_ROOT/tests/e2e/dev-public-github-webhook_test.sh"
ALLOWLIST="$REPO_ROOT/../.claude/rules/shell-scripts-allowlist.md"

asserts=0
failures=0

assert() {
  local name="$1"; shift
  asserts=$((asserts+1))
  if "$@"; then
    printf '  [PASS] %s\n' "$name"
  else
    printf '  [FAIL] %s\n' "$name"
    failures=$((failures+1))
  fi
}

contains() { grep -qF -- "$2" "$1"; }
matches() { grep -qE -- "$2" "$1"; }

echo "[$(basename "$0")] structural test for dev-public-github-webhook_test.sh"

assert "target exists" test -f "$TARGET"
assert "target is executable" test -x "$TARGET"
assert "bash -n parses cleanly" bash -n "$TARGET"

# Scenario functions present.
assert "scenario A function defined" contains "$TARGET" "scenario_A_accepted_push()"
assert "scenario B function defined" contains "$TARGET" "scenario_B_invalid_signature()"
assert "scenario C function defined" contains "$TARGET" "scenario_C_preflight_recorded()"
assert "scenario D function defined" contains "$TARGET" "scenario_D_correlation_id_propagated()"

# Pre-flight + cleanup.
assert "preflight function defined" contains "$TARGET" "preflight()"
assert "cleanup trap registered" matches "$TARGET" "trap cleanup EXIT INT TERM"

# Flag wiring.
assert "--skip-live flag handled" contains "$TARGET" "--skip-live)"
assert "--scenario= flag handled" contains "$TARGET" "--scenario=*)"
assert "--json flag handled" contains "$TARGET" "--json)"

# HMAC signing + metrics read.
assert "openssl HMAC sign helper present" contains "$TARGET" "openssl dgst -sha256 -hmac"
assert "metrics counter read helper present" contains "$TARGET" "loopzero_github_webhook_deliveries_total"

# Exit code contract.
assert "exit 2 on preflight failure" matches "$TARGET" "exit 2"
assert "exit 0/1 distinguishes pass/fail" matches "$TARGET" "exit 0 \|\| exit 1"

# Required headers in POST.
assert "X-Hub-Signature-256 header in POST" contains "$TARGET" "X-Hub-Signature-256:"
assert "X-GitHub-Event header in POST" contains "$TARGET" "X-GitHub-Event:"
assert "X-GitHub-Delivery header in POST" contains "$TARGET" "X-GitHub-Delivery:"

# EC-9: must validate that invalid-signature path returns 4xx (defense
# against silent-pass).
assert "scenario B asserts 401/403 status" matches "$TARGET" '\"401\".*\"403\"|\"403\".*\"401\"'

# Allowlist cross-check (Rule 12).
assert "LIVE script listed in allowlist" contains "$ALLOWLIST" "dev-public-github-webhook_test.sh"
assert "hermetic script listed in allowlist" contains "$ALLOWLIST" "check-dev-public-github-webhook-syntax_test.sh"

printf '\n[%s] %d/%d PASS\n' "$(basename "$0")" "$((asserts - failures))" "$asserts"
exit $((failures > 0 ? 1 : 0))
