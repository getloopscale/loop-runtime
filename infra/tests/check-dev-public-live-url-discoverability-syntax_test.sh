#!/usr/bin/env bash
# T5.1 of plan 2026-06-15-code-review-49-findings-remediation (R1-F2 HIGH
# closure — 3 of 4 ORPHAN companions). Hermetic structural test for the
# LIVE bash at infra/tests/e2e/dev-public-live-url-discoverability_test.sh.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-live-url-discoverability_test.sh"
PASS=0
FAIL=0

assert() {
  local label="$1"; shift
  if "$@"; then
    PASS=$((PASS+1))
    echo "✓ $label"
  else
    FAIL=$((FAIL+1))
    echo "✗ $label"
  fi
}

assert "file exists" test -f "$SCRIPT"
assert "bash -n syntax" bash -n "$SCRIPT"
assert "set -euo pipefail strict mode" grep -q '^set -euo pipefail' "$SCRIPT"
assert "log function defined" grep -q "^log() " "$SCRIPT"
assert "pass function defined" grep -q "^pass() " "$SCRIPT"
assert "fail function defined" grep -q "^fail() " "$SCRIPT"
assert "usage function defined" grep -q "^usage() {" "$SCRIPT"
assert "--skip-live flag wired" grep -q -- "--skip-live" "$SCRIPT"
assert "--json flag wired" grep -q -- "--json" "$SCRIPT"
assert "--help flag wired" grep -q -- "--help" "$SCRIPT"
assert "exit code contract documented (0/1/2)" \
  grep -qE 'Exit: 0|exit 0|exit 1|exit 2' "$SCRIPT"
assert "SC_RESULTS array tracking" grep -q 'SC_RESULTS' "$SCRIPT"
assert "live URL discoverability probe references status.url canonical field" \
  grep -qE 'status\.url|status_url|live[_-]url' "$SCRIPT"
assert "allowlist entry present" \
  grep -q "infra/tests/e2e/dev-public-live-url-discoverability_test.sh" \
  .claude/rules/shell-scripts-allowlist.md

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
