#!/usr/bin/env bash
# setup-cross-cluster-networking.sh
#
# Connects k3d cluster Docker containers to each other's Docker networks
# so that pods in one cluster can reach the API server of another cluster.
#
# This is required because k3d creates isolated Docker networks per cluster.
# Without this, ArgoCD in mgmt cannot reach the workload cluster, and
# Prometheus cannot scrape metrics from build/workload clusters.
#
# Also configures CoreDNS in the mgmt cluster to resolve host.k3d.internal
# to the workload cluster's server load balancer IP.
#
# This script is idempotent — safe to run multiple times.

set -euo pipefail

MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"

echo "=== Cross-cluster Docker network setup ==="

# --- Step 1: Connect mgmt cluster nodes to workload and build networks ---
# mgmt nodes need to reach:
#   - workload cluster (ArgoCD syncs apps there)
#   - build cluster (already connected by k3d, but verify)

connect_to_network() {
    local container="$1"
    local network="$2"

    if docker inspect "$container" --format '{{range $net, $_ := .NetworkSettings.Networks}}{{$net}} {{end}}' 2>/dev/null | grep -q "$network"; then
        echo "  [OK] $container already on $network"
    else
        if docker network connect "$network" "$container" 2>/dev/null; then
            echo "  [CONNECTED] $container -> $network"
        else
            echo "  [SKIP] $container -> $network (network or container not found)"
        fi
    fi
}

# Get all mgmt cluster containers
MGMT_CONTAINERS=$(docker ps --filter "name=k3d-loopzero-mgmt" --format '{{.Names}}' 2>/dev/null || true)
if [ -z "$MGMT_CONTAINERS" ]; then
    echo "ERROR: No k3d-loopzero-mgmt containers found. Are clusters running?"
    exit 1
fi

echo ""
echo "Connecting mgmt nodes to workload network..."
for container in $MGMT_CONTAINERS; do
    connect_to_network "$container" "k3d-loopzero-workload"
done

echo ""
echo "Connecting mgmt nodes to build network..."
for container in $MGMT_CONTAINERS; do
    connect_to_network "$container" "k3d-loopzero-build"
done

# --- Step 2: Get workload cluster LB IP on the workload network ---
echo ""
echo "Resolving workload cluster LB IP..."
WORKLOAD_LB_IP=$(docker inspect k3d-loopzero-workload-serverlb \
    --format '{{(index .NetworkSettings.Networks "k3d-loopzero-workload").IPAddress}}' 2>/dev/null || true)

if [ -z "$WORKLOAD_LB_IP" ]; then
    echo "WARNING: Could not determine workload LB IP. Skipping CoreDNS config."
    echo "  The workload cluster may not be running."
    exit 0
fi

echo "  Workload LB IP: $WORKLOAD_LB_IP"

# --- Step 3: Configure CoreDNS in mgmt cluster ---
# Add host.k3d.internal -> workload LB IP mapping to CoreDNS NodeHosts.
# CoreDNS in k3d uses the NodeHosts file for custom entries.
echo ""
echo "Configuring CoreDNS in mgmt cluster..."

NEEDS_UPDATE=$(kubectl --context "$MGMT_CONTEXT" get configmap coredns -n kube-system \
    -o jsonpath='{.data.NodeHosts}' 2>/dev/null | grep -c "host.k3d.internal" || true)

if [ "$NEEDS_UPDATE" -gt 0 ]; then
    # Check if the IP is already correct
    CURRENT_IP=$(kubectl --context "$MGMT_CONTEXT" get configmap coredns -n kube-system \
        -o jsonpath='{.data.NodeHosts}' 2>/dev/null | grep "host.k3d.internal" | awk '{print $1}')
    if [ "$CURRENT_IP" = "$WORKLOAD_LB_IP" ]; then
        echo "  [OK] CoreDNS already has host.k3d.internal -> $WORKLOAD_LB_IP"
    else
        echo "  [UPDATE] Updating host.k3d.internal from $CURRENT_IP to $WORKLOAD_LB_IP"
        kubectl --context "$MGMT_CONTEXT" get configmap coredns -n kube-system -o json | \
            python3 -c "
import sys, json
data = json.load(sys.stdin)
lines = [l for l in data['data']['NodeHosts'].splitlines() if 'host.k3d.internal' not in l]
lines.append('$WORKLOAD_LB_IP host.k3d.internal')
data['data']['NodeHosts'] = '\n'.join(lines) + '\n'
json.dump(data, sys.stdout)
" | kubectl --context "$MGMT_CONTEXT" apply -f - >/dev/null 2>&1
        kubectl --context "$MGMT_CONTEXT" rollout restart deployment coredns -n kube-system >/dev/null 2>&1
        kubectl --context "$MGMT_CONTEXT" rollout status deployment coredns -n kube-system --timeout=60s >/dev/null 2>&1
        echo "  [OK] CoreDNS updated and restarted"
    fi
else
    echo "  [ADD] Adding host.k3d.internal -> $WORKLOAD_LB_IP"
    kubectl --context "$MGMT_CONTEXT" get configmap coredns -n kube-system -o json | \
        python3 -c "
import sys, json
data = json.load(sys.stdin)
data['data']['NodeHosts'] = data['data']['NodeHosts'].rstrip('\n') + '\n$WORKLOAD_LB_IP host.k3d.internal\n'
json.dump(data, sys.stdout)
" | kubectl --context "$MGMT_CONTEXT" apply -f - >/dev/null 2>&1
    kubectl --context "$MGMT_CONTEXT" rollout restart deployment coredns -n kube-system >/dev/null 2>&1
    kubectl --context "$MGMT_CONTEXT" rollout status deployment coredns -n kube-system --timeout=60s >/dev/null 2>&1
    echo "  [OK] CoreDNS updated and restarted"
fi

echo ""
echo "=== Cross-cluster networking setup complete ==="
echo ""
echo "Verify DNS from mgmt cluster pods:"
echo "  kubectl --context $MGMT_CONTEXT run dns-test --rm -i --restart=Never --image=alpine/curl:latest -- nslookup host.k3d.internal"
