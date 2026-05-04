# Plan: Pre-Launch Gaps Closure

> **Version 1.3 (2026-05-04 ASYNC AMENDMENT-3 per ADR-0022)** — Resolve inconsistência entre v1.2 (P2.3 deferred) e P6 dogfood ("Application CR shows spec.scaling"). Como P2.3 (operator reconcile + CRD bump) foi formalmente deferido em MEET-066, o critério "Application CR shows spec.scaling" também fica deferido junto — ele depende do CRD bump que não aconteceu. P6 v1.3 criterion final: **"scale config persists in DB"** (achieved E2E 2026-05-04: HTTP 200 + DB row + idempotency + autoscaling shape verified live). Substitui v1.2.

> **Version 1.2 (2026-05-03 ASYNC AMENDMENT-2 per ADR-0022)** — Adiciona deferral de **Phase 2.3** (operator Application controller reconcile `spec.scaling`) via MEET-2026-05-03-066-ASYNC-AMENDMENT. Razão: trabalho substantivo (CRD bump + reconcile + 4 envtest specs) com risk surface alto; melhor em PR separado (`docs/plans/scale-actuation-plan.md` futuro). Phase 6 dogfood criteria ajustado — "scale takes effect" removido; "scale config persists in DB + Application CR shows spec.scaling" mantido. Iter1 in-scope: P0, P1, P2.1+2.2+2.4, P5, P6 (adjusted).

> **Version 1.1 (2026-05-03 ASYNC AMENDMENT per ADR-0022)** — Fecha os 5 gaps + 2 concerns. Phases 3 (cosign cutover) + 4 (CI cron evidence) **operator-deferred per MEET-2026-05-03-065-ASYNC-AMENDMENT** porque exigem 22+ dias wall-clock + gh CLI auth não disponível em loop. Phase 0+1+2+5+6 ficam in-scope para loop completion. Plan completion criteria revisado: P3+P4 marked deferred, não blocking loop. Outros critérios v1.0 inalterados.

> **Version 1.0 (original)** — Fecha os 5 gaps identificados na revisão pós-iter1 do `infra-sota-hardening-plan.md` que bloqueiam launch real para customers pagos. Em 1 frase: transforma "claims do CLAUDE.md" em "evidência empírica" — `loopzero scale` deixa de ser stub, cosign keyless flipa para enforce em produção, CI crons produzem primeira evidência GREEN, parallel test flakes são eliminados na raiz, divergências local↔prod ficam canonicas em matriz auditável. Resultado: status de "98% confidence launchable" deixa de ser autodeclarado e passa a ter contraprova mensurável.

## Context

A iter1 do `infra-sota-hardening-plan.md` (2026-05-03) entregou 18/18 tasks com governance amendments registradas (3 meeting records + 4 SOTA gates novos + 65 helm-unittests + 14 schemas + Taskfile 2348→150 linhas + cosign keyless code). Mas a revisão honesta pós-entrega identificou 5 áreas onde "task done" não significa "produção pronta":

1. **`loopzero scale` é stub documentado** — retorna `202 acknowledged` mas não persiste em DB nem aplica HPA. Pro tier ($29/mo) e Team ($79/mo) prometem scaling como feature; CLAUDE.md confirma "Not built: SSO/SAML, `loopzero scale` DB persistence (stub returns 202 acknowledged — use loopzero.yaml + loopzero deploy)". Primeiro customer pago descobre o gap → churn.

2. **Cosign key-based ainda é o root-of-trust em produção** — `helm-charts.yml` passou a fazer dual-signing keyless+key-based em iter1, mas `policy-controller` continua em modo `warn` com `static.action: pass` (audit-only). Se `COSIGN_KEY` secret for comprometido HOJE, supply chain inteiro confia em algo que pode ser substituído sem assinatura criptográfica nova. Cutover runbook (`docs/runbooks/cosign-keyless-cutover.md`) é aspiracional — não foi exercido.

3. **CI crons armados mas não rodaram** — `.github/workflows/cell2-soaks.yml` (Sunday 02:00 UTC) e `platform-health-7day-watch.yml` (hourly) foram criados em iter T5.2 do `fix-all-platform-issues-plan.md`. Maturity 4.0 gate (CLAUDE.md D7) exige "first CI Sunday cron GREEN producing real workflow run evidence". Hoje não há nenhuma run. Maturidade 3.9 → 4.0 fica bloqueada por evidência não coletada.

4. **CLI tests flakes em paralelo** — `npm test` paralelo fail 41/1243; mesmo set passa 1243/1243 com `--runInBand`. Sintoma: testes compartilham state global (portas, filesystem, env). Implicação: CI fail é ruído, devs aprendem a ignorar, regressão real se esconde. Quality bar de CI degrada com tempo se não corrigido na raiz.

5. **Rule 10 ("local equals production") parcialmente quebrado** — divergências reais hoje: (a) cert-manager local não emite TLS para `*.loopzero.local` (sem DNS público para ACME challenge); (b) cell-2 local roda em portas 8091/8444 mas DNS local resolve para 8090/8443 — probe nunca alcança cell-2 localmente; (c) cluster local não tem OIDC issuer aceito por Fulcio público (gate de cosign keyless Track-2). Cada divergência vive em runbook separado sem matriz consolidada. Risco: regressão escapa de dogfood porque caminho local é diferente de produção.

**Concerns secundárias (incluídas no plano):**

6. **Padrão recorrente "implemented but not wired"** — 4+ instâncias históricas (COMP-001 PreviewController, ARCH-001 MinIO CB, COMP-008 PR webhook, recente `SetupEnvironmentController` em iter1). Test `TestSetupControllersWiresAllControllers` cata DEPOIS — pattern continua. Workflow permite merge antes de wiring completo.

7. **Plan self-amendment durante execução** — iter1 passou de v1.0 para v1.1 com escopo ajustado (Track-2 deferido, Taskfile DoD redefinido, 11 actions com pragma TODO). Legítimo sob pre-release governance, mas estabelece precedente perigoso: "se não dá, emendamos o plano." Sem critério explícito, planos perdem força como contrato.

## Objective

**Done = sistema empiricamente 100% funcional para Starter + Pro tiers, com evidência mensurável (não autodeclarada).**

Metas mensuráveis:

