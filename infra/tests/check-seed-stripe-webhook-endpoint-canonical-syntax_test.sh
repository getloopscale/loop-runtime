#!/usr/bin/env bash
# check-seed-stripe-webhook-endpoint-canonical-syntax_test.sh
#
# Hermetic guard for seed-stripe-webhook-endpoint-canonical.sh (Red Team
# loopzero-cloud#156 closure). Per feedback_issue_fix_requires_failing_e2e_test:
# every issue closure requires hermetic + LIVE + dogfood triplet.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
SCRIPT="infra/scripts/local/seed-stripe-webhook-endpoint-canonical.sh"
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

# --- Structural invariants ---
assert "file exists" test -f "$SCRIPT"
assert "bash -n syntax clean" bash -n "$SCRIPT"
assert "strict mode set -euo pipefail" grep -q '^set -euo pipefail$' "$SCRIPT"
assert "3 flags wired: --verify" grep -q -- '--verify' "$SCRIPT"
assert "3 flags wired: --dry-run" grep -q -- '--dry-run' "$SCRIPT"
assert "3 flags wired: --help" grep -q -- '--help' "$SCRIPT"

# --- Canonical URL + Stripe API endpoints ---
assert "canonical webhook URL default app-dev.loopzero.example" \
  grep -q 'app-dev.loopzero.example/v1/webhooks/stripe' "$SCRIPT"
assert "Stripe API webhook_endpoints endpoint targeted" \
  grep -q 'api.stripe.com/v1/webhook_endpoints' "$SCRIPT"
assert "GET /v1/webhook_endpoints query for idempotency" \
  grep -Fq 'webhook_endpoints?limit' "$SCRIPT"
assert "POST /v1/webhook_endpoints for create" \
  grep -Fq 'POST' "$SCRIPT"

# --- 5 canonical enabled_events (matches webhook.go isHandledEvent + payment_failed) ---
# NOTE: -F fixed-string so [] doesn't become regex character-class in eval-piped assert.
assert "enabled_events: checkout.session.completed" \
  grep -Fq 'enabled_events[]=checkout.session.completed' "$SCRIPT"
assert "enabled_events: customer.subscription.created" \
  grep -Fq 'enabled_events[]=customer.subscription.created' "$SCRIPT"
assert "enabled_events: customer.subscription.updated" \
  grep -Fq 'enabled_events[]=customer.subscription.updated' "$SCRIPT"
assert "enabled_events: customer.subscription.deleted" \
  grep -Fq 'enabled_events[]=customer.subscription.deleted' "$SCRIPT"
assert "enabled_events: invoice.payment_failed" \
  grep -Fq 'enabled_events[]=invoice.payment_failed' "$SCRIPT"

# --- Real vs placeholder whsec discrimination ---
# Real Stripe test-mode whsec starts with `whsec_` (no `test_` subprefix);
# placeholder starts with `whsec_dev_`.
assert "guard against placeholder whsec_dev_ prefix" \
  grep -Fq 'whsec_dev_' "$SCRIPT"
assert "canonical whsec_ prefix positive guard" \
  grep -Fq '^whsec_' "$SCRIPT"

# --- Secret SoT + delegation ---
assert "mode 0600 enforcement on dev-credentials.json (secret-SoT contract)" \
  grep -q '"600"' "$SCRIPT"
assert "delegates K8s Secret materialization to sibling seed-stripe-secret" \
  grep -q 'seed-stripe-secret-canonical.sh' "$SCRIPT"

# --- Rollout restart (matches #154 pattern — pod boot re-reads envFrom) ---
assert "rollout restart deployment/loopzero-cloud post-materialization" \
  grep -q 'rollout restart deployment/loopzero-cloud' "$SCRIPT"
assert "rollout status loopzero-cloud waits Ready" \
  grep -q 'rollout status deployment/loopzero-cloud' "$SCRIPT"

# --- SKIP graceful (matches sibling seed graceful semantic) ---
assert "SKIP graceful when creds file absent" grep -q 'not found' "$SCRIPT"
assert "SKIP graceful on placeholder api_key" grep -q 'placeholder' "$SCRIPT"
assert "graceful exit 0 path present" grep -qE "^[[:space:]]*exit 0$" "$SCRIPT"

# --- Governance ---
assert "size ≤ 200 LoC (KISS)" test "$(wc -l < "$SCRIPT")" -le 200
assert "no Co-Authored-By trailer" ! grep -q "Co-Authored-By" "$SCRIPT"
assert "no --no-verify" ! grep -q "no-verify" "$SCRIPT"
assert "allowlist entry present" grep -q 'seed-stripe-webhook-endpoint-canonical.sh' "$ALLOWLIST"

# --- Red Team #156 audit trail ---
assert "Red Team loopzero-cloud#156 cited in header" \
  grep -q 'loopzero-cloud#156' "$SCRIPT"
assert "5 Whys root cause documented" \
  grep -q '5 Whys' "$SCRIPT"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
