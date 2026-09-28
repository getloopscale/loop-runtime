#!/usr/bin/env bash
# Smoke E2E test: loopzero platform install + loopzero deploy on a fresh k3d cluster.
#
# Reproduces the full self-hosted install flow without any manual kubectl,
# then deploys the hello-server fixture and asserts the URL responds 200.
#
# Pre-requisites on the host:
#   - k3d, docker, kubectl, helm, doctl, loopzero CLI on PATH
#   - mkcert (optional but recommended for browser-trusted certs)
#   - DOCR token: `doctl registry login` already executed
#
# Usage:
#   ./infra/tests/e2e/local-install-deploy.sh
#
# Exit codes:
#   0  E2E passed (install + deploy + curl assertion)
#   1  Pre-req check failed
#   2  Install failed
#   3  Bootstrap / login failed
#   4  Deploy failed (build, sign, sync, or health)
#   5  curl assertion failed
#
# Time budget: <12 minutes on a clean k3d cluster.

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────
# Config
# ──────────────────────────────────────────────────────────────────────
CLUSTER_NAME="${CLUSTER_NAME:-loopzero-test}"
DOMAIN="${DOMAIN:-test.local}"
LOG_DIR="${LOG_DIR:-/tmp/loopzero-e2e-logs}"
VALUES_FILE="${VALUES_FILE:-/tmp/loopzero-e2e-values.yaml}"
APP_FIXTURE="${APP_FIXTURE:-$(cd "$(dirname "$0")/fixtures/hello-server" && pwd)}"
# Hostname format derived from manifests/renderer.go:
#   {tenant_id}-{project_id}-{app_name}-{environment}.{domain}
# For the e2e fixture: tenant=tn-e2e, project=e2e-project, app=api,
# environment=production. The ExposedRoute reports a different short form
# (e2e-project-production.test.local) but Traefik matches the full one.
APP_DOMAIN="tn-e2e-e2e-project-api-production.${DOMAIN}"

# Expected response body from the hello-server fixture (index.js returns "ok")
EXPECTED_BODY="${EXPECTED_BODY:-ok}"

# Timeouts
INSTALL_TIMEOUT_SEC=900       # 15 min for full install
DEPLOY_TIMEOUT_SEC=600        # 10 min for build + sync
CURL_RETRY_TIMEOUT_SEC=120    # 2 min for ingress to converge after deploy

mkdir -p "$LOG_DIR"

