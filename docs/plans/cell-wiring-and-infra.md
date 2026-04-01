# Plano: Cell Architecture — Wiring, Pulumi & CF Worker

> **Objetivo:** Conectar o codigo Go ao runtime (main.go wiring), implementar o Cell
> provisioner Pulumi completo, e atualizar o CF Worker para multi-cell routing.
>
> **Escopo:** 3 blocos, 12 tasks. Mix de Go (wiring), TypeScript (Pulumi), JavaScript (CF Worker).
>
> **Pre-requisitos:** Phase 0 e Phase 1 codigo completo (28 pacotes passam).
>
> **Classificacao:** T2/T3 — contratos internos mudam, infra muda.

---

## Bloco 1: Runtime Wiring (main.go)

> Conectar ClientManager, SyncClientFactory e RoundRobinEngine ao runtime da API.
> Hoje existem como codigo morto — importado mas nao usado.

### W-1: Wire CellRouter no main.go

**Arquivo:** `api/cmd/main.go`

**O que fazer:**
1. Criar `StaticRouter` com config da Cell atual (single cell = passthrough):
```go
cellRouter, err := placement.NewStaticRouter([]placement.CellConfig{{
    CellID:      placement.DefaultCellID,
    KubeConfig:  cfg.RuntimeKubeconfig,
    KubeContext: cfg.RuntimeKubeContext,
    ArgocdURL:   cfg.ArgocdURL,
    ArgocdToken: cfg.ArgocdToken,
    IngressDomain: cfg.BaseDomain,
}})
```
2. Substituir `PassthroughRouter` (Phase 0) por `StaticRouter`
3. Passar `cellRouter` ao `CellMetricsCollector`, `BootstrapHandler`, `CellService`

**Impacto:** Zero mudanca comportamental — StaticRouter com 1 Cell = PassthroughRouter.

**DoD:**
- [ ] `StaticRouter` criado com config da Cell default
- [ ] `CellMetricsCollector` usa `StaticRouter`
- [ ] `BootstrapHandler` usa `RoundRobinEngine(cellRouter, querier, 500)` em vez de `DefaultEngine`
- [ ] `go build ./api/...` passa
- [ ] `go test ./api/...` passa
- [ ] Deploy E2E identico (zero mudanca comportamental)

---

### W-2: Wire SyncClientFactory no deploy flow

**Arquivos:**
- `api/cmd/main.go` — criar factory, injetar nos servicos
- `api/internal/services/container_delivery.go` — aceitar factory OU client
- `api/internal/services/release_convergence_loop.go` — aceitar factory OU client
- `api/internal/services/sync_callback_activator.go` — aceitar factory OU client

**O que fazer:**

Abordagem BACKWARD-COMPATIBLE: em vez de mudar as interfaces dos servicos,
criar um `SingleCellSyncAdapter` que wrapa a factory para single-cell:

```go
// SingleCellSyncAdapter wraps SyncClientFactory for backward compatibility.
// Returns the same client for all calls (single Cell).
// Phase 1+: services call factory.ClientFor(cellID) directly.
type SingleCellSyncAdapter struct {
    factory *argocd.SyncClientFactory
    cellID  string // "cell-default"
}

func (a *SingleCellSyncAdapter) TriggerSync(ctx context.Context, appName string) error {
    client, err := a.factory.ClientFor(ctx, a.cellID)
    if err != nil { return err }
    return client.TriggerSync(ctx, appName)
}
// ... implement WaitForSync, GetAppStatus, etc.
```

Isso permite:
- Zero mudanca nas interfaces `SyncClient` dos servicos
- Factory esta wired e funcional
- Quando Phase 1 ativar multi-cell, trocar `SingleCellSyncAdapter` por resolucao direta

**DoD:**
- [ ] `SingleCellSyncAdapter` criado
- [ ] `SyncClientFactory` criado no main.go
- [ ] Servicos recebem `SingleCellSyncAdapter` em vez de `rawSyncClient` direto
- [ ] `go build ./api/...` passa
- [ ] `go test ./api/...` passa
- [ ] Deploy flow identico (factory resolve para o mesmo ArgoCD)

---

### W-3: Wire ClientManager no main.go

**Arquivo:** `api/cmd/main.go`