- `loopzero scale --replicas N` persiste em DB + reconcilia HPA ≤ 60s; `loopzero status --json` mostra réplicas atuais idênticas ao requested
- `policy-controller` em modo `enforce` para keyless authority em ambiente dev; warn-mode para staging+prod até 7-day soak GREEN
- 1 run de cada CI cron (`cell2-soaks.yml` Stage 1 + `platform-health-7day-watch.yml`) GREEN com artefato persistido
- 0 fails em `npm test` paralelo (CI default mode); root cause de cada flake corrigido (não só `--runInBand` workaround)
- Matriz de divergências local↔prod canonical em `docs/runbooks/local-prod-divergence-matrix.md` com CI gate enforcing integrity
- `TestSetupControllersWiresAllControllers` extendido para detectar `Setup*` (não só `Setup*Controller`); knip orphan-exports gate em CI para TS
- Plan amendment policy formalizada: amendments durante execução requerem novo MEET record OR explicit ADR antes do v1.x bump

## ADRs

### D1 — `loopzero scale` é feature contratual de Pro/Team tier; stub é dívida pre-launch

**Decision:** Implementar persistência DB + reconciliação HPA real. Schema: nova tabela `app_scaling_config` (tenant_id, project_id, environment, app_name, min_replicas, max_replicas, target_cpu_pct, scale_to_zero, updated_at). API handler grava + emite `Application` CR update. Operator reconcilia CR → ScaledObject (KEDA) ou Deployment.replicas.

**Rationale:** Workaround atual (editar `loopzero.yaml` + redeploy) força rebuild + rollout completo de 3-5min para mudar 1 número. Mata UX de auto-scaling. Concorrentes (Vercel, Railway, Fly) têm scale instantâneo. Manter stub viola Plan caps documentados (Pro=5, Team=10) — cap está enforced mas action não.

**Consequences:** Habilita Pro/Team revenue real. Adiciona migração SQL forward-only. Operator ganha responsabilidade de scaling decision. Conflito potencial com KEDA HTTP add-on que também escala — resolver via priority chain (manual scale > HPA > KEDA HTTP).

### D2 — Cosign keyless cutover faseado; HALT criteria pre-defined

**Decision:** Seguir runbook 3-fase de `docs/runbooks/cosign-keyless-cutover.md` — Phase 1 enforce em dev, observe 7 dias; Phase 2 staging enforce; Phase 3 prod enforce + key-based removal + COSIGN_KEY revogação. HALT criteria: qualquer 5xx admission policy em production = revert para warn imediato.

**Rationale:** Dual-signing já está ativo desde iter1 — flip enforce é mecânico. Risco real é admission webhook latency em produção. SRE engineer veto authority de MEET-B exige p95 ≤ 2s antes de flip. Sem cutover, key-based continua sendo SPoF criptográfico.

**Consequences:** Elimina key management toil + adiciona Rekor transparency log. Webhook latency monitorada via `policy_controller_verify_duration_seconds` métrica. Rollback path: flip mode=warn via ArgoCD sync.

### D3 — CI cron evidence-only iteration: trigger workflow_dispatch para coletar primeira evidência

**Decision:** Disparar manualmente `cell2-soaks.yml` (Stage 1, ~75min) + `platform-health-7day-watch.yml` (1h cycle) via `gh workflow run` antes da próxima Sunday natural. Captura primeira run GREEN sem aguardar 168h naturais.

**Rationale:** Maturity 4.0 gate é evidence-driven. Aguardar Sunday natural + 7-day streak = 7-14 dias de espera passiva. Ativação manual coleta a evidência que falta hoje. Subsequent natural runs continuam regularmente — evidência só precisa ser primeira-prova.

**Consequences:** ~$2-5 cost (DOKS + cell-2 ops). Issue auto-criada em fail conforme workflow design. Habilita marcar maturity 4.0 candidate após primeiro Sunday natural confirmar.

### D4 — Parallel test flakes têm root-cause fix; `--runInBand` proibido como workaround permanente

**Decision:** Identificar via `jest --logHeapUsage --detectOpenHandles` quais testes mantêm state global (portas, FS, env vars). Refactor para isolamento (TempDir per test, port=0 dynamic allocation, env var snapshot+restore). CI continua paralelo padrão. `--runInBand` aceito APENAS em comando local de debug.

**Rationale:** `--runInBand` em CI custa 4-5x mais tempo (72s → 348s). Time-to-CI-feedback é métrica crítica. Root cause também previne futuras regressões — tests futuros herdam isolamento corretamente.

**Consequences:** Adiciona `port=0` + `socket.address().port` pattern em mock-api-server. Adiciona `tmpDir = await mkdtemp(prefix)` em fixture loader. Documentado em test isolation guide.

### D5 — Matriz de divergência local↔prod é canonical; CI gate enforcing

**Decision:** Criar `docs/runbooks/local-prod-divergence-matrix.md` enumerando cada divergência aceita (TLS path, cell-2 ports, OIDC issuer, etc.) com colunas: Aspecto / Local / Produção / Reason / Closure plan / Owner / Horizon. CI gate em `infra-gates.yml` valida que toda referência a divergência em código (`// LOCAL ONLY`, `# DEV path`) tem entry correspondente na matriz.

**Rationale:** Hoje as divergências vivem em comments scattered (cosign.go:93 menciona OIDC; CLAUDE.md menciona TLS; cell2-canary-stage1 menciona port). Sem matriz consolidada, regression detectada localmente = falsa segurança ("ah, local sempre faz isso"). Matriz força "intentional divergence" vs "accidental drift".

**Consequences:** Cada nova divergência aceita requer PR explícito atualizando matriz. CI fails se code grep encontra "LOCAL ONLY" sem matriz entry. Trade-off: gate-overhead vs auditability.

### D6 — Plan amendment policy: amendments em loop requerem MEET record + ADR

**Decision:** `.claude/rules/pre-release-governance.md` ganha seção "Plan amendment". Amendment durante execução de plano (i.e., scope reduction, DoD redefinition, task deferral) só é válida se: (a) novo MEET record é criado com Sponsor + Challenger; (b) plan version bump é registrado em ADR file; (c) amendment é commitada em PR separado antes da implementação que dela depende.

**Rationale:** Iter1 amended plan v1.0→v1.1 durante execução com 3 amendments (Track-2 deferred, Taskfile DoD redefined, 11 actions pragma TODO). Cada uma teve justificativa, mas nenhuma teve ADR isolada. Sem regra explícita, amendments futuras viram "self-grant" — agente decide unilateralmente que escopo mudou. Regra força contraditório.

