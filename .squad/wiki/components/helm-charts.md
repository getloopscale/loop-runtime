---
type: Component
title: "The two Helm chart trees"
description: "infra/helm bootstraps the platform through Helmfile; charts/ is the published OCI set ArgoCD consumes. Exactly two charts exist in both."
resource: "charts"
tags:
  - helm
  - chart
  - gitops
  - packaging
sources:
  - id: hc-charts
    resource: "charts/loopzero-api/Chart.yaml#L1-L5"
    note: "charts/ (published OCI charts)"
  - id: hc-common
    resource: "charts/loopzero-common/Chart.yaml#L1-L5"
    note: "loopzero-common (library chart)"
  - id: hc-api
    resource: "charts/loopzero-api/Chart.yaml#L1-L16"
    note: "loopzero-api (published OCI chart)"
  - id: hc-cloud
    resource: "charts/loopzero-cloud/Chart.yaml#L1-L16"
    note: "loopzero-cloud (published OCI chart)"
  - id: hc-adm
    resource: "charts/loopzero-admission-policies/Chart.yaml#L1-L12"
    note: "loopzero-admission-policies (published chart)"
  - id: hc-infra-helm
    resource: "infra/helm/loopzero-api/Chart.yaml#L1-L4"
    note: "infra/helm (bootstrap charts + value overlays)"
  - id: hc-pairs
    resource: "infra/quality/chart-pairs.txt#L1-L20"
    note: "chart-pairs parity contract"
  - id: hc-pol
    resource: "policies/charts/security_context.rego#L1-L8"
    note: "Conftest chart policies (Rego)"
  - id: hc-dep
    resource: "charts/loopzero-api/Chart.yaml#L1-L16"
    note: "Chart.yaml dependencies: loopzero-common \">=0.1.0 <1.0.0\" repository file://../loopzero-common"
  - id: hc-parity-ok
    resource: "infra/quality/chart-pairs.txt#L24-L25"
    note: "Verified: the two-chart parity claim holds, and the declared pair list matches disk"
  - id: hc-rbac-drift
    resource: "infra/helm/loopzero-api/templates/serviceaccount.yaml#L27"
    note: "The two loopzero-api ClusterRoles are a DECLARED parity pair and grant materially different RBAC — only one carries the documented blast-radius remediation"
  - id: hc-rego
    resource: "policies/charts/security_context.rego#L1"
    note: "policies/charts/*.rego are Conftest policies that nothing invokes"
  - id: hc-tmpl-name
    resource: "charts/loopzero-api/templates/clusterrole.yaml#L5"
    note: "Five declared ClusterRoles have no readable name: metadata.name is a Helm template expression"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

Two chart trees coexist **by design**, at different layers:

| Tree | Layer | Installed by | Dirs |
|---|---|---|---|
| `infra/helm/` | L2 bootstrap | Helmfile | 43 |
| `charts/` | L3 GitOps | ArgoCD, published as OCI | 12 |

Eight of the twelve published charts take a `file://../loopzero-common` dependency on the
`loopzero-common` library chart.[^hc-dep][^hc-common]

# Schema

