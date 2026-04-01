# Plano: Cell Architecture — Phase 2 (First Multi-Cell Deployment)

> **Objetivo:** Implantar e operar uma segunda Cell em producao. Validar que tenants na
> Cell-1 e Cell-2 operam de forma independente. Provar isolamento de blast radius.
>
> **Escopo:** 7 tasks. Provisionamento de Cell-2, federacao de metricas, logs multi-cell,
> deploy E2E cross-cell, teste de blast radius, roteamento multi-cell, e visibilidade CLI.
>
> **Classificacao:** T3 (arquitetura — novo cluster, novo roteamento, mudanca em observabilidade)
>
> **Status:** PENDING — Requer ADR aprovado para execucao
>
> **Trigger:** 200+ tenants em Cell-1, ou `loopzero_cell_tenant_count{cell_id="cell-default"} > 200`
>
> **Duracao:** 2 sprints
>
> **Pre-requisitos:** Phase 0 e Phase 1 completas e validadas em DigitalOcean

---

## Contexto

Phase 0 adicionou `cell_id` ao modelo de tenant e criou interfaces PlacementEngine e
CellRouter como passthroughs. Phase 1 implementou o Cell Provisioner (IaC para criar
clusters), PlacementEngine com logica real, e CellRouter resolvendo conexoes por cell_id.

Phase 2 e a primeira vez que o LoopZero opera com DOIS clusters de workload simultaneamente.
O objetivo nao e escala — e PROVA. Provar que:

1. Um tenant na Cell-2 tem deploy, logs, rollback e status funcionais
2. Uma falha na Cell-2 NAO afeta tenants na Cell-1 (blast radius isolation)
3. Observabilidade (metricas + logs) funciona cross-cell
4. Roteamento de trafego resolve corretamente para cada Cell

### O Que Muda

```
ANTES: 1 workload cluster, 1 ArgoCD, 1 Traefik, 1 Prometheus, 1 Promtail
DEPOIS: 2 workload clusters, cada um com ArgoCD + Traefik + Prometheus + Promtail
        Prometheus central federa metricas de ambas as Cells
        Loki recebe logs de ambas as Cells
        CF Worker roteia trafego para a Cell correta
```

### O Que NAO Muda

- Modelo de dados (cell_id ja existe desde Phase 0)
- PlacementEngine e CellRouter (ja implementados na Phase 1)
- Build plane (builds continuam no build cluster central)
- Deploy flow (API → build → manifest → Gitea push → ArgoCD sync)
- CLI commands existentes (nenhum comando muda, apenas `--cells` adicionado)
- API contracts publicos

---

## Tasks

### P2-1: Provision Cell-2 on DigitalOcean

**Classificacao:** T2 (infraestrutura — novo cluster em producao)

**Objetivo:** Usar o Cell Provisioner da Phase 1 para criar um segundo cluster DOKS
com todos os componentes necessarios para receber workloads.

**Arquivos:**
- `infra/pulumi/src/components/cells.ts` — invocar Cell Provisioner para Cell-2
- `infra/pulumi/Pulumi.dev.yaml` — adicionar config de Cell-2
- `infra/helm/argocd/` — values reutilizados para Cell-2
- `infra/helm/traefik/` — values reutilizados para Cell-2
- `infra/helm/cert-manager/` — values reutilizados para Cell-2

**Especificacao:**

1. Criar cluster DOKS `loopzero-cell-sfo3-02` via Cell Provisioner
   - Regiao: SFO3 (mesma que Cell-1 inicialmente)
   - Sizing: 2 nodes, `s-2vcpu-4gb` (medium, identico a Cell-1)
   - Kubernetes version: mesma que Cell-1 (v1.32.x)

2. Instalar ArgoCD via Helm no Cell-2
   - Configurado com acesso ao Gitea (mgmt cluster)
   - AppProject criado para Cell-2 tenants
   - Manual sync policy (identico a Cell-1)
   - RBAC: loopzero-api tem permissao de sync

