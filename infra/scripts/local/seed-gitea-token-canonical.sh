#!/usr/bin/env bash
# Canonical post-Cleanroom seed for Gitea API token rotation +
# cross-consumer synchronization. Without this, loopzero-api fails to push
# GitOps manifests (status 401 ensure repo) AND loopzero-tenant-operator
# fails to provision tenant Git repos (Tenant CR stuck in Provisioning
# phase with `GitRepoReady=False`).
#
# Canonical chain — 4 layers:
#
# Layer 1 — Authenticate to Gitea using gitea-admin Secret credentials
#   (username + password remain canonical across rotations; only the
#   token is mintable + rotatable).
#
# Layer 2 — Delete legacy `loopzero-api` token (if exists) + mint fresh one
#   via Gitea API POST /users/{user}/tokens. Token name `loopzero-api-token`
#   per the canonical naming convention used by all 3 consumers.
#
# Layer 3 — Sync the fresh token to 3 consumer Secrets in mgmt cluster:
#   loopzero-system/gitea-admin       key=token (cli + admin path)
#   loopzero-system/loopzero-api-tokens   key=gitea-token (loopzero-api envFrom)
#   loopzero-system/loopzero-tenant-operator key=GITEA_TOKEN (tenant operator)
#
# Layer 4 — Restart all 3 consumers so they pick up the new token.
#
# Idempotent: re-runs mint a NEW token + sync to all 3 (atomic rotation).
# Per `feedback_canonical_vs_codified_workaround` — Gitea tokens are
# mintable on-demand from the canonical credentials so rotation is the
# correct operational pattern, not preservation.

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
GITEA_PF_PORT="${GITEA_PF_PORT:-13000}"

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

log() { printf "%s [seed-gitea-token-canonical] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight — read canonical Gitea credentials
USER=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get secret gitea-admin -o jsonpath='{.data.username}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
PW=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get secret gitea-admin -o jsonpath='{.data.password}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
[[ -n "$USER" ]] || fail "Layer 1: gitea-admin.username missing — run task setup first"
[[ -n "$PW" ]] || fail "Layer 1: gitea-admin.password missing"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would mint fresh Gitea token + sync to 3 consumer Secrets + restart 2 deployments"
  exit 0
fi

# Layer 1 — port-forward + login probe
PF_PID=""
cleanup() {
  if [[ -n "$PF_PID" ]] && kill -0 "$PF_PID" 2>/dev/null; then
    kill "$PF_PID" 2>/dev/null || true
    wait "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  port-forward svc/gitea-http "$GITEA_PF_PORT":3000 >/dev/null 2>&1 &
PF_PID=$!
sleep 3
PROBE_CODE=$(curl -s -o /dev/null -w "%{http_code}" -u "$USER:$PW" "http://localhost:${GITEA_PF_PORT}/api/v1/user" || echo "000")
[[ "$PROBE_CODE" == "200" ]] || fail "Layer 1: gitea-admin credentials reject HTTP $PROBE_CODE (Gitea may not be Ready)"
log "Layer 1: canonical credentials verified (user=$USER, HTTP $PROBE_CODE)"

# Layer 2 — delete + mint
EXISTING_ID=$(curl -fsS -u "$USER:$PW" "http://localhost:${GITEA_PF_PORT}/api/v1/users/$USER/tokens" 2>/dev/null \
  | python3 -c "import json,sys
for t in json.load(sys.stdin):
    if t['name'] in ('loopzero-api', 'loopzero-api-token'):
        print(t['id']); break" 2>/dev/null || echo "")

if [[ -n "$EXISTING_ID" ]]; then
  curl -fsS -X DELETE -u "$USER:$PW" \
    "http://localhost:${GITEA_PF_PORT}/api/v1/users/$USER/tokens/$EXISTING_ID" >/dev/null 2>&1 || true
  log "Layer 2: deleted legacy token id=$EXISTING_ID"
fi

NEW_TOKEN=$(curl -fsS -X POST -u "$USER:$PW" -H "Content-Type: application/json" \
  -d '{"name":"loopzero-api-token","scopes":["all"]}' \
  "http://localhost:${GITEA_PF_PORT}/api/v1/users/$USER/tokens" 2>/dev/null \
  | python3 -c "import json,sys; print(json.load(sys.stdin).get('sha1', ''))" 2>/dev/null || echo "")
[[ -n "$NEW_TOKEN" ]] && [[ "$NEW_TOKEN" != "None" ]] || fail "Layer 2: token mint failed"
log "Layer 2: minted fresh token (first 8 chars: ${NEW_TOKEN:0:8}...)"

cleanup
PF_PID=""

# Layer 3 — sync to all 3 consumer Secrets
TOKEN_B64=$(printf "%s" "$NEW_TOKEN" | base64 -w 0)
kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  patch secret gitea-admin --type=json \
  -p="[{\"op\":\"replace\",\"path\":\"/data/token\",\"value\":\"$TOKEN_B64\"}]" >/dev/null
log "Layer 3a: gitea-admin.token synced"

kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  patch secret loopzero-api-tokens --type=json \
  -p="[{\"op\":\"replace\",\"path\":\"/data/gitea-token\",\"value\":\"$TOKEN_B64\"}]" >/dev/null
log "Layer 3b: loopzero-api-tokens.gitea-token synced"

kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  patch secret loopzero-tenant-operator --type=json \
  -p="[{\"op\":\"replace\",\"path\":\"/data/GITEA_TOKEN\",\"value\":\"$TOKEN_B64\"}]" >/dev/null
log "Layer 3c: loopzero-tenant-operator.GITEA_TOKEN synced"

# Layer 4 — restart consumers (per-deployment existence guard, canonical 2026-06-16).
# Deployments may not exist during Cleanroom dev-public bring-up (operator chart
# sync happens AFTER CRDs land); the Secret sync in Layer 3 is what matters —
# when the deployment IS eventually created, envFrom picks up the canonical
# token automatically. Skip-with-INFO is canonical (consistent with the
# secret_canonical_no_hardcode memory: resource-existence is a precondition, not
# an invariant the seed enforces).
restart_if_exists() {
  local deploy="$1"
  if kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" get "deployment/$deploy" >/dev/null 2>&1; then
    kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout restart "deployment/$deploy" >/dev/null
    kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" rollout status "deployment/$deploy" --timeout=120s >/dev/null
    log "Layer 4: $deploy restarted"
  else
    log "Layer 4: $deploy not yet deployed — skip (Secret sync in Layer 3 will be picked up on first create)"
  fi
}
restart_if_exists loopzero-api
restart_if_exists loopzero-tenant-operator

# Verify
if [[ "$VERIFY" == "1" ]]; then
  # Probe token works against Gitea
  kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
    port-forward svc/gitea-http "$GITEA_PF_PORT":3000 >/dev/null 2>&1 &
  PF_PID=$!
  sleep 3
  AUTH_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: token $NEW_TOKEN" \
    "http://localhost:${GITEA_PF_PORT}/api/v1/user" || echo "000")
  cleanup; PF_PID=""
  [[ "$AUTH_CODE" == "200" ]] || fail "verify: token-based auth HTTP $AUTH_CODE"
  log "OK verify: token-based auth HTTP $AUTH_CODE"
fi

log "DONE — canonical Gitea token chain seeded (4 layers, 3 consumers synced)"
