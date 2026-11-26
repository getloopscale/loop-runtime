#!/usr/bin/env bash
# validate-op-b-soak.sh — Phase 5 T5.0 pre-flight gate for Op B heal cleanup.
#
# Plan: 2026-05-25-live-ca-fetch-clustercredential-plan v1.1
# EC-19 amendment: 7-day soak gate must be VALIDATED before T5.1 deletes
# H19/H19a from the heal script. Without this script, "operator waits 7
# days" is unverifiable promise.
#
# Validates: loopruntime_clustercredential_ca_fetch_total{result="fallback"} must
# be ZERO over the configured window. Any non-zero fallback means Op B is
# silently degrading to snapshot caData — H19/H19a are still needed.
#
# Usage:
#   bash infra/scripts/local/validate-op-b-soak.sh [--days N]
#   bash infra/scripts/local/validate-op-b-soak.sh --days 7
#   PROMETHEUS_URL=http://prom.local bash infra/scripts/local/validate-op-b-soak.sh
#   DRY_RUN=1 bash infra/scripts/local/validate-op-b-soak.sh --days 7
#
# Env:
#   PROMETHEUS_URL  Prometheus API base URL (default: http://localhost:9090)
#   DRY_RUN         If "1", print the query + window without calling Prometheus
#
# Exit codes:
#   0 — fallback counter == 0 over window. Safe to proceed with T5.1.
#   1 — usage error
#   2 — fallback counter > 0. T5.1 BLOCKED. Investigate Warn logs for
#       audit="ca_fetch_fallback" entries.
#   3 — Prometheus unreachable or query failed.

set -euo pipefail

DAYS=7
PROM="${PROMETHEUS_URL:-http://localhost:9090}"
DRY_RUN="${DRY_RUN:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --days)
      DAYS="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      sed -n '1,30p' "$0"
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      exit 1
      ;;
  esac
done

# Validate DAYS is a positive integer.
if ! [[ "$DAYS" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: --days must be a positive integer (got: $DAYS)" >&2
  exit 1
fi

QUERY="sum(increase(loopruntime_clustercredential_ca_fetch_total{result=\"fallback\"}[${DAYS}d]))"

log() { printf '[validate-op-b-soak] %s\n' "$*" >&2; }

log "window: ${DAYS}d"
log "query:  $QUERY"
log "target: $PROM"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY RUN — skipping Prometheus call"
  echo "DRY_RUN: would query Prometheus at $PROM with: $QUERY"
  exit 0
fi

# Issue the query.
HTTP_RESPONSE=$(curl -sf --get \
  --max-time 10 \
  "${PROM}/api/v1/query" \
  --data-urlencode "query=${QUERY}" \
  2>&1) || {
    log "ERROR: Prometheus query failed at $PROM"
    log "  $HTTP_RESPONSE"
    exit 3
}

# Parse response: result is a list of objects, each with .value=[ts, "n"].
# sum(...) collapses to a single result; if empty (no series), use 0.
DELTA=$(echo "$HTTP_RESPONSE" | python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
except Exception as e:
    print(f"PARSE_ERROR: {e}", file=sys.stderr)
    sys.exit(3)

if data.get("status") != "success":
    print(f"PROM_ERROR: {data}", file=sys.stderr)
    sys.exit(3)

result = data.get("data", {}).get("result", [])
if not result:
    print("0")
    sys.exit(0)

total = sum(float(item["value"][1]) for item in result)
print(int(total))
') || {
    log "ERROR: failed to parse Prometheus response"
    exit 3
}

if [[ "$DELTA" -gt 0 ]]; then
  log "FAIL: $DELTA fallback events over ${DAYS}d. Op B is degrading silently."
  log "  T5.1 (H19/H19a deletion) BLOCKED."
  log "  Investigation steps:"
  log "    1. Grep operator logs for audit=\"ca_fetch_fallback\" entries"
  log "    2. Check cluster-cell-* Secret state — caData may still be valid snapshot"
  log "    3. Verify target cluster's default/kube-root-ca.crt ConfigMap is readable"
  log "    4. Confirm RBAC: SA in kubeconfig has get/configmaps in default namespace"
  log "  Re-run after fixing — counter must stay 0 for full ${DAYS}d window."
  exit 2
fi

log "PASS: 0 fallback events over ${DAYS}d. Op B self-healing confirmed."
log "  Safe to proceed with T5.1 (delete H19/H19a from k3d-cross-cluster-heal.sh)."
exit 0
