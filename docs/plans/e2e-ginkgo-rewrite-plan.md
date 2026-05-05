# Plan: E2E Test Rewrite — Ginkgo Against Real Infrastructure

> **Version 1.1** (corrigido 2026-05-05) — Reescrever todos os testes E2E do LoopZero como Ginkgo specs contra infraestrutura real (k3d). Substitui os 11 bash scripts frágeis em `infra/tests/dogfood/journeys/` por specs Go robustos que testam 100% do sistema — jornadas completas + edge cases + failure modes + chaos. Resultado: suite determinística, parallelizável, com assertions fortes, polling resiliente via `Eventually()`, e coverage completa de todos os cenários de produção. Zero mocks, zero stubs — CLI real contra infra real.
>
> **v1.1 changes:** Corrigidos 6 erros factuais (PIt count 13→7, full-loop.sh inexistente, fixture count 3→4, file count 52→53, cli_helpers_test.go omitido, forbiddenTerms duplicado). Adicionada overlap analysis com 53 testes Ginkgo existentes. Adicionada nota de governança T2. Ajustado timeout de suite em T0.3.

## Context

### O que existe hoje

**Ginkgo e2e (`infra/tests/e2e/`):**
- 53 test files, Ginkgo v2.28.1 + Gomega v1.39.1
- Helpers em **dois arquivos**:
  - `helpers_test.go`: `apiURL`, `httpGet`, `copyFixture`, `cleanupTenant`, `apiHealthy`, `clusterRunning`, `uniqueSuffix`, `verifyNoActiveBuilds`, `verifyAPIHealth`
  - `cli_helpers_test.go`: `runLoopZero`, `runLoopZeroNoToken`, `runLoopZeroWithStdin`, `expectNoInternalTerms`, `expectAllowedExitCode`, `parseJSON`, `loopzeroCLIAvailable`, `loopzeroTokenAvailable`, `jsonStringField`, `jsonBoolField`
  - Outros: `runLoopZeroInDir` (cli_validate_test.go), `runLoopZeroDeploy`/`initGitRepo` (deploy_happy_path_test.go), `runLoopZeroWithCI` (cli_preview_test.go)
- Seed: `infra/tests/seed/seed.go` (idempotent tenant + user + project via psql + kubectl)
- Fixtures: `hello-server`, `multi-app`, `dogfood-monorepo`, `static-frontend`
- Testes reais contra k3d (gated por `LOOPZERO_E2E=1`)
- **Cobertura parcial de jornadas:** `deploy_happy_path_test.go` + `deploy_e2e_test.go` (deploy), `concurrent_deploy_test.go` (2 specs com barrier pattern), `preview_lifecycle_test.go` (12 specs), 44 `cli_*_test.go` files (5-contract template por comando)
- **Gaps:** failure_scenarios_test.go tem 7 PIt (pending — não implementados); jornadas completas (deploy→scale→promote→rollback→delete) não existem em Ginkgo
- **Duplicação a resolver:** `forbiddenTerms` definido em `concurrent_deploy_test.go:30` e `internalTerms` em `cli_helpers_test.go:31` — listas idênticas, devem ser consolidadas

**Bash journeys (`infra/tests/dogfood/journeys/`):**
- 11 scripts + `_common.sh` (sem orquestrador — cada script é executado individualmente)
- Testam fluxos completos via CLI real
- **Problemas:** sleep loops frágeis, parsing com grep/python3, sem paralelismo, sem retry robusto, sem reporting estruturado, manutenção difícil, sem type safety

### Evidência dos problemas dos bash scripts

1. `wait_build_terminal` usa `sleep 15` loop — se API intermitente, perde 15s entre cada tentativa
2. `prove_dead` faz `kubectl get` + `wc -l` — fragile output parsing
3. Sem JUnit XML — CI não tem reporting granular
4. Sem orquestrador centralizado — scripts são executados manualmente um a um
5. Cleanup via `rm -rf` + `prove_dead` manual — DeferCleanup do Ginkgo é mais confiável
6. Auth refresh implementada manualmente (`ensure_token` com re-seed) — poderia ser BeforeEach resiliente

### Overlap entre Ginkgo existente e Bash journeys

Os 53 test files Ginkgo existentes já cobrem parcialmente o território das bash journeys:

| Cenário bash | Cobertura Ginkgo existente | Gap real |
|---|---|---|
| journey-01 (solo deploy) | `deploy_happy_path_test.go`, `deploy_e2e_test.go` — deploy + build | HTTP probe com host header, audit log, Prova de Morte |
| journey-02 (multi-server) | `cli_validate_test.go` — validação multi-app | Deploy real multi-app, worker pod verification |
| journey-03 (secrets) | `cli_secret_*.go` (3 files) — CRUD commands | Lifecycle completo: set → redeploy → verify env → delete → verify gone |
| journey-04 (scale) | Nenhum | Tudo: scale + cap rejection |
| journey-05 (promote) | `cli_promote_test.go` — CLI contract | Deploy staging → promote → verify production HTTP |
| journey-06 (rollback) | `cli_rollback_test.go` — CLI contract | Deploy v1 → v2 → rollback → verify version via HTTP |
| journey-09 (preview) | `preview_lifecycle_test.go` (12 specs) | CLI commands (list/inspect/delete) — webhook flow já coberto |
| journey-10 (failures) | `failure_scenarios_test.go` (7 PIt) | Implementar os PIt |
| Concurrent deploy | `concurrent_deploy_test.go` (2 specs, barrier) | Idempotency replay, 3+ concurrent, lock timeout |

**Implicação:** Journeys 01, 02, 03, 05, 06, 09 têm cobertura PARCIAL. O trabalho real é expandir os specs existentes com assertions de jornada (HTTP probe, audit, Prova de Morte), não reescrever do zero. Journeys 04, 07, 08, 10, 11 são genuinamente novos.

### O que motiva a reescrita

- **Determinismo:** Ginkgo `Eventually(func, timeout, interval)` > `while sleep N`
- **Paralelismo:** Ginkgo `--procs=4` + Labels > execução serial bloqueante
- **Assertions:** Gomega matchers com diff detalhado > `grep -q || journey_fail`
- **Maintenance:** Go com type safety > bash com string parsing
- **CI:** JUnit XML + GitHub Actions annotations > exit code opaco
- **Idempotência:** `DeferCleanup` garantido > cleanup manual que pode falhar

## Objective

**Done = suite Ginkgo em `infra/tests/e2e/` que testa 100% das jornadas do LoopZero + edge cases + failure modes contra k3d local. `ginkgo --label-filter="journey" ./infra/tests/e2e/` substitui completamente os 11 bash journey scripts. Zero bash scripts para testes.**