| Component | Path | Kind | Layer | Anchor | What it is |
|---|---|---|---|---|---|
| `charts` | `charts` | artifact | infrastructure | `charts/loopzero-api/Chart.yaml:1-5` | 12 charts consumed by ArgoCD at L3. Eight depend on loopzero-common (a type: library chart) via file://../loopzero-common; loopzero-admission-policies, loopzero-build-cwts, loopzero-monitoring and loopzero-common itself declare no dependency. Born 2026-04-23 — the youngest major tree in the repo — with 70 commits. |
| `charts.common` | `charts/loopzero-common` | library | foundation | `charts/loopzero-common/Chart.yaml:1-5` | type: library. Depended on by 8 of the 12 published charts via repository file://../loopzero-common with version range >=0.1.0 <1.0.0. |
| `chart.loopzero_common` | `charts/loopzero-common` | library | l3-gitops | `charts/loopzero-common/Chart.yaml:1-5` | type: library, version 0.3.0. Shared Helm helpers every other published chart depends on via a file:// repository reference. |
| `chart.loopzero_admission_policies` | `charts/loopzero-admission-policies` | artifact | l3-gitops | `charts/loopzero-admission-policies/Chart.yaml:1-12` | version 0.3.0, declares no loopzero-common dependency. Lives only in the published tree — infra/helm/loopzero-admission-policies does not exist, so it is NOT a parity pair despite the similar name of infra/helm/admission-policies. |
| `chart.loopzero_api` | `charts/loopzero-api` | artifact | l3-gitops | `charts/loopzero-api/Chart.yaml:1-16` | Control-plane API chart, version 0.2.0. Also exists at infra/helm/loopzero-api as the bootstrap copy — a declared parity pair. |
| `chart.loopzero_app_operator` | `charts/loopzero-app-operator` | artifact | l3-gitops | `charts/loopzero-app-operator/Chart.yaml:1-16` | Operator chart, version 0.1.0. |
| `chart.loopzero_cloud` | `charts/loopzero-cloud` | artifact | l3-gitops | `charts/loopzero-cloud/Chart.yaml:1-16` | Control-plane / billing chart, version 0.2.0. Second declared parity pair with infra/helm/loopzero-cloud. |
| `chart.loopzero_crds` | `charts/loopzero-crds` | artifact | l3-gitops | `charts/loopzero-crds/Chart.yaml:1-16` | CustomResourceDefinition bundle, version 0.2.0. |
| `chart.loopzero_dashboard` | `charts/loopzero-dashboard` | artifact | l3-gitops | `charts/loopzero-dashboard/Chart.yaml:1-16` | Dashboard chart, version 0.2.0. |
| `chart.loopzero_monitoring` | `charts/loopzero-monitoring` | artifact | l3-gitops | `charts/loopzero-monitoring/Chart.yaml:1-12` | version 0.2.0. Complementary to infra/helm/monitoring (infra vs config), not an overlapping pair. |
| `chart.loopzero_platform_operator` | `charts/loopzero-platform-operator` | artifact | l3-gitops | `charts/loopzero-platform-operator/Chart.yaml:1-16` | Operator chart, version 0.1.0. |
| `chart.loopzero_preview_operator` | `charts/loopzero-preview-operator` | artifact | l3-gitops | `charts/loopzero-preview-operator/Chart.yaml:1-16` | Operator chart, version 0.1.0. |
| `chart.loopzero_tenant_operator` | `charts/loopzero-tenant-operator` | artifact | l3-gitops | `charts/loopzero-tenant-operator/Chart.yaml:1-16` | Operator chart, version 0.1.0. |
| `infra.helm` | `infra/helm` | artifact | infrastructure | `infra/helm/loopzero-api/Chart.yaml:1-4` | 43 directories, of which 24 hold a Chart.yaml and 19 hold values only (argocd, argo-workflows, blackbox-exporter, cert-manager, external-secrets, gitea, loki, minio, monitoring, nfs-server-provisioner, ory-hydra, ory-kratos, otel, policy-controller, postgresql, promtail, redis, tempo, traefik) — those 19 are value overlays onto upstream charts, NOT charts. Installed by Helmfile L2. Born 2026-03-13, 214 commits. |
| `infra.chart_pairs_contract` | `infra/quality/chart-pairs.txt` | other | l2-platform | `infra/quality/chart-pairs.txt:1-20` | The machine-readable list of charts that exist in BOTH infra/helm/ and charts/ under the same name, read by `loopzero-ops quality chart-pairs`. The gate fails an undeclared pair AND a declaration that outlived its pair. |
| `infra.chart_policies` | `policies/charts` | other | l3-gitops | `policies/charts/security_context.rego:1-8` | Three Rego policies (security_context, priority_class, service_monitor_port) asserting that published charts use the loopzero-common security-context defaults. Measured 2026-09-11: one commit ever (2026-05-05) and no invoker anywhere in the tree. |

