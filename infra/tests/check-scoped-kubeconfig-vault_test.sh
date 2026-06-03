#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T7.3 of plan 2026-06-02-openbao-adoption v1.4.
#
# Hermetic test for scoped-kubeconfig.sh Vault kubernetes engine integration.
# Asserts:
#   (1) vault-k8s-engine-token.sh lib exists + has expected functions
#   (2) scoped-kubeconfig.sh sources the lib + has Vault-first conditional
#   (3) CLUSTER_ID → VAULT_ROLE mapping (build/cell/workload/mgmt)
#   (4) Legacy fallback path still present (cold-bootstrap chicken-and-egg)
#
# Hermetic: pure grep + bash parse; no kubectl/bao required.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCOPED="${REPO_ROOT}/infra/helmfile/scripts/lib/scoped-kubeconfig.sh"
LIB="${REPO_ROOT}/infra/helmfile/scripts/lib/vault-k8s-engine-token.sh"

FAIL_COUNT=0
PASS_COUNT=0
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }
pass() { PASS_COUNT=$((PASS_COUNT + 1)); green "  PASS: $*"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); red "  FAIL: $*"; }

blue "== Lib file exists =="
if [ -f "${LIB}" ]; then
  pass "vault-k8s-engine-token.sh present"
else
  fail "vault-k8s-engine-token.sh MISSING"
fi

blue "== Lib has expected functions =="
for fn in vault_k8s_engine_available mint_vault_k8s_token; do
  if grep -qE "^${fn}\(\)" "${LIB}"; then
    pass "${fn} declared"
  else
    fail "${fn} MISSING"
  fi
done

blue "== scoped-kubeconfig.sh sources the lib =="
if grep -qE "source.*vault-k8s-engine-token.sh|\\. .*vault-k8s-engine-token.sh" "${SCOPED}"; then
  pass "lib sourced"
else
  fail "lib NOT sourced"
fi

blue "== Vault-first conditional present =="
if grep -qE "if declare -f mint_vault_k8s_token" "${SCOPED}"; then
  pass "Vault path tried first"
else
  fail "Vault path NOT tried first"
fi

blue "== CLUSTER_ID → VAULT_ROLE mapping =="
# Three canonical roles (cell* + workload* both resolve to loopzero-runtime-workload).
for combo in "build" "workload" "mgmt"; do
  if grep -qE "loopzero-runtime-${combo}" "${SCOPED}"; then
    pass "loopzero-runtime-${combo} role mapped"
  else
    fail "loopzero-runtime-${combo} role MISSING from CLUSTER_ID switch"
  fi
done
if grep -qE 'cell\*\|workload\*' "${SCOPED}"; then
  pass "cell* + workload* case branch unified"
else
  fail "cell* case branch MISSING (cells fall through to mgmt by mistake)"
fi

blue "== Legacy fallback still present =="
if grep -qE 'kubernetes\.io/service-account-token' "${SCOPED}"; then
  pass "legacy SA-token Secret creation path present (cold-bootstrap fallback)"
else
  fail "legacy fallback REMOVED (cold-bootstrap broken)"
fi

blue "== Fallback gated on Vault empty token =="
if grep -qE 'if \[\[ -z "\$\{TOKEN\}" \]\]' "${SCOPED}"; then
  pass "legacy path only runs when Vault returned empty token"
else
  fail "legacy path NOT gated — would run unconditionally"
fi

echo
blue "================================================================"
if [ "${FAIL_COUNT}" -eq 0 ]; then
  green "ALL PASS (${PASS_COUNT}/${PASS_COUNT})"
  exit 0
else
  red "FAILURES: ${FAIL_COUNT}/$((PASS_COUNT + FAIL_COUNT))"
  exit 1
fi
