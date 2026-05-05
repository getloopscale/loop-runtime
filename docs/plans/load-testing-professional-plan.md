# Plan: Professional Load Testing System

> **Version 1.0** — Implementar um sistema de teste de carga profissional completo para o LoopZero PaaS, cobrindo todas as lacunas identificadas na pesquisa SOTA: soak testing automatizado, testes SSE/streaming, chaos+load combinado, testes de API pura, k6 Operator para distributed testing, shift-left CI gate em PRs, e observabilidade de resultados com alertas automáticos. O resultado é um sistema que detecta regressões de performance antes do merge, identifica memory leaks semanalmente, e valida resiliência sob falhas.

## Context

O LoopZero já possui uma base sólida de load testing (k6 + Go harness) com scripts L1-L5 e stress test. Porém a pesquisa SOTA (`docs/technical/load-testing-sota.md`, 2026-05-04) identificou **7 lacunas críticas**:

1. **Soak test não implementado** — referenciado no README mas script `soak-6h.js` não existe. Memory leaks e FD exhaustion passam despercebidos.
2. **SSE/WebSocket não testado** — `loopzero logs` e deploy progress stream nunca foram validados sob carga.
3. **Chaos + Load separados** — Chaos Mesh roda isolado; nunca validamos comportamento sob carga + falha simultânea.
4. **API endpoints sem benchmark** — nenhum teste de latência pura (p99 por endpoint) sem pipeline de deploy.
5. **k6 Operator ausente** — distributed testing impossível; L4/L5 limitados a single-pod.
6. **L1 não roda em PRs** — regressões de performance escapam até weekly cron.
7. **Alertas de soak inexistentes** — sem PrometheusRule para memory slope ou FD exhaustion.

**Evidência:** `infra/tests/k6/README.md` menciona `soak-l2-6h.js` e `soak-l2-cronjob.yaml` que não existem. `Glob("infra/tests/k6/scripts/soak*")` retorna vazio. `Glob("infra/tests/k6/manifests/**")` retorna vazio.

## Objective

**Done = sistema de load testing que detecta regressões em PRs (< 5 min), valida resiliência sob chaos, e identifica leaks semanalmente — tudo automatizado, zero intervenção manual.**

- 4 novos scripts k6 implementados e validados
- k6 Operator instalável via Helm
- Soak CronJob + alertas ativos no cluster
- CI gate L1 em PRs (shift-left)
- Runbook para falhas de soak
- Taskfile atualizado com todos os comandos
- README atualizado com inventário completo

## ADRs

### D1 — k6 permanece como ferramenta principal

**Decision:** Não migrar para Gatling, Locust ou Artillery.
**Rationale:** k6 é líder consolidado (29.9k stars), Kubernetes-native via Operator, JS scripting (equipe já usa), Prometheus remote-write built-in, arrival-rate executor resolve coordinated omission. A equipe já tem 6 scripts + shared libs. Custo de migração > 0, benefício = 0.
**Consequences:** Extensões via xk6 quando necessário (SSE). Sem dependência Python/JVM.

### D2 — k6 Operator para distributed testing (L3+)

**Decision:** Instalar k6 Operator (Grafana, v3.10.0) no build cluster para habilitar `TestRun` CRD com `parallelism: N`.
**Rationale:** L4 (500 tenants) e L5 (5000 tenants) precisam de múltiplos pods gerando carga. k6 single-pod satura em ~300k RPS. Operator sincroniza pods via initializer/starter jobs. Testkube rejeitado (overhead de controller+API+dashboard sem retorno).
**Consequences:** Build cluster ganha CRD `TestRun`. Manifests `testrun-l3-distributed.yaml` e `testrun-l4-distributed.yaml` exemplificam uso.

### D3 — Soak test via CronJob semanal (não k6 Operator)

**Decision:** Soak test roda como Job simples (não TestRun distribuído) via CronJob.
**Rationale:** Soak não precisa de alta concorrência — precisa de duração (6h). Single pod com 30 VUs é suficiente. CronJob é mais simples de operar, debugar e alertar.
**Consequences:** PrometheusRule alerta em Job failure + memory/FD slope. Runbook documenta diagnóstico.

### D4 — L1 smoke em PRs usa Go harness (não k6 in-cluster)

