#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.4
# Cross-cluster wiring: registers build + runtime cluster(s) with the mgmt
# cluster's ArgoCD, creates scoped kubeconfigs, and seeds Prometheus federation.
# Executed as a Helmfile post-sync hook from mgmt.helmfile.yaml.gotmpl.
#
# Usage:
#   wire-clusters.sh <env>
#
# Required env vars:
#   LOOPZERO_MGMT_KUBECONFIG, LOOPZERO_BUILD_KUBECONFIG, LOOPZERO_RUNTIME_KUBECONFIG
#   (optional) LOOPZERO_RUNTIME2_KUBECONFIG
set -euo pipefail
ENV="${1:?usage: $0 <env>}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
log() { printf '[wire-clusters] %s\n' "$*" >&2; }

if [[ -z "${LOOPZERO_MGMT_KUBECONFIG:-}" ]]; then
  log "ERROR: LOOPZERO_MGMT_KUBECONFIG not set"
  exit 1
fi

# ArgoCD cluster registration (B6 fix in lib/argocd-register.sh).
export KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG"

if [[ -n "${LOOPZERO_BUILD_KUBECONFIG:-}" ]]; then
  log "wiring build cluster..."
  SCOPED=$(KUBECONFIG="$LOOPZERO_BUILD_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" build "$LOOPZERO_BUILD_KUBECONFIG")
  KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" build "$SCOPED"
fi

if [[ -n "${LOOPZERO_RUNTIME_KUBECONFIG:-}" ]]; then
  log "wiring runtime cluster..."
  SCOPED=$(KUBECONFIG="$LOOPZERO_RUNTIME_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" runtime "$LOOPZERO_RUNTIME_KUBECONFIG")
  KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" runtime "$SCOPED"
  # CHG-2026-04-30-INFRA-NORMALIZE T5.3b: also create ClusterCredential CR
  # so K8sCellStore.LoadCells() picks it up. cell-1 maps to the canonical
  # first runtime cluster. Failure is non-fatal (operator can rerun the
  # lib script directly) — preserves wire-clusters.sh idempotency intent.
  KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG" "$SCRIPT_DIR/lib/clustercredential-create.sh" \
    cell-1 runtime "$SCOPED" \
    || log "WARN: ClusterCredential cell-1 creation failed (continuing)"
fi

if [[ -n "${LOOPZERO_RUNTIME2_KUBECONFIG:-}" ]]; then
  log "wiring runtime-2 cluster..."
  SCOPED=$(KUBECONFIG="$LOOPZERO_RUNTIME2_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" runtime-2 "$LOOPZERO_RUNTIME2_KUBECONFIG")
  KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" runtime-2 "$SCOPED"
  # T5.3b: cell-2 maps to runtime-2. Cell starts in 'maintenance' status
  # per cells migration 000003 seed, so admin pin admission rejects
  # until canary stage 1 flips it 'active'.
  KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG" "$SCRIPT_DIR/lib/clustercredential-create.sh" \
    cell-2 runtime-2 "$SCOPED" \
    || log "WARN: ClusterCredential cell-2 creation failed (continuing)"
fi

# B11 fix — Gitea repos archived: re-activate any archived repos so tenant
# delete-then-recreate works without manual unarchive.
if [[ -n "${LOOPZERO_GITEA_TOKEN:-}" && -n "${LOOPZERO_GITEA_URL:-}" ]]; then
  log "checking for archived gitea repos..."
  ARCHIVED=$(curl -sf -H "Authorization: token $LOOPZERO_GITEA_TOKEN" \
    "$LOOPZERO_GITEA_URL/api/v1/repos/search?archived=true&limit=50" 2>/dev/null \
    | python3 -c 'import sys,json; data=json.load(sys.stdin); [print(r["full_name"]) for r in data.get("data",[])]' 2>/dev/null || echo "")
  for repo in $ARCHIVED; do
    log "unarchiving $repo (B11)"
    curl -sf -X PATCH -H "Authorization: token $LOOPZERO_GITEA_TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"archived":false}' \
      "$LOOPZERO_GITEA_URL/api/v1/repos/$repo" >/dev/null || log "WARN: failed to unarchive $repo"
  done
fi

log "cross-cluster wiring complete (env=$ENV)"
