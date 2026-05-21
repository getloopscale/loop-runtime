#!/usr/bin/env bash
# cell-2-activation-plan T1.5 — Master gate orchestrator for ADR
# MEET-INFRA-NORMALIZE-T3-multi-cell conditions C1-C12.
#
# Single source of truth for "is cell-2 ready to activate?"
# Exit 0 only if every requested gate PASSES. Designed so a CI job
# (or operator pre-flip checklist) can run one command.
#
# Usage:
#   bash cell2-activation-gates.sh                     # all 12 gates
#   bash cell2-activation-gates.sh --gates c1,c2,c11   # subset
#   bash cell2-activation-gates.sh --json              # CI-friendly
#   bash cell2-activation-gates.sh --halt              # exit on first FAIL
#   bash cell2-activation-gates.sh --post-flip         # only post-flip gate
#   bash cell2-activation-gates.sh --invasive          # include invasive gates (none yet)
#
# Exit codes:
#   0 — all requested gates PASS
#   1 — at least one gate FAILED
#
# Each gate function lives in lib/cell2-gate-c<N>.sh and is sourced
# below. Gate functions echo a single PASS/FAIL/SKIP line and return
# exit code 0 (PASS or SKIP) or 1 (FAIL). SKIP semantics: gate cannot
# be evaluated in current environment (e.g., dev where C8 is N/A).
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LIB_DIR="$SCRIPT_DIR/lib"

# Default gate list (the canonical 12 ADR conditions).
ALL_GATES=(c1 c2 c3 c4 c5 c6 c7 c8 c9 c10 c11 c12)

# Flag parsing.
SUBSET=""
JSON=0
HALT=0
POST_FLIP=0
INVASIVE=0

for arg in "$@"; do
  case "$arg" in
    --gates=*)    SUBSET="${arg#*=}" ;;
    --gates)      shift; SUBSET="${1:-}" ;;
    --json)       JSON=1 ;;
    --halt)       HALT=1 ;;
    --post-flip)  POST_FLIP=1 ;;
    --invasive)   INVASIVE=1 ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      ;;
  esac
done

# Build list of gates to run.
GATES_TO_RUN=()
if [[ "$POST_FLIP" -eq 1 ]]; then
  GATES_TO_RUN=(postflip)
elif [[ -n "$SUBSET" ]]; then
  IFS=',' read -ra GATES_TO_RUN <<< "$SUBSET"
else
  GATES_TO_RUN=("${ALL_GATES[@]}")
fi

# Source all gate functions.
declare -A LOADED
for g in "${GATES_TO_RUN[@]}"; do
  lib_file="$LIB_DIR/cell2-gate-${g}.sh"
  if [[ ! -r "$lib_file" ]]; then
    echo "ERROR: missing gate lib $lib_file" >&2
    exit 1
  fi
  # shellcheck disable=SC1090
  source "$lib_file"
  LOADED[$g]=1
done
export REPO_ROOT

declare -A RESULT
overall=0

# JSON emitter — defined BEFORE main loop so HALT path can call it.
_emit_json() {
  local first=1
  printf '{"all_pass":%s,"gates":{' "$([[ $overall -eq 0 ]] && echo true || echo false)"
  for g in "${GATES_TO_RUN[@]}"; do
    if [[ $first -eq 1 ]]; then first=0; else printf ','; fi
    printf '"%s":"%s"' "$g" "${RESULT[$g]:-MISSING}"
  done
  printf '}}\n'
}

# Run each gate; capture result.
for g in "${GATES_TO_RUN[@]}"; do
  printf '[gate %s] running\n' "$g"
  if "gate_${g}"; then
    RESULT[$g]="PASS"
  else
    RESULT[$g]="FAIL"
    overall=1
    if [[ "$HALT" -eq 1 ]]; then
      printf '\n[gates] HALT on first FAIL (--halt). Final result: FAIL.\n'
      [[ "$JSON" -eq 1 ]] && _emit_json
      exit 1
    fi
  fi
done

# Print summary.
if [[ "$JSON" -eq 1 ]]; then
  _emit_json
else
  echo ""
  echo "[gates] Summary:"
  printf '  %-10s %s\n' "GATE" "RESULT"
  for g in "${GATES_TO_RUN[@]}"; do
    printf '  %-10s %s\n' "$g" "${RESULT[$g]:-MISSING}"
  done
  if (( overall == 0 )); then
    echo ""
    if (( POST_FLIP == 1 )); then
      echo "[gates] ALL PASS — post-flip checks GREEN; Stage 1 canary unblocked"
    else
      echo "[gates] ALL PASS — cell-2 is ready for migration 000007 + Stage 1 canary"
    fi
  else
    echo ""
    echo "[gates] FAIL — at least one gate failed. Do NOT proceed to migration 000007."
  fi
fi

exit "$overall"
