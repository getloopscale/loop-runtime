---
type: plan
title: "Plan: Infra Standardization + Secrets Posture Audit (incl. Infisical evaluation)"
status: deprecated
---

# Plan: Infra Standardization + Secrets Posture Audit (incl. Infisical evaluation)

> **Version 1.2** (2026-05-20) — Edge case review Pass 2 aplicado. 2 MUST
> FIX adicionais: EC-16 (`--env standalone-multi` adicionado às 5 envs
> em T4.2/T4.3/T4.4/T5.1) e EC-17 (T8.0 NEW — mandatory teardown
> pré-T8.1 validation). 5 SHOULD TEST consolidados em TDDs existentes
> (EC-18 helmfile precedence, EC-19 SOPS CI age key, EC-20
> `helm --validate` enforcement, EC-21 task e2e all 5 envs, EC-22
> Pulumi encryption salt preservation). 3 DOCUMENT capturados no Risk
> Register (EC-23 deprecation window, EC-24 multi-fingerprint SOPS,
> EC-25 CHANGELOG hygiene per phase). Severity trend Pass 1→Pass 2: 6→2
> MUST FIX (saturação se aproximando).
>
> **Version 1.1** (2026-05-20) — Edge case review aplicado incorporando 6
> MUST FIX (EC-1 a EC-6): adicionada T1.0 (cross-system path audit), nova
> T2.3 Step 0 (inventário completo localhost), reordenação T2.4/T2.5
> (requiredKeys agora é LAST task da Phase 2), nova T3.2.5 (dry-run
> isolado pré-migration Pulumi), expansão T4.2 Deep Dives (cell→cluster
> mapping table), expansão T4.3 TDD (preservação dos 11 LIVE patches
> Bugs #56-#82). SHOULD TEST items (EC-7..EC-12) consolidados em TDD
> existente. DOCUMENT items (EC-13..EC-15) ficam como notes no Risk
> Register + CHANGELOG entries.
>
> **Version 1.0** — Consolida `./infra` em uma estrutura paramétrica única
> onde drill é um **environment como qualquer outro** (não fork paralelo).
> Elimina 13 scripts `drill-*` duplicados, 4 diretórios chart `*-drill/`,
> 47 conditionals inline em mgmt.helmfile, 9 localhost hardcoded em
> ory-kratos canônico, 22KB monolítico em pulumi-drill, projeto Pulumi
> duplicado (`pulumi-infra` colide com `pulumi`). Adopta best practices
> da indústria (helmfile layered values, Pulumi stack-per-env, SOPS para
> secrets em git). Avalia Infisical e decide ADIAR (D6 = DEFER). Outcome
> esperado: zero false-negative SKIPs em Ginkgo drill, dogfood-drill +
> chaos-test-drill GREEN end-to-end, parity drill↔local↔prod sustentável.
> Estimativa: 8 semanas, ~$30 cloud cost.

## Context

A sessão drill 2026-05-20 produziu 36 Ginkgo FAILs + 117 SKIPs em 342
specs ([`.claude/knowledge-base/reviews/drill-2026-05-20/REPORT.md`](../../reviews/density-audit-2026-05-25/REPORT.md)) e
expôs 3 mecanismos de drift documentados em
`.claude/knowledge-base/strategy/infra-reorganization-2026-05-20.md`:

- **M-1 (parallel implementation):** 13 scripts `drill-*` em
  `infra/scripts/` reimplementam lógica que vive em
  `infra/helmfile/scripts/`. Fix em `seed-local-user.sh` não propaga
  para `drill-mint-jwt.sh`.
- **M-2 (per-env values divergence):** 4 diretórios
  `infra/helm/loopzero-{tenant,app,platform,preview}-operator-drill/`
  + 9 `values-*-drill.yaml.gotmpl` fora dos charts canônicos
  (`charts/`). Sem gate de sincronia.
- **M-3 (hardcoded test scaffolding):** `kubectl --context k3d-loopzero-mgmt`
  hardcoded em 13 sites de 6 arquivos `infra/tests/e2e/`. Fix parcial em
  commit `daf39d69`; lógica ainda hardcoded em `infra/scripts/provision-api-tokens.sh:14-16,208`.

Adicionalmente:
- `infra/helm/ory-kratos/values.yaml` (chart canônico) tem 9 linhas com
  `http://localhost:4455/` (linhas 25,50,75,85,91,95,98,101,53)
- `infra/helmfile/mgmt.helmfile.yaml.gotmpl` tem 47 `{{ if eq .Environment.Name "drill" }}`
- 5 projetos Pulumi com 2 colidindo no nome `loopzero-infra`
  (`infra/pulumi/` + `infra/pulumi-infra/`)
- `infra/pulumi-drill/index.ts` é 22KB monolítico (vs 9KB modular em
  `infra/pulumi/`)
- `infra/pulumi-shared/` vazio (dead dir)
- Velero BSL Unavailable no drill: bucket `loopzero-drill-velero-backups`
  retorna 404 NoSuchBucket porque `infra/pulumi-drill/` NÃO o cria
  (`finding-2026-05-20-velero-bsl-schedules-drill-gap.md`)

Industry research consolidada em
`.claude/knowledge-base/strategy/infra-reorganization-2026-05-20.md`
Section 4. Memórias binding aplicáveis: `feedback_velero_must_be_in_drill`,
`feedback_anti_lying_workaround_pattern`,
`feedback_golden_rules_cto_mandate` (Rule #0 — ambiente local fidedigno
ao prod).

## Objective

**Done = `infra/` paramétrico, sem fork drill, com secrets posture
auditada.**

Specific measurable goals:
1. ZERO scripts `infra/scripts/drill-*` remanescentes (13 → 0)
2. ZERO diretórios `infra/helm/loopzero-*-operator-drill/` (4 → 0)
3. ZERO `{{ if eq .Environment.Name "drill" }}` em `infra/helmfile/*.gotmpl` (47 → 0)
4. ZERO `http://localhost:` em `infra/helm/*/values.yaml` canônicos (9 → 0)
5. UM projeto Pulumi `infra/pulumi/` com stacks `dev`/`drill`/`prod`/`standalone`
   (4 projetos hoje → 1, mais `pulumi-platform/` opcional)
6. Ginkgo full no drill: 342 executed, ≥320 PASS, zero false-negative SKIPs
7. Dogfood-drill 11/11 journeys GREEN
8. Chaos-test-drill 10/10 scenarios GREEN
9. Decisão formal documentada sobre Infisical (D6)
10. SOPS adotado para secrets em git (ADR-INFRA-SOPS)

## ADRs

### D1 — Drill é stack do Pulumi `infra/pulumi/`, não projeto separado
**Decision:** Consolidar `infra/pulumi-drill/` em
`infra/pulumi/Pulumi.drill.yaml` (stack-level config). Component
Resources reusáveis em `infra/pulumi/src/components/`.

**Rationale:** Pulumi docs recommendam stack-per-env, não
project-per-env ([pulumi.com/docs/iac/guides/basics/organizing-projects-stacks](https://www.pulumi.com/docs/iac/guides/basics/organizing-projects-stacks/)).
Project-per-env duplica state, components, providers, contracts. A
sessão 2026-05-20 mostrou que `pulumi-drill/index.ts` monolítico (22KB)
diverge architecturally de `pulumi/index.ts` (9KB, componentizado).

**Consequences:** Permite ÚNICA fonte de verdade para componentes
(`LoopZeroMgmtCluster`, `LoopZeroBuildCluster`, etc.). Requer state migration
(Phase 3). Resolve colisão de nome `loopzero-infra` (D7).

### D2 — Helmfile usa layered values + placeholder pattern
**Decision:** Eliminar 47 inline `{{ if eq .Environment.Name "drill" }}`
substituindo por `infra/helmfile/values/<env>/<chart>.yaml`. Charts
canônicos (`charts/*/values.yaml`) usam placeholders (e.g.,
`{{ .Values.uiBaseUrl }}/login` em vez de `http://localhost:4455/login`).
Habilitar `requiredKeys: true` para fail-fast.

**Rationale:** Helmfile docs ([helmfile.readthedocs.io](https://helmfile.readthedocs.io/en/latest/writing-helmfile/))
recomendam layered values quando há >3 envs. LoopZero tem 5 envs (dev,
drill, prod, standalone, standalone-multi). 47 conditionals é code-smell
explícito documentado em [oneuptime](https://oneuptime.com/blog/post/2026-01-30-helmfile-layering/view).
Placeholders + fail-fast catch drift em template phase (cheap) em vez de
sync phase (expensive).

**Consequences:** Forças TODOS os envs a definir overrides explicitamente.
Quebra silent inheritance de localhost para drill (que estava
silenciosamente fallback para localhost antes do override drill).
Habilita golden-file regression tests (Phase 2 validation gate).

### D3 — Scripts parametrizados por `--env`, single canonical location
**Decision:** Deletar 13 scripts `drill-*`. Mover lógica para
`infra/helmfile/scripts/` (canonical) com flag `--env <name>` resolvendo
kubeconfig, contexto, registry prefix, cell topology.

**Rationale:** Memória `feedback_no_tribal_knowledge` (verbatim
"Voce deve sermpre corrigir nivel FAANG"). Fork de scripts é o anti-pattern
mais grave de drift — fix local não vai para drill, fix drill não vai
para local. Resolução: paramétrico, um arquivo, um teste.

**Consequences:** Mudanças em bootstrap propagam automaticamente para
todos os envs. `task setup` e `task drill:l2-scale` consomem o MESMO
script. Quebra qualquer hook/workflow que dependa do nome `drill-*` —
mapeamento em Phase 4 documentado.

### D4 — `ory-kratos/values.yaml` (canonical) usa placeholders
**Decision:** Substituir 9 linhas com `http://localhost:4455/` por
`{{ .Values.uiBaseUrl }}/...`. Cada env define `uiBaseUrl` em
`infra/helmfile/values/<env>/ory-kratos.yaml`:
- dev/standalone/standalone-multi: `http://localhost:4455`
- drill: `https://app.loopzero-drill.example`
- prod: `https://app.loopzero.example`

**Rationale:** Helmfile best practice — base values têm SETTINGS COMUNS;
hostnames são env-specific. Hardcode localhost em base é o source da
crise local↔drill (Bug #18 da sessão drill 2026-05-17).

**Consequences:** Quebra qualquer chart deployment SEM override de
`uiBaseUrl`. Mitigation: `values.schema.json` com `uiBaseUrl` como
required field; helm template falha fast.

### D5 — Test scaffolding parametrizado via `LOOPZERO_TEST_ENV` env var
**Decision:** Generalizar `cluster_kubectl_helpers_test.go` (commit
`daf39d69`) para ler `LOOPZERO_TEST_ENV` em vez de `DRILL_KUBECONFIG_DIR`.
Valores: `k3d` (default), `drill`, `prod`. Cada valor resolve um
kubectl helper específico (file-per-env, Open/Closed).

**Rationale:** A solução atual é drill-specific (env var
`DRILL_KUBECONFIG_DIR`). Adicionar prod no futuro requer novo env var
+ helper. Generalizar AGORA evita re-engineering em 6 meses.

**Consequences:** Test helpers ficam future-proof (adicionar prod = 1
nova função, sem editar callers). Permite hybrid testing (run local
test binary contra cluster cloud via Gefyra-style patterns).

### D6 — REJEITAR Infisical agora, ADOTAR SOPS para secrets em git
**Decision:**
- Manter External Secrets Operator 0.14.3 como secret sync canônico
- Adotar SOPS + age para criptografar secrets committados em git
  (cosign keys, ESO ClusterSecretStore configs)
- Não adotar Infisical neste plano
- Re-avaliar Infisical em 6 meses pós-GA (Sprint 12+) quando houver
  customer-managed envs

**Rationale:** LoopZero já tem:
- ESO 0.14.3 deployado e funcional
- Ory Kratos/Hydra para auth tokens (concern diferente)
- Cosign keys em-pipeline (build cluster only)
- Pulumi secret + Kubernetes Secret pattern

Infisical adicionaria: 1 service stateful (PostgreSQL + Redis próprios),
operational overhead, training curve. Wins documentados:
- Auto-redeploy on secret change (Infisical operator) — LoopZero já tem
  via ArgoCD sync + helmfile diff
- Dev UX para self-service — LoopZero é pré-release, dev count baixo
- MIT license — ESO também é MIT (Apache 2.0)

Vault tem capacidades superiores (dynamic secrets, PKI, transit
encryption) mas operational cost ainda maior. LoopZero não precisa de
dynamic secrets agora.

SOPS preenche o GAP REAL: secrets-em-git seguros. Atualmente
`infra/scripts/drill-helmfile-bootstrap.sh:732` grava
`$SERVICE_AUTH_SECRET > /tmp/drill-service-auth-secret.txt` —
world-readable. SOPS encrypts em git, descripta no apply.

**Consequences:**
- ZERO additional moving parts (SOPS é CLI, age é lib)
- Compatível com ESO via `helm-secrets` plugin
- Re-evaluation trigger: customer-managed envs OR compliance audit
  request OR ≥3 incidents de secret mishandling

References:
- [Top 5 Secrets Management Tools 2026](https://guptadeepak.com/top-5-secrets-management-tools-hashicorp-vault-aws-doppler-infisical-and-azure-key-vault-compared/)
- [Infisical vs Vault](https://infisical.com/blog/hashicorp-vault-alternatives)
- [Kubernetes Secrets Management 2026](https://infisical.com/blog/kubernetes-secrets-management)

### D7 — Pulumi state migration via export/import (NOT re-create)
**Decision:** Phase 3 migra state do `infra/pulumi-drill/` para
stack `drill` em `infra/pulumi/` via:
1. `pulumi stack export --stack drill > /tmp/drill-state.json`
2. Editar JSON: renomear project `loopzero-drill` → `loopzero-infra`, ajustar
   URNs
3. `pulumi stack init drill` em `infra/pulumi/`
4. `pulumi stack import < /tmp/drill-state.json`
5. `pulumi preview --stack drill` deve mostrar ZERO changes

**Rationale:** Re-create (delete + create) destroi recursos (DBs,
clusters) custando provisioning time + dados em flight. Pulumi state
import preserva resource URN bindings sem touch nos recursos físicos.

**Consequences:** State migration tem risco MEDIUM (URN edit é manual).
Mitigation: backup state pre-migration; dry-run em stack `drill-test`;
sponsor sign-off pre-migration.

### D8 — Backward compatibility: pre-release, allowed to break
**Decision:** Per CLAUDE.md "Pre-release governance" — backward
compatibility NÃO é required. Renomear scripts/paths/configs livremente.
Operator workflow updates documentados em CHANGELOG.

**Rationale:** LoopZero é pre-release. Custo de manter symlinks /
shims / aliases > custo de operator re-learning. Memória
`feedback_no_legacy_after_local_delete` aplica.

**Consequences:** Operadores precisam re-aprender comandos (e.g.,
`task drill:bootstrap` em vez de `bash drill-helmfile-bootstrap.sh`).
Mitigation: Phase 0 + 4 documentam mapping completo old→new.

### D9 — Migration sequence: Helm → Helmfile → Pulumi → Scripts → Tests
**Decision:** Ordem específica: Phase 1 (Helm consolidation) antes de
Phase 2 (Helmfile layered values) antes de Phase 3 (Pulumi unify) antes
de Phase 4 (Scripts dedup) antes de Phase 5 (Test param).

**Rationale:** Cada fase prepara fundação para a próxima:
- Phase 1 (Helm) é cosmetic — move files, mantém logic. Lowest risk.
- Phase 2 (Helmfile) consome Phase 1 (values consolidated)
- Phase 3 (Pulumi) consome Phase 2 (helmfile drill env stable)
- Phase 4 (Scripts) consome Phase 3 (Pulumi outputs stable)
- Phase 5 (Tests) consome Phase 4 (canonical script names)

**Consequences:** ZERO paralelismo entre Phase 1-5. Cada fase tem
validation gate antes da próxima. Adds 2 weeks vs paralelo, mas risk
goes from HIGH para LOW.

## Dependency Graph (v1.1 — post edge case review)

```
Phase 0 (Governance + ADRs)
    │
    ▼
T1.0 (NEW per EC-2 — cross-system path audit)
    │
    ▼
Phase 1 (Helm chart consolidation: T1.1 → T1.2 → T1.3 → T1.4 → T1.5) ───┐
    │                                                                    │
    ▼                                                                    │
Phase 2 (Helmfile layered values: T2.1 → T2.2 → T2.3 → T2.4 → T2.5)     │
       (EC-6: T2.4 regression FIRST, T2.5 requiredKeys LAST)             │
    │                                                                    │
    ▼                                                                    │ Phase 6 (Velero gap closure)
T3.1 (Componentize Pulumi)                                               │ runs parallel ANY time after Phase 0
    │                                                                    │
    ▼                                                                    │
T3.2.5 (NEW per EC-1 — dry-run on isolated drill-test stack)             │
    │                                                                    │
    ▼                                                                    │
T3.2 (REAL state migration — requires T3.2.5 GREEN)                      │
    │                                                                    │
    ▼                                                                    │
T3.3 → T3.4 (Pulumi unify complete)                                      │
    │                                                                    │
    ▼                                                                    │
Phase 4 (Scripts deduplication: T4.1 → T4.2 → T4.3 → T4.4 → T4.5) ◀──────┘
    │
    ▼
Phase 5 (Test scaffolding param: T5.1 → T5.2 → T5.3)
    │
    ▼
Phase 7 (Secrets posture + SOPS) ◀── may run parallel to Phase 5
    │
    ▼
Phase 8 (Final validation: Ginkgo + dogfood-drill + chaos-test-drill)
```

**EC-6 ordering inside Phase 2:** T2.4 (holistic regression) executes
BEFORE T2.5 (requiredKeys fail-fast). T2.5 has explicit prereq check on
T2.4 acceptance.

**EC-1 ordering inside Phase 3:** T3.2.5 (dry-run on isolated test stack)
executes BEFORE T3.2 (real state migration). T3.2 has explicit prereq
check on T3.2.5 GREEN.

**EC-17 ordering inside Phase 8:** T8.0 (mandatory teardown +
zero-residuals gate) executes BEFORE T8.1 (Ginkgo full). T8.1 → T8.2 →
T8.3 sequential. Without T8.0, Phase 8 validates against polluted
post-migration state.

Phases 0-5 are sequential. Phase 6 (Velero) and Phase 7 (Secrets/SOPS)
can run in parallel with later phases once Phase 0 ratifies their ADRs.
Phase 8 is the gate before plan completion.

---

## Phase 0: Governance + ADRs Ratification

**Objective:** Sponsor sign-off em 9 ADRs antes de qualquer mudança de código.

### T0.1 — Ratify ADRs D1-D9 via formal meeting

#### Objective
Sponsor revisa as 9 ADRs deste plano e ratifica explicitamente cada
decisão (ou requisita amendments).

#### Evidence
Per CLAUDE.md "Pre-release governance" — T2/T3 changes require
explicit sponsor review. Esta mudança toca 5 áreas (Helm, Helmfile,
Pulumi, Scripts, Tests) + secrets posture (Infisical decision) = T3 nível.

#### Files to edit
```
.claude/knowledge-base/meetings/MEET-2026-05-20-infra-standardization.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D1-pulumi-stack-per-env.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D2-helmfile-layered-values.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D3-scripts-param-by-env.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D4-kratos-placeholders.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D5-test-env-var.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D6-infisical-defer-sops-adopt.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D7-pulumi-state-migration.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D8-pre-release-bc-break.md (NEW)
.claude/knowledge-base/decisions/2026-05-20-D9-migration-sequence.md (NEW)
.claude/knowledge-base/index.md — append 10 ADR entries
```

#### Deep file dependency analysis
ADRs são leaf nodes (não dependem de código). Meeting record cita ADRs.
`index.md` indexa tudo.

#### Deep Dives
- **MEET record structure:** per `MEET-ASYNC-AMENDMENT-2026-05-16-002` pattern
  (sponsor synthesis + alternatives rejected + ratify timestamp)
- **ADR ID convention:** `2026-05-20-D{N}-<slug>.md` matching
  conversation memory pattern

#### Tasks
1. Draft MEET-2026-05-20-infra-standardization.md com 9 ADRs sumarizados
2. Draft 9 ADR files (one per decision)
3. Update `.claude/knowledge-base/index.md` com 10 entries
4. Sponsor review + ratification (out-of-band)
5. Commit ratified docs com tag `ADR-ratified-2026-05-XX`

#### TDD
```
RED:     test_meet_record_lists_all_9_ADRs() — grep MEET file for D1..D9
RED:     test_each_ADR_has_decision_rationale_consequences() — frontmatter check
RED:     test_index_md_has_10_new_entries() — count diff before/after
GREEN:   Write MEET + 9 ADR files + index update
REFACTOR: None expected
VERIFY:  bash infra/scripts/check-adr-completeness.sh (NEW hermetic)
```

#### Acceptance Criteria
- [ ] MEET-2026-05-20-infra-standardization.md exists
- [ ] 9 ADR files exist with proper YAML frontmatter (type, created_at, updated_at)
- [ ] `index.md` has new entries for MEET + 9 ADRs
- [ ] Sponsor signoff recorded (explicit confirmation text in MEET)
- [ ] Pass: hermetic test `infra/tests/check-adr-completeness_test.sh` GREEN
- [ ] /code-audit lint check (markdownlint) zero warnings

#### DoD
- [ ] All 9 ADRs ratified or amended
- [ ] Tests passing
- [ ] No "TBD" placeholders in MEET or ADRs
- [ ] Linked from `index.md` and `log.md`

---

## Phase 1: Helm Chart Consolidation

**Objective:** Eliminar 4 diretórios `loopzero-*-operator-drill/` + 5
arquivos `values-*-drill.yaml.gotmpl` espalhados. Tudo em
`infra/helmfile/values/drill/`.

### T1.0 — Audit ALL cross-system refs to old Helm chart paths (EC-2)

#### Objective
ANTES de mover qualquer arquivo (T1.1-T1.4), inventariar TODA referência
externa a `infra/helm/loopzero-*-operator-drill/` e `values-*-drill.yaml.gotmpl`:
ArgoCD ApplicationSets, CI workflows não listados explicitamente, Taskfile,
docs, scripts auxiliares.

#### Evidence
Edge case EC-2: o plano cobria `mgmt.helmfile.yaml.gotmpl` mas NÃO auditava
outros consumers. Past pattern (memória `feedback_no_tribal_knowledge`):
CI quebra silenciosa por path stale = horas de debug + drift.

#### Files to edit
```
docs/plans/2026-05-20-infra-cross-system-refs-inventory.md (NEW)
```

#### Deep file dependency analysis
Esta task é PURE READ — audit only. Output: inventory + mapping
old→new para cada ref encontrada. Inputs para T1.1-T1.4.

#### Tasks
1. `grep -rnE "values-(kratos|hydra|gitea|prometheus|loopzero-api)-drill|loopzero-(tenant|app|platform|preview)-operator-drill" .github/ infra/argocd-bootstrap/ infra/manifests/ Taskfile.yml taskfiles/ infra/helmfile/scripts/ infra/scripts/ docs/ README.md`
2. Classify each match: must-update (will break) vs cosmetic (comment/doc)
3. Produce inventory table: file:line → old path → new path
4. Sponsor review

#### TDD
```
RED:     test_inventory_no_blind_spots() — re-run grep, confirm 0 matches not in inventory
RED:     test_inventory_includes_argocd_applicationsets()
RED:     test_inventory_includes_taskfile_refs()
GREEN:   Generate inventory
REFACTOR: Group by file type
VERIFY:  bash infra/tests/check-cross-system-refs-inventory_test.sh
```

#### Acceptance Criteria
- [ ] Inventory file produced + reviewed
- [ ] grep audit on `infra/helm/loopzero-*-operator-drill` returns matches accounted for in inventory
- [ ] grep audit on `values-*-drill.yaml.gotmpl` same
- [ ] Sponsor sign-off
- [ ] Pass: /code-audit lint (markdown)

#### DoD
- [ ] Inventory complete
- [ ] All refs mapped
- [ ] Sponsor approved
- [ ] T1.1-T1.4 can proceed with full visibility

### T1.1 — Migrate 4 operator drill values to layered location

#### Objective
Move `infra/helm/loopzero-{tenant,app,platform,preview}-operator-drill/values-drill.yaml.gotmpl`
to `infra/helmfile/values/drill/loopzero-{name}-operator.yaml`.

#### Evidence
Section 2 Evidence #2 do deep research: 4 diretórios sem chart, só values.
Drift garantido vs `charts/loopzero-{name}-operator/`.

#### Files to edit
```
infra/helmfile/values/drill/loopzero-tenant-operator.yaml (NEW, moved from drill dir)
infra/helmfile/values/drill/loopzero-app-operator.yaml (NEW)
infra/helmfile/values/drill/loopzero-platform-operator.yaml (NEW)
infra/helmfile/values/drill/loopzero-preview-operator.yaml (NEW)
infra/helm/loopzero-tenant-operator-drill/ (DELETE entire dir)
infra/helm/loopzero-app-operator-drill/ (DELETE)
infra/helm/loopzero-platform-operator-drill/ (DELETE)
infra/helm/loopzero-preview-operator-drill/ (DELETE)
infra/helmfile/mgmt.helmfile.yaml.gotmpl — update values: paths to new location
```

#### Deep file dependency analysis
- `mgmt.helmfile.yaml.gotmpl` references old paths em release values: blocks
- Bootstrap scripts (`drill-helmfile-bootstrap.sh`) reference helmfile via
  `helmfile -e drill sync` — paths refresh automatic
- ArgoCD ApplicationSets pulling from `charts/` (canonical) — unaffected
- CI workflow `cell2-soaks.yml` invokes `helmfile sync` — unaffected

#### Deep Dives
- Values files are loaded in HELMFILE values: block by env condition
- Need to preserve `.gotmpl` extension if values use go templating (e.g.,
  reading from Pulumi outputs)
- Validate via `helmfile -e drill template -l name=loopzero-tenant-operator`
  before/after diff

#### Tasks
1. Create `infra/helmfile/values/drill/` directory structure
2. Move 4 values files (with `.gotmpl` if templated, else `.yaml`)
3. Update `mgmt.helmfile.yaml.gotmpl` values: paths
4. Delete 4 old `loopzero-*-operator-drill/` directories
5. Validate golden-file diff = empty for env=drill template output

#### TDD
```
RED:     test_no_legacy_drill_operator_dirs() — find infra/helm/*-operator-drill returns empty
RED:     test_helmfile_drill_template_unchanged() — golden file diff before/after
RED:     test_helmfile_other_envs_unchanged() — same for dev, prod, standalone, standalone-multi
GREEN:   Move values, update mgmt.helmfile paths, delete old dirs
REFACTOR: None expected (move + delete, no logic change)
VERIFY:  bash infra/tests/check-helm-chart-consolidation_test.sh
```

#### Acceptance Criteria
- [ ] 0 `loopzero-*-operator-drill` directories (was 4)
- [ ] 4 files in `infra/helmfile/values/drill/loopzero-*-operator.yaml`
- [ ] `helmfile -e drill template -l name=loopzero-tenant-operator` output identical pre/post (golden diff)
- [ ] Same for `loopzero-app-operator`, `loopzero-platform-operator`, `loopzero-preview-operator`
- [ ] Same for 4 other envs (dev, prod, standalone, standalone-multi) — no regressions
- [ ] Pass: /code-audit complexity (helmfile/values paths shouldn't change)
- [ ] Pass: /code-audit lint (kubeconform --strict on rendered manifests)
- [ ] Pass: /code-audit size (no file >500 lines)

#### DoD
- [ ] Old paths gone
- [ ] New paths consumed by helmfile
- [ ] Golden file regression suite GREEN
- [ ] CHANGELOG entry under [Unreleased]

### T1.2 — Migrate loopzero-api drill values

#### Objective
Move `infra/helm/loopzero-api/values-loopzero-api-drill.yaml.gotmpl` to
`infra/helmfile/values/drill/loopzero-api.yaml.gotmpl`.

#### Evidence
Same M-2 mechanism: values file co-located in `infra/helm/loopzero-api/`
breaks pattern (other env values like `values-prod.yaml` are also there,
so this is borderline OK — but consistency with operators in T1.1 wins).

#### Files to edit
```
infra/helmfile/values/drill/loopzero-api.yaml.gotmpl (NEW)
infra/helm/loopzero-api/values-loopzero-api-drill.yaml.gotmpl (DELETE)
infra/helmfile/mgmt.helmfile.yaml.gotmpl — update loopzero-api release values: path
```

#### Deep file dependency analysis
- mgmt.helmfile references this file in loopzero-api release
- CI workflows do NOT reference directly (helmfile abstracts)
- Tilt local dev unaffected (uses local k3d, not drill values)

#### Deep Dives
- File uses `.gotmpl` because it reads Pulumi outputs (JWT_ISSUER,
  kubeContexts). Preserve `.gotmpl` extension
- Move + update path in mgmt.helmfile — atomic commit

#### Tasks
1. Move file to new location
2. Update mgmt.helmfile.yaml.gotmpl
3. Validate `helmfile -e drill template -l name=loopzero-api` unchanged

#### TDD
```
RED:     test_loopzero_api_drill_values_in_new_location() — file exists at new path
RED:     test_loopzero_api_drill_values_not_in_old_location() — file absent at old path
RED:     test_loopzero_api_drill_template_unchanged() — golden file diff = 0
GREEN:   Move + path update
REFACTOR: None
VERIFY:  bash infra/tests/check-loopzero-api-drill-values_test.sh
```

#### Acceptance Criteria
- [ ] New path: `infra/helmfile/values/drill/loopzero-api.yaml.gotmpl`
- [ ] Old path: absent
- [ ] Golden diff = 0 for loopzero-api template under env=drill
- [ ] No regression in other envs
- [ ] Pass: kubeconform --strict on rendered manifests

#### DoD
- [ ] File moved
- [ ] mgmt.helmfile updated
- [ ] Tests GREEN

### T1.3 — Migrate ory-{kratos,hydra} drill values

#### Objective
Move `infra/helm/ory-kratos/values-kratos-drill.yaml.gotmpl` and
`infra/helm/ory-hydra/values-hydra-drill.yaml.gotmpl` to
`infra/helmfile/values/drill/`.

#### Evidence
Same as T1.2. ory-hydra-drill specifically encodes the issuer FQDN fix
from this session — critical to preserve verbatim.

#### Files to edit
```
infra/helmfile/values/drill/ory-kratos.yaml.gotmpl (NEW)
infra/helmfile/values/drill/ory-hydra.yaml.gotmpl (NEW)
infra/helm/ory-kratos/values-kratos-drill.yaml.gotmpl (DELETE)
infra/helm/ory-hydra/values-hydra-drill.yaml.gotmpl (DELETE)
infra/helmfile/mgmt.helmfile.yaml.gotmpl — update kratos + hydra release paths
```

#### Deep file dependency analysis
- Hydra drill values pin `issuer: http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/`
  (the fix from session 2026-05-20). Critical to preserve.
- Kratos drill values pin DOKS-specific URLs

#### Tasks
1. Move 2 files
2. Update mgmt.helmfile paths
3. Validate templates unchanged

#### TDD
```
RED:     test_kratos_drill_in_new_loc() / test_hydra_drill_in_new_loc()
RED:     test_hydra_issuer_preserved() — grep new file for "ory-hydra-public.loopzero-auth.svc.cluster.local:4444"
RED:     test_kratos_drill_template_diff_zero()
GREEN:   Move files
REFACTOR: None
VERIFY:  bash infra/tests/check-ory-drill-values_test.sh
```

#### Acceptance Criteria
- [ ] Both files moved
- [ ] Hydra issuer FQDN preserved (regression of session fix `commit ory-hydra-issuer`)
- [ ] Golden diff = 0 for both charts under env=drill
- [ ] Pass: kubeconform --strict

#### DoD
- [ ] Files moved
- [ ] Issuer fix preserved
- [ ] Tests GREEN

### T1.4 — Migrate gitea + monitoring drill values

#### Objective
Move `infra/helm/gitea/values-gitea-drill.yaml.gotmpl` and
`infra/helm/monitoring/values-prometheus-drill.yaml`.

#### Evidence
Same M-2 pattern. Completing the 9 drill values files migration.

#### Files to edit
```
infra/helmfile/values/drill/gitea.yaml.gotmpl (NEW)
infra/helmfile/values/drill/prometheus.yaml (NEW)
infra/helm/gitea/values-gitea-drill.yaml.gotmpl (DELETE)
infra/helm/monitoring/values-prometheus-drill.yaml (DELETE)
infra/helmfile/mgmt.helmfile.yaml.gotmpl — 2 path updates
```

#### Deep file dependency analysis
- Gitea drill values pin managed PG connection (Phase 2 plan T3.3 fix)
- Prometheus drill values disable alertmanager (no Slack webhook)
- Both files critical fixes from prior sessions — preserve verbatim

#### Tasks
1. Move 2 files
2. Update mgmt.helmfile
3. Validate templates unchanged

#### TDD
```
RED:     test_gitea_drill_in_new_loc()
RED:     test_prometheus_drill_in_new_loc()
RED:     test_gitea_managed_pg_preserved()
RED:     test_prometheus_alertmanager_disabled_preserved()
GREEN:   Move files
REFACTOR: None
VERIFY:  bash infra/tests/check-gitea-prometheus-drill_test.sh
```

#### Acceptance Criteria
- [ ] Both files moved
- [ ] Critical configs preserved (managed PG, alertmanager off)
- [ ] Golden diff = 0
- [ ] Pass: kubeconform --strict

#### DoD
- [ ] Files moved
- [ ] Tests GREEN

### T1.5 — Validate Phase 1 holistic regression

#### Objective
Run full helmfile template for all 5 envs, snapshot, diff against pre-Phase-1
golden snapshot.

#### Evidence
Phase 1 touches 9 values files + mgmt.helmfile. Holistic check ensures
nothing else regressed.

#### Files to edit
```
infra/tests/snapshots/helmfile-{dev,drill,prod,standalone,standalone-multi}-template.yaml (NEW or UPDATED)
infra/tests/check-helmfile-regression_test.sh (NEW)
```

#### Deep file dependency analysis
Snapshots are tracked files. Test compares current vs snapshot.

#### Tasks
1. Pre-Phase-1: capture golden snapshots in all 5 envs
2. Apply T1.1-T1.4 changes
3. Post-Phase-1: re-capture snapshots
4. `diff snapshot-pre snapshot-post` must equal empty
5. If diff non-empty: investigate, fix, repeat

#### TDD
```
RED:     test_drill_template_matches_snapshot()
RED:     test_dev_template_matches_snapshot()
RED:     test_prod_template_matches_snapshot()
RED:     test_standalone_template_matches_snapshot()
RED:     test_standalone_multi_template_matches_snapshot()
GREEN:   Adjust paths until ALL 5 diff = 0
REFACTOR: Consolidate golden file capture in single script
VERIFY:  bash infra/tests/check-helmfile-regression_test.sh
```

#### Acceptance Criteria
- [ ] All 5 envs: snapshot diff = 0
- [ ] Pass: /code-audit lint
- [ ] Pass: kubeconform --strict on every env

#### DoD
- [ ] Phase 1 complete
- [ ] Holistic regression GREEN
- [ ] Snapshot test committed

---

## Phase 2: Helmfile Layered Values + Placeholder Pattern

**Objective:** Eliminar 47 inline `{{ if eq .Environment.Name "drill" }}` +
9 localhost em ory-kratos canonical. Adicionar `values.schema.json` para fail-fast.

### T2.1 — Inventory all 47 inline drill conditionals

#### Objective
Documentar TODOS os 47 conditionals com line:column + classify
(release toggle, values path, set value, needs dependency).

#### Evidence
Section 2 Evidence #5: 47 references documented.

#### Files to edit
```
docs/plans/2026-05-20-infra-standardization-inventory.md (NEW)
```

#### Tasks
1. `grep -n "Environment.Name.*drill\|drill.*Environment.Name" infra/helmfile/mgmt.helmfile.yaml.gotmpl > inventory.txt`
2. Annotate each match: release name + conditional type
3. Map each to target layered values file
4. Sponsor review (verify no critical conditionals missed)

#### TDD
```
RED:     test_inventory_has_47_entries() — line count check
RED:     test_inventory_maps_to_layered_file() — every entry has target
GREEN:   Generate + classify
REFACTOR: Group by chart
VERIFY:  bash infra/tests/check-inventory-completeness_test.sh
```

#### Acceptance Criteria
- [ ] inventory.md has 47 entries
- [ ] Each entry has target layered values file
- [ ] Sponsor sign-off

#### DoD
- [ ] Inventory complete
- [ ] Mapping reviewed

### T2.2 — Convert 47 conditionals to layered values (batch by chart)

#### Objective
For each chart with drill conditional, create
`infra/helmfile/values/drill/<chart>.yaml` capturing the drill-specific
config. Remove the `{{ if eq .Environment.Name "drill" }}` from
mgmt.helmfile.

#### Evidence
Industry best practice: layered values over inline conditionals.

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — remove 47 conditionals
infra/helmfile/values/drill/<chart>.yaml (NEW per chart) — ~10-15 charts affected
infra/helmfile/values/dev/<chart>.yaml (NEW where conditional was for dev)
infra/helmfile/values/standalone/<chart>.yaml (NEW where applicable)
```

#### Deep file dependency analysis
mgmt.helmfile becomes shorter (22KB → ~14KB estimated). New values files
contain ONLY the env-specific overrides.

#### Deep Dives
- Pattern: chart needs different `set:` per env → move to per-env values
- Pattern: chart toggles install per env → use `installed: {{ ne .Environment.Name "drill" }}`
  pattern OR conditional `needs:` instead
- Some conditionals are LEGITIMATE (e.g., `installed: false` for charts
  not used in env) — those remain but should use `requires:` cleaner pattern

#### Tasks
1. Group inventory by chart
2. For each chart: create layered values file
3. Delete the inline conditional from mgmt.helmfile
4. Snapshot test diff = 0 for ALL 5 envs after each chart
5. Repeat for ~10-15 charts

#### TDD
```
RED:     test_zero_inline_drill_conditionals() — grep mgmt.helmfile, expect 0 matches
RED:     test_each_env_template_unchanged() — 5 snapshot diffs = 0
GREEN:   Per-chart migration
REFACTOR: Clean up redundant values files
VERIFY:  bash infra/tests/check-helmfile-no-inline-conditionals_test.sh
```

#### Acceptance Criteria
- [ ] `grep -c "if eq .Environment.Name \"drill\"" infra/helmfile/mgmt.helmfile.yaml.gotmpl` = 0
- [ ] Same for build.helmfile, runtime.helmfile
- [ ] All 5 env snapshots diff = 0
- [ ] Pass: kubeconform --strict
- [ ] Pass: /code-audit complexity (mgmt.helmfile complexity drops)

#### DoD
- [ ] Zero inline drill conditionals in any .gotmpl
- [ ] Layered values consume ALL drill-specific config
- [ ] Snapshot regression GREEN
- [ ] Tests passing

### T2.3 — Replace ory-kratos localhost hardcodes with placeholders

#### Objective
9 lines in `infra/helm/ory-kratos/values.yaml` from `http://localhost:4455/...`
to `{{ .Values.uiBaseUrl }}/...`. Move per-env values to layered files.

#### Evidence
Section 2 Evidence #4: 9 hardcoded localhost lines in canonical chart values.

#### Files to edit
```
infra/helm/ory-kratos/values.yaml — 9 replacements with {{ .Values.uiBaseUrl }}
infra/helm/ory-kratos/values.schema.json (NEW) — uiBaseUrl required
infra/helmfile/values/dev/ory-kratos.yaml — uiBaseUrl: http://localhost:4455
infra/helmfile/values/drill/ory-kratos.yaml.gotmpl — uiBaseUrl: https://app.loopzero-drill.example
infra/helmfile/values/prod/ory-kratos.yaml — uiBaseUrl: https://app.loopzero.example
infra/helmfile/values/standalone/ory-kratos.yaml — uiBaseUrl: http://localhost:4455
infra/helmfile/values/standalone-multi/ory-kratos.yaml — uiBaseUrl: http://localhost:4455
```

#### Deep file dependency analysis
- Schema enforces `uiBaseUrl` as required → helm template fails if env doesn't provide
- All 5 envs MUST provide value
- Existing drill issuer FQDN preserved in T1.3

#### Deep Dives
- 9 lines: `default_browser_return_url`, `base_redirect_uri`, 5x
  `ui_url` (registration, login, verification, recovery, settings),
  `error` ui_url, line 53 (extra context)
- Template variable: `{{ .Values.uiBaseUrl }}` resolves to env-specific
  value
- values.schema.json includes:
  ```json
  {
    "type": "object",
    "required": ["uiBaseUrl"],
    "properties": {
      "uiBaseUrl": {"type": "string", "pattern": "^https?://[^/]+$"}
    }
  }
  ```

#### Tasks
0. **EC-5 mitigation — global inventory:** `grep -rnE "http://localhost:(4455|3000|3002|4444|13001)" infra/helm/` para listar TODOS os hardcodes (não apenas kratos). Classify cada um:
   - Common base URL (`uiBaseUrl`) — used by kratos + loopzero-cloud + dashboard + notifications
   - Per-service URL (e.g., hydra issuer `http://localhost:4444/`) — handled separately per chart
   - Documentation/comment only — leave alone
   Produce mapping table: chart → hardcoded URL → target placeholder.
1. Add `uiBaseUrl` parameter to canonical kratos values.yaml + add per-service placeholders to other charts identified in step 0
2. Replace 9 hardcoded URLs in kratos with `{{ .Values.uiBaseUrl }}/<path>` + similar for other charts found
3. Add `values.schema.json` to kratos (and other charts that get new required fields)
4. Add per-env layered values in `infra/helmfile/values/{dev,drill,prod,standalone,standalone-multi}/<chart>.yaml`
5. Validate helm template fails if any newly-required field missing
6. Validate `uiBaseUrl` (and similar) schema regex: `^https?://[^/]+$` (no trailing slash — EC-8)

#### TDD
```
RED:     test_no_localhost_in_canonical_kratos_values() — grep ory-kratos/values.yaml
RED:     test_inventory_step0_covers_all_charts() — for each chart in inventory, has placeholder
RED:     test_schema_requires_uiBaseUrl() — schema.json has "required": ["uiBaseUrl"]
RED:     test_template_fails_without_uiBaseUrl() — helm template -f empty.yaml fails
RED:     test_drill_uiBaseUrl_correct() — value resolves to loopzero-drill.example
RED:     test_prod_uiBaseUrl_correct() — resolves to loopzero.example
RED:     test_uiBaseUrl_no_trailing_slash() — EC-8: schema regex rejects "https://app.example.com/"
RED:     test_uiBaseUrl_paths_concatenate_clean() — "{{ uiBaseUrl }}/login" never produces "//login"
GREEN:   Replace hardcodes, add schemas, add env values
REFACTOR: Document pattern in CONTRIBUTING
VERIFY:  bash infra/tests/check-kratos-placeholders_test.sh + bash infra/tests/check-localhost-inventory_test.sh
```

#### Acceptance Criteria
- [ ] `grep -c "http://localhost" infra/helm/ory-kratos/values.yaml` = 0
- [ ] values.schema.json present + has uiBaseUrl required
- [ ] helm template fails when uiBaseUrl absent
- [ ] All 5 envs resolve to correct URL
- [ ] Pass: kubeconform --strict
- [ ] Pass: /code-audit lint

#### DoD
- [ ] Zero localhost in canonical kratos values
- [ ] Schema enforces uiBaseUrl
- [ ] Per-env override works
- [ ] Tests GREEN

### T2.4 — Phase 2 holistic regression (BEFORE enabling requiredKeys — EC-6)

#### Objective
Re-snapshot all 5 envs. Verify diff = 0 vs pre-Phase-2. ORDERING per EC-6:
esta task roda ANTES da T2.5 (requiredKeys). Quando holistic regression
GREEN, então T2.5 pode habilitar fail-fast safely.

#### Evidence
Phase 2 touches 47 inline conditionals + 9 hardcoded URLs + schema/fail-fast.
Risk of regression high.

#### Files to edit
```
infra/tests/snapshots/helmfile-<env>-template.yaml — UPDATE
infra/tests/check-helmfile-regression_test.sh — add sanitizer for non-deterministic fields (EC-7)
```

#### Tasks
1. Pre-Phase-2 snapshots already captured in T1.5
2. Post-Phase-2: capture new snapshots after sanitizing non-deterministic fields (uid, creationTimestamp — EC-7)
3. Diff = 0 for all 5 envs
4. Standalone-multi covered explicitly (EC-9 — local 4-cluster topology)

#### TDD
```
RED:     test_drill_template_matches_pre_phase2_snapshot()
RED:     test_dev_template_matches_pre_phase2_snapshot()
RED:     test_prod_template_matches_pre_phase2_snapshot()
RED:     test_standalone_template_matches_pre_phase2_snapshot()
RED:     test_standalone_multi_template_matches_pre_phase2_snapshot() — EC-9 explicit
RED:     test_snapshot_sanitized_no_uid_timestamp() — EC-7: sanitizer removes uid + creationTimestamp
GREEN:   Iterate until clean
REFACTOR: Update snapshot files only after manual verification
VERIFY:  bash infra/tests/check-helmfile-regression_test.sh
```

#### Acceptance Criteria
- [ ] All 5 env snapshots diff = 0
- [ ] Sanitizer applied to remove uid, creationTimestamp, random secrets (EC-7)
- [ ] Pass: kubeconform --strict
- [ ] CHANGELOG entry

#### DoD
- [ ] Regression GREEN for ALL 5 envs
- [ ] Phase 2 ready for T2.5 fail-fast enablement

### T2.5 — Enable helmfile fail-fast (requiredKeys) — LAST step (EC-6)

#### Objective
Habilitar `requiredKeys: true` em helmfile environment specs para
detectar drift entre envs ANTES de sync.

**ORDERING NOTE (EC-6 fix):** Esta task é a ÚLTIMA da Phase 2. Habilitar
requiredKeys ANTES de T2.2/T2.3/T2.4 completarem quebra envs ainda não
migrados. Pre-requisito: TODOS os envs (dev, drill, prod, standalone,
standalone-multi) têm todas as values keys populadas explicitamente E
T2.4 holistic regression GREEN.

#### Evidence
Helmfile docs: "Helmfile fails when you tried to access missing keys in
environment values. This catches drift between environments early."

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — environments block, add requiredKeys
infra/helmfile/build.helmfile.yaml.gotmpl — same
infra/helmfile/runtime.helmfile.yaml.gotmpl — same
infra/helmfile/environments/dev.yaml — declare required keys
infra/helmfile/environments/drill.yaml — same
infra/helmfile/environments/prod.yaml — same
infra/helmfile/environments/standalone.yaml — same
infra/helmfile/environments/standalone-multi.yaml — same
```

#### Deep file dependency analysis
Adds gate to helmfile sync. Missing key in any env file → fail. Depende
de T2.4 GREEN (regression suite confirms all envs populated).

#### Deep Dives
- requiredKeys lists go in helmfile environments block, e.g.:
  ```yaml
  environments:
    drill:
      values:
      - environments/drill.yaml
      missingFileHandler: Error
  ```
- Each chart's values.schema.json + helmfile requiredKeys combine for
  defense-in-depth

#### Tasks
1. **PREREQ check:** verify T2.4 acceptance criteria all checked
2. Add requiredKeys to all 3 helmfile gotmpl files
3. Declare top-level required keys in each env file (e.g., uiBaseUrl, domain, cells)
4. Trigger a fail-fast test (deliberately remove a key, ensure helmfile fails)
5. Restore key, validate sync succeeds

#### TDD
```
RED:     test_t24_acceptance_passed_first() — EC-6: prereq gate
RED:     test_helmfile_fails_on_missing_uiBaseUrl()
RED:     test_helmfile_fails_on_missing_domain()
RED:     test_helmfile_succeeds_with_all_keys()
RED:     test_helmfile_values_merge_order_explicit() — EC-18: chart's values-prod.yaml FIRST, then values/prod/<chart>.yaml OVERRIDES
RED:     test_helmfile_uses_helm_validate_flag() — EC-20: helmfile sync invokes `helm template --validate`/`helm install --dry-run --validate` so values.schema.json is enforced
RED:     test_schema_violation_blocks_sync() — EC-20: deliberately bad uiBaseUrl (missing protocol) must fail helmfile sync, not just helm template
GREEN:   Add requiredKeys config + explicit values precedence + --validate flag
REFACTOR: Document required keys in env file headers
VERIFY:  bash infra/tests/check-helmfile-fail-fast_test.sh
```

#### Acceptance Criteria
- [ ] T2.4 acceptance all checked (EC-6 ordering)
- [ ] Helmfile fails fast on missing required key
- [ ] Helmfile succeeds with all keys
- [ ] Documentation per env file
- [ ] Pass: hermetic fail-fast test

#### DoD
- [ ] requiredKeys enforced
- [ ] Tests GREEN
- [ ] Phase 2 complete

---

## Phase 3: Pulumi Consolidation

**Objective:** Drill = stack do `infra/pulumi/`. Delete `pulumi-drill/`,
`pulumi-infra/` (name collision), `pulumi-shared/` (empty). Keep
`pulumi-platform/` separate (different concern).

### T3.1 — Componentize pulumi-drill monolith into reusable Component Resources

#### Objective
Extract `infra/pulumi-drill/src/index.ts` (22KB monolith) into Component
Resources living in `infra/pulumi/src/components/`.

#### Evidence
Section 2 Evidence #3: pulumi-drill is monolithic. Industry best practice:
ComponentResource reuso.

#### Files to edit
```
infra/pulumi/src/components/loopzero-mgmt-cluster.ts (NEW)
infra/pulumi/src/components/loopzero-build-cluster.ts (NEW)
infra/pulumi/src/components/loopzero-runtime-cluster.ts (NEW)
infra/pulumi/src/components/loopzero-managed-pg.ts (NEW)
infra/pulumi/src/components/loopzero-managed-valkey.ts (NEW)
infra/pulumi/src/components/loopzero-vpc.ts (NEW)
infra/pulumi/src/components/loopzero-spaces-bucket.ts (NEW) — includes Velero bucket
infra/pulumi/src/components/loopzero-cf-zone-records.ts (NEW)
```

#### Deep file dependency analysis
- Component Resources have constructor args + `super()` call + outputs
- Each component is independently testable
- Existing pulumi-drill code becomes thin orchestrator that instantiates components

#### Deep Dives
- Component naming: `class LoopZeroMgmtCluster extends ComponentResource`
- Args interface: `interface LoopZeroMgmtClusterArgs { region: string; nodeSize: string; nodeCount: number; vpcId: pulumi.Input<string>; k8sVersion: string; }`
- Outputs: `kubeconfig: pulumi.Output<string>; clusterId: pulumi.Output<string>;`
- Parent-child: pass `{parent: this}` in opts

#### Tasks
1. Read pulumi-drill index.ts thoroughly (22KB)
2. Identify 8 logical groups (clusters, DBs, VPC, Spaces, etc.)
3. Extract each as Component Resource
4. Add unit tests for component instantiation
5. (Don't yet wire to drill stack — that's T3.3)

#### TDD
```
RED:     test_loopzero_mgmt_cluster_creates_doks() — Pulumi mock test
RED:     test_loopzero_managed_pg_creates_database() — mock test
RED:     test_loopzero_spaces_bucket_creates_velero_bucket() — Velero gap closure
RED:     test_component_emits_kubeconfig_output()
GREEN:   Extract components
REFACTOR: Common args interfaces (e.g., DOResourceArgs)
VERIFY:  cd infra/pulumi && npm test
```

#### Acceptance Criteria
- [ ] 8 Component Resources in `src/components/`
- [ ] Unit tests for each (Pulumi mocks)
- [ ] No regression in existing `infra/pulumi/` tests
- [ ] Pass: /code-audit complexity (each component <500 lines)
- [ ] Pass: /code-audit coverage >= 80% for components

#### DoD
- [ ] Components extracted
- [ ] Tests passing
- [ ] Coverage met

### T3.2 — Migrate pulumi-drill state to infra/pulumi/ stack=drill

#### Objective
Per D7: state export from pulumi-drill, edit JSON, import to pulumi/ stack=drill.

**PREREQ (EC-1):** T3.2.5 (dry-run on isolated test stack) MUST be GREEN
before executing T3.2 against the real state.

#### Evidence
Need to preserve cluster IDs, DB IDs, etc. Re-create = $5+ cost + data
loss + 30min provision time.

#### Files to edit
```
infra/pulumi/Pulumi.drill.yaml (NEW) — stack config matching pulumi-drill's
infra/pulumi/src/stacks/drill.ts (NEW) — drill-specific composition using components
infra/pulumi/src/index.ts — switch on pulumi.getStack() to dispatch to stack composition
/tmp/drill-state-pre-migration.json (NEW, ephemeral) — backup
```

#### Deep file dependency analysis
- Pulumi state is in S3 backend (`s3://loopzero-drill-pulumi-state`)
- State JSON references project name `loopzero-drill` — must edit to `loopzero-infra`
- URN format: `urn:pulumi:drill::loopzero-drill::doks::loopzero-drill-mgmt-cluster`
  → `urn:pulumi:drill::loopzero-infra::loopzero:cluster:LoopZeroMgmtCluster::loopzero-drill-mgmt-cluster`

#### Deep Dives
- Migration script:
  ```bash
  cd infra/pulumi-drill && npx pulumi stack export --stack drill --show-secrets > /tmp/drill-state-pre-migration.json
  jq '.deployment.resources |= map(.urn = (.urn | sub("loopzero-drill::"; "loopzero-infra::")))' /tmp/drill-state-pre-migration.json > /tmp/drill-state-migrated.json
  cd ../pulumi && npx pulumi stack init drill
  npx pulumi stack import --file /tmp/drill-state-migrated.json
  npx pulumi preview --stack drill  # MUST show 0 changes
  ```
- Backup before migration; restore if preview shows changes

#### Tasks
1. Backup `pulumi-drill` state to `/tmp/`
2. Run migration script
3. Run `pulumi preview --stack drill` — must show ZERO changes
4. If non-zero: investigate URN mismatches, fix, retry
5. Once clean: archive `infra/pulumi-drill/` (don't delete yet — Phase 3.4)

#### TDD
```
RED:     test_drill_state_exported() — backup file exists
RED:     test_drill_state_imported() — `pulumi stack ls` shows drill in pulumi/
RED:     test_drill_preview_zero_changes() — preview output has "Resources: ... unchanged"
GREEN:   Run migration
REFACTOR: Document migration script in runbook
VERIFY:  bash infra/tests/check-pulumi-drill-migration_test.sh
```

#### Acceptance Criteria
- [ ] Drill stack exists in `infra/pulumi/`
- [ ] Preview shows 0 changes (state migrated cleanly)
- [ ] Backup state file archived
- [ ] Pass: hermetic migration test (dry-run)

#### DoD
- [ ] State migrated
- [ ] Preview clean
- [ ] Sponsor sign-off

### T3.2.5 — Dry-run state migration on isolated test stack (EC-1)

#### Objective
ANTES de aplicar T3.2 contra o state real do drill, executar a migration
em um stack-clone isolado para validar que o jq URN regex não corrompe
estado. EC-1 mitigation: risk de re-create de cluster por URN mismatch.

#### Evidence
EC-1 finding: `jq '... sub("loopzero-drill::"; "loopzero-infra::")'` é regex flat
sobre URNs aninhados. URNs com parent embedded ou strings que coincidentemente
contêm "loopzero-drill::" (resource IDs, secret names) podem ser corrompidos.
Pulumi state import com URN errado → preview mostra "create new resource"
→ destroy+create de cluster ativo.

#### Files to edit
```
infra/tests/check-pulumi-urn-migration_test.sh (NEW hermetic)
infra/scripts/dr/pulumi-state-migrate.sh (NEW) — wraps jq + validation
docs/plans/2026-05-20-pulumi-state-migration-runbook.md (NEW)
```

#### Deep file dependency analysis
- Hermetic test fixtures: synthetic state JSON with edge-case URNs
  (URNs containing "loopzero-drill::" in resource names, in parent refs, in
  secret strings)
- Wrapper script enforces URN-count invariant before/after edit
- Runbook documents operator steps for the LIVE migration

#### Deep Dives
- URN structure: `urn:pulumi:<stack>::<project>::<type>::<name>[::parent::name]`
- Edge cases for jq regex:
  - URN with provider type containing project name as substring
  - Resource name that literally is "loopzero-drill" (unlikely but possible
    if a Pulumi `Resource` was named "loopzero-drill" inside the stack)
  - Encrypted secrets that base64-encode bytes matching the pattern
- Mitigation: targeted jq path expression instead of `sub` on whole string:
  ```jq
  .deployment.resources |= map(
    if .urn | startswith("urn:pulumi:drill::loopzero-drill::")
    then .urn |= sub("::loopzero-drill::"; "::loopzero-infra::")
    else .
    end
  )
  ```
- Invariant: `jq '[.deployment.resources[] | .urn] | length'` must be
  unchanged before/after edit

#### Tasks
1. Write hermetic test with synthetic state JSON (URN edge cases)
2. Write `pulumi-state-migrate.sh` script with safer jq + invariant check
3. Run hermetic test — all edge cases handled
4. Operator executes script on REAL state with `--dry-run` flag first
5. `pulumi stack init drill-test` (NEW disposable stack)
6. Import migrated state into drill-test
7. `pulumi preview --stack drill-test` — must show 0 changes
8. Destroy drill-test stack (clean)
9. ONLY THEN proceed to T3.2 with real stack

#### TDD
```
RED:     test_urn_count_invariant() — count URNs before/after edit unchanged
RED:     test_nested_urn_parent_preserved() — parent URN refs not corrupted
RED:     test_resource_name_containing_drill_unchanged() — string-in-name preserved
RED:     test_encrypted_secret_not_corrupted() — base64-encoded bytes safe
RED:     test_dry_run_no_state_write() — --dry-run produces output but writes nothing
RED:     test_drill_test_stack_preview_zero_changes()
RED:     test_encryption_salt_preserved_or_remapped() — EC-22: pulumi-drill encryptionsalt (v1:rwO+QhqlQiQ=:...) survives migration; secrets re-decryptable after import
RED:     test_provider_config_urns_preserved() — DO + CF provider URNs map correctly
RED:     test_backup_state_decryptable() — EC-12: exported backup can be re-imported standalone
GREEN:   Implement targeted jq script + hermetic harness + salt-aware import
REFACTOR: Move common jq patterns to lib
VERIFY:  bash infra/tests/check-pulumi-urn-migration_test.sh
```

#### Acceptance Criteria
- [ ] Hermetic test 6/6 PASS
- [ ] Script supports `--dry-run` flag (per memory `feedback_dry_run_flag_mandatory_for_state_writers`)
- [ ] drill-test stack preview = 0 changes
- [ ] Runbook reviewed by sponsor
- [ ] Pass: shellcheck strict
- [ ] Pass: /code-audit complexity (script <200 lines)

#### DoD
- [ ] Dry-run validation GREEN
- [ ] Hermetic tests covering edge cases
- [ ] Operator runbook complete
- [ ] Sponsor sign-off BEFORE T3.2 with real state

### T3.3 — Write drill stack composition using Phase 3.1 components

#### Objective
`infra/pulumi/src/stacks/drill.ts` composes drill stack from Component
Resources. Reads Pulumi.drill.yaml config.

#### Evidence
Components without composition = unused code. Stack composition completes
the parametrization.

#### Files to edit
```
infra/pulumi/src/stacks/drill.ts (NEW)
infra/pulumi/src/stacks/prod.ts (NEW, future-ready)
infra/pulumi/src/stacks/dev.ts (NEW, future-ready)
infra/pulumi/src/index.ts — switch on pulumi.getStack()
```

#### Deep file dependency analysis
`src/index.ts` becomes:
```typescript
const stack = pulumi.getStack();
switch (stack) {
  case "drill": return import("./stacks/drill");
  case "prod": return import("./stacks/prod");
  default: throw new Error(`Unknown stack: ${stack}`);
}
```

#### Tasks
1. Write `src/stacks/drill.ts` using components
2. Wire `src/index.ts` dispatch
3. Validate `pulumi preview --stack drill` still 0 changes (composition matches state)
4. Iterate until clean

#### TDD
```
RED:     test_drill_stack_composes_mgmt_build_runtime_runtime2()
RED:     test_drill_stack_provisions_velero_bucket()  — closes Velero gap (D6.bis)
RED:     test_drill_stack_preview_zero_changes() — verify composition matches imported state
GREEN:   Write composition
REFACTOR: Extract common patterns
VERIFY:  cd infra/pulumi && npx pulumi preview --stack drill
```

#### Acceptance Criteria
- [ ] drill stack composable from components
- [ ] preview shows 0 changes
- [ ] Velero bucket included
- [ ] Pass: TypeScript strict mode
- [ ] Pass: /code-audit lint

#### DoD
- [ ] Stack composed
- [ ] Velero gap closed (engineering side)
- [ ] Tests GREEN

### T3.4 — Delete pulumi-drill, pulumi-infra, pulumi-shared

#### Objective
Remove legacy directories that are now superseded.

#### Evidence
Section 2 Evidence #3: 5 Pulumi projects, 2 with same name, 1 empty.

#### Files to edit
```
infra/pulumi-drill/ (DELETE entire directory)
infra/pulumi-infra/ (DELETE — name collision)
infra/pulumi-shared/ (DELETE — empty)
.github/workflows/cell2-soaks.yml — update Pulumi project path
.github/workflows/pulumi-reconstruction-drill.yml — update path
.github/workflows/drill-auto-destroy-watchdog.yml — update path
infra/scripts/drill-helmfile-bootstrap.sh — update Pulumi paths
README.md — update infra layout docs if present
```

#### Deep file dependency analysis
- 3 workflows reference pulumi-drill — must update
- Scripts referencing `cd infra/pulumi-drill && pulumi` change to `cd infra/pulumi && pulumi --stack drill`
- Pulumi state in S3 is preserved (state doesn't live in repo)

#### Tasks
1. Audit ALL refs to `pulumi-drill`, `pulumi-infra`, `pulumi-shared`
2. Update workflows + scripts to new paths
3. Delete the 3 directories
4. Validate CI still passes (dry-run workflows)

#### TDD
```
RED:     test_no_pulumi_drill_dir() — directory absent
RED:     test_no_pulumi_infra_dir() — directory absent
RED:     test_no_pulumi_shared_dir() — directory absent
RED:     test_workflows_reference_new_paths() — grep workflows
RED:     test_pulumi_drill_stack_in_main_project() — pulumi stack ls in infra/pulumi/ shows drill
GREEN:   Delete + update references
REFACTOR: None
VERIFY:  bash infra/tests/check-pulumi-cleanup_test.sh
```

#### Acceptance Criteria
- [ ] 3 directories deleted
- [ ] Workflows updated
- [ ] Scripts updated
- [ ] `pulumi stack ls` in `infra/pulumi/` shows drill stack
- [ ] CI green
- [ ] Pass: /code-audit dead code check

#### DoD
- [ ] Legacy gone
- [ ] References updated
- [ ] Tests GREEN

---

## Phase 4: Scripts Deduplication

**Objective:** 13 `drill-*` scripts → parameterized canonical in
`infra/helmfile/scripts/`. 6 `bootstrap-cell-*-cc{,-drill}.sh` → 1
parametrized.

### T4.1 — Map 13 drill-* scripts to canonical replacements

#### Objective
Inventory + design canonical replacement for each drill-* script.

#### Files to edit
```
docs/plans/2026-05-20-scripts-canonical-mapping.md (NEW)
```

#### Tasks
1. List 13 drill-* scripts
2. For each: identify canonical name + parameterization strategy
3. Document old→new mapping table

#### TDD
```
RED:     test_mapping_has_13_entries()
RED:     test_each_canonical_name_unique()
GREEN:   Draft mapping
VERIFY:  bash infra/tests/check-scripts-mapping_test.sh
```

#### Acceptance Criteria
- [ ] 13 entries in mapping doc
- [ ] Sponsor review

#### DoD
- [ ] Mapping ratified

### T4.2 — Consolidate bootstrap-cell-*-cc scripts (6 → 1)

#### Objective
6 files (`bootstrap-cell-{default,1,2}-cc{,-drill}.sh`) → 1
`infra/helmfile/scripts/bootstrap-cell-cc.sh --cell <id> --env <name>`.

#### Files to edit
```
infra/helmfile/scripts/bootstrap-cell-cc.sh (NEW, parametrized)
infra/scripts/bootstrap-cell-default-cc.sh (DELETE)
infra/scripts/bootstrap-cell-1-cc.sh (DELETE)
infra/scripts/bootstrap-cell-2-cc.sh (DELETE)
infra/scripts/bootstrap-cell-default-cc-drill.sh (DELETE)
infra/scripts/bootstrap-cell-1-cc-drill.sh (DELETE)
infra/scripts/bootstrap-cell-2-cc-drill.sh (DELETE)
Taskfile.yml — update task targets
.github/workflows/*.yml — update workflow refs
```

#### Deep file dependency analysis
- Tasks `task k3d:bootstrap-cell-{1,2}` call these scripts
- CI workflows call these scripts
- All callers need update

#### Deep Dives
- Args: `--cell <default|1|2> --env <dev|drill|prod|standalone|standalone-multi>` (EC-16: 5 envs explicit)
- Logic: resolve kubeconfig per env, resolve cluster name per cell, run kubectl apply
- Output: ClusterCredential resource in mgmt cluster

**EC-4 — Explicit cell→cluster mapping (CRITICAL):**

| Cell ID      | env=dev / standalone / standalone-multi (k3d) | env=drill (DOKS)                   | env=prod (DOKS)                    |
|--------------|------------------------------------------------|------------------------------------|------------------------------------|
| cell-default | `k3d-loopzero-workload`                            | `loopzero-drill-runtime-cluster`       | `loopzero-prod-runtime-cluster`        |
| cell-1       | `k3d-loopzero-workload` (same as default — local k3d topology limit) | `loopzero-drill-runtime-cluster` (same as default per D3) | `loopzero-prod-runtime-cluster` (same as default) |
| cell-2       | `k3d-loopzero-runtime-2`                           | `loopzero-drill-runtime-2-cluster`     | `loopzero-prod-runtime-2-cluster`      |

**Bug B82 history:** Past bug used `cluster-name=runtime` for k3d — wrong;
the docker container is `k3d-loopzero-workload-server-0`, not `k3d-loopzero-runtime-*`.
The mapping above MUST be in the canonical `lib/cell-cluster-map.sh`
file to prevent regression.

**Pulumi stack output dependency:** drill/prod cluster names come from
`pulumi stack output --stack <env> mgmtCluster/buildCluster/runtimeCluster/runtimeCluster2`
— hardcoding NEW names is anti-pattern; resolve dynamically.

#### Tasks
1. Write new script supporting all 6 combos (3 cells × 2 envs)
2. Add hermetic tests for arg parsing
3. Delete 6 old scripts
4. Update Taskfile + workflows
5. Smoke test all 6 combos

#### TDD
```
RED:     test_cell_default_dev() — script runs with --cell default --env dev
RED:     test_cell_1_drill() — same with drill
RED:     test_cell_2_drill() — verify k3d-loopzero-runtime-2 → drill runtime2 cluster
RED:     test_cell_default_standalone_multi() — EC-16: 5th env coverage
RED:     test_cell_1_standalone_multi() — EC-16
RED:     test_cell_2_standalone_multi() — EC-16
RED:     test_invalid_cell_fails() — exit 1 on bad --cell
RED:     test_invalid_env_fails() — exit 1 on bad --env (e.g., --env staging fails)
GREEN:   Write script
REFACTOR: Extract kubeconfig resolution to lib
VERIFY:  bash infra/tests/check-bootstrap-cell-cc_test.sh
```

#### Acceptance Criteria
- [ ] 1 script replaces 6
- [ ] All 6 combos tested
- [ ] Taskfile updated
- [ ] CI workflows updated
- [ ] Pass: shellcheck
- [ ] Pass: /code-audit complexity (script <300 lines)

#### DoD
- [ ] Consolidation complete
- [ ] Tests GREEN
- [ ] Callers updated

### T4.3 — Consolidate drill-helmfile-bootstrap.sh + canonical equivalent

#### Objective
`drill-helmfile-bootstrap.sh` (drill-specific) → canonical
`infra/helmfile/scripts/bootstrap-helmfile.sh --env <name>`.

#### Files to edit
```
infra/helmfile/scripts/bootstrap-helmfile.sh (NEW)
infra/scripts/drill-helmfile-bootstrap.sh (DELETE)
Taskfile.yml — task drill:bootstrap → bash infra/helmfile/scripts/bootstrap-helmfile.sh --env drill
.github/workflows/*.yml — update refs
```

#### Deep file dependency analysis
- `drill-helmfile-bootstrap.sh` is 940 LOC with drill-specific logic
- Most of that logic is generic (Phase 1-7 of bootstrap) — only kubeconfig + cell wiring differ per env
- Canonical script accepts `--env` and resolves env-specific bits

#### Deep Dives
- Phases preserved: pre-flight, Secrets generation, helmfile sync, cell wiring, cluster issuers, JWT mint, validation
- Per-env differs: kubeconfig source (DRILL_KUBECONFIG_DIR vs ~/.kube/config), bucket names, secret sources
- DRY pattern: per-env config in `infra/helmfile/scripts/lib/env-config.sh`

**EC-3 — 11 LIVE patches preservation (CRITICAL):**

Per memória `reference_drill_phase2_session_2026_05_18`, drill-helmfile-bootstrap.sh
acumulou 11 fixes inline (Bugs #56-#82). Cada um foi descoberto LIVE +
codificado in-script. Refatoração canônica DEVE preservar cada um
VERBATIM ou regression cascading retorna.

Lista dos 11 patches a preservar (cada um deve ter marker comment + test
que verifica linha existe ou comportamento equivalente):

| Bug ID | Patch description | Marker grep pattern |
|--------|-------------------|---------------------|
| #56    | Secret loopzero-cluster-kubeconfigs override PRE-Phase-3 | `loopzero-cluster-kubeconfigs` + `Bug #56` |
| #69    | pg_advisory_lock leaked → pg_terminate_backend pid | `pg_terminate_backend` |
| #71    | loopzero-packs v0.4.0 missing → LOOPZERO_PACKS_IMAGE=v0.2.0 fallback | `LOOPZERO_PACKS_IMAGE` |
| #72    | buildkit-registry-config Secret created | `buildkit-registry-config` |
| #73    | BuildKit k3d→DOCR registry override | `BUILDKIT_REGISTRY` |
| #74    | cosign-signing-key Secret generated | `cosign-signing-key` |
| #75    | loopzero-app SA imagePullSecrets fix | `imagePullSecrets` + `loopzero-app` |
| #77    | DOCR pull secret BOTH "loopzero-system" AND "loopzero-registry-pull" names | `loopzero-system` + `loopzero-registry-pull` |
| #79    | letsencrypt-prod ClusterIssuer loop including mgmt | `letsencrypt-prod` + loop |
| #80    | Hydra issuer FQDN cluster.local | `ory-hydra-public.loopzero-auth.svc.cluster.local:4444` |
| #82    | k3d cluster-name `workload` (not `runtime`) for cell-default | `cluster-name.*workload` |

#### Tasks
1. Read 940 LOC carefully — identify generic vs env-specific blocks
2. Annotate each of the 11 patches with `# Bug #NN — preserve verbatim` marker
3. Extract env-specific to `lib/env-config.sh` — supports ALL 5 envs (EC-16: dev/drill/prod/standalone/standalone-multi)
4. Write canonical script consuming lib — preserving all 11 markers
5. Add hermetic tests for `--env dev`, `--env drill`, `--env standalone-multi` (EC-16 5th env)
6. Add EC-3 preservation test: hermetic test greps for each of the 11 markers
7. Delete drill-helmfile-bootstrap.sh
8. Update Taskfile + workflows

#### TDD
```
RED:     test_bootstrap_helmfile_dev_dry_run() — env=dev produces helmfile sync cmd
RED:     test_bootstrap_helmfile_drill_dry_run() — env=drill same
RED:     test_bootstrap_helmfile_no_secret_to_tmp() — verify /tmp/ secret writes removed (D6 SOPS)
RED:     test_bootstrap_helmfile_fail_fast_missing_env_var()
RED:     test_bootstrap_preserves_bug_56_secret_override() — EC-3
RED:     test_bootstrap_preserves_bug_69_pg_terminate() — EC-3
RED:     test_bootstrap_preserves_bug_71_loopzero_packs_fallback() — EC-3
RED:     test_bootstrap_preserves_bug_72_buildkit_registry() — EC-3
RED:     test_bootstrap_preserves_bug_73_buildkit_docr() — EC-3
RED:     test_bootstrap_preserves_bug_74_cosign_key() — EC-3
RED:     test_bootstrap_preserves_bug_75_loopzero_app_imagepullsecrets() — EC-3
RED:     test_bootstrap_preserves_bug_77_dual_docr_pull_secrets() — EC-3
RED:     test_bootstrap_preserves_bug_79_letsencrypt_mgmt_loop() — EC-3
RED:     test_bootstrap_preserves_bug_80_hydra_fqdn_issuer() — EC-3
RED:     test_bootstrap_preserves_bug_82_cluster_name_workload() — EC-3
GREEN:   Write canonical + lib + preserve all 11 markers
REFACTOR: Extract more lib functions
VERIFY:  bash infra/tests/check-bootstrap-helmfile_test.sh + bash infra/tests/check-bootstrap-bug-preservation_test.sh
```

#### Acceptance Criteria
- [ ] 1 script replaces drill-helmfile-bootstrap.sh
- [ ] Hermetic tests cover dev + drill
- [ ] ZERO `/tmp/` secret writes (Phase 7 SOPS)
- [ ] EC-3: all 11 bug-marker preservation tests PASS (Bugs #56,#69,#71,#72,#73,#74,#75,#77,#79,#80,#82)
- [ ] Pass: shellcheck strict
- [ ] Pass: /code-audit complexity

#### DoD
- [ ] Consolidation complete
- [ ] Tests GREEN
- [ ] /tmp secrets eliminated
- [ ] EC-3: all 11 LIVE patches preserved + tests guard regression

### T4.4 — Consolidate drill-mint-jwt.sh + seed-local-user.sh

#### Objective
2 scripts → 1 `infra/helmfile/scripts/seed-jwt.sh --env <name>`.

#### Files to edit
```
infra/helmfile/scripts/seed-jwt.sh (NEW)
infra/scripts/drill-mint-jwt.sh (DELETE)
infra/helmfile/scripts/seed-local-user.sh (DELETE — superseded)
infra/helmfile/scripts/_common.sh — adjust if needed
infra/scripts/drill-dogfood-orchestrator.sh — update path
Taskfile.yml — update task targets
```

#### Deep file dependency analysis
- Hydra namespace differs: loopzero-system (k3d standalone) vs loopzero-auth (drill DOKS prod-parity)
- Both files have own implementations of mint() — Bug #35 from prior session lurks here
- Canonical resolves via `--env`

#### Tasks
1. Read both files
2. Identify divergent fields (Hydra ns, expires-in, output path)
3. Write canonical with `--env` switch
4. Hermetic tests
5. Update callers

#### TDD
```
RED:     test_seed_jwt_dev_hydra_ns_loopzero_system()
RED:     test_seed_jwt_drill_hydra_ns_loopzero_auth()  # prod-parity per D2
RED:     test_seed_jwt_default_expires_in_86400()  # 24h per session fix
RED:     test_seed_jwt_writes_to_xdg_config_home()
GREEN:   Write canonical
REFACTOR: None
VERIFY:  bash infra/tests/check-seed-jwt_test.sh
```

#### Acceptance Criteria
- [ ] 1 script
- [ ] Both envs supported
- [ ] Hydra ns correct per env
- [ ] Pass: shellcheck
- [ ] Pass: hermetic tests

#### DoD
- [ ] Consolidation complete
- [ ] Tests GREEN

### T4.5 — Consolidate remaining 10 drill-* scripts

#### Objective
Remaining: `drill-cancel-refresh`, `drill-chaos-orchestrator`,
`drill-cleanroom-orchestrator`, `drill-cost-report`,
`drill-dogfood-orchestrator`, `drill-evidence-capture`,
`drill-jwt-diagnose`, `drill-preflight`, `drill-state-reset`,
`drill-teardown-validator`, `drill-watchdog`.

#### Files to edit
Per-script: map old → new in `infra/helmfile/scripts/`. Some scripts (e.g.,
drill-watchdog, drill-teardown-validator) are drill-specific by domain
(disaster recovery) — keep in `infra/scripts/dr/` with clearer naming.

```
infra/helmfile/scripts/orchestrate-cleanroom.sh (NEW, from drill-cleanroom-orchestrator)
infra/helmfile/scripts/orchestrate-dogfood.sh (NEW)
infra/helmfile/scripts/orchestrate-chaos.sh (NEW)
infra/helmfile/scripts/diagnose-jwt.sh (NEW, from drill-jwt-diagnose)
infra/helmfile/scripts/preflight.sh (NEW)
infra/helmfile/scripts/evidence-capture.sh (NEW)
infra/scripts/dr/ (NEW dir)
infra/scripts/dr/cost-report.sh (was drill-cost-report)
infra/scripts/dr/teardown-validator.sh (was drill-teardown-validator)
infra/scripts/dr/state-reset.sh (was drill-state-reset)
infra/scripts/dr/watchdog.sh (was drill-watchdog)
infra/scripts/dr/cancel-refresh.sh (was drill-cancel-refresh)
infra/scripts/drill-* (DELETE all 11 remaining)
Taskfile.yml — update task targets
.github/workflows/cell2-soaks.yml — update refs
.github/workflows/drill-l2-scale.yml — update refs
.github/workflows/drill-auto-destroy-watchdog.yml — update refs
.github/workflows/pulumi-reconstruction-drill.yml — update refs
```

#### Tasks
1. Read each script
2. Move + parameterize
3. Add hermetic tests
4. Update Taskfile + workflows
5. Delete old

#### TDD
```
RED:     test_zero_drill_prefix_scripts() — find infra/scripts/drill-* returns empty
RED:     test_canonical_scripts_have_env_flag()
RED:     test_dr_scripts_in_dr_dir()
GREEN:   Migrate + delete
REFACTOR: Common lib usage
VERIFY:  bash infra/tests/check-scripts-canonical_test.sh
```

#### Acceptance Criteria
- [ ] 0 `infra/scripts/drill-*`
- [ ] All replacements in canonical locations
- [ ] CI workflows updated
- [ ] Pass: shellcheck
- [ ] Pass: /code-audit dead code check

#### DoD
- [ ] Consolidation complete
- [ ] Tests GREEN

---

## Phase 5: Test Scaffolding Parametrization

**Objective:** Replace `DRILL_KUBECONFIG_DIR` with `LOOPZERO_TEST_ENV` env var
across e2e helpers + test infrastructure.

### T5.1 — Refactor cluster_kubectl_helpers_test.go to LOOPZERO_TEST_ENV

#### Objective
Generalize the helper from commit `daf39d69` to support k3d/drill/prod.

#### Files to edit
```
infra/tests/e2e/cluster_kubectl_helpers_test.go — replace DRILL_KUBECONFIG_DIR with LOOPZERO_TEST_ENV
infra/tests/e2e/cluster_kubectl_drill_test.go (NEW) — drill-specific resolver
infra/tests/e2e/cluster_kubectl_local_test.go (NEW) — local k3d resolver
infra/tests/e2e/cluster_kubectl_prod_test.go (NEW, future-ready) — prod resolver placeholder
infra/tests/e2e/helpers_test.go — e2eBaseDomain() also param by LOOPZERO_TEST_ENV
infra/tests/e2e/deploy_suite_test.go — Hydra mint per env
infra/tests/seed/seed.go — kubectlArgs() param by LOOPZERO_TEST_ENV
```

#### Deep file dependency analysis
- e2eBaseDomain() in helpers_test.go currently uses DRILL_KUBECONFIG_DIR
- seed.kubectlArgs same pattern
- Both need refactor to share LOOPZERO_TEST_ENV resolution

#### Deep Dives
- Common interface:
  ```go
  type EnvResolver interface {
      KubectlArgs(role ClusterRole) []string
      BaseDomain() string
      APIURL() string
  }
  ```
- Each impl in own file (Open/Closed)
- Factory: `func NewEnvResolver(env string) EnvResolver`

#### Tasks
1. Define EnvResolver interface
2. Write 3 implementations (local, drill, prod-stub)
3. Refactor helpers to consume EnvResolver
4. Update CI workflows to set LOOPZERO_TEST_ENV
5. Backward compat: if LOOPZERO_TEST_ENV unset but DRILL_KUBECONFIG_DIR set, log deprecation warning and route to drill resolver. **EC-23 deprecation window: 1 sprint (~2 weeks)** entre T5.1 (introduce fallback) e T5.2 (remove fallback). Audit log shows N callers using legacy env var; T5.2 só prossegue quando counter == 0 OU 2 semanas passaram (whichever first).

#### TDD
```
RED:     test_env_resolver_local_returns_k3d_args()
RED:     test_env_resolver_drill_returns_drill_kc_args()
RED:     test_env_resolver_prod_returns_stub_err()
RED:     test_helpers_use_env_resolver()
RED:     test_seed_uses_env_resolver()
GREEN:   Refactor
REFACTOR: Extract common patterns
VERIFY:  cd infra/tests/e2e && go test -run TestEnvResolver
```

#### Acceptance Criteria
- [ ] EnvResolver interface defined
- [ ] 3 impls (local, drill, prod-stub)
- [ ] All e2e helpers consume EnvResolver
- [ ] LOOPZERO_TEST_ENV env var docs in README
- [ ] Pass: go vet
- [ ] Pass: /code-audit coverage >= 90% for resolver

#### DoD
- [ ] Refactor complete
- [ ] Tests GREEN

### T5.2 — Remove DRILL_KUBECONFIG_DIR back-compat fallback

#### Objective
After T5.1 deprecation period, remove the DRILL_KUBECONFIG_DIR fallback.
Only LOOPZERO_TEST_ENV=drill works.

#### Files to edit
```
infra/tests/e2e/cluster_kubectl_helpers_test.go — remove fallback
README.md or test docs — document LOOPZERO_TEST_ENV
.github/workflows/cell2-soaks.yml — set LOOPZERO_TEST_ENV=drill (instead of DRILL_KUBECONFIG_DIR)
infra/scripts/dr/teardown-validator.sh — set LOOPZERO_TEST_ENV
```

#### Tasks
1. Confirm all callers use LOOPZERO_TEST_ENV (not DRILL_KUBECONFIG_DIR)
2. Remove fallback code
3. Update CI workflows
4. Tests

#### TDD
```
RED:     test_drill_kubeconfig_dir_unrecognized() — env var without LOOPZERO_TEST_ENV fails
RED:     test_loopzero_test_env_drill_works()
GREEN:   Remove fallback
REFACTOR: None
VERIFY:  cd infra/tests/e2e && go test
```

#### Acceptance Criteria
- [ ] DRILL_KUBECONFIG_DIR refs gone (except DRILL_KUBECONFIG_DIR as test internal, OK)
- [ ] LOOPZERO_TEST_ENV documented
- [ ] CI workflows pass
- [ ] Pass: /code-audit dead code

#### DoD
- [ ] Back-compat removed
- [ ] Tests GREEN

### T5.3 — Add `--env <name>` to Taskfile e2e targets (EC-21: all 5 envs)

#### Objective
Adicionar `task e2e:<env>` para CADA um dos 5 envs do sistema: dev,
drill, prod, standalone, standalone-multi. EC-21: cobertura completa
em vez de só local+drill.

#### Files to edit
```
Taskfile.yml — add 5 task e2e:<env> targets
taskfiles/e2e.yml — env-specific test targets
README.md — document LOOPZERO_TEST_ENV + task targets
```

#### Tasks
1. Add task targets para os 5 envs:
   - `task e2e:dev`
   - `task e2e:drill`
   - `task e2e:prod` (stub que avisa "not yet supported" + exit 1 até prod cluster existir)
   - `task e2e:standalone`
   - `task e2e:standalone-multi`
2. Wire each to LOOPZERO_TEST_ENV
3. Document in README + Taskfile help text

#### TDD
```
RED:     test_task_e2e_dev_runs_with_env_dev()
RED:     test_task_e2e_drill_runs_with_env_drill()
RED:     test_task_e2e_prod_stub_exits_1() — until prod cluster ready
RED:     test_task_e2e_standalone_runs_with_env_standalone()
RED:     test_task_e2e_standalone_multi_runs() — EC-21 5th env coverage
RED:     test_taskfile_e2e_targets_cover_all_5_envs() — EC-21 explicit
GREEN:   Add 5 tasks
VERIFY:  task --list-all | grep e2e | wc -l == 5
```

#### Acceptance Criteria
- [ ] 5 task e2e:<env> targets present
- [ ] EC-21: ALL 5 envs covered (not just 2)
- [ ] Documented in README
- [ ] Pass: Taskfile lint

#### DoD
- [ ] 5 tasks ready
- [ ] Tests GREEN

---

## Phase 6: Velero Gap Closure (parallel)

**Objective:** Close finding `finding-2026-05-20-velero-bsl-schedules-drill-gap.md`.

### T6.1 — Add LoopZeroVeleroBucket Component Resource

#### Objective
Velero bucket creation as Component Resource (already in T3.1 plan; this
verifies it lands correctly).

#### Files to edit
```
infra/pulumi/src/components/loopzero-spaces-bucket.ts — add veleroBucket
infra/pulumi/Pulumi.drill.yaml — config loopzero:veleroBucket settings
infra/pulumi/src/stacks/drill.ts — instantiate LoopZeroVeleroBucket
```

#### Tasks
1. Add to component (T3.1 already drafts this)
2. Wire to drill stack (T3.3)
3. Validate `pulumi up` creates bucket

#### TDD
```
RED:     test_drill_stack_creates_velero_bucket() — Pulumi mock
RED:     test_velero_bucket_has_30d_lifecycle() — expiration policy
RED:     test_velero_bucket_acl_private()
GREEN:   Component + stack composition
VERIFY:  cd infra/pulumi && npx pulumi preview --stack drill | grep velero-bucket
```

#### Acceptance Criteria
- [ ] Bucket in Pulumi state
- [ ] 30d lifecycle policy
- [ ] Private ACL
- [ ] Pass: Pulumi mock tests

#### DoD
- [ ] Component done
- [ ] Tests GREEN

### T6.2 — Debug schedules subchart materialization

#### Objective
Find why `helmfile -e drill template -l name=velero` doesn't emit
Schedule CRs even though `values-schedules.yaml` is in values list.

#### Files to edit
```
infra/helm/velero/values-schedules.yaml — verify schedules: block exists and is shaped correctly
infra/helm/velero/templates/schedules.yaml (NEW if missing) — emit Schedule CRs from .Values.schedules
infra/helmfile/values/drill/velero.yaml — verify schedules NOT overridden empty
```

#### Tasks
1. `helmfile -e drill template -l name=velero` — capture output
2. Check if schedules block reaches the chart
3. If chart doesn't have schedules template, add it
4. Verify 5 schedules emitted per env

#### TDD
```
RED:     test_drill_template_emits_5_schedules() — yq parse, count Schedule kind
RED:     test_schedule_names_match_expected() — velero-loopzero-{argocd,crds,full,...}-backup
GREEN:   Add template if missing
REFACTOR: Document pattern
VERIFY:  bash infra/tests/check-velero-schedules_test.sh
```

#### Acceptance Criteria
- [ ] 5 Schedule CRs emitted in drill env
- [ ] Names match Journey 12 V-3 expected list
- [ ] Pass: kubeconform --strict for Schedule CRD

#### DoD
- [ ] Schedules emitted
- [ ] Tests GREEN

### T6.3 — Validate Journey 12 V-1..V-8 PASS on drill

#### Objective
Run Ginkgo journey label-filter Journey 12 against drill, target 8/8 PASS.

#### Files to edit
```
None — validation only
```

#### Tasks
1. Provision drill via Phase 3 unified Pulumi
2. Bootstrap via Phase 4 canonical script
3. Run `LOOPZERO_TEST_ENV=drill go test --label-filter="journey" --run="Velero" ./infra/tests/e2e/`
4. Capture JUnit XML
5. Verify 8 PASS

#### TDD
```
RED:     test_journey_12_pass_8_of_8_on_drill()
GREEN:   Iterate until clean
VERIFY:  ginkgo --label-filter="journey" results
```

#### Acceptance Criteria
- [ ] V-1..V-8: all PASS
- [ ] JUnit XML captured in `.claude/knowledge-base/reviews/`
- [ ] Audit entry in log.md

#### DoD
- [ ] Velero gap CLOSED
- [ ] Tests GREEN
- [ ] Cost-budget tracked

---

## Phase 7: Secrets Posture + SOPS Adoption

**Objective:** Per D6 — adopt SOPS for secrets in git. Eliminate `/tmp/`
secret writes. ESO stays as runtime sync.

### T7.1 — Install SOPS + age + helm-secrets plugin

#### Objective
Tooling presence + CI integration.

#### Files to edit
```
Taskfile.yml — task install:sops
infra/scripts/lib/sops.sh (NEW) — SOPS wrappers
README.md — SOPS quickstart
.github/workflows/lint.yml — add sops lint
.github/workflows/cell2-soaks.yml — inject SOPS_AGE_KEY env (EC-19)
.github/workflows/drill-l2-scale.yml — same (EC-19)
.github/workflows/pulumi-reconstruction-drill.yml — same (EC-19)
.github/workflows/ci.yml — same for any helmfile sync step (EC-19)
infra/tests/check-ci-sops-key_test.sh (NEW) — hermetic audit
```

#### Tasks
1. Document install via brew/apt
2. Add Taskfile target
3. Add helm-secrets plugin install to bootstrap

#### TDD
```
RED:     test_sops_binary_on_path()
RED:     test_age_binary_on_path()
RED:     test_helm_secrets_plugin_installed()
RED:     test_ci_workflows_have_sops_age_key_secret() — EC-19: grep .github/workflows/ for SOPS_AGE_KEY env injection
RED:     test_ci_workflows_helmfile_sync_can_decrypt() — EC-19: CI helmfile sync step has SOPS_AGE_KEY before invocation
GREEN:   Install + Taskfile + CI workflow age-key wiring
VERIFY:  task install:sops && which sops && helm secrets --version + bash infra/tests/check-ci-sops-key_test.sh
```

#### Acceptance Criteria
- [ ] Tools installed
- [ ] Documented
- [ ] Pass: hermetic test

#### DoD
- [ ] Tooling ready
- [ ] Tests GREEN

### T7.2 — Encrypt existing committed secrets (audit + migrate)

#### Objective
Audit repo for plaintext secrets in git. Encrypt with SOPS.

#### Files to edit
```
infra/secrets/<env>/<service>.yaml (NEW — SOPS-encrypted)
infra/scripts/check-secrets-plaintext.sh (NEW hermetic) — grep audit
.sops.yaml (NEW) — SOPS config + age recipients
```

#### Tasks
1. Audit: `grep -rE "BEGIN.*PRIVATE KEY|api[_-]?key|secret[_-]?key" infra/ docs/ | grep -v "_test\|README\|\.md"`
2. Identify true secrets
3. Encrypt + commit
4. Delete plaintext versions

#### TDD
```
RED:     test_no_plaintext_private_keys() — grep audit returns 0
RED:     test_sops_yaml_present()
RED:     test_age_recipient_keys_present()
RED:     test_sops_decrypt_with_each_recipient() — EC-10: per-recipient validation
RED:     test_sops_path_rules_isolate_envs() — EC-24: .sops.yaml creation_rules per path (infra/secrets/prod/* needs prod-key; infra/secrets/drill/* needs drill-key); dev key MUST NOT decrypt prod files
GREEN:   Migrate + multi-fingerprint path rules
VERIFY:  bash infra/tests/check-secrets-plaintext_test.sh
```

**EC-24 documentation:** `.sops.yaml` deve usar `creation_rules` por
path para isolar age keys por env:
```yaml
creation_rules:
  - path_regex: ^infra/secrets/prod/.*
    age: <prod-recipient-key>
  - path_regex: ^infra/secrets/(drill|dev|standalone.*)/.*
    age: <non-prod-recipients>
```
Operator com dev key NÃO pode decryptar prod secrets — isolation matches
DO project boundary. Initial implementation: single key for non-prod;
prod key generation + key-rotation cadence documented em Sprint 12+
follow-up.

#### Acceptance Criteria
- [ ] No plaintext secrets in git
- [ ] SOPS config in place
- [ ] Pass: secrets audit script

#### DoD
- [ ] Audit complete
- [ ] Migration complete
- [ ] Tests GREEN

### T7.3 — Eliminate /tmp secret writes in bootstrap scripts

#### Objective
Per session 2026-05-20: `infra/scripts/drill-helmfile-bootstrap.sh:732`
writes `SERVICE_AUTH_SECRET` to `/tmp/`. Replace with stdin / mounted Secret.

#### Files to edit
```
infra/helmfile/scripts/bootstrap-helmfile.sh — replace /tmp/ write with stdin pipe to kubectl
infra/helmfile/scripts/lib/secrets.sh (NEW) — secure secret handling
```

#### Tasks
1. Audit ALL `> /tmp/.*secret\|token\|key` in scripts
2. Replace with `echo "$VAR" | kubectl create secret --from-file=key=/dev/stdin`
3. Or via SOPS encrypt + ESO sync

#### TDD
```
RED:     test_no_tmp_secret_writes() — grep scripts for `> /tmp/.*secret`
GREEN:   Refactor
VERIFY:  bash infra/tests/check-no-tmp-secrets_test.sh
```

#### Acceptance Criteria
- [ ] Zero `/tmp/` secret writes
- [ ] Pass: secrets handling audit

#### DoD
- [ ] /tmp eliminated
- [ ] Tests GREEN

### T7.4 — Document Infisical re-evaluation triggers

#### Objective
Per D6: document WHEN to re-evaluate Infisical.

#### Files to edit
```
.claude/knowledge-base/decisions/2026-05-20-D6-infisical-defer-sops-adopt.md — add triggers
.claude/knowledge-base/strategy/secrets-posture-2026-05.md (NEW)
```

#### Tasks
1. List triggers (customer-managed envs, compliance audit, ≥3 secrets incidents)
2. Document evaluation framework
3. Set reminder (calendar / CI cron alert)

#### Acceptance Criteria
- [ ] D6 ADR includes triggers
- [ ] Strategy doc present

#### DoD
- [ ] Re-evaluation framework documented

---

## Phase 8: Final Validation

**Objective:** Ginkgo full no drill com novos fixes → ≥320 PASS / 0
false-negative SKIPs. Dogfood-drill 11/11. Chaos-test-drill 10/10.

### T8.0 — Mandatory teardown + fresh drill provision (EC-17)

#### Objective
ANTES de T8.1, garantir que o drill state está PRISTINE. Phases 1-7
podem deixar state polluído (URN remappings de T3.2.5, partial state
de aborted runs, residuals de migration testing). Validation contra
state polluído passa falsamente — não representa "fresh prod-identical".

#### Evidence
EC-17: T8.1 step 1 originalmente dizia "Provision drill via unified
Pulumi (T3.3)" mas não declarava teardown PRIMEIRO. Drill stack
sobrevivente da Phase 3 migration pode ter ZeroDay residuals.

#### Files to edit
```
None — operational step using existing teardown infrastructure
```

#### Deep file dependency analysis
- Consome `infra/scripts/dr/teardown-validator.sh` (post-Phase-4 rename)
- Output: ZERO drill residuals confirmed via doctl
- Output: cost-stop timestamp logged

#### Deep Dives
- Sequence:
  1. `cd infra/pulumi && pulumi destroy --stack drill --yes` (canonical Phase 3 path)
  2. `bash infra/scripts/dr/teardown-validator.sh` (force-delete residuals + audit)
  3. `doctl kubernetes cluster list | grep loopzero-drill` returns empty
  4. `doctl databases list | grep loopzero-drill` returns empty
  5. `doctl vpcs list | grep loopzero-drill` returns empty
  6. `cd infra/pulumi && pulumi up --stack drill --yes` (FRESH provision)
  7. Bootstrap via Phase 4 canonical script
  8. Proceed to T8.1

#### Tasks
1. Pulumi destroy drill stack
2. Run teardown validator (residual sweep)
3. Verify ZERO residuals via 3 doctl queries
4. Capture audit trail in `.claude/knowledge-base/reviews/drill-post-migration/`
5. ONLY THEN proceed to T8.1

#### TDD
```
RED:     test_drill_doks_residuals_zero() — post-teardown, kubectl cluster list empty
RED:     test_drill_db_residuals_zero() — doctl databases list empty
RED:     test_drill_vpc_residuals_zero() — doctl vpcs list empty
RED:     test_pulumi_drill_state_empty() — pulumi stack output --stack drill empty
GREEN:   Run teardown + verify
REFACTOR: None
VERIFY:  bash infra/tests/check-drill-teardown-residuals_test.sh
```

#### Acceptance Criteria
- [ ] ZERO drill residuals (clusters, DBs, VPCs)
- [ ] Audit trail captured
- [ ] Cost-stop timestamp logged
- [ ] Pass: hermetic residuals test

#### DoD
- [ ] Drill state PRISTINE
- [ ] Ready for T8.1 fresh provision

### T8.1 — Run Ginkgo full suite on drill (post-migration)

#### Objective
Validate ALL Phase 1-7 changes work end-to-end on drill.

#### Files to edit
```
.claude/knowledge-base/reviews/drill-2026-05-XX-post-migration/REPORT.md (NEW)
.claude/knowledge-base/reviews/drill-2026-05-XX-post-migration/ginkgo-full.xml (NEW)
```

#### Tasks
1. Provision drill via unified Pulumi (T3.3)
2. Bootstrap via canonical scripts (T4.3)
3. Run `LOOPZERO_TEST_ENV=drill go test --keep-going --timeout=4h ./infra/tests/e2e/`
4. Compare results vs 2026-05-20 baseline (189 PASS / 36 FAIL / 117 SKIP of 342)
5. Target: ≥320 PASS, 0 false-negative SKIPs (Velero now PASSES)

#### TDD
```
RED:     test_drill_executed_count_ge_330()
RED:     test_drill_passed_count_ge_320()
RED:     test_drill_velero_journey_8_of_8_pass()
GREEN:   Iterate fixes if needed
VERIFY:  bash infra/tests/check-ginkgo-drill-post-migration_test.sh
```

#### Acceptance Criteria
- [ ] Executed >= 330 of 342 (zero false-negative SKIPs)
- [ ] Passed >= 320
- [ ] Velero Journey 12: 8/8 PASS
- [ ] JUnit XML captured

#### DoD
- [ ] Full suite GREEN
- [ ] Baseline updated

### T8.2 — Run dogfood-drill skill end-to-end

#### Objective
Per /dogfood-drill skill: 11/11 journeys PASS on drill.

#### Tasks
1. Invoke /dogfood-drill skill
2. Capture JUnit + report
3. Verify Rule 3 (no internal terms)

#### TDD
```
RED:     test_dogfood_drill_11_of_11_pass()
RED:     test_rule_3_zero_violations()
GREEN:   Iterate
VERIFY:  bash infra/tests/check-dogfood-drill_test.sh
```

#### Acceptance Criteria
- [ ] 11/11 journeys PASS
- [ ] Zero Rule 3 violations
- [ ] Report in `.claude/knowledge-base/reviews/`

#### DoD
- [ ] Dogfood-drill GREEN

### T8.3 — Run chaos-test-drill end-to-end

#### Objective
10/10 chaos scenarios PASS on drill.

#### Tasks
1. Invoke /chaos-test-drill skill
2. Capture results
3. Validate SLO hypothesis held for each

#### TDD
```
RED:     test_chaos_10_scenarios_pass()
GREEN:   Iterate
VERIFY:  bash infra/tests/check-chaos-drill_test.sh
```

#### Acceptance Criteria
- [ ] 10/10 scenarios PASS
- [ ] All hypotheses held

#### DoD
- [ ] Chaos drill GREEN

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | 13 `drill-*` scripts (parallel impl) | T4.1, T4.2, T4.3, T4.4, T4.5 | Consolidate to canonical scripts in `infra/helmfile/scripts/` with `--env` flag |
| 2 | 4 `loopzero-*-operator-drill/` directories | T1.1 | Move values to `infra/helmfile/values/drill/` |
| 3 | 9 hardcoded `localhost:4455` in canonical kratos values | T2.3 | Replace with `{{ .Values.uiBaseUrl }}` placeholder + schema |
| 4 | 47 inline drill conditionals | T2.1, T2.2 | Layered values per chart per env |
| 5 | 5 Pulumi projects (2 colliding name) | T3.1, T3.2, T3.3, T3.4 | Consolidate to 1 + componentize + state migration |
| 6 | Pulumi-drill 22KB monolithic index.ts | T3.1, T3.3 | Component Resources + stack composition |
| 7 | 6 bootstrap-cell-*-cc scripts | T4.2 | 1 parametrized script |
| 8 | DRILL_KUBECONFIG_DIR env var (drill-specific) | T5.1, T5.2, T5.3 | Replace with LOOPZERO_TEST_ENV (k3d/drill/prod) |
| 9 | Hardcoded `k3d-loopzero-mgmt` in scripts | T4.3, T4.4, T4.5 | Resolve via `--env` flag |
| 10 | /tmp/ secret writes | T7.3 | stdin pipe / mounted Secret / SOPS |
| 11 | Velero bucket missing in drill | T6.1 | LoopZeroVeleroBucket Component Resource |
| 12 | Velero schedules subchart not materializing | T6.2 | Fix template emission |
| 13 | Journey 12 Velero 8 false-negative SKIPs | T6.3, T8.1 | Verify 8/8 PASS post-migration |
| 14 | No secrets-in-git encryption | T7.1, T7.2 | SOPS + age adoption |
| 15 | Infisical evaluation pending | D6, T7.4 | DEFER decision documented + re-evaluation triggers |
| 16 | Helmfile no fail-fast on missing env keys | T2.4 | requiredKeys: true + per-chart schemas |
| 17 | Backward compat ambiguity | D8 | Pre-release: allow break, document mapping |
| 18 | Sponsor not in loop on T3 change | T0.1 | 9 ADRs + MEET ratification |
| 19 | Ginkgo drill 36 FAIL / 117 SKIP baseline | T8.1 | Target ≥320 PASS / 0 false-negative SKIP |
| 20 | Dogfood-drill not validated post-fix | T8.2 | 11/11 PASS |
| 21 | Chaos-test-drill not validated post-fix | T8.3 | 10/10 PASS |
| 22 | pulumi-shared/ empty dir | T3.4 | DELETE |
| 23 | Operator workflow changes undocumented | T0.1, T4.1, CHANGELOG | Mapping doc + CHANGELOG entries |
| 24 | values.schema.json missing per chart | T2.3, T2.5 | Add for kratos (+ others optional) |
| 25 | CI workflows referencing legacy paths | T3.4, T4.5 | Update all workflow refs |
| 26 | EC-1: Pulumi state URN regex corruption risk | T3.2.5 (NEW) | Dry-run on isolated drill-test stack with URN-count invariant |
| 27 | EC-2: ArgoCD/CI refs to old Helm paths not audited | T1.0 (NEW) | Cross-system path inventory ahead of T1.1-T1.4 |
| 28 | EC-3: 11 LIVE bug patches preservation in T4.3 | T4.3 TDD expanded | Per-bug marker grep + preservation hermetic tests |
| 29 | EC-4: Cell→cluster mapping not explicit in T4.2 | T4.2 Deep Dives | Explicit mapping table local/drill/prod + B82 anti-regression |
| 30 | EC-5: localhost hardcodes beyond kratos | T2.3 Step 0 | Global inventory of localhost: hardcodes + per-chart placeholder decision |
| 31 | EC-6: T2.4 requiredKeys ordering (was step 4, must be LAST) | T2.4/T2.5 swap | Reordered: T2.4 regression FIRST, T2.5 requiredKeys LAST |
| 32 | EC-7: Helmfile template non-determinism (uid, timestamps) | T2.4 TDD | Sanitizer before snapshot diff |
| 33 | EC-8: uiBaseUrl trailing slash | T2.3 TDD | Schema regex `^https?://[^/]+$` |
| 34 | EC-9: standalone-multi env coverage in regression | T2.4 TDD | Explicit test_standalone_multi_template_matches_pre_phase2_snapshot |
| 35 | EC-10: SOPS age recipient rotation | T7.2 TDD (extend) | Per-recipient decrypt test |
| 36 | EC-11: Velero schedule order vs CRDs | T6.2 TDD | needs: graph test (CRDs before Schedule release) |
| 37 | EC-12: Pulumi state backup verifiable | T3.2.5 | Backup decryptable test |
| 38 | EC-16: `--env standalone-multi` ausente (5 envs, plan listava 4) | T4.2 args + TDD; T4.3 Tasks step 3+5; T5.3 task targets | Adicionar standalone-multi a TODAS as enumerações de envs |
| 39 | EC-17: T8.1 sem prereq teardown explícito | T8.0 (NEW) | Mandatory teardown + zero-residuals gate antes de T8.1 fresh provision |
| 40 | EC-18: Helmfile values merge precedence implícita | T2.5 TDD | Test that explicit values/<env>/<chart>.yaml OVERRIDES chart's own values-<env>.yaml |
| 41 | EC-19: SOPS age key não wired em CI workflows | T7.1 files-to-edit + TDD | Inject SOPS_AGE_KEY env em cell2-soaks/drill-l2-scale/pulumi-reconstruction/ci |
| 42 | EC-20: values.schema.json não enforced sem `helm --validate` | T2.5 TDD | Test helmfile sync invokes `helm template --validate` |
| 43 | EC-21: `task e2e:<env>` cobertura incompleta (2 of 5 envs) | T5.3 expanded | 5 task targets explicit incluindo standalone-multi |
| 44 | EC-22: Pulumi encryption salt drift durante migration | T3.2.5 TDD | Test encryptionsalt preserved or remapped; secrets re-decryptable |
| 45 | EC-23: Deprecation window length para LOOPZERO_TEST_ENV undefined | T5.1 Tasks step 5 | 1 sprint (~2 weeks) explícito; counter-based gate |
| 46 | EC-24: Multi-fingerprint SOPS para prod isolation | T7.2 TDD + doc | Per-path creation_rules em .sops.yaml; dev key NÃO decrypta prod |
| 47 | EC-25: CHANGELOG.md hygiene per phase | Global DoD | Explicit checkbox: each Phase 0-8 tem entry em [Unreleased] |

**Coverage: 47/47 gaps covered (100%)** — v1.2 expanded post edge-case review Pass 2

## Global Definition of Done

- [ ] All 8 phases completed
- [ ] All tests passing (Go + TypeScript + shell hermetic)
- [ ] Zero lint warnings (golangci-lint + ESLint + shellcheck + markdownlint)
- [ ] Backward compatibility broken intentionally per D8; mapping documented
- [ ] code-audit checks passing across all modified packages
- [ ] **`/cross-validation 2026-05-20-infra-standardization-and-secrets` PASSED** — zero CRITICO, zero GRAVE
- [ ] **EC-25: CHANGELOG.md hygiene** — each phase has a corresponding entry in `CHANGELOG.md` `[Unreleased]` section (one entry per Phase 0-8), referencing PR/issue, written for consumer (not git-log dump per CLAUDE.md Rule 6 mandate)
- [ ] Plan-specific criteria:
  - [ ] ZERO `infra/scripts/drill-*` scripts
  - [ ] ZERO `infra/helm/loopzero-*-operator-drill/` directories
  - [ ] ZERO `{{ if eq .Environment.Name "drill" }}` in `.gotmpl` files
  - [ ] ZERO `http://localhost:` in `infra/helm/*/values.yaml` canonical
  - [ ] ONE Pulumi project `infra/pulumi/` with stacks dev/drill/prod
  - [ ] Ginkgo drill: ≥320 PASS, zero false-negative SKIPs
  - [ ] Dogfood-drill: 11/11 GREEN
  - [ ] Chaos-test-drill: 10/10 GREEN
  - [ ] D6 (Infisical defer) ratificada via meeting
  - [ ] SOPS adopted + plaintext secrets gone
- [ ] **Evidence** (CONDITIONAL — only if plan is score-affecting):
  - [x] N/A — este plano é REORGANIZAÇÃO arquitetural pré-release; não move maturity score per se (mas reduz risco de regressão drift-induced)
- [ ] **`/dogfood full` PASSED** (Dogfood Gate)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.**

After Phase 8 completes:

```bash
# 1. Fresh local infrastructure
task teardown && task setup && tilt up
task doctor

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean local k3d infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to plan scope
- [ ] Prova de Vida passes for every deploy
- [ ] Prova de Morte passes for every delete
- [ ] No regressions vs pre-plan baseline (cleanroom dogfood 12/12)
- [ ] Observability check passes (logs, metrics, CRs)
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If S0/S1: fix → re-run `/dogfood full` → repeat until clean.

S2-S4: documented but do not block.

### Additional Drill Gate (after local dogfood)

```bash
/dogfood-drill   # 11/11 journeys on drill
/chaos-test-drill --full   # 10/10 scenarios on drill
```

Both must PASS per T8.2 + T8.3 acceptance.

---

## Risk Register

| Risk | Phase | Mitigation |
|------|-------|-----------|
| Pulumi state migration corrupts (EC-1) | 3 | T3.2.5 dry-run on isolated drill-test stack with URN-count invariant; backup pre-migration; sponsor sign-off; preview shows 0 changes before commit |
| Helm template regression (golden diff) | 1, 2 | Snapshot tests in T1.5, T2.4; sanitizer for uid/timestamps (EC-7) |
| CI/ArgoCD workflow break (path refs stale, EC-2) | 1, 3, 4 | T1.0 NEW cross-system audit; update workflow YAMLs in same commit |
| Operator script confusion (rename) | 4 | T4.1 mapping doc + CHANGELOG entries; T4.2 explicit cell→cluster table (EC-4) |
| LIVE bug patches lost in T4.3 refactor (EC-3) | 4 | Per-bug marker comments + 11 hermetic preservation tests |
| `uiBaseUrl` scope creep (EC-5) | 2 | T2.3 Step 0 global localhost inventory before refactor |
| T2.5 requiredKeys breaks intermediate envs (EC-6) | 2 | T2.4 regression GREEN as prereq; T2.5 is LAST in Phase 2 |
| Cost overrun (drill iterations) | 6, 8 | Budget approved $30; auto-teardown watchdog |
| Test scaffolding regression | 5 | T5.1 backward compat fallback during deprecation window |
| SOPS adoption blocking dev workflow | 7 | Per-dev age keys; CI uses different key; per-recipient decrypt test (EC-10) |
| **DOCUMENTED ACCEPTED RISKS:** |  |  |
| Git history retains plaintext secrets (EC-13) | 7 | filter-repo destructive; rotate exposed credentials instead; private repo |
| Sponsor approval delay (EC-14) | 0 | Document SLA expectation; escalate if >2 weeks |
| Live drill conflicts with migration (EC-15) | 1, 2, 3 | Document in CHANGELOG: "do not provision drill stack between Phase 1 start and Phase 3 completion" |
| EC-16 standalone-multi env omission | 4 | Fixed in v1.2 — all 5 envs explicit in T4.2/T4.3/T4.4/T5.3 |
| EC-17 polluted-state validation false-pass | 8 | Fixed in v1.2 — T8.0 NEW mandatory teardown gate |
| EC-18 silent helmfile values override | 2 | T2.5 TDD enforces explicit precedence; documented in dev notes |
| EC-19 CI helmfile sync sem age key | 7 | T7.1 wires SOPS_AGE_KEY em 4 workflows + hermetic test |
| EC-20 schema not enforced sem --validate | 2 | T2.5 TDD enforces --validate flag in helmfile invocations |
| EC-22 Pulumi encryption salt drift | 3 | T3.2.5 TDD test_encryption_salt_preserved_or_remapped |
| EC-23 deprecation window undefined | 5 | 1 sprint (~2 weeks) explícito em T5.1 step 5; counter-gate |
| EC-24 single-key SOPS leaks prod to dev | 7 | T7.2 TDD per-path creation_rules; prod isolated key Sprint 12+ |
| EC-25 CHANGELOG.md drift per phase | All | Global DoD checkbox per phase |

## Cost Estimate

| Item | Estimated cost |
|------|----------------|
| Drill iterations (6 × $3) | $18 |
| Cell2-soaks CI runs (3 × $0.50) | $1.50 |
| L2 validation runs (2 × $5) | $10 |
| **Total cloud cost** | **~$30** |
| Engineering time (8 weeks @ 1 FTE) | (out of budget scope) |

## Migration Steps for Operators

Per D8: pre-release allows break. Operator workflow changes documented:

| Old command | New command |
|-------------|-------------|
| `bash infra/scripts/drill-helmfile-bootstrap.sh` | `bash infra/helmfile/scripts/bootstrap-helmfile.sh --env drill` |
| `bash infra/scripts/drill-mint-jwt.sh` | `bash infra/helmfile/scripts/seed-jwt.sh --env drill` |
| `bash infra/scripts/bootstrap-cell-1-cc-drill.sh` | `bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 1 --env drill` |
| `cd infra/pulumi-drill && pulumi up` | `cd infra/pulumi && pulumi up --stack drill` |
| `DRILL_KUBECONFIG_DIR=/tmp/drill-kc go test ./...` | `LOOPZERO_TEST_ENV=drill go test ./...` |

CHANGELOG entry per phase + operator runbook update.

## References

### Industry research (Section 4 of strategy doc)
- [Helmfile official](https://helmfile.readthedocs.io/en/latest/writing-helmfile/)
- [Helmfile layering](https://oneuptime.com/blog/post/2026-01-30-helmfile-layering/view)
- [ArgoCD multi-env Helm](https://oneuptime.com/blog/post/2026-02-26-argocd-helm-values-multiple-environments/view)
- [Pulumi organizing projects + stacks](https://www.pulumi.com/docs/iac/guides/basics/organizing-projects-stacks/)
- [Pulumi structuring projects](https://www.pulumi.com/blog/iac-recommended-practices-structuring-pulumi-projects/)
- [Pulumi Nx monorepo](https://www.pulumi.com/blog/nx-monorepo/)
- [Pulumi golden paths](https://www.pulumi.com/blog/golden-paths-infrastructure-components-and-templates/)
- [ARMO local K8s tools](https://www.armosec.io/blog/best-local-kubernetes-tools/)
- [BLUESHOE k3d vs kind](https://www.blueshoe.io/blog/minikube-vs-k3d-vs-kind-vs-getdeck-beiboot/)
- [DigitalOcean DOKS vs EKS](https://www.digitalocean.com/resources/articles/kubernetes-digitalocean-vs-aws)

### Secrets management research (D6)
- [Top 5 Secrets Management Tools 2026](https://guptadeepak.com/top-5-secrets-management-tools-hashicorp-vault-aws-doppler-infisical-and-azure-key-vault-compared/)
- [Open Source Secrets Management DevOps 2026](https://infisical.com/blog/open-source-secrets-management-devops)
- [Secrets management mess 2026](https://blog.antnsn.dev/2026-p3-secrets-management-mess)
- [HashiCorp Vault Alternatives](https://infisical.com/blog/hashicorp-vault-alternatives)
- [Kubernetes Secrets Management 2026](https://infisical.com/blog/kubernetes-secrets-management)

### Project memory references
- `feedback_velero_must_be_in_drill` (binding rule)
- `feedback_anti_lying_workaround_pattern`
- `feedback_golden_rules_cto_mandate` (Rule #0)
- `feedback_no_tribal_knowledge`
- `feedback_no_legacy_after_local_delete`
- `reference_drill_phase2_session_2026_05_18`

### Session evidence
- `.claude/knowledge-base/strategy/infra-reorganization-2026-05-20.md` (deep research)
- [`.claude/knowledge-base/reviews/drill-2026-05-20/REPORT.md`](../../reviews/density-audit-2026-05-25/REPORT.md) (LIVE session)
- `.claude/knowledge-base/concepts/operations/finding-2026-05-20-velero-bsl-schedules-drill-gap.md`
