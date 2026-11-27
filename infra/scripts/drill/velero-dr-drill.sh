#!/usr/bin/env bash
# infra/scripts/drill/velero-dr-drill.sh
#
# velero-backup-dr-plan v1.0 / T4.2 — DR drill validation script.
#
# Validates the end-to-end Velero backup -> restore loop without ever
# deleting real CRDs. The drill creates a SCRATCH `Tenant` CR named
# `drill-test`, takes a backup, deletes only that CR, restores from the
# backup, and verifies it returns. Then cleans up the scratch CR.
#
# Invariants:
#   - The drill MUST NOT touch any real Tenant (only `drill-test`).
#   - The drill is idempotent — re-running on a dirty workspace cleans
#     stale `drill-test` artifacts before starting.
#   - Total drill timeout: 5 minutes.
#   - --dry-run mode performs only read-only checks (BSL status, list
#     backups). Useful in CI freshness checks where mutations are unsafe.
#
# Exit codes:
#   0  — PASS (all 9 steps green)
#   1  — FAIL (any step failed; JSON report on stdout details which)
#   2  — usage error / pre-conditions not met
#
# Output: JSON report to stdout, mirroring CI report conventions.
#
# EC-5 fix: drill-test Tenant has a complete spec so the TenantController
# does not crash if it reconciles before deletion. annotation
# `runtime.getloopscale.com/drill: "true"` marks the resource for cleanup automation.

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

DRILL_TIMEOUT_SECS="${DRILL_TIMEOUT_SECS:-300}"
KUBE_CONTEXT="${KUBE_CONTEXT:-k3d-loop-runtime-core}"
DRY_RUN=false
DRILL_TENANT_NAME="drill-test"
DRILL_TENANT_NAMESPACE="loop-runtime-system"
DRILL_BACKUP_NAME=""
DRILL_RESTORE_NAME=""
START_TS="$(date -u +%s)"
PHASES_FILE="$(mktemp -t velero-drill-phases.XXXXXX)"
trap 'rm -f "$PHASES_FILE"' EXIT

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

log() { printf '[velero-dr-drill] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; emit_report "failed"; exit 1; }

emit_phase() {
  local name="$1" status="$2" detail="${3:-}"
  printf '%s\n' "{\"phase\":\"${name}\",\"status\":\"${status}\",\"detail\":$(printf '%s' "${detail}" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))')}" >> "$PHASES_FILE"
}

emit_report() {
  local overall="$1"
  local end_ts
  end_ts="$(date -u +%s)"
  local duration=$(( end_ts - START_TS ))
  local phases_json
  if [[ -s "$PHASES_FILE" ]]; then
    phases_json="[$(paste -sd, "$PHASES_FILE")]"
  else
    phases_json="[]"
  fi
  printf '{"status":"%s","duration_seconds":%d,"context":"%s","dry_run":%s,"phases":%s}\n' \
    "$overall" "$duration" "$KUBE_CONTEXT" "$DRY_RUN" "$phases_json"
}

require() {
  command -v "$1" >/dev/null 2>&1 || { log "missing command: $1"; exit 2; }
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    --context)
      KUBE_CONTEXT="$2"
      shift 2
      ;;
    --timeout)
      DRILL_TIMEOUT_SECS="$2"
      shift 2
      ;;
    -h|--help)
      cat <<EOF
Usage: $0 [--dry-run] [--context <kube-context>] [--timeout <secs>]

Validates the Velero DR loop end-to-end. Default mode:
  - Creates a scratch 'drill-test' Tenant
  - Takes a backup
  - Deletes only the scratch CR
  - Restores from the backup
  - Verifies the CR returned
  - Cleans up

--dry-run skips mutations (only verifies BSL status + lists backups).
EOF
      exit 0
      ;;
    *)
      log "unknown argument: $1"
      exit 2
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Pre-conditions
# ---------------------------------------------------------------------------

require kubectl
require velero
require date
require python3

KUBECTL="kubectl --context $KUBE_CONTEXT"
VELERO="velero --kubecontext $KUBE_CONTEXT --namespace velero-system"

# ---------------------------------------------------------------------------
# Phase 1 — BSL status must be Available
# ---------------------------------------------------------------------------

