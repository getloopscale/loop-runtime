---
type: Entity
title: "The eight named RBAC identities"
description: "Every ClusterRole, Role and ServiceAccount the map could read by name, and what each one exists to do."
resource: "charts"
tags:
  - rbac
  - kubernetes
  - security
  - entity
sources:
  - id: r-chaos_orphan_cleanup
    resource: "infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml#L89"
    note: "chaos-orphan-cleanup"
  - id: r-loopzero_api
    resource: "infra/helm/loopzero-api/templates/serviceaccount.yaml#L18"
    note: "loopzero-api"
  - id: r-loopzero_app
    resource: "operators/internal/environment/environment_rbac.go#L104"
    note: "loopzero-app"
  - id: r-loopzero_build_sa
    resource: "charts/loopzero-build-cwts/templates/rbac.yaml#L6"
    note: "loopzero-build-sa"
  - id: r-loopzero_ctrl_runtime_rbac_admin
    resource: "infra/helmfile/scripts/lib/scoped-kubeconfig.sh#L53"
    note: "loopzero-ctrl-runtime-rbac-admin"
  - id: r-loopzero_deployer
    resource: "operators/internal/constants/resources.go#L79"
    note: "loopzero-deployer"
  - id: r-loopzero_smoke_test
    resource: "infra/helm/smoke-test/templates/job.yaml#L153"
    note: "loopzero-smoke-test"
  - id: r-loopzero_workflow_submitter
    resource: "charts/loopzero-build-cwts/templates/rbac.yaml#L42"
    note: "loopzero-workflow-submitter"
  - id: r-drift
    resource: "infra/helm/loopzero-api/templates/serviceaccount.yaml#L27"
    note: "The two loopzero-api ClusterRoles are a DECLARED parity pair and grant materially different RBAC — only one carries the documented blast-radius remediation"
  - id: r-tmpl
    resource: "charts/loopzero-api/templates/clusterrole.yaml#L5"
    note: "Five declared ClusterRoles have no readable name: metadata.name is a Helm template expression"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Eight identities the map could resolve to a literal name. They are not peers: two are tenant-facing,
three are bootstrap-layer, one is a build identity, one is destructive by purpose, and one is
created **imperatively by a shell script** and exists in no chart at all.

# Schema