Metas específicas:
- 11 jornadas completas migradas para Ginkgo specs
- 18+ edge cases implementados (concurrency, idempotency, chaos, timeouts)
- 7 failure scenarios (atualmente PIt) implementados
- Labels: `journey`, `edge-case`, `chaos`, `slow`, `smoke`
- Cada spec idempotente e auto-cleaning (Prova de Morte via DeferCleanup)
- Execução completa < 45 min (vs ~2h executando os 11 scripts serialmente)
- README com instruções de execução e troubleshooting

## Governance

**Classificação:** T2 (test infrastructure + Taskfile + operational procedure changes). Requer MEET record antes da execução per `pre-release-governance.md`. Este plano é a proposta — meeting deve aprovar abordagem antes de Phase 0.

## ADRs

### D1 — Ginkgo v2 como framework único

**Decision:** Todos os testes E2E usam Ginkgo v2 + Gomega. Nenhum bash script para testing.
**Rationale:** Já temos 52 files Ginkgo com helpers maduros. A equipe é Go-first. Ginkgo oferece: `Eventually` (polling), `DeferCleanup` (teardown garantido), Labels (filtering), `--procs` (parallelismo), JUnit XML (CI), `Ordered` (sequência quando necessário). Alternativas avaliadas: BATS (bash, mesmos problemas), Terratest (verbose demais), Venom (YAML, menos expressivo), Chainsaw (K8s-only, não testa CLI).
**Consequences:** Testes são Go code — exigem compilação. Tradeoff aceito: type safety + assertions fortes + IDE support valem o build time.

### D2 — exec.Command("loopzero") como interface de teste

**Decision:** Testes chamam o CLI binário `loopzero` via `exec.Command`. Nunca importam código interno do CLI.
**Rationale:** O teste deve provar o que o USUÁRIO experimenta — não o que funções internas retornam. Se o binário está no PATH e responde corretamente, o sistema funciona. Isso testa a stack completa: CLI → HTTP → API → DB → Argo → BuildKit → ArgoCD → Pod.
**Consequences:** Requer `loopzero` compilado e no PATH. Testes são mais lentos (process spawn) mas testam a realidade completa.

### D3 — Eventually para todo polling, Consistently para invariantes

**Decision:** Todo polling usa `Eventually(func, timeout, interval)`. Toda asserção de estabilidade usa `Consistently(func, duration, interval)`.
**Rationale:** Elimina sleep loops. `Eventually` re-executa a função a cada interval até timeout — é o padrão Ginkgo nativo e substitui 100% dos `while sleep` dos bash scripts. `Consistently` prova que algo PERMANECE verdade (ex: app continua respondendo 200 por 30s após rollback).
**Consequences:** Timeouts são explícitos por operação: deploy=25min, scale=2min, HTTP probe=5min, cleanup=5min.

### D4 — Labels para categorização e execução seletiva

**Decision:** Todo spec tem Labels. Categorias: `journey` (fluxos completos), `edge-case` (cenários limítrofes), `chaos` (com falha injetada), `slow` (>5min), `smoke` (subset mínimo <5min).
**Rationale:** Desenvolvedor roda `--label-filter="smoke"` no dia a dia (<5min). CI roda `--label-filter="journey || edge-case"`. Release roda ALL. Sem labels, é tudo-ou-nada.
**Consequences:** Todo `Describe`/`It` DEVE ter pelo menos um Label. Enforcement via code review.

### D5 — DeferCleanup para Prova de Morte

**Decision:** Todo spec que cria recursos usa `DeferCleanup` para garantir cleanup. Prova de Morte (namespace gone, ArgoCD app gone, resources removed) é uma assertion no cleanup, não apenas um side effect.
**Rationale:** Bash scripts usam `prove_dead` como assertion separada que pode ser ignorada. Com `DeferCleanup`, cleanup é GARANTIDO mesmo em panic/failure. E podemos ASSERT dentro do cleanup (Prova de Morte como validação).
**Consequences:** Specs são self-cleaning — rodar N vezes produz mesmo resultado.

### D6 — Fixtures consolidadas em infra/tests/e2e/fixtures/

**Decision:** Todas as fixtures vivem em `infra/tests/e2e/fixtures/`. Fixtures de `dogfood/fixtures/` são copiadas/consolidadas ali.
**Rationale:** Um lugar canônico. Evita duplicação (`solo-server` existe em ambos). Ginkgo helpers (`copyFixture`, `fixturesPath`) já apontam para `e2e/fixtures/`.
**Consequences:** Fixtures `broken-build`, `secret-app`, `versioned-app`, `multi-server` migram de `dogfood/fixtures/` para `e2e/fixtures/`.

### D7 — Suite suite_test.go única com múltiplos files

**Decision:** Todos os testes vivem no package `e2e` com uma suite (`deploy_suite_test.go`). Organização por file, não por package.
**Rationale:** Já é assim (52 files no mesmo package). Manter padrão. Ginkgo detecta automaticamente todos os `Describe` no package. Separar em sub-packages quebraria helpers compartilhados.
**Consequences:** Files nomeados semanticamente: `journey_solo_deploy_test.go`, `edge_concurrent_test.go`, `failure_build_test.go`.

### D8 — Timeouts explícitos por tipo de operação

**Decision:** Timeouts padronizados definidos como constantes:
- `deployTimeout = 25 * time.Minute` (build + sign + gitops + sync + probe)
- `scaleTimeout = 2 * time.Minute`
- `promoteTimeout = 5 * time.Minute`
- `rollbackTimeout = 3 * time.Minute`
- `httpProbeTimeout = 5 * time.Minute`
- `cleanupTimeout = 5 * time.Minute`
- `cliCommandTimeout = 30 * time.Second`

**Rationale:** Bash scripts usam magic numbers (`1500`, `300`, `180`). Constantes nomeadas são self-documenting e ajustáveis em um lugar.
**Consequences:** Se uma operação leva mais que seu timeout, o teste falha imediatamente com mensagem clara.

## Dependency Graph

```
Phase 0 (Foundation) ──▶ Phase 1 (Core Journeys) ──▶ Phase 3 (Cleanup)
                    │            │
                    │            ├── T1.1-T1.6 (paralelas entre si)
                    │            │
                    │            ▼
                    │     Phase 2 (Edge Cases + Failures)
                    │            │
                    │            ├── T2.1-T2.4 (paralelas entre si)
                    │            │
                    │            ▼
                    │     Phase 3 (Cleanup + Docs)
                    │
                    ├── T0.1 Helpers expandidos
                    ├── T0.2 Fixtures consolidadas
                    └── T0.3 Constants + suite config
```

- Phase 0 é fundação — tudo depende dele.
- Phase 1 tasks são independentes entre si (journeys não compartilham estado).
- Phase 2 depende de Phase 1 (reutiliza helpers de deploy). Tasks dentro são independentes.
- Phase 3 depende de ambas (cleanup + docs).

---

## Phase 0: Foundation

**Objective:** Expandir helpers compartilhados, consolidar fixtures, e configurar suite para suportar as novas jornadas.

