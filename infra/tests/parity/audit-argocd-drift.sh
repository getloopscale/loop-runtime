#!/usr/bin/env bash
# fix-all-platform-issues-plan T0.1 — ArgoCD drift triage.
#
# Reads every ArgoCD Application across configured contexts, classifies each
# non-Synced app into one of three classes (per ADR D4):
#   A — config drift (Helm values differ from live; fix = helmfile sync)
#   B — missing resources (Health=Missing; fix = enable+install chart)
#   C — orphaned tracking (CR exists but resources don't / labels changed)
#
# Output: Markdown table to stdout with one row per OutOfSync app + Fix Phase
# pointer (T3.1/T3.2/T3.3).
#
# Usage:
#   bash infra/tests/parity/audit-argocd-drift.sh [--context <kubeconfig-context>]
#   --context  override KUBECTL_CONTEXT (default: k3d-loopzero-mgmt)
#
# Exit codes:
#   0 — audit completed (table emitted; rows may be 0)
#   1 — kubectl unavailable / cluster unreachable
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context=*) KUBECTL_CONTEXT="${1#*=}"; shift ;;
    --context)   KUBECTL_CONTEXT="${2:-k3d-loopzero-mgmt}"; shift 2 ;;
    -h|--help)   sed -n '2,20p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

command -v kubectl >/dev/null || { echo "ERROR: kubectl missing" >&2; exit 1; }
command -v jq >/dev/null      || { echo "ERROR: jq missing" >&2; exit 1; }

# Probe cluster reachability — emit empty table on unreachable rather than
# silently producing a stale audit.
if ! kubectl --context "$KUBECTL_CONTEXT" --request-timeout=5s cluster-info >/dev/null 2>&1; then
  echo "## ArgoCD Drift Audit — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "**Status:** ERROR — context $KUBECTL_CONTEXT unreachable."
  exit 1
fi

# Header.
echo "## ArgoCD Drift Audit — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""
echo "Context: \`${KUBECTL_CONTEXT}\`"
echo ""
echo "| App | Sync | Health | Class | Hypothesis | Fix Phase |"
echo "|-----|------|--------|-------|------------|-----------|"

# Pull all applications across all namespaces.
APPS_JSON=$(kubectl --context "$KUBECTL_CONTEXT" -n argocd get applications -o json 2>/dev/null || echo '{"items":[]}')
TOTAL=$(echo "$APPS_JSON" | jq '.items | length')
NON_SYNCED=0

# Iterate apps, emit rows for non-Synced ones only.
echo "$APPS_JSON" | jq -r '.items[] | "\(.metadata.name)|\(.status.sync.status // "Unknown")|\(.status.health.status // "Unknown")|\((.status.conditions // []) | map(.message) | join(" || "))"' | \
while IFS='|' read -r name sync health conditions; do
  [[ -z "$name" ]] && continue
  if [[ "$sync" == "Synced" ]] && [[ "$health" == "Healthy" ]]; then
    continue
  fi

  # Classify per D4.
  class="A"
  fix_phase="T3.1"
  hypothesis="Helm values drift"
  if [[ "$health" == "Missing" ]]; then
    class="B"
    fix_phase="T3.2"
    hypothesis="Resources never created (chart disabled or never applied)"
  elif echo "$conditions" | grep -qiE "duplicate|orphaned"; then
    if echo "$conditions" | grep -qi "duplicate"; then
      class="A"
      fix_phase="T3.1"
      hypothesis="Duplicate keys in Helm values (e.g., env vars)"
    else
      class="C"
      fix_phase="T3.3"
      hypothesis="Orphaned resources tracked by ArgoCD (label drift or post-rotation)"
    fi
  elif echo "$conditions" | grep -qi "controller is configured to ignore"; then
    class="C"
    fix_phase="T3.3"
    hypothesis="Cluster Secret bearer token rotation gap (post-cell-pin)"
  fi

  echo "| ${name} | ${sync} | ${health} | ${class} | ${hypothesis} | ${fix_phase} |"
  NON_SYNCED=$((NON_SYNCED + 1))
done

# Summary footer.
echo ""
echo "**Total apps:** ${TOTAL}"
echo "**Non-Synced rows:** see table above (0 = environment is GREEN)"