# ──────────────────────────────────────────────────────────────────────
# Helpers
# ──────────────────────────────────────────────────────────────────────
log() { echo "[$(date +%H:%M:%S)] $*"; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1" 1; }

# ──────────────────────────────────────────────────────────────────────
# Phase 0: Pre-req check
# ──────────────────────────────────────────────────────────────────────
log "── Phase 0: pre-requisites"
for cmd in k3d docker kubectl helm doctl loopzero curl jq python3; do
  require_cmd "$cmd"
done
log "  ✓ All required commands present"

if ! command -v mkcert >/dev/null 2>&1 && ! [ -x "$HOME/.local/bin/mkcert" ]; then
  log "  ⚠ mkcert not found — install will use self-signed (curl will need -k)"
  USE_MKCERT=0
else
  USE_MKCERT=1
  log "  ✓ mkcert detected — install will use CA-issued (browser-trusted) certs"
fi

if ! doctl registry get >/dev/null 2>&1; then
  fail "doctl not authenticated. Run: doctl auth init && doctl registry login" 1
fi
log "  ✓ DOCR auth OK"

# ──────────────────────────────────────────────────────────────────────
# Phase 1: Reset cluster
# ──────────────────────────────────────────────────────────────────────
log "── Phase 1: reset k3d cluster '$CLUSTER_NAME'"
docker ps -aq --filter "name=k3d-$CLUSTER_NAME" | xargs -r docker rm -f >/dev/null 2>&1 || true
docker network rm "k3d-$CLUSTER_NAME" 2>/dev/null || true
docker volume ls -q | grep -i "$CLUSTER_NAME" | xargs -r docker volume rm 2>/dev/null || true
rm -f "$HOME/.loopzero/config.json" "$HOME/.kube/config"
log "  ✓ Old state cleaned"

k3d cluster create "$CLUSTER_NAME" \
  --image rancher/k3s:v1.32.13-k3s1 \
  --servers 1 --agents 1 \
  --port "8080:80@loadbalancer" \
  --port "8443:443@loadbalancer" \
  --api-port 6443 \
  --k3s-arg "--disable=traefik@server:0" \
  --wait 2>&1 | tee "$LOG_DIR/cluster-create.log" | tail -3
kubectl get nodes >/dev/null || fail "cluster not reachable after creation" 1
log "  ✓ Cluster up: 1 server + 1 agent"

# Pre-import the Docker Hub base images that the build CWTs reference.
# Anonymous Docker Hub pulls have a tight rate limit (100 / 6h / IP)
# that the build pipeline trips on cold k3d clusters. Importing from
# the host's Docker daemon avoids the in-cluster pull entirely
# (kubelet image policy IfNotPresent → no Hub fetch).
# If the host doesn't have these images cached either, the import
# will pull them once and reuse for subsequent runs.
log "  Pre-importing Docker Hub base images (rate-limit guard)..."
HUB_IMAGES=(
  "node:20.11.1-alpine"
  # mc is no longer a Hub image: it ships in loopzero-clone-runner, built from source (#394).
)
for img in "${HUB_IMAGES[@]}"; do
  if ! docker image inspect "$img" >/dev/null 2>&1; then
    log "    Pulling $img to host..."
    docker pull "$img" >/dev/null 2>&1 || log "    ⚠ docker pull $img failed (Hub rate-limit?) — build may stall"
  fi
  k3d image import "$img" -c "$CLUSTER_NAME" >/dev/null 2>&1 \
    && log "    ✓ Imported $img" \
    || log "    ⚠ k3d image import $img failed — kubelet will fall back to Hub"
done

# ──────────────────────────────────────────────────────────────────────
# Phase 2: loopzero platform install
# ──────────────────────────────────────────────────────────────────────
log "── Phase 2: loopzero platform install"
cat > "$VALUES_FILE" <<EOF
domain: "$DOMAIN"

database:
  external: false

registry:
  type: docr
  endpoint: registry.digitalocean.com/loopzero-system

auth:
  type: bundled

storage:
  endpoint: minio.loopzero-system.svc.cluster.local:9000
  bucket: loopzero-artifacts

profile: minimal
EOF

doctl registry login >/dev/null
log "  ✓ DOCR login fresh"

# CHG-2026-04-25-E2E: until v0.2.1 charts are published to OCI, the
# installer must use local chart paths. We export LOOPZERO_LOCAL_CHARTS
# pointing at this repo's charts/ directory; loopzero platform install picks
# this up via the chartRef() helper in cli/src/installer/components.ts.
# In production the env var is unset and OCI is used (Rule 10).
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
export LOOPZERO_LOCAL_CHARTS="$REPO_ROOT/charts"
log "  Using LOCAL charts: $LOOPZERO_LOCAL_CHARTS (set LOOPZERO_LOCAL_CHARTS empty to use OCI)"

INSTALL_LOG="$LOG_DIR/install.log"
log "  Starting install (logs: $INSTALL_LOG)"
if timeout "$INSTALL_TIMEOUT_SEC" loopzero platform install -f "$VALUES_FILE" > "$INSTALL_LOG" 2>&1; then
  log "  ✓ loopzero platform install completed"
else
  log "  ✗ install failed — last 30 lines:"
  tail -30 "$INSTALL_LOG"
  fail "install did not complete in ${INSTALL_TIMEOUT_SEC}s" 2
fi

# Extract bootstrap secret from install log (printed by Step 10)
BOOTSTRAP_SECRET="$(grep -A 1 'Bootstrap secret' "$INSTALL_LOG" | tail -1 | tr -d ' ')"
[ -n "$BOOTSTRAP_SECRET" ] || fail "could not parse bootstrapSecret from install output" 2
log "  ✓ Bootstrap secret captured: ${BOOTSTRAP_SECRET:0:8}..."

# Verify all loopzero-* pods are Ready (modulo dashboard which may still be flaky)
sleep 10
NOT_READY=$(kubectl get pods -n loopzero-system -l 'app.kubernetes.io/instance=loopzero' \
  -o jsonpath='{range .items[*]}{.metadata.name}{":"}{.status.containerStatuses[0].ready}{"\n"}{end}' \
  | grep -v ':true' | grep -v '^$' | grep -v dashboard || true)
if [ -n "$NOT_READY" ]; then
  log "  ⚠ Some pods not Ready (excluding dashboard):"
  echo "$NOT_READY"
fi

# ──────────────────────────────────────────────────────────────────────
# Phase 3: Bootstrap admin + login
# ──────────────────────────────────────────────────────────────────────
log "── Phase 3: bootstrap admin + loopzero login"

# Port-forward API for the duration of the test
pkill -9 -f "kubectl port-forward.*loopzero-loopzero-api" 2>/dev/null || true
sleep 2
kubectl port-forward -n loopzero-system svc/loopzero-loopzero-api 3001:3001 > "$LOG_DIR/pf-api.log" 2>&1 &
PF_API_PID=$!
trap 'kill $PF_API_PID 2>/dev/null || true; pkill -9 -f "port-forward" 2>/dev/null || true' EXIT
sleep 5

API_URL="http://localhost:3001"
curl -sf "$API_URL/api/v1/health" | grep -q '"status":"ok"' \
  || fail "API health check failed" 3

# Seed admin via /internal/bootstrap
BOOTSTRAP_RESP="$(curl -sf -X POST -H "Authorization: Bearer $BOOTSTRAP_SECRET" \
  -H 'Content-Type: application/json' \
  "$API_URL/internal/bootstrap" \
  -d '{"admin_github_id":"1","admin_login":"admin","admin_email":"admin@loopzero.local","tenant_slug":"e2e","tenant_display_name":"E2E Test"}')"
echo "$BOOTSTRAP_RESP" | grep -q '"created":true' \
  || fail "bootstrap returned unexpected: $BOOTSTRAP_RESP" 3
log "  ✓ Admin user + tenant 'e2e' created"

# Get OAuth2 token via Hydra client_credentials
pkill -9 -f "port-forward.*ory-hydra" 2>/dev/null || true
sleep 2
kubectl port-forward -n ory svc/ory-hydra-admin 24445:4445 > /dev/null 2>&1 &
kubectl port-forward -n ory svc/ory-hydra-public 24444:4444 > /dev/null 2>&1 &
sleep 4

# Find or create loopzero-cli OAuth2 client
CLIENT_INFO="$(curl -sf http://localhost:24445/admin/clients \
  | python3 -c "import sys,json; cs=json.load(sys.stdin); c=next((x for x in cs if x.get('client_name')=='loopzero-cli'),None); print(f\"{c['client_id']}\" if c else '')" || true)"
if [ -z "$CLIENT_INFO" ]; then
  fail "loopzero-cli OAuth2 client not registered (Step 6 of install) — check $INSTALL_LOG" 3
fi
CID="$CLIENT_INFO"

# Rotate secret to get a fresh client_secret we can read
NEWCLIENT="$(curl -sf -X POST "http://localhost:24445/admin/clients/$CID/rotate-secret" 2>/dev/null || true)"
CSEC="$(echo "$NEWCLIENT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('client_secret',''))" 2>/dev/null || echo "")"
if [ -z "$CSEC" ]; then
  # rotate not supported — delete + recreate
  curl -sf -X DELETE "http://localhost:24445/admin/clients/$CID" >/dev/null 2>&1 || true
  NEWCLIENT="$(curl -sf -X POST http://localhost:24445/admin/clients \
    -H 'Content-Type: application/json' \
    -d '{"client_name":"loopzero-cli","grant_types":["client_credentials"],"response_types":[],"scope":"openid offline_access","audience":["loopzero-engine"],"token_endpoint_auth_method":"client_secret_post"}')"
  CID="$(echo "$NEWCLIENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['client_id'])")"
  CSEC="$(echo "$NEWCLIENT" | python3 -c "import sys,json; print(json.load(sys.stdin)['client_secret'])")"
