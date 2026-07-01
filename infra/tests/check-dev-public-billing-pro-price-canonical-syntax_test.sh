#!/usr/bin/env bash
# Hermetic guard for dev-public-billing-pro-price-canonical_test.sh
# (Red Team loopzero-cloud#157 closure — Pro price alignment).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
LIVE="infra/tests/e2e/dev-public-billing-pro-price-canonical_test.sh"
ALLOWLIST=".claude/rules/shell-scripts-allowlist.md"
PASS=0; FAIL=0

assert() {
  local desc="$1"; shift
  if eval "$@" >/dev/null 2>&1; then
    printf '[PASS] %s\n' "$desc"; PASS=$((PASS+1))
  else
    printf '[FAIL] %s\n' "$desc" >&2; FAIL=$((FAIL+1))
  fi
}

assert "file exists" test -f "$LIVE"
assert "bash -n syntax clean" bash -n "$LIVE"
assert "strict mode set -euo pipefail" grep -q '^set -euo pipefail$' "$LIVE"
assert "#157 cited in header" grep -q 'loopzero-cloud#157' "$LIVE"
assert "OPERATOR HARD STOP documented" grep -q 'OPERATOR HARD STOP' "$LIVE"
assert "cleanroom-up-live-test-mandate cited" grep -q 'cleanroom-up-live-test-mandate' "$LIVE"
assert "feedback_issue_fix_requires_failing_e2e_test cited" \
  grep -q 'feedback_issue_fix_requires_failing_e2e_test_2026_06_25' "$LIVE"
assert "MEET-2026-05-12 D4=B business value cited" grep -q 'MEET-2026-05-12' "$LIVE"

assert "4 flags: --skip-live" grep -q -- '--skip-live' "$LIVE"
assert "4 flags: --scenario=" grep -q -- '--scenario=' "$LIVE"
assert "4 flags: --json" grep -q -- '--json' "$LIVE"
assert "4 flags: --help" grep -q -- '--help' "$LIVE"

assert "EXPECTED_CENTS default 2900 (US\$29)" grep -q 'EXPECTED_CENTS.*2900' "$LIVE"
assert "Scenario A tests Stripe price unit_amount" grep -q 'unit_amount' "$LIVE"
assert "Scenario B tests /v1/billing/checkout amount_total" grep -q 'amount_total' "$LIVE"
assert "Scenario B posts plan=pro" grep -q '"plan":"pro"' "$LIVE"
assert "queries Stripe API /v1/prices" grep -q 'api.stripe.com/v1/prices' "$LIVE"
assert "queries Stripe API /v1/checkout/sessions" grep -q 'api.stripe.com/v1/checkout/sessions' "$LIVE"

assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$LIVE"
assert "no --no-verify" ! grep -q "no-verify" "$LIVE"
assert "allowlist entry LIVE" grep -q 'dev-public-billing-pro-price-canonical_test.sh' "$ALLOWLIST"
assert "allowlist entry hermetic (self)" grep -q 'check-dev-public-billing-pro-price-canonical-syntax_test.sh' "$ALLOWLIST"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
