#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-default-cc.sh
#
# B24 fix (real-paas-cleanroom-dogfood-loop iter 22, 2026-05-05):
# bootstraps the cell-default ClusterCredential in standalone-multi mode.
# wire-clusters.sh creates cell-1 and cell-2 CRs but not cell-default;
# helm + heal codified the gap for cell-1/cell-2 only. Without this CR,
# K8sCellStore.LoadCells() returns count=2 and the build_reconcile loop
# never iterates cell-default → builds with cell_id=cell-default stay in
# RUNNING state forever even after Argo Workflow Succeeded.
#
# Why a separate script (not lib/clustercredential-create.sh): that lib
# uses CLUSTER_NAME for both the CR name and the Secret name. Calling it
# with cluster-name=workload would clash with the existing `workload`
# CR (cellID=cell-1). This script applies a CR named `cell-default` that
# REUSES workload-kubeconfig. K8sCellStore reads spec.cellID, not CR name,
# so two CRs sharing one Secret + one apiServer is valid.
#
# Idempotent: kubectl apply.
#
# Pre-conditions:
#   - mgmt cluster reachable via k3d-loopzero-mgmt context
#   - workload-kubeconfig Secret exists in loopzero-system (created by H12)
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -euo pipefail

CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
NS="${LOOPZERO_NS:-loopzero-system}"

if ! kubectl --context "$CTX" -n "$NS" get secret workload-kubeconfig >/dev/null 2>&1; then
  echo "[bootstrap-cell-default-cc] workload-kubeconfig Secret missing in ${NS}; skip" >&2
  exit 0
fi

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
    name: workload-kubeconfig
    namespace: loopzero-system
YAML

echo "[bootstrap-cell-default-cc] cell-default ClusterCredential applied" >&2
