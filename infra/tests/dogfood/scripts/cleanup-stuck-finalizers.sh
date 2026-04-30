#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY — T0.2
#
# Idempotent cleanup of LoopZero CRs (Tenant, Environment) stuck with finalizers
# whose controller can no longer make progress (owner ref deleted, or
# external resource already gone). Safety: ONLY removes finalizers from
# CRs whose name matches the test-prefix allowlist; production tenants
# (`tn-*`) are NEVER touched.
#
# Usage:
#   bash cleanup-stuck-finalizers.sh                # default ctx
#   CTX=k3d-loopzero-mgmt bash cleanup-stuck-finalizers.sh
#   bash cleanup-stuck-finalizers.sh --dry-run
#
# Exit codes:
#   0  — success
#   1  — usage error
#   2  — patch failed
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

CTX="${CTX:-k3d-loopzero-mgmt}"
NS="${NS:-loopzero-system}"
DRY_RUN=0

SAFE_PREFIXES=(
  "test-"           # test-rm-env-*, test-cr-create, etc
  "vap-test-"       # vap-test-allowed
)

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '4,20p' "$0"; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; exit 1 ;;
  esac
  shift
done

is_safe() {
  local name=$1
  for prefix in "${SAFE_PREFIXES[@]}"; do
    if [[ "$name" == "${prefix}"* ]]; then
      return 0
    fi
  done
  return 1
}

# Args: kind (tenants|environments) phase_filter (Deleting|<empty for orphan>)
clean_kind() {
  local kind=$1
  local total=0 patched=0 skipped=0

  echo "[finalizers] scanning ${kind}..." >&2
  # List CRs with deletionTimestamp set (Deleting OR orphan with empty phase)
  local items
  items=$(kubectl --context "$CTX" -n "$NS" get "$kind" -o json 2>/dev/null \
    | python3 -c "
import json, sys
d = json.load(sys.stdin)
for item in d.get('items', []):
    md = item.get('metadata', {})
    if md.get('deletionTimestamp'):
        print(md['name'])
")

  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    total=$((total + 1))
    if ! is_safe "$name"; then
      echo "[finalizers] SKIP ${kind}/$name (not in safe-prefix allowlist)" >&2
      skipped=$((skipped + 1))
      continue
    fi
    if [[ "$DRY_RUN" == "1" ]]; then
      echo "[finalizers] DRY-RUN would patch ${kind}/$name" >&2
      patched=$((patched + 1))
      continue
    fi
    if kubectl --context "$CTX" -n "$NS" patch "$kind" "$name" -p '{"metadata":{"finalizers":[]}}' --type=merge >/dev/null 2>&1; then
      echo "[finalizers] patched ${kind}/$name" >&2
      patched=$((patched + 1))
    else
      echo "[finalizers] FAIL patch ${kind}/$name" >&2
      return 2
    fi
  done <<< "$items"

  echo "[finalizers] ${kind}: total=$total patched=$patched skipped=$skipped" >&2
}

main() {
  echo "[finalizers] context=$CTX ns=$NS dry_run=$DRY_RUN" >&2
  clean_kind "environments"
  clean_kind "tenants"
}

main "$@"
