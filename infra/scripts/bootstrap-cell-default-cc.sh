#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-default-cc.sh
#
# Bootstraps the cell-default ClusterCredential pointing to k3d-loopzero-workload.
#
# History:
#   - B24/B25/B27/B29 (real-paas-cleanroom-dogfood-loop iter 22-25, 2026-05-05):
#     original implementation
#   - 2026-05-14 (plan T0.1): refactored to use shared library
#     infra/scripts/lib/bootstrap-cell-cc-common.sh; behavior preserved
#     (cell-default still maps to k3d-loopzero-workload).
#
# Usage:
#   bash infra/scripts/bootstrap-cell-default-cc.sh           # apply
#   bash infra/scripts/bootstrap-cell-default-cc.sh --dry-run # render manifests only
#
# Idempotent. Shellcheck clean.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

DRY_RUN="${1:-}"

LIB="$(cd "$(dirname "$0")" && pwd)/lib/bootstrap-cell-cc-common.sh"
if [[ ! -f "$LIB" ]]; then
  echo "[bootstrap-cell-default-cc] library not found: $LIB" >&2
  exit 1
fi
# shellcheck source=lib/bootstrap-cell-cc-common.sh
source "$LIB"

bootstrap_cell_cc "cell-default" "workload" "$DRY_RUN"
exit $?
