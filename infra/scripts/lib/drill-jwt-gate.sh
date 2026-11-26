#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# drill-jwt-gate.sh — JWT auth wiring validator for drill bootstrap.
#
# Plan: 2026-05-19-drill-skills-rigor-restoration-plan T2.2
# ADR D2: drift cross-cluster (Hydra issuer vs loop-runtime-api JWT_ISSUER) must be
# caught at bootstrap time, not via tardy orchestrator workaround.
#
# Function `verify_jwt_auth_e2e` mints a JWT via drill-mint-jwt.sh and curls
# the loop-runtime-api /projects endpoint. If the response is NOT 2xx (typically 401 or
# 403 when issuer/audience/JWKS drift), the function exits non-zero so the
# caller (bootstrap) can abort BEFORE Phase 5 cell wiring.
#
# Sourced (not executed). All testable via mocked curl/mint commands.

# Source guard
if [[ -n "${_DRILL_JWT_GATE_SOURCED:-}" ]]; then
  return 0
fi
_DRILL_JWT_GATE_SOURCED=1

# --- verify_jwt_auth_e2e ---
# Args:
#   $1 api_url    — loop-runtime-api base URL (e.g., https://api.drill-...)
#   $2 mint_cmd   — command to mint JWT (default: drill-mint-jwt.sh path)
#   $3 curl_bin   — curl binary (default: curl) — overridable for tests
#   $4 tenant_id  — tenant ID for X-Loop-Runtime-Tenant header (default: tn-e2etest)
# Exit: 0 if 2xx; 1 if mint fails or non-2xx response
# Stdout: structured status lines
verify_jwt_auth_e2e() {
  local api_url="${1:?api_url required}"
  local mint_cmd="${2:?mint_cmd required}"
  local curl_bin="${3:-curl}"
  local tenant_id="${4:-tn-e2etest}"

  # Step 1: mint fresh JWT (invalidate any local cache per T2.1 EC-2)
  rm -f ~/.loop-runtime/drill-token.txt ~/.loop-runtime/drill-token-cache.json 2>/dev/null || true

  local jwt
  jwt=$(bash -c "$mint_cmd" 2>/dev/null)
  if [[ -z "$jwt" || "$jwt" != *.*.* ]]; then
    echo "drill-jwt-gate: JWT mint failed (mint_cmd=$mint_cmd)"
    return 1
  fi

  # Step 2: curl /projects with JWT
  local status
  status=$("$curl_bin" -sS -m 30 \
    -H "Authorization: Bearer $jwt" \
    -H "X-Loop-Runtime-Tenant: $tenant_id" \
    -o /dev/null -w "%{http_code}" \
    "${api_url}/projects" 2>/dev/null || echo "000")

  # Step 3: assert 2xx
  case "$status" in
    2*)
      echo "drill-jwt-gate: PASS (HTTP $status)"
      return 0
      ;;
    *)
      echo "drill-jwt-gate: FAIL (HTTP $status — expected 2xx)"
      echo "drill-jwt-gate: investigate via infra/scripts/drill/jwt-diagnose.sh"
      return 1
      ;;
  esac
}
