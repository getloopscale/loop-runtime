# Plan: Dogfood 2026-05-08 — Fixes for 7 Findings

> **Version 1.1** — Corrigir os 7 achados (3× S1, 2× S2, 2× S3) detectados pelo `/dogfood full` em 2026-05-08 contra cleanroom recém-recriado. Os achados expõem (a) infraestrutura local que diverge de produção (migrações, OTel), (b) regressão silenciosa de feature crítica (ESO ExternalSecret não criado), (c) dead code com falha contínua observável (`build_events` GC), (d) gap de placement no path de seed E2E, (e) state-machine que não converge para `Healthy`, e (f) test scaffolding usando token legado. Pre-flight 10/11 PASS, journey suite 74/75 (98.7%), edge suite 17/21 (81.0%). Após este plano, todos os 7 achados estarão fechados, cleanroom dogfood deve voltar a 100% sem fail genuíno.

## Context

### Achados do dogfood 2026-05-08 (referência: `/tmp/dogfood-ginkgo-1778242738.xml` + `/tmp/dogfood-ginkgo-edge-1778245661.xml`)

| ID | Severidade | Categoria | Resumo |
|----|-----------|-----------|--------|
| F-1 | S1 | Migration debt + Local≠Production | `task db:migrate` faz `psql < $f` em loop sem golang-migrate; tabela `schema_migrations` ausente. Forward-only frágil dependendo de `IF NOT EXISTS` em todo SQL. |
| F-2 | S1 | Integração vertical CLI→API→ESO quebrada | `loopzero secret set` não criou `ExternalSecret` CR no workload cluster; pod ficou em `CreateContainerConfigError` aguardando Secret que nunca chegou. Journey 03 timeout 300s. |
| F-3 | S1 | Code rot + silent failure | `api/internal/db/build_gc_store.go:54` faz `DELETE FROM build_events` mas a tabela jamais foi criada. ERROR log a cada GC tick sem alerta. |
| F-4 | S2 | Multi-cell placement skip path | Tenants criados via `infra/tests/seed/seed.go` fazem `INSERT INTO tenants` sem `cell_id`. Edge multi-cell test falha porque tenant `e2e-tenant` sem placement. |
| F-5 | S3 | Test scaffolding bug | `edge_concurrency_test.go:137` e 8+ helpers usam `seed.AuthToken` legado em vez de `resolveToken()`. API prod-mode rejeita com 401, 3 specs falham. |
| F-6 | S2 | State-machine não-converge | 9 `LoopZeroApplication` CRs stuck em `Progressing` 36-74min após pods Running. `reconcileRouteStatus` depende de `ExposedRoute.Status.Phase==Reachable` que nunca evolui localmente. |
| F-7 | S3 | Observability noise local | `OTEL_EXPORTER_OTLP_ENDPOINT` aponta para collector inexistente em local; spam ERROR a cada 5s degrada legibilidade. |

### Evidência concreta

- `taskfiles/db.yml:104` — `task db:migrate` executa `for f in $(ls *.up.sql | sort); do kubectl exec ... psql < $f; done`. Não chama `golang-migrate`. Mas `api/Dockerfile:25` instala `github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1` — binário existe mas não é usado.
- `api/internal/db/build_gc_store.go:54` — `DELETE FROM build_events WHERE created_at < $1`. PG `\dt` mostra `sse_events` e `webhook_events` mas NÃO `build_events`.
- `api/internal/k8s/secret_k8s_manager.go:175-181` — path correto de criação de ExternalSecret existe; em local, ESO controller silent + `kubectl get externalsecret -A` returns `No resources found`. API logs (excluindo OTel spam) sem nenhuma menção a "secret" no path.
- `infra/tests/seed/seed.go:74` — `INSERT INTO tenants (id, display_name, slug, owner_id) VALUES (...) ON CONFLICT (id) DO NOTHING` — **sem `cell_id`**.
- `infra/tests/e2e/edge_concurrency_test.go:137` — `req.Header.Set("Authorization", "Bearer "+seed.AuthToken)` ignora `resolveToken()` que prioriza `LOOPZERO_TOKEN` env.
- `operators/internal/app/application_controller.go:301` — `computePhase` retorna `Progressing` quando `Reachable=false && EndpointsReady=false && (no Down/Degraded condition reasons)`.
- `api/internal/tracing/tracing.go:56` — `OTEL_EXPORTER_OTLP_ENDPOINT` lido de env. `LOOPZERO_TRACING_ENABLED=false` desliga sem overhead. Local não seta nenhum dos dois.

## Objective

**Done =** Os 7 findings têm fix mergeado em `develop`, validado por `/dogfood full` em cleanroom, com zero S0/S1 finding remanescente relacionado a estes 7 itens.

Metas mensuráveis:

1. `task db:migrate` invoca `golang-migrate up` real; `schema_migrations` table presente após qualquer setup; re-run é idempotente sem re-aplicar migrations já registradas.
2. Path `loopzero secret set` instrumentado com structured log + métrica `loopzero_secret_externalsecret_created_total{result}`; debug actionable se silently skip ocorrer.
3. Função `DeleteOldEvents` em `build_gc_store.go` removida (junto com qualquer caller); zero referências a `build_events` no codebase.
4. `infra/tests/seed/seed.go` chama `RoundRobinEngine.AssignAndExecute` (ou injeta `cell_id` direto via SQL) para tenants seed.
5. `edge_concurrency_test.go` + 8 helpers em `cli_*_test.go` usam `resolveToken()`; `seed.AuthToken` deprecated com comment apontando alternativa.
6. `ApplicationController.reconcileRouteStatus` adiciona caminho de "fast Healthy" quando workload Pod Ready=true (independente de ExposedRoute Reachable).
7. Local Tilt deployment seta `LOOPZERO_TRACING_ENABLED=false` por default (override permite ativar).

## ADRs

### D1 — Adopt golang-migrate canonical via task db:migrate
- **Decisão:** Substituir `for f in *.up.sql; do psql < $f` por `migrate -path -database -verbose up`. Binário já está em `api/Dockerfile`, expor para invocação via `kubectl exec` ou via Job pattern.
- **Rationale:** Idempotência, version tracking, rollback capability, parity local↔prod (assumindo prod usará o mesmo binário). Alternativa: implementar tracking via `IF NOT EXISTS` rigorosamente — rejected por dobrar manutenção e não dar rollback.
- **Consequências:** **Habilita** version-aware migrations + rollback futuro + auditabilidade. **Constrange** que toda nova migration tenha `.up.sql` com naming convention canonical (`NNNNNN_name.up.sql`) e que a tabela `schema_migrations` exista (golang-migrate cria automaticamente).

### D2 — Delete `build_events` GC method instead of adding migration
- **Decisão:** Remover `DeleteOldEvents` em `build_gc_store.go` e o caller scheduler. Não criar tabela `build_events`.
- **Rationale:** YAGNI. SSE events já são gerenciados via `sse_events` (com TTL próprio). Webhook deliveries via `github_webhook_deliveries`. Não há feature usando `build_events`. Adicionar migration apenas para tabela vazia que ninguém escreve = dead code resuscitação. Rule "Legacy code is an error. Delete it."
- **Consequências:** **Habilita** logs limpos sem ERROR contínuo. **Constrange** que se algum dia precisarmos de tabela específica para build events (timeline UI?), fazemos migration apropriada com schema de fato.

