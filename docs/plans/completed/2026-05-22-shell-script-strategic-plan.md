---
type: plan
title: "Plan: Shell Script Strategic Reduction (269 → <30) + External Tool Adoption (P0)"
status: deprecated
---

# Plan: Shell Script Strategic Reduction (269 → <30) + External Tool Adoption (P0)

> **Version 1.3** — Reduzir progressivamente o inventário de 269 scripts shell (44,586 LOC)
> para menos de 30 (<3,000 LOC) através de DELEGATION para orchestrators canônicos
> (helmfile/Tilt/Ginkgo/SDK-native Go), DELETE de dead code, e proibição explícita
> de novos scripts bash. Esse plano substitui o plano original
> `2026-05-21-bash-to-go-migration-plan.md` (cuja estratégia "migrar tudo para Go"
> revelou-se sub-ótima — virava shell-wrap em Go sem ganho real).
>
> **v1.1 changelog (2026-05-22 post-edge-case-review):** incorporou 4 MUST FIX edges
> identificados em [`docs/plans/2026-05-22-shell-script-strategic-edge-case-review.md`](./2026-05-22-shell-script-strategic-edge-case-review.md):
> - EC-01: T2.3.a (master-sync helmfile para evitar race postgresql↔gitea)
> - EC-02: T2.3.b (portable base64 — `kubectl -o go-template` em vez de `base64 -d`)
> - EC-03: T1.1.a (dynamic-invocation grep antes de validation) + T0.1 test
> - EC-04: T6.2 acceptance criteria + test para shallow clone scenario
>
> **v1.2 changelog (2026-05-22 post-kubetools-review):** incorporou Anti-Pattern #8
> (não reinventar tool externa madura como gate Go interno). Justificativa: análise
> kubetools.collabnix.com vs LoopZero (2026-05-22) revelou Rule 12 (grep-first) em
> CLAUDE.md + identified que Phase 6 cria 2 novos gates `pkg/quality/*` — ausência
> de "verifique tool externa primeiro" pode levar reimplementação de Pluto / Kubescape /
> outros como Go gates internos (anti-pattern simétrico ao D5 shell-wrap).
>
> **v1.3 changelog (2026-05-22):** adicionou **Phase 7 — Adopt External Tools (P0)** com
> 2 tasks paralelas materializando Anti-Pattern #8 ("adopt > reinvent"):
> - T7.1: Adopt **Pluto** (Fairwinds) — deprecated K8s API detector, CI gate em helm-charts.yml
> - T7.2: Adopt **OpenCost** — cost attribution per tenant via namespace label, mgmt cluster
>
> Justificativa: kubetools-review identificou estes como **P0 gaps reais** (zero matches
> em grep do repo). Pluto destrava futuro upgrade DOKS v1.32→v1.33. OpenCost destrava
> validação do pricing model Pro $29 / Team $99 para Sprint 9 prod canary.

---

## Context

### Estado atual (2026-05-22, branch `refactor/shell-scripts-strategic` @ `10d9389b`)

- **269 scripts .sh** totalizando **44,586 LOC** distribuídos em 15+ diretórios
- **Plano anterior** (`2026-05-21-bash-to-go-migration-plan.md`) migrou 22 scripts em uma sessão,
  mas auto-diagnóstico revelou: 6 das 22 migrações eram **shell-wrap em Go**
  (continuam dependendo de `pulumi`/`aws`/`kubectl`/`gh` CLIs externos via `shellrun.Runner`),
  net +5,500 LOC Go pra retirar -1,400 LOC bash sem reduzir dependências externas
- **Cleanroom (`task setup → infra:install:mgmt`) está BROKEN**: 2 bugs reais surfacearam
  (gitea `PASSWD=""` por design + ory-kratos/hydra `values.yaml.gotmpl` missing)
  porque o Taskfile bypassa o L2 helmfile orchestrator canônico
- **Tilt funciona** mas é o canonical dev loop não-documentado;
  `task setup` é vestige bit-rotted que ninguém roda

### Evidência da fragmentação arquitetural

Quando uma operação simples (bootstrap gitea) requer **4 arquivos coordenados**:
- `infra/helm/gitea/values.yaml` (chart values)
- `taskfiles/infra.yml` (install invocation)
- `infra/helmfile/scripts/bootstrap-patch-argocd-gitea-creds.sh` (post-install patch)
- `infra/helmfile/scripts/generate-secrets.sh` (pre-install secret generation)

…qualquer mudança quebra em 1 dos 4. Bash existe pra preencher o gap que o orchestrator
não cobre, mas isso fragmenta responsabilidade ao invés de centralizá-la.

### Os 3 insights estratégicos

1. **Bash existe porque orchestrator não engloba.** Solução não é migrar bash — é **fortalecer
   orchestrator** (helmfile/Tilt/ArgoCD) pra bash não precisar existir.
2. **Migrar bash sem eliminar dependência é teatro.** Shell-wrap em Go (`shellrun.Runner.Run("pulumi", ...)`)
   dobra LOC sem reduzir CLIs externos requeridos.
3. **Bash não é o problema; arquitetura é.** O custo dos 269 scripts é fragmentação de
   responsabilidade, não verbosidade.

### Links

- Origem: discussão da sessão 2026-05-21 (`Ralph loop` reflection)
- Plano substituído: `docs/plans/2026-05-21-bash-to-go-migration-plan.md` (DEPRECATED)
- Branch base: `10d9389b` (estado pré-migração)
- Develop atual: contém 22 migrações desta sessão (6 reverter como dívida, 16 manter como wins)

---

## Objective

**Done:** Repositório com **< 30 scripts shell** (~93% redução), nenhum no caminho crítico
operacional, cleanroom + tilt up + Ginkgo journey 76/76 funcionais 100%, **regra explícita
proibindo criação de novos scripts bash** como parte do sistema.

### Metas mensuráveis

- **N(bash) ≤ 30** (de 269 — redução ≥ 89%)
- **LOC(bash) ≤ 3,000** (de 44,586 — redução ≥ 93%)
- **`task setup` → `tilt up` → Ginkgo journey 76/76 GREEN** em fresh k3d
- **CI gate ativo** que **bloqueia** criação de novos `.sh` outside whitelist
- **Allowlist explícita** documentada com justificativa por script remanescente
- **Zero shell-wrap em Go** (auditoria automática: `pkg/*` que usa `shellrun.Runner.Run`
  para CLI quando há SDK Go nativo disponível)

---

## ADRs

### D1 — Tilt é o canonical dev loop

**Decision:** `tilt up` é o **único** dev loop suportado. `task setup` será
DEPRECATED com aviso e eventualmente removido.

**Rationale:** CLAUDE.md já lista Tilt como "Dev loop" canônico. `task setup` é um path
paralelo bit-rotted que duplica orquestração. Manter ambos = drift garantido.

**Consequences:**
- Documentação (CLAUDE.md, README) reescrita para "use `tilt up`"
- `task setup` vira alias depreciado com warn message
- Operadores aprendem único path
- Bug-hunt concentrado em UM orchestrator (Tiltfile + helmfile)

### D2 — Helmfile é o L2 orchestrator canônico

**Decision:** **Toda** invocação de `helm upgrade`/`helm install` no repo passa por
`helmfile sync`. Bare-helm em Taskfile/scripts é PROIBIDO.

**Rationale:** CLAUDE.md já declara: "**L2 (Helmfile):** orchestrator de 32 Helm releases…
**L2 é o SoT real desses workloads**, não Pulumi". Taskfile bare-helm é duplicação
do que helmfile já faz.

**Consequences:**
- Charts ganham `set:` blocks + `hooks:` directives para coisas que bash fazia
- ~30 scripts/linhas de Taskfile eliminados
- Cleanroom passa a usar mesmo path que Tilt (consistência)

### D3 — Criação de NOVOS scripts bash é PROIBIDA por padrão

**Decision:** A partir desta sessão, **qualquer novo arquivo `.sh` criado no repo
deve passar por gate CI explícito**. Default = BLOCK. Exceção requer ADR + entry
em `.claude/rules/shell-scripts-allowlist.md`.

**Rationale:** Sem essa regra, futuras sessões continuarão preencher gaps com bash,
re-inflando o inventário. A entropia precisa ser **estrutural**, não dependente de
disciplina humana.

**Consequences:**
- CI gate `loopzero-ops quality no-new-bash` bloqueia PRs que adicionam `.sh` sem allowlist
- Forçar developers a usar orchestrator nativo ou Go SDK
- 1 script novo legítimo → 1 entry na allowlist + justificativa = pequena fricção
  desincentiva criação acidental

### D4 — Bash existente sobrevive APENAS se passar 4 critérios

**Decision:** Para um `.sh` existente ficar no repo após este plano, deve atender
**TODOS** os 4 critérios:
1. **Necessário** — operação que não tem orchestrator canônico (Tilt/helmfile/ArgoCD/Pulumi-auto)
2. **Simples** — < 100 LOC, < 3 níveis de aninhamento, sem state machine
3. **Operator-time** — usado por humano em incident/drill/migration (não no caminho crítico CI/dev)
4. **Allowlisted** — entry em `.claude/rules/shell-scripts-allowlist.md` com justificativa

**Rationale:** Se um script falha em qualquer critério, há sempre uma alternativa
(orchestrator/Go SDK/Ginkgo). Os 4 critérios eliminam ambiguidade na decisão.

**Consequences:**
- ~15-25 scripts qualificam (break-glass, file-rfc, etc.)
- Restante (~240) vai para DELEGATE/MIGRATE/DELETE

### D5 — Shell-wrap em Go é PROIBIDO

**Decision:** `pkg/*` Go que usa `shellrun.Runner.Run("cmd", ...)` quando existe SDK
Go nativo (`pulumi-go-sdk`, `helm-sdk-go`, `client-go`, `aws-sdk-go-v2`, `go-github`) é
**dívida técnica** e **proibido** sem justificativa documentada.

**Rationale:** Shell-wrap não elimina dependência de CLI externa — apenas move sintaxe.
Net: dobra LOC sem ganho real. As 6 migrações shell-wrap desta sessão (categoria B)
são exemplo do anti-pattern.

**Consequences:**
- Migrações Go aprovadas APENAS quando: (a) pure-logic OR (b) SDK-native nativo
- CI gate `loopzero-ops quality no-shell-wrap` audita `pkg/*` por padrão `shellrun.Runner.Run`
  com argumento em allowlist de CLIs problemáticos

### D6 — `*_test.sh` morre com o `*.sh` que testa (auto-die)

**Decision:** Hermetic tests `infra/tests/scripts/X_test.sh` são deletados
**automaticamente** quando o `X.sh` correspondente é deletado/migrado.

**Rationale:** Não há valor em testes que validam código deletado. ~50 scripts
auto-eliminam quando consumers morrem.

**Consequences:**
- Net: redução automática sem trabalho explícito
- Migrações Go que substituem `.sh` devem **incluir testify** que substitui o `_test.sh`

### D7 — Tilt + helmfile cobrem 90% dos casos; bash fica para o resto

**Decision:** A divisão estratégica é:
- **Orchestration declarativa** (cluster bootstrap, install, sync) → **helmfile/Tilt**
- **Test orchestration** → **Ginkgo**
- **State management** (Pulumi state ops) → **Pulumi Automation API Go SDK**
- **Pure-logic CI gates** (regex, file walk, parse) → **pkg/quality testify**
- **Generators** → **`//go:generate`**
- **Operator-time simples e raro** → **bash allowlisted**

**Rationale:** Cada categoria tem ferramenta canônica madura. Bash deve ser exceção,
não regra.

