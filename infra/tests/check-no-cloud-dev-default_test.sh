#!/usr/bin/env bash
# check-no-cloud-dev-default_test.sh
#
# T1.1 hermetic guard of plan m1-commit-canonical-30-fixes (v1.2 SHIPPABLE 96.4).
# Fails if any test/seed script in archive/canonical-tests-v1/scripts/local/,
# infra/tests/e2e/, or infra/scripts/local/ declares a default env-var value
# pointing at `cloud-dev.loopzero.example` (Caddy 301 redirect trap post commit
# ee77b8da 2026-06-22).
#
# Exception (EC-1 absorption): paths terminated in /api/v1/webhooks/github are
# handler-direct (Caddyfile lines 142-156 — webhook handler is the single redir
# exception, goes straight to loopzero-api). The stage-2 grep filters those.
#
# Hermetic: NO network, NO kubectl. Pure regex over working tree.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
PASS=0; FAIL=0

# Stage 1 (broad): ${VAR:-https://cloud-dev.loopzero.example}
PATTERN='\$\{[A-Z_]+:-https://cloud-dev\.loopzero\.dev'

# Stage 2 (EC-1 absorption): exclude /api/v1/webhooks/github paths.
violations=$(grep -rEn "$PATTERN" \
  archive/canonical-tests-v1/scripts/local/ \
  infra/tests/e2e/ \
  infra/scripts/local/ 2>/dev/null \
  | grep -vE '/api/v1/webhooks/github' || true)

if [[ -z "$violations" ]]; then
  printf '[PASS] no cloud-dev defaults in test/seed scripts (webhook handler exception allowed)\n'
  PASS=$((PASS+1))
else
  printf '[FAIL] cloud-dev default detected (Caddy redirect 301 trap):\n%s\n' "$violations" >&2
  FAIL=$((FAIL+1))
fi

printf 'summary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
