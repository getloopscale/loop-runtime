#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.4 / B6 fix
# Registers a runtime cluster as an ArgoCD-managed cluster Secret.
#
# B6 fix: the ArgoCD cluster Secret expects `tlsClientConfig.caData` to be
# base64-encoded raw PEM. The kubeconfig already stores it base64 — DO NOT
# double-encode. We DECODE the kubeconfig field, then re-encode for the Secret.
#
# Usage:
#   argocd-register.sh <cluster-name> <kubeconfig-path>
#
# Idempotent (kubectl apply --dry-run pattern).
set -euo pipefail

CLUSTER_NAME="${1:?usage: $0 <cluster-name> <kubeconfig-path>}"
KUBECONFIG_PATH="${2:?usage: $0 <cluster-name> <kubeconfig-path>}"

# Issue #117 (generalizing #115) — pin the target cluster BY CONSTRUCTION.
#
# Note the asymmetry, it is the whole point of this script: $KUBECONFIG_PATH
# describes the cluster being REGISTERED (read as a file, parsed for server +
# CA), while the ArgoCD cluster Secret this script APPLIES must land on MGMT,
# where ArgoCD runs. Those are two different clusters. Before this pin the
# apply inherited the ambient kubecontext, so registering a runtime cluster
# while that context pointed at the runtime cluster wrote the Secret into the
# workload cluster — ArgoCD never saw it, and the failure surfaced much later
# as a cluster missing from `argocd cluster list`.
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loop-runtime-core}"
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

log() { printf '[argocd-register] %s\n' "$*" >&2; }

# yq is preferred; fall back to Python PyYAML if absent.
read_kubeconfig_field() {
  local field="$1" path="$2"
  if command -v yq >/dev/null 2>&1; then
    yq -r "$field" "$path"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "
import yaml, sys
with open('$path') as f:
    doc = yaml.safe_load(f)
def lookup(d, expr):
    # Convert .clusters[0].cluster.server style → step list
    steps = []
    for part in expr.lstrip('.').split('.'):
        if '[' in part:
            name, idx = part.split('[')
            idx = int(idx.rstrip(']'))
            steps.append((name, idx))
        else:
            steps.append((part, None))
    for name, idx in steps:
        if d is None: return ''
        d = d.get(name, None) if isinstance(d, dict) else d
        if idx is not None and isinstance(d, list):
            d = d[idx] if idx < len(d) else None
    return d if d is not None else ''
print(lookup(doc, '$field'))
"
  else
    log "ERROR: neither yq nor python3 available"
    exit 1
  fi
}

SERVER=$(read_kubeconfig_field ".clusters[0].cluster.server" "$KUBECONFIG_PATH")
if [[ -z "$SERVER" ]]; then
  log "ERROR: clusters[0].cluster.server missing in $KUBECONFIG_PATH"
  exit 1
fi

# B6 — kubeconfig stores caData as base64; decode to raw PEM, then encode for Secret.
CA_DATA=$(read_kubeconfig_field ".clusters[0].cluster.certificate-authority-data" "$KUBECONFIG_PATH")
if [[ -z "$CA_DATA" || "$CA_DATA" == "null" ]]; then
  log "ERROR: certificate-authority-data missing in $KUBECONFIG_PATH"
  exit 1
fi

# Sanity: decoded form must be PEM.
DECODED_HEAD=$(printf '%s' "$CA_DATA" | base64 -d 2>/dev/null | head -c 27)
if [[ "$DECODED_HEAD" != "-----BEGIN CERTIFICATE-----" ]]; then
  log "ERROR: caData is not valid base64-encoded PEM (got: $DECODED_HEAD)"
  exit 1
fi

TOKEN=$(read_kubeconfig_field ".users[0].user.token" "$KUBECONFIG_PATH")
if [[ -z "$TOKEN" ]]; then
  log "ERROR: no token found in $KUBECONFIG_PATH (need ServiceAccount-based kubeconfig)"
  exit 1
fi

CONFIG_JSON=$(printf '{"bearerToken":"%s","tlsClientConfig":{"caData":"%s"}}' "$TOKEN" "$CA_DATA")

# Create the Secret (ArgoCD watches namespace argocd for label argocd.argoproj.io/secret-type=cluster).
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: ${CLUSTER_NAME}-argocd-cluster
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: cluster
    runtime.getloopscale.com/managed-by: loop-runtime-platform
type: Opaque
stringData:
  name: ${CLUSTER_NAME}
  server: ${SERVER}
  config: |-
    ${CONFIG_JSON}
EOF
log "ArgoCD cluster registered: $CLUSTER_NAME ($SERVER)"
