# Plano: Cell Architecture — Phase 3 (Cell Automation)

> **Objetivo:** Automatizar provisionamento de Cells e rebalanceamento de tenants.
> Adicionar uma Cell se torna uma unica chamada de API ou trigger automatico.
>
> **Escopo:** 7 tasks. Provisionamento, migracao, monitoramento, decommission, placement avancado, cross-cell queries, runbook.
>
> **Classificacao:** T3 (arquitetura, multi-cluster, infra critica)
>
> **Trigger:** `loopzero_cell_tenant_count > 500` em qualquer Cell
>
> **Duracao:** 3 sprints
>
> **Principio:** "O operador nao deve ser o gargalo. A plataforma escala sozinha."

---

## Contexto

Com Phases 0-2 completas, o LoopZero suporta multiplas Cells com roteamento via CellRouter
e placement via PlacementEngine. Porem, criar uma Cell nova requer intervencao manual
(Pulumi up, configurar ClusterCredential, registrar no ArgoCD). Phase 3 elimina toda
intervencao manual — Cells sao provisionadas e decommissionadas automaticamente.

### O Que Muda

```
ANTES: Operador cria Cell manualmente (Pulumi CLI + registrar CR + configurar ArgoCD)
DEPOIS: API detecta Cell cheia → provisiona nova Cell automaticamente → tenants migrados sem downtime
```

### O Que NAO Muda

- Deploy flow identico (dentro de uma Cell)
- CLI identica para desenvolvedores
- API contracts de deploy identicos
- State machines identicas

---

## Tasks

### P3-1: Auto Cell Provisioning

**Classificacao:** T3 (infraestrutura automatizada, cria clusters)

**Arquivos:**
- `api/internal/services/cell_provisioner.go` (novo)
- `api/internal/routes/internal_cells.go` (novo)
- `infra/pulumi/src/components/cell-template.ts` (novo)

**Mecanismo:**
- Quando `loopzero_cell_tenant_count > max_tenants_per_cell`, PlacementEngine detecta que
  todas as Cells estao cheias durante `AssignCell()`
- Trigger pode ser automatico ou via API: `POST /internal/cells/provision`
- Usa Pulumi Automation API (TypeScript SDK) para criar infraestrutura da Cell
- Cell naming: `cell-{region}-{sequence}` (e.g., `cell-sfo3-03`)

**O que o provisioner cria:**
1. DOKS cluster (DigitalOcean Kubernetes) com sizing configuravel
2. ArgoCD instalado e configurado (Helm chart versionado)
3. Traefik instalado (ingress)
4. cert-manager instalado (TLS)
5. ClusterCredential CR no mgmt cluster
6. CellConfig registrado no CellRouter (PG ou ConfigMap)
7. DNS wildcard para a Cell

**Approval gate:**
- Modo manual: `POST /internal/cells/provision` com body `{"region": "sfo3"}`
- Modo auto: se `auto_provision_enabled = true` em config, provisiona sem aprovacao
- Default: manual (seguranca)

**Tempo estimado:** ~10 minutos (DOKS provisioning e o gargalo)

**Impacto:**
- Novo endpoint interno (nao exposto na CLI publica)
- Novo service com dependencia em Pulumi Automation API
- Requer credenciais DO (DIGITALOCEAN_TOKEN) acessiveis ao API server

**DoD:**
- [ ] `POST /internal/cells/provision` cria Cell completa
- [ ] Cell provisionada em < 15 minutos do trigger
- [ ] Zero steps manuais apos o trigger
- [ ] ClusterCredential CR criado automaticamente
- [ ] ArgoCD + Traefik + cert-manager instalados na nova Cell
- [ ] DNS wildcard configurado
- [ ] Metrica `loopzero_cell_provisioning_duration_seconds` emitida
- [ ] Teste de integracao: provisionar Cell e verificar que aceita deploys
- [ ] Validado em DigitalOcean

---

### P3-2: Tenant Rebalancing

**Classificacao:** T3 (migracao cross-cell, risco de downtime)

**Arquivos:**
- `api/internal/services/tenant_migrator.go` (novo)
- `api/internal/routes/internal_cells.go` (adicionar endpoint)
- `api/internal/db/migrations/NNNN_add_tenant_migration_state.up.sql` (novo)
- `pkg/statemachine/tenant.go` (adicionar estado `Migrating`)

**Processo de migracao (blue-green at Cell level):**