### D3 — Instrument SecretK8sManager with structured log + metric, NOT changing flow
- **Decisão:** Adicionar structured log "ExternalSecret created" e métrica Prometheus em `CreateOrUpdate` path. Não alterar lógica de criação. Investigar se construtor está retornando manager nil em local antes de assumir bug em flow.
- **Rationale:** Silent-skip é o sintoma, não a causa. Sem instrumentation, próxima recorrência é igualmente invisível. Corrigir flow sem entender causa-raiz arrisca whack-a-mole. Padrão registrado em memória `reference_audit_loop_session.md` ("silent-success").
- **Consequências:** **Habilita** debugging de regressões futuras. **Constrange** adicionar 1 metric + 1 log line por path; impacto cardinality controlado (labels: result=success|fail, tenant=X).

### D4 — Seed path injects cell_id via SQL (not via placement engine)
- **Decisão:** `infra/tests/seed/seed.go` faz `INSERT INTO tenants (..., cell_id) VALUES (..., 'cell-default') ON CONFLICT (id) DO UPDATE SET cell_id = COALESCE(tenants.cell_id, 'cell-default')`. Não chamar API placement engine.
- **Rationale:** Seed roda em SQL puro (`psql exec`); chamar API placement requer JWT, HTTP, etc. Para test fixture, hardcoded `cell-default` é aceitável (ADR-0008 default cell). O OPERADOR de testes E2E não testa placement decisions — testes específicos de Multi-Cell já fazem placement explicit via `loopzero admin tenants pin`.
- **Consequências:** **Habilita** Edge Multi-Cell test passar sem race. **Constrange** que se algum dia `cell-default` não existir (não-config válido), seed falha — mas isso é DESIRED (early fail).

### D5 — `resolveToken()` é canonical helper para JWT em testes E2E
- **Decisão:** `seed.AuthToken` mantém-se como fallback em `resolveToken()` mas helpers diretos (`deployWithIdempotencyKey`, etc.) DEVEM chamar `resolveToken()`. Add lint rule (or comment) em `seed.AuthToken` advising "use resolveToken() unless you really mean dev-mode-only".
- **Rationale:** Dois mecanismos coexistindo é confuso. Single canonical via `resolveToken()` evita 401 fantasmas. Memória `reference_e2e_journey_76_76_green_2026_05_06.md` documenta caso onde scaffolding bugs camuflaram problemas reais.
- **Consequências:** **Habilita** edge concurrency tests funcionarem. **Constrange** mais boilerplate em helpers (1 linha extra `token := resolveToken()`).

### D6 — ApplicationController fast-path Healthy when workload Pod Ready
- **Decisão:** `reconcileRouteStatus` adiciona check: se `app.Spec.WorkloadKind in {Deployment, Rollout}` AND workload está Ready/Healthy no workload cluster, `app.Status.EndpointsReady = true` mesmo se ExposedRoute ainda não está Reachable.
- **Rationale:** Production-fitness. Pod respondendo `/health` 200 é a fonte de verdade real. ExposedRoute reachable depende de Traefik route propagation + DNS + cert — ortogonal ao app estar funcional. Em local, sem DNS público, ExposedRoute pode nunca ficar Reachable. Em prod, latência de DNS pode atrasar ExposedRoute por minutos enquanto pod já serve.
- **Consequências:** **Habilita** Application phase converge para `Serving` ou `Healthy` mesmo em local. **Constrange** controller ler workload cluster — DEPS acréscimo. Mitigação: usar status já cached pelo Tenant CR ou Watch cross-cluster.

### D7 — Disable OTel exporter locally by default
- **Decisão:** Tilt deployment YAML em `infra/tilt/api-deployment.yaml` seta `LOOPZERO_TRACING_ENABLED=false` por default. Override via env var permite ativar quando Tempo está rodando.
- **Rationale:** OTel collector NÃO está instalado em local mgmt cluster. Logs ERROR a cada 5s ficam ruído. `tracing.go:Init` já tem feature flag — só usar.
- **Consequências:** **Habilita** logs limpos local. **Constrange** dev que quer testar tracing precisa exportar `LOOPZERO_TRACING_ENABLED=true` + ter Tempo running. Adequate trade-off.

## Dependency Graph

```
Phase 0 (Quick wins, paralelizáveis)
   ├── T0.1: F-7 OTel local off
   └── T0.2: F-5 test scaffolding seed.AuthToken→resolveToken

Phase 1 (S1 cleanup, paralelizáveis)
   ├── T1.1: F-3 build_events delete
   └── T1.2-T1.4: F-1 golang-migrate adoption

Phase 2 (S1 deep, sequencial após Phase 1)
   └── T2.1-T2.3: F-2 SecretK8sManager observability + diagnose

Phase 3 (S2 multi-cell, paralelo a Phase 2)
   └── T3.1-T3.2: F-4 seed cell_id injection

Phase 4 (S2 application phase, paralelo a Phase 2)
   └── T4.1-T4.2: F-6 ApplicationController fast-path Healthy

Phase 5 (Final validation, sequencial após Phase 4)
   └── /dogfood full em cleanroom
```

| Phase | Sequencial após | Paraleliza com | Estimativa |
|-------|-----------------|----------------|------------|
| 0 | — | — | 30min |
| 1 | — | 0 | 90min |
| 2 | 1 | 3, 4 | 90min |
| 3 | 1 | 2, 4 | 60min |
| 4 | 1 | 2, 3 | 90min |
| 5 | 2,3,4 | — | 60min |

**Total runtime estimate:** ~6h (paralelizando 1+0 e 2+3+4 em duas tracks).

---

## Phase 0: Quick wins

**Objective:** Resolver achados de menor severidade (S3) que reduzem ruído em log e desbloqueiam tests sem auth correto.

### T0.1 — Disable OTel exporter locally by default

#### Objective
Eliminar spam de log "traces export: rpc error: code = Unavailable" do API local.

#### Evidence
- `api/internal/tracing/tracing.go:56` lê `OTEL_EXPORTER_OTLP_ENDPOINT`
- `tracing.go:Init` tem early-return quando `LOOPZERO_TRACING_ENABLED=false` ("default true em dev, false em prod")
- Dogfood 2026-05-08 OBS-1: 5+ ERROR linhas/min, todas relacionadas a OTel
- Local k3d mgmt cluster NÃO tem Tempo/OTel collector instalado

#### Files to edit
```
infra/tilt/api-deployment.yaml — adicionar env LOOPZERO_TRACING_ENABLED=false (NEW env block)
infra/helm/loopzero-api/values.yaml — adicionar default tracing.enabled=false
infra/helm/loopzero-api/templates/deployment.yaml — gate LOOPZERO_TRACING_ENABLED env via .Values.tracing.enabled
docs/runbooks/local-development.md — documentar como reabilitar tracing localmente (NEW section, append)
```

