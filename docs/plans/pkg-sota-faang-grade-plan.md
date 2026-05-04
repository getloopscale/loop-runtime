# Plan: Shared Packages SOTA FAANG-Grade Improvement

> **Version 1.3** — Updated 2026-05-04 post Ralph loop iter 5 closure. **T2.2 ALL 5/5 Acceptance Criteria GREEN** (previously 3/5 at iter 1 close): pkg/conditions package + 12+ tests + 2 controllers migrated (tenant + environment via adapter pattern) + migration guide `docs/technical/pkg/conditions.md` + /code-audit. Adoption follow-up #1 advanced 0/8 → 2/8 controllers; remaining 6/8 trivial (1-line edit per controller per migration guide). T3.2 special validation criterion (c) "kstatus conditions visíveis em kubectl get tenant -o yaml" now empirically achievable. 23 commits across iter 1-5. Plan transitions to v2.0 "IMPLEMENTATION COMPLETE — DOGFOOD VALIDATED" when `pkg-soak.yml` first cron run is GREEN.
>
> **Version 1.2** — Updated 2026-05-04 post Ralph loop iter 4 closure. Phases 0-2 ✅ COMPLETE; Phase 3 ⏳ T3.1 ✅ DONE / T3.2 ⏳ infrastructure-encoded as `pkg-soak.yml` weekly cron (analog to `operator-split-soak.yml`). 21 commits across iter 1-4. Adoption follow-up #2 (CB Execute[T] migration) ✅ FULLY CLOSED iter 4 (4/4 operator consumers); #3 (ContextLogger migration) ✅ no-op confirmed (RegisterTraceExtractor unused in production); #1 + #4 tracked for separate change records. Plan transitions to v2.0 "IMPLEMENTATION COMPLETE — DOGFOOD VALIDATED" when `pkg-soak.yml` first cron run is GREEN.
>
> **Version 1.1** — Updated 2026-05-04 post Ralph loop iter 1-3 closure. Phases 0-2 ✅ COMPLETE; Phase 3 ⏳ T3.1 ✅ DONE / T3.2 deferred (analog to `operators-sota-faang-grade-plan` Final Dogfood Gate — wall-clock-bound, CI cron + production canary). 18 commits across iter 1-3. See **Status Snapshot v1.1** below + `docs/plans/pkg-sota-faang-grade-plan/HANDOFF.md` (TBD when first prod canary closes). 8 ADRs ratified async (0028-0035) + amendment A1 (CB Execute[T] package-level). All 18 internal task entries `[completed]` except T3.2 which requires human-in-the-loop dogfood validation per CONDITION-5.
>
> **Version 1.0** — Este plano executa cada recomendação de `docs/technical/pkg/improvement-roadmap.md` em uma sequência por fases, dependency-aware, levando o módulo `pkg/` de SOTA score 7.0/10 para 9.0/10. Resolve dead code documentado (`pkg/health` com 0 imports), gaps de segurança contra RFC 8725bis no `pkg/jwt`, ausência de generics+context em `pkg/circuitbreaker`, transport coupling em `pkg/errors`, dependência direta de `*sql.DB` em `pkg/enforcement`, global mutable state em `pkg/logging`, e introduz `pkg/conditions` kstatus-compatible para os 8 operators. Todas as fases são TDD-driven com RED-GREEN-REFACTOR, code-audit checks por task, e validação final via `/dogfood full`. Pré-release governance: Phase 0 obrigatória antes de qualquer commit em `pkg/` (mudanças aqui cascatam para 89 importadores em `api/internal` + 19 em `operators/internal`).

---

## Status Snapshot (v1.1 — 2026-05-04 post Ralph closure)

| Phase | Status | Tasks done | Evidence |
|---|:---:|---|---|
| 0 Governance Gate | ✅ | T0.1 | MEET-2026-05-04-PKG-SOTA + 8 ADRs (0028-0035) + amendment A1 |
| 1 Quick Wins | ✅ | T1.1, T1.2, T1.3, T1.4, T1.5, T1.6, T1.7 | 7/7 commits; pkg/health DELETED, JWT RFC 8725bis hardened, validators added, JWKS via CB, typed labels, errors.WrapWithStack, usage matrix gate G14 active |
| 2 Modernization | ✅ | T2.1, T2.2, T2.3 (FULL A+B+C), T2.4, T2.5, T2.6, T2.7 | 7/7 incl. BREAKING field removal; pkg/conditions created, pkg/errors/http RFC 9457, LimitsStore DIP, ContextLogger, property tests, state machine DOT viz |
| 3 Validation | ⏳ | T3.1 ✅ | T3.2 deferred to dedicated session |
| 3 T3.2 Final Dogfood Gate | ⏳ | encoded as deferred runbook | analogous to operators-sota Final Gate; wall-clock-bound (4h soak + chaos + CI cron) |

**Total commits in this loop:** 18 across iter 1-3.
**LIVE evidence:** all `cd pkg && go test -race ./...` GREEN (10 packages including new `pkg/conditions` + `pkg/errors/http`); api+operators+cloud build clean; `task pkg:usage-matrix:check` GREEN; `task pkg:state-machine-dots:check` GREEN; `validate-matrix:quick` PASSes the 2 new pkg-sota gates (G14, T2.7).
**CI gate:** Gate 9.56 (PKG-G14) + Gate 9.57 (state-machine-dots) added to `validate-matrix.sh`; future `.github/workflows/pkg-soak.yml` (TBD) will encode T3.2 Final Dogfood Gate.
**Production rollout:** the `pkg/conditions` operator migration (8 controllers) + `pkg/logging` ContextLogger migration (4 binaries) + `pkg/circuitbreaker` Execute[T] migration (4 remaining operator consumers) tracked as separate change records — they are non-blocking adoption work, not core plan scope.

## Governance Conditions Status (MEET-2026-05-04-PKG-SOTA)

| Condition | Status | Evidence |
|---|---|---|
| CONDITION-1 (sre): deprecation runbooks for D4+D7 | ✅ | CHANGELOG migration notes; 1-release window documented in code |
| CONDITION-2 (api): T2.3 staged A/B/C rollout | ✅ | Stage A (introduce) + Stage B (helpers) + Stage C (BREAKING field removal) all shipped |
| CONDITION-3 (verification): property tests in CI default | ✅ | T2.6 ran `quick.Check` 1000-10000 iter; included in `task test:unit` default |
| CONDITION-4 (security): T1.2 max defer iter 2 | ✅ | T1.2 shipped iter 1 |
| CONDITION-5 (sre): T3.2 dogfood non-skippable | ⏳ | Deferred to dedicated session — runbook below |

## T3.2 Deferred Runbook

`/dogfood full` is **MANDATORY** per CONDITION-5 (sre-engineer absolute veto). It was not executed during the Ralph loop because:

1. The dogfood gate is **wall-clock-bound** — requires `task teardown && task setup && tilt up` (10-30 min boot for k3d + helmfile + ArgoCD bootstrap), then `/dogfood full` (multiple deploy/promote/rollback flows × ~10 min each), then 4h soak per CONDITION-5 invariants.
2. The k3d cluster discovered in iter 3 was partially functional (30 PASS / 11 WARN / 0 FAIL via `task doctor`) but operators NOT running — meaning `/dogfood full` would abort early in any case.
3. `operators-sota-faang-grade-plan` v1.4 set the precedent: when wall-clock-bound dogfood gates exceed iteration budget, defer to CI cron + production canary. That plan's Final Dogfood Gate is encoded as `.github/workflows/operator-split-soak.yml` Saturday weekly cron — same pattern applies here.

**Execution checklist** (when dedicated session is available):

```bash
# 1. Fresh infrastructure
task teardown && task setup
tilt up   # in another terminal; wait until all operator pods Ready
task doctor  # expect 30+ PASS / 0 FAIL

# 2. Run full dogfood
/dogfood full

# 3. Special validation steps for pkg-sota scope
# (a) RFC 9457 problem+json shape — curl an error endpoint:
curl -s -X POST http://localhost:13001/api/v1/projects -d '{}' | jq .
# expect Content-Type: application/problem+json + fields: type/title/status/detail/code

# (b) Circuit breaker observability — simulate Hydra restart:
kubectl -n loopzero-system rollout restart deployment ory-hydra
# tail api logs for "JWKS fetch skipped: circuit breaker open"

# (c) kstatus conditions presence in operator-managed CRs:
kubectl get tenant <X> -o yaml | yq '.status.conditions'
# expect Ready / Reconciling / Stalled triplet (after pkg/conditions adoption follow-up)
```

**Pass criteria:**

- [ ] `/dogfood full` PASS on clean infrastructure
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to pkg-sota scope (errors HTTP migration, JWT hardening, conditions, CB Execute, etc.)
- [ ] Prova de Vida + Prova de Morte each deploy/delete passes
- [ ] No regressions in deploy/scaling/promote/rollback flows
- [ ] Rule 3 clean (zero internal terms in CLI output)
- [ ] Special validation (a)+(b)+(c) above all GREEN
- [ ] 4h soak with continuous deploy traffic per CONDITION-5
- [ ] CI cron `pkg-soak.yml` first GREEN run (analogous to operator-split-soak.yml weekly cron)

When all of the above are true, the plan transitions to v2.0 marked `IMPLEMENTATION COMPLETE — DOGFOOD VALIDATED`.

## Adoption Follow-ups

### #1 — pkg/conditions adoption in 8 operators (separate change record)

Currently `operators/internal/conditions/` has equivalent implementation per ADR-0025. Migration requires adapter `func ToMetav1(c conditions.Condition) metav1.Condition` + corresponding `FromMetav1`. Per ADR-0032 follow-up — **non-blocking**, separate change record.

### #2 — pkg/circuitbreaker.Execute[T] migration in 4 operator consumers ✅ COMPLETE (iter 4)

All 4 done: argocd (iter 2), gitea + harbor + minio (iter 4). Gate PKG-G16 fully GREEN.

### #3 — pkg/logging.ContextLogger migration ✅ NO-OP

Iter 4 audit revealed: `logging.RegisterTraceExtractor` is called ONLY in `pkg/logging/context_test.go` (self-tests). Zero production callers. The "10 importers" tracked by Real Usage Matrix all import `logging.NewLogger`, not the trace extractor — they don't use the deprecated global state. ADR-0034 deprecation can be safely fast-forwarded to removal in any future cleanup commit.

### #4 — pkg/errors.WrapWithStack adoption in error-prone code paths

Currently opt-in — `Wrap()` remains the default for routine domain errors. Document migration patterns when first incident benefits. Non-blocking.

### Status summary

These follow-ups demonstrate the plan's incremental-friendly design: every change provides backward compat or deprecation window, so adoption can be paced by domain teams without blocking the core SOTA improvements. **Adoption #2 fully closed**; **#3 confirmed no-op**; **#1 + #4 tracked for separate change records**.

---

## Context

A pesquisa SOTA `pkg/` (2026-05-04, `docs/technical/pkg/improvement-roadmap.md`) auditou os 9 pacotes em `pkg/` (3052 LOC produção / 5562 LOC testes) cruzando docs com código real e benchmark FAANG. Achados estratificados:

**Crítico (gates GREEN superficial, código revela gap):**

- **`pkg/health` é dead code documentado como flagship** — 0 imports em produção apesar de `architecture.md:69-78` e `research-basis.md:166-178` descreverem como "contrato de health compartilhado". Existe parallel implementation com escopo diferente em `api/internal/health/prober.go` (HTTP probing, não phase aggregation). Total: 253 LOC + 480 LOC tests sem consumidor.
- **`pkg/jwt` pré-RFC 8725bis (2026)** — `validator.go:78-99` não usa `goJWT.WithValidMethods()` whitelist explícito (vetor de algorithm confusion attack); não valida `typ` header (cross-JWT confusion); não respeita `Cache-Control` no JWKS fetch; não usa `pkg/circuitbreaker` próprio para JWKS endpoint (dogfooding gap, gate G16).
- **`LoopZeroError.HTTPStatus`** (`pkg/errors/errors.go:14-27`) acopla modelo de erro a HTTP transport — viola gate PKG-G08 e Google AIP "errors should be transport-agnostic". Operators e CLI importam `pkg/errors` mas não precisam de HTTP semantics.

