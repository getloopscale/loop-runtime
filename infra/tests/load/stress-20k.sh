#!/usr/bin/env bash
# Stress Test: 20k+ apps with real loopzero-stacks templates.
# Uses diverse frameworks (Go, Node, Python, Ruby, Rust, Java, PHP).
#
# Requirements:
#   - API accessible at LOOPZERO_API_URL
#   - LOOPZERO_SERVICE_AUTH set
#   - loopzero-stacks cloned at /tmp/loopzero-stacks
#   - Build cluster scaled (20+ nodes large)
#   - Runtime cluster scaled (50+ nodes large)
#
# Usage:
#   ./infra/tests/load/stress-20k.sh [target_tenants] [concurrency]
#   ./infra/tests/load/stress-20k.sh 5000 100

set -euo pipefail

TARGET_TENANTS=${1:-5000}
CONCURRENCY=${2:-50}
LOOPZERO_API_URL=${LOOPZERO_API_URL:-https://api.homolog.loopzero.example}
LOOPZERO_SERVICE_AUTH=${LOOPZERO_SERVICE_AUTH:-$(kubectl --context=do-nyc3-loopzero-infra-homolog-nyc3-mgmt -n loopzero-system get secret loopzero-api-secrets -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d)}
STACKS_DIR="/tmp/loopzero-stacks/templates"
BOOTSTRAP_SECRET="loopzero-dev-bootstrap-secret"

# Templates that have Dockerfile (can build without buildpack)
TEMPLATES=(go-api node-express python-fastapi ruby-sinatra rust-axum node-fastify php-slim java-spring)

echo "=== STRESS TEST: $TARGET_TENANTS tenants, concurrency $CONCURRENCY ==="
echo "  API: $LOOPZERO_API_URL"
echo "  Templates: ${#TEMPLATES[@]} diverse stacks"
echo "  Started: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

# --- Phase 1: Provision tenants ---
echo "--- Phase 1: Provisioning $TARGET_TENANTS tenants ---"
provision_tenant() {
  local i=$1
  local slug="stress-$i"
  curl -sf -X POST "$LOOPZERO_API_URL/internal/bootstrap" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $BOOTSTRAP_SECRET" \
    -d "{\"admin_github_id\":\"$((9000000+i))\",\"admin_login\":\"stress-user-$i\",\"tenant_slug\":\"$slug\",\"tenant_display_name\":\"Stress $i\",\"plan\":\"enterprise\"}" \
    > /dev/null 2>&1
  # Create project
  curl -sf -X POST "$LOOPZERO_API_URL/api/v1/projects" \
    -H "Content-Type: application/json" \
    -H "X-Service-Auth: $LOOPZERO_SERVICE_AUTH" \
    -H "X-LoopZero-User-ID: github-stress-user-$i" \
    -H "X-LoopZero-Tenant: tn-$slug" \
    -H "X-LoopZero-Email: stress-$i@loadtest.loopzero.dev" \
    -d "{\"name\":\"app-$i\"}" \
    > /dev/null 2>&1
}
export -f provision_tenant
export LOOPZERO_API_URL LOOPZERO_SERVICE_AUTH BOOTSTRAP_SECRET

seq 0 $((TARGET_TENANTS-1)) | xargs -P "$CONCURRENCY" -I {} bash -c 'provision_tenant {}'
echo "  Provisioned $TARGET_TENANTS tenants"

# --- Phase 2: Prepare bundles per template ---
echo "--- Phase 2: Preparing bundles ---"
BUNDLE_DIR="/tmp/stress-bundles"
rm -rf "$BUNDLE_DIR" && mkdir -p "$BUNDLE_DIR"

for tmpl in "${TEMPLATES[@]}"; do
  TMPDIR=$(mktemp -d)
  cp -r "$STACKS_DIR/$tmpl"/* "$TMPDIR/" 2>/dev/null || true
  rm -rf "$TMPDIR/.git"
  cd "$TMPDIR"
  git init -q && git config user.email "stress@loopzero.dev" && git config user.name "Stress"
  git add -A && git commit -q -m "stress $tmpl"
  git bundle create "$BUNDLE_DIR/$tmpl.bundle" HEAD
  cd /home/paulo/Projetos/loopzero/loopzero
  rm -rf "$TMPDIR"
done
echo "  Prepared ${#TEMPLATES[@]} bundles"

# --- Phase 3: Upload + submit builds ---
echo "--- Phase 3: Submitting $TARGET_TENANTS builds (concurrency $CONCURRENCY) ---"
submit_deploy() {
  local i=$1
  local slug="stress-$i"
  local tenant="tn-$slug"
  local tmpl_idx=$((i % ${#TEMPLATES[@]}))
  local tmpl="${TEMPLATES[$tmpl_idx]}"
  local bundle="$BUNDLE_DIR/$tmpl.bundle"

  # Read loopzero.yaml from template
  local loopzero_yaml
  loopzero_yaml=$(cat "$STACKS_DIR/$tmpl/loopzero.yaml" | sed "s/{{project-name}}/app-$i/g")

  # Upload source
  local upload_result
  upload_result=$(curl -sf -X POST "$LOOPZERO_API_URL/api/v1/source/upload" \
    -H "X-Service-Auth: $LOOPZERO_SERVICE_AUTH" \
    -H "X-LoopZero-User-ID: github-stress-user-$i" \
    -H "X-LoopZero-Tenant: $tenant" \
    -H "X-LoopZero-Email: stress-$i@loadtest.loopzero.dev" \
    -F "loopzero_yaml=$loopzero_yaml" \
    -F "bundle=@$bundle" 2>/dev/null) || { echo "FAIL upload $i"; return; }

  local commit_sha
  commit_sha=$(echo "$upload_result" | python3 -c "import json,sys; print(json.load(sys.stdin).get('commit_sha',''))" 2>/dev/null)
  [ -z "$commit_sha" ] && { echo "FAIL parse $i"; return; }

  # Submit build
  local framework port build_type
  case "$tmpl" in
    go-api) framework="custom"; port=8080; build_type="dockerfile" ;;
    node-*) framework="express"; port=3000; build_type="dockerfile" ;;
    python-*) framework="fastapi"; port=8000; build_type="dockerfile" ;;
    ruby-*) framework="sinatra"; port=4567; build_type="dockerfile" ;;
    rust-*) framework="axum"; port=8080; build_type="dockerfile" ;;
    php-*) framework="slim"; port=8000; build_type="dockerfile" ;;
    java-*) framework="spring-boot"; port=8080; build_type="dockerfile" ;;
    *) framework="custom"; port=3000; build_type="dockerfile" ;;
  esac

  curl -sf -X POST "$LOOPZERO_API_URL/api/v1/builds" \
    -H "Content-Type: application/json" \
    -H "X-Service-Auth: $LOOPZERO_SERVICE_AUTH" \
    -H "X-LoopZero-User-ID: github-stress-user-$i" \
    -H "X-LoopZero-Tenant: $tenant" \
    -H "X-LoopZero-Email: stress-$i@loadtest.loopzero.dev" \
    -H "Idempotency-Key: stress-$i-$(date +%s)" \
    -d "{\"project_id\":\"app-$i\",\"environment\":\"production\",\"commit_sha\":\"$commit_sha\",\"apps\":[{\"name\":\"api\",\"build\":\"$build_type\",\"framework\":\"$framework\",\"render\":\"server\",\"port\":$port}]}" \
    > /dev/null 2>&1 && echo "OK $i ($tmpl)" || echo "FAIL build $i"
}
export -f submit_deploy
export BUNDLE_DIR STACKS_DIR TEMPLATES

seq 0 $((TARGET_TENANTS-1)) | xargs -P "$CONCURRENCY" -I {} bash -c 'submit_deploy {}'

echo ""
echo "=== Stress test submitted ==="
echo "  Tenants: $TARGET_TENANTS"
echo "  Builds submitted: check queue depth"
echo "  Monitor: kubectl top nodes on build/runtime clusters"
echo "  Finished: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
