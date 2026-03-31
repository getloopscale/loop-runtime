# Plano: Cell Architecture — Phase 1 (Cell-Ready)

> **Objetivo:** Tornar o control plane do LoopZero capaz de gerenciar um segundo workload cluster.
> Ao final da Phase 1, adicionar uma nova Cell e uma mudanca de config no Pulumi,
> nao uma mudanca de codigo.
>
> **Escopo:** 8 tasks, mix de T1 e T2. Mudanca comportamental real: API, operators e Pulumi
> passam a resolver cell_id dinamicamente em vez de usar config hardcoded.
>
> **Classificacao:** T2 (contratos internos mudam: ArgoCD provisioner, SyncClient, K8s client,
> ClusterCredential CRD, DNS routing, Pulumi infra)
>
> **Status:** PENDING — requer Phase 0 completa + validacao em DigitalOcean
>
> **Principio:** "Adicionar Cell #2 deve ser tao simples quanto `pulumi config set cells[1]`."

---

## Contexto

Phase 0 adicionou `cell_id` ao modelo de tenant, criou interfaces `PlacementEngine` e `CellRouter`
como passthroughs, e garantiu que o sistema continua funcionando identicamente com uma Cell unica.

Phase 1 ATIVA multi-cell: o control plane passa a resolver `cell_id` dinamicamente para todas
as operacoes que interagem com workload clusters. Isso inclui:

1. Criar/resolver K8s clients por Cell
2. Provisionar ArgoCD Applications no ArgoCD correto
3. Sincronizar releases no ArgoCD correto
4. Provisionar Cells inteiras via Pulumi
5. Rotear DNS para o ingress correto por Cell
6. Colocar novos tenants na Cell com mais capacidade

### O Que Muda

```
ANTES: API/Operators usam um unico runtime kubeconfig hardcoded
DEPOIS: API/Operators resolvem cell_id → CellConfig → K8s client, ArgoCD URL, ingress IP
```

```
ANTES: config.RuntimeKubeconfig (unico)
DEPOIS: CellRouter.Resolve(cellID) → CellConfig{kubeconfig, argocdURL, ingressIP}
```

### O Que NAO Muda

- CLI identica (zero mudanca)
- API contracts HTTP identicos (nenhum campo novo na API publica)
- Deploy flow logicamente identico (bundle → build → release → sync)
- Gitea compartilhado entre Cells
- Build plane compartilhado (Argo Workflows)
- PostgreSQL compartilhado (queries filtram por cell_id quando necessario)

---

## Tasks

### P1-1: Multi-Cluster Client Manager

**Classificacao:** T2 (muda como API resolve K8s clients — impacto em operacoes de cluster)

**Arquivo:** `api/internal/cellmgr/client_manager.go` (novo)

**O que existe hoje:**
- `api/internal/config/config.go` tem `RuntimeKubeconfig` e `RuntimeKubeContext` hardcoded
- `api/cmd/main.go` usa esses valores diretamente (linha 126)
- Um unico K8s client e criado no bootstrap da API

**O que muda:**
- `ClientManager` gerencia N K8s clients, um por Cell
- Lazy init: client so e criado no primeiro uso de uma Cell
- Cached: client criado uma vez, reutilizado para requests subsequentes
- Thread-safe: `sync.RWMutex` protege o cache de clients
- Usa `CellRouter` (Phase 0) para resolver `cellID → CellConfig → kubeconfig`

