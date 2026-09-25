#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T2.2 — drill-mint-jwt.sh (closes finding-2026-05-15 S0 gap)
#
# Mints a JWT against the drill mgmt cluster via Hydra client_credentials grant.
# Analog of infra/helmfile/scripts/seed-local-user.sh adapted for DOKS drill
# (instead of k3d-loopzero-core local context).
#
# JWT script taxonomy (T2.6 of plan 2026-05-21-deep-review-remediation):
#   - infra/helmfile/scripts/lib/hydra-mint.sh  — SHARED LOW-LEVEL LIB
#     (sourced by both this script + seed-local-user.sh; the actual mint flow)
#   - infra/scripts/drill/drill-mint-jwt.sh      — DRILL ENTRY POINT (THIS FILE)
#     (drill-context wrapper, parametrizes hydra-mint.sh for DOKS)
#   - infra/scripts/drill/jwt-diagnose.sh        — DIAGNOSTIC TOOL
#     (NOT a minter — inspects an existing JWT for validity, claims, TTL)
# Not duplicates: each has a distinct role in the layer model. The agent's
# deep-review v2 flagged this as "consolidation candidate"; T2.6 audit
# confirmed the consolidation already happened via lib/hydra-mint.sh at
# an earlier T4.4 task (commit history). This header now documents the
# layering so future audits don't re-flag.
#
# Used by (operator-run — GitHub Actions removed 2026-05-23):
#   - task drill:l2-scale — L2 scale run (was bi-weekly cron + manual dispatch)
#   - drill soaks — Stage 1/2 soaks
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
#   SEED_ACCOUNT_ID   — account created when the user owns none (default acct-bootstrap)
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
SEED_ACCOUNT_ID="${SEED_ACCOUNT_ID:-acct-bootstrap}"

NS_LOOPZERO="${LOOPZERO_NAMESPACE:-loopzero-system}"
# Bug #35 LIVE fix (2026-05-18 iter 28): drill helmfile installs ory-hydra
# in loopzero-auth namespace (chart default), not loopzero-system. Default this to
# loopzero-auth to match drill. Local k3d standalone may override via env.
NS_HYDRA="${HYDRA_NAMESPACE:-loopzero-auth}"
HYDRA_PORT_LOCAL="${HYDRA_PORT_LOCAL:-14444}"

# Minimum JWT TTL — EC-07 fix. L3 spec runs 60min; buffer 1h.
MIN_JWT_TTL_SECONDS=7200

log() { printf '[drill-mint-jwt] %s\n' "$*" >&2; }

# loopzero-ops is the Go home of the logic this script used to re-implement in sed
# and SQL (#236). An installed binary wins; otherwise it is built from this
# checkout.
DRILL_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
loopzero_ops() {
  if command -v loopzero-ops >/dev/null 2>&1; then
    loopzero-ops "$@"
  else
    (cd "$DRILL_REPO_ROOT/cmd/loopzero-ops" && go run . "$@")
  fi
}

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
  # Parse password from pgUri (postgres:// and postgresql://, Bug #17). A
  # pgUri that does not parse aborts here rather than becoming the password.
  if [[ -n "$PG_URI_FULL" ]]; then
    DRILL_PG_PASS=$(printf '%s' "$PG_URI_FULL" | loopzero_ops dsn password)
    export DRILL_PG_HOST DRILL_PG_PORT DRILL_PG_PASS
    log "  ✓ DRILL_PG_HOST=$DRILL_PG_HOST DRILL_PG_PORT=$DRILL_PG_PORT (password parsed from pgUri)"
  fi
fi

