#!/usr/bin/env bash
# Hermetic test — install-config-contract Slice 1 T3.1.
# Asserts SECRETS.md documents every Secret-typed env var the cloud-API reads, and that
# values.example.yaml carries no real secret value (only sentinels) + validates against the
# hardened schema. NO kubectl, NO network. Pure grep + helm template.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART="$ROOT/charts/loopzero-cloud"
SECRETS="$CHART/SECRETS.md"
EXAMPLE="$CHART/values.example.yaml"
fail=0; pass=0
ok()  { pass=$((pass+1)); }
no()  { fail=$((fail+1)); echo "  FAIL: $1"; }

# A1 — every Secret-typed env var (from config.go) is documented in SECRETS.md
for k in STRIPE_API_KEY STRIPE_WEBHOOK_SECRET SESSION_TOKEN_SECRET LOOPZERO_SERVICE_AUTH_SECRET THEMORY_API_KEY; do
  grep -q "\`$k\`" "$SECRETS" && ok || no "SECRETS.md missing key $k"
done

# A2 — SECRETS.md documents generation for the generated secrets
grep -q 'openssl rand -hex 32' "$SECRETS" && ok || no "SECRETS.md missing openssl generation command"

# A3 — values.example.yaml carries NO real secret value (sentinels only; no sk_/whsec_/hex blobs)
if grep -qiE 'sk_live_[A-Za-z0-9]|whsec_[A-Za-z0-9]|[a-f0-9]{32,}' "$EXAMPLE"; then
  no "values.example.yaml appears to contain a real secret value"
else ok; fi

# A4 — values.example.yaml uses the existingSecret floor (references, not inline secrets)
grep -q '^existingSecret:' "$EXAMPLE" && ok || no "values.example.yaml missing existingSecret"

# A5 — values.example.yaml validates against the hardened schema (helm available)
if command -v helm >/dev/null 2>&1; then
  helm template "$CHART" -f "$EXAMPLE" >/dev/null 2>&1 && ok || no "values.example.yaml does not render against values.schema.json"
else
  echo "  SKIP A5 (helm not installed)"
fi

echo "loopzero-cloud-secret-manifest: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