```
1. Mark tenant as `migrating` in PG (prevents new deploys)
2. Sync all Gitea manifests to Cell-B ArgoCD
3. Wait for Cell-B ArgoCD healthy (all apps Synced + Healthy)
4. Update DNS/routing (CF KV: tenant subdomains → Cell-B)
5. Wait for DNS propagation (TTL-bound, tipicamente 30s)
6. Remove tenant resources from Cell-A ArgoCD
7. Update tenant.cell_id in PG → Cell-B
8. Mark tenant as `active`
```

**Durante a migracao:**
- Ambas as Cells servem trafego (blue-green)
- Novos deploys bloqueados (estado `migrating` impede `withEnvironmentLock`)
- Rollback: se Cell-B falhar health check no passo 3, revert para Cell-A
  - Remove recursos de Cell-B
  - Manter tenant em Cell-A
  - Marcar como `active` novamente

**TTL consideration:**
- DNS TTL padrao: 30s (Cloudflare)
- Durante migracao, ambas Cells respondem por no maximo 2x TTL
- Nenhuma requisicao perdida se ambas Cells tem o app healthy

**State machine change (tenant):**
```
Active → Migrating → Active
                   → Active (rollback se Cell-B falhar)
```

**API:** `POST /internal/cells/migrate` com body:
```json
{
  "tenant_id": "t-xxx",
  "target_cell_id": "cell-sfo3-03"
}
```

**DoD:**
- [ ] Endpoint de migracao implementado
- [ ] Estado `migrating` no tenant state machine
- [ ] Deploys bloqueados durante migracao
- [ ] Migracao completa com < 30s downtime (DNS TTL bound)
- [ ] Rollback automatico se Cell-B falhar health check
- [ ] Teste: migrar tenant entre 2 Cells e verificar que app continua acessivel
- [ ] Teste: deploy rejeitado durante migracao (estado `migrating`)
- [ ] Teste: rollback se Cell destino falhar
- [ ] Metrica `loopzero_tenant_migration_duration_seconds` emitida
- [ ] Validado em DigitalOcean

---

### P3-3: Cell Health Monitor

**Classificacao:** T2 (observability, alertas)

**Arquivos:**
- `api/internal/services/cell_health_monitor.go` (novo)
- `api/internal/metrics/metrics.go` (adicionar metricas)
- `infra/helm/monitoring/values.yaml` (adicionar alertas)

**O que o monitor verifica periodicamente (a cada 30s):**
- K8s API do cluster da Cell: reachable?
- Nodes: todos Ready?
- ArgoCD: application-controller rodando?
- Capacidade: tenant count atual vs max configurado
- Performance: ArgoCD reconcile latency (via ArgoCD metrics)
- Deploy success rate (via metricas do LoopZero)

**Metricas emitidas:**
```
loopzero_cell_health{cell_id="cell-sfo3-01"}              # 0=unhealthy, 1=healthy
loopzero_cell_node_count{cell_id="cell-sfo3-01"}           # numero de nodes
loopzero_cell_argocd_reconcile_latency_p95{cell_id="..."}  # latencia p95 do ArgoCD
loopzero_cell_deploy_success_rate{cell_id="..."}           # taxa de sucesso de deploys
```

**Alertas:**
```yaml
- alert: LoopZeroCellUnhealthy
  expr: loopzero_cell_health == 0
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Cell {{ $labels.cell_id }} is unhealthy"
    description: "K8s API unreachable or ArgoCD down for 5+ minutes."

- alert: LoopZeroCellCapacityHigh
  expr: loopzero_cell_tenant_count / loopzero_cell_max_tenants > 0.8
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Cell {{ $labels.cell_id }} at {{ $value | humanizePercentage }} capacity"
    description: "Consider provisioning a new Cell or rebalancing tenants."
```

**Dashboard:**
- Grafana dashboard "Cell Overview" com painel por Cell
- Health status, tenant count, deploy rate, ArgoCD latency

**DoD:**
- [ ] CellHealthMonitor implementado com periodic check (30s)
- [ ] Metricas `loopzero_cell_health`, `loopzero_cell_node_count` emitidas
- [ ] Alertas `LoopZeroCellUnhealthy` e `LoopZeroCellCapacityHigh` configurados
- [ ] Dashboard Grafana com visao por Cell
- [ ] Teste unitario: health check retorna 0 quando K8s API inalcancavel
- [ ] Teste unitario: health check retorna 1 quando tudo OK
- [ ] `loopzero_cell_health` visivel no `/metrics` endpoint
- [ ] Validado em DigitalOcean

---

### P3-4: Cell Decommissioning

**Classificacao:** T3 (destroi infraestrutura)

**Arquivos:**
- `api/internal/services/cell_provisioner.go` (adicionar decommission)
- `api/internal/routes/internal_cells.go` (adicionar endpoint)

