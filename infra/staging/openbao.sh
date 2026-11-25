#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# openbao.sh
#
# Installs OpenBao on the staging secrets cluster and wires the core cluster's
# loopzero-api to its transit engine, the way production runs it (ADR 2026-11-25
# tenant secrets on OpenBao): a dedicated cluster, HA Raft, the awskms auto-unseal.
#
#   1. secrets.helmfile (env staging) on the secrets cluster: cert-manager, the issuer,
#      the TLS certificate and OpenBao, which unseals itself with the KMS key
#      OPENBAO_AWS_KMS_KEY_ID (infra-live security/kms-openbao-unseal), authenticated by
#      the access key OPENBAO_SEAL_ACCESS_KEY_ID / OPENBAO_SEAL_SECRET_ACCESS_KEY
#      (identity/secrets-openbao-seal-user) through secrets-system/openbao-aws-creds.
#   2. A fresh vault (every rebuild) is initialised by vault-bootstrap-dev.sh with
#      recovery keys; the init file (root token, recovery keys) stays on the host, mode
#      0600, at OPENBAO_INIT_FILE. The script also runs the openbao-init job, creates the
#      loopzero-api AppRole and writes its secret_id and the SPKI pin into core.
#   3. The AppRole's role_id joins the secret_id in core's loopzero-openbao-approle.
#   4. On core, Service secrets-system/openbao has no selector: its EndpointSlice names
#      the secrets cluster's nodes at the NodePort, so loopzero-api dials
#      openbao.secrets-system.svc.cluster.local:8200, the name the certificate carries.
#
# Steps 2 and 3 run only when the vault is new or core lacks either AppRole half; a
# deploy of a running staging refreshes the endpoints and nothing else.
set -euo pipefail

SECRETS_CONTEXT="${SECRETS_CONTEXT:-secrets}"
CORE_CONTEXT="${CORE_CONTEXT:-core}"
INIT_FILE="${OPENBAO_INIT_FILE:-/var/lib/loopzero-staging/openbao-init.json}"
NODE_PORT="${OPENBAO_NODE_PORT:-30200}"
: "${OPENBAO_AWS_KMS_KEY_ID:?the awskms seal key (infra-live security/kms-openbao-unseal key_arn)}"
export OPENBAO_AWS_KMS_KEY_ID
: "${OPENBAO_SEAL_ACCESS_KEY_ID:?access key id of the seal user (infra-live identity/secrets-openbao-seal-user)}"
: "${OPENBAO_SEAL_SECRET_ACCESS_KEY:?secret access key of the seal user}"

log() { printf 'staging-openbao: %s\n' "$*" >&2; }
# The helmfile declares the k3d context; staging names its clusters by role, so the
# context is passed, as vault-bootstrap-dev.sh does for the openbao-init release.
hf() { helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e staging --kube-context "$SECRETS_CONTEXT" "$@"; }
bao_status() {
  kubectl --context "$SECRETS_CONTEXT" -n secrets-system exec openbao-0 -- \
    env BAO_SKIP_VERIFY=true bao status -format=json 2>/dev/null || true
}

hf --selector name=cert-manager sync
hf --selector name=cluster-issuer sync
hf --selector name=openbao-cert sync
# Staging only: the seal's access key (secrets.helmfile, emulator block), read by the
# OpenBao pods at start.
kubectl --context "$SECRETS_CONTEXT" -n secrets-system create secret generic openbao-aws-creds \
  --from-literal=access_key_id="$OPENBAO_SEAL_ACCESS_KEY_ID" \
  --from-literal=secret_access_key="$OPENBAO_SEAL_SECRET_ACCESS_KEY" \
  --dry-run=client -o yaml | kubectl --context "$SECRETS_CONTEXT" apply -f - >/dev/null
kubectl --context "$SECRETS_CONTEXT" -n secrets-system wait --for=condition=Ready certificate/openbao --timeout=180s
hf --selector name=openbao sync

approle_halves=$(kubectl --context "$CORE_CONTEXT" -n loopzero-system get secret loopzero-openbao-approle \
  -o 'go-template={{if .data.OPENBAO_VAULT_ROLE_ID}}role {{end}}{{if .data.OPENBAO_VAULT_SECRET_ID}}secret{{end}}' 2>/dev/null || true)
initialized=$(bao_status | python3 -c 'import json,sys; print(json.loads(sys.stdin.read() or "{}").get("initialized", False))')
if [[ "$initialized" == "True" && "$approle_halves" == "role secret" ]]; then
  log "vault initialised and loopzero-api holds both AppRole halves; bootstrap skipped"
else
  log "bootstrapping the vault (initialised: $initialized, core AppRole: '${approle_halves}')"
  install -d -m 0700 "$(dirname "$INIT_FILE")"
  OPENBAO_CONTEXT="$SECRETS_CONTEXT" TARGET_CONTEXT="$CORE_CONTEXT" AUTO_UNSEAL_MODE=awskms \
    HELMFILE_ENV=staging UNSEAL_FILE="$INIT_FILE" bash infra/scripts/local/vault-bootstrap-dev.sh
  sealed=$(bao_status | python3 -c 'import json,sys; print(json.loads(sys.stdin.read() or "{}").get("sealed", True))')
  [[ "$sealed" == "False" ]] || { log "OpenBao is still sealed after init: the awskms seal did not unseal it"; exit 1; }
  root_token=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["root_token"])' "$INIT_FILE")
  role_id=$(kubectl --context "$SECRETS_CONTEXT" -n secrets-system exec openbao-0 -- \
    env BAO_TOKEN="$root_token" BAO_SKIP_VERIFY=true bao read -field=role_id auth/approle/role/loopzero-api/role-id)
  [[ -n "$role_id" ]] || { log "the loopzero-api AppRole has no role_id"; exit 1; }
  kubectl --context "$CORE_CONTEXT" -n loopzero-system patch secret loopzero-openbao-approle --type merge \
    -p "{\"stringData\":{\"OPENBAO_VAULT_ROLE_ID\":\"$role_id\"}}" >/dev/null
  log "AppRole role_id written to $CORE_CONTEXT loopzero-system/loopzero-openbao-approle"
fi

# The secrets cluster's worker nodes; the control-plane node of the emulator's kind
# cluster is no node of an EKS cluster and is left out.
mapfile -t node_ips < <(kubectl --context "$SECRETS_CONTEXT" get nodes \
  -l '!node-role.kubernetes.io/control-plane' \
  -o 'jsonpath={range .items[*]}{.status.addresses[?(@.type=="InternalIP")].address}{"\n"}{end}')
[[ ${#node_ips[@]} -gt 0 ]] || { log "the secrets cluster has no worker node"; exit 1; }
endpoints=""
for ip in "${node_ips[@]}"; do
  endpoints+="  - addresses: [\"$ip\"]"$'\n'"    conditions: {ready: true}"$'\n'
done
kubectl --context "$CORE_CONTEXT" apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: secrets-system
---
apiVersion: v1
kind: Service
metadata:
  name: openbao
  namespace: secrets-system
  annotations:
    loopzero.dev/points-at: "OpenBao on the secrets cluster (infra/staging/openbao.sh)"
spec:
  ports:
    - name: https
      port: 8200
      targetPort: $NODE_PORT
      protocol: TCP
---
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: openbao-secrets-cluster
  namespace: secrets-system
  labels:
    kubernetes.io/service-name: openbao
addressType: IPv4
ports:
  - name: https
    port: $NODE_PORT
    protocol: TCP
endpoints:
$endpoints
EOF
log "core secrets-system/openbao points at ${node_ips[*]}:$NODE_PORT"
