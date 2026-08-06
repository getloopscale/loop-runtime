---
type: plan
title: "Plan: Helm Chart Render-Time Testing with Terratest"
status: deprecated
---

# Plan: Helm Chart Render-Time Testing with Terratest

> **Version 1.2** (2026-05-21 — DoD runtime target relaxed per
> MEET-ASYNC-AMENDMENT-2026-05-21-001 after empirical measurement.
> "<60s full suite" was speculative; real floor at 22 packages is
> ~6m15s with render cache + sync.Once. New targets: build-gate ≤30s,
> full suite ≤10min. Engineering side of plan COMPLETE.)
>
> **Version 1.1** (2026-05-20 — edge-case review incorporated, 3 MUST FIX items addressed via T1.3, T1.4, T3.2 exemption + D10, D11 ADRs)
>
> **Version 1.0** — Este plano adiciona uma camada de **render-time validation** (tier 2) entre os 6 hermetic schema gates já existentes (294+ assertions) e os LIVE operator gates (`/dogfood full`, drill LIVE). Usando `gruntwork-io/terratest` + Go, validamos que cada Helm chart **renderiza manifests K8s corretos** após substituição de values por env (dev/drill/prod). Catches bug classes que schemas (input validation) não pegam: label selector drift, conditional template branches, env-specific rendering errors, multi-env override consistency. Outcome: gates de regressão executáveis em <30s no CI, cobrindo 19 charts custom LoopZero (13 OCI + 6 infra/helm com templates/) com ~150+ render assertions, eliminando o gap entre "schema OK" e "deploys correctly".

## Context

### O Que Existe Hoje

Após o plano [`2026-05-20-infra-standardization-and-secrets-plan.md`](./2026-05-20-infra-standardization-and-secrets-plan.md) (37 commits, plan v1.2), o sistema tem **schema-side defense** strong:

| Gate | Cobertura | Status |
|---|---|---|
| `helm-values-schemas_test.sh` | 30 charts positive validation | GREEN |
| `helm-schemas-negative_test.sh` | 106 TDD red rejection cases | GREEN |
| `helm-charts-schema-coverage_test.sh` | 27 covered / 0 missing | GREEN |
| `helm-patterns-audit_test.sh` | 131 regex pattern cases | GREEN |
| `ci-path-filter-coverage_test.sh` | 47 workflows trigger validated | GREEN |
| `plan-2026-05-20-criteria_test.sh` | 4/4 plan criteria | GREEN |

**Total: 294+ hermetic assertions GREEN.**

### O Que Está Faltando (Evidência Concreta)

Análise dos 3 artigos de Helm chart testing (Gruntwork 2019, Camunda 2022, Microsoft 2026 — referenciados em conversa anterior desta sessão) revelou que o LoopZero cobre **tier 1 (schema)** mas **NÃO tem tier 2 (render-time)**.

**Bug classes que escapam tier 1 (schemas validam INPUT, não OUTPUT):**

1. **Label selector drift**: `spec.selector.matchLabels` vs `spec.template.metadata.labels` divergem em refactor → orphaned ReplicaSets em LIVE
2. **Conditional template branches**: `{{ if .Values.X }}` rendariza errado em env específico
3. **Multi-env override consistency**: `values-prod.yaml` registry typo passa schema mas deploy puxa imagem errada
4. **Security control removal**: securityContext block deletado em refactor — schema requires field, mas se chart não emite no template, deploy roda como root
5. **Required-by-config but optional-by-template fields**: schema diz obrigatório, mas template tem `{{ if }}` que pode skip — gap quando rendered
6. **Resource limits absent in rendered manifest**: VAP `requireResourceLimits` enforce em K8s, mas schema requires não garantem template usa `.Values.resources.limits`

### Evidência Específica do Repo

- 19 charts custom LoopZero com `templates/` directory (need render-time validation):
  - **13 OCI** (`charts/`): loopzero-admission-policies, loopzero-api, loopzero-app-operator, loopzero-build-cwts, loopzero-cloud, loopzero-common, loopzero-crds, loopzero-dashboard, loopzero-monitoring, loopzero-platform, loopzero-platform-operator, loopzero-preview-operator, loopzero-tenant-operator
  - **6 infra/helm** com templates/: admission-policies, argocd, cert-manager, chaos-mesh, cloudflared, harbor, network-policies, postgresql, gitea (9 actually — re-count earlier)
- Multi-env values existem: `infra/helmfile/values/{dev,drill,prod,standalone,standalone-multi}/<chart>.yaml.gotmpl` per-env overrides
- Bug histórico real: B50 (loopzero-cloud port 13002 vs chart-default 3000) — silenced para readinessProbe; só schema (added in commit a72f0d16) catches at lint, render-time test catches earlier still
- Bug histórico real: B17 (notifications.services vs notifications.notifiers) — argocd silently ignored deprecated key; schema fix em commit 09a1ca7b detecta input value, render-test detecta template field path

### Por Que Terratest Especificamente

Comparação per artigo Microsoft 2026 + Camunda 2022:

| Critério | Terratest | helm-unittest | Chart Testing (ct) |
|---|---|---|---|
| **Type safety** | Render → `appsV1.Deployment` struct → compile-time check | YAML string match (typo silently passes) | YAML string match |
| **Language features** | Go: loops, table-driven tests, shared setup | YAML DSL only | YAML DSL only |
| **Debugging** | Standard Go debugger + stack traces | YAML diff only | Container logs dump |
| **Ecosystem fit** | LoopZero já usa Go (api/, operators/, infra/tests/e2e/ Ginkgo) | Separate tool | Separate tool |
| **Cluster-less unit** | ✓ template only | ✓ template only | ✗ requires cluster |

LoopZero já tem `infra/tests/go.mod` (Go 1.25.10) + Ginkgo + client-go + Gomega instalados. Adding Terratest = `go get github.com/gruntwork-io/terratest`. Mínimo overhead.

### Cenário Atual de Custos

- LIVE drill iter: ~$15 + ~3h wall-clock (operator-gate)
- `/dogfood full`: ~45min + local k3d teardown/setup
- Tier 2 render-time tests proposed: **~30s in CI, $0** — preenche gap entre schema (segundos) e LIVE (~hours+$)

## Objective

**Done:** todo chart custom LoopZero (19 total) tem render-time tests em Go/Terratest que validam (a) defaults rendarizam manifests K8s estruturalmente corretos, (b) multi-env overrides produzem outputs específicos por env (dev/drill/prod), (c) security/scaling/networking invariants são emitidos no rendered output, integrados ao CI workflow `drill-hermetic-tests.yml` rodando em <60s total.

