#!/usr/bin/env bash
# seed-vault-cross-cluster-addr.sh
#
# T1.3 of plan m2-promote-11-canonical-seeds (v1.1 SHIPPABLE 95.2).
# Canonical OPENBAO_VAULT_ADDR wiring para loopzero-api Deployment apontando ao
# vault NodePort cross-cluster (k3d-loopzero-vault → k3d-loopzero-mgmt).
#
# Per Jeito A INQUEBRÁVEL (cleanroom-up-live-test-mandate.md): vault roda em
# cluster separado loopzero-vault; engine em mgmt precisa endereço cross-cluster
# resolvable. Default `http://openbao.vault-system.svc.cluster.local:8200`
# resolve apenas DENTRO do vault cluster — não funciona quando loopzero-api roda
# em mgmt.
#
# Pattern espelha seed-broker-canonical.sh + seed-vault-role-id-sync.sh.
#
# Flags:
#   --verify   assert env wired post-rollout
#   --dry-run  print intended actions without mutation
#   --help|-h  print this header
#
# Env vars: KUBE_CONTEXT (default k3d-loopzero-mgmt), VAULT_CONTEXT (default k3d-loopzero-vault).
# Pre-flight: k3d-loopzero-vault cluster running + openbao Service nodePort exposed.
set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
VAULT_CONTEXT="${VAULT_CONTEXT:-k3d-loopzero-vault}"
VERIFY=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-vault-cross-cluster-addr] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

# Discover vault Service nodePort
NODE_PORT=$(kubectl --context "$VAULT_CONTEXT" -n vault-system get svc openbao -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}' 2>/dev/null || echo "")
if [[ -z "$NODE_PORT" ]]; then
  log "FAIL — openbao Service nodePort not found in vault cluster (run task k3d:vault:bootstrap first)"
  exit 1
fi

# EC-1 absorption (v1.1): distinguish "container not running" from "container exists but IP not assigned"
docker inspect k3d-loopzero-vault-serverlb --format '{{.State.Running}}' 2>/dev/null | grep -qx true || { log "FAIL — k3d-loopzero-vault-serverlb container not running (run task k3d:vault:create first)"; exit 1; }

# Now safe to extract IP — container is Running
SERVERLB_IP=$(docker inspect k3d-loopzero-vault-serverlb --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null || echo "")
if [[ -z "$SERVERLB_IP" ]]; then
  log "FAIL — k3d-loopzero-vault-serverlb container running but IP not yet assigned (retry in ~5s)"
  exit 1
fi

VAULT_ADDR="http://${SERVERLB_IP}:${NODE_PORT}"
log "discovered cross-cluster VAULT_ADDR: $VAULT_ADDR"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY: kubectl set env deployment/loopzero-api OPENBAO_VAULT_ADDR=$VAULT_ADDR"
  exit 0
fi

# EC-2 absorption (v1.1): wait for any in-progress rollout BEFORE triggering a new one
# (T1.2 argocd-token may have started loopzero-api rollout that's still progressing)
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout status deploy/loopzero-api --timeout=120s 2>/dev/null || true

# Patch Deployment env (idempotent — kubectl set env upserts)
kubectl --context "$MGMT_CONTEXT" -n loopzero-system set env deployment/loopzero-api OPENBAO_VAULT_ADDR="$VAULT_ADDR"
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout status deploy/loopzero-api --timeout=60s

if [[ "$VERIFY" == "1" ]]; then
  ACTUAL=$(kubectl --context "$MGMT_CONTEXT" -n loopzero-system get deploy loopzero-api -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="OPENBAO_VAULT_ADDR")].value}')
  if [[ "$ACTUAL" == "$VAULT_ADDR" ]]; then
    log "PASS — OPENBAO_VAULT_ADDR wired ($ACTUAL)"
  else
    log "FAIL — actual=$ACTUAL expected=$VAULT_ADDR"
    exit 1
  fi
fi
