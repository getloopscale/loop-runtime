#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-2-cc.sh
#
# Bootstraps the cell-2 ClusterCredential pointing to k3d-loopzero-runtime-2.
#
# Plan: 2026-05-14-dogfood-findings-and-multi-cell-coverage T0.1
# Closes CTO Golden Rule #1 gap (cleanroom must be multi-cell by default).
#
# cell-2 is the cross-cluster cell: pods scheduled here land in
# k3d-loopzero-runtime-2, NOT loopzero-workload. Required for the real multi-cell
# Ginkgo spec (edge_multi_cell_test.go, plan Phase 4 / T4.1).
#
# Usage:
#   bash infra/scripts/bootstrap-cell-2-cc.sh           # apply
#   bash infra/scripts/bootstrap-cell-2-cc.sh --dry-run # render manifests only
#
# Idempotent. Shellcheck clean.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

DRY_RUN="${1:-}"

LIB="$(cd "$(dirname "$0")" && pwd)/lib/bootstrap-cell-cc-common.sh"
if [[ ! -f "$LIB" ]]; then
  echo "[bootstrap-cell-2-cc] library not found: $LIB" >&2
  exit 1
fi
# shellcheck source=lib/bootstrap-cell-cc-common.sh
source "$LIB"

bootstrap_cell_cc "cell-2" "runtime-2" "$DRY_RUN"
exit $?
