#!/usr/bin/env bash
# bootstrap-patch-admission-policies.sh
#
# B13 fix (real-paas-cleanroom-dogfood-loop iter 11, 2026-04-30).
# Extracted from Taskfile.yml bootstrap:standalone per T5.3 of plan
# 2026-05-21-deep-review-remediation.
#
# Installs the admission-policies chart with the k3d-loopzero-registry
# prefix added to the warn-mode ClusterImagePolicy allow-list. Without
# this prefix, every locally-built image is rejected by sigstore
# policy-controller at admission.
#
# Pre-requisites:
#   - bootstrap-patch-sigstore-policy-controller.sh ran successfully
#     (provides the ClusterImagePolicy CRD)
#
# Idempotent.

set -euo pipefail

CONTEXT="${KUBECONTEXT:-k3d-loopzero-cell-0}"

helm upgrade --install admission-policies infra/helm/admission-policies \
  --kube-context "$CONTEXT" \
  --namespace cosign-system --create-namespace \
  --set 'registries.additionalPrefixes[0]=k3d-loopzero-registry:5000/' \
  --wait --timeout 2m

echo "[bootstrap-patch-admission-policies] OK — admission-policies installed in $CONTEXT/cosign-system"
