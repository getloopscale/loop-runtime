#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# Plan 2026-05-16 T4.3 — Drill cost report helper.
#
# Computes drill run cost from provision/teardown timestamps + current DO
# pricing per Pulumi.drill.yaml sizing. Emits markdown report section
# consumable by the `/drill-test` skill output template.
#
# Usage:
#   bash infra/scripts/drill-cost-report.sh \
#     --start 2026-05-16T12:00:00Z \
#     --end   2026-05-16T13:30:00Z \
#     [--output /tmp/drill-cost-report.md]
#
# Or via Pulumi state (auto-detect):
#   bash infra/scripts/drill-cost-report.sh --auto
#
# Output: markdown to stdout (or --output file), exit 0.
#
# Pricing source (DO public, 2026-05-16):
#   mgmt cluster   (s-4vcpu-8gb-amd × 3 nodes): $0.0714/hr/node × 3 = $0.214/hr
#   build cluster  (s-2vcpu-4gb × 2 nodes):     $0.0357/hr/node × 2 = $0.071/hr
#   runtime cluster (s-2vcpu-4gb × 2 nodes):    $0.0357/hr/node × 2 = $0.071/hr
#   runtime-2 cluster (s-2vcpu-4gb × 2 nodes):  $0.0357/hr/node × 2 = $0.071/hr
#   managed PG (db-s-2vcpu-4gb):                $0.125/hr
#   Spaces (3 buckets, $5/mo baseline):         $0.007/hr negligible
#   ─────────────────────────────────────────────────────────────────
#   Total stack active:                         $0.552/hr (revised ceiling)
#
# Note: revised from $0.32/hr earlier estimate after including all 4
# clusters + PG ephemeral hourly. $0.552/hr is upper bound; actual may
# be lower due to DO billing rounding + autoscaling (drill runs at min
# nodes, doesn't scale up).

set -euo pipefail

# --- Constants (DO pricing 2026-05-16) ---
RATE_MGMT_HR=0.214       # 3 × s-4vcpu-8gb-amd
RATE_BUILD_HR=0.071      # 2 × s-2vcpu-4gb
RATE_RUNTIME_HR=0.071    # 2 × s-2vcpu-4gb
RATE_RUNTIME2_HR=0.071   # 2 × s-2vcpu-4gb
RATE_PG_HR=0.125         # db-s-2vcpu-4gb
RATE_TOTAL_HR=0.552

START_TS=""
END_TS=""
OUTPUT=""
AUTO=0

log() { printf '[drill-cost-report] %s\n' "$*" >&2; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --start) START_TS="$2"; shift 2 ;;
    --end)   END_TS="$2"; shift 2 ;;
    --output) OUTPUT="$2"; shift 2 ;;
    --auto)  AUTO=1; shift ;;
    --help|-h)
      echo "Usage: $0 --start <ISO> --end <ISO> [--output file] | --auto"
      exit 0
      ;;
    *) log "Unknown arg: $1"; exit 1 ;;
  esac
done

if [[ "$AUTO" -eq 1 ]]; then
  # Read Pulumi stack output for expires-at + creation timestamps.
  # Fallback: use mtime of Pulumi.drill.yaml as proxy for start.
  if command -v npx >/dev/null 2>&1 && [[ -d infra/pulumi-drill ]]; then
    PULUMI_OUT=$(cd infra/pulumi-drill && npx pulumi stack output --json --stack drill 2>/dev/null || echo '{}')
    # v2 flat named exports per L2-unlock T1.2 D1: `meta` is exported directly at top level
    # (not under .outputs.meta). Object structure within `meta` is unchanged for back-compat
    # with watchdog cron + this cost report.
    EXPIRES_AT=$(echo "$PULUMI_OUT" | python3 -c 'import sys,json
d=json.load(sys.stdin)
m=d.get("meta",{}) if isinstance(d,dict) else {}
print(m.get("expiresAtIso",""))' 2>/dev/null || true)
    EXPIRES_IN=$(echo "$PULUMI_OUT" | python3 -c 'import sys,json
d=json.load(sys.stdin)
m=d.get("meta",{}) if isinstance(d,dict) else {}
print(m.get("expiresInHours",0))' 2>/dev/null || echo 0)
    if [[ -n "$EXPIRES_AT" && "$EXPIRES_IN" -gt 0 ]]; then
      END_TS="$EXPIRES_AT"
      # start = end - expiresInHours
      START_EPOCH=$(($(date -u -d "$EXPIRES_AT" +%s) - EXPIRES_IN * 3600))
      START_TS=$(date -u -d "@$START_EPOCH" +%Y-%m-%dT%H:%M:%SZ)
      log "Auto-detected drill window: $START_TS → $END_TS ($EXPIRES_IN h)"
    else
      log "ERROR: Pulumi outputs missing expiresAtIso — cannot auto-detect"
      exit 1
    fi
  else
    log "ERROR: --auto requires npx + infra/pulumi-drill dir"
    exit 1
  fi
fi

if [[ -z "$START_TS" || -z "$END_TS" ]]; then
  log "ERROR: --start AND --end required (or --auto)"
  exit 1
fi

# --- Compute + render: delegated to the Go pure-logic core (Track C TC2.1) ---
# The duration/cost arithmetic + markdown moved to pkg/quality/drillcost
# (`loopruntime-ops drill cost`), parity-proven byte-identical vs this script's prior
# heredoc. This bash now keeps ONLY the --auto Pulumi-state gathering above
# (orchestration, ADR D1). Validation (invalid/missing/end<=start) is enforced
# by the Go subcommand. See docs/operations.md.
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
OPS_ARGS=(drill cost --start "$START_TS" --end "$END_TS")
[[ -n "$OUTPUT" ]] && OPS_ARGS+=(--output "$OUTPUT")
if command -v loopruntime-ops >/dev/null 2>&1; then
  loopruntime-ops "${OPS_ARGS[@]}"
else
  ( cd "$REPO_ROOT/cmd/loopruntime-ops" && go run . "${OPS_ARGS[@]}" )
fi