**Processo de decommission:**

```
1. Verify zero tenants assigned to Cell (SELECT COUNT(*) FROM tenants WHERE cell_id = ?)
2. If tenant_count > 0 → REFUSE with error (safety gate)
3. Delete all ArgoCD Applications in the Cell (should be empty, safety check)
4. Destroy K8s cluster via Pulumi Automation API
5. Remove ClusterCredential CR from mgmt cluster
6. Remove Cell from CellRouter registry
7. Remove DNS wildcard entries
8. Update metrics (remove cell_id labels)
```

**Safety gates:**
- Recusa decommission se `tenant_count > 0`
- Recusa decommission da `cell-default` (Cell primaria)
- Requires explicit confirmation: `POST /internal/cells/{cellID}/decommission`
  com header `X-Confirm-Decommission: true`

**API:** `POST /internal/cells/{cellID}/decommission`

**DoD:**
- [ ] Endpoint de decommission implementado
- [ ] Safety gate: recusa se tenants > 0
- [ ] Safety gate: recusa decommission de `cell-default`
- [ ] Cluster destruido via Pulumi Automation API
- [ ] ClusterCredential CR removido
- [ ] Cell removida do CellRouter
- [ ] DNS removido
- [ ] Teste: decommission de Cell vazia sucede
- [ ] Teste: decommission de Cell com tenants falha
- [ ] Teste: decommission de cell-default falha
- [ ] Metrica `loopzero_cell_decommissioned_total` emitida
- [ ] Validado em DigitalOcean

---

### P3-5: PlacementEngine — Affinity and Constraints

**Classificacao:** T2 (placement logic, impacta tenant assignment)

**Arquivos:**
- `api/internal/placement/engine.go` (refactor — adicionar constraints)
- `api/internal/placement/constraints.go` (novo)
- `api/internal/db/migrations/NNNN_add_cell_labels.up.sql` (novo)

**Placement rules:**

| Rule | Descricao | Exemplo |
|------|-----------|---------|
| Region affinity | Colocar tenant perto dos users (latencia) | `region: sfo3` |
| Anti-affinity | Espalhar envs do tenant entre Cells (HA, opt-in) | `spread: environments` |
| Capacity constraint | Nunca exceder `max_tenants_per_cell` | Hard limit |
| Label-based | Cells com labels para tenant tiers | `cell.tier=premium` |

**Modelo de dados:**
```sql
-- Cell labels (key-value pairs)
CREATE TABLE cell_labels (
    cell_id TEXT NOT NULL,
    label_key TEXT NOT NULL,
    label_value TEXT NOT NULL,
    PRIMARY KEY (cell_id, label_key)
);

-- Tenant placement preferences
ALTER TABLE tenants ADD COLUMN placement_preferences JSONB DEFAULT '{}';
```

**PlacementEngine.AssignCell() atualizado:**
```go
func (e *ConstraintEngine) AssignCell(ctx context.Context, opts PlacementOpts) (string, error) {
    cells, err := e.router.ListCells(ctx)
    // 1. Filter by region affinity
    // 2. Filter by label constraints
    // 3. Filter by capacity (exclude full Cells)
    // 4. Sort by least-loaded
    // 5. Return best candidate
}
```

**Configuracao:** via Pulumi stack config ou API `PUT /internal/cells/{cellID}/labels`

**DoD:**
- [ ] ConstraintEngine implementado com 4 regras
- [ ] Region affinity funcional
- [ ] Capacity constraint impede placement em Cell cheia
- [ ] Label-based filtering funcional
- [ ] Migration para cell_labels criada
- [ ] Teste: placement respeita region affinity
- [ ] Teste: placement respeita capacity constraint
- [ ] Teste: placement com 3 Cells e affinity rules
- [ ] Teste: placement falha se nenhuma Cell disponivel (trigger auto-provisioning)
- [ ] `go test ./api/...` passa

---

### P3-6: Cross-Cell API Queries

**Classificacao:** T2 (API contract, agrega dados de multiplas Cells)

**Arquivos:**
- `api/internal/services/cross_cell_aggregator.go` (novo)
- `api/internal/routes/status.go` (modificar para suportar cross-cell)

**Mecanismo:**
- `loopzero status --all` (operador view) query todas as Cells
- API faz fan-out: envia request para cada Cell em paralelo
- Cada Cell responde com seus dados locais
- API agrega e retorna resultado unificado

