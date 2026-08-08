---
type: plan
title: "Plan: Comprehensive Quality Gates — Every Change Tested"
status: deprecated
---

# Plan: Comprehensive Quality Gates — Every Change Tested

> **Version 1.1** (2026-05-21 — edge case review incorporated, 6 MUST FIX
> items addressed via T7.4 new + T2.2/T5.1/T5.2/T5.3/T6.2 amendments + 6
> SHOULD TEST items folded into TDD blocks + 3 DOCUMENT items captured
> in new "Risk Acknowledgements" section)
>
> **Version 1.0** (2026-05-21) — Estabelece um sistema rigoroso de Quality Gates que torna estatisticamente impossível mergeear código sem testes adequados. Mede coverage real (não apenas linhas), enforça mutation score em paths críticos, bloqueia PRs que reduzem coverage, obriga regression test para todo bug fix, detecta flaky tests automaticamente, e impõe SLO de runtime de testes. Outcome: passar de "alguns testes existem" para "toda alteração comprovadamente testada antes de chegar a `main`".

## Context

### O que existe hoje (evidência mensurada 2026-05-21)

| Componente | Estado | Evidência |
|------------|--------|-----------|
| Testes Go | 581 `*_test.go` files | `find . -name "*_test.go" \| wc -l` |
| Testes TS/JS | 90 `.test.ts`/`.spec.ts` files | `find cli/ -name "*.test.ts" \| wc -l` |
| Testes hermetic bash | 9 suites GREEN (294+ assertions) | `infra/tests/scripts/` |
| Testes helm render-time | 22 packages / ~210 assertions / 22 GREEN | Plano `2026-05-20-helm-render-time-testing-terratest` |
| Coverage Go (sample) | pkg/errors=84.8%, pkg/errors/http=93.8% | `go test -coverprofile` |
| Coverage CLI | uploaded como artifact em `cli-commands.yml`, **não enforced** | `.github/workflows/ci.yml:run: cd cli && npm test -- --ci --coverage` |
| Pacotes Go sem testes | `api/internal/server`, `api/internal/db/migrations*` | `find api/internal -maxdepth 2 -type d` sem `*_test.go` |
| Regression markers em testes | 134 files mencionam "regression" ou "Bug #" | `grep -rln` |
| Threshold de coverage enforced | **ZERO — não há gate que bloqueia PR** | `grep -rE "coverageThreshold" ci/` retorna vazio |
| Mutation testing | **NÃO existe** | `find . -name "stryker*" -o -name ".mutation*"` vazio |
| Flaky test detection | **NÃO existe** | nenhum retry/quarantine system |
| PR template gate | **NÃO existe** `.github/PULL_REQUEST_TEMPLATE.md` | `ls .github/PULL_REQUEST_TEMPLATE*` falha |
| Test pyramid validation | **NÃO existe** | nenhum gate mede unit:integration:E2E ratio |
| Test runtime SLO | **NÃO existe** | testes podem rodar arbitrariamente lento |

### Gaps reais (admitidos honestamente)

1. **Coverage pode despencar invisivelmente.** Hoje qualquer PR pode reduzir coverage de api/internal para 0% e mergear. O artifact é uploaded mas nenhum gate compara baseline ↔ PR.

2. **Coverage % é teatro sem mutation score.** 80% de linhas cobertas com `require.NoError(err)` é vacuoso. Sem mutation testing não há prova de que os testes realmente exercem comportamento.

3. **Bug fix sem regression test é regra, não exceção.** Não há gate que verifique: se commit message diz "fix #123", existe teste novo associado ao bug?

4. **Teste deletado é invisível.** Sem audit trail "este teste foi removido em commit X com justificativa Y", coverage pode subir artificialmente deletando testes que falham.

5. **Testes flaky degradam confiança.** Sem detecção automática, testes flaky ficam meses sendo "re-runzados" pelo dev, ensinando o time a ignorar falhas.

6. **Test runtime sem SLO.** Suite que cresce indefinidamente até "demora 30min em CI" mata o feedback loop. Hoje o helm-render leva ~6min e não há gate alertando se cresce para 30min.

7. **Test pyramid invertida silenciosamente.** Se equipe escreve só E2E porque "é mais fácil testar com k3d up", a pyramid inverte sem ninguém perceber. Sem gate, isso não é detectado.

8. **`t.Skip()` permanente sem issue link.** Testes skipados sem rastro viram dead code. Sem gate, fácil esconder problemas.

9. **Mock budget infinito.** Service com 10 mocks indica design ruim (SOLID/SRP violado). Sem gate quantificando, design degrada.

### Evidência específica do repo

- `api/internal/server` package **sem testes** (descoberto na survey desta task)
- `cli-commands.yml` workflow uploads coverage mas não threshold-blocks: `cli/coverage/` retention 7 dias, nenhum gate
- `infra/scripts/migration_test_coverage.sh` é o único exemplo de coverage-driven gate no repo (migrations only)
- 8 commits da rebase recente (commits de outras PRs) tocavam test files que eu modifiquei — sinal de que múltiplas mãos editam testes sem coordenação

### Por que agora

CLAUDE.md Rule 11: "Every claim affecting maturity score requires 3 evidence sources (T1 commit+test, T2 LIVE log, T3 CI run)". Hoje T1 é provável (commits + testes), T2 e T3 são frágeis (testes podem ser quebrados ou removidos sem alerta). Quality Gates rigorosos transformam Rule 11 de aspirational em enforced.

CLAUDE.md Rule 1: "Tests before code". Sem gate, essa rule é honor-system. Com Quality Gates, vira branch protection rule.

Per plano helm-render-time encerrado hoje: a sessão revelou que mesmo plano com 22 packages GREEN tinha b9f19dc8 build-break invisível pré-build-gate. Build-gate fechou ESSA classe; este plano fecha as 11 classes restantes.

## Objective

**Done:** todo PR para `main` ou `develop` passa por bateria de Quality Gates que (a) mede coverage real per package contra baseline e bloqueia regressão, (b) mede mutation score em paths críticos e bloqueia mergeear código com testes vacuosos, (c) exige regression test associado a todo commit que menciona "fix" + número de issue, (d) detecta + quarentena testes flaky automaticamente, (e) impõe SLO de runtime per test + per suite, (f) valida test pyramid ratio, (g) impede `t.Skip` sem link de issue, (h) limita mocks per package, (i) audita testes deletados. Tudo enforced em CI via Required Status Checks. Coverage baseline ≥80% per package + ≥90% para `pkg/`.

**Metas mensuráveis:**

- ≥80% line coverage em todos os 41 packages Go de `api/internal/`
- ≥90% line coverage em todos os 11 packages de `pkg/`
- ≥75% line coverage em `cli/src/` (TS)
- ≥60% mutation score em paths críticos (auth, lock, statemachine, naming, errors)
- Zero PRs mergeáveis sem coverage report
- Zero testes flaky em main por > 7 dias (auto-quarentena + issue)
- Test pyramid: unit ≥70%, integration ≤25%, E2E ≤5% (count-based)
- Suite runtime SLO: nenhum teste > 60s; nenhuma suite > 10min
- 100% dos bug fixes referenciam issue + linkam regression test no PR

## ADRs

### D1 — Coverage threshold enforcement via diff-aware tool (não global %)

**Decisão:** Adotar `diff-cover` (Python) ou `coverage-diff` para validar APENAS linhas tocadas pelo PR, não % global. Para Go usar `gocov-html` + script comparando per-package baseline.

**Rationale:**
- Threshold global cria perverse incentive: dev escreve testes para módulos triviais para subir o número, ignora paths críticos
- Diff-aware enforça "linha que você tocou deve estar coberta" — disciplina por código, não por package
- Combinado com per-package baseline tracking (não pode REGREDIR), cobre tanto o novo quanto o existente

**Consequences:**
- (+) Gate enforça disciplina onde importa (código novo)
- (+) PRs grandes não falham só porque um arquivo legado tem 50% coverage
- (−) Requer tooling (diff-cover) novo em CI
- (−) Baseline tracking requer storage (artifact ou checked-in JSON)

