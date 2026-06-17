#!/usr/bin/env bash
# baseline-diff.sh
#
# T1.1 of plan 2026-06-08-pre-release-test-rigor v1.2 — compares two snapshots
# produced by `baseline-capture.sh` and classifies the deltas with severity:
#
#   - additive   — key/item present in B but not in A. Treated as OK
#                  (post-rebuild rebuilds may add ephemeral state; not a regression).
#   - missing    — key/item present in A but not in B. Treated as FAIL
#                  (post-rebuild lost something we had pre-rebuild).
#   - mismatch   — same key, different value. Treated as FAIL
#                  (genuine drift).
#   - identical  — no change. Treated as OK (silent).
#
# Volatile field normalization (EC-7) is also applied here as a defensive
# second pass, in case A or B was captured by an older script version that
# didn't normalize.
#
# Output: JSON delta report to stdout + human markdown to stderr (when
# requested via --markdown). Exit code is the verdict:
#   0  identical OR only additive → verdict OK
#   1  at least one mismatch OR missing → verdict FAIL
#   2  invocation error (file not found, malformed JSON)
#
# Usage:
#   bash infra/scripts/local/baseline-diff.sh snapshot-a.json snapshot-b.json
#   bash infra/scripts/local/baseline-diff.sh snapshot-a.json snapshot-b.json --markdown
set -euo pipefail

[ $# -ge 2 ] || {
  echo "[baseline-diff] usage: $0 SNAPSHOT_A SNAPSHOT_B [--markdown]" >&2
  exit 2
}

SNAP_A="$1"
SNAP_B="$2"
MARKDOWN=0
shift 2
while [ $# -gt 0 ]; do
  case "$1" in
    --markdown) MARKDOWN=1 ; shift ;;
    *) echo "[baseline-diff] unknown arg: $1" >&2 ; exit 2 ;;
  esac
done

[ -f "$SNAP_A" ] || { echo "[baseline-diff] not found: $SNAP_A" >&2 ; exit 2 ; }
[ -f "$SNAP_B" ] || { echo "[baseline-diff] not found: $SNAP_B" >&2 ; exit 2 ; }

python3 "$(dirname "$0")/baseline_diff.py" "$SNAP_A" "$SNAP_B" "$MARKDOWN"
