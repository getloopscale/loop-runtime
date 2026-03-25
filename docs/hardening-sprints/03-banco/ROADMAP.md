# Sprint 3 — BANCO (PostgreSQL como infra crítica)

**Objetivo:** PG deixa de ser single point of failure. Failover testado empiricamente.
**Confiança:** MÉDIA (failover precisa de teste real, não teórico)
**Esforço:** ~4h
**Pré-requisito:** GATE-SPRINT-2 passou
**Rollback:** PG HA no DO Managed é irreversível (não volta para single-node sem recreation). Mas é upgrade não-destrutivo.

---

## Tasks

### Task 3.1 — Upgrade para HA

**Estado atual:** db-s-1vcpu-2gb, single node, DigitalOcean Managed PG v16.

**Ação:**
1. Arquivo: `infra/pulumi/Pulumi.dev.yaml` — mudar `database.nodes: 1` → `database.nodes: 2`
2. Deploy: `pulumi up --stack dev` (via CI ou manual)
3. Aguardar provisionamento (~10-15min)

**Validação pré-failover:**
- [ ] `doctl databases list` → 2 nodes, status Online
- [ ] API health deep → 200
- [ ] 1 deploy estável pré-failover

---

### Task 3.2 — Teste de failover DURANTE deploy ativo

**ESTE TESTE É OBRIGATÓRIO. Sem ele, HA é teórico.**

**Procedimento:**
1. Terminal 1: `loopzero deploy` (iniciar deploy real)
2. Esperar build status = RUNNING
3. Terminal 2: triggerar failover via DO Console (Database → Actions → Promote standby) ou `doctl databases maintenance-window update`
4. Observar nos logs da API:
   - Pool reconectou? (pgx log: "connection reset", "dial", "established")
   - Em quanto tempo?
5. Observar resultado do deploy:
   - Completou? → Excelente
   - Falhou gracefully (FAILED, não stuck)? → Aceitável
   - Ficou stuck? → BLOCKER — investigar
6. Verificar locks: `SELECT * FROM pg_locks WHERE locktype = 'advisory'` → zero locks orphanados
7. Executar 3 deploys pós-failover

**Registro do teste:**

| Item | Resultado |
|---|---|
| Failover triggado em: | ___ |
| API reconectou em: | ___s |
| Deploy em andamento: | completou / falhou / stuck |
| Locks orphanados: | sim / não |
| Deploy pós-failover 1: | ___ |
| Deploy pós-failover 2: | ___ |
| Deploy pós-failover 3: | ___ |

---

### Task 3.3 — Métricas de locks e pool

**Ação:**
1. Verificar métricas existentes: `curl -s http://<api>:9090/metrics | grep loopzero_`
2. Se não existirem, adicionar em `api/internal/metrics/`:
   - `loopzero_lock_wait_duration_seconds` (histogram) — instrumentar em `api/internal/lock/environment_lock.go`
   - `loopzero_lock_timeout_total` (counter) — incrementar quando lock timeout ocorre
   - `loopzero_db_pool_active_connections` (gauge) — ler de pgxpool stats
   - `loopzero_db_pool_idle_connections` (gauge) — ler de pgxpool stats

**Gate 3.3:**
- [ ] `curl <metrics> | grep loopzero_lock` → métricas presentes
- [ ] `curl <metrics> | grep loopzero_db_pool` → métricas presentes

---

## GATE-SPRINT-3

- [ ] PG com 2 nodes (HA)
- [ ] Failover testado DURANTE deploy ativo — resultado documentado
- [ ] API reconectou em <30s pós-failover
- [ ] Zero locks orphanados
- [ ] Métricas de lock/pool visíveis no Prometheus
- [ ] 3 deploys consecutivos estáveis pós-failover

**SE QUALQUER ITEM FALHAR → NÃO AVANÇAR PARA SPRINT 4.**
