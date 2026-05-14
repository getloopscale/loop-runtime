#!/usr/bin/env bash
# Provision homolog environment — validates the 3-project Pulumi split.
#
# Prerequisites:
#   - pulumi CLI logged in (pulumi login)
#   - DIGITALOCEAN_TOKEN env var set
#   - SPACES_ACCESS_KEY_ID + SPACES_SECRET_ACCESS_KEY env vars set
#   - CLOUDFLARE_API_TOKEN env var set (optional, for CF tunnel)
#   - PULUMI_CONFIG_PASSPHRASE env var set
#
# Usage:
#   ./infra/scripts/provision-homolog.sh [setup|preview|up|destroy]
#
# Steps:
#   1. Initialize stacks if needed
#   2. Set secrets from env vars
#   3. Deploy in order: infra → platform → control-plane

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
STACK="homolog"

log() { echo "$(date +%H:%M:%S) [homolog] $*"; }
fail() { echo "$(date +%H:%M:%S) [homolog] ERROR: $*" >&2; exit 1; }

# Validate required env vars
for var in DIGITALOCEAN_TOKEN SPACES_ACCESS_KEY_ID SPACES_SECRET_ACCESS_KEY; do
  if [ -z "${!var:-}" ]; then
    fail "Required env var $var is not set"
  fi
done

# ---------------------------------------------------------------------------
# Stack initialization
# ---------------------------------------------------------------------------

init_stack() {
  local project_dir="$1"
  local project_name="$2"

  log "Initializing stack $project_name/$STACK..."
  cd "$project_dir"

  # Install deps if needed
  if [ ! -d node_modules ]; then
    npm install --silent
  fi

  # Create stack if it doesn't exist
  pulumi stack init "$STACK" 2>/dev/null || true
  pulumi stack select "$STACK"

  # Set secrets from env vars
  pulumi config set --secret spacesAccessKeyId "$SPACES_ACCESS_KEY_ID" 2>/dev/null || true
  pulumi config set --secret spacesSecretAccessKey "$SPACES_SECRET_ACCESS_KEY" 2>/dev/null || true
  pulumi config set --secret digitaloceanToken "$DIGITALOCEAN_TOKEN" 2>/dev/null || true

  if [ -n "${CLOUDFLARE_API_TOKEN:-}" ]; then
    pulumi config set --secret cfApiToken "$CLOUDFLARE_API_TOKEN" 2>/dev/null || true
  fi

  # Tunnel secret (stable across runs)
  if ! pulumi config get tunnelSecret --stack "$STACK" >/dev/null 2>&1; then
    pulumi config set --secret tunnelSecret "$(openssl rand -base64 32)" 2>/dev/null || true
  fi

  log "  Stack $project_name/$STACK ready"
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------

cmd_setup() {
  log "=== Setting up homolog stacks ==="
  init_stack "$REPO_ROOT/infra/pulumi-infra" "loopzero-infra"
  init_stack "$REPO_ROOT/infra/pulumi-platform" "loopzero-platform"
  # Plan 2026-05-15 T2.1: pulumi-control-plane DELETED.
  # Control-plane workloads bootstrap via L2 helmfile (MEET-2026-05-12 D3).
  log "=== All stacks initialized ==="
}

cmd_preview() {
  log "=== Preview: loopzero-infra ==="
  cd "$REPO_ROOT/infra/pulumi-infra"
  pulumi stack select "$STACK"
  pulumi preview --diff

  log "=== Preview: loopzero-platform ==="
  cd "$REPO_ROOT/infra/pulumi-platform"
  pulumi stack select "$STACK"
  pulumi preview --diff

  # Plan 2026-05-15 T2.1: pulumi-control-plane preview removed (stack deleted).
}

cmd_up() {
  log "=== Deploying loopzero-infra (Phase 1) ==="
  cd "$REPO_ROOT/infra/pulumi-infra"
  pulumi stack select "$STACK"
  pulumi up --yes --skip-preview

  # Extract outputs for downstream stacks
  log "  Cluster registry:"
  pulumi stack output clusterRegistry --json 2>/dev/null | jq 'keys' || true
  log "  Cell registry:"
  pulumi stack output cellRegistry --json 2>/dev/null | jq 'keys' || true

  log "=== Deploying loopzero-platform (Phase 2) ==="
  cd "$REPO_ROOT/infra/pulumi-platform"
  pulumi stack select "$STACK"
  pulumi up --yes --skip-preview

  # Plan 2026-05-15 T2.1: loopzero-control-plane Phase 3 removed (stack deleted).
  # Control-plane workloads ship via L2 helmfile (post pulumi-platform).

  log "=== Homolog deployment complete ==="

  # Validation summary
  log "=== Validation Checklist ==="
  cd "$REPO_ROOT/infra/pulumi-infra"
  pulumi stack select "$STACK"

  CLUSTER_COUNT=$(pulumi stack output clusterRegistry --json 2>/dev/null | jq 'keys | length' || echo "0")
  CELL_COUNT=$(pulumi stack output cellRegistry --json 2>/dev/null | jq 'keys | length' || echo "0")
  log "  Clusters: $CLUSTER_COUNT (expected: 4 — 1 mgmt + 1 build + 2 runtime)"
  log "  Cells: $CELL_COUNT (expected: 2 — cell-default + cell-sfo3-02)"
}

cmd_destroy() {
  log "=== Destroying homolog (reverse order) ==="

  # Plan 2026-05-15 T2.1: loopzero-control-plane destroy removed (stack deleted).

  log "  Destroying loopzero-platform..."
  cd "$REPO_ROOT/infra/pulumi-platform"
  pulumi stack select "$STACK"
  pulumi destroy --yes --skip-preview 2>/dev/null || true

  log "  Destroying loopzero-infra..."
  cd "$REPO_ROOT/infra/pulumi-infra"
  pulumi stack select "$STACK"
  pulumi destroy --yes --skip-preview 2>/dev/null || true

  log "=== Homolog destroyed ==="
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

ACTION="${1:-setup}"
case "$ACTION" in
  setup)   cmd_setup ;;
  preview) cmd_preview ;;
  up)      cmd_setup && cmd_up ;;
  destroy) cmd_destroy ;;
  *)       fail "Usage: $0 [setup|preview|up|destroy]" ;;
esac