```go
package cellmgr

import (
    "context"
    "fmt"
    "sync"

    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"

    "github.com/loopzero/loopzero/api/internal/placement"
)

// ClientManager manages Kubernetes clients per Cell.
// Clients are lazily initialized and cached.
// ADR-0008 Phase 1.
type ClientManager struct {
    router  placement.Router
    clients map[string]kubernetes.Interface
    mu      sync.RWMutex
}

func New(router placement.Router) *ClientManager {
    return &ClientManager{
        router:  router,
        clients: make(map[string]kubernetes.Interface),
    }
}

// ClientFor returns a K8s client for the given cell_id.
// Creates the client on first call (lazy init), caches for subsequent calls.
func (m *ClientManager) ClientFor(ctx context.Context, cellID string) (kubernetes.Interface, error) {
    // Fast path: read lock
    m.mu.RLock()
    if client, ok := m.clients[cellID]; ok {
        m.mu.RUnlock()
        return client, nil
    }
    m.mu.RUnlock()

    // Slow path: write lock + create
    m.mu.Lock()
    defer m.mu.Unlock()

    // Double-check after acquiring write lock
    if client, ok := m.clients[cellID]; ok {
        return client, nil
    }

    cfg, err := m.router.Resolve(ctx, cellID)
    if err != nil {
        return nil, fmt.Errorf("resolve cell %q: %w", cellID, err)
    }

    restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(cfg.KubeConfig))
    if err != nil {
        return nil, fmt.Errorf("parse kubeconfig for cell %q: %w", cellID, err)
    }

    client, err := kubernetes.NewForConfig(restConfig)
    if err != nil {
        return nil, fmt.Errorf("create k8s client for cell %q: %w", cellID, err)
    }

    m.clients[cellID] = client
    return client, nil
}
```

**Wire no main.go:**
- Substituir `config.RuntimeKubeconfig` → `cellmgr.New(router)`
- Todos os call sites que usam o K8s client hardcoded passam a chamar `clientManager.ClientFor(ctx, cellID)`
- `cellID` vem do tenant (via `tenant.CellID` no DB ou no CRD)

**Arquivos impactados:**
- `api/internal/cellmgr/client_manager.go` (novo)
- `api/internal/cellmgr/client_manager_test.go` (novo)
- `api/cmd/main.go` (wire ClientManager)
- Todos os call sites que usam `RuntimeKubeconfig` diretamente

**DoD:**
- [ ] `ClientManager` criado com lazy init e cache thread-safe
- [ ] Interface `placement.Router` usada para resolver kubeconfigs
- [ ] `api/cmd/main.go` usa `ClientManager` em vez de `RuntimeKubeconfig` direto
- [ ] Call sites atualizados para receber `cellID` e usar `ClientFor`
- [ ] Teste unitario: 2 fake cells, verificar que o client correto e retornado por cell_id
- [ ] Teste unitario: concorrencia — 10 goroutines chamando `ClientFor` simultaneamente
- [ ] Teste unitario: cell_id desconhecido retorna erro tipado
- [ ] `go build ./api/...` passa
- [ ] `go test ./api/...` passa

---

### P1-2: Cell-Aware ArgoCD Provisioner

**Classificacao:** T2 (muda contrato do Provisioner — impacto no TenantController)

**Arquivos:**
- `operators/internal/argocd/provisioner.go` — adicionar `cellID` ao `ApplicationSpec`
- `operators/internal/argocd/real_provisioner.go` — resolver dynamic client per Cell
- `operators/internal/controller/tenant_gitops.go` — passar `cellID` ao chamar `EnsureApplication`

**O que existe hoje:**
- `Provisioner` interface em `provisioner.go` com `EnsureApplication(ctx, spec)` e `EnsureAppProject(ctx, spec)`
- `ApplicationSpec` tem `DestinationURL` hardcoded para um unico cluster
- `TenantReconciler.reconcileArgoCDApp()` em `tenant_gitops.go` chama `EnsureApplication` sem cell awareness
- `RealProvisioner` em `real_provisioner.go` usa um unico dynamic client

**O que muda:**

1. `ApplicationSpec` ganha campo `CellID`:
```go
type ApplicationSpec struct {
    // ... existing fields
    // CellID identifies which Cell this Application targets.
    // The provisioner resolves CellID to the correct ArgoCD instance.
    CellID string
}
```

2. `RealProvisioner` passa a usar `CellRouter` para resolver ArgoCD URL + dynamic client per Cell:
```go
type RealProvisioner struct {
    router        placement.Router
    clientFactory func(cellID string) (dynamic.Interface, error) // resolve per Cell
    // ... existing fields
}
```

