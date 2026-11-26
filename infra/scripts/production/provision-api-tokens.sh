#!/bin/bash
# Provisions API tokens for Gitea and ArgoCD, plus cross-cluster kubeconfigs.
# Idempotent — safe to run multiple times.
#
# Creates:
#   - loopruntime-api-tokens Secret keys     (gitea-token + argocd-token, via loopruntime-ops seed)
#   - loopruntime-build-kubeconfig ConfigMap  (build cluster access for API + operator)
#   - loopruntime-cell-0-kubeconfig ConfigMap (workload cluster access for operator)
#   - loopruntime-cluster-kubeconfigs keys    (build + cell-0 for loopruntime-api, via loopruntime-ops seed)
#
# ArgoCD auth: Uses a dedicated 'loopruntime-api' account with apiKey capability.
# Tokens from apiKey accounts do NOT expire (unlike admin session tokens which expire in 24h).
set -euo pipefail

MGMT_CTX="k3d-loopruntime-core"
BUILD_CTX="k3d-loopruntime-build"
WORKLOAD_CTX="k3d-loopruntime-cell-0"
# REVIEW-2026-03-30: Fixes MED-12 — was "loopruntime-system" but Pulumi uses "loopruntime-system".
# Local dev uses loopruntime-system, production uses loopruntime-system.
NAMESPACE="${LOOPRUNTIME_NAMESPACE:-loopruntime-system}"

# loopruntime-ops is the Go home of the token provisioning this script used to carry
# as curl calls (#236). An installed binary wins; otherwise it is built from
# this checkout.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
loopruntime_ops() {
  if command -v loopruntime-ops >/dev/null 2>&1; then
    loopruntime-ops "$@"
  else
    (cd "$REPO_ROOT/cmd/loopruntime-ops" && go run . "$@")
  fi
}

echo "=== Provisioning API tokens ==="

# ---------------------------------------------------------------------------
# 1-4. Gitea token, ArgoCD token, ArgoCD repo credentials
# ---------------------------------------------------------------------------
# These are the `loopruntime-ops seed` implementations the post-Cleanroom chain runs
# as well (seed-post-cleanroom-all.sh steps 6, 6b and 13). This script used to
# carry a second, curl-based copy of each — a different Gitea token name and
# scope set, a different repo-creds URL, and a Secret write that replaced keys
# other producers own — so whichever ran last decided what the cluster held.
# Each seed reads the admin credentials from the cluster (gitea-admin,
# argocd-initial-admin-secret), writes only its own keys, and fails the run
# instead of storing an empty token.
export CORE_CONTEXT="$MGMT_CTX"
loopruntime_ops seed gitea-token
if kubectl --context "$MGMT_CTX" get namespace argocd >/dev/null 2>&1; then
  loopruntime_ops seed argocd-token
  loopruntime_ops seed argocd-gitea-creds
else
  echo "WARNING: ArgoCD namespace not found. Skipping the ArgoCD token and repo credentials."
fi

