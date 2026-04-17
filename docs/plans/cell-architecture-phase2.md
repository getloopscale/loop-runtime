# Cell Architecture — Phase 2 (2a Prerequisite + 2b Activation)

**Criado:** 2026-04-17
**Última atualização:** 2026-04-17 (v1.0)
**Horizonte:** 6-10 semanas
**Objetivo:** Entregar a arquitetura cell-based definida em ADR-0008 até o ponto onde 2 cells isoladas possam ser demonstradas com failure independence real (não só diagrama).
**Origem:** MEET-2026-04-17-051 — reescopo unânime do Phase 2 original (CF Worker + KV foi rejeitado; veto triplo de SRE + Security + Production Design).

---

## 📌 Snapshot

**Este plano é gated:** Phase 2b só pode começar depois de Phase 2a completa.
O pedido original (ativar 2ª cell) foi DEFERIDO — **shared-services hardening é
prerequisito** para a promessa "cell failure affects only tenants in that cell"
ser operacionalmente verdadeira, não arquiteturalmente falsa.

```
[ATUAL] single cell, Gate #1 aberto, shared services SPOF
   │
   ▼
[Phase 2a] Hardening (6 tasks, ~3 sprints)
   │  • Close Gate #1 (L3 10× ≥95/100)
   │  • PG bump
   │  • Gitea/Harbor/MinIO HA
   │  • mgmt headroom
   │  • tenant-health dashboard
   ▼
[Phase 2b] Cell-2 Activation (17 tasks + 23 veto conditions, ~5 sprints)
   │  • PG-as-routing-authority (API data-path proxy)
   │  • Sticky-capacity placement com advisory lock
   │  • Kubeconfig em Secret + 24h rotation
   │  • Per-tenant MinIO STS
   │  • Cell health controller + synthetic probe
   │  • Alert aggregation cell:degraded
   │  • Chaos proof (video: cell-2 down, cell-1 serve 200 por 10min)
   ▼
[Phase 3] Migration + per-cell ArgoCD (não cobrido aqui)
```

**Investor demo target:** chaos video (não topology slide). DoD de Phase 2b.

---

## Princípios operacionais

1. **Shared services não podem ser SPOF multiplicado.** Se Gitea/Harbor/MinIO
   cair com 2 cells, afeta 2× tenants. Hardening antes de multiplicar.
2. **Uma authority por domínio.** PG é authority para `tenant → cell`. CF KV é
   cache com fallback (rejeitado como authority pelo meeting).
3. **Cada veto condition do MEET-051 vira task explícita.** Nada de "faremos
   depois" em check-box indocumentado.
4. **Chaos test é DoD, não follow-up.** Se cell-2 não passa pelas 6 cenas de
   falha com runbook validado, não vai para produção.
5. **Migration é Phase 3.** Phase 2b coloca novos tenants na cell-2; existentes
   ficam em `cell-default`. Sem exceção.

---

# PHASE 2a — Shared-Service Hardening

**Goal:** pre-requisito para cell-2 fazer sentido. Eliminar SPOFs
compartilhados + closar Gate #1.

## T-2a-01 · Close Gate #1 (L3 10× ≥95/100 consecutive)

**Tipo:** T1 (execução disciplinada)
**Esforço:** 1 dia (após T-08 resolver OU ativar LOOPZERO_LEAN_RESOURCES)
**Owner:** Infra + API
**Depends on:** `docs/plans/50k-immediate-focus.md` T-08 (DO quota) OU contingência lean-resources

### Tarefas

- [ ] 01.1 DO quota confirmado OU `kubectl set env deploy/loopzero-api LOOPZERO_LEAN_RESOURCES=true` aplicado (ver `docs/runbooks/runtime-capacity-bump.md`)
- [ ] 01.2 Rodar L3 v15 → ≥95/100 (sanity)
- [ ] 01.3 Rodar `tools/l3-stability-loop.sh --max-runs 10 --threshold 95`
- [ ] 01.4 Registrar 10 TSV rows em `/tmp/l3-stability/summary.tsv`
- [ ] 01.5 Commit evidência em `docs/plans/scale-50k-l3-readiness.md` seção "Gate #1 closed"

### Critérios de aceite