**Decision:** CI em PRs executa `go test ./load/ -run TestL1_` contra API dev, não k6 in-cluster.
**Rationale:** PRs não têm acesso ao cluster. Go harness roda standalone contra endpoint HTTPS. Feedback em < 5 min. k6 in-cluster exigiria cluster disponível em cada PR CI run.
**Consequences:** Gate é melhor que nada, mas não testa com real latency distribution. Accepted trade-off.

### D5 — Scripts usam `constant-arrival-rate` ou `ramping-arrival-rate` (open model)

**Decision:** Todos os novos scripts usam open workload model. L1 existente pode permanecer `per-vu-iterations` (smoke, CO não é relevante a 10 VUs).
**Rationale:** Closed model (wait for response before next request) causa coordinated omission — p99 reportado é artificialmente baixo. Open model (constant arrival rate regardless of response time) é a única forma correta de medir latência sob carga sustentada.
**Consequences:** Novos scripts: `constant-arrival-rate` ou `ramping-arrival-rate`. L1 mantém `per-vu-iterations` (ok para smoke).

### D6 — Chaos + Load são scripts separados executados em paralelo

**Decision:** `chaos-load.js` mede impacto; Chaos Mesh injeta falhas via CRD. O script k6 NÃO injeta chaos — apenas observa.
**Rationale:** Separação de concerns. k6 mede, Chaos Mesh injeta. Permite combinações arbitrárias (qualquer chaos experiment + qualquer k6 script). Reprodutível.
**Consequences:** Operador executa `kubectl apply -f chaos-experiment.yaml` + `task test:load:chaos` em paralelo.

## Dependency Graph

```
Phase 0 (Scripts k6) ──▶ Phase 1 (Infra) ──▶ Phase 2 (CI + Observability) ──▶ Phase 3 (Docs + Validation)
     │                        │
     │                        ├── T1.1 k6 Operator Helm (independente)
     │                        ├── T1.2 RBAC (independente)
     │                        └── T1.3 CronJob + Alerts (depende T0.1)
     │
     ├── T0.1 soak-6h.js (independente)
     ├── T0.2 sse-deploy-stream.js (independente)
     ├── T0.3 chaos-load.js (independente)
     └── T0.4 api-endpoints.js (independente)
```

Phase 0 tasks são todas paralelas entre si. Phase 1 depende de T0.1 (CronJob referencia soak script). Phase 2 é paralela internamente. Phase 3 é final.

---

## Phase 0: Scripts k6

**Objective:** Implementar os 4 scripts k6 que cobrem os gaps identificados na pesquisa SOTA.

### T0.1 — Soak Test (6 horas)

#### Objective
Script k6 que roda L2-level load (1 deploy/min) por 6 horas para detectar memory leaks, FD exhaustion, e connection pool starvation.

#### Evidence
- `infra/tests/k6/README.md` line 89: referencia `soak-l2-cronjob.yaml` que não existe
- `docs/technical/load-testing-sota.md`: "Soak test CronJob implementado" listado como gap de alta prioridade
- L1-L5 rodam por minutos (15 min a 4h) — insuficiente para detectar leaks sutis que se manifestam após 2+ horas

#### Files to edit
```
infra/tests/k6/scripts/soak-6h.js — (NEW) script de soak test 6h
```

#### Deep file dependency analysis
- **`soak-6h.js`** — novo arquivo. Importa de `./lib/loopzero-api.js` (createProject, uploadSource, submitBuild, pollBuildUntilDone, healthCheck, verifyAppLive, appURL) e `./lib/thresholds.js` (thresholdsFor). Usa `../fixtures/bundle-builder.js` (loadBundleBytes). Nenhum downstream depende dele diretamente; o CronJob (T1.3) referencia por nome.

#### Deep Dives
- **Executor:** `constant-arrival-rate` com `rate: 1, timeUnit: '1m'` = 1 deploy/minuto constante por 6h = 360 deploys total
- **Tenant rotation:** 50 tenants, `iterationInTest % 50` = cada tenant recebe ~7 deploys
- **Métricas de soak:** `loopzero_soak_iteration_total` (Counter), `loopzero_soak_hour_marker` (Counter, incrementa a cada ~60 iterações)
- **Invariante:** taxa de sucesso não deve degradar ao longo do tempo. Se hora 1 tem 100% e hora 6 tem 90%, o teste deve falhar.
- **Edge case:** se build travar (timeout 20min), o VU não é liberado — `maxVUs: 100` garante que não esgota o pool

