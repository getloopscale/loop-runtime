# Cell Drain Operational Runbook

> **Plan reference:** T3.1 of `docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Closes:** M09 Multi-Cell MEDIUM gap — `cells.status` field supports `draining`/`maintenance`/`retired` but procedure was undocumented.

## Purpose

Procedimento para transição controlada de uma cell entre estados `active` → `draining` → `maintenance` → `retired`. Aplicado em cenários como: upgrade físico do cluster, mudança regional, retirement de uma cell antiga, evacuação preventiva.

## Pre-conditions

- Operator tem credenciais `LOOPZERO_SERVICE_AUTH` para admin API
- Cell alvo está em `status='active'` (verificar via `loopzero admin cells list --json`)
- 2+ cells `active` no portfolio (não draining a única cell ativa)
- Capacidade disponível na cell destino (verificar `loopzero_cell_tenant_count`)

## States

| Estado | Significado | Aceita novos tenants? | Reconcilia existentes? |
|---|---|---|---|
| `active` | Operação normal | Sim (via RoundRobinEngine) | Sim |
| `draining` | Evacuação em curso | NÃO (CellPinner rejects) | Sim |
| `maintenance` | Hold para upgrade/manutenção | NÃO | Sim (best-effort) |
| `retired` | Cell desativada | NÃO | NÃO (cluster pode ser destroyed) |

Transições válidas:
- `active` → `draining` (operator)
- `draining` → `active` (rollback within 24h)
- `draining` → `maintenance` (após evacuation completa)
- `maintenance` → `retired` (após confirmação operacional)
- `retired` → (cluster destroyed, audit_log preserved)

## Procedure

### Step 1 — Mark cell as draining

```bash
loopzero admin cells update --cell cell-1 --status draining \
  --reason "scheduled-upgrade-2026-06" \
  --json
```

Effects:
- `cells.status` PG column UPDATE
- `loopzero_cell_status` metric muda labels
- `CellPinner.Pin` daqui pra frente rejects target=cell-1 com erro `CELL_NOT_ACCEPTING`
- Tenants existentes continuam reconciling normalmente

### Step 2 — Inventory tenants pinned to cell

```bash
loopzero admin tenants list --cell cell-1 --json | jq -r '.[].name'
```

Para cada tenant T retornado, decide destino (operator judgment):
- Plan-aware bin-packing: preferir cell com `loopzero_cell_tenant_count` MENOR
- Affinity-aware: tenants do mesmo customer → mesma cell destino quando possível
- SLA-aware: tenants Enterprise NÃO devem mover sem aviso prévio (24h SLA)

### Step 3 — Pin each tenant to destination cell

```bash
for tenant in $(loopzero admin tenants list --cell cell-1 --json | jq -r '.[].name'); do
  echo "Migrating $tenant..."
  loopzero admin tenants pin --tenant "$tenant" --to cell-2 \
    --reason "drain-cell-1-upgrade" \
    --json
  sleep 5  # rate-limit avoid hammering ArgoCD reconcile queue
done
```

**Note:** Para cells com 500 tenants (capacity max default), bulk pinning script automatizado é separate scope (deferred — operator-driven for now per ADR-0021 D3 sticky-placement).

Effects per tenant:
- DB UPDATE `tenants.cell_id = 'cell-2'`
- Tenant CR `spec.cellID` patched via MergePatch
- Tenant operator reconciles + re-emits ArgoCD App/AppProject `destination.name=cell-2`
- ArgoCD sync materializes resources on cell-2 cluster
- Audit log entry written

### Step 4 — Monitor evacuation progress

```bash
# Real-time tenant count on draining cell
watch -n 30 'kubectl exec -n loopzero-system deploy/loopzero-api -- \
  wget -qO- http://localhost:9090/metrics 2>/dev/null | \
  grep loopzero_cell_tenant_count'
