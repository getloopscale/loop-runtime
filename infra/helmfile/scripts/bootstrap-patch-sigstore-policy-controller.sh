#!/usr/bin/env bash
# bootstrap-patch-sigstore-policy-controller.sh
#
# B16 fix (real-paas-cleanroom-dogfood-loop iter 12, 2026-04-30).
# Extracted from Taskfile.yml bootstrap:standalone per T5.3 of plan
# 2026-05-21-deep-review-remediation.
#
# Sigstore policy-controller provides the ClusterImagePolicy CRD that
# admission-policies chart consumes. Required in workload cluster.
#
# Idempotent (helm upgrade --install).
#
# T2.2-followup (plan 2026-05-21-deep-review-remediation): adopts shared
# dry-run lib. --dry-run prints intended helm install without executing,
# useful for cleanroom previews and bootstrap script debugging.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../scripts/lib/dry-run.sh disable=SC1091
source "$SCRIPT_DIR/../../scripts/lib/dry-run.sh"
parse_dry_run_flag "$@"
if [ "${#REMAINING_ARGS[@]}" -gt 0 ]; then
  set -- "${REMAINING_ARGS[@]}"
else
  set --
fi

CONTEXT="${KUBECONTEXT:-k3d-loopzero-cell-0}"

# helm repo list is read-only — safe outside apply
helm repo list 2>/dev/null | grep -q '^sigstore' || apply helm repo add sigstore https://sigstore.github.io/helm-charts
# repo update tolerates transient network failures (best-effort refresh)
apply helm repo update >/dev/null 2>&1 || true

apply helm upgrade --install policy-controller sigstore/policy-controller \
  --version 0.8.1 \
  --kube-context "$CONTEXT" \
  --namespace cosign-system --create-namespace \
  -f infra/helm/policy-controller/values.yaml \
  --wait --timeout 3m

echo "[bootstrap-patch-sigstore-policy-controller] OK — policy-controller installed in $CONTEXT/cosign-system"
