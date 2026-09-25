#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T5.11 of plan 2026-06-02-openbao-adoption v1.3.
#
# Hermetic test for the loopzero-api projected SA token, on the one loopzero-api chart
# (charts/loopzero-api), in both of its installs: ArgoCD with the chart defaults and
# the Helmfile bootstrap with charts/loopzero-api/values-bootstrap.yaml.
#
# Asserts that when .Values.vault.enabled=true the chart renders:
#   (a) a projected serviceAccountToken volume with audience=vault.loopzero.io
#   (b) expirationSeconds: 3600
#   (c) a volumeMount at /var/run/secrets/vault.loopzero.io
# When vault.enabled=false (the default of both installs), neither is rendered.
#
# This test covered a second chart, infra/helm/loopzero-api, until it was deleted when
# the pair became one chart (docs/decisions.md, "One chart per bootstrap/published
# pair"). That chart also rendered LOOPZERO_VAULT_ADDR, LOOPZERO_VAULT_K8S_AUTH_PATH,
# LOOPZERO_VAULT_ROLE and LOOPZERO_VAULT_SA_JWT_PATH when vault was enabled; no binary reads
# any of the four and no install enabled vault, so they were not carried over.

set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
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

# Helper: ensure helm deps built (loopzero-common subchart)
ensure_deps() {
  local chart_dir="$1"
  # B-082 — the cause of the intermittency, measured 2026-08-19.
  #
  # Asking whether a `charts/` directory exists sent a chart WITHOUT dependencies
  # straight to `helm dependency build`, which has nothing to build and still
  # mutates helm's SHARED cache. Under concurrent gates that mutation is the
  # contention: this test failed after 123.89s surrounded by `Adding repo`, and
  # passed in 7.4s run alone on the same checkout.
  #
  # Asking for a `dependencies:` block is the right question; asking for the
  # directory was the question that is never satisfied.
  grep -q '^dependencies:' "${chart_dir}/Chart.yaml" 2>/dev/null || return 0
  if [ ! -d "${chart_dir}/charts" ] || [ -z "$(ls -A "${chart_dir}/charts" 2>/dev/null)" ]; then
    ( cd "${chart_dir}" && helm dependency build >/dev/null 2>&1 ) || true
  fi
}

ensure_deps charts/loopzero-api

# render_or_die — captures `helm template` telling a render failure apart from a
# missing field.
#
# THE DEFECT THIS FIXES: the captures used to be
#
#     RENDER_X=$(helm template ... 2>&1 || true)
#
# `2>&1` puts helm's error INSIDE the variable and `|| true` drops the exit code.
# When the render fails — and it fails transiently, from `helm dependency`
# contention when several gates run together — the variable holds the error
# message instead of YAML, and EVERY following `grep` reports `MISSING`. The gate
# then blames a field that is present for what was a broken helm run.
#
# Measured: this test blocked a push whose diff held ONLY workflow files, and
# passed on the same commit when run again. A flaky test is a bug
# (rules/testing.md), and this one also lied about the cause.
render_or_die() {
  local label="$1"; shift
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "${label}: helm template FAILED (exit ${rc}) — this is a broken render, not a missing field:"
    printf '%s\n' "$out" | sed 's/^/      /' >&2
    exit 1
  fi
  printf '%s' "$out"
}

# ----------------------------------------------------------------
# Chart system 1: charts/loopzero-api (ArgoCD L3)
# ----------------------------------------------------------------
blue "== charts/loopzero-api: vault.enabled=true =="

RENDER_PROD_ON=$(render_or_die "charts/loopzero-api vault=on" helm template loopzero-api ./charts/loopzero-api \
  --set vault.enabled=true \
  --show-only templates/deployment.yaml )

if echo "${RENDER_PROD_ON}" | grep -qE 'name: vault-sa-token'; then
  pass "charts/loopzero-api: vault-sa-token volume + volumeMount rendered"
else
  fail "charts/loopzero-api: vault-sa-token MISSING when vault.enabled=true"
fi

if echo "${RENDER_PROD_ON}" | grep -qE 'audience: vault.loopzero.io'; then
  pass "charts/loopzero-api: audience=vault.loopzero.io rendered"
else
  fail "charts/loopzero-api: audience=vault.loopzero.io MISSING"
fi

if echo "${RENDER_PROD_ON}" | grep -qE 'expirationSeconds: 3600'; then
  pass "charts/loopzero-api: REDACTED_SECRET_043 rendered"
else
  fail "charts/loopzero-api: REDACTED_SECRET_043 MISSING"
fi

if echo "${RENDER_PROD_ON}" | grep -qE 'mountPath: /var/run/secrets/vault.loopzero.io'; then
  pass "charts/loopzero-api: mountPath /var/run/secrets/vault.loopzero.io rendered"
else
  fail "charts/loopzero-api: mountPath MISSING"
fi

blue "== charts/loopzero-api: vault.enabled=false (default) =="

RENDER_PROD_OFF=$(render_or_die "charts/loopzero-api vault=off" helm template loopzero-api ./charts/loopzero-api \
  --show-only templates/deployment.yaml )

if echo "${RENDER_PROD_OFF}" | grep -qE 'name: vault-sa-token'; then
  fail "charts/loopzero-api: vault-sa-token RENDERED when vault.enabled=false (gate broken)"
else
  pass "charts/loopzero-api: vault-sa-token absent when vault.enabled=false"
fi

# ----------------------------------------------------------------
# The Helmfile bootstrap install: charts/loopzero-api + values-bootstrap.yaml
# ----------------------------------------------------------------
blue "== charts/loopzero-api bootstrap install: vault.enabled=false (its default) =="

RENDER_BOOTSTRAP_OFF=$(render_or_die "charts/loopzero-api bootstrap vault=off" helm template loopzero-api ./charts/loopzero-api \
  -f ./charts/loopzero-api/values-bootstrap.yaml \
  --show-only templates/deployment.yaml )

if echo "${RENDER_BOOTSTRAP_OFF}" | grep -qE 'name: vault-sa-token'; then
  fail "bootstrap install: vault-sa-token RENDERED while vault is off"
else
  pass "bootstrap install: vault-sa-token absent while vault is off"
fi

blue "== charts/loopzero-api bootstrap install: vault.enabled=true =="

RENDER_BOOTSTRAP_ON=$(render_or_die "charts/loopzero-api bootstrap vault=on" helm template loopzero-api ./charts/loopzero-api \
  -f ./charts/loopzero-api/values-bootstrap.yaml \
  --set vault.enabled=true \
  --show-only templates/deployment.yaml )

if echo "${RENDER_BOOTSTRAP_ON}" | grep -qE 'audience: vault.loopzero.io'; then
  pass "bootstrap install: audience=vault.loopzero.io rendered"
else
  fail "bootstrap install: audience=vault.loopzero.io MISSING"
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
