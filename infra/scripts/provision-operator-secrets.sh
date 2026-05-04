#!/usr/bin/env bash
# Provisions per-operator Secrets with the env-var schema expected by
# the 4 split operator charts (charts/loopzero-{tenant,app,preview,platform}-operator).
#
# MEET-070 / FB-#2 fix: chart Secret template ships empty stringData
# by default (.Values.secrets.giteaToken=""). For LOCAL cutover this
# script populates the existingSecret pattern with values sourced from
# loopzero-api-tokens (which has lowercase-hyphen keys).
#
# Schema mapping:
#   loopzero-api-tokens.gitea-token  →  loopzero-tenant-operator.GITEA_TOKEN
#                                   loopzero-app-operator.GITEA_TOKEN (no-op,
#                                       app-operator binary doesn't use it)
#
# This script complements provision-api-tokens.sh (which creates the
# loopzero-api-tokens Secret first). Run AFTER api tokens are provisioned.
#
# Usage:
#   bash infra/scripts/provision-operator-secrets.sh [<context>]
#
# Idempotent — safe to re-run after token rotation.

set -euo pipefail

CTX="${1:-k3d-loopzero-mgmt}"
NS="loopzero-system"
SOURCE_SECRET="loopzero-api-tokens"

log() { printf '[provision-operator-secrets %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

# 1. Sanity: source Secret exists.
if ! kubectl --context "$CTX" -n "$NS" get secret "$SOURCE_SECRET" >/dev/null 2>&1; then
  log "FAIL: $SOURCE_SECRET not found in $NS. Run: bash infra/scripts/provision-api-tokens.sh"
  exit 1
fi

GITEA_TOKEN=$(kubectl --context "$CTX" -n "$NS" get secret "$SOURCE_SECRET" \
  -o jsonpath='{.data.gitea-token}' | base64 -d)

if [[ -z "$GITEA_TOKEN" ]]; then
  log "FAIL: $SOURCE_SECRET.gitea-token is empty"
  exit 1
fi

log "Sourced GITEA_TOKEN (${#GITEA_TOKEN} chars) from $SOURCE_SECRET"

# 2. Create per-operator Secrets using the chart-expected schema.
# Only loopzero-tenant-operator binary needs GITEA_TOKEN; the other 3
# binaries don't use it but the chart Secret template renders the
# same shape regardless of which keys the binary actually consumes.
# When a chart's secrets.create=true, this script overrides the
# stringData to match what envFrom expects.

create_or_update_secret() {
  local name="$1"
  log "Patching Secret $name in $NS"
  kubectl --context "$CTX" -n "$NS" create secret generic "$name" \
    --from-literal=GITEA_TOKEN="$GITEA_TOKEN" \
    --dry-run=client -o yaml | \
    kubectl --context "$CTX" -n "$NS" apply -f - >/dev/null
}

# Tenant operator NEEDS GITEA_TOKEN (only one that does).
create_or_update_secret loopzero-tenant-operator

# App / preview / platform operators don't have GITEA_TOKEN dependency
# but the chart's Secret template still renders. Skipping these
# operators here means their Secret stays at chart-default empty;
# the chart should ideally not render a Secret for binaries that
# don't need it (FB-#2 follow-up: make secrets.create per-chart-default
# = false for app/preview/platform). For now, we let them stay empty
# because the binary's startup doesn't ENV-validate GITEA_TOKEN unless
# it actually uses it.

log "Done. Verify: kubectl --context $CTX -n $NS get secret loopzero-tenant-operator -o yaml"
