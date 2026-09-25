#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

# Set before the header below, not after it: this is a safety directive and the
# header is documentation. The `shell-hygiene` gate requires it inside the top 60
# lines, and on 2026-09-05 a correction added to that header pushed it to line 63
# and failed the chain. A long explanation should never be able to disarm
# `errexit`, whichever way the gate happens to be written.
#
# Two words were missing from this comment for a few minutes, eaten by backticks:
# it was written through `ssh host "... <<'QUOTED' ..."`, and the outer double
# quotes are interpreted by the LOCAL shell before the remote heredoc ever
# protects anything. Long text goes in a file that is copied over, never in a
# command line.
set -euo pipefail
#
# T3.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md
# Enforces ADR 2026-05-12 hard deadline for cosign keyed → keyless cutover.
#
# Reads:
#   - ConfigMap loopzero-system/loopzero-cosign-config: mode + cutover_deadline keys
#   - Current date (UTC)
#
# THAT CONFIGMAP DOES NOT EXIST, AND THIS GUARD HAS THEREFORE NEVER EVALUATED.
#
# CORRECTION, 2026-09-05: "therefore" was wrong, and the paragraph below is only
# half the reason. The guard never reached the mode read at all — the v3
# pre-check further down exited 1 first, on a DIFFERENT ConfigMap
# (loopzero-cosign-version-config), because it treated any non-empty target_version
# as an upgrade in progress and demanded a companion set_at. The chart ships
# target_version=v2.4.3 with set_at omitted and documents that as the resting
# state, so the guard refused its own shipped default and named the version
# tracker in the failure.
#
# Anyone following the note as first written would have provisioned the ConfigMap
# named below, watched the guard fail anyway, and been pointed at a third
# artifact. Fixed the same day (see the V3_BASELINE_VERSION block); the guard now
# falls through to exit 2 here, which is the loud missing-input failure this note
# describes.
# Measured 2026-08-18, re-measured 2026-08-19 and 2026-09-04 against `loopzero-core`:
# `kubectl -n loopzero-system get cm loopzero-cosign-config` -> NotFound. Nothing in this
# repository creates it — `grep -rn 'name: loopzero-cosign-config' --include='*.yaml'`
# returns nothing, and no script applies or patches it. Four places READ the name
# (this script, infra/scripts/local/baseline-capture.sh:126, and the alert
# remediation text in both copies of sigstore-alerts.yaml:105); none writes it.
#
# It is absent rather than pending, and the reason is that creating it is not an
# edit. The block condition below — deadline passed AND still keyed — is TRUE as
# of 2026-08-10:
#   * ADR-2026-05-12 + 90d = 2026-08-10, the same date this script carries as its
#     dry-run stub at line ~93;
#   * the live loopzero-core deployment sets no LOOPZERO_COSIGN_KEYLESS, and config.go:109
#     documents the `false` default as keyed.
# So the first `kubectl apply` that creates this ConfigMap makes this guard start
# blocking immediately. That is correct behaviour for an expired deadline and it
# is a production-affecting change, which makes it the cutover owner's call:
# either do the keyed->keyless cutover and provision with mode: keyless, or amend
# the deadline in ADR-2026-05-12 with a new date and a reason and provision with
# that. Tracked as issue #348. Do not provision it as a side effect of unrelated
# work — the point of writing this here is that the absence is a held decision,
# not an oversight someone should quietly fix.
#
# Registered by B-146, whose DoD offers exactly this alternative: provision it
# declaratively, or write down why it is not provisioned.
#
# Exit codes:
#   0 = OK (within window OR keyless mode active)
#   1 = BLOCK (past deadline AND still in keyed mode)
#   2 = ERROR (kubectl/parse error; operator must inspect)
#
# Emergency override (EC-7 of edge-case-review):
#   --emergency-override="<reason>" exits 0 once the audit_log row is written, 2 if it cannot be
#   Requires post-hoc Change Governor review within 24h.


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

