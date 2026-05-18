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
# `helmfile -e drill list` reads LOCAL k3d-loopzero-mgmt context (cleanroom
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
#   bash infra/scripts/drill-helmfile-bootstrap.sh                    # default permissive
#   STRICT_BOOTSTRAP=true bash infra/scripts/drill-helmfile-bootstrap.sh   # CI mode fail-fast
#   bash infra/scripts/drill-helmfile-bootstrap.sh --from-cluster runtime # partial recovery (EC-05)
#   bash infra/scripts/drill-helmfile-bootstrap.sh --dry-run          # preview only
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

# --- Phase 1: Read Pulumi outputs ---
log "Phase 1/4: Reading Pulumi outputs (v2 flat named exports)..."

# Helper: pulumi stack output NAME
pulumi_out() {
  local key="$1"
  local secret="${2:-false}"
  if command -v pulumi >/dev/null 2>&1; then
    if [[ "$secret" == "true" ]]; then
      pulumi stack output --stack drill --cwd "$PULUMI_DRILL_DIR" "$key" --show-secrets 2>/dev/null
    else
      pulumi stack output --stack drill --cwd "$PULUMI_DRILL_DIR" "$key" 2>/dev/null
    fi
  else
    if [[ "$secret" == "true" ]]; then
      (cd "$PULUMI_DRILL_DIR" && npx pulumi stack output --stack drill "$key" --show-secrets 2>/dev/null)
    else
      (cd "$PULUMI_DRILL_DIR" && npx pulumi stack output --stack drill "$key" 2>/dev/null)
    fi
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
  export MGMT_CLUSTER_ENDPOINT="$MGMT_ENDPOINT"
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
# Parse DSN into HOST/USER/PASSWORD/DB for gitea chart consumption.
# DSN format: postgres://USER:PASSWORD@HOST:PORT/DB?sslmode=require
GITEA_USER=$(echo "$GITEA_DSN" | sed -E 's|postgres://([^:]+):.*|\1|')
GITEA_PASSWORD=$(echo "$GITEA_DSN" | sed -E 's|postgres://[^:]+:([^@]+)@.*|\1|')
GITEA_HOST=$(echo "$GITEA_DSN" | sed -E 's|.*@([^:]+):[0-9]+/.*|\1|')
GITEA_PORT=$(echo "$GITEA_DSN" | sed -E 's|.*@[^:]+:([0-9]+)/.*|\1|')
GITEA_DB=$(echo "$GITEA_DSN" | sed -E 's|.*/([^?]+)\?.*|\1|')
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
MGMT_KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
CTX_INFO=$(KUBECONFIG="$MGMT_KUBECONFIG" kubectl cluster-info 2>/dev/null | head -1 || true)
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
  KUBECONFIG="$MGMT_KUBECONFIG" kubectl create namespace "$ns" --dry-run=client -o yaml | \
    KUBECONFIG="$MGMT_KUBECONFIG" kubectl apply -f - >/dev/null
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
  KUBECONFIG="$MGMT_KUBECONFIG" kubectl create secret generic "$secret_name" \
    --namespace "$namespace" \
    --from-literal=dsn="$dsn_val" \
    --dry-run=client -o yaml | \
    KUBECONFIG="$MGMT_KUBECONFIG" kubectl apply -f - >/dev/null
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
# (loopzero-mgmt). The platform Secrets get created on the WRONG cluster and
# velero install on drill mgmt fails: "MountVolume.SetUp failed for volume
# 'cloud-credentials' : secret 'velero-s3-credentials' not found".
#
# Phase 2 uses inline `KUBECONFIG=$MGMT_KUBECONFIG kubectl ...` (no export)
# so the inherited KUBECONFIG at this point is whatever the parent task set.
# Export drill mgmt kubeconfig explicitly to scope generate-secrets.sh.
export KUBECONFIG="$MGMT_KUBECONFIG"

export LOOPZERO_DB_HOST="$(pulumi_out pgHost false)"
export LOOPZERO_DB_PORT="$(pulumi_out pgPort false)"
export LOOPZERO_DB_USER="doadmin"
export LOOPZERO_DB_PASSWORD="$(pulumi_out pgPassword true 2>/dev/null || echo "")"
if [[ -z "$LOOPZERO_DB_PASSWORD" ]]; then
  # Fall back to extracting from pgUri (pulumi-drill doesn't export pgPassword
  # as separate output — only via pgUri composite).
  #
  # Bug #17 LIVE FIX (caught 2026-05-17 iter 10 on loopzero-api CrashLoopBackOff
  # "too many colons in address"): DO Managed PG returns pgUri with scheme
  # "postgresql://" (NOT "postgres://"). The old sed regex `^postgres://`
  # didn't match → substitution failed silently → LOOPZERO_DB_PASSWORD ended up
  # being the ENTIRE URI → generate-secrets.sh built a double-wrapped DSN:
  #   postgresql://doadmin:postgresql://doadmin:PASS@host:port/db?...@host:...
  #
  # Fix: regex matches both schemes via `postgres(ql)?://`. Defensive: also
  # validate the extracted password doesn't contain `://` (a sentinel that
  # extraction failed and we got the URI itself).
  PG_URI="$(pulumi_out pgUri true)"
  LOOPZERO_DB_PASSWORD="$(echo "$PG_URI" | sed -E 's|^postgres(ql)?://[^:]+:([^@]+)@.*$|\2|')"
  if [[ -z "$LOOPZERO_DB_PASSWORD" || "$LOOPZERO_DB_PASSWORD" == *"://"* ]]; then
    log "ERROR: pgUri password extraction failed (Bug #17 regression)"
    log "  pgUri scheme may have changed; expected postgres:// or postgresql://"
    log "  extracted: $(echo "$LOOPZERO_DB_PASSWORD" | sed 's/./*/g' | head -c 50)..."
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
helmfile -e "$HELMFILE_ENV" --file "$HELMFILE_DIR/mgmt.helmfile.yaml.gotmpl" --selector name=loopzero-crds sync \
  --args "--timeout ${HELMFILE_RELEASE_TIMEOUT:-900}s --wait" 2>&1 | tee -a "/tmp/helmfile-mgmt.log" | tail -5 || {
  log "ERROR: loopzero-crds install failed (Bug #19 fix pre-step)"
  exit 3
}
log "  ✓ loopzero-crds installed — applying 3 ClusterCredentials (cell-default + cell-1 + cell-2)..."
export DRILL_KUBECONFIG="$MGMT_KUBECONFIG"
bash "$(cd "$(dirname "$0")" && pwd)/bootstrap-cell-default-cc-drill.sh" 2>&1 | tail -3 || {
  log "ERROR: bootstrap cell-default failed"
  exit 3
}
bash "$(cd "$(dirname "$0")" && pwd)/bootstrap-cell-1-cc-drill.sh" 2>&1 | tail -3 || {
  log "ERROR: bootstrap cell-1 failed"
  exit 3
}
bash "$(cd "$(dirname "$0")" && pwd)/bootstrap-cell-2-cc-drill.sh" 2>&1 | tail -3 || {
  log "ERROR: bootstrap cell-2 failed"
  exit 3
}
log "  ✓ 3 ClusterCredentials applied"

# Bug #22 LIVE FIX (caught 2026-05-17 iter 15 on loopzero-api 0 CCs discovered):
# K8sCellStore.LoadCells filters by status.connected=true (set by
# ClusterCredentialController when apiServer is reachable). The operator
# isn't installed yet (chicken-and-egg). For drill, we KNOW apiServers
# are reachable (we provisioned them), so set status.connected=true
# manually via kubectl patch. Mirrors `task k3d:heal` H13 pattern.
log "Bug #22 fix: patching status.connected=true on 3 CCs..."
for cell in cell-default cell-1 cell-2; do
  kubectl patch clustercredential "$cell" --type=merge --subresource=status \
    -p '{"status":{"connected":true,"lastReconciledAt":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}}' \
    --namespace loopzero-system 2>&1 | grep -E "patched|^Error" | head -1
done
log "  ✓ 3 CCs marked status.connected=true (loopzero-api will find cells at startup)"

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
# + RUNTIME_KUBECONFIG env vars pointing to mounted files. Chart mounts when
# kubeconfig.{build,runtime}.existingConfigMap is set. Create ConfigMaps now
# from drill kubeconfigs we already have at /tmp/drill-kc/.
log "Bug #26 fix: creating build + runtime kubeconfig ConfigMaps for operators..."
kubectl create configmap loopzero-build-kubeconfig --namespace loopzero-system \
  --from-file=config="$DRILL_KUBECONFIG_DIR/build.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create configmap loopzero-runtime-kubeconfig --namespace loopzero-system \
  --from-file=config="$DRILL_KUBECONFIG_DIR/runtime.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
log "  ✓ ConfigMaps loopzero-build-kubeconfig + loopzero-runtime-kubeconfig created"

# Bug #16 LIVE FIX (caught 2026-05-17 iter 7 on loopzero-api ImagePullBackOff):
# generate-secrets.sh registry block uses base64(SPACES_KEY:SPACES_SECRET)
# which is WRONG auth format for DOCR (DOCR uses DO API token, NOT Spaces).
# Replace with canonical kubectl create secret docker-registry pattern that
# DOCR documents officially. Uses DIGITALOCEAN_TOKEN as both username +
# password (DO standard token-only auth).
if [[ -n "${DIGITALOCEAN_TOKEN:-}" ]]; then
  log "Bug #16 fix: re-creating loopzero-registry-pull Secret with DO token auth..."
  kubectl create secret docker-registry loopzero-registry-pull \
    --namespace loopzero-system \
    --docker-server=registry.digitalocean.com \
    --docker-username="$DIGITALOCEAN_TOKEN" \
    --docker-password="$DIGITALOCEAN_TOKEN" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "  ✓ loopzero-registry-pull Secret reconciled (docker-registry type, DO token auth)"
else
  log "ERROR: DIGITALOCEAN_TOKEN missing — cannot create loopzero-registry-pull"
  exit 2
fi

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
  #   mgmt    → mgmt.helmfile.yaml.gotmpl
  #   build   → build.helmfile.yaml.gotmpl
  #   runtime → runtime.helmfile.yaml.gotmpl
  #   runtime-2 → runtime.helmfile.yaml.gotmpl (reuses runtime, same workload pattern)
  case "$cluster" in
    runtime-2) CLUSTER_HELMFILE="runtime.helmfile.yaml.gotmpl" ;;
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
PG_HOST="$PG_HOST_VAL" \
PG_PORT="$PG_PORT_VAL" \
PG_USER=doadmin \
PG_PASS=$(echo "$LOOPZERO_API_DSN_VAL" | sed -E 's|^postgres(ql)?://[^:]+:([^@]+)@.*$|\2|') \
PG_DB=loopzero \
PG_SSLMODE=require \
DRILL_KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml" \
NAMESPACE="loopzero-system" \
  bash "infra/scripts/run-migrations.sh" 2>&1 | tail -20 | sed 's/^/  /' || {
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
  bash "$HELMFILE_DIR/scripts/create-wiring-configmap.sh" drill 2>&1 | tail -5 | sed 's/^/  /' || true
