#!/usr/bin/env bash
# update-cross-cluster-ips.sh — Updates cross-cluster IPs across all deployment manifests.
#
# k3d clusters share the `k3d-loopzero` Docker network. Server node IPs are used
# for cross-cluster communication (NodePorts accessible on server nodes).
# LB IPs are used for HTTP traffic (Traefik on workload).
#
# Components patched:
#   - infra/tilt/api-deployment.yaml       (GITEA/API cross-cluster URLs, workload ingress)
#   - infra/tilt/operator-deployment.yaml   (WORKLOAD_CLUSTER_URL for ArgoCD Applications)
#   - Build cluster secrets                 (loopzero-minio-credentials MINIO_ENDPOINT)
#   - infra/helm/monitoring/values.yaml     (scrape targets)
#
# ArgoCD cluster-workload secret and repo credentials are managed by
# provision-api-tokens.sh (uses Docker hostnames, not IPs).
#
# Run after: task setup OR task clusters:create
# Usage: bash infra/scripts/update-cross-cluster-ips.sh
set -euo pipefail

SHARED_NETWORK="k3d-loopzero"
DEPLOYMENT="infra/tilt/api-deployment.yaml"
OPERATOR_DEPLOYMENT="infra/tilt/operator-deployment.yaml"

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
MGMT_SERVER=$(get_ip k3d-loopzero-mgmt-server-0)
if [ -z "$MGMT_SERVER" ]; then
  echo "ERROR: Cannot find mgmt server IP on ${SHARED_NETWORK}."
  echo "  Are all clusters running? Run: k3d cluster list"
  exit 1
fi
echo "  Mgmt server: $MGMT_SERVER (Gitea:30000, API:30001, MinIO:32000)"

# Workload LB IP (HTTP/HTTPS traffic via Traefik)
WORKLOAD_LB=$(get_ip k3d-loopzero-workload-serverlb)
if [ -z "$WORKLOAD_LB" ]; then
  echo "ERROR: Cannot find workload LB IP on ${SHARED_NETWORK}."
  exit 1
fi
echo "  Workload LB: $WORKLOAD_LB (Traefik:80/443)"

# Workload server IP (K8s API: 6443)
WORKLOAD_SERVER=$(get_ip k3d-loopzero-workload-server-0)
echo "  Workload server: $WORKLOAD_SERVER (K8s API:6443)"

# Build LB IP (monitoring scrape targets)
BUILD_LB=$(get_ip k3d-loopzero-build-serverlb)
echo "  Build LB: $BUILD_LB (Argo metrics:30090)"

# ---------------------------------------------------------------------------
# Patch API deployment
# ---------------------------------------------------------------------------
echo ""
echo "=== Patching $DEPLOYMENT ==="

patch_env() {
  local file="$1" env_name="$2" new_value="$3"
  OLD=$(grep "${env_name}" -A1 "$file" | grep 'value:' | head -1 | sed 's/.*value: "\([^"]*\)".*/\1/')
  if [ -n "$OLD" ] && [ "$OLD" != "$new_value" ]; then
    sed -i "s|${OLD}|${new_value}|" "$file"
    echo "  ${env_name}: $OLD → $new_value"
  else
    echo "  ${env_name}: already correct ($new_value)"
  fi
}

patch_env "$DEPLOYMENT" "MINIO_CROSS_CLUSTER_ENDPOINT" "http://${MGMT_SERVER}:32000"
patch_env "$DEPLOYMENT" "GITEA_CROSS_CLUSTER_URL" "http://${MGMT_SERVER}:30000"
patch_env "$DEPLOYMENT" "API_CROSS_CLUSTER_URL" "http://${MGMT_SERVER}:30001"
patch_env "$DEPLOYMENT" "WORKLOAD_INGRESS_URL" "http://${WORKLOAD_LB}:80"

# ---------------------------------------------------------------------------
# Patch operator deployment
# ---------------------------------------------------------------------------
if [ -f "$OPERATOR_DEPLOYMENT" ]; then
  echo ""
  echo "=== Patching $OPERATOR_DEPLOYMENT ==="
  patch_env "$OPERATOR_DEPLOYMENT" "WORKLOAD_CLUSTER_URL" "https://${WORKLOAD_SERVER}:6443"
fi

# ---------------------------------------------------------------------------
# Build cluster MinIO credentials (cross-cluster endpoint)
# ---------------------------------------------------------------------------
echo ""
echo "=== Patching build cluster secrets ==="

BUILD_KUBECONFIG=$(k3d kubeconfig write loopzero-build 2>/dev/null || true)
if [ -n "$BUILD_KUBECONFIG" ]; then
  # loopzero-minio-credentials — MINIO_ENDPOINT used by CWTs
  CURRENT_MINIO=$(kubectl --kubeconfig="$BUILD_KUBECONFIG" get secret loopzero-minio-credentials -n argo \
    -o jsonpath='{.data.MINIO_ENDPOINT}' 2>/dev/null | base64 -d 2>/dev/null || true)
  NEW_MINIO="http://${MGMT_SERVER}:32000"
  if [ -n "$CURRENT_MINIO" ] && [ "$CURRENT_MINIO" != "$NEW_MINIO" ]; then
    ENCODED=$(echo -n "$NEW_MINIO" | base64 -w0)
    kubectl --kubeconfig="$BUILD_KUBECONFIG" patch secret loopzero-minio-credentials -n argo \
      -p "{\"data\":{\"MINIO_ENDPOINT\":\"${ENCODED}\"}}" 2>&1
    echo "  loopzero-minio-credentials MINIO_ENDPOINT: $CURRENT_MINIO → $NEW_MINIO"
  else
    echo "  loopzero-minio-credentials MINIO_ENDPOINT: already correct ($NEW_MINIO)"
  fi
else
  echo "WARNING: Cannot get kubeconfig for loopzero-build — skipping"
fi

# ---------------------------------------------------------------------------
# Monitoring values — cross-cluster scrape targets
# ---------------------------------------------------------------------------
MONITORING_VALUES="infra/helm/monitoring/values.yaml"
if [ -f "$MONITORING_VALUES" ]; then
  echo ""
  echo "=== Patching $MONITORING_VALUES ==="

  if [ -n "$BUILD_LB" ]; then
    OLD_ARGO=$(grep -o '"[0-9.]*:30090"' "$MONITORING_VALUES" | tr -d '"' || true)
    NEW_ARGO="${BUILD_LB}:30090"
    if [ -n "$OLD_ARGO" ] && [ "$OLD_ARGO" != "$NEW_ARGO" ]; then
      sed -i "s|\"${OLD_ARGO}\"|\"${NEW_ARGO}\"|g" "$MONITORING_VALUES"
      echo "  Argo Workflows target: $OLD_ARGO → $NEW_ARGO"
    else
      echo "  Argo Workflows target: already correct ($NEW_ARGO)"
    fi
  fi

  OLD_TRAEFIK=$(grep -o '"[0-9.]*:30091"' "$MONITORING_VALUES" | tr -d '"' || true)
  NEW_TRAEFIK="${WORKLOAD_LB}:30091"
  if [ -n "$OLD_TRAEFIK" ] && [ "$OLD_TRAEFIK" != "$NEW_TRAEFIK" ]; then
    sed -i "s|\"${OLD_TRAEFIK}\"|\"${NEW_TRAEFIK}\"|g" "$MONITORING_VALUES"
    echo "  Traefik target: $OLD_TRAEFIK → $NEW_TRAEFIK"
  else
    echo "  Traefik target: already correct ($NEW_TRAEFIK)"
  fi
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