**Alto (afeta DX e modernização):**

- **`pkg/circuitbreaker` v1-style** — sem generics (`Breaker[T any]`), sem `context.Context` propagation, sem `IsExcluded` para cancellation. `sony/gobreaker v2` é o de-facto Go standard com generics+context desde 2024.
- **`pkg/enforcement` acopla `*sql.DB`** (`limiter.go:14-23`, `counters.go:10-21`) — viola DIP. Tests usam `go-sqlmock` (991 LOC) para contornar — sintoma de design errado. Operators que precisem de plan limits importariam `database/sql` desnecessariamente.
- **`pkg/logging` global mutable state** (`context.go:17-20`) — `globalExtractor` + mutex global viola Google CDK design principle "no global state in libraries". Múltiplos extractors coexistindo (api+operators) impossível.

**Médio (DRY + safety):**

- **`pkg/id` sem validators exportados** — `IsValidBuildID(s)`, `ParseReleaseID(s)` ausentes; consumidores em `api/internal/db/` reimplementam regex inline.
- **`naming.Labels`** retorna `map[string]string` com chaves hardcoded — typos em consumidores não detectados em compile-time.
- **`pkg/errors.Wrap`** não captura stack — debugging em produção depende de re-execução do code-path.
- **State machines sem visualização** — 9 máquinas em `pkg/statemachine/` (~870 LOC) descritas em prosa; `looplab/fsm.Visualize()` é trivial e geraria diff visual em PRs.
- **Falta `pkg/conditions`** — 8 operators reconciliam `metav1.Condition` ad-hoc sem usar `meta.SetStatusCondition`/kstatus discipline (`Reconciling`/`Stalled` polaridade abnormal-true + `observedGeneration`).

**Drift documentação ↔ código** (gate PKG-G14 novo):

- `architecture.md` descreve `cloud/` como consumidor major; realidade é 4 imports vs 89 da `api/`.
- `research-basis.md` lista `pkg/health` como exemplo SOTA mas o pacote é unused.

Evidência completa em `docs/technical/pkg/improvement-roadmap.md`. Real Usage Matrix gerada via grep em `docs/technical/pkg/architecture.md`.

## Objective

**Definição de "done":** `pkg/` atinge SOTA score 9.0/10 (FAANG-grade benchmark vs `crossplane-runtime` + `google/go-cloud` + `sony/gobreaker v2` + `kstatus`), com zero dead code, RFC 8725bis compliance no JWT, generics+context no circuit breaker, transport-agnostic errors, DIP-compliant enforcement, no-global-state logging, e novo `pkg/conditions` adotado por 8 operators.

**Goals mensuráveis:**

1. `pkg/health` resolved (delete OU wired) — 0 dead-code packages em `pkg/`.
2. `pkg/jwt` passa em test suite RFC 8725bis (algorithm whitelist, `typ`, JWKS via CB, `none` rejection, HMAC-com-public-key rejection).
3. `pkg/circuitbreaker.Breaker[T]` exposto com `Execute(ctx, fn)`; v1 deprecated mas funcional (1 release window).
4. `pkg/errors.LoopZeroError.HTTPStatus` movido para `pkg/errors/http`; operators/CLI não importam HTTP semantics.
5. `pkg/enforcement` aceita `LimitsStore` interface; `*sql.DB` adapter em `api/internal/db`.
6. `pkg/logging.RegisterTraceExtractor` deprecated; `NewContextLogger(extractor)` é o caminho novo.
7. `pkg/conditions` criado e adotado por ≥ 1 controller; restantes documentados como adoption pipeline.
8. `pkg/id.IsValid*` validators exportados; consumidores migrados.
9. `naming.StandardLabels` typed struct exposta; `naming.Labels(...)` deprecated com same-shape compat.
10. `pkg/errors.Wrap` opcionalmente captura stack via `runtime.Callers`.
11. Property-based tests em `pkg/statemachine` e `pkg/naming`.
12. State machine `Visualize()` retorna DOT format; CI gera SVGs em `docs/state-machines/`.
13. `task pkg:usage-matrix` script + CI gate ativo (PKG-G14).
14. Drift `architecture.md` ↔ código resolvido.
15. `/dogfood full` GREEN em infra fresca.

## ADRs

### ADR D1: Resolver `pkg/health` deletando o package em vez de wirear

**Decisão:** `pkg/health/aggregator.go`, `phase.go`, `component.go` e respectivos tests serão DELETADOS. `api/internal/health/prober.go` (HTTP probing, escopo diferente) permanece.

**Rationale:**
- 0 importadores em produção há 2+ sprints (gate PKG-G01 NOT_MET).
- Phase aggregation conceitualmente correto está sendo migrado para `pkg/conditions` kstatus-compatible (D5) — duplicar Phase + Condition seria anti-pattern.
- Wirear o pacote sem use-case real seria YAGNI deliberado.
- Operators que precisarem agregar phase usarão `pkg/conditions.AggregateToPhase(conditions, observedGen, currentGen)` introduzido por D5.

**Consequences:**
- Habilita: docs honestos (gate PKG-G14 GREEN), pacote `pkg/conditions` único como source of truth de health-like state.
- Constrange: futuras phase aggregation devem usar Condition model, não Phase enum standalone.

### ADR D2: Hardenizar `pkg/jwt` para RFC 8725bis ANTES de Sprint 9

**Decisão:** Algorithm whitelist explícito via `goJWT.WithValidMethods([]string{"RS256"})`, `typ` header validation, alg case-sensitive verification, JWKS fetch via `pkg/circuitbreaker`. Implementado como QW (Phase 1), não Sprint 9.

**Rationale:**
- Algorithm confusion attacks são exploráveis hoje em produção (Hydra emite RS256 mas validator não whitelist explicitamente).
- RFC 8725bis está em final draft (`-04`); ajustar agora evita re-trabalho.
- Custo é baixo (~30 LOC + tests); benefício é fechar vetor de auth bypass.

**Consequences:**
- Habilita: gate PKG-G11 reforçado, gate PKG-G16 GREEN (eat-your-own-dogfood JWKS via CB), conformance documentável.
- Constrange: tokens com `alg=HS256` ou `typ` não-canônico passam a ser rejeitados — pode quebrar consumidores não-conformes (hoje só LoopZero emite, então safe).

### ADR D3: Separar `pkg/errors` em modelo (Code+Message+Cause+Stack) e transporte (`pkg/errors/http`)

**Decisão:** `LoopZeroError` mantém `Code`, `Message`, `Cause`, `Stack`. Campo `HTTPStatus` removido do tipo e movido para função `pkg/errors/http.Status(err) int` via lookup table. Helper `pkg/errors/http.WriteJSON(w, err)` emite RFC 9457 problem+json.

**Rationale:**
- Operators e CLI não devem importar HTTP semantics (gate PKG-G15 novo).
- `google.rpc.Status` precedent: separar código (int32), message, details (Any). HTTP mapping é responsabilidade do gateway.
- RFC 9457 problem+json é wire format formal — `pkg/errors/http` é o lugar natural.

**Consequences:**
- Habilita: gate PKG-G15 GREEN, RFC 9457 schema gerado de lookup table, errors trafegáveis em audit/CLI/gRPC sem ruído.
- Constrange: BREAKING CHANGE para callers que acessam `loopzeroErr.HTTPStatus` direto. Pre-release: aceitável (CHANGELOG documenta). Migration path: `errors.HTTPStatus(loopzeroErr)` → `errorshttp.Status(loopzeroErr)`.

### ADR D4: Migrar `pkg/circuitbreaker` para generics + context com deprecation window de 1 release

**Decisão:** Nova API `circuitbreaker.New[T any](cfg)` com `Execute(ctx context.Context, fn func(context.Context) (T, error)) (T, error)`. API antiga (`PreCheck()` + `RecordResult()`) marcada `Deprecated:` mas funcional por 1 release. `IsExcluded(err)` exclui `context.Canceled`/`DeadlineExceeded` das contagens.

**Rationale:**
- `sony/gobreaker v2` (2024+) é o de-facto Go standard; `mercari/go-circuitbreaker` para context-awareness.
- Type safety em Execute é ergonomia significativa para consumidores em `api/internal/services/workflow_circuit_breaker.go`.
- Cancellation propagation correta evita falsa-saturação do CB durante deploys cancelados.

**Consequences:**
- Habilita: gate PKG-G17 GREEN, `pkg/jwt` JWKS pode usar Execute genérico (D2 + dogfooding).
- Constrange: 1 release de coexistência (5 importadores migrados em ordem definida em T2.1).

### ADR D5: Introduzir `pkg/conditions` kstatus-compatible substituindo a função de `pkg/health`

**Decisão:** Novo pacote `pkg/conditions` re-exporta `metav1.Condition`, provê wrappers `SetCondition`, `IsReady`, `IsStalled`, `IsReconciling`, `AggregateToPhase`. Standard conditions: `ConditionReady`, `ConditionReconciling`, `ConditionStalled`. Polaridade abnormal-true seguindo kstatus.

**Rationale:**
- 8 operators reconciliam conditions ad-hoc (drift entre controllers).
- kstatus é a referência Kubernetes-native; Operator SDK + Kubebuilder oficialmente recomendam.
- Resolve a função conceitual de `pkg/health` (Phase aggregation) usando o modelo Condition que K8s consumers entendem.

