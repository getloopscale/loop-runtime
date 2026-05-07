#!/bin/bash
# Provisions API tokens for Gitea and ArgoCD, plus cross-cluster kubeconfigs.
# Idempotent — safe to run multiple times.
#
# Creates:
#   - loopzero-api-tokens Secret          (gitea-token + argocd-token)
#   - loopzero-build-kubeconfig ConfigMap  (build cluster access for API + operator)
#   - loopzero-runtime-kubeconfig ConfigMap (workload cluster access for operator)
#
# ArgoCD auth: Uses a dedicated 'loopzero-api' account with apiKey capability.
# Tokens from apiKey accounts do NOT expire (unlike admin session tokens which expire in 24h).
set -euo pipefail

MGMT_CTX="k3d-loopzero-mgmt"
BUILD_CTX="k3d-loopzero-build"
WORKLOAD_CTX="k3d-loopzero-workload"
# REVIEW-2026-03-30: Fixes MED-12 — was "loopzero-system" but Pulumi uses "loopzero-system".
# Local dev uses loopzero-system, production uses loopzero-system.
NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"

# REVIEW-2026-03-30: Fixes HIGH-06 — was "loopzero-admin", now "gitea_admin" to match Pulumi Helm config.
GITEA_USER="${GITEA_USER:-gitea_admin}"
GITEA_PASS="${GITEA_PASS:-loopzero-gitea-dev-2026}"

echo "=== Provisioning API tokens ==="

# ---------------------------------------------------------------------------
# 1. Gitea token
# ---------------------------------------------------------------------------
echo "Creating Gitea API token (repo + org scope)..."
kubectl --context "$MGMT_CTX" port-forward -n "$NAMESPACE" svc/gitea-http 13000:3000 &
GITEA_PORT_FORWARD_PID=$!
sleep 2

# Ensure 'loopzero' org exists (idempotent — Gitea returns 422 if already exists)
curl -s -X POST "http://localhost:13000/api/v1/orgs" \
  -u "${GITEA_USER}:${GITEA_PASS}" \
  -H "Content-Type: application/json" \
  -d '{"username":"loopzero","full_name":"LoopZero Platform","visibility":"private"}' > /dev/null 2>&1
echo "Gitea org 'loopzero': ensured"

# Scopes: write:repository (clone + push) + write:organization (create repos in org) + read:user
# Use fixed token name to avoid accumulating stale tokens on repeated runs (E-13).
TOKEN_NAME="loopzero-api"
# Delete existing token with this name (idempotent — 404 if not found)
curl -s -X DELETE "http://localhost:13000/api/v1/users/${GITEA_USER}/tokens/${TOKEN_NAME}" \
  -u "${GITEA_USER}:${GITEA_PASS}" > /dev/null 2>&1 || true
GITEA_TOKEN=$(curl -s -X POST "http://localhost:13000/api/v1/users/${GITEA_USER}/tokens" \
  -u "${GITEA_USER}:${GITEA_PASS}" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"${TOKEN_NAME}\",\"scopes\":[\"write:repository\",\"write:organization\",\"read:user\"]}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('sha1',''))")

kill "$GITEA_PORT_FORWARD_PID" 2>/dev/null; wait "$GITEA_PORT_FORWARD_PID" 2>/dev/null || true
echo "Gitea token: ${#GITEA_TOKEN} chars"

