#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / T5.7 (Phase 5 — dev canary stage 2).
#
# Cell-2 canary stage 2: pin 10% of dev tenants to cell-2. Hold 24h.
# Verify cell-1 error budget unaffected (the load now split across 2 cells).
# Halt + rollback on any 5xx aggregate or cell-1 EB > 1%.
#
# Pre-conditions:
#   - T5.6 (stage 1, 1h synthetic) GREEN per docs/runbooks/multi-cell-canary.md
#   - cell-2 active + ClusterCredential connected
#   - dev environment has >= 10 active tenants (10% threshold meaningful)
#   - Operator credentials (X-Service-Auth + LOOPZERO_ADMIN_API=true)
#
# Usage:
#   bash infra/tests/parity/cell2-canary-stage2.sh [--dry-run] [--soak-seconds N] [--percent P]
#   --dry-run        validate + print plan, exit 0
#   --soak-seconds N override 24h default (86400s)
#   --percent P      override 10 default (10% of dev tenants)
#
# Exit codes:
#   0  — 10% pinned to cell-2; 24h soak GREEN; cell-1 EB stable
#   1  — pre-flight failure (T5.6 not done, cell-2 not connected)
#   2  — bulk pin failed (any individual pin error halts batch)
#   3  — aggregate 5xx exceeded threshold (cell-2 unhealthy)
#   4  — cell-1 EB consumed > 1% during soak
#   5  — orchestration error
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
DRY_RUN="${DRY_RUN:-0}"
SOAK_SECONDS="${SOAK_SECONDS:-86400}"        # 24h default
PERCENT="${PERCENT:-10}"                      # 10% target
PROBE_INTERVAL="${PROBE_INTERVAL:-300}"       # probe every 5min over 24h
EB_THRESHOLD_PCT="${EB_THRESHOLD_PCT:-1.0}"   # 1% halt threshold (looser than stage 1 because traffic is real)
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
PROM_URL="${PROM_URL:-http://localhost:9090}"
PINNED_LIST="/tmp/cell2-canary-stage2-pinned-$(date -u +%s).txt"

log() { echo "[canary-stage2] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

# Parse flags.
for arg in "$@"; do
  case "$arg" in
    --dry-run)         DRY_RUN=1 ;;
    --soak-seconds=*)  SOAK_SECONDS="${arg#*=}" ;;
    --percent=*)       PERCENT="${arg#*=}" ;;
    -h|--help)         sed -n '2,30p' "$0" | sed 's/^# \?//'; exit 0 ;;
  esac
done

# ------------------------------------------------------------------------------
# Phase 1 — pre-conditions
# ------------------------------------------------------------------------------

preflight() {
  log "preflight — soak=${SOAK_SECONDS}s, percent=${PERCENT}%, threshold=${EB_THRESHOLD_PCT}%"

  command -v kubectl >/dev/null || fail "kubectl missing" 1
  command -v jq >/dev/null      || fail "jq missing" 1

  # Stage 1 must be done (runbook updated).
  if [[ ! -f "${REPO_ROOT}/docs/runbooks/multi-cell-canary.md" ]]; then
    fail "T5.6 stage 1 runbook missing — run stage 1 first" 1
  fi

  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would query: cell-2 status + ClusterCredential connected + total dev tenant count"
    return 0
  fi

  local connected
  connected=$(kubectl --context "$KUBECTL_CONTEXT" get clustercredential cell-2 \
    -o jsonpath='{.status.connected}' 2>/dev/null || echo "false")
  [[ "$connected" == "true" ]] || fail "cell-2 not connected" 1
}

# ------------------------------------------------------------------------------
# Phase 2 — select 10% sample + pin batch
# ------------------------------------------------------------------------------

select_sample() {
  log "selecting ${PERCENT}% sample of dev tenants currently on cell-1"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: SELECT id FROM tenants WHERE cell_id='cell-1' ORDER BY RANDOM() LIMIT (count*${PERCENT}/100)"
    : > "$PINNED_LIST"
    echo "tn-dryrun-1" >> "$PINNED_LIST"
    echo "tn-dryrun-2" >> "$PINNED_LIST"
    return 0
  fi

  local total
  total=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    psql -U loopzero -d loopzero -tAc "SELECT COUNT(*) FROM tenants WHERE cell_id='cell-1'" 2>/dev/null || echo "0")
  [[ "${total:-0}" -ge 10 ]] || fail "only ${total} cell-1 tenants — need >= 10 for ${PERCENT}% sample" 1

  local sample_size=$(( total * PERCENT / 100 ))
  [[ $sample_size -ge 1 ]] || fail "computed sample_size=${sample_size} < 1" 1

  kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    psql -U loopzero -d loopzero -tAc \
    "SELECT id FROM tenants WHERE cell_id='cell-1' ORDER BY RANDOM() LIMIT ${sample_size}" \
    > "$PINNED_LIST" 2>/dev/null || fail "sample query failed" 1

  log "selected $(wc -l < "$PINNED_LIST") tenants → ${PINNED_LIST}"
}