### T0.1 — Helpers Expandidos

#### Objective
Adicionar helpers que faltam para migrar as jornadas: `waitBuildTerminal`, `proveDead`, `waitHTTP200WithHost`, `deployAndWaitSuccess`, `assertAuditEntry`. Consolidar helpers duplicados existentes.

#### Evidence
- Bash `_common.sh` tem: `wait_build_terminal`, `wait_http_200`, `prove_dead`, `assert_audit_entry`, `wait_release_active`, `resolve_traefik_port_for_tenant`
- Ginkgo helpers existentes estão em **dois arquivos**: `helpers_test.go` (infra: apiURL, httpGet, copyFixture, cleanupTenant, etc.) e `cli_helpers_test.go` (CLI: runLoopZero, expectNoInternalTerms, expectAllowedExitCode, parseJSON, etc.)
- `expectNoInternalTerms` JÁ EXISTE em `cli_helpers_test.go:142` — NÃO criar duplicata
- `assertNoForbiddenTerms` JÁ EXISTE em `concurrent_deploy_test.go:70` — consolidar com `expectNoInternalTerms`
- `forbiddenTerms` (concurrent_deploy_test.go:30) e `internalTerms` (cli_helpers_test.go:31) são listas duplicadas — consolidar em `constants_test.go`
- `runLoopZeroInDir` JÁ EXISTE em `cli_validate_test.go:24` — mover para helpers compartilhados
- `runLoopZeroDeploy` e `initGitRepo` JÁ EXISTEM em `deploy_happy_path_test.go:34,59`
- 11 journeys vão reutilizar esses helpers — extrair uma vez, usar em todas

#### Files to edit
```
infra/tests/e2e/helpers_test.go — expandir com novos helpers de jornada (waitBuildTerminal, proveDead, etc.)
infra/tests/e2e/cli_helpers_test.go — consolidar helpers dispersos (mover runLoopZeroInDir, runLoopZeroDeploy, initGitRepo para cá)
infra/tests/e2e/constants_test.go — (NEW) timeouts, constantes, e lista unificada de forbidden terms
infra/tests/e2e/concurrent_deploy_test.go — remover forbiddenTerms local (migrar para constants_test.go)
```

#### Deep file dependency analysis
- **`helpers_test.go`** — já tem `apiURL()`, `httpGet()`, `copyFixture()`, `cleanupTenant()`, `apiHealthy()`, `clusterRunning()`, `uniqueSuffix()`, `verifyNoActiveBuilds()`, `verifyAPIHealth()`. Adicionamos funções de jornada sem quebrar existentes.
- **`cli_helpers_test.go`** — já tem `runLoopZero()`, `expectNoInternalTerms()`, `expectAllowedExitCode()`, `parseJSON()`, `runLoopZeroNoToken()`, `runLoopZeroWithStdin()`, `loopzeroCLIAvailable()`, `loopzeroTokenAvailable()`, `jsonStringField()`, `jsonBoolField()`. Consolidar helpers dispersos de outros files para cá.
- **`constants_test.go`** — novo arquivo com timeouts nomeados + lista unificada `forbiddenInternalTerms` (substituindo duplicatas em cli_helpers_test.go e concurrent_deploy_test.go). Consumido por todos os test files.

#### Deep Dives
- **`waitBuildTerminal(buildID, timeout)`** — polls `GET /api/v1/builds/{buildID}` via API até status ∈ {SUCCEEDED, FAILED, CANCELLED, TIMEOUT}. Usa `Eventually` internamente. Retorna status final. Refresh de token se 401.
- **`proveDead(tenant, project, env)`** — polls kubectl no workload cluster: verifica zero resources com label `loopzero.io/project=<project>` no namespace `loopzero-<tenant>-<env>`. Se namespace não existe, retorna imediatamente. Usa `Eventually` com `cleanupTimeout`.
- **`waitHTTP200WithHost(host, path, timeout)`** — resolve Traefik port via tenant cell lookup (mesma lógica de `resolve_traefik_port_for_tenant`). curl com `--connect-to`. Usa `Eventually`.
- **`deployAndWaitSuccess(dir, env, args...)`** — convenience: `runLoopZeroInDir` + extract buildID + `waitBuildTerminal` + assert SUCCEEDED. Retorna buildID e deploy JSON.
- **`assertAuditEntry(action, project)`** — GET `/api/v1/audit?limit=200` + busca entry com action matching. Usa Gomega matchers.

#### Tasks
1. Criar `infra/tests/e2e/constants_test.go` com timeouts + lista unificada `forbiddenInternalTerms`
2. Consolidar `forbiddenTerms` (concurrent_deploy_test.go) e `internalTerms` (cli_helpers_test.go) na lista unificada de constants_test.go
3. Mover `runLoopZeroInDir` (cli_validate_test.go), `runLoopZeroDeploy`/`initGitRepo` (deploy_happy_path_test.go) para `cli_helpers_test.go`
4. Implementar `waitBuildTerminal` em `helpers_test.go`
5. Implementar `proveDead` em `helpers_test.go`
6. Implementar `waitHTTP200WithHost` em `helpers_test.go`
7. Implementar `deployAndWaitSuccess` em `helpers_test.go`
8. Implementar `assertAuditEntry` em `helpers_test.go`
9. Implementar `resolveTraefikPort` em `helpers_test.go`
10. Implementar `ensureProjectDeleted` (idempotent cleanup helper)

#### TDD
```
RED:     TestWaitBuildTerminal_TimesOutOnUnknownBuild — polls unknown build, returns TIMEOUT
RED:     TestProveDead_NamespaceGone — namespace doesn't exist, returns immediately
RED:     TestDeployAndWaitSuccess_ExtractsBuildID — mock-ish: just verify buildID extraction from JSON
GREEN:   Implement all helpers
REFACTOR: Extract common polling pattern if duplicated
VERIFY:  go test ./infra/tests/e2e/ -run "TestHelper" -count=1
```

#### Acceptance Criteria
- [ ] `waitBuildTerminal` polls com interval 15s, timeout configurável, retorna status string
- [ ] `proveDead` verifica via kubectl que zero resources existem para project
- [ ] `waitHTTP200WithHost` resolve Traefik port por cell, faz HTTP GET com host header
- [ ] `deployAndWaitSuccess` é one-liner que encapsula deploy + wait
- [ ] `assertAuditEntry` busca audit log por action+project
- [ ] Todos usam `Eventually` internamente (não sleep loops)
- [ ] Token refresh em 401 (mesma lógica do bash `wait_build_terminal`)

#### DoD
- [ ] Helpers compilam sem erro
- [ ] Helpers integram com os 53 tests existentes (nenhum breaking change)
- [ ] `go vet ./infra/tests/e2e/` green

---

### T0.2 — Fixtures Consolidadas

