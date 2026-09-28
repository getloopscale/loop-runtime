#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD F-NEW iter 8 — Batch seed local user (Rule 10)
#
# Seeds the local database with the user, account, tenant and membership the
# machine-to-machine JWT issued to `loopzero-bootstrap` needs before the engine
# accepts it. Without this seed the flow stops at HTTP 403 NO_TENANT.
#
# Why a batch seed rather than the browser: Rule 10 (local equals production)
# requires the local environment to work end to end. In production a dashboard
# sign-up creates the user through Kratos and the membership through the
# TenantProvisioner; standalone-multi has no dashboard a person can reach, so
# this script reproduces the end state in the database without touching
# product code.
#
# Idempotent: rows are upserted or left alone, and the only deletion is of the
# user's memberships outside the canonical tenant (`loopzero-ops sql
# bootstrap-user`).
#
# Outputs:
#   ~/.loopzero/local-dogfood-token.txt — a JWT ready for the CLI:
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

# loopzero-ops holds the seed statements this script used to spell out (#236). An
# installed binary wins; otherwise it is built from this checkout.
SEED_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
loopzero_ops() {
  if command -v loopzero-ops >/dev/null 2>&1; then
    loopzero-ops "$@"
  else
    (cd "$SEED_REPO_ROOT/cmd/loopzero-ops" && go run . "$@")
  fi
}

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

# 2) Connect as loopzero_worker. The seed writes users, accounts, tenants and
#    tenant_members and deletes the user's memberships in every OTHER tenant —
#    cross-tenant work, which since 000064 only loopzero_worker may do (its named
#    *_worker_cross_tenant policies, 000062); the table owner is refused by FORCE
#    ROW LEVEL SECURITY. `task db:roles` sets this password (#396).
PGPASS=$(kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" get secret loopzero-db-credentials -o 'jsonpath={.data.worker-password}' 2>/dev/null | base64 -d)
if [[ -z "$PGPASS" ]]; then
  # T1.3 (plan 2026-05-21-deep-review-remediation): fail-fast instead of
  # hardcoded fallback. generate-secrets.sh writes worker-password and
  # `task db:roles` gives loopzero_worker that password.
  log "error: loopzero-db-credentials.worker-password missing in $NS_LOOPZERO — run generate-secrets.sh <env> core and task db:roles first"
  exit 1
fi

run_psql() {
  kubectl --context "$SEED_CTX" -n "$NS_LOOPZERO" exec "$PG_POD" -c postgresql -- \
    env PGPASSWORD="$PGPASS" psql -U loopzero_worker -d loopzero -v ON_ERROR_STOP=1 "$@"
}

log "seeding tenant=$TENANT_ID user=$USER_ID role=$ROLE"

# 3) The bootstrap-user rule: the JWT subject exists, owns an account, and is a
#    member of exactly ONE tenant. The JWT carries only `sub`, and the API
#    answers TENANT_REQUIRED when that user belongs to more than one tenant —
#    journey/dogfood runs accumulate memberships, so every other one is
#    removed. The statements live once, in `loopzero-ops sql bootstrap-user`
#    (infra/scripts/bootstrapuser), shared with the drill and E2E seeds; this
#    script only carries them to the in-cluster Postgres. Rendered first, so a
#    refused field stops here instead of reaching psql.
ACCOUNT_ID="${SEED_ACCOUNT_ID:-acct-bootstrap}"
SEED_SQL="$(loopzero_ops sql bootstrap-user \
  --user-id "$USER_ID" --email "$EMAIL" --display-name "$DISPLAY_NAME" \
  --username "$USERNAME" --account-id "$ACCOUNT_ID" \
  --tenant-id "$TENANT_ID" --role "$ROLE")"
run_psql -c "$SEED_SQL" >/dev/null
log "user, account, tenant and single membership ensured (owner=$USER_ID tenant=$TENANT_ID)"

# 4) Mint a fresh JWT via Hydra client_credentials (shared lib per T4.4).
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

# 5) Persist for CLI consumption.
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
