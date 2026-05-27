# Edge Case Review — 2026-05-20-helm-render-time-testing-terratest

**Data:** 2026-05-20
**Plano:** `docs/plans/2026-05-20-helm-render-time-testing-terratest-plan.md`
**Tasks analisadas:** 11 (T0.1, T1.1, T1.2, T2.1, T2.2, T3.1, T3.2, T4.1, T4.2, T5.1, T5.2)
**Edge cases encontrados:** 8 (MUST FIX: 3, SHOULD TEST: 3, DOCUMENT: 2)

---

## MUST FIX

### EC-1: helmfile values são `.gotmpl` (template), não `.yaml` puro — `helm.RenderTemplate` não aceita

- **Task afetada:** T2.2, T3.1, T3.2
- **Família:** Format
- **Cenário:** O plano T2.2 prescreve `helpers.NewOptions("infra/helmfile/values/dev/loopzero-api.yaml.gotmpl")`. Mas esses arquivos são **Go templates processados por helmfile** (contém `{{ requiredEnv "DRILL_HYDRA_SECRET" }}`, `{{ .Values.foo }}`, etc.) — NÃO YAML puro. `helm.RenderTemplate` espera plain YAML values; vai falhar com `error converting YAML to JSON: yaml: invalid map key` quando tentar parsear `{{ ... }}` blocks.

  Confirmado via `head -20 infra/helmfile/values/drill/loopzero-api.yaml.gotmpl` — file contains plain YAML, mas a path `values/drill/<chart>.yaml.gotmpl` é convention do helmfile que sometimes contém template directives.

- **Impacto:** Phase 2 + Phase 3 tests não conseguem renderizar com env-overrides; bloqueia 76 test files (19 charts × 4 env).

- **Fix sugerido:** Plano deve adicionar **T2.0 (pre-task)**: criar `infra/tests/helm/values/<env>/<chart>.yaml` (plain YAML, sem `.gotmpl`) replicando os values do helmfile equivalent, OU pre-processar `.gotmpl` via `helmfile template-values` antes de helm.RenderTemplate. Decisão D10 a adicionar ao plano: usar plain YAML stubs em `infra/tests/helm/values/` para test-only env overrides.

### EC-2: `loopzero-platform` umbrella + outros charts têm `dependencies:` → `helm template` falha sem `helm dep update`

