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
# for non-default deployments; default is k3d-loopzero-core which matches
# the standalone-multi local layout this script was written for.
SEED_CTX="${SEED_CTX:-k3d-loopzero-core}"

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
  # T1.3 (plan 2026-05-21-deep-review-remediation): fail-fast instead of
  # hardcoded fallback. loopzero-db-credentials must exist BEFORE seed runs —
  # generate-secrets.sh in bootstrap:standalone creates it.
  log "error: loopzero-db-credentials.password missing in $NS_LOOPZERO — run generate-secrets.sh first OR set LOOPZERO_DB_PASSWORD env var before task setup"
  exit 1
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

# 3.5) Insert bootstrap account FIRST. Migration 000044 made tenants.account_id
# NOT NULL after the 000037 backfill. Production code calls EnsureAccount on
# first login; this batch seed mirrors that path for standalone-multi.
# Canonical 1:1 user->account per migration 000034 UNIQUE(owner_id) — same
# constraint that enables ON CONFLICT idempotency in EnsureAccount itself.
# Re-runs after Cleanroom are a no-op (owner_id already has an account row).
# Pattern matches api/tests/integration/project_cap_per_account_test.go canonical seed.
ACCOUNT_ID="${SEED_ACCOUNT_ID:-acct-bootstrap}"
run_psql -c "
INSERT INTO accounts (id, display_name, plan, owner_id)
VALUES ('$ACCOUNT_ID', '$DISPLAY_NAME', 'starter', '$USER_ID')
ON CONFLICT (owner_id) DO NOTHING;
" >/dev/null
log "account ensured (owner=$USER_ID)"

# 4) Insert tenant. Schema requires slug (UNIQUE) + owner_id (FK to users) +
# account_id (NOT NULL since migration 000044). Resolve account_id by
# owner_id lookup (UNIQUE) so the seed works whether the account was just
# created above OR was minted by EnsureAccount on a prior first login.
# slug derived from id (replace anything non-alphanumeric with '-').
SLUG=$(printf '%s' "$TENANT_ID" | tr -c 'a-zA-Z0-9' '-' | tr '[:upper:]' '[:lower:]')
run_psql -c "
INSERT INTO tenants (id, slug, owner_id, display_name, account_id)
SELECT '$TENANT_ID', '$SLUG', '$USER_ID', '$TENANT_ID', a.id
FROM accounts a
WHERE a.owner_id = '$USER_ID'
ON CONFLICT (id) DO NOTHING;
" >/dev/null
log "tenant ensured (slug=$SLUG owner=$USER_ID account=$ACCOUNT_ID)"

# 5) Strip pre-existing memberships of this user OUTSIDE the canonical
#    tenant before inserting/updating the canonical row.
#
# F-03 from /dogfood full report (2026-05-06): journey/dogfood runs
# accumulate memberships in tenants like e2e-tenant. The Hydra-issued
# JWT carries only `sub=$USER_ID` and the auth middleware returns
# TENANT_REQUIRED when the user belongs to >1 tenant. This step keeps
# the bootstrap user single-tenant locally so loopzero whoami / loopzero deploy
# resolve cleanly without manual DB intervention between sessions.
#
# Pattern parallels infra/tests/seed/seed.go SeedDatabase, which already
# strips bootstrap memberships ahead of its INSERTs.
run_psql -c "
DELETE FROM tenant_members
WHERE user_id = '$USER_ID' AND tenant_id <> '$TENANT_ID';
" >/dev/null
log "stripped non-canonical memberships of $USER_ID"

# 6) Insert membership.
run_psql -c "
INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES ('$TENANT_ID', '$USER_ID', '$ROLE')
ON CONFLICT (tenant_id, user_id) DO UPDATE SET
  role = EXCLUDED.role,
  updated_at = now();
" >/dev/null
log "membership ensured"

# 6) Mint a fresh JWT via Hydra client_credentials (shared lib per T4.4).
CSEC=$(kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" get secret loopzero-bootstrap-credentials -o jsonpath='{.data.client_secret}' 2>/dev/null | base64 -d)
if [[ -z "$CSEC" ]]; then
  log "ERROR: loopzero-bootstrap-credentials Secret not found — run bootstrap-oauth-clients.sh first (context=$SEED_CTX)"
  exit 1
fi

# Plan 2026-05-20 T4.4 — consume shared lib (replaces ~25 LOC of duplicated
# port-forward + curl + token parse that was identical to drill-mint-jwt.sh).
# Lib path: same directory as this script + lib/hydra-mint.sh
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB_HYDRA_MINT="${SCRIPT_DIR}/lib/hydra-mint.sh"
if [[ ! -r "$LIB_HYDRA_MINT" ]]; then
  log "ERROR: shared lib not found at $LIB_HYDRA_MINT"
  exit 1
fi
# shellcheck source=lib/hydra-mint.sh
source "$LIB_HYDRA_MINT"

# shellcheck disable=SC2034  # All exported for mint_jwt
HYDRA_NAMESPACE="$NS_HYDRA"
HYDRA_PORT_LOCAL=14444
KUBECTL_CMD="kubectl --context $SEED_CTX"
CLIENT_ID="loopzero-bootstrap"
CLIENT_SECRET="$CSEC"
SCOPE="openid offline_access"
AUDIENCE="loopzero-engine"
# MIN_JWT_TTL deliberately unset (local k3d does not enforce EC-07 like drill)

if ! TOKEN=$(mint_jwt); then
  log "ERROR: JWT mint via shared lib failed (see lib stderr above)"
  exit 1
fi
if [[ -z "$TOKEN" ]]; then
  log "ERROR: lib returned success but token empty (unexpected)"
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