3. `reconcileArgoCDApp()` le `tenant.Spec.CellID` e passa no `ApplicationSpec.CellID`

4. Cada Cell tem sua propria instancia ArgoCD em URL diferente. O provisioner resolve
   `cellID → CellConfig.ArgocdURL` para saber onde criar o Application CR.

**Impacto:** O TenantController passa a criar ArgoCD Applications no ArgoCD correto para cada Cell.
Tenants na Cell A tem Applications no ArgoCD-A. Tenants na Cell B tem Applications no ArgoCD-B.

**DoD:**
- [ ] `ApplicationSpec.CellID` adicionado
- [ ] `RealProvisioner` aceita `CellRouter` e resolve dynamic client per Cell
- [ ] `reconcileArgoCDApp()` le `tenant.Spec.CellID` e passa no spec
- [ ] Interface `Provisioner` inalterada (CellID faz parte do spec, nao da interface)
- [ ] Teste envtest: 2 mock ArgoCD URLs, verificar Application CRs criados no "cluster" correto
- [ ] Teste envtest: tenant sem CellID (vazio) usa "cell-default" (backward compat)
- [ ] `go build ./operators/...` passa
- [ ] `go test ./operators/...` passa
- [ ] `kubeconform --strict` passa

---

### P1-3: Cell-Aware SyncClient

**Classificacao:** T2 (muda como releases sao sincronizadas — impacto no deploy flow)

**Arquivos:**
- `api/internal/argocd/sync_client.go` — nao modifica, permanece como implementacao single-cell
- `api/internal/argocd/sync_client_factory.go` (novo) — factory que cria SyncClient per cell_id
- `api/internal/services/container_delivery.go` — resolver cell_id antes de chamar SyncClient
- `api/internal/services/release_convergence_loop.go` — resolver cell_id
- `api/internal/services/sync_callback_activator.go` — resolver cell_id

**O que existe hoje:**
- `SyncClient` em `sync_client.go` conecta a um unico ArgoCD (BaseURL + Token hardcoded no config)
- `SyncClientConfig` tem `BaseURL`, `Token`, `TokenProvider`, `Insecure`, `CACertPath`
- `ContainerDeliveryActivator`, `ReleaseConvergenceLoop`, e `SyncCallbackActivator` usam um unico SyncClient injetado

**O que muda:**

1. `SyncClientFactory` cria e cacheia `SyncClient` per cell_id:
```go
package argocd

import (
    "context"
    "fmt"
    "sync"

    "github.com/loopzero/loopzero/api/internal/placement"
)

// SyncClientFactory creates and caches SyncClient instances per Cell.
// ADR-0008 Phase 1.
type SyncClientFactory struct {
    router  placement.Router
    clients map[string]*SyncClient
    mu      sync.RWMutex
}

func NewSyncClientFactory(router placement.Router) *SyncClientFactory {
    return &SyncClientFactory{
        router:  router,
        clients: make(map[string]*SyncClient),
    }
}

// ClientFor returns a SyncClient for the given cell_id.
func (f *SyncClientFactory) ClientFor(ctx context.Context, cellID string) (*SyncClient, error) {
    // ... lazy init + cache (same pattern as ClientManager)
}
```

2. `ContainerDeliveryActivator`, `ReleaseConvergenceLoop`, e `SyncCallbackActivator`
   recebem `SyncClientFactory` em vez de `*SyncClient`:
```go
// Antes:
type ContainerDeliveryActivator struct {
    syncClient *argocd.SyncClient
}

// Depois:
type ContainerDeliveryActivator struct {
    syncFactory *argocd.SyncClientFactory
}
```

3. Em cada operacao, resolver `cell_id` da release → tenant → cell_id:
```go
func (a *ContainerDeliveryActivator) Activate(ctx context.Context, release *db.Release) error {
    tenant, err := a.tenantStore.GetByID(ctx, release.TenantID)
    syncClient, err := a.syncFactory.ClientFor(ctx, tenant.CellID)
    return syncClient.TriggerSync(ctx, appName)
}
```