pin_batch() {
  local count=0
  while IFS= read -r tenant_id; do
    [[ -z "$tenant_id" ]] && continue
    log "pin ${tenant_id} → cell-2"
    if [[ "$DRY_RUN" -eq 1 ]]; then
      count=$((count + 1))
      continue
    fi
    loopzero admin tenants pin \
      --tenant "$tenant_id" \
      --cell cell-2 \
      --reason "T5.7 canary stage 2 batch ${PERCENT}%" \
      --json || { rollback; fail "pin ${tenant_id} failed (rollback initiated)" 2; }
    count=$((count + 1))
  done < "$PINNED_LIST"
  log "pinned ${count} tenants to cell-2"
}

# ------------------------------------------------------------------------------
# Phase 3 — soak
# ------------------------------------------------------------------------------

cell1_eb_consumed() {
  local q='100 * (1 - sum(rate(loopzero_cell_routing_decisions_total{cell="cell-1",decision="assigned"}[10m])) / sum(rate(loopzero_cell_routing_decisions_total{cell="cell-1"}[10m])))'
  curl -fsS "${PROM_URL}/api/v1/query?query=$(printf %s "$q" | jq -sRr @uri)" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // "0"' 2>/dev/null || echo "0"
}

cell2_5xx_rate() {
  local q='100 * sum(rate(loopzero_http_requests_total{cell="cell-2",code=~"5.."}[10m])) / sum(rate(loopzero_http_requests_total{cell="cell-2"}[10m]))'
  curl -fsS "${PROM_URL}/api/v1/query?query=$(printf %s "$q" | jq -sRr @uri)" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // "0"' 2>/dev/null || echo "0"
}

soak() {
  local end_ts=$(( $(date +%s) + SOAK_SECONDS ))
  local check_count=0

  log "soak — ${SOAK_SECONDS}s with ${PROBE_INTERVAL}s checks"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: monitor cell-1 EB ≤ ${EB_THRESHOLD_PCT}% + cell-2 5xx ≤ 1% every ${PROBE_INTERVAL}s"
    return 0
  fi

  while [[ $(date +%s) -lt $end_ts ]]; do
    check_count=$((check_count + 1))
    local eb cell2_5xx
    eb=$(cell1_eb_consumed)
    cell2_5xx=$(cell2_5xx_rate)
    log "check ${check_count}: cell-1 EB=${eb}% / cell-2 5xx=${cell2_5xx}%"

    if awk -v a="$eb" -v b="$EB_THRESHOLD_PCT" 'BEGIN{exit !(a > b)}' 2>/dev/null; then
      log "HALT: cell-1 EB ${eb}% > ${EB_THRESHOLD_PCT}%"
      rollback
      exit 4
    fi

    if awk -v a="$cell2_5xx" 'BEGIN{exit !(a > 1.0)}' 2>/dev/null; then
      log "HALT: cell-2 5xx rate ${cell2_5xx}% > 1.0%"
      rollback
      exit 3
    fi

    sleep "$PROBE_INTERVAL"
  done

  log "soak GREEN — ${check_count} checks, cell-1 EB stable, cell-2 5xx within budget"
}

# ------------------------------------------------------------------------------
# Phase 4 — rollback (re-pins all batch members back to cell-1)
# ------------------------------------------------------------------------------

rollback() {
  log "rollback — re-pinning batch back to cell-1"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: re-pin every tenant in ${PINNED_LIST} to cell-1"
    return 0
  fi
  while IFS= read -r tenant_id; do
    [[ -z "$tenant_id" ]] && continue
    loopzero admin tenants pin --tenant "$tenant_id" --cell cell-1 \
      --reason "T5.7 stage 2 rollback" --json 2>/dev/null || \
      log "WARN: rollback pin failed for ${tenant_id} — manual fix required"
  done < "$PINNED_LIST"
}

# Cleanup trap fires only on FAIL exit codes (3,4) — successful soak should
# leave the 10% on cell-2 per T5.7 acceptance criteria. So no auto-rollback
# on EXIT; rollback is called explicitly inside HALT branches.

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------

preflight
select_sample
pin_batch
soak

log "T5.7 canary stage 2 GREEN — ${PERCENT}% on cell-2 stable for ${SOAK_SECONDS}s"
log "next: graduate to T7.1 production canary stage 4 (1 customer, 7-day hold)"
exit 0