#### Deep file dependency analysis
- `infra/tilt/api-deployment.yaml`: deployment YAML usado por Tilt para hot-reload local. Adicionar env não impacta produção.
- `infra/helm/loopzero-api/values.yaml` + `templates/deployment.yaml`: Helm chart de bootstrap (helmfile L2). Default false só vale se sobrescrever. Production via ArgoCD L3 (chart `charts/loopzero-api`) MANTÉM tracing default true (overrideable).
- `tracing.go:isEnabled()` já trata flag corretamente — nenhuma mudança Go necessária.

#### Deep Dives
- **Invariante:** `LOOPZERO_TRACING_ENABLED=false` deve resultar em ZERO log linhas relacionadas a OTel/tracing.
- **Edge case:** se dev seta `LOOPZERO_TRACING_ENABLED=true` mas Tempo não está rodando, comportamento existente (`tracing.go:Init` retorna error mas continua) preserve — não regredir.

#### Tasks
1. Edit `infra/tilt/api-deployment.yaml` — adicionar `env: [{name: LOOPZERO_TRACING_ENABLED, value: "false"}]` no container loopzero-api.
2. Edit `infra/helm/loopzero-api/values.yaml` — adicionar bloco `tracing: {enabled: false}` (default).
3. Edit `infra/helm/loopzero-api/templates/deployment.yaml` — adicionar `{{ if not .Values.tracing.enabled }}env: ... LOOPZERO_TRACING_ENABLED: "false"{{ end }}`.
4. Append `docs/runbooks/local-development.md` — section "Habilitando tracing local: `export LOOPZERO_TRACING_ENABLED=true && task tilt:up`".
5. Reload Tilt: `tilt down && tilt up` para aplicar.
6. Verify: `kubectl logs -n loopzero-system deploy/loopzero-api --tail=100 | grep -c "traces export"` deve retornar 0 após 1min.

#### TDD
```
RED:    test_tracing_disabled_returns_noop_shutdown — em api/internal/tracing/tracing_test.go (existing); set LOOPZERO_TRACING_ENABLED=false, assert Init returns nil err and shutdown is no-op.
RED:    test_helm_loopzero_api_tracing_disabled_default — em api/internal/helm_chart_test.go (NEW); render chart with default values, assert deployment env contains LOOPZERO_TRACING_ENABLED=false.
GREEN:  Implement tasks 1-4 above.
REFACTOR: None expected — single-responsibility env addition.
VERIFY:  cd api && go test ./internal/tracing/... && task validate:helm-charts && kubectl rollout status -n loopzero-system deploy/loopzero-api --timeout=60s
```

#### Acceptance Criteria
- [ ] `kubectl logs -n loopzero-system deploy/loopzero-api --tail=500 | grep "traces export"` returns 0 lines (post Tilt reload)
- [ ] `helm template loopzero-api infra/helm/loopzero-api/ | grep "LOOPZERO_TRACING_ENABLED"` shows value `"false"`
- [ ] Production deploys via ArgoCD UNCHANGED (Helm chart `charts/loopzero-api` not modified)
- [ ] Pass: lint + tests in `api/internal/tracing/` + helm validate

#### DoD
- [ ] Tilt re-deploys cleanly (no API restart loop)
- [ ] Zero "traces export" lines em 5min de operação
- [ ] Doc atualizado com toggle instructions
- [ ] PR menciona ADR-0024 (D7 desta plan)

---

### T0.2 — Test scaffolding cleanup: seed.AuthToken → resolveToken()

#### Objective
Eliminar 4 fails do edge suite causados por uso de `seed.AuthToken` (legacy dev-mode-only) ao invés de `resolveToken()`.

#### Evidence
- `infra/tests/e2e/helpers_test.go` define `resolveToken()` com hierarquia documentada (LOOPZERO_TOKEN env > token file > seed.AuthToken legacy).
- `infra/tests/e2e/edge_concurrency_test.go:137` ignora helper e usa `seed.AuthToken` direto.
- 9+ outros tests fazem `os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)` — same anti-pattern (esses funcionam por sorte, porque `resolveToken()` resolve env primeiro).
- Dogfood edge fails: `idempotency key replay`, `3 concurrent deploys`, `lock released after build completes` — todos HTTP 401.

#### Files to edit
```
infra/tests/e2e/edge_concurrency_test.go — substituir seed.AuthToken por resolveToken() em deployWithIdempotencyKey
infra/tests/e2e/cli_secret_delete_test.go — refactor BeforeEach Setenv to use resolveToken()
infra/tests/e2e/cli_connect_test.go — same
infra/tests/e2e/cli_project_create_test.go — same
infra/tests/e2e/cli_env_delete_test.go — same
infra/tests/e2e/cli_env_list_test.go — same (2 sites)
infra/tests/e2e/cli_status_test.go — same (2 sites)
infra/tests/e2e/cli_billing_test.go — same
infra/tests/seed/seed.go — adicionar comment em AuthToken docstring: "DEPRECATED for HTTP API tests; use resolveToken() in helpers_test.go"
```

#### Deep file dependency analysis
- `edge_concurrency_test.go`: 3 specs usam `deployWithIdempotencyKey`. Single fix corrige 4 [FAILED] markers (3 fails lógicos + cascade).
- 8 `cli_*_test.go` files: cada um usa `os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)`. Substituir por `os.Setenv("LOOPZERO_TOKEN", resolveToken())` é null-op semântico HOJE (porque resolveToken pega LOOPZERO_TOKEN env primeiro), MAS protege contra regressão futura se alguém deletar `seed.AuthToken`.
- `seed/seed.go`: docstring update apenas. Não delete `seed.AuthToken` (Go test infra still uses it as fallback).

#### Deep Dives
- **Invariant:** `resolveToken()` deve sempre retornar non-empty para tests E2E rodarem.
- **Edge case:** Se nem `LOOPZERO_TOKEN` env nem token file existem, `resolveToken()` retorna `seed.AuthToken` (legacy). Tests rodando out-of-band sem seeded JWT continuam compilando, mesmo que falhem em runtime — preserve current behavior.

#### Tasks
1. Edit `edge_concurrency_test.go:137` — `req.Header.Set("Authorization", "Bearer "+resolveToken())`.
2. Grep `seed.AuthToken` in `infra/tests/e2e/`, replace each `os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)` → `os.Setenv("LOOPZERO_TOKEN", resolveToken())`.
3. Add docstring deprecation comment to `seed.AuthToken` const.
4. Re-run only the failing edge specs to verify fix.

