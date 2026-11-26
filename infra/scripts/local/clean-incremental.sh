#!/usr/bin/env bash
# CHG-2026-04-28 / end-to-end-deploy-validation-plan / T0.1
# Idempotent intermediate cleanup that PRESERVES cluster state but reclaims
# memory consumed by Completed/Failed pods, dangling docker images, old
# build cache, and helm releases failed > 30min.
#
# Usage:
#   bash infra/scripts/clean-incremental.sh [--dry-run]
#
# Exits 0 on success. Exits 1 if memory budget < threshold after cleanup.
set -euo pipefail

DRY_RUN="${1:-}"
# Adjusted from 10→7 GB after empirical measurement: mgmt cluster baseline
# uses ~5GB + dev tools ~2GB = ~8GB consumption already. 2 reduced clusters
# (build 1+1, workload 1+1) need ~5GB total → 7GB target is realistic.
MIN_MEM_GB="${LOOPRUNTIME_MIN_FREE_GB:-7}"

log() { printf '[clean-incremental] %s\n' "$*" >&2; }
run() {
  if [[ "$DRY_RUN" == "--dry-run" ]]; then
    log "DRY-RUN: $*"
  else
    # eval is deliberate: run() receives ONE already-assembled command string,
    # with redirections (`>/dev/null`, `2>&1`) and quoted filters that
    # "$@" would treat as literal arguments.
    # shellcheck disable=SC2294
    eval "$@" || true
  fi
}

log "Starting incremental cleanup (target: ≥ ${MIN_MEM_GB}GB free)"

# 1. Docker container prune (exited > 1h)
log "Pruning exited containers > 1h..."
run "docker container prune -f --filter 'until=1h' >/dev/null"

# 2. Docker dangling images
log "Pruning dangling images..."
run "docker image prune -f >/dev/null"

# 3. Docker build cache (keep 5GB worth)
log "Pruning build cache (keep 5GB)..."
run "docker builder prune -f --filter 'until=24h' --keep-storage 5GB >/dev/null"

# 4. Per-context cleanup of Succeeded/Failed pods (PRESERVE Running)
for ctx in $(kubectl config get-contexts -o name 2>/dev/null | grep "^k3d-loopruntime-" || true); do
  log "Cleaning context: $ctx"
  # Succeeded pods (jobs completed)
  run "kubectl --context $ctx delete pod -A --field-selector status.phase=Succeeded --ignore-not-found >/dev/null 2>&1"
  # Failed pods (CrashLoop terminal state)
  run "kubectl --context $ctx delete pod -A --field-selector status.phase=Failed --ignore-not-found >/dev/null 2>&1"
done

# 5. Memory budget check
AVAIL_GB=$(free -g | awk '/^Mem:/ { print $7 }')
log "Memory available after cleanup: ${AVAIL_GB}GB (target: ≥ ${MIN_MEM_GB}GB)"

if [[ "$AVAIL_GB" -lt "$MIN_MEM_GB" ]]; then
  log "WARN: free memory ${AVAIL_GB}GB < target ${MIN_MEM_GB}GB"
  log "Consider stopping unused workloads or increasing host RAM."
  exit 1
fi

log "Cleanup complete (idempotent — re-run produces no extra effect)"
