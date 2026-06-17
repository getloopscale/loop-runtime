#!/usr/bin/env bash
# T5.1 of plan 2026-06-15-code-review-49-findings-remediation (R1-F2 HIGH
# closure — 2 of 4 ORPHAN companions). Hermetic structural test for the
# LIVE bash at infra/tests/e2e/dev-public-oidc-redirect_test.sh.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-oidc-redirect_test.sh"
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
assert "log function defined" grep -q "^log() {" "$SCRIPT"
assert "assert function defined" grep -q "^assert() {" "$SCRIPT"
assert "test_provider function defined" grep -q "^test_provider() {" "$SCRIPT"
assert "OIDC redirect probe references provider name (google or github)" \
  grep -qiE 'google|github|oidc' "$SCRIPT"
assert "Kratos browser flow path referenced" \
  grep -qE '/self-service/login/browser|/self-service/methods/oidc/callback' "$SCRIPT"
assert "allowlist entry present" \
  grep -q "infra/tests/e2e/dev-public-oidc-redirect_test.sh" \
  .claude/rules/shell-scripts-allowlist.md

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
