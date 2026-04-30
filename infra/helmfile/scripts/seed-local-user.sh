#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD F-NEW iter 8 — Batch seed local user (Rule 10)
#
# Popula a base local com user + tenant + membership necessários para
# o JWT machine-to-machine emitido por `loopzero-bootstrap` ser aceito pelo
# loopzero-engine. Sem este seed o flow para em HTTP 403 NO_TENANT.
#
# **Why batch seed em vez de browser**: Rule 10 (Local equals production)
# exige que o ambiente local funcione fim-a-fim. Em produção, sign-up via
# dashboard cria user via Kratos + injeta membership via TenantProvisioner.
# Em standalone-multi não há dashboard humano-acessível — esse script
# replica o efeito final no banco SEM tocar no código de produto.
#
# Idempotente:
#   - users INSERT ... ON CONFLICT DO NOTHING
#   - tenants INSERT ... ON CONFLICT DO NOTHING
#   - tenant_members INSERT ... ON CONFLICT DO NOTHING
#
# Outputs:
#   ~/.loopzero/local-dogfood-token.txt — JWT pronto para uso pelo CLI:
#     loopzero login --token "$(cat ~/.loopzero/local-dogfood-token.txt)"
#
# Usage:
#   bash infra/helmfile/scripts/seed-local-user.sh
#   bash infra/helmfile/scripts/seed-local-user.sh --tenant tn-myseed
set -euo pipefail

USER_ID="${SEED_USER_ID:-loopzero-bootstrap}"
TENANT_ID="${SEED_TENANT_ID:-tn-e2etest}"
ROLE="${SEED_ROLE:-owner}"
EMAIL="${SEED_EMAIL:-bootstrap@loopzero.local}"
DISPLAY_NAME="${SEED_DISPLAY_NAME:-Bootstrap User}"
USERNAME="${SEED_USERNAME:-bootstrap}"

NS_LOOPZERO="${LOOPZERO_NAMESPACE:-loopzero-system}"
NS_HYDRA="${HYDRA_NAMESPACE:-loopzero-auth}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tenant) TENANT_ID="$2"; shift 2 ;;
    --user) USER_ID="$2"; shift 2 ;;
    --email) EMAIL="$2"; shift 2 ;;
    *) shift ;;
  esac
done

log() { printf '[seed-local-user] %s\n' "$*" >&2; }

# 2026-04-30 LIVE finding: this script invoked `kubectl` without an
# explicit --context, so it followed whatever the operator left in
# `kubectl config current-context`. When run after a context switch
# (e.g., dogfood preflight T0.2 retry after the operator inspected
# the build cluster), it pointed at the wrong cluster and silently
# returned PG_POD="" → "ERROR: postgresql pod not found". The token
# regen step fails with no useful hint about WHY.
#
# Fix: pin the management-cluster context explicitly. SEED_CTX overrides
# for non-default deployments; default is k3d-loopzero-mgmt which matches
# the standalone-multi local layout this script was written for.
SEED_CTX="${SEED_CTX:-k3d-loopzero-mgmt}"

# 1) Resolve postgresql pod.
PG_POD=$(kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [[ -z "$PG_POD" ]]; then
  log "ERROR: postgresql pod not found in $NS_LOOPZERO (context=$SEED_CTX)"
  log "  hint: verify with 'kubectl --context $SEED_CTX -n $NS_LOOPZERO get pods'"
  log "  hint: override via SEED_CTX=<your-context> if cluster name differs"
  exit 1
fi

# 2) DB password from Secret.
PGPASS=$(kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" get secret loopzero-db-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
if [[ -z "$PGPASS" ]]; then
  # Fallback to default (helmfile generated standalone password).
  PGPASS="loopzero-dev-password"
  log "warn: loopzero-db-credentials.password missing — falling back to default"
fi

run_psql() {
  kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" exec "$PG_POD" -c postgresql -- \
    env PGPASSWORD="$PGPASS" psql -U loopzero -d loopzero -v ON_ERROR_STOP=1 "$@"
}

log "seeding tenant=$TENANT_ID user=$USER_ID role=$ROLE"

# 3) Insert user FIRST (tenants.owner_id FK references users.id).
run_psql -c "
INSERT INTO users (id, email, display_name, username)
VALUES ('$USER_ID', '$EMAIL', '$DISPLAY_NAME', '$USERNAME')
ON CONFLICT (id) DO UPDATE SET
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  username = EXCLUDED.username,
  updated_at = now();
" >/dev/null
log "user ensured"

# 4) Insert tenant. Schema requires slug (UNIQUE) + owner_id (FK to users).
# slug derived from id (replace anything non-alphanumeric with '-').
SLUG=$(printf '%s' "$TENANT_ID" | tr -c 'a-zA-Z0-9' '-' | tr '[:upper:]' '[:lower:]')
run_psql -c "
INSERT INTO tenants (id, slug, owner_id, display_name)
VALUES ('$TENANT_ID', '$SLUG', '$USER_ID', '$TENANT_ID')
ON CONFLICT (id) DO NOTHING;
" >/dev/null
log "tenant ensured (slug=$SLUG owner=$USER_ID)"

# 5) Insert membership.
run_psql -c "
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('$TENANT_ID', '$USER_ID', '$ROLE')
ON CONFLICT (tenant_id, user_id) DO UPDATE SET
  role = EXCLUDED.role,
  updated_at = now();
" >/dev/null
log "membership ensured"

# 6) Mint a fresh JWT via Hydra client_credentials.
CSEC=$(kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" get secret loopzero-bootstrap-credentials -o jsonpath='{.data.client_secret}' 2>/dev/null | base64 -d)
if [[ -z "$CSEC" ]]; then
  log "ERROR: loopzero-bootstrap-credentials Secret not found — run bootstrap-oauth-clients.sh first (context=$SEED_CTX)"
  exit 1
fi

# Use a short-lived port-forward to Hydra public.
pkill -f "kubectl.*port-forward.*ory-hydra-public" 2>/dev/null || true
sleep 1
kubectl --context "$SEED_CTX" -n "$NS_HYDRA" port-forward svc/ory-hydra-public 14444:4444 >/dev/null 2>&1 &
PF_PID=$!
trap "kill $PF_PID 2>/dev/null || true" EXIT
sleep 4

TOKEN_RESP=$(curl -sf -X POST http://localhost:14444/oauth2/token \
  -d "grant_type=client_credentials" \
  -d "client_id=loopzero-bootstrap" \
  -d "client_secret=$CSEC" \
  -d "scope=openid offline_access" \
  -d "audience=loopzero-engine")

TOKEN=$(printf '%s' "$TOKEN_RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null || true)
if [[ -z "$TOKEN" ]]; then
  log "ERROR: token mint failed: $(printf '%s' "$TOKEN_RESP" | head -c 120)"
  exit 1
fi

# 7) Persist for CLI consumption.
mkdir -p "${HOME}/.loopzero"
chmod 700 "${HOME}/.loopzero"
TOKEN_FILE="${HOME}/.loopzero/local-dogfood-token.txt"
printf '%s' "$TOKEN" > "$TOKEN_FILE"
chmod 600 "$TOKEN_FILE"
log "token written: $TOKEN_FILE (len=${#TOKEN})"
log ""
log "Ready for dogfood. Run:"
log "  export LOOPZERO_TOKEN=\$(cat ~/.loopzero/local-dogfood-token.txt)"
log "  export LOOPZERO_URL=http://localhost:13001"
log "  loopzero whoami"