3. Instalar Traefik via Helm no Cell-2
   - LoadBalancer IP separado do Cell-1
   - 2 replicas, HA config identica
   - JSON logging, correlation-id tracking

4. Instalar cert-manager no Cell-2
   - Let's Encrypt DNS-01 via Cloudflare
   - ClusterIssuer configurado

5. Registrar Cell-2 no CellRouter (Phase 1)
   - `CellConfig` com kubeconfig, ArgoCD URL/token, ingress domain
   - PlacementEngine passa a poder atribuir tenants a Cell-2

**Validacao:**
```bash
# Cell-2 cluster operacional
kubectl --context loopzero-cell-sfo3-02 get nodes
# Deve retornar 2 nodes Ready

# ArgoCD healthy
kubectl --context loopzero-cell-sfo3-02 -n argocd get pods
# Todos Running

# Traefik serving
kubectl --context loopzero-cell-sfo3-02 -n traefik get svc
# LoadBalancer com IP externo

# cert-manager ready
kubectl --context loopzero-cell-sfo3-02 -n cert-manager get pods
# Todos Running
```

**DoD:**
- [ ] Cluster DOKS `loopzero-cell-sfo3-02` criado via Pulumi
- [ ] ArgoCD instalado e configurado com acesso ao Gitea
- [ ] Traefik instalado com LoadBalancer IP proprio
- [ ] cert-manager instalado com ClusterIssuer Let's Encrypt DNS-01
- [ ] Cell-2 registrada no CellRouter com CellConfig completo
- [ ] `kubectl get nodes` retorna 2 nodes Ready no Cell-2
- [ ] ArgoCD healthy no Cell-2 (pods Running, app controller operacional)
- [ ] Traefik serving no Cell-2 (LoadBalancer com IP externo atribuido)
- [ ] Cell-1 inalterada (nenhuma modificacao nos recursos existentes)
- [ ] Pulumi stack atualizado sem erros

---

### P2-2: Prometheus Federation

**Classificacao:** T2 (observabilidade — nova arquitetura de metricas)

**Objetivo:** Cada Cell roda um Prometheus local. O Prometheus central no mgmt cluster
federa metricas de todas as Cells. Grafana visualiza dados agregados com breakdown por Cell.

**Arquivos:**
- `infra/helm/monitoring/values-cell.yaml` (novo) — values para Prometheus local na Cell
- `infra/helm/monitoring/values.yaml` — adicionar federation config ao Prometheus central
- `infra/pulumi/src/components/cells.ts` — instalar Prometheus em cada Cell
- Grafana dashboards atualizados para filtro por `cell_id`

**Especificacao:**

1. Prometheus local em cada Cell
   - Scrape targets: ArgoCD, Traefik, workload pods (metricas `loopzero_*`)
   - Retention: 2h (apenas buffer para federation)
   - Expose `/federate` endpoint (ClusterIP, acessivel pelo mgmt cluster via cross-cluster)
   - Resource limits: 512Mi memory, 250m CPU

2. Prometheus central (mgmt cluster) — federation config
   ```yaml
   scrape_configs:
     - job_name: 'federate-cell-sfo3-02'
       honor_labels: true
       metrics_path: '/federate'
       params:
         'match[]':
           - '{__name__=~"loopzero_.*"}'
           - '{__name__=~"argocd_.*"}'
           - '{__name__=~"traefik_.*"}'
           - '{__name__=~"kube_pod_.*"}'
           - '{__name__=~"container_.*"}'
       static_configs:
         - targets: ['prometheus-cell-sfo3-02:9090']
           labels:
             cell_id: 'cell-sfo3-02'
   ```

3. Grafana dashboards atualizados
   - Variavel `$cell_id` adicionada a todos os dashboards relevantes
   - Panel "Cell Overview" com tenant count, app count, deploy rate por Cell
   - Alertas continuam centralizados (PrometheusRules no Prometheus central)

4. Alerta de Cell health
   ```yaml
   - alert: LoopZeroCellPrometheusDown
     expr: up{job=~"federate-cell-.*"} == 0
     for: 5m
     labels:
       severity: critical
     annotations:
       summary: "Cell Prometheus {{ $labels.cell_id }} is down"
       description: "Federation scrape failing for 5+ minutes. Cell metrics are stale."
   ```

