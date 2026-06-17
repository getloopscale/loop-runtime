#!/usr/bin/env bash
# Canonical post-Cleanroom seed for the admin-cli Hydra OAuth2 client.
#
# Registers an OAuth2 client in Ory Hydra whose `client_id` equals the admin
# user's UUID. The `client_credentials` grant emits a JWT with `sub ==
# client_id == admin_user_id`. The engine auth middleware
# (api/internal/middleware/auth.go:341) resolves tenant via DB lookup on
# `tenant_members.user_id = sub`, so the CLI-minted JWT lands in admin tenant
# without any code change.
#
# Complements seed-admin-loopzero-dev.sh (Kratos session for dashboard) with
# the matching CLI-side Hydra path. The dogfood-login Suite 0 T5.4 probe
# requires ≥ 3 Hydra clients live; bootstrap-oauth-clients.sh registers 2
# (loopzero-cli + loopzero-bootstrap), and this script adds the 3rd (admin-cli).
#
# Idempotent: probe by client_id BEFORE create; PUT-refresh when present.
#
# Pre-conditions:
#   - admin@loopzero.example identity in PG (seed-admin-loopzero-dev.sh)
#   - tenant_members row pairing admin user → admin tenant with role=owner
#   - Hydra admin API reachable in loopzero-auth ns (svc/ory-hydra-admin:4445)
#
# Refs:
#   - Memory: reference_admin_cli_hydra_client_canonical
#   - cycle-roadmap canonical post-Cleanroom chain (this is the 11th canonical
#     seed step that closes the dogfood-login Suite 0 T5.4 gate)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
HYDRA_NS="${HYDRA_NAMESPACE:-loopzero-auth}"
LOOPZERO_NS="${LOOPZERO_NAMESPACE:-loopzero-system}"
HYDRA_ADMIN_PF_PORT="${HYDRA_ADMIN_PF_PORT:-14445}"
PG_PORT="${PG_PORT:-15432}"
PG_USER="${PG_USER:-loopzero}"
PG_PASS="${PG_PASS:-loopzero-dev-password}"
PG_DB="${PG_DB:-loopzero}"
SECRET_OUT="${SECRET_OUT:-$HOME/.loopzero/admin-cli-client-secret.txt}"

log() { printf '[%(%H:%M:%SZ)T] [seed-admin-hydra-client-canonical] %s\n' -1 "$*" >&2; }
fail() { log "ERROR: $*"; exit 1; }

VERIFY=0
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --help|-h) sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) fail "Unknown arg: $arg" ;;
  esac
done

# Resolve admin user id + tenant id from PG (canonical SoT).
log "Layer 1: resolving admin user_id + tenant_id from PG"
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NS" port-forward svc/postgresql "$PG_PORT":5432 >/dev/null 2>&1 &
PG_PF=$!
trap 'kill "$PG_PF" "${HYDRA_PF:-}" 2>/dev/null || true' EXIT INT TERM
sleep 2

ADMIN_USER_ID="$(PGPASSWORD="$PG_PASS" psql -h 127.0.0.1 -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" -tAc \
  "SELECT id FROM users WHERE email='admin@loopzero.example';" 2>/dev/null | tr -d '[:space:]')"
[[ -n "$ADMIN_USER_ID" ]] || fail "admin@loopzero.example not in users table; run seed-admin-loopzero-dev.sh first"

ADMIN_TENANT="$(PGPASSWORD="$PG_PASS" psql -h 127.0.0.1 -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" -tAc \
  "SELECT tenant_id FROM tenant_members WHERE user_id='$ADMIN_USER_ID' AND role='owner' LIMIT 1;" 2>/dev/null | tr -d '[:space:]')"
[[ -n "$ADMIN_TENANT" ]] || fail "tenant_members row missing for admin (user_id=$ADMIN_USER_ID); run seed-admin-loopzero-dev.sh"

log "Layer 1 OK: admin user=$ADMIN_USER_ID tenant=$ADMIN_TENANT"

# Port-forward Hydra admin API
kubectl --context "$MGMT_CONTEXT" -n "$HYDRA_NS" port-forward svc/ory-hydra-admin "$HYDRA_ADMIN_PF_PORT":4445 >/dev/null 2>&1 &
HYDRA_PF=$!
sleep 2

