#!/usr/bin/env bash
# T-03 helper: capture capacity baselines using in-cluster port-forwards.
#
# Gitea and some other deps are ClusterIP-only (not exposed externally).
# This script port-forwards them briefly, captures baseline, tears down.
#
# Uso:
#   tools/capture-capacity-baselines.sh                   # all suites
#   tools/capture-capacity-baselines.sh gitea             # only gitea
#   tools/capture-capacity-baselines.sh loopzero-api          # only loopzero-api
set -eu

CTX_MGMT="${CTX_MGMT:-do-sfo3-loopzero-infra-dev-sfo3-mgmt}"
SUITE="${1:-all}"
OUTDIR="/tmp/capacity-bench"
mkdir -p "$OUTDIR"

run_bench_gitea() {
  echo "=== gitea-auth baseline ==="
  local PW
  PW=$(kubectl --context="$CTX_MGMT" -n platform-system get secret gitea-admin-secret -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
  if [ -z "$PW" ]; then
    echo "ERROR: gitea-admin-secret not found — skipping"
    return 1
  fi

  # Port-forward in background
  kubectl --context="$CTX_MGMT" -n platform-system port-forward svc/gitea-http 23000:3000 >/dev/null 2>&1 &
  local PF=$!
  sleep 3

  export LOOPZERO_CAPACITY=1
  export LOOPZERO_GITEA_URL="http://localhost:23000"
  export LOOPZERO_GITEA_USER="loopzero-admin"
  export LOOPZERO_GITEA_PASS="$PW"

  export PATH="/usr/local/go/bin:$PATH"
  ( cd /home/paulo/loopzero && go test ./infra/tests/capacity/ -run TestCapacityGiteaAuth -v \
    -args -rps=50 -duration=30s -workers=20 -out="$OUTDIR/gitea-auth-$(date +%s).json" ) 2>&1 | tail -15

  kill "$PF" 2>/dev/null || true
}

run_bench_loopzero_api() {
  echo "=== loopzero-api-build-submit baseline ==="
  local SA
  SA=$(kubectl --context="$CTX_MGMT" -n platform-system get secret loopzero-api-secrets -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d)

  export LOOPZERO_CAPACITY=1
  export LOOPZERO_API_URL="https://api.dev.loopzero.example"
  export LOOPZERO_SERVICE_AUTH="$SA"

  export PATH="/usr/local/go/bin:$PATH"
  ( cd /home/paulo/loopzero && go test ./infra/tests/capacity/ -run TestCapacityLoopZeroAPI -v \
    -args -rps=30 -duration=30s -workers=10 -out="$OUTDIR/loopzero-api-$(date +%s).json" ) 2>&1 | tail -15
}

case "$SUITE" in
  gitea)    run_bench_gitea ;;
  loopzero-api) run_bench_loopzero_api ;;
  all)
    run_bench_loopzero_api
    echo ""
    run_bench_gitea
    ;;
  *) echo "Usage: $0 [all|gitea|loopzero-api]"; exit 1 ;;
esac

echo ""
echo "=== results ==="
ls -la "$OUTDIR"/*.json 2>/dev/null | tail -5
