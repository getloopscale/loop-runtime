#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan 2026-05-16-drill-l2-unlock T3.1 (Finding #2 MEDIUM fix).
#
# Pulumi state recovery helper — mandatory pre-retry sequence per ADR D3.
#
# Why this exists
# ---------------
# Drill fault-finding run 2026-05-16 (Finding #2 MEDIUM) observed:
# v1 `pulumi up` attempt failed at K8s create but PG creation API was
# accepted by DO. v2 retry saw "cluster name not available". Pattern:
# parallel resource creation + partial failure → Pulumi state marks
# resources as "creating failed" but DO API may have accepted creates
# OR marked some as "ready". State + cloud diverge.
#
# Resolution (per D3):
#   1. `pulumi cancel` — clears any pending operations from state
#   2. `pulumi refresh` — reconciles state with actual cloud reality
#   3. Retry `pulumi up` normally — state is now accurate
#
# This script wraps that sequence safely with logging + idempotency.
# Operator/Ralph invokes ONLY after observing pulumi up failure mid-way.
# Skill SKILL.md Etapa 2 documents the trigger condition.
#
# Usage:
#   bash infra/scripts/drill-cancel-refresh.sh             # full sequence
#   bash infra/scripts/drill-cancel-refresh.sh --dry-run   # preview only
#
# Exit codes:
#   0 — Sequence completed (cancel + refresh both reported success)
#   1 — Tool missing (pulumi CLI)
#   2 — Stack not found (nothing to reconcile)
#   3 — pulumi cancel or refresh failed (operator must investigate)
#   4 — Argument error
#
# Environment:
#   PULUMI_ACCESS_TOKEN — required
#   PULUMI_CONFIG_PASSPHRASE — required if state uses passphrase secrets
#   PULUMI_DRILL_DIR — default infra/pulumi-drill (override for tests)
#   DRILL_AUDIT_DIR — default /tmp (audit destination)
#
# Idempotency: SAFE to re-run. Cancel on a stack with zero pending ops
# returns success. Refresh on a stack already aligned with cloud returns
# success. Belt-and-suspenders pattern documented in plan EC-08 (accepted
# risk during legitimate in-progress operations — operator-invoked context
# guarantees no false-positive interference).

set -euo pipefail

# --- Argument parsing ---
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help)
      cat <<'HELP'
drill-cancel-refresh.sh — Pulumi state recovery for drill (post-failure retry)

  --dry-run     Preview commands without executing
  -h, --help    Show this help

Plan: 2026-05-16-drill-l2-unlock T3.1 (Finding #2 fix)
HELP
      exit 0 ;;
    *)
      echo "[drill-cancel-refresh] ERROR: unknown flag $1" >&2
      exit 4 ;;
  esac
done

PULUMI_DIR="${PULUMI_DRILL_DIR:-infra/pulumi-drill}"
AUDIT_DIR="${DRILL_AUDIT_DIR:-/tmp}"
mkdir -p "$AUDIT_DIR"
AUDIT_FILE="$AUDIT_DIR/drill-cancel-refresh-$(date +%Y%m%dT%H%M%SZ).log"

log() { printf '[drill-cancel-refresh] %s\n' "$*" >&2; }

# --- Pre-flight ---
if ! command -v pulumi >/dev/null 2>&1; then
  log "ERROR: pulumi CLI not installed"
  exit 1
fi

if [[ ! -d "$PULUMI_DIR" ]]; then
  log "ERROR: Pulumi project dir not found: $PULUMI_DIR"
  exit 1
fi

cd "$PULUMI_DIR"

# Check stack exists
if ! pulumi stack ls --json 2>/dev/null | grep -q '"drill"'; then
  log "Stack 'drill' not found — nothing to reconcile (idempotent no-op)"
  echo "STACK_NOT_FOUND" > "$AUDIT_FILE"
  exit 2
fi

# --- Phase 1: cancel ---
log "Phase 1/2: pulumi cancel --stack drill --yes"
if [[ "$DRY_RUN" == "true" ]]; then
  log "  DRY-RUN: skipping cancel"
  {
    echo "DRY_RUN cancel skipped"
  } >> "$AUDIT_FILE"
else
  if pulumi cancel --stack drill --yes 2>>"$AUDIT_FILE"; then
    log "  ✓ cancel OK (any pending ops cleared)"
  else
    rc=$?
    # `pulumi cancel` exits 0 when no pending ops; non-zero usually means tool error
    log "  ✗ cancel failed (exit $rc) — see $AUDIT_FILE"
    exit 3
  fi
fi

# --- Phase 2: refresh ---
log "Phase 2/2: pulumi refresh --stack drill --yes"
if [[ "$DRY_RUN" == "true" ]]; then
  log "  DRY-RUN: skipping refresh"
  {
    echo "DRY_RUN refresh skipped"
  } >> "$AUDIT_FILE"
  log "Dry-run complete. Audit: $AUDIT_FILE"
  exit 0
fi

if pulumi refresh --stack drill --yes --skip-preview 2>>"$AUDIT_FILE"; then
  log "  ✓ refresh OK (state reconciled with cloud reality)"
else
  rc=$?
  log "  ✗ refresh failed (exit $rc) — operator must investigate"
  log "    Hint: review $AUDIT_FILE for refresh diagnostics"
  log "    Hint: 'pulumi stack' to see state details"
  exit 3
fi

log ""
log "✓ State recovery complete. Safe to retry 'pulumi up --stack drill --yes'."
log "  Audit: $AUDIT_FILE"
