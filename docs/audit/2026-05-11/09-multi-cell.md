# Module 09 — Multi-Cell Routing (placement + cellmgr + cell_pinner + cell_store)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.7 (1M context), code-validated
**Confidence:** 92%
**Maturity:** **3.7 / 4.0**

## TL;DR

Módulo **mais provado LIVE** auditado até aqui: cell-2 ativada 2026-05-02 com **12/12 ADR gates persistente**, 3/3 cells connected, **rollback P50=648ms / P95=705ms** (<<300s budget). Disciplina arquitetural impecável (TOCTOU via `pg_advisory_xact_lock`, determinismo race-tested, spec.cellID lookup). Bloqueia 4.0 **apenas operacionalmente** — production canary stages 4-5 + 4h soak + chaos pendentes Sprint 9.

---

## 1. Inventário Real

| Componente | LOC | Files |
|---|---|---|
| `placement/engine.go` | 201 | RoundRobinEngine bin-packing |
| `placement/dynamic_router.go` | 196 | CellStore polling + RWMutex cache |
| `placement/serializer.go` | 123 | pg_advisory_xact_lock wrapper |
| `cellmgr/client_manager.go` | 97 | K8s per-cell client caching |
| `services/cell_pinner.go` | 250 | Tenant pin orchestration |
| `services/cell_service.go` | 127 | CellStatus aggregation |
| `db/cell_store.go` | 136 | PostgresCellStore + capacity checks |
| **Tests** | **1,500** | 46 placement + 4 pinner + 3 client_manager + 32 integration |
| **TOTAL** | **~3,195** | |

**Cells ativas LIVE:** cell-default, cell-1, cell-2 (247 referências em código).

**ClusterCredential CRD fields:** clusterName (required), apiServer (HTTPS regex), secretRef (name+namespace), scope (argocd bool, operators bool), tls.insecure (default false), **cellID** (pattern `^cell-[a-z0-9-]+$`, default "cell-default"). Status: connected (bool), lastCheckedAt, observedGeneration, conditions (TypeConnected, TypeSecretNotFound).

**Admin endpoints:** `POST /api/v1/admin/tenants/pin`, `/evict`, `/bulk-delete` (admin_pin_cell.go:36-152, admin_evict.go:30-135).

---

## 2. Arquitetura Observada

**Placement lifecycle:**
```
bootstrap → placement (RoundRobinEngine, bin-pack least-loaded)
   → routing (DynamicRouter.Resolve, 60s refresh, RWMutex pure lookup)
   → pin (CellPinner.Pin, SERIALIZABLE tx, CR MergePatch)
   → drain (cells.status='draining', reject new pins)
```

**RoundRobinEngine** (`engine.go:77-174`):
- Bin-packing: assign to cell com menor tenant count entre os que NÃO estão em `maxPerCell` (default 500)
- Sticky: existing tenants reuse via `GetCell()`
- Optional `WithSerializer()` wraps em `AdvisoryLockSerializer(pool)` para multi-replica TOCTOU safety

**Serializer/pg_advisory_xact_lock** (`serializer.go:48-123`):
- Linha 96: `SELECT pg_advisory_xact_lock(0x706C636D746C636B)` transaction-scoped
- Count-read + placement decision + tenant INSERT todos sob lock atomic (fix MEET-053 D-01 race)
- `SET LOCAL lock_timeout = '30s'`; SQLSTATE 55P03 → `ErrPlacementLockTimeout`

**DynamicRouter** (`dynamic_router.go:119-160`):
- `Resolve(cellID)` = pure hash-table lookup sob RWMutex
- **Determinismo race-tested:** `TestDynamicRouter_Resolve_DeterministicConcurrent` com race detector
- 60s refresh tick + stale-data protection (MEET-053 A-03 rejeita 0-cell return se anterior >0)
- Métricas: `loopzero_cell_routing_decisions_total{cell, decision}`

**CellAwareRuntimeClientFactory** (`shared/runtime_client_factory.go:88-166`):
- `For(ctx, cellID)`: cellID="" ou "cell-default" → `f.DefaultClient` (legacy preserve)
- Multi-cell: list ClusterCredential CRs, lookup por `spec.cellID == requested` (**NÃO** metadata.name — MEET-053 B9)
- Cache write-lock double-check (linha 106-109)
- Usado por EnvironmentReconciler quando cria Namespace/RBAC em pin

**Self-healing:**
- **H12** (`clustercredential_controller.go:129-136`): ArgoCD cluster Secret sync SEMPRE roda ANTES de connectivity check
- **H13** (linha 432-434): ClusterCredential watches Secrets — kubeconfig edit triggers immediate reconcile

**Cells SQL schema** (000006_cells.up.sql):
```sql
CREATE TABLE cells (
    cell_id TEXT PRIMARY KEY CHECK (cell_id ~ '^cell-[a-z0-9-]+$'),
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','draining','maintenance','retired')),
    region TEXT NOT NULL DEFAULT 'unknown',
    capacity_tenants_max INTEGER CHECK (...),
    notes TEXT NOT NULL DEFAULT '',
    created_at/updated_at TIMESTAMPTZ
);
```
Index: `idx_cells_status_capacity` em `(status, capacity_tenants_max)`.

---

## 3. Contratos Públicos

**`POST /api/v1/admin/tenants/pin`:**
- Input: `{tenant_id, cell_id}`
- Response 200: `{tenant_id, previous_cell, new_cell, updated}`
- Errors: 400 (format), 403 (auth), 404 (tenant/cell not found), 409 (not pinnable / at capacity), 503 (pinner not configured)