phase_bsl_status() {
  local out
  if ! out=$($VELERO backup-location get -o json 2>&1); then
    emit_phase bsl_status fail "$out"
    return 1
  fi
  # `velero backup-location get -o json` returns a single object when one
  # BSL exists, or a `kind: List` envelope when multiple. Accept both.
  local phase
  phase=$(printf '%s' "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
items = d.get("items") if isinstance(d, dict) and d.get("kind") == "List" else [d]
print(items[0].get("status", {}).get("phase", "Unknown"))
' 2>&1)
  if [[ "$phase" != "Available" ]]; then
    emit_phase bsl_status fail "BSL phase=$phase (expected Available)"
    return 1
  fi
  emit_phase bsl_status pass "BSL phase=Available"
}

# ---------------------------------------------------------------------------
# Phase 2 — At least one schedule has run successfully
# ---------------------------------------------------------------------------

phase_schedules_have_runs() {
  local out
  if ! out=$($VELERO backup get --selector "velero.io/schedule-name=loop-runtime-crds-backup" -o json 2>&1); then
    emit_phase schedules_have_runs fail "$out"
    return 1
  fi
  local count
  count=$(printf '%s' "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
if isinstance(d, dict) and d.get("kind") == "List":
    items = d.get("items", [])
elif isinstance(d, dict) and d.get("metadata"):
    items = [d]
else:
    items = []
print(len([b for b in items if b.get("status", {}).get("phase") == "Completed"]))
' 2>&1)
  if [[ "${count:-0}" -lt 1 ]]; then
    # In a fresh install no scheduled run has happened yet — this is OK
    # in dry-run mode. In live mode it's a soft warning, not a fail.
    emit_phase schedules_have_runs warn "no completed loop-runtime-crds-backup runs yet (count=${count:-0})"
    return 0
  fi
  emit_phase schedules_have_runs pass "completed loop-runtime-crds-backup runs=$count"
}

# ---------------------------------------------------------------------------
# Phase 3 — Pre-cleanup any stale drill artifacts
# ---------------------------------------------------------------------------

phase_pre_cleanup() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase pre_cleanup skipped "dry-run mode"
    return 0
  fi
  # The Tenant CRD has a finalizer (`runtime.getloopscale.com/tenant-cleanup`) that blocks
  # delete until the controller finishes namespace teardown. Patching the
  # finalizer list to empty before delete avoids 30s waits on stale
  # drill-test resources from prior failed runs. This is safe because the
  # drill tenant is purely synthetic — no real workload to clean up.
  $KUBECTL -n "$DRILL_TENANT_NAMESPACE" patch tenant "$DRILL_TENANT_NAME" \
    --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
  $KUBECTL -n "$DRILL_TENANT_NAMESPACE" delete tenant "$DRILL_TENANT_NAME" \
    --ignore-not-found --grace-period=0 --force >/dev/null 2>&1 || true
  emit_phase pre_cleanup pass "stale drill resources cleared"
}

# ---------------------------------------------------------------------------
# Phase 4 — Create scratch Tenant CR
# ---------------------------------------------------------------------------

phase_create_drill_tenant() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase create_drill_tenant skipped "dry-run mode"
    return 0
  fi
  # EC-5: spec must be valid so TenantController does not panic if it
  # reconciles between create and delete. annotation runtime.getloopscale.com/drill=true
  # marks resource for cleanup automation + audit forensics.
  local manifest
  # EC-5: spec must satisfy every required field of the Tenant CRD
  # (runtime.getloopscale.com/v1) so the TenantController does not panic if it
  # reconciles between create and delete. The fields below mirror
  # `pkg/naming` defaults and the seed Tenant in infra/tests/seed/seed.go.
  manifest=$(cat <<EOF
apiVersion: runtime.getloopscale.com/v1
kind: Tenant
metadata:
  name: $DRILL_TENANT_NAME
  namespace: $DRILL_TENANT_NAMESPACE
  annotations:
    runtime.getloopscale.com/drill: "true"
    runtime.getloopscale.com/drill-created-at: "$(date -u +%FT%TZ)"
spec:
  displayName: "Drill Test Tenant Velero DR"
  plan: starter
  cellID: cell-0
  environments:
    - cluster: workload
      name: production
  gitops:
    branch: main
    repoName: drill-test-gitops
  limits:
    maxBuildConcurrency: 1
    maxCPU: "1"
    maxMemory: 1Gi
    maxStorage: 1Gi
  preview:
    maxEnvironments: 1
    ttlHours: 1
EOF
)
  local apply_out
  apply_out=$(printf '%s\n' "$manifest" | $KUBECTL apply -f - 2>&1)
  local apply_rc=$?
  if [[ "$apply_rc" -ne 0 ]]; then
    emit_phase create_drill_tenant fail "kubectl apply failed: $apply_out"
    return 1
  fi
  emit_phase create_drill_tenant pass "drill-test Tenant created"
}

# ---------------------------------------------------------------------------
# Phase 5 — Take on-demand backup of CRDs
# ---------------------------------------------------------------------------

phase_take_backup() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase take_backup skipped "dry-run mode"
    return 0
  fi
  DRILL_BACKUP_NAME="drill-crds-$(date +%s)"
  if ! $VELERO backup create "$DRILL_BACKUP_NAME" \
       --include-resources tenants.runtime.getloopscale.com \
       --wait >/dev/null 2>&1; then
    emit_phase take_backup fail "velero backup create $DRILL_BACKUP_NAME failed"
    return 1
  fi
  emit_phase take_backup pass "$DRILL_BACKUP_NAME"
}