**Validacao:**
```bash
# Deploy app no Cell-2
loopzero deploy --project test-app

# Verificar metricas no Prometheus central
curl -s 'http://prometheus:9090/api/v1/query?query=loopzero_build_total{cell_id="cell-sfo3-02"}'
# Deve retornar dados

# Verificar Grafana
# Dashboard "Cell Overview" mostra ambas as Cells
```

**DoD:**
- [ ] Prometheus local instalado no Cell-2 via Helm
- [ ] Federation configurada no Prometheus central
- [ ] `honor_labels: true` garantindo labels originais preservados
- [ ] Label `cell_id` adicionado automaticamente a metricas federadas
- [ ] Grafana dashboards atualizados com variavel `$cell_id`
- [ ] Alerta `LoopZeroCellPrometheusDown` configurado
- [ ] Metricas de app deployada no Cell-2 visiveis no Grafana central
- [ ] Metricas de Cell-1 inalteradas
- [ ] PrometheusRules centralizados continuam funcionando

---

### P2-3: Loki Multi-Cell Logs

**Classificacao:** T2 (observabilidade + API change interno)

**Objetivo:** Logs de workloads em qualquer Cell devem ser acessiveis via `loopzero logs`.
Cada Cell roda Promtail enviando logs para o Loki central. A API resolve `cell_id` para
o datasource correto.

**Arquivos:**
- `infra/helm/promtail/values-cell.yaml` (novo) — values para Promtail na Cell
- `infra/pulumi/src/components/cells.ts` — instalar Promtail em cada Cell
- `api/internal/loki/client.go` — tornar cell-aware via CellRouter
- `api/internal/services/log_service.go` — resolver cell_id antes de query Loki

**Especificacao:**

1. Promtail em cada Cell
   - Envia logs para Loki central (mgmt cluster)
   - Label `cell_id` adicionado a todos os log streams: `cell_id: "cell-sfo3-02"`
   - Pipeline stages: tenant extraction, namespace extraction, cell_id injection

2. Loki central
   - Recebe logs de todas as Cells
   - Queries filtram por `cell_id` quando necessario
   - Retention policy identica para todas as Cells

3. API cell-aware (`api/internal/loki/client.go`)
   ```go
   // Antes (Phase 0/1):
   func (c *Client) QueryLogs(ctx context.Context, query string) ([]LogEntry, error)

   // Depois (Phase 2):
   func (c *Client) QueryLogs(ctx context.Context, cellID string, query string) ([]LogEntry, error) {
       // Se Loki e central, adicionar filtro cell_id ao query LogQL
       // Se Loki e per-Cell, CellRouter resolve URL do Loki da Cell
       enrichedQuery := fmt.Sprintf(`{cell_id="%s"} | %s`, cellID, query)
       return c.execute(ctx, enrichedQuery)
   }
   ```

4. Log service resolve cell_id
   ```go
   func (s *LogService) GetBuildLogs(ctx context.Context, tenantID, buildID string) ([]LogEntry, error) {
       tenant, err := s.tenantStore.Get(ctx, tenantID)
       if err != nil {
           return nil, err
       }
       return s.lokiClient.QueryLogs(ctx, tenant.CellID, buildLogQuery(buildID))
   }
   ```

**Validacao:**
```bash
# Deploy app no Cell-2
loopzero deploy --project test-app-cell2

# Buscar logs
loopzero logs --project test-app-cell2 --environment production
# Deve retornar logs corretos do Cell-2, nao do Cell-1
```

**DoD:**
- [ ] Promtail instalado no Cell-2 via Helm
- [ ] Label `cell_id` adicionado a todos os log streams do Cell-2
- [ ] Loki central recebendo logs de ambas as Cells
- [ ] `api/internal/loki/client.go` aceita `cellID` e filtra queries
- [ ] Log service resolve `cell_id` do tenant antes de query
- [ ] `loopzero logs` retorna logs corretos para app no Cell-2
- [ ] `loopzero logs` retorna logs corretos para app no Cell-1 (nao regressao)
- [ ] Testes unitarios para Loki client cell-aware
- [ ] Testes unitarios para log service com cell_id resolution

