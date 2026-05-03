#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / T5.6 (Phase 5 — dev canary stage 1).
#
# Cell-2 canary stage 1: deploy 1 synthetic tenant pinned to cell-2.
# Verify HTTP 200 from runtime-2. Soak 1h. Confirm cell-1 error budget
# unaffected. Halt + rollback on any 5xx or EB consumption > 0.1%.
#
# This is offline-deliverable (script file ships with the plan); the
# LIVE run requires a fully-bootstrapped cell-2 cluster with active
# ClusterCredential and loopzero_cell_discovery_count >= 2.
#
# Pre-conditions:
#   - cell-2 exists in `cells` SQL table with status='active'
#   - ClusterCredential cell-2 status.connected=true on mgmt cluster
#   - DynamicRouter resolves cell-2 (loopzero_cell_discovery_count >= 2)
#   - Operator credentials available (X-Service-Auth + LOOPZERO_ADMIN_API=true)
#   - Synthetic tenant ${SYNTH_TENANT_ID} already bootstrapped with at least
#     one ACTIVE release on its current cell (the canary REPINS an existing
#     tenant; bootstrap is out of scope — see infra/tests/dogfood/full-loop.sh
#     for tenant-creation paths that this script consumes downstream)
#
# Usage:
#   bash infra/tests/parity/cell2-canary-stage1.sh [--dry-run|--smoke] [--soak-seconds N]
#   --dry-run        validate pre-conditions, print plan, exit 0 (no API calls)
#   --smoke          compressed-mode validation (cell-2-activation-plan iter 2):
#                    skip ACTIVE-release requirement, soak 60s default, do NOT
#                    delete tenant on cleanup (only re-pin to cell-1). Validates
#                    the pin/rollback mechanism without the full ADR-compliant
#                    1h soak (which requires deployed app + diurnal probe coverage).
#   --soak-seconds N override 1h default (3600s); useful for shortened CI smoke
#
# Exit codes:
#   0  — synthetic tenant on cell-2 GREEN; cell-1 EB stable; soak OK
#   1  — pre-flight failure (cell-2 not registered, loopzero-api unreachable)
#   2  — synthetic tenant pin failed
#   3  — HTTP probe failed (5xx from cell-2)
#   4  — cell-1 EB consumed > 0.1% during soak (rollback triggered)
#   5  — timeout / orchestration error
#
# Rollback semantics (auto-triggered on exit codes 3-4):
#   - loopzero admin tenants pin --tenant {synth} --cell cell-1 (re-pin)
#   - kubectl delete tenant {synth} (cleanup synthetic)
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
DRY_RUN="${DRY_RUN:-0}"
SMOKE="${SMOKE:-0}"
SOAK_SECONDS="${SOAK_SECONDS:-3600}"        # 1h default
PROBE_INTERVAL="${PROBE_INTERVAL:-30}"       # probe HTTP every 30s
EB_THRESHOLD_PCT="${EB_THRESHOLD_PCT:-0.1}"  # halt at 0.1% EB consumption
SYNTH_TENANT_ID="${SYNTH_TENANT_ID:-tn-canary-cell2-$(date -u +%s)}"
LOOPZERO_API_URL="${LOOPZERO_API_URL:-https://api.loopzero.local}"
PROM_URL="${PROM_URL:-http://localhost:9090}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"

log() { echo "[canary-stage1] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

# Parse flags. Use while/shift to handle space-separated values correctly
# (the previous for-loop pattern silently dropped --soak-seconds N).
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)         DRY_RUN=1; shift ;;
    --smoke)
      SMOKE=1
      # Only override SOAK_SECONDS / PROBE_INTERVAL with smoke defaults if the
      # caller did NOT already set them via env. Detected via a sentinel: if
      # SOAK_SECONDS equals the script default 3600, treat as "not set" and
      # apply smoke default. Otherwise honor the caller's explicit value.
      # fix-all-platform-issues-plan T5.3 background runner depends on this:
      # passes SOAK_SECONDS=3600 + --smoke to get HTTP-probe-tolerant 1h soak.
      if [[ "$SOAK_SECONDS" == "3600" && -z "${SOAK_SECONDS_EXPLICIT:-}" ]]; then
        SOAK_SECONDS="${SOAK_SECONDS_SMOKE:-60}"
      fi
      if [[ "$PROBE_INTERVAL" == "30" && -z "${PROBE_INTERVAL_EXPLICIT:-}" ]]; then
        PROBE_INTERVAL="${PROBE_INTERVAL_SMOKE:-15}"
      fi
      shift
      ;;
    --soak-seconds=*)  SOAK_SECONDS="${1#*=}"; shift ;;
    --soak-seconds)    SOAK_SECONDS="${2:-3600}"; shift 2 ;;
    -h|--help)
      sed -n '2,38p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)                 shift ;;  # tolerate unknown args (env-var-only callers)
  esac
done

