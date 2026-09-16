---
type: Operations
title: "Permission matrix — 746 grants across 15 RBAC subjects"
description: "Every (role, resource, verb) cell the map could read from source, with the file and line that grants it."
resource: "charts"
tags:
  - rbac
  - permissions
  - kubernetes
  - security
sources:
  - id: pm-api
    resource: "infra/helm/loopzero-api/templates/serviceaccount.yaml#L42-L44"
    note: "Kubernetes RBAC rule block inside a ClusterRole with a literal metadata.name. This is the grant site: the cell exists because this rule exists."
  - id: pm-ctrl
    resource: "infra/helmfile/scripts/lib/scoped-kubeconfig.sh#L53"
    note: "loopzero-ctrl-runtime-rbac-admin"
  - id: pm-deployer
    resource: "operators/internal/constants/resources.go#L79"
    note: "loopzero-deployer"
  - id: pm-app
    resource: "operators/internal/environment/environment_rbac.go#L104"
    note: "loopzero-app"
  - id: pm-wf
    resource: "charts/loopzero-build-cwts/templates/rbac.yaml#L42"
    note: "loopzero-workflow-submitter"
  - id: pm-drift
    resource: "infra/helm/loopzero-api/templates/serviceaccount.yaml#L27"
    note: "The two loopzero-api ClusterRoles are a DECLARED parity pair and grant materially different RBAC — only one carries the documented blast-radius remediation"
  - id: pm-tmpl
    resource: "charts/loopzero-api/templates/clusterrole.yaml#L5"
    note: "Five declared ClusterRoles have no readable name: metadata.name is a Helm template expression"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

**746 grant rows**, 15 role keys, 85 distinct resources. Each row is a `(role, resource, verb)`
cell read from a Kubernetes RBAC rule block in a chart or a script, with the line that grants
it.[^pm-api]

`✔` is an unconditional allow. `◐` is **conditional** — 17 cells, all belonging to
`loopzero-deployer`, whose rule set is plan-tiered by `rbacRulesForPlan`: every plan gets services,
configmaps and `apps/deployments`; growth and enterprise additionally get secrets; enterprise
alone gets ingresses and networkpolicies including delete.[^pm-deployer]

# The shape, before the detail

| Subject | Grants | Resources | What it is |
|---|---|---|---|
| `loopzero_api` | 175 | 27 | ClusterRole (declared, literal name) |
| `loopzero_tenant_operator` | 155 | 33 | no named entity recorded |
| `loopzero_ctrl_runtime_rbac_admin` | 74 | 19 | ClusterRole (created IMPERATIVELY, declared in no chart) |
| `loopzero_api_published` | 63 | 12 | no named entity recorded |
| `loopzero_workflow_submitter` | 47 | 7 | ClusterRole (declared, literal name) |
| `loopzero_platform_operator` | 46 | 12 | no named entity recorded |
| `loopzero_app_operator` | 41 | 12 | no named entity recorded |
| `loopzero_preview_operator` | 40 | 9 | no named entity recorded |
| `loopzero_deployer` | 32 | 6 | Role (namespaced, per environment) |
| `argo_ci_namespace` | 29 | 8 | no named entity recorded |
| `loopzero_build_sa` | 20 | 5 | Role (namespaced, argo) |
| `chaos_orphan_cleanup` | 12 | 4 | ClusterRole (declared, literal name) |
| `loopzero_smoke_test` | 10 | 5 | ClusterRole (declared, literal name) |
| `loopzero_platform_operator_secrets_argocd` | 1 | 1 | no named entity recorded |
| `loopzero_platform_operator_secrets_loopzero_system` | 1 | 1 | no named entity recorded |

Three readings worth carrying away.