#### TDD
```
RED:    Re-run edge_concurrency tests (`ginkgo --focus="idempotency key replay"`) — observe HTTP 401 (current state)
GREEN:  Apply replacement in step 1; re-run, expect HTTP 200/201/202 + same build_id on replay
REFACTOR: Simplify by extracting "auth header builder" helper (optional — 3 specs is borderline DRY threshold)
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Concurrency + Idempotency" --label-filter=edge-case ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] `grep -rn "seed.AuthToken" infra/tests/e2e/*.go | grep -v "// DEPRECATED" | wc -l` returns 0 (apart from doctring reference)
- [ ] Edge fails 3 → 0 (excluding F-4 Multi-Cell which is a separate root cause)
- [ ] No new fails introduced in journey suite

#### DoD
- [ ] Edge suite passes idempotency, concurrent, lock-release specs
- [ ] PR includes `ginkgo --focus="Concurrency + Idempotency"` showing GREEN

---

## Phase 1: S1 critical fixes — migrations + dead code

**Objective:** Resolver as duas falhas estruturais mais críticas (F-1 + F-3) que afetam confiabilidade local↔prod e geram observability noise.

### T1.1 — Delete `build_events` GC dead code (F-3)

#### Objective
Remover toda referência a tabela inexistente `build_events`, eliminando ERROR contínuo no log do API.

#### Evidence
- `api/internal/db/build_gc_store.go:54` — `DELETE FROM build_events` referencia tabela ausente.
- PG `\dt` lista 26 tabelas LoopZero — `build_events` NÃO está entre elas.
- `git log --all -- api/internal/db/migrations/*build_events*` — 0 commits criando tal migration.
- Dogfood OBS-1: 9 ERROR lines em 2000-line log (não-OTel) atribuíveis a este path.

#### Files to edit
```
api/internal/db/build_gc_store.go — remover DeleteOldEvents method
api/internal/services/build_gc.go — remover caller de DeleteOldEvents (se existir)
api/cmd/setup_background.go — remover scheduler de DeleteOldEvents
api/internal/db/build_gc_store_test.go — remover tests de DeleteOldEvents
```

#### Deep file dependency analysis
- `build_gc_store.go`: package `db`, exporta `PostgresBuildGCStore`. Method `DeleteOldEvents` é interface-required? Verificar interface `services.BuildGCStore` para saber se há outros implementadores.
- `services/build_gc.go`: scheduler que chama `DeleteOldBuilds`, `DeleteOldEvents`, `DeleteOldWebhookDeliveries` em loop. Removeremos o middle call.
- `setup_background.go`: wires the scheduler at startup. Pode ou não precisar update.
- Testes em `build_gc_store_test.go`: remover só testes que invocam `DeleteOldEvents`.

#### Deep Dives
- **Invariant:** Após delete, `task lint && go test ./...` passa em api/. Se interface `BuildGCStore` exige `DeleteOldEvents`, remove from interface também.
- **Edge case:** Se algum lugar (ex.: admin endpoint, dashboard) chama esse path, DELETE quebra. Grep: `grep -rn "DeleteOldEvents" api/`.

#### Tasks
1. Grep all callers of `DeleteOldEvents` in `api/`. Document them.
2. Remove method from `build_gc_store.go`.
3. Remove from `services.BuildGCStore` interface (if defined).
4. Remove caller in `services/build_gc.go` scheduler loop.
5. Remove tests in `build_gc_store_test.go`.
6. Update `services/build_gc_test.go` if it mocks the interface.
7. Run `go build ./...` and `go test ./...` in api/.

#### TDD
```
RED:    Reproduce — `kubectl logs -n loopzero-system deploy/loopzero-api --tail=500 | grep "build_events"` shows N hits
        Test: `test_build_gc_store_no_longer_references_build_events` (NEW) — assert no `build_events` token in compiled binary or source
GREEN:  Apply tasks 1-6.
REFACTOR: If `BuildGCStore` interface had only 3 methods, consider whether the abstraction is still needed (KISS). Evaluate.
VERIFY:  cd api && go build ./... && go test ./internal/db/... ./internal/services/...
```

#### Acceptance Criteria
- [ ] `grep -rn "build_events" api/ pkg/` returns 0 hits (excluindo este plano e changelog)
- [ ] `kubectl logs -n loopzero-system deploy/loopzero-api --since=5m | grep "build_events"` returns 0 lines
- [ ] All api/ unit tests pass
- [ ] `go vet ./...` clean

#### DoD
- [ ] PR removes the method, the caller, and tests
- [ ] CHANGELOG entry: `Removed: dead code DeleteOldEvents in build GC (table never existed) (#NNN)`

---

### T1.2 — Adopt golang-migrate canonical (F-1) — Refactor task db:migrate

#### Objective
Substituir `task db:migrate` plain-psql por `golang-migrate up` real. Tabela `schema_migrations` presente após qualquer setup; idempotente; skipping migrations já registradas.

#### Evidence
- `api/Dockerfile:25` instala binary `golang-migrate/migrate@v4.19.1` mas `taskfiles/db.yml:104` não usa.
- Dogfood pre-flight: `schema_migrations` ausente em mgmt PG.
- Memory `feedback_live_test_existing_db` documenta caso onde silent-skip mascarou bug em 000003.
- `task db:migrate` atual depende de cada migration ser `IF NOT EXISTS`. Forward-only frágil.

#### Files to edit
```
taskfiles/db.yml — refactor db:migrate task to use golang-migrate
infra/scripts/run-migrations.sh (NEW) — wrapper script for kubectl exec golang-migrate
api/internal/db/migrations/000001_initial_schema.up.sql — review for non-idempotent pieces (must still work since prod uses this path)
docs/runbooks/database-migrations.md — document new flow + how to inspect schema_migrations (NEW or update)
```

#### Deep file dependency analysis
- `taskfiles/db.yml:104`: defines `db:migrate`. Will replace command body.
- `infra/scripts/run-migrations.sh`: NEW file. Encapsulates `kubectl exec ... migrate -path /migrations -database $DSN -verbose up`. Idempotent.
- The PG pod doesn't have golang-migrate binary. **Option A**: invoke from loopzero-api pod (which has the binary in `api/Dockerfile`). **Option B**: invoke from a sidecar/Job. **Option C**: install golang-migrate locally in dev machine and connect via port-forward.
- **Decision per ADR D1**: Option A (from loopzero-api pod) is most-DRY because the binary already lives there. Pod has a volume mount of `/app/migrations` (we'll add).

#### Deep Dives
- **migrate command semantics:** `migrate -path /migrations -database $DSN up` reads files, compares with `schema_migrations` table, applies only missing ones. Creates table if absent.
- **DSN format:** `postgres://loopzero:loopzero-dev-password@postgresql:5432/loopzero?sslmode=disable&x-migrations-table=schema_migrations`.
- **Backfill of existing local DB:** Local DB has all tables but no `schema_migrations`. golang-migrate will see "no version applied" and try to re-apply EVERYTHING — most will succeed via `IF NOT EXISTS` but some may fail. **Solution**: use `migrate force <N>` to set version=N (highest existing migration) WITHOUT re-applying. Wrapper script must support this initial backfill.
- **Edge case (production):** Prod likely already runs `golang-migrate` via Pulumi or operator; check before merging — don't break prod.
- **Invariant:** `task db:migrate` re-run after first call should be no-op (zero migrations applied; "no change" log message).

#### Tasks
1. **EC-1 mitigation:** Pre-check ConfigMap viability — `wc -c api/internal/db/migrations/*.up.sql | tail -1`. If sum > 900KB: switch to **initContainer** that copies migrations from loopzero-api image (which already has them in `/app/migrations` per Dockerfile) into emptyDir mounted at `/migrations`. If < 900KB: use ConfigMap.
2. Add volume mount in `loopzero-api` Helm chart per option chosen in step 1. Either way, set `checksum/migrations` annotation on Deployment metadata to force pod restart on changes (avoids "stale ConfigMap" trap).
3. Write `infra/scripts/run-migrations.sh`:
   - Detect mgmt context.
   - **EC-2 mitigation:** Compute highest dynamically: `HIGHEST=$(ls /migrations/*.up.sql | sort -V | tail -1 | grep -oE '^[0-9]+' | sed 's/^0*//')`.
   - `kubectl exec -n loopzero-system deploy/loopzero-api -- migrate -path /migrations -database "$DSN" -verbose up`.
   - Detect "first run on existing DB": if `schema_migrations` absent AND `tenants` exists, run `migrate force "$HIGHEST"` first (NOT hardcoded 10).