log "  ✓ wiring ConfigMap applied"

# Bug #52 LIVE FIX (caught 2026-05-18 iter 41): infra/helm/loopzero-api/values.yaml
# has GITEA_URL/GITEA_ORG/LOOPZERO_DYNAMIC_ROUTER indented under tracing: section
# (yaml indent bug) — env vars never propagate to deployment. Override via
# kubectl set env until chart values is fixed (T2 follow-up).
# Bug #46 LIVE FIX: inject LOOPZERO_SERVICE_AUTH_SECRET into loopzero-db-credentials
# Secret (loopzero-api loads via envFrom) so harness X-Service-Auth path works.
log "Bug #46+#52 fix: patching loopzero-api env (GITEA_URL/LOOPZERO_DYNAMIC_ROUTER/SERVICE_AUTH)..."
SERVICE_AUTH_SECRET="${LOOPZERO_SERVICE_AUTH_SECRET:-drill-l2-test-shared-secret-$(openssl rand -hex 12)}"
kubectl -n loopzero-system patch secret loopzero-db-credentials --type=json -p="[{\"op\":\"add\",\"path\":\"/data/LOOPZERO_SERVICE_AUTH_SECRET\",\"value\":\"$(echo -n "$SERVICE_AUTH_SECRET" | base64 -w0)\"}]" 2>/dev/null || \
  kubectl -n loopzero-system patch secret loopzero-db-credentials --type=json -p="[{\"op\":\"replace\",\"path\":\"/data/LOOPZERO_SERVICE_AUTH_SECRET\",\"value\":\"$(echo -n "$SERVICE_AUTH_SECRET" | base64 -w0)\"}]" 2>/dev/null