| Critério | Valor |
|---|---|
| Runs ≥ 95/100 | 10/10 |
| p95 deploy delta min/max | < 10% |
| Zero live-patches | Nenhum `kubectl delete --type=json --force` |
| Cleanup entre runs | `admin-evict-tenants.sh` exit 0 |

### DoD

- 10 TSV rows committed + plan atualizado + Gate #1 ✅ em `50k-immediate-focus.md`.

---

## T-2a-02 · PostgreSQL bump (connection + capacity headroom)

**Tipo:** T2 (managed DB — mudança infra)
**Esforço:** 2-4h (Pulumi edit + managed DB failover window)
**Owner:** Infra
**Depends on:** nenhum (independente)

### Motivação

PG atual: `db-s-1vcpu-2gb` → `max_connections=50`. Live: ~36 em uso idle (72%).
Cell-2 adiciona mais processos (API replica, controllers). Overflow de pool sob
carga → 5xx em produção. Production Design Reviewer flagou como blocker.

### Tarefas

- [ ] 02.1 `Pulumi.dev.yaml`: `loopzero-infra:dbNodeClass: small → medium` (db-s-2vcpu-4gb → `max_connections=100`)
- [ ] 02.2 `npx pulumi preview` → confirmar só DB cluster muda (drift safety)
- [ ] 02.3 `npx pulumi up --target <db-urn>` em janela manutenção (managed PG failover ~1min)
- [ ] 02.4 Ajustar `DB_MAX_CONNS` no deploy loopzero-api: 25 → 40 (headroom)
- [ ] 02.5 Verificar `kubectl exec loopzero-api -- printenv | grep DB_MAX` + `pg_stat_activity` count

### Critérios de aceite

| Critério | Valor |
|---|---|
| max_connections live | ≥ 100 |
| Idle connections % | < 40% (vs 72% atual) |
| Pool saturation sob L3 | < 80% |
| Sem 5xx pool-overflow em L3 v16 | Zero |

### DoD

- Pulumi commit + live verificado + L3 v16 passa com pool monitorado.

---

## T-2a-03 · Gitea HA completion

**Tipo:** T2 (infra)
**Esforço:** 1-2 dias
**Owner:** Infra
**Depends on:** nenhum (Redis já committed bfbe818)

### Motivação

Bug #44 fix trouxe Redis para session/cache/queue (HA parcial). Falta: shared
storage backend (repos em NFS ou S3) + 2+ replicas consistentes.
Today: 1 replica Gitea. Rollback depends on Gitea = SPOF amplificado por cell.

### Tarefas

- [ ] 03.1 Decidir storage backend: NFS provisioner (já em uso — `mgmt-bootstrap.ts`) vs DO Spaces mount (cleaner, menos SPOF)
- [ ] 03.2 Configurar Gitea `ATTACHMENT_STORE_TYPE=minio` + `LFS_STORE_TYPE=minio` usando DO Spaces (source of truth dos repos continua git blob no storage shared)
- [ ] 03.3 Bumpar `gitea.replicas: 1 → 2` em `infra/helm/gitea/values.yaml`
- [ ] 03.4 Configurar `podAntiAffinity` para spread entre nodes
- [ ] 03.5 Load test: 200 concurrent `git clone` via `tools/gitea-loadtest.sh` (criar se não existe) → todos sucedem
- [ ] 03.6 Chaos test: `kubectl delete pod -l app=gitea --force` (1 replica) → outra replica atende < 10s

### Critérios de aceite

| Critério | Valor |
|---|---|
| Replicas live | ≥ 2 spread cross-node |
| 200 concurrent clones | 100% success |
| Pod kill recovery | < 10s TTFB vs alt replica |
| Lose 1 pod → rollback ainda funciona | `loopzero rollback` exit 0 |

### DoD

- Commit Pulumi + Helm + load-test output + chaos verificado + runbook atualizado.

---

## T-2a-04 · Harbor HA

**Tipo:** T2
**Esforço:** 1 dia
**Owner:** Infra
**Depends on:** T-2a-02 (PG bump — Harbor usa PG central)

### Motivação

Single Harbor core = SPOF para image pull em ALL cells. Cell-2 healthy mas
Harbor down = cell-2 não puxa imagens = zero deploys.

### Tarefas

