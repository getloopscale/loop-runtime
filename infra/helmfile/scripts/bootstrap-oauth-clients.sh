#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD F-NEW (Rule 10 — Local equals production)
#
# Registers in Ory Hydra the OAuth2 clients the platform requires for
# auth to work end-to-end. In production (DOKS), this step is "Step 6"
# of the install procedure — referenced in
# `infra/tests/e2e/local-install-deploy.sh:222` as a fail-loud check
# ("loopruntime-cli OAuth2 client not registered (Step 6 of install)"). In
# standalone-multi the step did NOT exist, leaving Hydra empty and breaking
# `loopscale login` (device flow → 502 AUTH_SERVICE_ERROR).
#
# Idempotent: GET first, POST if absent, PUT if present. Without
# touching product code — the whole flow goes through the Hydra admin REST API,
# aligned with the pattern of the other L2 scripts (generate-secrets.sh,
# wire-clusters.sh).
#
# Clients registered:
#   1. loopruntime-cli — public client (token_endpoint_auth_method=none) with
#      grant_types: device_code + refresh_token + authorization_code.
#      Used by the `loopscale login` device flow + dashboard PKCE.
#   2. loopruntime-bootstrap — confidential client with the client_credentials
#      grant. Used by automation/CI/dogfood loops to obtain a
#      machine-to-machine token without a browser.
#
# Usage (standalone):
#   KUBECONFIG=$(k3d kubeconfig write loopruntime-core) \
#     bash infra/helmfile/scripts/bootstrap-oauth-clients.sh
#
# Usage (helmfile hook):
#   called by the core.helmfile.yaml.gotmpl post-sync hook after Ory Hydra is ready.
#
# Outputs (only when bootstrap-oauth-clients runs for the first
# time or after a delete): the loopruntime-bootstrap client_secret in
# Secret loopruntime-system/loopruntime-bootstrap-credentials for use by jobs/CI/CLI.
set -euo pipefail

NS_HYDRA="${HYDRA_NAMESPACE:-loopruntime-auth}"
NS_LOOPRUNTIME="${LOOPRUNTIME_NAMESPACE:-loopruntime-system}"
HYDRA_ADMIN_SVC="${HYDRA_ADMIN_SVC:-ory-hydra-admin}"
HYDRA_ADMIN_PORT="${HYDRA_ADMIN_PORT:-4445}"

log() { printf '[bootstrap-oauth-clients] %s\n' "$*" >&2; }

# Wait for Hydra admin to be reachable. On a fresh install Hydra takes up to
# ~60s to finish migrate + become Ready. Without this wait, RegisterClient
# in loopruntime-cloud (which fires at startup) runs first and produces a silent 404.
#
# The port-forward is kept open by the caller (main()) to avoid a
# race condition between two overlapping TCP connections (observed problem:
# trap RETURN closed the forward before the next curl).
wait_for_hydra() {
  local i
  for i in $(seq 1 60); do
    if curl -sf --max-time 2 "http://localhost:14445/health/ready" >/dev/null 2>&1; then
      log "Hydra admin ready"
      return 0
    fi
    sleep 2
  done
  log "ERROR: Hydra admin not ready after 120s"
  return 1
}

