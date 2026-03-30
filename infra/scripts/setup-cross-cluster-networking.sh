#!/usr/bin/env bash
# setup-cross-cluster-networking.sh
#
# Ensures all k3d cluster nodes can reach each other across Docker networks.
#
# k3d cluster configs use `network: k3d-loopzero` (shared network), so all clusters
# are already on the same Docker network. This script verifies connectivity
# and configures CoreDNS for internal hostname resolution.
#
# This script is idempotent — safe to run multiple times.
set -euo pipefail

MGMT_CONTEXT="${MGMT_CONTEXT:-k3d-loopzero-mgmt}"
SHARED_NETWORK="k3d-loopzero"

echo "=== Cross-cluster networking setup ==="

# --- Step 1: Verify all clusters are on the shared network ---
echo ""
echo "Verifying cluster connectivity on ${SHARED_NETWORK}..."
for container in k3d-loopzero-mgmt-server-0 k3d-loopzero-build-server-0 k3d-loopzero-workload-server-0; do
  IP=$(docker inspect "$container" --format "{{(index .NetworkSettings.Networks \"${SHARED_NETWORK}\").IPAddress}}" 2>/dev/null || true)
  if [ -n "$IP" ]; then
    echo "  [OK] $container: $IP"
  else
    echo "  [WARN] $container not on ${SHARED_NETWORK} — attempting connect"
    docker network connect "$SHARED_NETWORK" "$container" 2>/dev/null || echo "  [FAIL] Could not connect $container"
  fi
done

# --- Step 2: Get workload cluster LB IP ---
echo ""
echo "Resolving workload cluster LB IP..."
WORKLOAD_LB_IP=$(docker inspect k3d-loopzero-workload-serverlb \
    --format "{{(index .NetworkSettings.Networks \"${SHARED_NETWORK}\").IPAddress}}" 2>/dev/null || true)

if [ -z "$WORKLOAD_LB_IP" ]; then
    echo "WARNING: Could not determine workload LB IP."
    echo "  The workload cluster may not be running."
    exit 0
fi
echo "  Workload LB IP: $WORKLOAD_LB_IP"

# --- Step 3: Configure CoreDNS in mgmt cluster ---
echo ""
echo "Configuring CoreDNS in mgmt cluster..."

NEEDS_UPDATE=$(kubectl --context "$MGMT_CONTEXT" get configmap coredns -n kube-system \
    -o jsonpath='{.data.NodeHosts}' 2>/dev/null | grep -c "host.k3d.internal" || true)

if [ "$NEEDS_UPDATE" -gt 0 ]; then
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