#### Tasks
1. Criar arquivo `infra/tests/k6/scripts/soak-6h.js`
2. Implementar executor `constant-arrival-rate` com duração 6h
3. Adicionar métricas custom (soak_iteration_total, soak_hour_marker)
4. Implementar `handleSummary` com breakdown por hora
5. Validar que `thresholdsFor('l2')` é aplicado

#### TDD
```
RED:     Executar `k6 run --dry-run soak-6h.js` — deve parsear sem erro
RED:     Thresholds devem incluir `loopzero_build_succeeded: rate>0.99`
GREEN:   Implementar script completo
REFACTOR: Extrair report helper se duplicar de L2
VERIFY:  k6 inspect scripts/soak-6h.js (validates options/thresholds without running)
```

#### Acceptance Criteria
- [ ] Script parseia sem erro via `k6 inspect`
- [ ] Usa `constant-arrival-rate` executor (CO-safe)
- [ ] Duração total = 6h + gracefulStop 30m
- [ ] 50 tenants com rotação
- [ ] Thresholds derivados de `thresholdsFor('l2')`
- [ ] Métricas custom: `loopzero_soak_iteration_total`, `loopzero_soak_hour_marker`
- [ ] handleSummary mostra breakdown temporal
- [ ] Segue style guide dos scripts existentes (SPDX, imports, groups)

#### DoD
- [ ] `k6 inspect scripts/soak-6h.js` retorna 0
- [ ] Script segue padrão dos existentes (lib imports, setup/default/teardown/handleSummary)
- [ ] Zero warnings no k6 parser

---

### T0.2 — SSE Deploy Stream Test

#### Objective
Script k6 que valida Server-Sent Events streaming durante deploys — cobertura para `loopzero logs` / deploy progress.

#### Evidence
- Nenhum script existente testa SSE
- `loopzero logs` usa SSE para streaming de deploy progress — nunca validado sob carga
- API tem endpoint `GET /api/v1/builds/{buildID}/events` (SSE)

#### Files to edit
```
infra/tests/k6/scripts/sse-deploy-stream.js — (NEW) script de teste SSE
```

#### Deep file dependency analysis
- **`sse-deploy-stream.js`** — novo arquivo. Importa de `./lib/loopzero-api.js`. Usa HTTP streaming pattern (k6 não tem SSE nativo; usa polling como fallback com tentativa de stream). O endpoint SSE é `GET /api/v1/builds/{buildID}/events`.

#### Deep Dives
- **k6 e SSE:** k6 v0.52 não tem suporte SSE nativo. Abordagem: (a) tentar `http.get` com `responseType: 'none'` para stream, (b) fallback para polling `GET /api/v1/builds/{buildID}` com métricas de latência do primeiro evento
- **Métricas:** `loopzero_sse_connect_duration_ms` (tempo até primeiro evento), `loopzero_sse_events_received` (total), `loopzero_sse_connection_drops` (drops inesperados), `loopzero_sse_build_terminal_received` (% que recebeu evento terminal)
- **Invariante:** ≥95% dos builds devem emitir evento terminal via SSE. Se menos, há bug de event emission.

#### Tasks
1. Criar `infra/tests/k6/scripts/sse-deploy-stream.js`
2. Implementar executor `constant-arrival-rate` (5 deploys/min, 10 min)
3. Submeter build via lib/loopzero-api.js
4. Implementar SSE connection attempt + polling fallback
5. Registrar métricas custom de SSE
6. handleSummary com report de SSE performance

#### TDD
```
RED:     `k6 inspect sse-deploy-stream.js` — deve parsear sem erro
RED:     Thresholds devem incluir `loopzero_sse_build_terminal_received: rate>0.95`
GREEN:   Implementar script completo
REFACTOR: None expected
VERIFY:  k6 inspect scripts/sse-deploy-stream.js
```

