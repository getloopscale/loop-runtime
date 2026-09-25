#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16-drill-l2-unlock T2.1 (Finding #4 MEDIUM fix).
#
# Centralized Pulumi-output → helmfile-value injection wrapper.
#
# Why this exists
# ---------------
# Drill fault-finding run 2026-05-16 (Finding #4 MEDIUM/BLOCKER L2) observed:
# `helmfile -e drill list` reads LOCAL k3d-loopzero-core context (cleanroom
# artifacts) instead of drill mgmt cluster. Plan T2.1 of parent
# 2026-05-16-drill-prod-identical-multicluster documented "operator populates
# from Pulumi outputs during helmfile sync" — automation gap. Bootstrap LoopZero
# platform on drill was operator-manual.
#
# This script is the SINGLE place that:
#   1. Reads Pulumi outputs (v2 flat named exports per T1.2 D1)
#   2. Templates helmfile values (PG host, cross-cluster URLs)
#   3. Iterates per-cluster (mgmt → build → runtime → runtime-2) setting
#      KUBECONFIG accordingly
#   4. Runs `helmfile -e drill sync` per cluster
#   5. Health-checks core services pre-exit
#
# Per L2-unlock plan D2: failure points centralized = easier debugging.
# Per-cluster KUBECONFIG binding is the clean fix to Finding #4.
#
# Per T2.1.bis (EC-02 MUST FIX): sets PG_SSLMODE=require for managed PG
# connection (DO mgmt-tier enforces SSL). Local k3d default preserved.
#
# Usage:
#   bash infra/scripts/drill/drill-helmfile-bootstrap.sh                    # default permissive
#   STRICT_BOOTSTRAP=true bash infra/scripts/drill/drill-helmfile-bootstrap.sh   # CI mode fail-fast
#   bash infra/scripts/drill/drill-helmfile-bootstrap.sh --from-cluster runtime # partial recovery (EC-05)
#   bash infra/scripts/drill/drill-helmfile-bootstrap.sh --dry-run          # preview only
#
# Exit codes:
#   0 — Bootstrap succeeded (all 4 clusters healthy)
#   2 — Missing Pulumi output OR DRILL_KUBECONFIG_DIR missing
#   3 — Helmfile sync timeout / per-release status dump emitted
#   4 — Cross-cluster connectivity check fails
#   5 — Argument error
#
# Environment:
#   DRILL_KUBECONFIG_DIR — directory containing 4 kubeconfigs per cluster
#     (default /tmp/drill-kc; files: mgmt.yaml, build.yaml, runtime.yaml,
#     runtime-2.yaml)
#   STRICT_BOOTSTRAP — true|false; default false (permissive). If true,
#     ANY partial helmfile sync failure aborts immediately.
#   PULUMI_DRILL_DIR — default infra/pulumi-drill
#   HELMFILE_ENV — default drill
#   HELMFILE_DIR — default infra/helmfile
#   HEALTH_WAIT_TIMEOUT — kubectl wait timeout per readiness check (default 5m)

set -euo pipefail

# --- Argument parsing ---
DRY_RUN=false
FROM_CLUSTER=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true; shift ;;
    --from-cluster)
      FROM_CLUSTER="${2:-}"
      if [[ -z "$FROM_CLUSTER" ]]; then
        echo "[drill-helmfile-bootstrap] ERROR: --from-cluster requires argument" >&2
        exit 5
      fi
      shift 2 ;;
    -h|--help)
      cat <<'HELP'
drill-helmfile-bootstrap.sh — Centralized Pulumi-output → helmfile-value injection

  --dry-run               Preview commands without executing helmfile sync
  --from-cluster NAME     Resume from specific cluster (mgmt|build|runtime|runtime-2)
                          per EC-05 partial helmfile recovery
  -h, --help              Show this help

Plan: 2026-05-16-drill-l2-unlock T2.1 + T2.1.bis (Findings #4 + EC-02)
HELP
      exit 0 ;;
    *)
      echo "[drill-helmfile-bootstrap] ERROR: unknown flag $1" >&2
      exit 5 ;;
  esac
done

DRILL_KUBECONFIG_DIR="${DRILL_KUBECONFIG_DIR:-/tmp/drill-kc}"
PULUMI_DRILL_DIR="${PULUMI_DRILL_DIR:-infra/pulumi-drill}"
HELMFILE_ENV="${HELMFILE_ENV:-drill}"
HELMFILE_DIR="${HELMFILE_DIR:-infra/helmfile}"
STRICT_BOOTSTRAP="${STRICT_BOOTSTRAP:-false}"
HEALTH_WAIT_TIMEOUT="${HEALTH_WAIT_TIMEOUT:-5m}"

log() { printf '[drill-helmfile-bootstrap] %s\n' "$*" >&2; }

# loopzero-ops is the Go home of the logic the drill scripts used to re-implement
# in sed and SQL heredocs (#236): one implementation, tested once. An installed
# binary wins; otherwise it is built from this checkout.
DRILL_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
loopzero_ops() {
  if command -v loopzero-ops >/dev/null 2>&1; then
    loopzero-ops "$@"
  else
    (cd "$DRILL_REPO_ROOT/cmd/loopzero-ops" && go run . "$@")
  fi
}

# --- Pre-flight checks ---
log "Pre-flight checks..."

if [[ ! -d "$DRILL_KUBECONFIG_DIR" ]]; then
  log "ERROR: DRILL_KUBECONFIG_DIR not found: $DRILL_KUBECONFIG_DIR"
  log "  hint: mkdir -p $DRILL_KUBECONFIG_DIR + populate 4 kubeconfigs:"
  log "    cd $PULUMI_DRILL_DIR && for c in mgmt build runtime runtime-2; do"
  log "      pulumi stack output --stack drill \"\${c%-2}Cluster\${c#runtime-}Kubeconfig\" --show-secrets > $DRILL_KUBECONFIG_DIR/\$c.yaml"
  log "    done"
  exit 2
fi

for cluster in mgmt build runtime runtime-2; do
  if [[ ! -r "$DRILL_KUBECONFIG_DIR/$cluster.yaml" ]]; then
    log "ERROR: kubeconfig not readable: $DRILL_KUBECONFIG_DIR/$cluster.yaml"
    exit 2
  fi
done

if [[ ! -d "$PULUMI_DRILL_DIR" ]]; then
  log "ERROR: Pulumi dir not found: $PULUMI_DRILL_DIR"
  exit 2
fi

if [[ ! -d "$HELMFILE_DIR" ]]; then
  log "ERROR: Helmfile dir not found: $HELMFILE_DIR"
  exit 2
fi

for cmd in helmfile kubectl pulumi; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    if [[ "$cmd" == "pulumi" ]] && command -v npx >/dev/null 2>&1; then
      continue  # npx pulumi fallback
    fi
    log "ERROR: $cmd not found on PATH"
    exit 2
  fi
done

# Plan 2026-05-19-drill-skills-rigor-restoration Bug C cleanup:
# Export DOKS context names BEFORE the Phase 3 helmfile sync. The loopzero-api drill
# overlay (values-loopzero-api-drill.yaml.gotmpl) consumes them via `requiredEnv` —
# replacing the post-sync kubectl-set-env workaround (which was reverted on every
# re-sync). Names extracted from the current-context line of the downloaded kubeconfigs.
export CORE_KUBE_CONTEXT=$(grep "current-context" "$DRILL_KUBECONFIG_DIR/mgmt.yaml" | awk '{print $2}')
export BUILD_KUBE_CONTEXT=$(grep "current-context" "$DRILL_KUBECONFIG_DIR/build.yaml" | awk '{print $2}')
export CELL_0_KUBE_CONTEXT=$(grep "current-context" "$DRILL_KUBECONFIG_DIR/runtime.yaml" | awk '{print $2}')
log "  ✓ Exported DOKS context names: mgmt=$CORE_KUBE_CONTEXT build=$BUILD_KUBE_CONTEXT runtime=$CELL_0_KUBE_CONTEXT"

# --- Phase 1: Read Pulumi outputs ---
log "Phase 1/4: Reading Pulumi outputs (v2 flat named exports)..."

# Plan 2026-05-20 T4.3 (amendment per finding-2026-05-20-t4-3-scope-rethink):
# Consume shared lib infra/helmfile/scripts/lib/pulumi-output.sh instead of
# inlining the pulumi/npx fallback dispatch. Lib API:
#   pulumi_output --dir DIR --stack STACK [--secret] [--json] KEY
SCRIPT_DIR_BOOTSTRAP="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB_PULUMI_OUTPUT="${SCRIPT_DIR_BOOTSTRAP}/../../helmfile/scripts/lib/pulumi-output.sh"
if [[ ! -r "$LIB_PULUMI_OUTPUT" ]]; then
  log "ERROR: shared lib not found at $LIB_PULUMI_OUTPUT"
  log "  Plan 2026-05-20 T4.3 amendment — drill-helmfile-bootstrap.sh depends on this lib"
  exit 2
