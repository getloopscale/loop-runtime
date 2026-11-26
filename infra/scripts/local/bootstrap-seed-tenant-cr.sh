#!/usr/bin/env bash
# infra/scripts/local/bootstrap-seed-tenant-cr.sh
#
# B26 fix (real-paas-cleanroom-dogfood-loop iter 22, 2026-05-05):
# bootstraps the Tenant CR for the seed tenant (tn-e2etest) in
# standalone-multi mode. Mirrors api/internal/k8s/tenant_cr_manager.go
# buildTenantCR() so dogfood seed parity matches the API code path.
#
# Why: seed-local-user.sh inserts user/tenant/membership rows in the
# DB but does NOT create a Kubernetes Tenant CR. loop-runtime-api creates the
# CR via EnsureTenant only on the project-create code path. journey-01
# bypasses project-create (calls /api/v1/source/upload + /api/v1/builds
# directly) so without this script no Tenant CR exists, no namespace,
# no ArgoCD AppProject, no app pod ever runs.
#
# Idempotent: kubectl apply.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

CTX="${MGMT_CTX:-k3d-loop-runtime-core}"
NS="${LOOP_RUNTIME_NS:-loop-runtime-system}"
TENANT_ID="${SEED_TENANT_ID:-tn-e2etest}"
DISPLAY_NAME="${SEED_TENANT_DISPLAY:-${TENANT_ID}}"
PLAN="${SEED_TENANT_PLAN:-starter}"

log() { printf '[bootstrap-seed-tenant-cr] %s\n' "$*" >&2; }

cat <<YAML | kubectl --context "$CTX" -n "$NS" apply -f -
apiVersion: runtime.getloopscale.com/v1
kind: Tenant
metadata:
  name: ${TENANT_ID}
  namespace: ${NS}
  labels:
    app.kubernetes.io/managed-by: bootstrap-seed-tenant-cr
spec:
  cellID: cell-0
  displayName: ${DISPLAY_NAME}
  plan: ${PLAN}
  # Empty by construction — canonical post-M2 plan T1.1
  # (2026-06-09-m2-operator-canonical-fix). The CRD schema requires
  # spec.environments[].project as RFC-1123 string (minLength=1); the
  # legacy {name: production, cluster: workload} entry without project
  # was rejected at admission post-T1.1. Mirror is
  # api/internal/k8s/tenant_cr_manager.go buildTenantCR() which emits
  # an empty list and lets the canonical T4.1 chain in
  # routes/projects.go append the production env via
  # EnvironmentEnsurer when the user creates a project. The CRD
  # constraint allows MinItems=0 so empty validates. See
  # feedback_no_hardcode_gate_2026_06_09 — operator MUST NOT hardcode
  # environments[].project in any seed/bootstrap path.
  environments: []
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

log "Tenant CR ${NS}/${TENANT_ID} applied (cellID=cell-0 plan=${PLAN})"