#### Objective
Copiar fixtures de `dogfood/fixtures/` para `e2e/fixtures/` como fonte canônica.

#### Evidence
- `dogfood/fixtures/` tem: `broken-build`, `multi-server`, `secret-app`, `solo-server`, `versioned-app`
- `e2e/fixtures/` já tem: `hello-server`, `multi-app`, `dogfood-monorepo`, `static-frontend` (4 fixtures)
- Journeys referenciam fixtures de dogfood — consolidar em um lugar

#### Files to edit
```
infra/tests/e2e/fixtures/solo-server/ — (NEW) copiar de dogfood/fixtures/solo-server
infra/tests/e2e/fixtures/multi-server/ — (NEW) copiar de dogfood/fixtures/multi-server
infra/tests/e2e/fixtures/secret-app/ — (NEW) copiar de dogfood/fixtures/secret-app
infra/tests/e2e/fixtures/versioned-app/ — (NEW) copiar de dogfood/fixtures/versioned-app
infra/tests/e2e/fixtures/broken-build/ — (NEW) copiar de dogfood/fixtures/broken-build
```

#### Tasks
1. Copiar 5 fixtures de `dogfood/fixtures/` para `e2e/fixtures/`
2. Verificar que cada fixture tem `loopzero.yaml` válido
3. Verificar que `copyFixture("solo-server")` funciona para os novos nomes

#### TDD
```
RED:     copyFixture("versioned-app") retorna erro (não existe)
GREEN:   Copiar fixture, função retorna path válido
VERIFY:  go test ./infra/tests/e2e/ -run "TestFixture" -count=1 (se existir)
```

#### Acceptance Criteria
- [ ] 5 fixtures copiadas com loopzero.yaml válido
- [ ] `copyFixture(name)` funciona para todas as 9 fixtures (4 existentes + 5 novas)
- [ ] Nenhuma fixture duplicada

#### DoD
- [ ] `ls infra/tests/e2e/fixtures/` mostra 9 diretórios
- [ ] Fixtures têm loopzero.yaml válido

---

### T0.3 — Suite Configuration

#### Objective
Configurar a suite para Labels, timeouts globais, e reporting.

#### Files to edit
```
infra/tests/e2e/deploy_suite_test.go — atualizar com Label config e reporter
```

#### Deep file dependency analysis
- **`deploy_suite_test.go`** — entrypoint da suite. Atualmente apenas `RegisterFailHandler` + `RunSpecs`. Precisa de `SuiteConfig` para timeout global, reporter para JUnit XML, e setup de environment variables.

#### Tasks
1. Adicionar `suiteConfig` com `DefaultReporterConfig` (JUnit path via env)
2. Adicionar `SynchronizedBeforeSuite` para seed database + verify infra
3. Adicionar `SynchronizedAfterSuite` para cleanup global
4. Configurar `GinkgoWriter` para structured output
5. Ajustar `taskfiles/test.yml` timeout de `15m` para `60m` (deploy timeout isolado é 25min — suite precisa acomodar múltiplas jornadas)

#### TDD
```
RED:     go test ./infra/tests/e2e/ -v (sem mudanças, apenas confirma que suite roda)
GREEN:   Adicionar configuração, suite continua rodando
VERIFY:  go test ./infra/tests/e2e/ -v --ginkgo.dry-run (lista todos os specs)
```

#### Acceptance Criteria
- [ ] Suite roda com `--label-filter="smoke"` (subset)
- [ ] JUnit XML gerado quando `JUNIT_REPORT_PATH` definido
- [ ] `SynchronizedBeforeSuite` verifica API + cluster health
- [ ] Skip automático se infra não estiver disponível

#### DoD
- [ ] Suite compila e roda com `LOOPZERO_E2E=1`
- [ ] Nenhum teste existente quebra

---

## Phase 1: Core Journeys

**Objective:** Migrar as 11 bash journeys para Ginkgo specs. Cada journey é um file separado com `Describe` Ordered.

### T1.1 — Journey 01: Solo Deploy + Prova de Vida + Prova de Morte

#### Objective
Deploy single-app, verificar HTTP 200, deletar project, verificar cleanup completo.

#### Evidence
- `journey-01-solo-deploy.sh`: validate → deploy → build SUCCEEDED → HTTP 200 → audit → delete → prove_dead
- Cobre o fluxo mais fundamental — se isso falha, nada funciona

#### Files to edit
```
infra/tests/e2e/journey_solo_deploy_test.go — (NEW) journey 01 em Ginkgo
```

#### Deep file dependency analysis
- Usa helpers: `copyFixture("solo-server")`, `initGitRepo`, `runLoopZeroInDir`, `deployAndWaitSuccess`, `waitHTTP200WithHost`, `assertAuditEntry`, `proveDead`
- Fixture: `solo-server` (loopzero.yaml com 1 app: api, type: server)
- Cleanup: `DeferCleanup` com `loopzero project delete` + `proveDead`

#### Deep Dives
- **Ordered container:** validate → deploy → probe → audit → delete → prove_dead (sequência obrigatória)
- **Prova de Vida:** `waitHTTP200WithHost` no host `{tenant}-solo-server-api-production.loopzero.local` com path `/health`
- **Prova de Morte:** `proveDead(tenant, "solo-server", "production")` — zero resources com label loopzero.io/project=solo-server
- **Idempotência:** `BeforeAll` deleta project se existir de run anterior
- **Rule 3:** `expectNoInternalTerms` em TODO output (validate, deploy, delete)

#### Tasks
1. Criar `infra/tests/e2e/journey_solo_deploy_test.go`
2. Implementar `Describe("Journey 01: Solo Deploy", Ordered, Label("journey", "smoke"))`
3. `BeforeAll`: cleanup idempotente + copyFixture + initGitRepo
4. `It("validates project structure")`: loopzero validate --json → exit 0, valid=true
5. `It("deploys and build succeeds")`: deployAndWaitSuccess → SUCCEEDED
6. `It("app responds HTTP 200")`: waitHTTP200WithHost → 200 + body contains project name
7. `It("audit log records deploy")`: assertAuditEntry("build.submitted", "solo-server")
8. `It("project delete succeeds")`: loopzero project delete --yes --json → exit 0
9. `It("Prova de Morte: cleanup complete")`: proveDead → zero resources