### D2 — Mutation testing scope: critical paths only, NOT all code

**Decisão:** Mutation testing roda APENAS em `pkg/{errors,jwt,naming,statemachine,enforcement,lock,id}` + `api/internal/{services,middleware/auth}`. NÃO em rotas, NÃO em init code, NÃO em testes.

**Rationale:**
- Mutation testing é caro (5-30min per package) — full repo seria 8+ horas
- Critical paths são onde bug = data loss / auth bypass / silent corruption
- Routes são thin wrappers sobre services; services são onde lógica vive
- Per Microsoft 2026 + Pitest community: 60% mutation score é forte; 80% é excepcional; 100% impossível (equivalent mutants)

**Consequences:**
- (+) Mutation cost ~5-10min em CI (não 8h)
- (+) Foca onde matters (cryptography, state, locks)
- (−) Outras áreas mantêm só line coverage (pode ter vacuous tests)
- (−) Operador precisa lista mantida do que é "critical path"

### D3 — Regression test enforcement via commit message lint

**Decisão:** Hook + CI gate que parsia commit messages. Se message contém `fix #N`, `closes #N`, `Bug #N`, OU se files tocados estão em allowlist de "bug-prone areas" (`api/internal/services/`, `pkg/statemachine/`, etc), então: PR DEVE conter pelo menos 1 novo teste OU file diff que adiciona teste para regressão. Se não, gate falha.

**Rationale:**
- Honor-system não funciona (134 files mencionam "Bug #" mas não há gate)
- Bug fix sem regression test = bug volta em 6 meses, "déjà vu"
- Allowlist de "bug-prone areas" captura PRs que tocam zonas críticas mesmo sem mencionar bug

**Consequences:**
- (+) Bug fixes traceable (PR → regression test → original bug)
- (+) Dev forçado a pensar "como esse bug volta?"
- (−) PRs cosméticos (typos em comments) tocando bug-prone area falham loud — precisa allowlist secundário ou `[skip-regression-test]` token
- (−) Determinar "novo teste" via diff requires parser

### D4 — Flaky test detection: retry 2× + auto-quarentena

**Decisão:** CI roda teste 1 vez. Se falha, re-run 1× automaticamente. Se passa no retry, marca como FLAKY no relatório. Se acumular 3 marcações FLAKY em 7 dias, gate automaticamente adiciona `t.Skip("FLAKY — see issue #N")` + abre issue. Quarentena tem TTL de 14 dias antes de delete automático.

**Rationale:**
- Re-run silencioso ensina time a confiar em "às vezes falha" — mata signal
- Quarentena explícita + issue força ownership (alguém tem que consertar antes de 14 dias)
- 14d TTL prevents quarantine virar dead code permanente

**Consequences:**
- (+) Flakies não corrompem o feedback loop
- (+) Audit trail (issue) permite priorização
- (−) Requer infra de retry + state tracking entre runs (DB ou artifact)
- (−) Dev precisa aceitar que retry-passou ainda é red flag

### D5 — Test runtime SLO: per-test 60s, per-suite 10min, hard fail

**Decisão:** Cada teste individual TEM 60s budget; cada suite (`go test ./pkg/X/...`) TEM 10min budget. Excedeu = CI fail hard. Operador pode opt-out per-test via tag `// slow_test` + issue link.

**Rationale:**
- Without SLO, suites crescem para 30-60min ("é só CI demorar mais")
- Feedback loop > 15min = dev não roda local, só commita e espera
- Per-test 60s coage developer a usar mocks/setup compartilhado quando apropriado
- Per-suite 10min força paralelismo ou split

**Consequences:**
- (+) Feedback loop preservado
- (+) Force devs a otimizar setup compartilhado
- (−) Tests legítimos lentos (integration, envtest) precisam tag explícita + justification
- (−) Pode mascarar testes que precisam ser lentos para serem corretos

### D6 — Coverage baseline: per-package JSON checked into repo

**Decisão:** `infra/quality/coverage-baseline.json` checked into repo, atualizado por bot pós-merge em main. Estrutura: `{ "package/path": { "line_coverage": 84.8, "min_required": 80 } }`. CI compara baseline vs PR coverage; se cai abaixo do `min_required` OU abaixo do baseline-3pp, fail.

**Rationale:**
- Checked-in baseline torna mudança visível (PR review pode ver quando alguém abaixa min_required)
- Per-package granularity evita "global coverage subiu, mas auth caiu de 95% para 60%" caso
- 3pp slack tolerância tolera flutuação natural de medições

**Consequences:**
- (+) Auditable + reviewable
- (+) Per-package = pressure where it counts
- (−) Manual operation when adicionar package (precisa atualizar baseline)
- (−) Discordância de versões de Go pode flutuar coverage

### D7 — Test pyramid validation via test naming convention

**Decisão:** Test files seguem convenção: `*_test.go` = unit (mock everything); `*_integration_test.go` = integration (real DB/K8s); `*_e2e_test.go` = E2E. Script conta files por categoria; ratio devem ser unit ≥70%, integration ≤25%, E2E ≤5%. Fail CI se ratio fora dos limites.

**Rationale:**
- Convenção textual permite gate simples (sem AST parsing)
- Pyramid invertida = sintoma de design ruim ou laziness
- Limites quantitativos forçam discussion when crescem unilateralmente

**Consequences:**
- (+) Gate simples (file count + naming)
- (+) Disciplina visível no nome do file
- (−) Operador deve renomear files que não seguem convenção
- (−) Pode subestimar integration se testes integration vivem em `*_test.go` sem renaming

### D8 — Mock budget: max 5 mocks per service test file

**Decisão:** Gate conta usos de `gomock.NewController`, `mock.NewMock*`, `jest.fn()`, `vi.fn()` per file. Limite: 5 mocks por file. Excedeu = fail com mensagem "design smell — considere refactor ou split file".

**Rationale:**
- 10+ mocks per file = service viola SRP, está fazendo muita coisa
- Limite arbitrário (5) mas força conversation antes de gerar tech debt

**Consequences:**
- (+) Catches design degradation early
- (−) Pode falhar legitimately em service complexo — opt-out via comment `// mock-budget: 8 — justified by X`

### D9 — `t.Skip()` requires issue link OR `_skip_test.go` filename

**Decisão:** AST scanner valida que toda chamada a `t.Skip(*)` tem string contendo `https://github.com/loopzero/loopzero/issues/N` OU o file termina em `_skip_test.go` (intentional permanent skip with reason in comment). Tudo mais = fail.

**Rationale:**
- Skip sem issue = dead code com aparência de coverage
- Issue link força ownership + visibilidade

**Consequences:**
- (+) No invisible debt
- (−) Operador legacy precisa migrar skips existentes

### D10 — Test deletion audit via git pre-receive hook

**Decisão:** PR que deleta `*_test.go` ou `*.test.ts` files DEVE incluir line em PR description: `Test deletion justification: <reason>` ou label `test-deletion-approved`. CI gate verifica + bloqueia merge sem.

**Rationale:**
- Test deletion é forma silenciosa de aumentar coverage % (gameable)
- Audit trail ensures intentional + reviewed

**Consequences:**
- (+) Coverage % cannot be gamed via deletion
- (−) PRs grandes que reorganizam tests precisam justificar explicitly

### D11 — Two-tier critical-path coverage: 90% line + 60% mutation

**Decisão:** `pkg/{auth,jwt,statemachine,naming,errors,lock,enforcement,id}` packages têm DOIS gates: line coverage ≥90% AND mutation score ≥60%. Outros packages: line only ≥80%.

**Rationale:**
- Crypto/state/lock são onde silent bugs causam data loss/auth bypass
- 90% line floor evita gaps; 60% mutation score evita vacuous tests
- Outros packages: line coverage suficiente; mutation expensive

**Consequences:**
- (+) Crypto + auth + state get scrutiny they need
- (−) Maintenance overhead for those packages higher
- (−) Adding new "critical" package requires adding to allowlist

### D12 — PR template enforcement + auto-checklist

**Decisão:** `.github/PULL_REQUEST_TEMPLATE.md` com checklist obrigatório (test added? regression test? coverage diff?). GitHub Actions `template-compliance.yml` gate verifica checkboxes marked. Unchecked = fail.