**Consequences:** Amendments custam mais governance overhead. Mas plans recuperam força como contrato — "DONE" significa "DONE per latest ADR-approved version". Ralph loops + autonomous loops devem invocar `meeting-orchestrator` para amendments.

### D7 — "Implemented but not wired" gate amplificado

**Decision:** Estender `TestSetupControllersWiresAllControllers` para cobrir todos os exported `Setup*` (não só `Setup*Controller` — pega webhooks, reconcilers, etc.). Adicionar `knip` plugin no CI TypeScript para detectar orphan exports em `cli/`, `infra/pulumi/`. Adicionar `deadcode` Go check em CI gate (já parcial em Taskfile).

**Rationale:** 4 instâncias históricas + 1 quase-miss em iter1 (reusable workflows que viraram dead-code para step-level use case) = pattern não ficou domado pelo gate atual. Amplificar cobertura previne classe inteira.

**Consequences:** Mais false-positives iniciais (test+infra exports legítimos serão flagged). Mitigation: explicit allowlist em `.knip.json` / `deadcode-allowlist.txt` com justificativa por entrada.

## Dependency Graph

```
Phase 0 (Governance) ──▶ todas as fases
       │
       ├──▶ Phase 1 (CI reliability foundation) ──▶ Phase 4 (cron evidence)
       │         │
       │         ├─ T1.1 parallel flake root-cause
       │         ├─ T1.2 wired-gate amplification
       │         └─ T1.3 knip orphan-exports
       │
       ├──▶ Phase 2 (Product completeness) ──▶ Phase 6 (dogfood)
       │         │
       │         ├─ T2.1 scale DB schema + migration
       │         ├─ T2.2 scale API handler real
       │         ├─ T2.3 operator HPA reconciliation
       │         └─ T2.4 CLI scale --json contract
       │
       ├──▶ Phase 3 (Supply chain hardening) ──▶ Phase 6 (dogfood)
       │         │
       │         ├─ T3.1 dev cluster keyless enforce flip
       │         ├─ T3.2 staging keyless enforce (after dev 7-day GREEN)
       │         ├─ T3.3 prod keyless enforce + key-based removal
       │         └─ T3.4 COSIGN_KEY revoke
       │
       ├──▶ Phase 4 (Soak evidence) ──▶ Phase 6 (dogfood)
       │         │
       │         ├─ T4.1 manual cell2-soaks Stage 1 trigger
       │         ├─ T4.2 manual platform-health 24h trigger
       │         └─ T4.3 evidence artifacts + closure docs
       │
       ├──▶ Phase 5 (Parity matrix) ──▶ Phase 6 (dogfood)
       │         │
       │         ├─ T5.1 matrix doc canonical
       │         ├─ T5.2 CI gate matrix integrity
       │         └─ T5.3 cell-2 local probe routing fix
       │
       └──▶ Phase 6 (Dogfood gate) — FINAL
```

**Paralelismo:** Phase 1, 2, 3, 4, 5 são independentes após Phase 0. Phase 6 (dogfood) é fechamento sequencial de tudo.

**Sequenciais obrigatórias:** Phase 0 antes de qualquer código. Phase 3.2 só após Phase 3.1 GREEN 7 dias. Phase 3.3 só após Phase 3.2 GREEN 7 dias. Phase 3.4 só após Phase 3.3 GREEN 24h.

---

## Phase 0: Governance

**Objective:** Codificar amendment policy + extender wired-gate em CI antes de qualquer trabalho.

### T0.1 — Plan amendment policy em pre-release-governance.md

#### Objective
Adicionar seção "Plan Amendment" que define quando + como planos podem ser emendados durante execução.

#### Evidence
Iter1 do `infra-sota-hardening-plan.md` v1.0→v1.1 fez 3 amendments (Track-2 deferred, Taskfile DoD redefinido, 11 actions pragma TODO) sem MEET record ou ADR explícita. Reviewer (eu) auto-emendou. Pattern perigoso documentado em concern #5 da revisão pós-iter1.

#### Files to edit
```
.claude/rules/pre-release-governance.md — adicionar seção "Plan Amendment Policy"
docs/meetings/adr/0022-plan-amendment-policy.md (NEW) — formaliza decisão
```

#### Deep file dependency analysis
- `pre-release-governance.md` é regra ativa em CLAUDE.md project instructions; mudanças aqui afetam todos agentes futuros.
- ADR 0022 é registro auditável da decisão. Naming convention: 0021 já usado, 0022 next.

#### Deep Dives

**Amendment levels:**

| Type | Trigger | Approval required |
|---|---|---|
| **Trivial** | Typo, comment, doc clarification | None — direct edit |
| **Scope refinement** | Task split into sub-tasks; same DoD | New MEET record (~15min) |
| **DoD redefinition** | Acceptance criteria changes | MEET + new ADR + version bump |
| **Task deferral** | Task moved to future plan | MEET + new ADR + ADR explica BLOCKER |
| **Plan abandon** | Plan declared infeasible | Full T3 meeting + Sponsor revoke |

**Workflow:**
1. Agent identifies amendment need during execution.
2. Agent STOPS implementation related to amendment.
3. Agent invokes `meeting-orchestrator` with amendment context.
4. Meeting produces MEET record + ADR (for DoD/deferral types).
5. Plan version bumped (v1.0 → v1.1) in commit.
6. Implementation resumes with amended scope.

#### Tasks
1. Ler `.claude/rules/pre-release-governance.md` atual; identificar onde inserir nova seção.
2. Escrever ADR-0022-plan-amendment-policy.md com Decision/Rationale/Consequences.
3. Adicionar seção "Plan Amendment" em pre-release-governance.md cross-referenciando ADR-0022.
4. Atualizar `infra-sota-hardening-plan.md` retroativamente — anotar v1.1 amendments como "ADR-0022 retroactively applied".

#### TDD
```
RED:    Procurar referência a "amendment" em pre-release-governance.md → 0 hits (gap presente)
GREEN:  Adicionar seção; grep "## Plan Amendment" → 1 hit
REFACTOR: None expected
VERIFY: grep -c "amendment" .claude/rules/pre-release-governance.md ≥ 5 (substantive coverage)
```

