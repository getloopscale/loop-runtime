#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# The nightly rebuild of staging, run by loopzero-staging-rebuild.timer: everything is
# torn down and built again from the head of develop (`task staging:up`), so staging
# never drifts from what the code describes.
#
# The engine images keep the pins the last deploy ran (infra/staging/deploy.sh). After
# the rebuild the deploy timer is made to handle the head of develop again, which
# deploys it and reports its statuses on the new environment. A failed rebuild raises
# the "rebuild" alarm (infra/staging/alarm.sh); a good one clears it.
#
# A good rebuild is followed by the restore drill of the platform's PostgreSQL
# (infra/staging/restore-drill.sh of the rebuilt checkout), which reports its own status
# and alarm. A failed drill fails this run, after everything above has been done.
set -euo pipefail

REMOTE="${DEPLOY_REMOTE:-https://github.com/getloopscale/loopzero.git}"
BRANCH="${DEPLOY_BRANCH:-develop}"
STATE_DIR="${DEPLOY_STATE_DIR:-/var/lib/loopzero-staging/deploy}"
UP_CMD="${REBUILD_UP_CMD:-task staging:up}"
ALARM="${REBUILD_ALARM_CMD:-/usr/local/lib/loopzero-staging/alarm.sh}"
# The restore drill, given the rebuilt commit. Empty: the checkout's restore-drill.sh.
RESTORE_DRILL_CMD="${REBUILD_RESTORE_DRILL_CMD:-}"

CHECKOUT="$STATE_DIR/checkout"
LOGS="$STATE_DIR/logs"
mkdir -p "$LOGS"
log() { printf '%s staging-rebuild: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }

if [[ ! -d "$CHECKOUT/.git" ]]; then
  git clone --quiet "$REMOTE" "$CHECKOUT"
fi
git -C "$CHECKOUT" fetch --quiet origin "$BRANCH"
git -C "$CHECKOUT" switch --quiet --detach FETCH_HEAD
sha=$(git -C "$CHECKOUT" rev-parse HEAD)

values=""
if [[ -f "$STATE_DIR/state-values.json" ]]; then
  values="$STATE_DIR/state-values.json"
fi
rebuild_log="$LOGS/rebuild-$(date -u +%Y%m%dT%H%M%SZ).log"
log "rebuilding staging at ${sha:0:7} (log: $rebuild_log)"
if (cd "$CHECKOUT" && STAGING_STATE_VALUES="$values" $UP_CMD) >"$rebuild_log" 2>&1; then
  rm -f "$STATE_DIR/handled"
  $ALARM clear rebuild "the nightly rebuild at ${sha:0:7} succeeded."
  log "rebuilt at ${sha:0:7}"
else
  code=$?
  $ALARM raise rebuild "The nightly rebuild at ${sha:0:7} failed: \`$UP_CMD\` exited $code." "$rebuild_log"
  log "error: the rebuild at ${sha:0:7} exited $code; see $rebuild_log"
  exit "$code"
fi

if [[ -z "$RESTORE_DRILL_CMD" ]]; then
  if [[ ! -f "$CHECKOUT/infra/staging/restore-drill.sh" ]]; then
    log "${sha:0:7} has no infra/staging/restore-drill.sh; no restore drill"
    exit 0
  fi
  RESTORE_DRILL_CMD="bash $CHECKOUT/infra/staging/restore-drill.sh"
fi
$RESTORE_DRILL_CMD "$sha"