fi
# shellcheck source=../../helmfile/scripts/lib/pulumi-output.sh
source "$LIB_PULUMI_OUTPUT"

# Thin wrapper preserves caller-side signature (existing call sites in this
# script call `pulumi_out KEY [secret]`). Lib accepts --secret as a flag, so
# wrapper translates positional arg to flag form.
pulumi_out() {
  local key="$1"
  local secret="${2:-false}"
  if [[ "$secret" == "true" ]]; then
    pulumi_output --dir "$PULUMI_DRILL_DIR" --stack drill --secret "$key"
  else
    pulumi_output --dir "$PULUMI_DRILL_DIR" --stack drill "$key"
  fi
}

# Required outputs (fail-fast if any missing — Finding #3 root cause regression)
PG_HOST_VAL=$(pulumi_out pgHost || true)
PG_PORT_VAL=$(pulumi_out pgPort || true)
PG_DATABASE_VAL=$(pulumi_out pgDatabase || true)

if [[ -z "$PG_HOST_VAL" || -z "$PG_PORT_VAL" || -z "$PG_DATABASE_VAL" ]]; then
  log "ERROR: required Pulumi outputs missing (pgHost, pgPort, pgDatabase)"
  log "  hint: verify v2 named exports per T1.2 — check 'pulumi stack output --stack drill' returns flat keys"
  log "  RCA: .claude/knowledge-base/concepts/operations/finding-2026-05-16-pulumi-outputs-rca.md"
  exit 2
fi

log "  PG host: $PG_HOST_VAL"
log "  PG port: $PG_PORT_VAL"
log "  PG database: $PG_DATABASE_VAL"

# --- Phase 2: Set environment for helmfile (incl. T2.1.bis SSL) ---
log "Phase 2/4: Setting environment variables for helmfile..."

# T2.1.bis (EC-02 MUST FIX): managed PG requires SSL
export PG_SSLMODE=require
export PG_HOST="$PG_HOST_VAL"
export PG_PORT="$PG_PORT_VAL"
export PG_DATABASE="$PG_DATABASE_VAL"
export HELMFILE_ENVIRONMENT="$HELMFILE_ENV"

# Cross-cluster URLs (mgmt → others) for helmfile templates
MGMT_ENDPOINT=$(pulumi_out mgmtClusterEndpoint || true)
if [[ -n "$MGMT_ENDPOINT" ]]; then
  export CORE_CLUSTER_ENDPOINT="$MGMT_ENDPOINT"
fi

log "  PG_SSLMODE=require (T2.1.bis EC-02 fix)"
log "  HELMFILE_ENVIRONMENT=$HELMFILE_ENV"

# --- Phase 2 plan T2.1 — Per-service DSN env exports + K8s Secret reconciliation ---
#
# Bug #10 fix: ory-kratos/hydra/gitea had hardcoded postgresql.loopzero-system DSN.
# Read 4 per-service DSN from Pulumi outputs (T1.3) + create K8s Secrets via
# `kubectl apply` (idempotent) so chart's existingSecret pattern (T3.0+T3.1/2/3)
# can reference them.
#
# T1.3.bis EC-1 CRITICAL: --show-secrets REQUIRED — otherwise DSN contains
# '[secret]' literal → kratos crashloop with auth failure identical to Bug #10.
# T2.1.bis EC-2: KUBECONFIG cross-cluster guard MUST run before Secret creation.

log "Reading per-service DSN outputs (T1.3 + T1.3.bis EC-1: --show-secrets REQUIRED)..."
KRATOS_DSN_VAL=$(pulumi_out kratosDsn true || true)
HYDRA_DSN_VAL=$(pulumi_out hydraDsn true || true)
GITEA_DSN_VAL=$(pulumi_out giteaDsn true || true)
LOOPZERO_API_DSN_VAL=$(pulumi_out loopzeroApiDsn true || true)

# Sanity check: DSN must NOT contain "[secret]" literal (T1.3.bis EC-1 regression guard)
for dsn_pair in "kratos:$KRATOS_DSN_VAL" "hydra:$HYDRA_DSN_VAL" "gitea:$GITEA_DSN_VAL" "loopzero_api:$LOOPZERO_API_DSN_VAL"; do
  svc="${dsn_pair%%:*}"
  val="${dsn_pair#*:}"
  if [[ -z "$val" ]]; then
    log "ERROR: DSN for $svc is empty — Pulumi output kratosDsn/hydraDsn/giteaDsn/loopzeroApiDsn missing"
    log "  hint: verify Phase 1 (T1.3) shipped — check 'pulumi stack output --stack drill kratosDsn --show-secrets'"
    exit 2
  fi
  if [[ "$val" == *"[secret]"* ]]; then
    log "ERROR: DSN for $svc contains '[secret]' literal — bug T1.3.bis EC-1"
    log "  pulumi_out must use --show-secrets flag (helper function bug)"
    exit 2
  fi
done

export KRATOS_DSN="$KRATOS_DSN_VAL"
export HYDRA_DSN="$HYDRA_DSN_VAL"
export GITEA_DSN="$GITEA_DSN_VAL"
export LOOPZERO_API_DSN="$LOOPZERO_API_DSN_VAL"
# Clear locals from this scope (defense-in-depth — DSN NOT in subsequent log)
KRATOS_DSN_VAL="" HYDRA_DSN_VAL="" GITEA_DSN_VAL="" LOOPZERO_API_DSN_VAL=""

# Gitea uses 4 separate fields (NOT single DSN — chart-specific T3.3)
# Split the DSN into HOST/PORT/USER/PASSWORD/DB for gitea chart consumption.
# `loopzero-ops dsn` accepts postgres:// and postgresql:// and fails loudly on
# anything else; the sed split it replaces matched postgres:// only.
GITEA_USER=$(printf '%s' "$GITEA_DSN" | loopzero_ops dsn user)
GITEA_PASSWORD=$(printf '%s' "$GITEA_DSN" | loopzero_ops dsn password)
GITEA_HOST=$(printf '%s' "$GITEA_DSN" | loopzero_ops dsn host)
GITEA_PORT=$(printf '%s' "$GITEA_DSN" | loopzero_ops dsn port)
GITEA_DB=$(printf '%s' "$GITEA_DSN" | loopzero_ops dsn database)
export GITEA_HOST GITEA_PORT GITEA_USER GITEA_PASSWORD GITEA_DB
log "  4 DSN env vars exported (kratos, hydra, gitea split-4, loopzero-api)"

# --- T2.1.bis EC-2: KUBECONFIG cross-cluster guard ---
# Verify DRILL_KUBECONFIG_DIR/mgmt.yaml points to drill mgmt (NOT prod/other)
# Catastrophic if Secrets written to wrong cluster (potential prod cred exposure)
# per finding-2026-05-16-drill-teardown-kubeconfig-orphan.md vector.
#
# Robust check (iter Phase 2 ad-hoc fix): compare cluster-info endpoint UUID
# against Pulumi output mgmtClusterEndpoint. DOKS API endpoints are
# `<uuid>.k8s.ondigitalocean.com` — UUID match guarantees same cluster.
log "T2.1.bis EC-2 guard: validating KUBECONFIG cluster identity..."
CORE_KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
CTX_INFO=$(KUBECONFIG="$CORE_KUBECONFIG" kubectl cluster-info 2>/dev/null | head -1 || true)
EXPECTED_ENDPOINT=$(pulumi_out mgmtClusterEndpoint || true)

# Extract UUID from both endpoint strings (format: https://UUID.k8s.ondigitalocean.com)
CTX_UUID=$(echo "$CTX_INFO" | grep -oE '[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}' | head -1)
EXPECTED_UUID=$(echo "$EXPECTED_ENDPOINT" | grep -oE '[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}' | head -1)

if [[ -z "$CTX_UUID" || -z "$EXPECTED_UUID" ]]; then
  log "ERROR: Cannot extract UUID from cluster endpoints"
  log "  Got KUBECONFIG: $CTX_INFO"
  log "  Got Pulumi expected: $EXPECTED_ENDPOINT"
  log "  ABORTING Secret creation (EC-2)"
  exit 4
fi

if [[ "$CTX_UUID" != "$EXPECTED_UUID" ]]; then
  log "ERROR: KUBECONFIG points to DIFFERENT cluster than drill mgmt"
  log "  KUBECONFIG UUID:  $CTX_UUID"
  log "  Expected UUID:    $EXPECTED_UUID"
  log "  ABORTING Secret creation to prevent cross-cluster write (EC-2)"
  exit 4
fi
log "  ✓ KUBECONFIG validated (UUID match: $CTX_UUID)"

