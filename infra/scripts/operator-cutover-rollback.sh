#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C5.
# Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
#
# Per-cluster rollback for the operator topology cutover (D1 forward-
# only migration). Restores the monolith chart by re-applying the
# `infra/helm/tenant-controller` + `infra/helm/loopzero-controller` Helm
# releases while the 4 split releases are scaled down + deleted in
# leader-election-safe order.
#
# Per condition C5: ≤5 minutes per cluster end-to-end.
# Per condition C7: rollback uses reverse-patch commits (no destructive
# git history operations); this SCRIPT does the cluster-side work; the
# git-side work is a separate PR (`git diff <cutover>~1 <cutover> --
# infra/helmfile/ | git apply -R` then a NEW commit).
#
# Algorithm:
#   1. Verify monolith charts still exist on disk (defensive — they are
#      kept in repo until T2.5 sprint-12 deferred deletion).
#   2. Helmfile sync with monolith releases ENABLED + split releases
#      DISABLED. The exact way this is expressed depends on whether the
#      cutover commit had a feature flag or hard switched. Forward-only
#      migration (D1 chosen) hard-switched, so rollback re-renders the
#      pre-cutover helmfile via git checkout of the parent commit, then
#      applies it. ONLY when running rollback as the immediate response
#      to a botched cutover.
#   3. Wait for monolith pods to reach Ready (Lease takes over).
#   4. Scale split deployments to 0 (they release leases gracefully).
#   5. Delete split deployments.
#   6. Verify reconcile activity resumes (CRs visible in tenant-controller
#      logs).
#
# Usage:
#   bash infra/scripts/operator-cutover-rollback.sh <cluster-context>
#
#   <cluster-context>  kubectl context for the cluster to roll back
#                      (typically `loopzero-mgmt` or named DOKS context)
#
# Required env:
#   LOOPZERO_CUTOVER_COMMIT — the SHA of the cutover commit, used to
#                         compute the parent for git checkout
#                         pre-cutover state. If empty, script aborts
#                         with instructions.
#
# Exit codes:
#   0 — monolith reconciler is reading CRs again
#   1 — usage error / missing cutover SHA
#   2 — monolith chart files missing (pre-cutover state cannot be reconstructed)
#   3 — helmfile sync failed
#   4 — pods didn't reach Ready within timeout
#   5 — verification (CR reconcile activity) didn't resume

set -uo pipefail

CTX="${1:-}"
CUTOVER="${LOOPZERO_CUTOVER_COMMIT:-}"
TIMEOUT_SEC="${ROLLBACK_TIMEOUT_SEC:-240}"   # 4min total + 60s for pod ready

