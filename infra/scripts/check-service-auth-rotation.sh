#!/usr/bin/env bash
# check-service-auth-rotation.sh
#
# ADR: decisions/2026-05-13-service-auth-ed25519-adoption.md § Rotation
# (Q4 decision: 90-day rotation cadence, mirroring §6.B.4 cosign keyless
# cutover ratchet).
#
# The cloud↔engine service-auth Ed25519 keypair must rotate at most
# every 90 days. This script tracks the LAST rotation date (stored in
# `.github/service-auth-ed25519-rotation-date.txt`, one ISO 8601 line)
# and ratchets:
#
#   - day <= 60: silent OK, exit 0 (in budget)
#   - 60 < day <= 90: warn (CI annotation), exit 0 (slip-alert window)
#   - day > 90: error, exit 2 (auto-block) UNLESS a sponsor exception
#               file exists at `.github/service-auth-rotation-exception.txt`
#               with an explicit `approved-by: <signature>` line.
#
# Behavior parity with check-cosign-cutover-deadline.sh — operator only
# needs to learn ONE ratchet pattern. Differences:
#   * Marker file name (rotation-date vs ga-date)
#   * Exception file name (matches rotation-date)
#   * Warn boundary is 60d (not 75d) — 90d rotation is shorter than
#     90d cutover, so the slip-alert window is symmetrically scaled.
#
# Exit codes:
#   0  pre-rotation-tracked OR within budget OR slip-approved
#   1  usage error (date parse failure, marker malformed)
#   2  deadline blown without sponsor exception
#
# Usage:
#   ./check-service-auth-rotation.sh
#   ./check-service-auth-rotation.sh --as-of 2026-09-15  # testing

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ROTATION_MARKER="$REPO_ROOT/.github/service-auth-ed25519-rotation-date.txt"
EXCEPTION_FILE="$REPO_ROOT/.github/service-auth-rotation-exception.txt"

AS_OF=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --as-of)
      AS_OF="$2"; shift 2
      ;;
    --as-of=*)
      AS_OF="${1#*=}"; shift
      ;;
    *)
      echo "usage: $0 [--as-of YYYY-MM-DD]" >&2
      exit 1
      ;;
  esac
done

today_epoch() {
  if [[ -n "$AS_OF" ]]; then
    date -d "$AS_OF" +%s 2>/dev/null || { echo "::error::invalid --as-of date: $AS_OF" >&2; exit 1; }
  else
    date +%s
  fi
}

# Pre-tracked mode: marker does not exist (rotation has not begun OR
# stack is dev-only). Dormant — exit 0 with informational note.
if [[ ! -f "$ROTATION_MARKER" ]]; then
  echo "[service-auth-rotation] no rotation marker at $ROTATION_MARKER — check is dormant. First rotation should create this file."
  exit 0
fi

rotation_date_raw=$(head -1 "$ROTATION_MARKER" | tr -d '[:space:]')
if [[ -z "$rotation_date_raw" ]]; then
  echo "::error::$ROTATION_MARKER exists but is empty"
  exit 1
fi

rotation_epoch=$(date -d "$rotation_date_raw" +%s 2>/dev/null) || {
  echo "::error::cannot parse rotation date '$rotation_date_raw' in $ROTATION_MARKER"
  exit 1
}

now=$(today_epoch)
days=$(( (now - rotation_epoch) / 86400 ))

echo "[service-auth-rotation] last rotation = $rotation_date_raw; days since = $days"

if [[ $days -le 60 ]]; then
  echo "[service-auth-rotation] within budget (day $days of 90) — OK."
  exit 0
fi

if [[ $days -le 90 ]]; then
  msg="service-auth Ed25519 rotation slip-alert: day $days of 90 (deadline approaching). Per ADR 2026-05-13-service-auth-ed25519-adoption.md the T0+60d slip-alert window has opened. Operator: generate a new keypair + pulumi config set + pulumi up; the rotation script's --record flag will refresh $ROTATION_MARKER."
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::warning::$msg"
  else
    echo "[service-auth-rotation] WARN: $msg" >&2
  fi
  exit 0
fi

# day > 90 — DEADLINE BLOWN. Check for sponsor exception.
if [[ -f "$EXCEPTION_FILE" ]]; then
  exception_signed=$(grep -c '^approved-by: ' "$EXCEPTION_FILE" 2>/dev/null || echo 0)
  if [[ $exception_signed -gt 0 ]]; then
    msg="service-auth Ed25519 rotation DEADLINE (90d) BLOWN at day $days — but sponsor exception present at $EXCEPTION_FILE. Permitting build to proceed."
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
      echo "::warning::$msg"
    else
      echo "[service-auth-rotation] WARN (exception-permitted): $msg" >&2
    fi
    exit 0
  fi
fi

msg="service-auth Ed25519 rotation DEADLINE BLOWN at day $days (>90 days). No sponsor exception found at $EXCEPTION_FILE. Per ADR 2026-05-13-service-auth-ed25519-adoption.md this auto-blocks. To unblock: (a) execute the rotation per the runbook and update $ROTATION_MARKER to today, OR (b) create $EXCEPTION_FILE with an 'approved-by: <name>' line carrying an explicit slip rationale."
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
  echo "::error::$msg"
else
  echo "[service-auth-rotation] BLOCK: $msg" >&2
fi
exit 2
