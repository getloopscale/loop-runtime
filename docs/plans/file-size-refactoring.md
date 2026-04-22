# File Size Refactoring Plan

> Plano executável para reduzir todos os arquivos >500 linhas (vermelhos) para <350 linhas.
> 19 arquivos vermelhos identificados: 5 Infra, 10 API, 2 CLI, 2 Operators.
> Regra: <350 verde, 350-400 amarelo, 400-500 laranja, >500 vermelho.
>
> **Principio:** Cada split é puro refactoring — zero mudança funcional. Move funções entre arquivos, não altera lógica.

---

## Visao Geral

| Sprint | Componente | Arquivos | Meta |
|--------|-----------|----------|------|
| **R1** | Operators | 2 arquivos | Todas <350 |
| **R2** | API cmd/ | 2 arquivos | Todas <350 |
| **R3** | API services/ | 2 arquivos | Todas <350 |
| **R4** | API routes+db+server | 4 arquivos | Todas <350 |
| **R5** | API manifests+argo | 1 arquivo | Todas <350 |
| **R6** | CLI | 2 arquivos | Todas <350 |
| **R7** | Infra Pulumi | 6 arquivos | Todas <350 |

**Tier de mudanca:** T0 para todos (puro refactoring, zero mudanca funcional).

---

## Sprint R1 — Operators (735 + 593 linhas)

### Task R1.1: Split environment_controller.go (735 → 4 arquivos)

**Arquivo:** `operators/internal/controller/environment_controller.go`

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `environment_controller.go` | Reconcile, reconcileDelete, SetupEnvironmentController | ~250 |
| `environment_rbac.go` | reconcileRBAC, ensurePullSecret, kickStuckPullBackoffPods, rbacRulesForPlan | ~220 |
| `environment_network.go` | reconcileNetworkPolicies, reconcileEnvDenyAllPolicy, reconcileEnvAllowIngressPolicy, reconcileEnvAllowEgressPolicy, ingressNS | ~180 |
| `environment_quota.go` | reconcileQuota, reconcileNamespace | ~150 |

**Criterios de aceite:**
- [ ] Nenhum arquivo resultante >350 linhas
- [ ] Todos os arquivos no mesmo package `controller`
- [ ] `EnvironmentReconciler` struct permanece em `environment_controller.go`
- [ ] Zero mudanca funcional — apenas move de funcoes

**DoD:**
- [ ] `go build ./operators/...` compila
- [ ] `go test ./operators/internal/controller/... -count=1` passa (188 specs, 0 failures)
- [ ] `wc -l` de cada arquivo <350

---

### Task R1.2: Split tenant_controller.go (593 → 2 arquivos)

**Arquivo:** `operators/internal/controller/tenant_controller.go`

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `tenant_controller.go` | Reconcile, runSteps, requiredProvisioningConditions, reconcileEnvironmentCRs, SetupTenantController | ~330 |
| `tenant_deletion.go` | reconcileDelete, timedCleanupStep, getDeletionRetryCount, setDeletionRetryCount, deletionBackoff | ~260 |

**Criterios de aceite:**
- [ ] Nenhum arquivo resultante >350 linhas
- [ ] Segue padrao existente (tenant_build.go, tenant_harbor.go, etc.)

**DoD:**
- [ ] `go build ./operators/...` compila
- [ ] `go test ./operators/internal/controller/... -count=1` passa
- [ ] `wc -l` de cada arquivo <350

---

## Sprint R2 — API cmd/ (1278 + 527 linhas)

### Task R2.1: Split api/cmd/main.go (1278 → 7 arquivos)