**Impacto:** Sync operations (TriggerSync, WaitForSync) sao roteadas para o ArgoCD correto
baseado no cell_id do tenant dono da release.

**DoD:**
- [ ] `SyncClientFactory` criado com lazy init e cache
- [ ] `ContainerDeliveryActivator` usa `SyncClientFactory`
- [ ] `ReleaseConvergenceLoop` usa `SyncClientFactory`
- [ ] `SyncCallbackActivator` usa `SyncClientFactory`
- [ ] Wire no `main.go`: `argocd.NewSyncClientFactory(router)`
- [ ] Teste unitario: 2 fake ArgoCD servers, verificar `TriggerSync` atinge o server correto per cell_id
- [ ] Teste unitario: cell_id desconhecido retorna erro tipado
- [ ] Teste unitario: factory retorna mesmo client para calls repetidos ao mesmo cell_id
- [ ] `go build ./api/...` passa
- [ ] `go test ./api/...` passa

---

### P1-4: Cell Provisioner (Pulumi)

**Classificacao:** T2 (infra change — novo componente Pulumi)

**Arquivo:** `infra/pulumi/src/components/cell.ts` (novo)

**O que existe hoje:**
- `infra/pulumi/src/components/infra.ts` cria 3 clusters (mgmt, build, workload) como recursos separados
- `infra/pulumi/src/components/runtime-bootstrap.ts` instala Helm charts no workload cluster
- `infra/pulumi/src/providers/digitalocean/` implementa DOKS, networking, etc.
- O workload cluster e hardcoded — nao existe conceito de "Cell" no Pulumi

**O que muda:**

1. `cell.ts` define uma Cell como `ComponentResource`:
```typescript
import * as pulumi from "@pulumi/pulumi";

export interface CellConfig {
  /** Unique cell identifier (e.g., "cell-default", "cell-us-east-1") */
  readonly cellId: string;
  /** DigitalOcean region (e.g., "sfo3", "nyc3") */
  readonly region: string;
  /** Node size (e.g., "s-2vcpu-4gb") */
  readonly nodeSize: string;
  /** Number of worker nodes */
  readonly nodeCount: number;
  /** Base domain for ingress (e.g., "dev.loopzero.example") */
  readonly baseDomain: string;
}

export interface CellOutput {
  /** Kubeconfig for the Cell's workload cluster */
  readonly kubeconfig: pulumi.Output<string>;
  /** ArgoCD URL within the Cell */
  readonly argocdUrl: pulumi.Output<string>;
  /** Traefik ingress public IP */
  readonly ingressIp: pulumi.Output<string>;
  /** Cell identifier (passthrough) */
  readonly cellId: string;
}
```

2. Uma Cell contem:
   - 1 DOKS cluster (via `digitalocean.KubernetesCluster`)
   - ArgoCD Helm install (via `kubernetes.helm.v3.Release`)
   - Traefik Helm install
   - cert-manager
   - Network policies
   - Prometheus agent (scrape local, remote-write para central)

3. A "cell-default" usa o workload cluster existente. Novas Cells sao criadas do zero.

4. Config no `Pulumi.dev.yaml`:
```yaml
loopzero:cells:
  - cellId: "cell-default"
    region: "sfo3"
    nodeSize: "s-2vcpu-4gb"
    nodeCount: 2
    baseDomain: "dev.loopzero.example"
  # Adicionar Cell #2 = adicionar item aqui
  # - cellId: "cell-us-east-1"
  #   region: "nyc3"
  #   nodeSize: "s-2vcpu-4gb"
  #   nodeCount: 2
  #   baseDomain: "dev.loopzero.example"
```

**Arquivos impactados:**
- `infra/pulumi/src/components/cell.ts` (novo)
- `infra/pulumi/src/components/infra.ts` (refatorar workload cluster para usar Cell)
- `infra/pulumi/src/index.ts` (iterar sobre cells config)
- `infra/pulumi/Pulumi.dev.yaml` (adicionar cells config)

