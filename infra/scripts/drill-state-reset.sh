#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
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

set -euo pipefail

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
  log "No legacy state entries detected — current state is already clean (v2 named exports)"
  {
    echo "**Result:** ALREADY_CLEAN"
    echo ""
    echo "No v0 singular keys + no v1 object-literal aggregation detected."
    echo "Current state matches v2 named-exports schema. No action needed."
  } >> "$AUDIT_FILE"
  log "Audit: $AUDIT_FILE"
  exit 0
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
log "Step 1/2: pulumi stack rm drill --yes"
if ! pulumi stack rm drill --yes 2>>"$AUDIT_FILE"; then
  log "ERROR: pulumi stack rm failed (see $AUDIT_FILE)"
  echo "**Result:** RM_FAILED" >> "$AUDIT_FILE"
  exit 3
fi

if [[ "$SKIP_INIT" == "true" ]]; then
  log "Step 2/2 SKIPPED (--skip-init)"
  {
    echo "**Result:** RM_COMPLETED_NO_REINIT"
    echo ""
    echo "Operator MUST run \`pulumi stack init drill\` before next \`pulumi up\`."
  } >> "$AUDIT_FILE"
else
  log "Step 2/2: pulumi stack init drill"
  if ! pulumi stack init drill --secrets-provider passphrase 2>>"$AUDIT_FILE"; then
    log "ERROR: pulumi stack init failed (see $AUDIT_FILE)"
    echo "**Result:** INIT_FAILED" >> "$AUDIT_FILE"
    exit 3
  fi
  {
    echo "**Result:** RESET_COMPLETED"
    echo ""
    echo "Stack 'drill' state is now empty. Next \`pulumi up\` will provision"
    echo "fresh resources (existing cloud resources unaffected unless imported)."
  } >> "$AUDIT_FILE"
fi

log "Audit: $AUDIT_FILE"
log "Done. Next \`pulumi up\` will re-establish state ↔ cloud alignment."