4. Refactor `taskfiles/db.yml:104` `db:migrate` to call `bash infra/scripts/run-migrations.sh`.
5. Manual one-time backfill in dev: rely on the dynamic-highest detection in the script (no hardcoded version).
6. Document in `docs/runbooks/database-migrations.md`.

#### TDD
```
RED:    test_run_migrations_creates_schema_migrations_table_on_fresh_db (NEW shell test in infra/tests/scripts/run-migrations_test.sh)
RED:    test_run_migrations_is_idempotent_on_re_invoke (NEW)
RED:    test_run_migrations_force_handles_existing_db_without_schema_migrations (NEW)
GREEN:  Implement script + Helm volume mount + Taskfile refactor.
REFACTOR: Extract DSN building to function.
VERIFY:  task db:reset && task db:migrate && task db:migrate (second call should be no-op)
         kubectl exec -n loopzero-system postgresql-0 -- psql -U loopzero -d loopzero -c "SELECT version, dirty FROM schema_migrations;"
```

#### Acceptance Criteria
- [ ] `task db:migrate` invokes golang-migrate via wrapper
- [ ] Fresh DB: `schema_migrations` table created with all 10 versions
- [ ] Re-run: zero migrations applied, exit 0
- [ ] Existing DB without `schema_migrations`: backfill via force completes
- [ ] Production migration path NOT changed (verify by reading current Pulumi migration job; if it already uses golang-migrate, document parity; if not, consider follow-up plan)

#### DoD
- [ ] CHANGELOG: `Changed: task db:migrate now uses golang-migrate canonical with version tracking`
- [ ] Doc updated
- [ ] Memory `feedback_live_test_existing_db` updated to reflect fix

---

### T1.3 — Backfill schema_migrations in existing local DB

#### Objective
One-time correction of cleanroom + warm dev DBs: register version=10 in `schema_migrations` so future runs are clean.

#### Evidence
- Local DB has all 10 migration's tables but no `schema_migrations` table.
- Without backfill, T1.2 wrapper script will try to re-apply everything → mixed results.

#### Files to edit
```
infra/scripts/run-migrations.sh — already covers via "force" detection in T1.2
infra/scripts/k3d-cross-cluster-heal.sh — add H17: backfill schema_migrations if missing
```

#### Deep file dependency analysis
- `k3d-cross-cluster-heal.sh`: list of self-healing recipes. New H17 codifies the backfill so cleanroom OR healing run never trips again.
- Goal: make this a one-time issue solved by infrastructure.

#### Tasks
1. Add H17 entry to `k3d-cross-cluster-heal.sh` invoking `bash infra/scripts/run-migrations.sh --force-if-stale`.
2. Add matrix entry in validation matrix doc.

#### TDD
```
RED:   Drop schema_migrations on running DB; verify dogfood pre-flight FAILS
GREEN: Run task k3d:heal; pre-flight PASSES
VERIFY: psql -c "SELECT * FROM schema_migrations" returns N=10 rows
```

#### Acceptance Criteria
- [ ] After `task k3d:heal`, `schema_migrations` exists with all 10 versions
- [ ] Heal is idempotent (re-run is no-op)

#### DoD
- [ ] PR adds H17 + matrix entry
- [ ] Memory `reference_validation_sweep_2026_04_30` updated

---

### T1.4 — Verify production parity for migrations

#### Objective
Confirm production migration path uses golang-migrate (not raw psql). If not, document gap and create follow-up plan.

#### Evidence
- Need to read `infra/pulumi/src/digitalocean/` for migration job/Helm hook.
- `api/Dockerfile:25` installs migrate binary — strong signal prod uses it.

#### Files to edit
```
docs/architecture/database/migrations.md — document current prod path (NEW or update)
```

#### Tasks
1. Read `infra/pulumi/src/digitalocean/` for migration job.
2. Document findings.
3. If prod uses raw psql like dev did, file FOLLOW-UP plan (separate from this).

#### Acceptance Criteria
- [ ] Documented prod migration path
- [ ] If gap exists: follow-up plan created

#### DoD
- [ ] doc committed

---

## Phase 2: SecretK8sManager observability + diagnose (F-2)

**Objective:** Restaurar Journey 03 Secrets passing. Investigar causa-raiz do silent skip e instrumentar para detectar regressões futuras.

### T2.1 — Diagnose: why ExternalSecret was not created during Journey 03

#### Objective
Determinar empiricamente o que aconteceu quando `loopzero secret set` foi chamado mas ExternalSecret não foi criado.

#### Evidence
- API logs nos 5min antes do timeout: ZERO menção a "secret", "external", ESO. Apenas OTel spam.
- `secretK8sSync` em `setup_k8s.go:38` é construído com `cfg.RuntimeKubeconfig` + `cfg.RuntimeKubeContext`. Se construct fail, log "secret K8s manager not available — ESO sync disabled".
- Search por essa msg em logs do dogfood: 0 hits. Ou success mas zero log de "criando ExternalSecret", ou silent failure no path interno.

#### Files to edit
```
infra/scripts/dogfood-diagnose-eso.sh (NEW) — debugger script
```

#### Deep file dependency analysis
- We need to TRY to reproduce the issue: re-run Journey 03 with extra logging or directly invoke `loopzero secret set` against fresh tenant and watch:
  - Is `secretK8sSync` non-nil at boot? Verify via API logs at startup.
  - Does `secret_k8s_manager.CreateOrUpdate` execute? Add temporary log if needed.
  - Does ESO controller in workload cluster see the ExternalSecret CR? Watch ESO controller logs.

#### Tasks
1. Write `infra/scripts/dogfood-diagnose-eso.sh`:
   - Create test tenant + project + env.
   - Set LOOPZERO_TOKEN + URL.
   - Invoke `loopzero secret set TEST_KEY=test_value`.
   - Immediately check: API log for "ExternalSecret" or "secretK8sSync"; workload cluster `kubectl get externalsecret -A`; ESO controller logs.
2. Run script against current cleanroom.
3. Document findings in `docs/runbooks/sprint8-eso-debug.md`.

#### TDD
```
RED:    Run dogfood-diagnose-eso.sh; observe whatever silent path is being taken.
ACTION: Capture evidence (logs, kubectl outputs, API trace).
GREEN:  Identify root cause from collected evidence.
```