**Rationale:**
- Template suaviza process for dev (não precisa lembrar)
- Gate enforça (não é honor system)

**Consequences:**
- (+) PR review starts with proof of testing discipline
- (−) Tediousness se template grande — mantém curto (5 checkboxes max)

### D13 — Dogfood Gate as final boundary (per CLAUDE.md)

**Decisão:** Quality Gates não substituem Dogfood Gate. Dogfood Gate é não-negociável (per CLAUDE.md), testa runtime real. Quality Gates protegem o caminho ATÉ dogfood; dogfood prova que funciona end-to-end.

**Rationale:**
- Tests passing ≠ system works (per CLAUDE.md TDD rule)
- Dogfood é tier 3; Quality Gates são tier 0-2

**Consequences:**
- (+) Camadas independentes catching different bug classes
- (−) Dogfood ainda requer operator + clean infra + ~45min

## Dependency Graph

```
Phase 0 (Governance + ADRs)
    │
    ▼
Phase 1 (Coverage Infrastructure)
    │
    ├─────────────────┬─────────────────┐
    ▼                 ▼                 ▼
Phase 2          Phase 3           Phase 4
(Coverage CI    (Mutation        (Per-domain
 Gates)          Testing)         coverage push)
    │                 │                 │
    └─────────────────┴─────────────────┘
                      │
                      ▼
Phase 5 (Regression Discipline)
                      │
                      ▼
Phase 6 (Runtime SLO + Flaky + Pyramid)
                      │
                      ▼
Phase 7 (Docs + Operator Handoff)
                      │
                      ▼
                  Dogfood Gate
```

- **Phase 0** blocks all others (ADRs need ratification)
- **Phases 2, 3, 4** parallelizable after Phase 1
- **Phase 5** blocks on Phase 2 (uses coverage tooling)
- **Phase 6** blocks on Phase 1 (test inventory needed)
- **Phase 7** blocks on all prior phases

---

## Phase 0: Governance + ADR Ratification

**Objective:** ratificar ADRs D1-D13 via MEET record antes de qualquer code change. Quality Gates são T2/T3 mudança (afeta TODO PR no projeto) → requer governance review per `.claude/rules/pre-release-governance.md`.

### T0.1 — Ratify ADRs via Operator Review

#### Objective
Sponsor + 5 specialist personas review ADRs D1-D13 + ratify or amend.

#### Evidence
- 13 ADRs definidos no preâmbulo deste plano
- Cada um afeta TODO PR no projeto = T2/T3 mudança per governance
- Sem ratificação, gates não podem ir live (blocking branch protection rules)

#### Files to edit
```
.claude/knowledge-base/meetings/MEET-2026-05-21-quality-gates.md (NEW) — ratification record
docs/plans/2026-05-21-comprehensive-quality-gates-plan.md — esta versão v1.0 contains ADRs D1-D13
```

#### Deep file dependency analysis
- MEET file segue formato existente (e.g., `MEET-2026-05-20-helm-render-tests.md`)
- Plan file referenced from MEET via `plan:` frontmatter field

#### Tasks
1. Sponsor reads plan v1.0
2. Specialist personas review ADRs (sync ou via spec subagents)
3. Each ADR ratified, amended, or rejected explicitly
4. MEET record written with decisions + alternatives rejected
5. Plan version bumped to v1.1 if ADR amended

#### TDD
```
N/A — governance task, no code
```

#### Acceptance Criteria
- [ ] MEET-2026-05-21-quality-gates.md exists with ratification block signed
- [ ] All 13 ADRs marked Ratified | Amended | Rejected
- [ ] Plan references MEET via cross-ref

#### DoD
- [ ] MEET file committed
- [ ] Plan v1.0 or v1.1 explicitly referenced from MEET
- [ ] No engineering blocked by un-ratified ADRs

---

## Phase 1: Coverage Measurement Infrastructure

**Objective:** instalar ferramentas + baseline data + CI plumbing para medir coverage Go + TS sem ainda enforçar gates.

### T1.1 — Coverage tooling: Go + TS setup

#### Objective
Instalar `go test -coverprofile` discipline + `jest --coverage` consistent reporting across the repo.

#### Evidence
- Go já tem `go test -cover` builtin; falta uso disciplinado per-package
- CLI já roda `npm test -- --ci --coverage` mas só uploads artifact

#### Files to edit
```
taskfiles/test.yml — add task test:coverage:go + test:coverage:cli + test:coverage:report
infra/scripts/quality/coverage-go.sh (NEW) — collect coverage per Go package, merge into single profile
infra/scripts/quality/coverage-cli.sh (NEW) — wrap npm test --coverage with consistent output
infra/scripts/quality/coverage-merge.sh (NEW) — merge Go + TS into unified report (lcov format)
.github/workflows/coverage-report.yml (NEW) — runs on push/PR, uploads artifact + comments diff
```

#### Deep file dependency analysis
- `taskfiles/test.yml` already has helm:test aliases (1187 LOC) — append new targets
- Existing `cli/coverage/` directory consumed by ci.yml — repurpose
- New `infra/scripts/quality/` directory — new convention

#### Deep Dives
- Go coverage: `go test -coverprofile=/tmp/c.out -covermode=atomic ./...` → merge per-package via `gocovmerge`
- TS coverage: `jest --coverage --coverageReporters=lcov` → `cli/coverage/lcov.info`
- Merge: `lcov --add-tracefile`
- Edge case: vendored deps — use `-coverpkg=$(go list ./... | grep -v vendor)`

#### Tasks
1. Write `infra/scripts/quality/coverage-go.sh` (collects all Go packages)
2. Write `infra/scripts/quality/coverage-cli.sh` (wraps Jest)
3. Write `infra/scripts/quality/coverage-merge.sh` (lcov merge)
4. Add `task test:coverage` orchestration target
5. Add `.github/workflows/coverage-report.yml` (upload artifact, comment PR)
6. Add `gocovmerge` binary install in CI

#### TDD
```
RED:     test_coverage_go_handles_empty_package (no .go files) — must exit 0
RED:     test_coverage_go_excludes_vendor (vendor/* not in output)
RED:     test_coverage_cli_writes_lcov (cli/coverage/lcov.info exists post-run)
RED:     test_coverage_merge_combines_go_ts (single lcov has both)
RED:     test_coverage_go_handles_doc_only_package (EC-10) — package with
         only doc.go (zero testable statements) excluded from report, no error
GREEN:   Implement scripts to pass
REFACTOR: Extract lcov path constants
VERIFY:  bash infra/tests/scripts/coverage-go_test.sh
```

#### Acceptance Criteria
- [ ] `task test:coverage` runs both Go + CLI coverage, outputs single lcov
- [ ] Coverage report uploaded as artifact in every PR run
- [ ] PR description gets coverage diff comment (auto-bot)
- [ ] All scripts have hermetic test in `infra/tests/scripts/`
- [ ] Pass: file ≤500 lines each
- [ ] Pass: shellcheck warning+ EXIT=0

#### DoD
- [ ] Coverage measurable per-package locally + CI
- [ ] No gate enforced yet (Phase 2 job)
- [ ] Hermetic tests GREEN

### T1.2 — Baseline coverage snapshot

#### Objective
Run full coverage today, snapshot per-package %, commit as `infra/quality/coverage-baseline.json`.

#### Evidence
- Phase 2 gates need baseline reference
- Per D6: checked-in baseline auditable in PR review

#### Files to edit
```
infra/quality/coverage-baseline.json (NEW) — per-package coverage % + min_required
infra/scripts/quality/baseline-update.sh (NEW) — bot-run script post-merge to main
.github/workflows/baseline-update.yml (NEW) — automation
```

#### Deep file dependency analysis
- JSON schema: `{ "go/pkg/path": { "line": 84.8, "min_required": 80, "last_updated": "2026-05-21" } }`
- baseline-update.sh runs on main merge, regenerates baseline from current coverage
- Pre-commit hook (optional) validates baseline syntax