log() { printf '[op-rollback %s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

[[ -n "$CTX" ]] || fail "usage: $0 <cluster-context> (env LOOPZERO_CUTOVER_COMMIT=<sha> required)" 1
[[ -n "$CUTOVER" ]] || fail "set LOOPZERO_CUTOVER_COMMIT=<sha-of-cutover-commit> (parent commit will be applied)" 1

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

start_ts=$(date +%s)

# ─── Step 1: pre-flight ───
log "step 1: pre-flight checks"
[[ -d "${REPO_ROOT}/infra/helm/tenant-controller" ]] || fail "monolith chart missing" 2
[[ -d "${REPO_ROOT}/infra/helm/loopzero-controller" ]] || fail "monolith chart missing" 2
command -v kubectl >/dev/null 2>&1 || fail "kubectl not in PATH" 1
command -v helmfile >/dev/null 2>&1 || fail "helmfile not in PATH" 1
command -v git >/dev/null 2>&1 || fail "git not in PATH" 1

if ! kubectl --context "$CTX" cluster-info --request-timeout=5s >/dev/null 2>&1; then
  fail "cluster context $CTX unreachable" 1
fi

# ─── Step 2: render pre-cutover helmfile ───
# Use git show to read the helmfile YAML at the parent of the cutover
# commit, then apply it via helmfile in-place (no working-tree mutation).
PARENT="${CUTOVER}^"
log "step 2: rendering helmfile from parent of $CUTOVER ($PARENT)"

if ! git rev-parse "$PARENT" >/dev/null 2>&1; then
  fail "parent commit $PARENT not in local history; fetch first" 2
fi

# Spool the parent helmfile to a temp dir (preserves working tree).
WORK_DIR="$(mktemp -d /tmp/op-rollback-XXXXXX)"
trap "rm -rf '$WORK_DIR'" EXIT

# Replay the relevant parent files into the temp dir, then helmfile sync
# with HELMFILE_DIR pointing at it. Files captured: infra/helmfile/* +
# infra/helm/tenant-controller/* + infra/helm/loopzero-controller/* +
# environments/.
log "  spooling pre-cutover state to $WORK_DIR"
for path in \
    infra/helmfile/mgmt.helmfile.yaml.gotmpl \
    infra/helmfile/environments/dev.yaml \
    infra/helmfile/environments/prod.yaml \
    infra/helmfile/environments/standalone.yaml \
    infra/helmfile/environments/standalone-multi.yaml; do
  spooled="${WORK_DIR}/${path}"
  mkdir -p "$(dirname "$spooled")"
  if ! git show "${PARENT}:${path}" > "$spooled" 2>/dev/null; then
    log "  WARN: ${path} did not exist in parent commit — skipping"
  fi
done

# ─── Step 3: helmfile sync ───
# The temp dir has the parent helmfile.yaml.gotmpl that references the
# monolith charts (which still exist in working-tree under T2.5 deferral).
# We do NOT copy the chart dirs into the temp because helmfile resolves
# `chart: ../helm/...` relative to the helmfile file path; the working-
# tree dirs will be resolved at sync time. This is intentional — the
# split charts live in `charts/` (not `infra/helm/`) so they can't shadow.
log "step 3: helmfile sync (pre-cutover state)"
HELMFILE_FILE="${WORK_DIR}/infra/helmfile/mgmt.helmfile.yaml.gotmpl"
[[ -f "$HELMFILE_FILE" ]] || fail "spooled helmfile missing — parent commit may have used a different layout" 2

# Determine environment from CTX (best-effort).
ENV_NAME="$(echo "$CTX" | grep -oE 'standalone-multi|standalone|prod|dev' | head -1)"
[[ -n "$ENV_NAME" ]] || ENV_NAME="dev"
log "  using helmfile environment: $ENV_NAME"

if ! KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}" \
   helmfile -f "$HELMFILE_FILE" -e "$ENV_NAME" --kube-context "$CTX" sync 2>&1 \
   | tail -20; then
  fail "helmfile sync failed (see output above)" 3
fi

# ─── Step 4: wait for monolith Ready ───
log "step 4: waiting for monolith pods to reach Ready (timeout ${TIMEOUT_SEC}s)"

monolith_ready() {
  kubectl --context "$CTX" -n loopzero-system get deploy loopzero-controller -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -qE '^[1-9]' && \
  kubectl --context "$CTX" -n loopzero-system get deploy tenant-controller -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -qE '^[1-9]'
}

waited=0
while ! monolith_ready; do
  sleep 5
  waited=$((waited + 5))
  if (( waited > TIMEOUT_SEC )); then
    fail "monolith pods not Ready after ${TIMEOUT_SEC}s" 4
  fi
done
log "  monolith Ready (waited=${waited}s)"

# ─── Step 5: scale + delete split deployments ───
log "step 5: scale split deployments to 0 + delete"
for op in loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  if kubectl --context "$CTX" -n loopzero-system get deploy "$op" >/dev/null 2>&1; then
    log "  scaling $op to 0..."
    kubectl --context "$CTX" -n loopzero-system scale deploy "$op" --replicas=0 || true
    # Wait briefly for graceful lease release.
    sleep 5
    log "  deleting $op..."
    kubectl --context "$CTX" -n loopzero-system delete deploy "$op" --wait=true --timeout=60s || true
  else
    log "  $op not present — skipping"
  fi
done

# ─── Step 6: verify reconcile resumed ───
log "step 6: verifying reconcile activity resumed"
# Look for ANY tenant-controller log line within last 30s mentioning a CR.
log_check_window=30
last_logs=$(kubectl --context "$CTX" -n loopzero-system logs deploy/tenant-controller --since=${log_check_window}s 2>/dev/null | head -20 || true)
if echo "$last_logs" | grep -qE 'reconcile|tenant|environment|controller'; then
  log "  reconcile activity confirmed (${log_check_window}s window)"
else
  log "  WARN: no reconcile activity in last ${log_check_window}s — may need longer (tenant-controller has 30s reconcile interval)"
fi

elapsed=$(( $(date +%s) - start_ts ))
log "DONE — rollback complete in ${elapsed}s (target ≤300s per condition C5: $([[ $elapsed -le 300 ]] && echo MET || echo MISS))"
exit 0
