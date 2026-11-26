#!/usr/bin/env bash
# update-cross-cluster-ips.sh — Updates cross-cluster IPs across all deployment manifests.
#
# k3d clusters share the `k3d-loopruntime` Docker network. Server node IPs are used
# for cross-cluster communication (NodePorts accessible on server nodes).
# LB IPs are used for HTTP traffic (Traefik on workload).
#
# Components patched:
#   - K8s ConfigMap loopruntime-api@loopruntime-system on mgmt cluster (Tilt-rendered from charts/loopruntime-api):
#       MINIO_CROSS_CLUSTER_ENDPOINT, GITEA_CROSS_CLUSTER_URL,
#       API_CROSS_CLUSTER_URL, WORKLOAD_INGRESS_URL
#       (post-T1.1.b commit 3fbde9a13: api-deployment.yaml uses envFrom now)
#   - Build cluster secrets                 (loopruntime-minio-credentials MINIO_ENDPOINT)
#   - infra/helm/promtail/values.yaml       (Loki cross-cluster endpoint)
#
# It edits no file git tracks (#264): the committed cross-cluster addresses are
# container names (monitoring scrape targets, the operators' WORKLOAD_CLUSTER_URL),
# which Docker's DNS resolves on the k3d network, so nothing needs rewriting.
#
# ArgoCD cluster-workload secret and repo credentials are managed by
# provision-api-tokens.sh (uses Docker hostnames, not IPs).
#
# Run after: task setup OR task clusters:create
# Usage: bash infra/scripts/update-cross-cluster-ips.sh
set -euo pipefail

SHARED_NETWORK="k3d-loopruntime"
DEPLOYMENT="infra/tilt/api-deployment.yaml"

if [ ! -f "$DEPLOYMENT" ]; then
  echo "ERROR: $DEPLOYMENT not found. Run from repo root."
  exit 1
fi

echo "=== Detecting cross-cluster IPs on ${SHARED_NETWORK} network ==="

# Helper: get IP on the shared network
get_ip() {
  docker inspect "$1" --format "{{(index .NetworkSettings.Networks \"${SHARED_NETWORK}\").IPAddress}}" 2>/dev/null || true
}

# Mgmt server node IP (NodePorts: 30000=Gitea, 30001=API, 32000=MinIO)
MGMT_SERVER=$(get_ip k3d-loopruntime-core-server-0)
if [ -z "$MGMT_SERVER" ]; then
  echo "ERROR: Cannot find mgmt server IP on ${SHARED_NETWORK}."
  echo "  Are all clusters running? Run: k3d cluster list"
  exit 1
fi
echo "  Mgmt server: $MGMT_SERVER (Gitea:30000, API:30001, MinIO:32000)"

# Workload LB IP (HTTP/HTTPS traffic via Traefik)
WORKLOAD_LB=$(get_ip k3d-loopruntime-cell-0-serverlb)
if [ -z "$WORKLOAD_LB" ]; then
  echo "ERROR: Cannot find workload LB IP on ${SHARED_NETWORK}."
  exit 1
fi
echo "  Workload LB: $WORKLOAD_LB (Traefik:80/443)"


# ---------------------------------------------------------------------------
# Patch API ConfigMap (loopruntime-api)
#
# Pre-T1.1.b (commit 3fbde9a13): these 4 envs were inline in
# infra/tilt/api-deployment.yaml; this script sed-patched the file.
#
# Post-T1.1.b: the 4 envs migrated to envFrom: configMapRef:
# loopruntime-api — rendered by the Tiltfile from charts/loopruntime-api with
# infra/tilt/loopruntime-api-values.yaml (it was loopruntime-api-runtime-config until the
# bootstrap chart infra/helm/loopruntime-api was deleted) (per code-review-remediation-147-findings
# no-hardcode mandate). This script now patches the LIVE ConfigMap on
# the mgmt cluster post-helmfile-sync, then triggers a rollout restart
# so the API pod's envFrom re-reads.
#
# Idempotent: kubectl patch is no-op when target equals current. Robust
# to docker network IP drift between runs (mgmt server's NodePort IP
# may differ run-to-run).
# ---------------------------------------------------------------------------
echo ""
echo "=== Patching loopruntime-api ConfigMap (mgmt cluster) ==="

MGMT_CTX="k3d-loopruntime-core"
API_CM_NS="loopruntime-system"
API_CM_NAME="loopruntime-api"

# Helper: patch ConfigMap key idempotently (no-op when value unchanged).
patch_cm_key() {
  local key="$1" new_value="$2"
  local current
  current=$(kubectl --context "$MGMT_CTX" get configmap "$API_CM_NAME" -n "$API_CM_NS" \
    -o jsonpath="{.data.${key}}" 2>/dev/null || true)
  if [ "$current" = "$new_value" ]; then
    echo "  ${key}: already correct ($new_value)"
    return 0
  fi
  # Use --type=merge with JSON body so we don't blow away other keys
  kubectl --context "$MGMT_CTX" patch configmap "$API_CM_NAME" -n "$API_CM_NS" \
    --type=merge -p "{\"data\":{\"${key}\":\"${new_value}\"}}" >/dev/null
  echo "  ${key}: ${current:-<unset>} → ${new_value}"
}

