#!/usr/bin/env bash
# infra/scripts/bootstrap-cell-default-cc.sh
#
# B24/B25/B27/B29 fix (real-paas-cleanroom-dogfood-loop iter 22-25, 2026-05-05):
# bootstraps the cell-default ClusterCredential in standalone-multi mode.
#
# Self-sufficient design:
#   1. Mint a workload-cluster SA token via lib/scoped-kubeconfig.sh
#      against `k3d kubeconfig get loopzero-workload`. Same token producer
#      that heal H12 uses for cell-1 — guaranteed valid for the workload
#      apiserver. (B29: prior version used the raw k3d admin kubeconfig
#      which has client-cert auth; clustercredential_controller.go
#      argoCDClusterConfig struct only supports BearerToken — empty
#      token would land in the ArgoCD cluster Secret and ArgoCD would
#      401 every sync.)
#   2. Resolve loopzero-workload server IP on the k3d-loopzero Docker bridge
#      (B27: CC controller in mgmt cluster cannot resolve the in-cluster
#      DNS hostname `k3d-loopzero-workload-server-0` via Docker bridge DNS.)
#   3. Apply ClusterCredential `cell-default` referencing a dedicated
#      `cell-default-kubeconfig` Secret. CR name distinct from H12's
#      `workload` so the lib script's CR_NAME=cluster-name pattern
#      doesn't collide. K8sCellStore differentiates by spec.cellID.
#
# Idempotent: kubectl apply.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
NS="${LOOPZERO_NS:-loopzero-system}"
SCOPED_LIB="$(cd "$(dirname "$0")/.." && pwd)/helmfile/scripts/lib/scoped-kubeconfig.sh"

log() { printf '[bootstrap-cell-default-cc] %s\n' "$*" >&2; }

if ! command -v k3d >/dev/null 2>&1 || ! command -v kubectl >/dev/null 2>&1 || ! command -v docker >/dev/null 2>&1; then
  log "k3d/kubectl/docker missing on PATH; skip"
  exit 0
fi

# 1. Resolve loopzero-workload server IP on the k3d-loopzero bridge.
WORKLOAD_IP=$(docker inspect k3d-loopzero-workload-server-0 \
  --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null \
  | head -1)
if [[ -z "$WORKLOAD_IP" ]]; then
  log "k3d-loopzero-workload-server-0 not running; skip"
  exit 0
fi

# 2. Get raw kubeconfig for loopzero-workload (needed by scoped-kubeconfig.sh
#    as the bootstrap kubeconfig — it talks to the workload apiserver to
#    create the SA + ClusterRole + Secret).
RAW_KC=$(mktemp)
trap 'rm -f "$RAW_KC"' EXIT

if ! k3d kubeconfig get loopzero-workload >"$RAW_KC" 2>/dev/null || [[ ! -s "$RAW_KC" ]]; then
  log "loopzero-workload kubeconfig unavailable"
  exit 0
fi

# Rewrite server URL to bridge IP so scoped-kubeconfig.sh can reach the
# workload apiserver from the host (k3d default points at localhost:port
# via LB which only works from outside the docker network).
sed -i "s|server: https://[^[:space:]]*|server: https://${WORKLOAD_IP}:6443|g" "$RAW_KC"

# 3. Mint SA token via scoped-kubeconfig.sh. Output is a kubeconfig with
#    a bearer token for the `loopzero-ctrl-runtime` SA in workload cluster's
#    loopzero-system namespace. SCOPED_KC will be the path printed by the lib.
SCOPED_KC=""
if [[ -x "$SCOPED_LIB" ]]; then
  # The lib prints "[scoped-kubeconfig] wrote /path/to/kubeconfig.yaml"
  # to stderr. We grep for the path token. Idempotent across runs.
  SCOPED_KC=$( "$SCOPED_LIB" workload "$RAW_KC" 2>&1 \
    | grep -oE '/[^[:space:]]+kube[^[:space:]]*\.yaml' | head -1 || echo "")
fi
if [[ -z "$SCOPED_KC" || ! -s "$SCOPED_KC" ]]; then
  log "scoped-kubeconfig.sh did not produce output; falling back to raw kc (B29 risk)"
  SCOPED_KC="$RAW_KC"
fi

# Rewrite scoped kubeconfig server URL too (in case lib left k3d default).
sed -i "s|server: https://[^[:space:]]*|server: https://${WORKLOAD_IP}:6443|g" "$SCOPED_KC"

# 4. Apply Secret loopzero-system/cell-default-kubeconfig with the bearer-token
#    kubeconfig.
kubectl --context "$CTX" create secret generic cell-default-kubeconfig \
  -n "$NS" \
  --from-file=kubeconfig="$SCOPED_KC" \
  --dry-run=client -o yaml \
  | kubectl --context "$CTX" apply -f - >/dev/null

# 5. Apply ClusterCredential cell-default with the resolved IP.
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
  # F-08 fix (2026-05-07): clusterName MUST equal cellID so the ArgoCD
  # cluster Secret registers as `name=cell-default` and matches what
  # the operator writes into ArgoCD App.spec.destination.name. Previously
  # clusterName=workload + operator using `server: <URL>` for cell-default
  # caused URL mismatch (operator env DNS hostname vs Secret IP), making
  # ArgoCD return InvalidSpecError on every sync. Aligning both sides on
  # logical name removes the URL-mismatch bug class entirely.
  clusterName: cell-default
  apiServer: https://${WORKLOAD_IP}:6443
  scope:
    argocd: true
    operators: true
  secretRef:
    name: cell-default-kubeconfig
    namespace: loopzero-system
YAML

log "cell-default CC + cell-default-kubeconfig (bearer-token kc, apiServer=https://${WORKLOAD_IP}:6443) applied"
