# Plan: Dogfood 2026-04-29 — Remediação dos 13 achados (F-01..F-13)

> **Version 1.0** — Endereça os 13 achados do dogfood pós-iter-12 (`docs/plans/end-to-end-deploy-validation-plan.md` v1.1) que confirmou HTTP 200 via Traefik mas revelou que o **caminho-do-usuário-real está quebrado em standalone-multi**: API não alcançável pelo CLI (S0), exit code drift sistêmico (S1), Tenant duplicado entre namespaces (S1), init silently-success com token inválido (S1) e mais 9 achados S2/S3. Este plano fecha cada achado com fix de código, regression test, runbook e gate de governança. Resultado esperado: dogfood-loop H1 (`login → init → deploy → status → rollback → delete` com `LOOPZERO_TOKEN` real) executável programaticamente em <30 min num k3d local fresh, com Prova de Vida + Prova de Morte verificáveis.

## Context

**O que existe hoje (estado pós-iter-12, validado em 2026-04-29):**

- 3 clusters k3d (mgmt+build+workload) operacionais com 38+ helm releases via L2 helmfile.
- Cadeia produto interna funciona: `CLI → API → Argo → BuildKit → S3 → Gitea → ArgoCD → Traefik → nginx-s3-gateway → MinIO → HTTP 200` (324 bytes do `index.html` real construído pelo BuildKit em iter 12).
- 56 Go regression tests passando (`infra/tests/load/regression_test.go`), B59-B82 todos com proteção permanente.
- ArgoCD Application `tn-e2etest-production` aponta para destino `https://172.18.0.10:6443` (workload k3d) com path-based routing CRDs (Certificate/Middleware/IngressRoute) aplicados.

**O que está quebrado (evidência do dogfood):**

1. **F-01 (S0):** `~/.loopzero/config.json` aponta para `localhost:13001`, k3d-mgmt mapeia `host:13001 → cluster:30001`, mas **nenhum Service ocupa NodePort 30001**. loopzero-api é `ClusterIP` only e a IngressRoute de `infra/helm/loopzero-api/templates/ingressroute.yaml:14` casa apenas `Host(\`api.{{ .Values.domain }}\`)` (`api.dev.loopzero.example`). Resultado: `curl localhost:13001/api/v1/health → HTTP 000`. Sem `tilt up` ou port-forward manual, **zero comandos CLI funcionam**.

2. **F-02 (S1, sistêmico):** LoopZeroError typed corretamente serializado em JSON (`exit_code:4`, `code:"AUTH_REQUIRED"`), mas `process.exit(err.exitCode)` esquecido em ~14 handlers. `cli/src/index.ts:179,188,238` setam `process.exitCode = 1|exitCode` em alguns catches; outros code paths emitem só mensagem. Cada `loopzero deploy --json` sem auth → JSON correto, processo retorna 0. CI/scripts com `set -e` não interrompem.

3. **F-03 (S2):** Commander.js não usa `.exitOverride()`. `loopzero nonexistent-command` e `loopzero deploy --bogus-flag` retornam exit 0 (devem ser 1 per Rule 4).

4. **F-04 (S1):** `kubectl get tenants -A` mostra 2 instâncias: `default/tn-e2etest` (plan=enterprise) E `loopzero-system/tn-e2etest` (plan=starter). Mesma duplicação em Environments. Plan enforcement (billing/quota/RBAC) indeterminístico. `.status.cellID` vazio apesar de `.spec.cellID=cell-default`.

5. **F-05 (S1):** `LOOPZERO_TOKEN=fake loopzero init --yes --json` retorna `{"warning":"Invalid or expired token"}` exit 0 — **silent-success em path auth degradado**. Próximo `loopzero deploy` falha surpresa.

6. **F-06 (S3):** `loopzero logout` quando não autenticado emite "Logout cancelled." (implica que houve prompt e usuário disse não — não houve).

7. **F-07 (S2):** `GET /api/v1/version` → 404. Endpoint público esperado pelo skill de dogfood não existe.

8. **F-08 (S3):** `GET /metrics` na porta 3001 (Service port) → 404. 209 `loopzero_*` metrics existem mas só em `localhost:9090` interno. ServiceMonitor aponta para 9090 (correto), mas usuários que tentam scrape via Service externo falham.

9. **F-09 (S2):** `operators/internal/controller/environment_rbac.go:143` faz `fmt.Errorf("source pull secret loopzero-system/loopzero-system: ...")` — concat errado de `<ns>/<name>` resulta em `Secret "loopzero-system" not found`. Loga "pull secret ensure failed (non-fatal)" continuamente.

10. **F-10 (S2):** `operators/internal/tracing/tracing.go:26` define `DefaultEndpoint = "otel-collector.loopzero-system.svc.cluster.local:4317"`. Em standalone-multi não há otel-collector — operator emite "rpc error: code = Unavailable desc = name resolver error" a cada 30s.

11. **F-11 (S2):** `argocd-application-controller` em StatefulSet 3-réplicas; sharding causa "Cluster has been assigned to shard 1 / Ignoring cluster" em pods que não possuem o shard. Sync conclui mas status fica em `Unknown::Progressing`. Health checks para Traefik IngressRoute/Middleware ausentes (Lua scripts).

12. **F-12 (S2):** `/api/v1/health/deep` retorna `{"argocd":"auth_failed","status":"degraded"}` HTTP 503 em standalone-multi. ArgoCD admin secret discovery falha.

13. **F-13 (S3):** 4 pods Argo Workflow em `Error` na ns `argo` há 1.5-2h. `infra/charts/loopzero-build-cwts` define `ttlStrategy.secondsAfterCompletion` mas pods de step com falha permanecem.

**Evidência consolidada:** ver `Relatório de Dogfooding — LoopZero PaaS (escopo: full)` produzido em 2026-04-29 (anexo a este plano via conversation context).

**Por que isso importa AGORA:** o plano-pai (`end-to-end-deploy-validation-plan` v1.1) declarou HTTP 200 como sucesso, mas o trabalho foi via `kubectl patch` + `helm upgrade` (operações de operador). Sem este plano, a próxima rodada de dogfood vai bloquear no mesmo lugar. Sem F-01 fix, customer self-hosting morre no `loopzero whoami`.

## Objective

**Done:** dogfood loop completo (`login → init → deploy → status → rollback → delete`) com `LOOPZERO_TOKEN` real, executando contra k3d standalone-multi fresh em <30min, **sem `tilt up` e sem port-forward manual**, com Prova de Vida + Prova de Morte programaticamente verificadas.

Specific measurable goals:

1. `loopzero whoami` (e todos os comandos auth-required) retornam exit code do processo === `exit_code` do JSON em ambos modos JSON e human (Rule 4 / F-02 fechado).
2. `loopzero platform install -e standalone-multi` deixa `localhost:13001/api/v1/health → HTTP 200` sem trabalho extra (F-01 fechado).
3. `kubectl get tenants -A` mostra **uma única** Tenant CR canônica em `loopzero-system`; webhook rejeita criação cross-namespace (F-04 fechado).
4. `loopzero init` com token inválido retorna exit 4 ANTES de escrever loopzero.yaml (F-05 fechado).
5. Operator logs limpos: zero "pull secret ensure failed" + zero "traces export rpc error" em standalone-multi (F-09 + F-10 fechados).
6. ArgoCD Application `tn-e2etest-production` consolida em `sync=Synced health=Healthy` em <5min após deploy via custom Lua health checks (F-11 fechado).
7. `/api/v1/version` retorna 200 com schema `{version, commit, build_date}` (F-07 fechado).
8. `loopzero nonexistent-command` retorna exit 1 (F-03 fechado).
9. Argo Workflow pods em `Error` removidos em <24h via TTL strategy ajustada (F-13 fechado).
10. **H1 dogfood loop** automatizado em `infra/tests/e2e/dogfood_loop_test.go` PASS gated em `LOOPZERO_E2E_DOGFOOD=1`.

## ADRs

