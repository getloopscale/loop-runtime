#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Shared library for bootstrap-cell-{default,1,2}-cc-drill.sh scripts.
# Plan 2026-05-16 T2.3.
#
# Provides bootstrap_cell_cc_drill() — idempotent ClusterCredential bootstrap
# for a given (cell_id, drill cluster pulumi output key) pair, against the
# drill mgmt cluster (DOKS, NOT k3d local).
#
# Cell-to-cluster mapping per D3:
#   cell-default + cell-1 → runtimeCluster (pulumi output key)
#   cell-2              → runtimeCluster2 (pulumi output key)
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
#   CELL_ID          — cell-default | cell-1 | cell-2
#   PULUMI_OUTPUT_KEY — runtimeCluster | runtimeCluster2
#   --dry-run        — render manifests to stdout, do NOT apply
#
# Env:
#   DRILL_KUBECONFIG  — path to drill mgmt cluster kubeconfig (REQUIRED)
#   DRILL_PULUMI_DIR  — path to pulumi-drill dir (default infra/pulumi-drill)
#   LOOPZERO_NS           — namespace for CR (default loopzero-system)
#
# Outputs (when not dry-run):
#   - Secret loopzero-system/<cell_id>-kubeconfig (bearer-token kubeconfig
#     for the target drill runtime cluster)
#   - ClusterCredential <cell_id> (spec.cellID=<cell_id>,
#     spec.apiServer=https://<uuid>.k8s.ondigitalocean.com)
#
# Idempotent: kubectl apply --server-side. Safe to re-run.
# Exit: 0 on success, 1 if pulumi output missing, 2 if mgmt unreachable.
# ---------------------------------------------------------------------------

bootstrap_cell_cc_drill() {
  local CELL_ID="${1:?bootstrap_cell_cc_drill requires CELL_ID}"
  local PULUMI_KEY="${2:?bootstrap_cell_cc_drill requires PULUMI_OUTPUT_KEY}"
  local DRY_RUN="${3:-}"

  local NS="${LOOPZERO_NS:-loopzero-system}"
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

  # 2. Create kubeconfig Secret in mgmt cluster.
  local SECRET_NAME="${CELL_ID}-kubeconfig"
  local KUBECONFIG_B64
  KUBECONFIG_B64=$(printf '%s' "$TARGET_KUBECONFIG" | base64 -w0)

  local SECRET_MANIFEST
  SECRET_MANIFEST=$(cat <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  namespace: ${NS}
  labels:
    loopzero.dev/cell-id: ${CELL_ID}
    loopzero.dev/drill: "true"
type: Opaque
data:
  kubeconfig: ${KUBECONFIG_B64}
EOF
)

  # 3. Create ClusterCredential CR in mgmt cluster.
  local CC_MANIFEST
  CC_MANIFEST=$(cat <<EOF
apiVersion: loopzero.io/v1alpha1
kind: ClusterCredential
metadata:
  name: ${CELL_ID}
  namespace: ${NS}
  labels:
    loopzero.dev/cell-id: ${CELL_ID}
    loopzero.dev/drill: "true"
spec:
  cellID: ${CELL_ID}
  clusterName: ${CELL_ID}
  apiServer: ${TARGET_ENDPOINT}
  kubeconfigSecretRef:
    name: ${SECRET_NAME}
    namespace: ${NS}
EOF
)

  if [[ "$DRY_RUN" == "--dry-run" ]]; then
    log_drill "DRY-RUN — manifests would be applied:"
    printf '%s\n---\n%s\n' "$SECRET_MANIFEST" "$CC_MANIFEST"
    return 0
  fi

  # 4. Apply Secret + CC via mgmt kubeconfig.
  log_drill "Applying Secret ${NS}/${SECRET_NAME}..."
  printf '%s\n' "$SECRET_MANIFEST" | \
    kubectl --kubeconfig "$DRILL_KUBECONFIG" apply --server-side -f - > /dev/null

  log_drill "Applying ClusterCredential ${NS}/${CELL_ID}..."
  printf '%s\n' "$CC_MANIFEST" | \
    kubectl --kubeconfig "$DRILL_KUBECONFIG" apply --server-side -f - > /dev/null

  log_drill "✓ Cell $CELL_ID wired to $PULUMI_KEY ($TARGET_ENDPOINT)"
  return 0
}