# ---------------------------------------------------------------------------
# 5. ArgoCD workload cluster registration
# ---------------------------------------------------------------------------
if kubectl --context "$MGMT_CTX" get namespace argocd >/dev/null 2>&1; then
  echo "Registering workload cluster in ArgoCD..."
  WORKLOAD_CERT=$(kubectl --context "$WORKLOAD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
  WORKLOAD_KEY=$(kubectl --context "$WORKLOAD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')
  # Use the same server URL that update-cross-cluster-ips.sh writes to WORKLOAD_CLUSTER_URL
  # in the operator deployment. The ArgoCD Application destination.server must match exactly.
  WORKLOAD_IP=$(docker inspect k3d-loopruntime-cell-0-server-0 --format '{{(index .NetworkSettings.Networks "k3d-loopruntime").IPAddress}}' 2>/dev/null)
  WORKLOAD_SERVER="https://${WORKLOAD_IP}:6443"
  echo "  Workload server: $WORKLOAD_SERVER"

  CLUSTER_CONFIG=$(python3 -c "
import json
config = {'tlsClientConfig': {'insecure': True, 'certData': '${WORKLOAD_CERT}', 'keyData': '${WORKLOAD_KEY}'}}
print(json.dumps(config))
")

  # F-08 fix (2026-05-07): name=cell-0 + Secret cluster-cell-0
  # match what bootstrap-cell-0-cc.sh writes into ClusterCredential.spec.clusterName
  # and what the operator (real_provisioner.go) writes into App.spec.destination.name.
  # Previously name=workload mismatched the operator output, causing
  # InvalidSpecError on every ArgoCD sync. Aligning all three layers
  # (provision-api-tokens, bootstrap-cell-0-cc, operator) on
  # logical name 'cell-0' eliminates the URL-mismatch bug class.
  kubectl --context "$MGMT_CTX" -n argocd create secret generic cluster-cell-0 \
    --from-literal=name=cell-0 \
    --from-literal=server="${WORKLOAD_SERVER}" \
    --from-literal=config="${CLUSTER_CONFIG}" \
    --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" -n argocd apply -f -
  kubectl --context "$MGMT_CTX" -n argocd label secret cluster-cell-0 argocd.argoproj.io/secret-type=cluster --overwrite 2>/dev/null
  # Cleanup: delete legacy cluster-workload Secret if present (idempotent).
  kubectl --context "$MGMT_CTX" -n argocd delete secret cluster-workload --ignore-not-found 2>/dev/null
  echo "ArgoCD cluster-cell-0: registered (insecure TLS, Docker hostname)"
fi

# ---------------------------------------------------------------------------
# 6. Build cluster kubeconfig
# ---------------------------------------------------------------------------
echo "Setting up build cluster kubeconfig..."

kubectl --context "$BUILD_CTX" apply -f - <<'RBAC'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: loopruntime-workflow-submitter
  namespace: argo
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: loopruntime-workflow-submitter
  namespace: argo
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["workflows"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: loopruntime-workflow-submitter
  namespace: argo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: loopruntime-workflow-submitter
subjects:
  - kind: ServiceAccount
    name: loopruntime-workflow-submitter
    namespace: argo
RBAC

BUILD_CERT=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
BUILD_KEY=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')
BUILD_SERVER="https://k3d-loopruntime-build-server-0:6443"

cat > /tmp/build-kubeconfig.yaml <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${BUILD_SERVER}
    insecure-skip-tls-verify: true
  name: build
contexts:
- context:
    cluster: build
    user: build-admin
  name: build
current-context: build
users:
- name: build-admin
  user:
    client-certificate-data: ${BUILD_CERT}
    client-key-data: ${BUILD_KEY}
KUBECONFIG

kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create configmap loopruntime-build-kubeconfig \
  --from-file=config=/tmp/build-kubeconfig.yaml \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -
rm -f /tmp/build-kubeconfig.yaml
echo "Build cluster kubeconfig: created (admin certs, insecure TLS)"

# ---------------------------------------------------------------------------
# 7. Runtime (workload) cluster kubeconfig
# ---------------------------------------------------------------------------
echo "Setting up runtime cluster kubeconfig..."

RUNTIME_CERT=$(kubectl --context "$WORKLOAD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
RUNTIME_KEY=$(kubectl --context "$WORKLOAD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')
# F-10 fix (2026-05-07 cleanroom dogfood): use docker-network IP, not DNS hostname.
# loopruntime-api pod runs inside k3d-loopruntime-core cluster and cannot resolve k3d container
# DNS names from inside cluster pods (B27: docker DNS only works between containers
# directly on the docker bridge, not from pod network). With DNS-based runtime
# kubeconfig, SecretK8sManager.runtimeDynamic could not reach the workload cluster,
# silently failing to create ExternalSecret CRs. Result: pod envFrom referenced a
# Secret that ESO never produced (because no ExternalSecret), pod hit
# CreateContainerConfigError, Journey 03 Secrets HTTP probe timed out.
RUNTIME_IP=$(docker inspect k3d-loopruntime-cell-0-server-0 --format '{{(index .NetworkSettings.Networks "k3d-loopruntime").IPAddress}}' 2>/dev/null)
if [ -z "$RUNTIME_IP" ]; then
  echo "ERROR: Cannot resolve k3d-loopruntime-cell-0-server-0 docker IP — falling back to DNS hostname"
  RUNTIME_SERVER="https://k3d-loopruntime-cell-0-server-0:6443"
else
  RUNTIME_SERVER="https://${RUNTIME_IP}:6443"
fi

# dogfood-2026-05-08-fixes T2.2 (F-2 root cause):
# Context name MUST be "in-cluster" to match CELL_0_KUBE_CONTEXT in
# infra/tilt/api-deployment.yaml + the charts/loopruntime-api Helmfile install
# (config.runtimeKubeContext in charts/loopruntime-api/values-bootstrap.yaml).
# B34 fix doc says the kubeconfig "ships with a single context named
# in-cluster", but F-11 (2026-05-07) regenerated it with name=runtime —
# silent drift that broke `loopscale secret set` (SecretK8sManager construct
# fails with `context "in-cluster" does not exist` → manager nil → ESO
# sync skipped → pod CreateContainerConfigError).
# Aligning the context name with mgmt + build (also in-cluster) restores
# consistency.
cat > /tmp/runtime-kubeconfig.yaml <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${RUNTIME_SERVER}
    insecure-skip-tls-verify: true
  name: in-cluster
contexts:
- context:
    cluster: in-cluster
    user: in-cluster-admin
  name: in-cluster
current-context: in-cluster
users:
- name: in-cluster-admin
  user:
    client-certificate-data: ${RUNTIME_CERT}
    client-key-data: ${RUNTIME_KEY}
KUBECONFIG

kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create configmap loopruntime-cell-0-kubeconfig \
  --from-file=config=/tmp/runtime-kubeconfig.yaml \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -

rm -f /tmp/runtime-kubeconfig.yaml
echo "Runtime cluster kubeconfig: created (admin certs, insecure TLS)"

# ---------------------------------------------------------------------------
# 7b. loopruntime-api's peer kubeconfigs (loopruntime-cluster-kubeconfigs: build + cell-0)
# ---------------------------------------------------------------------------
# F-11 (2026-05-07): generate-secrets.sh installs an in-cluster placeholder
# that points loopruntime-api at the core cluster, so ExternalSecret creation landed
# in the wrong cluster. This step used to patch the cell-0 key with the admin
# client certificates above; the heal's H2 then rewrote it with a 24h
# default-SA token, and a Python regen with a 720h one — three writers, three
# credential models. The keys have one minter now (#236), which needs the
# loopruntime-workflow-submitter SA created in section 6.
export CORE_CONTEXT="$MGMT_CTX"
loopruntime_ops seed cluster-kubeconfigs

# ---------------------------------------------------------------------------
# 8. MinIO buckets — not here
# ---------------------------------------------------------------------------
# MinIO buckets are declared once, in infra/helm/minio/values.yaml (`buckets:`), and
# created by the chart's own job during infra:install. This step used to create them a
# third time by running `mc` inside the server pod; loopruntime-minio carries no client since
# MinIO withdrew its images (#394), so it failed a cold setup and added nothing.

echo ""
echo "=== Done ==="
echo "Secret 'loopruntime-api-tokens' keys written in loopruntime-system (loopruntime-api and the tenant operator restarted by the seeds)"
echo "ConfigMap 'loopruntime-build-kubeconfig' created in $NAMESPACE"
echo "ConfigMap 'loopruntime-cell-0-kubeconfig' created in $NAMESPACE"
echo "loopruntime-cluster-kubeconfigs build + cell-0 keys written"
echo "ArgoCD repo + cluster secrets configured"
echo "MinIO buckets ensured"