if [[ -n "${DRILL_PG_HOST:-}" ]]; then
  # Managed PG path (preferred for drill — D4 mgmt-tier managed PG)
  DRILL_PG_PASS="${DRILL_PG_PASS:-loopzero-drill-password}"
  DRILL_PG_PORT="${DRILL_PG_PORT:-25060}"
  # Bug #39 LIVE fix (2026-05-18 iter 33): DO Managed PG DatabaseFirewall restricts
  # to cluster_uuid auth (per Bug #11 prod-parity mandate, CTO Rule #0). Local psql
  # cannot reach the DB (firewall blocks IP). Solution: run psql FROM INSIDE the
  # mgmt cluster via transient pod (cluster_uuid auth passes firewall).
  # Bug #38 v2 retention: still uses individual flags + PGPASSWORD/PGSSLMODE env
  # to avoid URI-encode pitfall with special chars in doadmin password.
  #   (b) Seed tables in `loopzero` DB per Bug #12 fix.
  DRILL_PG_DB="${DRILL_PG_DB:-loopzero}"
  run_psql() {
    # Bug #39 v3 (2026-05-18 iter 36): kubectl run --rm -i races on pod attach
    # (one-shot command pod terminates before attach completes). Use Job-like
    # pattern: run (no attach) → wait completion → logs → exitCode → delete.
    local POD_NAME="drill-psql-$$-$RANDOM"
    kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" run "$POD_NAME" \
      --restart=Never --image=postgres:16-alpine --quiet \
      --env="PGPASSWORD=$DRILL_PG_PASS" \
      --env="PGSSLMODE=require" \
      --command -- \
      psql --host="$DRILL_PG_HOST" --port="$DRILL_PG_PORT" \
           --username=doadmin --dbname="$DRILL_PG_DB" \
           --no-psqlrc -v ON_ERROR_STOP=1 "$@" >/dev/null 2>&1
    # Wait for pod to reach terminal state (Succeeded or Failed)
    kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" wait \
      --for=jsonpath='{.status.phase}'=Succeeded "pod/$POD_NAME" \
      --timeout=120s >/dev/null 2>&1 || true
    local PHASE EXIT
    PHASE=$(kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" get pod "$POD_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
    EXIT=$(kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" get pod "$POD_NAME" -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}' 2>/dev/null || echo "1")
    # Echo logs to stderr (preserves stdout for callers that pipe stdout)
    kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" logs "$POD_NAME" >&2 2>/dev/null || true
    kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" delete pod "$POD_NAME" --wait=false >/dev/null 2>&1 || true
    if [ "$PHASE" != "Succeeded" ] || [ "${EXIT:-1}" != "0" ]; then
      echo "ERROR: drill-psql pod $POD_NAME phase=$PHASE exitCode=$EXIT" >&2
      return "${EXIT:-1}"
    fi
    return 0
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
    # T1.3: fail-fast — drill MUST have loopzero-db-credentials reconciled
    # before JWT minting. If empty, helmfile bootstrap incomplete.
    echo "ERROR: loopzero-db-credentials.password missing in drill mgmt cluster (ns=$NS_LOOPZERO)" >&2
    echo "       Run drill-helmfile-bootstrap.sh first OR re-check Pulumi outputs." >&2
    exit 1
  fi
  run_psql() {
    kubectl --kubeconfig "$DRILL_KUBECONFIG" -n "$NS_LOOPZERO" exec "$PG_POD" -c postgresql -- \
      env PGPASSWORD="$PG_PASS_INCLUSTER" psql -U loopzero -d loopzero -v ON_ERROR_STOP=1 "$@"
  }
fi

log "Seeding user=$SEED_USER_ID tenant=$SEED_TENANT_ID role=$SEED_ROLE"
# The bootstrap-user rule (user + account + tenant + exactly ONE membership)
# lives once in `loopzero-ops sql bootstrap-user`, shared with the local and E2E
# seeds. This copy used to insert the tenant without account_id, which fails on
# every database migrated past 000044. Rendered first, so a refused field stops
# here instead of reaching the psql pod.
SEED_SQL="$(loopzero_ops sql bootstrap-user \
  --user-id "$SEED_USER_ID" --email "$SEED_EMAIL" --display-name "$SEED_DISPLAY_NAME" \
  --username "$SEED_USERNAME" --account-id "$SEED_ACCOUNT_ID" \
  --tenant-id "$SEED_TENANT_ID" --role "$SEED_ROLE")"
run_psql -c "$SEED_SQL" >/dev/null
log "Seed complete"

# --- Hydra port-forward + JWT mint via shared lib ---
# Plan 2026-05-20 T4.4 — consume infra/helmfile/scripts/lib/hydra-mint.sh
# (replaces ~55 LOC of duplicated port-forward + curl + TTL check that was
# identical to seed-local-user.sh logic).
#
# Per ADR D3 (scripts parametrized by env): we pre-resolve drill-specific
# env (kubeconfig, secret, namespace, port) here and DELEGATE the pure
# mint flow (port-forward + curl + EC-07 TTL validation) to the lib.
#
# Lib API contract (verified hermetically in lib self-test):
#   - mint_jwt reads: HYDRA_NAMESPACE, HYDRA_PORT_LOCAL, KUBECTL_CMD,
#     CLIENT_ID, CLIENT_SECRET, SCOPE, AUDIENCE, MIN_JWT_TTL
#   - mint_jwt writes: JWT to stdout, errors to stderr
#   - mint_jwt exits: 0 success, 1 deps missing, 2 mint failure, 4 TTL too short
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB_HYDRA_MINT="${SCRIPT_DIR}/../../helmfile/scripts/lib/hydra-mint.sh"
if [[ ! -r "$LIB_HYDRA_MINT" ]]; then
  log "ERROR: shared lib not found at $LIB_HYDRA_MINT"
  log "  Plan 2026-05-20 T4.4 — drill-mint-jwt.sh depends on this lib"
  exit 1
fi
# shellcheck source=../../helmfile/scripts/lib/hydra-mint.sh
source "$LIB_HYDRA_MINT"

log "Minting JWT via Hydra client_credentials grant (via shared lib)..."

# shellcheck disable=SC2034  # All exported for mint_jwt
HYDRA_NAMESPACE="$NS_HYDRA"
HYDRA_PORT_LOCAL="$HYDRA_PORT_LOCAL"
KUBECTL_CMD="kubectl --kubeconfig $DRILL_KUBECONFIG"
CLIENT_ID="loopzero-bootstrap"
CLIENT_SECRET="$CSEC"
SCOPE="openid offline_access"
AUDIENCE="loopzero-engine"
MIN_JWT_TTL="$MIN_JWT_TTL_SECONDS"

if ! TOKEN=$(mint_jwt); then
  RC=$?
  # Map lib exit codes to drill-mint-jwt.sh contract (preserve external API):
  #   lib 1 (deps missing)  → drill 2 (Hydra path issue)
  #   lib 2 (mint failure)  → drill 3 (JWT mint failed)
  #   lib 4 (TTL too short) → drill 4 (EC-07)
  case "$RC" in
    1|2) exit 3 ;;
    4)   exit 4 ;;
    *)   exit 3 ;;
  esac
