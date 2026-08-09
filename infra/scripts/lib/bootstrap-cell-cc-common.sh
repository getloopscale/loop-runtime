#!/usr/bin/env bash
# infra/scripts/lib/bootstrap-cell-cc-common.sh
#
# Shared library for bootstrap-cell-{default,1,2}-cc.sh scripts.
# Provides bootstrap_cell_cc() function — idempotent ClusterCredential
# bootstrap for a given (cell_id, k3d cluster) pair.
#
# Plan: 2026-05-14-dogfood-findings-and-multi-cell-coverage T0.1
# Closes CTO Golden Rule #1 gap: cleanroom must provision multi-cell by default.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# ---------------------------------------------------------------------------
# bootstrap_cell_cc CELL_ID K3D_CLUSTER_NAME [--dry-run]
#
# Args:
#   CELL_ID          — logical cell identifier (e.g., cell-default, cell-1, cell-2)
#                      Used as ClusterCredential CR name, Secret name prefix,
#                      and spec.cellID + spec.clusterName (F-08 alignment).
#   K3D_CLUSTER_NAME — k3d cluster suffix (e.g., workload, runtime-2).
#                      Resolved to docker container k3d-loopzero-<name>-server-0.
#   --dry-run        — render manifests to stdout, do NOT apply.
#
# Outputs (when not dry-run):
#   - Secret loopzero-system/<cell_id>-kubeconfig (bearer-token kubeconfig for the target cluster)
#   - ClusterCredential cell-id (cellID=cell_id, clusterName=cell_id, apiServer=https://<ip>:6443)
#
# Idempotent: kubectl apply --server-side. Safe to re-run.
# Exit: 0 on success, 1 if cluster missing, 2 if scoped-kubeconfig fails.
# ---------------------------------------------------------------------------