**`POST /api/v1/admin/tenants/evict`:**
- Input: `{pattern (SQL LIKE), ids[], dry_run}`
- Response 202: `{job_id, queued, matched[], accepted}`
- Rejeita pattern="%" ou "tn-%" (too broad)

**K8sCellStore lookup:** `LoadCells()` lê ClusterCredential CRs, filtra por `spec.cellID`, retorna `[]CellConfig{CellID, KubeConfig, KubeContext}`. Polling 60s via DynamicRouter.refresh().

---

## 4. Cobertura de Testes

**46 placement test funcs** cobrem: bin-packing least-loaded, capacity gate rejection, sticky assignment, Serializer integration com advisory lock, DynamicRouter determinismo (sequential + concurrent + race detector), stale-data protection (0-cell rejection), lock_timeout SQLSTATE mapping.

**32 integration tests** matching `*cell*_test.go` ou `*router*_test.go`.

**Migration 000007 test:** validate flip status maintenance → active idempotente.

**LIVE evidence (2026-05-02):**
- Rollback P50 = **648ms** (within 300s budget)
- Rollback P95 = **705ms** (within 300s budget)
- **12/12 ADR gates persistente LIVE GREEN**
- 3/3 cells connected
- EnvironmentReconciler cell-aware via CellAwareRuntimeClientFactory monolithic + split-binary

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| **Cell-2 production canary stages 4-5 pendentes** | **HIGH (operational)** | Migration 000007_activate_cell_2 WARNING; Sprint 9 readiness review needed |
| **4h soak + chaos + SLO gate pendentes** | **HIGH (operational)** | `cell2-soaks.yml` cron Sunday; primeira execução GREEN = gate closure |
| Sem automatic rebalancing | MÉDIA (ADR D3 intentional) | Operator-driven only; hot-spot = upgrade tier signal |
| Sem health-based placement preference | MÉDIA | RoundRobinEngine assume cells listadas saudáveis |
| Cache invalidation em token rotation | BAIXA | Mitigado por CR patch (restart reconciler); ADR-0008 Phase 2 follow-up |
| Sem deterministic ordering em counts iguais | BAIXA | Map iteration unordered; Resolve() ainda determinístico |
| TODO em router.go:47 (DefaultEngine deletion) | BAIXA | Cleanup pós single-cell removal |
| AppSecret CRD órfã cross-cut com M03 | MÉDIA | Tenant pin pode orphan AppSecrets |
| ReconciliationReport dead code cross-cut | INFO | M03 cross-cut |

---

## 6. Avaliação de Maturidade

**Score: 3.7 / 4.0** (confidence 92%)

### Evidências

1. **T1 (commit+test)** — `placement/engine.go:77-174` (RoundRobinEngine bin-pack); `placement/serializer.go:96` (advisory lock); `shared/runtime_client_factory.go:88-166` (CellAwareRuntimeClientFactory); 46 placement test funcs + race detector tests.
2. **T2 (LIVE log)** — Cell-2 activation **LIVE 2026-05-02** com 12/12 ADR gates persistente sem drift + postflip PASS + Stage 1+2 SMOKE GREEN; 3/3 cells connected; rollback P50=648ms/P95=705ms LIVE; H12/H13 healing validated.
3. **T3 (CI run)** — `cell2-soaks.yml` (cron) + `operator-split-soak.yml` + `multi-cell` workflows + 32 integration tests.

### Raciocínio

**Sustenta 3.5-3.7+:**
- TOCTOU safety codified (`pg_advisory_xact_lock` transaction-scoped)
- DynamicRouter determinismo race-tested (concurrent + sequential)
- CellAwareRuntimeClientFactory wired em 3 controllers (app, environment, preview)
- Self-healing H12 + H13 codified com migration test
- 3/3 cells LIVE GREEN persistent (12 ADR gates sem drift)
- Rollback P50/P95 dentro budget (LIVE-validated)
- Admin pin/evict endpoints + capacity gates + cell drain (active/draining/maintenance/retired)
- 46 placement test funcs + 32 integration tests

**Bloqueia 4.0 (operacional, não arquitetural):**
- Cell-2 production canary stages 4-5 pendentes Sprint 9 (requer 1 customer 7-day soak + tier rollout 10/50/100%)
- 4h soak + Chaos Mesh + SLO P95<705ms gates pendentes (requer sessão dedicada com traffic generators)
- Sem automatic rebalancing (intentional per ADR-0008/021 D3 — DNS continuity + state affinity)
- Sem health-based placement preference (Phase 2 follow-up)
- AppSecret CRD órfã cross-cuts (M03 gap)

### Caminho para 4.0

1. **Executar cell2-soaks.yml** primeiro cron Sunday GREEN — gate closure crítico
2. **Sprint 9 prod canary readiness review** — aprovar Stage 4 (1 customer 7-day) + Stage 5 (10/50/100%)
3. **4h soak + Chaos Mesh injection** com traffic generator dedicado
4. Implementar health-based placement preference (filter cells por `ClusterCredential.status.connected=true`)
5. Deterministic ordering em tie-break (sort cells por ID antes de iteração)
6. Token rotation cache invalidation event watcher (ClusterCredential reconcile)
7. Decidir AppSecret controller implementation (cross-cut M03)

---

**Próximas auditorias relacionadas:** Operators (M03), CI/CD (M12), Auth/Billing (M10).