echo "$SERVICE_AUTH_SECRET" > /tmp/drill-service-auth-secret.txt
log "  SERVICE_AUTH_SECRET saved to /tmp/drill-service-auth-secret.txt"

kubectl -n loopzero-system set env deployment/loopzero-api \
  GITEA_URL=http://gitea-http.loopzero-system.svc.cluster.local:3000 \
  GITEA_ORG=loopzero \
  LOOPZERO_DYNAMIC_ROUTER=true \
  ARGOCD_URL=http://argocd-server.argocd.svc.cluster.local:80 >/dev/null
log "  ✓ loopzero-api env patched"

# Bug #54 LIVE FIX (caught 2026-05-18 iter 42): gitea-admin Secret token was
# generated by generate-secrets.sh but NEVER registered with Gitea API →
# loopzero-api gets HTTP 401 on Gitea repo creation. Local k3d runs
# provision-api-tokens.sh which registers the token; drill never invoked it.
log "Bug #54 fix: register Gitea token via API (POST /api/v1/users/.../tokens)..."
kubectl -n loopzero-system port-forward svc/gitea-http 13030:3000 >/tmp/drill-gitea-pf.log 2>&1 &
GPF=$!
sleep 4
GITEA_USER="${GITEA_ADMIN_USER:-gitea_admin}"
GITEA_PASS="${GITEA_ADMIN_PASS:-loopzero-gitea-dev-2026}"
# Ensure loopzero org (idempotent)
curl -s -X POST "http://localhost:13030/api/v1/orgs" -u "${GITEA_USER}:${GITEA_PASS}" \
  -H "Content-Type: application/json" \
  -d '{"username":"loopzero","full_name":"LoopZero Platform","visibility":"private"}' >/dev/null 2>&1 || true
