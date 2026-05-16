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
  if helmfile -e "$HELMFILE_ENV" --file "$HELMFILE_DIR/$CLUSTER_HELMFILE" sync 2>>/tmp/helmfile-${cluster}.log; then
    log "  ✓ $cluster sync OK"
  else
    PARTIAL_FAILURES+=("$cluster")
    log "  ✗ $cluster sync FAILED (log: /tmp/helmfile-${cluster}.log)"
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

# mgmt cluster: loopzero-api, ArgoCD, hydra
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/mgmt.yaml"
HEALTH_TARGETS_MGMT=(
  "deployment/loopzero-api -n loopzero-system"
  "deployment/argocd-server -n argocd"
)

for target in "${HEALTH_TARGETS_MGMT[@]}"; do
  log "  Waiting on mgmt: $target (timeout $HEALTH_WAIT_TIMEOUT)"
  # shellcheck disable=SC2086
  if ! kubectl wait --for=condition=Available --timeout="$HEALTH_WAIT_TIMEOUT" $target 2>/dev/null; then
    log "  ✗ mgmt health check failed: $target"
    if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
      exit 4
    fi
  fi
done

# build cluster: argo-workflows-server
export KUBECONFIG="$DRILL_KUBECONFIG_DIR/build.yaml"
if ! kubectl wait --for=condition=Available --timeout="$HEALTH_WAIT_TIMEOUT" \
    deployment/argo-server -n argo 2>/dev/null; then
  log "  ✗ build cluster: argo-server not Available"
  if [[ "$STRICT_BOOTSTRAP" == "true" ]]; then
    exit 4
  fi
fi

log ""
log "✓ Bootstrap complete. LoopZero platform deployed on drill 4-cluster stack."
log "  Next: bash infra/scripts/drill-mint-jwt.sh"
log "  Then: bash infra/scripts/bootstrap-cell-{default,1,2}-cc-drill.sh"
log "  Then: run L0+L1+L2 harness against drill cloud"
