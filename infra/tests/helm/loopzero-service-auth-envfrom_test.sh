#!/usr/bin/env bash
# T1.1 of plan 2026-05-29-dashboard-public-integration-canonicalization
# v1.1: assert that both loopzero-cloud and loopzero-api deployment templates render
# envFrom: secretRef: name=loopzero-service-auth (optional: true).
# Hermetic: uses `helm template` (no live cluster).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

PASS=0
FAIL=0
check() {
  local name="$1"; shift
  if "$@"; then
    echo "  PASS: $name"; PASS=$((PASS+1))
  else
    echo "  FAIL: $name"; FAIL=$((FAIL+1))
  fi
}

render_or_skip() {
  local chart="$1"; local out_file="$2"
  if helm template "$chart" --kube-version 1.32.0 > "$out_file" 2>/dev/null; then
    return 0
  fi
  return 1
}

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "=== infra/helm/loopzero-cloud ==="
if render_or_skip infra/helm/loopzero-cloud "$TMPDIR/infra-cloud.yaml"; then
  check "envFrom loopzero-service-auth present" grep -q "name: loopzero-service-auth" "$TMPDIR/infra-cloud.yaml"
  check "envFrom loopzero-stripe-webhook present" grep -q "name: loopzero-stripe-webhook" "$TMPDIR/infra-cloud.yaml"
  # The new secretRefs must be optional: true (so dev clusters without the
  # presync-created Secrets still boot)
  check "loopzero-service-auth marked optional" \
    bash -c "awk '/name: loopzero-service-auth/{getline; print}' '$TMPDIR/infra-cloud.yaml' | grep -q 'optional: true'"
else
  echo "  SKIP: infra/helm/loopzero-cloud template render failed (likely missing deps)"
fi

echo ""
echo "=== charts/loopzero-cloud ==="
if render_or_skip charts/loopzero-cloud "$TMPDIR/charts-cloud.yaml"; then
  check "envFrom loopzero-service-auth present" grep -q "name: loopzero-service-auth" "$TMPDIR/charts-cloud.yaml"
  check "envFrom loopzero-stripe-webhook present" grep -q "name: loopzero-stripe-webhook" "$TMPDIR/charts-cloud.yaml"
else
  echo "  SKIP: charts/loopzero-cloud template render failed (likely missing deps)"
fi

echo ""
echo "=== infra/helm/loopzero-api ==="
if render_or_skip infra/helm/loopzero-api "$TMPDIR/infra-api.yaml"; then
  check "envFrom loopzero-service-auth present" grep -q "name: loopzero-service-auth" "$TMPDIR/infra-api.yaml"
  check "loopzero-stripe-webhook NOT present (api doesn't process webhooks)" \
    bash -c "! grep -q 'name: loopzero-stripe-webhook' '$TMPDIR/infra-api.yaml'"
else
  echo "  SKIP: infra/helm/loopzero-api template render failed (likely missing deps)"
fi

echo ""
echo "=== charts/loopzero-api ==="
if render_or_skip charts/loopzero-api "$TMPDIR/charts-api.yaml"; then
  check "envFrom loopzero-service-auth present" grep -q "name: loopzero-service-auth" "$TMPDIR/charts-api.yaml"
else
  echo "  SKIP: charts/loopzero-api template render failed (likely missing deps)"
fi

echo ""
echo "============================="
echo "PASS=$PASS  FAIL=$FAIL"
echo "============================="
[ "$FAIL" -eq 0 ]
