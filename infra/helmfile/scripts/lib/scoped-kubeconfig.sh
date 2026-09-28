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
  # F-22 (argo-rollouts-progressive-delivery-plan, 2026-05-07): added
  # argoproj.io for Rollouts/AnalysisRuns/AnalysisTemplates/Experiments.
  # Without this, tenant manifests render Rollout CRs but ArgoCD cannot
  # apply them — the App stays OutOfSync with health=Missing forever and
  # the workload pods never start (404 from Traefik). Discovered LIVE
  # during fresh-cleanroom Ginkgo journey run when 5/7 specs failed at
  # HTTP 200 because no pods came up; ArgoCD log: 'rollouts.argoproj.io
  # is forbidden: User "loopzero-ctrl-runtime" cannot create resource
  # "rollouts" in API group "argoproj.io"'.
  - apiGroups:
      - traefik.io
      - traefik.containo.us
      - cert-manager.io
      - acme.cert-manager.io
      - keda.sh
      - external-secrets.io
      - monitoring.coreos.com
      - argoproj.io
    resources: ["*"]
    verbs: ["create", "update", "patch", "delete"]
  # RBAC for tenant ServiceAccounts.
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings"]
    verbs: ["create", "update", "patch", "delete"]
  # The per-plan PriorityClass the tenant reconcile writes on the cell (#408). Shared
  # across tenants of a plan and never deleted with one, so no delete verb.
  - apiGroups: ["scheduling.k8s.io"]
    resources: ["priorityclasses"]
    verbs: ["create", "update", "patch"]
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

# T7.3 of openbao-adoption v1.4: try Vault kubernetes engine FIRST. When
# Vault is available + the engine + role exist, fetch a short-lived (TTL 1h)
# SA token instead of creating the legacy type=kubernetes.io/service-account-token
# Secret (long-lived static token). Legacy path remains as fallback for cold-
# bootstrap (chicken-and-egg before openbao-init runs) and for environments
# without Vault stack.
SCRIPT_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./vault-k8s-engine-token.sh
[ -f "${SCRIPT_LIB_DIR}/vault-k8s-engine-token.sh" ] && source "${SCRIPT_LIB_DIR}/vault-k8s-engine-token.sh"

TOKEN=""
if declare -f mint_vault_k8s_token >/dev/null 2>&1; then
  # Vault role naming convention (T7.2): loopzero-cell-0-<cluster-type>.
  # Map CLUSTER_ID prefix to role name; default to loopzero-cell-0-mgmt.
  case "${CLUSTER_ID:-}" in
    build*) VAULT_ROLE="loopzero-cell-0-build" ;;
    cell*|workload*) VAULT_ROLE="loopzero-cell-0-workload" ;;
    *)       VAULT_ROLE="loopzero-cell-0-mgmt" ;;
  esac
  if TOKEN=$(mint_vault_k8s_token "${VAULT_ROLE}" "${NAMESPACE}" 2>/dev/null) && [[ -n "${TOKEN}" ]]; then
    log "T7.3: SA token fetched via Vault kubernetes engine role=${VAULT_ROLE} (TTL 1h)"
  else
    log "T7.3: Vault kubernetes engine unavailable — falling back to legacy SA-token Secret"
    TOKEN=""
  fi
fi

# Legacy path (T7.3 fallback) — only when Vault engine did NOT return a token.
if [[ -z "${TOKEN}" ]]; then
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
fi

# CA cert is always read from K8s API (per pattern; not from Vault) since
# the kube-root-ca.crt ConfigMap is the canonical CA source (also see plan
# 2026-05-25-live-ca-fetch-clustercredential).
CA_DATA=$(kubectl get secret "${SA_NAME}-token" -n "$NAMESPACE" -o jsonpath='{.data.ca\.crt}' 2>/dev/null || true)
if [[ -z "${CA_DATA}" ]]; then
  # When Vault path is used + no legacy Secret was created, fall back to
  # the kube-root-ca.crt ConfigMap (canonical CA source per ADR).
  CA_DATA=$(kubectl get cm kube-root-ca.crt -n "$NAMESPACE" -o jsonpath='{.data.ca\.crt}' 2>/dev/null | base64 -w0)
fi
SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')

# B82 (end-to-end-deploy-validation-plan iter 12 / 2026-04-29): in k3d local
# clusters the kubeconfig server is `https://0.0.0.0:6444` (or similar) — that
# resolves to localhost inside the core-cluster ArgoCD pod and yields silent
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