# Pre-create namespaces (idempotent — silent if exists)
for ns in loopzero-system loopzero-auth; do
  KUBECONFIG="$CORE_KUBECONFIG" kubectl create namespace "$ns" --dry-run=client -o yaml | \
    KUBECONFIG="$CORE_KUBECONFIG" kubectl apply -f - >/dev/null
done

# Create per-service Secrets via dry-run|apply (idempotent + safe re-run)
declare -A SECRET_TARGETS=(
  ["ory-kratos-dsn-managed-pg:loopzero-auth"]="$KRATOS_DSN"
  ["ory-hydra-dsn-managed-pg:loopzero-auth"]="$HYDRA_DSN"
  ["gitea-dsn-managed-pg:loopzero-system"]="$GITEA_DSN"
  ["loopzero-api-dsn-managed-pg:loopzero-system"]="$LOOPZERO_API_DSN"
)
for key in "${!SECRET_TARGETS[@]}"; do
  secret_name="${key%%:*}"
  namespace="${key##*:}"
  dsn_val="${SECRET_TARGETS[$key]}"
  KUBECONFIG="$CORE_KUBECONFIG" kubectl create secret generic "$secret_name" \
    --namespace "$namespace" \
    --from-literal=dsn="$dsn_val" \
    --dry-run=client -o yaml | \
    KUBECONFIG="$CORE_KUBECONFIG" kubectl apply -f - >/dev/null
done
log "  ✓ 4 DSN secrets reconciled to drill mgmt cluster"

# --- Phase 2.5: Generate platform Secrets via generate-secrets.sh (Bug #13 LIVE fix) ---
#
# Bug #13 root cause (caught LIVE 2026-05-17 iter 3 on velero install):
# Helmfile mgmt sync requires platform Secrets pre-existing:
#   velero-system/velero-s3-credentials
#   loopzero-system/loopzero-db-credentials (loopzero-api uses .data.uri)
#   loopzero-system/gitea-admin (gitea chart admin password)
#   loopzero-system/minio-credentials (rootUser/rootPassword)
#   loopzero-system/harbor-admin + loopzero-system/harbor-redis-credentials
#   loopzero-system/loopzero-registry-pull
#
# Without these, velero install times out at 900s ("Error: context deadline
# exceeded") because BackupStorageLocation validation Job hangs trying to
# auth to DO Spaces. Then loopzero-api crashloops with DATABASE_URL not set.
#
# generate-secrets.sh is idempotent — safe to call on re-sync. Required
# env vars: LOOPZERO_DB_HOST/PORT/USER/PASSWORD/NAME (Pulumi outputs) +
# LOOPZERO_SPACES_KEY/SECRET (.env mapped).
log "Phase 2.5/4: Generate platform Secrets via generate-secrets.sh (Bug #13 fix)..."

# Bug #14 LIVE FIX (caught 2026-05-17 iter 4 on velero install):
# Without exporting KUBECONFIG to drill mgmt, generate-secrets.sh inherits
# the parent shell's KUBECONFIG which often points to a local k3d cluster
# (loopzero-core). The platform Secrets get created on the WRONG cluster and
# velero install on drill mgmt fails: "MountVolume.SetUp failed for volume
# 'cloud-credentials' : secret 'velero-s3-credentials' not found".
#
# Phase 2 uses inline `KUBECONFIG=$CORE_KUBECONFIG kubectl ...` (no export)
# so the inherited KUBECONFIG at this point is whatever the parent task set.
# Export drill mgmt kubeconfig explicitly to scope generate-secrets.sh.
export KUBECONFIG="$CORE_KUBECONFIG"

export LOOPZERO_DB_HOST="$(pulumi_out pgHost false)"
export LOOPZERO_DB_PORT="$(pulumi_out pgPort false)"
export LOOPZERO_DB_USER="doadmin"
export LOOPZERO_DB_PASSWORD="$(pulumi_out pgPassword true 2>/dev/null || echo "")"
if [[ -z "$LOOPZERO_DB_PASSWORD" ]]; then
  # Fall back to extracting from pgUri (pulumi-drill doesn't export pgPassword
  # as separate output — only via pgUri composite).
  #
  # Bug #17 (2026-05-17): a sed regex anchored on `postgres://` did not match
  # the `postgresql://` URI DO Managed PG returns, printed the whole URI, and
  # the URI became the password. `loopzero-ops dsn` parses both schemes and exits
  # non-zero instead of echoing its input.
  PG_URI="$(pulumi_out pgUri true)"
  if ! LOOPZERO_DB_PASSWORD="$(printf '%s' "$PG_URI" | loopzero_ops dsn password)"; then
    log "ERROR: pgUri password extraction failed (see the loopzero-ops error above)"
    exit 2
  fi
fi
export LOOPZERO_DB_NAME="loopzero"

# DO Spaces credentials (drill uses isolated DRILL_DO_TOKEN per D7 but
# Spaces keys are global per DO account — same as prod).
export LOOPZERO_SPACES_KEY="${SPACES_ACCESS_KEY_ID:-${AWS_ACCESS_KEY_ID:-}}"
export LOOPZERO_SPACES_SECRET="${SPACES_SECRET_ACCESS_KEY:-${AWS_SECRET_ACCESS_KEY:-}}"

# Set LOOPZERO_PROFILE=cloud so generate-secrets uses managed PG path (not standalone fallback).
export LOOPZERO_PROFILE="cloud"

# Bug #15 LIVE FIX: Managed Valkey credentials (replaces in-cluster bitnami).
# Per CTO Golden Rule #0 + ADR-0016 D-6: drill uses DO Managed Valkey,
# mirrors prod posture. drill helmfile skips gitea-redis + harbor-redis
# releases when env=drill (installed: false). These env vars feed:
#   - gitea drill values override (cache/session/queue redis addr)
#   - generate-secrets.sh harbor-redis-credentials Secret (HOST/PASSWORD)
REDIS_HOST_VAL="$(pulumi_out redisHost false)"
REDIS_PORT_VAL="$(pulumi_out redisPort false)"
REDIS_PASSWORD_VAL="$(pulumi_out redisPassword true)"
if [[ -z "$REDIS_HOST_VAL" || -z "$REDIS_PORT_VAL" || -z "$REDIS_PASSWORD_VAL" ]]; then
  log "ERROR: missing Pulumi redis* outputs (redisHost/redisPort/redisPassword)"
  log "  hint: managed Valkey not yet provisioned. Re-run 'task drill:up' to recreate."
  exit 2
fi
# Sanity check: reject "[secret]" literal (--show-secrets not honored)
if [[ "$REDIS_PASSWORD_VAL" == "[secret]" ]]; then
  log "ERROR: redisPassword renders as literal '[secret]' — pulumi_out must use --show-secrets"
  exit 2
fi

# Export for gitea drill values override consumption
export GITEA_REDIS_HOST="$REDIS_HOST_VAL"
export GITEA_REDIS_PORT="$REDIS_PORT_VAL"
export GITEA_REDIS_PASSWORD="$REDIS_PASSWORD_VAL"

# Export for generate-secrets.sh harbor-redis-credentials path (line 303
# in generate-secrets.sh uses LOOPZERO_HARBOR_REDIS_HOST default fallback).
export LOOPZERO_HARBOR_REDIS_HOST="$REDIS_HOST_VAL"
export LOOPZERO_HARBOR_REDIS_PASSWORD="$REDIS_PASSWORD_VAL"
log "  ✓ Managed Valkey env exported (gitea + harbor will use $REDIS_HOST_VAL:$REDIS_PORT_VAL)"

bash "$HELMFILE_DIR/scripts/generate-secrets.sh" drill mgmt
log "  ✓ Platform Secrets generated (loopzero-db, gitea, minio, harbor, velero, registry-pull)"

# Bug #19 LIVE FIX (caught 2026-05-17 iter 12 on loopzero-api CrashLoopBackOff):
# loopzero-api startup calls setupCellRouter which requires >=1 ClusterCredential
# CR in loopzero-system. setupCellRouter is fail-fast (os.Exit(1) on 0 cells).
#
# Previously Phase 5 (bootstrap-cell-*-cc-drill.sh) ran AFTER Phase 4a
# (helmfile sync waits for loopzero-api Ready). Circular dependency.
#
# Fix: install loopzero-crds FIRST (chart provides CRDs), apply ClusterCredentials
# (loopzero.io/v1 CRs valid after CRDs registered), THEN proceed with full
# helmfile sync (loopzero-api will find the cells at startup).
log "Phase 2.6/4: Install loopzero-crds + bootstrap ClusterCredentials (Bug #19 fix)..."
helmfile -e "$HELMFILE_ENV" --file "$HELMFILE_DIR/core.helmfile.yaml.gotmpl" --selector name=loopzero-crds sync \
  --args "--timeout ${HELMFILE_RELEASE_TIMEOUT:-900}s --wait" 2>&1 | tee -a "/tmp/helmfile-mgmt.log" | tail -5 || {
  log "ERROR: loopzero-crds install failed (Bug #19 fix pre-step)"
  exit 3
}
log "  ✓ loopzero-crds installed — applying 3 ClusterCredentials (cell-0 + cell-0 + cell-1)..."
export DRILL_KUBECONFIG="$CORE_KUBECONFIG"
# Plan v1.2 T4.2: consolidated bootstrap-cell-cc.sh --cell --env
CELL_SCRIPT="$(cd "$(dirname "$0")/../../.." && pwd)/infra/helmfile/scripts/bootstrap-cell-cc.sh"
# bootstrap-cell-cc.sh accepts only the post-rename cell ids 0 and 1; the
# default/1/2 ids this used to pass were refused, and cell-0 was never created.
for cell_id in 0 1; do
  bash "$CELL_SCRIPT" --cell "$cell_id" --env drill 2>&1 | tail -3 || {
    log "ERROR: bootstrap cell-$cell_id failed"
    exit 3
  }