```

Loki query para monitor pin operations:
```
{namespace="loopzero-system",app="loopzero-api"} |= "tenants.pin" | json | source_cell_id="cell-1"
```

Prometheus alert thresholds:
- `loopzero_cell_tenant_count{cell="cell-1"}` deve diminuir monotonically
- Se `loopzero_cell_pin_failures_total{cell="cell-1"}` > 0, investigar antes de prosseguir

### Step 5 — Confirm zero tenants on drained cell

```bash
# Loop até 0
while true; do
  count=$(kubectl exec -n loopzero-system deploy/loopzero-api -- \
    wget -qO- http://localhost:9090/metrics 2>/dev/null | \
    grep "loopzero_cell_tenant_count{.*cell_id=\"cell-1\"" | \
    awk '{print $NF}')
  echo "cell-1 tenant count: $count"
  [[ "$count" == "0" ]] && break
  sleep 60
done
```

### Step 6 — Transition to maintenance (optional)

```bash
loopzero admin cells update --cell cell-1 --status maintenance --json
```

Used when: upgrade/maintenance work happens (e.g., DOKS version bump, K8s version upgrade).

### Step 7 — Final retirement (when no plans to reactivate)

```bash
loopzero admin cells update --cell cell-1 --status retired --json
```

- ClusterCredential CR retained for audit trail
- Cluster pode ser destroyed via Pulumi `pulumi destroy --stack workload-cell-1`
- DB `cells.status='retired'` permanente

## SLO

| Métrica | Target | Como medir |
|---|---|---|
| Evacuation duration | ≤ 7 days for 500 tenants | wall-clock from drain → 0 count |
| Per-tenant pin latency | p95 ≤ 5s | `loopzero_admin_tenants_pin_duration_seconds` |
| Pin failure rate | < 0.1% | `loopzero_cell_pin_failures_total / loopzero_cell_pin_total` |
| Tenant impact (downtime) | 0 (sticky placement preserved on destino) | E2E probe per tenant URL |

## Rollback Procedure

Within 24h of `draining` transition AND com tenants ainda pinned em cell-1:

```bash
loopzero admin cells update --cell cell-1 --status active --json
```

Effects:
- CellPinner accepts new pins to cell-1 again
- Existing tenants já migrados NÃO retornam (sticky placement — operator must explicit pin-back se desejado)
- Audit log captures the reactivation

Se tenants já 100% evacuated, rollback é cosmetic — para "undo" the drain, operator deve fazer reverse-pin de cada tenant manualmente.

## Monitoring queries

Loki (cell-aware reconcile traces):
```
{namespace="loopzero-system"} | json | cell_id="cell-1" | line_format "{{.timestamp}} {{.level}} {{.msg}}"
```

Prometheus (tenant placement distribution):
```promql
sum by (cell_id) (loopzero_cell_tenant_count)
```

Prometheus (drain progress):
```promql
delta(loopzero_cell_tenant_count{cell_id="cell-1"}[1h])
```

## Failure modes

| Sintoma | Diagnóstico | Mitigação |
|---|---|---|
| `loopzero admin tenants pin` retorna `CELL_NOT_ACCEPTING` para destino | Destino também está draining/maintenance | Escolher outra cell ou reativar destino |
| `loopzero admin tenants pin` retorna `NO_CAPACITY` | Destino atingiu maxPerCell | Activate nova cell OR aumentar maxPerCell via Pulumi |
| ArgoCD sync lento após pin (>10min) | Repo-server cache stale | Force refresh: `argocd app actions run <tenant>-<env> refresh --hard` |
| Tenant CR `spec.cellID` divergent from DB | Operator reconcile race | Apply ADR-0021 idempotency — restart tenant-operator OR force reconcile |
| Cliente reporta degraded UX durante drain | Cross-cell DNS propagation lag | Documentar SLA: drains durante off-hours sempre |

## Audit trail

Toda operação grava `audit_log` row:
- `action: cell_status_update | tenant_pin`
- `actor: <operator>` (X-Service-Auth subject)
- `target: cell-1 OR tenant=X`
- `metadata: {old_status, new_status, reason}`
- `timestamp`

Query: `SELECT * FROM audit_log WHERE action LIKE 'cell_%' OR action LIKE 'tenant_pin%' ORDER BY timestamp DESC LIMIT 100;`

## Related

- ADR-0008 (multi-cell placement)
- ADR-0021 (sticky placement, no auto-rebalance)
- MEET-053 (B9 ClusterCredential lookup by spec.cellID)
- `pkg/placement/engine.go` (RoundRobinEngine)
- `api/internal/services/cell_pinner.go` (CellPinner)
- `[[placement-once-route-many]]`