---

### P2-4: Cross-Cell Deploy E2E Validation

**Classificacao:** T2 (validacao end-to-end de fluxo critico)

**Objetivo:** Validar que o ciclo completo de deploy funciona para tenants na Cell-2.
Todo o fluxo deve ser identico ao Cell-1 do ponto de vista do usuario.

**Arquivos:**
- `infra/tests/e2e/` — novos test cases para Cell-2 deploy
- PlacementEngine config para atribuir tenant de teste a Cell-2

**Especificacao:**

1. Criar tenant atribuido a Cell-2
   - PlacementEngine atribui `cell_id = "cell-sfo3-02"` ao novo tenant
   - Verificar que `tenant.CellID` esta correto no banco

2. Deploy completo via CLI
   ```bash
   # Login como usuario de teste no Cell-2
   loopzero login --token $CELL2_TEST_TOKEN

   # Init projeto
   loopzero init --name test-cell2-app --framework node

   # Deploy
   loopzero deploy
   # Deve completar: upload → build → manifest → gitea push → ArgoCD sync (Cell-2) → pod Running
   ```

3. Validar cada etapa
   - Upload de source bundle: Gitea (central, nao muda)
   - Build: Argo Workflows (build cluster central, nao muda)
   - Manifest generation: gitops push para Gitea (nao muda)
   - ArgoCD sync: Cell-2 ArgoCD (NAO Cell-1 ArgoCD)
   - Pod running: Cell-2 workload cluster
   - Health check: endpoint acessivel via Cell-2 Traefik

4. Validar operacoes pos-deploy
   ```bash
   # Status
   loopzero status --project test-cell2-app
   # Deve mostrar status correto

   # Logs
   loopzero logs --project test-cell2-app
   # Deve retornar logs do Cell-2

   # Rollback
   loopzero rollback --project test-cell2-app
   # Deve funcionar no Cell-2
   ```

5. Verificar zero impacto no Cell-1
   - Deploy em Cell-1 durante operacoes de Cell-2
   - Nenhuma degradacao de performance
   - Nenhum erro nos logs de Cell-1

**DoD:**
- [ ] Tenant de teste criado com `cell_id = "cell-sfo3-02"`
- [ ] `loopzero deploy` completa com sucesso no Cell-2
- [ ] Pod running no Cell-2 workload cluster (nao no Cell-1)
- [ ] ArgoCD sync executado no Cell-2 ArgoCD (nao no Cell-1)
- [ ] Health check endpoint acessivel via Cell-2 Traefik
- [ ] `loopzero status` retorna dados corretos para app no Cell-2
- [ ] `loopzero logs` retorna logs corretos para app no Cell-2
- [ ] `loopzero rollback` funciona para app no Cell-2
- [ ] Deploy simultaneo no Cell-1 funciona sem degradacao
- [ ] Teste E2E automatizado cobrindo deploy completo no Cell-2
- [ ] Teste E2E verificando zero impacto no Cell-1

---

### P2-5: Blast Radius Isolation Test

**Classificacao:** T2 (validacao critica de isolamento)

**Objetivo:** PROVAR que uma falha catastrofica na Cell-2 nao afeta operacoes na Cell-1.
Este e o teste mais importante da Phase 2 — sem ele, multi-cell nao tem valor.

**Arquivos:**
- `infra/tests/e2e/blast_radius_test.go` (novo) — teste de isolamento
- `infra/tests/chaos/cell_isolation_test.go` (novo) — chaos test

**Especificacao:**

1. Setup: apps em ambas as Cells
   ```
   Cell-1: project-a, project-b, project-c (3 projetos, 3 tenants)
   Cell-2: project-d, project-e, project-f (3 projetos, 3 tenants)
   Todos deployed e Running
   ```

