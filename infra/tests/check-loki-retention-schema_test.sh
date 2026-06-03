#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Trio compatibility regression test for T8.2 Loki retention_stream schema.
#
# Validates that the retention_stream YAML block is schema-valid + compatible
# with Loki SingleBinary mode (cleanroom default deployment).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOKI="${REPO_ROOT}/infra/helm/loki/values.yaml"

FAIL=0; PASS=0
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }
pass() { PASS=$((PASS+1)); green "  PASS: $*"; }
fail() { FAIL=$((FAIL+1)); red "  FAIL: $*"; }

blue "== Loki values.yaml is structurally valid YAML =="
if python3 -c "import yaml; yaml.safe_load(open('${LOKI}'))" 2>/dev/null; then
  pass "YAML parses cleanly"
else
  fail "YAML parse error"
fi

blue "== retention_stream array under limits_config =="
RETENTION=$(python3 -c "
import yaml
d = yaml.safe_load(open('${LOKI}'))
lc = d.get('loki', {}).get('limits_config', {})
rs = lc.get('retention_stream', [])
print('count:', len(rs))
for r in rs:
    print(f'  selector: {r.get(\"selector\")}')
    print(f'  period: {r.get(\"period\")}')
    print(f'  priority: {r.get(\"priority\")}')
" 2>&1)
echo "${RETENTION}"
if echo "${RETENTION}" | grep -qE '^count: 1'; then
  pass "Exactly 1 retention_stream entry (vault-system)"
else
  fail "Wrong retention_stream count"
fi
if echo "${RETENTION}" | grep -qE 'selector: \{namespace="vault-system"\}'; then
  pass "Selector matches vault-system NS"
else
  fail "Selector does NOT match vault-system NS"
fi
if echo "${RETENTION}" | grep -qE 'period: 2160h'; then
  pass "period: 2160h (90 days)"
else
  fail "period NOT 2160h"
fi

blue "== Default retention_period: 168h preserved (non-vault NS = 7d) =="
DEFAULT=$(python3 -c "
import yaml
d = yaml.safe_load(open('${LOKI}'))
print(d.get('loki', {}).get('limits_config', {}).get('retention_period', ''))
")
if [[ "${DEFAULT}" = "168h" ]]; then
  pass "Default retention_period: 168h (7 days for non-vault NS)"
else
  fail "Default retention_period drifted: ${DEFAULT}"
fi

blue "== SingleBinary deploymentMode preserved (cleanroom default) =="
DEPLOY=$(python3 -c "
import yaml
d = yaml.safe_load(open('${LOKI}'))
print(d.get('deploymentMode', ''))
")
if [[ "${DEPLOY}" = "SingleBinary" ]]; then
  pass "deploymentMode: SingleBinary (cleanroom-compatible)"
else
  fail "deploymentMode changed to ${DEPLOY} — cleanroom may break"
fi

blue "== retention_stream priority + selector format are Loki canonical =="
# Per Loki docs: priority must be int; selector must be LogQL stream selector
PRIO_VALID=$(python3 -c "
import yaml
d = yaml.safe_load(open('${LOKI}'))
rs = d.get('loki', {}).get('limits_config', {}).get('retention_stream', [])
if rs and isinstance(rs[0].get('priority'), int):
    print('OK')
")
if [[ "${PRIO_VALID}" = "OK" ]]; then
  pass "retention_stream[0].priority is int (Loki schema valid)"
else
  fail "retention_stream priority is NOT int — Loki rejects on startup"
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
