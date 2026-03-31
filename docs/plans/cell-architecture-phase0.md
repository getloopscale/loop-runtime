# Plano: Cell Architecture — Phase 0 (Foundation)

> **Objetivo:** Preparar o LoopZero para multi-cell sem quebrar nada. Adicionar `cell_id`
> ao modelo de tenant, criar interfaces PlacementEngine e CellRouter como passthroughs,
> e garantir que o sistema funciona identicamente ao atual (Cell unica = comportamento atual).
>
> **Escopo:** 6 tasks, todas T1 (local, single-component). Zero mudanca comportamental.
>
> **Classificacao:** T2 (adiciona campo ao modelo — migration DB)
>
> **Status:** APPROVED — ADR-0008
>
> **Principio:** "A melhor migracao e aquela que nao muda nada hoje mas permite tudo amanha."

---

## Contexto

ADR-0008 define Cell-Based Architecture como estrategia de escala para 50k+ apps.
Phase 0 adiciona a FUNDACAO sem ativar multi-cell. O sistema continua funcionando com
1 cluster, 1 ArgoCD, 1 Traefik — mas o modelo de dados esta pronto para N clusters.

### O Que Muda

```
ANTES: Tenant → (environments, projects)
DEPOIS: Tenant → cell_id → (environments, projects)
```

`cell_id` e um campo no tenant que indica em qual Cell o tenant reside.
Na Phase 0, todos os tenants tem `cell_id = "cell-default"`.

### O Que NAO Muda

- Deploy flow identico
- CLI identica
- API contracts identicos
- ArgoCD model identico
- Nenhum novo cluster criado

---

## Tasks

### P0-1: Migration — Adicionar `cell_id` ao Tenant

**Classificacao:** T2 (DB schema change)

**Arquivo:** `api/internal/db/migrations/NNNN_add_cell_id.up.sql`

**SQL:**
```sql
ALTER TABLE tenants ADD COLUMN cell_id TEXT NOT NULL DEFAULT 'cell-default';
CREATE INDEX idx_tenants_cell_id ON tenants(cell_id);

-- Comentario para auditoria
COMMENT ON COLUMN tenants.cell_id IS 'Cell assignment for tenant workloads. ADR-0008.';
```

**Impacto:**
- Forward-only migration (sem `.down.sql` — regra do projeto)
- Default `cell-default` para todos os tenants existentes
- Index para queries futuras `WHERE cell_id = ?`
- Zero impacto em queries existentes (campo novo, nao modifica existentes)

**DoD:**
- [ ] Migration file criado com numero sequencial correto
- [ ] `task db:migrate` executa sem erro
- [ ] `task db:seed` funciona com novo campo
- [ ] Tenant existentes tem `cell_id = 'cell-default'`
- [ ] Index criado

---

### P0-2: Modelo — Adicionar `CellID` ao Tenant struct

**Classificacao:** T1 (local change)

**Arquivos:**
- `api/internal/db/tenant_store.go` — adicionar `CellID` ao struct e queries
- `operators/api/v1/tenant_types.go` — adicionar `CellID` ao Tenant CRD spec

**Go struct (API):**
```go
type Tenant struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    CellID    string    `json:"cell_id"`    // ADR-0008: Cell assignment
    CreatedAt time.Time `json:"created_at"`
    // ... existing fields
}
```

**CRD spec (Operator):**
```go
type TenantSpec struct {
    // ... existing fields
    // CellID identifies which Cell this tenant's workloads run in.
    // Default: "cell-default". Set by PlacementEngine.
    // +kubebuilder:default="cell-default"
    CellID string `json:"cellID,omitempty"`
}
```

**Impacto:**
- Todos os `SELECT` queries que leem tenants incluem `cell_id`
- `INSERT` de novo tenant inclui `cell_id` (default `cell-default`)
- CRD spec atualizado (regenerar manifests com `make manifests`)

