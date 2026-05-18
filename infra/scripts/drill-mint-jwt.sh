#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T2.2 — drill-mint-jwt.sh (closes finding-2026-05-15 S0 gap)
#
# Mints a JWT against the drill mgmt cluster via Hydra client_credentials grant.
# Analog of infra/helmfile/scripts/seed-local-user.sh adapted for DOKS drill
# (instead of k3d-loopzero-mgmt local context).
#
# Used by:
#   - .github/workflows/drill-l2-scale.yml — bi-weekly L2 cron + manual dispatch
#   - .github/workflows/cell2-soaks.yml — Stage 1/2 soaks (post T3.4 redirect)
#   - .claude/skills/drill-test/SKILL.md — Etapa 3 (mint JWT) per drill ciclo
#
# Exit codes:
#   0 — JWT printed to stdout (single line, ready for `JWT=$(bash drill-mint-jwt.sh)`)
#   1 — DRILL_KUBECONFIG or DRILL_PG_HOST env missing
#   2 — Hydra unreachable / not ready / port-forward fails
#   3 — JWT mint failed (Hydra returned error or empty token)
#   4 — JWT TTL < 7200s (EC-07: must cover L3 60min spec + buffer)
#
# Environment inputs:
#   DRILL_KUBECONFIG  — path to drill mgmt cluster kubeconfig (Pulumi output)
#   DRILL_PG_HOST     — managed PG host (Pulumi output, used for user seed)
#   DRILL_PG_PASS     — managed PG password (defaults to "loopzero-drill-password" placeholder)
#   DRILL_PG_PORT     — managed PG port (default 25060 DO managed PG)
#   SEED_USER_ID      — user id (default loopzero-bootstrap)
#   SEED_TENANT_ID    — tenant id (default tn-e2etest)
#
# EC-04 fix: explicit Hydra readiness wait BEFORE exec (race condition where
# helmfile sync returns but Hydra pod still in init → exec fail → run wasted).
#
# EC-07 fix: assert JWT TTL >= 7200s (2h) to cover L3 60min spec + buffer.

set -euo pipefail

# --- Input validation ---
if [[ -z "${DRILL_KUBECONFIG:-}" ]]; then
  echo "ERROR: DRILL_KUBECONFIG env var is required (path to drill mgmt cluster kubeconfig)." >&2
  echo "  Hint (v2 flat named exports per L2-unlock T1.2 D1):" >&2
  echo "    cd infra/pulumi-drill && npx pulumi stack output --stack drill mgmtClusterKubeconfig --show-secrets > /tmp/drill-mgmt-kc.yaml" >&2
  echo "    export DRILL_KUBECONFIG=/tmp/drill-mgmt-kc.yaml" >&2
  exit 1
fi

if [[ ! -r "$DRILL_KUBECONFIG" ]]; then
  echo "ERROR: DRILL_KUBECONFIG file not readable: $DRILL_KUBECONFIG" >&2
  exit 1
fi

SEED_USER_ID="${SEED_USER_ID:-loopzero-bootstrap}"
SEED_TENANT_ID="${SEED_TENANT_ID:-tn-e2etest}"
SEED_ROLE="${SEED_ROLE:-owner}"
SEED_EMAIL="${SEED_EMAIL:-bootstrap@loopzero.local}"
SEED_DISPLAY_NAME="${SEED_DISPLAY_NAME:-Drill Bootstrap}"
SEED_USERNAME="${SEED_USERNAME:-bootstrap}"

NS_LOOPZERO="${LOOPZERO_NAMESPACE:-loopzero-system}"
# Bug #35 LIVE fix (2026-05-18 iter 28): drill helmfile installs ory-hydra
# in loopzero-auth namespace (chart default), not loopzero-system. Default this to
# loopzero-auth to match drill. Local k3d standalone may override via env.
NS_HYDRA="${HYDRA_NAMESPACE:-loopzero-auth}"
HYDRA_PORT_LOCAL="${HYDRA_PORT_LOCAL:-14444}"

# Minimum JWT TTL — EC-07 fix. L3 spec runs 60min; buffer 1h.
MIN_JWT_TTL_SECONDS=7200

log() { printf '[drill-mint-jwt] %s\n' "$*" >&2; }

# --- EC-04 fix: Hydra readiness wait (BEFORE any exec attempt) ---
# Stdout suppressed; only failure surfaces via log + exit 2.
log "Waiting for Hydra pod readiness (max 120s)..."
if ! kubectl --kubeconfig "$DRILL_KUBECONFIG" wait \
  --for=condition=Ready pod \
  -l "app.kubernetes.io/name=hydra" \
  -n "$NS_HYDRA" \
  --timeout=120s > /dev/null 2>&1; then
  log "ERROR: Hydra pod did not become Ready within 120s in namespace=$NS_HYDRA"
  log "  hint: verify with 'kubectl --kubeconfig $DRILL_KUBECONFIG -n $NS_HYDRA get pods -l app.kubernetes.io/name=hydra'"
  log "  hint: drill mgmt cluster may still be bootstrapping (helmfile sync incomplete)"
  exit 2