- **Task afetada:** T2.1, T3.1
- **Família:** I/O
- **Cenário:** Confirmed via `grep dependencies charts/*/Chart.yaml` — 5+ charts têm dep blocks: loopzero-platform, loopzero-api, loopzero-app-operator, loopzero-dashboard, loopzero-tenant-operator, loopzero-platform-operator. Cada um depende de `loopzero-common` (file:// local repo). `helm template` exige que `charts/<dep>/` exists em `<chart>/charts/` directory (populated por `helm dep update`). Em fresh checkout (e.g., CI runner), esse subdirectory não existe → render fails with `Error: found in Chart.yaml, but missing in charts/ directory: loopzero-common`.

- **Impacto:** Todos os tests T2.1 + T3.1 falham em fresh CI runner. Operator local que já rodou `task setup` provavelmente tem o cache, mascarando o bug em dev.

- **Fix sugerido:** `helpers.NewOptions` deve invocar `helm dep update` programaticamente antes do RenderTemplate, OU plano adiciona pre-step na CI workflow: `for c in charts/*/; do helm dep update $c; done` antes dos render tests. Adicionar como item explícito em T4.1.

### EC-3: Library chart `loopzero-common` não pode ser renderizada — emite ZERO templates

- **Task afetada:** T3.2
- **Família:** Format
- **Cenário:** Confirmed via `ls charts/loopzero-common/` — has `Chart.yaml` (type: library), `templates/`, `values.yaml`. Mas Helm library charts NÃO emitem manifests; templates definem partials/helpers consumidos por outros charts. `helm template charts/loopzero-common` produz output vazio. Test pattern do plano (render → assert struct fields) não aplica.

- **Impacto:** Plano T3.2 prescreve "loopzero-common single defaults_test.go" — mas que assertions? Sem manifests, não há fields para validar.

- **Fix sugerido:** Plano T3.2 deve **excluir explicitamente loopzero-common** da test suite render-time (escopo NÃO aplica a library charts) OR redefinir test scope para library chart: assert template helpers behavior via `helm template <consumer-chart>` que importa loopzero-common partials, NÃO direct render de loopzero-common. Atualizar D7 categories: library charts auto-exempt (similar ao coverage gate pattern do plan v1.2).

---

## SHOULD TEST

### EC-4: Empty `Containers` slice → `Spec.Containers[0]` panics

- **Task afetada:** T2.1, T3.1, T3.2
- **Teste sugerido:** `helpers.AssertNonEmptyContainers(t, deploy)` — wrapper que faz `require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)` ANTES de qualquer indexing. Test name: `test_helpers_panics_on_empty_containers` (assert helper itself catches the case).

  Real-world trigger: developer adds `{{ if .Values.X }}` around container definition. With X=false, Deployment renders without containers. Without guard, test panics with index out of range instead of clear failure.

### EC-5: Chart path resolution from non-CWD test invocation

- **Task afetada:** T1.2
- **Teste sugerido:** `test_ChartPath_resolves_from_any_cwd` — run helpers test from `/tmp/` instead of `infra/tests/` (via `os.Chdir`). Assert `ChartPath("loopzero-api")` still returns correct abs path.

  Real-world trigger: IDE invocation, `go test ./...` from repo root, or vendor-style test runner. Hardcoded relative paths break.

### EC-6: Tests asserting hardcoded values break on legitimate values.yaml updates

- **Task afetada:** T2.1 (Container category)
- **Teste sugerido:** `test_loopzeroapi_image_registry_format` — assert image MATCHES regex `^[a-z0-9.-]+/[a-z0-9._-]+(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$` (OCI ref format), NÃO hardcoded "registry.digitalocean.com/loopzero-system/loopzero-api". This catches "image has invalid format" without breaking on legitimate registry rename.

  Real-world trigger: drill migration to different registry; image registry bump. Test fails noisily on legitimate change; better to assert STRUCTURE (registry/repo:tag format) than EXACT VALUE for changeable fields.

---

## DOCUMENT

### EC-7: Tests fail noisily on legitimate template refactor

- **Risco aceito:** Microsoft 2026 explicitly: "If someone removes a security control during a refactor, the test fails immediately, not after a security review weeks later. Security becomes a CI gate, not a review checklist." This is **intended behavior** — render-time tests SHOULD fail noisily on template changes. Document in plan README that test failures are expected signal, not bug. Add as note in T5.2.

### EC-8: Not every chart has dev/drill/prod overrides

- **Risco aceito:** Plano T3.2 prescreve 4 env files per chart but some charts (e.g., loopzero-crds — CRD-only) have ZERO env overrides because CRDs don't differ per env. Tests para non-applicable envs should gracefully SKIP via `t.Skip("no <env> override for <chart>")` em vez de fail. Document in T5.2: env coverage = sparse matrix, not full grid.

---

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Não | (test files são wired via go test discovery) |
| Project name vs ID | Não | (test infrastructure, no DB) |
| ArgoCD notifiers not services | Não | (não aplica — render-time tests for K8s manifests) |
| Single ArgoCD App per tenant | Não | (não aplica) |
| CF scan imports conflicting apex | Não | (não aplica) |
| **Inputs from filesystem not validated** | SIM | EC-1 (.gotmpl format), EC-3 (library chart) |
| **External tool dependency unverified** | SIM | EC-2 (helm dep update needed) |
| **Defensive indexing missing** | SIM | EC-4 (empty Containers) |

---

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 0 | 0 | 0 | 0 |
| T1.1 | 0 | 0 | 0 | 0 |
| T1.2 | 1 | 0 | 1 (EC-5) | 0 |
| T2.1 | 2 | 0 | 2 (EC-4, EC-6) | 0 |
| T2.2 | 1 | 1 (EC-1) | 0 | 0 |
| T3.1 | 1 | 1 (EC-2) | 0 | 0 |
| T3.2 | 2 | 1 (EC-3) | 0 | 1 (EC-8) |
| T4.1 | 0 | 0 | 0 | 0 |
| T4.2 | 0 | 0 | 0 | 0 |
| T5.1 | 0 | 0 | 0 | 0 |
| T5.2 | 1 | 0 | 0 | 1 (EC-7) |

**Veredicto: PLANO PRECISA DE AJUSTE**

3 MUST FIX issues bloqueiam Phase 2/3 da execução do plano. Recomendação: incorporar EC-1, EC-2, EC-3 como sub-tasks ANTES de Phase 2 começar:

- **Plano deve adicionar T1.3 — Plain YAML env stubs**: criar `infra/tests/helm/values/<env>/<chart>.yaml` (plain YAML, sem `.gotmpl`) com mesmo content effective do helmfile post-render. Manual sync com helmfile values é trade-off aceito (vs render-helmfile-first complexity).

- **Plano deve adicionar T1.4 — Helm dep update step**: helpers.go invoca `helm dep update <chart>` programaticamente OR T4.1 CI workflow adiciona pre-step. Decisão D11.

- **Plano deve atualizar T3.2 — Library chart exemption**: loopzero-common removed from scope; document as auto-exempt via Chart.yaml `type: library` check.

Após esses 3 ajustes, o plano fica executável. SHOULD TEST items (EC-4, EC-5, EC-6) podem ser incorporados como sub-tests durante T2.1 implementation — não bloqueiam.

DOCUMENT items (EC-7, EC-8) são adições ao plan README durante T5.2.
