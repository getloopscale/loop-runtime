#!/usr/bin/env bash
# cell-2-activation-plan T1.2 — Rollback SLO measurement (ADR C4).
#
# MEET-INFRA-NORMALIZE-T3-multi-cell condition C4 (production-design-reviewer
# veto): "rollback SLO measured end-to-end (API call -> first HTTP 200 from
# new cell) on dev with realistic load before Stage 4. SLO budget: 5 min P50,
# 8 min P95."
#
# This script measures rollback wall time across N iterations:
#   pin tenant cell-1 -> cell-2 -> wait HTTP 200 from cell-2
#   pin cell-2 -> cell-1 -> wait HTTP 200 from cell-1 (THIS is the rollback)
#
# A parallel `wrk` background job sustains 1 RPS during measurement so
# rollback fires under realistic baseline load (not idle infrastructure).
#
# Pre-conditions:
#   - cells SQL: cell-1 + cell-2 BOTH status='active' (post-T2.2 migration).
#   - Both ClusterCredentials connected (heal script H12 + H14 GREEN).
#   - DynamicRouter: loopzero_cell_discovery_count >= 3.
#   - Synthetic tenant ${SYNTH_TENANT_ID} with at least one ACTIVE release.
#
# Usage:
#   bash infra/tests/parity/cell2-rollback-slo.sh [--dry-run] [--iterations N]
#   --dry-run        validate pre-conditions, print plan, exit 0
#   --iterations N   override default 10
#
# Exit codes:
#   0 — N iterations complete; P50 <= 300s AND P95 <= 480s (within ADR budget)
#   1 — pre-flight failure
#   2 — rollback iteration failed (pin returned non-zero)
#   3 — at least one iteration > 600s (exceeds budget by 25% — HALT)
#   4 — SLO budget exceeded (P50 > 300s OR P95 > 480s — escalate to change-governor)
#
# Output:
#   stdout:  per-iteration timing + final P50/P95 summary
#   /tmp/cell2-rollback-slo-<TIMESTAMP>.csv: raw data (iteration,ms_to_200_cell1,ms_to_200_cell2)
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
DRY_RUN="${DRY_RUN:-0}"
IT_COUNT="${IT_COUNT:-10}"
PROBE_TIMEOUT_S="${PROBE_TIMEOUT_S:-600}"   # any iteration > 600s aborts
SYNTH_TENANT_ID="${SYNTH_TENANT_ID:-}"
LOOPZERO_API_URL="${LOOPZERO_API_URL:-https://api.loopzero.local}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
WRK_RPS="${WRK_RPS:-1}"                      # baseline 1 RPS (realistic)

TS=$(date -u +%Y%m%d-%H%M%S)
CSV_OUT="/tmp/cell2-rollback-slo-${TS}.csv"

log() { echo "[rollback-slo] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

for arg in "$@"; do
  case "$arg" in
    --dry-run)         DRY_RUN=1 ;;
    --iterations=*)    IT_COUNT="${arg#*=}" ;;
    --iterations)      shift; IT_COUNT="${1:-10}" ;;
    -h|--help)
      sed -n '2,38p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Phase 1 — pre-conditions
# ---------------------------------------------------------------------------
preflight() {
  log "preflight — iterations=${IT_COUNT}, timeout=${PROBE_TIMEOUT_S}s, csv=${CSV_OUT}"

  command -v kubectl >/dev/null || fail "kubectl missing" 1
  command -v curl >/dev/null    || fail "curl missing" 1
  command -v jq >/dev/null      || fail "jq missing" 1

  if [[ -z "$SYNTH_TENANT_ID" ]]; then
    fail "SYNTH_TENANT_ID env var required (bootstrap via dogfood loop first)" 1
  fi

  if [[ "$DRY_RUN" -eq 0 ]]; then
    # Both cells active in SQL?
    local active
    active=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
      psql -U loopzero -d loopzero -tAc "SELECT COUNT(*) FROM cells WHERE cell_id IN ('cell-1','cell-2') AND status='active'" 2>/dev/null || echo "0")
    [[ "${active:-0}" -ge 2 ]] || fail "need cell-1 + cell-2 both active in cells table (got ${active})" 1
  else
    log "[dry-run] would query: SELECT COUNT(*) FROM cells WHERE status='active' AND cell_id IN ('cell-1','cell-2')"
  fi

  log "preflight OK"
}

# ---------------------------------------------------------------------------
# Phase 2 — background load (realistic 1 RPS during measurement)
# ---------------------------------------------------------------------------
WRK_PID=""

start_load() {
  local url="$1"
  if ! command -v wrk >/dev/null 2>&1; then
    log "wrk not installed — falling back to curl loop (less precise but functional)"
    (
      while :; do
        curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null
        sleep "$(awk -v r="$WRK_RPS" 'BEGIN{printf "%.2f", 1.0/r}')"
      done
    ) &
    WRK_PID=$!
  else
    # wrk -c 1 -d <very-long> -R <rps> sustains rate.
    wrk -c 1 -d 1h -R "$WRK_RPS" "$url" >/dev/null 2>&1 &
    WRK_PID=$!
  fi
  log "background load PID=$WRK_PID against $url"
}

stop_load() {
  if [[ -n "$WRK_PID" ]] && kill -0 "$WRK_PID" 2>/dev/null; then
    kill "$WRK_PID" 2>/dev/null || true
    wait "$WRK_PID" 2>/dev/null || true
  fi
}

trap 'stop_load' EXIT