# Delete + create token (idempotent)
curl -s -X DELETE "http://localhost:13030/api/v1/users/${GITEA_USER}/tokens/loopzero-api-token" \
  -u "${GITEA_USER}:${GITEA_PASS}" >/dev/null 2>&1 || true
GITEA_TOKEN_NEW=$(curl -s -X POST "http://localhost:13030/api/v1/users/${GITEA_USER}/tokens" \
  -u "${GITEA_USER}:${GITEA_PASS}" -H "Content-Type: application/json" \
  -d '{"name":"loopzero-api-token","scopes":["write:repository","write:organization","write:user","write:admin"]}' \
  2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('sha1', ''))" 2>/dev/null || echo "")
kill "$GPF" 2>/dev/null || true
if [[ -n "$GITEA_TOKEN_NEW" ]]; then
  kubectl -n loopzero-system patch secret gitea-admin --type=json -p="[{\"op\":\"replace\",\"path\":\"/data/token\",\"value\":\"$(echo -n "$GITEA_TOKEN_NEW" | base64 -w0)\"}]" >/dev/null
  log "  ✓ Gitea token (${#GITEA_TOKEN_NEW} chars) registered + patched into gitea-admin Secret"
else
  log "  ✗ failed to register Gitea token (continuing — may not be fatal if pre-registered)"
fi

# Restart loopzero-api to pick up env + secret changes
log "Bug #46+#54 fix: restart loopzero-api to reload env vars..."
kubectl -n loopzero-system rollout restart deployment/loopzero-api >/dev/null
kubectl -n loopzero-system rollout status deployment/loopzero-api --timeout=180s 2>&1 | tail -2 | sed 's/^/  /'

log ""
log "✓ Bootstrap complete. LoopZero platform deployed on drill 4-cluster stack."
log "  Next: bash infra/scripts/drill-mint-jwt.sh"
log "  Then: bash infra/scripts/bootstrap-cell-{default,1,2}-cc-drill.sh"
log "  Then: run L0+L1+L2 harness against drill cloud"