#### Deep Dives
- Initial min_required = floor(current_coverage - 5pp). Floor ensures even bad packages have target.
- Critical packages (per D11) have explicit min_required=90
- Sub-packages inherit parent min_required unless explicitly overridden

#### Tasks
1. Run `task test:coverage` to get current state
2. Parse lcov → per-package JSON
3. Set initial min_required per D11 (critical=90, default=floor-5pp)
4. Commit baseline
5. Write baseline-update.sh
6. Add baseline-update.yml (post-merge to main)

#### TDD
```
RED:     test_baseline_json_valid (jq parse succeeds)
RED:     test_baseline_has_critical_packages (pkg/jwt, pkg/statemachine, etc minRequired=90)
RED:     test_baseline_update_idempotent (run 2x produces same JSON)
GREEN:   Implement
REFACTOR: None
VERIFY:  bash infra/tests/scripts/baseline_test.sh
```

#### Acceptance Criteria
- [ ] Baseline JSON committed, parseable
- [ ] All Go + TS packages represented
- [ ] Critical packages have min_required=90
- [ ] baseline-update.sh + workflow exist
- [ ] Hermetic test passes

#### DoD
- [ ] Baseline file referenced by Phase 2 gates
- [ ] Workflow updates baseline on main merge

### T1.3 — Test inventory script

#### Objective
Single command catalogs ALL tests in repo across Go, TS, bash, with category (unit/integration/E2E) per D7.

#### Evidence
- D7 enforces pyramid; need inventory to validate
- 581 Go + 90 TS + 9 bash currently uncategorized

#### Files to edit
```
infra/scripts/quality/test-inventory.sh (NEW) — emit JSON catalog
infra/quality/test-inventory.json (NEW, generated) — checked-in catalog snapshot
```

#### Deep file dependency analysis
- Script walks repo, classifies by filename convention (per D7)
- JSON output: { "go/api/internal/services/builds_test.go": {"category":"unit","test_count":12} }
- Checked-in snapshot allows tracking of count drift over time

#### Tasks
1. Write classifier: `*_test.go` → unit (default); `*_integration_test.go` → integration; `*_e2e_test.go` → E2E
2. Extract test function count per file (`grep "^func Test"` or similar)
3. Output JSON
4. Commit snapshot
5. CI runs script + diff vs snapshot, alerts on +50% growth in any category

#### TDD
```
RED:     test_inventory_classifies_unit (*_test.go → unit)
RED:     test_inventory_classifies_integration (*_integration_test.go → integration)
RED:     test_inventory_counts_test_functions
RED:     test_inventory_diff_alerts_on_pyramid_inversion
GREEN:   Implement
VERIFY:  bash infra/tests/scripts/test-inventory_test.sh
```

#### Acceptance Criteria
- [ ] Inventory JSON committed
- [ ] 581 Go files classified
- [ ] 90 TS files classified
- [ ] Hermetic test passes
- [ ] CI alerts on pyramid inversion

#### DoD
- [ ] Phase 6 pyramid gate consumes this output
- [ ] Inventory regeneratable in <10s

---

## Phase 2: Quality Gates in CI (Coverage Enforcement)

**Objective:** Phase 1 measures, Phase 2 enforces. Make PR fail if coverage drops below baseline or new code uncovered.

### T2.1 — Coverage diff gate (PR-blocking)

#### Objective
For every PR, compute coverage of LINES TOUCHED. Fail if < 80% covered by new tests.

#### Evidence
- Per D1: diff-aware gate avoids global-% perverse incentives
- 134 files mention "regression" but no gate verifies PR-test ratio

#### Files to edit
```
.github/workflows/coverage-diff-gate.yml (NEW)
infra/scripts/quality/coverage-diff.sh (NEW) — wrap diff-cover for Go + TS
```

#### Deep file dependency analysis
- Workflow runs on PR
- Computes coverage on PR head + base
- Diff-cover runs per file in `git diff base..head`
- Fails CI if any touched line uncovered (with threshold per D1)

#### Deep Dives
- diff-cover supports `--compare-branch=origin/main` + `--fail-under=80`
- For Go: requires gocov-html intermediate
- Edge case: PR that only renames files (no new code) → diff is empty → PASS
- Edge case: PR that adds only test files → diff in test files → trivially 100% coverage → PASS

#### Tasks
1. Install diff-cover in CI
2. Wrap with coverage-diff.sh handling Go + TS lcov merge
3. Workflow runs on PR, uploads HTML report artifact
4. Comments PR with summary table
5. Fails CI if threshold breached

#### TDD
```
RED:     test_diff_gate_passes_when_new_code_covered
RED:     test_diff_gate_fails_when_new_code_uncovered (≥1 line < 80%)
RED:     test_diff_gate_ignores_renamed_files
RED:     test_diff_gate_passes_for_test_only_PRs
RED:     test_coverage_diff_passes_for_rename_only_PR (EC-7) — PR
         renaming 5 files without content change must PASS without error
GREEN:   Implement
VERIFY:  bash infra/tests/scripts/coverage-diff_test.sh
```

#### Acceptance Criteria
- [ ] Gate runs on every PR
- [ ] Fails PR when new code <80% covered
- [ ] PR comment shows touched-vs-covered table
- [ ] Artifact uploaded with HTML breakdown
- [ ] Required Status Check on develop + main branches

#### DoD
- [ ] No PR can merge to main with uncovered new code
- [ ] Hermetic test GREEN

### T2.2 — Per-package baseline regression gate

#### Objective
PR must not reduce any package's coverage below baseline-3pp (D6 tolerance).

#### Files to edit
```
.github/workflows/coverage-baseline-gate.yml (NEW)
infra/scripts/quality/baseline-compare.sh (NEW)
```

#### Deep Dives
- Read `infra/quality/coverage-baseline.json`
- For each package in PR diff, compute current coverage
- Compare against baseline.min_required and baseline.line-3pp
- Fail if any package regresses
- **EC-2 escape valve:** PR label `coverage-baseline-defer` allows
  one-PR exception (7-day TTL recorded in baseline JSON via deferral
  block); bot opens auto-issue for follow-up coverage push. Used for
  legitimate package splits where tests can't be written before the
  split commit. Without escape valve, refactors are bloqueados
  ("chicken-and-egg" — package created with 0% baseline, gate fails).

#### TDD
```
RED:     test_baseline_gate_fails_when_pkg_drops_below_min
RED:     test_baseline_gate_passes_with_3pp_slack
RED:     test_baseline_gate_handles_new_package (uses default 80%)
RED:     test_baseline_gate_honors_defer_label (EC-2) — PR with
         `coverage-baseline-defer` label + reason in body PASSES gate +
         writes deferral entry to baseline JSON with 7-day TTL
RED:     test_baseline_gate_rejects_defer_without_reason (EC-2 guard) —
         label without "Defer reason:" line in body still FAILS
GREEN:   Implement
VERIFY:  bash infra/tests/scripts/baseline-compare_test.sh
```

#### Acceptance Criteria
- [ ] Gate uses baseline JSON
- [ ] Per-package comparison
- [ ] 3pp slack honored
- [ ] New packages get default threshold

#### DoD
- [ ] Coverage regression impossível invisivelmente
- [ ] Required Status Check active

### T2.3 — Code-audit lint integration

#### Objective
Wire `task lint` results into PR gate. Zero warnings required.

#### Files to edit
```
.github/workflows/lint-gate.yml — existing CI lint hardened to fail on warnings (currently warnings tolerated in some packages)
.golangci.yml — config tightened
cli/.eslintrc.* — config tightened
```

#### TDD
```
RED:     test_lint_gate_fails_on_warning
RED:     test_lint_gate_passes_clean_repo
GREEN:   Tighten configs
VERIFY:  task lint exits 0 on clean repo
```

#### Acceptance Criteria
- [ ] `task lint` exit 0 on clean repo
- [ ] Any warning = fail
- [ ] Required Status Check

#### DoD
- [ ] Zero warnings tolerated in main

---

## Phase 3: Mutation Testing for Critical Paths

**Objective:** Install mutation testing tools, run on critical-path packages, fail PR if mutation score drops.

### T3.1 — Install + configure mutation framework