- [ ] 04.1 `harbor-bootstrap.ts`: bump `core.replicas: 1 → 2` + `registry.replicas: 1 → 2`
- [ ] 04.2 Redis Harbor em HA mode (já usado para cache) ou reuse existing Redis Sentinel se available
- [ ] 04.3 Validar S3 backend (já DO Spaces) suporta N replicas reading/writing
- [ ] 04.4 Load test: 100 concurrent `docker pull` → throughput > 50 MB/s aggregate

### Critérios de aceite

| Critério | Valor |
|---|---|
| Core replicas | ≥ 2 |
| Registry replicas | ≥ 2 |
| 100 concurrent pulls | 0 errors |
| Kill 1 replica → pulls continue | Sim |

### DoD

- Pulumi commit + load test result + canary passa (ver `tenant-bulk-evict.md` Harbor canary seção).

---

## T-2a-05 · MinIO distributed migration

**Tipo:** T3 (storage migration — critical flow)
**Esforço:** 2-3 dias
**Owner:** Infra + API
**Depends on:** nenhum
**Plano existente:** `docs/plans/minio-distributed-migration.md`

### Motivação

Single MinIO = SPOF para source bundles + build cache. Ver plano dedicado.

### Tarefas

Seguir `docs/plans/minio-distributed-migration.md`. Resumo:

- [ ] 05.1 Provisionar MinIO em modo distributed (mínimo 4 nodes)
- [ ] 05.2 Migrar bundles existentes
- [ ] 05.3 Switchover Gitea + API para novo endpoint
- [ ] 05.4 Decommission single-node

### DoD

- Plano dedicado `minio-distributed-migration.md` com DoD próprio.

---

## T-2a-06 · mgmt cluster memory headroom

**Tipo:** T2 (infra)
**Esforço:** 2h
**Owner:** Infra

### Motivação

mgmt nodes a 74% memory no idle (Production Design Reviewer). Adicionar cell-2
controllers + ArgoCD central maior = risco de OOM cascata.

### Tarefas

- [ ] 06.1 Avaliar: bump node class (`s-2vcpu-4gb → s-4vcpu-8gb-amd`) vs +1 node
- [ ] 06.2 Aplicar via Pulumi (mesmo procedimento de T-08 runtime, ver `docs/runbooks/runtime-capacity-bump.md`)
- [ ] 06.3 Verificar idle memory < 50% pós-bump

### DoD

- Pulumi commit + verify metrics.

---

## T-2a-07 · `tenant-health` Grafana dashboard

**Tipo:** T2 (observability)
**Esforço:** 1 dia
**Owner:** Observability/SRE

### Motivação

"Sem dashboard unificado, Phase 2 é un-operable" (SRE). Precisa single panel
respondendo para qualquer `tenant_id`: qual cell + status em cada stage
(API → build → image → sync → live).

### Tarefas

- [ ] 07.1 Query: `cell_id` vindo de PG via Grafana PG datasource OU label em métricas existentes
- [ ] 07.2 Métricas compostas: `loopzero_tenant_status{tenant_id,stage,cell_id}` (novo)
- [ ] 07.3 Dashboard JSON em `infra/helm/monitoring/dashboards/tenant-health.json`
- [ ] 07.4 Single panel table: tenant_id | cell | api ok | build last | image tag | sync status | live 200?

### DoD

- Dashboard published + demo query resolvendo "where is tenant X?" em < 5s.

---

## Gate Phase 2a Exit

Phase 2b **não pode começar** até:

| Gate | Status |
|---|---|
| Gate #1 fechado (T-2a-01) | ❌ |
| PG `max_connections ≥ 100` (T-2a-02) | ❌ |
| Gitea HA ≥ 2 replicas + chaos pass (T-2a-03) | ❌ |
| Harbor HA ≥ 2 core replicas (T-2a-04) | ❌ |
| MinIO distributed migrated (T-2a-05) | ❌ |
| mgmt memory headroom < 50% idle (T-2a-06) | ❌ |
| `tenant-health` dashboard live (T-2a-07) | ❌ |
| **Chaos: kill each shared service → platform recovers < 2min** | ❌ |
| **L3: 5 consecutive ≥ 98/100 (post-hardening regression check)** | ❌ |

---

# PHASE 2b — Cell-2 Activation

**Goal:** provisionar e ativar 2ª cell real com failure isolation validated.
**Blocker:** Phase 2a Gate Exit acima.