### D1 — Único endpoint user-facing local: NodePort fixo + IngressRoute localhost
**Decisão:** em standalone-multi, loopzero-api Service tipo `NodePort` com `nodePort: 30001` (mapeado por k3d-mgmt para `host:13001`) E IngressRoute adicional matching `Host(\`localhost\`) || Host(\`127.0.0.1\`) || Host(\`api.loopzero.local\`)` no entry point `web` (HTTP).
**Rationale:**
- NodePort sozinho funciona mas pula Traefik (loga ausente, métricas perdidas).
- IngressRoute sozinho exige cert válido para `localhost` (pain).
- NodePort + IngressRoute localhost atende: `loopzero whoami` via `localhost:13001` direto E `https://api.loopzero.local` via /etc/hosts hack opcional.
- Em dev/prod profiles, NodePort 30001 fica desabilitado por overlay (`service.type: ClusterIP`).
**Consequências:**
- (+) zero `tilt up` requirement em standalone-multi.
- (+) CLI funciona out-of-the-box após `loopzero platform install`.
- (−) NodePort consome porta no nó. Aceitável: cluster mgmt já tem 8 NodePorts, +1 = 9.

### D2 — Handler central de exit code em `cli/src/index.ts`
**Decisão:** todo erro propagado via `LoopZeroError` ou caído no catch global do action handler DEVE setar `process.exitCode = err.exitCode || 1` antes de qualquer `console.log`/`writeJSON`. Adicionar 1 wrapper `runCommand(handler)` em `cli/src/lib/run-command.ts` (NEW) que envolve o handler async, captura erros, escreve JSON ou human, e seta exitCode. Todos os 36 comandos migram para usar este wrapper.
**Rationale:**
- 14 lugares atualmente setam exitCode manualmente — repetição com bug latente em alguns paths (login, deploy, init, lists).
- Padrão central elimina drift e permite 1 regression test que faz `fork` + `waitpid` para validar `exit_code === parseInt(json.exit_code)`.
**Consequências:**
- (+) Rule 4 enforced em todos os comandos.
- (+) Adicionar novo comando = 1 linha, herda comportamento.
- (−) Refactor cross-cutting de 36 commands. Aceitável com TDD.

### D3 — Single Source of Truth para Tenant CR namespace = `loopzero-system`
**Decisão:** Tenant + Environment CRs SEMPRE em `loopzero-system`. API + tenant-controller passam a usar `loopzero-system` exclusivamente. ValidatingAdmissionPolicy ou webhook rejeita criação em outras namespaces. Migration script `infra/scripts/migrate-tenant-namespace.sh` move CRs existentes em `default` para `loopzero-system` preservando finalizers/spec.
**Rationale:**
- 2 tenants ativos com plans divergentes = bug crítico (qual manda em billing?).
- `default` é namespace user-friendly para outros recursos; CRs LoopZero são platform-scoped → `loopzero-system`.
- ValidatingAdmissionPolicy é nativo K8s 1.30+ (pin é 1.32) — sem dependência de cert-manager para webhook.
**Consequências:**
- (+) Plan enforcement determinístico.
- (+) RBAC simplificado (1 namespace para watch).
- (−) Customers que tinham Tenant em outro ns precisam migrar. Mitigado por migration script idempotente.

### D4 — Init falha-fast em token inválido
**Decisão:** `loopzero init` faz HEAD `/api/v1/whoami` ANTES de detectar apps. Se 401/403, exit 4 imediato. Se 5xx ou network error, exit 5 (partial) com flag `--offline` para forçar local-only.
**Rationale:**
- Silent-success era o anti-padrão Rule 8 (Falhe alto, falhe cedo, falhe claro) explicitamente proibido em CLAUDE.md.
- Adicionar `--offline` cobre legítimo caso de "criar loopzero.yaml sem internet" (CI bootstrap).
**Consequências:**
- (+) Usuário descobre auth break antes de ter local artifacts órfãos.
- (−) +1 round-trip à API em init. <100ms aceitável.

### D5 — OTel exporter opt-in (não default)
**Decisão:** `operators/internal/tracing/tracing.go` muda `DefaultEndpoint` de `otel-collector.loopzero-system.svc.cluster.local:4317` para `""` (vazio). Tracing só ativa se `OTEL_EXPORTER_OTLP_ENDPOINT` definido explicitamente. Helm values dev/prod setam o endpoint; standalone-multi não seta.
**Rationale:**
- Default "ligado para um collector que pode não existir" gera log noise + custo de retry sem benefício.
- Opt-in via env é padrão de OpenTelemetry SDK (não-presença = noop exporter).
**Consequências:**
- (+) Standalone-multi sem log noise.
- (−) Customers que esquecem de setar env perdem traces. Mitigado por documentação explícita no runbook.

### D6 — ArgoCD application-controller replicas = 1 em standalone-multi + Lua health checks Traefik/cert-manager
**Decisão:** `infra/helm/argocd/values-standalone-multi.yaml` (NEW overlay) seta `controller.replicas: 1`. `infra/argocd-bootstrap/configmap-resource-customizations.yaml` (NEW) define Lua scripts para `traefik.io/IngressRoute`, `traefik.io/Middleware`, `cert-manager.io/Certificate`.
**Rationale:**
- Sharding com 3 réplicas em k3d local causa cluster-reassignment race após restart.
- Health=`""` para Traefik CRDs vira `Progressing` infinito sem health check definido.
- Lua scripts ArgoCD são "config-as-data" — sem deploy de CRD adicional.
**Consequências:**
- (+) UI consolidada para deploys static frontend.
- (−) Replicas=1 perde HA do controller em standalone. Aceitável (1-cluster local).

### D7 — `/api/v1/version` é endpoint público (sem auth)
**Decisão:** novo `api/internal/routes/version.go` (NEW) registra GET `/api/v1/version` sem middleware de auth. Retorna `{version, commit, build_date, go_version}` injetados via build flags `-X main.Version=...`.
**Rationale:**
- Skill de dogfood + spec esperam endpoint público.
- Permite "smoke test" simples sem credenciais.
- Sem PII, sem dados sensíveis.
**Consequências:**
- (+) Health check externo simplificado.
- (−) Reveal versão pública (info disclosure trivial). Aceitável: produto open-source futuramente.

### D8 — `runCommand` wrapper como deep module
**Decisão:** novo `cli/src/lib/run-command.ts` exporta uma única função `runCommand<T>(handler: (ctx) => Promise<T>, options?: RunOptions)`. Encapsula: timeout, JSON vs human, exit code, error serialization, AbortController, telemetry. Handler recebe `ctx` com `{json: boolean, signal: AbortSignal, log: Logger}`. Comandos chamam `runCommand(async ctx => { ... return result; })`.
**Rationale:**
- Rule "deep module": 1 entrypoint simples (`runCommand`) cobre toda a complexidade transversal.
- Antes: cada comando duplicava try/catch/timeout/json. Refactor consolida ~600 linhas de boilerplate.
**Consequências:**
- (+) DRY + testabilidade central.
- (+) Adicionar nova feature transversal (p.ex. tracing) = 1 file change.
- (−) Mudança grande (36 commands migram). Mitigado por phased migration: D8 cobre F-02/F-03/F-05 num só lugar.

## Dependency Graph

```
Phase 0 (Diagnose) ──▶ Phase 1 (CLI exit code central — F-02/F-03/F-05/F-06)
                  │
                  ├──▶ Phase 2 (API expose local — F-01) ────┐
                  │                                          ├──▶ Phase 6 (Dogfood loop H1)
                  ├──▶ Phase 3 (Tenant SoT — F-04) ──────────┤
                  │                                          │
                  ├──▶ Phase 4 (Operator logs limpos — F-09/F-10)
                  │
                  └──▶ Phase 5 (ArgoCD UI + API endpoints — F-07/F-08/F-11/F-12/F-13)
```

- **Phase 1** e **Phase 2** podem rodar em paralelo (CLI vs infra).
- **Phase 3** é independente; toca operator + API + webhook.
- **Phase 4** depende só de operator code.
- **Phase 5** mistura ArgoCD config + API routes; pode paralelizar.
- **Phase 6** é gate sequencial — exige 1+2+3+4+5 mergeados.

---

## Phase 0: Diagnose Confirmation + Pre-Flight

**Objective:** reproduzir os 13 achados em commit-time + escrever 1 baseline test que falha (RED) por achado para servir de gatekeeper antes de mergear cada fix.

### T0.1 — Reproduzir cada achado em script idempotente

#### Objective
`infra/tests/dogfood/repro-findings.sh` (NEW) reproduz F-01..F-13 contra k3d standalone-multi, emite JSON `{finding_id, status, evidence}` para cada um, falha (exit 1) se QUALQUER achado já foi corrigido sem update no script.

