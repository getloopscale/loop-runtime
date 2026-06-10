#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T5.1-T5.9 of plan 2026-06-02-openbao-adoption v1.3.
#
# Hermetic test for per-cluster K8s auth mounts + per-binary roles in
# infra/helm/openbao-init. Asserts:
#   (T5.1) auth/k8s-mgmt mount enabled
#   (T5.2) auth/k8s-build mount enabled
#   (T5.3) auth/k8s-cell-default mount enabled
#   (T5.4) auth/k8s-cell-1 + auth/k8s-cell-2 mounts enabled
#   (T5.5) loopzero-api role on k8s-mgmt with TTL 1h
#   (T5.6) loopzero-tenant-operator role on k8s-mgmt
#   (T5.7) loopzero-app/preview/platform-operator roles on k8s-mgmt
#   (T5.8) argo-workflows role on k8s-build
#   (T5.9) external-secrets-broker role on each k8s-cell-* mount

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART_DIR="${REPO_ROOT}/infra/helm/openbao-init"

cd "${REPO_ROOT}"

FAIL_COUNT=0
PASS_COUNT=0

red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  green "  PASS: $*"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  red   "  FAIL: $*"
}

RENDER=$(helm template openbao-init ./infra/helm/openbao-init 2>&1 || true)
if [ -z "${RENDER}" ]; then
  red "FATAL: helm template returned empty output"
  exit 2
fi

blue "== T5.1: auth/k8s-mgmt mount enabled =="
if echo "${RENDER}" | grep -qE 'enable_auth "k8s-mgmt" "kubernetes"'; then
  pass "auth/k8s-mgmt enable_auth bao command rendered"
else
  fail "auth/k8s-mgmt enable_auth bao command MISSING"
fi

blue "== T5.2: auth/k8s-build mount enabled =="
if echo "${RENDER}" | grep -qE 'enable_auth "k8s-build" "kubernetes"'; then
  pass "auth/k8s-build enable_auth bao command rendered"
else
  fail "auth/k8s-build enable_auth bao command MISSING"
fi

blue "== T5.3: auth/k8s-cell-default mount enabled =="
if echo "${RENDER}" | grep -qE 'enable_auth "k8s-cell-default" "kubernetes"'; then
  pass "auth/k8s-cell-default enable_auth bao command rendered"
else
  fail "auth/k8s-cell-default enable_auth bao command MISSING"
fi

blue "== T5.4: auth/k8s-cell-1 + auth/k8s-cell-2 mounts enabled =="
if echo "${RENDER}" | grep -qE 'enable_auth "k8s-cell-1" "kubernetes"'; then
  pass "auth/k8s-cell-1 enable_auth bao command rendered"
else
  fail "auth/k8s-cell-1 enable_auth bao command MISSING"
fi
if echo "${RENDER}" | grep -qE 'enable_auth "k8s-cell-2" "kubernetes"'; then
  pass "auth/k8s-cell-2 enable_auth bao command rendered"
else
  fail "auth/k8s-cell-2 enable_auth bao command MISSING"
fi

blue "== T5.5: loopzero-api role on k8s-mgmt with TTL 1h =="
if echo "${RENDER}" | grep -qE 'write_auth_role "k8s-mgmt" "loopzero-api"'; then
  pass "loopzero-api role on k8s-mgmt rendered"
else
  fail "loopzero-api role on k8s-mgmt MISSING"
fi
LOOPZERO_API_ROLE_LINE=$(echo "${RENDER}" | grep -A4 'write_auth_role "k8s-mgmt" "loopzero-api"' | head -5)
if echo "${LOOPZERO_API_ROLE_LINE}" | grep -qE '"1h"'; then
  pass "loopzero-api TTL = 1h"
else
  fail "loopzero-api TTL != 1h"
fi

blue "== T5.6: loopzero-tenant-operator role on k8s-mgmt =="
if echo "${RENDER}" | grep -qE 'write_auth_role "k8s-mgmt" "loopzero-tenant-operator"'; then
  pass "loopzero-tenant-operator role on k8s-mgmt rendered"
else
  fail "loopzero-tenant-operator role on k8s-mgmt MISSING"
fi

blue "== T5.7: loopzero-{app,preview,platform}-operator roles on k8s-mgmt =="
for binary in loopzero-app-operator loopzero-preview-operator loopzero-platform-operator; do
  if echo "${RENDER}" | grep -qE "write_auth_role \"k8s-mgmt\" \"${binary}\""; then
    pass "${binary} role on k8s-mgmt rendered"
  else
    fail "${binary} role on k8s-mgmt MISSING"
  fi
done

blue "== T5.8: argo-workflows role on k8s-build =="
if echo "${RENDER}" | grep -qE 'write_auth_role "k8s-build" "argo-workflows"'; then
  pass "argo-workflows role on k8s-build rendered"
else
  fail "argo-workflows role on k8s-build MISSING"
fi

blue "== T5.9: external-secrets-broker role on each k8s-cell-* mount =="
for cell in k8s-cell-default k8s-cell-1 k8s-cell-2; do
  if echo "${RENDER}" | grep -qE "write_auth_role \"${cell}\" \"external-secrets-broker\""; then
    pass "external-secrets-broker role on ${cell} rendered"
  else
    fail "external-secrets-broker role on ${cell} MISSING"
  fi
done

blue "== Isolation invariant: NO loopzero-api role on cell-* mounts =="
# Negative assertion: loopzero-api ONLY lives on k8s-mgmt; if accidentally
# rendered on a cell mount, the SA-cluster isolation is broken.
for cell in k8s-cell-default k8s-cell-1 k8s-cell-2; do
  if echo "${RENDER}" | grep -qE "write_auth_role \"${cell}\" \"loopzero-api\""; then
    fail "loopzero-api role LEAKED to ${cell} mount (isolation broken)"
  else
    pass "loopzero-api absent from ${cell} (isolation preserved)"
  fi
done

blue "== Isolation invariant: NO external-secrets-broker on k8s-mgmt =="
# Negative assertion: ESO broker SA only lives in cells; if accidentally
# rendered on k8s-mgmt, ESO could read secrets meant for mgmt-side workloads.
if echo "${RENDER}" | grep -qE 'write_auth_role "k8s-mgmt" "external-secrets-broker"'; then
  fail "external-secrets-broker role LEAKED to k8s-mgmt mount (isolation broken)"
else
  pass "external-secrets-broker absent from k8s-mgmt (isolation preserved)"
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
