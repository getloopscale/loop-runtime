#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-default-cc.sh
#
# B24/B25/B27 fix (real-paas-cleanroom-dogfood-loop iter 22-23, 2026-05-05):
# bootstraps the cell-default ClusterCredential in standalone-multi mode.
# Self-sufficient: generates its own kubeconfig + dynamically resolves
# the workload cluster IP (does NOT depend on H12 having already created
# workload-kubeconfig).
#
# Why a separate script (not lib/clustercredential-create.sh): that lib
# uses CLUSTER_NAME for both the CR name AND the Secret name. Calling it
# with cluster-name=workload would clash with H12's `workload` CR
# (cellID=cell-1). This script applies a CR named `cell-default` with a
# dedicated `cell-default-kubeconfig` Secret (the kubeconfig itself
# from `k3d kubeconfig get loopzero-workload`, server URL rewritten to the
# Docker bridge IP — same pattern that workload CR uses for cell-1).
# K8sCellStore reads spec.cellID, not CR name, so two CRs against the
# same physical cluster (loopzero-workload) work fine.
#
# Idempotent: kubectl apply.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
NS="${LOOPZERO_NS:-loopzero-system}"

log() { printf '[bootstrap-cell-default-cc] %s\n' "$*" >&2; }

if ! command -v k3d >/dev/null 2>&1 || ! command -v kubectl >/dev/null 2>&1 || ! command -v docker >/dev/null 2>&1; then
  log "k3d/kubectl/docker missing on PATH; skip"
  exit 0
fi

# 1. Resolve loopzero-workload server IP on the k3d-loopzero bridge (current).
WORKLOAD_IP=$(docker inspect k3d-loopzero-workload-server-0 \
  --format '{{range .NetworkSettings.Networks}}{{if eq "k3d-loopzero" "k3d-loopzero"}}{{.IPAddress}}{{end}}{{end}}' 2>/dev/null \
  | head -1)
if [[ -z "$WORKLOAD_IP" ]]; then
  log "k3d-loopzero-workload-server-0 not running; skip"
  exit 0
fi

# 2. Get a kubeconfig that points at the resolved IP. k3d kubeconfig
#    defaults to localhost:port (which doesn't work from inside mgmt pods),
#    so we rewrite the server URL to the bridge IP.
KC_TMP=$(mktemp)
trap 'rm -f "$KC_TMP"' EXIT

if ! k3d kubeconfig get loopzero-workload >"$KC_TMP" 2>/dev/null || [[ ! -s "$KC_TMP" ]]; then
  log "loopzero-workload kubeconfig unavailable"
  exit 0
fi

# Rewrite server URL to the bridge IP so the CC controller (inside mgmt
# cluster) can reach the workload apiserver.
sed -i "s|server: https://[^[:space:]]*|server: https://${WORKLOAD_IP}:6443|g" "$KC_TMP"

# 3. Apply Secret loopzero-system/cell-default-kubeconfig (separate from
#    workload-kubeconfig owned by H12's cell-1 CR).
kubectl --context "$CTX" create secret generic cell-default-kubeconfig \
  -n "$NS" \
  --from-file=kubeconfig="$KC_TMP" \
  --dry-run=client -o yaml \
  | kubectl --context "$CTX" apply -f - >/dev/null

# 4. Apply ClusterCredential cell-default with the resolved IP.
cat <<YAML | kubectl --context "$CTX" apply -f -
apiVersion: loopzero.io/v1
kind: ClusterCredential
metadata:
  name: cell-default
  namespace: loopzero-system
  labels:
    loopzero.io/cell: cell-default
    loopzero.io/ingress-domain: loopzero.local
    app.kubernetes.io/managed-by: bootstrap-cell-default-cc
spec:
  cellID: cell-default
  clusterName: workload
  apiServer: https://${WORKLOAD_IP}:6443
  scope:
    argocd: true
    operators: true
  secretRef:
    name: cell-default-kubeconfig
    namespace: loopzero-system
YAML

log "cell-default CC + cell-default-kubeconfig Secret applied (apiServer=https://${WORKLOAD_IP}:6443)"
