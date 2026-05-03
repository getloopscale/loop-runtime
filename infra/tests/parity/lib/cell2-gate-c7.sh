#!/usr/bin/env bash
# cell-2-activation-plan T1.3 + ADR C7 — shared ingress-domain.
#
# Verifies ClusterCredential CRs for cell-1 + cell-2 carry IDENTICAL
# loopzero.io/ingress-domain label. Required for ≤5min rollback SLO.

gate_c7() {
  local mgmt_ctx="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"

  local d1 d2
  d1=$(kubectl --context "$mgmt_ctx" -n loopzero-system get clustercredentials \
    -o jsonpath='{.items[?(@.spec.cellID=="cell-1")].metadata.labels.loopzero\.io/ingress-domain}' 2>/dev/null || echo "")
  d2=$(kubectl --context "$mgmt_ctx" -n loopzero-system get clustercredentials \
    -o jsonpath='{.items[?(@.spec.cellID=="cell-2")].metadata.labels.loopzero\.io/ingress-domain}' 2>/dev/null || echo "")

  if [[ -z "$d1" ]] || [[ -z "$d2" ]]; then
    echo "    C7 FAIL: missing loopzero.io/ingress-domain label (cell-1='$d1' cell-2='$d2'). Run task k3d:heal."
    return 1
  fi
  if [[ "$d1" != "$d2" ]]; then
    echo "    C7 FAIL: ingress-domain mismatch — cell-1=$d1 vs cell-2=$d2 (rollback would require DNS change)"
    return 1
  fi
  echo "    C7 PASS: cell-1 + cell-2 share ingress-domain=$d1"
  return 0
}