bootstrap_cell_cc() {
  local CELL_ID="${1:?bootstrap_cell_cc requires CELL_ID}"
  local K3D_CLUSTER="${2:?bootstrap_cell_cc requires K3D_CLUSTER_NAME}"
  local DRY_RUN="${3:-}"

  local CTX="${MGMT_CTX:-k3d-loopzero-core}"
  local NS="${LOOPZERO_NS:-loopzero-system}"
  local SCOPED_LIB
  SCOPED_LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/../helmfile/scripts/lib/scoped-kubeconfig.sh"

  log_cell() { printf '[bootstrap-cell-cc:%s] %s\n' "$CELL_ID" "$*" >&2; }

  # Pre-flight: required tools.
  if ! command -v k3d >/dev/null 2>&1 \
     || ! command -v kubectl >/dev/null 2>&1 \
     || ! command -v docker >/dev/null 2>&1; then
    log_cell "k3d/kubectl/docker missing on PATH; aborting"
    return 1
  fi

  # 1. Resolve target cluster IP on k3d-loopzero bridge.
  #    Container must be Running AND IP must be valid IPv4 (docker inspect
  #    sometimes returns "invalid IP" or stale metadata when container is
  #    stopped — guard against that explicitly).
  local CONTAINER="k3d-loopzero-${K3D_CLUSTER}-server-0"
  if ! docker inspect "$CONTAINER" --format '{{.State.Status}}' 2>/dev/null | grep -q "^running$"; then
    log_cell "$CONTAINER not running (or absent); cannot bootstrap. Run 'task clusters:create' or 'k3d cluster start loopzero-${K3D_CLUSTER}'."
    return 1
  fi
  local TARGET_IP
  TARGET_IP=$(docker inspect "$CONTAINER" \
    --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null \
    | head -1)
  if [[ ! "$TARGET_IP" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_cell "$CONTAINER has invalid/empty IP ('$TARGET_IP'); cannot bootstrap"
    return 1
  fi
  log_cell "resolved $K3D_CLUSTER container IP: $TARGET_IP"

  # 2. Get raw kubeconfig for the target k3d cluster.
  local RAW_KC
  RAW_KC=$(mktemp)
  # shellcheck disable=SC2064
  trap "rm -f '$RAW_KC'" RETURN

  if ! k3d kubeconfig get "loopzero-${K3D_CLUSTER}" >"$RAW_KC" 2>/dev/null || [[ ! -s "$RAW_KC" ]]; then
    log_cell "loopzero-${K3D_CLUSTER} kubeconfig unavailable"
    return 1
  fi

  # Rewrite server URL to bridge IP (scoped-kubeconfig.sh talks to apiserver).
  sed -i "s|server: https://[^[:space:]]*|server: https://${TARGET_IP}:6443|g" "$RAW_KC"

  # 3. Mint SA token via scoped-kubeconfig.sh.
  local SCOPED_KC=""
  if [[ -x "$SCOPED_LIB" ]]; then
    SCOPED_KC=$("$SCOPED_LIB" "$K3D_CLUSTER" "$RAW_KC" 2>&1 \
      | grep -oE '/[^[:space:]]+kube[^[:space:]]*\.yaml' | head -1 || echo "")
  fi
  if [[ -z "$SCOPED_KC" || ! -s "$SCOPED_KC" ]]; then
    log_cell "scoped-kubeconfig.sh did not produce output for $K3D_CLUSTER"
    return 2
  fi

  sed -i "s|server: https://[^[:space:]]*|server: https://${TARGET_IP}:6443|g" "$SCOPED_KC"

  # 4. Apply / render Secret + CR.
  local SECRET_NAME="${CELL_ID}-kubeconfig"
  if [[ "$DRY_RUN" == "--dry-run" ]]; then
    log_cell "DRY RUN — manifests below, no apply:"
    kubectl --context "$CTX" create secret generic "$SECRET_NAME" \
      -n "$NS" \
      --from-file=kubeconfig="$SCOPED_KC" \
      --dry-run=client -o yaml
    cat <<YAML
apiVersion: loopzero.io/v1
kind: ClusterCredential
metadata:
  name: ${CELL_ID}
  namespace: ${NS}
  labels:
    loopzero.io/cell: ${CELL_ID}
    loopzero.io/ingress-domain: loopzero.local
    app.kubernetes.io/managed-by: bootstrap-cell-cc
spec:
  cellID: ${CELL_ID}
  clusterName: ${CELL_ID}
  apiServer: https://${TARGET_IP}:6443
  scope:
    argocd: true
    operators: true
  secretRef:
    name: ${SECRET_NAME}
    namespace: ${NS}
  # Issue #67: this script bootstraps k3d-local cells whose k3s servers use
  # self-signed CAs that rotate across container restarts. Without
  # insecure:true, the ArgoCD cluster Secret stores caData from the initial
  # kubeconfig and rejects subsequent connections with x509 "certificate
  # signed by unknown authority" — silently breaking deploy/promote/rollback.
  # Declarative source-of-truth on the CR eliminates the previous reactive
  # workaround (heal H19 patching the rendered Secret and being overwritten
  # by the next reconcile).
  tls:
    insecure: true
YAML
    return 0
  fi

  kubectl --context "$CTX" create secret generic "$SECRET_NAME" \
    -n "$NS" \
    --from-file=kubeconfig="$SCOPED_KC" \
    --dry-run=client -o yaml \
    | kubectl --context "$CTX" apply -f - >/dev/null

  cat <<YAML | kubectl --context "$CTX" apply -f - >/dev/null
apiVersion: loopzero.io/v1
kind: ClusterCredential
metadata:
  name: ${CELL_ID}
  namespace: ${NS}
  labels:
    loopzero.io/cell: ${CELL_ID}
    loopzero.io/ingress-domain: loopzero.local
    app.kubernetes.io/managed-by: bootstrap-cell-cc
spec:
  cellID: ${CELL_ID}
  clusterName: ${CELL_ID}
  apiServer: https://${TARGET_IP}:6443
  scope:
    argocd: true
    operators: true
  secretRef:
    name: ${SECRET_NAME}
    namespace: ${NS}
  # Issue #67: this script bootstraps k3d-local cells whose k3s servers use
  # self-signed CAs that rotate across container restarts. Without
  # insecure:true, the ArgoCD cluster Secret stores caData from the initial
  # kubeconfig and rejects subsequent connections with x509 "certificate
  # signed by unknown authority" — silently breaking deploy/promote/rollback.
  # Declarative source-of-truth on the CR eliminates the previous reactive
  # workaround (heal H19 patching the rendered Secret and being overwritten
  # by the next reconcile).
  tls:
    insecure: true
YAML

  log_cell "applied: CC ${CELL_ID} -> ${SECRET_NAME} -> https://${TARGET_IP}:6443 (k3d cluster: loopzero-${K3D_CLUSTER})"
}
