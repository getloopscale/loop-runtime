# Plan: `loopzero scale` End-to-End — Fechar B40 Gap (DB→Renderer→K8s)

> **Version 1.0** — Este plano fecha o gap B40 descoberto na sessão cleanroom 2026-05-05: o handler `loopzero scale` persiste em `app_scaling_config`, mas o renderer NUNCA lê dessa tabela. Resultado atual: a tabela é orfã do path de deploy, e `loopzero scale --replicas N` não muda `Deployment.spec.replicas` no cluster — o próximo `loopzero deploy` continua usando o valor do `loopzero.yaml`. O plano implementa a integração faltante (Plan T2.3 não-shipped), expande Journey-04 do dogfood com `kubectl` assertion no cluster, e fecha-se via dogfood completo cleanroom 12/12 GREEN no padrão de `real-paas-cleanroom-dogfood-loop-plan.md`. Outcome esperado: `loopzero scale --replicas 3` + `loopzero deploy` produz `Deployment.spec.replicas == 3` no workload cluster, validado por journey-04 hardening + assertion direta no cluster.

## Context

**Gap B40 (descoberto 2026-05-05 durante cleanroom session):** auditoria empírica de código revelou que `loopzero scale` é PARCIALMENTE funcional:

- ✅ `api/internal/routes/scale.go:159-235` — handler valida input, retorna `{replicas, autoscaling, status}` shape correta
- ✅ `api/cmd/setup_orchestrator.go:144-159` — `scaleApply` closure persiste em `app_scaling_config` via `PostgresScalingStore.Upsert`, retorna `applied`/`no_change`
- ✅ `api/internal/db/migrations/000009_app_scaling_config.up.sql` — tabela existe com constraints (`min<=max`, `scale_to_zero requires min=0`, unique key `(tenant, project, env, app)`)
- ❌ `api/internal/services/build_orchestrator_submit.go:107-124` — `BuildAppSpec.Replicas` é populado direto de `req.Apps[].Replicas` que vem do `loopzero.yaml` parsed pelo CLI
- ❌ `api/internal/manifests/renderer_deployment.go:113-114` — `replicas = app.Replicas` lê desse `BuildAppSpec`, NÃO consulta `scalingStore`
- ❌ `app_scaling_config` é tabela ORFÃ — escrita por `loopzero scale`, lida APENAS por unit tests do próprio store

**Evidência:**

```bash
$ grep -rnE "scalingStore\.|GetByApp\b" api/ --include="*.go" | grep -v "_test.go"
api/cmd/setup_orchestrator.go:157:		_, changed, err := scalingStore.Upsert(ctx, dbAdapter, c)
# → ZERO callers no path de deploy
```

```bash
# journey-04-scale.sh atual valida APENAS response shape:
$ cat infra/tests/dogfood/journeys/journey-04-scale.sh | grep -A3 "scale --replicas 3"
# Não há assertion 'kubectl get deploy -o jsonpath={.spec.replicas}'
```

**Por que importa:** `loopzero scale` é um comando contratual no `cli-reference.md` §6. Anunciar como funcional na CLI é mentira sem o renderer integrado. Um cliente Pro tier (`maxReplicas=5`) que rodar `loopzero scale --replicas 5` HOJE não vê efeito no cluster — só acredita ver porque o response shape diz `applied`.

**Memória aplicada:**
- `feedback_no_tribal_knowledge` — codify-everything, fix at canonical layer
- `feedback_live_test_existing_db` — LIVE testing reveals what mocks hide
- `reference_cleanroom_iter29_2026_05_05` — dogfood gate é o único proof
- `feedback_skill_md_outdated_vs_code` — não confiar em commit messages declaratórios; auditar código

**Referência commits relevantes:**
- `9c070f83` (2026-05-05) — honesty correction documenting B40 gap em CLAUDE.md/README.md/learnings
- `a24eb995` (B36) — scale handler + PostgresScalingStore wiring (T2.2 shipped, T2.3 não)
- Migration `000009` — tabela existe há 2+ dias mas não tem leitor

## Objective

`loopzero scale --replicas N --app X --env Y` + `loopzero deploy` muda `Deployment.spec.replicas` no workload cluster para `N` — validado live via cleanroom dogfood com kubectl assertion direto no cluster.

**Goals mensuráveis:**
1. `app_scaling_config` é lida por exatamente UM novo componente (`ScalingResolver`) entre `loopzero deploy` request e `BuildAppSpec` persistence — fechando a chain.
2. Precedência scaling resolved: DB row presente → DB value; DB row ausente → `loopzero.yaml` value (backward-compat).
3. Plan-cap enforcement (`enforceScaling`) opera sobre o config RESOLVED, não sobre request body — fechando o hole MEET-2026-04-23-059 Constraint 6.
4. Journey-04 expandido com 3 novas asserts no cluster: `Deployment.spec.replicas`, `HPA spec.{minReplicas,maxReplicas}`, `ScaledObject` existência (KEDA path quando `scale_to_zero=true`).
5. Dogfood gate cleanroom 12/12 GREEN end-to-end com B40 fix shipped.

## ADRs

### D1 — Source of Truth: DB > loopzero.yaml após primeiro `loopzero scale`

**Decision:** Quando `app_scaling_config` tem row para `(tenant, project, env, app)`, o **DB tem precedência absoluta** sobre valores do `loopzero.yaml` para esse app. `loopzero.yaml` é o seed inicial; depois do primeiro `loopzero scale`, DB vira fonte canônica.

**Rationale:**
- Alternativa A (sempre `loopzero.yaml`): faz `loopzero scale` ser totalmente inútil sem editar arquivo
- Alternativa B (sempre DB com seed automático no primeiro deploy): adiciona escrita implícita não-óbvia no path de deploy
- Alternativa C (DB > loopzero.yaml, fallback se vazio): o que escolho — preserva backward-compat, dá poder ao `loopzero scale`, semântica clara

**Consequences:**
- ✅ `loopzero scale` torna-se o knob de produção de scaling
- ✅ `loopzero.yaml` continua válido como bootstrap inicial
- ⚠️ Workflow team needs doc: editar `loopzero.yaml` + `loopzero deploy` NÃO substitui se `loopzero scale` foi rodado depois (DB tem precedência). Workaround: rodar `loopzero scale --replicas N` para sincronizar DB com novo valor antes do deploy.
- ❌ NÃO permitido: deploy sem consulta ao DB (chain broken)

### D2 — Resolver é deep module com interface clara

