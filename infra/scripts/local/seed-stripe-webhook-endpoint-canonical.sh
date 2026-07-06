#!/usr/bin/env bash
# seed-stripe-webhook-endpoint-canonical.sh
#
# Canonical post-Cleanroom Stripe test-mode webhook endpoint provisioning for
# Red Team loopzero-cloud#156 closure (payment succeeds but plan stays starter
# because Stripe never delivers checkout.session.completed to loopzero-cloud).
#
# Root cause (5 Whys):
#   1. Why plan fica starter? -> handler checkout.session.completed nunca roda
#   2. Why? -> Stripe test-mode NUNCA envia o evento pra loopzero-cloud
#   3. Why? -> Não há webhook endpoint configurado no Stripe test-mode
#   4. Why? -> Nenhum seed do post-Cleanroom chama Stripe API para criar
#   5. Why? -> Canonical chain parou em `loopzero-stripe-api-keys` — este script
#      fecha o gap materializing (a) webhook endpoint no Stripe + (b) whsec
#      real no `loopzero-stripe-webhook` Secret (substituindo o placeholder
#      `whsec_dev_*` que fazia signature validation falhar mesmo se evento
#      chegasse).
#
# 4-layer chain:
#   (1) Read stripe.api_key from ~/.loopzero/dev-credentials.json (mode 0600
#       enforced pre-read; secret SoT per feedback_secret_canonical_no_hardcode).
#   (2) Query Stripe API `GET /v1/webhook_endpoints?limit=100` for existing
#       endpoint targeting LOOPZERO_WEBHOOK_URL. If found AND dev-credentials.json
#       already has non-placeholder webhook_secret → SKIP (idempotent).
#       If found but placeholder secret → DELETE old endpoint (whsec is
#       returned ONLY at creation, so we must recreate to capture it).
#   (3) POST /v1/webhook_endpoints with url + 5 canonical enabled_events.
#       Capture the returned `secret` field (`whsec_test_*`).
#   (4) Persist whsec to (a) ~/.loopzero/dev-credentials.json `stripe.webhook_secret`
#       AND (b) `loopzero-stripe-webhook` K8s Secret via seed-stripe-secret-canonical.sh
#       (delegates to preserve DRY + mode 0600 invariants).
#   (5) Rollout restart loopzero-cloud so the pod picks up the new whsec via
#       envFrom secretRef (matches sibling seed-stripe-api-keys #154 chain).
#
# Related to Red Team #154 (billing rotas 404 pre-rollout) + #155 (STRIPE_PRICE_*
# unset). This is the THIRD layer of the billing chain: even after routes wired
# + price IDs set, without webhook endpoint + real whsec the payment never
# provisions the plan.
#
# Flags:
#   --verify   assert whsec present in dev-credentials.json + Stripe endpoint returns 200
#   --dry-run  print intended actions without mutation
#   --help|-h  print this header
#
# Env vars:
#   KUBE_CONTEXT           default k3d-loopzero-mgmt
#   LOOPZERO_DEV_CREDENTIALS   default ~/.loopzero/dev-credentials.json
#   LOOPZERO_WEBHOOK_URL       default https://app-dev.loopzero.example/v1/webhooks/stripe
#                          (public URL — Stripe test-mode POSTs sign+deliver here)
set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
CREDS_FILE="${LOOPZERO_DEV_CREDENTIALS:-$HOME/.loopzero/dev-credentials.json}"
WEBHOOK_URL="${LOOPZERO_WEBHOOK_URL:-https://app-dev.loopzero.example/v1/webhooks/stripe}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
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

log() { printf "%s [seed-stripe-webhook-endpoint] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

# SKIP graceful: creds file absent (operator hasn't run sponsor-provided setup)
if [[ ! -f "$CREDS_FILE" ]]; then
  log "SKIP — $CREDS_FILE not found (sponsor-blocked M5)"
  exit 0
fi

# FAIL hard: mode != 0600 (canonical secret-SoT contract)
MODE=$(stat -c '%a' "$CREDS_FILE" 2>/dev/null || stat -f '%Lp' "$CREDS_FILE" 2>/dev/null)
if [[ "$MODE" != "600" ]]; then
  log "FAIL — $CREDS_FILE mode $MODE (canonical 0600 required for secret SoT)"
  exit 1
fi

API_KEY=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('api_key',''))" 2>/dev/null || echo "")
if [[ -z "$API_KEY" || ! "$API_KEY" =~ ^(rk|sk)_(test|live)_ ]]; then
  log "SKIP — stripe.api_key placeholder/missing"
  exit 0
fi

EXISTING_WHSEC=$(python3 -c "import json; d=json.load(open('$CREDS_FILE')); print(d.get('stripe',{}).get('webhook_secret',''))" 2>/dev/null || echo "")

