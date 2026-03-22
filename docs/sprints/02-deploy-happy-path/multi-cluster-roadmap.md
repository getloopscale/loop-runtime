# Multi-Cluster Operator Roadmap

> Corrigir o TenantController para operar corretamente na topologia de 3 clusters.
>
> **Data:** 2026-03-22
> **Problema:** O controller usa um unico `client.Client` (in-cluster/mgmt). Cria namespaces no cluster errado.
> **Solucao:** Injetar clients separados para build e runtime clusters.

---

## Arquitetura Alvo

```
MGMT CLUSTER (Control Plane)
  TenantController
    r.Client        ──▶ mgmt    (Tenant CRDs, status, Gitea, ArgoCD)
    r.BuildClient   ──▶ build   (build-{tenant} ns, quota, networkpolicy, minio creds)
    r.RuntimeClient ──▶ runtime ({tenant}-{env} ns, quota, limitrange, RBAC, networkpolicy, priorityclass)

BUILD CLUSTER
  Argo Workflows
  build-{tenant}/  ← criado pelo TenantController via BuildClient

RUNTIME CLUSTER
  Traefik
  {tenant}-{env}/  ← criado pelo TenantController via RuntimeClient
```

## Mapeamento: metodo → client → cluster

| Metodo | Client | Cluster | Recursos criados |
|--------|--------|---------|-----------------|
| `Reconcile` (read/status) | `r.Client` | mgmt | Tenant CRD read, status update |
| `reconcileNamespaces` | `r.RuntimeClient` | runtime | Namespace per env |
| `reconcileResourceQuota` | `r.RuntimeClient` | runtime | ResourceQuota per env ns |
| `reconcileLimitRange` | `r.RuntimeClient` | runtime | LimitRange per env ns |
| `reconcileRBAC` | `r.RuntimeClient` | runtime | SA + Role + RoleBinding per env ns |
| `reconcileNetworkPolicies` | `r.RuntimeClient` | runtime | deny-all + allow-ingress + allow-egress |
| `reconcilePriorityClass` | `r.RuntimeClient` | runtime | PriorityClass per tenant |
| `reconcileBuildNamespace` | `r.BuildClient` | build | build-{tenant} ns + quota |
| `reconcileBuildMinIOCredentials` | `r.BuildClient` | build | MinIO Secret in build ns |
| `reconcileBuildNetworkPolicy` | `r.BuildClient` | build | NetworkPolicy in build ns |
| `reconcileGitRepo` | `r.Client` | mgmt | Gitea API call (no K8s) |
| `reconcileArgoCDApp` | `r.Client` | mgmt | ArgoCD API call (no K8s) |
| `reconcileDelete` | ALL | all 3 | List+delete namespaces on build AND runtime |

## Tasks (em ordem de execucao)

### Task 1: TenantReconciler struct + config

**Arquivo:** `operators/internal/controller/tenant_controller.go`

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

type TenantControllerConfig struct {
    GiteaURL             string
    WorkloadClusterURL   string
    MinIOEndpoint        string
    BuildKubeconfig      string   // path to build cluster kubeconfig
    RuntimeKubeconfig    string   // path to runtime cluster kubeconfig
}
```

**Criterio de aceite:** Compila sem erros.

### Task 2: Usar client correto em cada metodo

**Arquivos:**
- `tenant_namespaces.go` — substituir `r.Client` por `r.RuntimeClient` em todos os CreateOrUpdate/Get
- `tenant_network.go` — idem
- `tenant_rbac.go` — idem
- `tenant_priority.go` — idem
- `tenant_build.go` — substituir `r.Client` por `r.BuildClient`
- `tenant_controller.go` (`reconcileDelete`) — listar/deletar namespaces em AMBOS os clusters

**Padrao de substituicao:**

Para metodos do runtime (tenant_namespaces.go, tenant_network.go, tenant_rbac.go, tenant_priority.go):
```go
// Antes:
controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error { ... })

// Depois:
controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, ns, func() error { ... })
```

Para metodos do build (tenant_build.go):
```go
// Antes:
controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error { ... })

// Depois:
controllerutil.CreateOrUpdate(ctx, r.BuildClient, ns, func() error { ... })
```

Para reconcileDelete:
```go
// Listar em AMBOS os clusters
var buildNsList corev1.NamespaceList
r.BuildClient.List(ctx, &buildNsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name})

var runtimeNsList corev1.NamespaceList
r.RuntimeClient.List(ctx, &runtimeNsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name})

// Deletar em cada cluster usando o client correto
```

**Criterio de aceite:** Compila sem erros. Cada operacao usa o client do cluster correto.

### Task 3: Criar clients no main.go

**Arquivo:** `operators/cmd/main.go`

```go
// Read kubeconfigs
buildKubeconfig := os.Getenv("BUILD_KUBECONFIG")
runtimeKubeconfig := os.Getenv("RUNTIME_KUBECONFIG")

// Create rest.Config for each cluster
buildConfig, err := clientcmd.BuildConfigFromFlags("", buildKubeconfig)
if err != nil {
    setupLog.Error(err, "unable to create build cluster config")
    os.Exit(1)
}

runtimeConfig, err := clientcmd.BuildConfigFromFlags("", runtimeKubeconfig)
if err != nil {
    setupLog.Error(err, "unable to create runtime cluster config")
    os.Exit(1)
}

// Create clients
buildClient, err := client.New(buildConfig, client.Options{Scheme: scheme})
runtimeClient, err := client.New(runtimeConfig, client.Options{Scheme: scheme})
```

**Criterio de aceite:** Controller inicia e conecta nos 3 clusters.

### Task 4: Montar kubeconfigs no pod (Pulumi)

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts`

Na funcao `installTenantController`:
- Criar Secret com build + runtime kubeconfigs
- Montar volume no pod spec
- Env vars: `BUILD_KUBECONFIG=/etc/loopzero/build-kubeconfig`, `RUNTIME_KUBECONFIG=/etc/loopzero/runtime-kubeconfig`

**Criterio de aceite:** Pod monta os kubeconfigs. Env vars apontam para os arquivos.

### Task 5: Rebuild image + deploy

1. Commit codigo Go
2. Push para develop → build-images pipeline builda nova image
3. Push infra → infra-deploy pipeline atualiza pod spec
4. Validar controller Running e sem erros

**Criterio de aceite:**
- `kubectl -n platform-system logs -l app=tenant-controller` sem erros de RBAC ou connection
- `kubectl --kubeconfig=/tmp/loopzero-build.kubeconfig get ns` mostra namespaces criados pelo controller (apos criar Tenant CRD)
- `kubectl --kubeconfig=/tmp/loopzero-runtime.kubeconfig get ns` mostra namespaces de ambiente

---

## Dependencias

```
Task 1 ──┬──▶ Task 2
         └──▶ Task 3 ──▶ Task 5
Task 4 ─────────────────▶ Task 5
```

## Riscos

| Risco | Mitigacao |
|-------|----------|
| RBAC insuficiente nos clusters destino | Kubeconfigs do DOKS tem admin — suficiente para dev |
| Controller perde conexao com cluster destino | Health checks + restart automatico pelo K8s |
| CRDs nao existem nos clusters destino | CRDs so precisam existir no mgmt (onde o controller os observa) |
