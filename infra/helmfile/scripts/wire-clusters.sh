#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.4
# Cross-cluster wiring: registers build + runtime cluster(s) with the mgmt
# cluster's ArgoCD, creates scoped kubeconfigs, and seeds Prometheus federation.
# Executed as a Helmfile post-sync hook from core.helmfile.yaml.gotmpl.
#
# Usage:
#   wire-clusters.sh <env>
#
# Required env vars:
#   LOOPRUNTIME_CORE_KUBECONFIG, LOOPRUNTIME_BUILD_KUBECONFIG, LOOPRUNTIME_CELL_0_KUBECONFIG
#   (optional) LOOPRUNTIME_RUNTIME2_KUBECONFIG
set -euo pipefail
ENV="${1:?usage: $0 <env>}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
log() { printf '[wire-clusters] %s\n' "$*" >&2; }

if [[ -z "${LOOPRUNTIME_CORE_KUBECONFIG:-}" ]]; then
  log "ERROR: LOOPRUNTIME_CORE_KUBECONFIG not set"
  exit 1
fi

# ArgoCD cluster registration (B6 fix in lib/argocd-register.sh).
export KUBECONFIG="$LOOPRUNTIME_CORE_KUBECONFIG"

if [[ -n "${LOOPRUNTIME_BUILD_KUBECONFIG:-}" ]]; then
  log "wiring build cluster..."
  SCOPED=$(KUBECONFIG="$LOOPRUNTIME_BUILD_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" build "$LOOPRUNTIME_BUILD_KUBECONFIG")
  KUBECONFIG="$LOOPRUNTIME_CORE_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" build "$SCOPED"
fi

# cell-1-activation-plan T1.3 / ADR C7: shared ingress-domain across cells.
# Default for production is dev.getloopscale.com (matches Pulumi infra). Override
# via LOOPRUNTIME_INGRESS_DOMAIN env var. Local k3d sets loopruntime.local via heal script.
INGRESS_DOMAIN_VAL="${LOOPRUNTIME_INGRESS_DOMAIN:-dev.getloopscale.com}"

# Issue #67: local k3d uses self-signed CAs that rotate across k3s container
# restarts (server CA in /var/lib/rancher/k3s/server/tls/server-ca.crt is
# regenerated). The signal for k3d-local is INGRESS_DOMAIN=loopruntime.local (set
# explicitly by the local heal script and never used in prod/dev). When local,
# render the CR with spec.tls.insecure=true so the ArgoCD cluster Secret
# doesn't bind to a CA that becomes stale. Prod (DOKS, Droplet) keeps
# insecure=false to enforce TLS verification with the kubeconfig's CA.
TLS_INSECURE_VAL="false"
if [[ "$INGRESS_DOMAIN_VAL" == "loopruntime.local" ]]; then
  TLS_INSECURE_VAL="true"
  log "k3d-local detected (ingress-domain=loopruntime.local) — rendering ClusterCredential CRs with spec.tls.insecure=true (issue #67)"
fi

if [[ -n "${LOOPRUNTIME_CELL_0_KUBECONFIG:-}" ]]; then
  log "wiring runtime cluster..."
  SCOPED=$(KUBECONFIG="$LOOPRUNTIME_CELL_0_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" runtime "$LOOPRUNTIME_CELL_0_KUBECONFIG")
  KUBECONFIG="$LOOPRUNTIME_CORE_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" runtime "$SCOPED"
  # V9-CLEANROOM-FRESH-FAANG-FIX 2026-05-31: REMOVED legacy
  # `clustercredential-create.sh cell-0 runtime` call. The ADR-0008
  # canonical path is `bootstrap-cell-cc.sh --cell 1 --env <env>` which
  # already runs as a Helmfile hook with CR name = cellID (not
  # clusterName=runtime). This redundant call created a SECOND
  # ClusterCredential CR named `runtime` pointing at the same apiServer
  # URL as `cell-0`, leading to two ArgoCD cluster Secrets with the same
  # server URL. ArgoCD application-controller registered duplicate
  # informers per server URL; on Watch invalidation the orphan informer
  # left the controller pod with cancelled watches and the apps stuck
  # status.sync="" (V9-L22-class behavior). LIVE-validated 2026-05-31
  # cleanroom-fresh dogfood after deletion of `runtime` and `runtime-2`
  # CRs + restart of argocd-application-controller statefulset.
fi

if [[ -n "${LOOPRUNTIME_RUNTIME2_KUBECONFIG:-}" ]]; then
  log "wiring runtime-2 cluster..."
  SCOPED=$(KUBECONFIG="$LOOPRUNTIME_RUNTIME2_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" runtime-2 "$LOOPRUNTIME_RUNTIME2_KUBECONFIG")
  KUBECONFIG="$LOOPRUNTIME_CORE_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" runtime-2 "$SCOPED"
  # V9-CLEANROOM-FRESH-FAANG-FIX 2026-05-31: REMOVED legacy
  # `clustercredential-create.sh cell-1 runtime-2` call. Same root cause
  # as runtime above — the ADR-0008 canonical path is
  # `bootstrap-cell-cc.sh --cell 1 --env <env>` which runs in the same
  # Helmfile hook chain with CR name = cell-1.
fi

# B11 fix — Gitea repos archived: re-activate any archived repos so tenant
# delete-then-recreate works without manual unarchive.
if [[ -n "${LOOPRUNTIME_GITEA_TOKEN:-}" && -n "${LOOPRUNTIME_GITEA_URL:-}" ]]; then
  log "checking for archived gitea repos..."
  ARCHIVED=$(curl -sf -H "Authorization: token $LOOPRUNTIME_GITEA_TOKEN" \
    "$LOOPRUNTIME_GITEA_URL/api/v1/repos/search?archived=true&limit=50" 2>/dev/null \
    | python3 -c 'import sys,json; data=json.load(sys.stdin); [print(r["full_name"]) for r in data.get("data",[])]' 2>/dev/null || echo "")
  for repo in $ARCHIVED; do
    log "unarchiving $repo (B11)"
    curl -sf -X PATCH -H "Authorization: token $LOOPRUNTIME_GITEA_TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"archived":false}' \
      "$LOOPRUNTIME_GITEA_URL/api/v1/repos/$repo" >/dev/null || log "WARN: failed to unarchive $repo"
  done
fi

log "cross-cluster wiring complete (env=$ENV)"
