#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan T3.3 D11 — cleanup-orphan-chaos
#
# Detects chaos-mesh CRs (PodChaos, NetworkChaos, IOChaos, StressChaos) that
# have a deletionTimestamp older than the threshold AND still have finalizers
# blocking the actual deletion. Patches the finalizer out so the resource
# can be garbage-collected.
#
# Background (F-CHAOS-02): chaos-mesh controller sometimes leaves finalizers
# on NetworkChaos after delete completion (specially under daemon load).
# Without this script, "ghost" resources accumulate and cause pre-flight
# false-positives on the next /chaos-test run.
#
# Idempotent. Safe to run frequently (CronJob every 5min). Resources younger
# than the threshold are left alone (the controller may still be processing
# deletion legitimately).
#
# Usage:
#   ./cleanup-orphan-chaos.sh                 # default: 5min threshold
#   THRESHOLD_MIN=10 ./cleanup-orphan-chaos.sh  # override
#   DRY_RUN=1 ./cleanup-orphan-chaos.sh        # report only, no patches

set -euo pipefail

THRESHOLD_MIN="${THRESHOLD_MIN:-5}"
DRY_RUN="${DRY_RUN:-0}"
KUBECTL="${KUBECTL:-kubectl}"

# Chaos-mesh resource types managed by this cleanup.
CHAOS_KINDS=(podchaos networkchaos iochaos stresschaos)

now_unix=$(date -u +%s)

patched=0
inspected=0

for kind in "${CHAOS_KINDS[@]}"; do
  # Fetch all resources of kind across all namespaces.
  # Output: namespace name deletionTimestamp finalizers-count
  while IFS=$'\t' read -r ns name dts fcount; do
    [ -z "$ns" ] && continue
    inspected=$((inspected + 1))

    # Skip resources without deletionTimestamp (not being deleted)
    if [ -z "$dts" ] || [ "$dts" = "<no value>" ] || [ "$dts" = "null" ]; then
      continue
    fi

    # Skip resources without finalizers (deletion will complete naturally)
    if [ -z "$fcount" ] || [ "$fcount" = "0" ] || [ "$fcount" = "<no value>" ]; then
      continue
    fi

    # Parse deletionTimestamp (RFC3339) → unix epoch
    if ! dts_unix=$(date -u -d "$dts" +%s 2>/dev/null); then
      echo "WARN: $kind/$ns/$name: unparseable deletionTimestamp $dts; skipping" >&2
      continue
    fi

    age_min=$(( (now_unix - dts_unix) / 60 ))
    if [ "$age_min" -lt "$THRESHOLD_MIN" ]; then
      # Within threshold; let controller finish naturally
      continue
    fi

    echo "PATCH $kind/$ns/$name (age=${age_min}min, finalizers=${fcount})"
    if [ "$DRY_RUN" != "1" ]; then
      if "$KUBECTL" patch "$kind" "$name" -n "$ns" \
          --type=json -p '[{"op":"remove","path":"/metadata/finalizers"}]' \
          >/dev/null 2>&1; then
        patched=$((patched + 1))
      else
        echo "ERROR: failed to patch $kind/$ns/$name" >&2
      fi
    fi
  done < <(
    "$KUBECTL" get "$kind" --all-namespaces \
      -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.metadata.deletionTimestamp}{"\t"}{.metadata.finalizers}{"\n"}{end}' \
      2>/dev/null | awk -F'\t' '{
        # Count finalizers via simple bracket-comma pattern
        fcount = 0
        if ($4 != "" && $4 != "<no value>") {
          n = split($4, arr, ",")
          fcount = n
        }
        print $1 "\t" $2 "\t" $3 "\t" fcount
      }'
  )
done

echo "cleanup-orphan-chaos summary: inspected=$inspected patched=$patched dry_run=$DRY_RUN threshold_min=$THRESHOLD_MIN"
