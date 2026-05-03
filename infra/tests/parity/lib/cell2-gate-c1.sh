#!/usr/bin/env bash
# cell-2-activation-plan T0.3 + ADR C1 — cells table exists + cell-2 row.
# Also verifies discovery count >= 3 (matrix entry 156).
#
# Pre-conditions: kubectl context k3d-loopzero-mgmt reachable, postgresql-0 pod running.

gate_c1() {
  local mgmt_ctx="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
  local prom_url="${PROM_URL:-http://localhost:9090}"

  # 1. cells table has cell-2 row.
  # PGPASSWORD lookup from postgresql Secret (k3d default: loopzero-dev-password).
  local pgpass
  pgpass=$(kubectl --context "$mgmt_ctx" -n loopzero-system get secret postgresql \
    -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
  local cell2_count
  cell2_count=$(kubectl --context "$mgmt_ctx" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    env "PGPASSWORD=$pgpass" psql -U loopzero -d loopzero -tAc "SELECT COUNT(*) FROM cells WHERE cell_id='cell-2'" 2>/dev/null \
    | tr -d ' \r\n' || echo "0")
  if [[ "${cell2_count:-0}" -lt 1 ]]; then
    echo "    C1 FAIL: cells table missing cell-2 row (run migration 000006)"
    return 1
  fi

  # 2. ClusterCredential CR with spec.cellID=cell-2 exists.
  local cc_present
  cc_present=$(kubectl --context "$mgmt_ctx" -n loopzero-system get clustercredentials \
    -o jsonpath='{.items[?(@.spec.cellID=="cell-2")].metadata.name}' 2>/dev/null || echo "")
  if [[ -z "$cc_present" ]]; then
    echo "    C1 FAIL: no ClusterCredential CR with spec.cellID=cell-2 (run task k3d:heal — H14)"
    return 1
  fi

  # 3. Discovery count >= 3. Try Prometheus first (PROM_URL), then fall back
  # to loopzero-api /metrics directly (port-forward), then to in-cluster exec.
  # Local k3d clusters often don't have ServiceMonitor CRDs — Prometheus
  # may not scrape loopzero-api directly, so fallbacks are essential.
  local count=0
  _coerce_int() {
    case "$1" in ''|null|*[^0-9]*) echo 0 ;; *) echo "$1" ;; esac
  }
  # Try Prometheus first.
  local raw
  raw=$(curl -fsS "${prom_url}/api/v1/query?query=loopzero_cell_discovery_count" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // "0"' 2>/dev/null || echo "0")
  count=$(_coerce_int "$raw")
  # If Prometheus returns 0 or empty, fall back to direct /metrics scrape.
  if [[ "$count" -lt 3 ]]; then
    local raw_direct
    raw_direct=$(kubectl --context "$mgmt_ctx" -n loopzero-system exec deploy/loopzero-api -- \
      sh -c 'wget -qO- http://localhost:9090/metrics 2>/dev/null || curl -fsS http://localhost:9090/metrics 2>/dev/null' 2>/dev/null \
      | awk '/^loopzero_cell_discovery_count[[:space:]]/{print $2}' | head -1 || echo "0")
    count=$(_coerce_int "$raw_direct")
  fi
  if [[ "$count" -lt 3 ]]; then
    echo "    C1 FAIL: loopzero_cell_discovery_count=${count} (need >= 3; tried Prometheus + direct /metrics)"
    return 1
  fi

  echo "    C1 PASS: cells row present + CC present + discovery=${count}"
  return 0
}