#### Acceptance Criteria
- [ ] Root cause documented with reproducer
- [ ] Memory updated with finding

#### DoD
- [ ] Findings drive T2.2 fix

---

### T2.2 — Fix root cause from T2.1

#### Objective
Apply the fix identified by T2.1.

**Note:** Fix scope cannot be predicted without diagnose. Most likely candidates:
- (a) `RuntimeKubeconfig` env var not mounted in API → manager constructs but with bad config
- (b) Path `loopzero secret set` → API does NOT route to `secret_k8s_manager.CreateOrUpdate`
- (c) ESO ClusterSecretStore `loopzero-mgmt-store` mis-configured → ExternalSecret CR creation rejected

#### Files to edit
```
api/internal/k8s/secret_k8s_manager.go — likely add structured logging
api/internal/services/secret_service.go — verify call path to k8s manager
```

#### Deep file dependency analysis
- Will be filled in based on T2.1 findings.

#### TDD
```
RED:    Reproduce via T2.1 diagnose script — confirm bug
GREEN:  Apply targeted fix
REFACTOR: Add log if missing
VERIFY: dogfood-diagnose-eso.sh now shows successful create
```

#### Acceptance Criteria
- [ ] dogfood-diagnose-eso.sh shows ExternalSecret created in <5s
- [ ] ESO controller logs show "secret synced"

#### DoD
- [ ] Journey 03 passes in fresh re-run

---

### T2.3 — Instrumentation: structured log + Prometheus metric for ESO path

#### Objective
Adicionar `loopzero_secret_externalsecret_total{result="success|fail|skip"}` + structured log linha "ExternalSecret created namespace=X name=Y" para detecção de regressões.

#### Evidence
- ADR D3 — silent-skip foi invisible exatamente por falta de telemetria.
- Padrão existente: `loopzero_*` metrics em `/metrics` endpoint do API.

#### Files to edit
```
api/internal/k8s/secret_k8s_manager.go — adicionar log + metric instrumentation
api/internal/metrics/metrics.go — declarar nova métrica
api/internal/k8s/secret_k8s_manager_test.go — testar log + metric emission
```

#### Tasks
1. **EC-3 mitigation:** Declare `loopzeroSecretExternalSecretTotal` em `metrics.go` com labels APENAS `result="success|fail|skip"` (NÃO usar `tenant` label — cardinality unbound em prod com 10k+ tenants). Tenant info vai apenas em log estruturado.
2. Em `secret_k8s_manager.go:CreateOrUpdate` chamar `metrics.IncSecretESO("success")` no success path; "fail" no error path.
3. Add slog `slog.Info("ExternalSecret created", "namespace", ns, "name", name, "tenant", tenantID)` — `name` é o secret NAME (ex.: API_KEY); secret VALUE NUNCA aparece em log nem metric.
4. Tests usando `prometheus/testutil` para assert metric incremented.
5. **EC-8 SHOULD TEST:** assert que secret VALUE não aparece em capturado slog.

#### TDD
```
RED:    test_create_secret_emits_externalsecret_success_metric — uses fake K8s client + assertion on metric value
RED:    test_create_secret_emits_externalsecret_fail_metric_on_error — fake client returns error
RED:    test_create_secret_logs_structured_message — captures slog and asserts presence
GREEN:  Implement instrumentation
REFACTOR: Consider extracting metrics-emission helper if logic gets longer
VERIFY: cd api && go test ./internal/k8s/... -run "TestCreateSecret"
```

#### Acceptance Criteria
- [ ] Metric `loopzero_secret_externalsecret_total` exposed at /metrics
- [ ] After `loopzero secret set`, metric value increases by 1
- [ ] Log line "ExternalSecret created" present in API stdout

#### DoD
- [ ] Tests passing
- [ ] Metric documented in `docs/observability/metrics.md`

---

## Phase 3: Multi-cell seed cell_id (F-4)

**Objective:** Tenants criados pelo path E2E seed têm `cell_id` populado. Edge Multi-Cell test passa sem race.

### T3.1 — Inject cell_id in seed.go INSERT

#### Objective
`infra/tests/seed/seed.go` faz INSERT com `cell_id='cell-default'` nativo, e UPDATE conditional que preserva cell existente.

#### Evidence
- Dogfood Edge Multi-Cell falha em 3.7s: `tenant "e2e-tenant" must have a cell_id (placement once)`.
- `seed.go:74` faz INSERT sem `cell_id` + ON CONFLICT DO NOTHING.

#### Files to edit
```
infra/tests/seed/seed.go — add cell_id to INSERT, change ON CONFLICT to handle backfill
infra/tests/seed/seed_test.go — test cell_id is populated post-seed (if test exists)
```

#### Deep file dependency analysis
- `seed.go:74` SQL is parameterized via `fmt.Sprintf` — straightforward to add column.
- `tenants` table schema (migration 000001/000006): `cell_id TEXT NOT NULL` (probably) or with default 'cell-default'. Verify via `\d tenants`.
- Risk: if NOT NULL constraint without default, existing tests creating tenants via API path may also break — but those use `RoundRobinEngine` so fine.

#### Deep Dives
- **Invariant:** Post-seed, `SELECT cell_id FROM tenants WHERE id='e2e-tenant'` returns `'cell-default'`.
- **Edge case:** If existing seed has `cell_id` already set (manually pinned by previous test), preserve it. SQL: `ON CONFLICT (id) DO UPDATE SET cell_id = COALESCE(tenants.cell_id, EXCLUDED.cell_id)`.

#### Tasks
1. **EC-4 mitigation:** Antes do INSERT em tenants, garantir cell-default existe na tabela `cells`: `INSERT INTO cells (cell_id, status, region) VALUES ('cell-default', 'active', 'local') ON CONFLICT (cell_id) DO NOTHING`. Verificar primeiro se migration 000006 não faz isso já — se sim, skip task. Se há FK `tenants.cell_id REFERENCES cells.cell_id`, esta etapa é OBRIGATÓRIA.
2. Update SQL template in `seed.go:74` to include `cell_id`.
3. Use `COALESCE` in ON CONFLICT for non-destructive preserve: `ON CONFLICT (id) DO UPDATE SET cell_id = COALESCE(tenants.cell_id, EXCLUDED.cell_id)`.
4. If `seed_test.go` exists: add assertion.
5. Test: drop tenant, re-run seed, assert cell_id present.
6. Test: pin tenant to `cell-1` manually (via `loopzero admin tenants pin`), re-seed, assert cell_id ainda é `cell-1` (preserva pin).

#### TDD
```
RED:    test_seed_creates_tenant_with_cell_id (NEW or extend seed_test.go) — assert cell_id is non-null after seed
RED:    test_seed_preserves_existing_cell_id_on_re_run — pin tenant to cell-1, re-seed, assert cell_id still cell-1
GREEN:  Apply SQL change
REFACTOR: None
VERIFY: cd infra/tests/seed && go test -v ./...; psql ... "SELECT cell_id FROM tenants WHERE id='e2e-tenant'"
```

