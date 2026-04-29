#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.4 / B7 fix
# Creates a ServiceAccount + ClusterRole + ClusterRoleBinding + Secret on the
# target cluster, and emits a kubeconfig scoped to that SA.
#
# B7 fix: ClusterRole has `resources: ["*"]` and `verbs: ["get","list","watch"]`
# (PRE-FIX: missing `resources: ["*"]` caused `argocd cluster list` to drop
# the cluster after first health probe failure).
#
# Usage:
#   scoped-kubeconfig.sh <cluster-id> <kubeconfig-of-target> [output-path]
#
# The TARGET kubeconfig is consumed only to bootstrap the SA — the OUTPUT
# kubeconfig is what callers (loopzero-api, ArgoCD) use.
set -euo pipefail

CLUSTER_ID="${1:?usage: $0 <cluster-id> <kubeconfig-of-target> [output-path]}"
TARGET_KUBECONFIG="${2:?usage: $0 <cluster-id> <kubeconfig-of-target> [output-path]}"
OUTPUT="${3:-${HOME}/.kube/loopzero-${CLUSTER_ID}-scoped.yaml}"

SA_NAME="loopzero-ctrl-runtime"
NAMESPACE="loopzero-system"
CLUSTER_ROLE="loopzero-ctrl-runtime-rbac-admin"

log() { printf '[scoped-kubeconfig] %s\n' "$*" >&2; }

export KUBECONFIG="$TARGET_KUBECONFIG"

# 1) Namespace
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# 2) ServiceAccount
kubectl create serviceaccount "$SA_NAME" -n "$NAMESPACE" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# 3) ClusterRole — B7 + B81 fix.
# B7  (CHG-2026-04-28-LAYERED-BOOTSTRAP / 2026-04-28): cluster-scoped read on
#     `*/*` so ArgoCD's resource cache doesn't drop the cluster after the first
#     health probe.
# B81 (end-to-end-deploy-validation-plan iter 12 / 2026-04-29): the original
#     write list (pods/services/deployments/ingresses on core/apps/networking)
#     was too narrow — LoopZero-generated tenant manifests target Traefik
#     (Middleware, IngressRoute) and cert-manager (Certificate, Issuer)
#     CRDs as well as KEDA / ESO / Prometheus operator types. ArgoCD would
#     reach the cluster but get RBAC-forbidden on the apply, leaving Apps
#     OutOfSync with health=Missing forever. We grant create/update/patch/
#     delete on the framework CRDs LoopZero controllers depend on, plus the
#     namespace + RBAC bindings that the API renders for new tenants.
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${CLUSTER_ROLE}
  labels:
    loopzero.io/managed-by: loopzero-platform
rules:
  # Cluster-scoped read on all resources (B7).
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
  # Namespace lifecycle — ArgoCD CreateNamespace=true sync option.
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["create", "update", "patch"]
  # Core / apps / networking / batch / autoscaling / policy.
  - apiGroups: ["", "apps", "networking.k8s.io", "batch", "autoscaling", "policy", "discovery.k8s.io"]
    resources: ["*"]
    verbs: ["create", "update", "patch", "delete"]
  # Framework CRDs the tenant manifests depend on.
  - apiGroups:
      - traefik.io
      - traefik.containo.us
      - cert-manager.io
      - acme.cert-manager.io
      - keda.sh
      - external-secrets.io
      - monitoring.coreos.com
    resources: ["*"]
    verbs: ["create", "update", "patch", "delete"]
  # RBAC for tenant ServiceAccounts.
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings"]
    verbs: ["create", "update", "patch", "delete"]
EOF

# 4) ClusterRoleBinding
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${SA_NAME}-binding
  labels:
    loopzero.io/managed-by: loopzero-platform
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${CLUSTER_ROLE}
subjects:
  - kind: ServiceAccount
    name: ${SA_NAME}
    namespace: ${NAMESPACE}
EOF

# 5) Token secret
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: ${SA_NAME}-token
  namespace: ${NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: ${SA_NAME}
type: kubernetes.io/service-account-token
EOF

# 6) Wait for token to be populated
for _ in 1 2 3 4 5 6 7 8 9 10; do
  TOKEN=$(kubectl get secret "${SA_NAME}-token" -n "$NAMESPACE" -o jsonpath='{.data.token}' 2>/dev/null | base64 -d 2>/dev/null || true)
  if [[ -n "$TOKEN" ]]; then break; fi
  sleep 1
done
if [[ -z "${TOKEN:-}" ]]; then
  log "ERROR: SA token not populated after 10s"
  exit 1
fi

CA_DATA=$(kubectl get secret "${SA_NAME}-token" -n "$NAMESPACE" -o jsonpath='{.data.ca\.crt}')
SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')

# B82 (end-to-end-deploy-validation-plan iter 12 / 2026-04-29): in k3d local
# clusters the kubeconfig server is `https://0.0.0.0:6444` (or similar) — that
# resolves to localhost inside the mgmt-cluster ArgoCD pod and yields silent
# OutOfSync with no resources applied. Substitute the docker-bridge IP of the
# target cluster's *server-0* container, which IS in the cert SAN list and IS
# routable from inside the mgmt cluster. DOKS / cloud kubeconfigs already use
# routable LB hostnames, so this rewrite is a no-op there.
if [[ "$SERVER" =~ ^https?://(0\.0\.0\.0|127\.0\.0\.1|localhost)(:.*)?$ ]]; then
  K3D_SERVER_CONTAINER="k3d-loopzero-${CLUSTER_ID}-server-0"
  K3D_SERVER_IP=$(docker inspect "$K3D_SERVER_CONTAINER" \
    -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || true)
  if [[ -n "$K3D_SERVER_IP" ]]; then
    SERVER="https://${K3D_SERVER_IP}:6443"
    log "B82: rewrote kubeconfig server -> $SERVER (was localhost-only via k3d port mapping)"
  else
    log "B82 WARN: could not resolve docker bridge IP for $K3D_SERVER_CONTAINER — leaving SERVER=$SERVER"
  fi
fi

# 7) Emit scoped kubeconfig
mkdir -p "$(dirname "$OUTPUT")"
cat > "$OUTPUT" <<EOF
apiVersion: v1
kind: Config
current-context: ${CLUSTER_ID}
clusters:
  - name: ${CLUSTER_ID}
    cluster:
      server: ${SERVER}
      certificate-authority-data: ${CA_DATA}
contexts:
  - name: ${CLUSTER_ID}
    context:
      cluster: ${CLUSTER_ID}
      namespace: ${NAMESPACE}
      user: ${SA_NAME}
users:
  - name: ${SA_NAME}
    user:
      token: ${TOKEN}
EOF
chmod 600 "$OUTPUT"
log "scoped kubeconfig written: $OUTPUT"
echo "$OUTPUT"
