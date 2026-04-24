#!/usr/bin/env bash
# Health Check — validates platform is fully operational.
#
# Checks all 30+ deployments, critical services (Gitea, ArgoCD, Grafana, API),
# CF Tunnel, cluster nodes, and external endpoints.
#
# Required env vars: KUBECONFIG (mgmt cluster)
# Optional: BUILD_KUBECONFIG, RUNTIME_KUBECONFIG, BASE_DOMAIN
#
# Exit codes: 0 = healthy, 1 = degraded (some checks failed)
#
# Usage:
#   KUBECONFIG=/tmp/mgmt-kc.yaml bash infra/scripts/health-check.sh
#   KUBECONFIG=/tmp/mgmt-kc.yaml BUILD_KUBECONFIG=/tmp/build-kc.yaml bash infra/scripts/health-check.sh

set -uo pipefail

NAMESPACE="${NAMESPACE:-loopzero-system}"
BASE_DOMAIN="${BASE_DOMAIN:-dev.loopzero.example}"
FAILURES=0
CHECKS=0

pass() { CHECKS=$((CHECKS + 1)); echo "  ✓ $1"; }
fail() { CHECKS=$((CHECKS + 1)); FAILURES=$((FAILURES + 1)); echo "  ✗ $1" >&2; }

# ── 1. Deployments ──────────────────────────────────────
check_deployments() {
  echo "=== Deployments ==="
  NOT_READY=$(kubectl get deploy -n "$NAMESPACE" --no-headers 2>/dev/null | \
    awk '$2 != "0/0" { split($2, a, "/"); if (a[1] != a[2]) print $1 }')

  if [ -z "$NOT_READY" ]; then
    TOTAL=$(kubectl get deploy -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
    pass "All $TOTAL deployments ready"
  else
    for d in $NOT_READY; do
      READY=$(kubectl get deploy "$d" -n "$NAMESPACE" --no-headers 2>/dev/null | awk '{print $2}')
      fail "Deployment $d: $READY"
    done
  fi
}

# ── 2. Critical Services ────────────────────────────────
check_gitea() {
  echo "=== Gitea ==="
  STATUS=$(kubectl exec deploy/gitea -n "$NAMESPACE" -c gitea -- \
    wget -qO- http://localhost:3000/api/healthz 2>/dev/null | \
    python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  if [ "$STATUS" = "pass" ]; then
    pass "Gitea: healthy (cache + database)"
  else
    fail "Gitea: status=$STATUS"
  fi
}

check_argocd() {
  echo "=== ArgoCD ==="
  RUNNING=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=argocd-server \
    --no-headers 2>/dev/null | grep -c Running)
  if [ "$RUNNING" -ge 1 ]; then
    pass "ArgoCD server: $RUNNING pod(s) Running"
  else
    fail "ArgoCD server: not running"
  fi
}

check_grafana() {
  echo "=== Grafana ==="
  RUNNING=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=grafana \
    --no-headers 2>/dev/null | grep -c Running)
  if [ "$RUNNING" -ge 1 ]; then
    pass "Grafana: $RUNNING pod(s) Running"
  else
    fail "Grafana: not running"
  fi
}

