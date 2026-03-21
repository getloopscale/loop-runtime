#!/usr/bin/env bash
# update-cross-cluster-ips.sh — Updates cross-cluster IPs across all deployment manifests.
#
# k3d assigns dynamic Docker IPs to cluster loadbalancers. These IPs change
# every time clusters are recreated. This script detects the current IPs and
# patches all deployment YAMLs and ArgoCD cluster secrets automatically.
#
# Components patched:
#   - infra/tilt/api-deployment.yaml       (GITEA/API cross-cluster URLs, workload ingress)
#   - infra/tilt/operator-deployment.yaml   (WORKLOAD_CLUSTER_URL for ArgoCD Applications)
#   - ArgoCD cluster-workload secret        (workload cluster API server endpoint)
#   - infra/helm/monitoring/values.yaml     (scrape targets)
#
# Run after: task setup OR task clusters:create
# Usage: bash infra/scripts/update-cross-cluster-ips.sh
set -euo pipefail

DEPLOYMENT="infra/tilt/api-deployment.yaml"
OPERATOR_DEPLOYMENT="infra/tilt/operator-deployment.yaml"

if [ ! -f "$DEPLOYMENT" ]; then
  echo "ERROR: $DEPLOYMENT not found. Run from repo root."
  exit 1
fi

echo "=== Detecting cross-cluster IPs ==="

# Mgmt LB IP on the build network (for build cluster → mgmt Gitea/API callback)
MGMT_ON_BUILD=$(docker inspect k3d-loopzero-mgmt-serverlb \
  --format '{{(index .NetworkSettings.Networks "k3d-loopzero-build").IPAddress}}' 2>/dev/null)

if [ -z "$MGMT_ON_BUILD" ]; then
  echo "ERROR: Cannot find mgmt LB IP on k3d-loopzero-build network."
  echo "  Are all clusters running? Run: k3d cluster list"
  exit 1
fi
echo "  Mgmt LB on build network: $MGMT_ON_BUILD"

# Workload LB IP on the workload network (for API → workload ingress health probes)
WORKLOAD_LB=$(docker inspect k3d-loopzero-workload-serverlb \
  --format '{{(index .NetworkSettings.Networks "k3d-loopzero-workload").IPAddress}}' 2>/dev/null)

if [ -z "$WORKLOAD_LB" ]; then
  echo "ERROR: Cannot find workload LB IP on k3d-loopzero-workload network."
  exit 1
fi
echo "  Workload LB on workload network: $WORKLOAD_LB"

# Workload K8s API server — the LB IP that serves port 6443.
# This is the endpoint ArgoCD and the operator use to deploy to the workload cluster.
WORKLOAD_K8S_API="${WORKLOAD_LB}:6443"
echo "  Workload K8s API server: https://$WORKLOAD_K8S_API"

echo ""
echo "=== Patching $DEPLOYMENT ==="

# Patch GITEA_CROSS_CLUSTER_URL and API_CROSS_CLUSTER_URL
# These use the mgmt LB IP visible from the build cluster
sed -i "s|value: \"http://[0-9.]*:30000\"  # GITEA_CROSS_CLUSTER|value: \"http://${MGMT_ON_BUILD}:30000\"  # GITEA_CROSS_CLUSTER|" "$DEPLOYMENT" 2>/dev/null || true

# Generic sed for the cross-cluster URLs (match the line by context)
# GITEA_CROSS_CLUSTER_URL
OLD_GITEA=$(grep 'GITEA_CROSS_CLUSTER_URL' -A1 "$DEPLOYMENT" | grep 'value:' | head -1 | sed 's/.*value: "\([^"]*\)".*/\1/')
NEW_GITEA="http://${MGMT_ON_BUILD}:30000"
if [ -n "$OLD_GITEA" ] && [ "$OLD_GITEA" != "$NEW_GITEA" ]; then
  sed -i "s|${OLD_GITEA}|${NEW_GITEA}|g" "$DEPLOYMENT"
  echo "  GITEA_CROSS_CLUSTER_URL: $OLD_GITEA → $NEW_GITEA"
else
  echo "  GITEA_CROSS_CLUSTER_URL: already correct ($NEW_GITEA)"
fi

# API_CROSS_CLUSTER_URL
OLD_API=$(grep 'API_CROSS_CLUSTER_URL' -A1 "$DEPLOYMENT" | grep 'value:' | head -1 | sed 's/.*value: "\([^"]*\)".*/\1/')
NEW_API="http://${MGMT_ON_BUILD}:30001"
if [ -n "$OLD_API" ] && [ "$OLD_API" != "$NEW_API" ]; then
  sed -i "s|${OLD_API}|${NEW_API}|g" "$DEPLOYMENT"
  echo "  API_CROSS_CLUSTER_URL: $OLD_API → $NEW_API"