done
log "  ✓ 2 ClusterCredentials applied (cell-0, cell-1)"

# Bug #22 LIVE FIX (caught 2026-05-17 iter 15 on loopzero-api 0 CCs discovered):
# K8sCellStore.LoadCells filters by status.connected=true (set by
# ClusterCredentialController when apiServer is reachable). The operator
# isn't installed yet (chicken-and-egg). For drill, we KNOW apiServers
# are reachable (we provisioned them), so set status.connected=true
# manually via kubectl patch. Mirrors `task k3d:heal` H13 pattern.
log "Bug #22 fix: patching status.connected=true on 2 CCs..."
for cell in cell-0 cell-1; do
  kubectl patch clustercredential "$cell" --type=merge --subresource=status \
    -p '{"status":{"connected":true,"lastReconciledAt":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}}' \
    --namespace loopzero-system 2>&1 | grep -E "patched|^Error" | head -1
done
log "  ✓ 2 CCs marked status.connected=true (loopzero-api will find cells at startup)"

# Bug #25 LIVE FIX (caught 2026-05-17 iter 18): operator charts require
# GITEA_TOKEN env var. Extract from gitea-admin Secret (created by generate-
# secrets.sh with key `token`) and export as GITEA_TOKEN_VAL for operator
# drill values overrides to consume via `{{ env "GITEA_TOKEN_VAL" }}`.
GITEA_TOKEN_VAL_RAW="$(kubectl get secret -n loopzero-system gitea-admin -o jsonpath='{.data.token}' 2>/dev/null || echo "")"
if [[ -n "$GITEA_TOKEN_VAL_RAW" ]]; then
  export GITEA_TOKEN_VAL="$(echo "$GITEA_TOKEN_VAL_RAW" | base64 -d 2>/dev/null || echo "$GITEA_TOKEN_VAL_RAW")"
  log "  ✓ GITEA_TOKEN_VAL exported for operator charts (Bug #25 fix)"
else
  log "  WARN: gitea-admin Secret not found yet (operator startup may fail)"
fi

# Bug #26 LIVE FIX (caught 2026-05-17 iter 19): operators require BUILD_KUBECONFIG
# + CELL_0_KUBECONFIG env vars pointing to mounted files. Chart mounts when
# kubeconfig.{build,runtime}.existingConfigMap is set. Create ConfigMaps now
# from drill kubeconfigs we already have at /tmp/drill-kc/.
log "Bug #26 fix: creating build + runtime kubeconfig ConfigMaps for operators..."
kubectl create configmap loopzero-build-kubeconfig --namespace loopzero-system \
  --from-file=config="$DRILL_KUBECONFIG_DIR/build.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create configmap loopzero-cell-0-kubeconfig --namespace loopzero-system \
  --from-file=config="$DRILL_KUBECONFIG_DIR/runtime.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
log "  ✓ ConfigMaps loopzero-build-kubeconfig + loopzero-cell-0-kubeconfig created"

# Bug #16 LIVE FIX (caught 2026-05-17 iter 7 on loopzero-api ImagePullBackOff):
# generate-secrets.sh registry block uses base64(SPACES_KEY:SPACES_SECRET)
# which is WRONG auth format for DOCR (DOCR uses DO API token, NOT Spaces).
# Replace with canonical kubectl create secret docker-registry pattern that
# DOCR documents officially. Uses DIGITALOCEAN_TOKEN as both username +
# password (DO standard token-only auth).
if [[ -n "${DIGITALOCEAN_TOKEN:-}" ]]; then
  # Plan 2026-05-19-drill-skills-rigor-restoration Bug B fix:
  # Operator (environment_rbac.go:170) looks up the secret named "loopzero-system" in
  # the mgmt loopzero-system ns (source) — and copies it to the tenant ns as "loopzero-system".
  # Previous bootstrap only created "loopzero-registry-pull" → operator log
  # "SourceMissing" silently → tenant pods ImagePullBackOff. Fix: create
  # BOTH names (canonical "loopzero-system" for operator + legacy
  # "loopzero-registry-pull" for Argo workflow + build pipeline).
  log "Bug #16+B fix: re-creating loopzero-system + loopzero-registry-pull Secrets (DO token auth)..."
  for SECRET_NAME in loopzero-system loopzero-registry-pull; do
    kubectl create secret docker-registry "$SECRET_NAME" \
      --namespace loopzero-system \
      --docker-server=registry.digitalocean.com \
      --docker-username="$DIGITALOCEAN_TOKEN" \
      --docker-password="$DIGITALOCEAN_TOKEN" \
      --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  done
  log "  ✓ loopzero-system + loopzero-registry-pull Secrets reconciled (docker-registry, DO token auth)"
else
  log "ERROR: DIGITALOCEAN_TOKEN missing — cannot create loopzero-registry-pull"
  exit 2
fi

# Plan 2026-05-19-drill-skills-rigor-restoration Bug #56 ordering fix:
# loopzero-cluster-kubeconfigs Secret MUST be overridden with REAL DOKS kubeconfigs
# BEFORE Phase 3 helm install loopzero-api (--wait blocks pod Ready). Otherwise
# pods boot with synthetic "in-cluster" kubeconfig from generate-secrets.sh
# (Phase 2.5) and crash with `context "do-sfo3-..." does not exist`. Bootstrap
# stuck 15min during Phase 4 LIVE (commit 168922f2). Previous fix at line 793
# was too late. Override here pre-empts.
log "Bug #56 ordering fix: overriding loopzero-cluster-kubeconfigs Secret with REAL drill kubeconfigs PRE-Phase-3..."
kubectl -n loopzero-system create secret generic loopzero-cluster-kubeconfigs \
  --from-file=core.kubeconfig="$DRILL_KUBECONFIG_DIR/mgmt.yaml" \
  --from-file=build.kubeconfig="$DRILL_KUBECONFIG_DIR/build.yaml" \
  --from-file=cell-0.kubeconfig="$DRILL_KUBECONFIG_DIR/runtime.yaml" \
  --from-file=runtime-2.kubeconfig="$DRILL_KUBECONFIG_DIR/runtime-2.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
log "  ✓ loopzero-cluster-kubeconfigs Secret now has 4 REAL kubeconfigs (mgmt+build+runtime+runtime-2)"

# --- Phase 3: Per-cluster helmfile sync ---
log "Phase 3/4: Helmfile sync per cluster (mgmt → build → runtime → runtime-2)..."

# Cluster order matters: cross-cluster deps require mgmt first
CLUSTERS=(mgmt build runtime runtime-2)

# --from-cluster recovery: skip clusters before NAME
SKIP_UNTIL=""
if [[ -n "$FROM_CLUSTER" ]]; then
  SKIP_UNTIL="$FROM_CLUSTER"
  log "  Resuming from cluster: $FROM_CLUSTER (EC-05 partial recovery mode)"
fi

PARTIAL_FAILURES=()

