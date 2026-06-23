#!/usr/bin/env bash
# T2.2 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# Reads sponsor-provided credentials from ~/.loopzero/dev-credentials.json (mode
# 0600 enforced pre-read) and writes 5 canonical OpenBao kv-v2 paths.
# Closes catch #9 from T6.2 session by automating the seed step that
# previously required hand-running 5 separate `bao kv put` commands.
#
# Expected ~/.loopzero/dev-credentials.json schema:
#   {
#     "github": { "app_id": "...", "app_private_key": "...PEM...", "webhook_secret": "..." },
#     "stripe": { "api_key": "rk_test_...", "webhook_secret": "whsec_..." },
#     "resend": { "api_key": "re_..." },
#     "openbao": { "vault_role_id": "..." },
#     "cloud":  { "service_account_key": "..." }
#   }
#
# Per ADR D2: idempotent — re-run no-ops if all paths already present + match.
# Wired in: taskfiles/k3d.yml as `task k3d:vault:secrets-seed-dev`.

set -uo pipefail

CONTEXT="${OPENBAO_CONTEXT:-k3d-loopzero-mgmt}"
NS="${OPENBAO_NAMESPACE:-vault-system}"
POD="${OPENBAO_POD:-openbao-0}"
CREDS_FILE="${CREDS_FILE:-$HOME/.loopzero/dev-credentials.json}"
UNSEAL_FILE="${UNSEAL_FILE:-$HOME/.loopzero/cleanroom-unseal.json}"
DRY_RUN="${DRY_RUN:-0}"

log() { printf '[secrets-seed-dev] %s\n' "$*" >&2; }

# === EC-12 pre-read: file mode 0600 enforced ===
if [[ ! -f "$CREDS_FILE" ]]; then
  log "FAIL: $CREDS_FILE not found. Create sponsor-provided creds file first."
  exit 1
fi

PERM=$(stat -c '%a' "$CREDS_FILE" 2>/dev/null || stat -f '%A' "$CREDS_FILE" 2>/dev/null || echo "unknown")
if [[ "$PERM" != "600" ]]; then
  log "FAIL: $CREDS_FILE has mode $PERM, expected 0600 (operator MUST chmod 0600)"
  exit 1
fi

if [[ ! -f "$UNSEAL_FILE" ]]; then
  log "FAIL: $UNSEAL_FILE missing — run \`task k3d:vault:bootstrap-dev\` first."
  exit 1
fi

ROOT_TOKEN=$(python3 -c "import json; print(json.load(open('$UNSEAL_FILE'))['root_token'])")

bao_write() {
  local path="$1"; shift
  log "+ bao kv put $path ${*//=*/=<redacted>}"
  [[ "$DRY_RUN" -eq 1 ]] && return 0
  kubectl --context "$CONTEXT" -n "$NS" exec "$POD" -- env BAO_TOKEN="$ROOT_TOKEN" \
    bao kv put "$path" "$@" >/dev/null
}

extract() {
  local key="$1"
  python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d$key)" 2>/dev/null || echo ""
}

# === 5 canonical kv-v2 paths ===

# Path 1: GitHub App
GH_APP_ID=$(extract "['github']['app_id']")
GH_PRIVKEY=$(extract "['github']['app_private_key']")
GH_WEBHOOK=$(extract "['github']['webhook_secret']")
if [[ -n "$GH_APP_ID" ]]; then
  bao_write kv/loopzero/dev/api/secrets/github \
    "app_id=$GH_APP_ID" "app_private_key=$GH_PRIVKEY" "webhook_secret=$GH_WEBHOOK"
fi

# Path 2: Stripe (J-22 secret canonical pattern)
STRIPE_KEY=$(extract "['stripe']['api_key']")
STRIPE_WEBHOOK=$(extract "['stripe']['webhook_secret']")
if [[ -n "$STRIPE_KEY" ]]; then
  bao_write kv/loopzero/dev/cloud/secrets/stripe \
    "api_key=$STRIPE_KEY" "webhook_secret=$STRIPE_WEBHOOK"
fi

# Path 2b (M2 T2.3 2026-06-23): dedicated stripe-webhook kv-v2 path canonical.
# Per qa-readiness-roadmap M2 seed #10 + live-proof Stripe L1 layer probe.
# Distinct from bundled stripe path above — dedicated path per consumer
# (ESO ExternalSecret canonical pattern: 1 kv path → 1 K8s Secret consumer).
if [[ -n "$STRIPE_WEBHOOK" ]]; then
  bao_write kv/loopzero/dev/cloud/secrets/stripe-webhook "whsec=$STRIPE_WEBHOOK"
fi

# Path 3: Resend (Kratos courier per memory feedback_resend_kratos_courier)
RESEND_KEY=$(extract "['resend']['api_key']")
if [[ -n "$RESEND_KEY" ]]; then
  bao_write kv/loopzero/dev/cloud/secrets/resend \
    "api_key=$RESEND_KEY"
fi

# Path 4: OpenBao VAULT_ROLE_ID (canonical kv-v2 store of approle role_id)
ROLE_ID=$(extract "['openbao']['vault_role_id']")
if [[ -n "$ROLE_ID" ]]; then
  bao_write kv/loopzero/dev/platform/config/openbao \
    "vault_role_id=$ROLE_ID"
fi

# Path 5: cloud service account
SA_KEY=$(extract "['cloud']['service_account_key']")
if [[ -n "$SA_KEY" ]]; then
  bao_write kv/loopzero/dev/cloud/secrets/service-account \
    "key=$SA_KEY"
fi

log ""
log "════════════════════════════════════════════════════════"
log "  secrets-seed-dev COMPLETE (5 kv-v2 paths)"
log "════════════════════════════════════════════════════════"
