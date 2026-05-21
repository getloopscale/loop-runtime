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

set -euo pipefail

CONTEXT="${KUBECONTEXT:-k3d-loopzero-workload}"

helm repo list 2>/dev/null | grep -q '^sigstore' || helm repo add sigstore https://sigstore.github.io/helm-charts
helm repo update >/dev/null 2>&1 || true

helm upgrade --install policy-controller sigstore/policy-controller \
  --version 0.8.1 \
  --kube-context "$CONTEXT" \
  --namespace cosign-system --create-namespace \
  -f infra/helm/policy-controller/values.yaml \
  --wait --timeout 3m

echo "[bootstrap-patch-sigstore-policy-controller] OK — policy-controller installed in $CONTEXT/cosign-system"
