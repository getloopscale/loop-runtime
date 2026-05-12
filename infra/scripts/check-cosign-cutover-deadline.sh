#!/usr/bin/env bash
# check-cosign-cutover-deadline.sh
#
# Audit gap §6.B.4 / `decisions/2026-05-12-cosign-keyless-v11-cutover.md`:
# the cosign keyless v1.1 cutover ADR carries a hard 90-day deadline from
# v1.0 GA. The ADR claims "auto-blocks via W1.14 hook", but W1.14 only
# protects against ADR deletion — it does NOT track the deadline. This
# script closes that gap.
#
# Behavior:
#   - Reads the v1.0 GA date from `.github/v1.0-ga-date.txt` (one line,
#     ISO 8601 like "2026-06-15"). If the file does not exist (pre-GA),
#     the check is DORMANT and exits 0 silently.
#   - Computes days_since_ga = today - v1.0_ga_date.
#   - day <= 75: silent OK, exit 0
#   - 75 < day <= 90: warn (writes a GH annotation if running in CI),
#                      exit 0. Per ADR: "T0+75d sponsor exception"
#   - day > 90:    error, exit 2. Per ADR: "T0+90d auto-block"
#                      UNLESS a sponsor-exception file exists at
#                      `.github/cosign-cutover-exception.txt` whose
#                      content explicitly approves the slip with an
#                      operator signature line.
#
# Exit codes:
#   0  pre-GA OR within budget OR slip-approved-via-exception
#   1  usage error (date parse failure, etc.)
#   2  deadline blown without sponsor exception
#
# Usage:
#   ./check-cosign-cutover-deadline.sh
#   ./check-cosign-cutover-deadline.sh --as-of 2026-09-15  # for testing

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GA_MARKER="$REPO_ROOT/.github/v1.0-ga-date.txt"
EXCEPTION_FILE="$REPO_ROOT/.github/cosign-cutover-exception.txt"

# Test hook: --as-of <YYYY-MM-DD> overrides the "today" reference.
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

# DORMANT mode: pre-GA, marker file does not exist.
if [[ ! -f "$GA_MARKER" ]]; then
  echo "[cosign-cutover] pre-GA — marker $GA_MARKER absent. Check is dormant."
  exit 0
fi

ga_date_raw=$(head -1 "$GA_MARKER" | tr -d '[:space:]')
if [[ -z "$ga_date_raw" ]]; then
  echo "::error::$GA_MARKER exists but is empty"
  exit 1
fi

ga_epoch=$(date -d "$ga_date_raw" +%s 2>/dev/null) || {
  echo "::error::cannot parse v1.0 GA date '$ga_date_raw' in $GA_MARKER"
  exit 1
}

now=$(today_epoch)
days=$(( (now - ga_epoch) / 86400 ))

echo "[cosign-cutover] v1.0 GA = $ga_date_raw; days since = $days"

if [[ $days -le 75 ]]; then
  echo "[cosign-cutover] within budget (day $days of 90) — OK."
  exit 0
fi

if [[ $days -le 90 ]]; then
  msg="cosign keyless cutover slip-alert: day $days of 90 (deadline approaching). Per ADR 2026-05-12-cosign-keyless-v11-cutover.md the T0+75d sponsor-exception window has opened."
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::warning::$msg"
  else
    echo "[cosign-cutover] WARN: $msg" >&2
  fi
  exit 0
fi

# day > 90 — DEADLINE BLOWN. Check for sponsor exception.
if [[ -f "$EXCEPTION_FILE" ]]; then
  exception_signed=$(grep -c '^approved-by: ' "$EXCEPTION_FILE" 2>/dev/null || echo 0)
  if [[ $exception_signed -gt 0 ]]; then
    msg="cosign keyless cutover deadline (90d) BLOWN at day $days — but sponsor exception present at $EXCEPTION_FILE. Permitting build to proceed."
    if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
      echo "::warning::$msg"
    else
      echo "[cosign-cutover] WARN (exception-permitted): $msg" >&2
    fi
    exit 0
  fi
fi

msg="cosign keyless cutover DEADLINE BLOWN at day $days (>90 days post v1.0 GA). No sponsor exception found at $EXCEPTION_FILE. Per ADR 2026-05-12-cosign-keyless-v11-cutover.md this auto-blocks. To unblock: (a) execute the cutover and update the ADR status, OR (b) create $EXCEPTION_FILE with an 'approved-by: <name>' line carrying an explicit slip rationale."
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
  echo "::error::$msg"
else
  echo "[cosign-cutover] BLOCK: $msg" >&2
fi
exit 2
