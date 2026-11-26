#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan 2026-05-16 T4.1 — Drill auto-destroy watchdog.
#
# Operator-run periodically (~every 6h while a drill stack is active; the GitHub
# cron was removed 2026-05-23 — run via `task drill:watchdog` or the real CI).
# Scans DO for resources tagged with `loop-runtime-drill-expires-
# at-<ISO>`, parses the expiration, and destroys any stack whose
# expires-at < now via:
#   1. pulumi destroy --stack drill (best effort)
#   2. teardown-validator.sh (doctl fallback per T4.1.bis)
#
# Safety net for the case where operator/Ralph forgets `pulumi destroy`
# OR CI workflow fails before reaching its teardown step. Drill stack
# left up costs ~$0.32/hr. Without watchdog, single forgotten run could
# leak hundreds of dollars before billing dashboard catches it.
#
# Per D6: default expiresInHours=8 covers most drill runs. Stage 2
# drill-prod-identical-soak overrides to 26h (T3.4.bis EC-01). 48h ceiling enforced
# in Pulumi stack code.
#
# Idempotent: if no expired stacks, exits 0 with audit trail.
# Concurrent-safe: teardown-validator.sh uses flock.
#
# Exit codes:
#   0 — nothing expired, or the expired stack was destroyed and validated clean
#   1 — DIGITALOCEAN_TOKEN, doctl or the validator is missing
#   3 — the validator could not clean up (manual cleanup required)
#   4 — a DigitalOcean list failed: the drill cost state is unknown
#
# Environment:
#   DIGITALOCEAN_TOKEN — required (drill-scoped per D7)
#   PULUMI_ACCESS_TOKEN — required for pulumi destroy
#   PULUMI_CONFIG_PASSPHRASE — for stack decryption
#   DRILL_PULUMI_DIR — default infra/pulumi-drill

set -euo pipefail

# T2.2-followup (plan 2026-05-21-deep-review-remediation): adopt the
# shared dry-run lib. The watchdog gates `pulumi destroy`; in dry-run
# mode it logs intent without destroying — useful for operator
# sanity-check during incident response.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/dry-run.sh disable=SC1091
source "$SCRIPT_DIR/../lib/dry-run.sh"
parse_dry_run_flag "$@"
if [ "${#REMAINING_ARGS[@]}" -gt 0 ]; then
  set -- "${REMAINING_ARGS[@]}"
else
  set --
fi

# --- Input validation ---
if [[ -z "${DIGITALOCEAN_TOKEN:-}" ]]; then
  echo "[drill-watchdog] ERROR: DIGITALOCEAN_TOKEN required" >&2
  exit 1
fi
if ! command -v doctl >/dev/null 2>&1; then
  echo "[drill-watchdog] ERROR: doctl not installed" >&2
  exit 1
fi

PULUMI_DIR="${DRILL_PULUMI_DIR:-infra/pulumi-drill}"
VALIDATOR="$(cd "$(dirname "$0")" && pwd)/teardown-validator.sh"

log() { printf '[drill-watchdog] %s\n' "$*" >&2; }

log "Scanning DO for drill resources with expires-at tag..."

# Find any DOKS cluster tagged loop-runtime-drill (drill-mgmt, drill-build,
# drill-runtime, drill-runtime-2 all have loop-runtime-drill base tag).
# Then extract expires-at tag (loop-runtime-drill-expires-at-<ISO>).
# A failing list is not an empty one: reporting "no drill clusters" after a
# failed call would switch off the cost net exactly when it cannot see.
if ! DOKS_LIST=$(doctl kubernetes cluster list --format ID,Name,Tag --no-header 2>&1); then
  log "ERROR: could not list Kubernetes clusters — drill cost state unknown: $DOKS_LIST"
  exit 4
fi
DRILL_DOKS=$(echo "$DOKS_LIST" | grep -E "loop-runtime-drill" || true)

if [[ -z "$DRILL_DOKS" ]]; then
  log "✓ No drill clusters found — watchdog has nothing to do"
  exit 0
fi

NOW_EPOCH=$(date +%s)
EXPIRED_FOUND=0

