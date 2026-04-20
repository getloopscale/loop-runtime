#!/usr/bin/env bash
# infra:validate — Post-deploy pre-flight check.
# Verifies ALL cross-cluster resources, connectivity, and configuration coherence.
# Run after every `pulumi up` to detect silent degradation before testing.
#
# Usage:
#   task infra:validate
#   # OR directly:
#   ./infra/scripts/validate-infra.sh [mgmt-context] [build-context] [runtime-context]
#
# Exit codes: 0 = all pass, 1 = failures found

set -euo pipefail

MGMT_CTX="${1:-do-nyc3-loopzero-infra-homolog-nyc3-mgmt}"
BUILD_CTX="${2:-do-nyc3-loopzero-infra-homolog-nyc3-build}"
RUNTIME_CTX="${3:-do-nyc3-loopzero-infra-homolog-nyc3-runtime}"
NS="platform-system"

PASS=0
FAIL=0

check() {
  local desc="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "  ✅ $desc"
    ((PASS++))
  else
    echo "  ❌ $desc"
    ((FAIL++))
  fi
}

echo "=== Infra Validation — $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
echo ""

# --- MGMT CLUSTER ---
echo "▸ Management Cluster ($MGMT_CTX)"
check "Cluster reachable" kubectl --context="$MGMT_CTX" cluster-info
check "loopzero-api pods Running" kubectl --context="$MGMT_CTX" -n "$NS" get pods -l app=loopzero-api -o jsonpath='{.items[0].status.phase}' | grep -q Running
check "loopzero-controller Running" kubectl --context="$MGMT_CTX" -n "$NS" get pods -l app=loopzero-controller -o jsonpath='{.items[0].status.phase}' | grep -q Running
check "ArgoCD server Running" kubectl --context="$MGMT_CTX" -n "$NS" get pods -l app.kubernetes.io/name=argocd-server -o jsonpath='{.items[0].status.phase}' | grep -q Running
check "Gitea Running" kubectl --context="$MGMT_CTX" -n "$NS" get pods -l app.kubernetes.io/name=gitea -o jsonpath='{.items[0].status.phase}' | grep -q Running
check "ArgoCD repo-creds internal" kubectl --context="$MGMT_CTX" -n "$NS" get secret argocd-repo-creds-internal
check "ArgoCD repo-creds external" kubectl --context="$MGMT_CTX" -n "$NS" get secret argocd-repo-creds-external
check "ClusterCredential Connected" kubectl --context="$MGMT_CTX" -n "$NS" get clustercredential -o jsonpath='{.items[0].status.connected}' | grep -q true
check "loopzero-platform-tokens exists" kubectl --context="$MGMT_CTX" -n "$NS" get secret loopzero-platform-tokens
check "DOCR pull secret (mgmt)" kubectl --context="$MGMT_CTX" -n "$NS" get secret loopzero-infra-registry
check "CRDs installed (tenants)" kubectl --context="$MGMT_CTX" get crd tenants.loopzero.io
check "CRDs installed (clustercredentials)" kubectl --context="$MGMT_CTX" get crd clustercredentials.loopzero.io
echo ""

# --- BUILD CLUSTER ---
echo "▸ Build Cluster ($BUILD_CTX)"
check "Cluster reachable" kubectl --context="$BUILD_CTX" cluster-info
check "StorageClass local-path" kubectl --context="$BUILD_CTX" get sc local-path
check "PSA privileged (argo ns)" kubectl --context="$BUILD_CTX" get ns argo -o jsonpath='{.metadata.labels.pod-security\.kubernetes\.io/enforce}' | grep -q privileged
check "RBAC loopzero-ctrl-build-rbac-admin" kubectl --context="$BUILD_CTX" get clusterrole loopzero-ctrl-build-rbac-admin
check "MinIO credentials Secret" kubectl --context="$BUILD_CTX" -n argo get secret loopzero-minio-credentials
check "Registry config Secret" kubectl --context="$BUILD_CTX" -n argo get secret buildkit-registry-config
check "Cosign signing key" kubectl --context="$BUILD_CTX" -n argo get secret cosign-signing-key
check "DOCR pull secret (build)" kubectl --context="$BUILD_CTX" -n argo get secret loopzero-infra-registry
check "Argo Workflows controller Running" kubectl --context="$BUILD_CTX" -n argo get pods -l app.kubernetes.io/name=argo-workflows-workflow-controller -o jsonpath='{.items[0].status.phase}' | grep -q Running
echo ""

# --- RUNTIME CLUSTER ---
echo "▸ Runtime Cluster ($RUNTIME_CTX)"
check "Cluster reachable" kubectl --context="$RUNTIME_CTX" cluster-info
check "Traefik Running" kubectl --context="$RUNTIME_CTX" -n "$NS" get pods -l app.kubernetes.io/name=traefik -o jsonpath='{.items[0].status.phase}' | grep -q Running
check "cert-manager Running" kubectl --context="$RUNTIME_CTX" get pods -n cert-manager -l app=cert-manager -o jsonpath='{.items[0].status.phase}' | grep -q Running
check "DOCR pull secret (runtime)" kubectl --context="$RUNTIME_CTX" -n "$NS" get secret loopzero-infra-registry
check "ESO Running" kubectl --context="$RUNTIME_CTX" -n external-secrets get pods -l app.kubernetes.io/name=external-secrets -o jsonpath='{.items[0].status.phase}' | grep -q Running
echo ""

# --- CROSS-CLUSTER CONNECTIVITY ---
echo "▸ Cross-Cluster Connectivity"
check "API can reach Gitea" kubectl --context="$MGMT_CTX" -n "$NS" exec deploy/loopzero-api -c api -- wget -qO- --timeout=3 "http://gitea-http.$NS.svc.cluster.local:3000/api/v1/version"
check "API can reach ArgoCD" kubectl --context="$MGMT_CTX" -n "$NS" exec deploy/loopzero-api -c api -- wget -qO- --timeout=3 "http://argocd-server:80/api/v1/applications" 2>/dev/null
echo ""

# --- CONFIGURATION COHERENCE ---
echo "▸ Configuration Coherence"
RELEASE_TIMEOUT=$(kubectl --context="$MGMT_CTX" -n "$NS" exec deploy/loopzero-controller -- env 2>/dev/null | grep RELEASE_PENDING_TIMEOUT | cut -d= -f2 || echo "2m")
SYNC_TIMEOUT=$(kubectl --context="$MGMT_CTX" -n "$NS" exec deploy/loopzero-controller -- env 2>/dev/null | grep ARGOCD_SYNC_TIMEOUT | cut -d= -f2 || echo "5m")
check "RELEASE_PENDING_TIMEOUT >= ARGOCD_SYNC_TIMEOUT" test "${RELEASE_TIMEOUT%%m*}" -ge "${SYNC_TIMEOUT%%m*}" 2>/dev/null
check "DynamicRouter active (cell count > 0)" kubectl --context="$MGMT_CTX" -n "$NS" logs -l app=loopzero-api -c api --tail=50 2>/dev/null | grep -q "DynamicRouter active"
echo ""

# --- SUMMARY ---
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  PASS: $PASS  |  FAIL: $FAIL"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "⚠️  $FAIL checks failed. Fix before running deploys or load tests."
  exit 1
fi

echo ""
echo "✅ All checks passed. Infrastructure ready for deploys."
exit 0