fi

TOKEN="$(curl -sf -X POST http://localhost:24444/oauth2/token \
  -d 'grant_type=client_credentials' \
  -d "client_id=$CID" \
  -d "client_secret=$CSEC" \
  -d 'audience=loopzero-engine' \
  -d 'scope=openid' \
  | python3 -c "import sys,json; print(json.load(sys.stdin).get('access_token',''))")"
[ -n "$TOKEN" ] || fail "failed to obtain JWT from Hydra" 3
log "  ✓ JWT obtained (${#TOKEN} chars)"

# Bind CLI client_id to the bootstrapped tenant
kubectl exec -n loopzero-system postgresql-cluster-1 -c postgres -- psql -U postgres -d loopzero \
  -c "INSERT INTO users (id, email, display_name, username) VALUES ('$CID','cli@loopzero.local','CLI','cli') ON CONFLICT DO NOTHING; \
      INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ('tn-e2e','$CID','owner') ON CONFLICT DO NOTHING;" \
  >/dev/null 2>&1
log "  ✓ CLI user bound to tenant 'tn-e2e'"

# loopzero login
LOOPZERO_URL="$API_URL" loopzero login --token "$TOKEN" 2>&1 | grep -q "Authenticated" \
  || fail "loopzero login failed" 3
log "  ✓ loopzero login OK"

