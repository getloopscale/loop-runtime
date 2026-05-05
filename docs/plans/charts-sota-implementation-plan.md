# Plan: Charts SOTA FAANG-Grade Implementation

> **Version 1.0** — Implementa todos os 22 pontos identificados na pesquisa SOTA do domínio `charts/` (5 quick-wins + 6 Sprint 9 targets + 6 Post-Launch + 5 NEW gates CH-G15-G19), elevando o score SOTA de 7.2/10 para 9.0/10. O plano segue 7 fases ordenadas por dependência: governance → quick-wins → library expansion → CI hardening → schema/chartsnap → SBOM → documentação → post-launch. Cada task tem TDD estrito (helm-unittest red→green), evidência específica do código atual com arquivo e linhas, e DoD verificável. O resultado é uma camada `charts/` em paridade FAANG (Bitnami, Sigstore, Crossplane) com drift detection automatizada, supply-chain provenance completo, multi-K8s matrix testing, e zero ghost charts.

## Context

**Estado atual** (snapshot 2026-05-04 via `docs/technical/charts/improvement-roadmap.md`):

- Score SOTA: 7.2/10 com 50/75 dimensões avaliadas.
- 12/15 charts deployáveis com `values.schema.json` Draft-07 + `helm lint --strict` em CI (FORÇA).
- cosign keyless + key-based dual-signing operacional (`.github/workflows/helm-charts.yml` linhas 251-291) com Fulcio + Rekor + verify step (FAANG-bar, `MEET-2026-05-03-063 condition #2`).
- `loopzero-common` library chart com 16 helpers adotados pelos 9 charts deployáveis (FORÇA).
- 4 binários de operator split em charts isolados pós-ADR-0023 (FORÇA).

**Defeitos concretos detectados:**