#### Evidence
Sem script de repro, próxima sessão de fix re-investiga. iter 12 perdeu ~3h re-mapeando bugs já documentados.

#### Files to edit
```
infra/tests/dogfood/repro-findings.sh — (NEW) script bash que reproduz F-01..F-13 e emite JSON
infra/tests/dogfood/README.md — (NEW) docs do contrato JSON e como rodar
Taskfile.yml — adiciona task `dogfood:repro` que invoca o script
```

#### Deep file dependency analysis
- Script novo, sem downstream. `Taskfile.yml` (existing) ganha 1 entry de ~10 linhas. Não afeta build.

#### Deep Dives
- **Idempotência:** script precisa lidar com estado pós-fix (alguns achados já podem estar corrigidos em sessões futuras). Cada check retorna `{status: "REPRODUCED"|"FIXED"|"INDETERMINATE", evidence}`.
- **kubectl path:** usa `KUBECONFIG=$(k3d kubeconfig write loopzero-mgmt)` em vez de `--context` para funcionar offline-first.
- **Saída:** JSON em stdout, summary table em stderr. Exit 0 = todos achados ainda reproduzem (modo "antes do fix"); exit 1 = pelo menos 1 corrigido (atualize matriz!).

#### Tasks
1. Esqueleto bash com `set -euo pipefail`, traps de cleanup.
2. Função `check_F01()`: `curl -s -o /dev/null -w "%{http_code}" --max-time 3 http://localhost:13001/api/v1/health` — se HTTP=000 → REPRODUCED.
3. Função `check_F02()`: chama `loopzero deploy --json` sem auth, parsea exit code, compara com JSON `exit_code`.
4. ... uma função por achado.
5. `report()` agrega resultados em `--output json|table`.

#### TDD
```
RED:     test_repro_script_emits_valid_json — bash + jq smoke test
RED:     test_repro_script_detects_fix_when_F01_resolved — mock NodePort, expect status=FIXED
GREEN:   Implement script
REFACTOR: extract common kubectl helper
VERIFY:  task dogfood:repro
```

#### Acceptance Criteria
- [ ] Script executável (chmod 755), shellcheck-clean.
- [ ] JSON schema: `{findings: [{id, status, evidence, severity}], summary: {total, reproduced, fixed}}`.
- [ ] Exit 0 quando todos REPRODUCED, exit 1 quando algum FIXED.
- [ ] `task dogfood:repro` integrado no `task validate:full`.
- [ ] Pass: shellcheck zero warnings.

#### DoD
- [ ] Script roda em <60s.
- [ ] README.md descreve cada finding ID + comando para validar manualmente.
- [ ] CI gate (não-bloqueante) em `.github/workflows/ci.yml`.

---

## Phase 1: CLI Exit Code Central + Init Fail-Fast (F-02/F-03/F-05/F-06)

**Objective:** introduzir `runCommand` wrapper em `cli/src/lib/run-command.ts`, migrar 36 comandos, eliminar exit code drift e adicionar regression test que valida `process.exit === json.exit_code`.

### T1.1 — Implementar `runCommand` deep module (D8)

#### Objective
Criar wrapper `runCommand<T>(handler, options)` que centraliza: error catch (LoopZeroError + unknown), JSON vs human output, exit code, AbortController/timeout, structured logging.

#### Evidence
- `cli/src/index.ts:179,188,238` mostram 3 paths diferentes setando exitCode.
- `cli/src/register/{auth,project,env,scale}.ts` repetem boilerplate de try/catch/withTimeout.
- F-02 diretamente: `loopzero deploy --json` retorna exit 0 em vez de 4.

#### Files to edit
```
cli/src/lib/run-command.ts — (NEW) deep module com runCommand<T>(handler, options)
cli/src/lib/errors.ts — (existing) garante que LoopZeroError exporta exitCode field
cli/tests/unit/lib/run-command.test.ts — (NEW) ~15 specs
cli/src/index.ts — refactor catch global para usar runCommand
```

#### Deep file dependency analysis
- `cli/src/lib/run-command.ts` (NEW): export único `runCommand`. Importado por todos os 36 commands em phases subsequentes.
- `cli/src/lib/errors.ts` (existing): adicionar `static fromUnknown(err: unknown, defaultCode = 1): LoopZeroError` se ausente.
- `cli/src/index.ts` (existing): catch global atual tem 3 paths divergentes; consolida em chamada única a `runCommand` no top-level.

#### Deep Dives
- **API:**
  ```ts
  export interface RunOptions {
    json?: boolean;
    timeout?: number;        // ms
    signal?: AbortSignal;    // pre-existing signal (compose com SIGINT)
  }
  export interface RunContext {
    json: boolean;
    signal: AbortSignal;
    log: Logger;
  }
  export async function runCommand<T>(
    handler: (ctx: RunContext) => Promise<T>,
    options?: RunOptions
  ): Promise<void> {
    // 1. Setup AbortController + timeout
    // 2. Setup ctx.log (depende de json mode)
    // 3. try { result = await handler(ctx); writeOutput(result, ctx.json) }
    //    catch (e) { loopzeroErr = LoopZeroError.fromUnknown(e); writeError(loopzeroErr, ctx.json); process.exitCode = loopzeroErr.exitCode }
    // 4. cleanup signal listeners
  }
  ```
- **Invariante:** `runCommand` SEMPRE seta `process.exitCode` (0 em sucesso, loopzeroErr.exitCode em falha). Nunca chama `process.exit()` diretamente — deixa Node.js fazer flush dos streams.
- **Edge cases:** handler que `throw 'string'` → wrap em LoopZeroError(`UNKNOWN_ERROR`, exit 1). Handler que retorna `undefined` → no-op output em JSON mode (não emite `{"data":null}`).

#### Tasks
1. Definir interfaces em `cli/src/lib/run-command.ts`.
2. Implementar `runCommand` core (~80 linhas).
3. Adicionar `LoopZeroError.fromUnknown` em `cli/src/lib/errors.ts` se ausente.
4. Refactor `cli/src/index.ts` linha 90-240 para usar `runCommand` no top-level catch.
5. Validar via `cli/src/register/auth.ts` (1 comando como piloto).

#### TDD
```
RED:     test_runCommand_sets_exitCode_4_on_LoopZeroError_AUTH_REQUIRED
RED:     test_runCommand_sets_exitCode_1_on_unknown_error
RED:     test_runCommand_writes_json_when_options_json_true
RED:     test_runCommand_writes_human_when_options_json_false
RED:     test_runCommand_propagates_AbortSignal_to_handler
RED:     test_runCommand_aborts_on_timeout
RED:     test_runCommand_does_not_call_process_exit_directly
RED:     test_runCommand_handles_string_throw
RED:     test_runCommand_handles_undefined_return
GREEN:   Implementar core
REFACTOR: extrair writeOutput / writeError helpers
VERIFY:  cd cli && npm test -- run-command
```

#### Acceptance Criteria
- [ ] 9 specs novos em `cli/tests/unit/lib/run-command.test.ts` PASS.
- [ ] `runCommand` ≤ 100 linhas (deep module = simples API, complexidade interna).
- [ ] Cyclomatic complexity ≤ 8 (try/catch + branches).
- [ ] `cli/src/index.ts` reduzido em ≥ 30 linhas (consolidação).
- [ ] Pass: /code-audit complexity ≤ 10, coverage ≥ 90% para o novo módulo.

#### DoD
- [ ] `npm test` PASS (1108 → 1117 = +9 novos).
- [ ] `npm run lint` zero warnings.
- [ ] `auth.ts` migrado e funcionando (smoke: `loopzero whoami` exit code preserva).

---

### T1.2 — Migrar 36 comandos para `runCommand` + regression test fork-based

#### Objective
Substituir try/catch/exitCode boilerplate em todos os comandos pelo wrapper. Adicionar 1 regression test integrado que faz `child_process.spawnSync('loopzero', ['X', '--json'])` e valida `result.status === parseInt(JSON.parse(result.stdout).exit_code)`.

#### Evidence
- 14 lugares atualmente setam exitCode (grep counted earlier).
- F-02 sintoma: `loopzero deploy --json` exit 0 com `exit_code:4`. Precisa cobertura de TODOS os comandos.