#### Objective
Install `gremlins` (Go) + `stryker-mutator` (TS) on critical packages only per D2/D11.

#### Files to edit
```
infra/tools/gremlins.yaml (NEW) — Gremlins config: critical packages list
cli/stryker.conf.mjs (NEW) — Stryker config for CLI
infra/scripts/quality/mutation-run.sh (NEW)
.github/workflows/mutation-gate.yml (NEW)
```

#### Deep Dives
- Gremlins config: `target.packages = ["pkg/jwt/...", "pkg/statemachine/...", "pkg/lock/...", "pkg/errors/..."]`
- Mutation score: killed_mutants / total_mutants
- Threshold: 60% per D11
- Runtime budget: ~10min in CI (single-shot per PR, cached between)

#### TDD
```
RED:     test_mutation_runs_on_critical_pkg_only
RED:     test_mutation_score_calculated_correctly
RED:     test_mutation_fails_below_60_pct
GREEN:   Configure + script
VERIFY:  task quality:mutation
```

#### Acceptance Criteria
- [ ] Gremlins runs on 8 critical Go packages
- [ ] Stryker runs on `cli/src/auth/*` + `cli/src/output/*` (critical CLI)
- [ ] Mutation report uploaded
- [ ] Threshold 60% enforced
- [ ] CI budget < 15min

#### DoD
- [ ] Mutation gate Required Status Check on critical paths
- [ ] Hermetic test that score calc correct

### T3.2 — Baseline mutation scores

#### Objective
Establish per-package mutation baseline checked into repo (similar to coverage baseline).

#### Files to edit
```
infra/quality/mutation-baseline.json (NEW) — { "pkg/path": { "score": 0.65, "min_required": 0.60 } }
```

#### TDD
```
RED:     test_mutation_baseline_valid_json
RED:     test_mutation_baseline_has_critical_packages
RED:     test_mutation_baseline_run_3_times_within_5pp_variance (EC-11) —
         run mutation 3× on same commit; variance between scores MUST
         be <5pp. If ≥5pp, baseline uses mean-of-3 OR seed fixed via
         `--seed=42`. Catches non-deterministic mutation framework flap.
GREEN:   Run mutation once, commit baseline
VERIFY:  task quality:mutation:baseline
```

#### Acceptance Criteria
- [ ] Baseline JSON committed
- [ ] All critical packages have score ≥60%
- [ ] If <60%, add issue + opt-out comment in JSON
- [ ] Updates via bot on main merge

#### DoD
- [ ] Phase 4 push for sub-60% packages

---

## Phase 4: Per-Domain Coverage Push to Baseline

**Objective:** Raise low-coverage packages to ≥80% (default) or ≥90% (critical per D11). Phase 1 measured; Phase 4 fixes.

### T4.1 — Survey current Go coverage per package

#### Objective
Identify all Go packages below baseline + create issues + assign owners.

#### Files to edit
```
infra/quality/coverage-gaps-go.md (NEW, generated) — list of packages + current % + target
```

#### TDD
```
RED:     test_gap_report_lists_packages_below_threshold
GREEN:   Script generates report
```

#### Acceptance Criteria
- [ ] Gap report committed
- [ ] All packages <80% have GitHub issue
- [ ] Owner assigned per package (engineering team rotation)

#### DoD
- [ ] Visible backlog for Phase 4.2+

### T4.2 — Raise pkg/* to 90%

#### Objective
All `pkg/` packages reach ≥90% line coverage.

#### Files to edit
```
pkg/{N}/*_test.go — add tests for uncovered branches
```

#### Deep Dives
- pkg/errors at 84.8% — gap likely in error wrapping edge cases
- pkg/statemachine — likely missing tests for invalid transitions
- pkg/jwt — token expiry + malformed scenarios

#### TDD
```
RED:     For each gap, write test that fails because no current coverage
GREEN:   Coverage rises
VERIFY:  task test:coverage:pkg + diff vs baseline shows ↑
```

