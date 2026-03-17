#!/bin/bash
# Provisions API tokens for Gitea and ArgoCD, and build cluster kubeconfig.
# Idempotent — safe to run multiple times.
#
# ArgoCD auth: Uses a dedicated 'loopzero-api' account with apiKey capability.
# Tokens from apiKey accounts do NOT expire (unlike admin session tokens which expire in 24h).
# The account is configured in infra/helm/argocd/values.yaml:
#   accounts.loopzero-api: apiKey   (no login — cannot access UI)
# RBAC: sync + get on applications only (least privilege).
set -euo pipefail

MGMT_CTX="k3d-loopzero-mgmt"
BUILD_CTX="k3d-loopzero-build"
NAMESPACE="loopzero-system"

echo "=== Provisioning API tokens ==="

# 1. Gitea token — minimal scope (read + write repos only)
#
# The API only needs: clone source repos + push to GitOps repos.
# Scopes: write:repository (includes read). Never use "all".
echo "Creating Gitea API token (repo scope only)..."
GITEA_PORT_FORWARD_PID=""
kubectl --context "$MGMT_CTX" port-forward -n "$NAMESPACE" svc/gitea-http 13000:3000 &
GITEA_PORT_FORWARD_PID=$!
sleep 2

GITEA_TOKEN=$(curl -s -X POST "http://localhost:13000/api/v1/users/loopzero-admin/tokens" \
  -u "REDACTED_SECRET_037" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"api-$(date +%s)\",\"scopes\":[\"write:repository\",\"read:user\"]}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('sha1',''))")

kill "$GITEA_PORT_FORWARD_PID" 2>/dev/null; wait "$GITEA_PORT_FORWARD_PID" 2>/dev/null || true
echo "Gitea token: ${#GITEA_TOKEN} chars"

# 2. ArgoCD token — dedicated 'loopzero-api' account with apiKey capability
#
# The 'loopzero-api' account is defined in argocd-cm (via Helm values):
#   accounts.loopzero-api: apiKey    (apiKey only, no login — cannot access UI)
# RBAC grants: sync + get on all applications (least privilege).
#
# Unlike admin session tokens (/api/v1/session), apiKey tokens do NOT expire.
# This eliminates silent 24h expiration that broke all deploys.
echo "Creating ArgoCD API token (loopzero-api account, non-expiring)..."
ARGOCD_PASS=$(kubectl --context "$MGMT_CTX" -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)

kubectl --context "$MGMT_CTX" port-forward -n argocd svc/argocd-server 18080:80 &
ARGOCD_PORT_FORWARD_PID=$!
sleep 2

# Step 1: Admin login to get temporary session (just for generating the service token)
ADMIN_SESSION=$(curl -s -X POST "http://localhost:18080/api/v1/session" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASS\"}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

if [ -z "$ADMIN_SESSION" ]; then
  echo "ERROR: Failed to get admin session. Is ArgoCD running?"
  echo "  Ensure the ArgoCD Helm chart has been upgraded with the loopzero-api account:"
  echo "    helm upgrade argocd infra/helm/argocd/ -n argocd"
  kill "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null; wait "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null || true
  exit 1
fi

# Step 2: Generate non-expiring API token for loopzero-api account
# expiresIn=0 means the token never expires (ArgoCD default for apiKey accounts)
ARGOCD_TOKEN=$(curl -s -X POST "http://localhost:18080/api/v1/account/loopzero-api/token" \
  -H "Authorization: Bearer $ADMIN_SESSION" \
  -H "Content-Type: application/json" \
  -d "{\"expiresIn\":\"0\",\"id\":\"loopzero-api-$(date +%s)\"}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

kill "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null; wait "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null || true

if [ -z "$ARGOCD_TOKEN" ]; then
  echo "ERROR: Failed to generate loopzero-api token."
  echo "  Ensure the 'loopzero-api' account exists in argocd-cm:"
  echo "    accounts.loopzero-api: apiKey"
  echo "  Run: helm upgrade argocd infra/helm/argocd/ -n argocd --reuse-values"
  exit 1
fi

echo "ArgoCD token: ${#ARGOCD_TOKEN} chars (non-expiring, loopzero-api account)"

# 3. Create/update K8s secret with both tokens
echo "Creating K8s secret loopzero-api-tokens..."
kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create secret generic loopzero-api-tokens \
  --from-literal=gitea-token="$GITEA_TOKEN" \
  --from-literal=argocd-token="$ARGOCD_TOKEN" \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -

# 4. Build cluster kubeconfig — scoped ServiceAccount (not admin)
#
# The API only needs: create/get/list Workflows + ConfigMaps in the argo namespace.
# We create a dedicated ServiceAccount + Role + RoleBinding in the build cluster,
# then extract a token for kubeconfig. This replaces the overly-broad admin cert.
echo "Setting up scoped build cluster access..."

# Create ServiceAccount + RBAC in build cluster (idempotent)
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
---
apiVersion: v1
kind: Secret
metadata:
  name: loopzero-workflow-submitter-token
  namespace: argo
  annotations:
    kubernetes.io/service-account.name: loopzero-workflow-submitter
type: kubernetes.io/service-account-token
RBAC

# Wait for token to be populated
sleep 3

# Extract the scoped token
BUILD_TOKEN=$(kubectl --context "$BUILD_CTX" -n argo get secret loopzero-workflow-submitter-token -o jsonpath='{.data.token}' | base64 -d)
BUILD_CA=$(kubectl --context "$BUILD_CTX" -n argo get secret loopzero-workflow-submitter-token -o jsonpath='{.data.ca\.crt}' | base64 -w0)

if [ -z "$BUILD_TOKEN" ]; then
  echo "WARNING: Could not get scoped build token. Falling back to admin cert."
  BUILD_CERT=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
  BUILD_KEY=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')
  BUILD_CA=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
  USE_TOKEN=false
else
  echo "Build cluster token: scoped to argo namespace (workflows + configmaps only)"
  USE_TOKEN=true
fi

# Rewrite server URL to be accessible from mgmt cluster
BUILD_SERVER_INTERNAL="https://k3d-loopzero-build-server-0:6443"

if [ "$USE_TOKEN" = true ]; then
cat > /tmp/build-kubeconfig.yaml <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${BUILD_SERVER_INTERNAL}
    certificate-authority-data: ${BUILD_CA}
  name: build
contexts:
- context:
    cluster: build
    user: loopzero-workflow-submitter
    namespace: argo
  name: build
current-context: build
users:
- name: loopzero-workflow-submitter
  user:
    token: ${BUILD_TOKEN}
KUBECONFIG
else
cat > /tmp/build-kubeconfig.yaml <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${BUILD_SERVER_INTERNAL}
    certificate-authority-data: ${BUILD_CA}
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
fi

# Create configmap with the kubeconfig
kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create configmap loopzero-build-kubeconfig \
  --from-file=config=/tmp/build-kubeconfig.yaml \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -
rm -f /tmp/build-kubeconfig.yaml

echo ""
echo "=== Done ==="
echo "Secret 'loopzero-api-tokens' created in $NAMESPACE"
echo "ConfigMap 'loopzero-build-kubeconfig' created in $NAMESPACE"
echo ""
echo "Restart the API pod to pick up new tokens:"
echo "  kubectl --context $MGMT_CTX -n $NAMESPACE rollout restart deployment/loopzero-api"
