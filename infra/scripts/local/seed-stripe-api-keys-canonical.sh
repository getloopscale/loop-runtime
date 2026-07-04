#!/usr/bin/env bash
# seed-stripe-api-keys-canonical.sh
#
# Plan stripe-l2-wiring-canonical-fix T2.1.
# Canonical loopzero-stripe-api-keys K8s Secret materialization from
# ~/.loopzero/dev-credentials.json `stripe.api_key`.
#
# Sibling of seed-stripe-secret-canonical.sh — separate Secret per ADR D1
# (rotation cadence isolation: webhook secret vs API key rotate on different
# triggers per Stripe's runbook). Both Secrets are consumed by loopzero-cloud
# via envFrom secretRef in infra/helm/loopzero-cloud/templates/deployment.yaml.
#
# Graceful SKIP semantic (matches sibling):
# - ~/.loopzero/dev-credentials.json absent → exit 0 with SKIP log
# - stripe.api_key missing OR equals "REDACTED_SECRET_050" → exit 0 with SKIP
# - mode != 0600 → FAIL (canonical secret-SoT mode contract)
# - api_key prefix not rk_/sk_ → FAIL (EC-2 from edge-case review: prevents
#   silent acceptance of publishable_key (pk_*) pasted into api_key field)
# - real api_key (rk_/sk_ prefix) → materialize Secret (idempotent via dry-run|apply)
#
# Flags:
#   --verify   assert STRIPE_API_KEY key present in Secret post-apply
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

log() { printf "%s [seed-stripe-api-keys] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

# SKIP graceful: creds file absent (operator hasn't run sponsor-provided setup)
if [[ ! -f "$CREDS_FILE" ]]; then
  log "SKIP — $CREDS_FILE not found (sponsor-blocked)"
  exit 0
fi

# FAIL hard: mode != 0600 (canonical secret-SoT contract per feedback_secret_canonical_no_hardcode)
MODE=$(stat -c '%a' "$CREDS_FILE" 2>/dev/null || stat -f '%Lp' "$CREDS_FILE" 2>/dev/null)
if [[ "$MODE" != "600" ]]; then
  log "FAIL — $CREDS_FILE mode $MODE (canonical 0600 required for secret SoT)"
  exit 1
fi

# Extract api_key via python3 (jq-free)
API_KEY=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('api_key',''))" 2>/dev/null || echo "")
if [[ -z "$API_KEY" || "$API_KEY" == "REDACTED_SECRET_050" || "$API_KEY" == "REDACTED_SECRET_051" ]]; then
  log "SKIP — stripe.api_key placeholder/ausente (sponsor-blocked)"
  exit 0
fi

# Red Team loopzero/loopzero#155 closure (2026-07-01): extract per-plan Stripe
# price IDs so `internal/billing/plans.go::PriceIDForPlan` can resolve
# them via os.Getenv("STRIPE_PRICE_<PLAN>"). Sponsor populates
# dev-credentials.json under stripe.price_{starter,pro,team}; SKIP-graceful
# when absent (billing endpoints still return typed 503 per handler ladder).
PRICE_STARTER=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('price_starter',''))" 2>/dev/null || echo "")
PRICE_PRO=$(python3     -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('price_pro',''))"     2>/dev/null || echo "")
PRICE_TEAM=$(python3    -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('price_team',''))"    2>/dev/null || echo "")

# EC-2 absorption (edge-case review 2026-06-24): api_key prefix guard.
# Stripe secret keys start with rk_test_/sk_test_/rk_live_/sk_live_; publishable
# keys start with pk_*. Operators commonly paste publishable_key into api_key
# field by mistake — secret materializes silently, Stripe API rejects with 401.
# Fail loudly with diagnostic so the operator sees the bug immediately.
if ! [[ "$API_KEY" =~ ^(rk|sk)_(test|live)_ ]]; then
  log "FAIL — api_key has wrong prefix (got: ${API_KEY:0:8}...); expected rk_*/sk_*"
  log "       common cause: publishable_key (pk_*) pasted into stripe.api_key field"
  exit 1
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY: kubectl create secret loopzero-stripe-api-keys --from-literal=STRIPE_API_KEY=<redacted>"
  exit 0
fi

# Materialize Secret via dry-run | apply (idempotent — kubectl 3-way merge
# removes stale keys on the canonical migration path).
# #155: STRIPE_PRICE_{STARTER,PRO,TEAM} bundled into the same Secret so
# loopzero-cloud's envFrom picks them up in ONE restart.
kubectl --context "$MGMT_CONTEXT" -n loopzero-system create secret generic loopzero-stripe-api-keys \
  --from-literal=STRIPE_API_KEY="$API_KEY" \
  --from-literal=STRIPE_PRICE_STARTER="$PRICE_STARTER" \
  --from-literal=STRIPE_PRICE_PRO="$PRICE_PRO" \
  --from-literal=STRIPE_PRICE_TEAM="$PRICE_TEAM" \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CONTEXT" apply -f -

if [[ "$VERIFY" == "1" ]]; then
  log "verifying STRIPE_API_KEY key in Secret..."
  if kubectl --context "$MGMT_CONTEXT" -n loopzero-system get secret loopzero-stripe-api-keys -o jsonpath='{.data.STRIPE_API_KEY}' 2>/dev/null | grep -q .; then
    log "PASS — secret materialized"
  else
    log "FAIL — STRIPE_API_KEY key missing in loopzero-stripe-api-keys Secret"
    exit 1
  fi
fi

# Red Team loopzero/loopzero#154 closure (2026-07-01): roll out loopzero-cloud so the
# pod's `envFrom.secretRef.loopzero-stripe-api-keys` re-reads STRIPE_API_KEY.
# Without this, boot-order matters: pod started BEFORE this seed runs
# sees cfg.StripeAPIKey empty → `billingHandler == nil` → /v1/billing*
# routes never wired → 404 from chi router → upgrade Starter→Pro dead.
# Mirrors the canonical pattern in seed-broker-canonical.sh Layer 4.
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout restart deployment/loopzero-cloud >/dev/null
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout status deployment/loopzero-cloud --timeout=120s >/dev/null
log "Layer 4: loopzero-cloud restarted (env STRIPE_API_KEY now wired via envFrom)"