**Fan-out pattern:**
```go
func (a *Aggregator) QueryAllCells(ctx context.Context, query CellQuery) ([]CellResult, error) {
    cells, _ := a.router.ListCells(ctx)
    results := make(chan CellResult, len(cells))

    for _, cell := range cells {
        go func(c CellConfig) {
            ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
            defer cancel()
            result, err := a.queryCell(ctx, c, query)
            results <- CellResult{CellID: c.CellID, Data: result, Error: err}
        }(cell)
    }
    // Collect results, return partial if some Cells timeout
}
```

**Regras:**
- Timeout: 5s por Cell
- Resultados parciais sao aceitaveis (se Cell-3 timeout, retorna dados de Cell-1 e Cell-2)
- Resposta inclui campo `partial: true` se alguma Cell falhou
- Paginacao: cada Cell pagina independente, client-side merge

**Impacto em `--json` output:**
- Novo campo `cells` no response de `loopzero status --all` (campo novo — OK por Rule 5)
- Novo campo `partial` indicando se resultado e parcial

**DoD:**
- [ ] CrossCellAggregator implementado com fan-out paralelo
- [ ] Timeout de 5s por Cell
- [ ] Resultados parciais retornados quando Cell falha
- [ ] Campo `partial` no response JSON
- [ ] Teste: query com 3 Cells retorna dados agregados
- [ ] Teste: 1 Cell timeout → resultado parcial com `partial: true`
- [ ] Teste: todas Cells timeout → erro claro
- [ ] `go test ./api/...` passa

---

### P3-7: Cell Runbook

**Classificacao:** T0 (documentacao)

**Arquivo:** `docs/runbooks/cell-operations.md` (novo)

**Conteudo:**
1. Como provisionar uma nova Cell manualmente
2. Como migrar um tenant entre Cells
3. Como decommissionar uma Cell
4. Como lidar com falha de uma Cell (failover)
5. Como escalar uma Cell (adicionar nodes)
6. Como verificar saude de todas as Cells
7. Como consultar metricas de Cell
8. Troubleshooting: Cell provisionada mas nao recebe deploys
9. Troubleshooting: migracao presa no estado `migrating`

**Formato:** cada operacao com:
- Pre-requisitos
- Comandos CLI/API
- Verificacao de sucesso
- Rollback se falhar

**DoD:**
- [ ] Runbook cobre todas as 9 operacoes listadas
- [ ] Cada operacao tem comandos concretos
- [ ] Cada operacao tem verificacao de sucesso
- [ ] Cada operacao tem rollback/recovery
- [ ] Revisado por segundo engenheiro

---

## Checklist Global — Phase 3

### Gate: Phase 3 Complete

- [ ] P3-1: Cells provisionadas automaticamente quando trigger de capacidade dispara
- [ ] P3-2: Tenants migrados entre Cells com < 30s downtime
- [ ] P3-3: Monitoramento de saude de Cell com alertas
- [ ] P3-4: Cells vazias decommissionadas corretamente
- [ ] P3-5: PlacementEngine respeita affinity, capacity, e labels
- [ ] P3-6: Cross-cell API queries funcionam com resultados parciais
- [ ] P3-7: Runbook completo
- [ ] Todos: `go build ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `go test ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `task validate:manifests` passa
- [ ] Todos: testes de integracao com 3+ Cells passam
- [ ] Changelog atualizado
- [ ] Validado em DigitalOcean

### O Que NAO Fazer na Phase 3

- [ ] NAO implementar ApplicationSets (Phase 4)
- [ ] NAO otimizar ArgoCD por Cell (Phase 4)
- [ ] NAO implementar Cell template versioning (Phase 4)
- [ ] NAO fazer load test de 50k apps (Phase 4)
- [ ] NAO modificar deploy flow do desenvolvedor (transparente)
- [ ] NAO expor operacoes de Cell na CLI publica (interno apenas)

---

## Trigger para Phase 4

Phase 4 inicia quando QUALQUER destes criterios for atingido:

1. `loopzero_cell_tenant_count > 1000` em qualquer Cell
2. ArgoCD reconcile latency p95 > 30s em qualquer Cell
3. Total de ArgoCD Applications cross-cell > 10k
4. Decisao de negocio de otimizar custo operacional por Cell

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `docs/adr/0008-cell-based-architecture.md` | ADR que aprova esta arquitetura |
| `docs/plans/cell-architecture-phase0.md` | Phase 0 — foundation (cell_id, interfaces) |
| `docs/adr/0006-eliminate-state-duality.md` | State ownership model (PostgreSQL owns) |
| `infra/pulumi/src/components/` | Pulumi components existentes (referencia para cell-template) |
| `api/internal/placement/engine.go` | PlacementEngine (criado em Phase 0) |
| `api/internal/placement/router.go` | CellRouter (criado em Phase 0) |
