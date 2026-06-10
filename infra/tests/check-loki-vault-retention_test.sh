#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T8.2 + T8.5 of plan 2026-06-02-openbao-adoption v1.4.
#
# Hermetic test for Loki per-NS retention + DO Spaces archive cron.
# Asserts:
#   (T8.2) infra/helm/loki/values.yaml has retention_stream with
#          selector='{namespace="vault-system"}' period=2160h (90 days)
#          AND default retention_period remains 168h (7 days for other NS)
#   (T8.5) infra/helm/monitoring/manifests/cronjob-loki-vault-audit-archive.yaml
#          renders correctly with: schedule="0 4 1 * *",
#          concurrencyPolicy: Forbid, security hardening, DO Spaces env vars,
#          logcli + mc invocation pattern

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOKI_VALUES="${REPO_ROOT}/infra/helm/loki/values.yaml"
ARCHIVE_CRON="${REPO_ROOT}/infra/helm/monitoring/manifests/cronjob-loki-vault-audit-archive.yaml"

FAIL_COUNT=0
PASS_COUNT=0
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
blue()  { printf "\033[34m%s\033[0m\n" "$*"; }
pass() { PASS_COUNT=$((PASS_COUNT + 1)); green "  PASS: $*"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); red "  FAIL: $*"; }

blue "== T8.2: Loki retention_stream for vault-system =="
if [ ! -f "${LOKI_VALUES}" ]; then
  fail "loki values.yaml MISSING"
  exit 2
fi
if grep -qE 'retention_period: 168h' "${LOKI_VALUES}"; then
  pass "default retention_period: 168h preserved"
else
  fail "default retention_period: 168h MISSING (other NS retention broken)"
fi
if grep -qE 'retention_stream:' "${LOKI_VALUES}"; then
  pass "retention_stream array declared"
else
  fail "retention_stream array MISSING"
fi
if grep -qE 'namespace=\\?"vault-system\\?"' "${LOKI_VALUES}"; then
  pass "selector matches vault-system NS"
else
  fail "selector does NOT match vault-system NS"
fi
if grep -qE 'period: 2160h' "${LOKI_VALUES}"; then
  pass "period: 2160h (90 days) configured"
else
  fail "period: 2160h MISSING (forensic retention broken)"
fi

blue "== T8.5: Archive CronJob present =="
if [ ! -f "${ARCHIVE_CRON}" ]; then
  fail "cronjob-loki-vault-audit-archive.yaml MISSING"
  exit 2
fi
if grep -qE 'kind: CronJob' "${ARCHIVE_CRON}"; then
  pass "CronJob kind declared"
else
  fail "CronJob kind MISSING"
fi
if grep -qE 'schedule: "0 4 1 \* \*"' "${ARCHIVE_CRON}"; then
  pass "schedule = 04:00 UTC day 1 monthly"
else
  fail "schedule != 04:00 UTC day 1 monthly"
fi
if grep -qE 'concurrencyPolicy: Forbid' "${ARCHIVE_CRON}"; then
  pass "concurrencyPolicy: Forbid (no parallel runs)"
else
  fail "concurrencyPolicy NOT Forbid"
fi
if grep -qE 'runAsNonRoot: true' "${ARCHIVE_CRON}"; then
  pass "runAsNonRoot hardening present"
else
  fail "runAsNonRoot hardening MISSING"
fi
if grep -qE 'drop: \["ALL"\]' "${ARCHIVE_CRON}"; then
  pass "capabilities drop: ALL"
else
  fail "capabilities drop: ALL MISSING"
fi
if grep -qE 'logcli query' "${ARCHIVE_CRON}"; then
  pass "logcli query invocation present"
else
  fail "logcli query MISSING"
fi
if grep -qE 'mc cp' "${ARCHIVE_CRON}" && grep -qE 'loopzero-audit-logs-' "${ARCHIVE_CRON}"; then
  pass "mc cp to DO Spaces bucket"
else
  fail "mc cp to DO Spaces MISSING"
fi
if grep -qE 'name: AWS_ACCESS_KEY_ID' "${ARCHIVE_CRON}" && grep -qE 'name: AWS_SECRET_ACCESS_KEY' "${ARCHIVE_CRON}"; then
  pass "S3 creds env vars wired via secretKeyRef"
else
  fail "S3 creds env vars MISSING"
fi
if grep -qE '^\s+name: loki-archive-s3-creds' "${ARCHIVE_CRON}"; then
  pass "S3 creds Secret referenced (loki-archive-s3-creds)"
else
  fail "S3 creds Secret reference MISSING"
fi
if grep -qE 'namespace=vault-system' "${ARCHIVE_CRON}"; then
  pass "logcli query targets vault-system NS"
else
  fail "logcli query target NS != vault-system"
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