**Arquivo:** `api/cmd/main.go`

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `main.go` | main() entry point (config, logger, tracing, calls setup funcs, runs server) | ~200 |
| `setup_database.go` | setupDatabase, pool creation, pool metrics | ~120 |
| `setup_stores.go` | all store initializations (build, release, event, secret, bootstrap, etc.) | ~150 |
| `setup_workflows.go` | Argo workflow submitter, circuit breakers, ArgoCD sync client | ~150 |
| `setup_orchestrator.go` | build orchestrator, release convergence, rollback, status, logs | ~200 |
| `setup_background.go` | leader election, heartbeat, all background loops (reconcile, reapers, etc.) | ~300 |
| `setup_github.go` | GitHub App, webhook handlers, auto-deploy | ~180 |

**Criterios de aceite:**
- [ ] Nenhum arquivo resultante >350 linhas
- [ ] Todos no package `main`
- [ ] Funcoes de setup retornam (resultado, error) — main() orquestra
- [ ] Zero mudanca funcional

**DoD:**
- [ ] `go build ./api/cmd/...` compila
- [ ] `go test ./api/...` passa
- [ ] `wc -l` de cada arquivo <350

---

### Task R2.2: Split api/cmd/controller/main.go (527 → 4 arquivos)

**Arquivo:** `api/cmd/controller/main.go`

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `main.go` | main() entry (config, logger, calls setup, runs health server) | ~150 |
| `setup_services.go` | stores, Argo, ArgoCD, GitOps, K8s managers, orchestrator | ~180 |
| `leader.go` | leader election, heartbeat, background loops | ~150 |
| `helpers.go` | cellID, podName, silenceKlog, appCRAdapter, newHealthServer | ~100 |

**Criterios de aceite:**
- [ ] Nenhum arquivo >350 linhas
- [ ] Todos no package `main`

**DoD:**
- [ ] `go build ./api/cmd/controller/...` compila
- [ ] `go test ./api/...` passa
- [ ] `wc -l` de cada arquivo <350

---

## Sprint R3 — API services/ (781 + 767 linhas)

### Task R3.1: Split build_orchestrator.go (781 → 5 arquivos)

**Arquivo:** `api/internal/services/build_orchestrator.go`

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `build_orchestrator.go` | NewBuildOrchestrator, types, interfaces | ~250 |
| `build_orchestrator_submit.go` | SubmitBuild (Phase 1+2), failQueuedBuild | ~180 |
| `build_orchestrator_cancel.go` | CancelBuild (lock, state machine, workflow cancel) | ~100 |
| `build_orchestrator_complete.go` | OnBuildCompleted (3-phase), writeStaticRoutingEntries | ~200 |
| `build_orchestrator_retry.go` | RetryReleaseActivation, FindReleaseForBuild | ~80 |

**Criterios de aceite:**
- [ ] Nenhum arquivo >350 linhas
- [ ] BuildOrchestrator struct em build_orchestrator.go
- [ ] Cada operacao do lifecycle em arquivo separado

**DoD:**
- [ ] `go build ./api/...` compila
- [ ] `go test ./api/internal/services/...` passa
- [ ] `wc -l` de cada arquivo <350

---

### Task R3.2: Split build_reconcile_loop.go (767 → 5 arquivos)

**Arquivo:** `api/internal/services/build_reconcile_loop.go`

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `build_reconcile_loop.go` | NewBuildReconcileLoop, Start, types, interfaces | ~250 |
| `build_reconcile_dispatch.go` | reconcile (cell iteration, semaphore, dedup) | ~150 |
| `build_reconcile_single.go` | reconcileBuild (state dispatch, grace period) | ~100 |
| `build_reconcile_complete.go` | onWorkflowSucceeded, completeBuild, ensureApplicationCRs | ~180 |
| `build_reconcile_retry.go` | retryReleaseActivation, failBuild | ~150 |

**Criterios de aceite:**
- [ ] Nenhum arquivo >350 linhas
- [ ] BuildReconcileLoop struct no arquivo principal

**DoD:**
- [ ] `go build ./api/...` compila
- [ ] `go test ./api/internal/services/...` passa
- [ ] `wc -l` de cada arquivo <350

---

## Sprint R4 — API routes+db+server (876 + 542 + 538 + 578 linhas)