if kubectl --context "$MGMT_CTX" get configmap "$API_CM_NAME" -n "$API_CM_NS" >/dev/null 2>&1; then
  patch_cm_key "MINIO_CROSS_CLUSTER_ENDPOINT" "http://${MGMT_SERVER}:32000"
  patch_cm_key "GITEA_CROSS_CLUSTER_URL"      "http://${MGMT_SERVER}:30000"
  patch_cm_key "API_CROSS_CLUSTER_URL"        "http://${MGMT_SERVER}:30001"
  patch_cm_key "WORKLOAD_INGRESS_URL"         "http://${WORKLOAD_LB}:80"

  # Trigger rollout restart so envFrom re-reads the ConfigMap.
  # Idempotent: kubectl rollout restart adds a new annotation each call,
  # but the pod is only respun if a deploy actually exists.
  if kubectl --context "$MGMT_CTX" get deployment loopruntime-api -n "$API_CM_NS" >/dev/null 2>&1; then
    kubectl --context "$MGMT_CTX" rollout restart deployment loopruntime-api -n "$API_CM_NS" >/dev/null
    echo "  Triggered rollout restart of loopruntime-api Deployment to pick up ConfigMap changes"
  else
    echo "  loopruntime-api Deployment not yet present — Tilt will pick up ConfigMap on first apply"
  fi
else
  echo "WARNING: ConfigMap ${API_CM_NAME} not found in ${API_CM_NS} on ${MGMT_CTX}"
  echo "  Skipping API runtime URL patches — Tilt apply will create the ConfigMap separately"
  echo "  (the ConfigMap source is charts/loopruntime-api/templates/configmap.yaml, rendered by the Tiltfile)"
fi

# ---------------------------------------------------------------------------
# Build cluster MinIO credentials (cross-cluster endpoint)
# ---------------------------------------------------------------------------
echo ""
echo "=== Patching build cluster secrets ==="

BUILD_KUBECONFIG=$(k3d kubeconfig write loopruntime-build 2>/dev/null || true)
if [ -n "$BUILD_KUBECONFIG" ]; then
  # loopruntime-minio-credentials — MINIO_ENDPOINT used by CWTs
  CURRENT_MINIO=$(kubectl --kubeconfig="$BUILD_KUBECONFIG" get secret loopruntime-minio-credentials -n argo \
    -o jsonpath='{.data.MINIO_ENDPOINT}' 2>/dev/null | base64 -d 2>/dev/null || true)
  NEW_MINIO="http://${MGMT_SERVER}:32000"
  if [ -n "$CURRENT_MINIO" ] && [ "$CURRENT_MINIO" != "$NEW_MINIO" ]; then
    ENCODED=$(echo -n "$NEW_MINIO" | base64 -w0)
    kubectl --kubeconfig="$BUILD_KUBECONFIG" patch secret loopruntime-minio-credentials -n argo \
      -p "{\"data\":{\"MINIO_ENDPOINT\":\"${ENCODED}\"}}" 2>&1
    echo "  loopruntime-minio-credentials MINIO_ENDPOINT: $CURRENT_MINIO → $NEW_MINIO"
  else
    echo "  loopruntime-minio-credentials MINIO_ENDPOINT: already correct ($NEW_MINIO)"
  fi
else
  echo "WARNING: Cannot get kubeconfig for loopruntime-build — skipping"
fi

# ---------------------------------------------------------------------------
# Promtail values — Loki cross-cluster endpoint (build → mgmt)
# Promtail runs on the build cluster and ships logs to Loki on mgmt via NodePort.
# ---------------------------------------------------------------------------
PROMTAIL_VALUES="infra/helm/promtail/values.yaml"
if [ -f "$PROMTAIL_VALUES" ]; then
  echo ""
  echo "=== Patching $PROMTAIL_VALUES ==="
  NEW_LOKI_URL="http://${MGMT_SERVER}:30094/loki/api/v1/push"
  OLD_LOKI_URL=$(grep -o 'http://[^/]*:30094/loki/api/v1/push' "$PROMTAIL_VALUES" 2>/dev/null || true)
  if [ -z "$OLD_LOKI_URL" ]; then
    # First time — replace placeholder or cluster-local URL
    sed -i "s|url: http://.*loki.*/loki/api/v1/push|url: ${NEW_LOKI_URL}|" "$PROMTAIL_VALUES"
    sed -i "s|url: http://MGMT_SERVER_IP:30094/loki/api/v1/push|url: ${NEW_LOKI_URL}|" "$PROMTAIL_VALUES"
    echo "  Loki URL: → $NEW_LOKI_URL"
  elif [ "$OLD_LOKI_URL" != "$NEW_LOKI_URL" ]; then
    sed -i "s|${OLD_LOKI_URL}|${NEW_LOKI_URL}|" "$PROMTAIL_VALUES"
    echo "  Loki URL: $OLD_LOKI_URL → $NEW_LOKI_URL"
  else
    echo "  Loki URL: already correct ($NEW_LOKI_URL)"
  fi
fi

echo ""
echo "=== Done ==="
echo "Cross-cluster IPs updated. Run 'tilt up' to apply."