#### Files to edit
```
cli/src/register/auth.ts — usa runCommand
cli/src/register/deploy.ts — usa runCommand
cli/src/register/init.ts — usa runCommand
cli/src/register/project.ts — usa runCommand
cli/src/register/env.ts — usa runCommand
cli/src/register/build.ts — usa runCommand
cli/src/register/team.ts — usa runCommand
cli/src/register/secret.ts — usa runCommand
cli/src/register/cancel.ts — usa runCommand
cli/src/register/rollback.ts — usa runCommand
cli/src/register/promote.ts — usa runCommand
cli/src/register/logs.ts — usa runCommand
cli/src/register/status.ts — usa runCommand
cli/src/register/scale.ts — usa runCommand
cli/src/register/validate.ts — usa runCommand
cli/src/register/connect.ts — usa runCommand
cli/src/register/disconnect.ts — usa runCommand
cli/src/register/billing.ts — usa runCommand
cli/src/register/audit.ts — usa runCommand
cli/src/register/preview.ts — usa runCommand
cli/src/register/domain.ts — usa runCommand
cli/src/register/platform.ts — usa runCommand
cli/src/index.ts — adiciona .exitOverride() do Commander para F-03
cli/tests/integration/exit-code-contract.test.ts — (NEW) fork-based test para 36 commands
```

#### Deep file dependency analysis
Cada `register/*.ts` (existing): mudanças são puramente locais — substituem `try/catch/withTimeout/process.exitCode` por `runCommand(async ctx => {...})`. Lógica de negócio intocada. Downstream: `index.ts` registra os comandos via `register*()` functions; nenhuma mudança na ordem de registro.

`cli/src/index.ts`: adicionar `.exitOverride((err) => { if (err.code === 'commander.unknownCommand' || err.code === 'commander.unknownOption') { process.exitCode = 1; throw err; } })` no `program` root para F-03.

#### Deep Dives
- **Migration pattern (mecânico):**
  ```ts
  // BEFORE
  .action(async (opts) => {
    try {
      const result = await withTimeout(handler(opts), TIMEOUT);
      if (opts.json) console.log(JSON.stringify(result));
      else printHuman(result);
    } catch (e) {
      const err = LoopZeroError.fromUnknown(e);
      if (opts.json) console.log(JSON.stringify(err.toJSON()));
      else console.error(err.message);
      process.exitCode = err.exitCode;
    }
  });
  
  // AFTER
  .action(async (opts) => {
    await runCommand(async (ctx) => {
      return handler(opts, ctx);
    }, { json: opts.json, timeout: TIMEOUT });
  });
  ```
- **Regression test design:** spawn child process, capture stdout + status:
  ```ts
  const result = spawnSync('node', ['dist/index.js', 'whoami', '--json'], {
    env: { ...process.env, LOOPZERO_URL: 'http://localhost:9999' /* unreachable */ }
  });
  const parsed = JSON.parse(result.stdout.toString());
  expect(result.status).toBe(parsed.exit_code);
  ```
  Loop sobre 36 comandos com matrix `(command, expectedErrorCode)`.

#### Tasks
1. Migrar `auth.ts` (já piloto T1.1) — re-validar.
2. Migrar `deploy.ts`, `init.ts`, `project.ts`, `env.ts`, `build.ts` (top-5 mais usados).
3. Migrar restantes em batches de 5.
4. Adicionar `.exitOverride()` em `index.ts`.
5. Escrever `exit-code-contract.test.ts` com matrix de 36 comandos.
6. Rodar full test suite + dogfood repro script.

#### TDD
```
RED:     test_all_commands_set_exitCode_matching_json_exit_code (matrix 36 cmds)
RED:     test_unknown_command_returns_exit_1 (F-03)
RED:     test_unknown_flag_returns_exit_1 (F-03)
RED:     test_logout_when_unauth_says_already_logged_out_not_cancelled (F-06)
GREEN:   Migrar comandos + .exitOverride()
REFACTOR: extrair fixture spawnLoopZero() helper
VERIFY:  cd cli && npm test
```

#### Acceptance Criteria
- [ ] 36/36 commands migrados.
- [ ] `exit-code-contract.test.ts` com matrix completa PASS.
- [ ] `loopzero nonexistent-command` exit 1.
- [ ] `loopzero deploy --bogus-flag` exit 1.
- [ ] `loopzero logout` (sem auth) emite "Already logged out." exit 0.
- [ ] Pass: /code-audit complexity ≤ 10 em todos `register/*.ts`.

#### DoD
- [ ] `npm test` PASS (cobre +36 specs novos no contract test).
- [ ] `task dogfood:repro` reporta F-02/F-03/F-06 = FIXED.
- [ ] Smoke manual: `loopzero deploy --json && echo $?` → 4 (com auth ausente).

---

### T1.3 — Init fail-fast em token inválido (F-05)

#### Objective
`loopzero init` faz pre-flight `whoami` antes de detectar apps. Token inválido → exit 4 sem escrever loopzero.yaml. Flag `--offline` permite local-only.

#### Evidence
F-05 reprodução: `LOOPZERO_TOKEN=fake loopzero init --yes --json` → `{"warning":"Invalid or expired token"}` exit 0. Próximo `loopzero deploy` falha surpresa.

#### Files to edit
```
cli/src/register/init.ts — adiciona pre-flight whoami + --offline flag
cli/src/services/init-service.ts — (NEW se ausente, ou existing) lógica de detect que aceita ctx
cli/tests/unit/init/init-fail-fast.test.ts — (NEW) ~5 specs
```

#### Deep file dependency analysis
- `init.ts` (existing): orquestração; adicionar bloco `if (!opts.offline) { await preflightAuth() }` antes de `detectApps()`.
- `init-service.ts` (existing): `detectApps()` continua puro. `preflightAuth()` faz HEAD request ao API.

#### Deep Dives
- **API call:** `GET /api/v1/auth/whoami` (já existe); error codes 401/403 → `throw new LoopZeroError('AUTH_FAILED', 4)`. 5xx → `throw new LoopZeroError('SERVICE_UNAVAILABLE', 5)` com hint `--offline`.
- **`--offline` flag:** documentado como "skip server validation; loopzero.yaml created locally only; subsequent `loopzero deploy` will require login first". Adicionar warning no output se usado.
- **Edge case:** se usuário NÃO setou token (sem `~/.loopzero/config.json`) — init pode prosseguir? Decisão: NÃO. Em modo non-offline, init exige auth. Em offline mode, OK.

#### Tasks
1. Adicionar `--offline` boolean flag em init command.
2. Implementar `preflightAuth()` em `init-service.ts`.
3. Encadear no action handler antes de `detectApps()`.
4. Atualizar mensagens human/json.
5. Escrever specs.

#### TDD
```
RED:     test_init_fails_fast_with_REDACTED_SECRET_063_no_yaml_written
RED:     test_init_fails_fast_with_no_token_no_yaml_written
RED:     test_init_offline_proceeds_without_auth
RED:     test_init_offline_emits_warning_in_human_mode
RED:     test_init_offline_emits_warning_field_in_json_mode
GREEN:   Implementar preflightAuth + --offline
REFACTOR: None expected
VERIFY:  cd cli && npm test -- init
```

#### Acceptance Criteria
- [ ] Init com bad token: exit 4, NO loopzero.yaml escrito, mensagem clara com link p/ criar PAT.
- [ ] Init com `--offline`: exit 0, loopzero.yaml escrito, warning visível.
- [ ] Init sem auth + sem `--offline`: exit 4.
- [ ] Pass: /code-audit coverage ≥ 90% em init-service.ts.

#### DoD
- [ ] `task dogfood:repro` reporta F-05 = FIXED.
- [ ] Documentação `docs/cli-reference.md` atualizada com `--offline`.

---

## Phase 2: API Reachable Localmente em standalone-multi (F-01)

**Objective:** `helmfile sync -e standalone-multi` deixa `localhost:13001/api/v1/health → 200` sem trabalho extra.

### T2.1 — NodePort + IngressRoute localhost no chart loopzero-api

#### Objective
Adicionar `service.nodePort` opcional e segunda IngressRoute matching localhost no chart `infra/helm/loopzero-api/`. Standalone-multi overlay habilita ambos. Dev/prod mantém ClusterIP.

#### Evidence
F-01: `kubectl get svc loopzero-api → ClusterIP`. `docker port k3d-loopzero-mgmt-serverlb → 30001/tcp -> 0.0.0.0:13001` mas sem Service ocupando 30001. IngressRoute existing só casa `Host(\`api.dev.loopzero.example\`)`.

