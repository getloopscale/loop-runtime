#!/usr/bin/env bash
# Capacity validation runner for cell-budget.md assumptions.
#
# Validates the 5 "educated guess" assumptions from docs/architecture/cell-budget.md:
#   1. Go memory under load (projected 200-300 Mi)
#   2. BuildKit concurrent solves per node (projected ~4/node)
#   3. ArgoCD memory per Application (projected ~500 kB/app)
#   4. Gitea push latency vs repo count (projected p95 < 2s at 100 repos)
#   5. Pod memory by framework (projected ~50 Mi average)
#
# Prerequisites:
#   - LOOPZERO_CAPACITY=1 (set automatically by this script)
#   - Environment variables for the target DO environment (see each test)
#   - Port-forwards for metrics endpoints (tests 1, 2, 3)
#
# Results written to /tmp/capacity-bench/*.json
#
# Usage:
#   ./run-capacity.sh [test-name|all]
#
# Tests: go-memory, buildkit, argocd-memory, gitea-push, pod-memory, all

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export LOOPZERO_CAPACITY=1

usage() {
    echo "Usage: $0 [go-memory|buildkit|argocd-memory|gitea-push|pod-memory|all]"
    echo ""
    echo "Environment variables (all tests):"
    echo "  LOOPZERO_API_URL           LoopZero API endpoint"
    echo "  LOOPZERO_SERVICE_AUTH      Service auth bypass token"
    echo "  LOOPZERO_APP_DOMAIN        Base domain for deployed apps"
    echo ""
    echo "Additional env vars per test:"
    echo "  go-memory:      LOOPZERO_API_METRICS_URL (port 9090)"
    echo "  buildkit:       LOOPZERO_API_METRICS_URL, LOOPZERO_BUILD_NODES"
    echo "  argocd-memory:  LOOPZERO_ARGOCD_METRICS_URL, LOOPZERO_MGMT_CONTEXT"
    echo "  gitea-push:     LOOPZERO_GITEA_URL, LOOPZERO_GITEA_USER, LOOPZERO_GITEA_PASS"
    echo "  pod-memory:     LOOPZERO_RUNTIME_CONTEXT"
    exit 1
}

case "${1:-all}" in
    go-memory)
        echo ">>> Running: Go Memory Under Load"
        go test -v -count=1 -timeout 30m -run TestCapacityGoMemory "$SCRIPT_DIR"
        ;;
    buildkit)
        echo ">>> Running: BuildKit Concurrency"
        go test -v -count=1 -timeout 60m -run TestCapacityBuildKitConcurrency "$SCRIPT_DIR"
        ;;
    argocd-memory)
        echo ">>> Running: ArgoCD Memory per App"
        go test -v -count=1 -timeout 15m -run TestCapacityArgoCDMemoryPerApp "$SCRIPT_DIR"
        ;;
    gitea-push)
        echo ">>> Running: Gitea Push Latency"
        go test -v -count=1 -timeout 30m -run TestCapacityGiteaPushLatency "$SCRIPT_DIR"
        ;;
    pod-memory)
        echo ">>> Running: Pod Memory by Framework"
        go test -v -count=1 -timeout 120m -run TestCapacityPodMemoryByFramework "$SCRIPT_DIR"
        ;;
    all)
        echo ">>> Running: ALL capacity validation tests"
        echo ">>> Estimated time: 2-4 hours"
        echo ""
        go test -v -count=1 -timeout 4h \
            -run 'TestCapacity(GoMemory|BuildKitConcurrency|ArgoCDMemoryPerApp|GiteaPushLatency|PodMemoryByFramework)' \
            "$SCRIPT_DIR"
        ;;
    -h|--help|help)
        usage
        ;;
    *)
        echo "Unknown test: $1"
        usage
        ;;
esac

echo ""
echo ">>> Results saved to /tmp/capacity-bench/"
ls -la /tmp/capacity-bench/*.json 2>/dev/null || echo "(no results found)"
