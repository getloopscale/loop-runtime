#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-1-cc.sh
#
# Bootstraps the cell-1 ClusterCredential pointing to k3d-loopzero-workload cluster.
#
# Plan: 2026-05-14-dogfood-findings-and-multi-cell-coverage T0.1
# Closes CTO Golden Rule #1 gap (cleanroom must be multi-cell by default).
#
# In production, cell-1 maps to a dedicated DOKS runtime cluster. In local
# cleanroom, loopzero-workload + loopzero-runtime-2 are the only tenant-runtime
# clusters, so cell-1 shares the workload cluster with cell-default. This
# is intentional: it tests CR uniqueness + multi-cell lookup-by-cellID
# (K8sCellStore.LoadCells via spec.cellID) even when two cells share an
# apiServer endpoint. cell-2 (separate script) maps to runtime-2 and
# provides the cross-cluster placement coverage.
#
# Usage:
#   bash infra/scripts/bootstrap-cell-1-cc.sh           # apply
#   bash infra/scripts/bootstrap-cell-1-cc.sh --dry-run # render manifests only
#
# Idempotent. Shellcheck clean.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

DRY_RUN="${1:-}"

LIB="$(cd "$(dirname "$0")" && pwd)/lib/bootstrap-cell-cc-common.sh"
if [[ ! -f "$LIB" ]]; then
  echo "[bootstrap-cell-1-cc] library not found: $LIB" >&2
  exit 1
fi
# shellcheck source=lib/bootstrap-cell-cc-common.sh
source "$LIB"

bootstrap_cell_cc "cell-1" "workload" "$DRY_RUN"
exit $?
