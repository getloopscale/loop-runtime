#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T5.11 of plan 2026-06-02-openbao-adoption v1.3.
#
# Hermetic test for loopzero-api projected SA token + Vault env vars across
# BOTH chart systems (dual chart system per CLAUDE.md):
#   - charts/loopzero-api (ArgoCD L3, OCI registry)
#   - infra/helm/loopzero-api (Helmfile L2, bootstrap)
#
# Asserts that when .Values.vault.enabled=true, BOTH charts render:
#   (a) projected serviceAccountToken volume with audience=vault.loopzero.io
#   (b) REDACTED_SECRET_043
#   (c) volumeMount at /var/run/secrets/vault.loopzero.io
#   (d) Env vars LOOPZERO_VAULT_ADDR + LOOPZERO_VAULT_K8S_AUTH_PATH + LOOPZERO_VAULT_ROLE +
#       LOOPZERO_VAULT_SA_JWT_PATH (infra chart only — charts/loopzero-api uses
#       envFrom configmap pattern for config wiring)
# When vault.enabled=false (default), neither volume nor env vars are rendered.

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
  # B-082 — a causa da intermitencia, medida em 2026-08-19.
  #
  # A condicao abaixo pergunta se existe a pasta `charts/`. Um chart SEM dependencias
  # nunca vai ter uma, entao ele caia direto no `helm dependency build` — que nao tem o
  # que construir e mesmo assim mexe no cache COMPARTILHADO do helm. Sob execucao
  # concorrente dos gates, essa mutacao e' a contencao: este teste reprovou apos 123,89s
  # cercado de `Adding repo`, e passou em 7,4s rodando isolado no mesmo checkout.
  #
  # Medido: `infra/helm/loopzero-api/Chart.yaml` declara ZERO dependencias, e
  # `charts/loopzero-api` ja' tem o `loopzero-common-0.3.0.tgz` vendorizado — `helm template`
  # nos dois funciona sem construir nada (0s, exit 0).
  #
  # Perguntar pelo bloco `dependencies:` e' a pergunta certa; perguntar pela pasta era a
  # pergunta que nunca se satisfaz.
  grep -q '^dependencies:' "${chart_dir}/Chart.yaml" 2>/dev/null || return 0
  if [ ! -d "${chart_dir}/charts" ] || [ -z "$(ls -A "${chart_dir}/charts" 2>/dev/null)" ]; then
    ( cd "${chart_dir}" && helm dependency build >/dev/null 2>&1 ) || true
  fi
}

ensure_deps charts/loopzero-api
ensure_deps infra/helm/loopzero-api

# render_or_die — captura o `helm template` DISTINGUINDO falha de render de
# campo ausente.
#
# O DEFEITO QUE ISTO CONSERTA: as quatro capturas abaixo usavam
#
#     RENDER_X=$(helm template ... 2>&1 || true)
#
# O `2>&1` joga o erro do helm DENTRO da variavel e o `|| true` descarta o exit
# code. Quando o render falha — e ele falha de forma transitoria, por contencao
# de `helm dependency` quando varios gates rodam juntos — a variavel passa a
# conter a mensagem de erro em vez de YAML, e TODO `grep` seguinte reporta
# `MISSING`. O gate acusa "env LOOPZERO_VAULT_K8S_AUTH_PATH MISSING" quando o campo
# esta la' e o que quebrou foi o helm.
#
# Medido: este teste barrou um push cujo diff continha SO' arquivos de workflow,
# e passou verde no mesmo commit ao ser rodado de novo. Teste intermitente e'
# bug (rules/testing.md), e aqui a intermitencia ainda MENTIA sobre a causa —
# manda o proximo a debugar um chart que nunca esteve quebrado.
render_or_die() {
  local label="$1"; shift
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "${label}: helm template FALHOU (exit ${rc}) — isto NAO e' campo ausente, e' render quebrado:"
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
# Chart system 2: infra/helm/loopzero-api (Helmfile L2)
# ----------------------------------------------------------------
blue "== infra/helm/loopzero-api: vault.enabled=true =="

RENDER_INFRA_ON=$(render_or_die "infra/helm/loopzero-api vault=on" helm template loopzero-api ./infra/helm/loopzero-api \
  --set vault.enabled=true \
  --show-only templates/deployment.yaml )

if echo "${RENDER_INFRA_ON}" | grep -qE 'name: vault-sa-token'; then
  pass "infra/helm/loopzero-api: vault-sa-token volume + volumeMount rendered"
else
  fail "infra/helm/loopzero-api: vault-sa-token MISSING when vault.enabled=true"
fi

if echo "${RENDER_INFRA_ON}" | grep -qE 'audience: vault.loopzero.io'; then
  pass "infra/helm/loopzero-api: audience=vault.loopzero.io rendered"
else
  fail "infra/helm/loopzero-api: audience=vault.loopzero.io MISSING"
fi

if echo "${RENDER_INFRA_ON}" | grep -qE 'expirationSeconds: 3600'; then
  pass "infra/helm/loopzero-api: REDACTED_SECRET_043 rendered"
else
  fail "infra/helm/loopzero-api: REDACTED_SECRET_043 MISSING"
fi

# Env vars wiring (T5.12 SDK reads these — only in infra chart per env block layout)
for var in LOOPZERO_VAULT_ADDR LOOPZERO_VAULT_K8S_AUTH_PATH LOOPZERO_VAULT_ROLE LOOPZERO_VAULT_SA_JWT_PATH; do
  if echo "${RENDER_INFRA_ON}" | grep -qE "name: ${var}"; then
    pass "infra/helm/loopzero-api: env ${var} rendered"
  else
    fail "infra/helm/loopzero-api: env ${var} MISSING"
  fi
done

# Specific value assertions
if echo "${RENDER_INFRA_ON}" | grep -qE '"k8s-core"'; then
  pass "infra/helm/loopzero-api: LOOPZERO_VAULT_K8S_AUTH_PATH = k8s-core (T5.1 mount)"
else
  fail "infra/helm/loopzero-api: LOOPZERO_VAULT_K8S_AUTH_PATH != k8s-core"
fi

blue "== infra/helm/loopzero-api: vault.enabled=false (default) =="

RENDER_INFRA_OFF=$(render_or_die "infra/helm/loopzero-api vault=off" helm template loopzero-api ./infra/helm/loopzero-api \
  --show-only templates/deployment.yaml )

if echo "${RENDER_INFRA_OFF}" | grep -qE 'name: vault-sa-token'; then
  fail "infra/helm/loopzero-api: vault-sa-token RENDERED when vault.enabled=false (gate broken)"
else
  pass "infra/helm/loopzero-api: vault-sa-token absent when vault.enabled=false"
fi

if echo "${RENDER_INFRA_OFF}" | grep -qE 'name: LOOPZERO_VAULT_ADDR'; then
  fail "infra/helm/loopzero-api: LOOPZERO_VAULT_ADDR RENDERED when vault.enabled=false"
else
  pass "infra/helm/loopzero-api: LOOPZERO_VAULT_ADDR absent when vault.enabled=false"
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