**One identity is created imperatively and reconciled by nothing.**
`loopzero-ctrl-runtime-rbac-admin` appears in no manifest under `charts/` or `infra/helm/`. It is
piped into `kubectl apply` from a heredoc in
`infra/helmfile/scripts/lib/scoped-kubeconfig.sh`, and is otherwise referenced only by a
regression check that greps a live cluster for it. It exists on a cell **only if that bootstrap
script ran there**.[^pm-ctrl] It holds cluster-wide `get/list/watch` on `*/*` so ArgoCD's
resource cache does not drop the cluster, plus write verbs on eight framework CRD groups. A
missing apiGroup here fails **silently**, as an ArgoCD App stuck `OutOfSync` with
`health=Missing`.

**The identity tenant pods run as holds no Kubernetes access at all.** `loopzero-app` sets
`automountServiceAccountToken: false`; it exists to carry the `loopzero-system` imagePullSecret, not
to grant API access.[^pm-app] It is deliberately separate from `loopzero-deployer`, which is what
applies workloads.

**One grant exists because of an upstream regression.** `loopzero-workflow-submitter` includes
`workflowtaskresults` and `workflowtasksets` because the argoexec sidecar writes them back;
without that second rule every workflow step on Argo Workflows v3.5+ ends
`exit code 64: workflowtaskresults.argoproj.io is forbidden`.[^pm-wf]

# Schema


## `loopzero-api`

_ClusterRole (declared, literal name) — 175 grants over 27 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `apps/daemonsets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:42-44` |
| `apps/deployments` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:42-44` |
| `apps/replicasets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:42-44` |
| `apps/statefulsets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:42-44` |
| `argoproj.io/applications` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:31-33` |
| `argoproj.io/appprojects` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:31-33` |
| `argoproj.io/cronworkflows` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:23-25` |
| `argoproj.io/workflows` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:23-25` |
| `argoproj.io/workflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:23-25` |
| `batch/cronjobs` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:45-47` |
| `batch/jobs` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:45-47` |
| `core/configmaps` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` |
| `core/events` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` |
| `core/namespaces` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` |
| `core/pods` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` |
| `core/pods/exec` | ✔ | ✔ | — | ✔ | — | — | — | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:38-40` |
| `core/pods/log` | ✔ | ✔ | — | ✔ | — | — | — | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:38-40` |
| `core/secrets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` |
| `core/services` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` |
| `external-secrets.io/clustersecretstores` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:56-58` |
| `external-secrets.io/externalsecrets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:56-58` |
| `external-secrets.io/secretstores` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:56-58` |
| `networking.k8s.io/ingresses` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:49-51` |
| `networking.k8s.io/networkpolicies` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:49-51` |
| `loopzero.io/*` | — | — | — | — | — | — | — | ✔ | `infra/helm/loopzero-api/templates/serviceaccount.yaml:27-29` |
| `traefik.io/ingressroutes` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:52-54` |
| `traefik.io/middlewares` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `infra/helm/loopzero-api/templates/serviceaccount.yaml:52-54` |


## `loopzero-tenant-operator`

_RBAC subject (no named entity recorded) — 155 grants over 33 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `apps/deployments` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:76-78` |
| `argoproj.io/applications` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:81-83` |
| `argoproj.io/appprojects` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:81-83` |
| `argoproj.io/clusterworkflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:93-95` |
| `argoproj.io/workflows` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:93-95` |
| `argoproj.io/workflowtaskresults` | ✔ | ✔ | — | ✔ | — | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:89-91` |
| `argoproj.io/workflowtasksets` | ✔ | ✔ | — | ✔ | — | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:89-91` |
| `argoproj.io/workflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:93-95` |
| `coordination.k8s.io/leases` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:103-105` |
| `core/configmaps` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:73-75` |
| `core/events` | — | — | — | ✔ | — | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:98-100` |
| `core/limitranges` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:43-45` |
| `core/namespaces` | ✔ | ✔ | ✔ | ✔ | — | — | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:38-40` |
| `core/pods` | ✔ | ✔ | ✔ | — | — | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:86-88` |
| `core/pods/log` | ✔ | ✔ | ✔ | — | — | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:86-88` |
| `core/resourcequotas` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:43-45` |
| `core/secrets` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:63-65` |
| `core/serviceaccounts` | ✔ | ✔ | ✔ | ✔ | — | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:53-55` |
| `core/services` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:73-75` |
| `networking.k8s.io/networkpolicies` | ✔ | ✔ | ✔ | ✔ | ✔ | — | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:58-60` |
| `rbac.authorization.k8s.io/rolebindings` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:48-50` |
| `rbac.authorization.k8s.io/roles` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:48-50` |
| `scheduling.k8s.io/priorityclasses` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:68-70` |
| `loopzero.io/clustercredentials` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:33-35` |
| `loopzero.io/environments` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:17-19` |
| `loopzero.io/environments/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:23-25` |
| `loopzero.io/environments/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:20-22` |
| `loopzero.io/previewenvironments` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:17-19` |
| `loopzero.io/previewenvironments/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:23-25` |
| `loopzero.io/previewenvironments/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:20-22` |
| `loopzero.io/tenants` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:17-19` |
| `loopzero.io/tenants/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:23-25` |
| `loopzero.io/tenants/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-tenant-operator/templates/clusterrole.yaml:20-22` |


