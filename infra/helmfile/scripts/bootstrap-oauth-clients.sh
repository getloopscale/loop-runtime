#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD F-NEW (Rule 10 — Local equals production)
#
# Registra os OAuth2 clients no Ory Hydra que a plataforma exige para
# auth funcionar end-to-end. Em produção (DOKS), esse passo é o "Step 6"
# do install procedure — referenciado em
# `infra/tests/e2e/local-install-deploy.sh:222` como fail-loud check
# ("loopzero-cli OAuth2 client not registered (Step 6 of install)"). Em
# standalone-multi o passo NÃO existia, deixando Hydra vazio e quebrando
# `loopzero login` (device flow → 502 AUTH_SERVICE_ERROR).
#
# Idempotente: usa GET primeiro, POST se ausente, PUT se presente. Sem
# tocar em código de produto — todo o fluxo via Hydra admin REST API,
# alinhado com o pattern dos outros scripts L2 (generate-secrets.sh,
# wire-clusters.sh).
#
# Clients registrados:
#   1. loopzero-cli — public client (token_endpoint_auth_method=none) com
#      grant_types: device_code + refresh_token + authorization_code.
#      Usado por `loopzero login` device flow + dashboard PKCE.
#   2. loopzero-bootstrap — confidential client com client_credentials
#      grant. Usado por automation/CI/dogfood loops para obter token
#      machine-to-machine sem browser.
#
# Usage (standalone):
#   KUBECONFIG=$(k3d kubeconfig write loopzero-mgmt) \
#     bash infra/helmfile/scripts/bootstrap-oauth-clients.sh
#
# Usage (helmfile hook):
#   chamado pelo mgmt.helmfile.yaml.gotmpl post-sync hook após Ory Hydra ready.
#
# Outputs (somente quando bootstrap-oauth-clients é executado pela primeira
# vez ou após delete): client_secret do loopzero-bootstrap em
# Secret loopzero-system/loopzero-bootstrap-credentials para uso por jobs/CI/CLI.
set -euo pipefail

NS_HYDRA="${HYDRA_NAMESPACE:-loopzero-auth}"
NS_LOOPZERO="${LOOPZERO_NAMESPACE:-loopzero-system}"
HYDRA_ADMIN_SVC="${HYDRA_ADMIN_SVC:-ory-hydra-admin}"
HYDRA_ADMIN_PORT="${HYDRA_ADMIN_PORT:-4445}"

log() { printf '[bootstrap-oauth-clients] %s\n' "$*" >&2; }

# Wait for Hydra admin to be reachable. Em fresh install Hydra leva até
# ~60s para terminar migrate + ficar Ready. Sem este wait, RegisterClient
# em loopzero-cloud (que dispara no startup) corre antes e gera 404 silencioso.
#
# Port-forward é mantido aberto pelo caller (main()) para evitar
# race-condition de duas conexões TCP sobrepostas (problema observado:
# trap RETURN fecha o forward antes do próximo curl).
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

# upsert_client(name, payload_json) — idempotente. GET, POST se ausente,
# PUT se presente. Stdin: payload completo do client.
upsert_client() {
  local name="$1" payload="$2"
  local code

  # Probe direto por client_id (chave estável). client_name pode variar
  # ("loopzero-cli" vs "LoopZero CLI") entre paths de criação — não usar como
  # discriminador. GET /admin/clients/{id} retorna 404 se não existe.
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

  # Limpeza preventiva de port-forwards anteriores antes de abrir um novo.
  pkill -f "kubectl.*port-forward.*ory-hydra-admin" 2>/dev/null || true
  sleep 1

  # Port-forward único, mantido por toda a execução do main.
  kubectl --context "${MGMT_CONTEXT:-k3d-loopzero-mgmt}" -n "$NS_HYDRA" port-forward "svc/$HYDRA_ADMIN_SVC" "14445:$HYDRA_ADMIN_PORT" >/dev/null 2>&1 &
  local pf_pid=$!
  trap "kill $pf_pid 2>/dev/null || true" EXIT
  sleep 4

  wait_for_hydra || exit 1

  # 1) loopzero-cli — public client (device flow + PKCE).
  local loopzero_cli_payload='{
    "client_id": "loopzero-cli",
    "client_name": "loopzero-cli",
    "grant_types": ["urn:ietf:params:oauth:grant-type:device_code", "refresh_token", "authorization_code"],
    "response_types": ["code"],
    "scope": "openid offline_access",
    "token_endpoint_auth_method": "none",
    "redirect_uris": [
      "http://localhost:9876/auth",
      "http://localhost:3000/login/callback"
    ]
  }'
  upsert_client "loopzero-cli" "$loopzero_cli_payload" >/dev/null || exit 1

  # 2) loopzero-bootstrap — confidential client (client_credentials).
  # Usado por dogfood loops, CI automation, script de manutenção.
  local loopzero_bootstrap_payload='{
    "client_id": "loopzero-bootstrap",
    "client_name": "loopzero-bootstrap",
    "grant_types": ["client_credentials"],
    "response_types": [],
    "scope": "openid offline_access",
    "audience": ["loopzero-engine"],
    "token_endpoint_auth_method": "client_secret_post"
  }'
  local resp
  resp=$(upsert_client "loopzero-bootstrap" "$loopzero_bootstrap_payload") || exit 1

  # Persistir client_secret se foi criado novo. PUT em Hydra retorna 200
  # mas NÃO retorna o secret existente — para rotacionar use rotate-secret.
  local secret
  secret=$(printf '%s' "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("client_secret",""))' 2>/dev/null || true)
  if [[ -n "$secret" ]]; then
    log "persisting loopzero-bootstrap client_secret to Secret $NS_LOOPZERO/loopzero-bootstrap-credentials"
    kubectl --context "${MGMT_CONTEXT:-k3d-loopzero-mgmt}" -n "$NS_LOOPZERO" create secret generic loopzero-bootstrap-credentials \
      --from-literal=client_id=loopzero-bootstrap \
      --from-literal=client_secret="$secret" \
      --dry-run=client -o yaml | kubectl --context "${MGMT_CONTEXT:-k3d-loopzero-mgmt}" apply -f - >/dev/null
  else
    log "loopzero-bootstrap client_secret not in response (PUT path) — Secret not updated; rotate via Hydra admin if needed"
  fi

  log "bootstrap complete"
}

main "$@"
