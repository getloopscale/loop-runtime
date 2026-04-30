#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-PARITY — T0.1
#
# Idempotent cleanup of namespaces stuck in Terminating phase. These accumulate
# from operator finalizers that fail when an external delete races the
# controller's reconcileDelete() — leaving the namespace's finalizer list
# non-empty even though the underlying workload is gone.
#
# Safety: ONLY removes namespaces with prefix `loopzero-test-*` or matching the
# explicit allowlist below. Production tenant namespaces (`loopzero-{tenant}-*`)
# are NEVER touched.
#
# Usage:
#   bash cleanup-terminating-namespaces.sh                # default ctx
#   CTX=k3d-loopzero-workload bash cleanup-terminating-namespaces.sh
#   bash cleanup-terminating-namespaces.sh --dry-run      # show without patching
#
# Exit codes:
#   0  — success (zero ns terminating after run, or dry-run completed)
#   1  — usage error
#   2  — patch failed
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

CTX="${CTX:-k3d-loopzero-workload}"
DRY_RUN=0

# Allowlist: prefixes we are willing to force-finalize. Anything else is
# rejected as too dangerous.
SAFE_PREFIXES=(
  "loopzero-test-"
  "build-test-"
  "build-vap-test-"
  "loopzero-vap-test-"
  "loopzero-tn-e2etest-staging"   # journey-05 leftover
)

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      sed -n '4,20p' "$0"
      exit 0
      ;;
    *)
      echo "ERROR: unknown arg: $1" >&2
      exit 1
      ;;
  esac
  shift
done

is_safe() {
  local ns=$1
  for prefix in "${SAFE_PREFIXES[@]}"; do
    if [[ "$ns" == "${prefix}"* ]]; then
      return 0
    fi
  done
  return 1
}

main() {
  echo "[cleanup] context=$CTX dry_run=$DRY_RUN" >&2
  local terminating
  terminating=$(kubectl --context "$CTX" get ns --no-headers 2>/dev/null | awk '$2=="Terminating" {print $1}')

  if [[ -z "$terminating" ]]; then
    echo "[cleanup] no terminating namespaces — clean ✓" >&2
    return 0
  fi

  local total=0 patched=0 skipped=0
  while IFS= read -r ns; do
    [[ -z "$ns" ]] && continue
    total=$((total + 1))
    if ! is_safe "$ns"; then
      echo "[cleanup] SKIP $ns (not in safe-prefix allowlist)" >&2
      skipped=$((skipped + 1))
      continue
    fi
    if [[ "$DRY_RUN" == "1" ]]; then
      echo "[cleanup] DRY-RUN would patch $ns" >&2
      patched=$((patched + 1))
      continue
    fi
    # First clear metadata.finalizers (operator-set), then spec.finalizers
    # (kubernetes-set) via /finalize subresource. Terminating ns ignores
    # `kubectl patch` on the spec; only `kubectl replace --raw .../finalize`
    # actually removes it. Two-step is required.
    kubectl --context "$CTX" patch ns "$ns" -p '{"metadata":{"finalizers":[]}}' --type=merge >/dev/null 2>&1 || true
    if kubectl --context "$CTX" get ns "$ns" -o json 2>/dev/null \
        | python3 -c 'import json,sys; d=json.load(sys.stdin); d["spec"]["finalizers"]=[]; print(json.dumps(d))' \
        | kubectl --context "$CTX" replace --raw "/api/v1/namespaces/${ns}/finalize" -f - >/dev/null 2>&1; then
      echo "[cleanup] patched $ns (spec.finalizers cleared)" >&2
      patched=$((patched + 1))
    else
      echo "[cleanup] FAIL patch $ns (subresource /finalize)" >&2
      return 2
    fi
  done <<< "$terminating"

  echo "[cleanup] total=$total patched=$patched skipped=$skipped" >&2
}

main "$@"