### Task R4.1: Split routes/domains.go (876 → 5 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `domains.go` | DomainHandler struct, NewDomainHandler, WithCFZone, Get, List, types | ~250 |
| `domains_create.go` | Create (v1 TXT flow) | ~150 |
| `domains_cfzone.go` | createWithCFZone (v2 CF Zone flow) | ~150 |
| `domains_verify.go` | Verify (TXT lookup) | ~100 |
| `domains_dns.go` | Delete, ListDNSRecords, AddDNSRecords, helpers | ~200 |

**DoD:** `go build ./api/...` compila, testes passam, `wc -l` <350.

---

### Task R4.2: Split db/domain_store.go (542 → 3 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `domain_store.go` | CRUD (Create, Get, List, Update, Delete, Count) + scan helpers | ~250 |
| `domain_store_background.go` | FindAndClaim*, FindPending*, FindStale*, FindActive*, UpdateCNAMECheck | ~200 |
| `domain_store_cfzone.go` | CreateWithCFZone, UpdateCFZoneStatus, FindPendingNS, FindNSActive | ~100 |

**DoD:** `go build ./api/...` compila, testes passam, `wc -l` <350.

---

### Task R4.3: Split db/build_store.go (538 → 3 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `build_store.go` | CRUD (Create, GetByID, UpdateStatus, Set*, GetBuildApps) + scan | ~250 |
| `build_store_list.go` | ListByProject, ListByProjectAndEnv (queries com paginacao) | ~150 |
| `build_store_reconcile.go` | FindActiveBuilds, CancelActiveBuilds, HasActiveBuild | ~150 |

**DoD:** `go build ./api/...` compila, testes passam, `wc -l` <350.

---

### Task R4.4: Split server/server.go (578 → 3 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `server.go` | Config struct, Run, graceful shutdown | ~200 |
| `router.go` | NewRouter, middleware stack, route registration | ~300 |
| `validation.go` | ValidateProductionEnv, NewMetricsServer | ~80 |

**DoD:** `go build ./api/...` compila, testes passam, `wc -l` <350.

---

## Sprint R5 — API manifests (1017 linhas)

### Task R5.1: Split manifests/renderer.go (1017 → 5 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `renderer.go` | Render() entry point, types (ManifestInput, etc.) | ~200 |
| `renderer_deployment.go` | buildDeployment, buildPDB, security/probe/volume logic | ~250 |
| `renderer_ingress.go` | buildIngressResources, buildService, custom domains v1/v2 | ~280 |
| `renderer_static.go` | renderStaticFrontendApp, middleware, S3 routing | ~150 |
| `renderer_helpers.go` | appHostname, commonLabels, marshalResource, domainResourceNames, defaults | ~200 |

**DoD:** `go build ./api/...` compila, testes passam, `wc -l` <350.

---

## Sprint R6 — CLI (1889 + 647 linhas)

### Task R6.1: Split cli/src/index.ts (1889 → 8 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `index.ts` | createProgram() stub, cleanup, withTimeout, main | ~200 |
| `register/auth.ts` | login, logout, whoami command registration | ~200 |
| `register/deploy.ts` | deploy, validate, cancel, rollback registration | ~250 |
| `register/project.ts` | project-* command registration | ~200 |
| `register/team.ts` | team-* command registration | ~180 |
| `register/env.ts` | env-* command registration | ~180 |
| `register/domain.ts` | domain-* command registration | ~250 |
| `register/infra.ts` | connect, billing, audit, admin, build-*, status, logs, secret-* | ~350 |

**Criterios de aceite:**
- [ ] Cada arquivo exporta funcao `registerXCommands(program, deps)` 
- [ ] `index.ts` importa e chama cada register
- [ ] Nenhum arquivo >350 linhas
- [ ] `npm test` passa (747 tests)

