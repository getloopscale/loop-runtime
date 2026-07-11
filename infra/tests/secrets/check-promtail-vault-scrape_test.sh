#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T8.1 + T8.3 of plan 2026-06-02-openbao-adoption v1.4.
#
# Hermetic test for Promtail vault-audit scrape job + Grafana dashboard.
# Asserts:
#   (T8.1) promtail values.yaml.gotmpl has job_name: vault-audit + namespaces: [vault-system]
#   (T8.1) relabel adds namespace + container + loopzero_component=vault-audit labels
#   (T8.3) Grafana dashboard JSON parses + has expected panels

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROMTAIL="${REPO_ROOT}/infra/helm/promtail/values.yaml.gotmpl"
DASHBOARD="${REPO_ROOT}/infra/helm/monitoring/dashboards/openbao-audit.json"

FAIL_COUNT=0
PASS_COUNT=0
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }
pass() { PASS_COUNT=$((PASS_COUNT + 1)); green "  PASS: $*"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); red "  FAIL: $*"; }

blue "== T8.1: Promtail vault-audit scrape job =="
if grep -qE 'job_name: vault-audit' "${PROMTAIL}"; then
  pass "job_name: vault-audit declared"
else
  fail "job_name: vault-audit MISSING"
fi
# Check namespaces.names array contains vault-system
if awk '/job_name: vault-audit/,/^resources:/' "${PROMTAIL}" | grep -qE '\- vault-system'; then
  pass "namespaces filter = [vault-system]"
else
  fail "namespaces filter does NOT include vault-system"
fi
if awk '/job_name: vault-audit/,/^resources:/' "${PROMTAIL}" | grep -qE 'target_label: loopzero_component'; then
  pass "loopzero_component label set"
else
  fail "loopzero_component label NOT set"
fi
if awk '/job_name: vault-audit/,/^resources:/' "${PROMTAIL}" | grep -qE 'replacement: vault-audit'; then
  pass "loopzero_component replacement = vault-audit (Grafana filter target)"
else
  fail "loopzero_component replacement != vault-audit"
fi

blue "== T8.3: Grafana dashboard parses =="
if [ ! -f "${DASHBOARD}" ]; then
  fail "openbao-audit.json MISSING"
  exit 2
fi
if python3 -c "import json; json.load(open('${DASHBOARD}'))" 2>/dev/null; then
  pass "JSON parses cleanly"
else
  fail "JSON parse error"
fi
if python3 -c "import json; d=json.load(open('${DASHBOARD}')); assert d['uid']=='openbao-audit'" 2>/dev/null; then
  pass "uid = openbao-audit"
else
  fail "uid != openbao-audit"
fi

blue "== T8.3: Expected panels present =="
declare -a EXPECTED_TITLES=(
  "Auth Request Rate (5m)"
  "Policy Denials by Path (P1 alert source)"
  "Vault Seal Status (P1 alert source)"
  "Audit Log Entry Timestamp Lag (P1 alert source)"
  "Top Accessors (last 1h)"
  "Vault Audit Log (Loki"
  "Denial Heatmap (15m bins)"
)
for title in "${EXPECTED_TITLES[@]}"; do
  if grep -qF "\"${title}" "${DASHBOARD}"; then
    pass "panel '${title}' present"
  else
    fail "panel '${title}' MISSING"
  fi
done

blue "== T8.3: Datasource references =="
if grep -qE '"type": "prometheus"' "${DASHBOARD}"; then
  pass "Prometheus datasource ref present"
else
  fail "Prometheus datasource ref MISSING"
fi
if grep -qE '"type": "loki"' "${DASHBOARD}"; then
  pass "Loki datasource ref present"
else
  fail "Loki datasource ref MISSING"
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