#### TDD
```
RED:     ginkgo --label-filter="journey" --dry-run — lista "Journey 01" mas falha (não implementado)
GREEN:   Implementar specs, rodar contra k3d
REFACTOR: Extract common patterns to helpers if repeated
VERIFY:  LOOPZERO_E2E=1 ginkgo --label-filter="journey" --focus="Solo Deploy" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Deploy real executa e build SUCCEEDED
- [ ] HTTP 200 no app com host header correto
- [ ] Audit log contém entry de deploy
- [ ] Prova de Morte: namespace clean após delete
- [ ] Rule 3: zero internal terms em todo output
- [ ] Exit codes corretos (0 para success)
- [ ] Spec idempotente (rodar 2x produz mesmo resultado)

#### DoD
- [ ] Spec passa contra k3d local
- [ ] `--json` output validado (schema)
- [ ] Tempo total < 25 min

---

### T1.2 — Journey 02: Multi-Server Monorepo

#### Objective
Deploy monorepo com api + worker, verificar ambos running, validar naming correto.

#### Evidence
- `journey-02-multi-server.sh`: 2 apps (api server + worker), 2 builds, api tem IngressRoute, worker não
- Valida que multi-app pipeline funciona end-to-end

#### Files to edit
```
infra/tests/e2e/journey_multi_server_test.go — (NEW) journey 02
```

#### Deep Dives
- **2 builds paralelos:** Argo Workflow gera 2× build-image + 2× sign-image tasks
- **Naming:** deployments são `multi-server-api` e `multi-server-worker` (prefixados com project name)
- **Worker sem IngressRoute:** type=worker → não expõe endpoint HTTP, mas pod deve estar Running
- **Verificação worker:** kubectl get pod com label `loopzero.io/app=worker` → phase=Running

#### Tasks
1. Criar `infra/tests/e2e/journey_multi_server_test.go`
2. `Describe("Journey 02: Multi-Server", Ordered, Label("journey"))`
3. Deploy multi-server fixture
4. Assert 2 builds succeed (ou 1 build com 2 images — depende da pipeline)
5. Assert API HTTP 200
6. Assert worker pod Running (via kubectl)
7. Cleanup + Prova de Morte (ambos gone)

#### TDD
```
RED:     ginkgo --focus="Multi-Server" --dry-run — listed
GREEN:   Implement against k3d
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Multi-Server" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] 2 apps deployed com naming correto (project-prefixed)
- [ ] API: HTTP 200 via IngressRoute
- [ ] Worker: pod Running sem IngressRoute
- [ ] Prova de Morte: ambos deployments removed
- [ ] Rule 3 clean em todo output

#### DoD
- [ ] Spec passa contra k3d
- [ ] Multi-app build pipeline validada

---

### T1.3 — Journey 03: Secrets Lifecycle

#### Objective
Testar fluxo completo de secrets: set → deploy → app vê env var → delete → deploy → env var removida.

#### Evidence
- `journey-03-secrets.sh`: loopzero secret set + redeploy + curl /env/SECRET_PING + loopzero secret delete + redeploy + verify gone
- Spec §9: secrets lifecycle

#### Files to edit
```
infra/tests/e2e/journey_secrets_test.go — (NEW) journey 03
```

#### Deep Dives
- **ExternalSecret:** `loopzero secret set` cria ExternalSecret CR → ESO sincroniza → K8s Secret criado → pod mounts como env var
- **Verificação runtime:** app fixture `secret-app` expõe `/env/:name` que retorna o valor da env var
- **Temporal:** redeploy necessário para pod pegar novo Secret (pods não fazem hot-reload de Secrets)
- **Edge case:** `loopzero secret list --json` NUNCA retorna valores (apenas nomes)

#### Tasks
1. Criar `infra/tests/e2e/journey_secrets_test.go`
2. `Describe("Journey 03: Secrets Lifecycle", Ordered, Label("journey"))`
3. Deploy `secret-app` fixture
4. `loopzero secret set SECRET_PING=value --json` → exit 0
5. Redeploy (para pod pegar nova env)
6. HTTP GET `/env/SECRET_PING` → retorna value
7. `loopzero secret list --json` → contém "SECRET_PING", NÃO contém value
8. `loopzero secret delete SECRET_PING --json` → exit 0
9. Redeploy
10. HTTP GET `/env/SECRET_PING` → 404 ou empty
11. Cleanup

#### TDD
```
RED:     ginkgo --focus="Secrets" --dry-run — listed
GREEN:   Implement against k3d
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Secrets" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Secret set + redeploy → app vê env var
- [ ] Secret list mostra nome, NUNCA valor
- [ ] Secret delete + redeploy → env var removida
- [ ] Rule 3 clean
- [ ] Cleanup complete

#### DoD
- [ ] Spec passa contra k3d
- [ ] Secrets lifecycle end-to-end validado

---

### T1.4 — Journey 04-06: Scale + Promote + Rollback

#### Objective
Três jornadas que validam operações pós-deploy: scale, promote, rollback.

#### Evidence
- `journey-04-scale.sh`: loopzero scale --replicas 3 → verify deployment.spec.replicas == 3; scale --replicas 11 → 4xx (cap rejection)
- `journey-05-promote.sh`: deploy em staging → loopzero promote --from staging --to production → production mesma versão
- `journey-06-rollback.sh`: deploy v1 → deploy v2 → rollback to v1 → /version=v1

#### Files to edit
```
infra/tests/e2e/journey_scale_test.go — (NEW) journey 04
infra/tests/e2e/journey_promote_test.go — (NEW) journey 05
infra/tests/e2e/journey_rollback_test.go — (NEW) journey 06
```

#### Deep Dives
- **Scale:** `loopzero scale --replicas N` updates Deployment via API. Cap enforcement: Starter=2, Pro=5, Team=10. Exceeder → 4xx.
- **Promote:** Copia release de staging para production. Mesma imagem, novo release row. Production recebe tráfego via IngressRoute.
- **Rollback:** `loopzero rollback --to <release_id>` ativa release anterior. ArgoCD sync com manifesto do release antigo. Verificação: `/version` retorna conteúdo de VERSION.txt correspondente.
- **Fixture versioned-app:** VERSION.txt → app expõe GET /version → retorna conteúdo do arquivo no build time.

#### Tasks
1. **Scale:** deploy solo-server → scale --replicas 3 → kubectl verify → scale --replicas 11 → expect rejection
2. **Promote:** deploy versioned-app em staging (v1) → verify staging HTTP → promote to production → verify production HTTP mesma versão
3. **Rollback:** deploy v1 → verify /version=v1 → deploy v2 → verify /version=v2 → rollback to release v1 → verify /version=v1

#### TDD
```
RED:     ginkgo --focus="Scale|Promote|Rollback" --dry-run — listed
GREEN:   Implement each against k3d
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Scale" ./infra/tests/e2e/
         LOOPZERO_E2E=1 ginkgo --focus="Promote" ./infra/tests/e2e/
         LOOPZERO_E2E=1 ginkgo --focus="Rollback" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Scale: replicas mudam de fato (kubectl verify)
- [ ] Scale cap: over-plan → rejection com exit code correto
- [ ] Promote: production recebe mesma versão que staging
- [ ] Rollback: app reverte para versão anterior verificada via HTTP
- [ ] Rule 3 + exit codes corretos em todos

#### DoD
- [ ] 3 specs passam contra k3d
- [ ] Cada spec < 30 min