**DoD:**
- [ ] Struct atualizado em `tenant_store.go`
- [ ] Queries atualizadas (SELECT, INSERT)
- [ ] CRD type atualizado em `tenant_types.go`
- [ ] `make manifests` regenera CRD YAML
- [ ] `kubeconform --strict` passa
- [ ] `go build ./api/... ./operators/...` passa
- [ ] Testes existentes passam

---

### P0-3: PlacementEngine — Interface + Default Implementation

**Classificacao:** T1 (novo codigo, zero impacto)

**Arquivo:** `api/internal/placement/engine.go` (novo)

```go
package placement

// Engine decides which Cell a tenant should be placed in.
// ADR-0008: Phase 0 returns "cell-default" for all tenants.
// Phase 1+ implements actual placement logic.
type Engine interface {
    // AssignCell returns the cell_id for a new tenant.
    AssignCell(ctx context.Context) (cellID string, err error)

    // GetCell returns the cell_id for an existing tenant.
    GetCell(ctx context.Context, tenantID string) (cellID string, err error)
}

// DefaultEngine always returns "cell-default".
// This is the Phase 0 implementation — all tenants in one Cell.
type DefaultEngine struct{}

func NewDefaultEngine() *DefaultEngine {
    return &DefaultEngine{}
}

func (e *DefaultEngine) AssignCell(_ context.Context) (string, error) {
    return "cell-default", nil
}

func (e *DefaultEngine) GetCell(_ context.Context, _ string) (string, error) {
    return "cell-default", nil
}
```

**Impacto:** Zero. Nenhum call site usa ainda.

**DoD:**
- [ ] Interface definida
- [ ] DefaultEngine implementado
- [ ] Testes unitarios para DefaultEngine
- [ ] `go build ./api/...` passa

---

### P0-4: CellRouter — Interface + Passthrough Implementation

**Classificacao:** T1 (novo codigo, zero impacto)

**Arquivo:** `api/internal/placement/router.go` (novo)

```go
package placement

import "context"

// CellConfig holds connection details for a Cell's workload cluster.
type CellConfig struct {
    CellID         string
    KubeConfig     string // path to kubeconfig
    KubeContext    string // context name
    ArgocdURL      string // ArgoCD URL within the Cell
    ArgocdToken    string // ArgoCD token for the Cell
    IngressDomain  string // base domain for the Cell's ingress
}

// Router resolves cell_id to connection details.
// ADR-0008: Phase 0 returns the current single-cluster config.
type Router interface {
    // Resolve returns the CellConfig for a given cell_id.
    Resolve(ctx context.Context, cellID string) (*CellConfig, error)

    // ListCells returns all known Cells.
    ListCells(ctx context.Context) ([]CellConfig, error)
}

// PassthroughRouter always returns the current single-cluster config.
// This is the Phase 0 implementation.
type PassthroughRouter struct {
    defaultCell CellConfig
}

func NewPassthroughRouter(cfg CellConfig) *PassthroughRouter {
    return &PassthroughRouter{defaultCell: cfg}
}

func (r *PassthroughRouter) Resolve(_ context.Context, _ string) (*CellConfig, error) {
    return &r.defaultCell, nil
}

func (r *PassthroughRouter) ListCells(_ context.Context) ([]CellConfig, error) {
    return []CellConfig{r.defaultCell}, nil
}
```

**Impacto:** Zero. Nenhum call site usa ainda.

**DoD:**
- [ ] Interface definida
- [ ] PassthroughRouter implementado
- [ ] Testes unitarios
- [ ] `go build ./api/...` passa

---

### P0-5: Wire PlacementEngine no Tenant Creation

**Classificacao:** T1 (single call site change)

**Arquivo:** `api/internal/routes/auth.go` ou onde tenants sao criados

**Mudanca:**
Quando um novo tenant e criado (`loopzero login` → POST /auth/register), chamar
`placementEngine.AssignCell()` e setar `tenant.CellID` antes de persistir.

