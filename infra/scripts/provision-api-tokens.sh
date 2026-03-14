#!/bin/bash
# Provisions API tokens for Gitea and ArgoCD, and build cluster kubeconfig.
# Idempotent — safe to run multiple times.
set -euo pipefail

MGMT_CTX="k3d-loopzero-mgmt"
BUILD_CTX="k3d-loopzero-build"
NAMESPACE="loopzero-system"

echo "=== Provisioning API tokens ==="

# 1. Gitea token
echo "Creating Gitea API token..."
GITEA_PORT_FORWARD_PID=""
kubectl --context "$MGMT_CTX" port-forward -n "$NAMESPACE" svc/gitea-http 13000:3000 &
GITEA_PORT_FORWARD_PID=$!
sleep 2

GITEA_TOKEN=$(curl -s -X POST "http://localhost:13000/api/v1/users/loopzero-admin/tokens" \
  -u "REDACTED_SECRET_037" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"api-$(date +%s)\",\"scopes\":[\"all\"]}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('sha1',''))")

kill "$GITEA_PORT_FORWARD_PID" 2>/dev/null; wait "$GITEA_PORT_FORWARD_PID" 2>/dev/null || true
echo "Gitea token: ${#GITEA_TOKEN} chars"

# 2. ArgoCD token
echo "Creating ArgoCD API token..."
ARGOCD_PASS=$(kubectl --context "$MGMT_CTX" -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)

kubectl --context "$MGMT_CTX" port-forward -n argocd svc/argocd-server 18080:80 &
ARGOCD_PORT_FORWARD_PID=$!
sleep 2

ARGOCD_TOKEN=$(curl -s -X POST "http://localhost:18080/api/v1/session" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASS\"}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

kill "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null; wait "$ARGOCD_PORT_FORWARD_PID" 2>/dev/null || true
echo "ArgoCD token: ${#ARGOCD_TOKEN} chars"

# 3. Create/update K8s secret with both tokens
echo "Creating K8s secret loopzero-api-tokens..."
kubectl --context "$MGMT_CTX" -n "$NAMESPACE" create secret generic loopzero-api-tokens \
  --from-literal=gitea-token="$GITEA_TOKEN" \
  --from-literal=argocd-token="$ARGOCD_TOKEN" \
  --dry-run=client -o yaml | kubectl --context "$MGMT_CTX" apply -f -

# 4. Build cluster kubeconfig
echo "Extracting build cluster kubeconfig..."
BUILD_SERVER=$(kubectl --context "$BUILD_CTX" config view --minify -o jsonpath='{.clusters[0].cluster.server}')
BUILD_CA=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
BUILD_TOKEN_NAME=$(kubectl --context "$BUILD_CTX" -n argo get serviceaccount default -o jsonpath='{.secrets[0].name}' 2>/dev/null || echo "")

# For k3d, extract the user credentials directly
BUILD_CERT=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
BUILD_KEY=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')

# Rewrite server URL to be accessible from mgmt cluster
# k3d clusters share the Docker network, so we use the k3d server container name
BUILD_SERVER_INTERNAL="https://k3d-loopzero-build-server-0:6443"

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
