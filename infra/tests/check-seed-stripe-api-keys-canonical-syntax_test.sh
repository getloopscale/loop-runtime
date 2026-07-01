#!/usr/bin/env bash
# check-seed-stripe-api-keys-canonical-syntax_test.sh
#
# Hermetic guard for seed-stripe-api-keys-canonical.sh (sibling of
# check-seed-stripe-secret-canonical-syntax_test.sh).
# Per plan stripe-l2-wiring-canonical-fix T2.2.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
SCRIPT="infra/scripts/local/seed-stripe-api-keys-canonical.sh"
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

assert "file exists" test -f "$SCRIPT"
assert "bash -n syntax clean" bash -n "$SCRIPT"
assert "strict mode set -euo pipefail" grep -q '^set -euo pipefail$' "$SCRIPT"
assert "--verify wired" grep -q -- '--verify' "$SCRIPT"
assert "--dry-run wired" grep -q -- '--dry-run' "$SCRIPT"
assert "--help wired" grep -q -- '--help' "$SCRIPT"
# Canonical Secret name + data key
assert "loopzero-stripe-api-keys Secret target" grep -q 'loopzero-stripe-api-keys' "$SCRIPT"
assert "STRIPE_API_KEY data key (canonical env-var-name match)" \
  grep -qE 'from-literal=STRIPE_API_KEY=' "$SCRIPT"
assert "no legacy 'api_key' lowercase data key drift" \
  ! grep -qE 'from-literal=api_key=' "$SCRIPT"
# Mode 0600 enforcement (canonical secret-SoT contract per feedback_secret_canonical_no_hardcode)
assert "mode 0600 enforcement" grep -q '"600"' "$SCRIPT"
# Graceful SKIP semantic (matches sibling) — use fixed-string greps to avoid
# bash `eval` reinterpreting `|` as a shell pipe (assert helper uses eval).
assert "SKIP graceful when creds file absent" grep -q 'not found' "$SCRIPT"
assert "SKIP graceful on placeholder api_key" grep -q 'PLACEHOLDER' "$SCRIPT"
assert "graceful exit 0 path" grep -qE "^[[:space:]]*exit 0$" "$SCRIPT"
# EC-2 absorption (from edge-case review): api_key prefix validation.
# The seed's prefix-guard regex `^(rk|sk)_(test|live)_` lives at line ~72.
# Use a fixed-string substring uniquely identifying that regex (avoid the
# `(...|...)` alt because the `assert` helper uses `eval "$@"` which
# interprets `|` as a shell pipe and `(` as syntax).
assert "api_key prefix guard rk_test_/sk_test_ (EC-2) — line present" \
  grep -q "stripe.api_key field" "$SCRIPT"
assert "size ≤ 120 LoC" test "$(wc -l < "$SCRIPT")" -le 120
assert "no Co-Authored-By" ! grep -q "Co-Authored-By" "$SCRIPT"
assert "allowlist entry present" grep -q 'seed-stripe-api-keys-canonical.sh' "$ALLOWLIST"

# Red Team loopzero-cloud/loopzero#154 (2026-07-01): after materializing the
# Secret, the seed MUST roll out loopzero-cloud so the pod picks up
# STRIPE_API_KEY. Without a restart, boot-order dependency leaves
# billingHandler nil → /v1/billing* returns 404 → upgrade flow dead.
# Sibling seed-broker-canonical.sh establishes the canonical pattern
# ("rollout restart deployment/loopzero-api" post-materialization); this
# guard locks the same discipline for the Stripe seed.
assert "rollout restart loopzero-cloud post-materialization (#154)" \
  grep -q 'rollout restart deployment/loopzero-cloud' "$SCRIPT"
assert "rollout status loopzero-cloud waits Ready (#154)" \
  grep -q 'rollout status deployment/loopzero-cloud' "$SCRIPT"

printf '\nsummary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