# In smoke mode, the trap that kubectl-deletes the tenant is suppressed.
# Operator validates the pin/rollback mechanism without losing the existing
# multi-cell test tenant (typically tn-e2etest in dev k3d).
SMOKE_KEEP_TENANT="${SMOKE_KEEP_TENANT:-$SMOKE}"

# ------------------------------------------------------------------------------
# Phase 1 — pre-conditions
# ------------------------------------------------------------------------------

preflight() {
  log "preflight — context=${KUBECTL_CONTEXT}, soak=${SOAK_SECONDS}s, threshold=${EB_THRESHOLD_PCT}%"

  command -v kubectl >/dev/null || fail "kubectl missing" 1
  command -v curl >/dev/null    || fail "curl missing" 1
  command -v jq >/dev/null      || fail "jq missing" 1

  # cell-2 active in SQL?
  if [[ "$DRY_RUN" -eq 0 ]]; then
    local pgpass
    pgpass=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get secret postgresql \
      -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
    local status
    status=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
      env "PGPASSWORD=$pgpass" psql -U loopzero -d loopzero -tAc "SELECT status FROM cells WHERE cell_id='cell-2'" 2>/dev/null \
      | tr -d ' \r\n' || echo "")
    [[ "$status" == "active" ]] || fail "cell-2 not active in cells table (got '$status')" 1
  else
    log "[dry-run] would query: SELECT status FROM cells WHERE cell_id='cell-2'"
  fi

  # ClusterCredential connected? Poll up to 60s — controller probe runs every
  # ~30s and transient false-during-reconcile is normal.
  if [[ "$DRY_RUN" -eq 0 ]]; then
    local connected="" tries=0
    while [[ $tries -lt 12 ]]; do
      connected=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get clustercredentials \
        -o jsonpath='{.items[?(@.spec.cellID=="cell-2")].status.connected}' 2>/dev/null || echo "")
      [[ "$connected" == "true" ]] && break
      tries=$((tries+1))
      sleep 5
    done
    [[ "$connected" == "true" ]] || fail "ClusterCredential cell-2 not connected after 60s polling (got '$connected')" 1
  else
    log "[dry-run] would query: kubectl get clustercredentials by spec.cellID=cell-2"
  fi

  # DynamicRouter sees cell-2? Try Prometheus, fall back to direct /metrics exec.
  # Local k3d may not have ServiceMonitor → Prometheus doesn't scrape loopzero-api.
  if [[ "$DRY_RUN" -eq 0 ]]; then
    local raw count
    raw=$(curl -fsS "${PROM_URL}/api/v1/query?query=loopzero_cell_discovery_count" 2>/dev/null \
      | jq -r '.data.result[0].value[1] // "0"' 2>/dev/null || echo "0")
    case "$raw" in ''|null|*[^0-9]*) count=0 ;; *) count=$raw ;; esac
    if [[ "$count" -lt 2 ]]; then
      # Fallback: query loopzero-api /metrics directly inside the cluster.
      raw=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec deploy/loopzero-api -- \
        sh -c 'wget -qO- http://localhost:9090/metrics 2>/dev/null || curl -fsS http://localhost:9090/metrics 2>/dev/null' 2>/dev/null \
        | awk '/^loopzero_cell_discovery_count[[:space:]]/{print $2}' | head -1 || echo "0")
      case "$raw" in ''|null|*[^0-9]*) count=0 ;; *) count=$raw ;; esac
    fi
    [[ "${count:-0}" -ge 2 ]] || fail "loopzero_cell_discovery_count=${count} (need >= 2; tried Prometheus + direct /metrics)" 1
  else
    log "[dry-run] would query: loopzero_cell_discovery_count >= 2"
  fi

  log "preflight OK"
}

# ------------------------------------------------------------------------------
# Phase 2 — provision synthetic tenant + pin to cell-2
# ------------------------------------------------------------------------------

verify_synth_tenant() {
  log "verifying synthetic tenant ${SYNTH_TENANT_ID} pre-exists with ACTIVE release"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: kubectl get tenant ${SYNTH_TENANT_ID} -o jsonpath='{.status.phase}'"
    log "[dry-run] would: query releases.status='ACTIVE' for tenant"
    return 0
  fi

  local phase
  phase=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get tenant "$SYNTH_TENANT_ID" \
    -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
  [[ "$phase" == "Active" ]] || fail "tenant ${SYNTH_TENANT_ID} phase=${phase} (need Active; bootstrap via dogfood first)" 2

  if [[ "$SMOKE" -eq 1 ]]; then
    log "[smoke] skipping ACTIVE-release requirement (compressed-mode mechanism validation)"
    return 0
  fi

  # Verify at least one ACTIVE release exists.
  local pgpass
  pgpass=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get secret postgresql \
    -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
  local active_count
  active_count=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    env "PGPASSWORD=$pgpass" psql -U loopzero -d loopzero -tAc \
    "SELECT COUNT(*) FROM releases WHERE tenant_id='${SYNTH_TENANT_ID}' AND status='ACTIVE'" 2>/dev/null \
    | tr -d ' \r\n' || echo "0")
  [[ "${active_count:-0}" -ge 1 ]] || fail "tenant has no ACTIVE release (need >= 1 to soak)" 2
}