#### Acceptance Criteria
- [ ] Script parseia sem erro
- [ ] Usa `constant-arrival-rate` executor
- [ ] Submete builds reais e tenta ler SSE stream
- [ ] 4 métricas custom de SSE implementadas
- [ ] Thresholds incluem SSE-specific gates
- [ ] Fallback para polling se SSE indisponível

#### DoD
- [ ] `k6 inspect` green
- [ ] Padrão consistente com outros scripts

---

### T0.3 — Chaos + Load Combined

#### Objective
Script k6 que mede comportamento da plataforma durante chaos experiments (pod kill, network delay, PG disconnect). Não injeta chaos — apenas observa.

#### Evidence
- Chaos Mesh instalado (Sprint 8 Phase 0)
- `taskfiles/test.yml` já tem `test:chaos:*` tasks
- Nenhum script combina carga + chaos para medir degradação graceful

#### Files to edit
```
infra/tests/k6/scripts/chaos-load.js — (NEW) script chaos + load
```

#### Deep file dependency analysis
- **`chaos-load.js`** — novo arquivo. Importa de `./lib/loopzero-api.js`. Design 3-phase: baseline (5 min) → chaos (15 min) → recovery (5 min). Métricas segregadas por fase permitem comparar "antes" vs "durante" vs "depois" no Grafana.

#### Deep Dives
- **3 Fases:** Script usa `scenarios` com múltiplos executors OU single executor com lógica temporal
- **Métricas:** `loopzero_chaos_graceful_reject` (429/503 esperados), `loopzero_chaos_unexpected_5xx` (500 = bug)
- **Threshold relaxado:** `loopzero_build_succeeded: rate>0.85` (chaos ativo reduz success rate esperado)
- **Invariante:** zero 500 erros. 429/503 são esperados (backpressure graceful). 500 é bug exposto pelo chaos.
- **Recovery:** builds submetidos nos últimos 5 min devem ter >95% success (sistema se recuperou)

#### Tasks
1. Criar `infra/tests/k6/scripts/chaos-load.js`
2. Implementar 3 fases via stages do `ramping-arrival-rate`
3. Adicionar métricas de classificação de erro (graceful vs unexpected)
4. handleSummary com per-phase breakdown
5. Documentar em comments como usar com Chaos Mesh

#### TDD
```
RED:     `k6 inspect chaos-load.js` — parseia sem erro
RED:     Threshold `loopzero_chaos_unexpected_5xx: count<3` presente
GREEN:   Implementar script
REFACTOR: None expected
VERIFY:  k6 inspect scripts/chaos-load.js
```

#### Acceptance Criteria
- [ ] 3 fases (baseline, chaos, recovery)
- [ ] Métricas segregam erros esperados (429/503) de inesperados (500)
- [ ] Threshold relaxado para chaos (85% success)
- [ ] Recovery phase threshold apertado (95%)
- [ ] Comments explicam integração com Chaos Mesh

#### DoD
- [ ] `k6 inspect` green
- [ ] Documentação de uso em comments

---

### T0.4 — API Endpoints Pure HTTP Test

#### Objective
Script k6 que testa latência pura de endpoints HTTP da API (sem pipeline de deploy). Valida p99 por endpoint a 100 RPS.

#### Evidence
- Scripts L1-L5 medem deploy pipeline inteiro (minutos por iteração)
- Nenhum script mede latência HTTP pura por endpoint
- SLOs de API: health < 100ms p99, list < 500ms p99, get < 200ms p99

#### Files to edit
```
infra/tests/k6/scripts/api-endpoints.js — (NEW) script de benchmark de endpoints
```

#### Deep file dependency analysis
- **`api-endpoints.js`** — novo arquivo. Importa parcialmente de `./lib/loopzero-api.js` (auth headers pattern). Não usa pipeline functions (createProject, submitBuild). Testa endpoints diretamente com `http.get`/`http.post`. Métricas custom por endpoint.

#### Deep Dives
- **Executor:** `constant-arrival-rate` com `rate: 100, timeUnit: '1s'` = 100 RPS por 5 min
- **Endpoints:** GET /health, GET /projects, GET /builds/{id}, GET /environments, POST /projects
- **Métricas:** Trend por endpoint (`loopzero_api_health_duration`, `loopzero_api_projects_list_duration`, etc.)
- **Pre-requisito:** tenant `k6-api-tn-0` deve existir no DB (seed step)

