#!/usr/bin/env bash
# setup-cross-cluster-networking.sh
#
# Ensures all k3d cluster nodes can reach each other across Docker networks +
# materializes the canonical `host.k3d.internal` resolution in mgmt CoreDNS.
#
# All k3d cluster configs use `network: k3d-loopruntime` (shared network), so the
# clusters already see each other on the bridge. This script verifies that and
# pins `host.k3d.internal` to the docker bridge GATEWAY — the canonical k3d
# host-publish target shared by every `port: <host>:<nodePort>@loadbalancer`
# mapping (vault:8200, mgmt observability/tooling, workload ingress, k8s APIs).
#
# Canonical target = docker bridge gateway (NOT the workload-serverlb).
# Per ADR D6 of plan k3d-host-published-vault-reach (sponsor 2026-06-20):
#
#   #112 (decode_error 562) + #113 (Klipper hostPort gap) + #114 (MetalLB L2
#   ARP refuted by Docker bridge isolation) + #115 — all died on host-side name
#   resolution. The probe 2026-06-20 measured `host.k3d.internal → bridge
#   gateway` reaching EVERY platform port (vault 400 / mgmt 200 / dashboard 404
#   / workload 301 / k8s APIs 400) while `host.k3d.internal → workload-serverlb`
#   returned TCP RST in ~0.4ms on EVERY port (workload-serverlb only publishes
#   its own 8090/8443/30091/6444 from the host's perspective; nothing else).
#
#   The earlier J-17 Loki cross-cluster path wrongly pointed host.k3d.internal
#   at the workload-serverlb container IP. That path is superseded; the gateway
#   is the universal k3d host-publish reach for ALL clusters on the shared
#   bridge. This file is therefore the SINGLE place where the canonical
#   resolution is asserted on every `task setup` — hermetic guard at
#   `infra/tests/charts/host_k3d_internal_canonical_test.go` (TestHostK3dInternalCanonical)
#   locks the contract against regression.
#
# This script is idempotent — safe to run multiple times.
set -euo pipefail

CORE_CONTEXT="${CORE_CONTEXT:-k3d-loopruntime-core}"
SHARED_NETWORK="k3d-loopruntime"

echo "=== Cross-cluster networking setup ==="

# --- Step 1: Verify all clusters are on the shared network ---
echo ""
echo "Verifying cluster connectivity on ${SHARED_NETWORK}..."
for container in k3d-loopruntime-core-server-0 k3d-loopruntime-build-server-0 k3d-loopruntime-cell-0-server-0; do
  IP=$(docker inspect "$container" --format "{{(index .NetworkSettings.Networks \"${SHARED_NETWORK}\").IPAddress}}" 2>/dev/null || true)
  if [ -n "$IP" ]; then
    echo "  [OK] $container: $IP"
  else
    echo "  [WARN] $container not on ${SHARED_NETWORK} — attempting connect"
    docker network connect "$SHARED_NETWORK" "$container" 2>/dev/null || echo "  [FAIL] Could not connect $container"
  fi
done

# --- Step 2: Resolve the k3d-loopruntime bridge GATEWAY (canonical host-publish target) ---
echo ""
echo "Resolving k3d-loopruntime bridge gateway (canonical host.k3d.internal target per ADR D6)..."
GATEWAY_IP=$(docker network inspect "${SHARED_NETWORK}" -f '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true)

if [ -z "$GATEWAY_IP" ]; then
    echo "WARNING: Could not determine ${SHARED_NETWORK} bridge gateway IP."
    echo "  Is the docker network ${SHARED_NETWORK} present? (created by k3d cluster create)"
    exit 0
fi
echo "  Bridge gateway: $GATEWAY_IP"

# --- Step 3: Configure CoreDNS in mgmt cluster ---
echo ""
echo "Configuring CoreDNS in mgmt cluster..."

NEEDS_UPDATE=$(kubectl --context "$CORE_CONTEXT" get configmap coredns -n kube-system \
    -o jsonpath='{.data.NodeHosts}' 2>/dev/null | grep -c "host.k3d.internal" || true)

if [ "$NEEDS_UPDATE" -gt 0 ]; then
    CURRENT_IP=$(kubectl --context "$CORE_CONTEXT" get configmap coredns -n kube-system \
        -o jsonpath='{.data.NodeHosts}' 2>/dev/null | grep "host.k3d.internal" | awk '{print $1}')
    if [ "$CURRENT_IP" = "$GATEWAY_IP" ]; then
        echo "  [OK] CoreDNS already has host.k3d.internal -> $GATEWAY_IP"
    else
        echo "  [UPDATE] Updating host.k3d.internal from $CURRENT_IP to $GATEWAY_IP"
        kubectl --context "$CORE_CONTEXT" get configmap coredns -n kube-system -o json | \
            python3 -c "
import sys, json
data = json.load(sys.stdin)
lines = [l for l in data['data']['NodeHosts'].splitlines() if 'host.k3d.internal' not in l]
lines.append('$GATEWAY_IP host.k3d.internal')
data['data']['NodeHosts'] = '\n'.join(lines) + '\n'
json.dump(data, sys.stdout)
" | kubectl --context "$CORE_CONTEXT" apply -f - >/dev/null 2>&1
        kubectl --context "$CORE_CONTEXT" rollout restart deployment coredns -n kube-system >/dev/null 2>&1
        kubectl --context "$CORE_CONTEXT" rollout status deployment coredns -n kube-system --timeout=60s >/dev/null 2>&1
        echo "  [OK] CoreDNS updated and restarted"
    fi
else
    echo "  [ADD] Adding host.k3d.internal -> $GATEWAY_IP"
    kubectl --context "$CORE_CONTEXT" get configmap coredns -n kube-system -o json | \
        python3 -c "
import sys, json
data = json.load(sys.stdin)
data['data']['NodeHosts'] = data['data']['NodeHosts'].rstrip('\n') + '\n$GATEWAY_IP host.k3d.internal\n'
json.dump(data, sys.stdout)
" | kubectl --context "$CORE_CONTEXT" apply -f - >/dev/null 2>&1
    kubectl --context "$CORE_CONTEXT" rollout restart deployment coredns -n kube-system >/dev/null 2>&1
    kubectl --context "$CORE_CONTEXT" rollout status deployment coredns -n kube-system --timeout=60s >/dev/null 2>&1
    echo "  [OK] CoreDNS updated and restarted"
fi

echo ""
echo "=== Cross-cluster networking setup complete ==="
