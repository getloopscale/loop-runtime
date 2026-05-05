#!/usr/bin/env bash
# infra/scripts/bootstrap-seed-tenant-cr.sh
#
# B26 fix (real-paas-cleanroom-dogfood-loop iter 22, 2026-05-05):
# bootstraps the Tenant CR for the seed tenant (tn-e2etest) in
# standalone-multi mode. Mirrors api/internal/k8s/tenant_cr_manager.go
# buildTenantCR() so dogfood seed parity matches the API code path.
#
# Why: seed-local-user.sh inserts user/tenant/membership rows in the
# DB but does NOT create a Kubernetes Tenant CR. loopzero-api creates the
# CR via EnsureTenant only on the project-create code path. journey-01
# bypasses project-create (calls /api/v1/source/upload + /api/v1/builds
# directly) so without this script no Tenant CR exists, no namespace,
# no ArgoCD AppProject, no app pod ever runs.
#
# Idempotent: kubectl apply.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
NS="${LOOPZERO_NS:-loopzero-system}"
TENANT_ID="${SEED_TENANT_ID:-tn-e2etest}"
DISPLAY_NAME="${SEED_TENANT_DISPLAY:-${TENANT_ID}}"
PLAN="${SEED_TENANT_PLAN:-starter}"

log() { printf '[bootstrap-seed-tenant-cr] %s\n' "$*" >&2; }

cat <<YAML | kubectl --context "$CTX" -n "$NS" apply -f -
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: ${TENANT_ID}
  namespace: ${NS}
  labels:
    app.kubernetes.io/managed-by: bootstrap-seed-tenant-cr
spec:
  cellID: cell-default
  displayName: ${DISPLAY_NAME}
  plan: ${PLAN}
  environments:
    - name: production
      cluster: workload
  gitops:
    repoName: ${TENANT_ID}-gitops
    branch: main
  preview:
    maxEnvironments: 3
    ttlHours: 72
  limits:
    maxCPU: "4"
    maxMemory: "8Gi"
    maxStorage: "20Gi"
    maxBuildConcurrency: 2
YAML

log "Tenant CR ${NS}/${TENANT_ID} applied (cellID=cell-default plan=${PLAN})"