**Metas mensuráveis:**
- 19 charts cobertos com render-time tests
- ≥150 render assertions
- 3 envs validados per chart (dev + drill + prod baseline)
- CI runtime: <60s para suite completa
- Zero LIVE cluster dependency (template-only)
- Reuse de schemas existentes via mesma values.yaml input

## ADRs

### D1 — Terratest sobre helm-unittest

**Decisão:** Adopt `gruntwork-io/terratest` como framework de render-time testing.

**Rationale:**
- LoopZero já usa Go em 4 contextos (api/, operators/, pkg/, infra/tests/e2e/ Ginkgo) — zero novo language overhead
- Type safety via `helm.UnmarshalK8SYaml(t, output, &deployment)` → compile-time bug catch (typo em `secCtx.RunAsUsr` falha em build, não em runtime)
- Camunda 2022 reportou Chart Testing (ct) "actually were never able to run a successful test" devido a timeouts não-overridable + Docker image requirement
- Microsoft 2026 argument: YAML string match (helm-unittest) silenciosamente aceita `spec.template.spec.containers[0].securityContest` (typo) porque match-zero é "no assertion violated"

**Consequences:**
- (+) Compile-time field path validation
- (+) Reuse Go libs (`stretchr/testify` already in transitive deps)
- (+) Table-driven tests para multi-env coverage com setup compartilhado
- (−) Operator team needs Go literacy para iterate em tests
- (−) Slower onboarding vs YAML DSL para non-Go contributors

### D2 — Render-Only Tests (Tier 2), NÃO Integration Tier

**Decisão:** Esta camada implementa APENAS render-time tests (template → struct → assert). Integration tests com kind/minikube ficam para sprint futuro (out of scope).

**Rationale:**
- Render-time tests catch ≥80% dos bug classes do artigo Microsoft (label drift, conditional branches, env overrides)
- Integration tier requer infra adicional (kind cluster em CI, ~2-5min de bootstrap per run) — diminishing returns vs cost
- LIVE drill iter (existing) já cobre integration semantics quando operator runs ~$15 + ~3h
- Render-tests rodam em <60s = compatible com CI fast feedback loop

**Consequences:**
- (+) CI runtime <60s (target)
- (+) Zero external dependencies (no Docker daemon, no cluster)
- (+) Deterministic — same input + values = same output assertions
- (−) Não pega bugs que requerem real K8s API server (e.g., CRD admission webhook rejection — schema NÃO em values)
- (−) Não pega ServiceAccount token mount issues (real cluster behavior)

### D3 — Multi-Env Test Suites Per-Chart (Microsoft 2026 pattern)

**Decisão:** Cada chart tem subdirectories per env:
```
infra/tests/helm/<chart-name>/
├── defaults_test.go       # values.yaml apenas
├── dev_test.go            # values.yaml + values-dev.yaml
├── drill_test.go          # values.yaml + values-drill.yaml
└── prod_test.go           # values.yaml + values-prod.yaml
```

**Rationale:**
- Per artigo Microsoft: env drift bugs (works-in-dev breaks-in-prod) só são caught por test suite per-env
- `infra/helmfile/values/{env}/<chart>.yaml.gotmpl` já tem env overrides — render tests assertam o output do merge

**Consequences:**
- (+) Each env validado independentemente
- (+) `task helm:test:dev`, `task helm:test:prod` etc. (operator escolhe granularidade)
- (−) ~4× test files per chart (manageable; shared setup via helpers)

### D4 — Test Discovery via Go Build Tags

**Decisão:** Render tests usam build tag `//go:build helm_render` para isolar do existing Ginkgo journey tests.

**Rationale:**
- Existing `infra/tests/e2e/` é Ginkgo + LIVE cluster — `go test ./infra/tests/...` rodaria AMBOS sem tag isolation
- Build tag pattern: `go test -tags helm_render ./infra/tests/helm/...` para render only
- Camunda 2022 precedent: `//go:build integration` para isolar tier 2 do tier 1

**Consequences:**
- (+) CI workflow pode chamar suite específica
- (+) Local dev iterate apenas tests relevantes
- (−) New contributors need to know about the tag

### D5 — Shared Test Helpers em `infra/tests/helm/helpers/`

**Decisão:** Common setup (chart path resolution, options struct, unmarshal helpers) vive em `infra/tests/helm/helpers/` package, NÃO duplicate per chart.

**Rationale:**
- DRY: 19 charts × 4 env suites = 76 test files; shared setup reduces ~30 LOC per file
- Test structure based on Camunda 2022 `TemplateGoldenTest` pattern (shared base struct + per-chart specialization)

**Consequences:**
- (+) Single point of change para test infrastructure
- (+) Easier to evolve patterns (e.g., new env added — update helper, not 19 charts)

### D6 — Sem Golden Files (Diferenciamos do Camunda)

**Decisão:** NÃO adotar golden file approach (rendered YAML stored + compared).

**Rationale:**
- LoopZero schemas (T2.5 ext) já fazem input validation com regression protection
- Golden files duplicam: 19 charts × 4 envs = 76 golden files; cada chart bump = 76 file diffs noisy
- Camunda 2022 used golden because they didn't have schemas — LoopZero doesn't need it
- Field-level Go assertions são mais focadas (test FAILS on specific field, not on whole-yaml diff)

**Consequences:**
- (+) No golden file churn during chart iterations
- (+) Test failures are precise (field X differs, not "manifest changed")
- (−) Some bug classes (e.g., new labels added accidentally) só catchable via golden diff — accept this gap

### D7 — Coverage Targets por Microsoft 2026 Pattern Categories

**Decisão:** Cada chart cobre as 6 categorias (com adaptação):

| Category | Patterns | Status no LoopZero |
|---|---|---|
| Identity & Labels | resource names, 5 standard helm labels, selector alignment | NEW (this plan) |
| Configuration | env-specific configmap data, env var injection | NEW |
| Container | image registry per-env, ports, resource limits | NEW |
| Security | non-root, readOnlyRootFS, dropped caps, AppArmor, seccomp, SA token automount | NEW (complement INF-G08 schema gate) |
| Reliability | startup/liveness/readiness probes, volume mounts | NEW |
| Networking & Scaling | ingress hosts/TLS per-env, service port wiring, HPA bounds per-env | NEW |

**Rationale:**
- Microsoft 2026 documenta estes 6 como categorias industry-standard
- LoopZero schemas (esta sessão) already cobrem schema-side; agora cobre render-side

**Consequences:**
- (+) Mapeamento direto entre patterns e tests
- (+) Easy onboarding para new contributors (categoria → exemplos)
- (−) Initial implementation effort (~150 assertions across 19 charts)