#### Acceptance Criteria
- [ ] Post-seed: `cell_id` populated for `e2e-tenant`
- [ ] Re-seed preserves existing pin
- [ ] Edge Multi-Cell test passes (or moves to next failure if other root cause exists)

#### DoD
- [ ] Tests pass
- [ ] PR includes SQL migration spec change rationale

---

### T3.2 — Add invariant assertion in tenant queries

#### Objective
Defensa-em-profundidade: se um path adicionar tenant sem cell_id, falhar cedo (não deixar até dogfood).

#### Evidence
- `tenants` table: verify NOT NULL constraint on cell_id. If missing, add migration.

#### Files to edit
```
api/internal/db/migrations/000011_tenants_cell_id_not_null.up.sql (NEW, conditional on current schema)
api/internal/db/tenants_store.go — add validation in Insert (if no constraint added)
```

#### Tasks
1. `psql ... "\d tenants"` — verify cell_id constraint.
2. If nullable: write migration `ALTER TABLE tenants ALTER COLUMN cell_id SET NOT NULL` (after ensuring all rows have it via T3.1).
3. If already NOT NULL: skip task.

#### Acceptance Criteria
- [ ] `\d tenants` shows `cell_id TEXT NOT NULL`
- [ ] Re-seed works without violations

#### DoD
- [ ] Migration applied via T1.2 wrapper
- [ ] schema_migrations updated

---

## Phase 4: Application phase Healthy convergence (F-6)

**Objective:** LoopZeroApplication CRs convergem para `Healthy`/`Serving` quando workload pod is Ready, independente de ExposedRoute Reachable.

### T4.1 — Investigate why ExposedRoute never becomes Reachable in local

#### Objective
Determinar por que 9 LoopZeroApplication CRs ficam em Progressing 36-74min. Possibilidades: (a) ExposedRoute reconciler not running, (b) reachable checks DNS resolution that fails locally, (c) cert-manager issuance pendente.

#### Evidence
- `application_controller.go:204 reconcileRouteStatus` lê ExposedRoute.
- `app.Status.Reachable = route.Status.Phase == ExposedRoutePhaseReachable`.
- `route.Status.Phase` only becomes `Reachable` se ExposedRouteController convergir.

#### Files to edit
```
docs/runbooks/sprint8-application-phase-debug.md (NEW or update) — diagnose findings
```

#### Tasks
1. Read `operators/internal/exposedroute/exposed_route_controller.go` (or similar).
2. Identify what conditions are needed for `Reachable` phase.
3. Test: deploy app, immediately check `kubectl describe exposedroute -n loopzero-system`.

#### Acceptance Criteria
- [ ] Document why local never converges
- [ ] Decision: fix ExposedRoute (deep) OR add fast-path to ApplicationController (per ADR D6)

#### DoD
- [ ] Diagnosis committed

---

### T4.2 — ApplicationController fast-path: workload Ready → EndpointsReady

#### Objective
Per ADR D6: se workload Pod Ready=true, marcar `app.Status.EndpointsReady=true` imediatamente, sem esperar ExposedRoute Reachable.

#### Evidence
- ADR D6 rationale: pod respondendo /health 200 é fonte de verdade de "app working". ExposedRoute reachability é SEPARATE concern (DNS+cert+route propagation).
- Current `computePhase`:
  - Reachable=true → ApplicationPhaseReachable
  - EndpointsReady=true → ApplicationPhaseServing
- We want: pod Ready=true → EndpointsReady=true → phase = Serving.

#### Files to edit
```
operators/internal/app/application_controller.go — extend reconcileRouteStatus or add new reconcileWorkloadStatus
operators/internal/app/application_controller_test.go — add envtest spec for fast-path
```

#### Deep file dependency analysis
- Need cross-cluster client to read workload (Pods/Deployments/Rollouts). Is there an existing one in operators/internal/?
- Check `operators/internal/runtime/` or similar for cross-cell client access.
- Alternative: read from Tenant CR Status (which may already have aggregated workload state).

#### Deep Dives
- **Risk:** Adding cross-cluster client to ApplicationController introduces failure modes (workload cluster down → controller stuck). Mitigation: timeout 5s + degrade to existing path (rely on ExposedRoute).
- **Invariant:** EndpointsReady false → true transitions monotonically until app deletion.
- **Edge case:** Multi-cell tenant: read from correct cell's workload cluster.

#### Tasks
1. **EC-5 mitigation (cell-aware):** Reutilizar `CellAwareRuntimeClientFactory` (matrix entry 159, criada em cell-2-activation-plan iter 4) para resolver workload kubeconfig via `tenant.spec.cellID` + `placement.DynamicRouter.Resolve(cellID)`. NÃO criar nova abstração; NÃO hardcodar `cell-default`. Path: `operators/internal/runtime/cell_aware_factory.go` (verify location).
2. In `reconcileRouteStatus` (or new method `reconcileWorkloadStatus`): obter workload client via factory, query pod by labels (`app.kubernetes.io/instance=<app-name>`) for Ready condition.
3. Set `EndpointsReady=true` if pod Ready=true.
4. **EC-9 SHOULD TEST:** Workload cluster unreachable (timeout/connection-refused) → controller graceful degrade, retorna `Result{RequeueAfter: 30s}`, NÃO panic, mantém phase atual.
5. Tests: envtest com fake workload pod Ready=true, assert phase=Serving.
6. Tests: tenant em cell-1 (não default), fast-path resolve correto cluster.

#### TDD
```
RED:    test_app_phase_serving_when_pod_ready_even_if_route_not_reachable (NEW envtest)
RED:    test_app_phase_progressing_when_pod_not_ready
RED:    test_app_phase_uses_cell_aware_factory_for_multicell_tenant (NEW — pin tenant cell-1, assert correct kubeconfig used)
RED:    test_app_phase_falls_back_when_workload_cluster_unreachable (NEW — fake client returns connection refused; assert RequeueAfter set, phase preserved)
GREEN:  Implement workload-pod-aware fast path with CellAwareRuntimeClientFactory
REFACTOR: Extract pod-reader helper if logic > 30 lines
VERIFY: cd operators && go test ./internal/app/... -run "TestApplication.*Phase"
```

#### Acceptance Criteria
- [ ] Phase transitions to `Serving` within 30s of pod Ready
- [ ] Tests pass
- [ ] No regression in existing application_controller specs

#### DoD
- [ ] Dogfood post-fix: LoopZeroApplication CRs em phase ≠ Progressing após pod Ready

---

## Phase 5: Final validation — `/dogfood full` em cleanroom

**Objective:** Validar que TODOS os 7 findings estão fechados via fresh-run dogfood.

### T5.1 — Cleanroom + dogfood full re-run

#### Objective
Executar `/cleanroom` + `/dogfood full` em ambiente pristine. Esperado: 0 S0/S1 findings relacionados aos 7 itens originais.

#### Evidence
- Plan DoD não satisfeito sem fresh dogfood passing.
- Skill spec: "No plan is considered DONE without passing `/dogfood full`."