**O que fazer:**
1. Criar `ClientManager` com `StaticRouter`:
```go
clientMgr := cellmgr.New(cellRouter, cellmgr.DefaultClientFactory)
```
2. Disponibilizar via `ServerConfig` para servicos que precisam de K8s client per-Cell
3. Phase 0: ninguem usa diretamente (servicos usam o runtime client existente)
4. Phase 1+: servicos chamam `clientMgr.ClientFor(ctx, cellID)` em vez do client hardcoded

**DoD:**
- [ ] `ClientManager` criado no main.go
- [ ] Acessivel via `ServerConfig` (preparado para uso futuro)
- [ ] `go build ./api/...` passa
- [ ] Zero mudanca comportamental

---

### W-4: Wire RoundRobinEngine no BootstrapHandler

**Arquivo:** `api/cmd/main.go`

**O que fazer:**
Substituir `placement.NewDefaultEngine()` por `placement.NewRoundRobinEngine()`:
```go
cellQuerier := &db.PostgresCellStore{DB: dbAdapter}
placementEngine := placement.NewRoundRobinEngine(cellRouter, cellQuerier, 500)
```

Com 1 Cell, `RoundRobinEngine.AssignCell()` sempre retorna "cell-default"
(unica Cell disponivel). Zero mudanca comportamental.

**DoD:**
- [ ] `RoundRobinEngine` substituir `DefaultEngine` no main.go
- [ ] Novo tenant criado tem `cell_id = "cell-default"` (mesma funcionalidade)
- [ ] `go test ./api/...` passa

---

## Bloco 2: Pulumi Cell Provisioner

> Transformar o scaffold `cell.ts` em implementacao real que provisiona Cells completas.

### P-1: Refatorar runtimeCluster como Cell

**Arquivo:** `infra/pulumi/src/components/infra.ts`

**O que fazer:**
1. O `runtimeCluster` atual vira `cells[0]` (cell-default)
2. A funcao `createInfra()` aceita array de `CellConfig` do stack config
3. Se nenhuma Cell e configurada, usa `DEFAULT_CELL` (backward compat)
4. Cada Cell produz um `CellOutput` com kubeconfig + ArgoCD URL + ingress IP
5. Output `runtimeCluster` continua existindo (alias para `cells[0]`)

**Config Pulumi:**
```yaml
# Pulumi.dev.yaml — adicionar
loopzero:cells:
  - cellId: "cell-default"
    region: "sfo3"
    nodeSize: "s-2vcpu-4gb"
    nodeCount: 2
    baseDomain: "dev.loopzero.example"
```

**DoD:**
- [ ] `createInfra()` aceita cells config
- [ ] cell-default = runtimeCluster existente (backward compat)
- [ ] `pulumi preview` mostra zero mudancas com 1 Cell (identico ao atual)
- [ ] `pulumi preview` com 2 Cells mostra recursos adicionais
- [ ] Outputs: `cell_{id}_kubeconfig`, `cell_{id}_argocd_url`, `cell_{id}_ingress_ip`
- [ ] Vitest test para shape dos outputs

---

### P-2: Implementar createCell() completo

**Arquivo:** `infra/pulumi/src/components/cell.ts`

**O que fazer:** Substituir scaffold por implementacao real:
```typescript
export function createCell(config: CellConfig, opts?: CellOpts): CellOutput {
  // 1. DOKS cluster
  const cluster = new digitalocean.KubernetesCluster(...)

  // 2. K8s provider for this Cell
  const provider = new k8s.Provider(...)

  // 3. ArgoCD Helm install
  const argocd = new k8s.helm.v3.Release("argocd", { ... }, { provider })

  // 4. Traefik Helm install
  const traefik = new k8s.helm.v3.Release("traefik", { ... }, { provider })

  // 5. cert-manager
  const certManager = new k8s.helm.v3.Release("cert-manager", { ... }, { provider })

  // 6. Network policies
  // 7. ClusterCredential CR on mgmt cluster

  return { kubeconfig, argocdUrl, ingressIp, cellId }
}
```

**Dependencias:**
- Reusa providers existentes em `providers/digitalocean/`
- Reusa Helm values existentes em `infra/helm/`
- ArgoCD values parametrizados por tier (P4-3 argocd-tiers.yaml)

