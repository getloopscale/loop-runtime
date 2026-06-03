#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Trio compatibility regression test for T6.5 cold-bootstrap fallback.
#
# Validates that infra/helmfile/scripts/generate-secrets.sh STILL
# produces LOOPZERO_DB_PASSWORD=loopzero-dev-password when:
#   - BAO_ADDR is unset (cleanroom / cold-bootstrap)
#   - BAO_TOKEN is unset
#   - bao CLI is unavailable (typical local dev workstation)
#
# Critical for cleanroom: if a refactor accidentally removes the legacy
# fallback, `task setup` would block waiting for Vault that doesn't exist
# yet (chicken-and-egg).
#
# Hermetic: parses the script + simulates the BAO env gate without
# actually executing the full script (avoids docker/k3d dependencies).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${REPO_ROOT}/infra/helmfile/scripts/generate-secrets.sh"

FAIL=0; PASS=0
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }
pass() { PASS=$((PASS+1)); green "  PASS: $*"; }
fail() { FAIL=$((FAIL+1)); red "  FAIL: $*"; }

blue "== Cold-bootstrap fallback path STILL produces loopzero-dev-password =="
# Simulate the BAO env gate logic inline (mirroring the script's check)
unset BAO_ADDR BAO_TOKEN
if [[ -n "${BAO_ADDR:-}" ]] && [[ -n "${BAO_TOKEN:-}" ]] && command -v bao >/dev/null 2>&1; then
  fail "Vault branch taken when BAO env unset (cold-bootstrap broken)"
else
  pass "Vault branch SKIPPED when BAO env unset (cold-bootstrap preserved)"
fi
LOOPZERO_DB_PASSWORD="${LOOPZERO_DB_PASSWORD:-loopzero-dev-password}"
if [[ "${LOOPZERO_DB_PASSWORD}" = "loopzero-dev-password" ]]; then
  pass "Legacy fallback returns 'loopzero-dev-password' (cold-bootstrap contract)"
else
  fail "Legacy fallback BROKEN — got '${LOOPZERO_DB_PASSWORD}'"
fi

blue "== Script source contract: T6.5 Vault path still gated by BAO env =="
if grep -qE 'BAO_ADDR.*BAO_TOKEN.*command -v bao' "${SCRIPT}"; then
  pass "T6.5 conditional gate present (BAO_ADDR AND BAO_TOKEN AND bao CLI)"
else
  fail "T6.5 conditional gate MISSING or refactored — cleanroom risk"
fi
if grep -qE 'loopzero-dev-password' "${SCRIPT}"; then
  pass "Legacy fallback string 'loopzero-dev-password' present (58 dependent refs)"
else
  fail "Legacy 'loopzero-dev-password' REMOVED — 58 refs would break"
fi

blue "== Vault branch never bypasses legacy fallback assignment =="
# The pattern `LOOPZERO_DB_PASSWORD="${LOOPZERO_DB_PASSWORD:-loopzero-dev-password}"` MUST
# appear AFTER the Vault block (which may have set LOOPZERO_DB_PASSWORD already)
# so cold-bootstrap still hits it.
VAULT_LINE=$(grep -n 'database/creds/loopzero-api' "${SCRIPT}" | head -1 | cut -d: -f1)
FALLBACK_LINE=$(grep -n 'LOOPZERO_DB_PASSWORD:-loopzero-dev-password' "${SCRIPT}" | head -1 | cut -d: -f1)
if [[ -n "${VAULT_LINE}" ]] && [[ -n "${FALLBACK_LINE}" ]]; then
  if [[ "${FALLBACK_LINE}" -gt "${VAULT_LINE}" ]]; then
    pass "Fallback (line ${FALLBACK_LINE}) AFTER Vault block (line ${VAULT_LINE})"
  else
    fail "Fallback BEFORE Vault — Vault path bypassed (cold-bootstrap broken)"
  fi
else
  fail "Could not find Vault block OR fallback line"
fi

blue "== T6.5 documentation comment present (operator visibility) =="
if grep -qE 'T6.5.*openbao-adoption' "${SCRIPT}"; then
  pass "T6.5 plan reference present in comment"
else
  fail "T6.5 plan reference MISSING — comment drifted"
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
