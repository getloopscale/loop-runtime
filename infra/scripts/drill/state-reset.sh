#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
set -euo pipefail

# Plan 2026-05-16-drill-l2-unlock T1.2.bis (EC-01 MUST FIX).
#
# Pulumi state clean-slate helper for drill stack.
#
# Why this exists
# ---------------
# Drill fault-finding run 2026-05-16 (Finding #3, HIGH/BLOCKER L2) observed:
# `pulumi stack output --json` returned LEGACY single-cluster schema even
# after iter 1 commit `94d01f89` rewrote outputs to cluster map (v1) and
# iter L2-unlock T1.2 migrated to flat named exports (v2). The legacy state
# entries `cluster`, `clusterKubeconfig`, `bucket` (5-byte "null" string)
# persisted across schema migrations.
#
# Root cause hypotheses (see finding-2026-05-16-pulumi-outputs-rca.md):
#   A. Pulumi state cache retention across schema changes
#   B. Output<T> resolution failure in object-literal aggregation
#   C. Named-export quirk
#
# Fix path D1 (T1.2) addresses B+C. THIS script addresses A by providing a
# safe, idempotent operator-invoked clean-slate sequence:
#
#   1. Capture current `pulumi stack output --json` → audit trail
#   2. List legacy entries that would be removed (dry-run default)
#   3. Optional `pulumi stack rm` to delete STATE (NOT cloud resources)
#   4. Re-init clean state via `pulumi stack init`
#   5. Subsequent `pulumi up` re-establishes state ↔ cloud alignment
#
# CRITICAL DISTINCTION:
#   - `pulumi stack rm` deletes Pulumi STATE FILE (in S3 backend bucket)
#   - `pulumi destroy` deletes CLOUD RESOURCES (DOKS, PG, Spaces, VPC, etc.)
#
# This script ONLY touches STATE. Cloud resources are NEVER affected.
# Operator should run AFTER `pulumi destroy` (if cleaning legacy stack
# completely) OR after schema migration (if cloud has been already destroyed
# but state holds stale outputs).
#
# Usage:
#   bash infra/scripts/drill-state-reset.sh                  # dry-run (default)
#   bash infra/scripts/drill-state-reset.sh --force          # actually execute
#   bash infra/scripts/drill-state-reset.sh --force --skip-init  # only remove, no re-init
#
# Exit codes:
#   0 — Success (dry-run preview emitted, OR force-mode completed)
#   1 — Tool missing (pulumi not installed)
#   2 — Stack not found (nothing to reset)
#   3 — Pulumi state operation failed
#   4 — Argument error (bad flag combination)
#
# Environment:
#   PULUMI_ACCESS_TOKEN — required for `pulumi stack rm` operation
#   PULUMI_CONFIG_PASSPHRASE — required if state uses passphrase secrets
#   PULUMI_DRILL_DIR — default infra/pulumi-drill (override for tests)
#   DRILL_AUDIT_DIR — default /tmp (audit report destination)
#
# Idempotency: re-running is SAFE. Second invocation finds no legacy state
# and exits 0 with audit note. Operator can re-run after dropped network
# without consequences.


# --- Argument parsing ---
DRY_RUN=true
SKIP_INIT=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) DRY_RUN=false; shift ;;
    --skip-init) SKIP_INIT=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help)
      cat <<'HELP'
drill-state-reset.sh — Pulumi state clean-slate helper for drill stack

  --force        Actually execute destructive state ops (default is dry-run)
  --skip-init    After `pulumi stack rm`, do NOT re-init (operator handles)
  --dry-run      Explicit dry-run (default behavior)
  -h, --help     Show this help

Plan: 2026-05-16-drill-l2-unlock T1.2.bis (EC-01 MUST FIX)
HELP
      exit 0 ;;
    *)
      echo "[drill-state-reset] ERROR: unknown flag $1" >&2
      exit 4 ;;
  esac
done