# Relationships

- The L2 tree is driven by [`/components/helmfile-layer.md`](/components/helmfile-layer.md);
  the L3 tree is synced by ArgoCD from Gitea, described in
  [`/components/platform-orchestrators.md`](/components/platform-orchestrators.md).
- `infra.chart_pairs_contract` (`infra/quality/chart-pairs.txt`) declares which charts exist in
  both trees, and a gate refuses a pair that is undeclared **or** a declaration that outlived
  its pair.[^hc-pairs]
- The operator charts are installed into the control cluster by the core helmfile — see
  [`/components/cluster-topology.md`](/components/cluster-topology.md).

## The parity pair claim: verified, and then found to be violated in substance

`CLAUDE.md` says exactly **two** charts exist in both trees under the same `name` — `loopzero-api`
and `loopzero-cloud` — and that a third it used to claim never existed. Reading the `name:` field of
all 36 `Chart.yaml` files: **the claim holds exactly.**[^hc-parity-ok] `infra/helm/admission-policies`
is named `admission-policies`, not `loopzero-admission-policies`, so there is no third pair.

The contract that pair carries is *"when patching one, apply the same fix to the other."*
Measured, the two `loopzero-api` ClusterRoles **disagree on 162 of 200 distinct (resource, verb)
cells**, and the asymmetry runs the dangerous way: the published chart gates a cluster-wide
secrets/configmaps grant behind `.Values.rbac.legacyClusterWideSecrets` and carries a nine-line
comment recording it as *"deep review finding #1 (cluster-wide secrets/configmaps grant =
catastrophic blast radius)"*. The bootstrap chart carries neither the gate nor the
comment.[^hc-rbac-drift] The full permission matrix is
[`/operations/permissions.md`](/operations/permissions.md).

## Policies that assert to nobody

`policies/charts/*.rego` are three Conftest policies added in one commit on 2026-05-05, with no
write since. A repository-wide grep for `conftest` finds **no Taskfile target, no taskfile step,
no quality gate and no workflow** that runs them. They assert real invariants about the
published charts — non-root Deployments, `priorityClass`, ServiceMonitor port naming — and
assert them to nobody.[^hc-rego]

## A region the map could not read

Five declared ClusterRoles have **no readable name**: `metadata.name` is a Helm template
expression, so the identity only exists after `helm template` runs with values.[^hc-tmpl-name]
Those roles are present in [`/operations/permissions.md`](/operations/permissions.md) under
their template form, not under the name they will have in a cluster.

# Evidence

[^hc-charts]: `charts/loopzero-api/Chart.yaml:1-5`
[^hc-common]: `charts/loopzero-common/Chart.yaml:1-5`
[^hc-api]: `charts/loopzero-api/Chart.yaml:1-16`
[^hc-cloud]: `charts/loopzero-cloud/Chart.yaml:1-16`
[^hc-adm]: `charts/loopzero-admission-policies/Chart.yaml:1-12`
[^hc-infra-helm]: `infra/helm/loopzero-api/Chart.yaml:1-4`
[^hc-pairs]: `infra/quality/chart-pairs.txt:1-20`
[^hc-pol]: `policies/charts/security_context.rego:1-8`
[^hc-dep]: `charts/loopzero-api/Chart.yaml:1-16`
[^hc-parity-ok]: `infra/quality/chart-pairs.txt:24-25`
[^hc-rbac-drift]: `infra/helm/loopzero-api/templates/serviceaccount.yaml:27`
[^hc-rego]: `policies/charts/security_context.rego:1`
[^hc-tmpl-name]: `charts/loopzero-api/templates/clusterrole.yaml:5`
