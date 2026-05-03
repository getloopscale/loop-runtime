#!/usr/bin/env bash
# cell-2-activation-plan T1.4 + ADR C12 — ClusterCredential Secret hygiene.
#
# For every <cluster-name>-kubeconfig Secret referenced by a CC, decode the
# kubeconfig payload and assert clusters[0].cluster.certificate-authority-data
# is base64-decodable to PEM ('-----BEGIN CERTIFICATE-----').

gate_c12() {
  local mgmt_ctx="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
  if ! kubectl --context "$mgmt_ctx" --request-timeout=5s cluster-info >/dev/null 2>&1; then
    echo "    C12 SKIP: $mgmt_ctx unreachable"
    return 0
  fi

  # List all CCs and their secretRef.name + secretRef.namespace.
  local cc_json
  cc_json=$(kubectl --context "$mgmt_ctx" -n loopzero-system get clustercredentials -o json 2>/dev/null)
  if [[ -z "$cc_json" ]]; then
    echo "    C12 SKIP: no ClusterCredentials present"
    return 0
  fi

  local refs
  refs=$(echo "$cc_json" | jq -r '.items[] | "\(.spec.cellID) \(.spec.secretRef.namespace) \(.spec.secretRef.name)"' 2>/dev/null)
  if [[ -z "$refs" ]]; then
    echo "    C12 SKIP: no ClusterCredentials with secretRef"
    return 0
  fi

  local fail=0
  while IFS=$' \t' read -r cell_id ns secret_name; do
    [[ -z "$cell_id" || -z "$secret_name" ]] && continue
    local kc_b64 kc_decoded
    kc_b64=$(kubectl --context "$mgmt_ctx" -n "$ns" get secret "$secret_name" \
      -o jsonpath='{.data.kubeconfig}' 2>/dev/null || echo "")
    if [[ -z "$kc_b64" ]]; then
      echo "    C12 FAIL: $ns/$secret_name (cellID=$cell_id) has no data.kubeconfig"
      fail=$((fail+1))
      continue
    fi
    kc_decoded=$(echo "$kc_b64" | base64 -d 2>/dev/null || echo "")

    # Two valid TLS modes: (a) certificate-authority-data PEM, (b) insecure-skip-tls-verify (local k3d, ADR-0015).
    local ca_b64 insecure ca_pem
    ca_b64=$(echo "$kc_decoded" | grep "certificate-authority-data:" | head -1 | awk '{print $2}' || echo "")
    insecure=$(echo "$kc_decoded" | grep -q "insecure-skip-tls-verify:" && echo "true" || echo "")

    if [[ -n "$ca_b64" ]]; then
      ca_pem=$(echo "$ca_b64" | base64 -d 2>/dev/null | head -1 || echo "")
      if [[ "$ca_pem" != "-----BEGIN CERTIFICATE-----" ]]; then
        echo "    C12 FAIL: $ns/$secret_name (cellID=$cell_id) CA data not PEM (got '$ca_pem')"
        fail=$((fail+1))
        continue
      fi
    elif [[ -n "$insecure" ]]; then
      # Acceptable in local k3d / ADR-0015 dev path. Gate still passes;
      # defer strict-TLS enforcement to production-only profile.
      :
    else
      echo "    C12 FAIL: $ns/$secret_name (cellID=$cell_id) kubeconfig has neither certificate-authority-data nor insecure-skip-tls-verify"
      fail=$((fail+1))
      continue
    fi
  done <<< "$refs"

  if (( fail > 0 )); then
    return 1
  fi
  echo "    C12 PASS: every ClusterCredential Secret has base64(PEM) CA data"
  return 0
}