## T-2b-01 · Pulumi `cell.ts` real DOKS provisioning

**Tipo:** T3 (infra — nova cell real)
**Esforço:** 3-5 dias
**Owner:** Infra
**Depends on:** Phase 2a Gate Exit

### Tarefas

- [ ] 01.1 Remover placeholders de `infra/pulumi/src/components/cell.ts`
- [ ] 01.2 Implementar `createCell(name, region, config)`:
  - [ ] DOKS cluster via `factory.createKubernetesCluster`
  - [ ] Traefik via `bootstrap-shared.installTraefik` (per-cell ingress)
  - [ ] cert-manager via `installCertManager` (per-cell)
  - [ ] Prometheus Agent (remote-write → central Mimir/Thanos)
  - [ ] Promtail para logs
  - [ ] ESO ClusterSecretStore scoped
- [ ] 01.3 Output: `cellOutputs.kubeconfig` (secret-wrapped) + `cellOutputs.ingressIP`
- [ ] 01.4 `infra.ts`: param `cells: [{name, region, size}]` criando N cells
- [ ] 01.5 Pulumi preview drift-safe antes de apply (ver runbook `runtime-capacity-bump.md`)

### DoD

- 2 DOKS clusters live (`cell-default` adotado + `cell-2` novo)
- Cada cell com Traefik + cert-manager + Prometheus Agent
- Pulumi state clean (zero órfãos)

---

## T-2b-02 · Kubeconfig em Secret (não CRD spec) com 24h rotation

**Tipo:** T3 (security)
**Esforço:** 3 dias
**Owner:** Operator + Security review
**Depends on:** T-2b-01

### Veto condition referenciada

Security #1: "Kubeconfig TTL ≤1h OR Secret-stored ≤24h rotation, NEVER in CRD spec"

### Tarefas

- [ ] 02.1 Atualizar CRD `ClusterCredential` — remover `kubeconfig` do spec, usar `kubeconfigSecretRef: {name, namespace}`
- [ ] 02.2 ClusterCredentialController: watch Secret → hot-reload client
- [ ] 02.3 CronJob rotação 24h: `kubeconfig-rotator` chama DOKS API `kubernetes/clusters/{id}/kubeconfig`, writes Secret
- [ ] 02.4 Alert `KubeconfigStale` — Secret não rotated em >30h
- [ ] 02.5 Audit log: toda leitura de Secret kubeconfig logged com caller identity
- [ ] 02.6 Regression test: CRD spec/status NÃO contém kubeconfig (envtest)

### DoD

- CRD migrada + controller hot-reload funciona + rotation cron running + alert disparando em manual delay.

---

## T-2b-03 · API data-path proxy (PG-as-authority routing)

**Tipo:** T3 (api-boundaries — critical flow)
**Esforço:** 5 dias
**Owner:** API
**Depends on:** T-2b-01

### Veto conditions

Architect: "PG tenants.cell_id is single source; CF Worker out of dynamic path"
DistSys: "PG authority, KV cache with fallback"

### Decisão arquitetônica

**CF Worker fica SOMENTE para static delivery.** Dynamic routes (API, dashboard,
user apps) passam por proxy tenant-aware na mgmt cluster (Traefik middleware OU
Go reverse proxy dedicado).

### Tarefas

- [ ] 03.1 Decidir proxy: Traefik middleware Go plugin vs dedicated Go service
- [ ] 03.2 Implementar `/internal/route-lookup?host=X` endpoint na API: retorna `cell_id` + cell origin IP
- [ ] 03.3 Proxy lê `/internal/route-lookup` com 30s cache in-memory
- [ ] 03.4 CF Worker (route dinâmica) chama `/internal/route-lookup` como fallback quando KV miss
- [ ] 03.5 KV reconciliation cron (5 min): lê PG, sincroniza CF KV
- [ ] 03.6 Metric `loopzero_routing_source{source="kv_hit|kv_miss_pg_fallback|pg_direct"}`

### DoD

- Proxy rota 100% do tráfego dinâmico via PG lookup (KV opcional cache)
- Test: new tenant criado → primeira requisição roteada via PG fallback (KV não tem) → backfill KV → segunda requisição via KV hit
- Zero downtime migration (Traefik middleware reload)