#### Files to edit
```
infra/helm/loopzero-api/values.yaml — adiciona service.nodePort (default null)
infra/helm/loopzero-api/values-standalone-multi.yaml — seta service.type=NodePort + nodePort=30001 + ingress.localhost=true
infra/helm/loopzero-api/templates/deployment.yaml — Service template ganha .nodePort field condicional
infra/helm/loopzero-api/templates/ingressroute.yaml — adiciona segunda Route condicional matching localhost/127.0.0.1/api.loopzero.local
infra/tests/load/regression_test.go — adiciona TestF01LoopZeroApiNodePortInStandaloneMulti
docs/runbooks/end-to-end-deploy-validation.md — atualiza Phase 2 com nova validação
```

#### Deep file dependency analysis
- `values.yaml`: campo `service.nodePort` (default `null`) + `ingress.localhost` (default `false`). Sem breaking change em dev/prod.
- `values-standalone-multi.yaml`: overlay com `service.type=NodePort, service.nodePort=30001, ingress.localhost=true`.
- `deployment.yaml` Service block (linhas 127-141 atual): adicionar `{{- if .Values.service.nodePort }}nodePort: {{ . }}{{- end }}` em ports.
- `ingressroute.yaml`: bloco condicional `{{- if .Values.ingress.localhost }}` adiciona Route `Host(\`localhost\`) || Host(\`127.0.0.1\`) || Host(\`api.loopzero.local\`)` no entry point `web` (HTTP só, sem TLS).

#### Deep Dives
- **Por que entry point `web` (HTTP) e não websecure (HTTPS)?** TLS para `localhost` requer cert auto-signed (chrome warning); HTTP em loopback é aceitável para dev.
- **NodePort 30001 conflita?** k3d-mgmt já tem NodePort 30001 mapeado (k3d config). Ainda não claimed por Service. Após T2.1, loopzero-api Service ocupa 30001 → `host:13001` funciona.
- **Edge case: cluster sem CRD Traefik IngressRoute** (mgmt cluster TEM mas nem todos perfis). Validação: helm template renderiza sem erro mesmo se ingress.localhost=false.
- **Backward compat:** dev/prod profiles mantêm `service.type=ClusterIP, ingress.localhost=false` por default.

#### Tasks
1. Editar `values.yaml`: adicionar `service.nodePort: null` e `ingress.localhost: false`.
2. Editar `values-standalone-multi.yaml`: setar `service.type=NodePort, service.nodePort=30001, ingress.localhost=true`.
3. Editar `templates/deployment.yaml` Service: adicionar `nodePort` condicional.
4. Editar `templates/ingressroute.yaml`: adicionar Route condicional para localhost.
5. Helm template + diff para validar.
6. Adicionar Go regression test.
7. Atualizar runbook.

#### TDD
```
RED:     TestF01LoopZeroApiNodePortInStandaloneMulti — verifica values-standalone-multi.yaml tem service.type=NodePort + nodePort=30001
RED:     TestF01LoopZeroApiHasLocalhostIngressRoute — verifica template renderiza Route com Host(`localhost`) quando ingress.localhost=true
RED:     TestF01DevProductionStillClusterIP — verifica dev/prod overlays não alteram service.type
GREEN:   Editar values + templates
REFACTOR: None expected
VERIFY:  helm template infra/helm/loopzero-api -f values.yaml -f values-standalone-multi.yaml | grep -A2 'kind: Service'
```

#### Acceptance Criteria
- [ ] `helm template ... -f values-standalone-multi.yaml` renderiza Service `type: NodePort, nodePort: 30001`.
- [ ] Mesmo render contém 2 Routes na IngressRoute (`api.{domain}` + `localhost`).
- [ ] `helm template ... -f values.yaml` (dev default) mantém ClusterIP, 1 Route.
- [ ] 3 Go regression tests novos PASS.
- [ ] Pass: kubeconform validation no manifest renderizado.

#### DoD
- [ ] Após `helmfile sync -e standalone-multi`, `curl http://localhost:13001/api/v1/health → 200`.
- [ ] `task dogfood:repro` reporta F-01 = FIXED.

---

### T2.2 — CLI default endpoint corrigido + smoke check

#### Objective
Garantir que `~/.loopzero/config.json` default em standalone-multi aponta para endpoint que funciona pós-T2.1. Adicionar smoke test no `loopzero platform install` que valida API reachable antes de declarar sucesso.

#### Evidence
Atual: `~/.loopzero/config.json` tem `endpoint: http://localhost:13001`. Após T2.1, endpoint funciona. Mas `loopzero platform install` precisa validar isso para detectar regressões.

#### Files to edit
```
cli/src/commands/platform.ts — install termina com smoke test do endpoint
cli/src/installer/health-checks.ts — adiciona checkApiReachable()
cli/tests/unit/installer/health-checks.test.ts — adiciona spec checkApiReachable
infra/helmfile/scripts/health-check.sh — adiciona smoke API health
```

#### Deep file dependency analysis
- `platform.ts` action `install`: depois de `helmfileSync`, chama `await healthChecks.checkApiReachable(endpoint, 30000)` com retry 30x@1s.
- `health-checks.ts`: nova função `checkApiReachable(endpoint, timeoutMs)` que faz GET `/api/v1/health` e retorna `{ok, latency, response}`.

#### Deep Dives
- **Retry strategy:** 30 tentativas × 1s = 30s timeout total. Adequado para Service/Pod ficar Ready após Helm install.
- **Falha graceful:** se smoke falhar, install ainda termina com exit 0 mas warning explícito + sugestão "Run `tilt up` if running in dev mode".

#### Tasks
1. Adicionar `checkApiReachable()` em `health-checks.ts`.
2. Wire no fim de `platformInstall()`.
3. Adicionar spec.
4. Atualizar `infra/helmfile/scripts/health-check.sh`.

#### TDD
```
RED:     test_checkApiReachable_returns_ok_when_health_200
RED:     test_checkApiReachable_retries_on_failure
RED:     test_checkApiReachable_emits_warning_after_30s_timeout
GREEN:   Implementar
REFACTOR: None
VERIFY:  cd cli && npm test -- health-checks
```

#### Acceptance Criteria
- [ ] `loopzero platform install -e standalone-multi` termina com "API reachable at http://localhost:13001 ✓".
- [ ] Em dev profile sem NodePort, install termina com warning informativo.
- [ ] Pass: /code-audit coverage health-checks.ts ≥ 90%.

#### DoD
- [ ] CLI test PASS.
- [ ] Smoke E2E: `task setup -e standalone-multi && loopzero whoami` retorna `{"authenticated":false}` (não SERVICE_UNAVAILABLE).

---

## Phase 3: Tenant Single Source of Truth (F-04)

**Objective:** Tenant CRs SEMPRE em `loopzero-system`. ValidatingAdmissionPolicy rejeita criação fora. Migration script consolida tenants existentes.

### T3.1 — API + tenant-controller usam `loopzero-system` para Tenant/Environment CRs

#### Objective
Auditar todo o código que cria Tenant/Environment CRs e garantir namespace fixo `loopzero-system`. Eliminar parâmetro de namespace exposto a callers.

#### Evidence
F-04: `kubectl get tenants -A` mostra `default/tn-e2etest` E `loopzero-system/tn-e2etest`. `api/internal/k8s/secret_k8s_manager.go:64` já usa `mgmtNamespace: "loopzero-system"` mas alguma outra rota cria em `default`.

#### Files to edit
```
api/internal/k8s/tenant_cr_manager.go — remover param namespace, hard-code "loopzero-system"
api/internal/k8s/environment_cr_manager.go — idem (se existir)
api/internal/services/tenant_orchestrator.go — passar a usar manager sem ns
operators/internal/controller/tenant_controller.go — watch só em loopzero-system
api/internal/k8s/tenant_cr_manager_test.go — atualizar expects
docs/cli-reference.md — documentar SoT
```

#### Deep file dependency analysis
- `tenant_cr_manager.go` (existing): tem `Namespace` config field; remover, hard-code constant.
- `tenant_orchestrator.go` (caller): para de passar `namespace` em chamadas.
- `tenant_controller.go` (operator): `mgr.GetClient().List(..., InNamespace("loopzero-system"))` em watches.

