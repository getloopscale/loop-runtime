#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-16 T2.3 — Drill analog of bootstrap-cell-2-cc.sh.
#
# Bootstraps cell-2 ClusterCredential pointing to drill runtimeCluster2
# (per D3 mapping: cell-2 → runtimeCluster2, mirror prod).

set -euo pipefail

DRY_RUN="${1:-}"

LIB="$(cd "$(dirname "$0")" && pwd)/lib/bootstrap-cell-cc-drill-common.sh"
if [[ ! -f "$LIB" ]]; then
  echo "[bootstrap-cell-2-cc-drill] library not found: $LIB" >&2
  exit 1
fi
# shellcheck source=lib/bootstrap-cell-cc-drill-common.sh
source "$LIB"

# Per D3: cell-2 → runtimeCluster2 (separate cluster from cell-default+cell-1)
bootstrap_cell_cc_drill "cell-2" "runtimeCluster2" "$DRY_RUN"
exit $?