**DoD:**
- [ ] `createCell()` cria DOKS + ArgoCD + Traefik + cert-manager
- [ ] ArgoCD configurado com acesso ao Gitea compartilhado
- [ ] Traefik com LoadBalancer dedicado
- [ ] cert-manager com ClusterIssuer DNS-01
- [ ] `pulumi up` com 2 Cells provisiona segundo cluster
- [ ] `pulumi destroy` limpa recursos corretamente
- [ ] Validado em DigitalOcean

---

### P-3: Bootstrap de Cell (Helm + CRDs)

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` (modificar)

**O que fazer:**
1. `bootstrapCluster()` aceita N Cells em vez de 1 runtimeCluster
2. Para cada Cell: instala Helm charts (Prometheus agent, Promtail, network policies)
3. Cria `ClusterCredential` CR no mgmt cluster apontando para cada Cell

**DoD:**
- [ ] Bootstrap funciona para N Cells
- [ ] Cada Cell tem Prometheus agent + Promtail
- [ ] ClusterCredential CR criado automaticamente
- [ ] `pulumi up` e idempotente (re-run nao muda nada)

---

## Bloco 3: Cloudflare Worker Multi-Cell

> Atualizar o Worker JavaScript para rotear traffic dinamico para a Cell correta.

### CF-1: Adicionar cell routing ao Worker

**Arquivo:** `infra/cloudflare/worker.mjs`

**O que fazer:**

O Worker atual roteia APENAS apps estaticos (KV: `subdomain → {buildId, s3Prefix}`).
Para multi-cell, o Worker precisa tambem rotear apps dinamicos:

```javascript
async function handleRequest(request) {
  var url = new URL(request.url);
  var hostname = url.hostname;
  var suffix = "." + BASE_DOMAIN;

  if (!hostname.endsWith(suffix)) return fetch(request);
  var subdomain = hostname.slice(0, -suffix.length);

  // 1. Check static routing (existing — unchanged)
  var staticRoute = await LOOPZERO_ROUTING.get(subdomain, { type: "json" });
  if (staticRoute && staticRoute.buildId) {
    return handleStaticRequest(request, staticRoute); // existing logic
  }

  // 2. NEW: Check cell routing for dynamic apps
  // Extract tenant from subdomain: {app}.{tenant}.{baseDomain} → tenant
  var parts = subdomain.split(".");
  if (parts.length >= 2) {
    var tenant = parts[parts.length - 1]; // last segment before baseDomain
    var cellRoute = await LOOPZERO_ROUTING.get("cell:" + tenant, { type: "json" });
    if (cellRoute && cellRoute.ingressIp) {
      // Proxy to the Cell's Traefik ingress
      var targetUrl = "https://" + cellRoute.ingressIp + url.pathname + url.search;
      return fetch(targetUrl, {
        method: request.method,
        headers: request.headers,
        body: request.body,
        redirect: "follow",
      });
    }
  }

  // 3. Fallback: pass through to default origin (CF Tunnel)
  return fetch(request);
}
```

**KV Schema estendido:**
| Key | Value | Purpose |
|-----|-------|---------|
| `{subdomain}` | `{buildId, s3Prefix}` | Static app routing (existing) |
| `cell:{tenant}` | `{cellId, ingressIp}` | Dynamic app Cell routing (new) |

**DoD:**
- [ ] Worker resolve `cell:{tenant}` do KV para apps dinamicos
- [ ] Static routing continua funcionando (sem regressao)
- [ ] Fallback para CF Tunnel quando nenhuma entry existe
- [ ] Teste local: mock KV com 2 Cells, verificar proxy correto
- [ ] Deploy Worker via Pulumi/Wrangler

---

### CF-2: Pulumi atualiza KV na criacao de tenant

**Arquivos:**
- `api/internal/cfkv/client.go` — `PutCellRouting()` ja existe
- `api/internal/routes/bootstrap.go` — chamar `PutCellRouting` apos criacao

**O que fazer:**
Quando um tenant e criado (bootstrap), alem de setar `cell_id` no PG,
escrever a entry `cell:{tenant} → {cellId, ingressIp}` no CF KV.

```go
// No BootstrapHandler.Bootstrap(), apos store.Bootstrap():
if kvClient != nil {
    cellCfg, _ := cellRouter.Resolve(ctx, req.CellID)
    if cellCfg != nil {
        kvClient.PutCellRouting(ctx, tenantID, req.CellID, cellCfg.IngressDomain)
    }
}
```

**DoD:**
- [ ] Tenant creation escreve cell routing no CF KV
- [ ] KV entry tem cellId + ingressIp corretos
- [ ] Tenant deletion remove entry do KV
- [ ] `go test ./api/...` passa

---

### CF-3: Teste E2E multi-cell routing

**Arquivo:** `infra/tests/e2e/cell_routing_test.go` (novo)

**O que fazer:**
1. Criar 2 tenants em Cells diferentes
2. Verificar que requests sao roteados para o Traefik correto
3. Verificar que static apps continuam funcionando
4. Verificar fallback para CF Tunnel

**Pre-requisito:** 2 Cells provisionadas em DigitalOcean.

**DoD:**
- [ ] Teste E2E com 2 Cells
- [ ] Routing verificado para ambas Cells
- [ ] Static routing nao regrediu
- [ ] Fallback funciona

---

## Ordem de Execucao

```
Bloco 1 (Wiring):
  W-1 (CellRouter) → W-4 (RoundRobinEngine) → W-2 (SyncClientFactory) → W-3 (ClientManager)