# upsert_client(name, payload_json) — idempotent. GET, POST if absent,
# PUT if present. Stdin: the client's complete payload.
upsert_client() {
  local name="$1" payload="$2"
  local code

  # Probe directly by client_id (a stable key). client_name may vary
  # ("loopruntime-cli" vs "LoopRuntime CLI") across creation paths — do not use it as a
  # discriminator. GET /admin/clients/{id} returns 404 if it does not exist.
  local existing=""
  if curl -sf --max-time 5 "http://localhost:14445/admin/clients/$name" -o /dev/null 2>/dev/null; then
    existing="$name"
  fi

  if [[ -n "$existing" ]]; then
    log "$name already registered (client_id=$existing) — refreshing via PUT"
    code=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
      -H "Content-Type: application/json" \
      "http://localhost:14445/admin/clients/$existing" \
      -d "$payload")
    if [[ "$code" == "200" ]]; then
      log "$name updated"
      printf '%s' "$existing"
      return 0
    fi
    log "$name PUT failed (status $code) — falling back to delete+recreate"
    curl -sf -X DELETE "http://localhost:14445/admin/clients/$existing" >/dev/null 2>&1 || true
  fi

  log "$name not registered — creating via POST"
  local resp
  resp=$(curl -sf -X POST -H "Content-Type: application/json" \
    "http://localhost:14445/admin/clients" -d "$payload" 2>/dev/null)
  if [[ -z "$resp" ]]; then
    log "ERROR: POST /admin/clients failed for $name"
    return 1
  fi
  local cid
  cid=$(printf '%s' "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("client_id",""))')
  if [[ -z "$cid" ]]; then
    log "ERROR: POST response missing client_id: $(printf '%s' "$resp" | head -c 120)"
    return 1
  fi
  log "$name created (client_id=$cid)"
  printf '%s' "$resp"
}

main() {
  log "bootstrap OAuth2 clients in Hydra ($NS_HYDRA/$HYDRA_ADMIN_SVC)"

  # Preventive cleanup of previous port-forwards before opening a new one.
  pkill -f "kubectl.*port-forward.*ory-hydra-admin" 2>/dev/null || true
  sleep 1

  # A single port-forward, kept open for the whole execution of main.
  kubectl --context "${CORE_CONTEXT:-k3d-loopruntime-core}" -n "$NS_HYDRA" port-forward "svc/$HYDRA_ADMIN_SVC" "14445:$HYDRA_ADMIN_PORT" >/dev/null 2>&1 &
  local pf_pid=$!
  trap "kill $pf_pid 2>/dev/null || true" EXIT
  sleep 4

  wait_for_hydra || exit 1

  # 1) loopruntime-cli — public client (device flow + PKCE).
  local loopruntime_cli_payload='{
    "client_id": "loopruntime-cli",
    "client_name": "loopruntime-cli",
    "grant_types": ["urn:ietf:params:oauth:grant-type:device_code", "refresh_token", "authorization_code"],
    "response_types": ["code"],
    "scope": "openid offline_access",
    "token_endpoint_auth_method": "none",
    "redirect_uris": [
      "http://localhost:9876/auth",
      "http://localhost:3000/login/callback"
    ]
  }'
  upsert_client "loopruntime-cli" "$loopruntime_cli_payload" >/dev/null || exit 1

  # 2) loopruntime-bootstrap — confidential client (client_credentials).
  # Used by dogfood loops, CI automation, maintenance scripts.
  local loopruntime_bootstrap_payload='{
    "client_id": "loopruntime-bootstrap",
    "client_name": "loopruntime-bootstrap",
    "grant_types": ["client_credentials"],
    "response_types": [],
    "scope": "openid offline_access",
    "audience": ["loopruntime-engine"],
    "token_endpoint_auth_method": "client_secret_post"
  }'
  local resp
  resp=$(upsert_client "loopruntime-bootstrap" "$loopruntime_bootstrap_payload") || exit 1

  # Persist client_secret if the client was newly created. PUT in Hydra returns 200
  # but does NOT return the existing secret — to rotate, use rotate-secret.
  local secret
  secret=$(printf '%s' "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("client_secret",""))' 2>/dev/null || true)
  if [[ -n "$secret" ]]; then
    log "persisting loopruntime-bootstrap client_secret to Secret $NS_LOOPRUNTIME/loopruntime-bootstrap-credentials"
    kubectl --context "${CORE_CONTEXT:-k3d-loopruntime-core}" -n "$NS_LOOPRUNTIME" create secret generic loopruntime-bootstrap-credentials \
      --from-literal=client_id=loopruntime-bootstrap \
      --from-literal=client_secret="$secret" \
      --dry-run=client -o yaml | kubectl --context "${CORE_CONTEXT:-k3d-loopruntime-core}" apply -f - >/dev/null
  else
    log "loopruntime-bootstrap client_secret not in response (PUT path) — Secret not updated; rotate via Hydra admin if needed"
  fi

  log "bootstrap complete"
}

main "$@"