**Decision:** Criar componente `ScalingResolver` com interface mínima:
```
type ScalingResolver interface {
    Resolve(ctx, tenant, project, env string, fromYAML []AppSpec) ([]AppSpec, error)
}
```
Implementação `dbScalingResolver` consulta `scalingStore.GetByApp` por app no slice e merge com `fromYAML` (DB wins, mantém apps que só estão em yaml).

**Rationale:**
- Separa responsabilidade de resolução (deep module conforme Ousterhout) — single point of override
- Testável isoladamente (interface stub)
- `BuildOrchestrator` continua agnostic da fonte — recebe AppSpecs já resolvidos
- Permite swap futuro (mock em testes, alternative resolvers em prod)

**Consequences:**
- ✅ `enforceScaling` opera sobre AppSpec resolvido — plan-cap correto
- ✅ Path único de override testável
- ⚠️ Adiciona 1 query DB por deploy (latency budget +5-15ms, aceitável dado deploy é seconds-scale)

### D3 — `loopzero scale` continua síncrono ao DB write, sem auto-deploy

**Decision:** `loopzero scale` retorna `applied`/`no_change` indicando estado do DB, NÃO do K8s. CLI output documenta explicitamente: "Scaling persisted. Run `loopzero deploy` to apply to running pods."

**Rationale:**
- Auto-deploy implícito é surpreendente e perigoso (interrompe traffic)
- Manter contrato API atual (no breaking change)
- Mensagem CLI clara substitui ambiguidade

**Consequences:**
- ✅ Backward-compat mantido (status field shape inalterada)
- ✅ Operadores controlam quando scaling vira efetivo
- ⚠️ Doc precisa explicar gap entre DB write e cluster effect

### D4 — KEDA scale-to-zero requires `loopzero deploy` reapply

**Decision:** Mudar de fixed-replicas para scale-to-zero (ou vice-versa) **requer** `loopzero deploy` posterior. `loopzero scale --min 0 --max 5` grava DB; o efeito (criação de `ScaledObject` ou switch HPA→KEDA) acontece no deploy seguinte.

**Rationale:**
- Renderer gera diferente estrutura YAML (HPA vs ScaledObject) baseado em `min_replicas == 0` flag
- Atualizar Deployment.spec.replicas LIVE não consegue criar ScaledObject CR — precisa do gitops path
- Forçar deploy re-run mantém invariant "manifests refletem DB"

**Consequences:**
- ✅ Single path para criar/destruir KEDA resources
- ✅ Auditoria clara via Argo Workflow + ArgoCD sync
- ⚠️ Custo: mudança de scaling-mode requer build + sync (~3-4min) — aceitável dado é raro

### D5 — Plano fecha-se via cleanroom dogfood 12/12 GREEN (no padrão `real-paas-cleanroom-dogfood-loop-plan.md`)

**Decision:** Adoção integral do gate "destroy + setup + tilt + heal + 12 journeys all GREEN end-to-end without manual intervention". Plano não é DONE até `bash infra/scripts/dogfood-cleanroom-loop.sh` passar.

**Rationale:**
- Memória `reference_cleanroom_iter29_2026_05_05` provou que cleanroom catches bugs invisíveis em unit/envtest (B32-B39)
- Sem isso, B40 não teria sido descoberto — só pela auditoria explícita do user
- Manter o gate é a única forma de prevenir regressões análogas

**Consequences:**
- ✅ Confidence alta no veredict
- ⚠️ Gate caro (~33min cleanroom + ~17min setup); mitigated by running once at end

### D6 — Plan amendments seguem ADR-0022

**Decision:** Qualquer mudança escope (DoD redefine, task deferral, novos T2/T3 surgidos durante execução) precisa MEET record + version bump (v1.0 → v1.1). Trivial fixes (typo, comment) podem direct edit + commit.

**Rationale:** mesma rationale do plano cleanroom anterior; consistência operacional.

## Dependency Graph

```
Phase 0 ──▶ Phase 1 ──▶ Phase 2 ──▶ Phase 3 ──▶ Phase 4
   │           │
   │           └─ Phase 1.5 (parallel: ScalingResolver tests)
   │
   ▼
Phase 5 (depende de Phase 1+2+3+4 done) ──▶ Phase 6 (Dogfood Gate)
```

- **Phase 0** = preconditions (verify baseline). Sequential blocker.
- **Phase 1** = ScalingResolver deep module. Sequential blocker.
- **Phase 1.5** (parallel to Phase 1) = unit tests do Resolver. Sem dependência runtime — pode rodar paralelo.
- **Phase 2** = build_orchestrator_submit calls Resolver. Depends Phase 1.
- **Phase 3** = renderer + KEDA correctness with resolved config. Depends Phase 2.
- **Phase 4** = enforceScaling refactor (plan-cap on resolved). Depends Phase 1 (consume resolver output).
- **Phase 5** = Journey-04 hardening (kubectl asserts). Depends Phase 1+2+3+4 = E2E ready.
- **Phase 6** = Dogfood Gate cleanroom. Final blocker.

---

## Phase 0: Preconditions

**Objective:** baseline tag + state verification before mutation.

### T0.1 — Baseline tag + working tree clean

#### Objective
Capturar HEAD pré-implementação para reproduction posterior.

#### Evidence
Memória `reference_cleanroom_iter29_2026_05_05` documenta que tags `dogfood-cleanroom-baseline-${DATE}` foram úteis para auditoria pós-fato.

#### Files to edit
```
(no file edits — git tag operation only)
```

#### Deep file dependency analysis
N/A — git tag operation.

#### Deep Dives
- Tag annotated (não lightweight) — anexa autor, data, ref ao plano
- Local-only por padrão; promove a origin se human confirmar

#### Tasks
1. `git status` — confirm clean (or known-acceptable state)
2. `git tag -a "scale-b40-baseline-$(date +%Y%m%d)" -m "Baseline before B40 fix per docs/plans/loopzero-scale-end-to-end-plan.md"`
3. `git tag --list "scale-b40-*"` confirma created

#### TDD
```
N/A — tag operation
VERIFY: `git tag --list scale-b40-baseline-*` returns 1 entry
```

#### Acceptance Criteria
- [ ] Tag `scale-b40-baseline-${DATE}` exists local
- [ ] HEAD não-divergent de develop ou justified

#### DoD
- [ ] Tag operation completed

---

### T0.2 — Confirm gap empiricamente (RED proof)

#### Objective
Documentar a falha LIVE atual: `loopzero scale --replicas 3` + `loopzero deploy` → `Deployment.spec.replicas != 3`.

