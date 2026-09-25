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
NAMESPACE="loopzero-system"

# ── Helpers ──────────────────────────────────────────────

log()  { echo "[bootstrap] $*"; }
warn() { echo "[bootstrap] WARNING: $*" >&2; }
fail() { echo "[bootstrap] ERROR: $*" >&2; exit 1; }

# ensure_exists "label" "check_cmd" "create_cmd"
# Runs check_cmd. If it succeeds (resource exists), skips. Otherwise runs create_cmd.
#
# eval-safe (T2.3 of plan 2026-05-21-deep-review-remediation): the only
# callers in this file pass internally-constructed kubectl commands
# (e.g., 'kubectl get ns loopzero-system'). No user-input flows to $check_cmd
# or $create_cmd. Refactoring to array expansion would require touching
# all ~25 callsites; deferred until ergonomic value justifies the churn.
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
  TOKEN=""
  TOKEN_OUTPUT=$(kubectl exec deploy/gitea -n "$NAMESPACE" -c gitea -- \
    gitea admin user generate-access-token --username gitea_admin --token-name "loopzero-platform-$(date +%s)" --scopes "all" 2>&1) || {
    # Expected: "token already exists" or similar — not a real failure
    if echo "$TOKEN_OUTPUT" | grep -qi "already exist\|duplicate"; then
      warn "Gitea token already exists with different name — skipping generation"
      return
    fi
    fail "Gitea token generation failed unexpectedly: $TOKEN_OUTPUT"
  }
  TOKEN=$(echo "$TOKEN_OUTPUT" | grep -oP '(?:Access token was successfully created[:.]+\s*)\K\S+')

  if [ -z "$TOKEN" ]; then
    warn "Could not parse Gitea token from output — skipping"
    return
  fi

  # Create orgs — idempotent: org creation fails with "already exists" if re-run
  for org in loopzero loopzero-platform; do
    ORG_OUTPUT=$(kubectl exec deploy/gitea -n "$NAMESPACE" -c gitea -- \
      gitea admin org create --name "$org" 2>&1) || {
      if echo "$ORG_OUTPUT" | grep -qi "already exist"; then
        log "Org $org: already exists — skipping"
      else
        fail "Gitea org '$org' creation failed: $ORG_OUTPUT"
      fi
    }
  done

  log "Gitea: token generated, orgs created"
  # Plan v1.2 T7.3 / D6 — secret transport via script-scope var (no /tmp world-readable).
  export GITEA_TOKEN_OUT="$TOKEN"
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
  # Port-forward to ArgoCD server (HTTP on port 80 — server runs with --insecure)
  kubectl port-forward svc/argocd-server -n "$NAMESPACE" 8443:80 &
  PF_PID=$!
  sleep 3

  # Get admin password
  ARGOCD_PASS=$(kubectl get secret argocd-initial-admin-secret -n "$NAMESPACE" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
  if [ -z "$ARGOCD_PASS" ]; then
    kill $PF_PID 2>/dev/null
    warn "ArgoCD admin password not found — skipping"
    return
  fi

  # Login and generate apiKey (HTTP — ArgoCD runs behind Traefik for TLS)
  ARGOCD_TOKEN=$(curl -s -H "Content-Type: application/json" "http://localhost:8443/api/v1/session" \
    -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASS\"}" | \
    python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null)

  if [ -n "$ARGOCD_TOKEN" ]; then
    # loopzero-api account is pre-configured via Helm values (accounts.loopzero-api: apiKey).
    # No REST API for account creation in ArgoCD — only token generation.
    # Generate apiKey for loopzero-api
    API_KEY=$(curl -s -X POST "http://localhost:8443/api/v1/account/loopzero-api/token" \
      -H "Authorization: Bearer $ARGOCD_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"name\":\"loopzero-api-$(date +%s)\"}" | \
      python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))" 2>/dev/null)

    if [ -n "$API_KEY" ]; then
      # Plan v1.2 T7.3 / D6 — secret transport via script-scope var (no /tmp world-readable).
      export ARGOCD_TOKEN_OUT="$API_KEY"
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

  # Plan v1.2 T7.3 / D6 — read from script-scope vars (set by bootstrap_gitea/argocd)
  # OR fall back to existing K8s Secret (idempotent re-run). No /tmp world-readable transport.
  GITEA_TOKEN="${GITEA_TOKEN_OUT:-$(kubectl get secret loopzero-platform-tokens -n "$NAMESPACE" -o jsonpath='{.data.GITEA_ADMIN_TOKEN}' 2>/dev/null | base64 -d || echo '')}"
  ARGOCD_TOKEN="${ARGOCD_TOKEN_OUT:-$(kubectl get secret loopzero-platform-tokens -n "$NAMESPACE" -o jsonpath='{.data.ARGOCD_AUTH_TOKEN}' 2>/dev/null | base64 -d || echo '')}"

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
  TENANT_COUNT=$(kubectl exec deploy/loopzero-api -n "$NAMESPACE" -c api -- \
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
    "kubectl get tenant tn-loopzero -n loopzero-system 2>/dev/null" \
    "kubectl apply -f - <<'TENANT_EOF'
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: tn-loopzero
  # B-095: era `default`, e o CR ficava INVISIVEL ao tenant-operator, cujo cache e' restrito a
  # `loopzero-system` (operators/cmd/tenant-operator/main.go). O seed reportava sucesso e nada
  # reconciliava. Fora do lugar ele tambem seria indeletavel se a cerca
  # `loopzero-tenant-namespace-policy` fosse ligada: remover finalizer e' um UPDATE, e ela nega
  # UPDATE fora deste namespace.
  namespace: loopzero-system
spec:
  displayName: LoopZero Platform
  plan: growth
  environments:
    - name: production
      cluster: runtime
    - name: staging
      cluster: runtime
  gitops:
    repoName: tn-loopzero
    branch: main
  limits:
    maxBuildConcurrency: 3
    maxCPU: \"4\"
    maxMemory: 8Gi
    maxStorage: 20Gi
  preview:
    maxEnvironments: 5
    ttlHours: 72
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
  # Plan v1.2 T7.3 — no /tmp secret files to clean up (transport now via script vars).
}

main "$@"