#### Tasks
1. Criar `infra/tests/k6/scripts/api-endpoints.js`
2. Implementar `constant-arrival-rate` a 100 RPS
3. Implementar request para cada endpoint com tag + custom metric
4. Thresholds per-endpoint (health <100ms, list <500ms, get <200ms, create <1000ms)
5. handleSummary com tabela per-endpoint

#### TDD
```
RED:     `k6 inspect api-endpoints.js` — parseia sem erro
RED:     Threshold `loopzero_api_health_duration: p(99)<100` presente
GREEN:   Implementar script
REFACTOR: None expected
VERIFY:  k6 inspect scripts/api-endpoints.js
```

#### Acceptance Criteria
- [ ] 100 RPS via `constant-arrival-rate`
- [ ] 5 endpoints testados
- [ ] Trend metric por endpoint
- [ ] Thresholds per-endpoint baseados em SLO
- [ ] handleSummary com tabela comparativa

#### DoD
- [ ] `k6 inspect` green
- [ ] Thresholds derivados de SLOs documentados

---

## Phase 1: Infrastructure

**Objective:** Instalar infraestrutura para distributed testing, soak automatizado, e RBAC.

### T1.1 — k6 Operator Helm Chart

#### Objective
Chart Helm que instala o Grafana k6 Operator no cluster, habilitando CRD `TestRun` para distributed testing.

#### Evidence
- L4 (500 tenants) e L5 (5000 tenants) precisam de múltiplos pods (ADR D2)
- k6 single-pod satura em ~300k RPS
- Sem Operator, distributed é manual (múltiplos Jobs sem sincronização)

#### Files to edit
```
infra/helm/k6-operator/Chart.yaml — (NEW) chart metadata + dependency
infra/helm/k6-operator/values.yaml — (NEW) configuração do operator
```

#### Deep file dependency analysis
- **Chart.yaml** — declaração de dependência no chart oficial `grafana/k6-operator` v3.10.0. Pattern: mesmo de `infra/helm/` existentes (lightweight, inline values).
- **values.yaml** — resource limits, installCRDs=true, namespace config.
- **Downstream:** `testrun-l3-distributed.yaml` e `testrun-l4-distributed.yaml` dependem do CRD `TestRun` que este chart instala.

#### Tasks
1. Criar diretório `infra/helm/k6-operator/`
2. Escrever `Chart.yaml` com dependency no chart Grafana
3. Escrever `values.yaml` com resource limits e configuração
4. Validar com `helm template`

#### TDD
```
RED:     `helm template infra/helm/k6-operator/` falha (chart não existe)
GREEN:   Criar chart, `helm dependency update && helm template` passa
REFACTOR: None expected
VERIFY:  helm template infra/helm/k6-operator/ --debug
```

#### Acceptance Criteria
- [ ] `helm dependency update` resolve chart grafana/k6-operator
- [ ] `helm template` gera manifests válidos
- [ ] Resource limits definidos (CPU 200m/100m, Memory 256Mi/128Mi)
- [ ] installCRDs: true
- [ ] Chart segue pattern de `infra/helm/` existentes

#### DoD
- [ ] `helm template` green
- [ ] Nenhum `latest` tag
- [ ] Resource limits + requests definidos

---

### T1.2 — RBAC para k6 Runner

#### Objective
ServiceAccount + Role + RoleBinding para que Jobs k6 acessem ConfigMaps e Secrets necessários.

#### Evidence
- Job template (`job-template.yaml`) não especifica `serviceAccountName`
- Secrets `loopzero-api-secrets` precisam de acesso explícito
- CronJob de soak precisa de permissões persistentes

#### Files to edit
```
infra/tests/k6/manifests/rbac.yaml — (NEW) SA + Role + RoleBinding
```

#### Deep file dependency analysis
- **rbac.yaml** — novo. Define `ServiceAccount k6-runner`, `Role k6-runner` (get configmaps, get secrets/loopzero-api-secrets), `RoleBinding`. Referenciado pelo CronJob (T1.3) e TestRun manifests.

#### Tasks
1. Criar diretório `infra/tests/k6/manifests/`
2. Escrever `rbac.yaml` com SA + Role (minimal privileges) + RoleBinding
3. Validar com `kubectl apply --dry-run=client`