2. Falha induzida: matar ArgoCD no Cell-2
   ```bash
   kubectl --context loopzero-cell-sfo3-02 -n argocd scale deployment argocd-server --replicas=0
   kubectl --context loopzero-cell-sfo3-02 -n argocd scale deployment argocd-application-controller --replicas=0
   kubectl --context loopzero-cell-sfo3-02 -n argocd scale deployment argocd-repo-server --replicas=0
   ```

3. Verificar Cell-1 continua operacional
   ```bash
   # Deploy novo no Cell-1 — DEVE funcionar
   loopzero deploy --project project-a
   # Sucesso: build → manifest → ArgoCD sync (Cell-1) → pod running

   # Status de apps no Cell-1 — DEVE funcionar
   loopzero status --project project-a
   # Healthy

   # Logs de apps no Cell-1 — DEVE funcionar
   loopzero logs --project project-a
   # Retorna logs normais
   ```

4. Verificar Cell-2 falha graciosamente
   ```bash
   # Deploy no Cell-2 — DEVE falhar com erro claro
   loopzero deploy --project project-d
   # Erro: "Deploy failed: workload cluster unavailable. Your build completed successfully and will be deployed when the cluster recovers."
   # Exit code: 3 (deploy failure)

   # Circuit breaker DEVE abrir para Cell-2 APENAS
   # Cell-1 circuit breaker DEVE permanecer fechado
   ```

5. Verificar recovery automatica
   ```bash
   # Restaurar ArgoCD no Cell-2
   kubectl --context loopzero-cell-sfo3-02 -n argocd scale deployment argocd-server --replicas=1
   kubectl --context loopzero-cell-sfo3-02 -n argocd scale deployment argocd-application-controller --replicas=1
   kubectl --context loopzero-cell-sfo3-02 -n argocd scale deployment argocd-repo-server --replicas=1

   # Aguardar recovery
   # Circuit breaker DEVE fechar para Cell-2

   # Releases pendentes DEVEM ser resolvidas
   # ArgoCD sync reconcilia estado desejado automaticamente
   ```

6. Verificar metricas durante falha
   - `LoopZeroCellPrometheusDown` alerta NAO dispara (Prometheus e separado do ArgoCD)
   - `loopzero_deploy_failures_total{cell_id="cell-sfo3-02"}` incrementa
   - `loopzero_deploy_total{cell_id="cell-default"}` continua normal
   - Circuit breaker state visivel em metricas

**DoD:**
- [ ] 3 projetos deployed no Cell-1, 3 no Cell-2
- [ ] ArgoCD killed no Cell-2 (server, controller, repo-server)
- [ ] Deploy no Cell-1 continua funcionando durante falha do Cell-2
- [ ] Status e logs no Cell-1 funcionam durante falha do Cell-2
- [ ] Deploy no Cell-2 falha com mensagem clara (sem termos internos)
- [ ] Exit code 3 (deploy failure) para deploys no Cell-2 durante falha
- [ ] Circuit breaker abre para Cell-2 APENAS (Cell-1 circuito fechado)
- [ ] Recovery: ArgoCD restaurado no Cell-2 resolve estado pendente
- [ ] Circuit breaker fecha apos recovery do Cell-2
- [ ] Metricas refletem falha isolada no Cell-2
- [ ] Teste automatizado (Go, Ginkgo) cobrindo todo o cenario
- [ ] Resultado documentado como prova de blast radius isolation

---

### P2-6: Cloudflare Worker Multi-Cell Routing

**Classificacao:** T2 (roteamento de trafego — mudanca em edge proxy)

**Objetivo:** O CF Worker deve resolver para qual Cell rotear cada request. Tenants em
Cell-2 devem ter trafego direcionado ao Traefik do Cell-2, nao do Cell-1.

**Arquivos:**
- `infra/cloudflare/worker.mjs` — logica multi-cell
- `api/internal/cfkv/client.go` — escrever `cellId` e `cellIP` no KV
- `infra/pulumi/src/providers/cloudflare/` — KV schema update

**Especificacao:**