#### Acceptance Criteria
- [ ] ADR-0022 criada em `docs/meetings/adr/0022-plan-amendment-policy.md`
- [ ] Seção "Plan Amendment" em `.claude/rules/pre-release-governance.md`
- [ ] Tabela de amendment levels documentada
- [ ] Workflow steps documentados
- [ ] Cross-referência entre ADR + rule file
- [ ] Pass: `/code-audit` review (formato consistente com ADRs existentes)

#### DoD
- [ ] Doc commitada
- [ ] CHANGELOG.md `[Unreleased]/Added`: "Plan Amendment Policy formalizada (ADR-0022)"

---

### T0.2 — Extender TestSetupControllersWiresAllControllers para Setup*

#### Objective
Trocar regex `Setup*Controller` por `Setup*` no test que detecta "implemented but not wired", capturando webhooks, reconcilers, etc.

#### Evidence
4 instâncias históricas (CHANGELOG: COMP-001/ARCH-001/COMP-008) + iter1 catch (`SetupEnvironmentController`). Pattern continua porque escopo do teste é estreito.

#### Files to edit
```
operators/cmd/registration_test.go — regex update
operators/internal/controller/_doc.go (NEW se necessário) — allowlist para exports legítimos
```

#### Deep file dependency analysis
- `registration_test.go:60` (`exportedSetupControllerFuncs`) e `:90` (`wiredSetupControllerCalls`) ambas filtram por `HasSuffix("Controller")`.
- Mudar para `HasPrefix("Setup")` apenas vai capturar funções como `SetupCacheReader`, `SetupMetricsServer` etc. Precisa allowlist.

#### Deep Dives
**Allowlist pattern:**
- Entradas em comentário no test file ou em arquivo separado `cmd/setup-allowlist.go` com `var SetupAllowlist = []string{...}`.
- Cada entrada documentada: nome + reason ("test-only helper", "manual init from main()").

**Edge cases:**
- `SetupWithManager` é método receiver, NÃO é package-level func — already excluded by `fn.Recv != nil`.
- Funções como `SetupNewT(t *testing.T)` em test files — already excluded by `_test.go` suffix.

#### Tasks
1. Listar todas funções `Setup*` em `operators/internal/controller/` (incluindo non-Controller suffix).
2. Identificar quais devem ser wired vs allowlisted.
3. Criar `cmd/setup-allowlist.go` com lista justificada.
4. Atualizar regex no test para `HasPrefix("Setup")`.
5. Skip allowlisted entries no comparison.
6. Rodar test → identificar new failures → wire OR allowlist each.

#### TDD
```
RED:    Test falha com new regex listando funções não-wired (ex: SetupCacheReader)
GREEN:  Cada uma é wired em main.go OR adicionada à allowlist com reason
REFACTOR: Allowlist consolidada em arquivo separado se ≥ 3 entries
VERIFY: go test -run TestSetupControllersWiresAllControllers ./operators/cmd/...
```

#### Acceptance Criteria
- [ ] Test regex captura `Setup*` (não só `Setup*Controller`)
- [ ] Allowlist file existe com ≥ 1 entrada documentada (caso real)
- [ ] Test passes
- [ ] Pass: `/code-audit` lint check
- [ ] Pass: `/code-audit` complexity ≤ 10

#### DoD
- [ ] go test ./operators/cmd/... GREEN
- [ ] CHANGELOG.md `[Unreleased]/Changed`: "TestSetupControllersWiresAllControllers amplified to Setup* prefix"

---

### T0.3 — Knip orphan-exports gate em CI TypeScript

#### Objective
Adicionar knip detection step em `helm-charts.yml` ou novo workflow para detectar orphan exports em `cli/` + `infra/pulumi/`.

#### Evidence
Reusable workflows `reusable-setup-{go,helm,node}.yml` criados em iter1 são "implemented but unused" (composite actions são primitive correto para step-level reuse). Knip teria detectado.

#### Files to edit
```
.github/workflows/infra-gates.yml — adicionar knip job
cli/.knip.json (NEW) — knip config para CLI
infra/pulumi/.knip.json (NEW) — knip config para Pulumi
package.json (cli/) — devDep knip
```

#### Deep file dependency analysis
- `cli/` consume orchestrator é Commander; entry é `cli/dist/index.js`.
- `infra/pulumi/` entry é `index.ts` (Pulumi runs it via `pulumi up`).
- knip needs to know entries to chase reachability from.

#### Deep Dives
**Knip config example (cli/.knip.json):**
```json
{
  "entry": ["src/index.ts", "src/register/*.ts"],
  "project": ["src/**/*.ts"],
  "ignore": ["src/**/*.test.ts", "tests/**"],
  "ignoreDependencies": ["@types/*"]
}
```

**Failure mode:** baseline initial run will list MANY orphans. Address via either deletion (preferred) or explicit allowlist.

#### Tasks
1. Instalar knip: `cd cli && npm i -D knip`.
2. Criar `cli/.knip.json` + `infra/pulumi/.knip.json`.
3. Run `npx knip` localmente, capturar baseline.
4. Triage cada finding: delete code OR add to ignore.
5. Adicionar step em `infra-gates.yml`: `cd cli && npx knip --no-exit-code` (warn first), depois flip para fail.

#### TDD
```
RED:    npx knip --reporter list em cli/ produces ≥ 1 finding
GREEN:  Após cleanup, npx knip exits 0
REFACTOR: Consolidar ignore patterns
VERIFY: cd cli && npx knip
```

#### Acceptance Criteria
- [ ] `cli/.knip.json` configurado
- [ ] `infra/pulumi/.knip.json` configurado
- [ ] Knip baseline limpo OR documented allowlist
- [ ] CI step ativo
- [ ] Pass: `/code-audit` review

#### DoD
- [ ] knip GREEN no CI
- [ ] CHANGELOG.md `[Unreleased]/Added`: "Knip orphan-exports gate em CI"

---

## Phase 1: CI Reliability Foundation

**Objective:** Eliminar parallel test flakes via root-cause fix + amplificar wired-gate (já em Phase 0).

### T1.1 — Identificar e corrigir parallel test flakes

#### Objective
0 fails em `cd cli && npm test` (paralelo default). Não usar `--runInBand` como workaround permanente.

#### Evidence
Iter1 measurement: paralelo = 41 fails / 1243 PASS / 7 skip. `--runInBand` = 0 fails / 1243 PASS / 7 skip. Confirma flake é parallelism-induced.