## `loopzero-ctrl-runtime-rbac-admin`

_ClusterRole (created IMPERATIVELY, declared in no chart) — 74 grants over 19 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `*/*` | ✔ | ✔ | ✔ | — | — | — | — | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:49-51` |
| `acme.cert-manager.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `apps/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `argoproj.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `autoscaling/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `batch/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `cert-manager.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `core/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `core/namespaces` | — | — | — | ✔ | ✔ | ✔ | — | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:52-54` |
| `discovery.k8s.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `external-secrets.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `keda.sh/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `monitoring.coreos.com/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `networking.k8s.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `policy/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:55-57` |
| `rbac.authorization.k8s.io/rolebindings` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:91-93` |
| `rbac.authorization.k8s.io/roles` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:91-93` |
| `traefik.containo.us/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |
| `traefik.io/*` | — | — | — | ✔ | ✔ | ✔ | ✔ | — | `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:65-67` |


## `loopzero-api-published`

_RBAC subject (no named entity recorded) — 63 grants over 12 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `argoproj.io/clusterworkflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-api/templates/clusterrole.yaml:23-25` |
| `argoproj.io/workflows` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-api/templates/clusterrole.yaml:23-25` |
| `argoproj.io/workflows/status` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-api/templates/clusterrole.yaml:23-25` |
| `argoproj.io/workflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-api/templates/clusterrole.yaml:23-25` |
| `core/configmaps` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | `charts/loopzero-api/templates/clusterrole.yaml:40-42` |
| `core/namespaces` | ✔ | ✔ | ✔ | ✔ | — | — | — | — | `charts/loopzero-api/templates/clusterrole.yaml:27-29` |
| `core/pods` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-api/templates/clusterrole.yaml:45-47` |
| `core/pods/log` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-api/templates/clusterrole.yaml:45-47` |
| `core/secrets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | `charts/loopzero-api/templates/clusterrole.yaml:40-42` |
| `core/services` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-api/templates/clusterrole.yaml:45-47` |
| `loopzero.io/*` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-api/templates/clusterrole.yaml:16-18` |
| `loopzero.io/*/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-api/templates/clusterrole.yaml:19-21` |


## `loopzero-workflow-submitter`

_ClusterRole (declared, literal name) — 47 grants over 7 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `argoproj.io/clusterworkflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-build-cwts/templates/rbac.yaml:48-50` |
| `argoproj.io/workflows` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-build-cwts/templates/rbac.yaml:48-50` |
| `argoproj.io/workflowtaskresults` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | `charts/loopzero-build-cwts/templates/rbac.yaml:56-58` |
| `argoproj.io/workflowtasksets` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | `charts/loopzero-build-cwts/templates/rbac.yaml:56-58` |
| `argoproj.io/workflowtemplates` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-build-cwts/templates/rbac.yaml:48-50` |
| `core/pods` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-build-cwts/templates/rbac.yaml:59-61` |
| `core/pods/log` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-build-cwts/templates/rbac.yaml:59-61` |


## `loopzero-platform-operator`

_RBAC subject (no named entity recorded) — 46 grants over 12 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `coordination.k8s.io/leases` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:31-33` |
| `core/events` | — | — | — | ✔ | — | — | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:26-28` |
| `core/secrets` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:21-23` |
| `loopzero.io/clustercredentials` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:10-12` |
| `loopzero.io/clustercredentials/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:16-18` |
| `loopzero.io/clustercredentials/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:13-15` |
| `loopzero.io/gitrepositorybindings` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:10-12` |
| `loopzero.io/gitrepositorybindings/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:16-18` |
| `loopzero.io/gitrepositorybindings/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:13-15` |
| `loopzero.io/platformbootstraps` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:10-12` |
| `loopzero.io/platformbootstraps/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:16-18` |
| `loopzero.io/platformbootstraps/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-platform-operator/templates/clusterrole.yaml:13-15` |


## `loopzero-app-operator`

_RBAC subject (no named entity recorded) — 41 grants over 12 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `coordination.k8s.io/leases` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:50-52` |
| `core/events` | — | — | — | ✔ | — | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:45-47` |
| `core/secrets` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:30-32` |
| `core/services` | ✔ | ✔ | — | — | — | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:35-37` |
| `networking.k8s.io/ingresses` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:40-42` |
| `loopzero.io/exposedroutes` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:10-12` |
| `loopzero.io/exposedroutes/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:16-18` |
| `loopzero.io/exposedroutes/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:13-15` |
| `loopzero.io/tenants` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:25-27` |
| `loopzero.io/loopzeroapplications` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:10-12` |
| `loopzero.io/loopzeroapplications/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:16-18` |
| `loopzero.io/loopzeroapplications/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-app-operator/templates/clusterrole.yaml:13-15` |


## `loopzero-preview-operator`

_RBAC subject (no named entity recorded) — 40 grants over 9 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `coordination.k8s.io/leases` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:41-43` |
| `core/events` | — | — | — | ✔ | — | — | — | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:36-38` |
| `core/limitranges` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:26-28` |
| `core/namespaces` | ✔ | ✔ | ✔ | ✔ | — | — | ✔ | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:21-23` |
| `core/resourcequotas` | ✔ | ✔ | ✔ | ✔ | ✔ | — | — | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:26-28` |
| `networking.k8s.io/networkpolicies` | ✔ | ✔ | ✔ | ✔ | ✔ | — | ✔ | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:31-33` |
| `loopzero.io/previewenvironments` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:10-12` |
| `loopzero.io/previewenvironments/finalizers` | — | — | — | — | ✔ | — | — | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:16-18` |
| `loopzero.io/previewenvironments/status` | ✔ | — | — | — | ✔ | ✔ | — | — | `charts/loopzero-preview-operator/templates/clusterrole.yaml:13-15` |


## `loopzero-deployer`

_Role (namespaced, per environment) — 32 grants over 6 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `apps/deployments` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `operators/internal/environment/environment_rbac.go:209-219` |
| `core/configmaps` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `operators/internal/environment/environment_rbac.go:209-219` |
| `core/secrets` | ◐ | ◐ | — | ◐ | ◐ | ◐ | — | — | `operators/internal/environment/environment_rbac.go:226-232` |
| `core/services` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `operators/internal/environment/environment_rbac.go:209-219` |
| `networking.k8s.io/ingresses` | ◐ | ◐ | — | ◐ | ◐ | ◐ | ◐ | — | `operators/internal/environment/environment_rbac.go:234-239` |
| `networking.k8s.io/networkpolicies` | ◐ | ◐ | — | ◐ | ◐ | ◐ | ◐ | — | `operators/internal/environment/environment_rbac.go:234-239` |


## `argo-ci-namespace`

_RBAC subject (no named entity recorded) — 29 grants over 8 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `argoproj.io/workflows` | ✔ | ✔ | ✔ | ✔ | — | ✔ | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:20-22` |
| `argoproj.io/workflows/finalizers` | ✔ | ✔ | ✔ | ✔ | — | ✔ | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:20-22` |
| `argoproj.io/workflowtaskresults` | ✔ | ✔ | ✔ | ✔ | — | ✔ | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:23-25` |
| `argoproj.io/workflowtasksets` | ✔ | ✔ | ✔ | ✔ | — | ✔ | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:23-25` |
| `core/configmaps` | ✔ | ✔ | — | — | — | — | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:29-31` |
| `core/pods` | ✔ | ✔ | ✔ | — | — | — | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:26-28` |
| `core/pods/log` | ✔ | ✔ | ✔ | — | — | — | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:26-28` |
| `core/secrets` | ✔ | — | — | — | — | — | — | — | `infra/helm/argo-ci-namespace/templates/rbac.yaml:32-34` |