---

## T-2b-04 · PlacementEngine sticky-capacity com advisory lock

**Tipo:** T3 (api + distsys)
**Esforço:** 3 dias
**Owner:** API + DistSys review
**Depends on:** T-2b-01

### Veto conditions

DistSys: "`pg_advisory_xact_lock(hash('placement'), hash(cell_id))` wraps capacity-check + assignment"

### Tarefas

- [ ] 04.1 Estender `api/internal/placement/engine.go` com `CapacityAwareEngine`
- [ ] 04.2 Novo método `Place(ctx, tenantID) (cellID, error)`:
  - [ ] Lock `pg_advisory_xact_lock('placement', cell_id)` por cada cell candidata
  - [ ] Query capacity atual (namespaces count, pod count, etc.) dentro do lock
  - [ ] Primeira cell com `capacity_used < cell_quota * 0.8` vence
  - [ ] Fail-closed se todas cheias (typed error `ErrAllCellsFull`)
- [ ] 04.3 Tenant creation flow usa `Place()` em vez de RoundRobin
- [ ] 04.4 Test: 10 concurrent `POST /tenants` com cell-1 a 98% → 2 em cell-1, 8 em cell-2 (capacity-respecting)

### DoD

- Sticky funciona (mesmo tenant, mesma cell em retry)
- Advisory lock test: 10 concurrent → total placements = 10 (sem race)
- Fail-closed test: todas cells full → `503 CELLS_AT_CAPACITY`

---

## T-2b-05 · Cell health controller (formal contract)

**Tipo:** T3 (controllers)
**Esforço:** 4 dias
**Owner:** Operator + SRE review
**Depends on:** T-2b-01, T-2b-02

### Veto condition

SRE #3: "Cell health controller (not just CRD) evaluating formal contract"

### Formal contract

```go
cell_healthy =
  platform_pods_ready AND
  argocd_app_sync_ok AND
  traefik_reachable_external AND
  synthetic_deploy_success_5m AND
  kv_routing_consistent_with_db AND
  gitea_reachable_from_cell
```

### Tarefas

- [ ] 05.1 Extend `ClusterCredentialReconciler` com check loop (10s interval, 3-strike)
- [ ] 05.2 Status transitions: `Provisioning → Active → Degraded → Unreachable → Draining → Retired`
- [ ] 05.3 Probe functions: platformPodsReady, argoCDSyncOK, traefikReachable, syntheticDeploy, kvConsistency, giteaReachable
- [ ] 05.4 Emit métrica `cell_status{cell,state}` gauge
- [ ] 05.5 PlacementEngine filtra cells não-`Active` (Draining, Unreachable excluídos de novas placements)

### DoD

- 6 probes implementadas + transitions testadas em envtest + métrica emitindo.

---

## T-2b-06 · Synthetic probe per cell

**Tipo:** T2 (observability)
**Esforço:** 2 dias
**Owner:** SRE
**Depends on:** T-2b-01

### Veto condition

SRE #1: "Per-cell synthetic probe emitting `synthetic_deploy_success{cell}`"

### Tarefas

- [ ] 06.1 Tool `tools/synthetic-tenant.sh`: cria tenant-synth-{cell}-{ts}, deploys fixture, verifica 200, evicts
- [ ] 06.2 CronJob `synthetic-probe-{cell}` roda a cada 5min em mgmt contra cada cell
- [ ] 06.3 Emite métrica `synthetic_deploy_success{cell,step}` + `synthetic_deploy_duration_seconds`
- [ ] 06.4 SLI consumible: `rate(synthetic_deploy_success{cell="cell-2"}[30m])`

### DoD

- 2 cronjobs rodando (um por cell) + 30 min de métricas coletadas sem flap.

---

## T-2b-07 · Alertmanager cell:degraded aggregation

**Tipo:** T2 (observability)
**Esforço:** 1 dia
**Owner:** SRE
**Depends on:** T-2b-06

### Veto condition

SRE #2: "`cell:degraded` aggregation + inhibit rules tested via injected failure"

### Tarefas

- [ ] 07.1 PrometheusRule novo:
  ```yaml
  - alert: CellDegraded
    expr: count(app_down{cell="$cell"}) > 3
    for: 5m
  ```