check_api() {
  echo "=== LoopZero API ==="
  STATUS=$(kubectl exec deploy/loopzero-api -n "$NAMESPACE" -c api -- \
    wget -qO- http://localhost:8080/api/v1/health 2>/dev/null | \
    python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  if [ "$STATUS" = "ok" ]; then
    pass "API: healthy"
  else
    fail "API: status=$STATUS (may need images built)"
  fi
}

# ── 3. CF Tunnel ────────────────────────────────────────
check_tunnel() {
  echo "=== CF Tunnel ==="
  # Tunnel may be on runtime cluster
  for KC in "${RUNTIME_KUBECONFIG:-}" "$KUBECONFIG"; do
    if [ -n "$KC" ] && [ -f "$KC" ]; then
      READY=$(KUBECONFIG="$KC" kubectl get deploy cloudflared -n "$NAMESPACE" \
        --no-headers 2>/dev/null | awk '{print $2}')
      if [ -n "$READY" ]; then
        WANT=$(echo "$READY" | cut -d/ -f2)
        HAVE=$(echo "$READY" | cut -d/ -f1)
        if [ "$HAVE" = "$WANT" ] && [ "$WANT" != "0" ]; then
          pass "CF Tunnel: $READY"
        else
          fail "CF Tunnel: $READY"
        fi
        return
      fi
    fi
  done
  fail "CF Tunnel: deployment not found"
}

# ── 4. Cluster Nodes ───────────────────────────────────
check_cluster_nodes() {
  local label="$1"
  local kubeconfig="$2"

  echo "=== $label Cluster Nodes ==="
  if [ -z "$kubeconfig" ] || [ ! -f "$kubeconfig" ]; then
    fail "$label: kubeconfig not provided"
    return
  fi

  NOT_READY=$(KUBECONFIG="$kubeconfig" kubectl get nodes --no-headers 2>/dev/null | \
    grep -v Ready | wc -l)
  TOTAL=$(KUBECONFIG="$kubeconfig" kubectl get nodes --no-headers 2>/dev/null | wc -l)

  if [ "$NOT_READY" -eq 0 ] && [ "$TOTAL" -gt 0 ]; then
    pass "$label: $TOTAL node(s) Ready"
  else
    fail "$label: $NOT_READY/$TOTAL node(s) not Ready"
  fi
}

# ── 5. External Endpoints ──────────────────────────────
check_external() {
  echo "=== External Endpoints ==="
  for subdomain in api cloud grafana; do
    URL="https://${subdomain}.${BASE_DOMAIN}"
    HTTP_CODE=$(curl -sko /dev/null -w "%{http_code}" --connect-timeout 5 "$URL" 2>/dev/null)
    case "$HTTP_CODE" in
      200|301|302) pass "$URL → HTTP $HTTP_CODE" ;;
      000)         fail "$URL → connection refused/timeout" ;;
      *)           fail "$URL → HTTP $HTTP_CODE" ;;
    esac
  done
}

# ── 6. Platform Pods (custom images) ───────────────────
check_platform_pods() {
  echo "=== Platform Pods ==="
  for app in loopzero-api loopzero-controller tenant-controller loopzero-cloud loopzero-dashboard; do
    STATUS=$(kubectl get pods -n "$NAMESPACE" -l app="$app" --no-headers 2>/dev/null | \
      head -1 | awk '{print $3}')
    READY=$(kubectl get deploy "$app" -n "$NAMESPACE" --no-headers 2>/dev/null | awk '{print $2}')
    if [ "$STATUS" = "Running" ]; then
      pass "$app: $READY Running"
    elif [ -z "$STATUS" ]; then
      fail "$app: no pods found"
    else
      fail "$app: $READY $STATUS"
    fi
  done
}

# ── Main ────────────────────────────────────────────────
main() {
  echo "╔══════════════════════════════════════════╗"
  echo "║     LoopZero Platform Health Check           ║"
  echo "╚══════════════════════════════════════════╝"
  echo ""

  check_deployments
  echo ""
  check_platform_pods
  echo ""
  check_gitea
  check_argocd
  check_grafana
  check_api
  echo ""
  check_tunnel
  echo ""
  check_cluster_nodes "Build" "${BUILD_KUBECONFIG:-}"
  check_cluster_nodes "Runtime" "${RUNTIME_KUBECONFIG:-}"
  echo ""
  check_external

  echo ""
  echo "════════════════════════════════════════════"
  if [ "$FAILURES" -eq 0 ]; then
    echo "Result: HEALTHY ($CHECKS/$CHECKS checks passed)"
    exit 0
  else
    echo "Result: DEGRADED ($((CHECKS - FAILURES))/$CHECKS checks passed, $FAILURES failed)"
    exit 1
  fi
}

main "$@"