# --- Layer 2: probe existing endpoint ---
log "querying Stripe test-mode webhook endpoints for url=$WEBHOOK_URL"
EXISTING_ID=$(curl -sSf -u "$API_KEY:" \
  "https://api.stripe.com/v1/webhook_endpoints?limit=100" \
  | python3 -c "
import json, sys, os
d = json.load(sys.stdin)
target = os.environ['WEBHOOK_URL']
for ep in d.get('data', []):
    if ep.get('url') == target:
        print(ep.get('id',''))
        break
" 2>/dev/null || echo "")

# Canonical whsec = starts with `whsec_` but NOT `whsec_dev_` (placeholder).
# Stripe test-mode API returns `whsec_<random>` — no `test_` subprefix.
if [[ -n "$EXISTING_ID" && "$EXISTING_WHSEC" =~ ^whsec_ && ! "$EXISTING_WHSEC" =~ ^whsec_dev_ ]]; then
  log "SKIP — endpoint $EXISTING_ID exists AND whsec already canonical"
  # Still ensure Secret materialized (idempotent — sibling seed handles this)
  if [[ "$DRY_RUN" != "1" ]]; then
    bash "$SCRIPT_DIR/seed-stripe-secret-canonical.sh"
  fi
  exit 0
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY: would create Stripe webhook_endpoint url=$WEBHOOK_URL + 5 canonical events"
  log "DRY: would update $CREDS_FILE stripe.webhook_secret"
  log "DRY: would materialize loopzero-stripe-webhook Secret + rollout restart loopzero-cloud"
  exit 0
fi

# --- Layer 2.5: delete stale endpoint so we can recapture the whsec ---
if [[ -n "$EXISTING_ID" ]]; then
  log "deleting stale endpoint $EXISTING_ID (whsec placeholder — needs recreation to capture real secret)"
  curl -sSf -u "$API_KEY:" -X DELETE \
    "https://api.stripe.com/v1/webhook_endpoints/$EXISTING_ID" >/dev/null
fi

# --- Layer 3: POST /v1/webhook_endpoints ---
# Enabled events canonical per webhook.go isHandledEvent + invoice.payment_failed
# (documented in handler but not yet business-processed; still register for future).
log "creating Stripe webhook endpoint targeting $WEBHOOK_URL"
CREATE_RESPONSE=$(curl -sSf -u "$API_KEY:" -X POST \
  "https://api.stripe.com/v1/webhook_endpoints" \
  --data-urlencode "url=$WEBHOOK_URL" \
  -d "enabled_events[]=checkout.session.completed" \
  -d "enabled_events[]=customer.subscription.created" \
  -d "enabled_events[]=customer.subscription.updated" \
  -d "enabled_events[]=customer.subscription.deleted" \
  -d "enabled_events[]=invoice.payment_failed")

NEW_ID=$(printf '%s' "$CREATE_RESPONSE" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))")
NEW_WHSEC=$(printf '%s' "$CREATE_RESPONSE" | python3 -c "import json,sys; print(json.load(sys.stdin).get('secret',''))")

if [[ -z "$NEW_ID" || -z "$NEW_WHSEC" ]]; then
  log "FAIL — Stripe API did not return id/secret; response snippet: ${CREATE_RESPONSE:0:200}"
  exit 1
fi

if [[ ! "$NEW_WHSEC" =~ ^whsec_ || "$NEW_WHSEC" =~ ^whsec_dev_ ]]; then
  log "FAIL — returned whsec is placeholder shape (got prefix ${NEW_WHSEC:0:12})"
  exit 1
fi

log "endpoint $NEW_ID created, whsec captured (prefix ${NEW_WHSEC:0:12}...)"

# --- Layer 4: persist whsec to dev-credentials.json + K8s Secret ---
# dev-credentials.json write via python3 -c inline preserves existing keys +
# re-applies mode 0600 (canonical secret-SoT contract). Env-passing avoids
# bash interpolation issues with special chars in whsec.
CREDS_FILE="$CREDS_FILE" NEW_WHSEC="$NEW_WHSEC" python3 -c "import json,os;p=os.environ['CREDS_FILE'];d=json.load(open(p));d.setdefault('stripe',{})['webhook_secret']=os.environ['NEW_WHSEC'];json.dump(d,open(p,'w'),indent=2);os.chmod(p,0o600)"
log "persisted whsec to $CREDS_FILE stripe.webhook_secret"

# Delegate K8s Secret materialization to sibling (DRY — mode+SKIP semantics
# already canonical there per seed-stripe-secret-canonical.sh).
bash "$SCRIPT_DIR/seed-stripe-secret-canonical.sh" --verify

# --- Layer 5: rollout restart loopzero-cloud so pod picks up new whsec ---
# Matches sibling seed-stripe-api-keys-canonical.sh #154 rollout pattern.
log "rollout restart deployment/loopzero-cloud (pod boot re-reads envFrom secretRef)"
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout restart deployment/loopzero-cloud
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout status deployment/loopzero-cloud --timeout=120s

if [[ "$VERIFY" == "1" ]]; then
  log "verifying: endpoint listable via Stripe API"
  if curl -sSf -u "$API_KEY:" \
    "https://api.stripe.com/v1/webhook_endpoints/$NEW_ID" >/dev/null; then
    log "PASS — endpoint $NEW_ID responds 200"
  else
    log "FAIL — endpoint $NEW_ID unreachable via Stripe API"
    exit 1
  fi
fi

log "DONE — Stripe test-mode webhook endpoint canonical (#156 fix)"