# ---------------------------------------------------------------------------
# Phase 6 — Simulate disaster: delete scratch Tenant
# ---------------------------------------------------------------------------

phase_simulate_disaster() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase simulate_disaster skipped "dry-run mode"
    return 0
  fi
  # Same finalizer-bypass pattern as pre_cleanup: drill-test is synthetic,
  # finalizer cleanup just slows the drill down with no platform value.
  $KUBECTL -n "$DRILL_TENANT_NAMESPACE" patch tenant "$DRILL_TENANT_NAME" \
    --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
  if ! $KUBECTL -n "$DRILL_TENANT_NAMESPACE" delete tenant "$DRILL_TENANT_NAME" \
       --grace-period=0 --force >/dev/null 2>&1; then
    emit_phase simulate_disaster fail "delete drill-test failed"
    return 1
  fi
  emit_phase simulate_disaster pass "drill-test deleted (simulated loss)"
}

# ---------------------------------------------------------------------------
# Phase 7 — Restore from backup
# ---------------------------------------------------------------------------

phase_restore() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase restore skipped "dry-run mode"
    return 0
  fi
  DRILL_RESTORE_NAME="drill-restore-$(date +%s)"
  if ! $VELERO restore create "$DRILL_RESTORE_NAME" \
       --from-backup "$DRILL_BACKUP_NAME" \
       --wait >/dev/null 2>&1; then
    emit_phase restore fail "velero restore create $DRILL_RESTORE_NAME failed"
    return 1
  fi
  emit_phase restore pass "$DRILL_RESTORE_NAME"
}

# ---------------------------------------------------------------------------
# Phase 8 — Verify Tenant restored
# ---------------------------------------------------------------------------

phase_verify_restored() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase verify_restored skipped "dry-run mode"
    return 0
  fi
  # Wait up to 30s for the restored CR to appear (controller may patch
  # status fields after Velero applies the spec).
  local i
  for i in $(seq 1 15); do
    if $KUBECTL -n "$DRILL_TENANT_NAMESPACE" get tenant "$DRILL_TENANT_NAME" >/dev/null 2>&1; then
      emit_phase verify_restored pass "drill-test Tenant present after restore"
      return 0
    fi
    sleep 2
  done
  emit_phase verify_restored fail "drill-test Tenant not found 30s after restore"
  return 1
}

# ---------------------------------------------------------------------------
# Phase 9 — Cleanup
# ---------------------------------------------------------------------------

phase_cleanup() {
  if [[ "$DRY_RUN" == "true" ]]; then
    emit_phase cleanup skipped "dry-run mode"
    return 0
  fi
  $KUBECTL -n "$DRILL_TENANT_NAMESPACE" patch tenant "$DRILL_TENANT_NAME" \
    --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
  $KUBECTL -n "$DRILL_TENANT_NAMESPACE" delete tenant "$DRILL_TENANT_NAME" \
    --ignore-not-found --grace-period=0 --force >/dev/null 2>&1 || true
  if [[ -n "$DRILL_BACKUP_NAME" ]]; then
    $VELERO backup delete "$DRILL_BACKUP_NAME" --confirm >/dev/null 2>&1 || true
  fi
  if [[ -n "$DRILL_RESTORE_NAME" ]]; then
    $VELERO restore delete "$DRILL_RESTORE_NAME" --confirm >/dev/null 2>&1 || true
  fi
  emit_phase cleanup pass "drill artifacts cleaned"
}

# ---------------------------------------------------------------------------
# Run with timeout
# ---------------------------------------------------------------------------

run_with_timeout() {
  # Foreground sequence; if any phase fails, cleanup runs via trap.
  trap 'phase_cleanup; exit 1' ERR INT TERM

  phase_bsl_status                 || fail "bsl_status"
  phase_schedules_have_runs        || true   # warn-only
  phase_pre_cleanup                || fail "pre_cleanup"
  phase_create_drill_tenant        || fail "create_drill_tenant"
  phase_take_backup                || fail "take_backup"
  phase_simulate_disaster          || fail "simulate_disaster"
  phase_restore                    || fail "restore"
  phase_verify_restored            || fail "verify_restored"
  phase_cleanup                    || fail "cleanup"

  trap - ERR INT TERM
}

# Use coproc-style timeout: run drill in subshell, watchdog kills if too long.
(
  run_with_timeout
) &
DRILL_PID=$!

(
  sleep "$DRILL_TIMEOUT_SECS"
  if kill -0 "$DRILL_PID" 2>/dev/null; then
    log "drill exceeded ${DRILL_TIMEOUT_SECS}s timeout — killing"
    kill -TERM "$DRILL_PID" 2>/dev/null || true
  fi
) &
WATCHDOG_PID=$!

if wait "$DRILL_PID"; then
  kill "$WATCHDOG_PID" 2>/dev/null || true
  emit_report "passed"
  exit 0
else
  kill "$WATCHDOG_PID" 2>/dev/null || true
  emit_report "failed"
  exit 1
fi