#### TDD
```
RED:     `kubectl apply --dry-run=client -f rbac.yaml` falha (arquivo não existe)
GREEN:   Criar arquivo, dry-run passa
VERIFY:  kubectl apply --dry-run=client -f infra/tests/k6/manifests/rbac.yaml
```

#### Acceptance Criteria
- [ ] ServiceAccount `k6-runner` em `loopzero-system`
- [ ] Role com minimal privileges (get configmaps, get secret loopzero-api-secrets only)
- [ ] RoleBinding liga SA ao Role
- [ ] Labels `app.kubernetes.io/part-of: loopzero`

#### DoD
- [ ] `kubectl apply --dry-run=client` green
- [ ] Princípio de least privilege respeitado

---

### T1.3 — Soak CronJob + PrometheusRule

#### Objective
CronJob semanal que executa soak test + alertas Prometheus para memory/FD leak detection.

#### Evidence
- ADR D3: soak como CronJob simples
- `docs/technical/load-testing-sota.md`: "memory slope: deriv(process_resident_memory_bytes[1h]) > 0 por >2h = leak"
- Sem automação atual, soak nunca é executado na prática

#### Files to edit
```
infra/tests/k6/manifests/soak-cronjob.yaml — (NEW) CronJob semanal
infra/tests/k6/manifests/soak-alerts.yaml — (NEW) PrometheusRule para leak detection
```

#### Deep file dependency analysis
- **soak-cronjob.yaml** — depende de: ConfigMap `k6-scripts` (contendo soak-6h.js de T0.1), ConfigMap `k6-fixture-builder`, ConfigMap `k6-seed-sql`, Secret `loopzero-api-secrets`, ServiceAccount `k6-runner` (T1.2).
- **soak-alerts.yaml** — PrometheusRule independente. Alertas: `LoopZeroSoakTestFailed` (Job failure), `LoopZeroAPIMemoryLeak` (RSS slope > 0 por 2h durante soak), `LoopZeroAPIFDExhaustion` (FD slope > 0 por 2h).

#### Tasks
1. Escrever `soak-cronjob.yaml` (schedule: Sunday 02:00 UTC)
2. Escrever `soak-alerts.yaml` com 3 regras de alerta
3. Validar YAML syntax
4. Validar PrometheusRule spec

#### TDD
```
RED:     `kubectl apply --dry-run=client -f soak-cronjob.yaml` falha
RED:     `kubectl apply --dry-run=client -f soak-alerts.yaml` falha
GREEN:   Criar ambos, dry-run passa
VERIFY:  kubectl apply --dry-run=client -f infra/tests/k6/manifests/soak-cronjob.yaml
         kubectl apply --dry-run=client -f infra/tests/k6/manifests/soak-alerts.yaml
```

#### Acceptance Criteria
- [ ] CronJob schedule: `0 2 * * 0` (Sunday 02:00 UTC)
- [ ] `concurrencyPolicy: Forbid`
- [ ] `activeDeadlineSeconds: 28800` (8h hard limit)
- [ ] Uses ServiceAccount `k6-runner`
- [ ] Init containers: build-fixture + seed-db
- [ ] PrometheusRule com 3 alertas: SoakTestFailed, MemoryLeak, FDExhaustion
- [ ] Alertas têm `runbook_url` apontando para docs/runbooks/

#### DoD
- [ ] Both YAMLs passam dry-run
- [ ] PrometheusRule gera alertas corretos (PromQL válido)

---

### T1.4 — TestRun CRDs para Distributed Testing

#### Objective
Manifests de exemplo para usar k6 Operator em L3 (4 pods) e L4 (8 pods).

#### Files to edit
```
infra/tests/k6/manifests/testrun-l3-distributed.yaml — (NEW)
infra/tests/k6/manifests/testrun-l4-distributed.yaml — (NEW)
```

#### Deep file dependency analysis
- Dependem do CRD `TestRun` (v1alpha1) instalado pelo k6 Operator (T1.1).
- Referenciam ConfigMap `k6-scripts` e Secret `loopzero-api-secrets`.

#### Tasks
1. Escrever `testrun-l3-distributed.yaml` com `parallelism: 4`
2. Escrever `testrun-l4-distributed.yaml` com `parallelism: 8`
3. Include init container para bundle fixture