#### Acceptance Criteria
- [ ] Each pkg/* package ≥90% coverage
- [ ] Mutation score ≥60% (D11)
- [ ] Baseline JSON updated post-merge

#### DoD
- [ ] All pkg/ packages above threshold
- [ ] No regression in other packages

### T4.3 — Raise api/internal/* to 80%

Same pattern as T4.2 for 41 api/internal packages. NOTE: `api/internal/server` currently has ZERO tests — needs significant work.

### T4.4 — Raise operators/* to 80%

Same pattern for 8 operators. envtest already exists; gap is in unit tests for predicates + filter functions.

### T4.5 — Raise cli/src/* to 75%

Same pattern for CLI commands (65 commands; many command files lack unit tests covering exit codes + --json schema).

---

## Phase 5: Regression Test Discipline (PR Workflow)

**Objective:** enforce per D3 + D12 that every bug fix has a regression test, every PR has compliance.

### T5.1 — PR template + gate

#### Objective
Create PR template; gate enforces checkboxes per D12.

#### Files to edit
```
.github/PULL_REQUEST_TEMPLATE.md (NEW)
.github/workflows/template-compliance.yml (NEW)
```

#### Deep Dives
- Template has 5 checkboxes:
  - [ ] Tests added for new code (or N/A justified below)
  - [ ] Regression test added if fixing bug (or N/A)
  - [ ] Coverage diff PASS
  - [ ] No tests deleted (or justification below)
  - [ ] CHANGELOG entry added (or N/A)
- Gate parses PR body, fails if checkboxes unmarked
- **EC-5 bot allowlist:** Gate SKIPS template check if PR author is in
  `[dependabot[bot], renovate[bot], github-actions[bot]]` OR PR has
  label `dependencies` OR `automated`. Configurable via
  `.github/quality-gates-config.yml`. Without allowlist, dep automation
  is broken → CVEs don't patch.

#### TDD
```
RED:     test_template_gate_fails_unchecked
RED:     test_template_gate_passes_all_checked
RED:     test_template_gate_accepts_n_a_with_reason
RED:     test_template_gate_skips_dependabot_PRs (EC-5) — PR authored
         by `dependabot[bot]` PASSES gate without template check
RED:     test_template_gate_skips_renovate_PRs (EC-5) — same for renovate
RED:     test_template_gate_skips_PRs_with_dependencies_label (EC-5) —
         human-opened PR with `dependencies` label also exempt
GREEN:   Implement
```

#### Acceptance Criteria
- [ ] Template renders on new PR
- [ ] Gate enforces checkboxes
- [ ] Required Status Check

#### DoD
- [ ] PRs cannot skip template

### T5.2 — Bug-fix regression test gate

#### Objective
Per D3: commit messages with `fix #N` or `Bug #N` MUST have associated new test.

#### Files to edit
```
.github/workflows/regression-test-gate.yml (NEW)
infra/scripts/quality/check-regression-test.sh (NEW)
```

#### Deep Dives
- Parse commit messages in PR + PR TITLE + PR BODY (per EC-3 fix —
  squash-merge teams lose individual commit messages; PR title/body
  remain authoritative)
- Source priority: `pull_request.title` → `pull_request.body` →
  individual commits (in that order)
- If any matches `fix #\d+|Bug #\d+|closes #\d+|fixes #\d+|resolves #\d+`,
  check PR diff for new test (function starting with `Test` in
  `*_test.go` OR new `it(...)` block in `*.test.ts`)
- Issue number from message linked to test via `// Bug #N` comment or
  `// Regression: #N` annotation

#### TDD
```
RED:     test_regression_gate_fails_on_fix_without_test
RED:     test_regression_gate_passes_on_fix_with_test
RED:     test_regression_gate_skips_typo_only_PRs
RED:     test_regression_gate_reads_PR_title (EC-3) — PR title "Fix #123:
         wrong port" + no test → FAIL; same title + new test → PASS
RED:     test_regression_gate_reads_PR_body (EC-3) — PR title generic
         + body contains "Fixes #123" + no test → FAIL
RED:     test_regression_gate_aggregates_multiple_fix_refs (EC-3) — PR
         mentioning #123 + #124 needs ≥1 regression test linkando each
GREEN:   Implement
```

#### Acceptance Criteria
- [ ] Gate detects `fix #N` patterns
- [ ] Verifies test added with matching `// Bug #N` comment
- [ ] Required Status Check

#### DoD
- [ ] Bug fixes traceable

### T5.3 — Test deletion audit

#### Objective
Per D10: PRs deleting tests require justification.

#### Files to edit
```
.github/workflows/test-deletion-gate.yml (NEW)
```

#### Deep Dives
- Use `git diff --find-renames=80%` to detect renames (D+A pairs with
  ≥80% content similarity) — they classify as RENAME, not DELETE.
  Without this, EC-6 fires false positives every time someone renames
  a test file (e.g., `defaults_test.go` → `deployment_test.go`).
- Only TRUE deletions (no matching add, or <80% similarity) require
  justification.

#### TDD
```
RED:     test_deletion_gate_fails_without_justification
RED:     test_deletion_gate_passes_with_label_or_body
RED:     test_deletion_gate_passes_for_rename (EC-6) — PR with
         git mv old.go new.go (>80% identical content) NOT flagged
RED:     test_deletion_gate_passes_for_split_into_two_files (EC-6) —
         PR with old_test.go split into a_test.go + b_test.go classified
         as 2 renames (each ≥80% match a chunk), NOT 1 deletion
RED:     test_deletion_gate_still_fails_for_true_delete — PR removing
         old_test.go without replacement still requires justification
GREEN:   Implement using `git diff --find-renames=80%` + `--diff-filter=D`
```

#### Acceptance Criteria
- [ ] Gate detects TRUE test-file deletion in PR (rename-aware)
- [ ] Requires `test-deletion-approved` label OR `Test deletion justification:` in body
- [ ] Required Status Check
- [ ] EC-6 protection: renames + splits NOT flagged

#### DoD
- [ ] Test deletion auditable
- [ ] Refactors not bloqueados por false positives

---

## Phase 6: Runtime SLO + Flaky Detection + Pyramid

**Objective:** enforce D4 (flaky) + D5 (runtime) + D7 (pyramid) + D8 (mocks) + D9 (skip).

### T6.1 — Runtime SLO gate (per D5)

#### Objective
Fail CI if any single test >60s OR any suite >10min, unless tagged `// slow_test` with issue link.

#### Files to edit
```
infra/scripts/quality/runtime-slo.sh (NEW)
.github/workflows/runtime-slo-gate.yml (NEW)
```

#### Deep Dives
- Parse `go test -v` output for `--- PASS: TestX (60.01s)` patterns
- TS: parse Jest `--verbose` output for slowest test report
- Allowlist via `// slow_test: https://github.com/loopzero/loopzero/issues/N`

#### TDD
```
RED:     test_slo_gate_fails_test_over_60s_without_tag
RED:     test_slo_gate_passes_tagged_slow_test
RED:     test_slo_gate_fails_suite_over_10min
GREEN:   Implement
```

#### Acceptance Criteria
- [ ] SLO enforced + allowlist mechanism
- [ ] Helm-render full suite (~6min) PASSES under SLO

#### DoD
- [ ] Runtime cap visible + enforced

### T6.2 — Flaky test detection (per D4)

#### Objective
Auto-retry 1×; mark flaky in report; auto-quarentine after 3 marks in 7d.

#### Files to edit
```
.github/workflows/flaky-detection.yml (NEW)
infra/scripts/quality/flaky-track.sh (NEW)
.github/workflows/flaky-aggregator.yml (NEW) — daily cron, single-writer
```

#### Deep Dives
- Workflow runs all tests; on FAIL, single retry; if PASS, log retry event
- **EC-4 race fix:** Tracker NOT committed via PR auto-bot. Storage path:
  GitHub Actions cache (per-run snapshot) + uploaded as workflow artifact
  with 30d retention. Daily aggregator cron (`flaky-aggregator.yml`)
  uses `concurrency: { group: flaky-aggregator, cancel-in-progress: false }`
  ensuring SINGLE writer. Aggregator reads all artifacts from past 7
  days, computes flaky counts, opens issues for ≥3-mark tests.
- Workflow does NOT race because per-run only WRITES its own artifact
  (immutable post-write); aggregator is single-instance via concurrency.
- TTL 14d post-quarantine tracked by aggregator: if quarantined test
  still skipped after 14d, opens "remove or fix" follow-up issue.

#### TDD
```
RED:     test_flaky_detection_marks_retry_passed
RED:     test_flaky_auto_quarantine_after_3_marks
RED:     test_flaky_ttl_alert_after_14d
RED:     test_flaky_aggregator_concurrency_safe (EC-4) — 3 simulated
         simultaneous CI runs all write artifacts; aggregator reads all 3
         + computes correct count without losing data; race-free
RED:     test_flaky_aggregator_single_writer (EC-4) — concurrency group
         setting verified in workflow yaml (yq query asserts presence)
GREEN:   Implement with artifacts + single-writer cron
```

#### Acceptance Criteria
- [ ] Tracker updates on every CI run
- [ ] Auto-quarantine works
- [ ] TTL alerts open issues

#### DoD
- [ ] Flaky tests don't pollute main

### T6.3 — Test pyramid validation (per D7)

#### Objective
Use T1.3 inventory to enforce unit ≥70%, integration ≤25%, E2E ≤5%.

#### Files to edit
```
.github/workflows/pyramid-gate.yml (NEW)
infra/scripts/quality/pyramid-check.sh (NEW)
```

#### Deep Dives
- **EC-12 label override:** PR label `pyramid-defer` allows one-PR
  exception (e.g., sprint deliverable adds 50 E2E at once). Label TTL
  per PR (not persistent); auto-issue created to plan re-balancing in
  next sprint. Without override, sprint shipping blocked.

#### TDD
```
RED:     test_pyramid_fails_when_e2e_above_5_pct
RED:     test_pyramid_passes_at_70_25_5_ratio
RED:     test_pyramid_gate_allows_label_override (EC-12) — PR with
         `pyramid-defer` label PASSES gate + opens auto-issue for next
         sprint to re-balance pyramid
RED:     test_pyramid_gate_label_not_persistent (EC-12) — label valid
         for ONE PR only; subsequent PRs require fresh label
GREEN:   Implement
```

#### Acceptance Criteria
- [ ] Gate runs per PR
- [ ] Required Status Check
- [ ] EC-12 label override mechanism works + auto-issue created

#### DoD
- [ ] Pyramid inversion impossível silenciosamente
- [ ] Sprint deliverables não bloqueados sem escape hatch

### T6.4 — Mock budget gate (per D8)

#### Objective
Max 5 mocks per test file unless opt-out comment.

#### Files to edit
```
.github/workflows/mock-budget-gate.yml (NEW)
infra/scripts/quality/mock-budget.sh (NEW)
```

#### Deep Dives
- **EC-9 callback distinction:** `jest.fn()` used inline (e.g.,
  `setInterval(jest.fn(), 100)`) is callback assertion, NOT mock setup.
  Counter regex restricts to TOP-LEVEL `jest.mock(...)` /
  `vi.mock(...)` declarations + `gomock.NewController(t)` + `mock.New*`
  assignments to file-scope variables. Inline `jest.fn()` inside test
  body NOT counted.

#### TDD
```
RED:     test_mock_budget_fails_at_6_mocks
RED:     test_mock_budget_passes_at_5
RED:     test_mock_budget_honors_opt_out_comment
RED:     test_mock_budget_distinguishes_callback_from_mock (EC-9) — file
         with 6 inline `setInterval(jest.fn(), 100)` calls NOT counted
         as 6 mocks; only top-level `jest.mock()` declarations counted
RED:     test_mock_budget_gomock_controller_counted (EC-9) — Go file
         with 6 `gomock.NewController(t)` at top-level FAILS budget
GREEN:   Implement
```

#### Acceptance Criteria
- [x] Gate counts mocks correctly (declaration vs inline callback)
- [x] Honors opt-out (cli/tests/unit/platform-install-validate-l3.test.ts uses `// mock-budget: 7 — …`)
- [x] EC-9 distinction enforced via regex restricting to top-level

#### DoD
- [x] Design smell catchable (BLOCKING since 2026-05-21; previously advisory)
- [x] Callback false positives eliminated (silent-abort bug in `read_opt_out_budget` fixed: `set -euo pipefail` was aborting on files without the annotation)

### T6.5 — Skip-with-issue-link gate (per D9)

#### Objective
Every `t.Skip()` must have issue link OR be in `*_skip_test.go` file.

#### Files to edit
```
.github/workflows/skip-link-gate.yml (NEW)
infra/scripts/quality/skip-link-check.sh (NEW)
```

#### Deep Dives
- **EC-8 variants:** AST scanner matches ALL skip API forms:
  - Go: `t.Skip()`, `t.SkipNow()`, `t.Skipf("msg")`, `b.Skip()` (bench)
  - TS: `it.skip(...)`, `describe.skip(...)`, `test.skip(...)`,
    `xit(...)`, `xdescribe(...)`
  Each form requires issue URL in args/comment OR file ends in
  `_skip_test.go` / `.skip.test.ts`.

#### TDD
```
RED:     test_skip_link_fails_without_issue
RED:     test_skip_link_passes_with_link
RED:     test_skip_link_skipped_in_skip_test_files
RED:     test_skip_link_catches_SkipNow_variant (EC-8) — file with
         `t.SkipNow()` no link FAILS; with link PASSES
RED:     test_skip_link_catches_Skipf_variant (EC-8) — file with
         `t.Skipf("reason")` no link FAILS
RED:     test_skip_link_catches_jest_it_skip (EC-8) — TS file with
         `it.skip("test", ...)` no link FAILS
RED:     test_skip_link_catches_xit_xdescribe (EC-8) — TS file with
         `xit(...)` or `xdescribe(...)` no link FAILS
GREEN:   Implement with comprehensive regex coverage
```

#### Acceptance Criteria
- [x] Gate scans all *_test.go + *.test.ts
- [x] Requires URL or filename suffix
- [x] EC-8 covers ALL skip API variants (t.Skip/SkipNow/Skipf, it.skip,
      describe.skip, xit, xdescribe)
- [x] Baseline-aware blocking mode (graduated 2026-05-21) — 109 legacy
      violations grandfathered in `infra/quality/skip-link-baseline.txt`;
      NEW violations FAIL the gate. Backed by 10/10 hermetic tests in
      `infra/tests/scripts/check-skip-link-baseline_test.sh`.

#### DoD
- [x] No invisible skipped tests in ANY skip API form (BLOCKING since
      2026-05-21; baseline-aware so legacy debt can be paid down
      incrementally without re-introducing new violations)

---

## Phase 7: Documentation + Operator Handoff

**Objective:** document the Quality Gates system for operators + new contributors.

### T7.1 — Operator handoff doc

#### Files to edit
```
.claude/knowledge-base/concepts/operations/operator-handoff-2026-05-21-quality-gates.md (NEW)
```

Status matrix per phase + runbook for failed gates + escalation path.

#### Acceptance Criteria
- [ ] Operator handoff doc exists
- [ ] Per-gate troubleshooting included
- [ ] Cross-references to skill files

### T7.2 — Quality Gates README

#### Files to edit
```
infra/quality/README.md (NEW)
```

Quick-start for engineers, explaining gates + bypass procedure (emergency only with approval).

#### Acceptance Criteria
- [ ] README at canonical location
- [ ] Each gate has 1-paragraph description
- [ ] Bypass procedure documented (with sponsor approval req)

### T7.3 — Knowledge-base updates

Update `index.md` + `log.md` per `.claude/rules/auto-docs.md`.

### T7.4 — Branch Protection Rule Activation (EC-1 FIX — CRITICAL)

#### Objective
Wire EVERY new gate from Phases 2-6 into GitHub Branch Protection
Required Status Checks for `main` AND `develop`. Without this, gates
implemented but NOT enforced = decorative.

#### Evidence
- Per EC-1: "Implemented but not wired" systemic pattern from CLAUDE.md
- Plan global DoD mentions branch protection update but no task owned it
- 10 new gates created (T2.1, T2.2, T2.3, T3.1, T5.1, T5.2, T5.3, T6.1,
  T6.2, T6.3, T6.4, T6.5) — each must be Required Status Check

#### Files to edit
```
infra/scripts/quality/branch-protection-update.sh (NEW) — gh api script
.github/branch-protection-config.yml (NEW) — declarative gate list
.claude/knowledge-base/concepts/operations/branch-protection-runbook.md (NEW)
```

#### Deep file dependency analysis
- Script idempotent: read current branch protection JSON via
  `gh api /repos/loopzero/loopzero/branches/main/protection`
- Merge new Required Status Checks into existing list
- PUT updated config back
- Config YAML declarative source of truth (in repo, reviewable)

#### Deep Dives
- Required Status Check name MUST match exactly the GitHub Actions
  workflow job `name:` field
- Order of activation matters: enable gates AFTER PR merged that adds
  the workflow (otherwise workflow doesn't exist yet → all PRs blocked
  on unsatisfiable gate)
- Phased rollout: enable advisory mode first (warning-only via
  `continue-on-error: true`), then graduate to blocking after 7 days
  of baseline data confirms no false positives

#### Tasks
1. Write `branch-protection-update.sh` reading YAML config
2. Write `.github/branch-protection-config.yml` listing all 10+ gates
3. Document phased rollout in operator runbook
4. Run script against `develop` branch first (lower risk)
5. After 7 days advisory → graduate to blocking on `main`

#### TDD
```
RED:     test_branch_protection_script_is_idempotent (run 2x = same state)
RED:     test_branch_protection_preserves_existing_checks (no clobber)
RED:     test_branch_protection_config_yaml_valid_schema
RED:     test_branch_protection_failure_alerts_loudly (gh api error visible)
GREEN:   Implement
VERIFY:  bash infra/tests/scripts/branch-protection_test.sh (uses mock gh)
```

#### Acceptance Criteria
- [ ] Script committed + executable
- [ ] All 10+ new gates listed in config YAML
- [ ] Operator ran script against `develop` (verified via `gh api GET`)
- [ ] After 7d advisory mode, graduated to `main` blocking
- [ ] Runbook documents phased rollout + rollback procedure
- [ ] Pass: shellcheck warning+ EXIT=0
- [ ] Pass: file ≤500 lines

#### DoD
- [ ] All Quality Gates ENFORCED (not decorative)
- [ ] Branch protection state auditable via config YAML
- [ ] Phased rollout completed safely

---

## Risk Acknowledgements (Documented, Not Fixed)

Per edge case review 2026-05-21, the following risks are accepted
consciously — fix is either not warranted (KISS) or requires future
work outside this plan's scope.

### RA-1 (EC-13): CI cost +15-20min per PR may hit GitHub Actions free-tier cap

**Risk:** Plan adds ~15-20min CI runtime per PR. GitHub Actions free
tier for private repos: 2000 min/month. At 5 PRs/day × 20min × 30d =
3000 min/month → likely cap exceeded.

**Why accepted:** Not blocking; monitorable; mitigation paths exist:
- Move expensive gates (mutation) to cron-daily on main, not per PR
- Buy paid runners ($) — variable cost, predictable
- Use self-hosted runners (free runtime, infra cost)

**When to revisit:** Monitor GitHub Actions billing dashboard monthly.
If cap exceeded 2 consecutive months → T2 decision meeting on runner
strategy.

### RA-2 (EC-14): Go atomic coverage fluctuation across Go version bumps may exceed 3pp slack

**Risk:** D6 specifies 3pp slack on baseline regression gate. Go
version bumps (1.25 → 1.26 → 1.27, every ~6 months) can change atomic
coverage counters by 2-4pp due to compiler optimization changes,
affecting statement counts.

**Why accepted:** Low frequency event (twice a year), easy operator
mitigation (manual re-baseline post-bump). Adding auto-detection of
Go version change would add complexity for rare event.

**When to revisit:** If Go version bump triggers >5 false positive PR
failures in one week → add `quality:baseline:rebuild` task target +
operator runbook step "post Go bump, run rebuild + commit".

### RA-3 (EC-15): api/internal/server requires substantial refactor for testability

**Risk:** Phase 4.3 coverage push to 80% in `api/internal/server` may
discover hard-coded dependencies (no constructor DI) making tests
require heavy mocking that fights D8 mock budget. Real fix: refactor
server.go for constructor-injection pattern.

**Why accepted:** Phase 4.3 doesn't pre-commit to refactor; if discovered
during implementation, escalate to T2 design meeting. Don't pre-emptively
refactor without knowing actual gap shape.

**When to revisit:** During Phase 4.3 execution, if first attempt to
add tests requires >10 mocks per test file, STOP coverage push and
open T2 RFC for `api/internal/server` refactor.

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Coverage can drop invisibly | T2.1 + T2.2 | Diff gate + baseline gate; both Required Status Checks |
| 2 | Vacuous tests pass coverage % | T3.1 + T3.2 | Mutation testing on 8 critical packages, 60% threshold |
| 3 | Bug fixes without regression test | T5.1 + T5.2 | PR template + regression-test gate |
| 4 | Tests deleted invisibly | T5.3 | Test deletion audit gate |
| 5 | Flaky tests degrade signal | T6.2 | Auto-retry + quarantine + TTL alerts |
| 6 | Test runtime unbounded | T6.1 | Per-test 60s + per-suite 10min SLO |
| 7 | Pyramid inversion silent | T1.3 + T6.3 | Inventory + ratio gate (70/25/5) |
| 8 | t.Skip without issue link | T6.5 | AST-walking gate |
| 9 | Mock budget unbounded | T6.4 | 5-mock cap per file with opt-out |
| 10 | api/internal/server has NO tests | T4.3 | Per-domain coverage push to 80% |
| 11 | Critical paths need stronger gate | D11 + T4.2 + T3.1 | Dual gate: 90% line + 60% mutation |
| 12 | EC-1: Gates implementados mas não enforced (branch protection) | T7.4 NEW | Required Status Check activation via script |
| 13 | EC-2: Package split chicken-egg blocks legitimate refactor | T2.2 amendment | Opt-out label `coverage-baseline-defer` (7d TTL) |
| 14 | EC-3: Squash-merge loses "fix #N" → regression gate furada | T5.2 amendment | Parse PR title + body (não só commits) |
| 15 | EC-4: Flaky tracker JSON race em parallel CI runs | T6.2 amendment | Artifacts + single-writer cron via concurrency group |
| 16 | EC-5: Dependabot/Renovate PRs bloqueados pelo template gate | T5.1 amendment | Bot allowlist (dependabot/renovate + label `dependencies`) |
| 17 | EC-6: Test file rename = false positive em deletion gate | T5.3 amendment | `git diff --find-renames=80%` classifies rename ≠ delete |
| 18 | EC-7: Rename-only PR should PASS coverage diff | T2.1 TDD | New test case in TDD block |
| 19 | EC-8: Skip API variants (SkipNow, Skipf, xit, xdescribe) miss | T6.5 amendment | Comprehensive regex covering all variants |
| 20 | EC-9: jest.fn() callback false-positive em mock budget | T6.4 amendment | Regex restricts to top-level mock declarations only |
| 21 | EC-10: Doc-only Go package (zero statements) errors gate | T1.1 TDD | New test case + exclusion logic |
| 22 | EC-11: Mutation non-determinism flapeia baseline | T3.2 TDD | Variance test (≤5pp) + seed fix or mean-of-3 |
| 23 | EC-12: Sprint adding 50 E2E breaks pyramid gate | T6.3 amendment | Label `pyramid-defer` for one-PR override + auto-issue |

**Coverage: 23/23 gaps mapped (100%)**

## Global Definition of Done

- [ ] All 7 phases completed
- [ ] All tests passing (Go + TypeScript + bash hermetic)
- [ ] Zero lint warnings (golangci-lint + ESLint + shellcheck)
- [ ] Pre-release: BC break OK (new infrastructure)
- [ ] code-audit checks passing across all `infra/scripts/quality/`
- [ ] **`/cross-validation 2026-05-21-comprehensive-quality-gates` PASSED** — zero CRITICO, zero GRAVE
- [ ] CHANGELOG.md entry per phase
- [ ] Plan-specific criteria:
  - [ ] All 13 ADRs ratified via MEET
  - [ ] All 23 gaps in Coverage Matrix resolved (11 original + 12 edge-case)
  - [ ] Branch protection rules updated on main + develop (T7.4 — owned task, not orphan DoD line)
  - [ ] Coverage baseline JSON exists with critical packages ≥90% target
  - [ ] Mutation baseline JSON exists with critical packages ≥60% target
  - [ ] At least 5 sample PRs validated end-to-end through all gates
  - [ ] All new scripts have hermetic tests in `infra/tests/scripts/`
  - [ ] Operator handoff doc published
  - [ ] **Edge case validations explicitly tested:**
    - [ ] EC-2 escape valve works (coverage-baseline-defer label)
    - [ ] EC-3 PR title+body parsing works (squash-merge teams)
    - [ ] EC-4 flaky tracker race-free (concurrency group + artifacts)
    - [ ] EC-5 dependabot/renovate allowlist active
    - [ ] EC-6 rename detection works (--find-renames=80%)
    - [ ] EC-12 pyramid-defer label override + auto-issue mechanism
- [ ] **Evidence**: this plan IS score-affecting (raises maturity floor on testing) — evidence captured in:
  - [ ] T1 commits referenced in plan tasks
  - [ ] T2 LIVE log: full CI run with all gates active
  - [ ] T3 CI run: cron weekly Sunday auto-tests baseline
- [ ] **`/dogfood full` PASSED** (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

Per CLAUDE.md + plan template Rule 11.

### Execution

```bash
task teardown && task setup && tilt up
task doctor
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` on clean infra
- [ ] Pre-Flight Check: ALL pass
- [ ] Zero S0 findings
- [ ] Zero S1 findings related to this plan
- [ ] Quality Gates do NOT break existing dogfood journeys (no regression)
- [ ] Sample PR triggered post-implementation succeeds all gates

### On Failure

Fix all S0/S1 + re-run on fresh infra. S2-S4 documented but non-blocking.

---

## Edge Case Review (per skill rule 12)

Per skill mandate, `/edge-case-plan 2026-05-21-comprehensive-quality-gates` MUST run after plan creation. Any MUST FIX items incorporated as sub-tasks before final acceptance.

## Cross-Validation (per skill rule 13)

After implementation, `/cross-validation 2026-05-21-comprehensive-quality-gates` MUST pass with zero CRITICO + zero GRAVE before `/dogfood full`.

## Architecture Docs Note (skill rules 3 + 5)

`docs/architecture/` was deleted in 2026-05-11 governance consolidation per CLAUDE.md. The standard `/architecture-docs` mode 2 + mode 3 invocation cannot land in `docs/architecture/{domain}/` per that consolidation. For this plan, architecture content lives in:
- `.claude/knowledge-base/concepts/operations/operator-handoff-2026-05-21-quality-gates.md` (T7.1)
- `infra/quality/README.md` (T7.2)

A redirect stub at `docs/architecture/quality-gates/README.md` may be created per CV-L-2 closure pattern from prior plan.

---

## Estimated Scope (v1.1 — edge-case-incorporated)

- **Phases:** 7 (excluding Phase 0 governance)
- **Tasks:** 19 implementation (was 18; T7.4 added for EC-1) + 1 governance = 20 total
- **New scripts:** ~13 in `infra/scripts/quality/` (+1 for branch-protection)
- **New workflows:** ~11 in `.github/workflows/` (+1 for flaky-aggregator cron)
- **New gates:** 10 Required Status Checks
- **Estimated runtime impact on CI:** +15-20min per PR (see RA-1 for monitoring)
- **Estimated implementation time:** 4-6 weeks engineering complete; coverage push (Phase 4) iterative
- **Cost:** $0 incremental projected; RA-1 monitors actual usage
- **Edge case mitigations baked in:** 6 MUST FIX as code amendments + 6 SHOULD TEST as TDD cases + 3 DOCUMENT as Risk Acknowledgements
