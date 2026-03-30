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
NAMESPACE="loopzero-system"

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
  -u "REDACTED_SECRET_037" \
  -H "Content-Type: application/json" \
  -d '{"username":"loopzero","full_name":"LoopZero Platform","visibility":"private"}' > /dev/null 2>&1
echo "Gitea org 'loopzero': ensured"

TOKEN_NAME="loopzero-api"
curl -s -X DELETE "http://localhost:13000/api/v1/users/loopzero-admin/tokens/${TOKEN_NAME}" \
  -u "REDACTED_SECRET_037" > /dev/null 2>&1 || true
GITEA_TOKEN=$(curl -s -X POST "http://localhost:13000/api/v1/users/loopzero-admin/tokens" \
  -u "REDACTED_SECRET_037" \
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
# 4. Build cluster kubeconfig
# ---------------------------------------------------------------------------
# The API submits Argo Workflows (scoped to argo namespace).
# The operator creates namespaces, RBAC, quotas (cluster-wide).
# For local dev, we use admin certs with insecure TLS to handle
# cross-network certificate SAN mismatches between k3d clusters.
# Production uses scoped ServiceAccount tokens provisioned by Pulumi.
echo "Setting up build cluster kubeconfig..."

# API also needs a scoped SA for workflow submission (created here for Argo RBAC)
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

# For local dev: admin certs + insecure-skip-tls-verify.
# k3d API server certs contain SANs for Docker IPs that change on recreate.
# The cross-cluster networking script connects clusters to each other's networks,
# but the IPs used from mgmt → build differ from the SANs in the build cert.
BUILD_CERT=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
BUILD_KEY=$(kubectl --context "$BUILD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')

# Server URL: use the internal Docker hostname (resolved via Docker DNS on shared network)
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
# 5. Runtime (workload) cluster kubeconfig
# ---------------------------------------------------------------------------
# The operator creates namespaces, RBAC, NetworkPolicies, and ResourceQuotas
# on the workload cluster. It also creates PriorityClasses (cluster-scoped).
echo "Setting up runtime cluster kubeconfig..."

RUNTIME_CERT=$(kubectl --context "$WORKLOAD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}')
RUNTIME_KEY=$(kubectl --context "$WORKLOAD_CTX" config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}')

RUNTIME_SERVER="https://k3d-loopzero-workload-server-0:6443"

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

echo ""
echo "=== Done ==="
echo "Secret 'loopzero-api-tokens' created in $NAMESPACE"
echo "ConfigMap 'loopzero-build-kubeconfig' created in $NAMESPACE"
echo "ConfigMap 'loopzero-runtime-kubeconfig' created in $NAMESPACE"
echo ""
echo "Restart pods to pick up new tokens:"
echo "  kubectl --context $MGMT_CTX -n $NAMESPACE rollout restart deployment/loopzero-api deployment/tenant-controller"