# --- Resolve paths ---
PULUMI_DIR="${PULUMI_DRILL_DIR:-infra/pulumi-drill}"
AUDIT_DIR="${DRILL_AUDIT_DIR:-/tmp}"
mkdir -p "$AUDIT_DIR"
AUDIT_FILE="$AUDIT_DIR/drill-state-reset-audit-$(date +%Y%m%dT%H%M%SZ).md"

log() { printf '[drill-state-reset] %s\n' "$*" >&2; }

# --- Pre-flight checks ---
if ! command -v pulumi >/dev/null 2>&1; then
  log "ERROR: pulumi CLI not installed"
  exit 1
fi

if [[ ! -d "$PULUMI_DIR" ]]; then
  log "ERROR: Pulumi project dir not found: $PULUMI_DIR"
  exit 1
fi

# --- Start audit trail ---
{
  echo "# Drill State Reset Audit — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "**Plan:** 2026-05-16-drill-l2-unlock T1.2.bis (EC-01 MUST FIX)"
  echo "**Mode:** $([ "$DRY_RUN" = true ] && echo 'DRY-RUN' || echo 'FORCE')"
  echo "**Skip init:** $SKIP_INIT"
  echo "**Pulumi dir:** $PULUMI_DIR"
  echo "**Invocation:** $0 $*"
  echo ""
  echo "## Pre-reset state"
  echo ""
} > "$AUDIT_FILE"

# --- Capture current state ---
log "Capturing current Pulumi state outputs..."
cd "$PULUMI_DIR"

# Check if drill stack exists
if ! pulumi stack ls --json 2>/dev/null | grep -q '"drill"'; then
  log "Stack 'drill' not found — nothing to reset (this is idempotent path)"
  {
    echo "**Result:** STACK_NOT_FOUND (idempotent no-op)"
    echo ""
    echo "Stack 'drill' does not exist in this backend. Nothing to reset."
  } >> "$AUDIT_FILE"
  log "Audit: $AUDIT_FILE"
  exit 2
fi

# Capture outputs for audit
PRE_OUTPUTS=$(pulumi stack output --stack drill --json 2>/dev/null || echo '{}')
{
  echo '```json'
  echo "$PRE_OUTPUTS" | head -200
  echo '```'
  echo ""
  echo "## Legacy detection"
  echo ""
} >> "$AUDIT_FILE"

# --- Detect legacy keys that would warrant clean-slate ---
LEGACY_DETECTED=false
LEGACY_KEYS=""

# Check for v0 legacy singular keys (Finding #3 observed values)
for legacy_key in cluster clusterKubeconfig bucket; do
  if echo "$PRE_OUTPUTS" | grep -q "\"$legacy_key\""; then
    LEGACY_DETECTED=true
    LEGACY_KEYS="$LEGACY_KEYS $legacy_key"
  fi
done

# Check for v1 cluster-map (object-literal `outputs` key wrapping)
if echo "$PRE_OUTPUTS" | grep -q '"outputs"'; then
  LEGACY_DETECTED=true
  LEGACY_KEYS="$LEGACY_KEYS outputs(v1-object-literal)"
fi

