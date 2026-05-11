# Module 03 — Operators (`operators/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 88% (Claude-only, alguma incerteza sobre Phase 6 SOTA Dogfood Gate items)
**Maturity:** **3.3 / 4.0**

## TL;DR

4 binaries split LIVE com 8 controllers ativos, cell-aware reconciliation wired, 228 Ginkgo blocks. Bloqueia 4.0 por **AppSecret CRD órfã** (definida sem controller, HIGH), **ReconciliationReport como dead code declarado**, e idempotência TTL com `time.Now()` chamado duplo em `preview_controller.go:164`.

---

## 1. Inventário Real

| Métrica | Valor |
|---|---|
| Arquivos `.go` totais | 129 (75 prod + 54 test) |
| LOC total | 29,744 |
| LOC `internal/` produção | 10,790 |
| Binaries split (cmd/) | 4 (app, platform, tenant, preview) |
| CRDs definidos | 10 (8 wired + 2 unwired) |
| Controllers ativos | 8 |
| Ginkgo test blocks | 228 |
| Test files | 54 |

**LOC por subpacote:** tenant (3,651), platform (2,100), preview (1,691), app (1,355), harbor (629), argocd (532), gitea (522), conditions (486), shared (344), constants (218), metrics (198), httpclient (157), tracing (96).

**Top deps:** `sigs.k8s.io/controller-runtime v0.23.3`, `k8s.io/api v0.35.2`, `k8s.io/client-go v0.35.2`, `go.opentelemetry.io/otel v1.43.0`, `github.com/onsi/ginkgo/v2 v2.28.1`, `prometheus/client_golang v1.23.2`, `minio-go/v7`, `madmin-go/v3`.

---

## 2. Arquitetura Observada

**4 binaries split** (cada um com Manager + leader election dedicado):

| Binary | Controllers | Leader Election ID |
|---|---|---|
| `cmd/app-operator/main.go` | ApplicationController, ExposureController | `loopzero-app-operator.loopzero.io` |
| `cmd/platform-operator/main.go` | ClusterCredentialController, GitRepositoryBindingController, PlatformBootstrapController | `loopzero-platform-operator.loopzero.io` |
| `cmd/tenant-operator/main.go` | TenantController, EnvironmentController | `loopzero-tenant-operator.loopzero.io` |
| `cmd/preview-operator/main.go` | PreviewController | `loopzero-preview-operator.loopzero.io` |

**ApplicationReconciler** trace (`internal/app/application_controller.go:67-151`):
1. Span init (`shared.StartReconcileSpan`, linha 68)
2. Fetch CR + DeletionTimestamp check (linhas 83, 100)
3. Finalizer add (linha 104-112)
4. `reconcileHostname` (linha 114) → `naming.Hostname()`
5. `conditions.SetCondition()` ServiceResolved=True (linha 120-123)
6. `reconcileExposedRoute` (linha 125)
7. `reconcileWorkloadStatus` — **fast-path via CellAwareRuntimeClientFactory** (linha 135): `factory.For(ctx, cellID)` retorna runtime client específico
8. Status patch com kstatus summary + MergeFrom (linha 141-151, T5.1 fix)

**Cell-aware pattern** (`internal/shared/runtime_client_factory.go:91`):
- `NewCellAwareRuntimeClientFactory(mgmt, runtime, ns)` retorna factory
- `For(ctx, cellID)` resolve ClusterCredential CR via mgmt → retorna client específico
- Fallback graceful para static runtime client em erro
- Wired em app-operator (cmd/.../main.go:138) e tenant-operator EnvironmentController (linha 283-284, plan fix-all-platform-issues T1.1)

**State management:** 3 patterns observados (ADR-0025 D3 em padronização):
- **State Machine + Phase:** Tenant via `statemachine.NewTenantMachine()` + `TryTransition()`
- **Condition-only:** Application/ExposedRoute via `conditions.SetReadinessSummaryViaPkg()`
- **Direct mutation (ANTI-PATTERN):** `application_reaper.go:89` bypass do helper

**envtest centralizada** em `operatortest/`:
- `EnvtestHandle` (envtest_suite.go:42), `StartEnvtest()` (setup.go:164)
- Fakes: FakeGitProvisioner, FakeArgoCDProvisioner, FakeMinIOProvisioner
- `FakeRuntimeClientFactory` (envtest_suite.go:75) registra cada `For(cellID)` call para asserções T1.2
- Default timeout 30s (envtest_suite.go:379, subido de 15s contra CI jitter)

---

## 3. Contratos Públicos

**10 CRDs (em `operators/api/v1/`):**

| CRD | Phase enum | Status | Finalizer |
|---|---|---|---|
| LoopZeroApplication | Progressing\|Serving\|Reachable\|Degraded\|Down\|Cleaned | wired | loopzero.io/application |
| Tenant | Provisioning\|Active\|Suspended\|PendingDeletion\|Deleting | wired | loopzero.io/tenant |
| Environment | Pending\|Active\|Deleting\|Failed | wired | loopzero.io/environment |
| PreviewEnvironment | Creating\|Active\|Expiring\|Cleaning\|Failed | wired | loopzero.io/preview-cleanup |
| ExposedRoute | Pending\|CertIssued\|Reachable\|Failed | wired | loopzero.io/exposedroute |
| ClusterCredential | (phase string) | wired | loopzero.io/clustercredential |
| PlatformBootstrap | Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete\|Failed | wired | loopzero.io/platformbootstrap |
| GitRepositoryBinding | (phase string) | wired | loopzero.io/gitrepositorybinding |
| **AppSecret** | spec/status definidos | **UNWIRED — sem controller** | — |
| **ReconciliationReport** | spec/status definidos | **DEAD CODE** (comentário linha 33: "no controller implements") | — |