fi

if [[ -z "$TOKEN" ]]; then
  log "ERROR: lib returned success but token empty (unexpected — investigate)"
  exit 3
fi

# --- Decode actual TTL for logging (lib already validated >= MIN_JWT_TTL) ---
# Caller-side parse preserves observable contract: hermetic tests assert that
# stderr contains "JWT TTL: <ttl>s". Even though the lib already validated
# the minimum, the actual value is informative for operators inspecting logs.
PAYLOAD_B64=$(printf '%s' "$TOKEN" | cut -d. -f2)
case $((${#PAYLOAD_B64} % 4)) in
  2) PAYLOAD_B64="${PAYLOAD_B64}==" ;;
  3) PAYLOAD_B64="${PAYLOAD_B64}=" ;;
esac
PAYLOAD_JSON=$(printf '%s' "$PAYLOAD_B64" | tr '_-' '/+' | base64 -d 2>/dev/null || true)
EXP=$(printf '%s' "$PAYLOAD_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
IAT=$(printf '%s' "$PAYLOAD_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("iat",0))' 2>/dev/null || echo 0)
JWT_TTL=$((EXP - IAT))
log "JWT TTL: ${JWT_TTL}s (>= ${MIN_JWT_TTL_SECONDS}s required ✓)"

# --- Output: single line JWT to stdout (contract preserved) ---
printf '%s\n' "$TOKEN"