fi
log "Hydra Ready"

# --- Resolve loopzero-bootstrap-credentials Secret ---
CSEC=$(kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" \
  get secret loopzero-bootstrap-credentials -o jsonpath='{.data.client_secret}' 2>/dev/null | base64 -d || true)
if [[ -z "$CSEC" ]]; then
  log "ERROR: loopzero-bootstrap-credentials Secret not found in $NS_LOOPZERO"
  log "  hint: run bootstrap-oauth-clients.sh against drill mgmt cluster first (helmfile sync should do this)"
  exit 2
fi

# --- Seed user + tenant + membership via PG (idempotent) ---
# DRILL_PG_HOST may be managed PG endpoint (Pulumi output) OR in-cluster
# postgresql svc (default). If managed PG, use psql client directly (script
# expects DRILL_PG_HOST + DRILL_PG_PASS env). Else exec into pod.
SEED_SLUG=$(printf '%s' "$SEED_TENANT_ID" | tr -c 'a-zA-Z0-9' '-' | tr '[:upper:]' '[:lower:]')

# Bug #37 LIVE FIX (2026-05-18 iter 30): drill uses managed PG (no in-cluster
# postgresql svc). If DRILL_PG_HOST unset, auto-read from Pulumi outputs.
if [[ -z "${DRILL_PG_HOST:-}" ]] && [[ -d "infra/pulumi-drill" ]]; then
  log "DRILL_PG_HOST not set — auto-discovering from Pulumi outputs..."
  PULUMI_GET() {
    local key="$1"
    local extra="${2:-}"
    (cd infra/pulumi-drill && npx pulumi stack output --stack drill "$key" $extra 2>/dev/null)
  }
  DRILL_PG_HOST=$(PULUMI_GET pgHost) || true
  DRILL_PG_PORT=$(PULUMI_GET pgPort) || true
  PG_URI_FULL=$(PULUMI_GET pgUri --show-secrets) || true
  # Parse password from pgUri (regex matches postgres:// + postgresql:// per Bug #17)
  if [[ -n "$PG_URI_FULL" ]]; then
    DRILL_PG_PASS=$(echo "$PG_URI_FULL" | sed -E 's|^postgres(ql)?://[^:]+:([^@]+)@.*$|\2|')
    export DRILL_PG_HOST DRILL_PG_PORT DRILL_PG_PASS
    log "  ✓ DRILL_PG_HOST=$DRILL_PG_HOST DRILL_PG_PORT=$DRILL_PG_PORT (password parsed from pgUri)"
  fi
fi

if [[ -n "${DRILL_PG_HOST:-}" ]]; then
  # Managed PG path (preferred for drill — D4 mgmt-tier managed PG)
  DRILL_PG_PASS="${DRILL_PG_PASS:-loopzero-drill-password}"
  DRILL_PG_PORT="${DRILL_PG_PORT:-25060}"
  if ! command -v psql > /dev/null 2>&1; then
    log "ERROR: psql client not installed but DRILL_PG_HOST set (managed PG mode)"
    log "  hint: install postgresql-client OR unset DRILL_PG_HOST to use in-cluster fallback"
    exit 2
  fi
  # Bug #38 LIVE fix (2026-05-18 iter 31): two issues stacked
  # (a) DO managed PG requires sslmode=require — use connection URI to make
  #     all params (sslmode, host, port, user, db, password) unambiguous
  # (b) Seed tables (users/tenants/tenant_members) are in `loopzero` DB
  #     (per Bug #12 — loopzero-api migrations target ${drillPgLoopZero.name}),
  #     NOT in `defaultdb`. Connect to loopzero DB for seeds.
  DRILL_PG_DB="${DRILL_PG_DB:-loopzero}"
  run_psql() {
    psql "postgresql://doadmin:${DRILL_PG_PASS}@${DRILL_PG_HOST}:${DRILL_PG_PORT}/${DRILL_PG_DB}?sslmode=require" \
      -v ON_ERROR_STOP=1 \
      "$@"
  }
else
  # In-cluster fallback (rare for drill — typically only used in tests)
  log "warn: DRILL_PG_HOST not set — falling back to in-cluster postgresql exec"
  PG_POD=$(kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" \
    get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -z "$PG_POD" ]]; then
    log "ERROR: postgresql pod not found AND DRILL_PG_HOST not set"
    exit 1
  fi
  PG_PASS_INCLUSTER=$(kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" \
    get secret loopzero-db-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d || true)
  if [[ -z "$PG_PASS_INCLUSTER" ]]; then
    PG_PASS_INCLUSTER="loopzero-dev-password"
  fi
  run_psql() {
    kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" exec "$PG_POD" -c postgresql -- \
      env PGPASSWORD="$PG_PASS_INCLUSTER" psql -U loopzero -d loopzero -v ON_ERROR_STOP=1 "$@"
  }
fi

log "Seeding user=$SEED_USER_ID tenant=$SEED_TENANT_ID role=$SEED_ROLE"
run_psql -c "
INSERT INTO users (id, email, display_name, username)
VALUES ('$SEED_USER_ID', '$SEED_EMAIL', '$SEED_DISPLAY_NAME', '$SEED_USERNAME')
ON CONFLICT (id) DO UPDATE SET
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  username = EXCLUDED.username,
  updated_at = now();
" >/dev/null

run_psql -c "
INSERT INTO tenants (id, slug, owner_id, display_name)
VALUES ('$SEED_TENANT_ID', '$SEED_SLUG', '$SEED_USER_ID', '$SEED_TENANT_ID')
ON CONFLICT (id) DO NOTHING;
" >/dev/null

run_psql -c "
DELETE FROM tenant_members
WHERE user_id = '$SEED_USER_ID' AND tenant_id <> '$SEED_TENANT_ID';
" >/dev/null

run_psql -c "
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('$SEED_TENANT_ID', '$SEED_USER_ID', '$SEED_ROLE')
ON CONFLICT (tenant_id, user_id) DO UPDATE SET
  role = EXCLUDED.role,
  updated_at = now();
" >/dev/null
log "Seed complete"

# --- Hydra port-forward (drill mgmt cluster) ---
pkill -f "kubectl.*port-forward.*ory-hydra-public" 2>/dev/null || true
sleep 1
kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_HYDRA" \
  port-forward svc/ory-hydra-public "${HYDRA_PORT_LOCAL}":4444 >/dev/null 2>&1 &
PF_PID=$!
trap "kill $PF_PID 2>/dev/null || true" EXIT
sleep 4

# Sanity check Hydra reachable
if ! curl -sf -m 5 "http://localhost:${HYDRA_PORT_LOCAL}/.well-known/openid-configuration" >/dev/null 2>&1; then
  log "ERROR: Hydra not reachable on localhost:${HYDRA_PORT_LOCAL} after port-forward + 4s wait"
  exit 2
fi

# --- Mint JWT with EC-07 explicit TTL request ---
# Note: actual TTL is determined by Hydra client config (TTL=2h+). We request
# `scope=openid offline_access` to ensure refresh capability. Hydra client
# `loopzero-bootstrap` is provisioned by bootstrap-oauth-clients.sh with TTL
# matching MIN_JWT_TTL_SECONDS (operator must ensure helmfile env drill
# configures Hydra client TTL >= 7200s).
log "Minting JWT via Hydra client_credentials grant..."
TOKEN_RESP=$(curl -sf -X POST "http://localhost:${HYDRA_PORT_LOCAL}/oauth2/token" \
  -d "grant_type=client_credentials" \
  -d "client_id=loopzero-bootstrap" \
  -d "client_secret=$CSEC" \
  -d "scope=openid offline_access" \
  -d "audience=loopzero-engine" 2>&1 || true)

TOKEN=$(printf '%s' "$TOKEN_RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null || true)
if [[ -z "$TOKEN" ]]; then
  log "ERROR: JWT mint failed. Hydra response: $(printf '%s' "$TOKEN_RESP" | head -c 200)"
  exit 3
fi

# --- EC-07 fix: validate JWT TTL >= MIN_JWT_TTL_SECONDS ---
# Decode JWT payload (no signature verification — we trust Hydra issued it).
PAYLOAD_B64=$(printf '%s' "$TOKEN" | cut -d. -f2)
# Pad b64 if needed (JWT uses url-safe b64 without padding)
case $((${#PAYLOAD_B64} % 4)) in
  2) PAYLOAD_B64="${PAYLOAD_B64}==" ;;
  3) PAYLOAD_B64="${PAYLOAD_B64}=" ;;
esac
PAYLOAD_JSON=$(printf '%s' "$PAYLOAD_B64" | tr '_-' '/+' | base64 -d 2>/dev/null || true)
EXP=$(printf '%s' "$PAYLOAD_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
IAT=$(printf '%s' "$PAYLOAD_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("iat",0))' 2>/dev/null || echo 0)
JWT_TTL=$((EXP - IAT))

if [[ "$JWT_TTL" -lt "$MIN_JWT_TTL_SECONDS" ]]; then
  log "ERROR: JWT TTL (${JWT_TTL}s) below minimum required (${MIN_JWT_TTL_SECONDS}s)"
  log "  EC-07 fix: L3 spec runs 60min; buffer requires TTL >= ${MIN_JWT_TTL_SECONDS}s"
  log "  Hint: increase Hydra client loopzero-bootstrap accessTokenStrategy or TTL config in helmfile env drill"
  exit 4
fi
log "JWT TTL: ${JWT_TTL}s (>= ${MIN_JWT_TTL_SECONDS}s required ✓)"

# --- Output: single line JWT to stdout ---
printf '%s\n' "$TOKEN"