**DoD:**
- [ ] `CellConfig` e `CellOutput` interfaces definidas
- [ ] `createCell()` function cria todos os recursos de uma Cell
- [ ] cell-default usa o workload cluster existente (backward compat)
- [ ] `pulumi preview` com 1 Cell mostra recursos identicos ao atual
- [ ] `pulumi preview` com 2 Cells mostra recursos adicionais corretos
- [ ] Outputs exportam kubeconfig, ArgoCD URL, e ingress IP per Cell
- [ ] Zero breaking changes no stack existente com 1 Cell
- [ ] Vitest test para validar shape dos outputs

---

### P1-5: ClusterCredential CRD Per Cell

**Classificacao:** T2 (CRD schema change — impacto no operator)

**Arquivos:**
- `operators/api/v1/clustercredential_types.go` — adicionar campo `CellID` ao spec
- `operators/internal/controller/clustercredential_controller.go` — validar CellID, indexar
- `operators/config/crd/bases/` — regenerar CRD YAML

**O que existe hoje:**
- `ClusterCredentialSpec` tem `ClusterName`, `APIServer`, `SecretRef`, `Scope`, `TLS`
- `ClusterCredentialController` valida conectividade e cria ArgoCD cluster secret
- Nao existe conceito de Cell no CRD

**O que muda:**

1. `ClusterCredentialSpec` ganha campo `CellID`:
```go
type ClusterCredentialSpec struct {
    // ... existing fields

    // CellID identifies which Cell this cluster belongs to.
    // Used by CellRouter to discover available Cells and their kubeconfigs.
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:Pattern=`^cell-[a-z0-9-]+$`
    CellID string `json:"cellID"`
}
```

2. `ClusterCredentialController` valida unicidade de `CellID` (um ClusterCredential per Cell)

3. API descobre Cells listando `ClusterCredential` CRs:
```go
// No CellRouter (substitui PassthroughRouter):
func (r *K8sRouter) ListCells(ctx context.Context) ([]CellConfig, error) {
    var ccList loopzerov1.ClusterCredentialList
    if err := r.client.List(ctx, &ccList); err != nil {
        return nil, err
    }
    // Convert ClusterCredentials to CellConfigs
}
```

4. Quando Pulumi provisiona uma nova Cell, cria automaticamente um `ClusterCredential` CR
   no management cluster com o kubeconfig da nova Cell.

**Impacto:** `ClusterCredential` passa de "registro de cluster" para "registro de Cell".
O `CellRouter` (Phase 0 `PassthroughRouter`) e substituido por uma implementacao real
que descobre Cells via `ClusterCredential` list.

**Arquivos impactados:**
- `operators/api/v1/clustercredential_types.go`
- `operators/api/v1/clustercredential_types_test.go`
- `operators/internal/controller/clustercredential_controller.go`
- `operators/config/crd/bases/` (regenerar)
- `api/internal/placement/router.go` — nova impl `K8sRouter`
- `api/internal/placement/router_test.go` (novo)

**DoD:**
- [ ] Campo `CellID` adicionado ao `ClusterCredentialSpec`
- [ ] Validacao kubebuilder: MinLength=1, Pattern=`^cell-[a-z0-9-]+$`
- [ ] `make manifests` regenera CRD YAML
- [ ] `kubeconform --strict` passa
- [ ] `K8sRouter` implementa `placement.Router` usando ClusterCredential list
- [ ] `K8sRouter` substitui `PassthroughRouter` no wire do main.go
- [ ] Teste envtest: criar 2 ClusterCredentials com cellIDs diferentes, listar retorna ambos
- [ ] Teste envtest: ClusterCredential sem CellID e rejeitado pela validacao
- [ ] Teste unitario: `K8sRouter.Resolve("cell-us-east-1")` retorna CellConfig correto
- [ ] Teste unitario: `K8sRouter.Resolve("cell-inexistente")` retorna erro tipado
- [ ] `go build ./api/... ./operators/...` passa
- [ ] `go test ./api/... ./operators/...` passa

---

### P1-6: Cell-Aware DNS Routing

**Classificacao:** T2 (muda routing de DNS — impacto em acessibilidade de apps)

