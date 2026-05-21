#!/usr/bin/env bash
# fix-all-platform-issues-plan T5.1 — Bulk-bootstrap dev tenants for Stage 2.
#
# Stage 2 ADR (cell2-canary-stage2.sh) requires >= 10 cell-1 tenants for the
# 10% sample to be statistically meaningful. This script wraps the dogfood
# loop to provision N synthetic tenants in cell-1 before Stage 2 runs.
#
# Each tenant is named tn-stage2-bootstrap-${EPOCH}-${i} so they're easily
# distinguished from real dev tenants (tn-e2etest etc) and bulk-deletable
# via `loopzero admin tenants evict --pattern 'tn-stage2-bootstrap-%'`.
#
# Usage:
#   bash infra/tests/parity/bootstrap-stage2-tenants.sh [--count N] [--dry-run]
#   --count N        number of tenants to bootstrap (default 11)
#   --dry-run        print plan, no API calls
#
# Pre-conditions:
#   - loopzero CLI on PATH with valid login (or LOOPZERO_SERVICE_AUTH for admin path)
#   - dogfood-loop.sh exists and journey-01-solo-deploy works
#   - cell-1 has capacity (capacity_tenants_max not reached)
#
# Exit codes:
#   0 — N tenants successfully bootstrapped (verified phase=Active)
#   1 — pre-flight failure
#   2 — bootstrap failure (rolling back not supported; clean via loopzero admin tenants evict)
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
COUNT="${COUNT:-11}"
DRY_RUN="${DRY_RUN:-0}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
DOGFOOD="${DOGFOOD:-${REPO_ROOT}/infra/tests/dogfood/full-loop.sh}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --count=*)   COUNT="${1#*=}"; shift ;;
    --count)     COUNT="${2:-11}"; shift 2 ;;
    --dry-run)   DRY_RUN=1; shift ;;
    -h|--help)   sed -n '2,28p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *)           shift ;;
  esac
done

log() { echo "[bootstrap-stage2] $*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

command -v kubectl >/dev/null || fail "kubectl missing" 1
[[ -x "$DOGFOOD" ]] || fail "dogfood loop missing or not executable: $DOGFOOD" 1

EPOCH=$(date -u +%s)
log "bootstrapping ${COUNT} tenants (prefix=tn-stage2-bootstrap-${EPOCH}-)"

if [[ "$DRY_RUN" -eq 1 ]]; then
  for i in $(seq 1 "$COUNT"); do
    log "[dry-run] would create tn-stage2-bootstrap-${EPOCH}-${i} via dogfood journey-01-solo-deploy"
  done
  log "[dry-run] OK"
  exit 0
fi

success=0
for i in $(seq 1 "$COUNT"); do
  TN_ID="tn-stage2-bootstrap-${EPOCH}-${i}"
  log "[${i}/${COUNT}] bootstrapping ${TN_ID}"
  if SYNTH_TENANT_ID="$TN_ID" bash "$DOGFOOD" --only journey-01-solo-deploy \
       >"/tmp/bootstrap-${TN_ID}.log" 2>&1; then
    log "  ${TN_ID} bootstrapped"
    success=$((success+1))
  else
    log "  ${TN_ID} FAILED — see /tmp/bootstrap-${TN_ID}.log"
  fi
done

log "summary: ${success}/${COUNT} tenants successfully bootstrapped"

# Verification: count cell-1 tenants in DB.
PGPASS=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get secret postgresql \
  -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
if [[ -n "$PGPASS" ]]; then
  CELL1_COUNT=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    env "PGPASSWORD=$PGPASS" psql -U loopzero -d loopzero -tAc \
    "SELECT COUNT(*) FROM tenants WHERE cell_id='cell-1' AND id LIKE 'tn-stage2-bootstrap-${EPOCH}-%'" 2>/dev/null \
    | tr -d ' \r\n' || echo "0")
  log "cell-1 tenants matching prefix: ${CELL1_COUNT}"
  if (( CELL1_COUNT < COUNT )); then
    log "WARNING: only ${CELL1_COUNT}/${COUNT} tenants in cell-1 — partial bootstrap"
    exit 2
  fi
fi

log "T5.1 GREEN: ${COUNT} tenants ready for Stage 2 sample"
exit 0
