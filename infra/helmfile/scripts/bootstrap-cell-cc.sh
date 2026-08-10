#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Canonical bootstrap-cell-cc — consolidates 6 prior scripts into 1
# parameterized by --cell <id> --env <name>.
#
# Plan v1.2 T4.2 / D3 (scripts-param-by-env): replaces:
#   infra/scripts/bootstrap-cell-default-cc.sh
#   infra/scripts/bootstrap-cell-1-cc.sh
#   infra/scripts/bootstrap-cell-2-cc.sh
#   infra/scripts/bootstrap-cell-default-cc-drill.sh
#   infra/scripts/bootstrap-cell-1-cc-drill.sh
#   infra/scripts/bootstrap-cell-2-cc-drill.sh
#
# Cell-to-cluster mapping per EC-4 mitigation:
#
#   | Cell ID      | k3d local                       | drill DOKS                   |
#   |--------------|----------------------------------|------------------------------|
#   | cell-default | k3d-loopzero-cell-0 (cluster)      | runtimeCluster (pulumi key)  |
#   | cell-1       | k3d-loopzero-cell-0 (same default) | runtimeCluster (same default)|
#   | cell-2       | k3d-loopzero-cell-1 (cluster)     | runtimeCluster2 (pulumi key) |
#
# Per D3 ADR (multi-cell): cell-1 reuses cell-default's cluster in local k3d
# (topology limit — k3d host can't sustain a 4th K8s cluster on most laptops);
# in drill DOKS, both also map to runtimeCluster (mirrors prod posture per
# multi-cell sticky placement). Cell-2 always maps to a SECOND cluster
# (runtime-2 / runtimeCluster2).
#
# Usage:
#   bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell <default|1|2> --env <dev|drill|prod|standalone|standalone-multi> [--dry-run]
#
# Examples:
#   # Local k3d, cell-default → cluster loopzero-cell-0:
#   bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell default --env standalone-multi
#
#   # Drill DOKS, cell-2 → runtimeCluster2:
#   DRILL_KUBECONFIG=/tmp/drill-kc/mgmt.yaml \
#     bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 2 --env drill
#
# Idempotent. Shellcheck clean.
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $0 --cell <default|1|2> --env <dev|drill|prod|standalone|standalone-multi> [--dry-run]

Bootstraps a ClusterCredential CR + bearer-token Secret for the given
(cell, env) pair. See plan v1.2 T4.2 + EC-4 mapping table.
USAGE
  exit 1
}

CELL=""
ENV=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cell)
      CELL="$2"
      shift 2
      ;;
    --env)
      ENV="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "[bootstrap-cell-cc] unknown arg: $1" >&2
      usage
      ;;
  esac
done

# --- Arg validation ---
if [[ -z "$CELL" ]] || [[ -z "$ENV" ]]; then
  echo "[bootstrap-cell-cc] --cell and --env are required" >&2
  usage
fi

case "$CELL" in
  default|1|2) ;;
  *)
    echo "[bootstrap-cell-cc] --cell must be one of: default, 1, 2 (got: $CELL)" >&2
    exit 1
    ;;
esac

case "$ENV" in
  dev|drill|prod|standalone|standalone-multi) ;;
  *)
    echo "[bootstrap-cell-cc] --env must be one of: dev, drill, prod, standalone, standalone-multi (got: $ENV)" >&2
    exit 1
    ;;
esac

CELL_ID="cell-${CELL}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INFRA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# --- Env-specific resolution ---
case "$ENV" in
  drill|prod)
    # Cloud DOKS env — use Pulumi-backed lib
    LIB="$INFRA_ROOT/scripts/lib/bootstrap-cell-cc-drill-common.sh"
    if [[ ! -f "$LIB" ]]; then
      echo "[bootstrap-cell-cc] library not found: $LIB" >&2
      exit 1
    fi
    # shellcheck source=../../scripts/lib/bootstrap-cell-cc-drill-common.sh disable=SC1091
    source "$LIB"

    # Resolve Pulumi output key per EC-4 mapping
    case "$CELL" in
      default|1) PULUMI_KEY="runtimeCluster" ;;
      2)         PULUMI_KEY="runtimeCluster2" ;;
    esac

    echo "[bootstrap-cell-cc] env=$ENV cell=$CELL_ID → Pulumi output key=$PULUMI_KEY"
    bootstrap_cell_cc_drill "$CELL_ID" "$PULUMI_KEY" "$DRY_RUN"
    ;;

  dev|standalone|standalone-multi)
    # Local k3d env — use docker-inspect-backed lib
    LIB="$INFRA_ROOT/scripts/lib/bootstrap-cell-cc-common.sh"
    if [[ ! -f "$LIB" ]]; then
      echo "[bootstrap-cell-cc] library not found: $LIB" >&2
      exit 1
    fi
    # shellcheck source=../../scripts/lib/bootstrap-cell-cc-common.sh disable=SC1091
    source "$LIB"

    # Resolve k3d cluster name per EC-4 mapping (verified against B82)
    case "$CELL" in
      default|1) K3D_CLUSTER="workload" ;;
      2)         K3D_CLUSTER="runtime-2" ;;
    esac

    echo "[bootstrap-cell-cc] env=$ENV cell=$CELL_ID → k3d cluster=$K3D_CLUSTER"
    bootstrap_cell_cc "$CELL_ID" "$K3D_CLUSTER" "$DRY_RUN"
    ;;
esac