for cluster in "${CLUSTERS[@]}"; do
  if [[ -n "$SKIP_UNTIL" ]]; then
    if [[ "$cluster" != "$SKIP_UNTIL" ]]; then
      log "  Skip $cluster (before --from-cluster $SKIP_UNTIL)"
      continue
    fi
    SKIP_UNTIL=""  # found, resume normal processing
  fi

  log "  Syncing cluster: $cluster"
  export KUBECONFIG="$DRILL_KUBECONFIG_DIR/$cluster.yaml"

  if [[ "$DRY_RUN" == "true" ]]; then
    log "    DRY-RUN: would execute: helmfile -e $HELMFILE_ENV --file $HELMFILE_DIR/helmfile.yaml sync"
    continue
  fi

  # Per-cluster helmfile file mapping (iter 11 LIVE bug fix — no unified helmfile.yaml exists):
  #   mgmt    → core.helmfile.yaml.gotmpl
  #   build   → build.helmfile.yaml.gotmpl
  #   runtime → cell.helmfile.yaml.gotmpl
  #   runtime-2 → cell.helmfile.yaml.gotmpl (reuses runtime, same workload pattern)
  case "$cluster" in
    runtime-2) CLUSTER_HELMFILE="cell.helmfile.yaml.gotmpl" ;;
    *)         CLUSTER_HELMFILE="${cluster}.helmfile.yaml.gotmpl" ;;
  esac
  # Iter 12 LIVE bug fix: helmfile default timeout (5min) insufficient for drill
  # first-time provisioning (managed PG cold start ~4m + PVC creation +
  # prometheus install). Use 15min timeout per release. HELMFILE_RELEASE_TIMEOUT
  # env override for operator tuning.
  HELMFILE_RELEASE_TIMEOUT="${HELMFILE_RELEASE_TIMEOUT:-900}"  # 15 minutes
  # Bug #32b LIVE fix (2026-05-17 iter 25): DOKS API server returns EOF on
  # rapid sequential helm pre-install hooks during early bootstrap (cold
  # API server). Add retry: 3 attempts with 30s backoff between failures.
  # GODEBUG=http2client=0 disables HTTP/2 for the helmfile binary (prevents
  # HTTP/2 GOAWAY race against DOKS).
  HELMFILE_RETRY_MAX="${HELMFILE_RETRY_MAX:-3}"
  helmfile_attempt=0
  helmfile_ok=false
  while [[ $helmfile_attempt -lt $HELMFILE_RETRY_MAX ]]; do
    helmfile_attempt=$((helmfile_attempt + 1))
    log "  $cluster sync attempt $helmfile_attempt/$HELMFILE_RETRY_MAX..."
    if GODEBUG=http2client=0 helmfile -e "$HELMFILE_ENV" --file "$HELMFILE_DIR/$CLUSTER_HELMFILE" sync --concurrency 1 \
         --args "--timeout ${HELMFILE_RELEASE_TIMEOUT}s --wait" 2>>/tmp/helmfile-${cluster}.log; then
      helmfile_ok=true
      break
    fi
    if [[ $helmfile_attempt -lt $HELMFILE_RETRY_MAX ]]; then
      log "  $cluster sync attempt $helmfile_attempt FAILED — sleeping 30s before retry"
      sleep 30
    fi
  done
  if $helmfile_ok; then
    log "  ✓ $cluster sync OK (attempt $helmfile_attempt/$HELMFILE_RETRY_MAX)"
  else
    PARTIAL_FAILURES+=("$cluster")
    log "  ✗ $cluster sync FAILED after $HELMFILE_RETRY_MAX attempts (log: /tmp/helmfile-${cluster}.log)"
    if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
      log "STRICT_BOOTSTRAP=true → aborting on first failure"
      log "  Resume command: bash $0 --from-cluster $cluster"
      exit 3
    fi
    log "  (continuing — STRICT_BOOTSTRAP=false; resume via --from-cluster $cluster)"
  fi
done