```go
// Antes:
tenant := &db.Tenant{Name: tenantName}

// Depois:
cellID, err := placementEngine.AssignCell(ctx)
if err != nil {
    return fmt.Errorf("assign cell: %w", err)
}
tenant := &db.Tenant{Name: tenantName, CellID: cellID}
```

Na Phase 0, `AssignCell()` sempre retorna `"cell-default"` — zero mudanca comportamental.

**DoD:**
- [ ] PlacementEngine injetado no handler de criacao de tenant
- [ ] `tenant.CellID` setado antes do INSERT
- [ ] Wire no main.go: `placement.NewDefaultEngine()`
- [ ] Teste: novo tenant tem `cell_id = "cell-default"`
- [ ] `go test ./api/...` passa

---

### P0-6: Metrica `loopzero_cell_tenant_count`

**Classificacao:** T1 (nova metrica)

**Arquivo:** `api/internal/metrics/metrics.go`

```go
// Cell metrics — ADR-0008 Phase 0
var (
    CellTenantCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "loopzero_cell_tenant_count",
        Help: "Number of tenants assigned to each Cell.",
    }, []string{"cell_id"})
)
```

**Instrumentacao:** No tick do `BuildReconcileLoop` ou em um periodic collector,
query `SELECT cell_id, COUNT(*) FROM tenants GROUP BY cell_id` e setar o gauge.

**Alerta (futuro):**
```yaml
- alert: LoopZeroCellCapacityWarning
  expr: loopzero_cell_tenant_count > 500
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Cell {{ $labels.cell_id }} has 500+ tenants"
    description: "Consider provisioning a new Cell to distribute load."
```

**DoD:**
- [ ] Metrica registrada em metrics.go
- [ ] Instrumentacao no periodic collector
- [ ] `go build ./api/...` passa
- [ ] Metrica visivel no `/metrics` endpoint

---

## Checklist Global — Phase 0

### Gate: Phase 0 Complete

- [ ] P0-1: DB migration executada, `cell_id` no schema
- [ ] P0-2: Struct atualizado (API + CRD), queries atualizadas
- [ ] P0-3: PlacementEngine interface + DefaultEngine
- [ ] P0-4: CellRouter interface + PassthroughRouter
- [ ] P0-5: Tenant creation usa PlacementEngine
- [ ] P0-6: Metrica `loopzero_cell_tenant_count` emitida
- [ ] Todos: `go build ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `go test ./api/... ./operators/... ./pkg/...` passa
- [ ] Todos: `task validate:manifests` passa
- [ ] Zero mudanca comportamental verificada (deploy E2E identico)
- [ ] Changelog atualizado
- [ ] Validado em DigitalOcean

### O Que NAO Fazer na Phase 0

- [ ] NAO criar segundo cluster
- [ ] NAO modificar ArgoCD provisioner
- [ ] NAO modificar deploy flow
- [ ] NAO modificar CLI
- [ ] NAO modificar API contracts
- [ ] NAO implementar rebalancing
- [ ] NAO implementar cross-cell queries

---

## Trigger para Phase 1

Phase 1 inicia quando QUALQUER destes criterios for atingido:

1. `loopzero_cell_tenant_count{cell_id="cell-default"} > 50`
2. `loopzero_argocd_app_count > 200` (alerta existente LoopZeroArgoCDAppCountWarning)
3. Decisao de negocio de oferecer isolamento por Cell (tenant premium)

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `docs/adr/0008-cell-based-architecture.md` | ADR que aprova esta arquitetura |
| `docs/meetings/records/MEET-2026-03-31-024.md` | Reuniao que gerou o ADR |
| `docs/adr/0007-argocd-application-model.md` | Subsumed — blast radius resolvido por Cells |
| `docs/adr/0006-eliminate-state-duality.md` | State ownership model (PostgreSQL owns) |