# ---------------------------------------------------------------------------
# 2. ArgoCD token
# ---------------------------------------------------------------------------
ARGOCD_TOKEN=""
if kubectl --context "$MGMT_CTX" get namespace argocd >/dev/null 2>&1; then
  echo "Creating ArgoCD API token (loopzero-api account, non-expiring)..."
  ARGOCD_PASS=$(kubectl --context "$MGMT_CTX" -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null)

  if [ -n "$ARGOCD_PASS" ]; then
    kubectl --context "$MGMT_CTX" port-forward -n argocd svc/argocd-server 18080:80 &
    ARGOCD_PORT_FORWARD_PID=$!
    sleep 2

    ADMIN_SESSION=$(curl -s -X POST "http://localhost:18080/api/v1/session" \
      -H "Content-Type: application/json" \
      -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASS\"}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

    if [ -n "$ADMIN_SESSION" ]; then
      ARGOCD_TOKEN=$(curl -s -X POST "http://localhost:18080/api/v1/account/loopzero-api/token" \
        -H "Authorization: Bearer $ADMIN_SESSION" \
        -H "Content-Type: application/json" \
        -d "{\"expiresIn\":0,\"id\":\"loopzero-api-$(date +%s)\"}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

      if [ -n "$ARGOCD_TOKEN" ]; then
        echo "ArgoCD token: ${#ARGOCD_TOKEN} chars (non-expiring, loopzero-api account)"
      else
        echo "WARNING: Failed to generate loopzero-api token."
      fi
    else
      echo "WARNING: Failed to get ArgoCD admin session."
    fi

    kill "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null; wait "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null || true
  else
    echo "WARNING: ArgoCD admin secret not found."
  fi
else
  echo "WARNING: ArgoCD namespace not found. Skipping."
fi

# ---------------------------------------------------------------------------
# 3. Create K8s secret with tokens
# ---------------------------------------------------------------------------
echo "Creating K8s secret loopzero-api-tokens..."
kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create secret generic loopzero-api-tokens \
  --from-literal=gitea-token="$GITEA_TOKEN" \
  --from-literal=argocd-token="$ARGOCD_TOKEN" \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -

# ---------------------------------------------------------------------------
# 4. ArgoCD repo credentials (so ArgoCD can clone from Gitea)
# ---------------------------------------------------------------------------
if kubectl --context "$MGMT_CTX" get namespace argocd >/dev/null 2>&1 && [ -n "$GITEA_TOKEN" ]; then
  echo "Setting up ArgoCD repo credentials..."
  # Use repo-creds (credential template) — matches ALL repos under this URL prefix.
  # This is more robust than per-repo secrets because tenant repos are created dynamically.
  #
  # F-09 fix (2026-05-07 cleanroom dogfood): URL MUST end with trailing slash.
  # Without it, ArgoCD's repo-creds prefix matching becomes non-deterministic —
  # GenerateManifest intermittently returns "authentication required" even
  # though git ls-remote with the same creds works. Trailing slash makes the
  # URL prefix unambiguous (any repo path that follows is clearly a sub-path),
  # and ArgoCD's internal URL normalizer reliably picks up the creds.
  kubectl --context "$MGMT_CTX" -n argocd create secret generic argocd-creds-gitea \
    --from-literal=type=git \
    --from-literal=url="http://gitea-http.${NAMESPACE}.svc.cluster.local:3000/" \
    --from-literal=username="${GITEA_USER}" \
    --from-literal=password="${GITEA_TOKEN}" \
    --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" -n argocd apply -f -
  kubectl --context "$MGMT_CTX" -n argocd label secret argocd-creds-gitea argocd.argoproj.io/secret-type=repo-creds --overwrite 2>/dev/null
  echo "ArgoCD repo credentials: configured (credential template, trailing-slash URL)"
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
  WORKLOAD_IP=$(docker inspect k3d-loopzero-workload-server-0 --format '{{(index .NetworkSettings.Networks "k3d-loopzero").IPAddress}}' 2>/dev/null)
  WORKLOAD_SERVER="https://${WORKLOAD_IP}:6443"
  echo "  Workload server: $WORKLOAD_SERVER"

  CLUSTER_CONFIG=$(python3 -c "
import json
config = {'tlsClientConfig': {'insecure': True, 'certData': '${WORKLOAD_CERT}', 'keyData': '${WORKLOAD_KEY}'}}
print(json.dumps(config))
")

  # F-08 fix (2026-05-07): name=cell-default + Secret cluster-cell-default
  # match what bootstrap-cell-default-cc.sh writes into ClusterCredential.spec.clusterName
  # and what the operator (real_provisioner.go) writes into App.spec.destination.name.
  # Previously name=workload mismatched the operator output, causing
  # InvalidSpecError on every ArgoCD sync. Aligning all three layers
  # (provision-api-tokens, bootstrap-cell-default-cc, operator) on
  # logical name 'cell-default' eliminates the URL-mismatch bug class.
  kubectl --context "$MGMT_CTX" -n argocd create secret generic cluster-cell-default \
    --from-literal=name=cell-default \
    --from-literal=server="${WORKLOAD_SERVER}" \
    --from-literal=config="${CLUSTER_CONFIG}" \
    --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" -n argocd apply -f -
  kubectl --context "$MGMT_CTX" -n argocd label secret cluster-cell-default argocd.argoproj.io/secret-type=cluster --overwrite 2>/dev/null
  # Cleanup: delete legacy cluster-workload Secret if present (idempotent).
  kubectl --context "$MGMT_CTX" -n argocd delete secret cluster-workload --ignore-not-found 2>/dev/null
  echo "ArgoCD cluster-cell-default: registered (insecure TLS, Docker hostname)"
fi

# ---------------------------------------------------------------------------
# 6. Build cluster kubeconfig
# ---------------------------------------------------------------------------
echo "Setting up build cluster kubeconfig..."

kubectl --context "$BUILD_CTX" apply -f - <<'RBAC'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: loopzero-workflow-submitter
  namespace: argo
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: loopzero-workflow-submitter
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
  name: loopzero-workflow-submitter
  namespace: argo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: loopzero-workflow-submitter
subjects:
  - kind: ServiceAccount
    name: loopzero-workflow-submitter
    namespace: argo
RBAC

BUILD_CERT=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
BUILD_KEY=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')
BUILD_SERVER="https://k3d-loopzero-build-server-0:6443"

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

kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create configmap loopzero-build-kubeconfig \
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
# loopzero-api pod runs inside k3d-loopzero-mgmt cluster and cannot resolve k3d container
# DNS names from inside cluster pods (B27: docker DNS only works between containers
# directly on the docker bridge, not from pod network). With DNS-based runtime
# kubeconfig, SecretK8sManager.runtimeDynamic could not reach the workload cluster,
# silently failing to create ExternalSecret CRs. Result: pod envFrom referenced a
# Secret that ESO never produced (because no ExternalSecret), pod hit
# CreateContainerConfigError, Journey 03 Secrets HTTP probe timed out.
RUNTIME_IP=$(docker inspect k3d-loopzero-workload-server-0 --format '{{(index .NetworkSettings.Networks "k3d-loopzero").IPAddress}}' 2>/dev/null)
if [ -z "$RUNTIME_IP" ]; then
  echo "ERROR: Cannot resolve k3d-loopzero-workload-server-0 docker IP — falling back to DNS hostname"
  RUNTIME_SERVER="https://k3d-loopzero-workload-server-0:6443"
else
  RUNTIME_SERVER="https://${RUNTIME_IP}:6443"
fi

cat > /tmp/runtime-kubeconfig.yaml <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${RUNTIME_SERVER}
    insecure-skip-tls-verify: true
  name: runtime
contexts:
- context:
    cluster: runtime
    user: runtime-admin
  name: runtime
current-context: runtime
users:
- name: runtime-admin
  user:
    client-certificate-data: ${RUNTIME_CERT}
    client-key-data: ${RUNTIME_KEY}
KUBECONFIG

kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create configmap loopzero-runtime-kubeconfig \
  --from-file=config=/tmp/runtime-kubeconfig.yaml \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -
rm -f /tmp/runtime-kubeconfig.yaml
echo "Runtime cluster kubeconfig: created (admin certs, insecure TLS)"

# ---------------------------------------------------------------------------
# 8. MinIO buckets (idempotent — mc mb --ignore-existing)
# ---------------------------------------------------------------------------
# The lifecycle Job can fail if MinIO isn't ready during Helm install.
# By this point MinIO has been running for several minutes — guaranteed ready.
echo "Ensuring MinIO buckets exist..."
MINIO_POD=$(kubectl --context "$MGMT_CTX" -n "$NAMESPACE" get pod -l app=minio -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$MINIO_POD" ]; then
  kubectl --context "$MGMT_CTX" -n "$NAMESPACE" exec "$MINIO_POD" -- sh -c "
    mc alias set local http://localhost:9000 \$MINIO_ROOT_USER \$MINIO_ROOT_PASSWORD 2>/dev/null
    mc mb --ignore-existing local/loopzero-builds
    mc mb --ignore-existing local/loopzero-artifacts
    mc mb --ignore-existing local/loopzero-static-assets
  " 2>&1 | grep -E "created|already"
  echo "MinIO buckets: ensured"
else
  echo "WARNING: MinIO pod not found — buckets not created"
fi

echo ""
echo "=== Done ==="
echo "Secret 'loopzero-api-tokens' created in $NAMESPACE"
echo "ConfigMap 'loopzero-build-kubeconfig' created in $NAMESPACE"
echo "ConfigMap 'loopzero-runtime-kubeconfig' created in $NAMESPACE"
echo "ArgoCD repo + cluster secrets configured"
echo "MinIO buckets ensured"
echo ""
echo "Restart pods to pick up new tokens:"
echo "  kubectl --context $MGMT_CTX -n $NAMESPACE rollout restart deployment/loopzero-api deployment/tenant-controller"