#### TDD
```
RED:     YAML syntax check falha (arquivo não existe)
GREEN:   Criar arquivos com spec correta
VERIFY:  python3 -c "import yaml; yaml.safe_load(open('file.yaml'))" for each
```

#### Acceptance Criteria
- [ ] L3: `parallelism: 4`, resources limits definidos
- [ ] L4: `parallelism: 8`, resources limits definidos
- [ ] Ambos referenciam ConfigMap e Secret corretos
- [ ] Init container para fixture

#### DoD
- [ ] YAML válido
- [ ] Labels `app.kubernetes.io/part-of: loopzero`

---

## Phase 2: CI + Observability

**Objective:** Shift-left gate em PRs, Taskfile commands, e runbook operacional.

### T2.1 — CI Workflow: L1 Smoke em PRs

#### Objective
GitHub Actions workflow que roda L1 smoke test em PRs que tocam API/operators/pkg.

#### Evidence
- ADR D4: usa Go harness contra dev API
- `.github/workflows/load-test.yml` existente roda apenas Go harness manual/weekly
- Regressões de performance escapam até Monday cron

#### Files to edit
```
.github/workflows/load-test-pr.yml — (NEW) L1 smoke gate para PRs
```

#### Deep file dependency analysis
- Depende de `infra/tests/load/` (Go harness), `go.work`, `.github/actions/setup-go`.
- Trigger: PRs em `api/**`, `operators/**`, `pkg/**`, `infra/tests/load/**`, `infra/tests/k6/**`.
- Secret: `LOOPZERO_SERVICE_AUTH_SECRET` (GitHub Environment `infra-dev`).

#### Tasks
1. Criar `.github/workflows/load-test-pr.yml`
2. Health check antes de rodar (skip se dev API down)
3. `go test ./load/ -run TestL1_ -timeout 8m`
4. Post summary no PR
5. Concurrency: cancel-in-progress por branch

#### TDD
```
RED:     Workflow não existe
GREEN:   Criar workflow, `act` ou push PR para validar
VERIFY:  yamllint .github/workflows/load-test-pr.yml
```

#### Acceptance Criteria
- [ ] Trigger: PRs em api/operators/pkg/infra/tests
- [ ] timeout-minutes: 10
- [ ] Health check skip se API down (não falha PR por infra issue)
- [ ] concurrency cancel-in-progress
- [ ] Post summary no PR step summary
- [ ] Usa SHA-pinned actions

#### DoD
- [ ] YAML válido
- [ ] Actions SHA-pinned
- [ ] Secret referenciado corretamente

---

### T2.2 — Taskfile Commands

#### Objective
Adicionar todos os novos commands ao Taskfile para acesso unificado.

#### Evidence
- Taskfile já tem `test:load:l1` até `test:load:stress`
- Faltam: l4, l5, soak, sse, chaos, api, all, operator install, distributed runs

#### Files to edit
```
taskfiles/test.yml — adicionar novos tasks
```

#### Deep file dependency analysis
- `taskfiles/test.yml` é incluído pelo `Taskfile.yml` root. Novos tasks usam `infra/tests/runner/run-k6.sh` (já existe) e `kubectl apply` para manifests.

#### Tasks
1. Adicionar tasks: test:load:l4, test:load:l5, test:load:soak, test:load:sse, test:load:chaos, test:load:api, test:load:all
2. Adicionar tasks: k6:operator:install, k6:distributed:l3, k6:distributed:l4, k6:soak:apply

#### TDD
```
RED:     `task --list | grep "test:load:soak"` retorna vazio
GREEN:   Adicionar tasks, `task --list` mostra todos
VERIFY:  task --list | grep -c "test:load:" (deve ser >= 10)
```

#### Acceptance Criteria
- [ ] Todos os novos scripts têm task correspondente
- [ ] `test:load:all` executa L1→L2→L3→stress→API sequencialmente
- [ ] k6 operator install via Helm
- [ ] k6 soak apply instala CronJob + alerts

#### DoD
- [ ] `task --list` mostra todos os commands
- [ ] Zero erros de syntax no Taskfile

---

### T2.3 — Runbook: Soak Test Failure

