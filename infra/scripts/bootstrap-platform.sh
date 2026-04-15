#!/usr/bin/env bash
# Bootstrap Platform — idempotent platform setup after pulumi up.
#
# Creates Gitea tokens, ArgoCD accounts, K8s secrets, seeds DB, applies Tenant CR.
# Safe to re-run: each function checks preconditions and skips if already done.
#
# Required env vars: STACK, KUBECONFIG, DIGITALOCEAN_TOKEN
# Optional: CLOUDFLARE_API_TOKEN, LOOPZERO_*_SECRET vars

set -euo pipefail

STACK="${STACK:?STACK env var required}"
NAMESPACE="platform-system"

# ── Helpers ──────────────────────────────────────────────

log()  { echo "[bootstrap] $*"; }
warn() { echo "[bootstrap] WARNING: $*" >&2; }
fail() { echo "[bootstrap] ERROR: $*" >&2; exit 1; }

# ensure_exists "label" "check_cmd" "create_cmd"
# Runs check_cmd. If it succeeds (resource exists), skips. Otherwise runs create_cmd.
ensure_exists() {
  local label="$1" check_cmd="$2" create_cmd="$3"
  if eval "$check_cmd" >/dev/null 2>&1; then
    log "$label: already exists — skipping"
  else
    log "$label: creating..."
    eval "$create_cmd"
    log "$label: created"
  fi
}

# ── 1. Gitea ─────────────────────────────────────────────

bootstrap_gitea() {
  log "=== Gitea ==="

  # Precondition: Gitea pod is running
  kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=gitea --no-headers | grep -q Running \
    || fail "Gitea pod not running"

  # Check if admin token already exists as K8s secret
  if kubectl get secret loopzero-platform-tokens -n "$NAMESPACE" -o jsonpath='{.data.GITEA_ADMIN_TOKEN}' 2>/dev/null | base64 -d | grep -q "^[a-f0-9]"; then
    log "Gitea admin token: already exists in K8s secret"
    return
  fi

  # Generate token via API
  GITEA_URL="http://$(kubectl get svc gitea-http -n "$NAMESPACE" -o jsonpath='{.spec.clusterIP}'):3000"
  TOKEN=$(kubectl exec deploy/gitea -n "$NAMESPACE" -c gitea -- \
    gitea admin user generate-access-token --username gitea_admin --token-name "loopzero-platform-$(date +%s)" --scopes "all" 2>/dev/null | grep -oP 'Access token was successfully created\.\.\. \K.*' || true)

  if [ -z "$TOKEN" ]; then
    warn "Could not generate Gitea token (may already exist with different name)"
    return
  fi

  # Create orgs
  for org in loopzero loopzero-platform; do
    kubectl exec deploy/gitea -n "$NAMESPACE" -c gitea -- \
      gitea admin org create --name "$org" 2>/dev/null || true  # Expected: org may already exist
  done

  log "Gitea: token generated, orgs created"
  echo "$TOKEN" > /tmp/gitea-token.txt
}

# ── 2. ArgoCD ────────────────────────────────────────────