- [ ] 07.2 Alertmanager config: `group_by: [cell]` + `inhibit_rules: cell:degraded inhibits app_down{cell=$cell}`
- [ ] 07.3 Test injetando: `kubectl scale deploy/tenant-synth-app-X --replicas=0` em 5 tenants cell-2 → `CellDegraded` fires uma vez, `app_down` individuals suppressed

### DoD

- Test de injeção passa + runbook `cell-degraded.md` criado + paging routed.

---

## T-2b-08 · Per-tenant MinIO STS credentials

**Tipo:** T3 (security — credential isolation)
**Esforço:** 4 dias
**Owner:** API + Security review
**Depends on:** T-2a-05 (MinIO distributed)

### Veto condition

Security #2: "Per-tenant or per-cell storage credential isolation for MinIO/Spaces before 2nd cell goes live"

### Tarefas

- [ ] 08.1 MinIO STS policy per tenant: read/write apenas `tenant-{tenantID}/*`
- [ ] 08.2 API endpoint `/internal/storage-credentials` gera STS com 15min TTL
- [ ] 08.3 Build Workflow recebe creds via Workflow parameter (não env static secret)
- [ ] 08.4 Audit log de cada STS issuance
- [ ] 08.5 Cross-tenant negative test: tenant-A creds tentando GET tenant-B/bundle.tar → 403

### DoD

- Creds emitidas per build + TTL enforced + isolation validated via test.

---

## T-2b-09 · CF API token scoping + reconciler

**Tipo:** T2 (security)
**Esforço:** 1 dia
**Owner:** Security + Infra

### Veto condition

Security #3: "CF API token scoped to single KV namespace, ≤30d rotation, PG-first write-through"

### Tarefas

- [ ] 09.1 Criar CF API token novo com scope = KV namespace `loopzero-routing` SOMENTE
- [ ] 09.2 Rotate via script `tools/rotate-cf-token.sh` (30d cronjob)
- [ ] 09.3 Write path: sempre escrever PG primeiro, depois CF KV
- [ ] 09.4 Reconciliation cron 5min: diff PG vs CF KV, corrige drift, alert se drift > 1%

### DoD

- Token restrito + rotation automatizado + reconciler reporting drift < 1%.

---

## T-2b-10 · Cosign verification per cell

**Tipo:** T2 (supply-chain security)
**Esforço:** 2 dias
**Owner:** Security + K8s admission

### Veto condition

Security #4: "Cosign verification at each cell's admission controller"

### Tarefas

- [ ] 10.1 Deploy `cosigned` OR `kyverno` em cada cell (admission webhook)
- [ ] 10.2 Policy: pod spec com image NOT signed by LoopZero cosign key → reject
- [ ] 10.3 Test: tentar deployar pod com image não assinada → admission denied
- [ ] 10.4 Rotate cosign key (documentar procedure)

### DoD

- Admission policy live em ambas cells + negative test passa.

---

## T-2b-11 · Per-cell ESO (ClusterSecretStore scoped)

**Tipo:** T2 (security)
**Esforço:** 2 dias
**Owner:** Security + Operator

### Veto condition

Security #5: "Per-cell ESO with scoped SA; no mgmt-proxy pattern"

### Tarefas

- [ ] 11.1 Install ESO via cell.ts (parte do T-2b-01)
- [ ] 11.2 ClusterSecretStore local por cell, SA scope = tenant namespaces only
- [ ] 11.3 Validar: cell-2 ESO não pode ler secrets de mgmt ou cell-default

### DoD

- ESO per cell running + cross-cell read negative test passa.

---

## T-2b-12 · NetworkPolicy default-deny cross-cluster

**Tipo:** T2 (security)
**Esforço:** 1 dia
**Owner:** Security

### Veto condition

Security #6: "NetworkPolicy default-deny cross-cluster"

### Tarefas

- [ ] 12.1 Por cell: default-deny egress para IPs de outras cells (CIDR blocks)
- [ ] 12.2 Exceção allow-list: mgmt services (Gitea, Harbor, PG) por IP + porta
- [ ] 12.3 Test: pod em cell-2 curl para pod em cell-default → timeout

### DoD

- Policies live + negative test passa.

---

## T-2b-13 · Audit query cell_id filter

**Tipo:** T1 (security)
**Esforço:** 4h
**Owner:** API