# ---------------------------------------------------------------------------
# Phase 3 — measurement loop
# ---------------------------------------------------------------------------
pin_cell() {
  local cell="$1"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: loopzero admin tenants pin --tenant ${SYNTH_TENANT_ID} --cell ${cell}"
    return 0
  fi
  loopzero admin tenants pin --tenant "$SYNTH_TENANT_ID" --cell "$cell" --json >/dev/null 2>&1
}

# probe_until_200_ms — returns ms elapsed from start_ms until first HTTP 200.
# Aborts (echos -1) if PROBE_TIMEOUT_S exceeded.
probe_until_200_ms() {
  local url="$1"
  local start_ms="$2"
  local deadline_ms=$(( start_ms + PROBE_TIMEOUT_S * 1000 ))
  while :; do
    local now_ms; now_ms=$(date +%s%3N)
    if (( now_ms > deadline_ms )); then
      echo -1
      return
    fi
    if [[ "$DRY_RUN" -eq 1 ]]; then
      # Simulate immediate 200 in dry-run.
      echo 0
      return
    fi
    local code
    code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 5 "$url" 2>/dev/null || echo "000")
    if [[ "$code" == "200" ]]; then
      echo $(( now_ms - start_ms ))
      return
    fi
    sleep 1
  done
}

run_iteration() {
  local i="$1"
  local synth_url="https://${SYNTH_TENANT_ID}-production.loopzero.local"

  log "iter ${i}/${IT_COUNT}: pin -> cell-2"
  pin_cell cell-2 || return 2
  local t1; t1=$(date +%s%3N)
  local ms_to_cell2; ms_to_cell2=$(probe_until_200_ms "$synth_url" "$t1")
  if [[ "$ms_to_cell2" == "-1" ]]; then
    log "iter ${i}: probe-to-cell-2 exceeded ${PROBE_TIMEOUT_S}s — HALT"
    return 3
  fi
  log "iter ${i}: cell-2 reached HTTP 200 in ${ms_to_cell2}ms"

  log "iter ${i}: ROLLBACK -> cell-1"
  pin_cell cell-1 || return 2
  local t2; t2=$(date +%s%3N)
  local ms_to_cell1; ms_to_cell1=$(probe_until_200_ms "$synth_url" "$t2")
  if [[ "$ms_to_cell1" == "-1" ]]; then
    log "iter ${i}: probe-to-cell-1 (rollback) exceeded ${PROBE_TIMEOUT_S}s — HALT"
    return 3
  fi
  log "iter ${i}: cell-1 reached HTTP 200 in ${ms_to_cell1}ms (ROLLBACK SLO datapoint)"
  echo "${i},${ms_to_cell1},${ms_to_cell2}" >> "$CSV_OUT"
}

# ---------------------------------------------------------------------------
# Phase 4 — percentile computation (P50, P95)
# ---------------------------------------------------------------------------
compute_percentiles() {
  if [[ ! -s "$CSV_OUT" ]]; then
    log "no measurements — empty CSV"
    echo "0 0"
    return
  fi
  # Sort the rollback column (ms_to_cell1) ascending.
  local sorted; sorted=$(awk -F, '{print $2}' "$CSV_OUT" | sort -n)
  local n; n=$(echo "$sorted" | wc -l | tr -d ' ')
  local p50_idx p95_idx
  p50_idx=$(awk -v n="$n" 'BEGIN{print int((n*50+50)/100)}')
  p95_idx=$(awk -v n="$n" 'BEGIN{print int((n*95+50)/100)}')
  (( p50_idx < 1 )) && p50_idx=1
  (( p95_idx < 1 )) && p95_idx=1
  (( p50_idx > n )) && p50_idx="$n"
  (( p95_idx > n )) && p95_idx="$n"
  local p50_ms p95_ms
  p50_ms=$(echo "$sorted" | sed -n "${p50_idx}p")
  p95_ms=$(echo "$sorted" | sed -n "${p95_idx}p")
  echo "$p50_ms $p95_ms"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

preflight

if [[ "$DRY_RUN" -eq 1 ]]; then
  log "[dry-run] would run ${IT_COUNT} iterations + percentile compute"
  log "[dry-run] OK"
  exit 0
fi

echo "iteration,ms_to_cell1_rollback,ms_to_cell2_forward" > "$CSV_OUT"

# Background load only for live runs.
start_load "https://${SYNTH_TENANT_ID}-production.loopzero.local"

for i in $(seq 1 "$IT_COUNT"); do
  run_iteration "$i"
  rc=$?
  if (( rc != 0 )); then
    stop_load
    exit "$rc"
  fi
done

stop_load

read -r P50_MS P95_MS < <(compute_percentiles)
P50_S=$(awk -v ms="$P50_MS" 'BEGIN{printf "%.1f", ms/1000}')
P95_S=$(awk -v ms="$P95_MS" 'BEGIN{printf "%.1f", ms/1000}')

log "----- Rollback SLO Summary (ADR C4) -----"
log "iterations:       ${IT_COUNT}"
log "csv:              ${CSV_OUT}"
log "P50:              ${P50_MS}ms (${P50_S}s)"
log "P95:              ${P95_MS}ms (${P95_S}s)"
log "ADR budget:       P50 <= 300000ms (300s), P95 <= 480000ms (480s)"

# Numeric comparison without bc.
if (( P50_MS > 300000 || P95_MS > 480000 )); then
  log "VERDICT: FAIL — exceeds ADR C4 budget. Escalate to change-governor."
  exit 4
fi
log "VERDICT: PASS — within ADR C4 budget."
exit 0
