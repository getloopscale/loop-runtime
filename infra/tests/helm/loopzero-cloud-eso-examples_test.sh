#!/usr/bin/env bash
# Hermetic test — install-config-contract Slice 1 T3.2.
# Asserts each shipped ESO example (OpenBao/AWS SM/GCP SM) is valid YAML, declares a
# ClusterSecretStore + ExternalSecret targeting the chart's existingSecret convention,
# covers all 5 secret keys, and uses the right provider. NO kubectl, NO network.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
EX="$ROOT/charts/loopzero-cloud/examples"
fail=0; pass=0
ok() { pass=$((pass+1)); }
no() { fail=$((fail+1)); echo "  FAIL: $1"; }

declare -A PROVIDER=( [eso-openbao.yaml]=vault [eso-aws-sm.yaml]=aws [eso-gcp-sm.yaml]=gcpsm )

for f in eso-openbao.yaml eso-aws-sm.yaml eso-gcp-sm.yaml; do
  p="$EX/$f"
  [ -f "$p" ] || { no "$f missing"; continue; }
  # valid YAML (multi-doc)
  python3 -c "import sys,yaml;list(yaml.safe_load_all(open('$p')))" 2>/dev/null && ok || no "$f invalid YAML"
  grep -q 'kind: ClusterSecretStore' "$p" && ok || no "$f missing ClusterSecretStore"
  grep -q 'kind: ExternalSecret' "$p"     && ok || no "$f missing ExternalSecret"
  # target.name must be the existingSecret convention
  grep -qE 'name:\s*loopzero-cloud-secrets' "$p" && ok || no "$f target.name != loopzero-cloud-secrets"
  # provider correct
  grep -qE "^\s*${PROVIDER[$f]}:" "$p" && ok || no "$f missing provider ${PROVIDER[$f]}"
  # all 5 secret keys mapped
  for k in STRIPE_API_KEY STRIPE_WEBHOOK_SECRET SESSION_TOKEN_SECRET LOOPZERO_SERVICE_AUTH_SECRET THEMORY_API_KEY; do
    grep -q "secretKey: $k" "$p" && ok || no "$f missing secretKey $k"
  done
done

echo "loopzero-cloud-eso-examples: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
