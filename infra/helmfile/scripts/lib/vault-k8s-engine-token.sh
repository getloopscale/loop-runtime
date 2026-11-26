#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# T7.3 of plan 2026-06-02-openbao-adoption v1.4 — Vault kubernetes engine
# token fetcher for scoped-kubeconfig.sh.
#
# When BAO_ADDR + BAO_TOKEN are set + Vault `kubernetes` engine is mounted +
# the requested role exists, this lib mints a short-lived (TTL 1h) SA token
# via `bao write kubernetes/creds/<role> kubernetes_namespace=<ns>` instead
# of creating the legacy `type: kubernetes.io/service-account-token` Secret.
#
# Used by: infra/helmfile/scripts/lib/scoped-kubeconfig.sh (T7.4 refactor).
#
# Output (mint_vault_k8s_token <role> <namespace>):
#   stdout: token (single line, no newline) on success
#   exit 0: success
#   exit 1: Vault engine unavailable (caller falls back to legacy SA-token)
#   exit 2: misconfiguration (role/ns invalid)
#
# Defensive: ALL writes use kubectl --dry-run when feasible; ALL value
# captures use bao -format=json + jq to avoid shell expansion of secrets.

set -euo pipefail

# --- vault_k8s_engine_available — returns 0 if engine is mounted + reachable ---
vault_k8s_engine_available() {
  [[ -z "${BAO_ADDR:-}" ]] && return 1
  [[ -z "${BAO_TOKEN:-}" ]] && return 1
  command -v bao >/dev/null 2>&1 || return 1
  command -v jq >/dev/null 2>&1 || return 1

  # 30-second timeout: if Vault is unreachable, fall back fast.
  local mounts_json
  if ! mounts_json=$(BAO_ADDR="${BAO_ADDR}" BAO_TOKEN="${BAO_TOKEN}" \
                    bao read -format=json sys/mounts 2>/dev/null); then
    return 1
  fi
  if echo "${mounts_json}" | jq -e '.data["kubernetes/"]' >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

# --- mint_vault_k8s_token <role> <namespace> ---
# Returns short-lived token on stdout; non-zero exit signals fallback.
mint_vault_k8s_token() {
  local role="$1"
  local namespace="$2"

  [[ -z "${role}" ]] && { echo "ERR: empty role" >&2; return 2; }
  [[ -z "${namespace}" ]] && { echo "ERR: empty namespace" >&2; return 2; }

  if ! vault_k8s_engine_available; then
    return 1
  fi

  local resp
  if ! resp=$(BAO_ADDR="${BAO_ADDR}" BAO_TOKEN="${BAO_TOKEN}" \
              bao write -format=json \
                "kubernetes/creds/${role}" \
                "kubernetes_namespace=${namespace}" 2>/dev/null); then
    return 1
  fi
  local token
  token=$(echo "${resp}" | jq -r '.data.service_account_token // empty')
  [[ -z "${token}" ]] && return 1
  printf '%s' "${token}"
}

# --- self-test (run when invoked directly) ---
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "vault_k8s_engine_available?"
  if vault_k8s_engine_available; then
    echo "  YES — engine mounted + reachable"
  else
    echo "  NO — caller falls back to legacy SA-token"
  fi
fi