#### Deep Dives
- **Backward compat:** customers que tenham Tenant em outra ns precisam migrar (T3.3). API retorna 404 para tenants em ns errada — não silently retorna.
- **Why hard-code over config?** Constant é "law" do produto. Config faz parecer pluggable; tenant CR namespace é arquitetural.

#### Tasks
1. Grep `Namespace.*"default"` em api/internal/k8s.
2. Substituir por const `loopzeroSystemNamespace = "loopzero-system"`.
3. Atualizar callers.
4. Atualizar testes.
5. Smoke contra cluster local.

#### TDD
```
RED:     TestF04TenantCRAlwaysInLoopZeroSystem — fakeClient.Create chamado com ns=loopzero-system
RED:     TestF04EnvironmentCRAlwaysInLoopZeroSystem — idem
RED:     TestF04OperatorWatchesOnlyLoopZeroSystem — operator setup verifica InNamespace
GREEN:   Refactor
REFACTOR: extrair const para package-level
VERIFY:  cd api && go test ./internal/k8s/...
```

#### Acceptance Criteria
- [ ] `grep -rn 'Namespace.*"default"' api/internal/k8s` zero results para Tenant/Environment.
- [ ] All Go tests PASS.
- [ ] Pass: /code-audit complexity ≤ 10.

#### DoD
- [ ] Após `loopzero init` em tenant fresh, `kubectl get tenants -A` mostra apenas em `loopzero-system`.

---

### T3.2 — ValidatingAdmissionPolicy rejeita Tenant fora de `loopzero-system`

#### Objective
Adicionar VAP `loopzero-tenant-namespace-policy` que rejeita criação de Tenant/Environment CRs em qualquer namespace ≠ loopzero-system.

#### Evidence
F-04: defesa em profundidade — código garante via T3.1, VAP garante via API server.

#### Files to edit
```
infra/helm/admission-policies/templates/vap-tenant-namespace.yaml — (NEW) VAP + Binding
infra/helm/admission-policies/values.yaml — toggle tenantNamespaceLock (default true)
infra/tests/load/regression_test.go — TestF04VAPTenantNamespaceExists
```

#### Deep file dependency analysis
- VAP é cluster-scoped. Binding seleciona resources `loopzero.io/v1/{tenants,environments}` em `metadata.namespace != "loopzero-system"` → DENY.
- Sem dependência de cert-manager (VAP é nativo K8s 1.30+).

#### Deep Dives
- **CEL expression:**
  ```yaml
  validations:
    - expression: "object.metadata.namespace == 'loopzero-system'"
      message: "Tenant/Environment CRs must be created in 'loopzero-system' namespace (LoopZero PaaS SoT D3)"
      reason: Forbidden
  ```
- **Failure mode:** policyController vs ValidatingAdmissionPolicy — usa VAP nativo (sem dep externa).

#### Tasks
1. Criar `vap-tenant-namespace.yaml` chart template.
2. Adicionar toggle.
3. helm template + apply em k3d.
4. Tentar `kubectl create tenant test -n default` → DEVE falhar.

#### TDD
```
RED:     TestF04VAPTenantNamespaceExists — chart renderiza VAP
RED:     TestF04VAPCELExpressionCorrect — expression contém loopzero-system check
GREEN:   Implementar
VERIFY:  task validate:helm-charts
```

#### Acceptance Criteria
- [ ] `kubectl apply` de Tenant em `default` ns retorna `forbidden: ValidatingAdmissionPolicy`.
- [ ] Mesmo apply em `loopzero-system` SUCCESS.
- [ ] kubeconform PASS.

#### DoD
- [ ] Live test no k3d local: tentativa de duplicação rejeitada.

---

### T3.3 — Migration script consolida tenants existentes

#### Objective
`infra/scripts/migrate-tenant-namespace.sh` (NEW) idempotente: para cada Tenant em ns ≠ loopzero-system, copiar para loopzero-system preservando spec/finalizers, depois delete original. Mesmo p/ Environment.

#### Evidence
F-04 tem 2 tenants ativos pós-iter-12. Customers em produção podem ter o mesmo. Sem migration, T3.2 quebra deploys.

#### Files to edit
```
infra/scripts/migrate-tenant-namespace.sh — (NEW)
infra/tests/load/regression_test.go — TestF04MigrationScriptExists
docs/runbooks/tenant-namespace-migration.md — (NEW) procedure + rollback
Taskfile.yml — adiciona task migrate:tenants
```

#### Deep file dependency analysis
- Script bash idempotente: usa `kubectl get/apply/delete` com retries.
- Lógica: lista Tenants em todos ns, filtra `metadata.namespace != loopzero-system`, para cada um:
  1. `kubectl get tenant X -n default -o yaml > /tmp/X.yaml`
  2. Strip `metadata.{namespace,resourceVersion,uid,creationTimestamp}` (igual T17 backup pattern).
  3. `kubectl apply -f /tmp/X.yaml -n loopzero-system`
  4. Aguardar status.phase=Active.
  5. `kubectl delete tenant X -n default --wait`.

#### Deep Dives
- **Edge case:** Tenant tem finalizers; delete bloqueia. Script remove finalizers antes do delete (com confirmação `--force` flag).
- **Rollback:** backup antes de migrar em `~/.loopzero/backups/tenant-migration-<timestamp>/`.
- **Idempotência:** se Tenant já existe em loopzero-system com mesmo spec, skip migration (idempotent).

#### Tasks
1. Esqueleto bash com `set -euo pipefail`.
2. Função `list_misplaced_tenants()`.
3. Função `migrate_tenant(name, src_ns)`.
4. Backup antes.
5. Smoke contra cluster com tenant duplicado.

#### TDD
```
RED:     test_script_dry_run_lists_misplaced — bash test
RED:     test_script_migrates_tenant_preserves_spec
RED:     test_script_idempotent_on_already_migrated
RED:     test_script_writes_backup
GREEN:   Implementar
VERIFY:  bash infra/scripts/migrate-tenant-namespace.sh --dry-run
```

#### Acceptance Criteria
- [ ] Script chmod 755 + shellcheck-clean.
- [ ] `--dry-run` lista misplaced sem mudar nada.
- [ ] Após `task migrate:tenants`, `kubectl get tenants -A` mostra apenas loopzero-system.
- [ ] Pass: idempotência (rodar 2x = mesmo resultado).

#### DoD
- [ ] Migration script + runbook commited.
- [ ] Live test: tenant em default migra para loopzero-system, app continua serving HTTP 200.

---

## Phase 4: Operator Logs Limpos (F-09 + F-10)

**Objective:** zero "pull secret ensure failed" + zero "traces export rpc error" em standalone-multi.

### T4.1 — Fix concat `<ns>/<name>` em EnsurePullSecret (F-09)

#### Objective
`environment_rbac.go:143` faz concat literal `"loopzero-system/loopzero-system: ..."` quando deveria ser `<sourceNs>/<secretName>`. Bug está em construir o erro, não em código de busca — investigar verdadeiro chamador e fix path.

#### Evidence
F-09 evidence: `error: source pull secret loopzero-system/loopzero-system: Secret "loopzero-system" not found`. O secret name é "loopzero-system" (NS usado como NAME). Path errado.

#### Files to edit
```
operators/internal/controller/environment_rbac.go — fix concat + handle missing-secret-as-info
operators/internal/controller/environment_rbac_test.go — adicionar specs
```

#### Deep file dependency analysis
- `environment_rbac.go:92` loga `pull secret ensure failed (non-fatal)`; `:143` retorna error com path errado.
- Caller é `EnvironmentReconciler.Reconcile`; se source secret não existe, deveria fazer log INFO uma vez e seguir, não retry-loop ERROR.

#### Deep Dives
- **Root cause:** algum lugar passa `secretName=""` ou usa `namespace` como `name`. Investigar grep mais profundo.
- **Fix:** validar inputs ANTES de chamar EnsurePullSecret; se name vazio, skip silenciosamente com 1 log INFO + métrica `loopzero_env_pull_secret_skipped_total`.
- **Edge case:** standalone-multi não tem source pull secret (DO Registry). Em dev/prod, secret existe.

#### Tasks
1. Investigar `EnsurePullSecret` callers via grep.
2. Identificar onde `secretName` ou `secretNs` vem vazio.
3. Validar inputs early; log INFO + return nil se ausente.
4. Adicionar specs.
5. Smoke contra k3d.

