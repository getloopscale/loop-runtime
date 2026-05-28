#!/usr/bin/env bash
# T4.5 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
# Companion to docs/operations/rollback-m07-cutover-runbook.md.
# CR-10 (MEET-2026-05-28-001): canonical M07 cutover rollback
# orchestration. Always invoke with --dry-run first.
#
# Usage:
#   m07-cutover-rollback.sh --dry-run [flags]
#   m07-cutover-rollback.sh --execute [flags]
#
# Flags:
#   --target-rev N        loopzero-api ArgoCD revision to roll back to (required)
#   --build-rev N         loopzero-build-cwts ArgoCD revision to roll back to (required)
#   --skip-step3          skip Sigstore stack removal (Platform Lead sign-off)
#   --skip-step4          skip migration rollback (sponsor sign-off — default)
#   --emit-audit          write a release.cutover_rollback row to audit_log via psql
#   --argocd-cmd CMD      argocd binary (default: argocd)
#   --kubectl-cmd CMD     kubectl binary (default: kubectl)
#   --helm-cmd CMD        helm binary (default: helm)
#   --psql-cmd CMD        psql binary (default: psql)
#   --pulumi-cmd CMD      pulumi binary (default: pulumi)
#
# Exit codes:
#   0 success (dry-run prints OK; execute completes all selected steps)
#   1 usage error (missing or invalid flag)
#   2 step 0/1/2 failure (drain or App rollback)
#   3 step 3 failure (Sigstore stack removal)
#   4 step 4 failure (migration rollback — sponsor sign-off path)

set -euo pipefail

DRY_RUN=0
EXECUTE=0
SKIP_STEP3=0
SKIP_STEP4=1
EMIT_AUDIT=0
TARGET_REV=""
BUILD_REV=""

ARGOCD_CMD="argocd"
KUBECTL_CMD="kubectl"
HELM_CMD="helm"
PSQL_CMD="psql"
PULUMI_CMD="pulumi"

