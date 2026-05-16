#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T2.3 — Drill analog of bootstrap-cell-default-cc.sh.
#
# Bootstraps cell-default ClusterCredential pointing to drill runtimeCluster
# (per D3 mapping: cell-default + cell-1 → runtimeCluster, mirror prod).
#
# Usage:
#   DRILL_KUBECONFIG=/path/to/drill-mgmt-kc.yaml \
#     bash infra/scripts/bootstrap-cell-default-cc-drill.sh
#   bash infra/scripts/bootstrap-cell-default-cc-drill.sh --dry-run

set -euo pipefail

DRY_RUN="${1:-}"

LIB="$(cd "$(dirname "$0")" && pwd)/lib/bootstrap-cell-cc-drill-common.sh"
if [[ ! -f "$LIB" ]]; then
  echo "[bootstrap-cell-default-cc-drill] library not found: $LIB" >&2
  exit 1
fi
# shellcheck source=lib/bootstrap-cell-cc-drill-common.sh
source "$LIB"

# Per D3: cell-default → runtimeCluster (NOT runtimeCluster2)
bootstrap_cell_cc_drill "cell-default" "runtimeCluster" "$DRY_RUN"
exit $?
