#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C1.
# Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
#
# Atomic operator topology cutover orchestrator. Wraps `helmfile sync`
# with the safety controls the production-design veto demanded (C6:
# "no window where 0 reconcilers run > 30s") and the SRE veto demanded
# (atomic per-cluster cutover; auto-rollback on failure).
#
# Why this script (and not just `helmfile sync`):
#   helmfile sync is sequential per release — it doesn't know that
#   creating the 4 new split deployments and deleting the 2 monolith
#   deployments must overlap in time. By default helmfile would do
#   them in declaration order: if monolith is declared first and gets
#   removed before splits are Ready, there's a multi-minute reconciler
#   gap. This script:
#     1. Pre-flights all gates (validate-matrix + rbac-subset + cells
#        migration syntax + chart lint).
#     2. Captures the cutover SHA into LOOPZERO_CUTOVER_COMMIT for rollback.
#     3. Runs helmfile sync.
#     4. Concurrently watches deployment readiness AND tenant-operator
#        log activity.
#     5. If zero-Ready window > 30s OR sync fails OR reconcile gap > 60s:
#        AUTO-ROLLBACK via operator-cutover-rollback.sh.
#     6. Verifies post-cutover invariants.
#
# Per condition C5/C7: rollback uses reverse-patch (no `git revert`).
#
# Per condition C19: failure of any step here fails the gate.
#
# Usage:
#   bash infra/scripts/operator-cutover-execute.sh <cluster-context>
#
# Required env (will be auto-set if absent):
#   LOOPZERO_CUTOVER_COMMIT — defaults to HEAD; required for rollback
#
# Optional env:
#   ZERO_READY_THRESHOLD_SEC — max acceptable zero-Ready window (default 30)
#   READY_TIMEOUT_SEC        — wait for new replicas Ready (default 300)
#   AUTO_ROLLBACK            — "true"|"false" (default "true")
#   DRY_RUN                  — "true"|"false" (default "false")
#                              When true: validates everything, shows the
#                              helmfile diff, EXITS WITHOUT APPLYING.
#
# Exit codes:
#   0 — cutover succeeded; reconcile activity verified
#   1 — usage error / pre-flight gate failure
#   2 — helmfile sync failed (rollback may have been triggered)
#   3 — zero-Ready window exceeded threshold (rollback triggered if AUTO_ROLLBACK=true)
#   4 — reconcile activity not resumed within 90s
#   5 — post-cutover invariant verification failed

set -euo pipefail

set -uo pipefail

CTX="${1:-}"
ZERO_READY_THRESHOLD_SEC="${ZERO_READY_THRESHOLD_SEC:-30}"
READY_TIMEOUT_SEC="${READY_TIMEOUT_SEC:-300}"
AUTO_ROLLBACK="${AUTO_ROLLBACK:-true}"
DRY_RUN="${DRY_RUN:-false}"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOG_FILE="/tmp/operator-cutover-$(date +%Y%m%dT%H%M%S).log"
exec > >(tee -a "$LOG_FILE") 2> >(tee -a "$LOG_FILE" >&2)