#### TDD
```
RED:     TestF09EnsurePullSecretSkipsWhenNameEmpty — fake client, validate no error
RED:     TestF09EnsurePullSecretLogsInfoOnce — counter assertion
RED:     TestF09ErrorMessageContainsCorrectPath — quando secret real existe e busca falha
GREEN:   Validar inputs + skip path
REFACTOR: extrair pullSecretConfig{Ns, Name} struct
VERIFY:  cd operators && go test ./internal/controller/...
```

#### Acceptance Criteria
- [ ] Grep operator logs em standalone-multi por 5 min: zero "pull secret ensure failed".
- [ ] Existing dev/prod path mantém comportamento.
- [ ] Pass: /code-audit complexity ≤ 10.

#### DoD
- [ ] `kubectl logs -n loopzero-system -l app=tenant-controller --since=5m | grep "pull secret"` retorna vazio.

---

### T4.2 — OTel exporter opt-in (D5 / F-10)

#### Objective
`tracing.go:26` muda `DefaultEndpoint` para `""`. Tracing só ativa se env explícita.

#### Evidence
F-10: log "rpc error: code = Unavailable" a cada 30s.

#### Files to edit
```
operators/internal/tracing/tracing.go — DefaultEndpoint = ""
operators/internal/tracing/tracing_test.go — adicionar specs
infra/helm/tenant-controller/values.yaml — comentar OTEL_EXPORTER_OTLP_ENDPOINT como opt-in
infra/helm/tenant-controller/values-dev.yaml (NEW se ausente) — seta endpoint dev
infra/helm/tenant-controller/values-prod.yaml — idem
```

#### Deep file dependency analysis
- `tracing.go:51`: `endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")`; se vazio, retornar noop tracer (não criar exporter).
- Helm values: dev/prod overlays setam endpoint explícito; standalone-multi não.

#### Deep Dives
- **Noop tracer pattern:** OTel SDK tem `NoopTracerProvider`; usar quando endpoint vazio.
- **Edge case:** se variável estiver setada como string vazia (acidental), também noop.

#### Tasks
1. Editar tracing.go default.
2. Adicionar early-return noop em setupTracing().
3. Atualizar helm values.
4. Specs.

#### TDD
```
RED:     TestF10TracingNoopWhenEndpointEmpty
RED:     TestF10TracingActiveWhenEndpointSet
GREEN:   Implementar
VERIFY:  cd operators && go test ./internal/tracing/...
```

#### Acceptance Criteria
- [ ] Standalone-multi sem env var: zero logs "traces export".
- [ ] Dev/prod com env: tracing ativo (smoke via Tempo UI).
- [ ] Pass: /code-audit coverage tracing.go ≥ 90%.

#### DoD
- [ ] `kubectl logs -n loopzero-system -l app=tenant-controller --since=5m | grep "traces export"` vazio em standalone-multi.

---

## Phase 5: ArgoCD UI Coerente + API Endpoints (F-07/F-08/F-11/F-12/F-13)

**Objective:** ArgoCD App consolida em `Synced/Healthy`; `/api/v1/version` 200; `/metrics` documentado; argo Workflow pods limpos.

### T5.1 — ArgoCD application-controller replicas=1 + Lua health checks (F-11)

#### Objective
Em standalone-multi, `controller.replicas: 1` evita sharding race. Lua scripts dão health check para Traefik/cert-manager CRDs.

#### Evidence
F-11: 3 réplicas + restart causa "Ignoring cluster" race. Health="" para IngressRoute/Middleware.

#### Files to edit
```
infra/helm/argocd/values-standalone-multi.yaml — (NEW overlay) controller.replicas=1
infra/helm/argocd/values.yaml — adiciona configs.cm.resource.customizations.health (Lua scripts)
infra/argocd-bootstrap/configmap-resource-customizations.yaml — (NEW) ConfigMap argocd-cm com Lua
infra/helmfile/mgmt.helmfile.yaml.gotmpl — argocd em standalone-multi usa overlay
infra/tests/load/regression_test.go — TestF11ArgocdLuaHealthChecks + TestF11ReplicasOneInStandalone
```

#### Deep file dependency analysis
- ArgoCD chart suporta `configs.cm.resource.customizations.health.<group>_<kind>` para Lua.
- Lua simples para IngressRoute: `hs = {}; hs.status = "Healthy"; hs.message = "IngressRoute applied"; return hs`. Para Certificate: parse `.status.conditions` para Ready=True.

#### Deep Dives
- **Lua para Certificate:**
  ```lua
  hs = { status = "Progressing", message = "Issuing" }
  if obj.status ~= nil and obj.status.conditions ~= nil then
    for _, c in ipairs(obj.status.conditions) do
      if c.type == "Ready" and c.status == "True" then
        hs.status = "Healthy"
        hs.message = "Certificate ready"
      end
    end
  end
  return hs
  ```
- **Standalone-multi profile:** Certificate fica `Progressing` (no real LE). Lua retorna `Progressing` mas com mensagem clara — UI não fica `Unknown`.

#### Tasks
1. Criar values-standalone-multi.yaml overlay.
2. Adicionar Lua scripts em values.yaml configs.cm.
3. Helm template + diff.
4. Apply em k3d.
5. Trigger refresh em App `tn-e2etest-production`.
6. Verificar `kubectl get app -o jsonpath='{.status.health.status}'` = `Healthy` ou `Progressing` (não `Unknown`).

#### TDD
```
RED:     TestF11ArgocdLuaHealthChecksDefined — values.yaml contém customizations.health
RED:     TestF11ReplicasOneInStandalone — overlay seta controller.replicas=1
RED:     TestF11LuaScriptValidForIngressRoute — sintaxe Lua simples
GREEN:   Implementar
VERIFY:  task validate:helm-charts
```

#### Acceptance Criteria
- [ ] App `tn-e2etest-production` consolida em `Synced/Progressing` (com message claro) ou `Synced/Healthy` em <5min.
- [ ] Zero "Ignoring cluster" warnings nos logs do controller.
- [ ] Pass: kubeconform.

#### DoD
- [ ] Smoke: deploy → ArgoCD UI mostra status coerente.

---

### T5.2 — `/api/v1/version` endpoint (F-07)

#### Objective
Adicionar GET `/api/v1/version` sem auth retornando build metadata.

#### Files to edit
```
api/internal/routes/version.go — (NEW) handler
api/internal/routes/router.go — registra rota sem middleware auth
api/cmd/main.go — injeta build flags via -ldflags
api/internal/routes/version_test.go — (NEW) ~3 specs
```

#### Deep file dependency analysis
- `router.go`: adiciona `r.Get("/api/v1/version", versionHandler)` ANTES do middleware auth.

#### Deep Dives
- **Build flags:** `go build -ldflags "-X main.Version=v0.1.0 -X main.Commit=abc -X main.BuildDate=..."`.
- **Schema:** `{version, commit, build_date, go_version, platform}`.

#### Tasks
1. Criar version.go.
2. Wire em router.
3. Atualizar Dockerfile com ldflags.
4. Specs.

#### TDD
```
RED:     TestVersionEndpoint_Returns200_NoAuth
RED:     TestVersionEndpoint_SchemaCorrect
RED:     TestVersionEndpoint_HandlesUnsetBuildFlags
GREEN:   Implementar
VERIFY:  cd api && go test ./internal/routes/...
```

#### Acceptance Criteria
- [ ] `curl http://localhost:13001/api/v1/version` → 200 com JSON schema correto.
- [ ] Sem auth required.
- [ ] Pass: /code-audit complexity ≤ 5.

#### DoD
- [ ] `task dogfood:repro` reporta F-07 = FIXED.

---

### T5.3 — `/metrics` documentar como interno + ServiceMonitor docs (F-08)

#### Objective
F-08 não é bug, é gap de documentação. Adicionar nota em `docs/runbooks/observability.md` + smoke check que ServiceMonitor está scrapeando :9090.

#### Files to edit
```
docs/runbooks/observability.md — (NEW se ausente) seção "LoopZero metrics endpoints"
infra/helm/loopzero-api/templates/servicemonitor.yaml — (NEW se ausente) ServiceMonitor para :9090
```

#### Deep file dependency analysis
- ServiceMonitor já pode existir (verificar). Se não, criar para Prometheus operator scrape :9090.

