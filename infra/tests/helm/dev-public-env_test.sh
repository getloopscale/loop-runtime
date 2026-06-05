#!/usr/bin/env bash
# T2.1 hermetic test of plan 2026-06-04-dev-public-domain-canonicalization
#
# Validates infra/helmfile/environments/dev-public.yaml has the expected
# canonical shape: dev URLs (-dev.loopzero.example), shared cookie domain
# (loopzero.example), CORS + allowedReturnUrls coverage, OIDC env var names.
#
# Hermetic: no kubectl, no docker, no network. Reads YAML + asserts.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
ENV_FILE="$REPO_ROOT/infra/helmfile/environments/dev-public.yaml"

PASS=0
FAIL=0

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  if [[ "$expected" == "$actual" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n    expected: %s\n    actual:   %s\n' "$desc" "$expected" "$actual"
    FAIL=$((FAIL + 1))
  fi
}

assert_contains() {
  local desc="$1" haystack="$2" needle="$3"
  if printf '%s' "$haystack" | grep -qF -- "$needle"; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS + 1))
  else
    printf '  FAIL: %s\n    needle: %s\n    haystack: %s\n' "$desc" "$needle" "$haystack"
    FAIL=$((FAIL + 1))
  fi
}

[[ -f "$ENV_FILE" ]] || {
  printf 'FAIL: %s does not exist\n' "$ENV_FILE"
  exit 1
}

# Read whole file once
CONTENT=$(<"$ENV_FILE")

printf '== dev-public.yaml structural assertions ==\n'

assert_contains "publicAuthUrl points to auth-dev" "$CONTENT" 'publicAuthUrl: "https://auth-dev.loopzero.example"'
assert_contains "publicCloudUrl points to cloud-dev" "$CONTENT" 'publicCloudUrl: "https://cloud-dev.loopzero.example"'
assert_contains "uiBaseUrl points to app-dev" "$CONTENT" 'uiBaseUrl: "https://app-dev.loopzero.example"'
assert_contains "dashboardOrigin points to cloud-dev" "$CONTENT" 'dashboardOrigin: "https://cloud-dev.loopzero.example"'
assert_contains "cookieDomain is eTLD+1" "$CONTENT" 'cookieDomain: "loopzero.example"'

printf '\n== CORS + allowedReturnUrls cover both -dev hosts ==\n'

assert_contains "corsAllowedOrigins includes app-dev" "$CONTENT" '  - "https://app-dev.loopzero.example"'
assert_contains "corsAllowedOrigins includes cloud-dev" "$CONTENT" '  - "https://cloud-dev.loopzero.example"'
assert_contains "allowedReturnUrls includes app-dev exact" "$CONTENT" '  - "https://app-dev.loopzero.example"'
assert_contains "allowedReturnUrls includes app-dev trailing slash" "$CONTENT" '  - "https://app-dev.loopzero.example/"'
assert_contains "allowedReturnUrls includes cloud-dev exact" "$CONTENT" '  - "https://cloud-dev.loopzero.example"'
assert_contains "allowedReturnUrls includes cloud-dev trailing slash" "$CONTENT" '  - "https://cloud-dev.loopzero.example/"'

printf '\n== NO localhost URLs leak into dev-public ==\n'

if printf '%s' "$CONTENT" | grep -q "localhost:4455\|http://localhost\|127.0.0.1"; then
  printf '  FAIL: dev-public.yaml contains localhost URL(s) — regression risk\n'
  printf '%s\n' "$CONTENT" | grep -n "localhost\|127.0.0.1" | head -5
  FAIL=$((FAIL + 1))
else
  printf '  PASS: dev-public.yaml contains no localhost URLs\n'
  PASS=$((PASS + 1))
fi

printf '\n== NO legacy (non-dev) public URLs leak ==\n'

# Should NOT contain bare `app.loopzero.example` (without `-dev`) — only `app-dev`
LEGACY=$(printf '%s' "$CONTENT" | grep -oE 'https://(auth|app|cloud)\.loopzero\.dev' | head -3 || true)
if [[ -n "$LEGACY" ]]; then
  printf '  FAIL: dev-public.yaml contains legacy *.loopzero.example URL(s):\n%s\n' "$LEGACY"
  FAIL=$((FAIL + 1))
else
  printf '  PASS: dev-public.yaml has no legacy non-dev URLs\n'
  PASS=$((PASS + 1))
fi

printf '\n== D5: OAuth apps reused (no _DEV env var override) ==\n'

# Per D5 of plan 2026-06-04 (operator decision): legacy OAuth apps got
# `auth-dev` callbacks added to the SAME client_id. dev-public.yaml MUST
# NOT contain `_DEV` env var override keys — chart reads default
# LOOPZERO_GITHUB_CLIENT_ID / LOOPZERO_GOOGLE_CLIENT_ID for both envs.
if printf '%s' "$CONTENT" | grep -qE "LOOPZERO_(GITHUB|GOOGLE)_CLIENT_(ID|SECRET)_DEV"; then
  printf '  FAIL: dev-public.yaml references _DEV-suffix env vars (per D5, must reuse legacy):\n'
  printf '%s\n' "$CONTENT" | grep "_DEV" | head -3
  FAIL=$((FAIL + 1))
else
  printf '  PASS: dev-public.yaml does not duplicate OAuth env via _DEV suffix\n'
  PASS=$((PASS + 1))
fi

# Stripe webhook DOES split per-env (D5 caveat): loopzero-stripe-webhook-dev
# Secret name documented.
assert_contains "Stripe webhook split per-env documented" "$CONTENT" 'loopzero-stripe-webhook-dev'

printf '\n== summary ==\n'
printf '  PASS: %d\n' "$PASS"
printf '  FAIL: %d\n' "$FAIL"

[[ "$FAIL" -eq 0 ]] || exit 1