### Veto condition

Security #7: "Audit queries include cell_id; regression test tenant-A token + cell-B filter returns empty"

### Tarefas

- [ ] 13.1 Adicionar `cell_id` em todas queries de `audit_log`
- [ ] 13.2 Regression test `audit_cell_isolation_test.go`: tenant-A auth, `?cell=cell-2` → empty se tenant-A em cell-default

### DoD

- Test passa + handler validates cell_id ownership.

---

## T-2b-14 · Path-level gitops cell encoding

**Tipo:** T2 (gitops)
**Esforço:** 1 dia
**Owner:** API + GitOps review

### Veto condition

GitOps: "Path-level cell encoding (`gitops/cells/{cell-id}/...`) documented in spec §10"

### Tarefas

- [ ] 14.1 Migrar `gitops/{tenant}/{project}/{env}/` → `gitops/cells/{cell-id}/{tenant}/{project}/{env}/`
- [ ] 14.2 API `gitops.Push()` usa cellID do tenant na path
- [ ] 14.3 ArgoCD AppProject.sourceRepos filtra por path cell encoding
- [ ] 14.4 Update spec §10

### DoD

- Migration backfilled + new deploys usam novo path + spec updated.

---

## T-2b-15 · Migration state machine (types-only, não wired)

**Tipo:** T2 (pkg)
**Esforço:** 1 dia
**Owner:** Platform SDK

### Motivação

Meeting: "Migration is Phase 3, mas a state machine precisa existir em
`pkg/statemachine/` para review/documentação."

### Tarefas

- [ ] 15.1 `pkg/statemachine/migration.go` — states: `ACTIVE → PREPARING → DUAL → DRAINING → ACTIVE(new_cell)`
- [ ] 15.2 Transitions + invalid-event panics
- [ ] 15.3 Unit tests
- [ ] 15.4 Doc comment: "NOT wired — Phase 3"

### DoD

- Types + tests in package, zero callers (NoOp).

---

## T-2b-16 · Runbooks 6 failure modes

**Tipo:** T2 (docs)
**Esforço:** 2 dias
**Owner:** SRE

### Veto condition

SRE #5: "Runbooks merged for all 6 failure modes with tested recovery steps"

### Runbooks

- [ ] 16.1 `docs/runbooks/cell-partition.md` — cell fica unreachable de mgmt
- [ ] 16.2 `docs/runbooks/cell-degraded.md` — cell health=Degraded
- [ ] 16.3 `docs/runbooks/cell-at-capacity.md` — placement rejeita com CELLS_AT_CAPACITY
- [ ] 16.4 `docs/runbooks/kv-stale.md` — CF KV drift detected
- [ ] 16.5 `docs/runbooks/gitea-unreachable.md` — rollback degraded mode
- [ ] 16.6 `docs/runbooks/argocd-sync-stuck.md` — per-cell sync stuck

### DoD

- 6 runbooks committed + cada um com recovery steps testado em staging-equiv.

---

## T-2b-17 · Chaos test suite (DoD do Phase 2b)

**Tipo:** T2 (verification)
**Esforço:** 3 dias
**Owner:** SRE + DistSys

### Veto condition

**SRE #6 + Production Design #4 (investor demo requirement):**
"Chaos video: cell-2 down → cell-1 apps serve 200 for 10min, reverse validated"

### Cenários

- [ ] 17.1 Cell-2 API server down → cell-1 apps serve 200 for ≥ 10 min. Reverse.
- [ ] 17.2 Cell-2 slow (p95 +500ms) → burn-rate alert fires < 10min
- [ ] 17.3 Cell-2 at capacity → placement refuses new tenants (503), existing continue
- [ ] 17.4 KV stale (inject 5min lag) → detection < 2min, PG fallback < 2s
- [ ] 17.5 Gitea unreachable → rollback degraded mode OR fail-loud (com mensagem user)
- [ ] 17.6 ArgoCD sync stuck per-cell → alert + auto-page

### Tarefas

- [ ] 17.1 Implementar cada cenário como script `tools/chaos/<scenario>.sh`
- [ ] 17.2 Record MTTR por cenário em `docs/chaos/phase2b-results.md`
- [ ] 17.3 **Chaos video:** screen recording de cenário 1 (cell-2 down, cell-1 serving) — arquivo final pra investor demo