# Parse tags to find expires-at. The tag format is:
#   loop-runtime-drill-expires-at-YYYY-MM-DDTHH-MM-SS (sanitized ISO, no colons)
# We extract this, convert back to ISO with colons, parse to epoch.
log "Inspecting tags for expires-at..."

# Each cluster's Tag column may contain multiple tags space-separated;
# fetch full tag list per cluster via doctl kubernetes cluster get.
while IFS= read -r line; do
  ID=$(echo "$line" | awk '{print $1}')
  NAME=$(echo "$line" | awk '{print $2}')
  if ! TAGS=$(doctl kubernetes cluster get "$ID" --format Tag --no-header 2>&1); then
    log "ERROR: could not read the tags of $NAME ($ID): $TAGS"
    exit 4
  fi
  EXPIRES_TAG=$(echo "$TAGS" | grep -oE "loop-runtime-drill-expires-at-[0-9-]+" | head -1 || true)
  if [[ -z "$EXPIRES_TAG" ]]; then
    log "  $NAME: no expires-at tag found — skipping (untracked)"
    continue
  fi
  # Extract ISO portion (strip prefix)
  EXPIRES_ISO_SAFE=${EXPIRES_TAG#loop-runtime-drill-expires-at-}
  # Restore colons in time portion: YYYY-MM-DDTHH-MM-SS → YYYY-MM-DDTHH:MM:SS
  EXPIRES_ISO=$(echo "$EXPIRES_ISO_SAFE" | sed -E 's/T([0-9]{2})-([0-9]{2})-([0-9]{2})/T\1:\2:\3/')
  # Parse to epoch
  EXPIRES_EPOCH=$(date -u -d "${EXPIRES_ISO}Z" +%s 2>/dev/null || echo 0)
  if [[ "$EXPIRES_EPOCH" -eq 0 ]]; then
    log "  $NAME: malformed expires-at tag ($EXPIRES_TAG) — skipping"
    continue
  fi
  AGE_SECONDS=$((NOW_EPOCH - EXPIRES_EPOCH))
  if [[ "$AGE_SECONDS" -le 0 ]]; then
    REMAINING=$((-AGE_SECONDS))
    REMAINING_HOURS=$((REMAINING / 3600))
    log "  $NAME: expires in ${REMAINING_HOURS}h — skipping (still active)"
    continue
  fi
  # Expired!
  AGE_HOURS=$((AGE_SECONDS / 3600))
  log "  ✗ $NAME: EXPIRED ${AGE_HOURS}h ago (expires-at=$EXPIRES_ISO, now=$(date -u +%Y-%m-%dT%H:%M:%SZ))"
  EXPIRED_FOUND=1
done <<< "$DRILL_DOKS"

if [[ $EXPIRED_FOUND -eq 0 ]]; then
  log "✓ All drill clusters within expires-at window — watchdog no-op"
  exit 0
fi

# Found expired stack(s). Run pulumi destroy + validator.
log "Found expired drill stack(s). Initiating destroy + validate sequence..."

if [[ -n "${PULUMI_ACCESS_TOKEN:-}" ]] && [[ -d "$PULUMI_DIR" ]]; then
  log "Attempting pulumi destroy --stack drill..."
  if (cd "$PULUMI_DIR" && apply pulumi destroy --stack drill --yes --skip-preview 2>&1); then
    log "✓ pulumi destroy succeeded"
  else
    log "  pulumi destroy failed or partial; falling back to validator"
  fi
else
  log "  PULUMI_ACCESS_TOKEN not set OR pulumi-drill dir absent; skipping pulumi step, using validator only"
fi

log "Invoking teardown-validator.sh (doctl fallback)..."
if [[ -x "$VALIDATOR" ]]; then
  if bash "$VALIDATOR"; then
    log "✓ Watchdog: drill stack destroyed + validated clean"
    exit 0
  else
    log "ERROR: teardown-validator.sh failed — manual cleanup required"
    exit 3
  fi
else
  log "ERROR: teardown-validator.sh not found at $VALIDATOR"
  exit 1
fi