#### Evidence
`docs/learnings/cleanroom-iter28-29-learnings-20260505.md` §B36 GAP DESCOBERTO POST-FIX.

#### Files to edit
```
.test-reports/scale-b40-baseline-rep.md — (NEW) snapshot da falha atual
```

#### Deep file dependency analysis
- Output do report serve como RED proof — tests T1.x, T2.x, T3.x devem inverter este resultado.

#### Deep Dives
- Sequência exata: `loopzero project delete` (clean state) → `loopzero deploy` baseline → confirm replicas=1 → `loopzero scale --replicas 3` → confirm DB row inserted → `loopzero deploy` again → confirm `kubectl get deploy ... -o jsonpath='{.spec.replicas}'` STILL 1 (gap demonstrated)
- Se cluster atual sem fixture, usar `infra/tests/dogfood/fixtures/solo-server`

#### Tasks
1. Garantir cluster dev rodando (`task doctor` PASS)
2. Run sequence:
   ```bash
   loopzero project delete solo-server --yes
   loopzero deploy --env production --no-wait
   # wait build SUCCEEDED
   kubectl --context k3d-loopzero-workload -n loopzero-tn-e2etest-production \
     get deploy solo-server-api -o jsonpath='{.spec.replicas}'
   # Expected baseline: 1
   loopzero scale --app api --env production --replicas 3 --json | jq
   # Expected: status=applied, replicas=3 echoed
   kubectl --context k3d-loopzero-mgmt -n loopzero-system exec postgresql-0 -c postgresql -- \
     psql -U loopzero -d loopzero -tAc "SELECT min_replicas, max_replicas FROM app_scaling_config WHERE tenant_id='tn-e2etest' AND project_id='solo-server' AND environment='production' AND app_name='api';"
   # Expected: 3|3 (DB row inserted)
   loopzero deploy --env production --no-wait
   # wait SUCCEEDED
   kubectl --context k3d-loopzero-workload -n loopzero-tn-e2etest-production \
     get deploy solo-server-api -o jsonpath='{.spec.replicas}'
   # Expected per gap: STILL 1 (BUG)
   ```
3. Capture stdouts em `.test-reports/scale-b40-baseline-rep.md`
4. Commit: `docs(reports): scale B40 baseline RED proof — replicas stays 1 despite scale 3`

#### TDD
```
RED:    Live experiment shows `kubectl deploy.spec.replicas == 1` after `loopzero scale --replicas 3 + loopzero deploy`
GREEN:  Phase 1+2+3 fixes will invert — kubectl returns 3
VERIFY: Re-run sequence at end of Phase 5; expected output 3
```

#### Acceptance Criteria
- [ ] `.test-reports/scale-b40-baseline-rep.md` commitado com saída REAL do comando
- [ ] Documenta DB row presente (3|3) E cluster ainda em 1
- [ ] Audit trail clara para auditoria pós-fato

#### DoD
- [ ] Report committed
- [ ] Gap reproduzido empiricamente — não só código analysis

---

## Phase 1: ScalingResolver Deep Module

**Objective:** introduzir resolver que merge `loopzero.yaml` AppSpecs com `app_scaling_config` DB rows, com DB precedence (D1).

### T1.1 — Define `ScalingResolver` interface + DB impl

#### Objective
Encapsular lookup do `app_scaling_config` em interface mínima e testável (D2).

#### Evidence
Plan T2.3 declared mas NÃO shipped (per learnings doc B36 expanded section).

#### Files to edit
```
api/internal/services/scaling_resolver.go — (NEW) interface + dbScalingResolver impl
api/internal/services/scaling_resolver_test.go — (NEW) unit tests for resolver
```

#### Deep file dependency analysis
- **scaling_resolver.go (NEW)**: importa `db.ScalingStore`. Não tem callers ainda — wiring acontece em T2.1.
- **scaling_resolver_test.go (NEW)**: usa `fakeScalingStore` interface stub. Não impacta outros testes.

#### Deep Dives

**Data structures:**
```go
type ScalingResolver interface {
    Resolve(
        ctx context.Context,
        tenantID, projectID, environment string,
        fromYAML []AppSpec,
    ) ([]AppSpec, error)
}

type dbScalingResolver struct {
    store db.ScalingStore
}
```

