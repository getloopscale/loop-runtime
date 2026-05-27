#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# T3.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md
# Enforces ADR 2026-05-12 hard deadline for cosign keyed → keyless cutover.
#
# Reads:
#   - ConfigMap loopzero-system/loopzero-cosign-config: mode + cutover_deadline keys
#   - Current date (UTC)
#
# Exit codes:
#   0 = OK (within window OR keyless mode active)
#   1 = BLOCK (past deadline AND still in keyed mode)
#   2 = ERROR (kubectl/parse error; operator must inspect)
#
# Emergency override (EC-7 of edge-case-review):
#   --emergency-override="<reason>" forces exit 0 + audit_log entry
#   Requires post-hoc Change Governor review within 24h.

set -euo pipefail

CONFIG_NS="${COSIGN_CONFIG_NS:-loopzero-system}"
CONFIG_NAME="${COSIGN_CONFIG_NAME:-loopzero-cosign-config}"
DRY_RUN="${DRY_RUN:-0}"

EMERGENCY_REASON=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --emergency-override=*)
      EMERGENCY_REASON="${1#--emergency-override=}"
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      cat <<USAGE
Usage: $0 [--emergency-override="<reason>"] [--dry-run]

Verifies the cosign mode against the keyless cutover deadline.

Exit codes:
  0 = OK
  1 = BLOCK (past deadline + keyed mode)
  2 = ERROR (config unreadable)

Environment:
  COSIGN_CONFIG_NS    ConfigMap namespace (default: loopzero-system)
  COSIGN_CONFIG_NAME  ConfigMap name (default: loopzero-cosign-config)
  DRY_RUN=1           Print intended action without changing state
USAGE
      exit 0
      ;;
    *)
      echo "ERROR: unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

log() {
  echo "[check-cosign-cutover-deadline] $*"
}

audit_log() {
  local action="$1"
  local reason="$2"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY-RUN: would write audit_log entry: action=$action reason='$reason'"
    return 0
  fi
  if ! command -v psql >/dev/null 2>&1; then
    log "WARN: psql not found; audit_log entry skipped (action=$action)"
    return 0
  fi
  local actor="${USER:-unknown}"
  psql -v ON_ERROR_STOP=1 -c "INSERT INTO audit_log (action, actor, target, metadata, timestamp) VALUES (
    '$action',
    '$actor',
    'cosign-cutover',
    '{\"reason\":\"$reason\",\"script\":\"check-cosign-cutover-deadline.sh\"}',
    NOW()
  )" >/dev/null 2>&1 || log "WARN: audit_log write failed (continuing)"
}

read_cm_field() {
  local key="$1"
  if [[ "$DRY_RUN" == "1" ]]; then
    case "$key" in
      mode) echo "keyed" ;;
      cutover_deadline) echo "2026-08-10T00:00:00Z" ;;
      *) echo "" ;;
    esac
    return 0
  fi
  kubectl get cm "$CONFIG_NAME" -n "$CONFIG_NS" \
    -o jsonpath="{.data.$key}" 2>/dev/null
}

# Emergency override path
if [[ -n "$EMERGENCY_REASON" ]]; then
  log "WARN: EMERGENCY OVERRIDE engaged. reason='$EMERGENCY_REASON'"
  log "WARN: post-hoc Change Governor review required within 24h"
  audit_log "cosign_deadline_override" "$EMERGENCY_REASON"
  exit 0
fi

# Read mode
MODE="$(read_cm_field mode || true)"
if [[ -z "$MODE" ]]; then
  log "ERROR: cannot read mode from ConfigMap $CONFIG_NS/$CONFIG_NAME"
  log "       Ensure ConfigMap exists and has data.mode key"
  exit 2
fi

# Keyless: no deadline check needed
if [[ "$MODE" == "keyless" ]]; then
  log "OK: mode=keyless (deadline check N/A)"
  exit 0
fi

if [[ "$MODE" != "keyed" ]]; then
  log "ERROR: unexpected mode=$MODE (expected 'keyed' or 'keyless')"
  exit 2
fi

# Read deadline
DEADLINE_STR="$(read_cm_field cutover_deadline || true)"
if [[ -z "$DEADLINE_STR" ]]; then
  log "ERROR: cannot read cutover_deadline from ConfigMap"
  exit 2
fi

# Compare epoch seconds (portable across GNU date + BSD date)
NOW_EPOCH=$(date -u +%s)
if ! DEADLINE_EPOCH=$(date -u -d "$DEADLINE_STR" +%s 2>/dev/null); then
  # macOS BSD date fallback
  if ! DEADLINE_EPOCH=$(date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$DEADLINE_STR" +%s 2>/dev/null); then
    log "ERROR: cannot parse cutover_deadline='$DEADLINE_STR'"
    log "       Expected ISO-8601 UTC format (e.g., 2026-08-10T00:00:00Z)"
    exit 2
  fi
fi

# Decide
if (( NOW_EPOCH > DEADLINE_EPOCH )); then
  DAYS_PAST=$(( (NOW_EPOCH - DEADLINE_EPOCH) / 86400 ))
  log "FAIL: mode=keyed AND deadline passed ${DAYS_PAST} days ago (deadline=$DEADLINE_STR)"
  log "      Migration to keyless mode required per ADR 2026-05-12"
  log "      Override (emergency only): $0 --emergency-override=\"<reason>\""
  exit 1
fi

DAYS_LEFT=$(( (DEADLINE_EPOCH - NOW_EPOCH) / 86400 ))
log "OK: mode=keyed, ${DAYS_LEFT} days remaining until cutover deadline ($DEADLINE_STR)"
exit 0
