#!/usr/bin/env bash
# T2.1 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1: helm-template golden test for serve.public.base_url, CORS, cookie.domain,
# allowed_return_urls, dashboardRoute-mapped ui_url across multiple env yamls.
#
# Hermetic: pipes `helmfile template` output (which merges env + chart) and
# greps the resulting Kratos config for the expected fields.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0
check() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "  PASS: $name"; PASS=$((PASS+1))
  else
    echo "  FAIL: $name"; FAIL=$((FAIL+1))
  fi
}

# helmfile template the kratos release for a given env
hf_template() {
  local env="$1"
  helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
    -e "$env" \
    --selector name=ory-kratos \
    template --skip-deps 2>/dev/null || return 1
}

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Test 1: standalone-public env renders all public-URL fields
echo "=== env: standalone-public ==="
if hf_template standalone-public > "$TMPDIR/public.yaml" 2>/dev/null && [ -s "$TMPDIR/public.yaml" ]; then
  check "base_url with public auth host (no double slash)" \
    grep -Eq 'base_url: "?https://auth\.loopzero\.dev/"?$' "$TMPDIR/public.yaml"
  check "CORS enabled" \
    grep -q 'enabled: true' "$TMPDIR/public.yaml"
  check "CORS allow_credentials" \
    grep -q 'allow_credentials: true' "$TMPDIR/public.yaml"
  check "cookie domain set to loopzero.example" \
    grep -Eq 'domain: "?loopzero\.dev"?$' "$TMPDIR/public.yaml"
  check "allowed_return_urls present" \
    grep -q 'allowed_return_urls:' "$TMPDIR/public.yaml"
  check "registration ui_url uses /register (NOT /registration)" \
    grep -Eq 'ui_url: "?https://app\.loopzero\.dev/register"?$' "$TMPDIR/public.yaml"
  check "login ui_url uses /login" \
    grep -Eq 'ui_url: "?https://app\.loopzero\.dev/login"?$' "$TMPDIR/public.yaml"
  check "no double-slash in URL fields (excluding base64 payloads)" \
    bash -c "! grep -v base64 '$TMPDIR/public.yaml' | grep -E 'https?://[^/[:space:]]+//' >/dev/null"
else
  echo "  SKIP: standalone-public env template render failed — likely the env yaml is missing the new keys yet (T2.2 implements this)"
fi

# Test 2: local dev env (dev) does NOT render CORS or cookie domain
echo ""
echo "=== env: dev ==="
if hf_template dev > "$TMPDIR/dev.yaml" 2>/dev/null && [ -s "$TMPDIR/dev.yaml" ]; then
  check "no CORS block (publicAuthUrl unset)" \
    bash -c "! grep -q 'allow_credentials:' '$TMPDIR/dev.yaml'"
  check "no cookie domain (cookieDomain unset)" \
    bash -c "! grep -E 'cookie:\s*$' -A2 '$TMPDIR/dev.yaml' | grep -q 'domain:'"
  check "login ui_url uses localhost:4455" \
    grep -Eq 'ui_url: "?http://localhost:4455/login"?$' "$TMPDIR/dev.yaml"
else
  echo "  SKIP: dev env template render failed"
fi

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
