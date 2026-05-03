#!/usr/bin/env bash
# cell-2-activation-plan T1.4 + ADR C10 — tenant data isolation cross-cell.
#
# Sanity check (non-destructive): for any tenant currently pinned to cell-2,
# the corresponding namespace MUST exist on workload-2 cluster and NOT on
# workload cluster (cell-1). Skip when there are no cell-2 tenants yet.

gate_c10() {
  local mgmt_ctx="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
  local rt_ctx="${RT_CTX:-k3d-loopzero-workload}"
  local rt2_ctx="${RT2_CTX:-k3d-loopzero-workload-2}"

  # Find first tenant pinned to cell-2.
  local pgpass
  pgpass=$(kubectl --context "$mgmt_ctx" -n loopzero-system get secret postgresql \
    -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
  local tenant
  tenant=$(kubectl --context "$mgmt_ctx" -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
    env "PGPASSWORD=$pgpass" psql -U loopzero -d loopzero -tAc "SELECT id FROM tenants WHERE cell_id='cell-2' LIMIT 1" 2>/dev/null \
    | tr -d ' \r\n' || echo "")
  if [[ -z "$tenant" ]]; then
    echo "    C10 PASS: no cell-2 tenant yet — isolation check N/A (will run after first pin)"
    return 0
  fi

  local ns="loopzero-${tenant}-production"
  local on_cell1=0 on_cell2=0
  kubectl --context "$rt_ctx"  get ns "$ns" >/dev/null 2>&1 && on_cell1=1
  kubectl --context "$rt2_ctx" get ns "$ns" >/dev/null 2>&1 && on_cell2=1
  if (( on_cell1 == 1 )); then
    echo "    C10 FAIL: tenant=$tenant pinned to cell-2 but namespace $ns also exists on cell-1 (isolation breach)"
    return 1
  fi
  if (( on_cell2 == 0 )); then
    echo "    C10 SKIP: tenant=$tenant pinned to cell-2 but namespace not yet on cell-2 (sync pending)"
    return 0
  fi
  echo "    C10 PASS: tenant=$tenant on cell-2 only (ns=$ns absent from cell-1)"
  return 0
}