# audit_log writes one audit_log row and returns non-zero when it could not.
# Every value reaches psql as a variable read through :'name' quoting, never
# spliced into the statement, so operator text cannot break or extend it.
audit_log() {
  local action="$1"
  local reason="$2"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY-RUN: would write audit_log entry: action=$action reason='$reason'"
    return 0
  fi
  if ! command -v psql >/dev/null 2>&1; then
    log "ERROR: psql not found; cannot write the audit_log entry (action=$action)"
    return 1
  fi
  local actor="${USER:-unknown}"
  if ! psql -v ON_ERROR_STOP=1 -v "action=$action" -v "actor=$actor" -v "reason=$reason" >/dev/null <<'SQL'
INSERT INTO audit_log (action, actor, target, metadata, timestamp) VALUES (
  :'action',
  :'actor',
  'cosign-cutover',
  json_build_object('reason', :'reason', 'script', 'check-cosign-cutover-deadline.sh'),
  NOW()
);
SQL
  then
    log "ERROR: audit_log write failed (action=$action)"
    return 1
  fi
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
  # The audit row is the only trace of this bypass: no row, no bypass.
  if ! audit_log "cosign_deadline_override" "$EMERGENCY_REASON"; then
    log "ERROR: emergency override REFUSED — the bypass could not be recorded"
    exit 2
  fi
  exit 0
fi

# --- T0.2 of plan 2026-05-28-meet-001-engineering-closure v1.1: cosign v3
# upgrade deadline check (ADR-2026-05-28-cosign-v2.4.3-deadline). Runs BEFORE
# mode check because v3 upgrade is orthogonal to keyed/keyless mode (the v3
# upgrade lock-in concern stands even when keyless mode is active).
# Reads ConfigMap `loopzero-cosign-version-config` with REQUIRED fields:
#   - target_version (e.g., "v3"). Absent → PASS (no upgrade pending).
#   - set_at (YYYY-MM-DD). REQUIRED when target_version=v3.
# EC-1 fix: set_at is mandatory to prevent operator rewriting the ConfigMap
# to reset the clock.
V3_CM_NS="${COSIGN_V3_CONFIG_NS:-loopzero-system}"
V3_CM_NAME="${COSIGN_V3_CONFIG_NAME:-loopzero-cosign-version-config}"
# The version the chart ships as its default, meaning "no upgrade pending".
# charts/loopzero-admission-policies/templates/cosign-version-config.yaml emits
# target_version=v2.4.3 with set_at omitted and documents that exact state as
# the resting one, requiring set_at only when targetVersion != v2.4.3.
V3_BASELINE_VERSION="${COSIGN_V3_BASELINE_VERSION:-v2.4.3}"

v3_read_field() {
  local key="$1"
  if [[ "$DRY_RUN" == "1" ]]; then echo ""; return 0; fi
  kubectl get cm "$V3_CM_NAME" -n "$V3_CM_NS" \
    -o jsonpath="{.data.$key}" 2>/dev/null
}

V3_TARGET="$(v3_read_field target_version || true)"
# A pending upgrade is a target that DIFFERS from the shipped baseline. Treating
# any non-empty target_version as pending made the chart's own default state
# (target_version=v2.4.3, no set_at) exit 1 here, before the keyed/keyless logic
# below ever ran — so the guard blocked on the version tracker instead of
# reporting on the cutover it exists to enforce. Measured 2026-09-05.
if [[ -n "$V3_TARGET" && "$V3_TARGET" != "$V3_BASELINE_VERSION" ]]; then
  V3_SET_AT="$(v3_read_field set_at || true)"
  if [[ -z "$V3_SET_AT" ]]; then
    log "FAIL: ConfigMap $V3_CM_NS/$V3_CM_NAME has target_version=$V3_TARGET but missing required 'set_at' field; refusing ambiguous deadline (ADR-2026-05-28-cosign-v2.4.3-deadline)"
    exit 1
  fi
  # Parse set_at as epoch
  if ! V3_SET_AT_EPOCH=$(date -u -d "$V3_SET_AT" +%s 2>/dev/null); then
    if ! V3_SET_AT_EPOCH=$(date -u -j -f "%Y-%m-%d" "$V3_SET_AT" +%s 2>/dev/null); then
      log "FAIL: cannot parse set_at='$V3_SET_AT' (expected YYYY-MM-DD)"
      exit 1
    fi
  fi
  NOW_EPOCH_V3=$(date -u +%s)
  AGE_DAYS=$(( (NOW_EPOCH_V3 - V3_SET_AT_EPOCH) / 86400 ))
  if (( AGE_DAYS > 90 )); then
    log "FAIL: cosign v3 upgrade deadline T0+90d crossed (target=$V3_TARGET, set_at=$V3_SET_AT, age=${AGE_DAYS}d)"
    log "      Operator must complete cosign v2.4.3 → v3 upgrade per ADR-2026-05-28-cosign-v2.4.3-deadline"
    exit 1
  elif (( AGE_DAYS > 60 )); then
    REMAINING_DAYS=$((90 - AGE_DAYS))
    log "WARN: cosign v3 upgrade deadline T0+60d crossed (target=$V3_TARGET, set_at=$V3_SET_AT, age=${AGE_DAYS}d, ${REMAINING_DAYS}d to T0+90d FAIL)" >&2
  fi
fi
# --- end v3 deadline check ---

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

# T2.2 progressive warnings as cutover approaches. Operator should
# trigger Sigstore Scaffolding provisioning (infra/pulumi-platform/src/services/sigstore.ts)
# at T-30d so the 16h engineering window + soak completes before deadline.
if (( DAYS_LEFT <= 7 )); then
  log "URGENT: mode=keyed, only ${DAYS_LEFT} days remaining until cutover deadline ($DEADLINE_STR)"
  log "        Sigstore provisioning MUST start NOW or cosign signing will FAIL CLOSED"
  log "        Enable infra/pulumi-platform/src/services/sigstore.ts (T2.2)"
elif (( DAYS_LEFT <= 15 )); then
  log "WARN: mode=keyed, ${DAYS_LEFT} days remaining until cutover deadline ($DEADLINE_STR)"
  log "      T-15d: confirm sponsor approved T2.2 Sigstore Scaffolding deployment"
elif (( DAYS_LEFT <= 30 )); then
  log "NOTICE: mode=keyed, ${DAYS_LEFT} days remaining until cutover deadline ($DEADLINE_STR)"
  log "        T-30d: time to schedule T2.2 Change Governor meeting"
else
  log "OK: mode=keyed, ${DAYS_LEFT} days remaining until cutover deadline ($DEADLINE_STR)"
fi
exit 0
