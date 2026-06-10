#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T8.4 of plan 2026-06-02-openbao-adoption v1.3.
#
# Hermetic test for PrometheusRule vault-audit-alerts. Asserts:
#   (1) All 4 P1 alerts present (VaultAuthFailureRateHigh, VaultPolicyDenialRateHigh,
#       VaultSealedOrDown, VaultAuditLogStalled)
#   (2) All severity=critical
#   (3) All have runbook_url annotation
#   (4) YAML is valid (parses cleanly)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RULE_FILE="${REPO_ROOT}/infra/helm/monitoring/manifests/prometheusrule-vault-audit.yaml"

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

if [ ! -f "${RULE_FILE}" ]; then
  red "FATAL: PrometheusRule file missing: ${RULE_FILE}"
  exit 2
fi

blue "== YAML valid =="
if command -v yq >/dev/null 2>&1; then
  if yq eval '.spec.groups[0].rules | length' "${RULE_FILE}" >/dev/null 2>&1; then
    pass "yq parses YAML cleanly"
  else
    fail "yq parse error"
  fi
else
  echo "  WARN: yq not installed — skipping parse check"
fi

blue "== All 4 P1 alerts present =="
for alert in VaultAuthFailureRateHigh VaultPolicyDenialRateHigh VaultSealedOrDown VaultAuditLogStalled; do
  if grep -qE "alert:\s+${alert}\s*$" "${RULE_FILE}"; then
    pass "alert ${alert} declared"
  else
    fail "alert ${alert} MISSING"
  fi
done

blue "== All alerts severity=critical =="
SEVERITY_LINES=$(grep -E "^\s+severity:" "${RULE_FILE}" | sort -u)
NON_CRITICAL=$(echo "${SEVERITY_LINES}" | grep -vE "severity:\s+critical" || true)
if [ -z "${NON_CRITICAL}" ]; then
  pass "all 4 alerts severity=critical"
else
  fail "non-critical severities found: ${NON_CRITICAL}"
fi

blue "== All alerts have runbook_url annotation =="
RUNBOOK_COUNT=$(grep -cE "runbook_url:" "${RULE_FILE}" || echo 0)
if [ "${RUNBOOK_COUNT}" -ge 4 ]; then
  pass "all 4 alerts have runbook_url (count=${RUNBOOK_COUNT})"
else
  fail "expected >=4 runbook_url annotations, got ${RUNBOOK_COUNT}"
fi

blue "== team label = platform-security (Vault is security domain) =="
TEAM_COUNT=$(grep -cE "team:\s+platform-security" "${RULE_FILE}" || echo 0)
if [ "${TEAM_COUNT}" -ge 4 ]; then
  pass "all 4 alerts routed to platform-security (count=${TEAM_COUNT})"
else
  fail "expected >=4 team=platform-security, got ${TEAM_COUNT}"
fi

blue "== domain label = vault (consistent routing) =="
DOMAIN_COUNT=$(grep -cE "domain:\s+vault" "${RULE_FILE}" || echo 0)
if [ "${DOMAIN_COUNT}" -ge 4 ]; then
  pass "all 4 alerts have domain=vault (count=${DOMAIN_COUNT})"
else
  fail "expected >=4 domain=vault, got ${DOMAIN_COUNT}"
fi

blue "== Stalled-audit invariant label present (P1 forcing function) =="
if grep -qE 'invariant:\s+"audit-log-must-flow"' "${RULE_FILE}"; then
  pass "VaultAuditLogStalled carries invariant label (high-confidence adversary signal)"
else
  fail "VaultAuditLogStalled MISSING invariant label"
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