**Consequences:**
- Habilita: operators migram ad-hoc condition logic para uma única source; `kubectl get` exibe conditions consistentemente; ArgoCD health sync usa kstatus convention.
- Constrange: 8 controllers (operators/internal/controller/*) precisam refactor — programado em T2.2.

### ADR D6: Quebrar dependência direta de `*sql.DB` em `pkg/enforcement` via `LimitsStore` interface

**Decisão:** `PlanLimiter` aceita `LimitsStore interface { GetTenantPlan(ctx) (string, error); GetLimits(ctx, plan, metric) (hard, soft int, err error) }`. Implementação SQL em `api/internal/db/limits_store.go`. `*sql.DB` direto removido de `pkg/`.

**Rationale:**
- Viola DIP — pacote shared não deve depender de driver de banco.
- Tests com `go-sqlmock` (991 LOC) são sintoma de design quebrado.
- Operators ou CLI poderiam querer enforcement com backend diferente (e.g., in-memory cache, OPA policy).

**Consequences:**
- Habilita: tests usam in-memory `LimitsStore`, limits independente de Postgres.
- Constrange: `api/cmd/main.go` precisa wireup explícito do adapter (one-time cost).

### ADR D7: Eliminar global mutable state em `pkg/logging` introduzindo `ContextLogger`

**Decisão:** Novo tipo `ContextLogger` aceita `TraceExtractor` no construtor. `RegisterTraceExtractor` global marcado `Deprecated:` por 1 release.

**Rationale:**
- `google/go-cloud/internal/docs/design.md`: "the Go CDK should not introduce global state. Global state is difficult to reason about in large codebases".
- Race condition real: trocar tracing backend em runtime é seguro contra CDC, mas leitor concorrente pode ver state inconsistente entre `extractor` e `_test`.
- Operators e api hoje compartilham binary em alguns deploys — múltiplos extractors coexistindo é caso real.

**Consequences:**
- Habilita: tests sem reset global, múltiplos extractors, gate PKG-G16/Go CDK alignment.
- Constrange: 10 importadores migram (definidos em T2.5).

### ADR D8: Property-based tests em `pkg/statemachine` e `pkg/naming` via `testing/quick`

**Decisão:** Adotar `testing/quick` (stdlib) em vez de `gopter`. Property tests cobrem invariantes globais — terminais não aceitam events; Sanitize(s) sempre retorna empty OU passes ValidateKubernetesName. Mínimo 1000 random iterations por property.

**Rationale:**
- `testing/quick` é stdlib — zero deps externas (alinha com PKG-G03).
- Invariantes detectam bugs que casos listados não pegam (e.g., naming.Sanitize com unicode multi-byte, statemachine com event de terminal).

**Consequences:**
- Habilita: gate PKG-G18 GREEN, confiança em modificações futuras de naming/statemachine.
- Constrange: ~150 LOC de tests novos; nenhum runtime cost.

## Dependency Graph

```
Phase 0 (Governance) ──▶ Phase 1 (Quick Wins) ──▶ Phase 2 (Modernization) ──▶ Phase 3 (Validation + Dogfood)
                              │                          │
                              │                          ├─▶ T2.1 (CB generics) ──┐
                              ├─▶ T1.1 (kill health) ────┤                         │
                              ├─▶ T1.2 (JWT RFC8725bis)  ├─▶ T2.2 (conditions) ◀───┤
                              ├─▶ T1.3 (id validators)   │                         │
                              ├─▶ T1.4 (JWKS via CB)     ├─▶ T2.3 (errors split)   │
                              ├─▶ T1.5 (typed labels)    │                         │
                              ├─▶ T1.6 (errors stack)    ├─▶ T2.4 (enforcement DIP)│
                              └─▶ T1.7 (usage matrix) ───┤                         │
                                                          ├─▶ T2.5 (logging)       │
                                                          ├─▶ T2.6 (property tests)│
                                                          └─▶ T2.7 (state viz) ◀───┘
                                                                  │
                                                                  ▼
                                                          Phase 3 (sequential)
                                                          T3.1 → T3.2 (dogfood)
```

**Sequenciamento crítico:**

- Phase 0 BLOQUEIA Phase 1 — ADRs precisam ser ratificados antes de qualquer commit em `pkg/`.
- Phase 1 tasks são **paralelizáveis entre si** (cada QW toca um pacote diferente, exceto T1.4 que toca `pkg/jwt` e depende lightly de T1.2).
- Phase 2 T2.1 (CB generics) é blocker de T1.4 RE-VISIT no segundo ciclo (QW-4 inicial usa CB v1; após T2.1 migra para CB v2).
- Phase 2 T2.2 (conditions) habilita T1.1 final cleanup (delete `pkg/health` só após `pkg/conditions` provar viabilidade).
- Phase 3 é sequential: T3.1 (integration tests) → T3.2 (dogfood full).

---

## Phase 0: Governance Gate & ADRs

**Objective:** Ratificar 8 ADRs (D1-D8) via meeting T3 antes de qualquer mudança em `pkg/` (cascata para 89+19 importadores).

### T0.1 — Convocar Meeting Orchestrator T3 com pre-release governance

#### Objective
Submeter o plano completo (este doc) ao Meeting Orchestrator com especialistas Staff/Principal, ratificar ADRs D1-D8, capturar conditions/objections.

#### Evidence
- `pkg/` toca shared contracts; `.claude/rules/pre-release-governance.md` tier T3 ("Architecture, stack, critical flow") é mandatório.
- Histórico: `operators-sota-faang-grade-plan.md` v1.1 ratificou 5 ADRs via MEET-068; mesmo padrão se aplica.
- 8 ADRs propostos exigem múltiplos especialistas: api-engineer (errors HTTP split), platform-security-engineer (JWT RFC 8725bis), k8s-controller-engineer (conditions kstatus), platform-architect (pkg/health delete + conditions design), verification-architect (property-based), dx-engineer (typed labels + error model migration), distributed-systems-engineer (CB context cancellation), sre-engineer (deprecation windows).

#### Files to edit
```
docs/meetings/MEET-2026-05-XX-PKG-SOTA.md (NEW) — record do meeting
docs/meetings/adr/0028-pkg-health-deletion.md (NEW) — D1 formalizado
docs/meetings/adr/0029-jwt-rfc8725bis-hardening.md (NEW) — D2 formalizado
docs/meetings/adr/0030-errors-transport-decoupling.md (NEW) — D3 formalizado
docs/meetings/adr/0031-circuitbreaker-generics-context.md (NEW) — D4 formalizado
docs/meetings/adr/0032-conditions-kstatus-package.md (NEW) — D5 formalizado
docs/meetings/adr/0033-enforcement-dip-limits-store.md (NEW) — D6 formalizado
docs/meetings/adr/0034-logging-no-global-state.md (NEW) — D7 formalizado
docs/meetings/adr/0035-pkg-property-based-tests.md (NEW) — D8 formalizado
CHANGELOG.md — entry "[Unreleased] Added: 8 ADRs (0028-0035) for pkg/ SOTA improvement"
```

#### Deep file dependency analysis
- `MEET-2026-05-XX-PKG-SOTA.md` — record canônico do meeting; lista participantes, alternativas avaliadas, conditions impostas. Sem dependentes downstream — é input para ADRs.
- `adr/0028-0035` — cada ADR é referenciado por commits Phase 1+ via convenção `Implements ADR-NNNN`. Linkado de plan version bumps quando amendments ocorrem (ADR-0022 plan amendment policy).
- `CHANGELOG.md` — segue Keep a Changelog. ADR refs em `Added` section.

#### Deep Dives
- **Meeting agenda** (4 blocos):
  1. Apresentar SOTA score 7.0/10 e os 7 gaps (improvement-roadmap.md exec summary).
  2. Validar D1-D8 individualmente. Especialistas levantam vetos/conditions.
  3. Negociar deprecation window (D4 e D7 propõem 1 release).
  4. Aprovar plan v1.0 ou exigir v1.1 com conditions.
- **Veto-holders esperados:**
  - platform-security-engineer (absolute veto) — D2 RFC 8725bis hardening; pode exigir migração imediata vs deprecation window.
  - sre-engineer (absolute veto on production readiness) — D3/D4/D7 deprecation windows precisam runbook de fallback.
  - verification-architect (conditional veto) — D8 property-based deve incluir specific invariantes listados.

#### Tasks
1. Rodar `/meeting` skill com tier=T3, escopo=plan v1.0 inteiro.
2. Capturar specialist positions em MEET record.
3. Para cada ADR D1-D8, criar arquivo `adr/0028-0035-*.md` seguindo template `docs/meetings/adr/0001-*.md`.
4. Adicionar entry `CHANGELOG.md [Unreleased] Added`.
5. Se conditions BINDING emergirem, atualizar este plan para v1.1 com Governance Conditions Appendix.
6. Commit `docs(governance): MEET-NNN + ADRs 0028-0035 ratify pkg-sota plan`.

#### TDD
```
RED:     N/A (governance task, no code).
GREEN:   N/A.
REFACTOR: N/A.
VERIFY:  task validate:docs (kubeconform on examples + markdown lint)
```

#### Acceptance Criteria
- [ ] MEET record committed em `docs/meetings/MEET-2026-05-XX-PKG-SOTA.md`.
- [ ] 8 ADRs (0028-0035) committed e ratificados (status: ACCEPTED).
- [ ] Cada ADR cita pelo menos 2 alternativas rejeitadas com rationale.
- [ ] CHANGELOG entry presente em [Unreleased].
- [ ] Meeting orchestrator declarou APPROVED ou APPROVED_WITH_CONDITIONS.
- [ ] Se conditions binding, plan version bumped para v1.1 com Governance Conditions Appendix.

#### DoD
- [ ] Todos os files acima committed.
- [ ] `git log` mostra MEET + ADRs em commits separados.
- [ ] `task validate:docs` GREEN.
- [ ] No code in `pkg/` modified yet (separation enforced).

---

## Phase 1: Quick Wins (Foundation Hardening)

**Objective:** 7 quick wins paralelizáveis que fecham gaps de segurança (JWT) e qualidade (dead code, validators) em ≤ 14 sessões totais.

### T1.1 — Resolver `pkg/health` (delete per ADR D1)

#### Objective
Deletar `pkg/health/aggregator.go`, `phase.go`, `component.go` e tests, removendo 379 LOC de dead code documentado como flagship.

#### Evidence
- `grep -r "github.com/loopzero/loopzero/pkg/health" --include="*.go"` retorna 0 matches em produção.
- `architecture.md` antes do update PKG-G14 listava `pkg/health` como "active". Agora marcado ⚠️.
- Phase aggregation usecase real será coberto por `pkg/conditions.AggregateToPhase` (T2.2).

#### Files to edit
```
pkg/health/aggregator.go (DELETE)
pkg/health/aggregator_test.go (DELETE)
pkg/health/component.go (DELETE)
pkg/health/component_test.go (DELETE)
pkg/health/phase.go (DELETE)
pkg/health/phase_test.go (DELETE)
docs/technical/pkg/architecture.md — remover linha de pkg/health da tabela
docs/technical/pkg/research-basis.md — remover seção "Health contracts"
docs/technical/pkg/improvement-roadmap.md — anotar QW-1 = DONE
CHANGELOG.md — entry "[Unreleased] Removed: pkg/health (dead code, 0 importadores). Phase aggregation moves to pkg/conditions per ADR-0028."
```

#### Deep file dependency analysis
- `pkg/health/*` — 0 importadores externos. Deletar é safe.
- `architecture.md` — Real Usage Matrix já marca pkg/health 0 imports. Consistência pós-delete.
- `research-basis.md:166-178` — seção "Health contracts" descreve `pkg/health/aggregator.go` como evidence. Remove e adiciona referência a ADR-0028.

#### Deep Dives
- **Por que delete e não rename para `experimental/`?** Pre-release governance permite breaks; manter código não usado é YAGNI.
- **Edge case:** verificar se `infra/tests/e2e/` usa `pkg/health` indiretamente (via reflection ou test fixtures). Comando: `grep -r "health.Aggregate\|health.Phase" --include="*.go"`.
- **Migration note futura:** quando `pkg/conditions` introduzir `AggregateToPhase`, semantics são compatíveis (worst-phase wins) então consumidores potenciais futuros podem migrar 1:1.

#### Tasks
1. `git rm pkg/health/aggregator.go pkg/health/aggregator_test.go`.
2. `git rm pkg/health/component.go pkg/health/component_test.go`.
3. `git rm pkg/health/phase.go pkg/health/phase_test.go`.
4. `rmdir pkg/health` (se vazio após removes).
5. `cd pkg && go test ./...` — confirmar GREEN.
6. `cd api && go test ./...` — confirmar zero referências quebradas.
7. `cd operators && go test ./...` — confirmar zero referências quebradas.
8. Update docs (architecture.md, research-basis.md, improvement-roadmap.md).
9. CHANGELOG entry.
10. Commit `refactor(pkg): delete pkg/health dead code per ADR-0028`.

#### TDD
```
RED:     N/A (delete code; no behavior to test). Verification is "tests still pass post-delete."
GREEN:   N/A.
REFACTOR: None expected.
VERIFY:  cd pkg && go test ./... && cd ../api && go test ./... && cd ../operators && go test ./...
```

#### Acceptance Criteria
- [ ] `pkg/health/` directory removed.
- [ ] `find /home/paulo/loopzero -name "*.go" -exec grep -l "pkg/health" {} \;` retorna apenas docs ou nada.
- [ ] All Go test suites GREEN.
- [ ] `architecture.md`, `research-basis.md`, `improvement-roadmap.md` atualizados.
- [ ] CHANGELOG entry committed.
- [ ] Pass: /code-audit lint check (zero warnings).

#### DoD
- [ ] Todos os files acima committed.
- [ ] `task lint` + `task test:unit` GREEN.
- [ ] PR description references ADR-0028.

---

### T1.2 — Hardenize `pkg/jwt` para RFC 8725bis

#### Objective
Implementar algorithm whitelist explícito, `typ` validation, alg case-sensitive verification em `pkg/jwt/validator.go`.

#### Evidence
- `pkg/jwt/validator.go:78-99` faz validação ad-hoc de signing method via type-assertion.
- RFC 8725 §3.1 + draft-ietf-oauth-rfc8725bis-04 §2.1, §2.6 obrigam practices não implementadas.
- Algorithm confusion attack PortSwigger é exploitable hoje contra deployments com `alg=HS256` injected (mitigation atual: type-assertion bloqueia HS256, mas frágil — case-sensitive miss seria suficiente).

#### Files to edit
```
pkg/jwt/validator.go — adicionar WithValidMethods + typ validation
pkg/jwt/validator_test.go — adicionar 4 RED tests para attack vectors
CHANGELOG.md — entry "[Unreleased] Security: pkg/jwt RFC 8725bis hardening (algorithm whitelist, typ validation). Implements ADR-0029."
```

#### Deep file dependency analysis
- `pkg/jwt/validator.go` — chamado por `api/internal/middleware/auth.go` (jwt validation), `cloud/internal/auth/session.go` (session token issuance). Mudança é additive — adicionar opção mais estrita ao parser; consumers não mudam.
- `pkg/jwt/validator_test.go` — 915 LOC já existem. Adicionar 4 testes não impacta cobertura.
- Nenhum consumer downstream precisa mudar — opção é interna ao Validator.

#### Deep Dives
- **Algorithm whitelist via stdlib jwt/v5:**
  ```go
  goJWT.WithValidMethods([]string{"RS256"})
  ```
  Esta opção REJEITA tokens com `alg` fora da lista DURANTE parse, antes de keyFunc — é defense-in-depth contra HS256 injection.

- **`typ` header validation:**
  ```go
  if typ, hasTyp := token.Header["typ"].(string); hasTyp && typ != "" && typ != "JWT" && typ != "at+jwt" {
      return nil, fmt.Errorf("unexpected typ header: %q", typ)
  }
  ```
  Hydra emite tokens com `typ=JWT` (RFC 7519) ou `typ=at+jwt` (RFC 9068 OAuth profile). Outros valores indicam token de outro propósito → rejeitar.

- **Alg case-sensitive (rfc8725bis-04 §2.1):**
  golang-jwt v5 já trata case-sensitive internamente, mas `WithValidMethods` reforça explicitness.

- **Session tokens (HMAC):**
  `SessionTokenIssuer.validateWithSecret` (linha 198) também precisa do whitelist — `WithValidMethods([]string{"HS256"})`. Ergonomia: const `AllowedAccessAlgs = []string{"RS256"}`, `AllowedSessionAlgs = []string{"HS256"}` no top do arquivo.

- **Edge case — tokens sem `typ`:** RFC 7519 não OBRIGA `typ`; existing tokens sem `typ` devem passar (backward compat). Lógica: `if hasTyp && typ != "" && typ not in allowlist` rejeita; ausente = ok.

#### Tasks
1. Definir constants `AllowedAccessAlgs`, `AllowedSessionAlgs` no top de `validator.go`.
2. Adicionar `goJWT.WithValidMethods(AllowedAccessAlgs)` em `Validate()` (linha ~99).
3. Adicionar `goJWT.WithValidMethods(AllowedSessionAlgs)` em `validateWithSecret()` (linha ~204).
4. Adicionar bloco `typ` validation pós-parse em ambas funções.
5. Atualizar `validator_test.go` com 4 testes RED (HS256-com-public-key rejection, `none` algo rejection, `typ=evil` rejection, baseline RS256 ainda funciona).
6. Confirmar tests passing GREEN.
7. CHANGELOG entry.
8. Commit `security(pkg/jwt): RFC 8725bis algorithm whitelist + typ validation per ADR-0029`.

#### TDD
```
RED:     test_validate_rejects_hs256_with_public_key_as_secret() — token "alg=HS256" assinado com a public key como secret deve falhar
RED:     test_validate_rejects_none_algorithm() — token "alg=none" deve falhar mesmo sem signature check
RED:     test_validate_rejects_unexpected_typ() — token "typ=evil" deve falhar com mensagem clara
RED:     test_validate_session_rejects_rs256_token() — session validator (HS256-only) deve rejeitar RS256
GREEN:   Adicionar WithValidMethods + typ block. Tests passam.
REFACTOR: Extrair `validateTypHeader(token *goJWT.Token) error` se shared between Validate + ValidateSession.
VERIFY:  cd pkg/jwt && go test ./...
```

#### Acceptance Criteria
- [ ] `goJWT.WithValidMethods([]string{"RS256"})` presente em `Validate()`.
- [ ] `goJWT.WithValidMethods([]string{"HS256"})` presente em `validateWithSecret()`.
- [ ] `typ` header validation rejeita valores fora `{"", "JWT", "at+jwt"}`.
- [ ] 4 novos tests GREEN.
- [ ] `pkg/jwt/validator_test.go` cobertura ≥ 90%.
- [ ] Pass: /code-audit complexity check (cyclomatic complexity <= 10).
- [ ] Pass: /code-audit coverage check (coverage >= 90%).
- [ ] Pass: /code-audit lint check (zero warnings).

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry committed.
- [ ] PR references ADR-0029.

---

### T1.3 — Adicionar validators a `pkg/id`

#### Objective
Expor `IsValidBuildID`, `IsValidReleaseID`, `IsValidPreviewID`, `IsValidAppAttestationID` para eliminar parsing duplicado em consumidores.

#### Evidence
- `pkg/id/id.go:14-39` gera 4 prefixos sem expor validators.
- `grep -r "build-\[0-9a-f\]\|HasPrefix(.\+, \"build-\"\|HasPrefix(.\+, \"rel-\"" --include="*.go" api/` mostra ≥ 6 sites com regex/HasPrefix inline.

#### Files to edit
```
pkg/id/id.go — adicionar 4 validators + 1 regex pre-compiled per prefix
pkg/id/id_test.go — 8 testes (1 valid + 1 invalid per prefix)
api/internal/db/build_store.go — substituir `strings.HasPrefix(s, "build-")` por `id.IsValidBuildID(s)` quando aplicável
api/internal/db/release_store.go — idem para release
CHANGELOG.md — entry "[Unreleased] Added: pkg/id validators (IsValidBuildID, IsValidReleaseID, IsValidPreviewID, IsValidAppAttestationID)."
```

#### Deep file dependency analysis
- `pkg/id/id.go` — ganha 4 funções públicas + 4 vars `*regexp.Regexp` package-level. Existing API preservada.
- `pkg/id/id_test.go` — adiciona 8 cases.
- `api/internal/db/build_store.go` e `release_store.go` — locais com parsing manual identificados via grep. Migrar é trivial (1 linha cada). Localizar via `grep -nE 'HasPrefix\(.+, "(build|rel|prev|raa)-"\)'`.

#### Deep Dives
- **Regex compilation:** `var validBuildIDRe = regexp.MustCompile(\`^build-[0-9a-f]{16}$\`)`. Const tempo de boot, zero alocação por chamada (compiled once).
- **Por que não Parse functions?** YAGNI. IDs são opacos para business logic; só validators são usados hoje. Adicionar `Parse` só se houver consumer real.
- **Migration de consumers:** opcional em T1.3 (deixa core change minimal). Migration full em PR separado se convier.

#### Tasks
1. Adicionar 4 vars `validXxxIDRe` em `pkg/id/id.go`.
2. Adicionar 4 funções `IsValidXxxID(s string) bool` que retornam `validXxxIDRe.MatchString(s)`.
3. Adicionar 8 testes em `id_test.go` (cada validator: 1 case valid, 1 invalid).
4. Migrar 2 consumers em `api/internal/db/` (build_store, release_store) substituindo HasPrefix por validator.
5. Confirmar `cd pkg && go test ./...` GREEN; `cd api && go test ./...` GREEN.
6. CHANGELOG entry.
7. Commit `feat(pkg/id): add IsValid validators per ADR-0028 supporting facilities`.

#### TDD
```
RED:     test_is_valid_build_id_accepts_canonical_format() — "build-abcdef0123456789" returns true
RED:     test_is_valid_build_id_rejects_wrong_prefix() — "rel-abcdef0123456789" returns false
RED:     test_is_valid_build_id_rejects_short_hex() — "build-abc" returns false
RED:     test_is_valid_build_id_rejects_uppercase_hex() — "build-ABCDEF0123456789" returns false (RFC 1123-ish discipline)
RED:     (idem para Release, Preview, AppAttestation — total 8 RED)
GREEN:   Implementar validators + regex constants.
REFACTOR: Extract helper `validateHexID(s, prefix string, n int) bool` se reduzir LOC.
VERIFY:  cd pkg/id && go test ./... -v
```

#### Acceptance Criteria
- [ ] 4 validators exportados.
- [ ] 8 testes GREEN.
- [ ] 2 consumers em `api/internal/db/` migrados.
- [ ] Pass: /code-audit coverage check (>= 95% em pkg/id).
- [ ] Pass: /code-audit complexity check (<= 5 cyclomatic).

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry committed.
- [ ] No regression em api tests.

---

### T1.4 — JWKS fetch via `pkg/circuitbreaker` (eat-your-own-dogfood)

#### Objective
Wrap `pkg/jwt.fetchJWKS` em circuit breaker para tolerar Hydra outages sem cascading auth failures.

#### Evidence
- `pkg/jwt/validator.go:249-287` faz HTTP GET direto contra JWKS endpoint.
- LoopZero já tem `pkg/circuitbreaker` mas `pkg/jwt` não usa — DRY violation cross-pkg, gate PKG-G16 NOT_MET.
- Hydra outage em produção (CHG-2026-04-30 multi-cell) demonstrou que JWKS fetch timeout serializa todas chamadas authenticated.

#### Files to edit
```
pkg/jwt/validator.go — wrap fetchJWKS em circuit breaker; adicionar grace period para keys expiradas
pkg/jwt/validator_test.go — 3 testes (CB open, CB half-open recovery, stale-keys grace)
api/cmd/main.go — passar circuit breaker config ao Validator
CHANGELOG.md — entry "[Unreleased] Changed: pkg/jwt JWKS fetch protected by pkg/circuitbreaker (graceful degradation on Hydra outage). Implements ADR-0029 + gate PKG-G16."
```

#### Deep file dependency analysis
- `pkg/jwt/validator.go` — `Validator` struct ganha campo `cb *circuitbreaker.Breaker`. Construtor `NewValidator` aceita `cb` opcional (nil = sem CB, mantendo backward compat para callers existentes durante deprecation window).
- `api/cmd/main.go` — uma linha adicional: `cb := circuitbreaker.New(circuitbreaker.Config{Name: "hydra-jwks", OpenThreshold: 5, RetryInterval: 30 * time.Second}, nil)` e passar para `NewValidator`.
- Tests usam `*httptest.Server` controlando 503 responses.

#### Deep Dives
- **Estratégia de graceful degradation:**
  - CB Closed: fetch JWKS normally, cache ttl 5min.
  - CB Open: REUSE cached keys mesmo expirados (dentro de grace window de 1h). Log WARN. Não bloqueia auth.
  - CB Open + cache empty + grace expired: retorna `ErrJWKSUnavailable` → 503.
- **Circuit breaker config rationale:**
  - `OpenThreshold = 5` — tolera blip transiente.
  - `RetryInterval = 30s` — Hydra restart típico < 30s.
  - `CloseThreshold = 1` — single success readmite.
- **Edge cases:**
  - JWKS endpoint OK mas retorna body vazio: count como failure.
  - JWKS endpoint OK mas TLS handshake falhou: count como failure.
  - JWKS endpoint OK mas decode falhou: count como failure (configuração inválida).
  - JWKS endpoint OK e novo set válido: count success.

#### Tasks
1. Adicionar campo `cb *circuitbreaker.Breaker` a `Validator`.
2. Modificar `NewValidator` para aceitar CB opcional via `WithCircuitBreaker(cb)` option.
3. Modificar `fetchJWKS` para chamar `cb.PreCheck()` antes de HTTP, `cb.RecordResult(err)` após.
4. Adicionar `staleGracePeriod` field + lógica em `getKey` para usar cache stale durante CB open.
5. Adicionar 3 RED tests (CB open block fetch; CB half-open recover; stale grace serves cached).
6. Update `api/cmd/main.go` para wireup.
7. CHANGELOG entry.
8. Commit `feat(pkg/jwt): protect JWKS fetch with circuit breaker per ADR-0029 + PKG-G16`.

#### TDD
```
RED:     test_validate_uses_stale_cache_when_cb_open() — JWKS server retorna 503; cached keys ainda válidas; auth passes; CB rec failure
RED:     test_validate_returns_error_when_cb_open_and_no_cache() — JWKS down + cache empty → ErrJWKSUnavailable
RED:     test_cb_recovers_after_jwks_endpoint_back() — após retry interval, CB half-open faz probe; success → closed
GREEN:   Implementar CB integration.
REFACTOR: Extract `(v *Validator) fetchOrServeStale(kid string) (*rsa.PublicKey, error)` helper.
VERIFY:  cd pkg/jwt && go test ./... -race
```

#### Acceptance Criteria
- [ ] `Validator` integra `pkg/circuitbreaker`.
- [ ] Stale cache served durante CB open (max 1h grace).
- [ ] 3 testes GREEN com `httptest.Server` simulando outage.
- [ ] api/cmd/main.go passa CB ao construtor.
- [ ] Race detector clean (`go test -race`).
- [ ] Pass: /code-audit lint + coverage.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.
- [ ] Manual smoke: stop Hydra container, confirm api ainda autentica via cached keys.

---

### T1.5 — Tipar `naming.Labels` via `StandardLabels` struct

#### Objective
Substituir `naming.Labels(tenant, project, env) map[string]string` por `naming.StandardLabels{...}.AsMap()` typed.

#### Evidence
- `pkg/naming/naming.go:240-249` retorna map com chaves hardcoded "loopzero.io/tenant", "loopzero.io/project" etc.
- Risk: typo `labels["loopzero.io/tenent"]` em consumer não detectado em compile-time.
- Selectors em `operators/internal/controllers/` usam essas chaves textualmente — mismatch silencioso.

#### Files to edit
```
pkg/naming/naming.go — adicionar type StandardLabels + constants + AsMap()
pkg/naming/naming_test.go — 3 testes (AsMap shape, constants stable, deprecation alias still works)
api/internal/services/* (multiple) — migrar para StandardLabels onde aplicável (incrementally)
operators/internal/controllers/* — idem
CHANGELOG.md — entry "[Unreleased] Added: naming.StandardLabels typed struct + label constants. Deprecated: naming.Labels (use AsMap)."
```

#### Deep file dependency analysis
- `pkg/naming/naming.go` — adicionar:
  ```go
  const (
      LabelTenant      = "loopzero.io/tenant"
      LabelProject     = "loopzero.io/project"
      LabelEnvironment = "loopzero.io/environment"
      LabelManagedBy   = "loopzero.io/managed-by"
  )
  type StandardLabels struct {
      Tenant, Project, Environment string
  }
  func (l StandardLabels) AsMap() map[string]string { ... }
  ```
- `Labels()` antigo PERMANECE com `// Deprecated:` comment, internamente delega para `StandardLabels{...}.AsMap()`.
- Consumers podem migrar oportunisticamente.

#### Deep Dives
- **Por que struct + AsMap em vez de fluent map?** Compile-time typo detection. Programmer escreve `StandardLabels{Tenant: t, Projet: p}` (typo) → compiler erra.
- **Constants exportadas:** Selectors podem usar `naming.LabelTenant` em vez de string literal `"loopzero.io/tenant"`. Auditável via grep.
- **Backward compat:** `Labels(t, p, e)` permanece. Migration é PR separado.

#### Tasks
1. Adicionar constants + StandardLabels + AsMap em `pkg/naming/naming.go`.
2. Marcar `Labels()` antigo como `// Deprecated: Use StandardLabels{...}.AsMap()`.
3. Tests: AsMap shape + constants stable + deprecated alias delegates.
4. Migration opcional de 3-5 callsites em api/operators (escolher os mais critical para valor demonstrativo).
5. CHANGELOG entry.
6. Commit `feat(pkg/naming): introduce StandardLabels typed struct per gate PKG-G02`.

#### TDD
```
RED:     test_standard_labels_as_map_returns_canonical_keys() — verifica all 3 keys present + correct values
RED:     test_label_constants_match_canonical() — LabelTenant == "loopzero.io/tenant" (regression guard)
RED:     test_deprecated_labels_function_still_returns_compatible_map() — antiga API funciona
GREEN:   Implementar.
REFACTOR: None expected.
VERIFY:  cd pkg/naming && go test ./...
```

#### Acceptance Criteria
- [ ] 4 constants `Label*` exportadas.
- [ ] `StandardLabels` struct + `AsMap()` exportados.
- [ ] `Labels()` marcado deprecated.
- [ ] 3 testes GREEN.
- [ ] No breaking changes nos consumers existentes.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.
- [ ] Pass: /code-audit checks.

---

### T1.6 — Capturar stack trace em `errors.Wrap`

#### Objective
Adicionar `Stack []runtime.Frame` capture lazy em `LoopZeroError.Wrap`. Expor `StackTrace() string` para logging estruturado.

#### Evidence
- `pkg/errors/errors.go:64-72` faz wrap mas perde call context.
- Production debugging em iter 12 dogfood (CHG-2026-04-29) foi 3x mais lento porque wrappers genéricos `[INTERNAL_ERROR]` mascaravam origem.
- cockroachdb/errors pattern proves stack capture é zero-cost se não usado.

#### Files to edit
```
pkg/errors/errors.go — adicionar campo Stack + WithStack option + StackTrace method
pkg/errors/errors_test.go — 4 testes (capture, format, no-capture-when-not-requested, ToJSON omits stack)
CHANGELOG.md — entry "[Unreleased] Added: errors.WithStack option captures call stack for debugging."
```

#### Deep file dependency analysis
- `pkg/errors/errors.go` — `LoopZeroError` struct ganha `Stack []uintptr` (programa counters; resolve para Frames sob demanda em StackTrace()).
- `Wrap(context string)` recebe variant `WrapWithStack(context string)` que captura.
- `StackTrace() string` formats via `runtime.CallersFrames`.
- `ToJSON()` continua omitindo Stack (não vaza em API responses).
- Consumers existentes não mudam — feature é opt-in.

#### Deep Dives
- **Custo:** `runtime.Callers(skip, pcs)` com pcs `[32]uintptr` é ~150ns. Lazy resolve via CallersFrames só quando StackTrace() é chamado.
- **Semantics:** capture na PRIMEIRA wrap (preserve origem). Subsequent wraps NÃO recapture.
- **Logging integration:** `slog.Error("msg", "stack", err.StackTrace())` natural.
- **Edge case:** LoopZeroError sentinel (var ErrLockTimeout = ...) sem Wrap não tem stack — that's ok, sentinels são identidade.

#### Tasks
1. Adicionar campo `Stack []uintptr` em `LoopZeroError`.
2. Adicionar método `(e *LoopZeroError) StackTrace() string` que resolve via runtime.CallersFrames.
3. Adicionar `(e *LoopZeroError) WrapWithStack(context string) *LoopZeroError` que copy + capture stack se ainda nil.
4. Tests: capture works, multi-wrap não duplica, sentinel sem capture, ToJSON omits.
5. CHANGELOG entry.
6. Commit `feat(pkg/errors): optional stack capture via WrapWithStack per gate PKG-G08`.

#### TDD
```
RED:     test_wrap_with_stack_captures_call_site() — StackTrace() contém nome da função teste
RED:     test_wrap_without_stack_returns_empty_stack_trace() — StackTrace() retorna ""
RED:     test_to_json_omits_stack_field() — ToJSON() output não tem "stack" key
RED:     test_double_wrap_with_stack_preserves_first_capture() — segundo Wrap não sobrescreve
GREEN:   Implementar.
REFACTOR: Extract `captureStack(skip int) []uintptr` helper.
VERIFY:  cd pkg/errors && go test ./... -v
```

#### Acceptance Criteria
- [ ] WrapWithStack captura ≤ 32 frames.
- [ ] StackTrace() format human-readable.
- [ ] ToJSON omits stack.
- [ ] 4 testes GREEN.
- [ ] Backward compat preservado.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.
- [ ] Pass: /code-audit.

---

### T1.7 — Doc/code alignment + `task pkg:usage-matrix` (gate PKG-G14)

#### Objective
Criar script que regenera Real Usage Matrix em `architecture.md` automaticamente; CI step que falha se doc estiver out-of-date.

#### Evidence
- Pesquisa SOTA detectou drift (`pkg/health` 0 imports mas docs descrevem como flagship).
- Gate PKG-G14 introduzido — precisa de evidence automatizado.

#### Files to edit
```
infra/scripts/pkg-usage-matrix.sh (NEW) — gera tabela markdown
taskfiles/dev.yml — adicionar task `pkg:usage-matrix`
.github/workflows/ci.yml — step "validate pkg usage matrix"
docs/technical/pkg/architecture.md — auto-gerada section bracketed por <!-- USAGE-MATRIX-START --> <!-- USAGE-MATRIX-END -->
CHANGELOG.md — entry "[Unreleased] Added: task pkg:usage-matrix CI gate enforcing doc/code alignment."
```

#### Deep file dependency analysis
- `infra/scripts/pkg-usage-matrix.sh` — bash script: itera `pkg/*/`, faz `grep -r "pkg/X" --include="*.go"`, conta por top-level (api, operators, cloud), gera markdown table.
- `taskfiles/dev.yml` — adiciona task simples invocando o script com flag `--check` (CI) ou `--write` (local).
- `.github/workflows/ci.yml` — step `task pkg:usage-matrix -- --check` que diff e falha se mudou.
- `architecture.md` — primeira execução commits a tabela atual; PRs subsequentes que mudem imports precisam regenerar.

#### Deep Dives
- **Script semantics:**
  - `--write` mode: regenera section, escreve arquivo. Local dev.
  - `--check` mode: regenera em buffer, diff contra arquivo, exit 1 if differ. CI.
- **Rationale do markup HTML:** sed-based replacement preserva o resto do arquivo; permite humans escreverem prosa fora dos markers.
- **Edge case:** novo pacote em `pkg/` aparece automaticamente. Pacote deletado some.

#### Tasks
1. Criar `infra/scripts/pkg-usage-matrix.sh` (~80 LOC bash).
2. Adicionar task `pkg:usage-matrix` em `taskfiles/dev.yml`.
3. Editar `architecture.md` — envolver Real Usage Matrix em markers HTML.
4. Rodar `task pkg:usage-matrix -- --write` para regenerar.
5. Commit script + initial table.
6. Adicionar CI step em `.github/workflows/ci.yml`.
7. CHANGELOG entry.
8. Commit `feat(infra): pkg usage matrix automation per gate PKG-G14`.

#### TDD
```
RED:     test_usage_matrix_check_fails_on_drift() — manual: insert fake import, run --check, expect exit 1
RED:     test_usage_matrix_write_idempotent() — run --write twice, second produces no diff
GREEN:   Implementar script.
REFACTOR: None expected.
VERIFY:  bash infra/scripts/pkg-usage-matrix.sh --check && bash infra/scripts/pkg-usage-matrix.sh --write && git diff --exit-code docs/technical/pkg/architecture.md
```

#### Acceptance Criteria
- [ ] Script idempotente quando código não muda.
- [ ] Detect drift (manual test).
- [ ] CI step adicionado.
- [ ] architecture.md tem markers HTML stable.
- [ ] task command works.

#### DoD
- [ ] Script committed.
- [ ] CI workflow updated.
- [ ] CHANGELOG entry.

---

## Phase 2: Modernization (Sprint 9 Targets)

**Objective:** 7 mudanças de fôlego médio que modernizam `pkg/` para padrões 2025-2026 (generics, kstatus, RFC 9457, no-global-state, property-based).

### T2.1 — Migrar `pkg/circuitbreaker` para generics + context

#### Objective
Nova API `circuitbreaker.New[T any](cfg)` com `Execute(ctx, fn)` retornando `(T, error)`. v1 API marcada Deprecated mas funcional por 1 release.

#### Evidence
- `sony/gobreaker v2` é de-facto Go standard — generics + context cancellation desde 2024.
- 5 importadores existentes (api/internal/services/workflow_circuit_breaker.go, etc) podem migrar incrementally.
- Cancellation propagation evita falsa-saturação durante deploy cancelado.

#### Files to edit
```
pkg/circuitbreaker/circuitbreaker.go — rewrite para generics; manter v1 API marcada Deprecated
pkg/circuitbreaker/circuitbreaker_test.go — adicionar tests v2 generics + context cancellation
api/internal/services/workflow_circuit_breaker.go — migrar para v2 (exemplo)
api/internal/services/pg_circuit_breaker.go — idem
pkg/jwt/validator.go — migrar JWKS CB para v2 (após T1.4 ter usado v1)
CHANGELOG.md — entry "[Unreleased] Added: circuitbreaker.Breaker[T] generic + context-aware. Deprecated: legacy PreCheck/RecordResult API per ADR-0031."
```

#### Deep file dependency analysis
- `pkg/circuitbreaker/circuitbreaker.go` — recebe novo type `Breaker[T any]`. Exemplo:
  ```go
  type Breaker[T any] struct { ... } // shadow type-param-free version internally
  func (b *Breaker[T]) Execute(ctx context.Context, fn func(ctx context.Context) (T, error)) (T, error)
  ```
  v1 `Breaker` permanece como alias para `Breaker[any]` ou parallel struct durante deprecation.
- 5 importadores migrados em ordem: pg_circuit_breaker → workflow_circuit_breaker → jwt → outros.
- Tests novos validam: cancellation excludes from failures, generic return type, deprecated API still works.

#### Deep Dives
- **Sintaxe migration:**
  - Antes: `if err := cb.PreCheck(); err != nil { ... }; result, err := doStuff(); cb.RecordResult(err); ...`
  - Depois: `result, err := cb.Execute(ctx, func(ctx context.Context) (Result, error) { return doStuff(ctx) })`
- **`IsExcluded(err)` config option:**
  ```go
  type Config struct {
      ...
      IsExcluded func(error) bool // optional: errors that don't count as failure
  }
  ```
  Default: exclude `context.Canceled`, `context.DeadlineExceeded`.
- **State callback signature:** mantém `func(name string, from, to State)` — não ganha types-params.
- **Edge case:** Generic em `T = any` funciona como before. `T = struct{}` para void operations.

#### Tasks
1. Renomear `Breaker` para `breakerImpl` (não-exportado).
2. Criar `Breaker[T any]` exportado wrapping breakerImpl.
3. Implementar `Execute(ctx, fn)`.
4. Adicionar `IsExcluded` field em Config com default sensível.
5. Adicionar deprecation comments em PreCheck/RecordResult.
6. Tests v2: 6 novos cases.
7. Migrate pg_circuit_breaker.go, workflow_circuit_breaker.go.
8. Migrate jwt/validator.go (de v1 que T1.4 introduziu para v2).
9. CHANGELOG entry.
10. Commit `refactor(pkg/circuitbreaker): generic + context-aware per ADR-0031`.

#### TDD
```
RED:     test_execute_returns_typed_result() — `result, err := cb.Execute(ctx, fn)` retorna T concrete
RED:     test_execute_propagates_context_cancellation() — ctx.Cancel(); fn vê ctx.Err
RED:     test_execute_excludes_canceled_from_failure_count() — cancelled requests não contam
RED:     test_execute_excludes_deadline_exceeded_from_failure_count() — timeout do ctx não conta
RED:     test_execute_open_state_returns_err_circuit_open() — após OpenThreshold falhas, ErrCircuitOpen
RED:     test_legacy_precheck_recordresult_still_works() — backward compat
GREEN:   Implementar generics + context.
REFACTOR: Internal breakerImpl share state; Breaker[T] é wrapper genérico.
VERIFY:  cd pkg/circuitbreaker && go test ./... -race && cd ../../api && go test ./...
```

#### Acceptance Criteria
- [ ] `Breaker[T any]` exportado.
- [ ] `Execute(ctx, fn)` propaga cancellation.
- [ ] `IsExcluded` default ignora context errors.
- [ ] v1 API funcional.
- [ ] 6+ testes GREEN com -race.
- [ ] 3+ migrations done in api/internal/services e pkg/jwt.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.
- [ ] Pass: /code-audit.

---

### T2.2 — Introduzir `pkg/conditions` kstatus-compatible

#### Objective
Novo pacote `pkg/conditions` substituindo o conceito de Phase aggregation com `metav1.Condition` model. 8 operators migram para usá-lo.

#### Evidence
- 8 controllers em `operators/internal/controllers/` reconciliam conditions ad-hoc.
- kstatus + Operator SDK + Kubebuilder oficialmente recomendam `meta.SetStatusCondition`/`meta.IsStatusConditionTrue`.
- ADR D5 ratifica.

#### Files to edit
```
pkg/conditions/conditions.go (NEW) — wrappers around metav1
pkg/conditions/conditions_test.go (NEW) — 12+ testes
pkg/conditions/aggregate.go (NEW) — AggregateToPhase + Ready/Stalled/Reconciling helpers
pkg/conditions/aggregate_test.go (NEW)
operators/internal/controllers/tenant_controller.go — migrar
operators/internal/controllers/environment_controller.go — migrar
(... 6 outros controllers — incrementally)
CHANGELOG.md — entry "[Unreleased] Added: pkg/conditions kstatus-compatible package per ADR-0032 + gate PKG-G16."
```

#### Deep file dependency analysis
- `pkg/conditions/conditions.go` — re-exporta `metav1.Condition`; provê:
  ```go
  const (
      ConditionReady       = "Ready"
      ConditionReconciling = "Reconciling"
      ConditionStalled     = "Stalled"
  )
  func SetCondition(conds *[]metav1.Condition, name, reason, message string, status metav1.ConditionStatus) bool
  func IsReady(conds []metav1.Condition) bool
  func IsStalled(conds []metav1.Condition) bool
  func IsReconciling(conds []metav1.Condition) bool
  ```
- `aggregate.go` — `AggregateToPhase(conds, observedGen, currentGen) Phase` retornando string entre `Pending|Provisioning|Ready|Stalled`. Phase enum local ou exposed?
- 8 controllers migram em ordem definida; cada um é seu próprio commit. Em T2.2 fazer 2-3 controllers (tenant + environment); restantes em follow-ups.

#### Deep Dives
- **Polaridade abnormal-true (kstatus):** ausência de Stalled = OK. Stalled=True = problema. Reconciling=True = trabalho em progresso. Ready=True = fully reconciled.
- **observedGeneration discipline:** AggregateToPhase recebe `observedGen, currentGen`. Se `observedGen < currentGen` → Provisioning (controller ainda não viu spec change).
- **Why not import sigs.k8s.io/cli-utils/pkg/kstatus directly?** kstatus tem dep pesada. LoopZero wrappers são ~50 LOC e cobrem 95% do uso. Mantém pkg/conditions com 1 dep externa (apimachinery, já presente).
- **Migration template per controller:**
  - Antes: `cond := metav1.Condition{Type: "Ready", ...}; meta.SetStatusCondition(&status.Conditions, cond)`
  - Depois: `conditions.SetCondition(&status.Conditions, conditions.ConditionReady, "FullyReconciled", "all subresources ready", metav1.ConditionTrue)`

#### Tasks
1. Criar `pkg/conditions/conditions.go` + tests.
2. Criar `pkg/conditions/aggregate.go` + tests.
3. Migrar `tenant_controller.go` para usar wrappers.
4. Migrar `environment_controller.go` para usar wrappers.
5. Documentar migration pattern em `docs/technical/pkg/conditions.md` (NEW).
6. CHANGELOG entry.
7. Commit `feat(pkg/conditions): kstatus-compatible conditions package per ADR-0032`.

#### TDD
```
RED:     test_set_condition_inserts_new_condition() — empty list → 1 condition após Set
RED:     test_set_condition_updates_existing_by_type() — existing Ready=False → Ready=True (no duplication)
RED:     test_set_condition_updates_last_transition_time_on_status_change() — only when status changed
RED:     test_is_ready_returns_true_only_when_ready_true() — anti-confusion
RED:     test_aggregate_returns_pending_when_observed_lt_current_gen() — observedGen=1, currentGen=2
RED:     test_aggregate_returns_ready_when_all_conditions_normal() — empty conditions OR Ready=True
RED:     test_aggregate_returns_stalled_when_stalled_true() — abnormal-true polarity
RED:     test_tenant_controller_uses_pkg_conditions_helpers() — integration in operators
RED:     (3-4 mais para edge cases)
GREEN:   Implementar pkg/conditions.
REFACTOR: Extract const ReasonNotFound, ReasonError, ReasonTimeout if shared.
VERIFY:  cd pkg/conditions && go test ./... -v && cd ../../operators && go test ./...
```

#### Acceptance Criteria
- [ ] `pkg/conditions/conditions.go` exporta SetCondition + Is* helpers.
- [ ] `pkg/conditions/aggregate.go` exporta AggregateToPhase.
- [ ] 12+ testes GREEN.
- [ ] 2 controllers migrados (tenant + environment) com green envtests.
- [ ] Migration guide em `docs/technical/pkg/conditions.md`.
- [ ] Pass: /code-audit.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.
- [ ] PR references ADR-0032.

---

### T2.3 — Desacoplar `LoopZeroError` de HTTP transport

#### Objective
Remover campo `HTTPStatus` de `LoopZeroError`. Mover mapeamento Code→HTTPStatus para `pkg/errors/http.Status(err)`. Helper `pkg/errors/http.WriteJSON(w, err)` emite RFC 9457.

#### Evidence
- ADR D3 + gate PKG-G15.
- 70 importadores de `pkg/errors`; subset que acessa HTTPStatus identificável via grep.

#### Files to edit
```
pkg/errors/errors.go — remover field HTTPStatus do struct (BREAKING CHANGE pre-release)
pkg/errors/errors_test.go — atualizar tests
pkg/errors/http/mapping.go (NEW) — Status(err) lookup table
pkg/errors/http/mapping_test.go (NEW)
pkg/errors/http/writer.go (NEW) — WriteJSON RFC 9457
pkg/errors/http/writer_test.go (NEW)
api/internal/middleware/error_handler.go — usar errorshttp.WriteJSON
(múltiplos handlers em api/internal/routes que acessam HTTPStatus direto) — migrar
CHANGELOG.md — entry "[Unreleased] BREAKING: LoopZeroError.HTTPStatus removed; use errorshttp.Status(err). RFC 9457 problem+json via errorshttp.WriteJSON. Implements ADR-0030 + gate PKG-G15."
```

#### Deep file dependency analysis
- `pkg/errors/errors.go` — LoopZeroError loses HTTPStatus field. Sentinels (ErrLockTimeout, ErrTenantNotFound, ...) lose HTTPStatus init.
- `pkg/errors/http/mapping.go` — package-level `var statusByCode = map[string]int{ "LOCK_TIMEOUT": 409, "TENANT_NOT_FOUND": 404, ... }`.
- `Status(err) int` faz `errors.As` para extrair Code, lookup, fallback 500.
- `WriteJSON(w, err)` emite `{"type": "...", "title": "...", "status": int, "detail": "...", "code": "..."}` per RFC 9457.
- BREAKING: callers que faziam `loopzeroErr.HTTPStatus` precisam migrar para `errorshttp.Status(loopzeroErr)`.
- Pre-release: aceitável (CHANGELOG documenta + CHG meeting decisão registrada).

#### Deep Dives
- **RFC 9457 schema example:**
  ```json
  {
    "type": "https://loopzero.example/errors/lock-timeout",
    "title": "Lock Timeout",
    "status": 409,
    "detail": "could not acquire environment lock within timeout",
    "code": "LOCK_TIMEOUT",
    "instance": "/projects/foo/environments/staging"
  }
  ```
- **`type` URI strategy:** convencionar `https://loopzero.example/errors/{kebab-case-code}`. Pode ser served estaticamente em CF Worker em fase 2.
- **Migration path:**
  - Phase 2a: introduzir `errorshttp.Status` + helper `WriteJSON`.
  - Phase 2b: BREAKING delete `HTTPStatus` field + migrar callers.
  - Phase 2c: emitir RFC 9457 wire format default em `WriteJSON`.

#### Tasks
1. Criar `pkg/errors/http/mapping.go` com lookup table inicial baseada em valores HTTPStatus existentes.
2. Criar `pkg/errors/http/writer.go` emitindo RFC 9457 problem+json.
3. Tests para ambos.
4. Migrar `api/internal/middleware/error_handler.go` para usar errorshttp.WriteJSON.
5. Grep todos `loopzeroErr.HTTPStatus`/`HTTPStatus:` patterns. Migrar para errorshttp.Status.
6. Remover field HTTPStatus de LoopZeroError. Atualizar sentinels (não são mais structs com 4 fields, mas com 3).
7. Tests update.
8. CHANGELOG (BREAKING).
9. Commit `refactor(pkg/errors): decouple HTTP transport per ADR-0030 + gate PKG-G15`.

#### TDD
```
RED:     test_status_returns_mapped_for_known_code() — Status(ErrLockTimeout) == 409
RED:     test_status_returns_500_for_unknown_code() — Status(non-LoopZeroError) == 500
RED:     test_write_json_emits_rfc9457_shape() — content-type problem+json, fields type/title/status/detail/code
RED:     test_write_json_includes_instance_when_request_present() — instance = r.URL.Path
RED:     test_loopzero_error_no_longer_has_httpstatus_field() — reflection check (compile-time too)
RED:     test_legacy_callers_migrated() — grep zero `loopzeroErr.HTTPStatus` em api/internal
GREEN:   Implementar pkg/errors/http + remove field.
REFACTOR: Extract `defaultStatus = 500`, `defaultType = "https://loopzero.example/errors/internal"`.
VERIFY:  cd pkg/errors && go test ./... && cd ../../api && go test ./... && grep -r "loopzeroErr\.HTTPStatus\|HTTPStatus:" api/internal | wc -l (== 0)
```

#### Acceptance Criteria
- [ ] `pkg/errors/http/` package created.
- [ ] `LoopZeroError.HTTPStatus` field removed.
- [ ] All consumers migrated.
- [ ] RFC 9457 problem+json shape valid.
- [ ] Pass: /code-audit.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry (BREAKING).
- [ ] PR references ADR-0030.
- [ ] Manual: curl an error endpoint, verify problem+json shape.

---

### T2.4 — Injetar `LimitsStore` em `pkg/enforcement`

#### Objective
`PlanLimiter` aceita `LimitsStore` interface. SQL implementation lives em `api/internal/db/limits_store.go`. `*sql.DB` direto removido de `pkg/enforcement`.

#### Evidence
- ADR D6.
- `pkg/enforcement/limiter.go:14-23` + `counters.go:10-21` acoplam ao driver.

#### Files to edit
```
pkg/enforcement/limiter.go — refactor para usar LimitsStore
pkg/enforcement/store.go (NEW) — interface LimitsStore
pkg/enforcement/limiter_test.go — usar in-memory mock (não go-sqlmock)
pkg/enforcement/counters.go — counters como CountFn (já são) — só atualizar para usar interface
api/internal/db/limits_store.go (NEW) — SQL adapter
api/internal/db/limits_store_test.go (NEW)
api/cmd/main.go — wireup adapter
CHANGELOG.md — entry "[Unreleased] Changed: pkg/enforcement aceita LimitsStore interface (DIP). SQL adapter em api/internal/db. Implements ADR-0033."
```

#### Deep file dependency analysis
- `pkg/enforcement/store.go` — define interface:
  ```go
  type LimitsStore interface {
      GetTenantPlan(ctx context.Context, tenantID string) (string, error)
      GetLimits(ctx context.Context, plan, metric string) (hardLimit, softLimit int, err error)
  }
  ```
- `pkg/enforcement/limiter.go` — `PlanLimiter` ganha `store LimitsStore` em vez de `db *sql.DB`. Imports: removido `database/sql`.
- `api/internal/db/limits_store.go` — implementa interface usando `*sql.DB`. Queries SQL movem para cá (eram inline em pkg/enforcement).
- `api/cmd/main.go` — `store := db.NewLimitsStore(pgDB); limiter := enforcement.NewPlanLimiter(store, logger)`.
- Tests em pkg/enforcement usam `inMemoryLimitsStore` map-based.

#### Deep Dives
- **In-memory test fake:**
  ```go
  type fakeLimitsStore struct { plans map[string]string; limits map[[2]string][2]int }
  func (f *fakeLimitsStore) GetTenantPlan(ctx, t) (string, error) { return f.plans[t], nil }
  ```
  Substitui ~991 LOC de go-sqlmock setup.
- **Migration impact:** `pkg/enforcement` go.mod loses `database/sql` import. `pkg/go.mod` continua com `golang-jwt/jwt/v5` única dep.
- **Edge case:** soft/hard limits semantics preserved exactly.

#### Tasks
1. Criar `pkg/enforcement/store.go` com interface.
2. Refactor `limiter.go` para aceitar interface.
3. Refactor `counters.go` se necessário (provável: já é fn, só ajustar wireup).
4. Reescrever `limiter_test.go` com fake store (drop go-sqlmock).
5. Criar `api/internal/db/limits_store.go` + tests com sqlmock (movido de pkg/enforcement).
6. Update `api/cmd/main.go` wireup.
7. Remove `github.com/DATA-DOG/go-sqlmock` de `pkg/go.sum` (já tinha mas só para tests).
8. CHANGELOG entry.
9. Commit `refactor(pkg/enforcement): inject LimitsStore interface per ADR-0033`.

#### TDD
```
RED:     test_check_limit_uses_store_get_tenant_plan() — fake retorna "starter", limiter respeita
RED:     test_check_limit_falls_back_starter_on_store_error() — store erro → fall back to starter
RED:     test_check_value_with_in_memory_store() — testa CheckValue inteiro com fake (zero sql)
RED:     test_pkg_enforcement_no_longer_imports_database_sql() — grep cabeçalho do package
GREEN:   Implementar interface + adapters.
REFACTOR: Extract baselineErrPolicy se duplicado.
VERIFY:  cd pkg/enforcement && go test ./... && cd ../../api && go test ./...
```

#### Acceptance Criteria
- [ ] Interface `LimitsStore` exportada.
- [ ] `pkg/enforcement` não importa `database/sql`.
- [ ] SQL adapter em `api/internal/db/limits_store.go`.
- [ ] Testes pkg/enforcement usam fake; testes db usam sqlmock.
- [ ] Pass: /code-audit.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.
- [ ] PR references ADR-0033.

---

### T2.5 — Eliminar global state em `pkg/logging`

#### Objective
Substituir `globalExtractor` por `ContextLogger` construct. `RegisterTraceExtractor` deprecated com warning.

#### Evidence
- ADR D7.
- `pkg/logging/context.go:17-20` viola Google CDK principle.

#### Files to edit
```
pkg/logging/context.go — adicionar ContextLogger; marcar RegisterTraceExtractor deprecated
pkg/logging/context_test.go — adicionar tests para ContextLogger
api/cmd/main.go — migrar para NewContextLogger
operators/cmd/*/main.go — idem (4 binaries)
CHANGELOG.md — entry "[Unreleased] Added: ContextLogger explicit constructor. Deprecated: RegisterTraceExtractor global state. Implements ADR-0034."
```

#### Deep file dependency analysis
- `pkg/logging/context.go` — adicionar:
  ```go
  type ContextLogger struct {
      base      *slog.Logger
      extractor TraceExtractor
  }
  func NewContextLogger(base *slog.Logger, extractor TraceExtractor) *ContextLogger
  func (c *ContextLogger) FromContext(ctx context.Context) *slog.Logger
  ```
  Existing `FromContext(ctx, logger)` permanece funcional via global (deprecated).
- 10 importadores migram. Padrão típico:
  - Antes: `logging.RegisterTraceExtractor(...)` em main; depois `logging.FromContext(ctx, baseLogger)` em handlers.
  - Depois: `cl := logging.NewContextLogger(baseLogger, extractor)` em main; `cl.FromContext(ctx)` em handlers.

#### Deep Dives
- **Compatibility:** durante 1 release, ambas APIs coexistem. CHANGELOG warns.
- **Race detector:** `globalExtractor` + `extractorMu` lifeless após migração; pode delete em release N+1.

#### Tasks
1. Adicionar ContextLogger struct + constructor + FromContext method.
2. Marcar global API deprecated.
3. Tests para ContextLogger (race detector clean).
4. Migrate main.go em api + 4 operators.
5. CHANGELOG entry.
6. Commit `refactor(pkg/logging): introduce ContextLogger per ADR-0034`.

#### TDD
```
RED:     test_context_logger_from_context_enriches_with_trace() — extractor invocado, logger ganha trace_id
RED:     test_context_logger_no_extractor_returns_base() — nil extractor → unchanged logger
RED:     test_two_context_loggers_isolated() — duas instâncias com extractors diferentes não conflitam
RED:     test_deprecated_global_api_still_works() — backward compat
GREEN:   Implementar.
REFACTOR: Extract enrichLogger helper.
VERIFY:  cd pkg/logging && go test ./... -race && cd ../../api && go test ./...
```

#### Acceptance Criteria
- [ ] ContextLogger exportado e funcional.
- [ ] Global state ainda funciona (deprecated).
- [ ] 4 binaries migrados.
- [ ] Pass: /code-audit + race detector.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.

---

### T2.6 — Property-based tests em `pkg/statemachine` e `pkg/naming`

#### Objective
Adicionar `testing/quick`-based property tests verificando invariantes globais.

#### Evidence
- ADR D8 + gate PKG-G18.

#### Files to edit
```
pkg/statemachine/properties_test.go (NEW) — property tests
pkg/naming/properties_test.go (NEW) — property tests
CHANGELOG.md — entry "[Unreleased] Added: property-based tests for pkg/statemachine and pkg/naming per ADR-0035 + gate PKG-G18."
```

#### Deep file dependency analysis
- `properties_test.go` em ambos: usa `testing/quick` (stdlib). Define generators para inputs aleatórios. Run 1000 iterations per property.

#### Deep Dives
- **State machine invariants:**
  ```go
  func TestProperty_TerminalStatesRejectAllEvents(t *testing.T) {
      for _, machine := range []*Machine{NewBuildMachine(), NewReleaseMachine(), ...} {
          // for each terminal state in machine.Terminals():
          //   for 1000 random events:
          //     assert TryTransition(terminal, event) returns error
      }
  }
  func TestProperty_TransitionsAreDeterministic(t *testing.T) {
      // (state, event) -> always same destination
  }
  ```
- **Naming invariants:**
  ```go
  func TestProperty_SanitizeAlwaysProducesValidOrEmpty(t *testing.T) {
      f := func(s string) bool {
          out := Sanitize(s)
          if out == "" { return true }
          return ValidateKubernetesName(out) == nil
      }
      if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil { t.Fatal(err) }
  }
  ```

#### Tasks
1. Criar `pkg/statemachine/properties_test.go` com 3-4 property tests.
2. Criar `pkg/naming/properties_test.go` com 2-3 property tests.
3. CI runs em `task test:unit` (default).
4. CHANGELOG entry.
5. Commit `test(pkg): property-based invariant tests per ADR-0035`.

#### TDD
```
RED:     N/A (property tests devem passar — descobrir bugs se falhar)
GREEN:   Implementar tests; corrigir bugs descobertos.
REFACTOR: None expected.
VERIFY:  go test -count=1 ./pkg/statemachine ./pkg/naming
```

#### Acceptance Criteria
- [ ] 5-7 property tests adicionados.
- [ ] Cada property runs ≥ 1000 iterations.
- [ ] All GREEN.
- [ ] Bugs descobertos durante implementation são fixed.

#### DoD
- [ ] All tests passing.
- [ ] CHANGELOG entry.

---

### T2.7 — Visualização de state machines

#### Objective
Adicionar `Machine.Visualize() string` retornando DOT format. CI gera SVGs em `docs/state-machines/`.

#### Evidence
- 9 state machines em `pkg/statemachine/` documented em prosa.
- looplab/fsm `.Visualize()` é trivial — DOT format é diff-friendly.

#### Files to edit
```
pkg/statemachine/visualize.go (NEW) — DOT generator
pkg/statemachine/visualize_test.go (NEW) — verifies DOT shape
infra/scripts/gen-state-machine-svgs.sh (NEW) — itera + dot -Tsvg
docs/state-machines/build.svg (NEW)
docs/state-machines/release.svg (NEW)
(... 7 outros)
.github/workflows/ci.yml — step "validate state-machines svgs up-to-date"
CHANGELOG.md — entry "[Unreleased] Added: state machine DOT/SVG visualization per gate PKG-G13 + PKG-G18."
```

#### Deep file dependency analysis
- `pkg/statemachine/visualize.go` — `func (m *Machine) Visualize() string` percorre transitions, emite DOT. Determinístico (sort transitions).
- Script bash itera state machines via `cd /tmp && cat > main.go << EOF ... EOF; go run main.go > x.dot; dot -Tsvg x.dot > svg`.
- CI step grava SVG em `docs/state-machines/`, faz git diff exit 1 se mudou.

#### Deep Dives
- **DOT determinism:** ordenar transitions por (From, Event) lex; terminals com double-circle.
- **Why not graphviz dependency em CI?** Script roda local; CI só verifica diff. Manter.

#### Tasks
1. Implement Visualize().
2. Tests verifying shape.
3. Bash script.
4. Generate initial SVGs.
5. CI step.
6. CHANGELOG.
7. Commit.

#### TDD
```
RED:     test_visualize_returns_dot_with_all_states() — every state aparece como node
RED:     test_visualize_returns_dot_with_terminal_double_circle() — terminals styled
RED:     test_visualize_is_deterministic() — same input → same output
GREEN:   Implementar.
REFACTOR: None expected.
VERIFY:  cd pkg/statemachine && go test ./... && bash infra/scripts/gen-state-machine-svgs.sh && git diff --exit-code docs/state-machines/
```

#### Acceptance Criteria
- [ ] `Machine.Visualize() string` exportado.
- [ ] 9 SVGs em `docs/state-machines/`.
- [ ] CI step ativo.
- [ ] Pass: /code-audit.

#### DoD
- [ ] All tests passing.
- [ ] SVGs committed.
- [ ] CHANGELOG entry.

---

## Phase 3: Validation & Dogfood

**Objective:** Validar que todas as mudanças pkg/ funcionam end-to-end em infra fresca, sem regressões.

### T3.1 — Integration test sweep + `task validate-matrix`

#### Objective
Rodar todos os test suites + validate matrix em pkg + api + operators + cloud, confirmar zero regressões.

#### Evidence
- 14 mudanças em `pkg/` cascataram para 89+19 importadores. Risk de regression é real.
- Validate matrix infra-normalize já cobre 10 gates offline — ampliar para incluir gates pkg.

#### Files to edit
```
infra/scripts/validate-matrix.sh — adicionar 5 gates pkg-specific
docs/runbooks/pkg-sota-validation.md (NEW) — sequência de validação
```

#### Deep file dependency analysis
- `validate-matrix.sh` — adicionar:
  - `pkg:no-dead-code` — `find pkg -name "*.go" | xargs deadcode` (deve ser empty exceto sentinels intentional).
  - `pkg:usage-matrix-fresh` — invocar T1.7 script.
  - `pkg:state-machine-svgs-fresh` — invocar T2.7 script.
  - `pkg:no-database-sql-leak` — grep `database/sql` em pkg/ (exceto via interface).
  - `pkg:no-http-coupling` — grep `HTTPStatus:` em pkg/errors.

#### Deep Dives
- **Sequência:**
  1. `cd pkg && go test -race ./...`
  2. `cd api && go test -race ./...`
  3. `cd operators && go test -race ./...`
  4. `task validate:manifests` (kubeconform)
  5. `task lint`
  6. `task validate-matrix`

#### Tasks
1. Adicionar 5 gates ao validate-matrix.sh.
2. Criar runbook.
3. Rodar sweep completo.
4. Commit `test: full pkg-sota validation matrix`.

#### TDD
```
RED:     N/A (regression test sweep)
GREEN:   N/A.
REFACTOR: None.
VERIFY:  task validate-matrix && (cd pkg && go test -race ./...) && (cd api && go test -race ./...) && (cd operators && go test -race ./...)
```

#### Acceptance Criteria
- [ ] Validate matrix passes 15/15 gates (10 baseline + 5 novos pkg).
- [ ] All Go tests GREEN with -race.
- [ ] Lint clean.
- [ ] No internal terms in CLI output (Rule 3 grep clean).

#### DoD
- [ ] All checks GREEN.
- [ ] Runbook committed.

---

### T3.2 — `/dogfood full` em infra fresca

#### Objective
Executar dogfood end-to-end completo para confirmar que o sistema funciona como usuário real.

#### Evidence
- Plan-specific Dogfood Gate (mandatory).
- Histórico: dogfood iter 12+13 (CHG-2026-04-29) detectaram bugs invisíveis a unit tests.

#### Files to edit
```
docs/runbooks/dogfood-pkg-sota-evidence.md (NEW) — output do dogfood
```

#### Deep file dependency analysis
- N/A — execução, não code change.

#### Deep Dives
- **Pre-flight:**
  1. `task teardown && task setup && tilt up`
  2. `task doctor` — esperar GREEN
  3. `task k3d:heal` — quaisquer hacks codificados
- **Dogfood execution:** `/dogfood full` (skill já registrado).
- **Special validation steps for pkg-sota scope:**
  - Confirm RFC 9457 problem+json em error response (curl test).
  - Confirm circuit breaker logs durante Hydra restart simulation.
  - Confirm conditions kstatus shape em `kubectl get tenant <X> -o yaml`.
  - Rule 3 final check: zero internal terms em CLI output.

#### Tasks
1. Reset infra fresh.
2. Run `/dogfood full`.
3. Capture output.
4. Special validation steps.
5. Commit evidence file.

#### TDD
```
RED:     N/A (validation gate)
GREEN:   N/A.
REFACTOR: None.
VERIFY:  /dogfood full output marked PASS; zero S0/S1.
```

#### Acceptance Criteria
- [ ] `/dogfood full` PASS.
- [ ] Zero S0 (Bloqueante) findings.
- [ ] Zero S1 (Grave) findings related to pkg-sota scope.
- [ ] Special validation steps PASS.
- [ ] Evidence file committed.

#### DoD
- [ ] Dogfood evidence committed.
- [ ] Plan version bumped to v1.X reflecting completion.

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | `pkg/health` é dead code documentado como flagship (QW-1) | T1.1 | Delete package per ADR D1. |
| 2 | `pkg/jwt` pré-RFC 8725bis — algorithm whitelist ausente (QW-2) | T1.2 | `WithValidMethods` whitelist + `typ` validation. |
| 3 | `pkg/id` sem validators exportados (QW-3) | T1.3 | 4 IsValid* functions + 2 consumer migrations. |
| 4 | `pkg/jwt` JWKS sem circuit breaker, falha cascateia (QW-4) | T1.4 | Wrap fetchJWKS em pkg/circuitbreaker + grace period. |
| 5 | `naming.Labels` retorna map untyped (QW-5) | T1.5 | StandardLabels typed struct + 4 constants. |
| 6 | `errors.Wrap` não captura stack (QW-6) | T1.6 | WrapWithStack opcional + StackTrace method. |
| 7 | Doc/code drift (QW-7 + gate PKG-G14) | T1.7 | task pkg:usage-matrix script + CI gate. |
| 8 | `pkg/circuitbreaker` v1-style sem generics+context (S9-1) | T2.1 | New Breaker[T] + Execute(ctx, fn) + IsExcluded. |
| 9 | Falta `pkg/conditions` kstatus-compatible (S9-2) | T2.2 | New package + 2 controller migrations. |
| 10 | `LoopZeroError.HTTPStatus` acopla transport (S9-3 + gate PKG-G15) | T2.3 | pkg/errors/http subpackage + RFC 9457 problem+json. |
| 11 | `pkg/enforcement` direct *sql.DB (S9-4) | T2.4 | LimitsStore interface + adapter em api/internal/db. |
| 12 | `pkg/logging` global mutable state (S9-5) | T2.5 | ContextLogger + deprecation. |
| 13 | Falta property-based tests (S9-6 + gate PKG-G18) | T2.6 | testing/quick em statemachine + naming. |
| 14 | State machines sem visualização (S9-7) | T2.7 | Visualize() DOT + SVG generation + CI gate. |
| 15 | Gate PKG-G14: docs reflect real usage | T1.7 | `task pkg:usage-matrix` + CI. |
| 16 | Gate PKG-G15: no transport coupling in domain errors | T2.3 | HTTPStatus extracted to pkg/errors/http. |
| 17 | Gate PKG-G16: contract layer eats own dogfood | T1.4, T2.1 | JWT JWKS via pkg/circuitbreaker; v2 generic. |
| 18 | Gate PKG-G17: generics + context-aware primitives | T2.1 | Breaker[T] with Execute(ctx, fn). |
| 19 | Gate PKG-G18: property-based tests | T2.6 | 5-7 property tests with 1000+ iterations. |
| 20 | Governance: T3 review + ADRs ratified | T0.1 | 8 ADRs + MEET record. |

**Coverage: 20/20 gaps covered (100%)**

## Global Definition of Done

- [ ] All Phase 0 ADRs ratified.
- [ ] All Phase 1 quick wins committed.
- [ ] All Phase 2 modernization tasks committed.
- [ ] All tests passing (Go pkg + api + operators + cloud).
- [ ] Zero lint warnings (golangci-lint v2.11+).
- [ ] `task validate:manifests` passes (kubeconform --strict).
- [ ] `task validate-matrix` passes (15/15 gates).
- [ ] `task pkg:usage-matrix --check` GREEN.
- [ ] State machine SVGs up-to-date.
- [ ] No internal terms in CLI output (Rule 3 grep clean).
- [ ] Backward compat preserved within deprecation windows OR pre-release breaks documented in CHANGELOG.
- [ ] code-audit checks passing across all modified packages (complexity ≤ 10, coverage ≥ 90%, file size ≤ 500 lines).
- [ ] CHANGELOG.md `[Unreleased]` section reflects every change with ADR refs.
- [ ] `/dogfood full` PASSED on fresh infrastructure.

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure (operator must run before dogfood)
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Heal idempotent hacks (k3d-specific)
task k3d:heal

# 3. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure.
- [ ] Pre-Flight Check: ALL checks PASS.
- [ ] Zero S0 (Bloqueante) findings.
- [ ] Zero S1 (Grave) findings related to this plan's scope (pkg-sota).
- [ ] Prova de Vida passes for every deploy performed during dogfood.
- [ ] Prova de Morte passes for every delete performed during dogfood.
- [ ] No regressions in previously passing flows (deploy happy path, scaling, promote, rollback).
- [ ] Observability check passes (logs, metrics, CRs in correct state).
- [ ] **Rule 3 clean: zero internal terms in any CLI output (`ArgoCD`, `Gitea`, `tenant_id`, `CRD`, `kubebuilder`, `MinIO`, `Kaniko`)**.
- [ ] Special validation: RFC 9457 problem+json shape em error responses.
- [ ] Special validation: circuit breaker observability durante Hydra restart simulation.
- [ ] Special validation: kstatus conditions visíveis em `kubectl get tenant -o yaml` para 1+ controller migrado.

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings.
2. Re-run `/dogfood full` on fresh infrastructure.
3. Repeat until zero S0/S1.

S2-S4 findings are documented em `docs/runbooks/dogfood-pkg-sota-evidence.md` mas não bloqueiam plan completion.

---

## Post-Launch Deferred (Out of Scope)

Os 5 itens PL-1 a PL-5 do `improvement-roadmap.md` são EXPLICITAMENTE escopados FORA deste plano:

- **PL-1: cockroachdb/errors network portability** — separate change record após 1.0 launch.
- **PL-2: RFC 9457 OpenAPI auto-gen** — depends on T2.3 + spec generator pipeline.
- **PL-3: pkg/audit typed events** — separate plan post-launch.
- **PL-4: pkg/observability primitives** — pode emergir naturalmente após adoção wide de pkg/conditions.
- **PL-5: ULID-style IDs** — migration intrusiva; aguarda demanda real.

Decisão de escopo: este plano foca em fechar gaps que afetam **launch readiness**. PL items são evolução, não correção.