**Algorithm (Resolve):**
1. For each `app` in `fromYAML`:
   - Call `store.GetByApp(ctx, tenantID, projectID, environment, app.Name)`
   - If `ErrScalingConfigNotFound` → keep app unchanged (yaml is authoritative)
   - If row exists → override:
     - `app.Replicas = row.MinReplicas` if `MinReplicas == MaxReplicas` (fixed)
     - `app.Replicas = 0` if `ScaleToZero` (KEDA)
     - `app.Autoscaling = &AppAutoscalingSpec{Min: row.MinReplicas, Max: row.MaxReplicas, TargetCPU: ...}` if `MinReplicas < MaxReplicas` (HPA)
   - If err other → return wrapped err (don't swallow)
2. Return resolved slice (preserves order)

**Invariants:**
- After Resolve: `len(fromYAML) == len(result)` — apps not dropped
- App order preserved (deterministic for build_plan)
- Only overrides scaling fields; other fields (`Type`, `Port`, `Secrets`, etc.) unchanged

**Edge cases:**
- Empty `fromYAML` → returns empty slice, nil err
- `store == nil` → returns `fromYAML` unchanged + slog.Warn (defensive — preserve current behavior if wiring missing)
- DB query fails (PG down) → return err (caller decides if retryable)

#### Tasks
1. Create `api/internal/services/scaling_resolver.go` with interface + struct stub
2. Implement `dbScalingResolver.Resolve` per algorithm above
3. Wire into `services` package exports (lowercase struct, uppercase constructor)
4. Add unit tests in `scaling_resolver_test.go`

#### TDD

```
RED:    TestResolve_DBRowOverridesYAML_FixedReplicas — fromYAML.Replicas=1, DB row min=max=3 → result.Replicas=3
RED:    TestResolve_DBRowOverridesYAML_HPA — fromYAML no autoscaling, DB row min=2 max=5 → result.Autoscaling={Min:2,Max:5}
RED:    TestResolve_DBRowOverridesYAML_KEDA — fromYAML.Replicas=1, DB row scale_to_zero=true min=0 max=5 → result.Replicas=0, Autoscaling={Min:0,Max:5}
RED:    TestResolve_NoDBRow_PreservesYAML — fromYAML.Replicas=2, DB returns ErrScalingConfigNotFound → result.Replicas=2 (unchanged)
RED:    TestResolve_MultipleApps_OnlyOverrideMatching — 2 apps, DB row only for app[0] → app[0] overridden, app[1] unchanged
RED:    TestResolve_DBError_PropagatesErr — store returns generic err → Resolve returns wrapped err
RED:    TestResolve_NilStore_ReturnsYAML — store=nil → returns yaml unchanged + warn

GREEN:  Implement scaling_resolver.go per the algorithm

REFACTOR: Extract apply-row helper if cyclomatic > 8

VERIFY: cd api && go test ./internal/services/ -run TestResolve -v
```

#### Acceptance Criteria
- [ ] All 7 RED tests fail before implementation
- [ ] After GREEN, all 7 pass
- [ ] Cyclomatic complexity Resolve ≤ 8
- [ ] File ≤ 200 lines (small + focused)
- [ ] No new lint warnings (`golangci-lint run ./internal/services/`)
- [ ] `go vet` clean

#### DoD
- [ ] All 7 RED tests added
- [ ] Implementation passes them
- [ ] `go test ./internal/services/ -run TestResolve` GREEN
- [ ] `go vet` + `golangci-lint` GREEN
- [ ] File committed

---

### T1.2 — Wire `ScalingResolver` no setup_orchestrator

#### Objective
Construct + injetar `dbScalingResolver` em `BuildOrchestrator.Submit` chain.

#### Evidence
`api/cmd/setup_orchestrator.go:144` JÁ tem `scalingStore` instantiated; basta passar para `dbScalingResolver`.

#### Files to edit
```
api/cmd/setup_orchestrator.go — instantiate dbScalingResolver from existing scalingStore
api/internal/services/build_orchestrator.go — accept ScalingResolver in constructor / Deps
```

#### Deep file dependency analysis
- **setup_orchestrator.go**: já tem `scalingStore := db.NewPostgresScalingStore(dbAdapter)`. Add line: `scalingResolver := services.NewDBScalingResolver(scalingStore)` + pass to `BuildOrchestratorDeps`.
- **build_orchestrator.go**: add `ScalingResolver services.ScalingResolver` to `Deps` struct (line ~290 after `PlanCap`). Constructor adds field assignment.
- **Caller chain**: `setup_orchestrator → BuildOrchestrator → SubmitBuild` — wired through.

#### Deep Dives

**Why constructor injection (not late-binding):**
- Resolver é dependência crítica do path de Submit; missing wiring é programming error, not runtime
- Late-binding via context would obscure dependency graph
- Constructor-time matches existing pattern (`PlanCap`, `KEDAEnabled`, etc.)

**Initialization order in setup_orchestrator:**
```
dbAdapter → scalingStore → scaleApply (existing, T2.2 commit)
                       └─→ scalingResolver (NEW T1.2)
                              ↓
                       BuildOrchestratorDeps.ScalingResolver
                              ↓
                       NewBuildOrchestrator(...).submit uses it (T2.x)
```

**Backward-compat:**
- If `Deps.ScalingResolver == nil`, `Submit` falls back to current behavior (no DB lookup) — preserves existing tests that don't pass resolver
- New tests pass non-nil resolver to exercise integration

#### Tasks
1. Add `ScalingResolver` field to `BuildOrchestratorDeps` struct
2. Add field assignment in `NewBuildOrchestrator`
3. Add nil-safe accessor `o.resolveScaling(ctx, tenantID, projectID, env, apps)` that returns apps unchanged if resolver nil
4. In setup_orchestrator.go: instantiate resolver, pass via Deps

#### TDD

```
RED:    TestBuildOrchestrator_NilResolver_PreservesAppSpec — submit with nil resolver, apps unchanged in BuildAppSpec
RED:    TestBuildOrchestrator_WithResolver_UsesDBValues — submit with stub resolver returning replicas=5, BuildAppSpec.Replicas=5

GREEN:  Wire resolver into Deps + accessor + setup_orchestrator instantiation

REFACTOR: None expected (small additive change)

VERIFY: cd api && go test ./internal/services/ -run TestBuildOrchestrator_.*Resolver
```

#### Acceptance Criteria
- [ ] 2 RED tests added, fail before wiring
- [ ] After GREEN, both pass
- [ ] No regression em outros build_orchestrator tests
- [ ] setup_orchestrator.go nova linha consistent com pattern existente
- [ ] `go test ./...` em api/ GREEN

#### DoD
- [ ] 2 RED tests + GREEN
- [ ] All existing api tests still pass
- [ ] Lint clean

---

## Phase 2: Build Orchestrator Submit Integration

**Objective:** chamar `ScalingResolver.Resolve` antes de criar `BuildAppSpec` no `submit`.

### T2.1 — Substituir `req.Apps` direto por resolved apps

#### Objective
Em `build_orchestrator_submit.go`, antes do plan-cap check + BuildAppSpec persist, resolve `req.Apps` via resolver. Resolved slice substitui `req.Apps` para o resto do submit.

#### Evidence
`api/internal/services/build_orchestrator_submit.go:107-124` é o site único onde `BuildAppSpec.Replicas` é populado. Inserir resolve aqui é minimal-surface change.

#### Files to edit
```
api/internal/services/build_orchestrator_submit.go — add resolve call before enforceScaling
api/internal/services/build_orchestrator_submit_test.go — new tests for integration
```

#### Deep file dependency analysis
- **build_orchestrator_submit.go**: ~line 60 (before enforceScaling call), insert:
  ```go
  resolvedApps, err := o.resolveScaling(ctx, req.TenantID, req.ProjectID, req.Environment, req.Apps)
  if err != nil {
      return fmt.Errorf("submit build: resolve scaling: %w", err)
  }
  req.Apps = resolvedApps  // Local mutation — req escopo do submit, safe
  ```
- **enforceScaling** (~line 64) consome `req.Apps` resolved → plan-cap correto (D1+D2)
- **BuildAppSpec persist** (~line 108) consome `req.Apps` resolved → DB persist resolved
- **manifest_builder.go** (line 110): downstream chain reads `Build.Apps[*].Autoscaling.MaxReplicas` from build store row, which was populated from resolved spec

#### Deep Dives

**Why mutate req local (not refactor signature):**
- Submit() é função grande (~150 linhas); refactor signature touches every test
- Local mutation contained to submit scope — no leak
- Pattern preserves existing assert sites em testes

**Why before plan-cap check (D2):**
- Plan caps são protective — devem operar sobre o que vai virar K8s state, não sobre user-supplied yaml
- Hole MEET-2026-04-23-059 Constraint 6 explicitly cita this — "auth path must enforce on resolved config"
- Otherwise: user envia yaml com `replicas: 1`, loopzero scale para `5`, plan cap 3 não detecta porque check vê yaml=1

**Edge case (race with concurrent scale):**
- T submitBuild fires resolve at t1
- T scale handler upsert at t2 > t1
- Submit's persisted BuildAppSpec reflects t1 state (snapshot)
- Acceptable — next deploy picks up t2 (eventual consistency)
- Documentar em comment

#### Tasks
1. Add resolve call in submit (~line 60), inside transaction's tx scope (RR isolation)
2. Update enforceScaling site to consume resolved
3. Update BuildAppSpec creation site to consume resolved
4. Add 3 new tests covering: scale-then-deploy, scale-then-deploy with plan-cap rejection, no-scale-then-deploy (yaml-only)

#### TDD

```
RED:    TestSubmit_ScaleThenDeploy_PersistsResolvedReplicas — pre-existing DB row replicas=3, deploy with yaml replicas=1 → BuildAppSpec.Replicas=3
RED:    TestSubmit_ScaleThenDeploy_PlanCapEnforcedOnResolved — DB row replicas=10 (from cap-bypass scale), Pro tier (cap=5), deploy → 422 PlanCapExceeded
RED:    TestSubmit_NoScaleConfig_UsesYAML — DB no row, yaml replicas=2 → BuildAppSpec.Replicas=2 (backward-compat)

GREEN:  Insert resolve call + propagate

REFACTOR: None expected — additive

VERIFY: cd api && go test ./internal/services/ -run TestSubmit_ -v
```

#### Acceptance Criteria
- [ ] 3 RED tests fail before code change
- [ ] After GREEN, all 3 pass
- [ ] No regression em existing submit tests
- [ ] Resolve happens INSIDE transaction (consistent with serializable isolation)
- [ ] `go test ./...` em api/ GREEN

#### DoD
- [ ] 3 RED + GREEN
- [ ] Existing tests preserved
- [ ] Race condition documented in comment

---

## Phase 3: Renderer Correctness Pass

**Objective:** validar empiricamente que renderer gera Deployment + HPA + ScaledObject corretos com config resolved (não há mudança de código no renderer; só verificação).

### T3.1 — Renderer fixture tests para resolved configs

#### Objective
Adicionar fixtures de renderer que provam que apps com `Replicas`/`Autoscaling` (resolved from DB) produzem manifests corretos.

#### Evidence
`api/internal/manifests/renderer_deployment.go:99-114` já lê `app.Replicas` + `app.Autoscaling` do AppManifestInput. Não tem dependência direta no scalingStore — então NENHUMA mudança de código no renderer é necessária. Mas precisamos provar que está correto via fixture.

#### Files to edit
```
api/internal/manifests/renderer_test.go — add 3 new fixtures (fixed/HPA/KEDA)
```

#### Deep file dependency analysis
- **renderer_test.go**: existing pattern uses goldens. Add 3 new sub-tests with explicit `Replicas`/`Autoscaling` inputs, assert YAML output via existing snapshot diff.
- **No production code changes** — just proof.

#### Deep Dives

**Three scenarios to fixture:**

| Scenario | Input | Expected manifest |
|---|---|---|
| Fixed | `Replicas=3, Autoscaling=nil` | `Deployment.spec.replicas=3`, no HPA, no ScaledObject |
| HPA | `Autoscaling={Min:2, Max:5, TargetCPU:80}` | `Deployment.spec.replicas=2`, `HorizontalPodAutoscaler.spec.{minReplicas:2, maxReplicas:5}`, no ScaledObject |
| KEDA | `Autoscaling={Min:0, Max:5}` | `Deployment.spec.replicas=0`, no HPA, `ScaledObject.spec.{minReplicaCount:0, maxReplicaCount:5}` |

#### Tasks
1. Read existing `renderer_test.go` patterns (golden file? in-memory assert?)
2. Add `TestRender_ResolvedFixed_DeploymentReplicas3`
3. Add `TestRender_ResolvedHPA_GeneratesHPA`
4. Add `TestRender_ResolvedKEDA_GeneratesScaledObject`
5. Run tests; expected GREEN immediately (no code change needed)

#### TDD

```
RED:    Three new tests above (these MAY pass on first run since renderer reads inputs correctly already; that's acceptable — they're regression tests, not driving new code)
GREEN:  Renderer code unchanged
REFACTOR: None expected
VERIFY: cd api && go test ./internal/manifests/ -run TestRender_Resolved -v
```

#### Acceptance Criteria
- [ ] 3 fixture tests added
- [ ] All 3 pass without renderer code change (proves renderer already handles resolved input correctly)
- [ ] Test names self-documenting
- [ ] No flakes (deterministic YAML diff)

#### DoD
- [ ] 3 fixture tests GREEN
- [ ] Renderer empirically validated as resolved-config-correct

---

## Phase 4: Plan-Cap Enforcement on Resolved

**Objective:** garantir que `enforceScaling` (B15) opera no spec resolvido — não no request body.

### T4.1 — Move enforceScaling call AFTER resolve (já implícito em T2.1)

#### Objective
Verify that ordering em build_orchestrator_submit é resolve → enforce, não enforce → resolve.

#### Evidence
T2.1 inseriu resolve antes de enforceScaling. T4.1 é a verification + test that confirms plan-cap rejection ON RESOLVED.

#### Files to edit
```
api/internal/services/build_orchestrator_plan_limit_test.go — add 2 tests for resolved-config plan caps
```

#### Deep file dependency analysis
- **Plan_limit_test.go**: existing tests pass `Apps` directly. New tests add `ScalingResolver` stub returning over-cap config; submit should reject.

#### Deep Dives

**Test scenarios:**

| Scenario | yaml | DB resolved | Plan cap | Expected |
|---|---|---|---|---|
| YAML under, DB over | `replicas:1` | `replicas:10` | Pro=5 | Reject (resolved > cap) |
| YAML over, DB under | `replicas:10` | `replicas:3` | Pro=5 | Accept (resolved <= cap) |

Last scenario CRÍTICO: provar que DB pode "salvar" um yaml com over-cap. Isso é semantically a feature — operador pode configurar yaml com max desired e usar `loopzero scale` para enforce real value within cap.

#### Tasks
1. Add `TestEnforceScaling_DBResolvedExceedsPlanCap_Reject`
2. Add `TestEnforceScaling_YAMLExceedsButDBUnder_Accept`
3. Run tests; should FAIL until T2.1 was correctly ordered

#### TDD

```
RED:    TestEnforceScaling_DBResolvedExceedsPlanCap_Reject — yaml=1, DB=10, cap=5 → 422
RED:    TestEnforceScaling_YAMLExceedsButDBUnder_Accept — yaml=10, DB=3, cap=5 → 200
GREEN:  No new code (depends on T2.1 ordering); verify tests pass
REFACTOR: None
VERIFY: cd api && go test ./internal/services/ -run TestEnforceScaling
```

#### Acceptance Criteria
- [ ] 2 new tests added
- [ ] Both pass after T2.1 in place
- [ ] Documentation comment in build_orchestrator_submit.go cites D2 + Constraint 6

#### DoD
- [ ] 2 tests GREEN
- [ ] enforceScaling provadamente sees resolved config

---

## Phase 5: Journey-04 Hardening

**Objective:** journey-04 NUNCA mais aceita `loopzero scale` como funcional sem validar `kubectl deploy.spec.replicas` no cluster.

### T5.1 — Add kubectl assertions ao journey-04

#### Objective
Após `loopzero scale --replicas 3` + `loopzero deploy`, journey-04 valida que workload cluster reflete o scaling via `kubectl`.

#### Evidence
Memória `feedback_live_test_existing_db` + B36 GAP descoberto pós-fix: tests sem cluster assertion mascararam o gap por 2+ dias. Esta task fecha o feedback loop.

#### Files to edit
```
infra/tests/dogfood/journeys/journey-04-scale.sh — add 3 new assertions
```

#### Deep file dependency analysis
- **journey-04-scale.sh** atual valida só response shape + HTTP 200 + audit. Adicionar:
  1. Pre-scale snapshot: `kubectl ... get deploy ${PROJECT}-${APP} -o jsonpath='{.spec.replicas}'` → store
  2. Run `loopzero scale --replicas 3 --json`
  3. Run `loopzero deploy --env production` (force redeploy to apply)
  4. Wait build SUCCEEDED
  5. Post-scale assert: `kubectl ... get deploy ... -o jsonpath='{.spec.replicas}'` == 3
  6. Idempotency assert: re-run `loopzero scale --replicas 3 --json` → status `no_change`

#### Deep Dives

**Cluster assertion command shape:**
```bash
kubectl --context "$WORKLOAD_CTX" -n "loopzero-${TENANT_ID}-${ENV}" \
  get deploy "${PROJECT}-${APP}" -o jsonpath='{.spec.replicas}'
```

**Why force redeploy:** D3 afirma que `loopzero scale` não auto-deploya. Test simula real workflow: scale → next deploy.

**Plan cap probe expansion:** já existe over-cap probe; expandir para validar response shape rejection (`PlanCapExceeded` typed error).

**KEDA path:** opcional T5.2 — adicionar test for `loopzero scale --min 0 --max 5` produces `ScaledObject` CR. Cobertura via fixture renderer já feita em T3.1; live cluster assertion é nice-to-have. Marcar como SECONDARY — incluir se cabe no journey budget.

#### Tasks
1. Read current journey-04-scale.sh
2. Replace pseudo-stub assertion (`status==applied`) with cluster assertion sequence
3. Add idempotency probe (no_change)
4. Update final journey log message: "Journey 04 GREEN — scale resolved E2E (DB→deploy→kubectl)"

#### TDD

```
RED:    journey-04 currently passes (because it asserts only response shape)
        → After Phase 1+2 changes, kubectl deploy.spec.replicas WILL be 3
        → New assertion (kubectl == 3) IS the test that proves Phase 1+2 worked
GREEN:  Modify journey-04 sh + run via dogfood
REFACTOR: None
VERIFY: bash infra/tests/dogfood/journeys/journey-04-scale.sh exits 0 + log mostra "kubectl asserts replicas=3"
```

#### Acceptance Criteria
- [ ] Journey-04 has explicit `kubectl get deploy ... -o jsonpath` assertion
- [ ] Journey-04 has idempotency `no_change` probe after first scale
- [ ] Journey-04 still produces "Journey 04 GREEN" log line
- [ ] Total runtime < 6min (tolerable for dogfood loop)

#### DoD
- [ ] journey-04 modified + tested standalone
- [ ] Cleanroom dogfood (T6.1) ainda passa journey-04 GREEN

---

### T5.2 — Optional: KEDA scale-to-zero live assertion

#### Objective
Probe live: `loopzero scale --min 0 --max 5` produces `ScaledObject` CR.

#### Evidence
D4 — KEDA path requires deploy reapply. Validar live garante that the path actually wires.

#### Files to edit
```
infra/tests/dogfood/journeys/journey-04-scale.sh — add SECONDARY block (gated por env var)
```

#### Deep file dependency analysis
- Same file as T5.1; additive block.

#### Deep Dives
- KEDA may not be installed in standalone-multi by default — check `kubectl get crd scaledobjects.keda.sh` first; if missing, skip with `journey_log "GAP: KEDA not installed, skipping scale-to-zero probe"`.

#### Tasks
1. Add CRD-presence check
2. If present: scale to-zero, deploy, assert ScaledObject exists
3. If absent: log SKIP and proceed

#### TDD
```
N/A — environment-gated probe
VERIFY: live run shows either ScaledObject created OR SKIP message
```

#### Acceptance Criteria
- [ ] Probe runs only when KEDA CRD present
- [ ] No flake: deterministic skip path

#### DoD
- [ ] Probe present in journey-04
- [ ] Documentation comment cites D4

---

## Phase 6: Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> Same gate as `real-paas-cleanroom-dogfood-loop-plan.md` — destroy + setup + tilt + heal + 11 journeys + preflight, all GREEN end-to-end.

### T6.1 — Cleanroom dogfood 12/12 GREEN com B40 fix

#### Objective
Validar que B40 fix não regrediu nenhum journey, E que journey-04 hardening passa cluster assertion.

#### Evidence
Memória `reference_cleanroom_iter29_2026_05_05` — esta gate catches what unit tests miss. Crítico após mudança em build_orchestrator path.

#### Files to edit
```
(no file edits in this task — execução pura)
```

#### Deep file dependency analysis
N/A — full system test.

#### Deep Dives

**Pre-condition:**
- All previous Phases (0-5) DONE + commits in develop
- API image rebuilt with new ScalingResolver + submit path
- Tilt YAML unchanged (resolver é serviço in-process, não env)
- Migration 000009 applied (já está)

**Execution:**
```bash
# 1. Tag pre-dogfood snapshot (for reproduction)
git tag -a "scale-b40-pre-dogfood-$(date +%Y%m%d)" -m "Before final dogfood, all phases complete"

# 2. Run cleanroom
bash infra/scripts/dogfood-cleanroom-loop.sh
# Expected: 12/12 GREEN in ~33-50min total
```

**Pass criteria:**
- preflight 16/16 PASS
- journey-01..11 all GREEN
- journey-04 specifically: kubectl assertion shows replicas=3 (proves B40 fixed)
- journey-04 idempotency probe: status=no_change
- Rule 3 audit: zero internal terms in CLI output (per CLAUDE.md)

**On Failure:**
1. Identify journey FAIL
2. Capture log + cluster state snapshot
3. Classify (T0/T1/T2/T3 per pre-release-governance.md)
4. T0/T1 → fix + re-run; T2/T3 → STOP, invocar meeting-orchestrator
5. Max 5 iterations per D4 of cleanroom plan

#### Tasks
1. Verify all Phase 0-5 commits in develop branch (no orphan branches)
2. Tag pre-dogfood
3. Run `bash infra/scripts/dogfood-cleanroom-loop.sh`
4. Capture report in `.test-reports-cleanroom/`
5. If GREEN: append to `docs/reports/dogfood-cleanroom-${DATE}.md` with B40 fix verification
6. If RED: classify + fix + iterate

#### TDD
```
RED:    Cleanroom run BEFORE Phase 1+2+3 changes — journey-04 cluster assertion FAILS (replicas stays 1)
GREEN:  After all Phases shipped — journey-04 cluster assertion PASSES (replicas=3)
VERIFY: bash infra/scripts/dogfood-cleanroom-loop.sh → exit 0 + 12/12 GREEN
```

#### Acceptance Criteria
- [ ] `dogfood-cleanroom-loop.sh` exits 0
- [ ] preflight 16/16 PASS
- [ ] journey-04 GREEN with explicit cluster assertion log line: `replicas=3 confirmed via kubectl`
- [ ] journey-04 idempotency probe: `no_change`
- [ ] Other 10 journeys: GREEN (no regression)
- [ ] Rule 3 audit: 0 matches em journey logs (per CLAUDE.md)
- [ ] Total runtime < 60min (target ~50min)

#### DoD
- [ ] Cleanroom 12/12 GREEN
- [ ] Report committed
- [ ] Tag `scale-b40-converged-${DATE}` created

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | `app_scaling_config` é tabela orfã (ninguém lê) | T1.1, T1.2, T2.1 | Novo `ScalingResolver` lê tabela; injetado em `BuildOrchestratorDeps`; chamado em submit |
| 2 | `BuildAppSpec.Replicas` populated direto de `req.Apps` (yaml) | T2.1 | Submit faz `req.Apps = resolve(req.Apps)` antes de persist |
| 3 | `enforceScaling` plan-cap opera no yaml, não no resolved | T2.1, T4.1 | Resolve precede enforceScaling — plan-cap correto |
| 4 | Renderer não testa resolved configs | T3.1 | 3 fixture tests para Fixed/HPA/KEDA |
| 5 | Journey-04 só valida response shape, não cluster | T5.1 | `kubectl get deploy -o jsonpath='{.spec.replicas}'` assertion |
| 6 | Idempotency `no_change` não testada live | T5.1 | Re-run scale com mesmo valor → `no_change` |
| 7 | KEDA scale-to-zero live unverified | T5.2 | Optional probe + ScaledObject assertion |
| 8 | Plan cap hole MEET-2026-04-23-059 Constraint 6 | T2.1, T4.1 | Resolve antes de enforce → cap operates on actual K8s state |
| 9 | Backward-compat sem DB row | T1.1 (ErrScalingConfigNotFound branch), T2.1 | Test `TestSubmit_NoScaleConfig_UsesYAML` |
| 10 | Race scale↔deploy concurrent | T2.1 (comment) | Documented as eventual-consistency; tx isolation snapshot |
| 11 | Sem cleanroom regression check | T6.1 | Mandatory dogfood gate 12/12 GREEN |
| 12 | CLAUDE.md/README.md still cite outdated loopzero scale status | T7.1 | Update docs after dogfood GREEN |

**Coverage: 12/12 gaps covered (100%)**

---

## Phase 7: Plan Closure (post-dogfood)

### T7.1 — Update CLAUDE.md / README.md / learnings doc

#### Objective
Reflect que B40 está fixado; remover o "Partially functional" label.

#### Files to edit
```
CLAUDE.md — Status section + loopzero scale paragraph
README.md — What Works table + What's Not Built (remove gap)
docs/learnings/cleanroom-iter28-29-learnings-20260505.md — append "B40 RESOLVED" section
docs/plans/loopzero-scale-end-to-end-plan.md — append Convergence Log
```

#### Tasks
1. CLAUDE.md: replace "partially functional" paragraph with "fully functional" + reference to plan + Phase 6 GREEN
2. README.md: loopzero scale row updated to ✅; What's Not Built row removed
3. learnings doc: new section "## B40 RESOLVED — 2026-XX-XX" with phase summary + commits
4. Plan doc: Convergence Log table preenchido + final outcome PASS

#### Acceptance Criteria
- [ ] All 4 files updated coherently
- [ ] Cross-refs valid (commits, tags, plan paths)
- [ ] Single commit `docs(claude,readme,plan,learnings): close B40 — loopzero scale fully functional`

#### DoD
- [ ] All updates committed
- [ ] No conflicting claims between docs

### T7.2 — Tag converged

#### Objective
Permanent tag marking B40 fix shipped.

#### Tasks
1. `git tag -a "scale-b40-converged-$(date +%Y%m%d)" -m "B40 fixed — loopzero scale fully functional E2E. See docs/plans/loopzero-scale-end-to-end-plan.md Convergence Log."`
2. List tags to confirm

#### Acceptance Criteria
- [ ] Tag exists local

#### DoD
- [ ] Tagged

---

## Global Definition of Done

- [ ] All phases (0-7) completed
- [ ] All tests passing (Go + sh journey)
- [ ] Zero lint warnings (`golangci-lint run ./...` em api/)
- [ ] Backward compatibility preserved (yaml-only deploy still works)
- [ ] code-audit checks passing across `api/internal/services/`, `api/internal/manifests/`
- [ ] Plan-specific criteria:
  - [ ] `loopzero scale --replicas 3` + `loopzero deploy` → kubectl `Deployment.spec.replicas == 3` (Phase 6 evidence)
  - [ ] `app_scaling_config` row written by scale + read by deploy (chain closed)
  - [ ] Plan caps enforce on resolved config (T4.1 tests)
  - [ ] Backward-compat: yaml-only flow unchanged (T1.1 NoDBRow test + T2.1 NoScale test)
- [ ] **`bash infra/scripts/dogfood-cleanroom-loop.sh` PASSED — 12/12 GREEN** (Phase 6)
- [ ] CLAUDE.md / README.md / learnings reflect new state (Phase 7)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> No plan is considered DONE without passing cleanroom dogfood 12/12 GREEN. Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via cleanroom destroy/setup/journeys proves the system works — this principle is the canonical learnings of `reference_cleanroom_iter29_2026_05_05`.

### Execution

```bash
# 1. Verify pre-conditions
git status                            # working tree clean (or known state)
git log --oneline scale-b40-baseline-...HEAD | wc -l  # 12+ commits expected
task doctor                            # health check

# 2. Tag pre-dogfood snapshot
git tag -a "scale-b40-pre-dogfood-$(date +%Y%m%d)" -m "All phases complete"

# 3. Run cleanroom dogfood (destroys all 4 k3d clusters!)
bash infra/scripts/dogfood-cleanroom-loop.sh

# 4. On GREEN: tag + report
git tag -a "scale-b40-converged-$(date +%Y%m%d)" -m "12/12 GREEN, B40 fixed"
```

### Pass Criteria

- [ ] `dogfood-cleanroom-loop.sh` executed on clean infrastructure (post-`task clean:all`)
- [ ] Pre-Flight Check: ALL 16 checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to B40 scope (other journeys may have known gaps documented)
- [ ] **Journey-04 specifically:** `kubectl get deploy ${PROJECT}-${APP} -o jsonpath='{.spec.replicas}'` returns scaled value (3 in test fixture)
- [ ] Journey-04 idempotency: re-run scale com mesmo valor → response `status: no_change`
- [ ] Prova de Vida passes for every deploy performed during dogfood
- [ ] Prova de Morte passes for every delete performed during dogfood
- [ ] No regressions in previously passing flows (journey-01, 02, 03, 05, 06, 07, 08, 09, 10, 11)
- [ ] Observability check passes (logs, metrics, CRs in correct state)
- [ ] Rule 3 clean: `grep -E "ArgoCD|Gitea|MinIO|tenant_id|advisory lock|Kaniko|kubebuilder|envtest|GitOps|gitops-push|CRD" /tmp/journey-*.log | grep -v "^\[k3d-heal\]\|^\[_common\.sh\]"` returns 0 matches

### On Failure

If `dogfood-cleanroom-loop.sh` finds S0 or S1 issues:
1. Capture: `dogfood_state.json` + last journey log + cluster `kubectl get app -A` + `kubectl get deploy -A` + API logs since 5min
2. Classify (T0/T1/T2/T3 per `.claude/rules/pre-release-governance.md`)
3. **T0/T1 → fix + commit + re-run** cleanroom (segue Phase 3 template do plan cleanroom)
4. **T2/T3 → STOP, invocar `meeting-orchestrator`**, registrar ADR + MEET, retomar após APPROVED
5. **Max 5 iterations** per D4 of cleanroom plan; iter 6+ → escalar como problema arquitetural

S2-S4 findings documentados em report mas NÃO bloqueiam closeout (per learnings doc).

---

## Convergence Log

> Append-only durante Phase 6+. Cada iteração adiciona 1 entrada.

| Iter | Date | Failed journey | Root-cause | Fix commit | Re-run result |
|------|------|----------------|------------|------------|----------------|
| 0 | 2026-05-05 | (baseline pré-fix) | replicas stays 1 — DB orphan | n/a (proof) | `b70b0f61` RED proof committed |
| 1 | 2026-05-05 | n/a (Phase 1 GREEN) | ScalingResolver impl + 8 unit tests | `355a4668` | T1.1 GREEN |
| 1.5 | 2026-05-05 | n/a (Phase 1.2 GREEN) | Wiring via Deps + setup_orchestrator | `6bc148da` | T1.2+T2.1 GREEN, all api tests pass |
| 2 | 2026-05-05 | LIVE post-rebuild | scale handler persisted UUID, resolver expected NAME | `5ed939c9` | replicas=2 confirmed via kubectl after fix |
| 3 | 2026-05-05 | n/a (Phase 5) | journey-04 hardened with kubectl asserts | `05935c84` | journey-04 standalone GREEN 5m12s |
| 3.5 | 2026-05-06 00:00 | task clean:all (Taskfile YAML colon parse) | echo with colon → mapping | `469b69a7`+`211601c4` | task --list works |
| 4 (final) | 2026-05-06 00:52 | n/a — 12/12 GREEN | cleanroom converged | n/a | preflight 6s + 11 journeys all PASS in 3144s |

**Final outcome:** **PASS — 12/12 GREEN** (cleanroom iter 4, 2026-05-06 00:52).

- B40 chain CLOSED: loopzero scale → app_scaling_config → ScalingResolver → enforceScaling (resolved) → BuildAppSpec → renderer → Deployment.spec.replicas
- Total commits since baseline `scale-b40-baseline-20260505`: 8
- Bug discovered + fixed during cleanroom: 3 (UUID-vs-name in scale handler; Taskfile YAML colon × 2)
- Cleanroom runtime: 3144s (~52min) — within budget (target <60min)
- Rule 3 audit: 0 forbidden terms in CLI output

**Closing tag:** `scale-b40-converged-20260506`.

---

## Notes

**Não-objetivos deste plano:**
- Auto-deploy implícito ao `loopzero scale` (D3 explicit)
- Cell-aware scaling (single cell scope; multi-cell handled by existing routing)
- HPA metric customization beyond CPU (current spec)
- Vertical Pod Autoscaler (out of scope)

**Rollback strategy:**
- All changes additive em api/internal/services/scaling_resolver.go (NEW file) + minor edits em build_orchestrator_submit.go (~10 lines)
- Revert path: `git revert <commits>` + remove `app_scaling_config` rows manually if needed (DB is authoritative; remove rows = revert to yaml-only)
- No DB migration adicional — existing 000009 sufficient

**Risk register:**
- **R1 (Medium):** race scale↔deploy may produce stale BuildAppSpec. Mitigation: tx isolation + comment in code (T2.1).
- **R2 (Low):** ScalingResolver query latency adds ~5-15ms per deploy. Mitigation: indexed lookup (idx_app_scaling_config_lookup já existe em migration 000009).
- **R3 (Low):** existing tests pass `nil` resolver → silent fallback. Mitigation: T1.2 tests both paths explicitly.
- **R4 (Medium):** KEDA may not be installed in standalone — T5.2 already gates on CRD presence.