1. **Ghost charts**: `loopzero-platform/Chart.lock` linhas 23-25 referenciam `loopzero-controller` que não tem `Chart.yaml` (apenas vendoreia `charts/loopzero-controller/charts/loopzero-common-0.2.0.tgz`). Mesmo defeito em `charts/loopzero-tenant-controller/`. Dead code pós-split.
2. **Drift regressivo de policies**: `charts/loopzero-admission-policies/templates/` tem 6 templates; `infra/helm/admission-policies/templates/` tem 11. Faltam no published: `cluster-image-policy.yaml` (cosign warn-mode), `policy-controller-alerts.yaml`, `tenant-namespace-alerts.yaml`, `vap-no-runtime-image-mutations.yaml`, `vap-tenant-namespace.yaml`. Inversão da expectativa (production stricter que distributable).
3. **Sem drift detector** entre `infra/helm/` e `charts/` para overlaps reais (`loopzero-api`, `loopzero-cloud`) e conceituais (admission-policies). Apenas CRDs têm `crd-sync` job.
4. **CRD drift comparator é só nominal** (`metadata.name`) — mudanças em `spec.versions[].schema` passariam silenciosamente.
5. **helm-unittest cobertura 5/13 charts** — `loopzero-cloud`, `loopzero-crds`, `loopzero-dashboard`, `loopzero-monitoring`, `loopzero-admission-policies`, `loopzero-build-cwts`, `loopzero-platform`, `loopzero-common` sem unit tests.
6. **loopzero-common faltam 6 helpers Bitnami-style** identificados como duplicação: `tplvalues.render`, `kubeVersion`, `checksumAnnotations` (5 deployments duplicam), `imagePullSecrets`, `secrets.name`, `tmpVolumeMount` (5 deployments duplicam).
7. **Sem `chart-testing (ct)` install/upgrade matrix** — CI faz `helm template + kubeconform` mas nunca instala em Kind.
8. **Sem matriz K8s** — `kubeconform --kubernetes-version 1.31.0` único.
9. **Sem `loopzero-platform/values.schema.json`** — typos em `global.registry` ou `<sub-chart>.enabled` passam silenciosamente.
10. **Sem chartsnap** — drift agregado em umbrella não detectável.
11. **Sem SBOM attestation por chart** — container images têm syft SBOM (Sprint 6); charts ficam atrás.
12. **loopzero-monitoring naming colide** — chart é só PrometheusRules+dashboards, não stack.
13. **Polaris audit em `continue-on-error: true`** baseline-first (MEET-062 condition #1).
14. **Helm v3.14.4** pinada — Helm 3.18.5 endureceu schema validation; falta plano de upgrade.
15. **Sem helm-diff em CI gate** (apesar de prereq em CLAUDE.md).
16. **Sem helm-drift cron** para detection de drift cluster-real.

**Evidência adicional** consultada:
- [docs/technical/charts/improvement-roadmap.md](../technical/charts/improvement-roadmap.md) — 226 linhas com 5 quick-wins + 6 Sprint 9 + 6 Post-Launch.
- [docs/technical/charts/sota-gates.md](../technical/charts/sota-gates.md) — 19 gates (14 status + 5 NEW).
- `.github/workflows/helm-charts.yml` linhas 1-340.
- `charts/loopzero-platform/Chart.lock` (deps com sha256 digest header).
- `charts/loopzero-common/templates/_helpers.tpl` (357 linhas, 16 helpers).
- 8 buscas web SOTA: Bitnami common, helm-unittest, JSON Schema strict, cosign OCI, OCI deps, Crossplane, chart-testing, helm-diff (sources catalogados em `docs/technical/charts/research-basis.md`).

## Objective

**"Done" = score SOTA 9.0/10 com todos os 22 pontos da pesquisa fechados, evidenciados por CI green, dogfood E2E pass e gates CH-G01 a CH-G19 todos com status MET.**

Metas mensuráveis:

1. **Zero ghost charts**: `helm dependency update charts/loopzero-platform` produz Chart.lock 100% resolvíveis sem warnings.
2. **Zero drift regressivo**: `charts/loopzero-admission-policies` é superset de `infra/helm/admission-policies` (count + content match).
3. **Drift detection automatizada**: `infra/scripts/audit-helm-drift.sh` + step CI rodando em cada PR; drift conhecido documentado em ADR.
4. **CRD drift semântico**: `crd-sync` job compara `spec.versions[].schema` via `yq`, não apenas `metadata.name`.
5. **helm-unittest 13/13 charts deployáveis** (excluindo legacy ghost que vão ser deletados).
6. **loopzero-common v0.3.0 com 22 helpers** (16 atuais + 6 novos: tplvalues.render, kubeVersion, checksumAnnotations, imagePullSecrets, secrets.name, tmpVolumeMount).
7. **chart-testing (ct) install + upgrade em K8s 1.30/1.31/1.32 matrix** rodando em CI por PR.
8. **`loopzero-platform/values.schema.json`** validando `global.registry` + sub-chart toggles.
9. **chartsnap baseline** comitado em `charts/loopzero-platform/__snapshot__/`.
10. **SBOM CycloneDX attestation** em todo chart publicado, verificável via `cosign verify-attestation`.
11. **loopzero-monitoring → loopzero-monitoring-rules** renomeado (ou ADR documentando coexistência).
12. **Polaris severity=danger** após baseline reviewed.
13. **Helm v3.18.x** migrado em CI com schema regression validado.
14. **helm-diff gate** em CI para PRs que tocam `charts/`.
15. **helm-drift cron** rodando 6/6h em produção alertando via webhook.

## ADRs

### ADR-0036 (D1) — Quick-wins-first sequencing strategy

- **Decision**: Executar Phase 1 (5 quick-wins) ANTES de qualquer outro trabalho em `charts/`. Cada quick-win é XS-S, reversível, e destrava evidência observável.
- **Rationale**: Os 5 quick-wins têm ROI altíssimo (1-2 sessões cada) e fecham 3 dos 5 NEW gates (CH-G15, CH-G16, CH-G17 partial). Ghost chart deletion é zero-risk; drift admission-policies é 5 commits puros de cópia. Posicionar tarde causaria atraso desnecessário em métricas que viram baseline para outras phases.
- **Consequences**: Permite, ao final do Phase 1, dizer "score saiu de 7.2 para ~7.8 em 1 sprint". Habilita medição de progresso. Não causa débito técnico se Phase 2+ atrasar.

### ADR-0037 (D2) — Selective Bitnami common port (5-6 helpers, não 40)

- **Decision**: `loopzero-common` vai crescer de 16 para 22 helpers. Portar APENAS os 6 identificados no roadmap por evidência de duplicação real: `tplvalues.render`, `kubeVersion`, `checksumAnnotations`, `imagePullSecrets`, `secrets.name`, `tmpVolumeMount`. Nenhum outro será adicionado especulativamente.
- **Rationale**: Bitnami common 2.x tem 40+ helpers, mas LoopZero precisa só dos que cobrem ≥ 2 caso de uso real no código atual. Adotar todos cria framework opaco (CH-G07 nota), aumenta superfície breaking, atrasa onboarding. YAGNI aplicado: só portar quando há ≥ 2 caso de uso provado.
- **Consequences**: `loopzero-common` permanece menor (~25 helpers cap) e legível. Cada novo helper requer evidência prévia de duplicação. CH-G17 fica MET com cap claro.

### ADR-0038 (D3) — Manter `file://` deps em dev + CI rewrite OCI no publish

- **Decision**: NÃO migrar `charts/loopzero-platform/Chart.yaml` para `oci://...@sha256:...` em dev. Manter `file://` para subcharts internos. CI continua rewriting `file://` → OCI no publish step.
- **Rationale**: Migrar OCI em dev requer auth OCI em cada onboarding (`helm registry login`), quebra air-gapped develop, complica `helm dependency update` local. Status quo já funciona: dev usa file:// (zero auth, fast), release usa OCI (immutable, signed). Crossplane pattern (digest pinning) é overkill para subcharts internos do mesmo monorepo.
- **Consequences**: Onboarding permanece simples. `helm pull oci://.../loopzero-platform` funciona pós-CI rewrite. Documentar dual code path em README do umbrella.

### ADR-0039 (D4) — Multi-K8s matrix policy (3 versões: N-1, N, N+1)

- **Decision**: CI matrix testa K8s 1.30, 1.31, 1.32. Atualizar a cada release K8s upstream para refletir N-1/N/N+1, mantendo DOKS production version (atualmente 1.32.13) como N.
- **Rationale**: Janela de suporte K8s upstream é ~3 versões. Cobrir só N (DOKS atual) deixa upgrade frágil — quebra em N+1 só vira evidente em prod canary. Testar 3 versões cobre janela de upgrade DOKS sem custo CI excessivo (~5min add por versão via `ct install`). Elastic Helm Charts (`helpers/matrix.yml`) e Bitnami GKE/TKG/AKS são referência.
- **Consequences**: CI ~10-15min mais lento (paralelizável via matrix). Evidência de compat antes de prod canary. Bumps de K8s viram upgrade plan amplo.

### ADR-0040 (D5) — SBOM attestation parity com container images Sprint 6

- **Decision**: Cada chart publicado recebe SBOM CycloneDX gerado por `syft` e attestation via `cosign attest --predicate sbom.json --type cyclonedx <REF>`. Sprint 6 já estabeleceu o padrão para imagens; charts replicam.
- **Rationale**: Charts assinados sem SBOM = supply chain incompleto. Bitnami Secure Images shipam SBOM in-toto + provenance. Sigstore project assina release com cosign + Syft + Rekor. LoopZero já tem cosign keyless operacional; adicionar `cosign attest` é incremento marginal (~30s CI) com retorno alto.
- **Consequences**: Charts ganham paridade supply chain. Habilita `cosign verify-attestation` downstream. Sprint 9 ClusterImagePolicy pode opcionalmente requerer attestation.

### ADR-0041 (D6) — Drift detector strategy (advisory → blocking gradual)

- **Decision**: `infra/scripts/audit-helm-drift.sh` + step CI rodando em PR. Inicialmente advisory (warning, não-bloqueante) por 30 dias para baseline; depois bloqueante após review e ADRs documentando drifts intencionais.
- **Rationale**: Bloqueio imediato em CI para drift desconhecido travaria PRs em massa enquanto baseline é estabelecido. Pattern advisory→blocking é o mesmo usado pelo Polaris audit hoje (continue-on-error baseline-first per MEET-062). Permite team formalizar quais drifts são intencionais (ex: loopzero-api dev minimal vs prod full) via ADRs antes de fazer gate.
- **Consequences**: 30 dias de runway para classificar drifts existentes em "intencional" (ADR) vs "regressão" (fix). Após D+30, drift detectado bloqueia merge. Polaris graduation acompanha.

### ADR-0042 (D7) — Ghost chart deletion é XS reversível, fora de governance T2 

- **Decision**: Delete de `charts/loopzero-controller/` e `charts/loopzero-tenant-controller/` é classificado T1 (local cleanup), não T2. Não requer meeting/governance prévio.
- **Rationale**: Esses dirs não têm `Chart.yaml`, não renderizam manifestos, não são referenciados em CI exceto pela entrada ghost no Chart.lock. Deletar é equivalente a remover comentário órfão. ADR-0023 (operator split) já formalizou que monolito legado ia ser removido. Pre-release governance reserva T2 para mudanças com impacto observável; ghost deletion é puro cleanup.
- **Consequences**: Phase 1 task 1.1 procede sem governance gate adicional. Outros tasks em Phase 1 (admission-policies port, drift script) são T2 e seguem governance.

## Dependency Graph

```
Phase 0: Governance Gate
   │
   ▼
Phase 1: Quick Wins (5 tasks, paralelo dentro do phase)
   │  T1.1: Delete ghost charts ──────────────┐
   │  T1.2: Port admission-policies templates ┤ (paralelo)
   │  T1.3: helm:audit-drift script + CI step ┤
   │  T1.4: crd-sync semantic diff ───────────┤
   │  T1.5: helm-unittest 4 charts faltantes ─┘
   │
   ▼
Phase 2: loopzero-common v0.3.0 (sequencial: helpers → tests → migration)
   │  T2.1-T2.6: 6 novos helpers (paralelo)
   │  T2.7: helm-unittest tests para os 6 helpers
   │  T2.8: Migration de consumer charts
   │
   ▼
Phase 3: CI Hardening                  Phase 4: Schema + Chartsnap (paralelo a Phase 3)
   │  T3.1: ct.yaml + ct lint job        │  T4.1: loopzero-platform/values.schema.json
   │  T3.2: Kind cluster + matrix K8s    │  T4.2: chartsnap baseline + CI
   │  T3.3: ct install --upgrade         │  T4.3: helm-diff em CI gate
   │  T3.4: kubeconform multi-version    │
   │  T3.5: helm-unittest auto-loop     │
   │
   └──┬──────────────┬───────────────┘
      ▼              ▼
   Phase 5: SBOM Attestation
   │  T5.1: syft generate SBOM per chart
   │  T5.2: cosign attest --predicate
   │  T5.3: ClusterImagePolicy verify-attestation
   │  T5.4: cosign verify-attestation step
   │
   ▼
Phase 6: Strategic Documentation
   │  T6.1: ADR loopzero-api dual chart drift
   │  T6.2: loopzero-monitoring → loopzero-monitoring-rules
   │  T6.3: README umbrella documenting file://+OCI rewrite
   │
   ▼
Phase 7: Post-Launch (deferred but planned)
   │  T7.1: Polaris severity=danger graduation
   │  T7.2: Helm v3.14.4 → v3.18.x migration
   │  T7.3: Conftest policies for charts
   │  T7.4: helm-drift CI cron
   │  T7.5: Multi-namespace policy testing
```

**Sequencing notes:**

- **Phase 0** (governance) é gate obrigatório antes de qualquer T2/T3 work.
- **Phase 1** quick-wins são paralelizáveis entre si (touchpoints distintos).
- **Phase 2** é sequencial internamente: helpers → tests → migration; mas roda paralelo a Phase 3+4.
- **Phase 3 + Phase 4** podem rodar em paralelo (jobs CI independentes; schema + chartsnap são adições, não mutuamente dependentes).
- **Phase 5** depende de Phase 3 estar GREEN (SBOM piggy-backs no `package-push` job).
- **Phase 6** é low-effort, pode rodar a qualquer momento após Phase 1.
- **Phase 7** marcada como "Post-Launch" — incluída por completude do escopo "todos os pontos" mas executada após GA.

---

## Phase 0: Governance Gate

**Objective:** Submeter as 7 ADRs (D1-D7) ao change-governor + meeting-orchestrator, obter ratificação e validation plan antes de qualquer implementação.

### T0.1 — Submit governance request for all T2/T3 changes in this plan

#### Objective
Levar este plano (especificamente os componentes T2: drift detection, library expansion, schema, SBOM; T3: file:// strategy ADR-0038) para review formal via change-governor → meeting-orchestrator → decision-record-moderator.

#### Evidence
- `.claude/rules/pre-release-governance.md`: T2/T3 changes que tocam Helm chart modifications, CI changes, credential/secret handling REQUIRED meeting.
- ADR-0038 (file:// strategy) é T3 (architecture decision sobre distribution model).
- ADRs D2, D4, D5, D6 são T2 (contracts, pipeline, supply chain).
- D1, D3, D7 são T0/T1.

#### Files to edit
```
docs/meetings/MEET-CHARTS-SOTA-IMPLEMENTATION.md (NEW) — meeting record
docs/meetings/adr/0036-quick-wins-first-sequencing.md (NEW) — D1
docs/meetings/adr/0037-selective-bitnami-common-port.md (NEW) — D2
docs/meetings/adr/0038-file-deps-dev-oci-publish.md (NEW) — D3
docs/meetings/adr/0039-multi-k8s-matrix-3-versions.md (NEW) — D4
docs/meetings/adr/0040-sbom-attestation-parity-charts.md (NEW) — D5
docs/meetings/adr/0041-drift-detector-advisory-blocking.md (NEW) — D6
docs/meetings/adr/0042-ghost-chart-deletion-t1.md (NEW) — D7
```

#### Deep file dependency analysis
- Cada ADR é arquivo standalone, padrão dos 35 ADRs existentes (`0001` a `0035`).
- `MEET-*.md` registra o meeting completo com participantes, posições, decisões.
- Meetings completed habilita Phase 1+ a iniciar (ratificado).

#### Deep Dives
- Submitter: chief-pentester ou change-governor (este plan).
- Required reviewers per CLAUDE.md governance:
  - **production-design-reviewer** (drift detection, multi-K8s matrix → operational fitness)
  - **security-blast-radius-reviewer** (SBOM attestation, file:// strategy → supply chain) — ABSOLUTE veto
  - **change-validation-planner** (test strategy)
  - **platform-security-engineer** (cosign attestation, signing → security) — ABSOLUTE veto
  - **gitops-engineer** (drift detection vs ArgoCD reconcile)
  - **principal-architect** (overall coherence)

#### Tasks
1. Convocar meeting via `/meeting` skill com escopo "charts SOTA implementation, 7 ADRs, T2/T3 review".
2. Cada agent revisor produz position statement.
3. Decision-record-moderator consolida em ADRs 0036-0042.
4. Meeting record salvo em `MEET-CHARTS-SOTA-IMPLEMENTATION.md`.
5. APPROVED status ou redirecionar (block ou amend per ADR-0022).

#### TDD
N/A — governance/decision phase, sem código.

#### Acceptance Criteria
- [ ] 7 ADRs salvos em `docs/meetings/adr/0036-0042-*.md`
- [ ] MEET record contém: participantes, position de cada reviewer, alternativas rejeitadas, decisão final, validation plan
- [ ] Status final: APPROVED com ou sem condições explícitas
- [ ] Conditions são listadas em cada DoD de tasks afetadas
- [ ] Pass: `ls docs/meetings/adr/003{6,7,8,9}-*.md docs/meetings/adr/004{0,1,2}-*.md` retorna 7 arquivos

#### DoD
- [ ] Meeting completed com decisão registrada
- [ ] 7 ADRs com frontmatter (status, date, deciders) corretos
- [ ] Sponsor sign-off (paulohenriquevn@gmail.com)
- [ ] Phase 1 unblocked

---

## Phase 1: Quick Wins (5 tasks paralelos)

**Objective:** Fechar 5 defeitos com ROI altíssimo (1-2 sessões cada). Move score 7.2 → ~7.8 em 1 sprint.

### T1.1 — Delete ghost charts (`loopzero-controller`, `loopzero-tenant-controller`)

#### Objective
Remover dead code referenciado em `loopzero-platform/Chart.lock` que viola CH-G15 (NEW gate).

#### Evidence
- `charts/loopzero-controller/` e `charts/loopzero-tenant-controller/` não têm `Chart.yaml`. Apenas `charts/loopzero-common-0.2.0.tgz` vendoreado. Verificado via `ls charts/loopzero-controller/` (returned: `charts  tests`).
- `charts/loopzero-platform/Chart.lock` linhas 23-25 referenciam `loopzero-controller`.
- `charts/loopzero-platform/values.yaml` linhas 25-26 mantêm `loopzero-controller.enabled: true` + `loopzero-tenant-controller.enabled: true`.
- ADR-0023 (operator split) deletou o monolito legado em T1.5.

#### Files to edit
```
charts/loopzero-controller/                                         — DELETE recursivamente (dir inteiro)
charts/loopzero-tenant-controller/                                  — DELETE recursivamente (dir inteiro)
charts/loopzero-platform/Chart.yaml                                 — remover deps loopzero-controller (linhas 51-54 do trecho dependencies)
charts/loopzero-platform/Chart.lock                                  — regenerar via helm dependency update
charts/loopzero-platform/values.yaml                                 — remover linhas 25-26 (loopzero-controller + loopzero-tenant-controller toggles)
docs/technical/charts/architecture.md                           — atualizar tabela "Mapa Por Chart" removendo entries GHOST
```

#### Deep file dependency analysis
- `charts/loopzero-platform/Chart.yaml`: dependencies array tem 12 entries; remover 1 (`loopzero-controller`); `loopzero-tenant-controller` não estava em Chart.yaml original mas dir vazio existe.
- `charts/loopzero-platform/Chart.lock`: regenerado pelo `helm dependency update` que vai detectar a remoção.
- `charts/loopzero-platform/values.yaml`: linhas 25-26 são toggles — sem dependents downstream.
- CI workflow `helm-charts.yml` step "Update dependencies" (linhas 29-35) faz `helm dependency update` em loop por chart com deps; deletion não quebra (entries some, lock re-emerges válido).
- Nenhum operator/API/CLI Go code referencia esses charts.

#### Deep Dives
- **Invariant pré-deletion**: `loopzero-platform/Chart.yaml` lista 12 deps; após deletion vai listar 11.
- **Invariant pós-deletion**: `helm template charts/loopzero-platform` produz manifestos sem warnings de "missing chart".
- **Edge case**: alguém pode ter uma instância existente com `loopzero-controller.enabled: true` em values custom — não-issue porque o chart não renderizava nada (sem Chart.yaml = sem templates).

#### Tasks
1. `git rm -r charts/loopzero-controller charts/loopzero-tenant-controller`.
2. Editar `charts/loopzero-platform/Chart.yaml` removendo 1 entry de `dependencies` (loopzero-controller).
3. Editar `charts/loopzero-platform/values.yaml` removendo linhas 25-26 (2 toggles).
4. Rodar `helm dependency update charts/loopzero-platform` para regenerar `Chart.lock`.
5. Verificar Chart.lock não tem mais loopzero-controller/loopzero-tenant-controller.
6. Atualizar tabela em `docs/technical/charts/architecture.md` (remover linhas GHOST).
7. Commit com mensagem: `chore(charts): delete ghost charts loopzero-controller, loopzero-tenant-controller (CH-G15)`.

#### TDD
```
RED:     test_no_ghost_charts_in_lock — assert "loopzero-controller" not in $(cat charts/loopzero-platform/Chart.lock)
RED:     test_no_ghost_dirs — assert ! -d charts/loopzero-controller && ! -d charts/loopzero-tenant-controller
GREEN:   git rm -r + Chart.yaml/values.yaml edits + helm dependency update
REFACTOR: None expected (puro delete)
VERIFY:  bash -c '! grep -q "loopzero-controller" charts/loopzero-platform/Chart.lock && ! grep -q "loopzero-tenant-controller" charts/loopzero-platform/values.yaml && [ ! -d charts/loopzero-controller ]'
```

#### Acceptance Criteria
- [ ] `ls charts/loopzero-controller charts/loopzero-tenant-controller` retorna "No such file or directory"
- [ ] `grep -q loopzero-controller charts/loopzero-platform/Chart.lock` retorna 1 (não encontrado)
- [ ] `grep -q loopzero-tenant-controller charts/loopzero-platform/values.yaml` retorna 1
- [ ] `helm dependency update charts/loopzero-platform` exit 0 sem warnings de missing chart
- [ ] `helm template charts/loopzero-platform` exit 0
- [ ] CI `helm-charts.yml` job `lint` permanece GREEN
- [ ] Pass: /code-audit lint check (zero warnings em arquivos editados)

#### DoD
- [ ] All tasks completed
- [ ] CH-G15 status: NOT_MET → MET
- [ ] Architecture.md tabela atualizada
- [ ] CI green
- [ ] PR aprovada e merged

---

### T1.2 — Port 5 missing templates from `infra/helm/admission-policies/` to `charts/loopzero-admission-policies/`

#### Objective
Fechar drift regressivo onde production bootstrap tem postura de segurança mais estrita que published chart. Violação atual de CH-G09.

#### Evidence
- `ls charts/loopzero-admission-policies/templates/` retorna 6 templates.
- `ls infra/helm/admission-policies/templates/` retorna 11 templates.
- Faltam no published: `cluster-image-policy.yaml`, `policy-controller-alerts.yaml`, `tenant-namespace-alerts.yaml`, `vap-no-runtime-image-mutations.yaml`, `vap-tenant-namespace.yaml`.
- `cluster-image-policy.yaml` é o ClusterImagePolicy `loopzero-images-warn` (Sprint 6 ship per CHG-2026-04-26-SPRINT6-IMPL).

#### Files to edit
```
charts/loopzero-admission-policies/templates/cluster-image-policy.yaml          (NEW) — copiar de infra/helm com adaptação para chart values
charts/loopzero-admission-policies/templates/policy-controller-alerts.yaml      (NEW) — idem
charts/loopzero-admission-policies/templates/tenant-namespace-alerts.yaml       (NEW) — idem
charts/loopzero-admission-policies/templates/vap-no-runtime-image-mutations.yaml (NEW) — idem
charts/loopzero-admission-policies/templates/vap-tenant-namespace.yaml          (NEW) — idem
charts/loopzero-admission-policies/values.yaml                                  — adicionar toggles (clusterImagePolicy.enabled, alerts.enabled, etc)
charts/loopzero-admission-policies/values.schema.json                            — estender schema com novos values
charts/loopzero-admission-policies/Chart.yaml                                    — bump version 0.2.0 → 0.3.0 (minor: features adicionadas)
```

#### Deep file dependency analysis
- `infra/helm/admission-policies/templates/<X>.yaml`: source of truth atual; templates são VAPs CEL + ClusterImagePolicy + PrometheusRules.
- `charts/loopzero-admission-policies/values.yaml`: precisa ganhar 4-5 novas keys (cosign cert ref para ClusterImagePolicy, alert grouping interval, etc).
- `charts/loopzero-admission-policies/values.schema.json`: estender properties com novas keys.
- `Chart.yaml` minor bump (não breaking — features adicionadas).

#### Deep Dives
- **ClusterImagePolicy `loopzero-images-warn`** (warn-mode): valida que imagens batem regex de registries permitidos (DOCR + Harbor); falha → warn (não block) per Sprint 6 baseline. Verificar se há referência keyless-pubkey (Fulcio cert OIDC issuer) que precise vir de Sprint 9 quando deny-flip ocorrer.
- **PrometheusRules** policy-controller-alerts e tenant-namespace-alerts: alertas para policy-controller down + tenant namespace breaches. Compatível com Prometheus Operator já instalado.
- **VAP no-runtime-image-mutations**: bloqueia que controllers (não-admin) mutem `image:` em pods já criados — anti-pattern de "deploy via kubectl edit".
- **VAP tenant-namespace**: garante que namespaces `loopzero-tenant-*` têm labels/annotations corretos (`loopzero.io/tenant`, `loopzero.io/cell-id`).

#### Tasks
1. Copiar 5 templates de `infra/helm/admission-policies/templates/` para `charts/loopzero-admission-policies/templates/` com `cp`.
2. Refatorar templates para usarem `loopzero-common` helpers e values toggles em vez de hardcoded values do bootstrap.
3. Estender `charts/loopzero-admission-policies/values.yaml` com novas keys + defaults.
4. Estender `charts/loopzero-admission-policies/values.schema.json`.
5. Bump `version: 0.2.0` → `0.3.0` em `Chart.yaml`.
6. Adicionar `tests/cluster-image-policy_test.yaml` + `tests/alerts_test.yaml` (helm-unittest) cobrindo:
   - ClusterImagePolicy renderiza com `images.glob` correto (regex).
   - PrometheusRule grupos têm `interval: 1m` default.
   - VAPs têm `failurePolicy: Fail` para enforcement.
7. CI roda helm-unittest auto (após T3.5 do Phase 3).
8. Atualizar `docs/technical/charts/architecture.md` removendo nota de drift regressivo.

#### TDD
```
RED:     test_renders_cluster_image_policy — assert spec.images contains "registry.digitalocean.com/loopzero-system/*"
RED:     test_renders_policy_controller_alerts — assert .spec.groups[0].rules[].alert == "PolicyControllerDown"
RED:     test_renders_tenant_namespace_alerts — assert spec.groups exists
RED:     test_renders_vap_no_runtime_image_mutations — assert spec.failurePolicy == "Fail"
RED:     test_renders_vap_tenant_namespace — assert spec.matchConstraints.resourceRules
RED:     test_count_match_bootstrap — count(charts/loopzero-admission-policies/templates) == count(infra/helm/admission-policies/templates)
GREEN:   Port 5 templates + extend values + extend schema
REFACTOR: Extrair common helper se ≥ 2 PrometheusRules duplicarem groupBy/labels
VERIFY:  helm unittest charts/loopzero-admission-policies && helm template charts/loopzero-admission-policies | kubeconform --strict
```

#### Acceptance Criteria
- [ ] `ls charts/loopzero-admission-policies/templates/ | wc -l` retorna 11+
- [ ] `helm template charts/loopzero-admission-policies | grep -c 'kind: ClusterImagePolicy'` ≥ 1
- [ ] `helm template charts/loopzero-admission-policies | grep -c 'kind: PrometheusRule'` ≥ 2
- [ ] `helm template charts/loopzero-admission-policies | grep -c 'kind: ValidatingAdmissionPolicy'` ≥ 8
- [ ] helm-unittest suite cluster-image-policy_test passa
- [ ] CI `helm-charts.yml` job `validate` GREEN
- [ ] CH-G09 status: NOT_MET → MET
- [ ] Pass: /code-audit complexity check (templates ≤ 100 linhas cada)
- [ ] Pass: /code-audit coverage check (tests ≥ 90%)

#### DoD
- [ ] All tasks completed
- [ ] 5 novos templates portados
- [ ] Schema estendido + validate
- [ ] helm-unittest tests passando
- [ ] CI green
- [ ] CH-G09 MET registrado em sota-gates.md

---

### T1.3 — Add `infra/scripts/audit-helm-drift.sh` + CI step

#### Objective
Operacionalizar CH-G16 (NEW gate) com automação. Detecta drift entre `infra/helm/<X>` e `charts/loopzero-<X>` para os 2 overlaps reais (loopzero-api, loopzero-cloud) + 1 conceitual (admission-policies).

#### Evidence
- CH-G11 é "paper gate" (sem automação). Apenas CRDs têm drift detector.
- Drift detectado durante research: loopzero-api (12 vs 3 templates), loopzero-admission-policies (6 vs 11 — fechado em T1.2), loopzero-cloud (não inspecionado).

#### Files to edit
```
scripts/audit-helm-drift.sh                        (NEW) — bash script principal
scripts/audit-helm-drift.test.sh                    (NEW) — testes do script (bats ou plain bash)
.github/workflows/helm-charts.yml                  — adicionar step "Audit drift" no job lint, com continue-on-error: true (advisory por 30 dias per ADR-0041)
docs/runbooks/charts-drift-audit.md                 (NEW) — runbook de uso/interpretação
```

#### Deep file dependency analysis
- `infra/scripts/audit-helm-drift.sh`: standalone, usa `yq`, `find`, `diff`. Não modifica state, só lê.
- `.github/workflows/helm-charts.yml`: adiciona step antes de "Polaris audit" (linha ~80). Step roda script + emite annotation se drift detectado.
- `docs/runbooks/charts-drift-audit.md`: documenta como interpretar output, classify drift como "intencional" (precisa ADR) ou "regressão" (precisa fix).

#### Deep Dives
- **Algorithm:**
  1. Detectar pairs: `(charts/<X>, infra/helm/<X>)` para cada nome em ambos lados.
  2. Detectar pairs conceituais via mapping declarado: `charts/loopzero-admission-policies ↔ infra/helm/admission-policies`, `charts/loopzero-monitoring ↔ infra/helm/monitoring (skip if not exist)`.
  3. Para cada pair, comparar:
     - Template count (mismatch = drift)
     - Template names (set diff)
     - Defaults críticos via `yq`: `securityContext`, `resources`, `priorityClassName`, `image.repository` em values.yaml
  4. Emitir output JSON estruturado com `intentional: bool`, `severity: high|medium|low`, `templates_only_in_published: []`, `templates_only_in_bootstrap: []`, `defaults_diff: {}`.
- **Intentional flagging**: lê `docs/meetings/adr/*-drift-*.md` para detectar drifts pre-aprovados; se ADR matcha pair, marca `intentional: true`.

#### Tasks
1. Escrever `infra/scripts/audit-helm-drift.sh` com lógica acima.
2. Escrever testes bash (bats ou plain) que:
   - Caso A: pair sem drift → exit 0, output empty.
   - Caso B: pair com drift sem ADR → exit 1, output JSON com severity=high.
   - Caso C: pair com drift com ADR → exit 0, output JSON com intentional=true.
3. Adicionar step CI em `helm-charts.yml`:
   ```yaml
   - name: Audit drift between charts/ and infra/helm/
     continue-on-error: true  # advisory per ADR-0041
     run: bash infra/scripts/audit-helm-drift.sh --output-format=github-annotations
   ```
4. Escrever `docs/runbooks/charts-drift-audit.md` com:
   - Como rodar localmente
   - Como interpretar output
   - Workflow para classificar drift como intencional (criar ADR)
   - Workflow para fix de regressão
5. Adicionar entry em `docs/technical/charts/sota-gates.md` linkando para script.

#### TDD
```
RED:     test_no_drift_returns_zero — pair (loopzero-crds, loopzero-crds) [criado fake] sem drift → exit 0
RED:     test_drift_no_adr_returns_one — pair (X, Y) com 5 templates only-in-bootstrap, sem ADR → exit 1
RED:     test_drift_with_adr_returns_zero_intentional — pair (X, Y) drift + ADR docs/meetings/adr/0NNN-drift-X-y.md → exit 0
RED:     test_emits_github_annotations — --output-format=github-annotations emite ::warning::
GREEN:   Implement audit-helm-drift.sh
REFACTOR: Extract YAML diff helpers into separate file if > 200 LOC
VERIFY:  bash infra/scripts/audit-helm-drift.test.sh
```

#### Acceptance Criteria
- [ ] `infra/scripts/audit-helm-drift.sh` exists e é executável (chmod +x)
- [ ] Script detecta 0 drifts após T1.2 (admission-policies fechou)
- [ ] Script detecta drift loopzero-api dual chart (12 vs 3 templates) → marca como "needs ADR"
- [ ] CI step `Audit drift` aparece em `helm-charts.yml`
- [ ] CI step usa `continue-on-error: true` (advisory)
- [ ] Runbook documenta workflow de classificação
- [ ] Pass: shellcheck `infra/scripts/audit-helm-drift.sh` zero warnings
- [ ] Pass: /code-audit complexity check (script ≤ 300 LOC)

#### DoD
- [ ] All tasks completed
- [ ] Script + test + CI step + runbook
- [ ] CH-G16 status: NOT_MET → MET (advisory mode)
- [ ] Plan ADR-0041 referenced
- [ ] CI green com warnings se drift detectado

---

### T1.4 — Extend `crd-sync` job for semantic spec diff

#### Objective
Atualizar CI job `crd-sync` (linhas 215-247 de helm-charts.yml) para comparar `spec.versions[].schema` semanticamente, não apenas `metadata.name`. Fecha gap em CH-G06.

#### Evidence
- `crd-sync` atual compara `count` + `metadata.name` (linha 233-236).
- Mudanças em `spec.versions[].schema.openAPIV3Schema` passariam silenciosamente — bug latente.
- `yq` já está disponível no CI (linha 196).

#### Files to edit
```
.github/workflows/helm-charts.yml                         — substituir step "Check CRD content drift" (linhas 232-247)
scripts/crd-semantic-diff.sh                              (NEW) — script auxiliar para diff comparable
scripts/crd-semantic-diff.test.sh                          (NEW) — testes
```

#### Deep file dependency analysis
- `.github/workflows/helm-charts.yml`: o step "Check CRD content drift" é refatorado para usar `infra/scripts/crd-semantic-diff.sh`.
- `infra/scripts/crd-semantic-diff.sh`: usa `yq` para extrair spec, normaliza (remove status, helm annotations, creationTimestamp), faz diff.

#### Deep Dives
- **Algorithm:**
  1. Para cada `operators/config/crd/bases/loopzero.io_*.yaml`:
     - Extract `spec` via `yq '.spec'`
     - Normalize: remove `status`, remove helm template syntax tokens (`{{ .. }}` substituir por placeholders), sort keys.
  2. Para cada `charts/loopzero-crds/templates/<short>.yaml`:
     - Render via `helm template` (resolve helm syntax)
     - Extract `spec` via `yq '.spec'`
     - Normalize idem.
  3. Compare normalized specs via `diff -u`. Diff != empty → drift detected.
- **Edge case Helm syntax**: chart templates podem ter `{{ if }}` que renderiza condicionalmente; `helm template --set` com defaults para resolver.
- **Edge case status field**: source CRDs em `operators/` podem ter `status:` fields; chart templates não. Normalize remove `status`.

#### Tasks
1. Escrever `infra/scripts/crd-semantic-diff.sh`:
   - Args: `<source-yaml> <chart-template-yaml>`
   - Render chart template via `helm template`
   - Normalize ambos
   - Diff
2. Escrever `infra/scripts/crd-semantic-diff.test.sh` com fixtures:
   - Caso A: source e chart idênticos → exit 0
   - Caso B: source adiciona property em schema → exit 1, output mostra +property
   - Caso C: source muda enum value → exit 1
3. Refactor step CI "Check CRD content drift" para iterar e chamar script.
4. Atualizar mensagem de erro com referência ao script para reproduzir local.

#### TDD
```
RED:     test_identical_specs_no_diff — fixture com source/chart iguais → exit 0
RED:     test_added_property_detected — fixture com property nova em source → exit 1, output contém "+property"
RED:     test_changed_enum_detected — fixture com enum mudado → exit 1
RED:     test_normalize_removes_status — source com status: {}, chart sem → normalized iguais
GREEN:   Implement crd-semantic-diff.sh
REFACTOR: Extract normalize() if used > 1x
VERIFY:  bash infra/scripts/crd-semantic-diff.test.sh && bash infra/scripts/crd-semantic-diff.sh operators/config/crd/bases/loopzero.io_tenants.yaml charts/loopzero-crds/templates/tenants.yaml
```

#### Acceptance Criteria
- [ ] Script detecta diff semântico, não apenas nominal
- [ ] CI job `crd-sync` falha se source CRD adiciona property que chart não tem
- [ ] CI job `crd-sync` GREEN no estado atual (CRDs em sync)
- [ ] Mensagem de erro indica como reproduzir local
- [ ] Pass: shellcheck zero warnings

#### DoD
- [ ] All tasks completed
- [ ] CRD drift semântico funcionando
- [ ] CH-G06 status: PARTIAL → MET
- [ ] CI green

---

### T1.5 — Add helm-unittest suites to 4 charts (cloud, dashboard, monitoring, admission-policies)

#### Objective
Aumentar cobertura helm-unittest de 5/13 (38%) para 9/13 (69%). Padrão das 5 suites existentes em `loopzero-api/tests/` e operadores.

#### Evidence
- `find charts -path '*/tests/*_test.yaml' | wc -l` retorna 12 (de 5 charts: api + 4 operators).
- Faltam: loopzero-cloud, loopzero-dashboard, loopzero-monitoring, loopzero-admission-policies (4 charts).
- Pattern existente: 3 suites por chart (security-context, networkpolicy, servicemonitor).

#### Files to edit
```
charts/loopzero-cloud/tests/security-context_test.yaml          (NEW)
charts/loopzero-cloud/tests/networkpolicy_test.yaml             (NEW)
charts/loopzero-cloud/tests/servicemonitor_test.yaml            (NEW)
charts/loopzero-dashboard/tests/security-context_test.yaml      (NEW)
charts/loopzero-dashboard/tests/networkpolicy_test.yaml         (NEW)
charts/loopzero-monitoring/tests/prometheusrules_test.yaml      (NEW) — diferente: testa renderização de rules + group intervals
charts/loopzero-monitoring/tests/dashboards_test.yaml            (NEW)
charts/loopzero-admission-policies/tests/vap_test.yaml          (NEW) — testa renderização dos 8 VAPs + failurePolicy
charts/loopzero-admission-policies/tests/cluster-image-policy_test.yaml  (NEW) — após T1.2
charts/loopzero-admission-policies/tests/alerts_test.yaml       (NEW) — após T1.2
```

#### Deep file dependency analysis
- Suites são standalone YAML; padrão `helm-unittest` BDD-style.
- CI step "Run helm-unittest" (linha 65 de helm-charts.yml) hardcoda lista de 5 charts; T3.5 do Phase 3 vai loopar `charts/*/tests/` automaticamente.

#### Deep Dives
- **loopzero-monitoring testing**: diferente do template-deployment. Tests focam em:
  - PrometheusRule groups têm `interval: 1m` default
  - Alerts têm `severity: warning|critical` label
  - `for: 5m` em alerts críticos
  - Dashboards têm `apiVersion: v1` + `kind: ConfigMap`
- **loopzero-admission-policies VAP testing**:
  - Cada VAP tem `failurePolicy: Fail`
  - `validations[].expression` é CEL válido
  - `matchConstraints.resourceRules` cobre Pods + Deployments

#### Tasks
1. Para cada um dos 4 charts, criar suite `tests/security-context_test.yaml` (template existente em loopzero-api).
2. Adaptar paths (deployment.yaml dependencies) por chart.
3. Para loopzero-monitoring, criar 2 suites específicas (prometheusrules + dashboards).
4. Para loopzero-admission-policies, criar 3 suites (VAPs + ClusterImagePolicy + alerts).
5. Verificar todas suites passam: `helm unittest charts/loopzero-{cloud,dashboard,monitoring,admission-policies}`.
6. Atualizar `helm-charts.yml` step lista: trocar hardcoded list para `for chart in charts/*/tests/; do helm unittest "$(dirname $chart)"; done` (T3.5 fará isso definitivamente; aqui apenas adicionar entries).

#### TDD
```
RED:     loopzero-cloud security-context: assert containerSecurityContext.runAsNonRoot == true
RED:     loopzero-cloud networkpolicy: assert spec.policyTypes contains Ingress + Egress
RED:     loopzero-cloud servicemonitor: assert spec.endpoints[0].port == "metrics"
RED:     loopzero-dashboard security-context: idem
RED:     loopzero-monitoring prometheusrules: assert .spec.groups[0].interval == "1m"
RED:     loopzero-monitoring dashboards: assert kind == "ConfigMap" + label grafana_dashboard
RED:     loopzero-admission-policies vap: assert spec.failurePolicy == "Fail" para todos VAPs
GREEN:   Write suites
REFACTOR: Extract shared suite via $ref se padrão for repetitivo
VERIFY:  helm unittest charts/loopzero-cloud charts/loopzero-dashboard charts/loopzero-monitoring charts/loopzero-admission-policies
```

#### Acceptance Criteria
- [ ] 4 charts ganham helm-unittest suites
- [ ] `find charts -path '*/tests/*_test.yaml' | wc -l` retorna ≥ 22 (de 9 charts: api + 4 ops + 4 novos)
- [ ] Cada chart tem ≥ 3 suites
- [ ] Cada suite tem ≥ 4 asserts
- [ ] CI roda todos suites GREEN
- [ ] Pass: helm unittest charts/* (loop) GREEN

#### DoD
- [ ] All tasks completed
- [ ] Cobertura helm-unittest: 5/13 → 9/13 (69%)
- [ ] CH-G12 PARTIAL avança
- [ ] CI green

---

## Phase 2: loopzero-common v0.3.0 — Library Helper Expansion

**Objective:** Adicionar 6 helpers Bitnami-style identificados como duplicação real, eliminando ~80 LOC duplicadas em 5 deployments.

### T2.1 — `loopzero-common.tplvalues.render` helper

#### Objective
Permitir renderizar values com Go template (útil em annotations dinâmicas, labels custom).

#### Evidence
- Bitnami common 2.x define `common.tplvalues.render` como pattern central.
- LoopZero hoje não tem; templates que precisam values templated fazem `{{ tpl ... }}` inline.

#### Files to edit
```
charts/loopzero-common/templates/_helpers.tpl    — adicionar bloco `tplvalues.render`
charts/loopzero-common/tests/tplvalues_test.yaml (NEW)
```

#### Deep file dependency analysis
- `_helpers.tpl` ganha 1 novo bloco `{{- define "loopzero-common.tplvalues.render" -}}`.
- Tests rodam contra um template fixture que usa o helper.
- Charts consumidores migram em T2.7 quando há caso de uso.

#### Deep Dives
- **Bitnami pattern:**
```gotemplate
{{- define "common.tplvalues.render" -}}
{{- $value := typeIs "string" .value | ternary .value (.value | toYaml) }}
{{- if contains "{{" (toJson .value) }}
  {{- if .scope }}
      {{- tpl (cat "{{- with $.RelativeScope -}}" $value "{{- end }}") (merge (dict "RelativeScope" .scope) .context) }}
  {{- else }}
    {{- tpl $value .context }}
  {{- end }}
{{- else }}
    {{- $value }}
{{- end }}
{{- end -}}
```

#### Tasks
1. Portar pattern Bitnami adaptado para namespace `loopzero-common.`.
2. Escrever fixture template em `tests/fixtures/tplvalues-test.yaml`.
3. Suite asserta:
   - Render simples: `{ value: "hello" }` → `"hello"`
   - Render templated: `{ value: "{{ .Release.Name }}", context: . }` → "<release-name>"

#### TDD
```
RED:     test_renders_plain_string — value: "hello" → "hello"
RED:     test_renders_templated_value — value: "{{ .Release.Name }}", context: . → "test-release"
RED:     test_renders_yaml_object — value: { foo: bar } → "foo: bar\n"
GREEN:   Add tplvalues.render to _helpers.tpl
REFACTOR: None
VERIFY:  helm unittest charts/loopzero-common
```

#### Acceptance Criteria
- [ ] Helper definido em `_helpers.tpl`
- [ ] 3 asserts passam
- [ ] `grep -c 'tplvalues.render' charts/loopzero-common/templates/_helpers.tpl` ≥ 1

#### DoD
- [ ] Helper + tests + verify

---

### T2.2 — `loopzero-common.kubeVersion` guard

#### Objective
Retornar versão K8s (`.Values.kubeVersion` ou `Capabilities.KubeVersion.Version`) para apiVersion compat checks.

#### Evidence
- Bitnami `common.kubeVersion` é base para `common.capabilities.*.apiVersion` resolvers.
- Útil quando matrix testing K8s 1.30/1.31/1.32 (ADR-0039) revela API divergence.

#### Files to edit
```
charts/loopzero-common/templates/_helpers.tpl    — adicionar bloco `kubeVersion`
charts/loopzero-common/tests/kubeVersion_test.yaml (NEW)
```

#### Deep Dives
- Pattern: `{{- default .Capabilities.KubeVersion.Version .Values.kubeVersion -}}`
- Fallback: se `.Values.kubeVersion` set, use; else use `.Capabilities.KubeVersion.Version`.

#### Tasks
1. Definir helper.
2. Tests assertam:
   - Default = `Capabilities.KubeVersion.Version`
   - Override via `.Values.kubeVersion: "1.30.0"` → "1.30.0"

#### TDD
```
RED:     test_default_uses_capabilities — output == .Capabilities.KubeVersion.Version
RED:     test_override_via_values — values.yaml: kubeVersion: "1.30.0" → output == "1.30.0"
GREEN:   Add helper
VERIFY:  helm unittest charts/loopzero-common
```

#### Acceptance Criteria
- [ ] Helper definido + tests passam

#### DoD
- [ ] Helper + tests + verify

---

### T2.3 — `loopzero-common.checksumAnnotations` (ELIMINATES 5x DUPLICATION)

#### Objective
Extrair pattern duplicado em 5 deployments: `checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}` + `checksum/secret: ...`.

#### Evidence
- `charts/loopzero-tenant-operator/templates/deployment.yaml` linhas 16-17 + equivalente em api, app-operator, preview-operator, platform-operator. 5x duplicação.

#### Files to edit
```
charts/loopzero-common/templates/_helpers.tpl                      — adicionar bloco
charts/loopzero-common/tests/checksumAnnotations_test.yaml         (NEW)
```

#### Deep Dives
- Pattern:
```gotemplate
{{- define "loopzero-common.checksumAnnotations" -}}
checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
checksum/secret: {{ include (print $.Template.BasePath "/secret.yaml") . | sha256sum }}
{{- end -}}
```
- Edge case: chart sem `secret.yaml` (ex: loopzero-monitoring). Helper deve tolerar `{{ include "missing" . }}` → empty string. Use conditional render.

#### Tasks
1. Definir helper com conditional para missing files.
2. Test fixture chart com configmap.yaml + secret.yaml → asserta annotations.
3. Test fixture chart só com configmap.yaml → asserta apenas checksum/config.

#### TDD
```
RED:     test_renders_both_checksums — chart com cm + secret → 2 annotations
RED:     test_renders_only_configmap — chart só com cm → 1 annotation
RED:     test_changes_invalidate_checksum — modify configmap → diferente sha
GREEN:   Add helper with conditional includes
VERIFY:  helm unittest charts/loopzero-common
```

#### Acceptance Criteria
- [ ] Helper definido + tests passam

#### DoD
- [ ] Helper + tests + verify

---

### T2.4 — `loopzero-common.imagePullSecrets` global merger

#### Objective
Centralizar acesso defensivo a `.Values.global.imagePullSecrets`.

#### Evidence
- 5 deployments fazem `{{- with .Values.global.imagePullSecrets }}...{{- end }}`.

#### Files to edit
```
charts/loopzero-common/templates/_helpers.tpl
charts/loopzero-common/tests/imagePullSecrets_test.yaml (NEW)
```

#### Deep Dives
- Pattern: merge global + chart-local imagePullSecrets, dedup.

#### Tasks
1. Definir helper.
2. Tests:
   - Apenas global → renderiza global
   - Apenas chart-local → renderiza local
   - Ambos → merge com dedup

#### TDD
```
RED:     test_renders_global_only
RED:     test_renders_local_only
RED:     test_merges_global_and_local
GREEN:   Add helper
VERIFY:  helm unittest charts/loopzero-common
```

#### Acceptance Criteria
- [ ] Helper definido + tests passam

#### DoD
- [ ] Helper + tests + verify

---

### T2.5 — `loopzero-common.secrets.name` (existingSecret pattern)

#### Objective
Resolve `existingSecret` toggle reimplementado em 5 deployments.

#### Evidence
- `charts/loopzero-tenant-operator/templates/deployment.yaml` linhas 69-73 + equivalente em outros 4.

#### Files to edit
```
charts/loopzero-common/templates/_helpers.tpl
charts/loopzero-common/tests/secrets_test.yaml (NEW)
```

#### Deep Dives
- Pattern Bitnami:
```gotemplate
{{- define "loopzero-common.secrets.name" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "loopzero-common.fullname" . -}}
{{- end -}}
{{- end -}}
```

#### Tasks + TDD + DoD: idem T2.1-T2.4 padrão.

---

### T2.6 — `loopzero-common.tmpVolumeMount` (ROFS compat)

#### Objective
Padronizar `tmp` emptyDir + volumeMount para `readOnlyRootFilesystem: true` compat. Duplicado em 5 deployments.

#### Evidence
- `charts/loopzero-tenant-operator/templates/deployment.yaml` linhas 96-98 (mount) + 111-114 (volume) + equivalente em outros 4.

#### Files to edit
```
charts/loopzero-common/templates/_helpers.tpl
charts/loopzero-common/tests/tmpVolumeMount_test.yaml (NEW)
```

#### Deep Dives
- 2 helpers separados (volume, volumeMount) ou 1 helper que renderiza ambos com pivot.
- Pattern simples:
```gotemplate
{{- define "loopzero-common.tmpVolume" -}}
- name: tmp
  emptyDir:
    sizeLimit: {{ .Values.tmpVolume.sizeLimit | default "64Mi" }}
{{- end -}}
{{- define "loopzero-common.tmpVolumeMount" -}}
- name: tmp
  mountPath: /tmp
{{- end -}}
```

#### Tasks + TDD + DoD: padrão.

---

### T2.7 — Migrate 5 deployment templates to use new helpers

#### Objective
Eliminar duplicação real substituindo código inline pelos helpers T2.3, T2.4, T2.5, T2.6.

#### Evidence
- Após T2.1-T2.6, helpers existem mas não são usados.

#### Files to edit
```
charts/loopzero-api/templates/deployment.yaml                    — substituir inline por includes
charts/loopzero-tenant-operator/templates/deployment.yaml         — idem
charts/loopzero-app-operator/templates/deployment.yaml           — idem
charts/loopzero-preview-operator/templates/deployment.yaml       — idem
charts/loopzero-platform-operator/templates/deployment.yaml      — idem
charts/loopzero-common/Chart.yaml                                 — bump 0.2.0 → 0.3.0
charts/loopzero-{api,*-operator}/Chart.yaml                       — bump loopzero-common dep para >=0.3.0
```

#### Deep file dependency analysis
- 5 deployments perdem ~16 linhas cada (~80 LOC total).
- Charts deps bump não-breaking (helpers existentes inalterados).
- helm-unittest existing tests continuam passando (template render permanece equivalente).

#### Tasks
1. Para cada um dos 5 deployments, substituir:
   - linhas 16-17 → `{{- include "loopzero-common.checksumAnnotations" . | nindent 8 }}`
   - linhas 29-32 (imagePullSecrets) → `{{- include "loopzero-common.imagePullSecrets" . | nindent 6 }}`
   - linhas 69-73 (existingSecret) → `name: {{ include "loopzero-common.secrets.name" . }}`
   - linhas 96-98 + 111-114 (tmp volume + mount) → `{{- include "loopzero-common.tmpVolumeMount" . | nindent 12 }}` + `{{- include "loopzero-common.tmpVolume" . | nindent 8 }}`
2. Bump loopzero-common version 0.2.0 → 0.3.0.
3. Bump consumer charts dep range para `>=0.3.0 <1.0.0`.
4. Run helm-unittest existing tests — deve passar sem mudanças (render equivalente).
5. Run `helm template charts/loopzero-tenant-operator` e verificar diff é zero (apenas refactor).

#### TDD
```
RED:     test_render_unchanged_after_migration — diff helm template before vs after migration is empty
RED:     test_existing_security_context_test_still_passes — existing helm-unittest suites pass
GREEN:   Migrate 5 deployments
REFACTOR: None (já é refactor)
VERIFY:  helm template charts/loopzero-{api,tenant-operator,app-operator,preview-operator,platform-operator} | sha256sum should match pre-migration
```

#### Acceptance Criteria
- [ ] 5 deployments migrados
- [ ] `grep -c 'checksum/config: {{' charts/loopzero-*/templates/deployment.yaml` retorna 0 (todos via include)
- [ ] helm-unittest GREEN para todas suites existentes
- [ ] `helm template` output diff before/after = vazio
- [ ] Pass: /code-audit complexity check (deployments ≤ 100 linhas após migration)

#### DoD
- [ ] All migrations completed
- [ ] CH-G07 PARTIAL → MET via CH-G17 path
- [ ] CH-G17 status: PARTIAL → MET
- [ ] loopzero-common v0.3.0 released

---

### T2.8 — Add `tests/_helpers_test.yaml` covering all 22 loopzero-common helpers

#### Objective
Cobrir os 6 novos helpers (T2.1-T2.6) + os 16 existentes que hoje não têm dedicated tests.

#### Evidence
- `charts/loopzero-common/tests/` inexistente atualmente.

#### Files to edit
```
charts/loopzero-common/tests/labels_test.yaml          (NEW)
charts/loopzero-common/tests/image_test.yaml           (NEW)
charts/loopzero-common/tests/securitycontext_test.yaml (NEW)
charts/loopzero-common/tests/serviceaccount_test.yaml  (NEW)
charts/loopzero-common/tests/affinity_test.yaml        (NEW)
charts/loopzero-common/tests/servicemonitor_test.yaml  (NEW)
charts/loopzero-common/tests/pdb_test.yaml             (NEW)
charts/loopzero-common/tests/networkpolicy_test.yaml   (NEW)
```

#### Tasks + TDD + DoD: padrão.

#### Acceptance Criteria
- [ ] 22 helpers testados
- [ ] `find charts/loopzero-common/tests -name '*_test.yaml' | wc -l` ≥ 8

---

## Phase 3: CI Hardening — chart-testing + matrix K8s

**Objective:** Adicionar `ct lint`, `ct install`, `ct upgrade` em K8s 1.30/1.31/1.32 matrix. Fecha CH-G18 (NEW).

### T3.1 — Add `ct.yaml` config + `ct lint` job

#### Files to edit
```
ct.yaml                                       (NEW) — root config
.github/workflows/helm-charts.yml             — add job ct-lint
```

#### Deep Dives
- ct.yaml padrão:
```yaml
chart-dirs: [charts]
chart-repos: []
test-values: []
kubernetes-versions: [v1.30.0, v1.31.0, v1.32.0]
validate-maintainers: true
check-version-increment: true
validate-chart-schema: true
helm-extra-args: --timeout 600s
```

#### Tasks + TDD + DoD: padrão.

#### Acceptance Criteria
- [ ] ct.yaml exists
- [ ] CI job ct-lint passes
- [ ] CH-G18 partial path

---

### T3.2 — Add `ct install` job with Kind cluster + K8s matrix

#### Files to edit
```
.github/workflows/helm-charts.yml — add job install-test with strategy.matrix.k8s-version
```

#### Deep Dives
- Pattern (per pesquisa Vazquez 2026):
```yaml
install-test:
  runs-on: ubuntu-latest
  strategy:
    fail-fast: false
    matrix:
      k8s-version: [v1.30.0, v1.31.0, v1.32.0]
  steps:
    - uses: actions/checkout@v4
    - uses: helm/kind-action@v1.10.0
      with:
        cluster_name: chart-testing
        node_image: kindest/node:${{ matrix.k8s-version }}
    - uses: helm/chart-testing-action@v2.6.1
    - name: ct install
      run: ct install --config ct.yaml
```

#### Tasks + TDD + DoD: padrão. CI gate.

#### Acceptance Criteria
- [ ] Matrix [1.30, 1.31, 1.32] testada
- [ ] Cada versão tem job próprio
- [ ] CH-G18 status: NOT_MET → MET

---

### T3.3 — Add `ct install --upgrade` for chart upgrade testing

#### Files to edit
```
.github/workflows/helm-charts.yml — extend install-test with --upgrade flag
```

#### Tasks + TDD + DoD: padrão.

#### Acceptance Criteria
- [ ] Upgrade test cobre version N-1 → N

---

### T3.4 — Update kubeconform multi-version

#### Files to edit
```
.github/workflows/helm-charts.yml — kubeconform step in matrix
```

#### Tasks: pattern matrix idem T3.2.

#### Acceptance Criteria
- [ ] kubeconform roda 3 versões K8s

---

### T3.5 — Auto-discover helm-unittest charts

#### Objective
Substituir hardcoded list (linha 65 de helm-charts.yml) por loop sobre `charts/*/tests/`.

#### Files to edit
```
.github/workflows/helm-charts.yml — replace hardcoded list with loop
```

#### Deep Dives
```yaml
- name: Run helm-unittest (auto-discovery)
  run: |
    helm plugin install https://github.com/helm-unittest/helm-unittest 2>/dev/null || true
    FAILED=0
    for tests_dir in charts/*/tests/; do
      chart=$(dirname "$tests_dir")
      echo "::group::helm unittest $chart"
      if ! helm unittest "$chart"; then
        FAILED=1
      fi
      echo "::endgroup::"
    done
    exit $FAILED
```

#### Acceptance Criteria
- [ ] CI auto-discovers all charts with tests/
- [ ] Após T1.5 + T2.8, runs em ≥ 12 charts

---

## Phase 4: Schema Umbrella + Chartsnap (paralelo a Phase 3)

### T4.1 — `loopzero-platform/values.schema.json`

#### Objective
Validar `global.registry` + sub-chart toggles no umbrella.

#### Files to edit
```
charts/loopzero-platform/values.schema.json (NEW)
```

#### Deep Dives
- Schema Draft-07.
- `global.registry` regex pattern para FQDN.
- Cada `<sub-chart>.enabled` boolean obrigatório.
- `additionalProperties: true` no top-level (compat-safe).

#### Tasks + TDD + DoD: padrão.

#### Acceptance Criteria
- [ ] Schema bloqueia typo em global.registry
- [ ] CI helm lint --strict passa
- [ ] CH-G14 PARTIAL avança

---

### T4.2 — chartsnap baseline + CI step

#### Files to edit
```
charts/loopzero-platform/__snapshot__/v2/      (NEW dir, baseline criada via --update)
.helmignore                                  — adicionar `__snapshot__/v2/`
.github/workflows/helm-charts.yml           — add step chartsnap
```

#### Deep Dives
- `helm chartsnap charts/loopzero-platform -f charts/loopzero-platform/profiles/values-prod.yaml --update` cria baseline.
- CI roda sem `--update`, falha em diff.

#### Tasks + TDD + DoD: padrão.

#### Acceptance Criteria
- [ ] Baseline em git
- [ ] `.helmignore` excluding snapshots
- [ ] CI gate funcional

---

### T4.3 — helm-diff CI gate em PRs

#### Files to edit
```
.github/workflows/helm-charts.yml — add helm-diff job
```

#### Deep Dives
- Per pesquisa: `HELM_DIFF_THREE_WAY_MERGE=true` + `HELM_DIFF_NORMALIZE_MANIFESTS=true`.
- Job roda só em PR (não push).
- Dry-run mode (não conecta cluster).

#### Tasks + TDD + DoD: padrão.

#### Acceptance Criteria
- [ ] Helm diff runs em PRs que tocam charts/
- [ ] CH-G05 PARTIAL → MET

---

## Phase 5: SBOM Attestation (depende de Phase 3 GREEN)

### T5.1 — Generate SBOM per chart with syft

#### Files to edit
```
.github/workflows/helm-charts.yml — add SBOM gen step in package-push job
```

#### Deep Dives
- syft já é dependência transitiva (Sprint 6).
- `syft <chart>.tgz -o cyclonedx-json > sbom.json`.

#### Tasks + TDD + DoD: padrão CI.

---

### T5.2 — `cosign attest --predicate` per chart

#### Files to edit
```
.github/workflows/helm-charts.yml — add cosign attest step
```

#### Deep Dives
- `cosign attest --predicate sbom.json --type cyclonedx <REF>`.
- OIDC keyless (já configurado em Sprint 6).

#### Tasks + TDD + DoD: padrão.

---

### T5.3 — Update ClusterImagePolicy verify-attestation

#### Files to edit
```
charts/loopzero-admission-policies/templates/cluster-image-policy.yaml — add attestation verifier
infra/helm/admission-policies/templates/cluster-image-policy.yaml — propagate change
```

#### Deep Dives
- Sigstore Policy Controller v0.13+ supports `signatureFormat: bundle` for attestations.

#### Tasks + TDD + DoD: padrão.

---

### T5.4 — `cosign verify-attestation` step in CI

#### Files to edit
```
.github/workflows/helm-charts.yml — add verify-attestation step after sign
```

#### Acceptance Criteria
- [ ] CI verifies SBOM attestation post-publish
- [ ] CH-G19 status: NOT_MET → MET

---

## Phase 6: Strategic Documentation

### T6.1 — ADR for loopzero-api dual chart drift (intentional)

#### Files to edit
```
docs/meetings/adr/0043-loopzero-api-dual-chart-intentional-drift.md (NEW)
```

#### Tasks
1. Document drift: published full RBAC vs bootstrap minimal local-dev.
2. Reference T1.3 audit-helm-drift script that flags this drift as `intentional: true`.

---

### T6.2 — Rename `loopzero-monitoring` → `loopzero-monitoring-rules` OR keep + ADR

#### Files to edit
```
charts/loopzero-monitoring/Chart.yaml — possibly rename
charts/loopzero-platform/Chart.yaml   — update dep if rename
docs/meetings/adr/0044-loopzero-monitoring-naming-decision.md (NEW)
```

#### Decision
- Optional: rename é Sprint-level effort; ADR documenting current naming + future intent é suficiente.

---

### T6.3 — README umbrella documenting file://+OCI rewrite

#### Files to edit
```
charts/loopzero-platform/README.md (NEW or extend) — explain dev (file://) vs publish (OCI) flow
```

#### Acceptance Criteria
- [ ] Documentação completa do dual code path
- [ ] Reference to ADR-0038

---

## Phase 7: Post-Launch (deferred but planned)

### T7.1 — Polaris severity=danger graduation

#### Files to edit
```
.github/workflows/helm-charts.yml — remove continue-on-error: true on Polaris audit
```

#### Pre-condition
- Baseline reviewed and findings closed/suppressed.
- Per MEET-062 condition #1.

---

### T7.2 — Helm v3.14.4 → v3.18.x migration

#### Files to edit
```
.github/actions/setup-helm/action.yml — bump version
charts/*/values.schema.json — review for additionalProperties strictness regression
```

#### Risk
- Helm 3.18.5 endureceu schema validation; pode quebrar charts atuais.

---

### T7.3 — Conftest policies for charts

#### Files to edit
```
policies/charts/*.rego (NEW)
.github/workflows/helm-charts.yml — add Conftest step
```

---

### T7.4 — helm-drift CI cron

#### Files to edit
```
.github/workflows/charts-drift-cron.yml (NEW)
```

#### Schedule
```yaml
on:
  schedule:
    - cron: '0 */6 * * *'
```

---

### T7.5 — Multi-namespace policy testing

#### Files to edit
```
charts/loopzero-admission-policies/tests/multi-namespace_test.yaml (NEW)
```

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Ghost charts (loopzero-controller, loopzero-tenant-controller) | T1.1 | Delete via git rm + Chart.yaml/lock/values cleanup |
| 2 | Drift regressivo loopzero-admission-policies vs bootstrap | T1.2 | Port 5 missing templates + tests + schema |
| 3 | Sem drift detector entre charts/ e infra/helm/ | T1.3 | infra/scripts/audit-helm-drift.sh + CI step + runbook |
| 4 | CRD drift comparator nominal-only | T1.4 | infra/scripts/crd-semantic-diff.sh + refactor crd-sync job |
| 5 | helm-unittest cobertura 5/13 | T1.5 + T3.5 | 4 charts ganham suites + auto-discovery loop |
| 6 | loopzero-common faltam 6 helpers | T2.1-T2.6 | Add tplvalues.render, kubeVersion, checksumAnnotations, imagePullSecrets, secrets.name, tmpVolumeMount |
| 7 | 5 deployments duplicam patterns | T2.7 | Migrate 5 deployments to use new helpers (~80 LOC removed) |
| 8 | loopzero-common sem testes | T2.8 | 8 suites cobrindo 22 helpers |
| 9 | Sem chart-testing (ct) | T3.1 | ct.yaml + ct lint job |
| 10 | Sem matriz K8s 1.30/1.31/1.32 | T3.2 + T3.4 | Kind matrix + kubeconform multi-version |
| 11 | Sem ct install --upgrade | T3.3 | Upgrade test in matrix |
| 12 | Hardcoded helm-unittest list | T3.5 | Auto-discovery loop |
| 13 | Sem schema umbrella | T4.1 | loopzero-platform/values.schema.json |
| 14 | Sem chartsnap | T4.2 | Baseline + CI gate |
| 15 | Sem helm-diff em CI | T4.3 | helm-diff PR gate |
| 16 | Sem SBOM attestation por chart | T5.1-T5.4 | syft + cosign attest + verify in CI + ClusterImagePolicy |
| 17 | loopzero-api dual chart drift undocumented | T6.1 | ADR-0043 |
| 18 | loopzero-monitoring naming colide | T6.2 | ADR-0044 ou rename |
| 19 | Umbrella file://+OCI dual path undocumented | T6.3 | README |
| 20 | Polaris baseline-first sem graduation | T7.1 | Remove continue-on-error |
| 21 | Helm v3.14.4 desatualizado | T7.2 | Migration to v3.18.x |
| 22 | Sem Conftest, helm-drift cron, multi-ns testing | T7.3, T7.4, T7.5 | Add policies + cron + tests |
| 23 | Governance gate para 7 ADRs | T0.1 | Meeting + ADRs 0036-0042 |
| 24 | CH-G15 NEW gate (ghost charts) | T1.1 | MET após delete |
| 25 | CH-G16 NEW gate (drift detector) | T1.3 | MET após script + CI step |
| 26 | CH-G17 NEW gate (library coverage) | T2.1-T2.7 | MET após 6 helpers + migration |
| 27 | CH-G18 NEW gate (multi-K8s matrix) | T3.1-T3.4 | MET após ct + matrix |
| 28 | CH-G19 NEW gate (SBOM attestation) | T5.1-T5.4 | MET após attest pipeline |
| 29 | CH-G09 NOT_MET → MET | T1.2 | Drift fixed |
| 30 | CH-G06 PARTIAL → MET | T1.4 | Semantic diff |

**Coverage: 30/30 gaps covered (100%).**

## Global Definition of Done

- [ ] All 7 phases completed
- [ ] All tests passing (helm-unittest 13/13 charts + ct install matrix 3 K8s versions + chartsnap baseline + helm-diff gate)
- [ ] Zero lint warnings (helm lint --strict + golangci-lint para Go scripts + shellcheck para bash)
- [ ] Backward compatibility preserved (loopzero-common v0.3.0 minor bump não breaks existing consumers)
- [ ] code-audit checks passing across all modified packages
- [ ] All 19 SOTA gates (CH-G01 to CH-G19) status MET
- [ ] SOTA score 7.2/10 → 9.0/10 measured via re-audit
- [ ] All 7 ADRs (0036-0042) ratified + 2 strategic ADRs (0043-0044) for drift documentation
- [ ] Governance compliance: T2/T3 changes via meeting + decision-record per pre-release-governance.md
- [ ] **`/dogfood full` PASSED** (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure (operator + new charts must run before dogfood)
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Verify charts published with new helpers + signed + SBOM attested
helm pull oci://registry.digitalocean.com/loopzero-system/helm/loopzero-platform --version <new-version>
cosign verify oci://.../loopzero-platform:<version> \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp "^https://github\.com/getloopscale/loopzero/"
cosign verify-attestation oci://.../loopzero-platform:<version> --type cyclonedx \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp "^https://github\.com/getloopscale/loopzero/"

# 3. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to charts changes
- [ ] Prova de Vida passes for every deploy performed during dogfood (charts render correctly, deployments healthy)
- [ ] Prova de Morte passes for every delete performed during dogfood
- [ ] No regressions in previously passing flows
- [ ] Observability check passes (logs, metrics, CRs in correct state)
- [ ] Rule 3 clean: zero internal terms in any CLI output
- [ ] **NEW**: SBOM attestation verifiable for all published charts
- [ ] **NEW**: `helm-charts.yml` CI green em PR de teste com mudança trivial em template (regression sanity check)
- [ ] **NEW**: Drift detector (`audit-helm-drift.sh`) returns 0 drifts (após T6.1 ADRs)
- [ ] **NEW**: All 4 split operator charts deploy and reconcile correctly with new loopzero-common v0.3.0

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented in [docs/runbooks/charts-sota-dogfood-findings.md](../runbooks/charts-sota-dogfood-findings.md) but do not block plan completion.

### Rollback Plan

Se Phase 5 (SBOM attestation) ou Phase 3 (matrix CI) introduzem regressão impossível de fix em 1 sprint:
- Revert merges via `git revert` (NOT `git checkout` per CLAUDE.md rule 4)
- Document rollback em ADR-0045 (rollback decision)
- Re-classify task como Post-Launch
- Plan version bump 1.0 → 1.1

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| loopzero-common v0.3.0 helpers introduzem regression silent em 5 deployments | Médio | Alto | T2.7 verify diff zero + helm-unittest existing tests + chartsnap baseline antes de Phase 4 |
| ct install matrix CI ~10-15min slower | Alta | Baixo | Paralelizar via matrix.k8s-version + cache Kind images |
| Polaris graduation revela findings ocultos | Médio | Médio | Phase 7 deferred Post-Launch; baseline review é separate task |
| Helm v3.18.x migration quebra schemas | Médio | Alto | Phase 7 deferred + dedicated test em fork branch |
| Drift detector bloqueia PRs após D+30 | Alta | Médio | ADR-0041 explicit advisory→blocking gradual; runbook documenta classification workflow |
| Ghost chart deletion quebra unknown consumer | Baixíssimo | Alto | Pre-flight `grep -rn loopzero-controller` em todo repo (zero matches expected) + revert plan |
| OCI registry rate limiting durante CI matrix | Baixo | Médio | helm-charts.yml já usa immutability guard (skip se exists); cache layer recomendado |

---

## Estimated Effort

| Phase | Tasks | Effort | Owner |
|---|---|---|---|
| 0 | T0.1 (governance) | 1 sessão | meeting-orchestrator + chief |
| 1 | T1.1-T1.5 (quick-wins) | 2-3 sessões (paralelizáveis) | infra-engineer + api-engineer |
| 2 | T2.1-T2.8 (loopzero-common) | 3-4 sessões | platform-sdk-engineer |
| 3 | T3.1-T3.5 (CI matrix) | 2-3 sessões | infra-engineer |
| 4 | T4.1-T4.3 (schema + chartsnap) | 1-2 sessões | infra-engineer |
| 5 | T5.1-T5.4 (SBOM) | 2 sessões | platform-security-engineer |
| 6 | T6.1-T6.3 (docs) | 1 sessão | decision-record-moderator |
| 7 | T7.1-T7.5 (post-launch) | Sprint 10+ | various |

**Total Sprint 9 (Phases 0-6):** ~12-16 sessões / ~3-4 weeks single-engineer pace.

---

## References

- [docs/technical/charts/improvement-roadmap.md](../technical/charts/improvement-roadmap.md) — pesquisa SOTA fonte deste plano
- [docs/technical/charts/sota-gates.md](../technical/charts/sota-gates.md) — 19 gates com critérios mensuráveis
- [docs/technical/charts/research-basis.md](../technical/charts/research-basis.md) — 14 fontes externas
- [docs/technical/charts/paas-benchmark.md](../technical/charts/paas-benchmark.md) — score comparativo
- [Bitnami common library](https://github.com/bitnami/charts/tree/main/bitnami/common) — referência library chart
- [helm-unittest](https://github.com/helm-unittest/helm-unittest)
- [chart-testing](https://github.com/helm/chart-testing)
- [Sigstore Cosign](https://docs.sigstore.dev/cosign/signing/other_types/)
- [Sigstore Policy Controller](https://docs.sigstore.dev/policy-controller/overview/)
- [helm-diff](https://github.com/databus23/helm-diff)
- [helm-chartsnap](https://github.com/jlandowner/helm-chartsnap)
- [Helm 3.18.5 schema breaking change](https://community.replicated.com/t/helm-3-18-5-upgrade-impact-schema-validation-changes/1577)
- ADR-0023 (operator split) — base para ghost chart deletion
- MEET-2026-05-03-063 (cosign keyless) — base para SBOM attestation parity
- CLAUDE.md — pre-release-governance, design-principles, loopzero-design rules