**Env vars críticas:**
- tenant-operator: GITEA_TOKEN, BUILD_KUBECONFIG, RUNTIME_KUBECONFIG (required)
- app-operator: BASE_DOMAIN (required), RUNTIME_KUBECONFIG (optional)
- platform-operator: ARGOCD_NAMESPACE (optional)
- preview-operator: RUNTIME_KUBECONFIG, BASE_DOMAIN (required)

**Health probes:** `:8081/healthz` + `/readyz` via `healthz.Ping` em todos os 4 binaries.

---

## 4. Cobertura de Testes

- **54 `_test.go` files** com **228 Ginkgo blocks**
- Por subpacote: tenant (166 blocks/13 files), platform (25/4), app (24/4), preview (13/4)
- **Todos 8 controllers** têm envtest specs via operatortest harness
- Fakes comprehensivos para Gitea, ArgoCD, MinIO, Harbor
- envtest cluster shared para mgmt + build + runtime clients
- **0 Pending/PIt/XIt** explícitos encontrados

**Gap T1.2 (resolvido):** Pre-T1.2, todos os tests passavam `factory=nil`, dando zero coverage de cell routing. Resolvido com `EnableCellAwareFactory=true` em EnvtestConfig + FakeRuntimeClientFactory tracking via `h.CellAwareFactory.Calls()`.

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação |
|---|---|---|
| **AppSecret CRD órfã** (CRD definida, sem controller) | **HIGH** | `api/v1/appsecret_types.go` |
| **ReconciliationReport dead code** (comentário explícito) | INFORMATIONAL | `api/v1/reconciliationreport_types.go:33` |
| Application Reaper bypass do helper de conditions | MÉDIA | `internal/app/application_reaper.go:89` |
| Harbor provisioning condition omitida de required set | MÉDIA | `internal/tenant/tenant_controller.go:429` |
| Preview TTL com `time.Now()` chamado duplo | MÉDIA | `internal/preview/preview_controller.go:164` |
| Harbor token TTL idempotência (token rotates desnecessariamente) | BAIXA | `internal/harbor/client.go:125` |
| MinIO token TTL idempotência | BAIXA | `internal/tenant/tenant_build.go:299` |
| TODO/FIXME/STUB count | **3 markers explícitos** + 6 `context.TODO()` em testes (ok) | — |

**Direct status mutations:** 0 ocorrências de `.Status.State =`. ✅
**Leader election:** todos 4 binaries com `--leader-elect` default `true`. ✅
**Idempotência reconcile:** 3 instâncias time-based, 1 com risco real (preview_controller.go:164).

---

## 6. Avaliação de Maturidade

**Score: 3.3 / 4.0** (confidence 88%)

### Evidências

1. **T1 (commit+test)** — `cmd/{app,platform,tenant,preview}-operator/main.go` (4 binaries com leader election), `operatortest/envtest_suite.go:75` (FakeRuntimeClientFactory T1.2), 228 Ginkgo blocks em 54 test files.
2. **T2 (LIVE log)** — Operators SOTA Phase 0-5 + Phase 6 parcial (2026-05-04), 8/8 pods 4 split operators boot LIVE simultaneo, cell-2 ativada (2026-05-02) com cell-aware factory wired.
3. **T3 (CI run)** — `operator-split-soak`, `multi-cell` workflows em `.github/workflows/`.

### Raciocínio

**Sustenta 3.0+:**
- 4 binaries split funcionais LIVE (ADR-0023 Stage A validated)
- 10 CRDs com conditions/observedGeneration discipline
- envtest coverage abrangente (228 blocks)
- Cell-aware factory wired em 3 controllers (app, environment, preview)
- Leader election em todos os 4 binaries
- 0 direct status mutations no path principal

**Bloqueia 4.0:**
- **AppSecret CRD órfã** (gap HIGH conhecido) — CRD registrada em scheme sem reconcile loop
- **ReconciliationReport dead code declarado** — placeholder não-funcional
- **Application Reaper bypass do helper** (`application_reaper.go:89`)
- **Preview TTL flakiness** (`time.Now()` duplo, linha 164)
- Harbor provisioning condition gap (`tenant_controller.go:429`)
- Phase 6 Dogfood Gate pendente (4h soak + chaos + SLO P95<705ms requerem sessão dedicada)

### Caminho para 3.6-4.0

1. **Implementar AppSecret controller** ou remover CRD do scheme (decisão T2 explícita)
2. **Decidir ReconciliationReport**: implementar (ADR-0031 drift detection) ou deletar
3. Migrar `application_reaper.go:89` para `conditions.SetCondition()`
4. Fix `preview_controller.go:164`: reusar variável `now` no `After()` check
5. Adicionar Harbor à `requiredProvisioningConditions()`
6. Executar Dogfood Gate (4h soak + Chaos Mesh + SLO P95<705ms)

---

**Próximas auditorias relacionadas:** pkg/ State Machines (M11), Multi-Cell (M09), Build Pipeline (M07).
