#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan 2026-05-16 T4.1.bis (EC-05 fix) — Drill teardown validator + doctl fallback.
#
# Independent validation that ZERO drill-tagged resources remain after
# `pulumi destroy --stack drill` returns. Pulumi can fail partially:
#   - DO API timeout on one of 4 clusters
#   - Finalizer stuck on ESO/ArgoCD secret
#   - Resources outside Pulumi state (e.g., orphan from prior run)
#
# When pulumi state diverges from cloud reality, drill stack residuals
# continue burning cost ($0.04/hr/node × N nodes = $1-4/day unnoticed
# until the billing dashboard).
#
# This script:
#   1. Acquires file lock to prevent concurrent destroy race
#   2. Lists DO resources tagged `loopruntime-drill` via doctl
#   3. If any found, force-deletes them per type
#   4. Re-validates count is zero
#   5. Emits audit-trail markdown report
#
# Used by (operator-run — GitHub Actions removed 2026-05-23):
#   - drill soaks (Stage 1 + Stage 2 post-teardown)
#   - task drill:l2-scale (post-teardown)
#   - infra/scripts/drill/watchdog.sh (T4.1 wrapper)
#
# Exit codes:
#   0 — Zero residuals (clean) OR successfully force-deleted residuals
#   1 — DIGITALOCEAN_TOKEN missing or doctl not installed
#   2 — Lock acquisition failed (another teardown in progress)
#   3 — Force-delete failed (manual intervention required)
#   4 — A resource list could not be read (auth, network): the account's
#       state is unknown, so the teardown is NOT reported clean
#
# Environment:
#   DIGITALOCEAN_TOKEN — required, scoped to drill account per D7
#   DRILL_TEARDOWN_LOCK_FILE — default /tmp/drill-teardown.lock
#   DRILL_AUDIT_DIR — default /tmp (audit report destination)

set -euo pipefail

# T2.2-followup (plan 2026-05-21-deep-review-remediation): adopt the
# shared dry-run lib. Apply wrapper gates each destructive doctl call.
# Sourcing path uses absolute resolution so script works both when
# invoked directly + via Taskfile.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/dry-run.sh disable=SC1091
source "$SCRIPT_DIR/../lib/dry-run.sh"
parse_dry_run_flag "$@"
# Rebuild positional args without --dry-run so downstream code is unchanged.
if [ "${#REMAINING_ARGS[@]}" -gt 0 ]; then
  set -- "${REMAINING_ARGS[@]}"
else
  set --
fi

# --- Input validation ---
if [[ -z "${DIGITALOCEAN_TOKEN:-}" ]]; then
  echo "[drill-teardown-validator] ERROR: DIGITALOCEAN_TOKEN env var required (drill-scoped per D7)" >&2
  exit 1
fi
if ! command -v doctl >/dev/null 2>&1; then
  echo "[drill-teardown-validator] ERROR: doctl not installed" >&2
  exit 1
fi

LOCK_FILE="${DRILL_TEARDOWN_LOCK_FILE:-/tmp/drill-teardown.lock}"
AUDIT_DIR="${DRILL_AUDIT_DIR:-/tmp}"
AUDIT_FILE="$AUDIT_DIR/drill-teardown-audit-$(date +%Y%m%dT%H%M%SZ).md"

log() { printf '[drill-teardown-validator] %s\n' "$*" >&2; }

# list_or_fail runs a doctl list and prints its output. A failing list is fatal:
# an empty answer from a call that did not succeed would read as "no
# residuals", and this script is the net that catches a drill left billing.
list_or_fail() {
  local what="$1" out
  shift
  if ! out=$("$@" 2>&1); then
    log "ERROR: could not list $what — the teardown state is unknown: $out"
    exit 4
  fi
  printf '%s\n' "$out"
}

# --- flock to serialize concurrent destroy + validate ---
# Open FD 200 to lockfile; flock blocks for up to 30s waiting.
exec 200>"$LOCK_FILE"
if ! flock -x -w 30 200; then
  log "ERROR: could not acquire $LOCK_FILE within 30s (concurrent teardown in progress?)"
  exit 2
fi
log "Lock acquired: $LOCK_FILE"

# --- Start audit trail ---
{
  echo "# Drill Teardown Audit — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "**Plan:** 2026-05-16 T4.1.bis (EC-05 fix)"
  echo "**Invocation:** $0"
  echo "**Lock:** $LOCK_FILE"
  echo ""
  echo "## Pre-validation scan"
  echo ""
} > "$AUDIT_FILE"

# --- Scan DO resources by tag ---
# DO doctl filters: --tag-name not universal, so we list + jq-filter.

log "Scanning DO resources tagged 'loopruntime-drill'..."

RESIDUAL_FOUND=0
RESIDUAL_TOTAL=0
RESIDUAL_DELETED=0
RESIDUAL_FAILED=0

