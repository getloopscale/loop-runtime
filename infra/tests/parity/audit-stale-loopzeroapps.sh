#!/usr/bin/env bash
# fix-all-platform-issues-plan T0.2 — Stale LoopZeroApplication triage.
#
# For each LoopZeroApplication CR with phase=Progressing for > 1h, derive the
# expected GitOps manifest path, check if it exists in the latest commit
# of the tenant's gitops repo, and classify:
#   A — manifest still in source (real bug; reconcile should make it Active)
#   B — manifest absent (orphan — safe to clean up via T2.1 / T2.2 reaper)
#   C — gitops repo not bound (ambiguous; manual review)
#
# Output: Markdown table to stdout.
#
# Usage:
#   bash infra/tests/parity/audit-stale-loopzeroapps.sh
#
# Exit codes:
#   0 — audit completed
#   1 — kubectl unavailable
#
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-mgmt}"
GITEA_POD="${GITEA_POD:-deploy/gitea}"
GITEA_NS="${GITEA_NS:-loopzero-system}"
STALE_HOURS="${STALE_HOURS:-1}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) sed -n '2,18p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

command -v kubectl >/dev/null || { echo "ERROR: kubectl missing" >&2; exit 1; }
command -v jq >/dev/null      || { echo "ERROR: jq missing" >&2; exit 1; }

if ! kubectl --context "$KUBECTL_CONTEXT" --request-timeout=5s cluster-info >/dev/null 2>&1; then
  echo "## Stale LoopZeroApplication Audit — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "**Status:** ERROR — context $KUBECTL_CONTEXT unreachable."
  exit 1
fi

echo "## Stale LoopZeroApplication Audit — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""
echo "Stale threshold: phase=Progressing for > ${STALE_HOURS}h"
echo ""
echo "| Name | Tenant | Project | Env | Age | Class | Manifest in GitOps | Cleanup Path |"
echo "|------|--------|---------|-----|-----|-------|--------------------|--------------|"

NOW_EPOCH=$(date +%s)
THRESHOLD_S=$((STALE_HOURS * 3600))

# List all LoopZeroApps with phase=Progressing.
APPS_JSON=$(kubectl --context "$KUBECTL_CONTEXT" -n loopzero-system get loopzeroapplications -o json 2>/dev/null || echo '{"items":[]}')

echo "$APPS_JSON" | jq -r '.items[] | select(.status.phase == "Progressing") | "\(.metadata.name)|\(.spec.tenantRef // "")|\(.spec.project // "")|\(.spec.environment // "")|\(.metadata.creationTimestamp)"' | \
while IFS='|' read -r name tenant project env created; do
  [[ -z "$name" ]] && continue
  created_epoch=$(date -d "$created" +%s 2>/dev/null || echo "$NOW_EPOCH")
  age_s=$((NOW_EPOCH - created_epoch))
  age_h=$((age_s / 3600))
  if (( age_s < THRESHOLD_S )); then
    continue
  fi

  # Derive expected gitops repo + manifest path.
  # Convention: loopzero-platform/{tenant}-gitops.git, path: {env}/{appName}.yaml
  repo_path="/data/git/gitea-repositories/loopzero-platform/${tenant}-gitops.git"
  manifest_path="${env}/${project}.yaml"

  # Check repo exists.
  repo_exists=$(kubectl --context "$KUBECTL_CONTEXT" -n "$GITEA_NS" exec "$GITEA_POD" -c gitea -- \
    sh -c "test -d ${repo_path} && echo yes || echo no" 2>/dev/null | tr -d ' \r\n')
  if [[ "$repo_exists" != "yes" ]]; then
    echo "| ${name} | ${tenant} | ${project} | ${env} | ${age_h}h | C | repo absent | Manual review |"
    continue
  fi

  # Clone temp + check manifest path.
  manifest_present=$(kubectl --context "$KUBECTL_CONTEXT" -n "$GITEA_NS" exec "$GITEA_POD" -c gitea -- \
    sh -c "cd /tmp && rm -rf check-${tenant} && git clone -q ${repo_path} check-${tenant} 2>/dev/null && test -f check-${tenant}/${manifest_path} && echo yes || echo no; rm -rf /tmp/check-${tenant} 2>/dev/null" 2>/dev/null \
    | tr -d ' \r\n' | tail -c 3)

  if [[ "$manifest_present" == "yes" ]]; then
    echo "| ${name} | ${tenant} | ${project} | ${env} | ${age_h}h | A | present | T2.2 reaper handles (real bug — should reconcile) |"
  else
    echo "| ${name} | ${tenant} | ${project} | ${env} | ${age_h}h | B | ABSENT | T2.1 manual cleanup |"
  fi
done

echo ""
echo "**Total stale LoopZeroApps:** $(echo "$APPS_JSON" | jq -r '[.items[] | select(.status.phase == "Progressing")] | length')"