#### Files to edit
```
cli/tests/integration/_fixtures/mock-api-server.ts — port=0 dynamic
cli/tests/integration/_fixtures/spawn-helper.ts — temp dir per test
cli/tests/integration/exit-code-contract.test.ts — review for shared state
cli/jest.config.ts — testTimeout, maxWorkers tune
docs/runbooks/test-isolation-guide.md (NEW) — pattern doc
```

#### Deep file dependency analysis
- `mock-api-server.ts` provavelmente bind em porta fixa → conflito quando 4 workers paralelos.
- `spawn-helper.ts` provavelmente compartilha `/tmp` paths → race condition em FS.
- `exit-code-contract.test.ts` foi o failure case observado em iter1 — usa spawn de CLI em subprocess.

#### Deep Dives
**Pattern: dynamic port allocation:**
```typescript
const server = createServer(...);
await new Promise<void>((r) => server.listen(0, '127.0.0.1', () => r()));
const port = (server.address() as AddressInfo).port;
```

**Pattern: temp dir per test:**
```typescript
import { mkdtemp } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';

beforeEach(async () => {
  this.tmp = await mkdtemp(join(tmpdir(), 'loopzero-test-'));
});
afterEach(async () => {
  await rm(this.tmp, { recursive: true, force: true });
});
```

**Pattern: env var snapshot+restore:**
```typescript
let originalEnv: NodeJS.ProcessEnv;
beforeEach(() => { originalEnv = { ...process.env }; });
afterEach(() => { process.env = originalEnv; });
```

#### Tasks
1. Run `npm test --logHeapUsage --detectOpenHandles 2>&1 > /tmp/jest-paralelo.log`.
2. Identificar testes fail em paralelo.
3. Para cada test, classificar root cause (port / fs / env / module-state).
4. Aplicar pattern correspondente.
5. Re-run paralelo até 0 fails consistente (3 runs consecutivas).
6. Documentar patterns em `test-isolation-guide.md`.

#### TDD
```
RED:    npm test em paralelo fail ≥ 1 test
GREEN:  3 runs paralelas consecutivas: 0 fails
REFACTOR: Consolidate fixture helpers em _fixtures/
VERIFY: for i in 1 2 3; do cd cli && npm test || exit 1; done
```

#### Acceptance Criteria
- [ ] `npm test` (paralelo default) passa 3 vezes consecutivas
- [ ] mock-api-server usa port=0
- [ ] spawn-helper usa mkdtemp
- [ ] Test runtime ≤ 100s (paralelo)
- [ ] Pass: `/code-audit` review
- [ ] Pass: `npx jest --runInBand` ainda passa (não regredir)

#### DoD
- [ ] CI workflow `cli-commands.yml` GREEN sem `--runInBand`
- [ ] `docs/runbooks/test-isolation-guide.md` shipped
- [ ] CHANGELOG.md `[Unreleased]/Fixed`: "CLI parallel test flakes — root cause em port + tempdir + env state"

---

## Phase 2: Product Completeness — `loopzero scale` real

**Objective:** Implementar persistência DB + reconciliação HPA real para `loopzero scale`. Sair do stub 202.

### T2.1 — Migração SQL `app_scaling_config` table

#### Objective
Schema para persistir configuração de scaling per-app.

#### Evidence
CLAUDE.md: "`loopzero scale` DB persistence (stub returns 202 acknowledged — use loopzero.yaml + loopzero deploy)" listado como "Not built". Plan caps já enforced (`pkg/enforcement/plan_caps.go`) — falta apenas storage+reconcile.

#### Files to edit
```
api/internal/db/migrations/000009_app_scaling_config.up.sql (NEW)
api/internal/db/scaling_store.go (NEW) — CRUD interface
api/internal/db/scaling_store_test.go (NEW)
```

#### Deep file dependency analysis
- Forward-only migration convention (`.up.sql`, no `.down.sql`).
- Latest migration is 000008. New is 000009.
- `scaling_store.go` segue pattern de `release_store.go` (interface + Postgres impl).

#### Deep Dives
**Schema:**
```sql
CREATE TABLE app_scaling_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment IN ('production','staging','preview')),
    app_name TEXT NOT NULL,
    min_replicas INTEGER NOT NULL CHECK (min_replicas >= 0),
    max_replicas INTEGER NOT NULL CHECK (max_replicas >= min_replicas),
    target_cpu_pct INTEGER CHECK (target_cpu_pct BETWEEN 1 AND 100),
    scale_to_zero BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, project_id, environment, app_name)
);

CREATE INDEX idx_app_scaling_config_lookup
    ON app_scaling_config (tenant_id, project_id, environment);

COMMENT ON TABLE app_scaling_config IS
    'CHG-2026-05-XX-SCALE-REAL — scaling config per app. Replaces loopzero.yaml-only path.';
```

**Invariants:**
- `(tenant_id, project_id, environment, app_name)` is unique
- `min_replicas` cannot exceed plan caps (enforced at handler level)
- `max_replicas >= min_replicas` (DB CHECK)
- `target_cpu_pct` only valid when `max_replicas > min_replicas` (HPA scenario)

#### Tasks
1. Criar migração `000009_app_scaling_config.up.sql`.
2. Apply localmente: `task db:migrate`.
3. Validar schema: `psql ... -c "\\d app_scaling_config"`.
4. Criar `ScalingStore` interface em `scaling_store.go`.
5. Implementar `PostgresScalingStore`.
6. Tests: Create / Get / Update / List / Delete por tenant.

#### TDD
```
RED:    TestScalingStore_GetByApp_ReturnsNilWhenNotFound
RED:    TestScalingStore_Upsert_CreatesNewRow
RED:    TestScalingStore_Upsert_UpdatesExistingRow
RED:    TestScalingStore_RejectsMaxLessThanMin
RED:    TestScalingStore_PlanCapsEnforced
GREEN:  Implement migration + store
REFACTOR: Extract validation to Validate() method
VERIFY: cd api && go test ./internal/db/... -run Scaling
```

#### Acceptance Criteria
- [ ] Migration applies sem erro
- [ ] Schema tem todos os CHECK constraints
- [ ] `ScalingStore` interface tem 5 métodos
- [ ] Postgres impl + 5 unit tests GREEN
- [ ] Pass: `/code-audit` complexity ≤ 10
- [ ] Pass: `kubeconform` (não aplicável — DB)

#### DoD
- [ ] `task db:migrate` GREEN
- [ ] go test ./internal/db/... GREEN
- [ ] CHANGELOG.md `[Unreleased]/Added`: "app_scaling_config table (migration 000009)"