---

### T1.5 — Journey 07-09: Domain + Autodeploy + Preview

#### Objective
Jornadas avançadas: custom domain, GitHub autodeploy, preview environments.

#### Evidence
- `journey-07-custom-domain.sh`: loopzero domain add → CF Zone flow → DNS verification
- `journey-08-github-autodeploy.sh`: webhook → auto build (requer GitHub App ou mock webhook)
- `journey-09-preview.sh`: POST /previews API → preview created → CLI commands → delete → cleanup

#### Files to edit
```
infra/tests/e2e/journey_custom_domain_test.go — (NEW) journey 07
infra/tests/e2e/journey_autodeploy_test.go — (NEW) journey 08
infra/tests/e2e/journey_preview_test.go — (NEW) journey 09
```

#### Deep Dives
- **Custom domain (local limitation):** CF Zone Management requer real DNS. Localmente testamos o API flow até o ponto de "pending_ns" → verify CLI output correto. Full flow requer DO.
- **Autodeploy (local limitation):** GitHub webhook requer public ingress. Localmente, POST synthetic webhook ao API como journey-08 faz.
- **Preview:** Já existe `preview_lifecycle_test.go` com webhook simulation. Expandir para cobrir CLI commands: `loopzero preview list/inspect/delete`.

#### Tasks
1. **Domain:** `loopzero domain add example.com --json` → verify API response + CLI output
2. **Autodeploy:** POST synthetic webhook → verify build triggered → build SUCCEEDED
3. **Preview:** POST /previews → `loopzero preview list` → `loopzero preview inspect` → `loopzero preview delete` → verify gone

#### TDD
```
RED:     ginkgo --focus="Domain|Autodeploy|Preview" --dry-run
GREEN:   Implement against k3d
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Domain" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Domain: API accepts, CLI shows pending_ns state
- [ ] Autodeploy: synthetic webhook triggers real build
- [ ] Preview: full CRUD lifecycle via CLI + API
- [ ] Cleanup em todas (DeferCleanup)
- [ ] Skip annotation se feature requer infra indisponível

#### DoD
- [ ] 3 specs passam (ou Skip com reason claro se infra indisponível)

---

### T1.6 — Journey 10-11: Failure Modes + Admin

#### Objective
Cenários de falha (exit codes, Rule 3) e comandos admin.

#### Evidence
- `journey-10-failures.sh`: invalid token → exit 4; quota exceeded → 403; build failure → exit 2; Rule 3 validation em todos
- `journey-11-admin.sh`: loopzero admin tenants evict --dry-run; pattern too broad rejection; missing service-auth → reject

#### Files to edit
```
infra/tests/e2e/journey_failures_test.go — (NEW) journey 10
infra/tests/e2e/journey_admin_test.go — (NEW) journey 11
```

#### Deep Dives
- **Failure exit codes:** `loopzero deploy` com broken Dockerfile → build fails → exit 2. Token inválido → exit 4. Deploy com env locked → exit 3.
- **Rule 3 enforcement:** TODO output de cenários de erro é verificado contra internal terms.
- **Admin:** requer `LOOPZERO_SERVICE_AUTH` header. Testa: dry-run nomatch=0, pattern too broad rejection, missing auth rejection, real tenant match.
- **Fixture broken-build:** Dockerfile com `FROM nonexistent-image:v99` → build failure garantida.

#### Tasks
1. **Failures:** invalid token → exit 4; broken build → exit 2; Rule 3 em todos
2. **Admin:** evict dry-run, pattern rejection, auth missing, real match

#### TDD
```
RED:     ginkgo --focus="Failures|Admin" --dry-run
GREEN:   Implement against k3d
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Failures" ./infra/tests/e2e/
         LOOPZERO_E2E=1 ginkgo --focus="Admin" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Exit code 4 para invalid/missing token
- [ ] Exit code 2 para build failure
- [ ] Rule 3: ZERO internal terms em mensagens de erro
- [ ] Admin: dry-run funciona, broad pattern rejected, missing auth rejected
- [ ] CI mode (CI=true): no spinners, structured output

#### DoD
- [ ] Specs cobrem todos os exit codes (0/1/2/3/4/5)
- [ ] Rule 3 verificado em TODO output

---

## Phase 2: Edge Cases + Failure Scenarios

**Objective:** Implementar edge cases que os bash scripts não cobrem + preencher os 7 PIt do failure_scenarios_test.go.

### T2.1 — Concurrency + Idempotency

#### Objective
Expandir testes de concurrency existentes e adicionar idempotency key replay e lock timeout.

#### Evidence
- `concurrent_deploy_test.go` já existe com 2 specs robustos:
  - "should reject concurrent deploys for the same environment" (barrier pattern, asserts 1×0 + 1×3, DA-18 message, Rule 3, JSON validity)
  - "should allow concurrent deploys to different environments" (production+staging, both exit 0)
- **O que já está coberto:** 2 goroutines same env (1×202+1×409), 2 goroutines diff env (both succeed), Rule 3 em todo output, JSON validity
- **O que falta:** idempotency replay, lock timeout recovery, 3+ concurrent deploys
- CLAUDE.md: "Concurrent-build check MUST be inside the lock"

#### Files to edit
```
infra/tests/e2e/edge_concurrency_test.go — (NEW) expanded concurrency tests
```

#### Deep Dives
- **Idempotency-Key:** POST deploy com mesmo key 2x → segunda vez retorna cached response (202), NÃO cria novo build
- **Lock timeout:** Deploy A locks env → Deploy B waits 5s → timeout → 409 com "deploy in progress"
- **3 concurrent:** 1×202 + 2×409 (only one wins)
- **Cross-env allowed:** Deploy to production + deploy to staging simultaneously → both succeed

#### Tasks
1. `It("idempotency key replay returns cached response")` — (NEW)
2. `It("3 concurrent deploys: 1 wins, 2 rejected with 409")` — (NEW, expande os 2 goroutines existentes)
3. `It("lock released after build completes, next deploy succeeds")` — (NEW)
4. Manter specs existentes de `concurrent_deploy_test.go` (2 specs com barrier pattern já cobrem same-env rejection + cross-env success)

