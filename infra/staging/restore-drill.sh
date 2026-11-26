#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Proves that staging's platform PostgreSQL recovers from its backups, and says how it
# went: the staging/restore-drill status of the commit staging runs, and the
# "restore-drill" alarm (infra/staging/alarm.sh), raised on a failure and cleared on a
# success.
#
#   infra/staging/restore-drill.sh <sha>
#
# The drill is `loop-runtime-ops drill postgres-restore`, built from this checkout: it writes
# a marker to the running cluster, recovers a temporary cluster from the bucket, compares
# and deletes it. The nightly rebuild runs this on the environment it has just built
# (infra/staging/rebuild.sh); it can be run by hand at any time.
set -euo pipefail

REPO="${DEPLOY_REPO:-getloopscale/loop-runtime}"
GITHUB_API="${DEPLOY_GITHUB_API:-https://api.github.com}"
STATE_DIR="${DEPLOY_STATE_DIR:-/var/lib/loop-runtime-staging/deploy}"
TOKEN_FILE="${DEPLOY_STATUS_TOKEN_FILE:-/etc/loop-runtime-staging/github-status-token}"
STATUS_URL_BASE="${DEPLOY_STATUS_URL_BASE:-https://staging.getloopscale.com}"
KUBECONFIG_FILE="${VALIDATE_KUBECONFIG:-/var/lib/loop-runtime-staging/kubeconfig}"
EMULATOR_DIR="${VALIDATE_EMULATOR_DIR:-/var/lib/aws-emulator/.aws/emulator/9597}"
ALARM="${RESTORE_DRILL_ALARM_CMD:-/usr/local/lib/loop-runtime-staging/alarm.sh}"
# The drill itself. Empty: loop-runtime-ops built from this checkout, against the core cluster.
DRILL_CMD="${RESTORE_DRILL_CMD:-}"

log() { printf '%s staging-restore-drill: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
fail() { log "error: $*"; exit 1; }

sha="${1:-}"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || fail "usage: restore-drill.sh <sha>"
short=${sha:0:7}
[[ -r "$TOKEN_FILE" ]] || fail "no status token at $TOKEN_FILE: create a fine-grained token with Commit statuses read and write on $REPO"
TOKEN="$(<"$TOKEN_FILE")"

# status <state> <description>: the staging/restore-drill commit status of the commit.
status() {
  local body
  body=$(jq -n --arg state "$1" --arg description "${2:0:140}" --arg context "staging/restore-drill" \
    --arg target "$STATUS_URL_BASE" '{state: $state, description: $description, context: $context, target_url: $target}')
  curl -fsS --max-time 30 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" -X POST "$GITHUB_API/repos/$REPO/statuses/$sha" -d "$body" >/dev/null \
    || fail "could not post staging/restore-drill $1 on $short"
}

logs="$STATE_DIR/logs"
mkdir -p "$logs"
drill_log="$logs/restore-drill-$(date -u +%Y%m%dT%H%M%SZ).log"

if [[ -z "$DRILL_CMD" ]]; then
  root="$(cd "$(dirname "$0")/../.." && pwd)"
  bin="$STATE_DIR/restore-drill/loop-runtime-ops"
  mkdir -p "$(dirname "$bin")"
  (cd "$root/cmd/loop-runtime-ops" && GOWORK=off go build -o "$bin" .) || fail "could not build loop-runtime-ops from $root"
  export KUBECONFIG="$KUBECONFIG_FILE" AWS_CONFIG_FILE="$EMULATOR_DIR/config" \
    AWS_SHARED_CREDENTIALS_FILE="$EMULATOR_DIR/emulator_credentials" AWS_CA_BUNDLE="$EMULATOR_DIR/ca.pem" \
    AWS_ENDPOINT_URL=https://localhost:9597 AWS_REGION=us-east-1
  DRILL_CMD="$bin drill postgres-restore --context core"
fi

status pending "recovering a temporary cluster from the backups"
log "running the restore drill at $short (log: $drill_log)"
if $DRILL_CMD >"$drill_log" 2>&1; then
  status success "the platform PostgreSQL recovered from its backups"
  $ALARM clear restore-drill "the restore drill at $short succeeded."
  log "the restore drill at $short succeeded"
else
  code=$?
  status failure "the platform PostgreSQL did not recover from its backups; see the restore-drill alarm"
  $ALARM raise restore-drill "The restore drill at $short failed (exit $code): the platform PostgreSQL did not recover from its backups. Runbook: docs/runbooks.md, \"Platform PostgreSQL recovery\"." "$drill_log"
  log "error: the restore drill at $short exited $code; see $drill_log"
  exit "$code"
fi