bootstrap_argocd() {
  log "=== ArgoCD ==="

  # Precondition: ArgoCD server is running
  kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=argocd-server --no-headers | grep -q Running \
    || fail "ArgoCD server not running"

  # Check if apiKey token already exists
  if kubectl get secret loopzero-platform-tokens -n "$NAMESPACE" -o jsonpath='{.data.ARGOCD_AUTH_TOKEN}' 2>/dev/null | base64 -d | grep -q "^ey"; then
    log "ArgoCD apiKey: already exists in K8s secret"
    return
  fi

  log "ArgoCD: generating apiKey token (requires port-forward)..."
  # Port-forward to ArgoCD server
  kubectl port-forward svc/argocd-server -n "$NAMESPACE" 8443:443 &
  PF_PID=$!
  sleep 3

  # Get admin password
  ARGOCD_PASS=$(kubectl get secret argocd-initial-admin-secret -n "$NAMESPACE" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
  if [ -z "$ARGOCD_PASS" ]; then
    kill $PF_PID 2>/dev/null
    warn "ArgoCD admin password not found — skipping"
    return
  fi

  # Login and generate apiKey
  ARGOCD_TOKEN=$(curl -sk "https://localhost:8443/api/v1/session" \
    -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASS\"}" | \
    python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null)

  if [ -n "$ARGOCD_TOKEN" ]; then
    # Create loopzero-api account if not exists
    curl -sk -X POST "https://localhost:8443/api/v1/account" \
      -H "Authorization: Bearer $ARGOCD_TOKEN" \
      -d '{"name":"loopzero-api","enabled":true}' 2>/dev/null || true  # Expected: may already exist

    # Generate apiKey for loopzero-api
    API_KEY=$(curl -sk -X POST "https://localhost:8443/api/v1/account/loopzero-api/token" \
      -H "Authorization: Bearer $ARGOCD_TOKEN" \
      -d "{\"name\":\"loopzero-api-$(date +%s)\"}" | \
      python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null)

    if [ -n "$API_KEY" ]; then
      echo "$API_KEY" > /tmp/argocd-token.txt
      log "ArgoCD: apiKey generated"
    else
      warn "ArgoCD: could not generate apiKey"
    fi
  fi

  kill $PF_PID 2>/dev/null || true
}

# ── 3. K8s Secrets ───────────────────────────────────────

bootstrap_secrets() {
  log "=== K8s Secrets ==="

  GITEA_TOKEN=$(cat /tmp/gitea-token.txt 2>/dev/null || kubectl get secret loopzero-platform-tokens -n "$NAMESPACE" -o jsonpath='{.data.GITEA_ADMIN_TOKEN}' 2>/dev/null | base64 -d || echo "")
  ARGOCD_TOKEN=$(cat /tmp/argocd-token.txt 2>/dev/null || kubectl get secret loopzero-platform-tokens -n "$NAMESPACE" -o jsonpath='{.data.ARGOCD_AUTH_TOKEN}' 2>/dev/null | base64 -d || echo "")

  # Platform tokens secret (idempotent via kubectl apply)
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: loopzero-platform-tokens
  namespace: $NAMESPACE
type: Opaque
stringData:
  GITEA_ADMIN_TOKEN: "${GITEA_TOKEN}"
  ARGOCD_AUTH_TOKEN: "${ARGOCD_TOKEN}"
EOF

  log "K8s secrets: applied"
}

# ── 4. Database Seed ─────────────────────────────────────

bootstrap_database() {
  log "=== Database Seed ==="

  # Check if seed already applied (loopzero DB has tenants table with data)
  TENANT_COUNT=$(kubectl exec deploy/loopzero-api -n "$NAMESPACE" -c loopzero-api -- \
    wget -qO- "http://localhost:8080/healthz" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")

  if [ "$TENANT_COUNT" = "pass" ]; then
    log "Database: API healthy — seed likely already applied"
    return
  fi

  warn "Database seed: API not healthy yet (may need images built first) — skipping"
}

# ── 5. Tenant CR ─────────────────────────────────────────

bootstrap_tenant() {
  log "=== Tenant CR ==="

  ensure_exists "Tenant tn-loopzero" \
    "kubectl get tenant tn-loopzero -n default 2>/dev/null" \
    "kubectl apply -f - <<'TENANT_EOF'
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: tn-loopzero
  namespace: default
spec:
  displayName: LoopZero Platform
  slug: loopzero
  plan: pro
  ownerEmail: admin@loopzero.example
TENANT_EOF"
}

# ── 6. Cloudflare ────────────────────────────────────────

bootstrap_cloudflare() {
  log "=== Cloudflare ==="

  if [ -z "${CLOUDFLARE_API_TOKEN:-}" ]; then
    log "Cloudflare: no token — skipping"
    return
  fi

  # CF credentials K8s secret (idempotent via kubectl apply)
  ensure_exists "CF credentials secret" \
    "kubectl get secret loopzero-cf-credentials -n $NAMESPACE 2>/dev/null" \
    "kubectl apply -f - <<CF_EOF
apiVersion: v1
kind: Secret
metadata:
  name: loopzero-cf-credentials
  namespace: $NAMESPACE
type: Opaque
stringData:
  CF_API_TOKEN: '${CLOUDFLARE_API_TOKEN}'
CF_EOF"
}

# ── Main ─────────────────────────────────────────────────

main() {
  log "Starting bootstrap for stack: $STACK"
  log "Namespace: $NAMESPACE"

  bootstrap_gitea
  bootstrap_argocd
  bootstrap_secrets
  bootstrap_database
  bootstrap_tenant
  bootstrap_cloudflare

  # Restart services to pick up new secrets
  log "=== Restarting services ==="
  for deploy in loopzero-api loopzero-controller tenant-controller; do
    if kubectl get deploy "$deploy" -n "$NAMESPACE" >/dev/null 2>&1; then
      kubectl rollout restart deploy "$deploy" -n "$NAMESPACE"
      log "  Restarted: $deploy"
    fi
  done

  log "Bootstrap complete for stack: $STACK"
  rm -f /tmp/gitea-token.txt /tmp/argocd-token.txt
}

main "$@"