1. KV routing table estendida
   ```json
   // Antes (single-cell):
   {
     "buildId": "bld-abc123",
     "s3Prefix": "tenant-1/project-a/bld-abc123"
   }

   // Depois (multi-cell):
   {
     "buildId": "bld-abc123",
     "s3Prefix": "tenant-1/project-a/bld-abc123",
     "cellId": "cell-sfo3-02",
     "cellOrigin": "https://cell-sfo3-02.internal.loopzero.example"
   }
   ```

2. CF Worker routing logic
   ```javascript
   // Static apps: rota para DO Spaces CDN (cell-independent)
   // O s3Prefix ja e unico por build, nao importa a Cell
   if (routeEntry.s3Prefix) {
     return fetchFromSpaces(routeEntry.s3Prefix, request);
   }

   // Dynamic apps: rota para Cell-specific Traefik
   // Usa cellOrigin para determinar o upstream
   if (routeEntry.cellOrigin) {
     return fetchFromCell(routeEntry.cellOrigin, request);
   }

   // Fallback: Cell default (backward compatibility)
   return fetchFromCell(DEFAULT_CELL_ORIGIN, request);
   ```

3. CF Tunnel per Cell (dynamic apps)
   - Opcao A: CF Tunnel separado por Cell (cada Cell tem seu tunnel)
   - Opcao B: DNS A record separado por Cell (cada Cell tem IP publico)
   - Recomendado: CF Tunnel per Cell (melhor seguranca, zero IPs publicos)
   - Configuracao: `cloudflared tunnel` no Cell-2 apontando para Cell-2 Traefik

4. API atualiza KV com cell info
   ```go
   // Ao ativar release, incluir cellId e cellOrigin no KV
   func (s *StaticDelivery) ActivateRelease(ctx context.Context, release *Release) error {
       tenant, _ := s.tenantStore.Get(ctx, release.TenantID)
       cellConfig, _ := s.cellRouter.Resolve(ctx, tenant.CellID)

       kvEntry := map[string]string{
           "buildId":    release.BuildID,
           "s3Prefix":   release.S3Prefix,
           "cellId":     tenant.CellID,
           "cellOrigin": cellConfig.IngressDomain,
       }
       return s.cfkvClient.Put(ctx, release.Subdomain, kvEntry)
   }
   ```

**Validacao:**
```bash
# Request para app no Cell-1
curl -v https://app-cell1.dev.loopzero.example/health
# Headers mostram roteamento para Cell-1 Traefik

# Request para app no Cell-2
curl -v https://app-cell2.dev.loopzero.example/health
# Headers mostram roteamento para Cell-2 Traefik

# Static app (ambas as Cells)
curl -v https://static-app.dev.loopzero.example/
# Sempre rota para DO Spaces CDN (cell-independent)
```

**DoD:**
- [ ] KV schema estendido com `cellId` e `cellOrigin`
- [ ] CF Worker atualizado para rotear dynamic apps por Cell
- [ ] CF Worker mantem roteamento existente para static apps (cell-independent)
- [ ] CF Tunnel configurado para Cell-2 (ou DNS A record)
- [ ] API escreve `cellId` e `cellOrigin` no KV ao ativar release
- [ ] Request para app no Cell-1 roteado para Cell-1 Traefik
- [ ] Request para app no Cell-2 roteado para Cell-2 Traefik
- [ ] Static apps continuam funcionando (sem regressao)
- [ ] Fallback para Cell default quando `cellOrigin` ausente
- [ ] Validado em DigitalOcean com requests reais

---

### P2-7: Cell Status in CLI (`loopzero status --cells`)

**Classificacao:** T2 (API contract + CLI output)

**Objetivo:** Operadores da plataforma podem visualizar o estado de todas as Cells via CLI.
Nao e um comando para tenants — e para administracao da plataforma.

**Arquivos:**
- `api/internal/routes/internal.go` — endpoint `GET /internal/cells`
- `api/internal/services/cell_service.go` (novo) — logica de agregacao
- `cli/src/commands/status.ts` — adicionar flag `--cells`
- `cli/tests/unit/status.test.ts` — testes para `--cells`