### D8 — Pre-Release: Sem Backward Compatibility Constraint

**Decisão:** Pre-release status (per CLAUDE.md) permite breaking test infrastructure layout.

**Rationale:**
- `infra/tests/helm/` é NEW dir — não há backward compat concerns
- Mesmo se houvesse, pre-release permits.

**Consequences:**
- (+) Free design da estrutura optimal
- (−) None — pre-release gives full freedom

### D9 — CI Integration via drill-hermetic-tests.yml

**Decisão:** Render tests integram-se ao existing `drill-hermetic-tests.yml` workflow (NÃO criar new workflow).

**Rationale:**
- Existing workflow já tem Helm CLI, Go setup, hermetic tests pattern
- Adding 1 step `bash <run helm render tests>` vs full new workflow = less CI complexity
- Path filter já cobre `infra/helm/**` + `charts/**` + `infra/tests/scripts/**`; extend to `infra/tests/helm/**`

**Consequences:**
- (+) Single CI workflow para todos hermetic tests
- (+) Reuse path filter discipline (gate added 2026-05-20 commit `5a8ea651`)

### D10 — Plain YAML env stubs em `infra/tests/helm/values/`, NÃO `.gotmpl`

**Decisão:** Criar `infra/tests/helm/values/<env>/<chart>.yaml` (plain YAML stubs, sem helmfile templating directives) para test-only env overrides. Manual sync com `infra/helmfile/values/<env>/<chart>.yaml.gotmpl` é trade-off aceito.

**Rationale:**
- Edge case EC-1 (review 2026-05-20): helmfile values são `.gotmpl` files (contém `{{ requiredEnv "..." }}`, `{{ .Values.X }}` directives processed by helmfile, NOT helm). `helm.RenderTemplate` falha ao parsear template directives como YAML.
- Alternative considered (rejected): pre-process `.gotmpl` via `helmfile template-values` então pass output to helm. Rejeitado porque adiciona helmfile como hard dependency dos tests + duplicates env values resolution já feito em CI workflows.
- Manual sync overhead: low — env-specific overrides são small (typically 5-20 lines per chart per env). Drift between helmfile values + test stubs is caught via Phase 3 tests asserting env-specific fields.

**Consequences:**
- (+) Tests rodam SEM helmfile dependency
- (+) Test stubs são explicit, readable, focused on what asserts
- (−) Manual sync overhead quando helmfile values mudam (mitigated via Phase 3 assertions catching drift)
- (−) Test env stubs NÃO são source of truth — helmfile values são; este é INTENTIONAL (tests validate that chart renders CORRECTLY given specific values, not that helmfile values are CORRECT)

### D11 — Helm Dependency Resolution Programatic via helpers.go

**Decisão:** `helpers.NewOptions` invoca `helm dep update <chartPath>` programaticamente antes de RenderTemplate. CI workflow não precisa pre-step.

**Rationale:**
- Edge case EC-2 (review 2026-05-20): charts com `dependencies:` block (loopzero-platform, loopzero-api, loopzero-app-operator, loopzero-dashboard, loopzero-tenant-operator, loopzero-platform-operator) precisam `helm dep update` para popular `<chart>/charts/<dep>/` subdirectory antes de render.
- Fresh CI checkout (e.g., GitHub Actions runner) não tem dep cache → fail without dep update.
- Operator local geralmente tem cache OK; mascara o bug em dev. Programmatic invocation closes the parity gap (CTO Rule 0 "ambiente local fidedigno").
- Alternative considered (rejected): CI workflow pre-step `for c in charts/*/; do helm dep update $c; done` — adds workflow complexity + breaks if charts/ dirs grow.

**Consequences:**
- (+) Tests self-contained — invocation alone is sufficient
- (+) Same behavior local + CI (parity restored)
- (+) Idempotent — dep update is no-op if already up-to-date
- (−) Slight runtime overhead per test invocation (~200ms per chart for dep update) — mitigated via cache + parallelism

### D12 — Library Charts Auto-Exempt

**Decisão:** Charts com `Chart.yaml type: library` são auto-exempt do scope render-time test. Identified via `helpers.IsLibraryChart()` check.

**Rationale:**
- Edge case EC-3 (review 2026-05-20): loopzero-common é library chart — emits zero manifests; `helm template` produces empty output; render-and-assert pattern não aplica.
- Same pattern já estabelecido em `helm-charts-schema-coverage_test.sh` (commit 5d947698) — library charts auto-exempt via `is_library_chart()` helper.
- Consistent treatment: schemas exempt library; render tests exempt library.

**Consequences:**
- (+) Zero false failures from rendering library charts
- (+) Discipline: library chart testing é SEPARATE concern (test consumer charts that import library partials)
- (−) Library chart bugs (e.g., broken `loopzero-common.fullname` partial) só caught via consumer-chart render tests — accept this gap (consumer tests catch via behaviour)

## Dependency Graph

```
Phase 0 (Governance) ──▶ Phase 1 (Infra)
                            │
                            ▼
                        Phase 2 (Pilot 1 chart)
                            │
                            ▼
                        Phase 3 (Roll out 19 charts)
                            │
                            ▼
                        Phase 4 (CI integration)
                            │
                            ▼
                        Phase 5 (Operator handoff + docs)
```

**Sequential blockers:**
- Phase 1 → 2: precisa Terratest installed + helpers package created
- Phase 2 → 3: pilot proves pattern works before scale
- Phase 3 → 4: tests must exist before CI wires them
- Phase 4 → 5: CI must be green before operator handoff

**Parallel opportunities:**
- Phase 3 charts podem ser paralelos entre si (cada chart é isolated)

---

## Phase 0: Governance + ADR Ratification

**Objective:** Document the 9 ADRs (D1-D9) + obtain operator ratification for D1 (Terratest adoption) + D7 (6 categories scope).

### T0.1 — Ratify ADRs via Operator Review

#### Objective
ADRs D1-D9 são ratificadas por operator antes de scaffolding begin.

#### Evidence
- Pre-release governance (CLAUDE.md): "Every relevant change must pass through mandatory technical review before implementation"
- T2 governance class: contracts + pipeline + operations impact

#### Files to edit
```
docs/plans/2026-05-20-helm-render-time-testing-terratest-plan.md — esta versão v1.0 contains ADRs D1-D9
.claude/knowledge-base/meetings/MEET-2026-05-20-helm-render-tests.md (NEW) — ratification record
```

#### Deep file dependency analysis
- Plan file: this v1.0 doc; após edits → v1.1 with ratification reference
- MEET file: novo, follows existing MEET pattern (e.g., `MEET-2026-05-20-infra-standardization.md`)

#### Deep Dives
None — purely governance.