---

### T2.2 — API handler `POST /api/v1/projects/{name}/scale` real

#### Objective
Trocar stub 202 por handler que: valida plan caps, persiste em `app_scaling_config`, atualiza `Application` CR `spec.scaling`.

#### Evidence
`api/internal/routes/scale.go:70` é o handler. Atualmente retorna 202 acknowledged sem persistir.

#### Files to edit
```
api/internal/routes/scale.go — substituir stub
api/internal/routes/scale_test.go — atualizar assertions
api/internal/services/scale_service.go (NEW) — orchestration
api/internal/services/scale_service_test.go (NEW)
```

#### Deep file dependency analysis
- Handler chama `ScaleService.Apply(...)` — service faz DB write + CR patch.
- Service depende de: `ScalingStore` (T2.1), `K8sClient` (existing), `EnforcementService` (plan caps).
- Tests use `httptest.NewRequest` pattern existente.

#### Deep Dives
**Flow:**
1. CLI → `POST /scale` com `{environment, app, replicas | autoscaling: {min, max, target_cpu_pct}}`.
2. Handler valida shape + auth + plan caps.
3. Handler chama `ScaleService.Apply(ctx, tenantID, projectID, env, app, config)`.
4. Service:
   - `withEnvironmentLock` (Rule 6 + 7)
   - DB upsert via `ScalingStore.Upsert`
   - K8s patch: `Application` CR `.spec.scaling = {...}`
   - return `ScaleResponse{applied: true, replicas: ...}`
5. Operator (T2.3) reconcilia CR → Deployment.replicas + ScaledObject.

**Idempotency:** request hash na DB upsert; mesmo request retorna 200 com `applied: false, reason: "no change"`.

#### Tasks
1. Atualizar `scale.go` handler para chamar `ScaleService.Apply`.
2. Implementar `ScaleService` em `services/scale_service.go`.
3. Atualizar `scale_test.go` para new behavior.
4. Adicionar `scale_service_test.go` com 5+ scenarios.

#### TDD
```
RED:    TestScaleHandler_Apply_PersistsToDb
RED:    TestScaleHandler_Apply_PatchesApplicationCR
RED:    TestScaleHandler_RejectsBeyondPlanCaps (existing — verify)
RED:    TestScaleHandler_Idempotent_NoOpOnSameRequest
RED:    TestScaleHandler_LockTimeout
GREEN:  Implement service + handler integration
REFACTOR: None expected
VERIFY: cd api && go test ./internal/routes/... -run Scale
```

#### Acceptance Criteria
- [ ] Handler retorna 200 (não 202) quando aplicado
- [ ] DB row criada/atualizada
- [ ] K8s `Application` CR mostra `spec.scaling`
- [ ] Plan caps enforced
- [ ] Idempotent (mesmo request → no change)
- [ ] `withEnvironmentLock` aplicado
- [ ] Pass: `/code-audit` complexity ≤ 10
- [ ] Pass: `/code-audit` coverage ≥ 90%

#### DoD
- [ ] go test ./internal/routes/... GREEN
- [ ] go test ./internal/services/... GREEN
- [ ] CHANGELOG.md `[Unreleased]/Changed`: "loopzero scale: stub 202 → real persistência + CR patch"

---

### T2.3 — Operator `Application` controller reconcilia `spec.scaling`

#### Objective
Quando `Application.spec.scaling` muda, operator atualiza `Deployment.replicas` (manual) OR `ScaledObject` (autoscaling).

#### Evidence
Hoje operator ignora `spec.scaling` — Deployment.replicas vem de `loopzero.yaml` no build path.

#### Files to edit
```
operators/internal/controller/application_controller.go — adicionar reconcileScaling
operators/internal/controller/application_controller_test.go — envtest specs
operators/api/v1/application_types.go — extend ApplicationSpec.Scaling
```

#### Deep file dependency analysis
- `ApplicationSpec` provavelmente já tem campo Scaling — validar; se não, adicionar via CRD update + helm chart sync.
- KEDA `ScaledObject` é managed via separate CR — operator gerencia OR delega para KEDA HTTP add-on.
- Reconcile loop é idempotent — chama `ctrl.Update` apenas se diff.

#### Deep Dives
**Spec extension:**
```go
type ApplicationSpec struct {
    // existing fields...
    Scaling *ScalingSpec `json:"scaling,omitempty"`
}

type ScalingSpec struct {
    MinReplicas    int32  `json:"minReplicas"`
    MaxReplicas    int32  `json:"maxReplicas"`
    TargetCPUPct   *int32 `json:"targetCpuPct,omitempty"`
    ScaleToZero    bool   `json:"scaleToZero,omitempty"`
}
```

**Reconcile branches:**
- `scaling == nil`: do nothing (legacy — replicas from build).
- `scaling.MinReplicas == scaling.MaxReplicas`: fixed replicas → patch `Deployment.spec.replicas`.
- `scaling.MinReplicas < scaling.MaxReplicas` + `target_cpu_pct`: HPA → ensure `ScaledObject` CR.
- `scaling.ScaleToZero == true` + min == 0: KEDA HTTP add-on (HTTPScaledObject).

#### Tasks
1. Atualizar `ApplicationSpec` + CRD + chart sync.
2. Adicionar método `reconcileScaling` em controller.
3. envtest specs: 4 branches (nil, fixed, HPA, scale-to-zero).
4. Testar idempotência (reconcile 2× = no-op).

#### TDD
```
RED:    TestApplicationController_ReconcileScaling_FixedReplicas
RED:    TestApplicationController_ReconcileScaling_HPA
RED:    TestApplicationController_ReconcileScaling_ScaleToZero
RED:    TestApplicationController_ReconcileScaling_Idempotent
GREEN:  Implement reconcileScaling + 4 branches
REFACTOR: Extract scaling decision logic to internal/scaling/
VERIFY: cd operators && go test -tags=envtest ./internal/controller/...
```

#### Acceptance Criteria
- [ ] CRD bump (loopzero.io/v1alpha1 → v1alpha1 with new Scaling field)
- [ ] Reconcile produces correct K8s manifest per branch
- [ ] 4 envtest specs GREEN
- [ ] Idempotent
- [ ] Pass: `/code-audit` size ≤ 500 lines per file

#### DoD
- [ ] go test ./operators/internal/controller/... GREEN
- [ ] CHANGELOG.md `[Unreleased]/Added`: "Application controller reconciles spec.scaling"

