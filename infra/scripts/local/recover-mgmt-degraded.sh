#!/usr/bin/env bash
# recover-mgmt-degraded.sh — T0.2 of plan
# 2026-05-29-dashboard-public-integration-canonicalization
#
# WARNING: --force --grace-period=0 will drop in-flight requests on pods in
# normal Terminating state. Use only when apiserver shows
# `context deadline exceeded` or pods are stuck Terminating > 5 minutes.
# v1.1 EC-D1 documents this trade-off explicitly.
#
# Strategy:
#   1) For each pod in Terminating state for > GRACE_SECONDS, force-delete.
#   2) For each Deployment with .status.unavailableReplicas > 0, rollout restart.
#   3) Poll readiness for up to READY_TIMEOUT.
#
# All kubectl calls use --request-timeout=10s (v1.1 EC-03) and retry with
# exponential backoff (3 tries: 5s/15s/30s). If 3 fail, abort with a clear
# message instructing the operator to restart the k3s agent OR reboot.
#
# Usage:
#   bash infra/scripts/local/recover-mgmt-degraded.sh [--namespace=NS] [--dry-run]
#
# Exit codes:
#   0  recovery succeeded OR nothing to recover (idempotent no-op)
#   1  apiserver unreachable after 3 retries
#   3  pods still not Ready after READY_TIMEOUT
set -euo pipefail

CONTEXT="${CONTEXT:-k3d-loop-runtime-core}"
NAMESPACE="${NAMESPACE:-loop-runtime-system}"
DRY_RUN="${DRY_RUN:-0}"
GRACE_SECONDS="${GRACE_SECONDS:-300}"
READY_TIMEOUT="${READY_TIMEOUT:-300}"

while [ $# -gt 0 ]; do
  case "$1" in
    --namespace=*) NAMESPACE="${1#*=}"; shift ;;
    --context=*)   CONTEXT="${1#*=}"; shift ;;
    --dry-run)     DRY_RUN=1; shift ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

log() { printf '[%(%H:%M:%S)T] %s\n' -1 "$*" >&2; }

# v1.1 EC-03: every kubectl call goes through this wrapper with timeout + backoff
kc_retry() {
  local args=("$@")
  # Override via env for hermetic tests (KC_RETRY_DELAYS="1 2 3" for fast retries)
  local delays_str="${KC_RETRY_DELAYS:-5 15 30}"
  read -ra delays <<< "$delays_str"
  local attempt=0
  while [ "$attempt" -le "${#delays[@]}" ]; do
    if kubectl --context="$CONTEXT" --request-timeout=10s "${args[@]}" 2>&1; then
      return 0
    fi
    if [ "$attempt" -eq "${#delays[@]}" ]; then
      log "kubectl ${args[*]} FAILED after 3 retries"
      log ""
      log "Apiserver unreachable. Try one of:"
      log "  1. docker restart k3d-loop-runtime-core-server-0   # restart k3s control-plane"
      log "  2. k3d cluster start loop-runtime-core              # restart all k3d nodes"
      log "  3. Reboot the droplet                       # nuclear option"
      return 1
    fi
    local delay="${delays[$attempt]}"
    log "kubectl ${args[*]} failed; retrying in ${delay}s (attempt $((attempt+2))/$((${#delays[@]}+1)))"
    sleep "$delay"
    attempt=$((attempt + 1))
  done
}

log "=== recover-mgmt-degraded: ns=$NAMESPACE context=$CONTEXT dry-run=$DRY_RUN ==="

# 1) Find stuck Terminating pods
log "Step 1: scanning for pods stuck Terminating > ${GRACE_SECONDS}s"
# v1.1 EC-03: if apiserver unreachable, kc_retry exhausts all retries and
# returns 1; we propagate that so the operator gets exit 1 + the operator hint.
if ! POD_JSON=$(kc_retry get pods -n "$NAMESPACE" -o json); then
  log "FATAL: cannot list pods after retries"
  exit 1
fi
STUCK_PODS=$(printf '%s' "$POD_JSON" | python3 -c "
import json, sys, datetime
now = datetime.datetime.now(datetime.timezone.utc)
try:
    pods = json.load(sys.stdin).get('items', [])
except Exception:
    sys.exit(0)
for p in pods:
    ts = p.get('metadata', {}).get('deletionTimestamp')
    if not ts:
        continue
    dt = datetime.datetime.fromisoformat(ts.replace('Z', '+00:00'))
    age = (now - dt).total_seconds()
    if age > $GRACE_SECONDS:
        print(p['metadata']['name'])
" 2>/dev/null || echo "")

if [ -z "$STUCK_PODS" ]; then
  log "  none found"
else
  for pod in $STUCK_PODS; do
    log "  force-deleting stuck Terminating pod: $pod"
    if [ "$DRY_RUN" = "1" ]; then
      log "  [dry-run] kubectl delete pod $pod --grace-period=0 --force"
    else
      kc_retry delete pod "$pod" -n "$NAMESPACE" --grace-period=0 --force --ignore-not-found || true
    fi
  done
fi

# 2) Find degraded Deployments
log "Step 2: scanning for Deployments with unavailableReplicas > 0"
DEGRADED_DEPLOYS=$(kc_retry get deploy -n "$NAMESPACE" \
  -o json | python3 -c "
import json, sys
deps = json.load(sys.stdin).get('items', [])
for d in deps:
    name = d['metadata']['name']
    unavailable = d.get('status', {}).get('unavailableReplicas', 0)
    if unavailable and unavailable > 0:
        print(name)
" 2>/dev/null || true)

if [ -z "$DEGRADED_DEPLOYS" ]; then
  log "  none found"
else
  for dep in $DEGRADED_DEPLOYS; do
    log "  rollout restart: deploy/$dep"
    if [ "$DRY_RUN" = "1" ]; then
      log "  [dry-run] kubectl rollout restart deploy/$dep"
    else
      kc_retry rollout restart -n "$NAMESPACE" "deploy/$dep" || true
    fi
  done
fi

# 3) Wait for readiness
if [ "$DRY_RUN" = "1" ]; then
  log "[dry-run] skipping readiness wait"
  exit 0
fi

if [ -z "$STUCK_PODS" ] && [ -z "$DEGRADED_DEPLOYS" ]; then
  log "Step 3: nothing was acted on; cluster appears healthy"
  exit 0
fi

log "Step 3: polling readiness for up to ${READY_TIMEOUT}s"
DEADLINE=$(( $(date +%s) + READY_TIMEOUT ))
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  NOT_READY=$(kc_retry get pods -n "$NAMESPACE" -o json | python3 -c "
import json, sys
pods = json.load(sys.stdin).get('items', [])
not_ready = 0
for p in pods:
    if p.get('metadata', {}).get('deletionTimestamp'):
        continue
    statuses = p.get('status', {}).get('containerStatuses', [])
    for c in statuses:
        if not c.get('ready'):
            not_ready += 1
            break
print(not_ready)
" 2>/dev/null || echo "999")

  if [ "$NOT_READY" = "0" ]; then
    log "  all pods Ready"
    exit 0
  fi
  log "  $NOT_READY pods still not Ready; sleeping 10s"
  sleep 10
done

log "TIMEOUT: pods still not Ready after ${READY_TIMEOUT}s"
log "Inspect with: kubectl --context=$CONTEXT -n $NAMESPACE get pods"
exit 3
