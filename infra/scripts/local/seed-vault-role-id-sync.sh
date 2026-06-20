#!/usr/bin/env bash
# Canonical post-Cleanroom seed for OpenBao AppRole role_id sync into
# loopzero-api env. Without this, loopzero-api fails secret encryption with
# "vault: AppRole login returned 400: invalid role or secret ID" because
# vault-bootstrap-dev.sh always mints a FRESH role_id per Cleanroom,
# but the canonical loopzero-api chart values
# (infra/helmfile/values/dev-public/loopzero-api.yaml.gotmpl) hardcode
# OPENBAO_VAULT_ROLE_ID to a SPECIFIC UUID (the value from the FIRST
# bootstrap that's now stale).
#
# Per memory [[feedback_no_hardcode_gate_2026_06_09]]: hardcoded UUIDs
# in chart values are the anti-pattern this script closes. The CANONICAL
# end-state is for the chart to consume role_id from a K8s Secret
# (sibling of secret_id in loopzero-openbao-approle); this script is the
# bridge until that chart refactor lands.
#
# Layers:
# 1. Authenticate to OpenBao with root token (read from ~/.loopzero/cleanroom-unseal.json)
# 2. Read auth/approle/role/loopzero-api/role-id (canonical fresh value)
# 3. Override loopzero-api Deployment env OPENBAO_VAULT_ROLE_ID with fresh value
# 4. Restart loopzero-api so it re-authenticates to vault
#
# Idempotent: re-runs with the same role_id are no-op.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
VAULT_CONTEXT="${VAULT_CONTEXT:-k3d-loopzero-vault}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
VAULT_NAMESPACE="${VAULT_NAMESPACE:-vault-system}"
UNSEAL_FILE="${LOOPZERO_VAULT_UNSEAL_FILE:-$HOME/.loopzero/cleanroom-unseal.json}"

DRY_RUN=0
VERIFY=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --verify) VERIFY=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-vault-role-id-sync] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight
[[ -r "$UNSEAL_FILE" ]] \
  || fail "unseal file missing at $UNSEAL_FILE — run vault-bootstrap-dev.sh first"
kubectl --context "$VAULT_CONTEXT" -n "$VAULT_NAMESPACE" get pod openbao-0 >/dev/null 2>&1 \
  || fail "openbao-0 pod missing in $VAULT_CONTEXT/$VAULT_NAMESPACE — run task k3d:vault:create && task k3d:vault:bootstrap (Jeito A INQUEBRÁVEL — bootstrap-in-mgmt PROHIBITED)"

ROOT_TOKEN=$(python3 -c "
import json,sys
d = json.load(open('$UNSEAL_FILE'))
sys.stdout.write(d.get('root_token',''))
" 2>/dev/null)
[[ -n "$ROOT_TOKEN" ]] || fail "root_token not found in $UNSEAL_FILE"

# Layer 1+2 — read fresh role_id from vault (in k3d-loopzero-vault cluster per Jeito A)
ROLE_ID=$(kubectl --context "$VAULT_CONTEXT" -n "$VAULT_NAMESPACE" \
  exec openbao-0 -- env BAO_SKIP_VERIFY=true BAO_TOKEN="$ROOT_TOKEN" \
  bao read -field=role_id auth/approle/role/loopzero-api/role-id 2>/dev/null | tail -1)

if [[ -z "$ROLE_ID" ]] || [[ "${#ROLE_ID}" -lt 30 ]]; then
  fail "could not read role_id from vault (got: '${ROLE_ID:0:20}...')"
fi
log "Layer 1+2: canonical role_id = ${ROLE_ID:0:8}... (len=${#ROLE_ID})"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would set loopzero-api OPENBAO_VAULT_ROLE_ID env to canonical + restart"
  exit 0
fi

# Layer 3 — check current value, override only if different
CURRENT_RID=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get deploy loopzero-api -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="OPENBAO_VAULT_ROLE_ID")].value}' 2>/dev/null || echo "")

if [[ "$CURRENT_RID" == "$ROLE_ID" ]]; then
  log "Layer 3: loopzero-api OPENBAO_VAULT_ROLE_ID already canonical (no-op)"
else
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    set env deployment/loopzero-api OPENBAO_VAULT_ROLE_ID="$ROLE_ID" >/dev/null
  log "Layer 3: loopzero-api OPENBAO_VAULT_ROLE_ID env synced to canonical"

  # Layer 4 — wait for rollout
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    rollout status deployment/loopzero-api --timeout=120s >/dev/null
  log "Layer 4: loopzero-api rolled out with fresh role_id"
fi

# Verify
if [[ "$VERIFY" == "1" ]]; then
  # Probe loopzero-api can encrypt a secret via vault transit
  JWT_FILE="$HOME/.loopzero/local-dogfood-session-jwt.txt"
  [[ -r "$JWT_FILE" ]] || { log "WARN verify: JWT seed missing; skipping encrypt probe"; exit 0; }
  JWT=$(cat "$JWT_FILE")

  CLOUD_BASE="${LOOPZERO_CLOUD_BASE:-https://cloud-dev.loopzero.example}"
  PROBE_NAME="VAULT_ROLE_PROBE_$(date +%s)"
  RESP=$(curl -sk -X POST -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
    -d "{\"project\":\"teste\",\"name\":\"$PROBE_NAME\",\"value\":\"probe\",\"environment\":\"production\",\"app\":\"api\"}" \
    "$CLOUD_BASE/engine/api/v1/secrets" 2>/dev/null || echo "")
  if echo "$RESP" | grep -q "INVALID_REQUEST\|invalid role or secret ID"; then
    log "FAIL verify: vault encrypt rejected — $RESP"
    exit 1
  fi
  if echo "$RESP" | grep -q '"created":true\|"name"'; then
    log "OK verify: vault transit encrypt PASS (probe secret created)"
    # Best-effort cleanup
    curl -sk -X DELETE -H "Authorization: Bearer $JWT" \
      "$CLOUD_BASE/engine/api/v1/secrets/$PROBE_NAME?project=teste&environment=production&app=api" >/dev/null 2>&1 || true
  else
    log "WARN verify: unexpected response — $RESP"
  fi
fi

log "DONE — canonical vault role_id synced"