---

### T2.4 — CLI `loopzero scale` registra contrato `--json` real

#### Objective
CLI usa endpoint real; `--json` retorna `{applied: true, replicas: N, autoscaling: {...}}` consistente com handler.

#### Evidence
`cli/src/register/scale.ts` é 75 linhas; provavelmente já chama API mas espera 202. Atualizar para 200 + new schema.

#### Files to edit
```
cli/src/register/scale.ts — atualizar response handling
cli/tests/integration/scale.test.ts (NEW se não existe) — schema-check + exit codes
```

#### Tasks
1. Read scale.ts current state.
2. Atualizar para 200-OK path.
3. `--json` schema spec'd em `docs/cli-reference.md` (verificar/atualizar).
4. Tests: success (replicas=3), autoscaling (min=2 max=5 cpu=70), beyond-cap (exit 1), idempotent (no-change message).

#### TDD
```
RED:    test_scale_fixed_replicas_returns_200_with_applied_true
RED:    test_scale_autoscaling_persists_targets
RED:    test_scale_beyond_plan_cap_exits_1
RED:    test_scale_idempotent_no_change_message
GREEN:  Update register/scale.ts
REFACTOR: None expected
VERIFY: cd cli && npx jest tests/integration/scale.test.ts
```

#### Acceptance Criteria
- [ ] CLI exits 0 on success
- [ ] `--json` schema documentado
- [ ] No internal terms (Rule 3)
- [ ] Pass: `/schema-check scale`

#### DoD
- [ ] CLI tests GREEN
- [ ] CHANGELOG.md `[Unreleased]/Changed`: "loopzero scale: stub → real persistência"

---

## Phase 3: Supply Chain Hardening — Cosign Keyless Cutover

**Objective:** Flip `policy-controller` keyless authority de `warn` para `enforce` em produção, removendo key-based como SPoF.

### T3.1 — Dev cluster keyless enforce flip + 7-day observation

#### Objective
Aplicar Phase 1 do `cosign-keyless-cutover.md` runbook em ambiente dev. `loopzero-images-keyless` ClusterImagePolicy passa de `warn` para `enforce`.

#### Evidence
Iter1 entregou keyless authority configurada em modo `warn`. Cutover runbook 3-phase já documentado mas não exercitado.

#### Files to edit
```
infra/helm/admission-policies/values.dev.yaml — clusterImagePolicyKeyless.mode: enforce
infra/helm/admission-policies/values.production.yaml — keep warn for now
docs/runbooks/cosign-keyless-cutover.md — annotate Phase 1 actual execution timestamp
```

#### Tasks
1. Verify dual-signing still active in `helm-charts.yml` + `build-images.yml` (iter1 deliverable).
2. Update dev values.yaml mode → enforce.
3. ArgoCD sync.
4. Smoke test: deploy known-good image (passes); deploy `nginx:latest` unsigned (rejected).
5. Monitor `policy_controller_verify_duration_seconds` for 7 days.

#### TDD
```
RED:    Apply unsigned manifest → expect rejection
RED:    Apply keyless-signed manifest → expect admission
GREEN:  Phase 1 enforce active; metrics stable
REFACTOR: None
VERIFY: kubectl --context k3d-loopzero-workload apply -f infra/tests/admission/keyless-signed.yaml
```

#### Acceptance Criteria
- [ ] Mode flipped in dev
- [ ] Smoke tests: signed PASS, unsigned REJECT
- [ ] p95 verify duration ≤ 2s for 7 consecutive days
- [ ] Zero unexpected rejections in known-good apps

#### DoD
- [ ] 7-day observation log appended to runbook
- [ ] CHANGELOG.md `[Unreleased]/Security`: "Dev cluster keyless enforce mode active"

---

### T3.2 — Staging cluster keyless enforce (after T3.1 7-day GREEN)

[same shape as T3.1 but staging]

#### Acceptance Criteria
- [ ] Staging mode flipped after dev 7d GREEN
- [ ] 7-day staging observation GREEN

---

### T3.3 — Production cluster keyless enforce + key-based removal

[same shape; targets prod values.yaml]

#### Acceptance Criteria
- [ ] Prod keyless enforce active
- [ ] Key-based cosign step REMOVED from helm-charts.yml + build-images.yml
- [ ] policy `loopzero-images-warn` (key-based) DELETED
- [ ] Coverage audit: 100% keyless for last 24h

---

### T3.4 — COSIGN_KEY secret revogação

#### Objective
GitHub Actions secret `COSIGN_KEY` + `COSIGN_PASSWORD` revogados após T3.3 GREEN 24h.

#### Tasks
1. Audit Rekor: confirm 100% keyless coverage 24h.
2. `gh secret delete COSIGN_KEY` + `gh secret delete COSIGN_PASSWORD`.
3. Update runbook with completion timestamp.
4. Update CLAUDE.md: remove "cosign keyless verification with Fulcio pendente Sprint 9".

#### Acceptance Criteria
- [ ] Secrets revogados
- [ ] CLAUDE.md atualizado
- [ ] Cutover runbook closure section preenchida

---

## Phase 4: Soak Evidence — CI Cron First-Run

**Objective:** Trigger manual de `cell2-soaks.yml` Stage 1 + `platform-health-7day-watch.yml` para coletar primeira evidência GREEN.

### T4.1 — Manual trigger `cell2-soaks.yml` Stage 1

#### Objective
Run Stage 1 (1h synthetic soak em cell-2) via `gh workflow run cell2-soaks.yml -f stage=stage1`. Capture artefatos.

#### Evidence
Workflow exists since fix-all-platform-issues iter 1+2 (CHANGELOG 2026-05-02). Cron natural não rodou ainda.

#### Files to edit (or trigger)
```
None — trigger via gh CLI; capture artifact.
```

#### Tasks
1. `gh workflow run cell2-soaks.yml -f stage=stage1`.
2. Aguardar conclusão (~75min).
3. Download artifact + commit em `docs/evidence/cell2-soaks-2026-05-XX/` (NEW).
4. Update CLAUDE.md: "first CI Sunday cron GREEN" → CHECK with timestamp.

#### Acceptance Criteria
- [ ] Workflow run conclusion: success
- [ ] Stage 1 1h soak GREEN per ADR conditions
- [ ] Artifact persisted

---

### T4.2 — Manual trigger `platform-health-7day-watch.yml`