**Consequences:**
- Plan tem fases dedicadas para cada categoria
- Sucesso = cada `.sh` remanescente cabe em "operator-time simples e raro"

---

## Dependency Graph

```
Phase 0: Real Usage Audit
    │
    ▼
Phase 1: Delete Dead Code ───────────┐
    │                                 │
    ▼                                 │
Phase 2: Delegate to Helmfile         │  (Phase 1 podia paralelizar
    │  (UNBLOCKS CLEANROOM)           │   com 2, mas serializamos
    │                                 │   pra reduzir conflitos)
    ▼                                 │
Phase 3: Deprecate task setup ───────┤
    │  (canonicalize Tilt)            │
    │                                 │
    ▼                                 │
    ├──────────────────────────┐      │
    │                          │      │
Phase 4: Migrate to Ginkgo  Phase 5: SDK-native Go   (paralelo)
    │ (test orchestrators)   │ (Pulumi Auto API,
    │                        │  generators, etc.)
    ▼                        ▼
    └────────┬───────────────┘
             ▼
    ┌────────┴───────────────┐
    │                        │
Phase 6: Lock The Door   Phase 7: Adopt External Tools  (paralelo)
(no-new-bash gate,       (T7.1 Pluto CI gate ∥
 no-shell-wrap gate,      T7.2 OpenCost helm)
 allowlist)
    │                        │
    └────────┬───────────────┘
             ▼
       (Dogfood Gate + Cross-Validation)
```

**Sequencial:** Phase 0 → 1 → 2 → 3 → [4 ∥ 5] → [6 ∥ 7] → Dogfood
**Razão da sequência:** Phase 2 desbloqueia cleanroom (critical path). Phase 3 só
faz sentido após Phase 2 funcionar. Phases 4 e 5 podem paralelizar (touch sets
diferentes). Phases 6 e 7 também paralelas (Phase 6 = governance interno; Phase 7 =
external tool adoption; zero file overlap).

---

## Phase 0: Real Usage Audit

**Objective:** Antes de deletar/migrar/manter QUALQUER script, ter dados objetivos sobre
quem chama o quê, último modificação, LOC, e disposição proposta.

### T0.1 — Build `loopzero-ops scripts audit` Go tool

#### Objective
Ferramenta Go que escaneia todos os `.sh` do repo, faz grep de callers em CI/Taskfile/
helmfile/outros scripts, e emite CSV canônico para review humano.

#### Evidence
Sem inventário objetivo, qualquer decisão de "este script é dead" vira opinião.
Plano anterior (`2026-05-21-bash-to-go-migration-plan.md`) migrou 22 scripts sem
audit prévio e descobriu (post-hoc) que 6 eram teatro.

#### Files to edit
```
pkg/quality/scriptsaudit/check.go        — (NEW) audit logic + CSV emitter
pkg/quality/scriptsaudit/check_test.go   — (NEW) testify tests (≥90% cov)
cmd/loopzero-ops/quality_subcmd.go           — add `scripts-audit` gate
docs/scripts-audit/2026-05-22.csv        — (NEW) baseline audit output
```

#### Deep file dependency analysis
- `pkg/quality/scriptsaudit/check.go` (NEW): puro Go, sem shell-out. Usa `filepath.WalkDir`
  pra encontrar `.sh`, lê linhas pra contar LOC, `regexp` pra buscar callers.
- `cmd/loopzero-ops/quality_subcmd.go`: adiciona case `scripts-audit` no switch.
  Reusa pattern dos 16 gates pure-logic já migrados.
- `docs/scripts-audit/2026-05-22.csv`: artefato de baseline. Reviewed por operador
  antes de Phase 1.

#### Deep Dives
**Caller detection regex:** `<script-name>(?:\.sh)?\b` em arquivos:
- `.github/workflows/*.yml`
- `Taskfile.yml`, `taskfiles/*.yml`
- `infra/helmfile/**/*.gotmpl`
- `infra/scripts/**/*.sh` (cross-script calls)
- `Tiltfile`, `Tiltfile.local`
- `infra/tests/**/*.sh` (hermetic tests reference parent)

**Disposition column** (one of):
- `KEEP-OPERATOR` — passes all 4 D4 criteria
- `DELEGATE-HELMFILE` — bare-helm wrapper, migrate to helmfile sync
- `DELEGATE-TILT` — covered by Tiltfile path
- `DELEGATE-ARGOCD` — covered by ArgoCD self-manage
- `MIGRATE-GO-PUREFN` — pure logic, candidate for pkg/quality
- `MIGRATE-GO-SDK` — has Go SDK alternative
- `MIGRATE-GINKGO` — test orchestrator pattern
- `MIGRATE-GENERATOR` — `//go:generate` candidate
- `AUTO-DIE` — `*_test.sh` whose parent is gone/migrating
- `DELETE-DEAD` — 0 callers + > 90d untouched
- `REVIEW-NEEDED` — defaults; operator must decide

**Invariants:**
- Toda linha do CSV preenchida (no nulls em disposition)
- Sum(KEEP + DELEGATE + MIGRATE + AUTO + DELETE + REVIEW) == N(total .sh)
- LOC per script accurate (sanity: `wc -l` comparison)

**Edge cases:**
- Scripts referenciados apenas em comments de outros files → still count
- Scripts referenciados em CHANGELOG mas não em código vivo → mark REVIEW-NEEDED
- Scripts referenciados em `_legacy/` paths → mark DELETE-DEAD candidate
- Symlinked scripts (rare, but possible) → handle via `filepath.EvalSymlinks`

#### Tasks
1. Criar `pkg/quality/scriptsaudit/` package (check.go + test)
2. Implementar `ScanRepo(root string) ([]Script, error)` que retorna struct com
   {Path, LOC, LastModUTC, Callers []string, Disposition string}
3. Implementar `detectCallers(scriptName string, searchPaths []string) []string`
   usando regexp + filepath.WalkDir
4. Implementar `proposeDisposition(s Script) string` (heurísticas baseadas em D4
   + categorização per propósito)
5. Implementar `EmitCSV(scripts []Script, w io.Writer) error` (encoding/csv)
6. Wire em `cmd/loopzero-ops/quality_subcmd.go` como `quality scripts-audit`
7. Rodar contra repo, salvar baseline em `docs/scripts-audit/2026-05-22.csv`

#### TDD
```
RED:     TestScanRepo_FindsAllShellScripts() — assert finds 269 (baseline)
RED:     TestScanRepo_ExcludesNodeModules() — vendor/ + node_modules/ excluded
RED:     TestDetectCallers_FindsTaskfileRefs() — fixture: Taskfile com `bash X.sh` → caller
RED:     TestDetectCallers_FindsCIYamlRefs() — fixture: .github/workflows/Y.yml com X.sh
RED:     TestDetectCallers_FindsCrossScriptRefs() — script A.sh chama bash B.sh → B has caller A
RED:     TestProposeDisposition_DeadCode() — 0 callers + last-mod>90d → DELETE-DEAD
RED:     TestProposeDisposition_HermeticTest() — `X_test.sh` exists, X.sh exists → AUTO-DIE
RED:     TestProposeDisposition_BareHelmWrapper() — script contains `helm upgrade` → DELEGATE-HELMFILE
RED:     TestProposeDisposition_PureLogicGate() — script in `check*.sh` pattern → MIGRATE-GO-PUREFN
RED:     TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead() — EC-03: script cujo basename aparece em `bash "$VAR"` em outro arquivo → caller_count >= 1 (não 0). Fixture: scriptA.sh chama `bash "$DIR/scriptB.sh"`; scriptB.sh deve ter scriptA.sh como caller.
RED:     TestScanRepo_HandlesSymlinks_NoCycle() — EC-07: fixture com symlink `alias.sh -> ../real.sh`; assert (a) script listado UMA vez (não duas), (b) callers de `alias.sh` E `real.sh` consolidados.
RED:     TestEmitCSV_StableSchema() — output columns: path,loc,last_mod_utc,caller_count,disposition,callers
RED:     TestEmitCSV_DeterministicOrder() — sorted by path

GREEN:   Implement scanRepo + detectCallers + proposeDisposition + emitCSV

REFACTOR: extract `dispositionHeuristics` map for testability

VERIFY:  cd pkg && go test -coverprofile=/tmp/sa.cov ./quality/scriptsaudit/...
         # coverage ≥ 90%
         cd /home/paulo/loopzero && /tmp/loopzero-ops quality scripts-audit > docs/scripts-audit/2026-05-22.csv
         wc -l docs/scripts-audit/2026-05-22.csv  # should be 269+1 (header)
```

#### Acceptance Criteria
- [ ] `pkg/quality/scriptsaudit` package criado com ≥90% test coverage
- [ ] `loopzero-ops quality scripts-audit` emite CSV com 269 entries + header
- [ ] CSV reviewed por operador (HUMAN GATE — registrado em [docs/scripts-audit/REVIEWED.md](../../scripts-audit/REVIEWED.md))
- [ ] Pass: `/code-audit` complexity check (≤10 cyclomatic)
- [ ] Pass: `/code-audit` lint (zero warnings golangci-lint)
- [ ] Pass: `/code-audit` size (file ≤500 LOC)

#### DoD
- [ ] Tasks 1-7 completos
- [ ] Hermetic tests passam (`go test ./quality/scriptsaudit/...`)
- [ ] Baseline CSV committado em `docs/scripts-audit/2026-05-22.csv`
- [ ] [`docs/scripts-audit/REVIEWED.md`](../../scripts-audit/REVIEWED.md) documenta operador-review com timestamp
- [ ] CHANGELOG entry sob `### Added`

---

## Phase 1: Delete Dead Code

**Objective:** Eliminar scripts com 0 callers documentados, reduzindo inventário
imediatamente sem mudar comportamento de nada vivo.

### T1.1 — Audit + propose `DELETE-DEAD` set

#### Objective
A partir do CSV de Phase 0, isolar scripts com `disposition=DELETE-DEAD`, validar
manualmente (sample de 5-10), e produzir lista final pra deletion.

#### Evidence
CSV de T0.1 marca disposição inicial. Mas falsos-positivos existem (script
referenciado em runbook MD mas não em código vivo, por exemplo). Validação manual
necessária antes de delete em massa.

#### Files to edit
```
docs/scripts-audit/2026-05-22-dead-set.md — (NEW) dead set documented + validation
docs/scripts-audit/2026-05-22-dead-set-validated.csv — (NEW) validated subset
```

#### Deep file dependency analysis
- [`docs/scripts-audit/2026-05-22-dead-set.md`](../../scripts-audit/2026-05-22-dead-set.md): lista cada script proposto pra delete +
  evidência ("0 callers em [...]" + "last-mod > 90d") + decisão manual
- `docs/scripts-audit/2026-05-22-dead-set-validated.csv`: subset confirmado para
  Phase 1.2 executar

#### Deep Dives
**Validation methodology:**
1. Para cada script em `disposition=DELETE-DEAD`, executar manual:
   - `git log --follow -- <script>` — último commit + autor + razão
   - `grep -r <script-basename> docs/ .claude/ README.md` — refs em docs/runbooks
2. Se script aparece em runbook MD ativo: re-classify como `KEEP-OPERATOR` ou
   `MIGRATE-GO-SDK` (não DELETE)
3. Se script é `_legacy/` path: confirm via `git log --diff-filter=A` que é mesmo legacy

**Anti-pattern evitado:**
- NÃO deletar script só porque tem 0 callers no código — verificar docs/runbooks
- NÃO deletar script tocado nos últimos 30d (pode ser WIP)

