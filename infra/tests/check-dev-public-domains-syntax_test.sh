#!/usr/bin/env bash
# J-11 Domains plan v1.1 T4.3 — hermetic structural test for the LIVE bash
# test at infra/tests/e2e/dev-public-domains_test.sh.

set -euo pipefail

SCRIPT="infra/tests/e2e/dev-public-domains_test.sh"
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
assert "scenario A function defined" grep -q "^scenario_a_add_returns_nameservers" "$SCRIPT"
assert "scenario B function defined" grep -q "^scenario_b_polling_reaches_active" "$SCRIPT"
assert "scenario C function defined (happy path)" grep -q "^scenario_c_reverify_happy_path" "$SCRIPT"
assert "scenario D function defined (rate-limit ADR D7)" grep -q "^scenario_d_reverify_rate_limit_enforced" "$SCRIPT"
assert "FAANG canonical: ONE invariant per scenario (no 200 OR 429 bundle)" \
  bash -c "! grep -qE 'expected 200 \(\|expected 200 OR 429' '$SCRIPT'"
assert "preflight function defined" grep -q "^preflight" "$SCRIPT"
assert "--skip-live flag wired" grep -q -- "--skip-live" "$SCRIPT"
assert "--json flag wired" grep -q -- "--json" "$SCRIPT"
assert "--scenario= flag wired" grep -q -- "--scenario=" "$SCRIPT"
assert "operator HARD STOP documented" grep -q "OPERATOR HARD STOP" "$SCRIPT"
assert "ADR D5 referenced" grep -q "ADR D5" "$SCRIPT"
assert "ADR D6 EC-2 (backend already wired) referenced" grep -q "ADR D6 EC-2" "$SCRIPT"
assert "ADR D7 (rate limit + debounce) referenced" grep -q "ADR D7" "$SCRIPT"
assert "cleanroom-up-live-test-mandate cited" grep -q "cleanroom-up-live-test-mandate" "$SCRIPT"
assert "exit code reflects FAIL count" grep -q 'exit \$(( FAIL_COUNT > 0' "$SCRIPT"
assert "JWT seed env var documented" grep -q "TOKEN_FILE" "$SCRIPT"
assert "unique-domain-per-run guard (avoid 409 already-exists)" grep -q "PROBE_DOMAIN.*date" "$SCRIPT"
assert "Scenario C asserts ONLY 200 (no 200 OR 429 bundle in single arm)" \
  grep -q 'POST /verify expected 200 (happy path)' "$SCRIPT"
assert "Scenario D asserts ONLY 429 (canonical rate-limit invariant, ADR D7)" \
  grep -q '2nd POST /verify expected 429' "$SCRIPT"
assert "allowlist entry present" \
  grep -q "infra/tests/e2e/dev-public-domains_test.sh" .claude/rules/shell-scripts-allowlist.md

echo
echo "=== summary: $PASS pass / $FAIL fail ==="
exit $(( FAIL > 0 ? 1 : 0 ))
