#!/usr/bin/env bash
# check-webhook-canonical-path_test.sh
#
# T1.3 hermetic guard of plan m1-commit-canonical-30-fixes (v1.2 SHIPPABLE 96.4).
# Fails if any test/seed script references the WRONG webhook path
# `/engine/api/v1/webhooks/github` (which Caddy 301-redirects to app-dev and
# returns 404 there — no handler at /engine/webhooks) instead of the canonical
# `/api/v1/webhooks/github` (handler-direct per Caddyfile lines 142-156).
#
# Hermetic: NO network, NO kubectl. Pure regex over working tree.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
PASS=0; FAIL=0

PATTERN='/engine/api/v1/webhooks/github'

violations=$(grep -rEn "$PATTERN" \
  archive/canonical-tests-v1/scripts/local/ \
  infra/tests/e2e/ \
  infra/scripts/local/ 2>/dev/null || true)

if [[ -z "$violations" ]]; then
  printf '[PASS] webhook URL uses canonical /api/v1/webhooks/github (handler-direct)\n'
  PASS=$((PASS+1))
else
  printf '[FAIL] /engine/api/v1/webhooks/github detected (Caddy 301 trap):\n%s\n' "$violations" >&2
  FAIL=$((FAIL+1))
fi

printf 'summary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