#### Tasks
1. Filter CSV de T0.1 para `disposition=DELETE-DEAD` → working set
2. **T1.1.a (EC-03 anti-false-positive):** ANTES de marcar qualquer script como confirmed-dead,
   rodar grep para invocações dinâmicas:
   ```bash
   for script in $(awk -F, '$5=="DELETE-DEAD"{print $1}' docs/scripts-audit/2026-05-22.csv); do
     basename=$(basename "$script")
     # Procura padrões como: bash "$VAR", source "$DIR/X.sh", bash $(fn)
     if grep -rE "bash[[:space:]]+[\"\$]|source[[:space:]]+[\"\$]" \
          --include="*.sh" --include="*.yml" --include="*.gotmpl" infra/ taskfiles/ Taskfile.yml \
          | grep -q "$basename"; then
       echo "FALSE-POSITIVE candidate: $script (referenced via interpolation)"
     fi
   done
   ```
   Qualquer script que apareça em interpolation → re-classify como `REVIEW-NEEDED`, NÃO delete.
3. Para cada script remanescente, executar validation method acima (git log + docs grep)
4. Manter `dead-set.md` com decisão fundamentada por script
5. Produzir `dead-set-validated.csv` com subset confirmado
6. Submit para review (commit + push, operator review na PR/branch)

#### TDD
```
RED:     N/A — esta task é diagnóstico/análise manual, não tem código pra TDD
         (o test que cobre o anti-falso-positivo está no T0.1: TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead)
VERIFY:  cat docs/scripts-audit/2026-05-22-dead-set-validated.csv | wc -l
         # esperado: 20-30 scripts (subset conservador)
         git log --oneline -1 -- docs/scripts-audit/2026-05-22-dead-set-validated.csv
         # verifica que existe + tem timestamp
         # T1.1.a verification:
         grep -E "REVIEW-NEEDED.*\(interpolation\)" docs/scripts-audit/2026-05-22-dead-set.md
         # esperado: 0+ entries (qualquer script com dynamic-call reclassificado)
```

#### Acceptance Criteria
- [ ] `dead-set.md` lista 20-50 scripts candidatos + rationale por linha
- [ ] `dead-set-validated.csv` é subset conservador (excluir qualquer dúvida)
- [ ] Cada entry no validated CSV tem evidence column ("0 callers em [...], last-mod 2025-XX-XX")

#### DoD
- [ ] Tasks 1-5 completos
- [ ] Validated CSV committado
- [ ] PR description tem inventário do que será deletado

### T1.2 — Bulk delete validated dead set

#### Objective
Executar `git rm` em massa do subset validado, criando 1 commit atômico por dir
(facilita revert se necessário).

#### Evidence
Phase 1 só funciona se chegar até a tela final. T1.1 produz a lista; T1.2 executa.

#### Files to edit
```
infra/scripts/<dead-script-1>.sh — DELETE
infra/scripts/<dead-script-2>.sh — DELETE
... (N entries from dead-set-validated.csv)
CHANGELOG.md — (NEW entry under ### Removed)
```

#### Deep file dependency analysis
- Cada `.sh` deletado: verify NÃO está em callers list de outro script
  (validation already done em T1.1 mas re-check em T1.2 antes de delete)
- Companion `_test.sh` files: auto-delete junto (D6 — auto-die)
- CHANGELOG: 1 entry per directory grouping ("Removed 8 dead scripts from infra/scripts/")

#### Deep Dives
**Commit strategy:**
- 1 commit por subdirectory (e.g., `infra/scripts/`, `infra/tests/scripts/`)
- Commit message format:
  ```
  chore(cleanup): delete N dead scripts in {dir}/ (Phase 1.2)

  Per docs/scripts-audit/2026-05-22-dead-set-validated.csv. 0 callers
  documentados em CI/Taskfile/helmfile/Tiltfile/cross-scripts. last-mod
  > 90 days. No active runbook references.

  Scripts deleted:
  - <name1>.sh (N LOC, last-mod YYYY-MM-DD)
  - <name2>.sh (N LOC, last-mod YYYY-MM-DD)
  ...

  Total: -N LOC bash.
  ```

**Safety nets:**
- `git revert` trivial se descobrir caller posterior
- Branch isolada (`refactor/shell-scripts-strategic`) — não toca develop até merge
- Operator approval no PR antes de merge

#### Tasks
1. `cd /home/paulo/loopzero`
2. Para cada subdirectory grouping em validated CSV:
   a. `git rm <scripts>`
   b. Commit per pattern acima
3. Update CHANGELOG.md `### Removed` section
4. Push branch

#### TDD
```
RED:     TestNoCallerRemainsForDeletedScripts — corre `loopzero-ops quality scripts-audit`
         após delete, verifica que NENHUM script remanescente lista um deletado como caller
GREEN:   Os deletes em si (manual git rm)
VERIFY:  /tmp/loopzero-ops quality scripts-audit > /tmp/post-delete.csv
         python3 -c "import csv; [print(r) for r in csv.DictReader(open('/tmp/post-delete.csv'))
           if any(c in DELETED_SET for c in r['callers'].split(';'))]"
         # esperado: empty output (no orphan callers)
```

#### Acceptance Criteria
- [ ] N(deleted) matches `dead-set-validated.csv` exactly
- [ ] Post-delete audit confirms zero orphan callers
- [ ] CHANGELOG entry covers all directories affected
- [ ] Branch buildable (no `make`/`task` broken)

#### DoD
- [ ] N commits (1 per dir grouping) committed + pushed
- [ ] `task lint` passa no branch
- [ ] CI workflows (que não foram afetados) ainda funcionam (validar via dry-run de Taskfile)

---

## Phase 2: Delegate to Helmfile (UNBLOCKS CLEANROOM)

**Objective:** Substituir todas as invocações bare-helm em `taskfiles/infra.yml` por
`helmfile sync` apropriado, adicionando `set:`/`hooks:` no helmfile para cobrir
gaps (gitea PASSWD, kratos/hydra DSN env).

### T2.1 — Audit bare-helm calls + map to helmfile releases

#### Objective
Cada `helm upgrade --install X ...` em Taskfile → release name correspondente em
`infra/helmfile/mgmt.helmfile.yaml.gotmpl`. Produzir mapping table.

#### Evidence
Bug discovery da sessão 2026-05-21: cleanroom falhou em gitea (PASSWD empty),
kratos/hydra (values.yaml missing — só `.gotmpl`). Helmfile já tem as 32 releases
declaradas corretamente.

#### Files to edit
```
docs/scripts-audit/2026-05-22-helmfile-mapping.md — (NEW) mapping table
```

#### Deep file dependency analysis
- Read `taskfiles/infra.yml` exhaustively
- Read `infra/helmfile/mgmt.helmfile.yaml.gotmpl` releases
- Read `infra/helmfile/build.helmfile.yaml.gotmpl` releases
- Read `infra/helmfile/runtime.helmfile.yaml.gotmpl` releases
- Match each bare-helm to its helmfile release; flag any without match

#### Deep Dives
**Expected mapping examples:**
```
Taskfile call                                  | Helmfile release    | Action
helm upgrade --install postgresql ...          | mgmt:postgresql     | helmfile sync --selector name=postgresql
helm upgrade --install gitea ...               | mgmt:gitea          | helmfile sync --selector name=gitea + add set: PASSWD
helm upgrade --install ory-kratos ...          | mgmt:ory-kratos     | helmfile sync (already gotmpl-ready)
helm upgrade --install argocd ...              | mgmt:argocd         | helmfile sync --selector name=argocd
helm upgrade --install argo-workflows ...      | build:argo-workflows| helmfile -f build.helmfile sync --selector ...
helm upgrade --install cert-manager ...        | runtime:cert-manager| helmfile -f runtime.helmfile sync --selector ...
```

**Flags to add to helmfile releases:**
- `set:` blocks para injetar secrets (gitea PASSWD, etc.)
- `hooks:` directives para presync/postsync bash que ainda for necessário

#### Tasks
1. `grep -n 'helm upgrade --install' taskfiles/infra.yml` → enumerate all
2. Para cada release, find correspondent em mgmt/build/runtime helmfile
3. Documentar gaps (releases que NÃO têm match — precisam ser ADICIONADAS ao helmfile)
4. Documentar secrets/env vars que cada release precisa
5. Produzir `helmfile-mapping.md` com tabela completa

#### TDD
```
RED:     N/A — task de análise + documentação
VERIFY:  grep -c '| helmfile' docs/scripts-audit/2026-05-22-helmfile-mapping.md
         # esperado: N (count de bare-helm calls em Taskfile)
```

#### Acceptance Criteria
- [ ] Mapping table cobre 100% das bare-helm calls em Taskfile
- [ ] Gaps (releases sem helmfile equivalent) explicitamente listados
- [ ] Secrets/env vars per release documentados

#### DoD
- [ ] `helmfile-mapping.md` committed
- [ ] Operator review (HUMAN GATE — registrar approval)

### T2.2 — Fix helmfile releases for secret injection

#### Objective
Para cada release que precisa de secret/env injection (gitea PASSWD, ory-kratos/hydra
DSN env), adicionar `set:` block ou `environments.values` no helmfile.

#### Evidence
- Gitea `values.yaml` tem `PASSWD: ""` (T1.3 comment: "override via helmfile inline or --set")
- ory-kratos/hydra usam `requiredEnv "LOOPZERO_DB_PASSWORD"` no DSN (precisa env exportado)

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — add set: blocks for gitea + others
infra/helm/ory-kratos/values.yaml.gotmpl — fix YAML escape '\"' → '"' (single-quoted YAML)
infra/helm/ory-hydra/values.yaml.gotmpl — fix YAML escape '\"' → '"' (single-quoted YAML)
infra/helmfile/values/standalone-multi/gitea.yaml.gotmpl — (maybe) add PASSWD override
```

#### Deep file dependency analysis
- `mgmt.helmfile.yaml.gotmpl` gitea release: add `set:` block ou referência a env var
  via `values:` com gotmpl que faz `kubectl get secret postgresql -o jsonpath=...`
- `ory-kratos/values.yaml.gotmpl` linha 16: substituir `"postgres://...{{ requiredEnv \"LOOPZERO_DB_PASSWORD\" }}..."`
  por `'postgres://...{{ requiredEnv "LOOPZERO_DB_PASSWORD" }}...'` (YAML single-quoted)
- `ory-hydra/values.yaml.gotmpl` linha 20: mesma correção

#### Deep Dives
**Helmfile pattern para inject Secret value at sync time:**
```yaml
releases:
  - name: gitea
    chart: gitea-charts/gitea
    values:
      - ../helm/gitea/values.yaml
    set:
      - name: gitea.config.database.PASSWD
        value: '{{ exec "kubectl" (list "--context" .Values.kubeContext "-n" "loopzero-system" "get" "secret" "postgresql" "-o" "jsonpath={.data.password}") | b64dec }}'
```

OU alternativa com env var (mais simples):
```yaml
environments:
  standalone-multi:
    values:
      - kubeContext: k3d-loopzero-mgmt
    secrets:
      - LOOPZERO_DB_PASSWORD: ref+kubectl://secret/postgresql?context=k3d-loopzero-mgmt&namespace=loopzero-system&key=password
```

(usa helmfile-secrets plugin se disponível, OU fallback para shell `kubectl ... | b64decode`)

**YAML escape fix:**
```yaml
# WRONG (gotmpl can't parse \" inside YAML "")
dsn: "postgres://...{{ requiredEnv \"PASSWORD\" }}..."

