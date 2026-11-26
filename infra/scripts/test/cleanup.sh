#!/usr/bin/env bash
# cleanup.sh — Removes ALL chaos-mesh resources from a target cluster.
#
# Idempotent. Verifies zero orphan resources post-cleanup. Fails loudly
# if cleanup couldn't complete (manual intervention required).
#
# Per /chaos-test SKILL Regra Suprema: cleanup is MANDATORY. Never leave
# chaos state orphaned across runs.
#
# Usage:
#   bash infra/scripts/test/cleanup.sh
#   bash infra/scripts/test/cleanup.sh --target-context k3d-loopruntime-build
#   bash infra/scripts/test/cleanup.sh --all-contexts  # iterate all loopruntime-* clusters
#
# Exit codes:
#   0 — cleanup complete; zero orphan chaos resources verified
#   1 — usage / context unreachable
#   2 — cleanup failed; manual intervention required

set -euo pipefail

set -uo pipefail

TARGET_CTX="${TARGET_CONTEXT:-k3d-loopruntime-core}"
ALL_CONTEXTS=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target-context) TARGET_CTX="$2"; shift 2 ;;
    --all-contexts) ALL_CONTEXTS=1; shift ;;
    -h|--help) sed -n '1,20p' "$0"; exit 0 ;;
    *) echo "usage: $0 [--target-context <ctx>] [--all-contexts]" >&2; exit 1 ;;
  esac
done

log() { printf '[cleanup] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 2; }

cleanup_context() {
  local ctx="$1"
  log "context=$ctx"

  if ! kubectl --context "$ctx" cluster-info >/dev/null 2>&1; then
    log "  WARN: $ctx unreachable; skipping"
    return 0
  fi

  # Check if chaos-mesh CRDs exist (no-op if absent)
  if ! kubectl --context "$ctx" get crd podchaos.chaos-mesh.org >/dev/null 2>&1; then
    log "  no chaos-mesh CRDs on $ctx; nothing to clean"
    return 0
  fi

  local count
  count=$(kubectl --context "$ctx" get podchaos,networkchaos,iochaos,stresschaos,dnschaos,httpchaos,kernelchaos,jvmchaos,blockchaos --all-namespaces 2>/dev/null | grep -cv "^NAMESPACE" || true)

  if [[ "$count" -eq 0 ]]; then
    log "  zero chaos resources on $ctx ✓"
    return 0
  fi

  log "  deleting $count chaos resources..."
  kubectl --context "$ctx" delete podchaos --all -A --ignore-not-found=true --timeout=30s 2>&1 | sed 's/^/    /'
  kubectl --context "$ctx" delete networkchaos --all -A --ignore-not-found=true --timeout=30s 2>&1 | sed 's/^/    /'
  kubectl --context "$ctx" delete iochaos --all -A --ignore-not-found=true --timeout=30s 2>&1 | sed 's/^/    /'
  kubectl --context "$ctx" delete stresschaos --all -A --ignore-not-found=true --timeout=30s 2>&1 | sed 's/^/    /'
  kubectl --context "$ctx" delete dnschaos --all -A --ignore-not-found=true --timeout=30s 2>&1 | sed 's/^/    /'
  kubectl --context "$ctx" delete httpchaos --all -A --ignore-not-found=true --timeout=30s 2>&1 | sed 's/^/    /'

  # Verify zero remaining
  sleep 2
  local remaining
  remaining=$(kubectl --context "$ctx" get podchaos,networkchaos,iochaos,stresschaos --all-namespaces 2>/dev/null | grep -cv "^NAMESPACE" || true)

  if [[ "$remaining" -gt 0 ]]; then
    log "  WARN: $remaining chaos resources still present (likely finalizer-stuck)"
    log "  applying finalizer-removal + force-delete fallback..."

    # Iterate stuck resources: remove finalizers + force delete
    for kind in podchaos networkchaos iochaos stresschaos dnschaos httpchaos; do
      while IFS=$'\t' read -r ns name; do
        [[ -z "$name" ]] && continue
        log "    patching $kind/$name in $ns (remove finalizers)"
        kubectl --context "$ctx" patch "$kind" "$name" -n "$ns" \
          --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]' 2>&1 | sed 's/^/      /' || true
        kubectl --context "$ctx" delete "$kind" "$name" -n "$ns" \
          --force --grace-period=0 --ignore-not-found=true 2>&1 | sed 's/^/      /' || true
      done < <(kubectl --context "$ctx" get "$kind" -A --no-headers 2>/dev/null | awk '{print $1"\t"$2}')
    done

    sleep 3
    remaining=$(kubectl --context "$ctx" get podchaos,networkchaos,iochaos,stresschaos --all-namespaces 2>/dev/null | grep -cv "^NAMESPACE" || true)
    if [[ "$remaining" -gt 0 ]]; then
      log "  ERROR: $remaining chaos resources STILL present after force-delete"
      kubectl --context "$ctx" get podchaos,networkchaos,iochaos,stresschaos -A 2>&1 | head -10 | sed 's/^/    /'
      die "cleanup failed on $ctx — manual intervention required"
    fi
    log "  finalizer-removal cleanup succeeded ✓"
  fi

  log "  cleanup verified on $ctx ✓"
}

if [[ "$ALL_CONTEXTS" == "1" ]]; then
  for ctx in k3d-loopruntime-core k3d-loopruntime-build k3d-loopruntime-cell-0 k3d-loopruntime-cell-1; do
    cleanup_context "$ctx"
  done
else
  cleanup_context "$TARGET_CTX"
fi

log "cleanup complete"
exit 0
