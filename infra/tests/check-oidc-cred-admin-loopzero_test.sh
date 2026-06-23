#!/usr/bin/env bash
# check-oidc-cred-admin-loopzero_test.sh
#
# T1.4 hermetic guard of plan m1-commit-canonical-30-fixes (v1.2 SHIPPABLE 96.4).
# Fails if any test/seed script references the fabricated credential
# `paulo@loopzero.test` (does NOT exist in dev-public; Kratos returns 401) instead
# of the canonical `admin@loopzero.example` / `loopzero-dev-password`
# (LIVE-validated 2026-06-16; sponsor verbatim "SEMPRE vamos usar nos testes";
# memory project_canonical_dev_user_credentials).
#
# Hermetic: NO network, NO kubectl. Pure regex over working tree.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
PASS=0; FAIL=0

# Non-canonical credentials that MUST NOT appear in test/seed scripts.
PATTERN='paulo@loopzero\.test|LoopZeroDev2026!'

violations=$(grep -rEn "$PATTERN" \
  archive/canonical-tests-v1/scripts/local/ \
  infra/tests/e2e/ \
  infra/scripts/local/ 2>/dev/null || true)

if [[ -z "$violations" ]]; then
  printf '[PASS] no fabricated paulo@loopzero.test credentials (canonical: admin@loopzero.example)\n'
  PASS=$((PASS+1))
else
  printf '[FAIL] fabricated credentials detected (canonical: admin@loopzero.example/loopzero-dev-password):\n%s\n' "$violations" >&2
  FAIL=$((FAIL+1))
fi

printf 'summary: %d pass / %d fail\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