## `loopzero-build-sa`

_Role (namespaced, argo) — 20 grants over 5 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `core/configmaps` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `charts/loopzero-build-cwts/templates/rbac.yaml:15-17` |
| `core/persistentvolumeclaims` | ✔ | ✔ | — | ✔ | — | — | ✔ | — | `charts/loopzero-build-cwts/templates/rbac.yaml:18-20` |
| `core/pods` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-build-cwts/templates/rbac.yaml:12-14` |
| `core/pods/log` | ✔ | ✔ | ✔ | — | — | — | — | — | `charts/loopzero-build-cwts/templates/rbac.yaml:12-14` |
| `core/secrets` | ✔ | ✔ | — | ✔ | ✔ | ✔ | — | — | `charts/loopzero-build-cwts/templates/rbac.yaml:15-17` |


## `chaos-orphan-cleanup`

_ClusterRole (declared, literal name) — 12 grants over 4 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `chaos-mesh.org/iochaos` | ✔ | ✔ | — | — | — | ✔ | — | — | `infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml:91-93` |
| `chaos-mesh.org/networkchaos` | ✔ | ✔ | — | — | — | ✔ | — | — | `infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml:91-93` |
| `chaos-mesh.org/podchaos` | ✔ | ✔ | — | — | — | ✔ | — | — | `infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml:91-93` |
| `chaos-mesh.org/stresschaos` | ✔ | ✔ | — | — | — | ✔ | — | — | `infra/helm/chaos-mesh/templates/orphan-cleanup-cronjob.yaml:91-93` |


## `loopzero-smoke-test`

_ClusterRole (declared, literal name) — 10 grants over 5 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `apps/daemonsets` | ✔ | ✔ | — | — | — | — | — | — | `infra/helm/smoke-test/templates/job.yaml:164-166` |
| `apps/deployments` | ✔ | ✔ | — | — | — | — | — | — | `infra/helm/smoke-test/templates/job.yaml:164-166` |
| `apps/statefulsets` | ✔ | ✔ | — | — | — | — | — | — | `infra/helm/smoke-test/templates/job.yaml:164-166` |
| `core/namespaces` | ✔ | ✔ | — | — | — | — | — | — | `infra/helm/smoke-test/templates/job.yaml:161-163` |
| `core/pods` | ✔ | ✔ | — | — | — | — | — | — | `infra/helm/smoke-test/templates/job.yaml:161-163` |


## `loopzero-platform-operator-secrets-argocd`

_RBAC subject (no named entity recorded) — 1 grants over 1 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `core/secrets` | — | — | — | — | — | — | ✔ | — | `charts/loopzero-platform-operator/templates/role-argocd.yaml:18-20` |


## `loopzero-platform-operator-secrets-loopzero-system`

_RBAC subject (no named entity recorded) — 1 grants over 1 resources._

| Resource | get | list | watch | create | update | patch | delete | * |
|---|---|---|---|---|---|---|---|---|
| `core/secrets` | — | — | — | — | — | — | ✔ | — | `charts/loopzero-platform-operator/templates/role-loopzero-system.yaml:16-18` |


# Relationships

- The eight named identities and what each exists to do are
  [`/entities/rbac-roles.md`](/entities/rbac-roles.md).
- `loopzero-deployer` and `loopzero-app` are created per environment by step 11 of
  [`/flows/environment-reconcile-loop.md`](/flows/environment-reconcile-loop.md).
- The charts that declare these roles are
  [`/components/helm-charts.md`](/components/helm-charts.md); the clusters they land on are
  [`/components/cluster-topology.md`](/components/cluster-topology.md).
- Admission policies constrain what these identities may do beyond RBAC — see
  [`/components/platform-runtime-services.md`](/components/platform-runtime-services.md).

# What this matrix does not cover

**A parity pair that is not at parity.** `role.loopzero_api` (175 grants, from
`infra/helm/loopzero-api`) and `role.loopzero_api_published` (63 grants, from `charts/loopzero-api`) are the
**same chart name in two trees**, declared as a parity pair whose contract is *"when patching
one, apply the same fix to the other."* They disagree on **162 of 200** distinct (resource, verb)
cells, and only the published one gates the cluster-wide secrets/configmaps grant behind
`.Values.rbac.legacyClusterWideSecrets` with the comment recording it as a catastrophic blast
radius.[^pm-drift]

**Five ClusterRoles have no readable name.** `metadata.name` is a Helm template expression, so
the identity exists only after `helm template` runs with values.[^pm-tmpl] Their grants are in
the matrix; the name they will carry in a cluster is not derivable from source. This is a
dynamic-only region, recorded rather than guessed.

**One anchor in this matrix was off by a line, and was repaired during the run.** The 12
conditional `loopzero-deployer` grants on `networking.k8s.io` cited a span of
`operators/internal/environment/environment_rbac.go` running one line past the end of a 239-line
file. It now reads `234-239` and re-verifies clean.

The grants were never in doubt: the rule block those 12 cells describe **is** in that function —
the enterprise branch granting `ingresses` and `networkpolicies` — so the cells were right and
the citation overshot. It is recorded here rather than quietly dropped once fixed, because it was
the single genuinely stale `file:line` anchor out of 2,025, and a map that repairs its own
citations without leaving a trace cannot be audited.

**Testing a policy with the default kubeconfig proves nothing.** The k3d kubeconfig is admin and
belongs to `system:masters`, a group the ValidatingAdmissionPolicies exempt — the test passes
because the delete was *allowed*. A real test uses
`--as=system:serviceaccount:<ns>:<sa>` with a non-exempt SA **and grants it RBAC**; without RBAC
the `Forbidden` comes from RBAC and says nothing about the policy.

# Evidence

[^pm-api]: `infra/helm/loopzero-api/templates/serviceaccount.yaml:42-44`
[^pm-ctrl]: `infra/helmfile/scripts/lib/scoped-kubeconfig.sh:53`
[^pm-deployer]: `operators/internal/constants/resources.go:79`
[^pm-app]: `operators/internal/environment/environment_rbac.go:104`
[^pm-wf]: `charts/loopzero-build-cwts/templates/rbac.yaml:42`
[^pm-drift]: `infra/helm/loopzero-api/templates/serviceaccount.yaml:27`
[^pm-tmpl]: `charts/loopzero-api/templates/clusterrole.yaml:5`