#### Tasks
1. Operator runs `/cleanroom` (destroys + recriates all 4 k3d clusters + heal).
2. Operator runs `/dogfood full`.
3. Verify pre-flight 11/11 PASS.
4. Verify journey suite ≥ 75/75.
5. Verify edge suite ≥ 21/21.
6. Verify no S0/S1 findings tied to F-1 to F-7.

#### Acceptance Criteria
- [ ] `/cleanroom` PASS gate 38/0
- [ ] Pre-flight: schema_migrations table present, version=10
- [ ] Pre-flight: registry has loopzero-packs:v0.4.0 + dockerfile-lint:v0.1.0
- [ ] Journey suite: 75/75 PASS (no Journey 03 timeout)
- [ ] Edge suite: 21/21 PASS (no idempotency 401, no Multi-Cell placement fail)
- [ ] OBS check: 0 ERROR lines about "build_events" or "traces export"
- [ ] OBS check: LoopZeroApplication CRs NOT stuck in Progressing 30+ min

#### DoD
- [ ] Final dogfood report committed
- [ ] CHANGELOG entry summarizing all 7 fixes

---

## Coverage Matrix

| # | Finding | Severity | Resolution Tasks | Notes |
|---|---------|----------|------------------|-------|
| F-1 | DB migrations sem tracking | S1 | T1.2 + T1.3 + T1.4 | golang-migrate adoption + backfill + prod parity |
| F-2 | ExternalSecret silent skip | S1 | T2.1 + T2.2 + T2.3 | diagnose first, then fix + instrument |
| F-3 | build_events dead code | S1 | T1.1 | remove dead code (D2) |
| F-4 | Seed tenant sem cell_id | S2 | T3.1 + T3.2 | inject cell_id + NOT NULL invariant |
| F-5 | seed.AuthToken legacy | S3 | T0.2 | replace with resolveToken() |
| F-6 | Application phase Progressing stuck | S2 | T4.1 + T4.2 | diagnose + fast-path Healthy |
| F-7 | OTel local spam | S3 | T0.1 | LOOPZERO_TRACING_ENABLED=false default |

**Coverage: 7/7 findings covered (100%).**

## Global Definition of Done

- [ ] All phases (0-5) completed
- [ ] All Go tests passing in `api/`, `operators/`, `pkg/`
- [ ] All TS tests passing in `cli/`
- [ ] Zero `golangci-lint` warnings
- [ ] Zero `eslint` warnings
- [ ] Backward compatibility preserved (or justified pre-release break)
- [ ] All Helm charts pass `helm template + kubeconform --strict`
- [ ] CHANGELOG.md updated with `Fixed`, `Removed`, `Changed` entries
- [ ] **`/cross-validation dogfood-2026-05-08-fixes` PASSED** — zero CRITICO, zero GRAVE
- [ ] **`/dogfood full` PASSED on fresh cleanroom** (Phase 5 / T5.1)
- [ ] No regression in journey or edge suites compared to 2026-05-08 baseline
- [ ] CLAUDE.md sprint plan updated with finalization status

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Per skill spec: "Tests passing is not proof the system works — dogfood is."

After ALL phases (0-4) are complete and ALL tests pass, the final validation is a full dogfood audit on pristine infrastructure.

### Execution

```bash
# 1. Fresh infrastructure (operator)
/cleanroom

# 2. Wait for cleanroom OK gate (38 PASS / 0 FAIL)

# 3. Run full dogfood
/dogfood full

# 4. Confirm zero S0/S1 findings tied to F-1..F-7
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure (post `/cleanroom`)
- [ ] Pre-Flight Check: 11/11 PASS (including new schema_migrations check)
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope (F-1, F-2, F-3)
- [ ] Zero S2 findings related to F-4, F-6
- [ ] Zero S3 findings related to F-5, F-7
- [ ] Prova de Vida passes for every deploy in dogfood
- [ ] Prova de Morte passes for every delete in dogfood
- [ ] No regressions in previously passing flows
- [ ] OBS-1: 0 ERROR lines about `build_events`
- [ ] OBS-1: 0 lines about `traces export rpc error`
- [ ] OBS-5: LoopZeroApplication CRs converge to phase ≠ Progressing within 5min of pod Ready
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds new S0 or S1 issues:
1. Classify: is it within F-1..F-7 scope, or NEW finding?
2. If within scope: fix in this plan; re-run.
3. If NEW: file follow-up plan; assess whether NEW issue should block this plan or be deferred.

S2-S4 NEW findings are documented but do not block plan completion (subject to operator review).

---

## Notes on Architecture Docs + Edge-Case Plan

The skill spec mandates:

- **Pre-Plan AS-IS** invocation of `/architecture-docs` Mode 2 for affected domains. Affected domains: `database` (migrations), `secrets`, `tenant`, `application`, `observability`. **Status:** to be invoked by operator before plan execution begins.
- **Post-Plan PROPOSED** invocation of `/architecture-docs` Mode 3. **Status:** to be invoked after plan signoff.
- **Edge-Case review** via `/edge-case-plan dogfood-2026-05-08-fixes`. **Status:** ✅ INVOKED 2026-05-08 — 12 edge cases identificados (5 MUST FIX, 5 SHOULD TEST, 2 DOCUMENT). Os 5 MUST FIX foram incorporados como sub-tasks/mitigations dentro das tasks T1.2 (EC-1, EC-2), T2.3 (EC-3), T3.1 (EC-4), T4.2 (EC-5). Os 5 SHOULD TEST foram listados como casos TDD adicionais. Os 2 DOCUMENT (EC-11, EC-12) ficam como risco aceito documentado.

### Edge case → task mapping (Version 1.0 → 1.1 amendment, ADR-0022 trivial)

| Edge case | Severidade | Mitigado em |
|-----------|-----------|-------------|
| EC-1 ConfigMap >1MB limit | MUST FIX | T1.2 task 1 (pre-check + initContainer fallback) |
| EC-2 hardcoded `force 10` | MUST FIX | T1.2 task 3 (HIGHEST dynamic) |
| EC-3 cardinality `tenant` label | MUST FIX | T2.3 task 1 (label apenas `result`) |
| EC-4 FK violation `cell-default` | MUST FIX | T3.1 task 1 (ensure cells row before INSERT) |
| EC-5 fast-path não cell-aware | MUST FIX | T4.2 task 1 (CellAwareRuntimeClientFactory) |
| EC-6 LOOPZERO_TRACING_ENABLED override | SHOULD TEST | T0.1 TDD list |
| EC-7 idempotência re-run | SHOULD TEST | T1.2 TDD list |
| EC-8 secret VALUE não logs | SHOULD TEST | T2.3 task 5 |
| EC-9 workload cluster unreachable | SHOULD TEST | T4.2 task 4 |
| EC-10 metric não double-count | SHOULD TEST | T2.3 (additional TDD case) |
| EC-11 seed.AuthToken legacy preservado | DOCUMENT | T0.2 Deep Dives "Edge case" |
| EC-12 T2.2 escopo aberto | DOCUMENT | T2.2 Note + ADR-0022 trigger se mudança contracts |

Plan amendment (per ADR-0022) será necessário se durante implementação surgirem MUST-FIX adicionais não cobertos.