| Role | Kind | Permission rows | Distinct resources | Anchor |
|---|---|---|---|---|
| **`chaos-orphan-cleanup`** | ClusterRole (declared, literal name) | 12 | 4 | `infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml:89` |
| **`loopzero-api`** | ClusterRole (declared, literal name) | 175 | 27 | `infra/helm/loopzero-api/templates/serviceaccount.yaml:18` |
| **`loopzero-app`** | ServiceAccount (namespaced, per environment) | 0 | 0 | `operators/internal/environment/environment_rbac.go:104` |
| **`loopzero-build-sa`** | Role (namespaced, argo) | 20 | 5 | `charts/loopzero-build-cwts/templates/rbac.yaml:6` |
| **`loopzero-ctrl-runtime-rbac-admin`** | ClusterRole (created IMPERATIVELY, declared in no chart) | 74 | 19 | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:53` |
| **`loopzero-deployer`** | Role (namespaced, per environment) | 32 | 6 | `operators/internal/constants/resources.go:79` |
| **`loopzero-smoke-test`** | ClusterRole (declared, literal name) | 10 | 5 | `infra/helm/smoke-test/templates/job.yaml:153` |
| **`loopzero-workflow-submitter`** | ClusterRole (declared, literal name) | 47 | 7 | `charts/loopzero-build-cwts/templates/rbac.yaml:42` |


## `chaos-orphan-cleanup`

Declared ClusterRole for the chaos-mesh orphan-cleanup CronJob. Exists to delete chaos experiment leftovers, so its verbs are destructive by purpose — worth separating from the platform identities above when reading a permission matrix.


## `loopzero-api`

Declared ClusterRole bound to the loopzero-api ServiceAccount by the BOOTSTRAP chart infra/helm/loopzero-api. Note the chart dual system: infra/helm/loopzero-api and charts/loopzero-api are two charts with the same `name`, and the published OCI one declares its ClusterRole with a template-resolved name instead (charts/loopzero-api/templates/clusterrole.yaml:5). Only the bootstrap side has a literal name readable from source.


## `loopzero-app`

The identity tenant PODS run as — deliberately separate from loopzero-deployer. AutomountServiceAccountToken is set to false, so a workload pod carries no Kubernetes API token at all; it exists to hold the loopzero-system imagePullSecret, not to grant API access.


## `loopzero-build-sa`

Namespaced Role in the argo namespace on the BUILD cluster, bound to the build workflow ServiceAccount. Read on pods and pods/log; create/update/patch on configmaps and secrets; create/delete on persistentvolumeclaims — the surface one build step needs and no more.


## `loopzero-ctrl-runtime-rbac-admin`

NOT part of the chart layer: this ClusterRole appears in no manifest under charts/ or infra/helm/. It is piped into `kubectl apply` from a heredoc in infra/helmfile/scripts/lib/scoped-kubeconfig.sh (the ClusterRole body starts at line 52) and is only otherwise referenced by infra/helmfile/scripts/regression-checks.sh:113, which greps the live cluster for it. So it exists on a runtime cell only if that bootstrap script ran there; nothing declarative reconciles it. It is bound to the loopzero-ctrl-runtime ServiceAccount in loopzero-system and is the identity ArgoCD uses to apply tenant manifests. Cluster-wide get/list/watch on */* (B7, so ArgoCD's resource cache does not drop the cluster), create/update/patch/delete on core+apps+networking+batch+autoscaling+policy, and the same verbs on eight framework CRD groups (traefik.io, traefik.containo.us, cert-manager.io, acme.cert-manager.io, keda.sh, external-secrets.io, monitoring.coreos.com, argoproj.io). A missing apiGroup here fails silently as an ArgoCD App stuck OutOfSync with health=Missing.


## `loopzero-deployer`

Namespaced Role created by the Environment reconciler in every tenant environment namespace, bound to the loopzero-deployer ServiceAccount. Its rule set is PLAN-TIERED by rbacRulesForPlan: every plan gets services+configmaps and apps/deployments; growth and enterprise additionally get secrets; enterprise alone gets ingresses+networkpolicies including delete.


## `loopzero-smoke-test`

Declared ClusterRole for the post-install smoke-test Job in infra/helm/smoke-test. Bootstrap-layer verification identity, not a tenant-facing one.


## `loopzero-workflow-submitter`

Declared ClusterRole that lets loopzero-api create Workflows on the build cluster: full verbs on workflows/workflowtemplates/clusterworkflowtemplates, plus workflowtaskresults+workflowtasksets which the argoexec sidecar writes back — without that second rule every workflow step ends 'exit code 64: workflowtaskresults.argoproj.io is forbidden' on Argo Workflows v3.5+.


# Relationships

- The full `(role, resource, action)` matrix — 746 rows across 15 role keys — is
  [`/operations/permissions.md`](/operations/permissions.md).
- `loopzero-deployer` and `loopzero-app` are created per environment by the Environment reconciler traced
  in [`/flows/environment-reconcile-loop.md`](/flows/environment-reconcile-loop.md), step 11.
- `loopzero-workflow-submitter` is what lets the API create Workflows on the build cluster — see
  [`/components/build-orchestration.md`](/components/build-orchestration.md).
- The charts that declare them are in
  [`/components/helm-charts.md`](/components/helm-charts.md); the clusters they exist on are in
  [`/components/cluster-topology.md`](/components/cluster-topology.md).

## Two things a reader should not assume

**The declared parity pair does not grant the same RBAC.** `infra/helm/loopzero-api` and
`charts/loopzero-api` are one of exactly two declared parity pairs, and their ClusterRoles disagree
on **162 of 200** distinct (resource, verb) cells — with only the published chart carrying the
gate and the comment recording the cluster-wide secrets grant as a catastrophic blast
radius.[^r-drift]

**Five more ClusterRoles exist that this table cannot name.** Their `metadata.name` is a Helm
template expression, so the identity only comes into being after `helm template` runs with
values.[^r-tmpl] They are a dynamic-only region: readable in a cluster, not from source.

# Evidence

[^r-chaos_orphan_cleanup]: `infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml:89`
[^r-loopzero_api]: `infra/helm/loopzero-api/templates/serviceaccount.yaml:18`
[^r-loopzero_app]: `operators/internal/environment/environment_rbac.go:104`
[^r-loopzero_build_sa]: `charts/loopzero-build-cwts/templates/rbac.yaml:6`
[^r-loopzero_ctrl_runtime_rbac_admin]: `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:53`
[^r-loopzero_deployer]: `operators/internal/constants/resources.go:79`
[^r-loopzero_smoke_test]: `infra/helm/smoke-test/templates/job.yaml:153`
[^r-loopzero_workflow_submitter]: `charts/loopzero-build-cwts/templates/rbac.yaml:42`
[^r-drift]: `infra/helm/loopzero-api/templates/serviceaccount.yaml:27`
[^r-tmpl]: `charts/loopzero-api/templates/clusterrole.yaml:5`