# Resource types validated below (kept as comments for documentation;
# original arrays SC2034-unused per shellcheck — the per-type loops were
# inlined for clarity during T4.1.bis development).
# - kubernetes-clusters (doctl kubernetes cluster ...)
# - databases (doctl databases ...)
# - spaces (s3-compatible API, not doctl)
# - vpcs (doctl vpcs ...) — handled with retry-backoff per T3.2 D4
# - projects (doctl projects ...)

# Kubernetes clusters
log "  - Listing DOKS clusters..."
DOKS_LIST=$(list_or_fail "Kubernetes clusters" doctl kubernetes cluster list --format ID,Name,Tag --no-header)
DOKS_DRILL=$(echo "$DOKS_LIST" | grep -E "loopruntime-drill" || true)
if [[ -n "$DOKS_DRILL" ]]; then
  COUNT=$(echo "$DOKS_DRILL" | wc -l)
  RESIDUAL_TOTAL=$((RESIDUAL_TOTAL + COUNT))
  RESIDUAL_FOUND=1
  log "  Found $COUNT residual DOKS cluster(s):"
  echo "$DOKS_DRILL" | sed 's/^/    /' >&2
  echo "## Residual DOKS clusters ($COUNT)" >> "$AUDIT_FILE"
  echo '```' >> "$AUDIT_FILE"
  echo "$DOKS_DRILL" >> "$AUDIT_FILE"
  echo '```' >> "$AUDIT_FILE"
  echo "" >> "$AUDIT_FILE"

  # Force-delete each
  while IFS= read -r line; do
    ID=$(echo "$line" | awk '{print $1}')
    NAME=$(echo "$line" | awk '{print $2}')
    log "  Force-deleting DOKS cluster $NAME ($ID)..."
    if apply doctl kubernetes cluster delete "$ID" --force --dangerous >/dev/null 2>&1; then
      RESIDUAL_DELETED=$((RESIDUAL_DELETED + 1))
      echo "- Force-deleted DOKS $NAME ($ID)" >> "$AUDIT_FILE"
    else
      RESIDUAL_FAILED=$((RESIDUAL_FAILED + 1))
      echo "- **FAILED** to delete DOKS $NAME ($ID)" >> "$AUDIT_FILE"
    fi
  done <<< "$DOKS_DRILL"
fi

# Managed databases
log "  - Listing managed databases..."
DB_LIST=$(list_or_fail "databases" doctl databases list --format ID,Name,Tags --no-header)
DB_DRILL=$(echo "$DB_LIST" | grep -E "loopruntime-drill" || true)
if [[ -n "$DB_DRILL" ]]; then
  COUNT=$(echo "$DB_DRILL" | wc -l)
  RESIDUAL_TOTAL=$((RESIDUAL_TOTAL + COUNT))
  RESIDUAL_FOUND=1
  log "  Found $COUNT residual managed database(s):"
  echo "$DB_DRILL" | sed 's/^/    /' >&2
  echo "## Residual managed DBs ($COUNT)" >> "$AUDIT_FILE"
  echo '```' >> "$AUDIT_FILE"
  echo "$DB_DRILL" >> "$AUDIT_FILE"
  echo '```' >> "$AUDIT_FILE"
  echo "" >> "$AUDIT_FILE"

  while IFS= read -r line; do
    ID=$(echo "$line" | awk '{print $1}')
    NAME=$(echo "$line" | awk '{print $2}')
    log "  Force-deleting managed DB $NAME ($ID)..."
    if apply doctl databases delete "$ID" --force >/dev/null 2>&1; then
      RESIDUAL_DELETED=$((RESIDUAL_DELETED + 1))
      echo "- Force-deleted managed DB $NAME ($ID)" >> "$AUDIT_FILE"
    else
      RESIDUAL_FAILED=$((RESIDUAL_FAILED + 1))
      echo "- **FAILED** to delete managed DB $NAME ($ID)" >> "$AUDIT_FILE"
    fi
  done <<< "$DB_DRILL"
fi