# RIGHT (single-quoted YAML, double-quoted gotmpl literal)
dsn: 'postgres://...{{ requiredEnv "PASSWORD" }}...'
```

**Invariants:**
- Gitea install converge no primeiro try (sem CrashLoop)
- Kratos/Hydra install converge sem template render error
- Secrets nunca aparecem em values.yaml plain — sempre injected at sync time

**Edge cases:**
- `kubectl exec` em helmfile values requer contexto correto (`--context k3d-loopzero-mgmt`)
- Secret pode não existir no primeiro sync (race: postgresql install precisa terminar antes)
  → solucionado por `needs:` directive no helmfile (gitea needs postgresql)

#### Tasks
1. Editar `ory-kratos/values.yaml.gotmpl` linha 16: single-quote YAML fix
2. Editar `ory-hydra/values.yaml.gotmpl` linha 20: single-quote YAML fix
3. Editar `mgmt.helmfile.yaml.gotmpl` gitea release: adicionar `set:` block
   pra `PASSWD` via exec kubectl
4. Validar local: `cd infra/helmfile && helmfile -e standalone-multi --selector name=gitea template`
   (template, não sync — só renderiza, não aplica)
5. Validar local: kratos + hydra template também
6. Commit + push

#### TDD
```
RED:     TestHelmfileTemplate_GiteaHasPASSWD — helmfile template gitea + grep PASSWD não vazio
RED:     TestHelmfileTemplate_KratosDSNRendered — helmfile template kratos + grep "postgres://" rendered
RED:     TestHelmfileTemplate_HydraDSNRendered — helmfile template hydra + grep "postgres://" rendered
GREEN:   Implement set: + escape fixes
VERIFY:  LOOPZERO_DB_PASSWORD=test123 helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
           -e standalone-multi --selector name=ory-kratos template | grep "postgres://loopzero:test123@"
         # exit 0 → DSN injected corretamente
```

#### Acceptance Criteria
- [ ] `helmfile template` para gitea/kratos/hydra renders sem erro
- [ ] DSN/PASSWD aparecem populados (não vazio) no rendered template
- [ ] Nenhuma credencial em values.yaml plain (always injected)

#### DoD
- [ ] Tasks 1-6 completos
- [ ] `helmfile template` PASS para 3 releases
- [ ] Edits committed em branch

### T2.3 — Rewrite `infra:install:mgmt` to use helmfile

#### Objective
Substituir os ~10 `helm upgrade --install ...` bloks em `taskfiles/infra.yml` por
chamadas `helmfile sync --selector name=X`.

#### Evidence
Bug raiz da cleanroom: bypassa helmfile, duplica orquestração.

#### Files to edit
```
taskfiles/infra.yml — rewrite infra:install:mgmt, infra:install:build, infra:install:workload
```

#### Deep file dependency analysis
- `infra:install:mgmt`: era 10+ bare-helm calls → torna-se `helmfile sync` per selector
  (ou single `helmfile sync` se ordem por `needs:` já está correta)
- `infra:install:build`: similar — converter argo-workflows + buildkit cwts
- `infra:install:workload`: converter cert-manager + traefik + external-secrets + argo-rollouts

#### Deep Dives
**T2.3.a (EC-01) MASTER-SYNC pattern (mandatory — overrides per-selector default):**

Em fresh cleanroom, `helmfile needs:` só resolve dentro da MESMA invocation helmfile.
Se postgresql install em invocation A e gitea install em invocation B (separadas com `--selector`),
o `set: PASSWD` do gitea (que usa `exec kubectl get secret postgresql`) falha porque
postgresql Secret ainda não existe no momento da resolução do gitea values.

**Pattern OBRIGATÓRIO** — single master sync per cluster:
```yaml
infra:install:mgmt:
  cmds:
    - |
      # Single sync — helmfile resolve dependency order via needs:
      helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
        -e standalone-multi \
        --kube-context {{.MGMT_CONTEXT}} \
        sync
```

Helmfile releases MUST declarar `needs:` explicit:
```yaml
- name: postgresql
  # ... (no needs:)
- name: gitea
  needs: [loopzero-system/postgresql]  # garante postgresql Ready antes de gitea sync
```

**T2.3.b (EC-02) PORTABLE secret extraction (mandatory — substitui `base64 -d`):**

Pattern `kubectl ... | base64 -d` quebra em macOS (BSD `base64` usa `-D` minúsculo errado).
Substituir por `kubectl -o go-template` (decoded direto, zero pipe):

```yaml
# WRONG (Linux-only, breaks Mac)
export LOOPZERO_DB_PASSWORD=$(kubectl ... get secret postgresql -o jsonpath='{.data.password}' | base64 -d)

# RIGHT (portable, Mac+Linux)
export LOOPZERO_DB_PASSWORD=$(kubectl --context {{.MGMT_CONTEXT}} -n loopzero-system \
  get secret postgresql -o go-template='{{`{{.data.password | base64decode}}`}}')
```

Note: o pattern `{{`...`}}` escapa as Go template braces dentro do gotmpl Taskfile.
Alternativa simpler se Taskfile não tem gotmpl: usar shell heredoc OU `openssl base64 -d -A`.

**Invariants:**
- Cada release de Taskfile original tem equivalente helmfile invocation
- LOOPZERO_DB_PASSWORD exportado quando releases que precisam vão rodar
- Cluster context correto per invocation (mgmt vs build vs workload)
- **Master sync resolve ordering via `needs:` — NÃO usar --selector que quebra needs**
- **Secret extraction usa `go-template` ou `openssl` — NÃO `base64 -d` (portability)**

**Edge cases:**
- Helmfile sync pode timeout em rede lenta → `--timeout` flag
- Secret postgresql pode não existir no primeiro sync → `needs:` no helmfile resolve **(SE master sync usado per T2.3.a)**

#### Tasks
1. Backup `taskfiles/infra.yml` (git tracks)
2. **T2.3.a (EC-01) — MASTER SYNC:** Rewrite `infra:install:mgmt` para usar SINGLE
   `helmfile sync` (sem `--selector`) per cluster. Verificar releases declaram `needs:`
   apropriadamente em `mgmt.helmfile.yaml.gotmpl` (gitea needs postgresql, ory-* needs
   postgresql, argocd needs Secret, etc.).
3. **T2.3.b (EC-02) — PORTABLE BASE64:** Substituir TODO `kubectl ... | base64 -d`
   em `taskfiles/` por `kubectl -o go-template='{{.data.X | base64decode}}'` OU
   `openssl base64 -d -A`. Verificar via grep que zero ocorrências de `base64 -d`
   restam em taskfiles/.
4. Rewrite `infra:install:build` similar (master sync + portable extraction)
5. Rewrite `infra:install:workload` similar
6. Validar local: `task setup` from fresh state (delete clusters, recreate, install)
7. Commit + push

#### TDD
```
RED:     TestTaskfile_NoBareHelmUpgrade_InInstallMgmt — grep 'helm upgrade --install'
         em taskfiles/infra.yml infra:install:mgmt block → exit 1 (none found)
RED:     TestTaskfile_NoBareBase64Decode_InInstallTargets — EC-02: grep 'base64 -d'
         em taskfiles/infra.yml install:* blocks → exit 1 (none found)
RED:     TestTaskfile_UsesMasterHelmfileSync_NoSelector — EC-01: grep 'helmfile.*--selector'
         em infra:install:mgmt block → exit 1 (zero matches = master sync). Selectors
         OK em outras tasks operacionais; apenas install blocks são master-sync.
RED:     TestHelmfile_GiteaNeedsPostgresql — EC-01: parse mgmt.helmfile.yaml.gotmpl,
         assert gitea release tem postgresql em needs: list.
GREEN:   Edit replaces
VERIFY:  task clean:all && task setup
         # PASS criterion: exit 0 + all 4 clusters Running + all helm releases Deployed
         # T2.3.b portability sanity (run em Mac OR docker linux/amd64):
         grep -RE 'base64[[:space:]]+-d\b' taskfiles/
         # esperado: zero matches (zero portabilidade Linux-only)
```

#### Acceptance Criteria
- [ ] Zero `helm upgrade --install` em `infra:install:*` blocks
- [ ] **EC-01:** `infra:install:mgmt`/`build`/`workload` usam SINGLE master `helmfile sync`
      (sem `--selector`); ordering resolvido via `needs:` declarations no .gotmpl
- [ ] **EC-01:** `mgmt.helmfile.yaml.gotmpl` tem `needs:` explícito em gitea+ory-* releases
      apontando para postgresql
- [ ] **EC-02:** Zero ocorrências de `base64 -d` em `taskfiles/`; substituições via
      `kubectl -o go-template` OU `openssl base64 -d -A` (portable)
- [ ] `task setup` from cold start succeeds (exit 0) em ≤25min
- [ ] All Bitnami/upstream charts converge primeiro try
- [ ] LIVE smoke: `kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods` mostra
      gitea/postgresql/minio/argocd/ory-* todos Running

#### DoD
- [ ] Tasks 1-7 completos (incluindo T2.3.a + T2.3.b)
- [ ] `task setup` PASS LIVE em fresh cluster (Linux mínimo; Mac validation se possível)
- [ ] Commit + push
- [ ] EC-01 verificado: `grep -c 'needs:' infra/helmfile/mgmt.helmfile.yaml.gotmpl` ≥ 4
- [ ] EC-02 verificado: `grep -RE 'base64[[:space:]]+-d\b' taskfiles/` retorna zero matches

### T2.4 — Validate cleanroom funcional

#### Objective
Rodar full cleanroom skill (`/cleanroom`) e confirmar 4/4 phases PASS.

#### Evidence
Goal binding original (cleared via /goal clear): "cleanroom 100% funcional".
Continua sendo o success criterion deste plano.

#### Files to edit
N/A — esta task é execução + validação, não código

#### Tasks
1. `task clean:all && task setup` from cold start
2. `tilt up` (background)
3. Aguardar tilt build images + deploy API/operators
4. `task doctor`
5. Verificar PASS=N FAIL=0 (skill spec: "PASS=38" baseline)

#### TDD
```
RED:     N/A — task de validação operacional
VERIFY:  task doctor 2>&1 | tee /tmp/doctor.txt
         grep -E "^PASS: " /tmp/doctor.txt  # esperado: PASS: ≥38
         grep -E "^FAIL: " /tmp/doctor.txt  # esperado: FAIL: 0
```

#### Acceptance Criteria
- [ ] task setup exit 0 em ≤25min
- [ ] tilt up converge (build + deploy completo) em ≤15min
- [ ] task doctor PASS=N FAIL=0 (N depending on baseline)
- [ ] Zero S0/S1 findings

#### DoD
- [ ] Cleanroom execução documentada em `docs/cleanroom-runs/2026-05-22.md`
  com timestamps + log snippets

---

## Phase 3: Deprecate `task setup` chain (canonicalize Tilt)

**Objective:** Após Phase 2 desbloquear cleanroom, formalizar Tilt como único dev loop
e marcar `task setup` como deprecated.

### T3.1 — Update CLAUDE.md + README to canonicalize Tilt

#### Objective
Documentação clara que dev loop é `tilt up`. `task setup` vira pré-requisito
(criar clusters + install platform) mas dev loop é Tilt.

#### Files to edit
```
CLAUDE.md — atualizar Sprint Plan / development commands section
README.md — first-steps section
docs/operations/dev-loop.md — (NEW) canonical guide
```

#### Tasks
1. CLAUDE.md: substituir `task setup; tilt up` por explicit "task setup creates platform,
   tilt up runs dev loop"
2. README.md: substituir Getting Started com canonical flow
3. Criar [`dev-loop.md`](../../operations/dev-loop.md) com troubleshooting (port-forwards, common errors)

#### TDD
```
RED:     N/A — documentação
VERIFY:  grep -c "task setup.*tilt up\|tilt up.*task setup" CLAUDE.md README.md
         # esperado: 0 (no co-mention as if equal)
         grep -c "canonical dev loop" docs/operations/dev-loop.md
         # esperado: ≥1