if [[ "$LEGACY_DETECTED" == "false" ]]; then
  # Phase 2 plan T4.1 (D3 + EC-6) — phantom-resource detection extension
  # State may have v2 schema but resourceCount > 0 from teardown that didn't
  # `pulumi stack rm`. Verify cloud-empty via doctl before declaring "clean".
  log "No legacy schema detected — checking for phantom resources (T4.1 D3)..."

  RESOURCE_COUNT=0
  if pulumi stack ls --json 2>/dev/null > /tmp/.drill-state-reset-stacks.json; then
    RESOURCE_COUNT=$(python3 -c "
import sys, json
try:
    stacks = json.load(open('/tmp/.drill-state-reset-stacks.json'))
    for s in stacks:
        if s.get('name') == 'drill':
            print(s.get('resourceCount', 0))
            sys.exit(0)
    print(0)
except Exception:
    print(0)
" 2>/dev/null || echo 0)
    rm -f /tmp/.drill-state-reset-stacks.json
  fi

  if [[ "$RESOURCE_COUNT" -eq 0 ]]; then
    log "State + cloud truly clean (v2 schema, zero resources)"
    {
      echo "**Result:** ALREADY_CLEAN"
      echo ""
      echo "No v0/v1 legacy keys + zero phantom resources. No action needed."
    } >> "$AUDIT_FILE"
    log "Audit: $AUDIT_FILE"
    exit 0
  fi

  # State has resources — verify via doctl whether cloud has them too
  log "State has $RESOURCE_COUNT resources. Verifying cloud state via doctl..."
  if ! command -v doctl >/dev/null 2>&1; then
    # EC-6 safe fallback: NO auto-rm without verified cloud state
    log "WARN: doctl unavailable — cannot verify cloud state"
    log "  Cannot classify as phantom vs real. Operator must manually decide."
    log "  Re-run with --force if you've verified cloud is empty externally."
    {
      echo "**Result:** AMBIGUOUS_STATE_DOCTL_UNAVAILABLE"
      echo ""
      echo "State has $RESOURCE_COUNT resources but doctl not installed."
      echo "Operator must verify cloud state manually before --force."
    } >> "$AUDIT_FILE"
    exit 2
  fi

  CLUSTER_COUNT=$(doctl kubernetes cluster list --no-header 2>/dev/null | grep -c loop-runtime-drill || echo 0)
  DB_COUNT=$(doctl databases list --no-header 2>/dev/null | grep -c loop-runtime-drill || echo 0)

  if [[ "$CLUSTER_COUNT" -gt 0 || "$DB_COUNT" -gt 0 ]]; then
    log "Cloud has REAL resources ($CLUSTER_COUNT clusters + $DB_COUNT DBs)"
    log "  This is NOT phantom — operator must `task drill:down` first."
    {
      echo "**Result:** REAL_RESOURCES_EXIST"
      echo ""
      echo "State + cloud both have resources. NOT phantom. Run task drill:down first."
    } >> "$AUDIT_FILE"
    exit 2
  fi

  # State has resources but cloud is empty → PHANTOM. Mark for force-rm.
  log "Detected phantom resources (state=$RESOURCE_COUNT, cloud=0) — eligible for force-rm"
  LEGACY_DETECTED=true
  LEGACY_KEYS="phantom-resources(state=$RESOURCE_COUNT,cloud=0)"
fi

log "Legacy state entries detected: $LEGACY_KEYS"
{
  echo "Detected legacy keys requiring clean-slate:"
  for key in $LEGACY_KEYS; do
    echo "- $key"
  done
  echo ""
  echo "## Action"
  echo ""
} >> "$AUDIT_FILE"

# --- Execute or dry-run ---
if [[ "$DRY_RUN" == "true" ]]; then
  log "DRY-RUN mode — would execute the following sequence:"
  log "  1. pulumi stack rm drill --yes (deletes STATE — cloud resources unaffected)"
  if [[ "$SKIP_INIT" == "false" ]]; then
    log "  2. pulumi stack init drill --secrets-provider passphrase (fresh state)"
  fi
  log "  Re-run with --force to actually execute."
  {
    echo "**Result:** DRY_RUN (no destructive ops executed)"
    echo ""
    echo "Planned sequence (re-run with --force):"
    echo "1. \`pulumi stack rm drill --yes\` — deletes STATE only"
    if [[ "$SKIP_INIT" == "false" ]]; then
      echo "2. \`pulumi stack init drill --secrets-provider passphrase\` — fresh state"
    fi
  } >> "$AUDIT_FILE"
  log "Audit: $AUDIT_FILE"
  exit 0
fi

# --- Force mode: execute destructive state ops ---
log "FORCE MODE — executing destructive state operations"

# Iter 11 LIVE bug fix: backup Pulumi.<stack>.yaml BEFORE rm/init operations
# because `pulumi stack init` OVERWRITES the file with empty config + new
# encryptionsalt. Without backup, all stack config (domain, doProject,
# region, K8s versions) and encrypted secrets (cfAccountId) are LOST.
PULUMI_STACK_YAML="$PULUMI_DIR/Pulumi.drill.yaml"
PULUMI_STACK_BACKUP=""
if [[ -f "$PULUMI_STACK_YAML" ]]; then
  PULUMI_STACK_BACKUP="${AUDIT_DIR}/Pulumi.drill.yaml.backup-$(date +%Y%m%dT%H%M%SZ)"
  cp "$PULUMI_STACK_YAML" "$PULUMI_STACK_BACKUP"
  log "Backed up Pulumi.drill.yaml → $PULUMI_STACK_BACKUP"
fi

log "Step 1/2: pulumi stack rm drill --yes --force"
# --force needed when state has phantom resources (cloud was teardown via
# `pulumi destroy` separately + state still has stale resource registrations).
# Iter 11 LIVE bug discovery: drill state had 4 phantom resources (v0 legacy
# from cluster bb47d1d8 teardown in iter 5 fault-finding); pulumi stack rm
# without --force rejected. --force is appropriate in this script context
# because this script operates ONLY on state (NEVER cloud resources — that's
# `pulumi destroy`).
if ! pulumi stack rm drill --yes --force 2>>"$AUDIT_FILE"; then
  log "ERROR: pulumi stack rm --force failed (see $AUDIT_FILE)"
  # Restore backup even on failure (defensive)
  [[ -n "$PULUMI_STACK_BACKUP" && -f "$PULUMI_STACK_BACKUP" ]] && cp "$PULUMI_STACK_BACKUP" "$PULUMI_STACK_YAML"
  echo "**Result:** RM_FAILED" >> "$AUDIT_FILE"
  exit 3
fi

if [[ "$SKIP_INIT" == "true" ]]; then
  log "Step 2/2 SKIPPED (--skip-init)"
  # Still restore backup so Pulumi.drill.yaml has original config + salt
  [[ -n "$PULUMI_STACK_BACKUP" && -f "$PULUMI_STACK_BACKUP" ]] && cp "$PULUMI_STACK_BACKUP" "$PULUMI_STACK_YAML"
  {
    echo "**Result:** RM_COMPLETED_NO_REINIT"
    echo ""
    echo "Operator MUST run \`pulumi stack init drill\` before next \`pulumi up\`."
    echo "Pulumi.drill.yaml backup restored from $PULUMI_STACK_BACKUP."
  } >> "$AUDIT_FILE"
else
  log "Step 2/2: pulumi stack init drill"
  if ! pulumi stack init drill --secrets-provider passphrase 2>>"$AUDIT_FILE"; then
    log "ERROR: pulumi stack init failed (see $AUDIT_FILE)"
    [[ -n "$PULUMI_STACK_BACKUP" && -f "$PULUMI_STACK_BACKUP" ]] && cp "$PULUMI_STACK_BACKUP" "$PULUMI_STACK_YAML"
    echo "**Result:** INIT_FAILED" >> "$AUDIT_FILE"
    exit 3
  fi
  # Iter 11 LIVE bug fix: pulumi stack init wrote NEW encryptionsalt + empty
  # config to Pulumi.drill.yaml. Restore backup so all stack config + encrypted
  # secrets (e.g., cfAccountId) are preserved with original salt → pulumi up
  # can decrypt secrets normally.
  if [[ -n "$PULUMI_STACK_BACKUP" && -f "$PULUMI_STACK_BACKUP" ]]; then
    cp "$PULUMI_STACK_BACKUP" "$PULUMI_STACK_YAML"
    log "Restored Pulumi.drill.yaml from backup (preserves config + encryptionsalt + secrets)"
  fi
  {
    echo "**Result:** RESET_COMPLETED"
    echo ""
    echo "Stack 'drill' state is now empty. Next \`pulumi up\` will provision"
    echo "fresh resources (existing cloud resources unaffected unless imported)."
    echo "Pulumi.drill.yaml restored from $PULUMI_STACK_BACKUP (preserves config + salt + secrets)."
  } >> "$AUDIT_FILE"
fi

log "Audit: $AUDIT_FILE"
log "Done. Next \`pulumi up\` will re-establish state ↔ cloud alignment."