else
  echo "  API_CROSS_CLUSTER_URL: already correct ($NEW_API)"
fi

# WORKLOAD_INGRESS_URL
OLD_WORKLOAD=$(grep 'WORKLOAD_INGRESS_URL' -A1 "$DEPLOYMENT" | grep 'value:' | head -1 | sed 's/.*value: "\([^"]*\)".*/\1/')
NEW_WORKLOAD="http://${WORKLOAD_LB}:80"
if [ -n "$OLD_WORKLOAD" ] && [ "$OLD_WORKLOAD" != "$NEW_WORKLOAD" ]; then
  sed -i "s|${OLD_WORKLOAD}|${NEW_WORKLOAD}|g" "$DEPLOYMENT"
  echo "  WORKLOAD_INGRESS_URL: $OLD_WORKLOAD → $NEW_WORKLOAD"
else
  echo "  WORKLOAD_INGRESS_URL: already correct ($NEW_WORKLOAD)"
fi

# ---------------------------------------------------------------------------
# Operator deployment — WORKLOAD_CLUSTER_URL
# ---------------------------------------------------------------------------
if [ -f "$OPERATOR_DEPLOYMENT" ]; then
  echo ""
  echo "=== Patching $OPERATOR_DEPLOYMENT ==="

  OLD_WORKLOAD_K8S=$(grep 'WORKLOAD_CLUSTER_URL' -A1 "$OPERATOR_DEPLOYMENT" | grep 'value:' | head -1 | sed 's/.*value: "\([^"]*\)".*/\1/')
  NEW_WORKLOAD_K8S="https://${WORKLOAD_K8S_API}"
  if [ -n "$OLD_WORKLOAD_K8S" ] && [ "$OLD_WORKLOAD_K8S" != "$NEW_WORKLOAD_K8S" ]; then
    sed -i "s|${OLD_WORKLOAD_K8S}|${NEW_WORKLOAD_K8S}|" "$OPERATOR_DEPLOYMENT"
    echo "  WORKLOAD_CLUSTER_URL: $OLD_WORKLOAD_K8S → $NEW_WORKLOAD_K8S"
  else
    echo "  WORKLOAD_CLUSTER_URL: already correct ($NEW_WORKLOAD_K8S)"
  fi
else
  echo "WARNING: $OPERATOR_DEPLOYMENT not found — skipping operator patch"
fi

# ---------------------------------------------------------------------------
# ArgoCD cluster secret — workload cluster endpoint
# ---------------------------------------------------------------------------
echo ""
echo "=== Patching ArgoCD cluster-workload secret ==="