```

#### Acceptance Criteria
- [ ] CLAUDE.md cite Tilt explicitamente como canonical
- [ ] dev-loop.md criado com sections: Quick Start, Troubleshooting, FAQ

#### DoD
- [ ] Docs committed
- [ ] Operator review

### T3.2 — Add deprecation warning to `task setup`

#### Objective
Adicionar print warning em `task setup` indicando que dev loop deve ser via Tilt.

#### Files to edit
```
Taskfile.yml — adicionar echo warning no início de setup target
```

#### Tasks
1. Adicionar primeiro cmd em `setup:` target: `echo "[NOTICE] task setup creates platform; dev loop is via 'tilt up'. See docs/operations/dev-loop.md"`

#### TDD
```
RED:     TestTaskfileSetup_HasDeprecationNotice — grep "tilt up" no Taskfile.yml setup block
GREEN:   Edit
VERIFY:  task setup --dry-run 2>&1 | grep -c "tilt up"
         # esperado: ≥1
```

#### Acceptance Criteria
- [ ] Warning aparece no início de task setup

#### DoD
- [ ] Commit + push

---

## Phase 4: Migrate Test Orchestrators to Ginkgo

**Objective:** Converter scripts em `infra/tests/parity/`, `infra/tests/dogfood/`
(active, não _legacy) para Ginkgo specs, reaproveitando fixtures de `infra/tests/e2e/`.

### T4.1 — Audit parity/dogfood scripts + map to Ginkgo Describe blocks

#### Objective
Para cada script de teste shell em parity/dogfood, identificar:
- O que testa (probe + assertion)
- Fixtures necessárias
- Equivalent Ginkgo `Describe("...") / It("...")` block

#### Files to edit
```
docs/scripts-audit/2026-05-22-ginkgo-mapping.md — (NEW) mapping
```

#### Tasks
1. Para cada `.sh` em `infra/tests/parity/`: identificar tipo (degrau 1/2/3, k3d/droplet/DOKS)
2. Para cada `.sh` em `infra/tests/dogfood/` (não _legacy): identificar journey
3. Documentar mapping table

#### TDD
```
RED:     N/A — análise
VERIFY:  cat docs/scripts-audit/2026-05-22-ginkgo-mapping.md | grep -c '^|'
         # esperado: ≥30 entries (uma per script de teste)
```

#### Acceptance Criteria
- [ ] Mapping cobre 100% dos scripts em parity/ e dogfood/ (não _legacy)

#### DoD
- [ ] Doc committed + reviewed

### T4.2 — Convert top-5 parity scripts to Ginkgo specs

#### Objective
Migrate 5 scripts mais representativos pra Ginkgo. Estabelecer pattern reutilizável
pelos restantes.

#### Files to edit
```
infra/tests/e2e/parity_test.go — (NEW) Ginkgo Describe blocks
infra/tests/parity/validate-matrix.sh — DELETE (substituído por Ginkgo)
infra/tests/parity/audit-regression-matrix.sh — DELETE
infra/tests/parity/k3d-virgin-install.sh — DELETE
infra/tests/parity/droplet-install.sh — KEEP (operator-time DOKS, no Ginkgo equivalent)
infra/tests/parity/doks-install.sh — KEEP (operator-time DOKS)
```

#### Tasks
1. Criar `parity_test.go` com Ginkgo `Describe("Parity matrix")` + 5 `It` blocks
2. Migrar logic + assertions dos 3 scripts deletados
3. Validar: `LOOPZERO_E2E=1 go run ginkgo -v --label-filter=parity ./infra/tests/e2e/`
4. Commit + delete bash + push

#### TDD
```
RED:     TestParityMatrix_K3dVirgin — Ginkgo It block testa k3d virgin path
GREEN:   Implement
VERIFY:  LOOPZERO_E2E=1 go run github.com/onsi/ginkgo/v2/ginkgo -v \
           --label-filter="parity" --timeout=10m ./infra/tests/e2e/
         # esperado: pass
