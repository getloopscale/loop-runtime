#!/bin/bash
# Unified load test runner for all levels.
#
# Usage:
#   LOOPZERO_SERVICE_AUTH=<token> ./run-tests.sh l1
#   LOOPZERO_SERVICE_AUTH=<token> ./run-tests.sh l2
#   LOOPZERO_SERVICE_AUTH=<token> ./run-tests.sh l3
#   LOOPZERO_SERVICE_AUTH=<token> ./run-tests.sh l4
#   LOOPZERO_SERVICE_AUTH=<token> LOOPZERO_MULTI_CELL=1 ./run-tests.sh l5
#   LOOPZERO_SERVICE_AUTH=<token> ./run-tests.sh unit   # unit tests only
#
# Environment variables:
#   LOOPZERO_API_URL          API endpoint (default: https://api.dev.getloopscale.com)
#   LOOPZERO_APP_DOMAIN       App domain (default: dev.getloopscale.com)
#   LOOPZERO_SERVICE_AUTH     Service auth token (required for real tests)
#   LOOPZERO_BOOTSTRAP_SECRET Bootstrap secret (default: loopzero-dev-bootstrap-secret)
#   LOOPZERO_FIXTURE_PATH     Fixture dir (default: ../e2e/fixtures/hello-server)
#   LOOPZERO_MULTI_CELL       Set to 1 for multi-cell tests
#   LOOPZERO_USE_PORT_FORWARD If set, starts kubectl port-forward

set -euo pipefail

LEVEL="${1:-unit}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Defaults
export LOOPZERO_API_URL="${LOOPZERO_API_URL:-https://api.dev.getloopscale.com}"
export LOOPZERO_APP_DOMAIN="${LOOPZERO_APP_DOMAIN:-dev.getloopscale.com}"
export LOOPZERO_REAL_DEPLOY=1

echo "═══════════════════════════════════════════════════════"
echo "  LOOPZERO LOAD TEST — Level: ${LEVEL^^}"
echo "═══════════════════════════════════════════════════════"
echo "  API:     $LOOPZERO_API_URL"
echo "  Domain:  $LOOPZERO_APP_DOMAIN"
echo ""

# Port-forward mode
PF_PID=""
if [[ -n "${LOOPZERO_USE_PORT_FORWARD:-}" ]]; then
  CONTEXT="${LOOPZERO_CORE_CONTEXT:-do-sfo3-loopzero-infra-dev-sfo3-mgmt}"
  echo "  Starting port-forward (context: $CONTEXT)..."
  kubectl --context="$CONTEXT" -n loopzero-system port-forward svc/loopzero-api 18080:8080 &
  PF_PID=$!
  sleep 3
  export LOOPZERO_API_URL="http://localhost:18080"
  echo "  Port-forward active → $LOOPZERO_API_URL"
fi

cleanup() {
  if [[ -n "$PF_PID" ]]; then
    kill "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

cd "$SCRIPT_DIR"

case "$LEVEL" in
  unit)
    echo "Running unit tests (no infra needed)..."
    go test -v -count=1 -run 'TestPrepareFixtureBundle|TestNewTenantIdentity' ./
    ;;
  l1)
    export LOOPZERO_LOAD_LEVEL=l1
    echo "Running L1 — 10 tenants..."
    go test -v -count=1 -timeout 45m \
      -run 'TestRealDeploy_L1|TestL1_' ./
    ;;
  l2)
    export LOOPZERO_LOAD_LEVEL=l2
    echo "Running L2 — 50 tenants..."
    go test -v -count=1 -timeout 3h \
      -run 'TestRealDeploy_L2' ./
    ;;
  l3)
    export LOOPZERO_LOAD_LEVEL=l3
    echo "Running L3 — 100 tenants..."
    go test -v -count=1 -timeout 4h \
      -run 'TestRealDeploy_L3' ./
    ;;
  l4)
    export LOOPZERO_LOAD_LEVEL=l4
    echo "Running L4 — 500 tenants..."
    go test -v -count=1 -timeout 8h \
      -run 'TestRealDeploy_L4' ./
    ;;
  l5)
    export LOOPZERO_LOAD_LEVEL=l5
    export LOOPZERO_MULTI_CELL="${LOOPZERO_MULTI_CELL:-1}"
    echo "Running L5 — multi-cell (1000-5000 tenants)..."
    go test -v -count=1 -timeout 12h \
      -run 'TestL5_' ./
    ;;
  all)
    export LOOPZERO_LOAD_LEVEL=all
    echo "Running ALL levels (L1-L4)..."
    go test -v -count=1 -timeout 24h \
      -run 'TestRealDeploy_L[1234]' ./
    ;;
  *)
    echo "Unknown level: $LEVEL"
    echo "Usage: $0 {unit|l1|l2|l3|l4|l5|all}"
    exit 1
    ;;
esac