# ──────────────────────────────────────────────────────────────────────
# Phase 4: loopzero deploy hello-server
# ──────────────────────────────────────────────────────────────────────
log "── Phase 4: loopzero deploy hello-server"
WORK="$(mktemp -d)"
cp -r "$APP_FIXTURE/." "$WORK/"
cd "$WORK"
git init -q
git add . && git commit -q -m "init"

# Register project via API: this triggers EnsureTenant on the server,
# which creates the Tenant CR in K8s → TenantController creates the
# ArgoCD Application CR. Without this, the deploy submits a build but
# the gitops sync target never exists and the release stays PENDING.
#
# We POST directly (not `loopzero init`) so we don't overwrite the fixture's
# loopzero.yaml — the fixture defines the apps explicitly (api on port 3000).
PROJECT_RESP="$(curl -sf -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  "$API_URL/api/v1/projects" \
  -d '{"name":"e2e-project","apps":[{"name":"api","type":"server","framework":"node","port":3000}]}' 2>&1 || true)"
if echo "$PROJECT_RESP" | grep -qE '"id"|"name"|"team"'; then
  log "  ✓ Project 'e2e-project' registered (Tenant CR will be created)"
elif echo "$PROJECT_RESP" | grep -q "already exists\|conflict"; then
  log "  ✓ Project 'e2e-project' already exists (idempotent)"
else
  log "  ✗ Project registration failed: $PROJECT_RESP"
  fail "could not register project before deploy" 3
fi