```

#### Acceptance Criteria
- [ ] 5 Ginkgo It blocks substituem 3 shell scripts (consolidam logic)
- [ ] Ginkgo run PASS local
- [ ] Bash deletado em mesmo commit

#### DoD
- [ ] Commits committed + pushed
- [ ] Pattern documented in `docs/scripts-audit/parity-ginkgo-pattern.md`

### T4.3 — Convert remaining parity + dogfood scripts (batch)

#### Objective
Aplicar pattern de T4.2 aos remaining scripts. Cada batch é 1 commit.

#### Files to edit
- Multiple `infra/tests/e2e/*_test.go` (NEW Ginkgo specs)
- Multiple `infra/tests/parity/*.sh` (DELETE)
- Multiple `infra/tests/dogfood/*.sh` (DELETE, exceto _legacy queja é DELETE-DEAD)

#### Tasks
1. Por batch (5-10 scripts), repetir pattern T4.2
2. Validar Ginkgo run após cada batch
3. CHANGELOG entry por batch

#### TDD + Acceptance + DoD
Same structure as T4.2, repeated per batch.

### T4.4 — Delete `_legacy/` entirely

#### Objective
12 scripts em `infra/tests/dogfood/journeys/_legacy/` são por definição legacy.
DELETE em massa.

#### Tasks
1. `git rm -r infra/tests/dogfood/journeys/_legacy/`
2. CHANGELOG entry
3. Commit + push

#### Acceptance Criteria
- [ ] `_legacy/` directory removed
- [ ] Zero callers (verified via T0.1 audit re-run)

---

## Phase 5: SDK-native Go Migrations (parallel with Phase 4)

**Objective:** Pulumi ops + generators + remaining pure-logic gates. **SDK-native only**
— shell-wrap em Go é PROIBIDO per D5.

### T5.1 — Pulumi Automation API migration (replaces pulumi-archive, pulumi-orphan-rm)

#### Objective
Rewrite as 2 migrações shell-wrap desta sessão (pulumi-archive + pulumi-orphan-rm)
para usar `pulumi-go-sdk` Automation API. Net: elimina dependência `pulumi` CLI binary.

#### Files to edit
```
pkg/pulumi/archive/check.go — REWRITE usando pulumi.AutoLocalWorkspace
pkg/pulumi/archive/check_test.go — UPDATE (Mock via auto.NewLocalWorkspace + testdata)
pkg/pulumi/orphanrm/check.go — REWRITE usando auto API
pkg/pulumi/orphanrm/check_test.go — UPDATE
go.mod (pkg/) — ADD github.com/pulumi/pulumi/sdk/v3
```

#### Deep file dependency analysis
- `pkg/pulumi/archive/check.go`: substituir `shellrun.Runner.Run(ctx, "pulumi", ...)`
  por `auto.NewLocalWorkspace + ws.SelectStack + stack.Export`
- `pkg/pulumi/archive/check_test.go`: testar via testdata fixtures de stack state
  JSON (não via MockRunner)

#### Deep Dives
**Automation API pattern:**
```go
import "github.com/pulumi/pulumi/sdk/v3/go/auto"

ws, err := auto.NewLocalWorkspace(ctx,
    auto.WorkDir(filepath.Join(root, "infra/pulumi-infra")),
    auto.EnvVars(map[string]string{"PULUMI_CONFIG_PASSPHRASE": passphrase}))
if err != nil { return err }

stack, err := auto.SelectStack(ctx, "dev", ws)
if err != nil { return err }

state, err := stack.Export(ctx)  // No pulumi CLI shellout — pure Go API
if err != nil { return err }
```

**Trade-off:** Automation API requer same `PULUMI_CONFIG_PASSPHRASE` env, mas elimina
binary dep. Net: melhor em CI (sem `pulumi` install step), mas dependency adicionada
ao go.mod.

#### Tasks
1. `cd pkg && go get github.com/pulumi/pulumi/sdk/v3`
2. Rewrite `pkg/pulumi/archive/check.go` usando auto API
3. Rewrite `pkg/pulumi/archive/check_test.go` (testdata-based)
4. Same para `pkg/pulumi/orphanrm/`
5. Build + test
6. Commit + push

#### TDD
```
RED:     TestAutoAPI_ExportStack — test export via auto.NewLocalWorkspace + mock workspace
RED:     TestAutoAPI_NoShellrunCalled — sanity: package não importa shellrun
GREEN:   Implement
VERIFY:  cd pkg && go test ./pulumi/...
         # esperado: cov ≥ 90%
         grep -rn 'shellrun' pkg/pulumi/  # esperado: 0 matches
```

#### Acceptance Criteria
- [ ] Zero `shellrun` import em `pkg/pulumi/*`
- [ ] Test coverage ≥ 90%
- [ ] LIVE smoke: roda contra real Pulumi stack sem `pulumi` CLI no PATH

#### DoD
- [ ] Tasks 1-6 completos
- [ ] Commits committed + pushed
- [ ] go.mod updated

### T5.2 — Migrate generators to `//go:generate`

#### Objective
Scripts geradores (gen-state-machine-dots.sh, generate-secrets-key, etc.) → `go generate`
em arquivo Go apropriado.

#### Files to edit
```
pkg/statemachine/visualize.go — adicionar `//go:generate go run ./cmd/gen-dots`
pkg/statemachine/cmd/gen-dots/main.go — (NEW) generator binary
infra/scripts/gen-state-machine-dots.sh — DELETE
```

#### Tasks
1. Criar `pkg/statemachine/cmd/gen-dots/main.go` — calls `Visualize()` + writes `.dot` files
2. Adicionar `//go:generate` directive em visualize.go
3. Documentar workflow: `cd pkg && go generate ./statemachine/...`
4. Delete bash + Taskfile entry update

#### TDD
```
RED:     TestGenerator_ProducesAllDotFiles — `go generate` produces N .dot files
GREEN:   Implement
VERIFY:  cd pkg && go generate ./statemachine/...
         ls docs/state-machines/*.dot | wc -l  # esperado: N (matches state machine count)
```

#### Acceptance Criteria
- [ ] `go generate` produces expected `.dot` files
- [ ] Bash deletado
- [ ] Taskfile updated to call `go generate`

#### DoD
- [ ] Tasks 1-4 completos
- [ ] Commits committed + pushed

### T5.3 — Identify remaining pure-logic gates (audit-time decision)

#### Objective
Phase 0 audit identifies scripts ainda candidatos a pure-logic Go migration.
Migrar OS QUE SOBRARAM após Phase 2 (delegate to helmfile reduz set).

#### Files to edit
Multiple `pkg/quality/<gate>/check.go` (NEW) per migration

#### Tasks
Same pattern as the 16 already migrated (Categoria A).
1. Per script: check.go + check_test.go + wire in quality_subcmd
2. Delete bash + CHANGELOG entry

#### TDD + Acceptance + DoD
Same template per migration.

---

## Phase 6: Lock The Door (No-New-Bash Gate + Allowlist)

**Objective:** Tornar a redução **estrutural** — bloquear via CI a criação de novos
`.sh` outside da allowlist. Sem isso, o esforço regridir.

### T6.1 — Create `.claude/rules/shell-scripts-allowlist.md`

#### Objective
Lista explícita de scripts bash KEEP-OPERATOR per D4 criteria, com justificativa
linha-a-linha.

#### Files to edit
```
.claude/rules/shell-scripts-allowlist.md — (NEW)
```

#### Deep Dives
**Format:**
```markdown
# Shell Scripts Allowlist

Scripts permitidos no repo. Cada entry justifica per D4 (necessário + simples +
operator-time + allowlisted). Novos scripts requerem PR adding-entry-here + ADR.

## infra/scripts/

| Script | LOC | Justificativa |
|---|---:|---|
| break-glass.sh | 145 | Operator-time emergency procedure; cluster-admin SA + audit trail. Sem alternative Go SDK ad-hoc. Critério D4: SIM (4/4). |
| migrate-tenant-namespace.sh | 229 | One-off migration operator-driven; complex multi-step kubectl orchestration. Critério D4: PARCIAL (op-time SIM, simples NO). Plano P7: avaliar Go migration. |
...
```

#### Tasks
1. Listar scripts post-Phase 5 (after delete + migrate)
2. Per script, validar criteria D4 (4 checkmarks)
3. Documentar justificativa per linha
4. Marcar scripts que FALHAM critério como "review-needed-Phase-7"

#### TDD
```
RED:     N/A — documentação
VERIFY:  wc -l .claude/rules/shell-scripts-allowlist.md  # esperado: ≥30 lines (header + entries)
         # Count entries vs actual scripts:
         grep -c '^| ' .claude/rules/shell-scripts-allowlist.md
         find infra/ scripts/ -name '*.sh' | wc -l
         # esperado: counts iguais (1 entry per script)
```

#### Acceptance Criteria
- [ ] Toda `.sh` no repo tem entry na allowlist
- [ ] Toda entry tem justificativa D4

#### DoD
- [ ] Allowlist committed
- [ ] Operator review

### T6.2 — Build `loopzero-ops quality no-new-bash` CI gate

#### Objective
Gate Go que falha PR se introduz novo `.sh` não presente na allowlist.

#### Files to edit
```
pkg/quality/nonewbash/check.go — (NEW)
pkg/quality/nonewbash/check_test.go — (NEW)
cmd/loopzero-ops/quality_subcmd.go — wire gate
.github/workflows/ci.yml — invoke gate em PRs
```

#### Deep Dives
**Algorithm:**
1. Parse `.claude/rules/shell-scripts-allowlist.md` → extract allowed set
2. `git diff --name-only --diff-filter=A origin/main...HEAD -- '*.sh'` → new scripts
3. For each new script: check if in allowlist
4. If yes: PASS. If no: FAIL with helpful message ("Add entry to allowlist OR use Go/helmfile alternative")

**Edge cases:**
- `.sh` em `_legacy/` paths: still blocked (forbidden zone)
- Renamed file (R status): treated as new + old (require allowlist update)
- Modified existing file (M status): NOT blocked (only A status)
- **EC-04 shallow clone trap:** `git diff origin/main...HEAD` requer histórico
  completo da base branch. GitHub Actions default `actions/checkout` é `fetch-depth: 1`
  (shallow). Sem fetch-depth: 0, base-ref unreachable → diff vira "tudo novo" →
  gate marca TODOS os scripts allowlistados como violations → CI fica permanentemente
  vermelho → operadores aprendem a desabilitar gate → D3 fica letra-morta.

**EC-04 mitigation requirements (mandatory):**
1. Gate detecta unreachable base-ref e falha com erro CLARO (não com 100 falsos positivos)
2. CI workflow YAML usa `actions/checkout@v4` com `fetch-depth: 0`
3. Gate documenta no help: "Requires full git history; set fetch-depth: 0 in checkout"

#### Tasks
1. Implement `parseAllowlist(path string) (map[string]bool, error)`
2. Implement `Check(cfg Config) (*Outcome, error)` with git diff logic
3. **EC-04 pre-flight:** Antes do diff, executar `git rev-parse --verify {baseRef}`;
   se falha, retornar `LoopZeroError{Code: "BASE_REF_UNREACHABLE", Hint: "Set fetch-depth: 0 in actions/checkout"}`
4. Wire to `quality nonewbash` subcommand
5. Add to ci.yml as PR-blocking step **com `fetch-depth: 0` explícito** no checkout
6. Test: fixture com PR fictício adicionando `.sh` → gate FAIL

#### TDD
```
RED:     TestParseAllowlist_ParsesMarkdownTable — fixture allowlist → set with N entries
RED:     TestCheck_NewBashWithoutAllowlist_Blocks — fixture diff with new .sh → Pass=false
RED:     TestCheck_NewBashWithAllowlist_Passes — fixture diff with .sh in allowlist → Pass=true
RED:     TestCheck_ModifiedExistingBash_NotBlocked — fixture diff with M (not A) → Pass=true
RED:     TestCheck_ShallowClone_FailsWithClearError — EC-04: simular base-ref unreachable
         (criar repo com `git clone --depth=1` em tmpdir, rodar gate); assert error.Code==
         "BASE_REF_UNREACHABLE" + hint mention "fetch-depth: 0". Garante que falha é
         actionable e NÃO produz 100 falsos positivos silenciosamente.
RED:     TestCheck_RenamedBashOutsideAllowlist_Blocked — EC-06: fixture diff com
         `R100 old.sh new.sh` onde new.sh não está em allowlist; assert Pass=false
         + mensagem ("Rename detected: add 'new.sh' to allowlist OR keep old name")
GREEN:   Implement
VERIFY:  cd pkg && go test ./quality/nonewbash/...  # cov ≥ 90%
         /tmp/loopzero-ops quality nonewbash --base-ref origin/main --head-ref HEAD
         # exit 0 on clean branch, exit 1 if branch added unauthorized .sh
         # EC-04 sanity: verificar ci.yml tem fetch-depth: 0
         grep -A2 'actions/checkout' .github/workflows/ci.yml | grep -c 'fetch-depth: 0'
         # esperado: ≥1 (no job que invoca gate)
```

#### Acceptance Criteria
- [ ] Gate detecta novos `.sh` (A status) outside allowlist
- [ ] Gate aceita `.sh` em allowlist
- [ ] Gate ignora modificações (M status) — só new files
- [ ] **EC-04:** Gate falha com `BASE_REF_UNREACHABLE` typed error quando base-ref unreachable
      (não produz falsos positivos em massa)
- [ ] **EC-04:** CI workflow YAML usa `actions/checkout@v4` com `fetch-depth: 0` no job
      que invoca `loopzero-ops quality nonewbash`
- [ ] **EC-06:** Gate trata file rename (R status) como new file (requires allowlist update)
- [ ] CI workflow invoca gate em todos PRs

#### DoD
- [ ] Tasks 1-6 completos (incluindo EC-04 pre-flight)
- [ ] CI workflow updated (com fetch-depth: 0)
- [ ] Self-test em PR fictício: adicionar `.sh` random → CI fails (deterministicamente)
- [ ] Self-test EC-04: simular shallow clone localmente, gate falha com erro claro
      (não com 100 falsos positivos)

### T6.3 — Build `loopzero-ops quality no-shell-wrap` audit gate

#### Objective
Auditar `pkg/*` por padrão `shellrun.Runner.Run(<CLI-on-allowlist>, ...)` quando há
SDK Go nativo disponível.

#### Files to edit
```
pkg/quality/noshellwrap/check.go — (NEW)
pkg/quality/noshellwrap/check_test.go — (NEW)
cmd/loopzero-ops/quality_subcmd.go — wire
.github/workflows/ci.yml — invoke
```

#### Deep Dives
**Forbidden CLI set** (CLIs que têm Go SDK nativo):
- `pulumi` → use `pulumi-go-sdk` automation API
- `helm` → use `helm.sh/helm/v3` SDK
- `kubectl` → use `client-go` (k8s.io/client-go)
- `aws` → use `aws-sdk-go-v2`
- `gh` → use `go-github` (github.com/google/go-github)

**Allowed CLI set** (legítimos pra shellrun):
- `git` (no good Go alternative)
- `diff`, `sed`, `awk`, `grep` (system utils)
- `docker`, `k3d` (Go SDKs exist but immature)

**Algorithm:**
1. `filepath.WalkDir pkg/`, encontrar arquivos `.go`
2. Per file: parse `ast.File`, find `shellrun.Runner.Run` calls
3. Para cada call, extract primeiro argument literal string
4. Se em forbidden set + sem `//nolint:shell-wrap` justification comment → VIOLATION

**Pre-existing exceptions:** os 6 shell-wraps desta sessão (categoria B) precisam ser
migrados (Phase 5.1 cobre pulumi-archive/pulumi-orphan-rm já). Outros (chaos/cleanuporphan,
credentialsisolation, upstream/rfcfile) podem manter `//nolint:shell-wrap` comment com
justificativa OR também migrar.

#### Tasks
1. Implement AST walker
2. Per call site, check CLI argument
3. Allow `//nolint:shell-wrap` escape hatch with required reason
4. Wire to quality gate + CI

#### TDD
```
RED:     TestNoShellWrap_DetectsForbiddenCLI — fixture pkg with shellrun.Run("pulumi"...) → violation
RED:     TestNoShellWrap_AllowsAllowedCLI — fixture pkg with shellrun.Run("git"...) → PASS
RED:     TestNoShellWrap_HonorsNolintComment — fixture with //nolint:shell-wrap → PASS
GREEN:   Implement AST walker
VERIFY:  cd pkg && go test ./quality/noshellwrap/...
         /tmp/loopzero-ops quality noshellwrap  # exit 0 OR exit 1 with list of violations
```

#### Acceptance Criteria
- [ ] Gate detecta `shellrun.Runner.Run("pulumi"...)` sem nolint
- [ ] Gate aceita allowlisted CLIs (git, diff, etc.)
- [ ] Gate aceita nolint comment com reason

#### DoD
- [ ] Tasks completos
- [ ] CI workflow invoca gate

### T6.4 — Final inventory validation

#### Objective
Re-run `loopzero-ops quality scripts-audit` post-Phase-5 e confirmar metas atingidas.

#### Tasks
1. Rodar audit
2. Comparar com baseline 2026-05-22
3. Verificar metas: N(bash) ≤ 30, LOC(bash) ≤ 3,000
4. Documentar redução em `docs/scripts-audit/2026-05-22-final.md`

#### TDD
```
VERIFY:  /tmp/loopzero-ops quality scripts-audit > /tmp/final.csv
         python3 -c "
import csv
rows = list(csv.DictReader(open('/tmp/final.csv')))
n = len(rows)
loc = sum(int(r['loc']) for r in rows)
print(f'N={n} LOC={loc}')
assert n <= 30, f'N={n} exceeds 30'
assert loc <= 3000, f'LOC={loc} exceeds 3000'
"
```

#### Acceptance Criteria
- [ ] N(bash) ≤ 30
- [ ] LOC(bash) ≤ 3,000
- [ ] All remaining scripts em allowlist

#### DoD
- [ ] Final audit committed
- [ ] CHANGELOG section "Phase 6 complete: 269 → N scripts (X% reduction)"

---

## Phase 7: Adopt External Tools (P0 gaps from kubetools-review)

**Objective:** Adotar 2 external tools identificadas como P0 gaps reais na análise
kubetools-review (2026-05-22). Materializa Anti-Pattern #8 ("adopt > reinvent")
implementando a ferramenta externa madura em vez de criar gate Go interno equivalente.

**Trade-off explícito:** Phase 7 é scope addition além do shell-script-reduction
original. Inclusa porque (a) Anti-Pattern #8 criou anchor que necessita demonstração
prática, (b) ambas adoções têm impacto direto no go-to-market (Pluto destrava DOKS
upgrade; OpenCost destrava pricing validation Sprint 9), (c) execução paralela
a Phase 6 — zero file overlap.

### T7.1 — Adopt Pluto (Fairwinds deprecated K8s API detector)

#### Objective
Adicionar CI gate que escaneia manifests por APIs Kubernetes deprecated/removed,
falhando PR se encontrar. Materializa "tool externa madura > Go gate interno"
(Anti-Pattern #8) para o caso deprecated-API check.

#### Evidence
DOKS hoje roda v1.32.13; v1.33 já é stable. Próximo upgrade pode quebrar manifests
usando APIs deprecated (e.g., `policy/v1beta1 PodDisruptionBudget` removida em v1.25,
mas hipoteticamente APIs novas vão sair na v1.34+). Sem Pluto, descobrimos no
upgrade live = production incident potencial.

Pluto é tool madura (Fairwinds, ativa, último release < 6 meses), CLI binary
distribuído, **NÃO requer instalação no cluster** — escaneia manifests estáticos.

#### Files to edit
```
.github/workflows/helm-charts.yml  — add `pluto-check` job (NEW step)
.trivyignore-pluto                 — (NEW, optional) suppressions documentadas
infra/scripts/pluto-scan.sh        — (NEW se necessário, allowlisted; OR usar Action direto)
docs/operations/k8s-upgrade-runbook.md — (NEW) reference Pluto output em upgrade flow
.claude/rules/shell-scripts-allowlist.md — entry se pluto-scan.sh criado
```

#### Deep file dependency analysis
- `helm-charts.yml`: adicionar job paralelo `pluto-check` que renderiza TODOS os
  Helm charts (`infra/helm/*` + `charts/*`) via `helm template` e roda Pluto contra o
  output. Não substitui kubeconform/Polaris — complementa.
- `pluto-scan.sh` opcional: pode rodar Action `FairwindsOps/pluto@<sha>` direto
  no workflow YAML, evitando criar script. **Decisão default:** Action direto (sem script).
- [`k8s-upgrade-runbook.md`](../../operations/k8s-upgrade-runbook.md): doc operator que `pluto detect-helm` deve rodar antes
  de aceitar upgrade DOKS, e como interpretar output (FATAL vs DEPRECATED).

#### Deep Dives

**Pluto modes (per docs):**
- `pluto detect` — scan stdin (raw YAML)
- `pluto detect-files -d <dir>` — scan directory recursively
- `pluto detect-helm` — scan installed Helm releases (requires kubeconfig — NÃO usar em CI)
- `pluto detect-api-resources` — scan via kubeconfig (NÃO usar em CI)

**CI workflow integration pattern (canonical):**
```yaml
- name: Render all Helm charts for Pluto scan
  run: |
    mkdir -p /tmp/rendered-manifests
    for chart in infra/helm/*/ charts/*/; do
      name=$(basename "$chart")
      helm template "$name" "$chart" > "/tmp/rendered-manifests/${name}.yaml" 2>/dev/null || true
    done

- name: Pluto - detect deprecated K8s APIs
  uses: FairwindsOps/pluto/github-action@<sha>  # pin SHA per INF-G18
  with:
    fileFolder: /tmp/rendered-manifests
    target-versions: k8s=v1.32.0  # current DOKS version
    only-show-removed: false  # show deprecated + removed
```

**Failure thresholds (decisão default — revisável):**
- **FAIL CI** se: API removida no target version (FATAL); API deprecated com replacement
  disponível há > 2 versions
- **WARN only** (não fail) se: API deprecated recente (current version - 1)

**Invariants:**
- Pluto runs em TODO PR que toca `infra/helm/**`, `charts/**`, `.github/workflows/helm-charts.yml`
- Pluto runs em weekly cron (proativo — captura novas deprecations sem PR trigger)
- Pluto version pinned via Renovate (não floating)
- Suppressions documentadas em `.trivyignore-pluto` com link para ADR/runbook

**Edge cases:**
- Helm chart com `range` ou `if` que produz YAML condicional → Pluto vê só rendered output, não template; aceitável.
- Chart com CRDs próprios (loopzero-crds) → Pluto não conhece — passa silently. OK (não é deprecated API check).
- Action `FairwindsOps/pluto@<sha>` requer `fileFolder` existir + ter `.yaml`/`.yml` files; empty dir → exit 0 silently. **Test fixture mandatório** para validar deteção real.

#### Tasks
1. Adicionar job `pluto-check` em `helm-charts.yml` paralelo aos existentes
2. SHA-pin Action via Renovate config (`.github/renovate.json` já trata)
3. Criar fixture de teste: chart com `apiVersion: policy/v1beta1` (removed em v1.25)
   → Pluto detect → CI fails
4. Criar [`docs/operations/k8s-upgrade-runbook.md`](../../operations/k8s-upgrade-runbook.md) (ou seção em runbook existente)
   documentando workflow de upgrade DOKS com Pluto check
5. Update `.claude/rules/shell-scripts-allowlist.md` (T6.1) **SE** decidir criar
   `infra/scripts/pluto-scan.sh` (default decision: NÃO criar — Action direto)
6. Commit + push

#### TDD
```
RED:     TestPlutoFixture_DetectsRemovedAPI — fixture com kind: PodDisruptionBudget
         apiVersion: policy/v1beta1 (removido em v1.25); rodar pluto detect-files
         → exit code != 0
RED:     TestPlutoFixture_PassesOnCurrent — fixture com kind: PodDisruptionBudget
         apiVersion: policy/v1 (current) → exit 0
RED:     TestWorkflow_PlutoJobPresent — grep `pluto-check:` em helm-charts.yml → 1+
GREEN:   Implement workflow job + fixture
VERIFY:  # Local validation (requires Pluto binary):
         pluto detect-files -d infra/helm/ --target-versions k8s=v1.32.0
         # Expected: exit 0 today (no deprecated APIs in current manifests)
         # CI validation:
         act -j pluto-check  # GitHub Actions local runner
```

#### Acceptance Criteria
- [ ] `pluto-check` job em `helm-charts.yml` (paralelo)
- [ ] Action SHA-pinned
- [ ] Fixture test demonstra detecção de API removida
- [ ] Runbook DOKS upgrade documenta Pluto check
- [ ] LIVE: workflow PASS contra current manifests (zero deprecated detected hoje)
- [ ] LIVE: workflow FAIL contra fixture deliberada

#### DoD
- [ ] Tasks 1-6 completos
- [ ] CI green em PR
- [ ] Fixture test funciona (deliberado FAIL)
- [ ] CHANGELOG entry sob `### Added` mencionando Pluto adoption + tool inventory README update
- [ ] README.md § Quality & Validation Gates atualizado adicionando Pluto entry

### T7.2 — Adopt OpenCost (cost attribution per tenant)

#### Objective
Instalar OpenCost no mgmt cluster, integrado com Prometheus existente, para
emitir métricas de custo por tenant (via namespace label). Materializa
Anti-Pattern #8 ("adopt > reinvent") para o caso cost-attribution — alternativa
a `pkg/quality/costattribution/` Go interno (que seria reimplementação).

#### Evidence
Pricing model documentado em CLAUDE.md (Starter Free / Pro $29 / Team $99 /
Enterprise custom) não tem **nenhuma** validação de custo real per-tenant.
Sprint 9 gate "(b) 1 prod customer day-trial with no incidents" requer baseline
para responder: "Pro $29 cobre custo de 10 projects do customer X?".

OpenCost é tool madura (CNCF Sandbox, ex-Kubecost OSS, ativa), Helm chart oficial
em `opencost-charts/opencost`, integra com Prometheus federation (já temos).
Cost source = DigitalOcean pricing (free public data).

#### Files to edit
```
infra/helm/opencost/Chart.yaml                              — (NEW) chart wrapper
infra/helm/opencost/values.yaml                             — (NEW) defaults
infra/helm/opencost/values.schema.json                      — (NEW) schema (per T3.1 pattern)
infra/helmfile/mgmt.helmfile.yaml.gotmpl                    — add opencost release (needs: monitoring)
infra/helmfile/values/standalone-multi/opencost.yaml.gotmpl — (NEW) env-specific config
infra/helm/monitoring/templates/opencost-podmonitor.yaml    — (NEW) Prometheus scrape config
charts/loopzero-monitoring/templates/opencost-rules.yaml        — (NEW, optional) recording rules
docs/operations/cost-attribution.md                         — (NEW) operator guide
README.md                                                   — update Stack table adding OpenCost
CLAUDE.md                                                   — update Stack/Helm charts list
```

#### Deep file dependency analysis
- `infra/helm/opencost/`: wrapper chart pointing to upstream `opencost/opencost`
  via dependency, com overrides LoopZero-specific (namespace label = `loopzero.io/tenant`).
- `mgmt.helmfile.yaml.gotmpl`: adicionar release com `needs: [loopzero-system/monitoring]`
  (precisa Prometheus rodando primeiro pra scrape).
- `monitoring/templates/opencost-podmonitor.yaml`: PodMonitor CR que faz Prometheus
  scrapear `:9003/metrics` do OpenCost pod.
- [`cost-attribution.md`](../../operations/cost-attribution.md): doc operador — como rodar `kubectl port-forward svc/opencost 9090`
  pra ver dashboard local; como query `kubectl_pod_cost_per_hour` per `loopzero.io/tenant`.

#### Deep Dives

**OpenCost deployment topology (decisão de design):**
- **Onde instalar:** mgmt cluster (Prometheus federation já lá; multi-cluster querying
  via federation está OK).
- **Per-cell ou central:** central (mgmt). Cada cell tem seu Prometheus → federation
  envia métricas pra mgmt → OpenCost lá calcula attribution.
- **Storage:** OpenCost mantém estado em memória (rolling window 15d default). Long-term
  storage via Prometheus existente (Thanos seria over-engineering hoje).

**Cost calculation source (decisão de design):**
- DigitalOcean **on-demand** pricing (public, free): https://www.digitalocean.com/pricing
- OpenCost `kubecostProductConfigs.customPricesEnabled: true` com `cpu/$0.0...`, `ram/$0.0...`
  override per DOKS node sizes
- **Alternative rejected:** AWS pricing API (irrelevante — não usamos AWS), Cloud Marketplace
  pricing (DO não publica em formato OpenCost-native)

**Tenant attribution label:**
- LoopZero tenant CR `metadata.labels[loopzero.io/tenant]` já existe em workload namespaces
- OpenCost `kubecostMetrics.emitNamespaceAnnotationsAsLabels: true` para emergir labels
- Query Prometheus: `sum by (namespace) (kubecost_namespace_cost_per_hour{label_loopzero_io_tenant!=""})`

**Multi-cell consideration:**
- Cell-default + cell-1 + cell-2 cada um tem workload namespaces — OpenCost central
  precisa scraping cross-cluster
- Default: Prometheus federation já scrapea cells → OpenCost calcula sobre métricas
  federadas — works out of the box, NOT requires per-cell OpenCost install

**Invariants:**
- OpenCost pod Running em mgmt cluster pós-sync
- Prometheus scrapea `:9003/metrics` (200 OK, métricas presentes)
- Query `sum(kubecost_namespace_cost_per_hour) by (namespace)` retorna dados pra todos os tenant namespaces
- Cost per tenant visível na Grafana via dashboard novo ([`docs/operations/cost-attribution.md`](../../operations/cost-attribution.md) referencia)

**Edge cases:**
- Tenant namespace recém-criado → OpenCost leva 1h pra ter dados meaningful (rolling window). DOC.
- Tenant suspended → namespace ainda existe; cost continua attribuído. Comportamento esperado.
- Cell drain → workloads movem, cost attribuição segue namespace. OK.

#### Tasks
1. Criar `infra/helm/opencost/` wrapper chart (Chart.yaml + values.yaml + values.schema.json)
2. Adicionar release em `mgmt.helmfile.yaml.gotmpl` com `needs: [loopzero-system/monitoring]`
3. Adicionar PodMonitor em `infra/helm/monitoring/templates/opencost-podmonitor.yaml`
4. Validar local: `helmfile -e standalone-multi --selector name=opencost template` renderiza sem erro
5. Validar local: instalar em k3d mgmt cluster, verificar pod Running + metrics endpoint
6. Criar [`docs/operations/cost-attribution.md`](../../operations/cost-attribution.md) com query examples + Grafana dashboard ID
7. Update README.md § Stack + § Helm Charts Inventory adicionando OpenCost
8. Update CLAUDE.md § Stack (Platform Charts section) adicionando OpenCost
9. Commit + push

#### TDD
```
RED:     TestHelmfileTemplate_OpenCostRendersSemErro — helmfile template opencost → exit 0
RED:     TestOpenCostPod_RunningPostSync — k8s assertion: pod loopzero-system/opencost Running
         apos `helmfile sync --selector name=opencost` em test cluster
RED:     TestPodMonitor_OpenCostScrapeConfigured — PodMonitor CR criada com endpoints[0].port=metrics
RED:     TestPrometheusQuery_OpenCostMetricsPresent — query `up{job="opencost"}=1` retorna >=1
         apos pod Ready + Prometheus scrape window (60s)
GREEN:   Implement chart + helmfile entry + PodMonitor
VERIFY:  # Local validation:
         task setup  # mgmt cluster + monitoring + opencost
         kubectl --context k3d-loopzero-mgmt -n loopzero-system get pods -l app=opencost
         # Expected: 1/1 Running
         kubectl --context k3d-loopzero-mgmt -n loopzero-system port-forward svc/opencost 9003:9003 &
         curl -sf http://localhost:9003/metrics | grep kubecost_
         # Expected: metrics present
         # Grafana validation:
         kubectl --context k3d-loopzero-mgmt -n loopzero-system port-forward svc/grafana 3000:80 &
         # Browser → http://localhost:3000 → import OpenCost dashboard
```

#### Acceptance Criteria
- [ ] `infra/helm/opencost/` chart criado com values.schema.json
- [ ] Helmfile release adicionada com `needs:` correto
- [ ] PodMonitor configurada
- [ ] LIVE local: pod Running + métricas presentes em Prometheus
- [ ] Operator runbook [`docs/operations/cost-attribution.md`](../../operations/cost-attribution.md) criado
- [ ] README.md + CLAUDE.md Stack tables atualizadas
- [ ] Pass: schema validation (`task validate:helm-charts`)
- [ ] Pass: kubeconform contra rendered template

#### DoD
- [ ] Tasks 1-9 completos
- [ ] Local validation: kubecost métricas presentes em Prometheus
- [ ] Cross-cell scenario validado: cell-1 + cell-2 workloads aparecem em queries (via federation)
- [ ] CHANGELOG entry sob `### Added` mencionando OpenCost adoption
- [ ] Grafana dashboard ID documentado (community dashboard ID OR custom JSON em `infra/helm/monitoring/dashboards/opencost.json`)

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Inventário objetivo de 269 scripts + callers | T0.1 | `loopzero-ops quality scripts-audit` emite CSV |
| 2 | Identificar dead code candidates | T1.1 + T0.1 | CSV disposition column |
| 3 | Delete dead scripts | T1.2 | git rm em batch per dir |
| 4 | Desbloquear cleanroom (Bug A gitea PASSWD) | T2.2 + T2.3 | helmfile set: block injects PASSWD from postgres Secret |
| 5 | Desbloquear cleanroom (Bug B kratos/hydra .gotmpl) | T2.2 + T2.3 | YAML escape fix + helmfile sync replaces bare-helm |
| 6 | Eliminar bare-helm em Taskfile | T2.3 | infra:install:* delegates to helmfile sync |
| 7 | Canonicalize Tilt como dev loop | T3.1 + T3.2 | docs + deprecation warning |
| 8 | Migrate test orchestrators to Ginkgo | T4.1 + T4.2 + T4.3 | parity_test.go + dogfood specs |
| 9 | Delete `_legacy/` | T4.4 | git rm -r |
| 10 | Eliminate pulumi CLI shell-wrap | T5.1 | pulumi-go-sdk Automation API |
| 11 | Generators to `//go:generate` | T5.2 | Idiomatic Go pattern |
| 12 | Block creation of new bash scripts | T6.1 + T6.2 | Allowlist + CI gate `no-new-bash` |
| 13 | Block shell-wrap em Go | T6.3 | CI gate `no-shell-wrap` audita pkg/* |
| 14 | Validate redução atingida | T6.4 | Re-audit + comparar com baseline |
| 15 | **EC-01:** Race postgresql↔gitea em cleanroom | T2.3.a + T2.3 needs: invariant | Master helmfile sync respeita `needs:` |
| 16 | **EC-02:** `base64 -d` quebra em macOS | T2.3.b | Substituir por `kubectl -o go-template` ou `openssl base64 -d -A` |
| 17 | **EC-03:** Audit caller-detection misses dynamic invocations | T1.1.a + T0.1 TDD | Grep `bash "$VAR"` pattern antes de validation; test cobertura |
| 18 | **EC-04:** CI shallow clone falsifica gate no-new-bash | T6.2 EC-04 pre-flight + `fetch-depth: 0` | Pre-flight rev-parse + checkout config explicit |
| 19 | **P0 gap kubetools-review:** Deprecated K8s API detection (DOKS upgrade safety) | T7.1 | Pluto CI gate em helm-charts.yml com SHA-pin |
| 20 | **P0 gap kubetools-review:** Cost attribution per tenant (Sprint 9 pricing validation) | T7.2 | OpenCost helm chart no mgmt cluster, attribution via `loopzero.io/tenant` label |

**Coverage: 20/20 gaps covered (100%)**

---

## Global Definition of Done

- [ ] Phases 0-7 completos (ordem sequencial respeitada per dependency graph; 6 e 7 paralelas)
- [ ] Toda task tem tests passing (Go testify + Ginkgo onde aplicável)
- [ ] Zero golangci-lint warnings em `pkg/quality/scriptsaudit`, `pkg/quality/nonewbash`,
      `pkg/quality/noshellwrap`, `pkg/pulumi/archive`, `pkg/pulumi/orphanrm`
- [ ] Pre-release bias: backward compat NÃO required; deletes deliberados
- [ ] code-audit checks passing nos new packages
- [ ] **`/cross-validation 2026-05-22-shell-script-strategic-plan` PASSED** — zero CRITICO,
      zero GRAVE
- [ ] Metas mensuráveis atingidas:
  - [ ] N(bash) ≤ 30 (de 269)
  - [ ] LOC(bash) ≤ 3,000 (de 44,586)
  - [ ] `task setup` PASS em fresh cluster
  - [ ] `tilt up` converge
  - [ ] CI gate `no-new-bash` ATIVO
  - [ ] CI gate `no-shell-wrap` ATIVO
  - [ ] `.claude/rules/shell-scripts-allowlist.md` cobre 100% dos remaining
  - [ ] **T7.1:** Pluto CI gate ATIVO em `helm-charts.yml` — PASS contra current manifests
  - [ ] **T7.2:** OpenCost pod Running mgmt cluster — métricas presentes em Prometheus
        com attribution per tenant via `loopzero.io/tenant` label
  - [ ] README.md § Tool Inventory atualizado (Pluto + OpenCost movidos de "P0 gap" para adopted)
  - [ ] CLAUDE.md § Stack + § Tool Inventory atualizados removendo Pluto + OpenCost dos "v1.x deferred"
- [ ] **Evidence:** N/A — este plano NÃO afeta maturity score (é refactor interno).
- [ ] **`/dogfood full` PASSED** (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

### Execution

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up &
task doctor

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS (esperado ≥38 após Phase 2)
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to script-reduction scope (gitea PASSWD,
      kratos/hydra DSN, helmfile sync, Tilt canonicalization)
- [ ] Prova de Vida passes for every deploy performed during dogfood
- [ ] Prova de Morte passes for every delete performed during dogfood
- [ ] No regressions in previously passing flows (Ginkgo journey 76/76)
- [ ] Observability check passes
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1:
1. Fix ALL S0/S1 findings (não workarounds — root-cause per D5 mindset)
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings documented mas não block plan completion.

---

## Cross-Validation

Mandatory antes de `/dogfood`. Per `.claude/skills/cross-validation`:

```
/cross-validation 2026-05-22-shell-script-strategic
```

Must return APROVADO with:
- Zero CRITICO findings
- Zero GRAVE findings
- Every task DoD checkbox verified
- Every test name verified to exist in code
- Every file path verified to be edited
- ADRs D1-D7 verified in code (e.g., D5 verified by absence of shell-wrap, D3 verified
  by CI gate ATIVO)

REPROVADO em cross-validation bloqueia progression para dogfood.

---

## Anti-Patterns Forbidden

1. **Shell-wrap em Go.** Migrar `script.sh` que invoca `pulumi`/`helm`/`kubectl`/`aws`/`gh`
   para Go que invoca o mesmo via `shellrun.Runner.Run` = PROIBIDO per D5. Use SDK
   nativo OU deixe bash.

2. **Migration sem usage audit.** Toda migração depende de Phase 0 (T0.1) ter rodado e
   o CSV reviewed. Não migrar "no escuro" como o plano anterior.

3. **Creation de novos `.sh` para preencher gap.** Per D3, é PROIBIDO. Se há gap, fix
   o orchestrator (helmfile/Tilt/ArgoCD).

4. **Deletion sem validation.** Phase 1 requer T1.1 validation manual antes de T1.2
   bulk delete. Anti-pattern: trust audit blindly.

5. **Helmfile bypass.** Per D2, bare-helm é PROIBIDO. Se uma task precisa de "helm
   install X", deve ser via helmfile (que tem release X declarado).

6. **`task setup` reinvention.** Per D1, Tilt é canonical. Não criar 3o orchestrator.

7. **Skip cross-validation.** Per global DoD, cross-validation é mandatory. Pular
   cross-validation = plan incompleto.

8. **Reinventar tool externa madura como gate Go interno.** **Antes** de implementar
   qualquer `pkg/quality/X` novo (Phase 6 cria 2; Phase 5 sugere "remaining pure-logic
   gates"), verificar kubetools / CNCF landscape per **Rule 12** (`CLAUDE.md` + [`README.md`](../../README.md)
   § Tool Inventory). Implementar Go gate apenas se:
   - (a) nenhuma tool externa madura cobre o caso (e.g., Pluto cobre deprecated-API;
     Kubescape cobre security posture; OpenCost cobre cost — não reimplementar);
   - (b) OU lógica é pure-logic do **domínio LoopZero** que não tem equivalente externo
     (e.g., `no-new-bash` que parseia allowlist própria, `no-shell-wrap` que audita
     `pkg/*` por padrão `shellrun.Runner.Run`, `prodimagetags` que enforce ADR-0020 D1).

   **Pattern simétrico ao D5 (no shell-wrap em Go):** D5 proíbe Go envolvendo CLI
   externa quando há SDK Go nativo. Anti-pattern #8 proíbe Go reimplementando lógica
   quando há tool externa madura. Em ambos os casos, **adopt > reinvent**.

   **Trade-off documentado:** Tool externa adiciona dependency (binary download em CI
   OU Helm chart no cluster) — aceitável se substitui implementação interna que
   precisaria manutenção. Tool externa abandonada (último release > 2y) = NOT mature
   = OK reimplementar.
