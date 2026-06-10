#!/usr/bin/env bash
# T2.2 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1: assert per-env yaml has the right keys (or correctly omits them).
# Hermetic: parses YAML directly via python+yaml.
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

ENV_DIR=infra/helmfile/environments

key_set() {
  local file="$1" key="$2"
  python3 -c "
import yaml
d = yaml.safe_load(open('$file')) or {}
v = d.get('$key')
if v is None or v == '' or (isinstance(v, list) and len(v) == 0):
    exit(1)
exit(0)
"
}

key_absent() {
  local file="$1" key="$2"
  python3 -c "
import yaml
d = yaml.safe_load(open('$file')) or {}
exit(0 if '$key' not in d else 1)
"
}

echo "=== prod.yaml ==="
for k in publicAuthUrl publicCloudUrl cookieDomain corsAllowedOrigins allowedReturnUrls dashboardRoute; do
  check "$k present" key_set $ENV_DIR/prod.yaml $k
done

echo ""
echo "=== drill.yaml ==="
for k in publicAuthUrl publicCloudUrl cookieDomain corsAllowedOrigins allowedReturnUrls dashboardRoute; do
  check "$k present" key_set $ENV_DIR/drill.yaml $k
done

echo ""
echo "=== standalone-public.yaml ==="
check "exists" test -f $ENV_DIR/standalone-public.yaml
for k in publicAuthUrl publicCloudUrl cookieDomain corsAllowedOrigins allowedReturnUrls dashboardRoute; do
  check "$k present" key_set $ENV_DIR/standalone-public.yaml $k
done

# v1.1 EC-07 — local envs MUST OMIT publicAuthUrl (Go-template default
# semantics: empty string is NOT zero-value). Setting publicAuthUrl: ""
# would render `serve.public.base_url: "/"` (broken).
echo ""
echo "=== dev.yaml (v1.1 EC-07: keys must be ABSENT, not empty) ==="
for k in publicAuthUrl cookieDomain corsAllowedOrigins allowedReturnUrls; do
  check "$k ABSENT in dev.yaml" key_absent $ENV_DIR/dev.yaml $k
done

echo ""
echo "=== standalone-multi.yaml (v1.1 EC-07) ==="
for k in publicAuthUrl cookieDomain corsAllowedOrigins allowedReturnUrls; do
  check "$k ABSENT in standalone-multi.yaml" key_absent $ENV_DIR/standalone-multi.yaml $k
done

# v1.1 EC-S1: validate that allowedReturnUrls in prod includes all 4 expected
# patterns (with + without trailing slash for app + cloud).
echo ""
echo "=== prod.yaml allowedReturnUrls coverage ==="
check "prod includes both app variants" python3 -c "
import yaml
urls = yaml.safe_load(open('$ENV_DIR/prod.yaml'))['allowedReturnUrls']
expected = {'https://app.loopzero.example', 'https://app.loopzero.example/'}
exit(0 if expected.issubset(set(urls)) else 1)
"
check "prod includes both cloud variants" python3 -c "
import yaml
urls = yaml.safe_load(open('$ENV_DIR/prod.yaml'))['allowedReturnUrls']
expected = {'https://cloud.loopzero.example', 'https://cloud.loopzero.example/'}
exit(0 if expected.issubset(set(urls)) else 1)
"

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