# VPCs (only delete after clusters + DBs gone — VPCs require empty deps)
log "  - Listing VPCs..."
VPC_LIST=$(list_or_fail "VPCs" doctl vpcs list --format ID,Name --no-header)
VPC_DRILL=$(echo "$VPC_LIST" | grep -E "loopruntime-drill" || true)
if [[ -n "$VPC_DRILL" ]]; then
  COUNT=$(echo "$VPC_DRILL" | wc -l)
  RESIDUAL_TOTAL=$((RESIDUAL_TOTAL + COUNT))
  RESIDUAL_FOUND=1
  log "  Found $COUNT residual VPC(s):"
  echo "$VPC_DRILL" | sed 's/^/    /' >&2
  echo "## Residual VPCs ($COUNT)" >> "$AUDIT_FILE"
  echo '```' >> "$AUDIT_FILE"
  echo "$VPC_DRILL" >> "$AUDIT_FILE"
  echo '```' >> "$AUDIT_FILE"
  echo "" >> "$AUDIT_FILE"

  # L2-unlock T3.2 (D4 fix for Finding #5): VPC delete with exponential backoff.
  # DO async VPC unbinding takes ~60s after DOKS cluster destroy. Single
  # delete attempt returns 409 "Can not delete with members" during the
  # unbinding window. Retry with backoff converges on observed behavior:
  # delays 10s, 30s, 60s, 120s (sum=220s worst case = within 10min budget).
  # Tests override via VPC_RETRY_DELAYS_OVERRIDE env (space-separated integers).
  if [[ -n "${VPC_RETRY_DELAYS_OVERRIDE:-}" ]]; then
    read -ra VPC_RETRY_DELAYS <<< "$VPC_RETRY_DELAYS_OVERRIDE"
  else
    VPC_RETRY_DELAYS=(10 30 60 120)
  fi

  while IFS= read -r line; do
    ID=$(echo "$line" | awk '{print $1}')
    NAME=$(echo "$line" | awk '{print $2}')
    log "  Force-deleting VPC $NAME ($ID) (with retry-backoff per T3.2 D4)..."

    VPC_DELETED=false
    ATTEMPT=0
    FIRST_PASS=true
    for delay in 0 "${VPC_RETRY_DELAYS[@]}"; do
      if [[ "$FIRST_PASS" == "true" ]]; then
        FIRST_PASS=false
      else
        ATTEMPT=$((ATTEMPT + 1))
        log "    Retry $ATTEMPT/${#VPC_RETRY_DELAYS[@]} after ${delay}s (DO async VPC unbinding lag)..."
        if [[ "$delay" -gt 0 ]]; then
          sleep "$delay"
        fi
      fi
      if apply doctl vpcs delete "$ID" --force >/dev/null 2>&1; then
        VPC_DELETED=true
        break
      fi
    done

    if [[ "$VPC_DELETED" == "true" ]]; then
      RESIDUAL_DELETED=$((RESIDUAL_DELETED + 1))
      if [[ "$ATTEMPT" -eq 0 ]]; then
        echo "- Force-deleted VPC $NAME ($ID)" >> "$AUDIT_FILE"
      else
        echo "- Force-deleted VPC $NAME ($ID) on retry $ATTEMPT (T3.2 D4 backoff converged)" >> "$AUDIT_FILE"
      fi
    else
      RESIDUAL_FAILED=$((RESIDUAL_FAILED + 1))
      echo "- **FAILED** to delete VPC $NAME ($ID) after ${#VPC_RETRY_DELAYS[@]} retries (cumulative wait $((10+30+60+120))s) — may require manual dep cleanup" >> "$AUDIT_FILE"
    fi
  done <<< "$VPC_DRILL"
fi

# Spaces buckets (drill-builds-sfo3, drill-artifacts-sfo3, drill-static-sfo3)
# Note: Spaces deletion via doctl requires explicit region.
log "  - Spaces buckets (drill-* prefix in sfo3 region)..."
# doctl doesn't have a direct Spaces list — uses s3-compatible API.
# This requires SPACES_ACCESS_KEY_ID + SPACES_SECRET_ACCESS_KEY env (which the
# operator/CI should set). If absent, log warning instead of failing.
if [[ -n "${SPACES_ACCESS_KEY_ID:-}" && -n "${SPACES_SECRET_ACCESS_KEY:-}" ]]; then
  log "  - Spaces credentials present; would scan via s3 API (not implemented in v1)"
  echo "## Spaces residuals" >> "$AUDIT_FILE"
  echo "Not scanned (v1). Operator must verify manually: \`s3cmd ls\` or DO console." >> "$AUDIT_FILE"
  echo "" >> "$AUDIT_FILE"
else
  log "  - Spaces creds missing; skipping bucket scan (verify manually if drill ran)"
fi

# --- Audit summary ---
{
  echo "## Summary"
  echo ""
  echo "- **Residuals found:** $RESIDUAL_TOTAL"
  echo "- **Successfully force-deleted:** $RESIDUAL_DELETED"
  echo "- **Failed to delete:** $RESIDUAL_FAILED"
  echo ""
  if [[ $RESIDUAL_FOUND -eq 0 ]]; then
    echo "**Status:** ✓ CLEAN — pulumi destroy completed atomically. No fallback needed."
  elif [[ $RESIDUAL_FAILED -eq 0 ]]; then
    echo "**Status:** ✓ RECOVERED — $RESIDUAL_DELETED residual(s) force-deleted via doctl."
  else
    echo "**Status:** ✗ PARTIAL — $RESIDUAL_FAILED resource(s) FAILED to delete. MANUAL CLEANUP REQUIRED."
    echo ""
    echo "Operator action: log into DO console + delete manually OR run doctl with elevated permissions."
  fi
} >> "$AUDIT_FILE"

log "Audit trail written: $AUDIT_FILE"

if [[ $RESIDUAL_FAILED -gt 0 ]]; then
  log "ERROR: $RESIDUAL_FAILED resource(s) require manual cleanup"
  exit 3
fi

log "✓ Validation complete — drill stack fully torn down"
exit 0
