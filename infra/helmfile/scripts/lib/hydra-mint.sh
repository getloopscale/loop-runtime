#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan v1.2 T4.4 incremental — Shared Hydra JWT mint lib.
#
# Eliminates duplication between drill-mint-jwt.sh + seed-local-user.sh.
# Both scripts had identical Hydra client_credentials mint flow with only
# (1) Hydra namespace, (2) port-forward target port, (3) kubectl auth
# context differing per env.
#
# Per ADR D3 (scripts param by env): this lib accepts env-resolved
# inputs and emits JWT to stdout. Callers (drill / local) compose the
# env-specific resolution before sourcing this lib.
#
# Usage (sourced, not executed directly):
#
#   source infra/helmfile/scripts/lib/hydra-mint.sh
#
#   # Drill caller (DOKS):
#   HYDRA_NAMESPACE=loop-runtime-auth \
#     HYDRA_PORT_LOCAL=4444 \
#     KUBECTL_CMD="kubectl --kubeconfig=$DRILL_KUBECONFIG --insecure-skip-tls-verify" \
#     CLIENT_ID=loop-runtime-engine \
#     CLIENT_SECRET=$DRILL_HYDRA_SECRET \
#     mint_jwt
#
#   # Local caller (k3d):
#   HYDRA_NAMESPACE=loop-runtime-system \
#     HYDRA_PORT_LOCAL=14444 \
#     KUBECTL_CMD="kubectl --context $SEED_CTX" \
#     CLIENT_ID=loop-runtime-engine \
#     CLIENT_SECRET=$LOCAL_HYDRA_SECRET \
#     mint_jwt
#
# Output:
#   - JWT printed to stdout (single line)
#   - Exit 0 on success, 1 on Hydra unreachable, 2 on mint failure

# --- Pre-flight check ---
hydra_mint_check_deps() {
  if ! command -v curl >/dev/null; then
    echo "[hydra-mint] ERROR: curl required" >&2
    return 1
  fi
  if ! command -v jq >/dev/null && ! command -v python3 >/dev/null; then
    echo "[hydra-mint] ERROR: jq or python3 required for JSON parsing" >&2
    return 1
  fi
  return 0
}

# --- Kill any prior port-forward on the same port (idempotent) ---
hydra_mint_kill_prior_pf() {
  local port="${1:?port required}"
  pkill -f "kubectl.*port-forward.*ory-hydra-public.*${port}:" 2>/dev/null || true
  sleep 0.5
}

# --- Start port-forward in background, wait for readiness ---
hydra_mint_start_pf() {
  local kubectl_cmd="${1:?kubectl_cmd required}"
  local namespace="${2:?namespace required}"
  local local_port="${3:?local_port required}"
  local target_port="${4:-4444}"

  hydra_mint_kill_prior_pf "$local_port"

  # shellcheck disable=SC2086  # kubectl_cmd intentionally word-split
  $kubectl_cmd -n "$namespace" port-forward "svc/ory-hydra-public" \
    "${local_port}:${target_port}" >/dev/null 2>&1 &
  local pf_pid=$!

  # Wait up to 10s for port to be ready
  local i=0
  while [[ $i -lt 20 ]]; do
    if curl -sf "http://localhost:${local_port}/.well-known/openid-configuration" >/dev/null 2>&1; then
      echo "$pf_pid"
      return 0
    fi
    sleep 0.5
    i=$((i+1))
  done

  kill "$pf_pid" 2>/dev/null || true
  echo "[hydra-mint] ERROR: Hydra port-forward not ready after 10s" >&2
  return 1
}

# --- Mint JWT via client_credentials grant ---
# Inputs (env vars):
#   HYDRA_NAMESPACE   — kubernetes namespace where ory-hydra-public svc lives
#   HYDRA_PORT_LOCAL  — local port to bind port-forward
#   KUBECTL_CMD       — kubectl command prefix (e.g., "kubectl --context X")
#   CLIENT_ID         — OAuth client_id
#   CLIENT_SECRET     — OAuth client_secret
#   SCOPE             — optional scope (default: empty)
#   AUDIENCE          — optional audience (default: empty; drill uses "loop-runtime-engine")
#   MIN_JWT_TTL       — optional min TTL seconds (default: 0 disabled; drill EC-07 uses 7200)
# Output: JWT to stdout
# Exit: 0 success, 2 mint failure, 4 JWT TTL too short (EC-07)
mint_jwt() {
  hydra_mint_check_deps || return 1

  local pf_pid
  pf_pid=$(hydra_mint_start_pf "$KUBECTL_CMD" "$HYDRA_NAMESPACE" "$HYDRA_PORT_LOCAL") || return 1

  # Mint via OAuth2 token endpoint
  local token_resp
  token_resp=$(curl -sf -X POST "http://localhost:${HYDRA_PORT_LOCAL}/oauth2/token" \
    -d "grant_type=client_credentials" \
    -d "client_id=${CLIENT_ID}" \
    -d "client_secret=${CLIENT_SECRET}" \
    ${SCOPE:+-d "scope=${SCOPE}"} \
    ${AUDIENCE:+-d "audience=${AUDIENCE}"} \
    2>&1) || {
    kill "$pf_pid" 2>/dev/null || true
    echo "[hydra-mint] ERROR: curl failed: $token_resp" >&2
    return 2
  }

  kill "$pf_pid" 2>/dev/null || true

  # Extract access_token (prefer jq; fall back to python3)
  local jwt
  if command -v jq >/dev/null; then
    jwt=$(echo "$token_resp" | jq -r '.access_token // empty')
  else
    jwt=$(echo "$token_resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("access_token",""))')
  fi

  if [[ -z "$jwt" ]]; then
    echo "[hydra-mint] ERROR: empty JWT (token_resp: $token_resp)" >&2
    return 2
  fi

  # EC-07 TTL validation (drill must have JWT TTL >= 2h to cover L3 60min spec)
  if [[ -n "${MIN_JWT_TTL:-}" && "$MIN_JWT_TTL" -gt 0 ]]; then
    local payload_b64
    payload_b64=$(echo "$jwt" | cut -d. -f2)
    case $((${#payload_b64} % 4)) in
      2) payload_b64="${payload_b64}==" ;;
      3) payload_b64="${payload_b64}=" ;;
    esac
    local payload_json exp iat ttl
    payload_json=$(echo "$payload_b64" | tr '_-' '/+' | base64 -d 2>/dev/null || true)
    exp=$(echo "$payload_json" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("exp",0))' 2>/dev/null || echo 0)
    iat=$(echo "$payload_json" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("iat",0))' 2>/dev/null || echo 0)
    ttl=$((exp - iat))
    if [[ "$ttl" -lt "$MIN_JWT_TTL" ]]; then
      echo "[hydra-mint] ERROR: JWT TTL ${ttl}s < min ${MIN_JWT_TTL}s (EC-07 fail)" >&2
      return 4
    fi
  fi

  echo "$jwt"
  return 0
}

# Self-test if invoked directly (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "infra/helmfile/scripts/lib/hydra-mint.sh — Hydra JWT mint library"
  echo ""
  echo "Sourced (not executed directly). Functions:"
  echo "  hydra_mint_check_deps           — verify curl + jq/python3 present"
  echo "  hydra_mint_kill_prior_pf PORT   — kill stale port-forward"
  echo "  hydra_mint_start_pf KCTL NS PORT [TARGET] — start + wait readiness"
  echo "  mint_jwt                        — full flow via env vars"
  echo ""
  echo "See header for usage example."
fi