#### Objective
Runbook operacional para quando o soak test falha ou alertas de memory/FD disparam.

#### Files to edit
```
docs/runbooks/soak-test-failure.md — (NEW) runbook
```

#### Tasks
1. Escrever runbook com: sintomas, diagnóstico, resolução, prevenção
2. Cobrir 3 cenários: Job failure, memory leak, FD exhaustion
3. Incluir comandos kubectl/promql para diagnóstico

#### Acceptance Criteria
- [ ] 3 cenários cobertos com steps concretos
- [ ] Comandos copy-paste para diagnóstico
- [ ] Link para dashboard Grafana
- [ ] Severity classification

#### DoD
- [ ] Runbook completo e acionável

---

## Phase 3: Documentation + Validation

**Objective:** Atualizar README, validar tudo funciona, fechar gaps.

### T3.1 — README Atualizado

#### Objective
Atualizar `infra/tests/k6/README.md` e `infra/tests/load/README.md` com inventário completo.

#### Files to edit
```
infra/tests/k6/README.md — atualizar com novos scripts e distributed testing
infra/tests/load/README.md — atualizar com novos tasks
```

#### Tasks
1. Adicionar novos scripts ao inventário
2. Documentar k6 Operator usage
3. Documentar soak CronJob
4. Atualizar Quick Start com novos commands

#### Acceptance Criteria
- [ ] Todos os 10 scripts documentados
- [ ] k6 Operator section com install + usage
- [ ] Soak section com CronJob + alerts
- [ ] Distributed testing section

#### DoD
- [ ] README reflete realidade do repo

---

### T3.2 — Validation Run

#### Objective
Executar `k6 inspect` em todos os novos scripts + `helm template` + `kubectl dry-run` em todos manifests.

#### Tasks
1. `k6 inspect` cada novo script
2. `helm template infra/helm/k6-operator/`
3. `kubectl apply --dry-run=client` cada manifest em `infra/tests/k6/manifests/`
4. `yamllint .github/workflows/load-test-pr.yml`
5. `task --list | grep test:load` confirma todos os tasks

#### Acceptance Criteria
- [ ] Zero erros em todas as validações
- [ ] Inventário final: 10 scripts, 5 manifests, 1 Helm chart, 1 workflow, 12 tasks

#### DoD
- [ ] Validation matrix GREEN

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Soak test não implementado | T0.1, T1.3 | Script + CronJob + Alerts |
| 2 | SSE/WebSocket não testado | T0.2 | Script com SSE streaming |
| 3 | Chaos + Load separados | T0.3 | Script que mede durante chaos |
| 4 | API endpoints sem benchmark | T0.4 | Script 100 RPS pure HTTP |
| 5 | k6 Operator ausente | T1.1, T1.4 | Helm chart + TestRun examples |
| 6 | L1 não roda em PRs | T2.1 | CI workflow shift-left |
| 7 | Alertas de soak inexistentes | T1.3 | PrometheusRule 3 alertas |
| 8 | RBAC não definido | T1.2 | SA + Role + RoleBinding |
| 9 | Taskfile incompleto | T2.2 | 12 novos tasks |
| 10 | Sem runbook para soak failure | T2.3 | Runbook operacional |
| 11 | README desatualizado | T3.1 | Inventário completo |
| 12 | Distributed testing impossível | T1.1, T1.4 | k6 Operator + TestRun CRDs |

**Coverage: 12/12 gaps covered (100%)**

## Global Definition of Done

- [ ] Todas as 4 phases completas
- [ ] `k6 inspect` green em todos os 4 novos scripts
- [ ] `helm template` green para k6-operator chart
- [ ] `kubectl apply --dry-run=client` green para todos manifests
- [ ] `yamllint` green para workflow
- [ ] `task --list | grep "test:load:" | wc -l` >= 10
- [ ] README reflete inventário real
- [ ] Zero lint warnings
- [ ] **`/dogfood full` PASSED** (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Run L1 smoke to validate load test system works
task test:load:l1

# 3. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] `task test:load:l1` passes (k6 L1 smoke works end-to-end)
- [ ] `task test:load:api` passes (API benchmark works)
- [ ] No regressions in previously passing flows
- [ ] Observability check passes (k6 metrics appear in Prometheus)
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.
