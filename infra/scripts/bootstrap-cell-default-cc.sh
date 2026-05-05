#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-default-cc.sh
#
# B24/B25 fix (real-paas-cleanroom-dogfood-loop iter 22, 2026-05-05):
# bootstraps the cell-default ClusterCredential in standalone-multi mode.
# Self-sufficient: generates its own scoped kubeconfig (does NOT depend on
# H12 having already created workload-kubeconfig).
#
# Why a separate script (not lib/clustercredential-create.sh): that lib
# uses CLUSTER_NAME for both the CR name AND the Secret name. Calling it
# with cluster-name=workload would clash with H12's `workload` CR
# (cellID=cell-1). This script applies a CR named `cell-default` with a
# dedicated `cell-default-kubeconfig` Secret. K8sCellStore reads
# spec.cellID, not CR name, so two CRs against the same physical cluster
# (loopzero-workload) work fine — they differ only in spec.cellID.
#
# Idempotent: kubectl apply.
#
# Pre-conditions:
#   - mgmt cluster reachable via k3d-loopzero-mgmt context
#   - loopzero-workload k3d cluster running
#   - infra/helmfile/scripts/lib/scoped-kubeconfig.sh available
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
NS="${LOOPZERO_NS:-loopzero-system}"
SCOPED_LIB="$(cd "$(dirname "$0")/.." && pwd)/helmfile/scripts/lib/scoped-kubeconfig.sh"

log() { printf '[bootstrap-cell-default-cc] %s\n' "$*" >&2; }

if ! command -v k3d >/dev/null 2>&1; then
  log "k3d not on PATH; skip"
  exit 0
fi
if ! command -v kubectl >/dev/null 2>&1; then
  log "kubectl not on PATH; skip"
  exit 0
fi

# 1. Get a scoped kubeconfig for loopzero-workload (least-privilege; same
#    pattern as heal H12 for cell-1).
WORKLOAD_KC=$(mktemp)
trap 'rm -f "$WORKLOAD_KC"' EXIT

if ! k3d kubeconfig get loopzero-workload > "$WORKLOAD_KC" 2>/dev/null || [[ ! -s "$WORKLOAD_KC" ]]; then
  log "loopzero-workload kubeconfig unavailable (cluster not running?); skip"
  exit 0
fi

SCOPED=""
if [[ -x "$SCOPED_LIB" ]]; then
  SCOPED=$(KUBECONFIG="$WORKLOAD_KC" "$SCOPED_LIB" workload "$WORKLOAD_KC" 2>/dev/null \
    | grep -oE '/[^[:space:]]+kube[^[:space:]]*' | head -1 || echo "")
fi
[[ -n "$SCOPED" && -s "$SCOPED" ]] || SCOPED="$WORKLOAD_KC"

# 2. Apply Secret loopzero-system/cell-default-kubeconfig (separate from
#    workload-kubeconfig owned by cell-1).
kubectl --context "$CTX" create secret generic cell-default-kubeconfig \
  -n "$NS" \
  --from-file=kubeconfig="$SCOPED" \
  --dry-run=client -o yaml \
  | kubectl --context "$CTX" apply -f - >/dev/null

# 3. Apply ClusterCredential cell-default referring to cell-default-kubeconfig.
kubectl --context "$CTX" apply -f - <<'YAML'
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
  apiServer: https://k3d-loopzero-workload-server-0:6443
  scope:
    argocd: true
    operators: true
  secretRef:
    name: cell-default-kubeconfig
    namespace: loopzero-system
YAML

log "cell-default ClusterCredential + cell-default-kubeconfig Secret applied"