#### Tasks
1. Operator reads plan v1.0
2. Operator either ratifies or proposes amendments
3. Write MEET record with ratification statement
4. Update plan with v1.1 + ratification cross-ref

#### TDD
N/A — governance task, no code.

#### Acceptance Criteria
- [ ] MEET-2026-05-20-helm-render-tests.md exists com ratification statement
- [ ] Plan referenced as v1.1 com link para MEET

#### DoD
- [ ] Ratification documented in knowledge-base

---

## Phase 1: Test Infrastructure Setup

**Objective:** Add Terratest deps + create `infra/tests/helm/` directory structure + shared helpers package.

### T1.1 — Add Terratest Dependencies

#### Objective
`infra/tests/go.mod` ganha Terratest + stretchr/testify deps explicit (testify já está indirect).

#### Evidence
Current `infra/tests/go.mod` (verified earlier in session):
- Go 1.25.10 ✓
- Ginkgo + Gomega ✓
- client-go v0.35.2 ✓
- **Terratest: NOT present** — needs add

#### Files to edit
```
infra/tests/go.mod — add direct require for terratest + testify
infra/tests/go.sum — auto-regenerated by `go mod tidy`
```

#### Deep file dependency analysis
- `go.mod`: existing module `github.com/loopzero/loopzero/infra/tests`. Adding `github.com/gruntwork-io/terratest v0.46.16` (latest stable) + `github.com/stretchr/testify v1.10.0`
- `go.sum`: cryptographic checksums; regenerated via `go mod tidy`

#### Deep Dives

**Terratest version choice**: v0.46.16 é latest stable as of 2026. Includes helm + k8s modules. Compatible com Go 1.22+.

**Why explicit testify**: já indirect, but render tests use it extensively. Explicit require ensures version stability.

#### Tasks
1. `cd infra/tests && go get github.com/gruntwork-io/terratest@v0.46.16`
2. `go get github.com/stretchr/testify@v1.10.0`
3. `go mod tidy`
4. Verify no transitive conflicts: `go build ./...`

#### TDD
```
RED:     N/A — dependency add, no logic
GREEN:   `go mod tidy` succeeds without errors
REFACTOR: None expected
VERIFY:  cd infra/tests && go build ./... — should succeed
```

#### Acceptance Criteria
- [ ] `grep terratest infra/tests/go.mod` returns direct require
- [ ] `go build ./...` from `infra/tests/` succeeds
- [ ] No vulnerability warnings (`govulncheck`)

#### DoD
- [ ] go.mod + go.sum committed
- [ ] `go build ./...` exit 0

### T1.2 — Create Directory Structure + Helpers Package

#### Objective
Establish `infra/tests/helm/` layout + shared helpers package.

#### Evidence
Per D5: shared setup reduces ~30 LOC per file. Per D3: per-env structure.

#### Files to edit
```
infra/tests/helm/helpers/helpers.go (NEW) — chart path resolution, options builder, unmarshal helpers
infra/tests/helm/helpers/helpers_test.go (NEW) — self-tests
infra/tests/helm/README.md (NEW) — doc explaining structure + how to add tests
```

#### Deep file dependency analysis
- `helpers.go`: novo arquivo, ~100 LOC. Exports:
  - `ChartPath(chartName string) string` — resolves `infra/helm/<name>` ou `charts/<name>`
  - `NewOptions(envValuesFile string) *helm.Options` — builds Terratest helm.Options com merged values
  - `RenderTemplate(t *testing.T, opts *helm.Options, chart, release string, templates []string) string` — wrapper around `helm.RenderTemplate`
  - `MustUnmarshalDeployment(t *testing.T, yaml string) appsV1.Deployment` — typed unmarshal com error handling

- `helpers_test.go`: validates that helpers work — render a known chart, assert it unmarshales

#### Deep Dives

**Why a helpers package?**: Microsoft 2026 example shows duplicate setup per chart (verbose). Camunda 2022 base struct pattern is cleaner. We adopt Camunda pattern but as functional Go package (not struct).

**Chart path resolution algorithm**:
```go
func ChartPath(chartName string) (string, error) {
  // Try infra/helm/ first (bootstrap charts)
  if exists("infra/helm/" + chartName + "/Chart.yaml") {
    return abs("infra/helm/" + chartName), nil
  }
  // Fall back to charts/ (OCI published)
  if exists("charts/" + chartName + "/Chart.yaml") {
    return abs("charts/" + chartName), nil
  }
  return "", fmt.Errorf("chart %q not found in infra/helm/ or charts/", chartName)
}
```

#### Tasks
1. Create directory `infra/tests/helm/helpers/`
2. Write `helpers.go` with 4 exported functions
3. Write `helpers_test.go` validating resolution + render flow
4. Write `infra/tests/helm/README.md` with structure overview

#### TDD
```
RED:     TestChartPathResolution() — paths for known charts (loopzero-api, postgresql) should resolve; unknown chart should error
RED:     TestNewOptionsMergesValues() — base values.yaml + values-drill.yaml should produce merged Options
RED:     TestRenderTemplateProducesOutput() — render loopzero-api → non-empty output
RED:     TestMustUnmarshalDeployment() — given valid YAML, returns populated Deployment struct
GREEN:   Implement helpers.go
REFACTOR: None expected
VERIFY:  cd infra/tests && go test -tags helm_render ./helm/helpers/...
```

#### Acceptance Criteria
- [ ] `infra/tests/helm/helpers/helpers.go` exists
- [ ] 4 functions exported
- [ ] All RED tests transitioned to GREEN
- [ ] `go vet ./helm/helpers/...` clean
- [ ] Pass: /code-audit lint check (zero warnings)
- [ ] Pass: /code-audit size check (file ≤ 500 lines)

#### DoD
- [ ] helpers package self-tests pass
- [ ] README documents pattern
- [ ] Committed

### T1.3 — Plain YAML Env Stubs (NEW — EC-1 fix per D10)

#### Objective
Create `infra/tests/helm/values/<env>/<chart>.yaml` plain YAML stubs (NOT .gotmpl) replicating env-specific overrides for test consumption.

#### Evidence
- Edge case review (2026-05-20) EC-1: `infra/helmfile/values/<env>/<chart>.yaml.gotmpl` files são helmfile-templated (contém `{{ requiredEnv "..." }}` directives) → `helm.RenderTemplate` falha ao parsear como YAML.
- Confirmed via `head -20 infra/helmfile/values/drill/loopzero-api.yaml.gotmpl` (review evidence).

