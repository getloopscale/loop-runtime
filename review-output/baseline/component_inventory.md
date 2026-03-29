# Component Inventory — Cluster Management & Control Plane

> 18 components registered in review database

## Clusters (3)

| ID | Name | Technology | Path | Notes |
|----|------|-----------|------|-------|
| mgmt-cluster | Management Cluster | Kubernetes (k3d/DOKS) | infra/k3d/mgmt-cluster.yaml | Control plane. Hosts operators, Gitea, PG, MinIO, ArgoCD. Port 6443 local. |
| build-cluster | Build Cluster | Kubernetes (k3d/DOKS) | infra/k3d/build-cluster.yaml | Build plane. Argo Workflows + BuildKit. Port 6445 local. |
| workload-cluster | Workload Cluster | Kubernetes (k3d/DOKS) | infra/k3d/workload-cluster.yaml | Workload plane. User apps via ArgoCD. Port 6446 local. |

## Operator Controllers (7)

| ID | Name | Path | Dependencies | CRD | Wired |
|----|------|------|-------------|-----|-------|
| TenantController | Tenant Controller | operators/internal/controller/tenant_controller.go | gitea-provisioner, argocd-provisioner, minio-provisioner, build-cluster, workload-cluster | Tenant | Yes |
| EnvironmentController | Environment Controller | operators/internal/controller/environment_controller.go | workload-cluster | Environment | Yes |
| ApplicationController | Application Controller | operators/internal/controller/application_controller.go | — | LoopZeroApplication | Yes |
| ExposureController | Exposure Controller | operators/internal/controller/exposure_controller.go | — | ExposedRoute | Yes |
| PlatformBootstrapController | Platform Bootstrap Controller | operators/internal/controller/platformbootstrap_controller.go | gitea-provisioner, argocd-provisioner, httpClient | PlatformBootstrap | Yes |
| GitRepositoryBindingController | Git Repository Binding Controller | operators/internal/controller/gitrepositorybinding_controller.go | httpClient | GitRepositoryBinding | Yes |
| ClusterCredentialController | Cluster Credential Controller | operators/internal/controller/clustercredential_controller.go | argocd namespace | ClusterCredential | Yes |

## Provisioners (3)

| ID | Name | Technology | Interface | Real Implementation |
|----|------|-----------|-----------|-------------------|
| gitea-provisioner | Gitea Provisioner | Go + HTTP | gitea.GitRepoProvisioner | gitea.RealProvisioner |
| argocd-provisioner | ArgoCD Provisioner | Go + dynamic K8s client | argocd.Provisioner | argocd.RealProvisioner |
| minio-provisioner | MinIO Provisioner | Go + HTTP (Admin API) | minio.CredentialProvisioner | minio.AdminProvisioner |

## Infrastructure Components (5)

| ID | Name | Technology | Path |
|----|------|-----------|------|
| operator-entrypoint | Operator Entrypoint | Go | operators/cmd/main.go |
| statemachine-pkg | State Machine Library | Go | pkg/statemachine/ |
| helm-charts | Helm Charts (13) | Helm | infra/helm/ |
| infra-deploy-workflow | Infra Deploy Workflow | GitHub Actions | .github/workflows/infra-deploy.yml |
| taskfile-automation | Taskfile Automation | Taskfile | Taskfile.yml |

## CRDs (10 types — 7 wired, 3 unwired)

| CRD | Group | Controller | Status |
|-----|-------|-----------|--------|
| Tenant | loopzero.io/v1 | TenantController | Wired |
| Environment | loopzero.io/v1 | EnvironmentController | Wired |
| LoopZeroApplication | loopzero.io/v1 | ApplicationController | Wired |
| ExposedRoute | loopzero.io/v1 | ExposureController | Wired |
| PlatformBootstrap | loopzero.io/v1 | PlatformBootstrapController | Wired |
| GitRepositoryBinding | loopzero.io/v1 | GitRepositoryBindingController | Wired |
| ClusterCredential | loopzero.io/v1 | ClusterCredentialController | Wired |
| AppSecret | loopzero.io/v1 | — | **Unwired** (Sprint 6) |
| PreviewEnvironment | loopzero.io/v1 | — | **Unwired** (Sprint 5) |
| ReconciliationReport | loopzero.io/v1 | — | **Unwired** (not scheduled) |

## Helm Chart Versions

| Chart | Repository | Deployed To | Values File |
|-------|-----------|-------------|-------------|
| PostgreSQL | bitnami | mgmt | infra/helm/postgresql/values.yaml |
| Gitea 1.22.6 | gitea-charts | mgmt | infra/helm/gitea/values.yaml |
| MinIO | minio | mgmt | infra/helm/minio/values.yaml |
| ArgoCD 2.13.3 | argo | mgmt | infra/helm/argocd/values.yaml |
| Argo Workflows | argo | build | infra/helm/argo-workflows/values.yaml |
| cert-manager 1.14.5 | jetstack | workload | infra/helm/cert-manager/values.yaml |
| Traefik 28.3.0 | traefik | workload | infra/helm/traefik/values.yaml |
| kube-prometheus-stack 25.27.0 | prometheus-community | mgmt | infra/helm/monitoring/values.yaml |
| OpenTelemetry Collector | open-telemetry | mgmt | infra/helm/otel/values.yaml |
| Loki 6.24.0 | grafana | mgmt | infra/helm/loki/values.yaml |
| Promtail 6.16.6 | grafana | build | infra/helm/promtail/values.yaml |
| External Secrets 0.14.3 | external-secrets | mgmt | infra/helm/external-secrets/values.yaml |
| Network Policies | local | multiple | infra/helm/network-policies/ |
