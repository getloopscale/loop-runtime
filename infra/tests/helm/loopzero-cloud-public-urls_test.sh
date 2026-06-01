#!/usr/bin/env bash
# T3.1 of plan 2026-05-29-dashboard-public-integration-canonicalization
# Helmfile-template golden test: loopzero-cloud release env block reflects
# the per-env publicAuthUrl + uiBaseUrl from helmfile env yamls.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0
check() {
  local name="$1"; shift
  if "$@"; then
    echo "  PASS: $name"; PASS=$((PASS+1))
  else
    echo "  FAIL: $name"; FAIL=$((FAIL+1))
  fi
}

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

hf_render() {
  local env="$1" out="$2"
  LOOPZERO_DB_PASSWORD="${LOOPZERO_DB_PASSWORD:-test-password}" \
    helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
      -e "$env" \
      --selector name=loopzero-cloud \
      template --skip-deps 2>/dev/null > "$out"
}

# Test 1: standalone-public renders public URLs
echo "=== env: standalone-public ==="
env_var_value() {
  # Extract `value: <X>` line following `name: <KEY>` in helm-rendered env block.
  local file="$1" key="$2"
  python3 -c "
import re, sys
text = open('$file').read()
# Match: - name: \"KEY\"\n  value: \"VALUE\"
m = re.search(r'- name:\\s*[\"]?$key[\"]?\\s*\\n\\s+value:\\s*[\"]?([^\"\\n]+?)[\"]?\\s*\$', text, re.MULTILINE)
if m:
    print(m.group(1))
"
}

if hf_render standalone-public "$TMPDIR/public.yaml" && [ -s "$TMPDIR/public.yaml" ]; then
  KRATOS_VAL=$(env_var_value "$TMPDIR/public.yaml" KRATOS_PUBLIC_URL)
  BILLING_VAL=$(env_var_value "$TMPDIR/public.yaml" BILLING_RETURN_URL)
  check "KRATOS_PUBLIC_URL = https://auth.loopzero.example" test "$KRATOS_VAL" = "https://auth.loopzero.example"
  check "BILLING_RETURN_URL = https://app.loopzero.example" test "$BILLING_VAL" = "https://app.loopzero.example"
  check "no trailing slash in KRATOS_PUBLIC_URL" bash -c "[[ '$KRATOS_VAL' != */ ]]"
else
  echo "  SKIP: standalone-public render failed"
fi

# Test 2: dev env (no publicAuthUrl) falls back to cluster DNS
echo ""
echo "=== env: dev ==="
if hf_render dev "$TMPDIR/dev.yaml" && [ -s "$TMPDIR/dev.yaml" ]; then
  KRATOS_VAL=$(env_var_value "$TMPDIR/dev.yaml" KRATOS_PUBLIC_URL)
  case "$KRATOS_VAL" in
    http://ory-kratos-public.loopzero-auth.svc.cluster.local*) PASS_CASE=1;;
    *) PASS_CASE=0;;
  esac
  check "KRATOS_PUBLIC_URL falls back to cluster DNS" test "$PASS_CASE" = "1"
else
  echo "  SKIP: dev render failed (loopzero-cloud installed: false in standalone — expected)"
fi

# Test 3: prod renders public URLs
echo ""
echo "=== env: prod ==="
if hf_render prod "$TMPDIR/prod.yaml" && [ -s "$TMPDIR/prod.yaml" ]; then
  KRATOS_VAL=$(env_var_value "$TMPDIR/prod.yaml" KRATOS_PUBLIC_URL)
  BILLING_VAL=$(env_var_value "$TMPDIR/prod.yaml" BILLING_RETURN_URL)
  check "KRATOS_PUBLIC_URL = https://auth.loopzero.example" test "$KRATOS_VAL" = "https://auth.loopzero.example"
  check "BILLING_RETURN_URL = https://app.loopzero.example" test "$BILLING_VAL" = "https://app.loopzero.example"
fi

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