#### TDD
```
RED:     ginkgo --focus="Concurrency" --dry-run
GREEN:   Implement with goroutines + barrier pattern
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Concurrency" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Idempotency-Key replay retorna mesma response
- [ ] Lock timeout correto (5s)
- [ ] 3 concurrent: exactly 1 wins
- [ ] Cross-env: ambos succeed
- [ ] Rule 3 + exit codes em TODOS os cenários

#### DoD
- [ ] Specs determinísticos (passam 10x consecutivos)

---

### T2.2 — Failure Scenarios (implementar os 7 PIt)

#### Objective
Implementar os failure scenarios que estão como PIt (pending) em `failure_scenarios_test.go`.

#### Evidence
- `failure_scenarios_test.go` tem 7 `PIt()` — sprint 8 exit gate requer implementação:
  - Build Failures (2): Dockerfile syntax error, dependency install failure
  - Deploy Failures (2): health probe fail, sync timeout
  - Partial Failures (1): some apps succeed others fail
  - Recovery (2): retry after transient failure, lock release on API crash
- Cenários: Dockerfile syntax error, dependency install failure, health probe fail, sync timeout, partial multi-app failure, transient retry, lock recovery

#### Files to edit
```
infra/tests/e2e/failure_scenarios_test.go — implementar os PIt como It reais
```

#### Deep Dives
- **Dockerfile syntax error:** fixture `broken-build` com Dockerfile inválido → exit 2
- **Health probe timeout:** app que não responde /health → deploy timeout → exit 3
- **Partial failure:** multi-app com 1 app válido + 1 broken → exit 5 (partial)
- **Recovery:** após falha, próximo deploy com código fixo deve funcionar (lock released)

#### Tasks
1. Converter todos 7 `PIt` para `It` um por um
2. Implementar fixtures necessárias (broken-dockerfile, slow-health, partial-fail)
3. Assert exit codes corretos por cenário
4. Assert mensagens de erro são actionable (sugerem próximo passo)
5. Assert Rule 3 em TODAS as mensagens de erro

#### TDD
```
RED:     Converter PIt → It (specs falham porque não têm body)
GREEN:   Implementar cada spec
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="Failure" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] 7 specs implementados (zero PIt restante)
- [ ] Exit code 2 para build failures
- [ ] Exit code 3 para deploy failures
- [ ] Exit code 5 para partial failures
- [ ] Mensagens sugerem ação (ex: "Run `loopzero deploy` to retry")
- [ ] Recovery funciona após cada falha
- [ ] Rule 3 em todas as mensagens

#### DoD
- [ ] Zero PIt no failure_scenarios_test.go
- [ ] Todos os exit codes validados

---

### T2.3 — State Machine + Timeout Edge Cases

#### Objective
Testar edge cases de state machines: invalid transitions, build timeout (>30min), cancel mid-build.

#### Evidence
- CLAUDE.md: "Invalid transitions panic (programming error)"
- State machine: QUEUED→RUNNING→RELEASING→SUCCEEDED. QUEUED>5m timeout, RUNNING>30m timeout.
- `loopzero cancel` durante build → CANCELLING → CANCELLED

#### Files to edit
```
infra/tests/e2e/edge_state_machine_test.go — (NEW) state machine edge cases
```

#### Deep Dives
- **Cancel mid-build:** `loopzero deploy --no-wait` → esperar status RUNNING → `loopzero cancel --json` → verify CANCELLED
- **Build already terminal:** `loopzero cancel` em build SUCCEEDED → exit 1 (usage error: "Build already completed")
- **Timeout awareness:** Não podemos esperar 30min reais. Testamos via: build que falha rapidamente (broken-build) e verificamos que state machine transiciona corretamente.

#### Tasks
1. `It("cancel during RUNNING transitions to CANCELLED")`
2. `It("cancel on SUCCEEDED returns usage error")`
3. `It("failed build transitions to FAILED, not stuck")`
4. `It("deploy after cancelled build succeeds")`

#### TDD
```
RED:     ginkgo --focus="State Machine" --dry-run
GREEN:   Implement
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="State Machine" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] Cancel mid-build → CANCELLED
- [ ] Cancel on terminal → error with correct exit code
- [ ] Builds never stuck (always reach terminal)
- [ ] Post-failure deploy works

#### DoD
- [ ] State transitions verified via API polling

---

### T2.4 — JSON Schema + CI Mode + Multi-Cell

#### Objective
Validar --json schema stability, CI mode behavior, e multi-cell routing.

#### Evidence
- Rule 5: "--json field names are a stability contract"
- CLAUDE.md: "CI mode (CI=true): no spinners, no color, structured output"
- Multi-cell: deploy no cell correto baseado em tenant placement

#### Files to edit
```
infra/tests/e2e/edge_json_schema_test.go — (NEW) JSON contract validation
infra/tests/e2e/edge_ci_mode_test.go — (NEW) CI mode behavior
infra/tests/e2e/edge_multi_cell_test.go — (NEW) cell routing (if 2+ cells available)
```

#### Deep Dives
- **JSON schema:** Para cada comando com --json, validar que campos obrigatórios existem e tipos são corretos. Não validar valores — validar shape.
- **CI mode:** Com `CI=true`, output não deve ter ANSI codes, spinners (ora), ou prompts interativos.
- **Multi-cell:** Se workload cluster tem cell-2 configurado, verify que deploy para tenant pinned em cell-2 resulta em pod no cluster cell-2.

#### Tasks
1. JSON schema: validate `loopzero deploy --json`, `loopzero status --json`, `loopzero rollback --json` fields
2. CI mode: `CI=true loopzero deploy --json` → no ANSI, structured only
3. Multi-cell: conditional spec (Skip if single-cell) — tenant pinned → pod in correct cell

#### TDD
```
RED:     ginkgo --focus="JSON Schema|CI Mode|Multi-Cell" --dry-run
GREEN:   Implement
VERIFY:  LOOPZERO_E2E=1 ginkgo --focus="JSON Schema" ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] JSON fields obrigatórios presentes em todos os comandos
- [ ] CI mode: zero ANSI escape codes no output
- [ ] Multi-cell: pod land no cell correto (se disponível)
- [ ] All commands validate Rule 3 + Rule 4

#### DoD
- [ ] Schema validation passes para 8+ commands
- [ ] CI mode deterministic

---

## Phase 3: Cleanup + Documentation

**Objective:** Remover bash scripts, atualizar README, validar suite completa.

### T3.1 — Deprecar Bash Journeys

#### Objective
Marcar bash journeys como deprecated, mover para `_legacy/`. NÃO deletar imediatamente (referência).

#### Files to edit
```
infra/tests/dogfood/journeys/_legacy/ — (NEW) mover bash scripts para cá
```

#### Tasks
1. Criar `infra/tests/dogfood/journeys/_legacy/`
2. Mover todos os `journey-*.sh` + `_common.sh` para `_legacy/`
3. Manter `preflight.sh` (ainda útil como quick health check)
4. Adicionar `_legacy/DEPRECATED.md` com: "Use `ginkgo --label-filter=journey ./infra/tests/e2e/` em vez destes scripts."

#### Acceptance Criteria
- [ ] Bash journeys movidos para `_legacy/`
- [ ] `_legacy/DEPRECATED.md` documenta alternativa
- [ ] Nenhum Taskfile task referencia os scripts antigos

#### DoD
- [ ] Scripts em `_legacy/` (não deletados, apenas movidos)

---

### T3.2 — Taskfile Integration

#### Objective
Adicionar tasks para executar os novos Ginkgo specs por categoria.