**Especificacao:**

1. API endpoint `GET /internal/cells`
   - Rota interna (nao exposta publicamente, requer auth de operador)
   - Response:
     ```json
     {
       "cells": [
         {
           "cell_id": "cell-default",
           "region": "sfo3",
           "tenant_count": 45,
           "app_count": 120,
           "cluster_health": "Ready",
           "deploy_health": "Healthy",
           "created_at": "2026-01-15T10:00:00Z"
         },
         {
           "cell_id": "cell-sfo3-02",
           "region": "sfo3",
           "tenant_count": 12,
           "app_count": 35,
           "cluster_health": "Ready",
           "deploy_health": "Healthy",
           "created_at": "2026-03-31T14:00:00Z"
         }
       ]
     }
     ```

2. Cell service — agregacao de dados
   ```go
   type CellService struct {
       tenantStore  db.TenantStore
       cellRouter   placement.Router
       // Metricas de health vem do Prometheus ou do health check direto
   }

   func (s *CellService) ListCells(ctx context.Context) ([]CellStatus, error) {
       cells, err := s.cellRouter.ListCells(ctx)
       if err != nil {
           return nil, err
       }

       var result []CellStatus
       for _, cell := range cells {
           tenantCount, _ := s.tenantStore.CountByCell(ctx, cell.CellID)
           appCount, _ := s.tenantStore.CountAppsByCell(ctx, cell.CellID)
           health := s.checkCellHealth(ctx, cell)

           result = append(result, CellStatus{
               CellID:        cell.CellID,
               Region:        extractRegion(cell.CellID),
               TenantCount:   tenantCount,
               AppCount:      appCount,
               ClusterHealth: health.Cluster,
               DeployHealth:  health.Deploy,
               CreatedAt:     cell.CreatedAt,
           })
       }
       return result, nil
   }
   ```

3. CLI `loopzero status --cells`
   - Flag `--cells` ativada apenas para operadores (token com role adequado)
   - Output humano:
     ```
     CELL            TENANTS  APPS   ARGOCD   CLUSTER
     cell-default    45       120    Healthy  Ready
     cell-sfo3-02    12       35     Healthy  Ready
     ```
   - Output JSON (`--json --cells`):
     ```json
     {
       "ok": true,
       "cells": [
         {
           "cell_id": "cell-default",
           "region": "sfo3",
           "tenant_count": 45,
           "app_count": 120,
           "cluster_health": "Ready",
           "deploy_health": "Healthy"
         }
       ]
     }
     ```
   - Exit code 0 (sucesso) ou 4 (auth error se nao e operador)

4. Termos internos proibidos na output
   - "ArgoCD" nunca aparece — coluna chama "DEPLOY" (deploy_health)
   - "cluster" e aceitavel (nao e termo interno do LoopZero)
   - Nenhum termo da lista proibida (Rule 3)

**DoD:**
- [ ] Endpoint `GET /internal/cells` implementado
- [ ] Response JSON segue schema definido acima
- [ ] CellService agrega tenant count, app count, health por Cell
- [ ] `loopzero status --cells` mostra tabela formatada (output humano)
- [ ] `loopzero status --cells --json` retorna JSON valido (spec §0.3 contract)
- [ ] Flag `--cells` requer auth de operador (exit code 4 se nao autorizado)
- [ ] Nenhum termo interno na output (sem "ArgoCD", sem "ArgoCD health")
- [ ] Testes unitarios CLI: `--cells` success, `--cells` sem auth, `--cells --json` shape
- [ ] Testes unitarios API: `GET /internal/cells` success, unauthorized
- [ ] Testes unitarios CellService: aggregation logic

---

## Checklist Global — Phase 2

### Gate: Phase 2 Complete