usage() {
  cat >&2 <<USAGE
Usage: $0 (--dry-run | --execute) --target-rev N --build-rev N [flags]

Required (exactly one):
  --dry-run     print every command without mutating state
  --execute     run the rollback against live infrastructure

Required:
  --target-rev N     loopzero-api revision (from \`argocd app history loopzero-api\`)
  --build-rev  N     loopzero-build-cwts revision

Optional:
  --skip-step3       skip Sigstore stack removal
  --skip-step4       skip migration rollback (default ON — sponsor sign-off required)
  --emit-audit       write release.cutover_rollback row to audit_log
  --argocd-cmd CMD   override argocd binary path
  --kubectl-cmd CMD  override kubectl binary path
  --helm-cmd CMD     override helm binary path
  --psql-cmd CMD     override psql binary path
  --pulumi-cmd CMD   override pulumi binary path

See docs/operations/rollback-m07-cutover-runbook.md for the full procedure
and sign-off matrix.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)     DRY_RUN=1; shift ;;
    --execute)     EXECUTE=1; shift ;;
    --target-rev)  TARGET_REV="$2"; shift 2 ;;
    --build-rev)   BUILD_REV="$2"; shift 2 ;;
    --skip-step3)  SKIP_STEP3=1; shift ;;
    --skip-step4)  SKIP_STEP4=1; shift ;;
    --no-skip-step4) SKIP_STEP4=0; shift ;;
    --emit-audit)  EMIT_AUDIT=1; shift ;;
    --argocd-cmd)  ARGOCD_CMD="$2"; shift 2 ;;
    --kubectl-cmd) KUBECTL_CMD="$2"; shift 2 ;;
    --helm-cmd)    HELM_CMD="$2"; shift 2 ;;
    --psql-cmd)    PSQL_CMD="$2"; shift 2 ;;
    --pulumi-cmd)  PULUMI_CMD="$2"; shift 2 ;;
    -h|--help)     usage; exit 0 ;;
    *)             echo "ERROR: unknown flag: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ "$DRY_RUN" == "0" && "$EXECUTE" == "0" ]]; then
  echo "ERROR: pass exactly one of --dry-run or --execute." >&2
  usage
  exit 1
fi
if [[ "$DRY_RUN" == "1" && "$EXECUTE" == "1" ]]; then
  echo "ERROR: --dry-run and --execute are mutually exclusive." >&2
  exit 1
fi
if [[ -z "$TARGET_REV" || -z "$BUILD_REV" ]]; then
  echo "ERROR: --target-rev and --build-rev are required." >&2
  usage
  exit 1
fi

run() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf 'DRY-RUN: %s\n' "$*"
    return 0
  fi
  printf '+ %s\n' "$*" >&2
  "$@"
}

run_sh() {
  # Run a shell pipeline (already expanded). Same dry-run semantics.
  if [[ "$DRY_RUN" == "1" ]]; then
    printf 'DRY-RUN: %s\n' "$*"
    return 0
  fi
  printf '+ %s\n' "$*" >&2
  bash -c "$*"
}

echo "=== M07 Cutover Rollback ==="
echo "Mode:           $([[ "$DRY_RUN" == "1" ]] && echo "DRY-RUN" || echo "EXECUTE")"
echo "Target rev (api):           $TARGET_REV"
echo "Target rev (build-cwts):    $BUILD_REV"
echo "Skip step 3 (Sigstore):     $SKIP_STEP3"
echo "Skip step 4 (migration):    $SKIP_STEP4"
echo "Emit audit row:             $EMIT_AUDIT"
echo "============================"

# ---------------------------------------------------------------------------
# Step 0a — Drain in-flight workflows
# ---------------------------------------------------------------------------
echo ""
echo "--- Step 0a: drain in-flight Argo Workflows ---"
run_sh "$KUBECTL_CMD get workflows -n argo --no-headers | awk '\$2 == \"Running\" {print \$1}' | xargs -r -n1 $ARGOCD_CMD terminate -n argo"

# ---------------------------------------------------------------------------
# Step 0b — Discovery: print history for operator confirmation
# ---------------------------------------------------------------------------
echo ""
echo "--- Step 0b: ArgoCD history (operator must confirm target revision matches) ---"
run "$ARGOCD_CMD" app history loopzero-api
run "$ARGOCD_CMD" app history loopzero-build-cwts

# ---------------------------------------------------------------------------
# Step 1 — Roll back loopzero-api
# ---------------------------------------------------------------------------
echo ""
echo "--- Step 1: roll back loopzero-api to revision $TARGET_REV ---"
if ! run "$ARGOCD_CMD" app rollback loopzero-api "$TARGET_REV"; then
  echo "ERROR: Step 1 failed — STOP and escalate to Platform Lead." >&2
  exit 2
fi
run "$ARGOCD_CMD" app wait loopzero-api --health --sync --timeout 300

# ---------------------------------------------------------------------------
# Step 2 — Roll back loopzero-build-cwts
# ---------------------------------------------------------------------------
echo ""
echo "--- Step 2: roll back loopzero-build-cwts to revision $BUILD_REV ---"
if ! run "$ARGOCD_CMD" app rollback loopzero-build-cwts "$BUILD_REV"; then
  echo "ERROR: Step 2 failed — STOP and escalate to Platform Lead." >&2
  exit 2
fi
run "$ARGOCD_CMD" app wait loopzero-build-cwts --health --sync --timeout 300

# ---------------------------------------------------------------------------
# Step 3 — Sigstore stack removal (Platform Lead sign-off)
# ---------------------------------------------------------------------------
if [[ "$SKIP_STEP3" == "1" ]]; then
  echo ""
  echo "--- Step 3 SKIPPED (--skip-step3) ---"
else
  echo ""
  echo "--- Step 3: remove Sigstore stack + fall back to public Sigstore ---"
  run "$HELM_CMD" uninstall sigstore-extras -n fulcio-system
  run "$HELM_CMD" uninstall fulcio -n fulcio-system
  run "$HELM_CMD" uninstall rekor -n rekor-system
  run "$HELM_CMD" uninstall ctlog -n ctlog-system
  run "$PULUMI_CMD" -s production config set loopzero:CosignFulcioURL https://fulcio.sigstore.dev
  run "$PULUMI_CMD" -s production config set loopzero:CosignRekorURL  https://rekor.sigstore.dev
  if ! run "$PULUMI_CMD" -s production up --yes; then
    echo "ERROR: Step 3 Pulumi up failed — investigate before retrying." >&2
    exit 3
  fi
fi

# ---------------------------------------------------------------------------
# Step 4 — Migration rollback (sponsor sign-off; default SKIP)
# ---------------------------------------------------------------------------
if [[ "$SKIP_STEP4" == "1" ]]; then
  echo ""
  echo "--- Step 4 SKIPPED (forward-only policy; sponsor sign-off required) ---"
else
  echo ""
  echo "--- Step 4: drop migration 000020 triggers (sponsor sign-off path) ---"
  run "$PSQL_CMD" -c "DROP TRIGGER IF EXISTS releases_attestation_url_immutable ON releases;"
  run "$PSQL_CMD" -c "DROP TRIGGER IF EXISTS releases_attestation_url_audit ON releases;"
  run "$PSQL_CMD" -c "DROP FUNCTION IF EXISTS reject_attestation_url_mutation();"
  if ! run "$PSQL_CMD" -c "DROP FUNCTION IF EXISTS audit_attestation_url_set();"; then
    echo "ERROR: Step 4 migration rollback failed." >&2
    exit 4
  fi
fi

# ---------------------------------------------------------------------------
# Audit marker
# ---------------------------------------------------------------------------
if [[ "$EMIT_AUDIT" == "1" ]]; then
  echo ""
  echo "--- Emitting release.cutover_rollback audit row ---"
  run "$PSQL_CMD" -c "INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, metadata) VALUES ('system', 'release.cutover_rollback', 'system:m07-rollback-runbook', 'platform', 'm07-cutover', jsonb_build_object('target_rev', '$TARGET_REV', 'build_rev', '$BUILD_REV', 'skip_step3', $SKIP_STEP3, 'skip_step4', $SKIP_STEP4));"
fi

echo ""
echo "=== M07 Cutover Rollback complete ==="
echo "Next steps:"
echo "  1. Submit a probe build: loopzero deploy --project probe --env staging --wait"
echo "  2. Watch LoopZeroCosignSignErrorRate alert clear within 15 min"
echo "  3. Watch LoopZeroReleaseAttestationRekorURLNullRate alert clear within 15 min"
echo "  4. Log incident link + revisions used + actual time consumed"