**Arquivos:**
- `infra/cloudflare/worker.mjs` — estender para resolver tenant → cell → ingress IP
- `infra/pulumi/src/providers/cloudflare/static-delivery.ts` — estender KV para cells
- `api/internal/cfkv/client.go` — adicionar metodo para escrever cell routing entries

**O que existe hoje:**
- Cloudflare Worker (`worker.mjs`) roteia requests estaticos usando KV (`loopzero-routing`)
- KV contem entries `{subdomain} → {buildId, s3Prefix}` para apps estaticos
- Apps de container usam Cloudflare Tunnel → Traefik (IP unico do workload cluster)
- Um unico Tunnel/Traefik atende todos os tenants

**O que muda:**

1. KV ganha entries de cell routing: `cell:{tenant} → {cellId, ingressIp}`
2. Worker resolve tenant de qualquer request:
   - Extrai tenant do hostname: `{app}.{tenant}.dev.loopzero.example` → tenant
   - Verifica primeiro KV de static routing (existente)
   - Se nao for estatico, consulta `cell:{tenant}` para descobrir o ingress IP correto
   - Proxy para o ingress IP da Cell do tenant

3. Quando tenant e criado, `PlacementEngine` escreve `cell:{tenant} → {cellId, ingressIp}` no KV:
```go
// No handler de criacao de tenant (ou no operator):
err := cfkvClient.PutCellRouting(ctx, tenantName, cellID, ingressIP)
```

4. Cada Cell tem seu proprio Traefik com IP publico (ou Tunnel) dedicado.

**Phase 1 simplificacao:** Usa Cloudflare Workers com KV lookup (ja existe infra).
NAO usa DNS records por Cell (evita propagacao de DNS). O Worker faz proxy direto para o IP correto.

**DoD:**
- [ ] Worker atualizado para resolver cell routing via KV
- [ ] KV schema estendido: `cell:{tenant}` → `{cellId, ingressIp}`
- [ ] `cfkvClient.PutCellRouting()` implementado
- [ ] Tenant creation escreve cell routing no KV
- [ ] Worker faz proxy para ingress IP correto baseado no tenant
- [ ] Teste unitario: mock KV, verificar routing resolution para 2 Cells
- [ ] Teste unitario: tenant sem entry no KV usa cell-default (fallback)
- [ ] Worker continua roteando static apps corretamente (nao quebra)
- [ ] Validado com 2 Cells em DigitalOcean

---

### P1-7: PlacementEngine — Round-Robin Strategy

**Classificacao:** T1 (substitui DefaultEngine por RoundRobinEngine — logica interna)

**Arquivo:** `api/internal/placement/engine.go` (modificar)

**O que existe hoje:**
- `DefaultEngine` sempre retorna `"cell-default"` (Phase 0)
- `Engine` interface com `AssignCell(ctx)` e `GetCell(ctx, tenantID)`
- Wire no handler de criacao de tenant

**O que muda:**

1. `RoundRobinEngine` substitui `DefaultEngine`:
```go
// RoundRobinEngine assigns new tenants to the Cell with fewest tenants.
// Existing tenants keep their cell_id (sticky assignment).
// ADR-0008 Phase 1.
type RoundRobinEngine struct {
    router         Router
    tenantStore    TenantCounter // interface: CountByCell(ctx) → map[cellID]int
    maxPerCell     int           // default 500
}

func NewRoundRobinEngine(router Router, store TenantCounter, maxPerCell int) *RoundRobinEngine {
    return &RoundRobinEngine{
        router:     router,
        tenantStore: store,
        maxPerCell: maxPerCell,
    }
}

// AssignCell returns the cell_id of the Cell with fewest tenants (bin-packing).
// Returns error if all Cells are at capacity.
func (e *RoundRobinEngine) AssignCell(ctx context.Context) (string, error) {
    cells, err := e.router.ListCells(ctx)
    counts, err := e.tenantStore.CountByCell(ctx)
    // Find cell with fewest tenants that is not at maxPerCell
    // If all at capacity, return LoopZeroError with CellsAtCapacity code
}

// GetCell returns the cell_id for an existing tenant (reads from DB).
func (e *RoundRobinEngine) GetCell(ctx context.Context, tenantID string) (string, error) {
    tenant, err := e.tenantStore.GetByID(ctx, tenantID)
    return tenant.CellID, nil
}
```

