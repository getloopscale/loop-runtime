#!/usr/bin/env bash
# B-033 T1.1 hermetic gate — dev-public MUST publish LOOPZERO_PROMETHEUS_URL for cost aggregator.
#
# Root cause (BACKLOG.md § B-033):
#   LOOPZERO_PROMETHEUS_URL vazio no Deployment → cost aggregator boot-desliga com
#   `logger.Info("cost aggregator disabled")` → cost_counters vazia → CostEnforcer
#   middleware degrada para pass-through. As três métricas de custo ficam sem teto.
#
# Contract asserted by this gate (hermetic — no live cluster):
#   A1  env LOOPZERO_PROMETHEUS_URL is set on the loopzero-cloud container
#   A2  value equals http://prometheus-server.monitoring.svc.cluster.local:80 (D1 canonical)
#   A3  PARITY — value is byte-identical to loopzero-app-operator overlay's .prometheusUrl.
#       Absorves EC-1 do edge-case-plan: editar um dos dois arquivos sem o outro
#       passa em silêncio; A3 pega o drift nomeando ambos os valores.
#
# Sibling pattern (same class, same toolchain):
#   infra/tests/check-loopzero-cloud-rag-dashboard-wiring_test.sh
#   infra/tests/check-loopzero-cloud-lens-dashboard-wiring_test.sh

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "=== Rendering helmfile dev-public loopzero-cloud... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/core.helmfile.yaml.gotmpl \
  --selector name=loopzero-cloud \
  template > "$TMP/loopzero-cloud.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored (loopzero-cloud)"; cat "$TMP/err.log"; exit 1
  }

echo "=== Rendering helmfile dev-public loopzero-app-operator... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/core.helmfile.yaml.gotmpl \
  --selector name=loopzero-app-operator \
  template > "$TMP/loopzero-app-operator.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored (loopzero-app-operator)"; cat "$TMP/err.log"; exit 1
  }

fail=0
pass() { echo "PASS — $1"; }
fail_() { echo "FAIL — $1"; fail=$((fail + 1)); }

# --- A1/A2: LOOPZERO_PROMETHEUS_URL present on loopzero-cloud container with canonical value ---
URL=$(yq eval-all '(select(.kind == "Deployment" and .metadata.name == "loopzero-cloud")
  | .spec.template.spec.containers[]
  | select(.name == "loopzero-cloud")
  | .env[]
  | select(.name == "LOOPZERO_PROMETHEUS_URL")
  | .value) // ""' "$TMP/loopzero-cloud.yaml")

case "$URL" in
  "") fail_ "LOOPZERO_PROMETHEUS_URL env absent from Deployment/loopzero-cloud — cost aggregator will boot-disable, cost_counters stays empty, and CostEnforcer degrades to pass-through (see BACKLOG.md § B-033)" ;;
  http://prometheus-server.monitoring.svc.cluster.local:80) pass "A1/A2 LOOPZERO_PROMETHEUS_URL=$URL (canonical value)" ;;
  *) fail_ "A1/A2 LOOPZERO_PROMETHEUS_URL='$URL' — expected 'http://prometheus-server.monitoring.svc.cluster.local:80' (D1: reuse the URL loopzero-app-operator.yaml.gotmpl:28 already consumes; two coordinates → silent drift)" ;;
esac

# --- A3 (parity — absorbs EC-1): loopzero-cloud env URL == loopzero-app-operator prometheusUrl ---
# Sem esta asserção, editar UM dos dois arquivos sem o outro passa em silêncio:
# ambos os overlays apontam para lugares diferentes e o cost aggregator quebra no
# runtime — a mesma classe do defeito original de B-033.
#
# Como o chart de loopzero-app-operator transforma seu campo `prometheusUrl:` em env var
# com nome distinto (LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL) no manifest renderizado,
# a comparação mais confiavel e' extrair o valor literal DIRETO do overlay dev-public
# (o arquivo canonico citado pelo plano D1: loopzero-app-operator.yaml.gotmpl linha 28).
OP_OVERLAY="infra/helmfile/values/dev-public/loopzero-app-operator.yaml.gotmpl"
if [ ! -f "$OP_OVERLAY" ]; then
  fail_ "A3 parity: $OP_OVERLAY não existe — não posso comparar com loopzero-cloud (D1 do plano cita esse arquivo como origem canônica)"
else
  OP_URL=$(grep -E '^\s*prometheusUrl:\s*"' "$OP_OVERLAY" | head -1 | sed -E 's/.*prometheusUrl:\s*"([^"]+)".*/\1/')
  if [ -z "$OP_URL" ]; then
    fail_ "A3 parity: prometheusUrl não encontrado em $OP_OVERLAY — asserção de lockstep D1 sem base"
  elif [ "$URL" != "$OP_URL" ]; then
    fail_ "A3 parity DRIFT: loopzero-cloud.LOOPZERO_PROMETHEUS_URL='$URL' != loopzero-app-operator.prometheusUrl='$OP_URL'. D1 do plano decide reusar a MESMA URL nos dois; editar um sem o outro é o modo de falha que este check existe para pegar"
  else
    pass "A3 parity: loopzero-cloud.LOOPZERO_PROMETHEUS_URL == loopzero-app-operator.prometheusUrl (== $URL)"
  fi
fi

if [ "$fail" -eq 0 ]; then
  echo "OK — cost aggregator wiring intact"; exit 0
else
  echo "FAIL — $fail assertion(s) failed"; exit 1
fi
