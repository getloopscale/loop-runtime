#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Shared library for bootstrap-cell-{default,1,2}-cc-drill.sh scripts.
# Plan 2026-05-16 T2.3.
#
# Provides bootstrap_cell_cc_drill() — idempotent ClusterCredential bootstrap
# for a given (cell_id, drill cluster pulumi output key) pair, against the
# drill mgmt cluster (DOKS, NOT k3d local).
#
# Cell-to-cluster mapping per D3:
#   cell-0 + cell-0 → runtimeCluster (pulumi output key)
#   cell-1              → runtimeCluster2 (pulumi output key)
#
# Unlike the local k3d analog (bootstrap-cell-cc-common.sh), this drill
# variant:
#   - Reads kubeconfig from `pulumi stack output --json` (drill stack),
#     NOT from docker inspect of k3d container.
#   - Uses managed cluster endpoint (https://<uuid>.k8s.ondigitalocean.com),
#     NOT k3d 172.18.0.X bridge IP.
#   - Drill mgmt context is loaded from DRILL_KUBECONFIG env var
#     (Pulumi output mgmtCluster.kubeconfig file path).

set -euo pipefail

# ---------------------------------------------------------------------------
# bootstrap_cell_cc_drill CELL_ID PULUMI_OUTPUT_KEY [--dry-run]
#
# Args:
#   CELL_ID          — cell-0 | cell-0 | cell-1
#   PULUMI_OUTPUT_KEY — runtimeCluster | runtimeCluster2
#   --dry-run        — render manifests to stdout, do NOT apply
#
# Env:
#   DRILL_KUBECONFIG  — path to drill mgmt cluster kubeconfig (REQUIRED)
#   DRILL_PULUMI_DIR  — path to pulumi-drill dir (default infra/pulumi-drill)
#   LOOP_RUNTIME_NS           — namespace for CR (default loop-runtime-system)
#
# Outputs (when not dry-run):
#   - Secret loop-runtime-system/<cell_id>-kubeconfig (bearer-token kubeconfig
#     for the target drill runtime cluster)
#   - ClusterCredential <cell_id> (spec.cellID=<cell_id>,
#     spec.apiServer=https://<uuid>.k8s.ondigitalocean.com)
#
# Idempotent: kubectl apply through clustercredential-create.sh. Safe to re-run.
# Exit: 0 on success, 1 if pulumi output missing, 2 if mgmt unreachable.
# ---------------------------------------------------------------------------

bootstrap_cell_cc_drill() {
  local CELL_ID="${1:?bootstrap_cell_cc_drill requires CELL_ID}"
  local PULUMI_KEY="${2:?bootstrap_cell_cc_drill requires PULUMI_OUTPUT_KEY}"
  local DRY_RUN="${3:-}"

  local NS="${LOOP_RUNTIME_NS:-loop-runtime-system}"
  local PULUMI_DIR="${DRILL_PULUMI_DIR:-infra/pulumi-drill}"

  log_drill() { printf '[bootstrap-cell-cc-drill:%s] %s\n' "$CELL_ID" "$*" >&2; }

  # Pre-flight
  if [[ -z "${DRILL_KUBECONFIG:-}" ]]; then
    log_drill "ERROR: DRILL_KUBECONFIG env var required (drill mgmt kubeconfig path)"
    return 1
  fi
  if [[ ! -r "$DRILL_KUBECONFIG" ]]; then
    log_drill "ERROR: DRILL_KUBECONFIG not readable: $DRILL_KUBECONFIG"
    return 1
  fi

  for cmd in kubectl npx jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      log_drill "ERROR: $cmd missing on PATH"
      return 1
    fi
  done

  # 1. Read target cluster details from pulumi-drill stack outputs (v2 flat schema).
  # Per L2-unlock plan T1.2 D1: outputs migrated from object-literal aggregation
  # (.outputs.${PULUMI_KEY}.endpoint) to flat named exports
  # (pulumi stack output ${PULUMI_KEY}Endpoint). RCA in
  # `finding-2026-05-16-pulumi-outputs-rca.md`.
  log_drill "Reading pulumi-drill named exports for $PULUMI_KEY..."

  local TARGET_ENDPOINT TARGET_KUBECONFIG
  if [[ -n "${MOCK_PULUMI_JSON:-}" ]]; then
    # Test path — fixture provides full json blob in flat schema.
    TARGET_ENDPOINT=$(printf '%s' "$MOCK_PULUMI_JSON" | jq -r ".${PULUMI_KEY}Endpoint // empty")
    TARGET_KUBECONFIG=$(printf '%s' "$MOCK_PULUMI_JSON" | jq -r ".${PULUMI_KEY}Kubeconfig // empty")
  else
    TARGET_ENDPOINT=$(cd "$PULUMI_DIR" && npx pulumi stack output --stack drill "${PULUMI_KEY}Endpoint" 2>/dev/null || true)
    TARGET_KUBECONFIG=$(cd "$PULUMI_DIR" && npx pulumi stack output --stack drill "${PULUMI_KEY}Kubeconfig" --show-secrets 2>/dev/null || true)
  fi

  if [[ -z "$TARGET_ENDPOINT" || -z "$TARGET_KUBECONFIG" ]]; then
    log_drill "ERROR: pulumi-drill stack output missing ${PULUMI_KEY}Endpoint or ${PULUMI_KEY}Kubeconfig"
    log_drill "  hint: run 'pulumi up --stack drill' first"
    log_drill "  hint: or test fixture is missing — provide MOCK_PULUMI_JSON env"
    log_drill "  hint: schema is FLAT named exports (v2), not nested .outputs.X.field"
    return 1
  fi

  log_drill "Resolved target: $TARGET_ENDPOINT"

  # 2. Secret + ClusterCredential in the drill mgmt cluster — through the one
  #    ClusterCredential constructor, so a drill cell carries the same canonical
  #    labels (runtime.getloopscale.com/cell, which the multi-cell runbook selects on) as a local
  #    one. This used to be a heredoc copy labelled loop-runtime.dev/cell-id +
  #    loop-runtime.dev/drill, invisible to that selector.
  local CC_CREATE TARGET_KC_FILE
  CC_CREATE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../helmfile/scripts/lib/clustercredential-create.sh"
  TARGET_KC_FILE="$(mktemp)"
  # shellcheck disable=SC2064
  trap "rm -f '$TARGET_KC_FILE'" RETURN
  printf '%s' "$TARGET_KUBECONFIG" >"$TARGET_KC_FILE"

  local DRY="false"
  if [[ "$DRY_RUN" == "--dry-run" ]]; then
    log_drill "DRY-RUN — manifests would be applied:"
    DRY="true"
  fi
  MGMT_KUBECONFIG="$DRILL_KUBECONFIG" LOOP_RUNTIME_NS="$NS" API_SERVER="$TARGET_ENDPOINT" \
    MANAGED_BY="bootstrap-cell-cc-drill" DRY_RUN="$DRY" \
    bash "$CC_CREATE" "$CELL_ID" "$CELL_ID" "$TARGET_KC_FILE" || return 2
  [[ "$DRY" == "true" ]] && return 0

  log_drill "✓ Cell $CELL_ID wired to $PULUMI_KEY ($TARGET_ENDPOINT)"
  return 0
}
