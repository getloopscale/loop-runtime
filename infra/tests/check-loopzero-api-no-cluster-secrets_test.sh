#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T5.13 of plan 2026-06-02-openbao-adoption v1.3.
#
# Hermetic test for the legacyClusterWideSecrets backward-compat flag
# in charts/loopzero-api/templates/clusterrole.yaml. Closes deep review
# finding #1 (cluster-wide secrets/configmaps grant = catastrophic
# blast radius).
#
# Asserts:
#   (1) When .Values.rbac.legacyClusterWideSecrets is true (default),
#       helm template renders the secrets/configmaps grant.
#   (2) When set to false, helm template OMITS the secrets/configmaps
#       grant entirely (no `resources: ["secrets", "configmaps"]` line).
#   (3) The loopzero.io CRDs grant is ALWAYS present (independent of flag).
#   (4) The values.yaml default is true (backward compat preserved).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHART_DIR="${REPO_ROOT}/charts/loopzero-api"

# helm dependency build is REQUIRED before --show-only (loopzero-common subchart).
# Run once if .helm/charts/ missing (idempotent).
if [ ! -d "${CHART_DIR}/charts" ] || [ -z "$(ls -A "${CHART_DIR}/charts" 2>/dev/null)" ]; then
  ( cd "${CHART_DIR}" && helm dependency build >/dev/null 2>&1 ) || {
    echo "WARN: helm dependency build failed — test may produce empty output"
  }
fi

# helm template MUST run from a directory NOT named "charts/" (relative-path
# disambiguation rule). Run from REPO_ROOT so the path ./charts/loopzero-api is
# unambiguous.
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

blue "== Test 1: values.yaml default is true =="
DEFAULT_FLAG=$(grep -E "^[[:space:]]+legacyClusterWideSecrets:" "${CHART_DIR}/values.yaml" | awk '{print $NF}')
if [ "${DEFAULT_FLAG}" = "true" ]; then
  pass "values.yaml legacyClusterWideSecrets default = true (backward compat preserved)"
else
  fail "values.yaml legacyClusterWideSecrets default = ${DEFAULT_FLAG}, want true"
fi

blue "== Test 2: flag=true (default) renders secrets/configmaps grant =="
RENDER_TRUE=$(helm template loopzero-api ./charts/loopzero-api 2>/dev/null \
  --show-only templates/clusterrole.yaml || true)
if echo "${RENDER_TRUE}" | grep -qE 'resources:[[:space:]]*\["secrets",[[:space:]]*"configmaps"\]'; then
  pass "flag=true: secrets/configmaps cluster-wide grant present (legacy compat)"
else
  fail "flag=true: secrets/configmaps cluster-wide grant MISSING (legacy compat broken)"
fi

blue "== Test 3: flag=false omits secrets/configmaps grant =="
RENDER_FALSE=$(helm template loopzero-api ./charts/loopzero-api \
  --set rbac.legacyClusterWideSecrets=false 2>/dev/null \
  --show-only templates/clusterrole.yaml || true)
if echo "${RENDER_FALSE}" | grep -qE 'resources:[[:space:]]*\["secrets",[[:space:]]*"configmaps"\]'; then
  fail "flag=false: secrets/configmaps cluster-wide grant STILL PRESENT (deep review finding #1 NOT closed)"
else
  pass "flag=false: secrets/configmaps cluster-wide grant ABSENT (deep review finding #1 closed)"
fi

blue "== Test 4: loopzero.io CRDs grant is ALWAYS present =="
if echo "${RENDER_FALSE}" | grep -qE 'apiGroups:[[:space:]]*\["loopzero\.io"\]'; then
  pass "flag=false: loopzero.io CRDs grant preserved (control plane unaffected)"
else
  fail "flag=false: loopzero.io CRDs grant MISSING (control plane broken — over-scoped removal)"
fi

blue "== Test 5: Argo Workflows grant is ALWAYS present =="
if echo "${RENDER_FALSE}" | grep -qE 'apiGroups:[[:space:]]*\["argoproj\.io"\]'; then
  pass "flag=false: argoproj.io grant preserved (build pipeline unaffected)"
else
  fail "flag=false: argoproj.io grant MISSING (build pipeline broken — over-scoped removal)"
fi

blue "== Test 6: Namespaces create grant is ALWAYS present =="
if echo "${RENDER_FALSE}" | grep -qE 'resources:[[:space:]]*\["namespaces"\]'; then
  pass "flag=false: namespaces grant preserved (tenant provisioning unaffected)"
else
  fail "flag=false: namespaces grant MISSING (tenant provisioning broken)"
fi

echo
blue "================================================================"
if [ "${FAIL_COUNT}" -eq 0 ]; then
  green "ALL PASS (${PASS_COUNT}/6)"
  exit 0
else
  red "FAILURES: ${FAIL_COUNT}/$((PASS_COUNT + FAIL_COUNT))"
  exit 1
fi