# Idempotent register OR refresh
EXISTING_HTTP="$(curl -sS -o /tmp/hydra-existing.json -w '%{http_code}' \
  "http://localhost:${HYDRA_ADMIN_PF_PORT}/admin/clients/${ADMIN_USER_ID}" 2>/dev/null || echo "000")"

if [[ "$EXISTING_HTTP" == "200" ]]; then
  # Already registered — reuse existing secret if file present, otherwise rotate.
  if [[ -s "$SECRET_OUT" ]]; then
    SECRET="$(cat "$SECRET_OUT")"
    log "Layer 2: client $ADMIN_USER_ID already registered — reusing persisted secret"
  else
    SECRET="$(openssl rand -hex 32)"
    log "Layer 2: client $ADMIN_USER_ID exists but no persisted secret — rotating"
  fi
  PAYLOAD="$(jq -nc --arg id "$ADMIN_USER_ID" --arg secret "$SECRET" \
    '{client_id:$id, client_name:"loopzero-admin-cli", client_secret:$secret,
      grant_types:["client_credentials"], response_types:[],
      scope:"openid offline_access", audience:["loopzero-engine"],
      token_endpoint_auth_method:"client_secret_post"}')"
  HTTP="$(curl -sS -o /tmp/hydra-put.json -w '%{http_code}' -X PUT \
    -H "Content-Type: application/json" -d "$PAYLOAD" \
    "http://localhost:${HYDRA_ADMIN_PF_PORT}/admin/clients/${ADMIN_USER_ID}" 2>/dev/null || echo "000")"
  [[ "$HTTP" == "200" ]] || fail "PUT /admin/clients/${ADMIN_USER_ID} HTTP $HTTP body=$(head -c 200 /tmp/hydra-put.json)"
else
  SECRET="$(openssl rand -hex 32)"
  PAYLOAD="$(jq -nc --arg id "$ADMIN_USER_ID" --arg secret "$SECRET" \
    '{client_id:$id, client_name:"loopzero-admin-cli", client_secret:$secret,
      grant_types:["client_credentials"], response_types:[],
      scope:"openid offline_access", audience:["loopzero-engine"],
      token_endpoint_auth_method:"client_secret_post"}')"
  HTTP="$(curl -sS -o /tmp/hydra-post.json -w '%{http_code}' -X POST \
    -H "Content-Type: application/json" -d "$PAYLOAD" \
    "http://localhost:${HYDRA_ADMIN_PF_PORT}/admin/clients" 2>/dev/null || echo "000")"
  [[ "$HTTP" == "201" ]] || fail "POST /admin/clients HTTP $HTTP body=$(head -c 200 /tmp/hydra-post.json)"
  log "Layer 2: client $ADMIN_USER_ID created"
fi

mkdir -p "$(dirname "$SECRET_OUT")"
printf '%s' "$SECRET" > "$SECRET_OUT"
chmod 600 "$SECRET_OUT"
log "Layer 3: secret persisted at $SECRET_OUT (mode 0600)"

if [[ "$VERIFY" == "1" ]]; then
  COUNT="$(curl -sS "http://localhost:${HYDRA_ADMIN_PF_PORT}/admin/clients" | jq 'length')"
  log "VERIFY: Hydra clients live count=$COUNT (expect ≥ 3 for dogfood-login Suite 0)"
  [[ "$COUNT" -ge 3 ]] || fail "VERIFY: client count $COUNT < 3"
  TOKEN_HTTP="$(curl -sS -o /tmp/hydra-token.json -w '%{http_code}' -X POST \
    -d "grant_type=client_credentials&client_id=$ADMIN_USER_ID&client_secret=$SECRET&audience=loopzero-engine" \
    "http://localhost:${HYDRA_ADMIN_PF_PORT/14445/14444}/oauth2/token" 2>/dev/null || echo "000")"
  log "VERIFY: token mint HTTP=$TOKEN_HTTP (informational — port 4444 may need separate pf)"
fi

log "DONE — admin-cli Hydra client canonical (client_id=$ADMIN_USER_ID, secret at $SECRET_OUT)"