[similar — 1h health check, capture evidence]

---

### T4.3 — Closure docs + maturity 4.0 candidacy update

#### Objective
Atualizar `docs/maturity-assessment.md` + CLAUDE.md com evidência coletada. Marcar maturity 4.0 candidate (não declarar GREEN sem 7-day natural streak).

#### Files to edit
```
docs/maturity-assessment.md — append "First CI cron evidence collected"
CLAUDE.md — update "Path to maturity 4.0" line
```

#### Acceptance Criteria
- [ ] Evidence cited with run ID + timestamp
- [ ] maturity 3.9 → 4.0 candidate (await Sunday natural streak)

---

## Phase 5: Local↔Prod Parity Matrix

**Objective:** Canonicalizar divergências aceitas + CI gate enforcing matrix integrity.

### T5.1 — Matriz canonical em `local-prod-divergence-matrix.md`

#### Objective
Doc enumerando cada divergência aceita.

#### Files to edit
```
docs/runbooks/local-prod-divergence-matrix.md (NEW)
```

#### Deep Dives
**Schema:**
| ID | Aspecto | Local | Produção | Reason | Closure plan | Owner | Horizon |
|---|---|---|---|---|---|---|---|
| LP-001 | TLS cert issuance | cert-manager letsencrypt-prod (não emite *.loopzero.local) | CF-managed TLS | Sem DNS público local | Use mkcert OR self-signed for local probe | infra-engineer | 2026-Q3 |
| LP-002 | Cell-2 probe routing | Port 8091/8444 (DNS resolve para 8090/8443) | DNS resolve para cell-2 LB | k3d port mapping | Add /etc/hosts override OR k3d port-forward | infra-engineer | 2026-Q3 |
| LP-003 | OIDC issuer (cosign keyless) | k3d SA tokens não aceitos por public Fulcio | GHA OIDC tokens accepted by Fulcio | Public Fulcio policy | Private Fulcio OR Spire deployment | platform-security-engineer | Sprint 9 |
| LP-004 | ... | ... | ... | ... | ... | ... | ... |

#### Tasks
1. Audit codebase for `LOCAL ONLY`, `# DEV path`, `// dev:`, etc. comments.
2. Each instance gets a matrix row.
3. Reason + Closure plan + Owner + Horizon mandatory.

#### Acceptance Criteria
- [ ] All divergences in code have matrix entry
- [ ] Each entry has Owner + Horizon
- [ ] Document linkable from CLAUDE.md

---

### T5.2 — CI gate matrix integrity

#### Objective
`infra-gates.yml` job that fails if code grep finds `LOCAL ONLY` without matrix entry.

#### Files to edit
```
scripts/check-divergence-matrix.sh (NEW)
.github/workflows/infra-gates.yml — adicionar job
```

#### Tasks
1. Script greps for divergence markers in code.
2. For each match, validate matrix has matching ID.
3. Fail if mismatch.

#### Acceptance Criteria
- [ ] Script GREEN on current state (or list mismatches)
- [ ] CI job ativo

---

### T5.3 — Cell-2 local probe routing fix

#### Objective
Probe URLs (test infra) consideram cell pinning para resolver para porta correta.

#### Tasks
1. Modify `infra/tests/dogfood/_common.sh` HTTP probe to check tenant cellID via API.
2. If cellID=cell-2 + LOCAL env, use port 8091/8444.
3. Otherwise default 8090/8443.

#### Acceptance Criteria
- [ ] Dogfood journey-01 passa para tenant pinned em cell-2

---

## Phase 6: Dogfood Gate (FINAL)

**Objective:** Validar end-to-end que TODAS as phases anteriores funcionam juntas em sistema real.

### T6.1 — Fresh infra setup + `/dogfood full`

#### Objective
Validação E2E em infra zerada.

#### Tasks
1. `task teardown && task setup && tilt up`.
2. `task doctor` GREEN.
3. `/dogfood full` (todas journeys).
4. Triage findings: 0 S0, 0 S1.

#### Acceptance Criteria
- [ ] Pre-Flight ALL PASS
- [ ] Zero S0
- [ ] Zero S1 in plan scope
- [ ] Prova de Vida + Prova de Morte para cada journey
- [ ] Observability checks PASS
- [ ] Rule 3 clean

---

## Coverage Matrix

| # | Gap | Task(s) | Resolution |
|---|---|---|---|
| 1 | `loopzero scale` stub 202 | T2.1 + T2.2 + T2.3 + T2.4 | DB migration + handler + operator + CLI |
| 2 | Cosign key-based prod SPoF | T3.1 + T3.2 + T3.3 + T3.4 | Faseado dev→staging→prod cutover |
| 3 | CI crons sem evidência | T4.1 + T4.2 + T4.3 | Manual trigger + artifact + maturity update |
| 4 | CLI parallel test flakes | T1.1 | Root-cause: port=0 + tempdir + env restore |
| 5 | Local↔prod parcialmente quebrado | T5.1 + T5.2 + T5.3 | Matriz + CI gate + cell-2 probe fix |
| 6 | Padrão "implemented but not wired" | T0.2 + T0.3 | Setup* regex + knip TS |
| 7 | Plan amendment policy | T0.1 | ADR-0022 + governance rule |

**Coverage: 7/7 gaps (100%)**

## Global Definition of Done

- [ ] Todas Phases 0-6 completed
- [ ] Go test ./... GREEN
- [ ] CLI test paralelo GREEN (sem `--runInBand`)
- [ ] Pulumi vitest 337/337 GREEN
- [ ] helm-unittest GREEN
- [ ] Backward compat preserved (pre-release exception: scale schema is new)
- [ ] code-audit checks GREEN per package modified
- [ ] CHANGELOG.md atualizado
- [ ] CLAUDE.md atualizado (Not built section, maturity, supply chain status)
- [ ] **`/dogfood full` PASSED em fresh infra**

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure (operator must run before dogfood)
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] Prova de Vida passes for every deploy performed during dogfood
- [ ] Prova de Morte passes for every delete performed during dogfood
- [ ] **`loopzero scale` works end-to-end for ≥ 1 journey** (T2 verification)
- [ ] **Cosign keyless verify GREEN** for every image deployed (T3 verification)
- [ ] **No new "implemented but not wired" findings** (T0.2/T0.3 enforcement)
- [ ] No regressions in previously passing flows
- [ ] Observability check passes (logs, metrics, CRs in correct state)
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.