#### Files to edit
```
taskfiles/test.yml — adicionar tasks para Ginkgo journeys
```

#### Tasks
1. `test:e2e:journey` — roda todas as jornadas (`--label-filter="journey"`)
2. `test:e2e:smoke` — subset rápido (`--label-filter="smoke"`, <5min)
3. `test:e2e:edge` — edge cases (`--label-filter="edge-case"`)
4. `test:e2e:all` — tudo (`LOOPZERO_E2E=1 ginkgo ./infra/tests/e2e/`)
5. `test:e2e:focus` — run com focus específico (variável FOCUS)

#### Acceptance Criteria
- [ ] `task test:e2e:smoke` roda em < 5 min
- [ ] `task test:e2e:journey` roda todas as jornadas
- [ ] `task test:e2e:all` é a suite completa
- [ ] `task --list | grep "test:e2e"` mostra 5 tasks

#### DoD
- [ ] Tasks funcionam contra k3d

---

### T3.3 — README

#### Objective
Documentar como rodar os novos testes.

#### Files to edit
```
infra/tests/e2e/README.md — (NEW) documentação completa
```

#### Tasks
1. Quick Start: 3 comandos (setup + smoke + all)
2. Labels explicadas com exemplos
3. Troubleshooting: timeout, auth, infra not ready
4. Como adicionar novo test
5. Relação com antigos bash journey scripts

#### Acceptance Criteria
- [ ] Quick Start funciona copy-paste
- [ ] Labels documentadas
- [ ] Troubleshooting cobre top-5 issues

#### DoD
- [ ] README completo e prático

---

### T3.4 — Validation Run

#### Objective
Rodar suite completa contra k3d, confirmar que substitui os 11 bash journey scripts.

#### Tasks
1. `task setup && tilt up && task doctor`
2. `LOOPZERO_E2E=1 ginkgo --label-filter="journey" ./infra/tests/e2e/` — todas passam
3. `LOOPZERO_E2E=1 ginkgo --label-filter="edge-case" ./infra/tests/e2e/` — todas passam
4. `LOOPZERO_E2E=1 ginkgo ./infra/tests/e2e/ --junit-report=report.xml` — XML gerado
5. Comparar cobertura: todo cenário que os 11 bash journeys testavam está coberto em Ginkgo
6. `ginkgo --label-filter="smoke"` < 5 min confirmado

#### Acceptance Criteria
- [ ] Zero falhas em journeys
- [ ] Zero falhas em edge cases
- [ ] JUnit XML válido
- [ ] Smoke < 5 min
- [ ] Full suite < 45 min
- [ ] 100% cobertura vs bash journeys

#### DoD
- [ ] Validation matrix GREEN

---

## Coverage Matrix

| # | Requirement | Task(s) | Resolution |
|---|------------|---------|-----------|
| 1 | Solo deploy + Prova de Vida/Morte | T1.1 | Ginkgo Ordered spec with DeferCleanup |
| 2 | Multi-server monorepo | T1.2 | Verify 2 apps + naming + worker pod |
| 3 | Secrets lifecycle | T1.3 | Set → verify env → delete → verify gone |
| 4 | Scale + cap rejection | T1.4 | Scale replicas + over-cap 4xx |
| 5 | Promote staging→prod | T1.4 | Deploy staging → promote → verify prod |
| 6 | Rollback v1→v2→v1 | T1.4 | Deploy twice → rollback → verify version |
| 7 | Custom domain | T1.5 | API flow + CLI output validation |
| 8 | GitHub autodeploy | T1.5 | Synthetic webhook → build triggered |
| 9 | Preview environments | T1.5 | Full CRUD lifecycle |
| 10 | Failure modes (exit codes) | T1.6 | Exit 1/2/3/4/5 validated |
| 11 | Admin commands | T1.6 | Evict dry-run, auth gate, pattern rejection |
| 12 | Concurrent deploys | T2.1 | 1×202 + N×409 deterministic (2-goroutine specs JÁ EXISTEM em concurrent_deploy_test.go; T2.1 expande para 3+ e idempotency) |
| 13 | Idempotency replay | T2.1 | Same key → cached response (NEW) |
| 14 | Lock timeout | T2.1 | 5s wait → 409 (NEW) |
| 15 | State machine transitions | T2.3 | Cancel, terminal check, recovery |
| 16 | Build timeout | T2.3 | Broken build → FAILED (not stuck) |
| 17 | Failure scenarios (7 PIt) | T2.2 | All 7 implemented |
| 18 | JSON schema validation | T2.4 | Field presence + type check |
| 19 | CI mode | T2.4 | No ANSI, structured output |
| 20 | Multi-cell routing | T2.4 | Pod in correct cell |
| 21 | Rule 3 (no internal terms) | ALL | expectNoInternalTerms in every spec |
| 22 | Exit codes (0-5 only) | ALL | expectAllowedExitCode in every spec |
| 23 | Prova de Morte (cleanup) | ALL | DeferCleanup + proveDead assertion |
| 24 | SSE streaming | T1.5 (autodeploy) + T2.2 | Build events received during deploy |
| 25 | Auth expired mid-op | T2.2 | Token refresh handling |
| 26 | Orphan cleanup (reaper) | T2.3 | After delete, zero orphan CRs |

**Coverage: 26/26 requirements covered (100%)**

## Global Definition of Done

- [ ] All 4 phases complete
- [ ] `ginkgo --label-filter="journey" ./infra/tests/e2e/` — 11 specs PASS
- [ ] `ginkgo --label-filter="edge-case" ./infra/tests/e2e/` — 10+ specs PASS
- [ ] `failure_scenarios_test.go` — zero PIt remaining (7 implementados)
- [ ] Zero `golangci-lint` warnings in `infra/tests/e2e/`
- [ ] `go vet ./infra/tests/e2e/` green
- [ ] JUnit XML gerado e válido
- [ ] Smoke suite < 5 min
- [ ] Full suite < 45 min
- [ ] Bash journeys deprecated em `_legacy/` (11 scripts + _common.sh)
- [ ] Taskfile tasks funcionais
- [ ] README documentado
- [ ] **`/dogfood full` PASSED**

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Run the NEW Ginkgo E2E suite (replaces 11 bash journey scripts)
LOOPZERO_E2E=1 ginkgo -v --label-filter="journey || edge-case" \
  --junit-report=dogfood-report.xml \
  ./infra/tests/e2e/

# 3. Run full dogfood (validates beyond just tests)
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] Ginkgo journey suite: 11/11 PASS
- [ ] Ginkgo edge-case suite: 10+/10+ PASS
- [ ] Prova de Vida passes for every deploy
- [ ] Prova de Morte passes for every delete
- [ ] No regressions in previously passing flows
- [ ] Rule 3 clean: zero internal terms in any output
- [ ] JUnit XML generated and parseable

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.
