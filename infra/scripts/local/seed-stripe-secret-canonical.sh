#!/usr/bin/env bash
# seed-stripe-secret-canonical.sh
#
# T1.4 of plan m2-promote-11-canonical-seeds (v1.1 SHIPPABLE 95.2).
# Canonical loopzero-stripe-webhook K8s Secret materialization from
# ~/.loopzero/dev-credentials.json `stripe.webhook_secret`.
#
# Used by loopzero-cloud Stripe webhook handler in loopzero-system. Distinct from
# OpenBao kv path `kv/loopzero/dev/cloud/secrets/stripe-webhook` (canonical SoT
# external — populated by secrets-seed-dev.sh) — this script materializes the
# K8s Secret consumed by the runtime workload until ESO bridges the kv path.
#
# Graceful SKIP semantic per ADR D1 drawback + sponsor-blocked M5:
# - When ~/.loopzero/dev-credentials.json absent → exit 0 with SKIP log
# - When stripe.webhook_secret missing OR equals "whsec_PLACEHOLDER" → exit 0 with SKIP
# - When mode != 0600 → FAIL (canonical secret-SoT mode contract)
# - When real whsec present → materialize Secret (idempotent via dry-run|apply)
#
# Flags:
#   --verify   assert whsec key present in Secret post-apply
#   --dry-run  print intended actions without mutation
#   --help|-h  print this header
#
# Env vars: KUBE_CONTEXT (default k3d-loopzero-mgmt), LOOPZERO_DEV_CREDENTIALS (default ~/.loopzero/dev-credentials.json).
set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
CREDS_FILE="${LOOPZERO_DEV_CREDENTIALS:-$HOME/.loopzero/dev-credentials.json}"
VERIFY=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-stripe-secret] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

# SKIP graceful: creds file absent (operator hasn't run sponsor-provided setup)
if [[ ! -f "$CREDS_FILE" ]]; then
  log "SKIP — $CREDS_FILE not found (sponsor-blocked M5)"
  exit 0
fi

# FAIL hard: mode != 0600 (canonical secret-SoT contract per feedback_secret_canonical_no_hardcode)
MODE=$(stat -c '%a' "$CREDS_FILE" 2>/dev/null || stat -f '%Lp' "$CREDS_FILE" 2>/dev/null)
if [[ "$MODE" != "600" ]]; then
  log "FAIL — $CREDS_FILE mode $MODE (canonical 0600 required for secret SoT)"
  exit 1
fi

# Extract whsec via python3 (jq-free)
WHSEC=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('webhook_secret',''))" 2>/dev/null || echo "")
if [[ -z "$WHSEC" || "$WHSEC" == "whsec_PLACEHOLDER" ]]; then
  log "SKIP — stripe.webhook_secret placeholder/ausente (sponsor-blocked M5)"
  exit 0
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY: kubectl create secret loopzero-stripe-webhook --from-literal=STRIPE_WEBHOOK_SECRET=<redacted>"
  exit 0
fi

# Materialize Secret via dry-run | apply (idempotent — replaces stale Secret).
# Secret data key MUST be STRIPE_WEBHOOK_SECRET (canonical env-var-name match):
# envFrom secretRef exposes Secret keys as env var names verbatim, and
# cloud/internal/config/config.go reads `envOr("STRIPE_WEBHOOK_SECRET", "")`.
# (stripe-l2-wiring-canonical-fix T1.1 — renamed from legacy `whsec` 2026-06-24.)
kubectl --context "$MGMT_CONTEXT" -n loopzero-system create secret generic loopzero-stripe-webhook \
  --from-literal=STRIPE_WEBHOOK_SECRET="$WHSEC" \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CONTEXT" apply -f -

if [[ "$VERIFY" == "1" ]]; then
  log "verifying STRIPE_WEBHOOK_SECRET key in Secret..."
  if kubectl --context "$MGMT_CONTEXT" -n loopzero-system get secret loopzero-stripe-webhook -o jsonpath='{.data.STRIPE_WEBHOOK_SECRET}' 2>/dev/null | grep -q .; then
    log "PASS — secret materialized"
  else
    log "FAIL — STRIPE_WEBHOOK_SECRET key missing in loopzero-stripe-webhook Secret"
    exit 1
  fi
fi