#### Files to edit
```
infra/tests/helm/values/dev/<chart>.yaml (NEW × per applicable chart)
infra/tests/helm/values/drill/<chart>.yaml (NEW × per applicable chart)
infra/tests/helm/values/prod/<chart>.yaml (NEW × per applicable chart)
infra/tests/helm/values/README.md (NEW) — explains sync protocol with helmfile values
```

#### Deep file dependency analysis
- Per chart per env: ~10-30 LOC plain YAML (subset of helmfile values relevant to render assertions)
- README documents: this is test-only override; source of truth is helmfile values; manual sync required when helmfile values change
- Phase 3 assertions catch drift (if test stub diverges from intent, assertion fails)

#### Deep Dives

**Sync protocol** (documented in values README):
1. Helmfile values change in `infra/helmfile/values/<env>/<chart>.yaml.gotmpl`
2. Operator MUST manually sync relevant fields to `infra/tests/helm/values/<env>/<chart>.yaml`
3. Phase 3 render tests catch divergence via env-specific assertions
4. NO automated sync — explicit human review of what test should validate

**Initial scope (T1.3 only creates stubs for charts covered in Phase 2)**:
- `infra/tests/helm/values/{dev,drill,prod}/loopzero-api.yaml` — 3 files for pilot
- Other charts get their stubs as part of Phase 3 (T3.1, T3.2)

#### Tasks
1. Create directory `infra/tests/helm/values/{dev,drill,prod}/`
2. Manual create `loopzero-api.yaml` per env with relevant fields (image registry, replicas, resources)
3. Write README explaining sync protocol
4. Verify rendering via `helm template charts/loopzero-api -f infra/tests/helm/values/drill/loopzero-api.yaml`

#### TDD
```
RED:     test_env_stub_renders_without_error — helm template + drill stub succeeds
GREEN:   stubs created with sufficient content
REFACTOR: None expected
VERIFY:  helm template charts/loopzero-api -f infra/tests/helm/values/drill/loopzero-api.yaml
```

#### Acceptance Criteria
- [ ] 3 env stubs exist for loopzero-api (dev, drill, prod)
- [ ] Each is plain YAML (no `{{` directives)
- [ ] `helm template charts/loopzero-api -f <stub>` succeeds without error
- [ ] README documents sync protocol
- [ ] Pass: /code-audit size (each stub ≤100 lines)

#### DoD
- [ ] 3 stubs committed
- [ ] README committed
- [ ] Manual render validates each stub

### T1.4 — helpers.NewOptions Invokes helm dep update (NEW — EC-2 fix per D11)

#### Objective
`helpers.NewOptions` programmatically invokes `helm dep update <chartPath>` before any render attempt.

#### Evidence
- Edge case review (2026-05-20) EC-2: 5+ charts em scope (loopzero-platform, loopzero-api, loopzero-app-operator, loopzero-dashboard, loopzero-tenant-operator, loopzero-platform-operator) têm `dependencies:` block puxando loopzero-common via `file://../loopzero-common`.
- Fresh CI checkout sem cache → render fails with `Error: found in Chart.yaml, but missing in charts/ directory: loopzero-common`.
- Operator local typically has cache OK — masks bug em dev (CI/local parity violation per CTO Rule 0).

#### Files to edit
```
infra/tests/helm/helpers/helpers.go — extend NewOptions to invoke helm dep update
infra/tests/helm/helpers/helpers_test.go — add test that NewOptions(chart_with_deps) succeeds in fresh state
```

#### Deep file dependency analysis
- helpers.go: add `runHelmDepUpdate(chartPath string) error` private function; call from NewOptions before returning
- helpers_test.go: add test that creates temp dir, copies chart with deps, deletes cached charts/ dir, calls NewOptions, asserts success

#### Deep Dives

**Algorithm**:
```go
func NewOptions(envValuesFile string) (*helm.Options, error) {
    chartPath := /* resolved earlier in flow */
    // Ensure dependencies are resolved (idempotent if already up-to-date)
    if hasDependencies(chartPath) {
        if err := runHelmDepUpdate(chartPath); err != nil {
            return nil, fmt.Errorf("helm dep update failed: %w", err)
        }
    }
    return &helm.Options{
        ValuesFiles: []string{envValuesFile},
    }, nil
}
```

**Performance**: `helm dep update` is fast (~200ms per chart for cache hit). Acceptable for <60s CI budget.

#### Tasks
1. Extend helpers.go with `hasDependencies` + `runHelmDepUpdate` functions
2. Integrate into NewOptions flow
3. Add test that exercises dep-update path
4. Verify against fresh-state simulation

#### TDD
```
RED:     test_NewOptions_resolves_dependencies — chart with deps + fresh state (no charts/ subdir) → NewOptions succeeds
RED:     test_NewOptions_idempotent_dep_update — calling NewOptions twice doesn't fail
GREEN:   helpers.go extended
REFACTOR: None expected
VERIFY:  go test -tags helm_render ./helm/helpers/...
```