#### Tasks
1. Validar ServiceMonitor existe.
2. Criar runbook section.

#### TDD
```
RED:     TestF08ServiceMonitorScrapesMetricsPort — chart renderiza ServiceMonitor com endpoint :9090
GREEN:   Adicionar SM se ausente
VERIFY:  helm template ... | kubeconform
```

#### Acceptance Criteria
- [ ] Runbook documenta endpoints internos vs externos.
- [ ] ServiceMonitor existe e scrape funciona.

#### DoD
- [ ] `task dogfood:repro` reporta F-08 = FIXED ou DOCUMENTED.

---

### T5.4 — ArgoCD admin secret discovery em standalone (F-12)

#### Objective
API `/health/deep` consegue autenticar contra ArgoCD em standalone-multi.

#### Evidence
F-12: `argocd: auth_failed`. Provavelmente API procura secret em ns errada ou nome diferente em standalone vs DOKS.

#### Files to edit
```
api/internal/health/deep.go (ou similar) — fix discovery path
api/internal/health/deep_test.go — adicionar specs
infra/helm/loopzero-api/values-standalone-multi.yaml — variável ARGOCD_AUTH_TOKEN se necessário
```

#### Tasks
1. Investigar como API autentica contra ArgoCD.
2. Padronizar via Secret `loopzero-argocd-token` em ns loopzero-system.
3. Helmfile script gera token via `argocd account generate-token` e cria Secret.
4. Specs.

#### TDD
```
RED:     TestF12HealthDeepArgocdAuth_StandaloneMulti
GREEN:   Fix discovery
VERIFY:  curl /api/v1/health/deep → status=ok
```

#### Acceptance Criteria
- [ ] `/api/v1/health/deep` retorna `status: ok` HTTP 200 em standalone-multi.

#### DoD
- [ ] Smoke pós-helmfile sync: `curl /api/v1/health/deep | jq .status` = `"ok"`.

---

### T5.5 — Argo Workflow TTL em pods de step com Error (F-13)

#### Objective
Garantir que Workflow ttlStrategy se aplica também aos pods de steps em `Error` (não apenas top-level Workflow).

#### Evidence
F-13: 4 pods Error há 1.5-2h.

#### Files to edit
```
api/internal/argo/workflow_builder.go — verificar SecondsAfterFailure também aplicado a step pods
charts/loopzero-build-cwts/templates/*.yaml — adicionar podGC.strategy=OnPodCompletion
infra/tests/load/regression_test.go — TestF13PodGCStrategyDefined
```

#### Deep file dependency analysis
- Argo Workflow `spec.podGC.strategy=OnPodCompletion` deleta pod imediatamente após complete (Error ou Success).

#### Tasks
1. Adicionar podGC em workflow_builder.go.
2. Smoke + verify pods removidos.

#### TDD
```
RED:     TestF13WorkflowHasPodGCOnPodCompletion
GREEN:   Add podGC field
VERIFY:  cd api && go test ./internal/argo/...
```

#### Acceptance Criteria
- [ ] Workflows novos têm podGC.strategy=OnPodCompletion.
- [ ] Pods em Error removidos em <60s.

#### DoD
- [ ] `kubectl get pods -n argo | grep Error` retorna vazio após 60s pós-complete.

---

## Phase 6: Dogfood Loop H1 Automatizado

**Objective:** validar todo o caminho do usuário via teste Go gated em `LOOPZERO_E2E_DOGFOOD=1`.

### T6.1 — `infra/tests/e2e/dogfood_loop_test.go`

#### Objective
Test Go orquestra: `loopzero login → init → deploy → status → rollback → delete` com `LOOPZERO_TOKEN` real, valida Prova de Vida + Prova de Morte.

#### Files to edit
```
infra/tests/e2e/dogfood_loop_test.go — (NEW)
infra/tests/e2e/helpers.go — (NEW) spawnLoopZero, kubectlGet, waitForHTTP200
.github/workflows/ci.yml — gate LOOPZERO_E2E_DOGFOOD em job opcional
docs/runbooks/dogfood-loop-h1.md — (NEW) procedure
```

#### Deep file dependency analysis
- Teste exige cluster k3d up + GH_TOKEN válido. Gated em env var → skip por default em CI.

#### Deep Dives
- **Steps:**
  1. loopzero login --token $LOOPZERO_TOKEN
  2. loopzero whoami → exit 0
  3. cd /tmp/dogfood-test && git init + simple node app
  4. loopzero init --yes
  5. loopzero deploy
  6. wait HTTP 200 from URL
  7. PV checks (pod, sa, securityContext)
  8. loopzero status → ACTIVE
  9. (segundo deploy + loopzero rollback) → PV verifica versão antiga
  10. loopzero project delete --yes
  11. PM checks (pod gone, repo gone)
- **Cleanup:** test sempre roda `loopzero project delete` no defer.

#### Tasks
1. Helper functions.
2. TestDogfoodLoopH1.
3. Runbook.
4. CI gate.

#### TDD
```
RED:     TestDogfoodLoopH1 — gated, requer LOOPZERO_E2E_DOGFOOD=1 + LOOPZERO_TOKEN
GREEN:   Implementar test orchestrator
VERIFY:  LOOPZERO_E2E_DOGFOOD=1 LOOPZERO_TOKEN=... go test ./infra/tests/e2e/...
```

#### Acceptance Criteria
- [ ] Test PASS em <30min.
- [ ] Cobre 6 jornadas (login, init, deploy, status, rollback, delete).
- [ ] PV + PM verificadas.
- [ ] CI job opcional configurado.

#### DoD
- [ ] Test PASS local com LOOPZERO_TOKEN real.
- [ ] Documentado em CLAUDE.md como gate de release.

---

## Coverage Matrix

| # | Achado | Severidade | Task(s) | Resolução |
|---|---|---|---|---|
| F-01 | API inalcançável CLI | S0 | T2.1, T2.2 | NodePort + IngressRoute localhost no chart loopzero-api |
| F-02 | Exit code drift sistêmico | S1 | T1.1, T1.2 | runCommand wrapper + 36 commands migrados + contract test |
| F-03 | Comando inexistente exit 0 | S2 | T1.2 | Commander .exitOverride() |
| F-04 | Tenants duplicados ns | S1 | T3.1, T3.2, T3.3 | Hard-code loopzero-system + VAP + migration script |
| F-05 | Init silently-success | S1 | T1.3 | preflightAuth + --offline flag |
| F-06 | Logout cancelled msg | S3 | T1.2 | Migrado em runCommand pass + msg corrigida |
| F-07 | /api/v1/version 404 | S2 | T5.2 | Novo endpoint público |
| F-08 | /metrics 404 externo | S3 | T5.3 | Documentado + ServiceMonitor |
| F-09 | Pull secret loop | S2 | T4.1 | Validar inputs + skip path |
| F-10 | Traces RPC error | S2 | T4.2 | Default endpoint vazio (opt-in) |
| F-11 | ArgoCD Unknown stuck | S2 | T5.1 | replicas=1 + Lua health checks |
| F-12 | argocd auth_failed deep | S2 | T5.4 | Token discovery padronizado |
| F-13 | Argo pods Error | S3 | T5.5 | podGC.strategy=OnPodCompletion |

**Coverage: 13/13 achados (100%)**

## Global Definition of Done

- [ ] All 7 phases completados.
- [ ] `task dogfood:repro` reporta TODOS 13 findings = FIXED.
- [ ] CLI test suite PASS (cobertura inclui contract test 36-cmd matrix).
- [ ] API + operator Go tests PASS.
- [ ] kubeconform PASS em todos manifests novos.
- [ ] `task validate:layered-bootstrap` PASS.
- [ ] H1 dogfood loop test PASS local com LOOPZERO_TOKEN real.
- [ ] CHANGELOG.md atualizado com 13 finding fixes.
- [ ] `docs/runbooks/bootstrap-regression-matrix.md` expandido para 116 entries (103 + 13).
- [ ] CI gate adicionado para dogfood-loop-h1 (opcional, manual trigger).
- [ ] Customer self-hosting validado: `task setup -e standalone-multi && loopzero whoami` funciona out-of-the-box.
- [ ] ArgoCD UI mostra App `Synced/Healthy` para deploys static frontend e backend.
- [ ] Operator logs limpos em standalone-multi (zero loop errors).