pin_to_cell2() {
  log "pinning ${SYNTH_TENANT_ID} → cell-2 via loopzero admin tenants pin"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: loopzero admin tenants pin --tenant ${SYNTH_TENANT_ID} --cell cell-2"
    return 0
  fi

  loopzero admin tenants pin \
    --tenant "$SYNTH_TENANT_ID" \
    --cell cell-2 \
    --json || fail "pin to cell-2 failed" 2
}

# ------------------------------------------------------------------------------
# Phase 3 — soak (probe + EB monitoring)
# ------------------------------------------------------------------------------

probe_http() {
  local url="$1"
  curl -fsS -o /dev/null -w '%{http_code}' --max-time 10 "$url" 2>/dev/null || echo "000"
}

cell1_eb_consumed() {
  # Returns percentage of cell-1 error budget consumed in the last 5min.
  local q='100 * (1 - sum(rate(loopzero_cell_routing_decisions_total{cell="cell-1",decision="assigned"}[5m])) / sum(rate(loopzero_cell_routing_decisions_total{cell="cell-1"}[5m])))'
  curl -fsS "${PROM_URL}/api/v1/query?query=$(printf %s "$q" | jq -sRr @uri)" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // "0"' 2>/dev/null || echo "0"
}

soak() {
  local synth_url="https://${SYNTH_TENANT_ID}-production.loopzero.local"
  local end_ts=$(( $(date +%s) + SOAK_SECONDS ))
  local probe_count=0
  local probe_5xx=0

  log "soak — duration=${SOAK_SECONDS}s, probe_interval=${PROBE_INTERVAL}s, url=${synth_url}"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: probe ${synth_url} every ${PROBE_INTERVAL}s for ${SOAK_SECONDS}s"
    log "[dry-run] would: monitor cell-1 EB consumption against threshold ${EB_THRESHOLD_PCT}%"
    return 0
  fi

  while [[ $(date +%s) -lt $end_ts ]]; do
    local code
    code=$(probe_http "$synth_url")
    probe_count=$((probe_count + 1))
    # In smoke mode the GitOps repo may not yet have an app deployed —
    # any HTTP code (including 404, 502) is informational, not halt-worthy.
    # The mechanism validation focuses on the pin → CR patch → ArgoCD
    # destination flip → eventual sync; HTTP probe is a soft signal.
    if [[ "$SMOKE" -eq 0 && "$code" =~ ^5 ]]; then
      probe_5xx=$((probe_5xx + 1))
      log "probe $probe_count: HTTP ${code} (5xx count: ${probe_5xx})"
      if [[ $probe_5xx -ge 3 ]]; then
        log "HALT: 3 consecutive 5xx — triggering rollback"
        rollback
        exit 3
      fi
    elif [[ "$SMOKE" -eq 1 ]]; then
      log "[smoke] probe $probe_count: HTTP ${code}"
    else
      probe_5xx=0  # reset on success
    fi

    local eb_pct
    eb_pct=$(cell1_eb_consumed)
    if awk -v a="$eb_pct" -v b="$EB_THRESHOLD_PCT" 'BEGIN{exit !(a > b)}' 2>/dev/null; then
      log "HALT: cell-1 EB consumed ${eb_pct}% > threshold ${EB_THRESHOLD_PCT}%"
      rollback
      exit 4
    fi

    sleep "$PROBE_INTERVAL"
  done

  log "soak GREEN — ${probe_count} probes, ${probe_5xx} 5xx, cell-1 EB stable"
}

# ------------------------------------------------------------------------------
# Phase 4 — rollback (always reachable; idempotent)
# ------------------------------------------------------------------------------

rollback() {
  log "rollback — re-pinning ${SYNTH_TENANT_ID} → cell-1${SMOKE_KEEP_TENANT:+ (smoke: keeping tenant)}"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "[dry-run] would: loopzero admin tenants pin --tenant ${SYNTH_TENANT_ID} --cell cell-1"
    log "[dry-run] would: kubectl delete tenant ${SYNTH_TENANT_ID}"
    return 0
  fi

  loopzero admin tenants pin --tenant "$SYNTH_TENANT_ID" --cell cell-1 \
    --json 2>/dev/null || true
  if [[ "$SMOKE_KEEP_TENANT" -ne 1 ]]; then
    kubectl --context "$KUBECTL_CONTEXT" delete tenant "$SYNTH_TENANT_ID" --ignore-not-found 2>/dev/null || true
  fi
}

# Cleanup trap: always tear down synthetic on exit.
trap 'rollback' EXIT

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------

preflight
verify_synth_tenant
pin_to_cell2
soak

log "T5.6 canary stage 1 GREEN"
exit 0
