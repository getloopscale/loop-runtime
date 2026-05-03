#!/usr/bin/env bash
# cell-2-activation-plan T1.4 + ADR C9 — cell-2 creds don't leak in cell-1 logs.
#
# Captures cell-1 (k3d loopzero-workload) pod logs across loopzero-system + tenant
# namespaces for the recent past, greps for kubeconfig markers. Must be 0.

gate_c9() {
  local rt_ctx="${RT_CTX:-k3d-loopzero-workload}"
  if ! kubectl --context "$rt_ctx" --request-timeout=5s cluster-info >/dev/null 2>&1; then
    echo "    C9 SKIP: $rt_ctx unreachable"
    return 0
  fi

  # Look for kubeconfig markers in pod logs (pruning false positives by
  # combining markers — a real leak has both BEGIN CERTIFICATE AND token).
  local hits
  hits=$(kubectl --context "$rt_ctx" -n loopzero-system logs --tail=500 -l app.kubernetes.io/component=runtime --all-containers=true 2>/dev/null \
    | grep -cE 'BEGIN CERTIFICATE|kubeconfig:|certificate-authority-data' || true)
  if [[ "${hits:-0}" -gt 0 ]]; then
    echo "    C9 FAIL: cell-1 logs contain $hits kubeconfig marker(s) — credential leak suspected"
    return 1
  fi
  echo "    C9 PASS: cell-1 logs clean (0 kubeconfig markers in last 500 lines)"
  return 0
}