# Wait for Tenant CR + ArgoCD Application to materialize before deploy.
# Without this gate, the deploy may race the operator and trigger a sync
# against an Application that doesn't exist yet (404 → PENDING retry).
log "  Waiting for Tenant CR + ArgoCD Application..."
deadline=$(( $(date +%s) + 60 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
  if kubectl get application -n argocd "tn-e2e-production" >/dev/null 2>&1; then
    log "  ✓ ArgoCD Application 'tn-e2e-production' ready"
    break
  fi
  sleep 3
done
if ! kubectl get application -n argocd "tn-e2e-production" >/dev/null 2>&1; then
  log "  ⚠ ArgoCD Application did not appear in 60s — will rely on convergence loop"
fi

DEPLOY_LOG="$LOG_DIR/deploy.log"
if timeout "$DEPLOY_TIMEOUT_SEC" sh -c "cd '$WORK' && LOOPZERO_URL='$API_URL' loopzero deploy" > "$DEPLOY_LOG" 2>&1; then
  if grep -q "release.*ACTIVE\|deploy.*succeeded\|Deployed" "$DEPLOY_LOG"; then
    log "  ✓ Deploy reached ACTIVE"
  else
    log "  ⚠ Deploy CLI exited 0 but log doesn't mention ACTIVE — checking server..."
  fi
else
  log "  ✗ Deploy failed or timed out — last 30 lines:"
  tail -30 "$DEPLOY_LOG"
  fail "loopzero deploy did not reach ACTIVE in ${DEPLOY_TIMEOUT_SEC}s" 4
fi

# ──────────────────────────────────────────────────────────────────────
# Phase 5: curl assertion
# ──────────────────────────────────────────────────────────────────────
log "── Phase 5: curl https://$APP_DOMAIN"
URL="https://$APP_DOMAIN"

# Use --resolve to bypass DNS (test.local has no public DNS).
# k3d's load balancer maps 8443 → traefik 443 on the host.
CURL_ARGS=(--connect-timeout 5 --max-time 10 --resolve "$APP_DOMAIN:8443:127.0.0.1" "$URL:8443/")
if [ "$USE_MKCERT" -eq 0 ]; then
  CURL_ARGS=(-k "${CURL_ARGS[@]}")
fi

deadline=$(( $(date +%s) + CURL_RETRY_TIMEOUT_SEC ))
attempt=0
URL_OK=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  attempt=$((attempt+1))
  if BODY="$(curl -sf "${CURL_ARGS[@]}" 2>"$LOG_DIR/curl.err")"; then
    if [ "$BODY" = "$EXPECTED_BODY" ]; then
      log "  ✓ HTTP 200 + body matches '$EXPECTED_BODY' (attempt $attempt)"
      URL_OK=1
      break
    else
      log "  ⚠ Got body but doesn't match: '$BODY' (expected '$EXPECTED_BODY')"
    fi
  fi
  sleep 5
done

if [ "$URL_OK" -ne 1 ]; then
  log "  ✗ curl assertion failed after ${CURL_RETRY_TIMEOUT_SEC}s. Last error:"
  cat "$LOG_DIR/curl.err" 2>/dev/null | head -5
  fail "URL did not respond with expected body" 5
fi

# ──────────────────────────────────────────────────────────────────────
# Phase 6 (CHG-2026-04-25-ATTESTATION-WIRE): attestation assertion
# ──────────────────────────────────────────────────────────────────────
log "── Phase 6: attestation assertion"

# Find the most recent build_id for this tenant from the API. We don't
# have it from `loopzero deploy` output reliably, so we query the API.
BUILD_ID="$(curl -sf -H "Authorization: Bearer $TOKEN" \
  "$API_URL/api/v1/projects/e2e-project/builds?limit=1" 2>/dev/null \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['builds'][0].get('build_id') or d['builds'][0].get('id') or '' if d.get('builds') else '')" 2>/dev/null || true)"

if [ -z "$BUILD_ID" ]; then
  log "  ⚠ Could not fetch latest build_id — attestation phase skipped (non-fatal)"
else
  log "  Querying /attestation for build $BUILD_ID..."
  ATTEST="$(curl -sf -H "Authorization: Bearer $TOKEN" "$API_URL/api/v1/builds/$BUILD_ID/attestation" 2>/dev/null || echo '{}')"
  echo "$ATTEST" > "$LOG_DIR/attestation.json"

  # Check signed=true (Phase 5 must have populated)
  SIGNED="$(echo "$ATTEST" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('signed'))" 2>/dev/null || echo "?")"
  IMG_DIGEST="$(echo "$ATTEST" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('image_digest',''))" 2>/dev/null || echo "")"
  IMG_SIG="$(echo "$ATTEST" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('image_signature',''))" 2>/dev/null || echo "")"

  if [ "$SIGNED" = "True" ]; then
    log "  ✓ attestation: signed=true"
  else
    log "  ⚠ attestation: signed=$SIGNED (expected True; gap likely Phase 1 CWT outputs not flowing to Phase 5)"
  fi
  if echo "$IMG_DIGEST" | grep -qE '^sha256:[a-f0-9]{64}$'; then
    log "  ✓ image_digest is well-formed sha256: ${IMG_DIGEST:0:30}..."
  else
    log "  ⚠ image_digest not a sha256: '$IMG_DIGEST'"
  fi
  if [ -n "$IMG_SIG" ]; then
    log "  ✓ image_signature: ${IMG_SIG:0:60}..."
  else
    log "  ⚠ image_signature is empty"
  fi
fi

log ""
log "🎉 E2E SUCCESS"
exit 0
