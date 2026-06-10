#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T10.1-T10.4 of plan 2026-06-02-openbao-adoption v1.3.
#
# Hermetic test for Day 30 verify + Day 60 AES delete CronJobs. Asserts:
#   (T10.1) Day 30 CronJob exists when day30VerifyCron.enabled=true
#   (T10.1) Day 30 default schedule = "0 3 1 * *"
#   (T10.1) Day 30 EC-12 filter flag present
#   (T10.3) Day 60 CronJob exists when day60AesDeleteCron.enabled=true
#   (T10.3) Day 60 defaults dryRun=true + shredAesKey=false (safety)
#   (T10.4) Day 60 command includes --shred-aes-key flag
# Disabled-by-default: both crons absent when vault.* enabled=false (default).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

# Ensure helm deps
if [ ! -d "infra/helm/loopzero-api/charts" ] || [ -z "$(ls -A infra/helm/loopzero-api/charts 2>/dev/null)" ]; then
  ( cd infra/helm/loopzero-api && helm dependency build >/dev/null 2>&1 ) || true
fi

RENDER_OFF=$(helm template loopzero-api ./infra/helm/loopzero-api 2>&1 || true)
RENDER_DAY30=$(helm template loopzero-api ./infra/helm/loopzero-api \
  --set vault.day30VerifyCron.enabled=true 2>&1 || true)
RENDER_DAY60=$(helm template loopzero-api ./infra/helm/loopzero-api \
  --set vault.day60AesDeleteCron.enabled=true 2>&1 || true)

blue "== Day 30 + Day 60 disabled by default =="
if echo "${RENDER_OFF}" | grep -qE 'kind: CronJob.*loopzero-secrets-day30'; then
  fail "Day 30 cron rendered when enabled=false (gate broken)"
elif echo "${RENDER_OFF}" | grep -qE 'name: loopzero-secrets-day30-verify'; then
  fail "Day 30 cron metadata.name rendered when enabled=false"
else
  pass "Day 30 cron absent when enabled=false"
fi
if echo "${RENDER_OFF}" | grep -qE 'name: loopzero-secrets-day60-aes-delete'; then
  fail "Day 60 cron rendered when enabled=false (gate broken)"
else
  pass "Day 60 cron absent when enabled=false"
fi

blue "== Day 30 cron present when enabled =="
if echo "${RENDER_DAY30}" | grep -qE 'name: loopzero-secrets-day30-verify'; then
  pass "Day 30 cron rendered"
else
  fail "Day 30 cron NOT rendered when enabled=true"
fi
if echo "${RENDER_DAY30}" | grep -qE 'schedule: "0 3 1 \* \* \*"'; then
  fail "Day 30 schedule has 5 fields not 5 — corrupted"
elif echo "${RENDER_DAY30}" | grep -qE 'schedule: "0 3 1 \* \*"'; then
  pass "Day 30 default schedule = '0 3 1 * *' (03:00 UTC day 1 monthly)"
else
  fail "Day 30 default schedule not as expected"
fi
if echo "${RENDER_DAY30}" | grep -qE '\--filter-deleted-tenants'; then
  pass "Day 30 EC-12 --filter-deleted-tenants flag present"
else
  fail "Day 30 EC-12 filter MISSING (crypto-shredded tenants will cause noise)"
fi
if echo "${RENDER_DAY30}" | grep -qE 'concurrencyPolicy: Forbid'; then
  pass "Day 30 concurrencyPolicy: Forbid (no parallel runs)"
else
  fail "Day 30 concurrencyPolicy not Forbid"
fi

blue "== Day 60 cron present when enabled + safety defaults =="
if echo "${RENDER_DAY60}" | grep -qE 'name: loopzero-secrets-day60-aes-delete'; then
  pass "Day 60 cron rendered"
else
  fail "Day 60 cron NOT rendered when enabled=true"
fi
if echo "${RENDER_DAY60}" | grep -qE '\--dry-run=true'; then
  pass "Day 60 default --dry-run=true (safety — no destructive ops)"
else
  fail "Day 60 default NOT --dry-run=true"
fi
if echo "${RENDER_DAY60}" | grep -qE '\--shred-aes-key=false'; then
  pass "Day 60 default --shred-aes-key=false (safety — irreversible op gated)"
else
  fail "Day 60 default NOT --shred-aes-key=false"
fi
if echo "${RENDER_DAY60}" | grep -qE 'migrate-to-transit'; then
  pass "Day 60 command invokes migrate-to-transit subcmd"
else
  fail "Day 60 command DOES NOT invoke migrate-to-transit"
fi

blue "== Security context hardening present (both crons) =="
for cron_render in "${RENDER_DAY30}" "${RENDER_DAY60}"; do
  if echo "${cron_render}" | grep -qE 'runAsNonRoot: true'; then
    pass "runAsNonRoot: true present"
  else
    fail "runAsNonRoot: true MISSING"
  fi
  if echo "${cron_render}" | grep -qE 'readOnlyRootFilesystem: true'; then
    pass "readOnlyRootFilesystem: true present"
  else
    fail "readOnlyRootFilesystem: true MISSING"
  fi
done

echo
blue "================================================================"
if [ "${FAIL_COUNT}" -eq 0 ]; then
  green "ALL PASS (${PASS_COUNT}/${PASS_COUNT})"
  exit 0
else
  red "FAILURES: ${FAIL_COUNT}/$((PASS_COUNT + FAIL_COUNT))"
  exit 1
fi