**DoD:**
- [ ] `cd cli && npm run build` compila
- [ ] `cd cli && npm test` passa
- [ ] `wc -l` de cada arquivo <350

---

### Task R6.2: Split cli/src/commands/deploy.ts (647 → 4 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `deploy.ts` | DeployCommand.execute() orchestration (core flow) | ~250 |
| `deploy-validator.ts` | auth check, loopzero.yaml validation, env validation, app selection | ~120 |
| `deploy-streamer.ts` | SSE stream handling, event dispatch, activity timeout | ~150 |
| `deploy-types.ts` | DeployDeps, DeployOptions, UploadResult, SSEEvent, etc. | ~80 |

**DoD:** `cd cli && npm run build && npm test` passa, `wc -l` <350.

---

## Sprint R7 — Infra Pulumi (3770 + 1795 + 612 + 563 + 547 + 508 linhas)

### Task R7.1: Split mgmt-bootstrap.ts (3770 → 12 arquivos) — CRITICO

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `mgmt-bootstrap.ts` | bootstrapMgmtCluster() orchestrator only | ~250 |
| `mgmt-argocd.ts` | installArgoCD | ~170 |
| `mgmt-gitea.ts` | installGitea + installGiteaRedis + installNfsProvisioner | ~280 |
| `mgmt-gitea-backup.ts` | createGiteaBackupCronJob | ~140 |
| `mgmt-minio.ts` | installMinIO | ~70 |
| `mgmt-ory.ts` | installOryKratos + installOryHydra | ~300 |
| `mgmt-cloud-service.ts` | installCloudService | ~250 |
| `mgmt-api.ts` | installLoopZeroAPI (secrets, migrations, deployment) | ~350 |
| `mgmt-tenant-controller.ts` | installTenantController (CRDs, RBAC, deployment) | ~350 |
| `mgmt-controller.ts` | installLoopZeroController | ~140 |
| `mgmt-ingress.ts` | createAPIGatewayRoute, createCloudGatewayRoute, createPlatformIngressRoutes | ~250 |
| `mgmt-network.ts` | createPlatformNetworkPolicies + createScopedKubeconfig + registerRuntimeCluster | ~250 |

**Criterios de aceite:**
- [ ] Nenhum arquivo >350 linhas
- [ ] Cada funcao `install*` exportada do seu modulo
- [ ] `mgmt-bootstrap.ts` importa e orquestra
- [ ] Ordem de dependencias mantida (Pulumi `dependsOn`)

**DoD:**
- [ ] `cd infra/pulumi && npx tsc --noEmit` compila
- [ ] `cd infra/pulumi && npm test` passa
- [ ] `wc -l` de cada arquivo <350

---

### Task R7.2: Split mgmt-control-plane-services.ts (1795 → 8 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `mgmt-control-plane-services.ts` | Exports, constants, orchestrator | ~150 |
| `cp-ory-kratos.ts` | installOryKratos + Kratos UI | ~220 |
| `cp-ory-hydra.ts` | installOryHydra | ~100 |
| `cp-cloud-service.ts` | installCloudService (ConfigMap + Deployment + Dashboard) | ~200 |
| `cp-api-service.ts` | installLoopZeroAPI (Secret + RBAC + Deployment) | ~350 |
| `cp-tenant-controller.ts` | installTenantController (CRDs + RBAC + Deployment) | ~350 |
| `cp-ingress.ts` | createAPIGatewayRoute + createCloudGatewayRoute + createPlatformIngressRoutes | ~200 |
| `cp-network.ts` | createPlatformNetworkPolicies + createScopedKubeconfig + registerRuntimeCluster | ~250 |

**DoD:** `cd infra/pulumi-control-plane && npx tsc --noEmit` compila, `wc -l` <350.

---

### Task R7.3: Split mgmt-observability.ts (612 → 4 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `mgmt-observability.ts` | Exports + installPrometheus | ~280 |
| `grafana.ts` | installGrafana + provisionGrafanaDashboards | ~170 |
| `tempo.ts` | installTempo | ~70 |
| `otel-collector.ts` | installOtelCollector | ~100 |