### DoD

- 6 scripts committed + results doc + video gravado.

---

## Gate Phase 2b Exit (Phase 2 completo)

| Gate | Status |
|---|---|
| Phase 2a Exit completo | ❌ (prereq) |
| 2 ClusterCredential CRs registradas | ❌ |
| Cell-2 provisionada via Pulumi (não doctl) | ❌ |
| All 8 SRE veto conditions ✅ | ❌ |
| All 8 Security veto conditions ✅ | ❌ |
| All 8 DistSys invariants validated | ❌ |
| All 4 Architect conditions met | ❌ |
| All 6 Production Design conditions met | ❌ |
| Chaos video recorded | ❌ |
| ADR-0010 committed | ❌ |

---

# Sequence (topological)

```
Phase 2a (paralelo):
  T-2a-01 ─┐
  T-2a-02 ─┤
  T-2a-06 ─┼─► Phase 2a Gate Exit ─► Phase 2b
  T-2a-07 ─┤
  T-2a-03 ─┤ (Gitea HA)
  T-2a-04 ─┤ (Harbor HA, depends T-2a-02)
  T-2a-05 ─┘ (MinIO distributed)

Phase 2b (sequenciado):
  T-2b-01 (cell provisioning) ──┬─► T-2b-02 (kubeconfig secret)
                                ├─► T-2b-03 (API proxy)
                                ├─► T-2b-04 (placement)
                                ├─► T-2b-05 (health controller, depends 02)
                                └─► T-2b-06 (synthetic probe)
                                      │
                                      └─► T-2b-07 (alertmanager, depends 06)

  T-2a-05 ─► T-2b-08 (MinIO STS per-tenant)
  T-2b-01 ─► T-2b-09 (CF token scoping)
            └─► T-2b-10 (cosign per cell)
            └─► T-2b-11 (per-cell ESO)
            └─► T-2b-12 (NetworkPolicy)
  T-2b-03 ─► T-2b-13 (audit cell_id)
  T-2b-03 ─► T-2b-14 (gitops path cell encoding)
  independente: T-2b-15 (migration state machine types)
  T-2b-05 + T-2b-07 ─► T-2b-16 (runbooks, needs real failure modes)
  tudo acima ─► T-2b-17 (chaos suite) ── DoD Phase 2 ✅
```

**Tempo-ideal:** Phase 2a ~3 sprints (3 semanas com 1 eng). Phase 2b ~5 sprints
(5 semanas com 2 eng). Total ~2 meses de engenharia focada.

---

# Fora de escopo (explicito)

| Item | Fase |
|---|---|
| Tenant migration entre cells | Phase 3 |
| Per-cell ArgoCD | Phase 3 (começa quando central satura > 500 tenants) |
| Per-cell Gitea replicas | **REJEITADO** (MEET-051 vetou — dual-write split-brain) |
| Auto-sync em qualquer cell | **REJEITADO** (ADR-0007 mandatory manual sync) |
| 3ª+ cells | Phase 3 (automation) |
| Prometheus federation hierárquica | **REJEITADO** (SRE: per-cell + remote-write é mais simples) |
| BGP Anycast | **REJEITADO** (infeasible em DO) |
| GKE Multi-Cluster Gateway | **REJEITADO** (cloud-locked) |

---

# Referências

- **Meeting record:** `docs/meetings/records/MEET-2026-04-17-051.md`
- **ADR base:** `docs/meetings/adr/0008-cell-based-architecture.md`
- **ADR follow-up (to write):** `docs/meetings/adr/0010-phase2-rescope.md`
- **Plano immediate:** `docs/plans/50k-immediate-focus.md` (T-08, T-09)
- **MinIO migration:** `docs/plans/minio-distributed-migration.md`
- **Runbooks:** `docs/runbooks/runtime-capacity-bump.md`, `tenant-bulk-evict.md`, `argocd-sharding.md`
- **Prior ArgoCD meeting:** `docs/meetings/records/MEET-2026-03-31-024.md`

---

## Changelog

- **v1.0 (2026-04-17):** Plano criado a partir do MEET-051. 7 tasks Phase 2a + 17 tasks Phase 2b. Veto conditions materializadas como tasks explícitas. Chaos video como DoD para investor-demo proof.