2. `TenantCounter` interface (nova) para desacoplar do store concreto:
```go
type TenantCounter interface {
    CountByCell(ctx context.Context) (map[string]int, error)
}
```

3. Query SQL para `CountByCell`:
```sql
SELECT cell_id, COUNT(*) as count FROM tenants GROUP BY cell_id
```

4. Config: `maxPerCell` vem do config (`CELL_MAX_TENANTS`, default 500)

**DoD:**
- [ ] `RoundRobinEngine` implementado
- [ ] `TenantCounter` interface definida e implementada no tenant store
- [ ] `CountByCell` query adicionada ao tenant store
- [ ] Config `CELL_MAX_TENANTS` adicionado (default 500)
- [ ] Wire no main.go: `placement.NewRoundRobinEngine(router, store, cfg.CellMaxTenants)`
- [ ] Teste unitario: 10 tenants criados com 2 Cells, distribuicao balanceada (5/5)
- [ ] Teste unitario: 1 Cell no capacity (500 tenants), novo tenant vai para Cell #2
- [ ] Teste unitario: todas as Cells no capacity, retorna erro `CellsAtCapacity`
- [ ] Teste unitario: `GetCell` retorna cell_id existente do tenant (sticky)
- [ ] Teste unitario: `DefaultEngine` mantido como fallback para single-cell (backward compat)
- [ ] `go build ./api/...` passa
- [ ] `go test ./api/...` passa

---

### P1-8: Cell Health Dashboard (Grafana)

**Classificacao:** T1 (observabilidade — nao muda comportamento)

**Arquivos:**
- `infra/helm/monitoring/dashboards/cell-health.json` (novo)
- `infra/helm/monitoring/values.yaml` — registrar dashboard

**O que existe hoje:**
- Prometheus coleta metricas do API e clusters
- Grafana tem dashboards de deploy, SLO, e system health
- Metrica `loopzero_cell_tenant_count` criada na Phase 0 (P0-6)
- Prometheus unico no management cluster (fase 1 nao tem federation)

**O que muda:**

1. Dashboard "Cell Health" no Grafana com paineis:
   - **Tenant Count per Cell:** gauge mostrando `loopzero_cell_tenant_count` per cell_id
   - **App Count per Cell:** gauge com `count(kube_pod_info{namespace=~"loopzero-.*"}) by (cluster)`
   - **ArgoCD Sync Latency per Cell:** histogram `argocd_app_reconcile_duration_seconds` por cell_id
   - **Pod Count per Cell:** `sum(kube_pod_info) by (cluster)`
   - **Cell Capacity:** progress bar `loopzero_cell_tenant_count / 500 * 100` per cell_id
   - **Sync Failures per Cell:** `sum(rate(argocd_app_sync_total{phase="Error"})) by (cluster)`

2. Phase 1 usa scrape direto (single Prometheus). Phase 2 migrara para federation.

3. Alerta `LoopZeroCellCapacityWarning` (definido na P0-6) ativado com threshold real.

**DoD:**
- [ ] Dashboard JSON criado com 6 paineis
- [ ] Dashboard registrado no Grafana via Helm values
- [ ] `loopzero_cell_tenant_count` visivel e populado no dashboard
- [ ] Painel de capacidade mostra % de uso per Cell
- [ ] `kubeconform --strict` passa (sem impacto em manifests K8s)
- [ ] Dashboard validado no Grafana em DigitalOcean

---

## Ordem de Execucao

As tasks tem dependencias. A ordem recomendada e:

