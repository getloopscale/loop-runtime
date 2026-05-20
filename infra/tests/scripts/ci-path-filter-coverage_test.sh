#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Plan 2026-05-20 T2.5 ext — hermetic gate that validates EVERY workflow
# referencing `bash scripts/X.sh` also includes scripts/X.sh in its
# paths: filter. Catches silent no-trigger bug class.
#
# Why this gate matters
# ---------------------
# CI workflows can pass all gates but silently fail to re-trigger when
# their dependencies change. Pattern observed during 2026-05-20 cascade
# chain audit:
#   - helm-charts.yml ran `bash scripts/run-polaris.sh` but didn't
#     trigger on changes to that script
#   - infra-gates.yml ran `bash scripts/check-divergence-matrix.sh`
#     but didn't trigger on changes to script OR the matrix file
#
# Without this gate, a future contributor adding `bash scripts/new-script.sh`
# to a workflow could forget the path filter — gate appears to work
# locally but silently fails to re-validate when script changes.
#
# Exit codes:
#   0 — All script references have corresponding path filter entries
#   1 — One or more workflows have missing path filter entries
#   5 — Missing test dependency

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
WORKFLOWS_DIR="$ROOT/.github/workflows"

log() { printf '[ci-path-filter-coverage] %s\n' "$*" >&2; }

if [[ ! -d "$WORKFLOWS_DIR" ]]; then
  log "ERROR: $WORKFLOWS_DIR not found"
  exit 5
fi

# Workflows exempt from this check (e.g., reusable workflows that are
# called from other workflows — they inherit triggers from caller)
EXEMPT_WORKFLOWS=(
  # Reusable workflows are called via `uses: ./.github/workflows/X.yml`
  # and inherit triggers from caller — path filter on the reusable is
  # ignored by GitHub Actions
  "reusable-setup-helm.yml"
)

is_exempt() {
  local wf_name="$1"
  for exempt in "${EXEMPT_WORKFLOWS[@]}"; do
    if [[ "$wf_name" == "$exempt" ]]; then
      return 0
    fi
  done
  return 1
}

# Workflows without paths: filter trigger on EVERY push/PR — no filter
# means no missing-filter problem to worry about
has_paths_filter() {
  local wf="$1"
  # Check if 'paths:' appears anywhere within the on: block
  awk '/^on:/,/^[a-z]/{print}' "$wf" | grep -q "paths:"
}

TOTAL=0
PASS=0
FAIL=0
FAIL_LIST=()

for wf in "$WORKFLOWS_DIR"/*.yml; do
  wf_name=$(basename "$wf")
  TOTAL=$((TOTAL + 1))

  if is_exempt "$wf_name"; then
    continue
  fi

  # Skip if workflow has no paths: filter (runs on every change)
  if ! has_paths_filter "$wf"; then
    PASS=$((PASS + 1))
    continue
  fi

  # Find all `bash scripts/X.sh` references in workflow
  referenced=$(grep -oE "bash scripts/[a-zA-Z._-]+\.sh" "$wf" 2>/dev/null | sed 's|bash ||' | sort -u)

  if [[ -z "$referenced" ]]; then
    PASS=$((PASS + 1))
    continue
  fi

  # Find scripts in path filter
  in_paths=$(awk '/paths:/,/^[a-z]/{print}' "$wf" | grep -oE "scripts/[a-zA-Z._-]+\.sh" | sort -u)

  # Find references NOT in path filter
  missing=$(comm -23 <(echo "$referenced") <(echo "$in_paths"))

  # Strip empty
  missing_clean=$(echo "$missing" | tr -d '[:space:]')

  if [[ -z "$missing_clean" ]]; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    while IFS= read -r m; do
      [[ -z "$m" ]] && continue
      FAIL_LIST+=("$wf_name: missing path filter for $m")
    done <<< "$missing"
  fi
done

# --- Report ---
log "Total workflows scanned: $TOTAL"
log "Path-filter-coverage PASS: $PASS"
log "Path-filter-coverage FAIL: $FAIL"

if [[ ${#FAIL_LIST[@]} -gt 0 ]]; then
  echo ""
  log "FAIL — workflows reference scripts/ NOT in their paths: filter:"
  for entry in "${FAIL_LIST[@]}"; do
    log "  $entry"
  done
  echo ""
  log "Fix: add the script path to the workflow's on.{push,pull_request}.paths"
  log "filter so changes to the script trigger CI re-validation."
  exit 1
fi

log "PASS — all script references covered by path filter"
exit 0
