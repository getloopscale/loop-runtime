# Multi-Cluster Operator Roadmap

> Roadmap executável para o TenantController multi-cluster.
>
> **Data:** 2026-03-22
> **Spec:** §6 (Architecture), §7.3 (Operators), §14 (Multi-tenancy), §17 (Security)
> **Princípio:** Convergência primeiro — provar que o controller cria recursos nos clusters corretos antes de qualquer polish.

---

## Índice

- [Contexto e Problema](#contexto-e-problema)
- [Arquitetura Alvo](#arquitetura-alvo)
- [Mapeamento: Método → Client → Cluster](#mapeamento-método--client--cluster)
- [Fase 1 — Struct e Clients (COMPLETA)](#fase-1--struct-e-clients-completa)
- [Fase 2 — Wiring em Produção (COMPLETA)](#fase-2--wiring-em-produção-completa)
- [Fase 3 — Remediação de Blockers (PENDENTE)](#fase-3--remediação-de-blockers-pendente)
- [Grafo de Dependências](#grafo-de-dependências)
- [Riscos e Mitigações](#riscos-e-mitigações)
- [Critérios de Aceite Globais](#critérios-de-aceite-globais)

---

## Contexto e Problema

O LoopZero opera em topologia de 3 clusters (spec §6, §19):

- **mgmt** — control plane: operators, PostgreSQL, Gitea, ArgoCD, MinIO
- **build** — build plane: Argo Workflows + Kaniko
- **runtime** — workload plane: apps dos tenants, Traefik

O TenantController originalmente usava um único `client.Client` (in-cluster/mgmt), criando todos os recursos no cluster errado. Namespaces de build ficavam no mgmt, namespaces de runtime ficavam no mgmt — tudo no lugar errado.

**Solução:** Injetar clients separados para build e runtime clusters, mantendo `r.Client` para o mgmt (onde o controller roda e observa CRDs).

---

## Arquitetura Alvo

```
MGMT CLUSTER (Control Plane)
  TenantController
    r.Client        ──▶ mgmt    (Tenant CRDs, status, Gitea API, ArgoCD API)
    r.BuildClient   ──▶ build   (build-{tenant} ns, quota, networkpolicy, minio creds)
    r.RuntimeClient ──▶ runtime ({tenant}-{env} ns, quota, limitrange, RBAC, networkpolicy, priorityclass)

BUILD CLUSTER (Build Plane)
  Argo Workflows
  build-{tenant}/  ← criado pelo TenantController via BuildClient

RUNTIME CLUSTER (Workload Plane)
  Traefik
  {tenant}-{env}/  ← criado pelo TenantController via RuntimeClient
```

---

## Mapeamento: Método → Client → Cluster

| Método | Client | Cluster | Recursos criados |
|--------|--------|---------|-----------------|
| `Reconcile` (read/status) | `r.Client` | mgmt | Tenant CRD read, status update |
| `reconcileNamespaces` | `r.RuntimeClient` | runtime | Namespace per env + ResourceQuota + LimitRange |
| `reconcileRBAC` | `r.RuntimeClient` | runtime | ServiceAccount + Role + RoleBinding per env ns |
| `reconcileNetworkPolicies` | `r.RuntimeClient` | runtime | deny-all + allow-ingress + allow-egress |
| `reconcilePriorityClass` | `r.RuntimeClient` | runtime | PriorityClass per tenant |
| `reconcileBuildNamespace` | `r.BuildClient` | build | build-{tenant} ns + ResourceQuota |
| `reconcileBuildNetworkPolicy` | `r.BuildClient` | build | NetworkPolicy (egress restrict) |
| `reconcileBuildMinIOCredentials` | `r.BuildClient` | build | Secret with per-tenant MinIO creds |
| `reconcileGitRepo` | `r.Client` | mgmt | Gitea API call (no K8s resources) |
| `reconcileArgoCDApp` | `r.Client` | mgmt | ArgoCD API call (no K8s resources) |
| `reconcileDelete` | ALL | all 3 | List+delete namespaces on build AND runtime |

---

## Fase 1 — Struct e Clients (COMPLETA)

> **Status:** ✅ Implementada no commit `6243cd2`
> **Escopo:** Mudar o TenantReconciler para aceitar 3 clients e usar o correto em cada método.

### Task 1.1: TenantReconciler struct multi-cluster ✅

**Arquivo:** `operators/internal/controller/tenant_controller.go:35-45`

```go
type TenantReconciler struct {
    client.Client                           // mgmt — CRDs, status
    BuildClient   client.Client             // build — build-{tenant} namespaces
    RuntimeClient client.Client             // runtime — {tenant}-{env} namespaces
    GitProvisioner     gitea.GitRepoProvisioner
    ArgoCDProvisioner  argocd.Provisioner
    MinIOProvisioner   miniop.CredentialProvisioner
    GiteaURL           string
    WorkloadClusterURL string
    MinIOEndpoint      string
}
```

**Evidência:** Struct compilado, campos documentados com comments multi-cluster (linhas 30-34).

### Task 1.2: Substituição de clients em todos os reconcilers ✅

**Padrão aplicado:**

| Arquivo | Antes | Depois |
|---------|-------|--------|
| `tenant_namespaces.go` | `r.Client` | `r.RuntimeClient` (3 usages) |
| `tenant_rbac.go` | `r.Client` | `r.RuntimeClient` (3 usages) |
| `tenant_network.go` | `r.Client` | `r.RuntimeClient` (3 usages) |
| `tenant_priority.go` | `r.Client` | `r.RuntimeClient` (1 usage) |
| `tenant_build.go` | `r.Client` | `r.BuildClient` (4 usages) |
| `tenant_controller.go` (delete) | `r.Client` | `r.BuildClient` + `r.RuntimeClient` (8 usages) |

**Evidência:** 22 substituições totais confirmadas via grep.

### Task 1.3: SetupTenantController aceita build + runtime clients ✅

**Arquivo:** `operators/internal/controller/tenant_controller.go:345`

```go
func SetupTenantController(mgr ctrl.Manager,
    gitProvisioner gitea.GitRepoProvisioner,
    argoCDProvisioner argocd.Provisioner,
    buildClient client.Client,
    runtimeClient client.Client,
    cfg TenantControllerConfig) error
```

**Evidência:** Assinatura expandida, clients propagados para `TenantReconciler` (linhas 357-365).

---

## Fase 2 — Wiring em Produção (COMPLETA)

> **Status:** ✅ Implementada no commit `6243cd2`
> **Escopo:** Criar clients reais no main.go + montar kubeconfigs no pod via Pulumi.

### Task 2.1: Criar clients no main.go ✅

**Arquivo:** `operators/cmd/main.go:82-116`

- `BUILD_KUBECONFIG` → `clientcmd.BuildConfigFromFlags` → `client.New` → `buildClient`
- `RUNTIME_KUBECONFIG` → `clientcmd.BuildConfigFromFlags` → `client.New` → `runtimeClient`
- Ambos required (exit 1 se ausentes)
- Log de confirmação: `"build cluster client created"`, `"runtime cluster client created"`

**Evidência:** main.go compila e passa ambos clients para `SetupTenantController` (linha 118).

### Task 2.2: Montar kubeconfigs no pod via Pulumi ✅

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts`

- Secret `tenant-controller-kubeconfig` com `build-kubeconfig` e `runtime-kubeconfig` (linhas 841-883)
- Volume montado no pod spec
- Env vars: `BUILD_KUBECONFIG=/etc/loopzero/build-kubeconfig`, `RUNTIME_KUBECONFIG=/etc/loopzero/runtime-kubeconfig`

**Evidência:** Pod inicia, conecta nos 3 clusters, logs sem erros de RBAC/connection.

---

## Fase 3 — Remediação de Blockers (COMPLETA)

> **Status:** ✅ 3 blockers corrigidos em 2026-03-22
> **Escopo:** Corrigir gaps que impedem compilação de testes e wiring completo do MinIO.

### BLOCKER-1: suite_test.go não compila — assinatura desatualizada

**Severidade:** 🔴 Critical — nenhum teste do operator roda

**Problema:** `operators/internal/controller/suite_test.go:96` chama `SetupTenantController` com 4 argumentos:

```go
// ATUAL (quebrado):
err = controller.SetupTenantController(mgr, fakeGitProvider, fakeArgoCDProvider,
    controller.TenantControllerConfig{})

// NECESSÁRIO (6 argumentos):
err = controller.SetupTenantController(mgr, fakeGitProvider, fakeArgoCDProvider,
    buildClient, runtimeClient, controller.TenantControllerConfig{})
```

**Erro de compilação:**
```
suite_test.go:96:83: not enough arguments in call to controller.SetupTenantController
    have (manager.Manager, *fakeGiteaProvisioner, *fakeArgoCDProvisioner, TenantControllerConfig)
    want (controllerruntime.Manager, gitea.GitRepoProvisioner, argocd.Provisioner,
          client.Client, client.Client, TenantControllerConfig)
```

**Task 3.1: Atualizar suite_test.go para multi-cluster** ✅

**Arquivo:** `operators/internal/controller/suite_test.go`

**Implementação:**
- No envtest, usar o mesmo `cfg` para criar `buildClient` e `runtimeClient` (em testes, os 3 "clusters" apontam para o mesmo envtest API server — separação lógica, não física)
- Passar ambos clients para `SetupTenantController`

```go
// Criar clients para build e runtime (apontam para o mesmo envtest)
buildClient, err := client.New(cfg, client.Options{Scheme: scheme})
Expect(err).NotTo(HaveOccurred())

runtimeClient, err := client.New(cfg, client.Options{Scheme: scheme})
Expect(err).NotTo(HaveOccurred())

err = controller.SetupTenantController(mgr, fakeGitProvider, fakeArgoCDProvider,
    buildClient, runtimeClient, controller.TenantControllerConfig{})
```

**Critério de aceite:**
- `cd operators && go build ./...` compila sem erros
- `cd operators && go test ./internal/controller/... -count=1` roda (pode falhar por falta de envtest binaries, mas não por compilação)

**Nota arquitetural:** Em envtest todos os clients apontam para o mesmo API server. Isso é suficiente para validar que o controller usa o client correto (podemos verificar via labels nos namespaces criados). Para validação real de separação, os testes E2E contra 3 k3d clusters são necessários (Sprint 8).

---

### BLOCKER-2: MinIOProvisioner nunca instanciado

**Severidade:** 🟡 Medium — credenciais compartilhadas (segurança, não funcionalidade)

**Problema:** O código de provisioning per-tenant de MinIO existe completo em:
- `operators/internal/minio/provisioner.go` — `AdminProvisioner` com `EnsureTenantCredentials`
- `operators/internal/controller/tenant_build.go:81-85` — nil check + chamada
- `operators/internal/controller/tenant_build.go:104-133` — `reconcileBuildMinIOCredentials`

Mas o `MinIOProvisioner` nunca é criado no `main.go` nem passado para o controller:
- `main.go` não importa o package `minio`
- `SetupTenantController` não aceita `MinIOProvisioner` como parâmetro
- O reconciler recebe `MinIOProvisioner: nil` → o nil check em `tenant_build.go:81` sempre pula

**Impacto:** Builds funcionam (usam credenciais globais do MinIO via Argo Workflow templates), mas não têm isolamento multi-tenant. Um build comprometido pode ler source bundles e envenenar caches de outros tenants (spec §17.2, §17.4).

**Task 3.2: Wire MinIOProvisioner no main.go e SetupTenantController** ✅

**Arquivos:**
- `operators/cmd/main.go` — criar `AdminProvisioner` a partir de env vars
- `operators/internal/controller/tenant_controller.go` — expandir `SetupTenantController` para aceitar `MinIOProvisioner` + `MinIOEndpoint`

**Implementação main.go:**

```go
import miniop "github.com/loopzero/loopzero/operators/internal/minio"

// MinIO per-tenant credential provisioner (optional — nil disables per-tenant isolation)
var minIOProvisioner miniop.CredentialProvisioner
minIOEndpoint := os.Getenv("MINIO_ENDPOINT")
minIOAccessKey := os.Getenv("MINIO_ACCESS_KEY")
minIOSecretKey := os.Getenv("MINIO_SECRET_KEY")
if minIOEndpoint != "" && minIOAccessKey != "" && minIOSecretKey != "" {
    minIOProvisioner = miniop.NewAdminProvisioner(miniop.AdminConfig{
        Endpoint:  minIOEndpoint,
        AccessKey: minIOAccessKey,
        SecretKey: minIOSecretKey,
    })
    setupLog.Info("minio per-tenant provisioner enabled", "endpoint", minIOEndpoint)
}
```

**Implementação SetupTenantController:**

```go
func SetupTenantController(mgr ctrl.Manager,
    gitProvisioner gitea.GitRepoProvisioner,
    argoCDProvisioner argocd.Provisioner,
    buildClient client.Client,
    runtimeClient client.Client,
    cfg TenantControllerConfig) error {
    // ... existing code ...
    return ctrl.NewControllerManagedBy(mgr).
        For(&loopzerov1.Tenant{}).
        Named("tenant").
        Complete(&TenantReconciler{
            // ... existing fields ...
            MinIOProvisioner:   cfg.MinIOProvisioner,  // NEW
            MinIOEndpoint:      cfg.MinIOEndpoint,      // NEW
        })
}
```

**Alteração em TenantControllerConfig:**

```go
type TenantControllerConfig struct {
    GiteaURL           string
    WorkloadClusterURL string
    MinIOEndpoint      string
    MinIOProvisioner   miniop.CredentialProvisioner // NEW
}
```

**Critério de aceite:**
- Com env vars MinIO: provisioner ativado, credenciais per-tenant criadas no Secret
- Sem env vars MinIO: provisioner nil, comportamento atual mantido (graceful degradation)
- `suite_test.go` continua funcionando (MinIOProvisioner = nil no test config)

---

### BLOCKER-3: MinIOEndpoint não propagado para reconciler

**Severidade:** 🟡 Medium — vinculado ao BLOCKER-2

**Problema:** `SetupTenantController` recebe `cfg.MinIOEndpoint` mas nunca o propaga:

```go
// tenant_controller.go:354-365 — MinIOEndpoint AUSENTE:
Complete(&TenantReconciler{
    Client:             mgr.GetClient(),
    BuildClient:        buildClient,
    RuntimeClient:      runtimeClient,
    GitProvisioner:     gitProvisioner,
    ArgoCDProvisioner:  argoCDProvisioner,
    GiteaURL:           giteaURL,
    WorkloadClusterURL: workloadClusterURL,
    // MinIOEndpoint: ???    ← MISSING
    // MinIOProvisioner: ??? ← MISSING
})
```

**Resultado:** Mesmo que o MinIOProvisioner fosse instanciado, o `r.MinIOEndpoint` no Secret (tenant_build.go:128) seria string vazia.

**Task 3.3: Propagar MinIOEndpoint e MinIOProvisioner no SetupTenantController** ✅

**Arquivo:** `operators/internal/controller/tenant_controller.go:354-365`

**Implementação:** Adicionar os campos faltantes no `Complete()`:

```go
Complete(&TenantReconciler{
    Client:             mgr.GetClient(),
    BuildClient:        buildClient,
    RuntimeClient:      runtimeClient,
    GitProvisioner:     gitProvisioner,
    ArgoCDProvisioner:  argoCDProvisioner,
    MinIOProvisioner:   cfg.MinIOProvisioner,   // NEW
    GiteaURL:           giteaURL,
    WorkloadClusterURL: workloadClusterURL,
    MinIOEndpoint:      cfg.MinIOEndpoint,       // NEW
})
```

**Critério de aceite:**
- `r.MinIOEndpoint` preenchido quando `cfg.MinIOEndpoint` é fornecido
- Secret criado pelo `reconcileBuildMinIOCredentials` contém endpoint correto
- Testes compilam e passam

---

### Task 3.4: Atualizar Pulumi com env vars MinIO para o controller pod ✅

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts`

**Implementação:** Na função `installTenantController`, adicionar env vars:

```typescript
{ name: "MINIO_ENDPOINT", value: "http://minio.loopzero-system.svc.cluster.local:9000" },
{ name: "MINIO_ACCESS_KEY", valueFrom: { secretKeyRef: { name: "minio-credentials", key: "accesskey" } } },
{ name: "MINIO_SECRET_KEY", valueFrom: { secretKeyRef: { name: "minio-credentials", key: "secretkey" } } },
```

**Critério de aceite:**
- Pod do controller inicia com as 3 env vars
- Log mostra `"minio per-tenant provisioner enabled"`

---

## Grafo de Dependências

```
Fase 1 (COMPLETA)
  Task 1.1: struct ─────┐
  Task 1.2: clients ────┤
  Task 1.3: setup fn ───┘
       │
Fase 2 (COMPLETA)
  Task 2.1: main.go clients ──┐
  Task 2.2: pulumi kubeconfig ┘
       │
Fase 3 (PENDENTE)
  Task 3.1: fix suite_test.go ◀── BLOCKER-1 (Critical)
       │
  Task 3.2: wire MinIOProvisioner ◀── BLOCKER-2 (Medium)
       │
  Task 3.3: propagate MinIOEndpoint ◀── BLOCKER-3 (depende de 3.2)
       │
  Task 3.4: pulumi MinIO env vars ◀── depende de 3.2 + 3.3
```

**Ordem de execução recomendada:**
1. **Task 3.1** (fix compilação — desbloqueia todos os testes)
2. **Task 3.2 + 3.3** (podem ser feitas juntas — mesmo commit)
3. **Task 3.4** (deploy com MinIO wiring completo)

---

## Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|:---:|:---:|----------|
| RBAC insuficiente nos clusters destino | Baixa | Alto | Kubeconfigs DOKS têm admin — suficiente para dev |
| Controller perde conexão com cluster destino | Média | Médio | Health checks + restart automático pelo K8s |
| CRDs não existem nos clusters destino | Baixa | Baixo | CRDs só precisam existir no mgmt (onde o controller os observa) |
| MinIO Admin API não acessível do pod | Média | Médio | NetworkPolicy do namespace do controller precisa permitir egress para MinIO |
| envtest não simula separação real de clusters | N/A | Baixo | Limitação aceita — validação real em E2E com 3 k3d clusters (Sprint 8) |

---

## Critérios de Aceite Globais

### Convergência (prova de que funciona)

```
[ ] `cd operators && go build ./...` compila sem erros
[ ] `cd operators && go test ./internal/controller/... -count=1` passa
[ ] Controller pod Running sem erros de RBAC ou connection nos logs
[ ] Criar Tenant CRD → build-{tenant} namespace aparece no build cluster
[ ] Criar Tenant CRD → {tenant}-{env} namespaces aparecem no runtime cluster
[ ] Deletar Tenant CRD → namespaces removidos de AMBOS os clusters
```

### Segurança (MinIO isolation — após Task 3.2-3.4)

```
[ ] Secret `loopzero-minio-credentials` no build namespace tem credenciais per-tenant
[ ] Credenciais scoped para `loopzero-builds/{tenantID}/*` (não acesso global)
[ ] Tenant A não consegue acessar source bundles do Tenant B
```

### Não-regressão

```
[ ] Nenhum teste existente quebrado
[ ] Nenhum internal term em output de CLI
[ ] Controller idempotente (reconcile repetido = noop)
```
