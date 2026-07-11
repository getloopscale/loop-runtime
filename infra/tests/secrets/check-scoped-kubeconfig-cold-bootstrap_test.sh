#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Trio compatibility regression test for T7.3 cold-bootstrap path.
#
# Validates that infra/helmfile/scripts/lib/scoped-kubeconfig.sh STILL
# completes successfully when:
#   - vault-k8s-engine-token.sh lib is ABSENT (cleanroom / upgrade missed file)
#   - mint_vault_k8s_token function is NOT declared
#
# Critical for cleanroom: if a refactor accidentally removes the legacy
# Secret-type-service-account-token block, scoped-kubeconfig would fail
# silently and ALL cell-wiring breaks.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="${REPO_ROOT}/infra/helmfile/scripts/lib/scoped-kubeconfig.sh"

FAIL=0; PASS=0
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }
pass() { PASS=$((PASS+1)); green "  PASS: $*"; }
fail() { FAIL=$((FAIL+1)); red "  FAIL: $*"; }

blue "== Source line uses conditional [ -f ] guard =="
# Pattern: [ -f "${SCRIPT_LIB_DIR}/vault-k8s-engine-token.sh" ] && source ...
if grep -qE '\[ -f.*vault-k8s-engine-token.sh.*\] && source' "${SCRIPT}"; then
  pass "Vault lib source guarded by [ -f ] (no error when file absent)"
else
  fail "Source NOT guarded — cold-bootstrap fails when lib missing"
fi

blue "== Vault path gated by declare -f check (not bash -e fail-fast) =="
if grep -qE 'declare -f mint_vault_k8s_token' "${SCRIPT}"; then
  pass "Vault path gated by 'declare -f mint_vault_k8s_token'"
else
  fail "Vault path NOT gated by declare -f — would fail when function missing"
fi

blue "== Legacy SA-token Secret creation block PRESENT as fallback =="
if grep -qE 'kubernetes\.io/service-account-token' "${SCRIPT}"; then
  pass "Legacy 'type: kubernetes.io/service-account-token' Secret creation present"
else
  fail "Legacy SA-token Secret REMOVED — cold-bootstrap fails (chicken-and-egg)"
fi

blue "== Legacy block GATED on Vault-returned empty TOKEN =="
# Pattern: `if [[ -z "${TOKEN}" ]]; then` after Vault attempt
if grep -qE 'if \[\[ -z "\$\{TOKEN\}" \]\]' "${SCRIPT}"; then
  pass "Legacy block ONLY runs when Vault returned empty TOKEN"
else
  fail "Legacy block NOT gated — would always run (defeats Vault refactor)"
fi

blue "== CA fallback to kube-root-ca.crt ConfigMap when Vault path used =="
# When Vault returns a token, no Secret is created, so the CA must come
# from the kube-root-ca.crt ConfigMap (canonical CA source).
if grep -qE 'kube-root-ca\.crt' "${SCRIPT}"; then
  pass "CA fallback to kube-root-ca.crt ConfigMap present"
else
  fail "CA fallback MISSING — Vault path produces kubeconfig without CA"
fi

blue "== Wait loop for token-populated still works (legacy resilience) =="
if grep -qE 'for _ in 1 2 3 4 5 6 7 8 9 10' "${SCRIPT}"; then
  pass "10-iteration wait loop for legacy SA token still present"
else
  fail "Legacy wait loop REMOVED — race condition on token populate"
fi

echo
blue "================================================================"
if [ "${FAIL}" -eq 0 ]; then
  green "ALL PASS (${PASS}/${PASS})"
  exit 0
else
  red "FAILURES: ${FAIL}/$((PASS + FAIL))"
  exit 1
fi
