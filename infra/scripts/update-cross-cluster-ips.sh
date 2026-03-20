#!/usr/bin/env bash
# update-cross-cluster-ips.sh — Updates cross-cluster IPs in api-deployment.yaml.
#
# k3d assigns dynamic Docker IPs to cluster loadbalancers. These IPs change
# every time clusters are recreated. This script detects the current IPs and
# patches the deployment YAML automatically.
#
# Run after: task setup OR task clusters:create
# Usage: bash infra/scripts/update-cross-cluster-ips.sh
set -euo pipefail

DEPLOYMENT="infra/tilt/api-deployment.yaml"

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