# The cluster-workload secret tells ArgoCD where the workload K8s API server lives.
# It must match WORKLOAD_CLUSTER_URL so that ArgoCD Applications can sync.
# For local k3d clusters, we use insecure TLS because the API server certificate
# contains SANs for the original Docker IPs, which change on cluster recreate.
ARGOCD_KUBECONFIG=$(k3d kubeconfig write loopzero-mgmt 2>/dev/null)
if [ -n "$ARGOCD_KUBECONFIG" ]; then
  CURRENT_SERVER=$(kubectl --kubeconfig="$ARGOCD_KUBECONFIG" get secret cluster-workload -n argocd \
    -o jsonpath='{.data.server}' 2>/dev/null | base64 -d 2>/dev/null)
  NEW_SERVER="https://${WORKLOAD_K8S_API}"

  if [ "$CURRENT_SERVER" != "$NEW_SERVER" ]; then
    # Update server URL
    ENCODED_SERVER=$(echo -n "$NEW_SERVER" | base64 -w0)
    kubectl --kubeconfig="$ARGOCD_KUBECONFIG" patch secret cluster-workload -n argocd \
      --type='json' -p="[{\"op\":\"replace\",\"path\":\"/data/server\",\"value\":\"${ENCODED_SERVER}\"}]" 2>&1
    echo "  ArgoCD cluster-workload server: $CURRENT_SERVER → $NEW_SERVER"
  else
    echo "  ArgoCD cluster-workload server: already correct ($NEW_SERVER)"
  fi

  # Always ensure TLS config uses insecure mode for local k3d clusters.
  # k3d API server certs have SANs for specific Docker IPs that change on recreate.
  BEARER_TOKEN=$(kubectl --kubeconfig="$ARGOCD_KUBECONFIG" get secret cluster-workload -n argocd \
    -o jsonpath='{.data.config}' 2>/dev/null | base64 -d 2>/dev/null \
    | python3 -c "import sys,json; print(json.load(sys.stdin).get('bearerToken',''))" 2>/dev/null)
  if [ -n "$BEARER_TOKEN" ]; then
    NEW_CONFIG=$(printf '{"bearerToken":"%s","tlsClientConfig":{"insecure":true}}' "$BEARER_TOKEN")
    ENCODED_CONFIG=$(echo -n "$NEW_CONFIG" | base64 -w0)
    kubectl --kubeconfig="$ARGOCD_KUBECONFIG" patch secret cluster-workload -n argocd \
      --type='json' -p="[{\"op\":\"replace\",\"path\":\"/data/config\",\"value\":\"${ENCODED_CONFIG}\"}]" 2>&1
    echo "  ArgoCD cluster-workload TLS: insecure=true (k3d local dev)"
  fi

  # Update all ArgoCD Applications that reference the old server URL
  APPS_WITH_OLD_SERVER=$(kubectl --kubeconfig="$ARGOCD_KUBECONFIG" get applications -n argocd \
    -o jsonpath='{range .items[*]}{.metadata.name}={.spec.destination.server}{"\n"}{end}' 2>/dev/null \
    | grep -v "$NEW_SERVER" | grep -v "^=" | cut -d= -f1)
  for app_name in $APPS_WITH_OLD_SERVER; do
    if [ -n "$app_name" ]; then
      kubectl --kubeconfig="$ARGOCD_KUBECONFIG" patch application "$app_name" -n argocd \
        --type='json' -p="[{\"op\":\"replace\",\"path\":\"/spec/destination/server\",\"value\":\"${NEW_SERVER}\"}]" 2>&1
      echo "  ArgoCD Application $app_name: destination updated to $NEW_SERVER"
    fi
  done
else
  echo "WARNING: Cannot get kubeconfig for loopzero-mgmt — skipping ArgoCD secret patch"
fi

# ---------------------------------------------------------------------------
# Monitoring values — cross-cluster scrape targets
# ---------------------------------------------------------------------------
MONITORING_VALUES="infra/helm/monitoring/values.yaml"
if [ -f "$MONITORING_VALUES" ]; then
  echo ""
  echo "=== Patching $MONITORING_VALUES ==="

  # Build cluster LB IP on build network (for Argo Workflows metrics)
  BUILD_LB=$(docker inspect k3d-loopzero-build-serverlb \
    --format '{{(index .NetworkSettings.Networks "k3d-loopzero-build").IPAddress}}' 2>/dev/null)
  if [ -n "$BUILD_LB" ]; then
    # Replace the Argo Workflows target IP (format: "IP:30090")
    OLD_ARGO=$(grep -o '"[0-9.]*:30090"' "$MONITORING_VALUES" | tr -d '"')
    NEW_ARGO="${BUILD_LB}:30090"
    if [ -n "$OLD_ARGO" ] && [ "$OLD_ARGO" != "$NEW_ARGO" ]; then
      sed -i "s|\"${OLD_ARGO}\"|\"${NEW_ARGO}\"|g" "$MONITORING_VALUES"
      echo "  Argo Workflows target: $OLD_ARGO → $NEW_ARGO"
    else
      echo "  Argo Workflows target: already correct ($NEW_ARGO)"
    fi
  fi

  # Workload LB IP for Traefik metrics
  OLD_TRAEFIK=$(grep -o '"[0-9.]*:30091"' "$MONITORING_VALUES" | tr -d '"')
  NEW_TRAEFIK="${WORKLOAD_LB}:30091"
  if [ -n "$OLD_TRAEFIK" ] && [ "$OLD_TRAEFIK" != "$NEW_TRAEFIK" ]; then
    sed -i "s|\"${OLD_TRAEFIK}\"|\"${NEW_TRAEFIK}\"|g" "$MONITORING_VALUES"
    echo "  Traefik target: $OLD_TRAEFIK → $NEW_TRAEFIK"
  else
    echo "  Traefik target: already correct ($NEW_TRAEFIK)"
  fi
fi

echo ""
echo "=== Done ==="
echo "Cross-cluster IPs updated. Run 'tilt up' to apply."
echo "If monitoring is already running, apply with:"
echo "  helm upgrade monitoring prometheus-community/kube-prometheus-stack --kube-context k3d-loopzero-mgmt -n monitoring -f infra/helm/monitoring/values.yaml --reuse-values"