Bloco 2 (Pulumi):
  P-1 (refatorar runtimeCluster) → P-2 (createCell completo) → P-3 (bootstrap N Cells)

Bloco 3 (CF Worker):
  CF-1 (Worker multi-cell) → CF-2 (KV na criacao de tenant) → CF-3 (E2E teste)

Bloco 1 pode ser feito SEM infraestrutura (backward compat com 1 Cell).
Bloco 2 requer DigitalOcean para validacao.
Bloco 3 requer Cloudflare + 2 Cells provisionadas.
```

---

## Checklist Global

### Gate: Wiring Complete (Bloco 1 — sem infra)
- [x] W-1: StaticRouter no main.go — single Cell backward compat
- [x] W-2: SyncClientFactory ready (not wired yet — single syncClient is backward compat)
- [x] W-3: ClientManager ready (not wired yet — no call sites need per-cell K8s client)
- [x] W-4: RoundRobinEngine substituiu DefaultEngine no main.go
- [x] `go build ./api/... ./operators/... ./pkg/...` passa
- [x] `go test ./api/... ./pkg/...` passa (28 packages, zero failures)
- [x] Zero mudanca comportamental com 1 Cell

### Gate: Pulumi Complete (Bloco 2 — requer DO)
- [ ] P-1: runtimeCluster refatorado como Cell
- [ ] P-2: createCell() provisiona DOKS + ArgoCD + Traefik
- [ ] P-3: Bootstrap instala Helm charts em N Cells
- [ ] `pulumi preview` com 2 Cells mostra recursos corretos
- [ ] `pulumi up` cria segundo cluster em DigitalOcean
- [ ] Validado com deploy E2E em ambas Cells

### Gate: CF Worker Complete (Bloco 3 — requer CF + 2 Cells)
- [x] CF-1: Worker atualizado com cell routing logic (worker.mjs)
- [ ] CF-2: Tenant creation escreve cell routing no KV (wiring needed)
- [ ] CF-3: E2E teste com 2 Cells passa (requer 2 Cells)
- [ ] Static routing sem regressao (requer CF deploy)

---

## O Que NAO Fazer

- [ ] NAO modificar CLI (zero mudanca)
- [ ] NAO modificar API contracts publicos
- [ ] NAO implementar tenant migration (Phase 3)
- [ ] NAO implementar auto Cell provisioning (Phase 3)
- [ ] NAO implementar ApplicationSets (Phase 4)
- [ ] NAO implementar Prometheus federation (Phase 2)

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `api/internal/cellmgr/client_manager.go` | ClientManager ja implementado |
| `api/internal/argocd/sync_client_factory.go` | SyncClientFactory ja implementado |
| `api/internal/placement/engine.go` | RoundRobinEngine ja implementado |
| `api/internal/placement/router.go` | StaticRouter ja implementado |
| `api/cmd/main.go:268-278` | TODO comment com instrucoes de wiring |
| `infra/pulumi/src/components/cell.ts` | Scaffold a completar |
| `infra/cloudflare/worker.mjs` | Worker a atualizar |
| `api/internal/cfkv/client.go` | PutCellRouting ja implementado |