**DoD:** `cd infra/pulumi && npx tsc --noEmit` compila, `wc -l` <350.

---

### Task R7.4: Split bootstrap-shared.ts (563 → 3 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `bootstrap-shared.ts` | Types, constants, parseKubeconfig, createPlatformNamespace | ~200 |
| `bootstrap-cert-ingress.ts` | installCertManager, installTraefik, lookupLoadBalancerEndpoint, createClusterIssuer | ~250 |
| `bootstrap-registry.ts` | createDockerRegistryPullSecret, installMetricsServer | ~100 |

**DoD:** `cd infra/pulumi && npx tsc --noEmit` compila, `wc -l` <350.

---

### Task R7.5: Split build-bootstrap.ts (547 → 4 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `build-bootstrap.ts` | bootstrapBuildCluster orchestrator | ~200 |
| `build-argo.ts` | Argo Workflows Helm + CWTs + metrics NodePort + RBAC | ~150 |
| `build-loki.ts` | Loki Helm + NodePort + Promtail | ~120 |
| `build-secrets.ts` | cosign, minio, buildkit, DOCR secrets | ~100 |

**DoD:** `cd infra/pulumi && npx tsc --noEmit` compila, `wc -l` <350.

---

### Task R7.6: Split runtime-bootstrap.ts (508 → 3 arquivos)

| Novo Arquivo | Funcoes | Linhas Est. |
|-------------|---------|-------------|
| `runtime-bootstrap.ts` | bootstrapRuntimeCluster + bootstrapRuntimeCluster2 orchestrators | ~200 |
| `runtime-eso.ts` | installESO + ESO RBAC + ClusterSecretStore | ~160 |
| `runtime-monitoring.ts` | Promtail + kube-state-metrics + NodePort | ~150 |

**DoD:** `cd infra/pulumi && npx tsc --noEmit` compila, `wc -l` <350.

---

## Ordem de Execucao

```
R1 (operators) ───┐
R2 (api cmd)  ────┤
R3 (api svc)  ────┤── Todos independentes, podem ser paralelos
R4 (api misc) ────┤
R5 (api mnfst)────┤
R6 (cli)      ────┤
R7 (infra)    ────┘
```

**Nenhuma dependencia entre sprints.** Cada sprint toca um componente diferente. Podem ser executados em qualquer ordem ou em paralelo.

---

## Gate de Conclusao

Apos todos os sprints:

```bash
# Verificacao final
go build ./operators/... ./api/... ./pkg/...
go test ./operators/... ./api/... ./pkg/...
cd cli && npm run build && npm test
cd infra/pulumi && npx tsc --noEmit

# Contagem final
find operators/ api/ pkg/ -name "*.go" -not -name "*_test.go" -not -name "zz_generated*" \
  -exec wc -l {} + | awk '$1 > 350' | sort -rn
# Esperado: 0 arquivos Go acima de 350

find cli/src -name "*.ts" -not -name "*.test.ts" -not -path "*/node_modules/*" \
  -exec wc -l {} + | awk '$1 > 350' | sort -rn
# Esperado: 0 arquivos TS acima de 350

find infra/pulumi*/src -name "*.ts" -not -name "*.d.ts" -not -path "*/node_modules/*" \
  -exec wc -l {} + | awk '$1 > 350' | sort -rn
# Esperado: 0 arquivos TS acima de 350
```

**Total: 19 arquivos vermelhos → ~80 arquivos verdes (<350 linhas cada).**

| Metrica | Antes | Depois |
|---------|-------|--------|
| Arquivos >500 (vermelho) | 19 | 0 |
| Arquivos 400-500 (laranja) | 16 | 0-2 |
| Arquivos 350-400 (amarelo) | 10 | 0-3 |
| Arquivos <350 (verde) | ~150 | ~230 |