```
P1-5 (ClusterCredential CRD)        ← fundacao: Cells sao descobertas via CRD
  ↓
P1-1 (ClientManager)                ← usa CellRouter que depende de ClusterCredential
  ↓
P1-4 (Cell Provisioner Pulumi)      ← provisiona Cells e cria ClusterCredential CRs
  ↓
P1-2 (ArgoCD Provisioner)           ← usa CellRouter para resolver ArgoCD per Cell
P1-3 (SyncClient Factory)           ← usa CellRouter para resolver ArgoCD per Cell
  ↓ (P1-2 e P1-3 em paralelo)
P1-7 (PlacementEngine Round-Robin)  ← atribui tenants a Cells (precisa de ListCells)
  ↓
P1-6 (DNS Routing)                  ← precisa de PlacementEngine para escrever routing
  ↓
P1-8 (Dashboard)                    ← pode ser feito em qualquer momento, sem dependencias
```

---

## Checklist Global — Phase 1

### Gate: Phase 1 Complete

- [ ] P1-1: ClientManager gerencia N K8s clients per Cell
- [ ] P1-2: ArgoCD Provisioner cria Applications no ArgoCD correto per Cell
- [ ] P1-3: SyncClient Factory roteia sync operations para ArgoCD correto per Cell
- [ ] P1-4: Cell Provisioner cria Cells inteiras via Pulumi config
- [ ] P1-5: ClusterCredential CRD tem CellID, K8sRouter descobre Cells
- [ ] P1-6: DNS routing via CF Worker resolve tenant → Cell → ingress IP
- [ ] P1-7: PlacementEngine distribui tenants entre Cells (round-robin)
- [ ] P1-8: Grafana dashboard mostra metricas per Cell
- [ ] Todos: `go build ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `go test ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `task validate:manifests` passa
- [ ] Todos: `task lint` passa
- [ ] Adicionar Cell #2 requer APENAS: Pulumi config + ClusterCredential CR + DNS config
- [ ] Zero mudancas de codigo necessarias para adicionar Cell #2
- [ ] Deploy E2E funciona em ambas as Cells
- [ ] Changelog atualizado
- [ ] Validado em DigitalOcean com 2 workload clusters

### O Que NAO Fazer na Phase 1

- [ ] NAO implementar tenant migration entre Cells (Phase 2)
- [ ] NAO implementar Prometheus federation (Phase 2)
- [ ] NAO implementar Cell-aware build plane (build compartilhado e suficiente)
- [ ] NAO implementar auto-scaling de Cells (manual provisioning)
- [ ] NAO implementar Cell failover/DR (Phase 3)
- [ ] NAO modificar CLI (zero mudanca na interface do usuario)
- [ ] NAO modificar API contracts HTTP (zero mudanca nos endpoints publicos)
- [ ] NAO implementar cross-cell queries (tenant vive em uma unica Cell)

---

## Trigger para Phase 2

Phase 2 inicia quando QUALQUER destes criterios for atingido:

1. `loopzero_cell_tenant_count{cell_id=ANY} > 400` (80% da capacidade)
2. Necessidade de rebalancear tenants entre Cells (tenant migration)
3. Prometheus federation necessaria (multiplas Cells com metricas locais)
4. Decisao de negocio de oferecer Cell regional (e.g., "Cell EU" para GDPR)

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `docs/adr/0008-cell-based-architecture.md` | ADR que define a arquitetura Cell |
| `docs/plans/cell-architecture-phase0.md` | Phase 0 — fundacao (pre-requisito) |
| `docs/meetings/records/MEET-2026-03-31-024.md` | Reuniao que gerou o ADR |
| `api/internal/placement/engine.go` | PlacementEngine interface (Phase 0) |
| `api/internal/placement/router.go` | CellRouter interface (Phase 0) |
| `api/internal/argocd/sync_client.go` | SyncClient atual (single-cell) |
| `operators/internal/argocd/provisioner.go` | ArgoCD Provisioner interface |
| `operators/api/v1/clustercredential_types.go` | ClusterCredential CRD |
| `infra/pulumi/src/components/infra.ts` | Infra provisioner atual |
| `infra/cloudflare/worker.mjs` | CF Worker para static delivery |
| `infra/pulumi/src/providers/cloudflare/static-delivery.ts` | CF static delivery Pulumi |