- [ ] P2-1: Cell-2 provisionada e operacional no DigitalOcean
- [ ] P2-2: Prometheus federation funcional (metricas de ambas as Cells visiveis)
- [ ] P2-3: Loki multi-cell funcional (`loopzero logs` funciona em ambas as Cells)
- [ ] P2-4: Deploy E2E validado no Cell-2 (deploy, status, logs, rollback)
- [ ] P2-5: Blast radius isolation PROVADO (Cell-2 down, Cell-1 operacional)
- [ ] P2-6: CF Worker multi-cell routing funcional
- [ ] P2-7: `loopzero status --cells` operacional
- [ ] Todos: `go build ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `go test ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `cd cli && npm test` passa (498+ testes)
- [ ] Todos: `task validate:manifests` passa
- [ ] Todos: `task lint` passa
- [ ] Zero regressao em testes existentes
- [ ] Changelog atualizado
- [ ] Validado em DigitalOcean (NAO apenas testes locais)

### O Que NAO Fazer na Phase 2

- [ ] NAO implementar rebalancing de tenants entre Cells
- [ ] NAO implementar auto-scaling de Cells
- [ ] NAO implementar Cell decommissioning
- [ ] NAO implementar multi-region (ambas as Cells estao em SFO3)
- [ ] NAO modificar build plane (builds continuam no cluster central)
- [ ] NAO criar mais que 2 Cells (validacao minima primeiro)
- [ ] NAO expor `--cells` para usuarios nao-operadores

---

## Trigger para Phase 3

Phase 3 (Multi-Region) inicia quando QUALQUER destes criterios for atingido:

1. `loopzero_cell_tenant_count{cell_id="cell-sfo3-02"} > 200` (Cell-2 precisando de terceira Cell)
2. Requisito de negocio para latencia < 100ms em regiao diferente de SFO3
3. Requisito regulatorio de data residency (dados em regiao especifica)
4. Decisao de negocio de oferecer Cells regionais como feature premium

---

## Ordem de Execucao

```
P2-1 (Provision Cell-2)
  |
  ├── P2-2 (Prometheus Federation)  ──┐
  ├── P2-3 (Loki Multi-Cell)         ├── parallelizable
  └── P2-6 (CF Worker Routing)      ──┘
         |
         v
      P2-4 (Deploy E2E Validation)
         |
         v
      P2-5 (Blast Radius Isolation Test)
         |
         v
      P2-7 (Cell Status CLI)
```

P2-1 e pre-requisito para tudo. P2-2, P2-3, P2-6 podem ser feitas em paralelo.
P2-4 depende de P2-2, P2-3, P2-6 estarem prontas (precisa de metricas, logs, e roteamento).
P2-5 depende de P2-4 (precisa de apps deployed em ambas as Cells).
P2-7 pode ser feita apos P2-1, mas valida melhor apos P2-5 (com dados reais).

---

## Riscos e Mitigacoes

| Risco | Impacto | Mitigacao |
|-------|---------|-----------|
| Cross-cluster networking (mgmt → Cell-2) | Federation e Promtail falham | Testar conectividade antes de instalar. CF Tunnel ou VPC peering. |
| ArgoCD no Cell-2 sem acesso ao Gitea | Sync falha, deploy impossivel | Validar credenciais Gitea antes de criar AppProject. |
| CF Worker routing incorreto | Trafego vai para Cell errada | Fallback para Cell default. Smoke test obrigatorio. |
| Custo de infraestrutura dobra | Budget impactado | Cell-2 com sizing minimo. Desligar quando nao estiver testando. |
| Circuit breaker abre para Cell errada | Cell saudavel bloqueada | Circuit breaker DEVE ser per-cell_id. Teste P2-5 valida isso. |

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `docs/adr/0008-cell-based-architecture.md` | ADR que aprova esta arquitetura |
| `docs/plans/cell-architecture-phase0.md` | Phase 0 — fundacao (cell_id, interfaces) |
| `docs/meetings/records/MEET-2026-03-31-024.md` | Reuniao que gerou o ADR |
| `infra/cloudflare/worker.mjs` | CF Worker atual (single-cell) |
| `api/internal/loki/client.go` | Loki client atual (single-cell) |
| `api/internal/placement/` | PlacementEngine e CellRouter (Phase 0/1) |
