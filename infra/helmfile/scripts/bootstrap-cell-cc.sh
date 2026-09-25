#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Canonical bootstrap-cell-cc — consolidates 6 prior scripts into 1
# parameterized by --cell <id> --env <name>.
#
# Plan v1.2 T4.2 / D3 (scripts-param-by-env): consolidates the per-cell scripts that
# existed before (`bootstrap-cell-{default,1,2}-cc.sh` and the `-drill` variants), all
# deleted today.
#
# ## Cell ↔ cluster: 1:1, and each cluster is named after the cell it hosts
#
#   | Cell ID  | k3d local           | drill DOKS                   |
#   |----------|---------------------|------------------------------|
#   | cell-0   | k3d-loopzero-cell-0     | runtimeCluster (pulumi key)  |
#   | cell-1   | k3d-loopzero-cell-1     | runtimeCluster2 (pulumi key) |
#
# THIS HEADER WAS LYING, and the way it lied is the clearest record of the risk
# that `B-001` names (*"blind global substitution destroys the repository"*). A
# substitution of `cell-default` → `cell-0` over the previous text produced:
#
#   - two table rows with the SAME id (`cell-0` and `cell-0`), because `cell-default`
#     and `cell-0` were distinct rows that collapsed into one;
#   - two file names repeated in the list of replaced scripts, for the same reason;
#   - the sentence *"cell-0 reuses cell-0's cluster"*, which asserts nothing;
#   - `Cell-2` and `runtime-2` cited in the prose — neither of the two exists;
#   - and worst of all: `--cell <default|1|2>` in the Usage, telling the operator to pass
#     values the code **rejects** (`case "$CELL"` accepts `0` and `1`, and nothing else).
#
# The text above now describes what the code does. The 2:1 relation (`cell-default` + `cell-1`
# on the same cluster) was eliminated by `B-005`, and `DefaultCellID` is `cell-0`
# (`pkg/domain/cell.go`).
#
# Usage:
#   bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell <0|1> --env <dev|drill|prod|standalone|standalone-multi> [--dry-run]
#
# Examples:
#   # Local k3d, cell-0 → cluster loopzero-cell-0:
#   bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 0 --env standalone-multi
#
#   # Drill DOKS, cell-1 → runtimeCluster2:
#   # `mgmt.yaml` here is NOT a leftover of the rename: it is the file name the drill
#   # writes today (`drill-helmfile-bootstrap.sh:143`, `evidence-capture.sh:221`). The drill
#   # half was not renamed, and changing the name HERE without changing it there would
#   # break the example.
#   DRILL_KUBECONFIG=/tmp/drill-kc/mgmt.yaml \
#     bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 1 --env drill
#
# Idempotent. Shellcheck clean.
set -euo pipefail

# The Usage string below is the SECOND copy of the one in the header above. The
# 2026-08-17 header fix corrected the comment copy and left this one printing
# `--cell <default|1|2>` -- the very defect the header declares fixed -- until it was
# re-measured on 2026-09-06 (B-136). Both copies must move together, and `case "$CELL"`
# below is the only authority on which values are accepted.
usage() {
  cat <<USAGE
Usage: $0 --cell <0|1> --env <dev|drill|prod|standalone|standalone-multi> [--dry-run]

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

# One cell per cluster. The set used to be `default, 1, 2` — three names for two machines,
# because `default` and `1` both mapped to the same cluster. The 1:1 unification of
# 2026-08-09 (ADR docs/decisions.md) eliminated the
# duplicate; `default` no longer exists as a cell.
case "$CELL" in
  0|1) ;;
  *)
    echo "[bootstrap-cell-cc] --cell must be one of: 0, 1 (got: $CELL)" >&2
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
    # The Pulumi output keys do NOT change: they are an external contract of legacy code,
    # alive until the AWS cutover. Only the cell side was renumbered.
    case "$CELL" in
      0) PULUMI_KEY="runtimeCluster" ;;
      1) PULUMI_KEY="runtimeCluster2" ;;
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
    # The cluster name IS the cell's — this is where the 2:1 relation lived in shell.
    case "$CELL" in
      0) K3D_CLUSTER="cell-0" ;;
      1) K3D_CLUSTER="cell-1" ;;
    esac

    echo "[bootstrap-cell-cc] env=$ENV cell=$CELL_ID → k3d cluster=$K3D_CLUSTER"
    bootstrap_cell_cc "$CELL_ID" "$K3D_CLUSTER" "$DRY_RUN"
    ;;
esac