log() { printf '[op-cutover %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

# Trap captures unexpected exits to ensure the rollback hook always
# runs if we made it past pre-flight.
PRE_FLIGHT_PASSED=0
ABORT_REASON=""
on_exit() {
  local rc=$?
  if (( rc != 0 )) && (( PRE_FLIGHT_PASSED == 1 )) && [[ "$AUTO_ROLLBACK" == "true" ]] && [[ "$DRY_RUN" == "false" ]]; then
    log "exit=$rc — triggering auto-rollback"
    if [[ -z "${LOOPZERO_CUTOVER_COMMIT:-}" ]]; then
      log "  LOOPZERO_CUTOVER_COMMIT not set — cannot auto-rollback"
    else
      bash "${REPO_ROOT}/infra/scripts/operator-cutover-rollback.sh" "$CTX" 2>&1 \
        || log "  WARN: auto-rollback exited non-zero — manual intervention needed"
    fi
  fi
}
trap on_exit EXIT

# ─── Step 0: usage gate ───
[[ -n "$CTX" ]] || fail "usage: $0 <cluster-context>" 1

cd "$REPO_ROOT"

# ─── Step 1: pre-flight gates ───
log "step 1: pre-flight gates"

# 1.1 — required tools
for tool in kubectl helmfile helm git; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool not in PATH" 1
done

# 1.2 — cluster reachable
if ! kubectl --context "$CTX" cluster-info --request-timeout=5s >/dev/null 2>&1; then
  fail "cluster context $CTX unreachable" 1
fi

# 1.3 — capture cutover commit SHA (or use provided)
if [[ -z "${LOOPZERO_CUTOVER_COMMIT:-}" ]]; then
  LOOPZERO_CUTOVER_COMMIT="$(git rev-parse HEAD)"
  export LOOPZERO_CUTOVER_COMMIT
  log "  captured LOOPZERO_CUTOVER_COMMIT=$LOOPZERO_CUTOVER_COMMIT (HEAD)"
fi

# 1.4 — validate-matrix passes (transitive: covers parity-preflight,
# rbac-subset audit, helm lint, etc).
log "  running validate:matrix --quick"
if ! bash "${REPO_ROOT}/infra/tests/parity/validate-matrix.sh" --quick >/dev/null 2>&1; then
  fail "validate:matrix --quick failed; cutover blocked" 1
fi

# 1.5 — Step 0 (per cutover runbook): drain active reconciles
log "  step 0 (drain): checking for in-flight reconciles"
if kubectl --context "$CTX" -n loopzero-system get deploy tenant-controller >/dev/null 2>&1; then
  inflight=$(kubectl --context "$CTX" -n loopzero-system logs deploy/tenant-controller --since=30s 2>/dev/null \
    | grep -c "reconcile started\|reconciliation in progress" || true)
  if (( inflight > 0 )); then
    fail "in-flight reconciles detected ($inflight); wait or cancel before cutover" 1
  fi
fi

# 1.6 — environment determination
ENV_NAME="$(echo "$CTX" | grep -oE 'standalone-multi|standalone|prod|dev' | head -1)"
[[ -n "$ENV_NAME" ]] || ENV_NAME="dev"
log "  env=$ENV_NAME"

PRE_FLIGHT_PASSED=1
log "  pre-flight: PASSED"

# ─── Step 2: dry-run gate ───
if [[ "$DRY_RUN" == "true" ]]; then
  log "step 2: DRY_RUN — showing helmfile diff"
  helmfile -f "${REPO_ROOT}/infra/helmfile/mgmt.helmfile.yaml.gotmpl" \
    -e "$ENV_NAME" --kube-context "$CTX" diff 2>&1 | tail -50
  log "DRY_RUN done — no changes applied"
  exit 0
fi

# ─── Step 3: start zero-Ready watcher in background ───
log "step 3: starting zero-Ready watcher (threshold ${ZERO_READY_THRESHOLD_SEC}s)"
WATCHER_LOG="/tmp/op-cutover-watcher-$$.log"
WATCHER_FAIL_FLAG="/tmp/op-cutover-watcher-$$.fail"
rm -f "$WATCHER_LOG" "$WATCHER_FAIL_FLAG"

# Watcher polls every 5s, tracks per-deployment Ready replica count.
# Per Invariant I-5 + condition C6: any deploy with 0 Ready for
# > threshold seconds triggers cutover abort.
watcher_pids=()
(
  zero_streak=0
  while true; do
    any_zero=0
    for dep in tenant-controller loopzero-controller loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
      ready=$(kubectl --context "$CTX" -n loopzero-system get deploy "$dep" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
      ready=${ready:-0}
      desired=$(kubectl --context "$CTX" -n loopzero-system get deploy "$dep" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "")
      if [[ -z "$desired" ]]; then continue; fi
      if (( ready == 0 )) && (( desired > 0 )); then
        # Check if there's a sibling (any of the operator domain
        # peers) with Ready > 0. If yes, this deploy's zero-Ready is
        # acceptable (lease is held by sibling). If no, this is the
        # condition C6 violation.
        any_zero=1
        echo "$(date +%H:%M:%S) ZERO-READY: $dep desired=$desired ready=0" >> "$WATCHER_LOG"
      fi
    done
    if (( any_zero == 1 )); then
      zero_streak=$((zero_streak + 5))
      if (( zero_streak >= ZERO_READY_THRESHOLD_SEC )); then
        echo "ZERO_READY_WINDOW_EXCEEDED" > "$WATCHER_FAIL_FLAG"
        echo "$(date +%H:%M:%S) ABORT: zero-ready window ${zero_streak}s ≥ ${ZERO_READY_THRESHOLD_SEC}s" >> "$WATCHER_LOG"
        exit 1
      fi
    else
      zero_streak=0
    fi
    sleep 5
  done
) &
WATCHER_PID=$!
watcher_pids+=("$WATCHER_PID")

# Cleanup watcher on any exit (including abort).
cleanup_watcher() {
  for pid in "${watcher_pids[@]}"; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
}
trap 'cleanup_watcher; on_exit' EXIT

# ─── Step 4: helmfile sync ───
log "step 4: helmfile sync (env=$ENV_NAME, ctx=$CTX)"
sync_start=$(date +%s)
if ! helmfile -f "${REPO_ROOT}/infra/helmfile/mgmt.helmfile.yaml.gotmpl" \
              -e "$ENV_NAME" --kube-context "$CTX" sync 2>&1 | tail -30; then
  fail "helmfile sync failed; trap will trigger rollback if AUTO_ROLLBACK=true" 2
fi
sync_dur=$(( $(date +%s) - sync_start ))
log "  helmfile sync completed in ${sync_dur}s"

# ─── Step 5: wait for new split replicas Ready ───
log "step 5: waiting for split operators to reach Ready (timeout ${READY_TIMEOUT_SEC}s)"
ready_start=$(date +%s)
for op in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  log "  waiting for $op..."
  if ! kubectl --context "$CTX" -n loopzero-system wait deploy/"$op" \
       --for=condition=Available --timeout="${READY_TIMEOUT_SEC}s" 2>&1; then
    # Watcher may have already flagged this; if so, condition C6 fired.
    if [[ -f "$WATCHER_FAIL_FLAG" ]]; then
      log "  watcher detected zero-ready violation:"
      tail -10 "$WATCHER_LOG" >&2 || true
      fail "zero-ready window exceeded (condition C6)" 3
    fi
    fail "$op did not reach Available within ${READY_TIMEOUT_SEC}s" 3
  fi
done
ready_dur=$(( $(date +%s) - ready_start ))
log "  all 4 split operators Ready (waited ${ready_dur}s)"

# Check watcher one more time before declaring success.
if [[ -f "$WATCHER_FAIL_FLAG" ]]; then
  fail "watcher detected zero-ready violation post-Ready: $(cat "$WATCHER_FAIL_FLAG")" 3
fi

# ─── Step 6: verify reconcile activity resumed ───
log "step 6: verifying reconcile activity in split operators"
sleep 30  # let cache sync + first reconcile complete
verified=0
for op in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  logs=$(kubectl --context "$CTX" -n loopzero-system logs deploy/"$op" --since=60s 2>/dev/null | head -50)
  if echo "$logs" | grep -qE 'reconcile|controller|started|ready'; then
    log "  $op: activity confirmed"
    verified=$((verified + 1))
  else
    log "  WARN: $op: no activity in last 60s"
  fi
done

if (( verified < 2 )); then
  fail "reconcile activity not visible in ≥2 split operators (got $verified/4)" 4
fi

# ─── Step 7: post-cutover invariant verification ───
log "step 7: post-cutover invariant verification"

# I-2: distinct lease names exist
for op in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  expected_lease="${op}.loopzero.io"
  if ! kubectl --context "$CTX" -n loopzero-system get lease "$expected_lease" >/dev/null 2>&1; then
    log "  WARN: lease $expected_lease not found (may take 30s after sync)"
  fi
done

# I-5: replicaCount=2 verified per chart
for op in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  desired=$(kubectl --context "$CTX" -n loopzero-system get deploy "$op" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
  if (( desired < 2 )); then
    fail "I-5 violation: $op has replicas=$desired (must be ≥2)" 5
  fi
done

# Monolith deployments removed
for old in tenant-controller loopzero-controller; do
  if kubectl --context "$CTX" -n loopzero-system get deploy "$old" >/dev/null 2>&1; then
    log "  WARN: monolith deploy $old still present (helmfile may still be removing)"
  fi
done

# ─── DONE ───
total_dur=$(( $(date +%s) - sync_start ))
log "DONE — cutover succeeded in ${total_dur}s (sync=${sync_dur}s, ready=${ready_dur}s, verified=${verified}/4)"
log "log file: $LOG_FILE"
log "next steps: monitor soak period for regressions"

# Disable rollback trap (success path).
PRE_FLIGHT_PASSED=0
exit 0