#### Acceptance Criteria
- [ ] NewOptions invokes helm dep update for charts with deps
- [ ] Idempotent (calling 2× doesn't error)
- [ ] Test confirms fresh-state recovery
- [ ] Pass: /code-audit lint

#### DoD
- [ ] Tests pass
- [ ] Self-contained (no manual pre-step needed)

---

## Phase 2: Pilot — 1 Chart End-to-End

**Objective:** Implement full test suite for 1 chart (loopzero-api — most-used custom chart) to validate pattern works at scale.

### T2.1 — loopzero-api Defaults Test Suite

#### Objective
`infra/tests/helm/loopzero-api/defaults_test.go` covers Microsoft 2026 6 categories for default values.

#### Evidence
- loopzero-api is most-deployed custom chart (commits 56c33f8d, 9d069a45 added/refined its schema)
- 22 bugs caught in session highlight need for render validation (B17 notifications, B50 port)

#### Files to edit
```
infra/tests/helm/loopzero-api/defaults_test.go (NEW) — render with values.yaml only, assert all 6 categories
```

#### Deep file dependency analysis
- defaults_test.go: ~200 LOC, table-driven for each category. Imports `helpers` package.

#### Deep Dives

**Test structure (per Microsoft 2026 + D7 categories)**:

```go
//go:build helm_render

package loopzeroapi_test

import (
    "testing"
    appsV1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    "github.com/stretchr/testify/require"
    "github.com/loopzero/loopzero/infra/tests/helm/helpers"
)

func TestLoopZeroAPI_Defaults_IdentityAndLabels(t *testing.T) {
    opts := helpers.NewOptions("")  // no env override
    output := helpers.RenderTemplate(t, opts, "loopzero-api", "loopzero-api-test",
        []string{"templates/deployment.yaml"})
    var deploy appsV1.Deployment
    helpers.MustUnmarshalK8SYaml(t, output, &deploy)
    
    // 5 standard helm/k8s labels
    labels := deploy.ObjectMeta.Labels
    require.Equal(t, "loopzero-api", labels["app.kubernetes.io/name"])
    require.Equal(t, "loopzero-api-test", labels["app.kubernetes.io/instance"])
    require.NotEmpty(t, labels["app.kubernetes.io/version"])
    require.Equal(t, "Helm", labels["app.kubernetes.io/managed-by"])
    require.NotEmpty(t, labels["helm.sh/chart"])
    
    // Selector alignment
    selectorMatch := deploy.Spec.Selector.MatchLabels
    podLabels := deploy.Spec.Template.ObjectMeta.Labels
    require.Equal(t, selectorMatch["app.kubernetes.io/name"],
                     podLabels["app.kubernetes.io/name"])
}

func TestLoopZeroAPI_Defaults_Container(t *testing.T) {
    // image registry, ports, resource limits
}

func TestLoopZeroAPI_Defaults_Security(t *testing.T) {
    // non-root, readOnlyRootFS, dropped caps, seccomp, automount=false
}

func TestLoopZeroAPI_Defaults_Reliability(t *testing.T) {
    // startup/liveness/readiness probes
}

func TestLoopZeroAPI_Defaults_Networking(t *testing.T) {
    // service port wiring matches container port
}

// HPA / ConfigMap categories may have own files when chart emits them
```

**Per categoria — exemplos de assertions** (from Microsoft 2026 patterns):

| Category | Example assertion |
|---|---|
| Identity | `require.Equal(t, "loopzero-api", deploy.Name)` |
| Labels | `require.Equal(t, "loopzero-api", deploy.Labels["app.kubernetes.io/name"])` |
| Selector alignment | `require.Equal(t, deploy.Spec.Selector.MatchLabels, deploy.Spec.Template.ObjectMeta.Labels)` |
| Container | `require.Equal(t, "registry.digitalocean.com/loopzero-system/loopzero-api", deploy.Spec.Template.Spec.Containers[0].Image)` |
| Security | `require.True(t, *deploy.Spec.Template.Spec.SecurityContext.RunAsNonRoot)` |
| Reliability | `require.NotNil(t, deploy.Spec.Template.Spec.Containers[0].LivenessProbe)` |
| Networking | matching deploy.containerPort vs service.targetPort |

#### Tasks
1. Create `infra/tests/helm/loopzero-api/defaults_test.go`
2. Implement 6 category test functions (or sub-tests in single function)
3. Run: `go test -tags helm_render ./helm/loopzero-api/...`
4. Iterate until all assertions pass with rendered template default values

#### TDD
```
RED:     TestLoopZeroAPI_Defaults_IdentityAndLabels — assertions about 5 labels + selector alignment
RED:     TestLoopZeroAPI_Defaults_Container — image registry pattern, port = 13002 (B50 protection at render-time)
RED:     TestLoopZeroAPI_Defaults_Security — all 6 invariants from INF-G08
RED:     TestLoopZeroAPI_Defaults_Reliability — 3 probes present
RED:     TestLoopZeroAPI_Defaults_Networking — service.port matches container.port
GREEN:   Tests pass against actual `helm template` of loopzero-api with default values.yaml
REFACTOR: Extract shared "render deployment" boilerplate into helpers if duplicate becomes obvious
VERIFY:  go test -tags helm_render -v ./helm/loopzero-api/...
```

#### Acceptance Criteria
- [ ] 5+ test functions in defaults_test.go
- [ ] All pass with default values.yaml render
- [ ] `go vet ./helm/loopzero-api/...` clean
- [ ] Test runtime <5s
- [ ] Pass: /code-audit lint
- [ ] Pass: /code-audit size (file ≤500 lines — if exceeds, split per category)

#### DoD
- [ ] All TDD tests GREEN
- [ ] Test file committed
- [ ] Validates pilot pattern works

### T2.2 — loopzero-api Per-Env Test Suite (dev + drill + prod)

#### Objective
3 separate test files validating that per-env overrides produce correct outputs.

#### Evidence
- `infra/helmfile/values/dev/`, `drill/`, `prod/` exist with overrides
- Microsoft 2026: "the most common Helm chart bugs is environment drift"

#### Files to edit
```
infra/tests/helm/loopzero-api/dev_test.go (NEW)
infra/tests/helm/loopzero-api/drill_test.go (NEW)
infra/tests/helm/loopzero-api/prod_test.go (NEW)
```

#### Deep file dependency analysis
- Each file: ~80 LOC, focus on env-specific overrides (replicas, registry per-env, ingress host per-env)

#### Deep Dives

**dev_test.go example**:
```go
//go:build helm_render

package loopzeroapi_test

func TestLoopZeroAPI_Dev_ImageRegistry(t *testing.T) {
    opts := helpers.NewOptions("infra/helmfile/values/dev/loopzero-api.yaml.gotmpl")
    output := helpers.RenderTemplate(t, opts, "loopzero-api", "loopzero-api-test",
        []string{"templates/deployment.yaml"})
    var deploy appsV1.Deployment
    helpers.MustUnmarshalK8SYaml(t, output, &deploy)
    
    // dev uses local registry
    image := deploy.Spec.Template.Spec.Containers[0].Image
    require.Contains(t, image, "k3d-loopzero-registry:5000")  // dev-specific
}

func TestLoopZeroAPI_Drill_ImageRegistry(t *testing.T) {
    // drill uses DOCR
    require.Contains(t, image, "registry.digitalocean.com/loopzero-system/")
}

func TestLoopZeroAPI_Prod_HPABounds(t *testing.T) {
    // prod has higher minReplicas
    var hpa autoscalingV2.HorizontalPodAutoscaler
    require.Equal(t, int32(3), *hpa.Spec.MinReplicas)
}
```

#### Tasks
1-3. Create dev/drill/prod test files
4. Validate each env produces expected outputs
5. Confirm env drift CATCHES if you swap values between envs

#### TDD
```
RED:     TestLoopZeroAPI_Dev_ImageRegistry — k3d-loopzero-registry
RED:     TestLoopZeroAPI_Drill_ImageRegistry — registry.digitalocean.com
RED:     TestLoopZeroAPI_Prod_HPABounds — minReplicas ≥ 3
RED:     test that swapping prod values into dev would FAIL the test (regression demo)
GREEN:   tests pass for each env
REFACTOR: shared assertions might move to helpers
VERIFY:  task helm:test:per-env-loopzero-api
```

#### Acceptance Criteria
- [ ] 3 env files committed (dev, drill, prod)
- [ ] Each env has ≥3 env-specific assertions
- [ ] All tests GREEN
- [ ] Env swap exercise documents in comment that test catches drift

#### DoD
- [ ] All multi-env tests pass
- [ ] Pattern proven scalable to other charts

---

## Phase 3: Roll Out — 19 Charts

**Objective:** Apply pilot pattern to all 19 custom LoopZero charts.

### T3.1 — Generate Test Skeleton Per Chart (10 charts)

#### Objective
Half of the charts get full test suite (defaults + per-env).

**Charts in scope (10 — first batch by importance):**
1. loopzero-cloud (custom)
2. loopzero-platform (umbrella)
3. loopzero-dashboard
4. loopzero-admission-policies (charts/ + infra/helm/ both)
5. loopzero-tenant-operator
6. loopzero-app-operator
7. loopzero-platform-operator
8. loopzero-preview-operator
9. loopzero-crds
10. loopzero-monitoring

#### Evidence
These 10 are the OCI-published charts consumed by ArgoCD at Day-2 runtime. Render-time correctness directly affects production deploys.

#### Files to edit
For each chart:
```
infra/tests/helm/<chart>/defaults_test.go (NEW)
infra/tests/helm/<chart>/dev_test.go (NEW)
infra/tests/helm/<chart>/drill_test.go (NEW)
infra/tests/helm/<chart>/prod_test.go (NEW)
```

#### Deep file dependency analysis
- 10 charts × 4 files = 40 new files
- Each file ~80-200 LOC
- All share `helpers` package — no duplication

#### Deep Dives

**Per-chart customizations** (some charts don't have all categories — adapt):

| Chart | Categories Applicable |
|---|---|
| loopzero-cloud | All 6 |
| loopzero-platform (umbrella) | Identity, Configuration only (no pods of its own) |
| loopzero-dashboard | All 6 |
| loopzero-admission-policies | Identity, Configuration (VAP/ClusterImagePolicy CRs) |
| loopzero-tenant-operator | All 6 |
| loopzero-app-operator | All 6 |
| loopzero-platform-operator | All 6 |
| loopzero-preview-operator | All 6 |
| loopzero-crds | Identity only (just CRDs) |
| loopzero-monitoring | Identity, Configuration, Networking (Prometheus rules) |

#### Tasks
For each of 10 charts:
1. Create directory
2. Generate 4 test files following loopzero-api pattern
3. Customize assertions per chart's actual templates
4. Run tests; iterate until GREEN

#### TDD
```
RED:     For each chart: ~3-6 RED tests per categoria applicable
GREEN:   Tests pass for each chart's rendered defaults + each env override
REFACTOR: Move newly-discovered patterns into helpers
VERIFY:  task helm:test:all-charts
```

#### Acceptance Criteria
- [ ] 10 charts × 4 files = 40 new test files
- [ ] Each file has ≥3 assertions
- [ ] Total assertion count: ≥120 across 10 charts
- [ ] All tests GREEN
- [ ] `task helm:test:all-charts` runtime: <30s

#### DoD
- [ ] All 10 chart test suites pass
- [ ] Committed

### T3.2 — Remaining 8 Charts (loopzero-common EXEMPT per D12 — EC-3 fix)

#### Objective
Apply same pattern to remaining 8 custom LoopZero charts. **`loopzero-common` library chart EXEMPT per D12.**

**Charts in scope (8):**
- charts/: loopzero-build-cwts only (loopzero-common library exempt — emits zero manifests)
- infra/helm/ custom (7): admission-policies, argocd, cert-manager, chaos-mesh, cloudflared, harbor, network-policies, postgresql, gitea

**Library chart handling (D12):**
- `loopzero-common` has `Chart.yaml type: library` → render produces empty output → no render-time tests applicable
- helpers function `IsLibraryChart(chartPath)` returns true for loopzero-common
- Skipped via build-tag conditional OR explicit skip in T3.2 test discovery
- Coverage gate (existing `helm-charts-schema-coverage_test.sh`) already exempts library; same treatment here

#### Evidence
Schemas added this session (Phase v1.2 T2.5 ext) for all of these — render tests complement.

#### Files to edit
```
infra/tests/helm/<each-chart>/{defaults,dev,drill,prod}_test.go (NEW × 9 × 4 = 36 files)
```

Exception: `loopzero-common` is library chart — single `defaults_test.go` validates library function rendering only.

#### Tasks (per chart)
1-4. Same as T3.1

#### TDD
Same pattern as T3.1.

#### Acceptance Criteria
- [ ] 9 charts coverage
- [ ] All tests GREEN
- [ ] Total assertion count session-wide: ≥150

#### DoD
- [ ] All 19 charts covered
- [ ] CI runtime budget met (<60s for full suite)

---

## Phase 4: CI Integration

**Objective:** Wire render tests into `drill-hermetic-tests.yml` workflow.

### T4.1 — Add Render Tests Step to drill-hermetic-tests.yml

#### Objective
CI runs render tests on every PR touching helm charts or test code.

#### Evidence
Existing workflow `.github/workflows/drill-hermetic-tests.yml` already has Go setup + helm install. Adding 1 step is minimal change.

#### Files to edit
```
.github/workflows/drill-hermetic-tests.yml — add 1 step + extend path filter
```

#### Deep file dependency analysis
- Workflow YAML: add step "Render-time tests" after "plan-2026-05-20-criteria"
- Path filter: add `infra/tests/helm/**` + `infra/tests/go.mod`

#### Deep Dives

**Step content**:
```yaml
- name: T2.5 ext+1 — helm render-time tests (Terratest)
  working-directory: infra/tests
  run: |
    go test -tags helm_render -timeout 5m -v ./helm/...
```

#### Tasks
1. Edit workflow YAML — add step
2. Extend path filter to trigger on test/chart changes
3. Validate workflow YAML still parses

#### TDD
```
RED:     Workflow YAML syntax valid (yaml.safe_load)
RED:     ci-path-filter-coverage_test.sh still GREEN with extended filter
GREEN:   New step added; existing tests still run
REFACTOR: None
VERIFY:  python3 -c "import yaml; yaml.safe_load(open('.github/workflows/drill-hermetic-tests.yml'))"
```

#### Acceptance Criteria
- [ ] YAML valid
- [ ] Step added in correct order (after Python-jsonschema gates)
- [ ] Path filter extended
- [ ] ci-path-filter-coverage_test.sh GREEN

#### DoD
- [ ] CI workflow updated
- [ ] Committed

### T4.2 — Add Taskfile Targets

#### Objective
Operator can run render tests locally easily.

#### Files to edit
```
taskfiles/test.yml — add helm:test, helm:test:dev, helm:test:drill, helm:test:prod, helm:test:<chart>
```

#### Tasks
1. Add 4 env-specific targets
2. Add per-chart parameterized target
3. Test locally: `task helm:test:drill`

#### TDD
```
RED:     task helm:test → fails if tests missing
RED:     task helm:test:dev → only dev tests
GREEN:   targets defined, run target succeeds
REFACTOR: None
VERIFY:  task helm:test
```

#### Acceptance Criteria
- [ ] 5+ task targets added
- [ ] All targets exit 0 after T3 completion
- [ ] `bash -n taskfiles/test.yml` no syntax errors

#### DoD
- [ ] Targets work
- [ ] Documented in helm/README.md

---

## Phase 5: Operator Handoff + Documentation

**Objective:** Operator can iterate on tests + understand patterns.

### T5.1 — Update operator-handoff Doc

#### Files to edit
```
.claude/knowledge-base/concepts/operations/operator-handoff-2026-05-20-plan-v1-2-engineering-complete.md — add render-tests gate row
```

#### Tasks
1. Add "Render-time tests" row to handoff doc tables
2. Reference new pre-flight checks

#### Acceptance Criteria
- [ ] Doc updated

#### DoD
- [ ] Committed

### T5.2 — README + Pattern Catalog

#### Files to edit
```
infra/tests/helm/README.md — extend with categorization + examples
docs/architecture/helm-testing/README.md (NEW) — high-level pattern doc
```

#### Tasks
1. Document 6 categories + assertion patterns
2. Add troubleshooting guide

#### Acceptance Criteria
- [ ] README explains structure
- [ ] Architecture doc links to plan + ADRs

#### DoD
- [ ] Documented

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | No render-time validation today | T1.1, T2.1, T3.1, T3.2 | Add Terratest + 19 chart suites |
| 2 | Multi-env drift bugs uncovered | T2.2, T3.1, T3.2 | Per-env test files (dev/drill/prod) |
| 3 | Label selector drift uncatchable | T2.1 IdentityAndLabels category | Selector alignment assertion |
| 4 | Security removal during refactor | T2.1 Security category | INF-G08 invariants in render-time |
| 5 | B50 (port mismatch) pattern | T2.1 Container category | service.port matches container.port |
| 6 | B17 (notifications.services key) | T3.1 loopzero-* charts conditional tests | render output asserts correct field path |
| 7 | CI fast feedback loop | T4.1 | <60s suite runtime |
| 8 | Operator local iteration | T4.2 | Taskfile targets |
| 9 | Documentation drift | T5.1, T5.2 | Handoff + README |
| 10 | Pre-release governance | T0.1 | ADR ratification |

**Coverage: 10/10 (100%)**

---

## Global Definition of Done

- [ ] All 6 phases completed
- [ ] All tests passing (Go render-tests + existing tier 1 gates)
- [ ] Zero lint warnings (golangci-lint)
- [ ] Pre-release: BC break OK (this is new infrastructure)
- [ ] code-audit checks passing across `infra/tests/helm/...`
- [ ] **`/cross-validation 2026-05-20-helm-render-time-testing-terratest` PASSED** — zero CRITICO, zero GRAVE
- [ ] **CHANGELOG.md hygiene** — entry per phase in `[Unreleased]`
- [ ] Plan-specific criteria:
  - [ ] 19 charts cobertos com render-time tests (v1.2 amendment:
        amended to 18 — the planned 19 included 19 values-only
        directories without Chart.yaml that cannot be rendered;
        true scope is 18 real charts + 2 published-variant pkgs)
  - [ ] ≥150 render assertions total (measured: ~210)
  - [ ] 4 env files per applicable chart (only loopzero-api today —
        other charts have no helmfile env values to test against)
  - [ ] CI runtime: build-gate ≤30s (`task helm:test:vet`)
  - [ ] CI runtime: full suite ≤10min (`task helm:test`) — per
        MEET-ASYNC-AMENDMENT-2026-05-21-001 the original "<60s"
        target was unrealistic at 22 packages × helm dep update
        per process; measured floor is ~6m15s after render cache
        + sync.Once optimization
  - [ ] Zero LIVE cluster dependency
  - [ ] Existing 6 hermetic gates still GREEN (no regression)
- [ ] **Evidence**: N/A — este plano é INFRASTRUCTURE addition pré-release; não move maturity score per se (mas adds tier 2 protection layer)
- [ ] **`/dogfood full` PASSED** (Dogfood Gate)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.**

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure
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
- [ ] No regressions in previously passing flows
- [ ] Observability check passes (logs, metrics, CRs in correct state)
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.

---

## Risk Register

| Risk | Mitigation |
|---|---|
| Terratest API breaking change | Pin v0.46.16, review changelog before any future bump |
| Go module dependency conflict | `go mod tidy` after add; build verification step |
| Tests too slow (>60s budget) | Profile slow charts; parallelize via `t.Parallel()`; cache rendered output |
| False positive on legitimate refactor | Asserts target structural invariants (e.g., selector matches pod labels), not exact field values where flexibility wanted |
| Operator learning curve (Go) | README + pattern catalog + helpers package reduces boilerplate |
| Schemas + render tests redundant | NOT redundant — schemas validate INPUT, render-tests validate OUTPUT (orthogonal coverage) |

---

## Cost Estimate

| Phase | Effort | Risk |
|---|---|---|
| Phase 0 | <1h (governance review) | LOW |
| Phase 1 | ~3h (setup + helpers) | LOW |
| Phase 2 | ~4h (pilot proves pattern) | MEDIUM (uncovers chart-specific issues) |
| Phase 3 | ~12h (19 charts × ~40min each) | MEDIUM (some charts have edge cases) |
| Phase 4 | ~1h (CI wire) | LOW |
| Phase 5 | ~2h (docs) | LOW |
| **Total** | **~23h** | Effort distributed across sprint |

Cloud cost: **$0** (zero LIVE cluster dependency).

---

## References

- Microsoft 2026 article: "Unit Testing Helm Charts with Terratest" — 16 patterns × 6 categories framework
- Camunda 2022 article: "Advanced Test Practices For Helm Charts" — Terratest > Chart Testing comparison + golden files pattern
- Gruntwork 2019 article: "Automated Testing for Kubernetes and Helm Charts using Terratest" — foundational render-and-assert pattern
- `gruntwork-io/terratest` v0.46.16 — https://github.com/gruntwork-io/terratest
- LoopZero plan v1.2 (parent): [`docs/plans/2026-05-20-infra-standardization-and-secrets-plan.md`](./2026-05-20-infra-standardization-and-secrets-plan.md)
- LoopZero schema gates (this session): commit range `7b87117a..HEAD` (37 commits)