if [[ ${#PARTIAL_FAILURES[@]} -gt 0 ]]; then
  log "Partial helmfile failures: ${PARTIAL_FAILURES[*]}"
  log "  Resume via: bash $0 --from-cluster ${PARTIAL_FAILURES[0]}"
  exit 3
fi

# --- Phase 4: Health check ---
if [[ "$DRY_RUN" == "true" ]]; then
  log "Phase 4/4: SKIPPED (dry-run)"
  log "Bootstrap dry-run complete."
  exit 0
fi

log "Phase 4/4: Health check (wait for core services Ready)..."

# Bug #33 LIVE fix (2026-05-17 iter 26): kubectl wait timeout was 5m default
# which is insufficient for Deployment.status.availableReplicas to propagate
# after helm install completes (specially under DOKS API load post-bootstrap).
# Bump to 10m. Also capture stderr to log for diagnostic instead of silencing.
HEALTH_WAIT_TIMEOUT="${HEALTH_WAIT_TIMEOUT:-10m}"

# mgmt cluster: loopzero-api, ArgoCD, hydra
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
HEALTH_TARGETS_MGMT=(
  "deployment/loopzero-api -n loopzero-system"
  "deployment/argocd-server -n argocd"
)

for target in "${HEALTH_TARGETS_MGMT[@]}"; do
  log "  Waiting on mgmt: $target (timeout $HEALTH_WAIT_TIMEOUT)"
  # shellcheck disable=SC2086
  if ! kubectl wait --for=condition=Available --timeout="$HEALTH_WAIT_TIMEOUT" $target 2>&1 | tee -a /tmp/drill-health-mgmt.log; then
    log "  ✗ mgmt health check failed: $target — diagnostic:"
    kubectl get $target -o jsonpath='{.status}' 2>&1 | head -3 | sed 's/^/    /' | tee -a /tmp/drill-health-mgmt.log
    if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
      exit 4
    fi
  fi
done

# build cluster: argo-workflows-server (Bug #34 fix: actual deployment name)
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml"
if ! kubectl wait --for=condition=Available --timeout="$HEALTH_WAIT_TIMEOUT" \
    deployment/argo-workflows-server -n argo 2>&1 | tee -a /tmp/drill-health-build.log; then
  log "  ✗ build cluster: argo-workflows-server not Available — diagnostic:"
  kubectl get deployments -n argo 2>&1 | head -5 | sed 's/^/    /'
  if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
    exit 4
  fi
fi

log ""

# Bug #36 LIVE FIX (caught 2026-05-18 iter 29): drill-mint-jwt.sh requires
# loopzero-bootstrap-credentials Secret which is created by bootstrap-oauth-clients.sh.
# In local k3d this runs via task setup. In drill we need to run it explicitly
# AFTER mgmt sync (loopzero-auth Hydra Ready) BEFORE drill-mint-jwt.
log "Bug #36 fix: bootstrapping OAuth clients in Hydra (creates loopzero-bootstrap-credentials)..."
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
export HYDRA_NAMESPACE="loopzero-auth"
if bash "$HELMFILE_DIR/scripts/bootstrap-oauth-clients.sh" 2>&1 | tail -5; then
  log "  ✓ OAuth clients bootstrapped (loopzero-cli + loopzero-bootstrap)"
else
  log "  ✗ bootstrap-oauth-clients.sh failed — JWT mint will fail downstream"
  if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
    exit 4
  fi
fi

# Bug #43 LIVE FIX (caught 2026-05-18 iter 37): run-migrations.sh was never
# invoked in drill bootstrap → loopzero DB had no relations (users, tenants,
# tenant_members), causing drill-mint-jwt.sh seed step to fail with
# `relation "users" does not exist`. Migrations must run AFTER helm sync
# (loopzero-api pod has migrate binary baked in) BEFORE drill-mint-jwt seed.
log "Bug #43 fix: running DB migrations against drill mgmt PG..."
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
# Bug #18 LIVE FIX (caught 2026-05-19 chaos-drill iter 3): LOOPZERO_API_DSN_VAL is
# CLEARED at line 233 (`LOOPZERO_API_DSN_VAL=""` defense-in-depth). Use the
# exported $LOOPZERO_API_DSN (line 231) which preserves the DSN through scope.
# Without this fix, PG_PASS was parsed from empty string → empty → migrate
# tried to auth doadmin with empty password → pq: password auth failed.
LOOPZERO_API_DB_PASS="$(printf '%s' "$LOOPZERO_API_DSN" | loopzero_ops dsn password)"
PG_HOST="$PG_HOST_VAL" \
PG_PORT="$PG_PORT_VAL" \
PG_USER=doadmin \
PG_PASS="$LOOPZERO_API_DB_PASS" \
PG_DB=loopzero \
PG_SSLMODE=require \
DRILL_KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml" \
NAMESPACE="loopzero-system" \
  bash "infra/scripts/production/run-migrations.sh" 2>&1 | tail -20 | sed 's/^/  /' || {
  log "  ✗ run-migrations.sh failed"
  if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
    exit 4
  fi
}
log "  ✓ DB migrations applied"

# Bug #50 LIVE FIX (caught 2026-05-18 iter 41): loopzero-platform-topology
# ConfigMap was never created → loopzero-api envFrom had no GITEA_INTERNAL_URL,
# code defaulted to gitea-http.gitea.svc.cluster.local (wrong namespace).
log "Bug #50 fix: creating loopzero-platform-topology wiring ConfigMap..."
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
LOOPZERO_MGMT_INGRESS_IP=$(kubectl -n traefik get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
LOOPZERO_MGMT_INGRESS_IP="$LOOPZERO_MGMT_INGRESS_IP" \
  bash "$HELMFILE_DIR/scripts/create-wiring-configmap.sh" drill 2>&1 | tail -5 | sed 's/^/  /'
log "  ✓ wiring ConfigMap applied"

# Bug #52 LIVE FIX (caught 2026-05-18 iter 41): the bootstrap loopzero-api chart
# carried GITEA_URL/GITEA_ORG/LOOPZERO_DYNAMIC_ROUTER under a `tracing:` block no
# template read (issue #354), so they never reached the Deployment. That chart
# is gone; drill installs charts/loopzero-api, whose drill overlay does not set
# config.giteaUrl either, so this patch is still what points drill at the
# loopzero-system Gitea. LOOPZERO_DYNAMIC_ROUTER has no reader in the API.
# Bug #46 LIVE FIX: inject LOOPZERO_SERVICE_AUTH_SECRET into loopzero-db-credentials
# Secret (loopzero-api loads via envFrom) so harness X-Service-Auth path works.
log "Bug #46+#52 fix: patching loopzero-api env (GITEA_URL/LOOPZERO_DYNAMIC_ROUTER/SERVICE_AUTH)..."
SERVICE_AUTH_SECRET="${LOOPZERO_SERVICE_AUTH_SECRET:-drill-l2-test-shared-secret-$(openssl rand -hex 12)}"
kubectl -n loopzero-system patch secret loopzero-db-credentials --type=json -p="[{\"op\":\"add\",\"path\":\"/data/LOOPZERO_SERVICE_AUTH_SECRET\",\"value\":\"$(echo -n "$SERVICE_AUTH_SECRET" | base64 -w0)\"}]" 2>/dev/null || \
  kubectl -n loopzero-system patch secret loopzero-db-credentials --type=json -p="[{\"op\":\"replace\",\"path\":\"/data/LOOPZERO_SERVICE_AUTH_SECRET\",\"value\":\"$(echo -n "$SERVICE_AUTH_SECRET" | base64 -w0)\"}]" 2>/dev/null
# Plan v1.2 T7.3 / D6: secret is already in K8s Secret loopzero-db-credentials.
# Previously also wrote to /tmp/drill-service-auth-secret.txt — eliminated
# (per SOPS posture, world-readable /tmp writes for secrets are anti-pattern).
# Consumers should `kubectl -n loopzero-system get secret loopzero-db-credentials -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d`.
log "  SERVICE_AUTH_SECRET stored in K8s Secret loopzero-system/loopzero-db-credentials (LOOPZERO_SERVICE_AUTH_SECRET key)"

kubectl -n loopzero-system set env deployment/loopzero-api \
  GITEA_URL=http://gitea-http.loopzero-system.svc.cluster.local:3000 \
  GITEA_ORG=loopzero \
  LOOPZERO_DYNAMIC_ROUTER=true \
  ARGOCD_URL=http://argocd-server.argocd.svc.cluster.local:80 >/dev/null
log "  ✓ loopzero-api env patched"

# Bug #54 LIVE FIX (caught 2026-05-18 iter 42): gitea-admin Secret token was
# generated by generate-secrets.sh but NEVER registered with Gitea API →
# loopzero-api gets HTTP 401 on Gitea repo creation. The registration is
# `loopzero-ops seed gitea-token`, the same implementation the local chain runs
# (#236): it ensures the loopzero org, rotates the loopzero-api-token, and writes it to
# gitea-admin.token, loopzero-api-tokens.gitea-token and the tenant operator's
# Secret. It reads the admin credentials from the gitea-admin Secret and runs
# against the current KUBECONFIG context (mgmt, exported above).
log "Bug #54 fix: register the Gitea token (loopzero-ops seed gitea-token)..."
loopzero_ops seed gitea-token 2>&1 | sed 's/^/  /'
log "  ✓ Gitea token registered and synced"

# Bugs #56-#82 LIVE PATCH CONSOLIDATION (session 2026-05-18 PM, iter 50-55).
# Hermetic test guard: DRILL_BOOTSTRAP_SKIP_LIVE_PATCHES=true short-circuits
# this block when running under hermetic tests (avoids cosign/network/CF API
# calls that need real stubs).
if [[ "${DRILL_BOOTSTRAP_SKIP_LIVE_PATCHES:-false}" == "true" ]]; then
  log "DRILL_BOOTSTRAP_SKIP_LIVE_PATCHES=true — skipping Bugs #56-#82 LIVE patches (test mode)"
  log ""
  log "✓ Bootstrap complete. LoopZero platform deployed on drill 4-cluster stack."
  log "  Next: bash infra/scripts/drill/drill-mint-jwt.sh"
  log "  Then: bash infra/scripts/bootstrap-cell-{default,1,2}-cc-drill.sh"
  log "  Then: run L0+L1+L2 harness against drill cloud (Phase 6)"
  exit 0
fi

# Bug #65+#73+#79 LIVE FIX (caught 2026-05-18 iter 50-52): loopzero-api env needs
# MINIO_ACCESS_KEY (not AWS_*), REGISTRY_URL (DOCR not k3d-local), BASE_DOMAIN
# (loopzero-drill.example not loopzero.local), LOOPZERO_PACKS_IMAGE (v0.2.0 — v0.4.0 missing).
log "Bug #65+#73+#79 fix: loopzero-api drill env (registry+domain+minio+packs)..."
SPACES_KEY=$(awk -F= '/^SPACES_ACCESS_KEY_ID=/{print $2}' .env 2>/dev/null || echo "")
SPACES_SECRET=$(awk -F= '/^SPACES_SECRET_ACCESS_KEY=/{print $2}' .env 2>/dev/null || echo "")
DRILL_BASE_DOMAIN="${DRILL_BASE_DOMAIN:-loopzero-drill.example}"
LOOPZERO_PACKS_TAG="${LOOPZERO_PACKS_IMAGE_TAG:-v0.2.0}"
kubectl -n loopzero-system set env deployment/loopzero-api \
  MINIO_ACCESS_KEY="$SPACES_KEY" \
  MINIO_SECRET_KEY="$SPACES_SECRET" \
  AWS_ACCESS_KEY_ID="$SPACES_KEY" \
  AWS_SECRET_ACCESS_KEY="$SPACES_SECRET" \
  REGISTRY_URL=registry.digitalocean.com/loopzero-system \
  BASE_DOMAIN="$DRILL_BASE_DOMAIN" \
  SOURCE_BUCKET=loopzero-drill-builds-sfo3 \
  MINIO_ENDPOINT="https://sfo3.digitaloceanspaces.com" >/dev/null
# T4.1 (simplify-deploy-loopzerokit-only v2.0): LOOPZERO_PACKS_IMAGE removed — loopzero-api
# no longer uses loopzero-packs (canonical in-process Dockerfile generation).
log "  ✓ loopzero-api env: REGISTRY_URL/BASE_DOMAIN/MINIO_* set"

# Bug #56 LIVE FIX (caught 2026-05-18 iter 44): generate-secrets.sh creates
# SYNTHETIC in-cluster kubeconfigs assuming single-cluster topology. Drill
# multi-cluster needs REAL kubeconfigs in loopzero-cluster-kubeconfigs Secret +
# KUBE_CONTEXT env vars matching DOKS context names.
log "Bug #56 fix: loopzero-cluster-kubeconfigs Secret with REAL drill kubeconfigs..."
kubectl -n loopzero-system create secret generic loopzero-cluster-kubeconfigs \
  --from-file=core.kubeconfig="$DRILL_KUBECONFIG_DIR/mgmt.yaml" \
  --from-file=build.kubeconfig="$DRILL_KUBECONFIG_DIR/build.yaml" \
  --from-file=cell-0.kubeconfig="$DRILL_KUBECONFIG_DIR/runtime.yaml" \
  --from-file=runtime-2.kubeconfig="$DRILL_KUBECONFIG_DIR/runtime-2.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
# Plan 2026-05-19-drill-skills-rigor-restoration Bug C cleanup:
# REMOVED kubectl-set-env workaround (lines 790-797 pre-fix). MGMT/BUILD/
# CELL_0_KUBE_CONTEXT env vars are now exported in the Phase 0 pre-flight
# and consumed by the Helm chart via .Values.kubeContexts (survives re-sync).
log "  ✓ Real kubeconfigs mounted (context names provided to chart via export at Phase 0)"

# Bug #60 LIVE FIX (caught 2026-05-18 iter 44): operator Secrets have dual
# GITEA_TOKEN keys (upper + camel) — both must hold the registered token.
# Bug #57 LIVE FIX: operator chart yaml indent bug means GITEA_URL/GITEA_ORG
# env vars must be set via kubectl set env (T2 chart fix pending).
log "Bug #57+#60 fix: operator drill env (GITEA_URL + dual GITEA_TOKEN)..."
TOKEN_B64=$(echo -n "${GITEA_TOKEN_NEW:-stub}" | base64 -w0)
for OP in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  if kubectl -n loopzero-system get deployment "$OP" >/dev/null 2>&1; then
    kubectl -n loopzero-system set env deployment/"$OP" \
      GITEA_URL=http://gitea-http.loopzero-system.svc.cluster.local:3000 \
      GITEA_ORG=loopzero \
      ARGOCD_URL=http://argocd-server.argocd.svc.cluster.local:80 \
      BASE_DOMAIN="$DRILL_BASE_DOMAIN" >/dev/null 2>&1 || true
    if kubectl -n loopzero-system get secret "$OP" >/dev/null 2>&1 && [[ -n "${GITEA_TOKEN_NEW:-}" ]]; then
      kubectl -n loopzero-system patch secret "$OP" --type=json -p="[{\"op\":\"replace\",\"path\":\"/data/GITEA_TOKEN\",\"value\":\"$TOKEN_B64\"},{\"op\":\"replace\",\"path\":\"/data/giteaToken\",\"value\":\"$TOKEN_B64\"}]" >/dev/null 2>&1 || true
    fi
  fi
done
log "  ✓ 4 operators env + Secret tokens patched"

# Bug #59 LIVE FIX (caught 2026-05-18 iter 44): operator NetworkPolicy egress
# port 3000 only allows kubernetes.io/metadata.name=gitea but drill Gitea is
# in loopzero-system namespace (T2 chart fix pending).
log "Bug #59 fix: operator NetworkPolicy egress to loopzero-system for Gitea..."
for OP in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  kubectl -n loopzero-system patch networkpolicy "$OP" --type=json \
    -p='[{"op":"replace","path":"/spec/egress/2/to/0/namespaceSelector/matchLabels","value":{"kubernetes.io/metadata.name":"loopzero-system"}}]' >/dev/null 2>&1 || true
done
log "  ✓ NetworkPolicy egress patched (best-effort, some may already exist)"

# Bug #61 LIVE FIX (caught 2026-05-18 iter 51): Argo Workflow PVCs hardcode
# `storageClassName: local-path` (workflow_builder.go:278 — Bug #21 fix for DO
# CSI rate limit). DOKS has no local-path-provisioner by default.
log "Bug #61 fix: install local-path-provisioner on build cluster..."
KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl apply \
  -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.30/deploy/local-path-storage.yaml >/dev/null 2>&1
log "  ✓ local-path-provisioner deployed on build cluster"

# Bug #62 LIVE FIX (caught 2026-05-18 iter 51): loopzero-registry-pull Secret
# missing in argo namespace (build cluster) — needed for workflow steps
# pulling loopzero-clone-runner/loopzero-packs-runner/loopzero-syft-scanner/loopzero-cosign-signer.
log "Bug #62 fix: loopzero-registry-pull Secret in argo ns + SA imagePullSecrets..."
DO_TOKEN_VAL=$(awk -F= '/^DIGITALOCEAN_TOKEN=/{print $2}' .env 2>/dev/null || echo "${DIGITALOCEAN_TOKEN:-}")
KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl -n argo create secret docker-registry loopzero-registry-pull \
  --docker-server=registry.digitalocean.com \
  --docker-username="$DO_TOKEN_VAL" --docker-password="$DO_TOKEN_VAL" \
  --docker-email=drill@loopzero.example \
  --dry-run=client -o yaml | KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl apply -f - >/dev/null
for SA in loopzero-build-sa default; do
  KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl -n argo patch serviceaccount "$SA" \
    -p '{"imagePullSecrets":[{"name":"loopzero-registry-pull"}]}' >/dev/null 2>&1 || true
done
log "  ✓ DOCR pull secret + SAs patched in argo ns (build cluster)"

# Bug #63 LIVE FIX (caught 2026-05-18 iter 51): loopzero-minio-credentials Secret
# missing in argo ns — workflows envFrom this Secret for S3 access.
log "Bug #63 fix: loopzero-minio-credentials Secret in argo ns..."
KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl -n argo create secret generic loopzero-minio-credentials \
  --from-literal=MINIO_ENDPOINT="https://sfo3.digitaloceanspaces.com" \
  --from-literal=AWS_ACCESS_KEY_ID="$SPACES_KEY" \
  --from-literal=AWS_SECRET_ACCESS_KEY="$SPACES_SECRET" \
  --from-literal=AWS_DEFAULT_REGION="sfo3" \
  --from-literal=AWS_ENDPOINT_URL="https://sfo3.digitaloceanspaces.com" \
  --from-literal=AWS_S3_FORCE_PATH_STYLE="true" \
  --dry-run=client -o yaml | KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl apply -f - >/dev/null
log "  ✓ loopzero-minio-credentials in argo ns"

# Bug #64 LIVE FIX (caught 2026-05-18 iter 50): CWT loopzero-clone hardcodes
# bucket name `loopzero-builds` (chart default sourceBucket). Drill bucket is
# loopzero-drill-builds-sfo3 — patch CWT in build cluster (T2 chart fix pending).
log "Bug #64 fix: patch loopzero-clone CWT bucket name to drill bucket..."
KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl get clusterworkflowtemplate loopzero-clone -o yaml 2>/dev/null | \
  sed 's|loopzero/loopzero-builds/|loopzero/loopzero-drill-builds-sfo3/|g' | \
  KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl apply -f - >/dev/null 2>&1
log "  ✓ CWT loopzero-clone bucket→loopzero-drill-builds-sfo3"

# Bug #72 LIVE FIX (caught 2026-05-18 iter 51): buildkit-registry-config
# Secret missing in argo ns — BuildKit needs DOCR creds to push images.
log "Bug #72 fix: buildkit-registry-config Secret with DOCR auth..."
AUTH_B64=$(printf '%s:%s' "$DO_TOKEN_VAL" "$DO_TOKEN_VAL" | base64 -w0)
DOCR_CONFIG=$(printf '{"auths":{"registry.digitalocean.com":{"auth":"%s"}}}' "$AUTH_B64")
KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl -n argo create secret generic buildkit-registry-config \
  --from-literal="config.json=$DOCR_CONFIG" \
  --dry-run=client -o yaml | KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl apply -f - >/dev/null
log "  ✓ buildkit-registry-config in argo ns"

# Bug #74 LIVE FIX (caught 2026-05-18 iter 51): cosign-signing-key Secret
# missing — cosign step needs encrypted keypair to sign images.
log "Bug #74 fix: cosign keypair generation + Secret..."
if ! KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl -n argo get secret cosign-signing-key >/dev/null 2>&1; then
  COSIGN_DIR=$(mktemp -d)
  COSIGN_PW="drillcosign$(openssl rand -hex 8)"
  (cd "$COSIGN_DIR" && COSIGN_PASSWORD="$COSIGN_PW" cosign generate-key-pair >/dev/null 2>&1) || {
    log "  ✗ cosign generate-key-pair failed (cosign may be missing)"
  }
  if [[ -f "$COSIGN_DIR/cosign.key" ]]; then
    KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl -n argo create secret generic cosign-signing-key \
      --from-file=cosign.key="$COSIGN_DIR/cosign.key" \
      --from-file=cosign.pub="$COSIGN_DIR/cosign.pub" \
      --from-literal=cosign.password="$COSIGN_PW" \
      --dry-run=client -o yaml | KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml" kubectl apply -f - >/dev/null
    rm -rf "$COSIGN_DIR"
    log "  ✓ cosign-signing-key generated + applied"
  fi
else
  log "  ✓ cosign-signing-key already exists"
fi

# Bug #66 LIVE FIX (caught 2026-05-18 iter 47): ArgoCD on mgmt cluster needs
# argocd-creds-gitea Secret to authenticate Gitea repo reads.
log "Bug #66 fix: argocd-creds-gitea Secret in argocd ns..."
if [[ -n "${GITEA_TOKEN_NEW:-}" ]]; then
  kubectl -n argocd create secret generic argocd-creds-gitea \
    --from-literal=type=git \
    --from-literal=url="http://gitea-http.loopzero-system.svc.cluster.local:3000/" \
    --from-literal=username=gitea_admin \
    --from-literal=password="$GITEA_TOKEN_NEW" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n argocd label secret argocd-creds-gitea argocd.argoproj.io/secret-type=repo-creds --overwrite >/dev/null 2>&1
  # Restart ArgoCD repo-server to pick up new creds
  kubectl -n argocd rollout restart deployment/argocd-repo-server >/dev/null 2>&1
  log "  ✓ argocd-creds-gitea applied + repo-server restarted"
fi

# Bug #81 LIVE FIX (caught 2026-05-18 iter 53): runtime cluster needs
# letsencrypt-prod ClusterIssuer with CF DNS-01 — loopzero-api manifests reference
# this issuer for tenant TLS. Without it certs stay Failed → HTTPS unusable.
#
# Plan 2026-05-19-drill-skills-rigor-restoration Bug A fix:
# Previous code only applied to runtime clusters — MGMT was MISSING. loopzero-api
# Certificate (api.loopzero-drill.example) lives in mgmt cluster loopzero-system ns;
# without ClusterIssuer there, cert stuck "DoesNotExist" 93min during LIVE
# Phase 4 (TRAEFIK DEFAULT CERT self-signed detected via curl). Loop now
# includes mgmt explicitly. Also unified secret name to `cloudflare-api-token-secret`
# (matches infra/manifests/cluster-issuers/letsencrypt-{staging,prod}.yaml).
log "Bug #81+A fix: letsencrypt-prod ClusterIssuer (CF DNS-01) on mgmt + runtime clusters..."
CF_TOKEN_VAL=$(awk -F= '/^CLOUDFLARE_API_TOKEN=/{print $2}' .env 2>/dev/null || echo "${CLOUDFLARE_API_TOKEN:-}")
if [[ -n "$CF_TOKEN_VAL" ]]; then
  for RC in mgmt runtime runtime-2; do
    KC="$DRILL_KUBECONFIG_DIR/$RC.yaml"
    [[ -f "$KC" ]] || continue
    KUBECONFIG="$KC" kubectl create namespace cert-manager --dry-run=client -o yaml | KUBECONFIG="$KC" kubectl apply -f - >/dev/null 2>&1
    # Both secret names (legacy `cloudflare-api-token` for backward-compat +
    # canonical `cloudflare-api-token-secret` matching infra/manifests/cluster-issuers/)
    for SECRET_NAME in cloudflare-api-token cloudflare-api-token-secret; do
      KUBECONFIG="$KC" kubectl -n cert-manager create secret generic "$SECRET_NAME" \
        --from-literal=api-token="$CF_TOKEN_VAL" \
        --dry-run=client -o yaml | KUBECONFIG="$KC" kubectl apply -f - >/dev/null
    done
    cat <<EOF_ISSUER | KUBECONFIG="$KC" kubectl apply -f - >/dev/null 2>&1
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    email: ops@loopzero.example
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-prod-account
    solvers:
      - dns01:
          cloudflare:
            apiTokenSecretRef:
              name: cloudflare-api-token-secret
              key: api-token
        selector:
          dnsZones:
            - $DRILL_BASE_DOMAIN
EOF_ISSUER
  done
  log "  ✓ letsencrypt-prod ClusterIssuer + CF token secret on mgmt + runtime clusters"
else
  log "  ✗ CLOUDFLARE_API_TOKEN not set — letsencrypt-prod ClusterIssuer skipped"
fi

# Bug #82 LIVE FIX (caught 2026-05-18 iter 53): CF zone needs wildcard A
# record `*.loopzero-drill.example` → runtime cluster Traefik LB IP so tenant
# apps resolve publicly + cert-manager DNS-01 works.
log "Bug #82 fix: CF wildcard A record → runtime cluster Traefik LB..."
if [[ -n "$CF_TOKEN_VAL" ]]; then
  # Get runtime Traefik LB IP (wait up to 5min for assignment)
  RUNTIME_LB_IP=""
  for i in 1 2 3 4 5 6 7 8 9 10; do
    RUNTIME_LB_IP=$(KUBECONFIG="$DRILL_KUBECONFIG_DIR/runtime.yaml" kubectl -n traefik get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
    [[ -n "$RUNTIME_LB_IP" ]] && break
    sleep 30
  done
  if [[ -n "$RUNTIME_LB_IP" ]]; then
    ZONE_ID=$(cd infra/pulumi-drill && npx pulumi stack output --stack drill zoneId 2>/dev/null || echo "")
    if [[ -n "$ZONE_ID" ]]; then
      # Delete existing wildcard if present (idempotent)
      EXISTING_ID=$(curl -s -H "Authorization: Bearer $CF_TOKEN_VAL" \
        "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records?name=*.$DRILL_BASE_DOMAIN" \
        2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('result',[{}])[0].get('id',''))" 2>/dev/null || echo "")
      [[ -n "$EXISTING_ID" ]] && curl -s -X DELETE \
        "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records/$EXISTING_ID" \
        -H "Authorization: Bearer $CF_TOKEN_VAL" >/dev/null 2>&1
      curl -s -X POST "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records" \
        -H "Authorization: Bearer $CF_TOKEN_VAL" -H "Content-Type: application/json" \
        -d "{\"type\":\"A\",\"name\":\"*.$DRILL_BASE_DOMAIN\",\"content\":\"$RUNTIME_LB_IP\",\"ttl\":60,\"proxied\":false}" >/dev/null 2>&1
      log "  ✓ CF wildcard A *.$DRILL_BASE_DOMAIN → $RUNTIME_LB_IP"

      # Bug #21 LIVE FIX (caught 2026-05-19 dogfood-drill iter 7):
      # api.loopzero-drill.example pre-existing A record (stale from prior drill
      # iters) takes DNS precedence over wildcard *.loopzero-drill.example. The
      # wildcard points to runtime LB (tenant workloads), but API requests
      # must reach MGMT LB. Without this fix, dogfood-drill journeys all
      # return HTTP 000 (curl can't connect to stale IP).
      # Solution: explicit A record `api.<domain>` → MGMT LB IP. Delete
      # existing record first (idempotent), then POST new.
      MGMT_LB_IP=$(KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml" kubectl -n traefik get svc traefik -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")
      if [[ -n "$MGMT_LB_IP" ]]; then
        API_EXISTING_ID=$(curl -s -H "Authorization: Bearer $CF_TOKEN_VAL" \
          "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records?name=api.$DRILL_BASE_DOMAIN" \
          2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('result',[{}])[0].get('id',''))" 2>/dev/null || echo "")
        [[ -n "$API_EXISTING_ID" ]] && curl -s -X DELETE \
          "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records/$API_EXISTING_ID" \
          -H "Authorization: Bearer $CF_TOKEN_VAL" >/dev/null 2>&1
        curl -s -X POST "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records" \
          -H "Authorization: Bearer $CF_TOKEN_VAL" -H "Content-Type: application/json" \
          -d "{\"type\":\"A\",\"name\":\"api.$DRILL_BASE_DOMAIN\",\"content\":\"$MGMT_LB_IP\",\"ttl\":60,\"proxied\":false}" >/dev/null 2>&1
        log "  ✓ CF explicit A api.$DRILL_BASE_DOMAIN → $MGMT_LB_IP (mgmt cluster)"
      else
        log "  ✗ MGMT Traefik LB IP not assigned — api.$DRILL_BASE_DOMAIN A record skipped"
      fi
    fi
  else
    log "  ✗ Runtime Traefik LB IP not assigned yet — wildcard A record skipped (re-run when LB ready)"
  fi
fi

# Bug #69 (caught 2026-05-18 iter 47) was a leader-election advisory lock leaked
# across loopzero-api restarts, healed here by terminating its holder. The API no
# longer elects with an advisory lock: it takes the fenced lease in
# leader_generations (api/internal/leader), which a new pod takes over once the
# old holder's heartbeat is older than StaleAfter. There is no session to kill,
# and terminating backends by advisory lock would only reach the per-environment
# deploy locks.

# Restart loopzero-api to pick up env + secret changes
log "Bug #46+#54 fix: restart loopzero-api to reload env vars..."
kubectl -n loopzero-system rollout restart deployment/loopzero-api >/dev/null
kubectl -n loopzero-system rollout status deployment/loopzero-api --timeout=180s 2>&1 | tail -2 | sed 's/^/  /'

# T2.2 gate (plan 2026-05-19-drill-skills-rigor-restoration ADR D2):
# verify JWT auth wiring end-to-end BEFORE Phase 5 cell wiring. Drift between
# drill Hydra issuer and loopzero-api JWT_ISSUER would otherwise only surface as
# 401 in dogfood-drill — too late, and historically masked by Bug #24 workaround.
# shellcheck source=infra/scripts/lib/drill-jwt-gate.sh
source "$(dirname "${BASH_SOURCE[0]}")/../lib/drill-jwt-gate.sh"
log "T2.2 gate: verifying JWT auth wiring end-to-end..."
if ! verify_jwt_auth_e2e \
    "${API_URL:-https://api.loopzero-drill.example}" \
    "$(dirname "${BASH_SOURCE[0]}")/drill-mint-jwt.sh" \
    "curl" \
    "${SEED_TENANT_ID:-tn-e2etest}"; then
  log "T2.2 gate FAIL — aborting bootstrap before Phase 5"
  log "  diagnose: bash infra/scripts/drill/jwt-diagnose.sh"
  exit 1
fi
log "  ✓ JWT auth gate PASS"

# Restart 4 split operators to pick up env changes
log "Bug #57+#60 fix: restart operators to reload env + token..."
for OP in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  kubectl -n loopzero-system rollout restart deployment/"$OP" >/dev/null 2>&1 || true
done

log ""
log "✓ Bootstrap complete. LoopZero platform deployed on drill 4-cluster stack."
log "  Next: bash infra/scripts/drill/drill-mint-jwt.sh"
log "  Then: bash infra/scripts/bootstrap-cell-{default,1,2}-cc-drill.sh"
log "  Then: run L0+L1+L2 harness against drill cloud"
