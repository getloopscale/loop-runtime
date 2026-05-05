# Charts Improvement Roadmap

**Research date:** 2026-05-04
**Researcher:** Claude (SOTA Research Skill, Opus 4.7)
**Current SOTA score:** 7.2 / 10
**Target SOTA score:** 9.0 / 10 (Sprint 9 exit)

## Executive Summary

A camada `charts/` está **acima da média da indústria** em três áreas: library chart bem desenhado (`loopzero-common`), `values.schema.json` em 12/12 charts deployáveis com `helm lint --strict` em CI, e cosign keyless **dual-signing já operacional** (Fulcio + Rekor + verify step) — algo que pouquíssimas PaaS open-source entregam hoje. Está **abaixo do esperado** em três áreas que vão pesar até a GA: (1) dois charts fantasma (`loopzero-controller`, `loopzero-tenant-controller`) referenciados em `Chart.lock` mas sem `Chart.yaml`, indicando dead code não-removido pós-split de operadores; (2) drift severo entre `infra/helm/admission-policies/` (11 templates) e `charts/loopzero-admission-policies/` (6 templates) — production bootstrap tem postura de segurança **mais estrita** do que o chart publicado, invertendo o esperado; (3) cobertura de helm-unittest em apenas 5 de 13 charts deployáveis, sem chart-testing (ct), sem matriz de versões K8s e sem chartsnap. O gate CH-G11 ("Published charts and bootstrap charts stay deliberately aligned") existe no papel mas não tem ferramenta de drift detection — só CRDs são auditadas, admission-policies e charts duplicados não são. O gap é fechável em 2-3 sprints com ações concretas neste roadmap.

## Competitive Position

| Dimensão | LoopZero | Best-in-class | Gap | Esforço |
|---|---|---|---|---|
| Library chart amplitude | 3/5 (16 helpers) | 5/5 Bitnami common (40+ helpers, `tplvalues.render`, `kubeVersion`, `apiVersion` resolvers) | Falta `tplvalues.render`, `kubeVersion`, `apiVersion`, `secrets.name`, presets de resources, helper de checksum | M |
| `values.schema.json` rigor | 4/5 (Draft-07, regex, enums) | 5/5 (strict mode com `additionalProperties: false` validado contra Helm 3.18.5) | Schemas usam `additionalProperties: true` (compat-safe) — não bloqueiam typos silenciosos em values | S |
| Chart signing | 5/5 (cosign keyless + key-based dual, Fulcio + Rekor + verify) | 5/5 (idem Sigstore, Bitnami) | Nenhum — LoopZero já está no FAANG-bar | — |
| Drift detection (charts vs infra/helm) | 1/5 (apenas CRDs) | 4/5 (Helmfile diff + helm-drift + per-chart matrix) | Sem ferramenta para admission-policies, loopzero-api, loopzero-cloud overlap | S |
| Test coverage (unit + ct + snapshot) | 2/5 (5/13 charts, sem ct, sem snapshot, sem matriz K8s) | 5/5 (Bitnami: ct + helm-unittest + multi-K8s + helm-chartsnap) | Falta cobrir 8 charts, adicionar ct install, K8s matrix, chartsnap | L |
| Umbrella distribution | 4/5 (CI rewrite file://→OCI funciona) | 5/5 (deps OCI nativas com digest pinning) | `Chart.yaml` em dev usa file:// — código de DEV ≠ código de RELEASE | M |
| CRD drift detection | 4/5 (count + name match em CI) | 5/5 (kubebuilder controller-gen + diff semântico de spec) | Drift detector compara só nomes, não spec interno (mudança em `spec.versions[].schema` passaria) | S |
| Anti-patterns (dead code, naming) | 2/5 (`loopzero-controller` + `loopzero-tenant-controller` fantasmas no `Chart.lock`) | 5/5 | Dead code óbvio que não foi removido pós-ADR-0023 split | XS |
| SBOM + provenance attestation | 3/5 (cosign sign sim, SBOM não) | 5/5 (cosign attest + SLSA provenance) | Imagens têm syft SBOM (Sprint 6); charts não têm SBOM attestation | M |
| Polaris / Conftest / Kyverno admission gates | 2/5 (Polaris baseline-first, sem Conftest, sem severity=danger) | 5/5 | MEET-062 condition #1 ainda em continue-on-error baseline | M |

## Quick Wins (1-2 sessões cada)

1. **DELETE `loopzero-controller` e `loopzero-tenant-controller` ghost charts** → remove 2 entradas de `loopzero-platform/Chart.lock` (linhas 23-25), 2 dependencies de `loopzero-platform/Chart.yaml`, 2 entradas de `loopzero-platform/values.yaml` (linhas 25-26). Esses charts não têm `Chart.yaml` (apenas `charts/loopzero-common-0.2.0.tgz` vendorada) e foram marcados como deletados no T1.5 (ADR-0023) mas continuam como dependências do umbrella. **Impacto:** elimina dead code que `helm dependency update` silenciosamente ignora, fecha auditoria do split de operadores, reduz superfície do umbrella de 12 → 10 deps. **Risco:** zero (já não renderizam).

2. **Fechar drift `loopzero-admission-policies` ↔ `infra/helm/admission-policies`** → portar 5 templates faltantes para o chart publicado: `cluster-image-policy.yaml` (cosign warn-mode), `policy-controller-alerts.yaml`, `tenant-namespace-alerts.yaml`, `vap-no-runtime-image-mutations.yaml`, `vap-tenant-namespace.yaml`. **Impacto:** ArgoCD-distributable passa a ter mesma postura de segurança que bootstrap; encerra inversão atual onde production tem mais policy do que published; valida CH-G11 com evidência. **Risco:** baixo — basta `cp` + ajustar values + smoke test.

3. **Adicionar drift detector script `task helm:audit-drift`** → bash script que compara helpers e templates compartilhados entre `infra/helm/<X>` e `charts/loopzero-<X>` para os 2 overlaps reais (`loopzero-api`, `loopzero-cloud`) e 1 conceitual (`loopzero-admission-policies` ↔ `admission-policies`). Output: lista de templates faltantes em cada lado + diff de defaults críticos (`securityContext`, `resources`, `priorityClassName`). Roda em CI como step `audit-drift` no `helm-charts.yml`. **Impacto:** transforma CH-G11 de paper-gate em automação. **Risco:** baixo — script é puro filesystem.

4. **Estender `crd-sync` job para diff semântico de spec** → atualmente compara só `metadata.name` (linha 233-236 de `helm-charts.yml`). Adicionar `yq '.spec' src.yaml` vs `yq '.spec' dst.yaml` com normalização. **Impacto:** detecta mudanças em `spec.versions[].schema` que hoje passam silenciosamente. **Risco:** baixo — yq já está instalado no CI.

5. **Adicionar helm-unittest a `loopzero-cloud`, `loopzero-dashboard`, `loopzero-monitoring`, `loopzero-admission-policies`** → mesmo padrão das suites existentes em `loopzero-api/tests/` (security-context, networkpolicy, servicemonitor). 4 charts × 3 suites × ~6 asserts cada = ~72 novos asserts. **Impacto:** dobra cobertura de unit tests em charts (5/13 → 9/13). **Risco:** baixo — copy-paste de suite existente + ajuste de paths.

## Sprint 9 Targets

1. **`loopzero-common` v0.3.0 — adicionar 6 helpers Bitnami-style** → arquivos afetados: `charts/loopzero-common/templates/_helpers.tpl`, todos os 9 charts deployáveis que dependem.
   - `loopzero-common.tplvalues.render` — para renderizar values com templating (padrão Bitnami; útil em annotations, labels custom)
   - `loopzero-common.kubeVersion` — guard para incompatibilidade de API (ex: `policy/v1` vs `policy/v1beta1`)
   - `loopzero-common.checksumAnnotations` — extrai padrão duplicado em `loopzero-api/templates/deployment.yaml:16-17`, `loopzero-tenant-operator/templates/deployment.yaml:16-17`, `loopzero-app-operator/.../deployment.yaml`, `loopzero-preview-operator/.../deployment.yaml`, `loopzero-platform-operator/.../deployment.yaml`
   - `loopzero-common.imagePullSecrets` — merger global + chart-local (hoje cada chart implementa direto via `.Values.global.imagePullSecrets`)
   - `loopzero-common.secrets.name` — para `existingSecret` pattern (hoje `loopzero-tenant-operator/templates/deployment.yaml:69-73` reimplementa)
   - `loopzero-common.tmpVolumeMount` — para `readOnlyRootFilesystem: true` compat (hoje `loopzero-tenant-operator/templates/deployment.yaml:96-114` duplica em 4 operadores)
   
   **Impacto:** elimina ~80 linhas de duplicação cross-charts (5 deployments × ~16 linhas), torna `loopzero-common` mais alinhado com Bitnami common. **Risco:** médio — cada novo helper precisa de helm-unittest test em `loopzero-common` (que hoje não tem testes).

2. **CI: chart-testing (ct) install + Kind cluster matrix** → arquivo: `.github/workflows/helm-charts.yml` (novo job `install-test`).
   - Adicionar `ct.yaml` na raiz do repo com `chart-dirs: [charts]`, `validate-chart-schema: true`, `kubernetes-versions: [v1.30.0, v1.31.0, v1.32.0]`.
   - Job paralelo ao `validate` que usa `helm/kind-action@v1.10.0` + `helm/chart-testing-action@v2.6.1` + `ct install --config ct.yaml --upgrade`.
   - Roda em `pull_request` com `ct list-changed` (só charts alterados) para velocidade.
   
   **Impacto:** valida render real em cluster (não só `helm template`), cobre 3 versões K8s, testa upgrade de versão N-1 → N. **Risco:** médio — adiciona ~5min ao CI; precisa garantir que charts são "ct-installable" (alguns operadores precisam CRDs pré-instaladas — pode requerer `pre-install-hook`).

3. **`loopzero-platform/values.schema.json` — schema do umbrella** → arquivo: `charts/loopzero-platform/values.schema.json`.
   - Validar `global.registry` (string + minLength + pattern para FQDN).
   - Enforce `<sub-chart>.enabled` boolean para cada subchart.
   - Adicionar `additionalProperties: true` no top-level (compat-safe Helm 3.18.5) mas `false` em sub-objetos críticos onde queremos pegar typos.
   
   **Impacto:** valida composição da plataforma antes de instalar (typo em `global.registry` ou `loopzero-tenant-operatr.enabled` quebra hoje silenciosamente). **Risco:** baixo — testar com `helm lint --strict charts/loopzero-platform`.

4. **Chartsnap whole-chart snapshot** → arquivo novo: `charts/loopzero-platform/snapshots/` + workflow CI.
   - `helm plugin install https://github.com/jlandowner/helm-chartsnap`
   - `helm chartsnap charts/loopzero-platform -f values-prod.yaml --update` para gerar baseline
   - CI step: `helm chartsnap charts/loopzero-platform -f values-prod.yaml` (sem `--update`) — falha em diff
   - Adicionar `__snapshot__/v2/*` ao `.helmignore`
   
   **Impacto:** detecta drift agregado em todo o umbrella (mudança em `loopzero-common` que afeta 9 charts vira 1 diff revisável). **Risco:** baixo — chartsnap é estável; necessário disciplina de review do diff antes de `--update`.

5. **SBOM attestation por chart com cosign** → arquivo: `.github/workflows/helm-charts.yml` (estender step "Sign charts with cosign").
   - Após `cosign sign`, gerar `syft <chart>.tgz -o cyclonedx-json > sbom.json`
   - `cosign attest --predicate sbom.json --type cyclonedx <REF>`
   - Atualizar `cluster-image-policy.yaml` para validar attestation (futuro Sprint 9)
   
   **Impacto:** charts ganham SBOM (paridade com imagens Sprint 6); permite supply-chain validation downstream. **Risco:** baixo — syft já é dependência transitiva; pipeline já tem cosign + OIDC.

6. **Drift `loopzero-api` published vs bootstrap — documentar OU consolidar** → o drift hoje é grande (12 templates published, ~3 templates bootstrap). Decidir:
   - Opção A (recomendada): manter dual deliberadamente, escrever ADR formalizando "bootstrap = local-dev minimal, published = production-ready"
   - Opção B: consolidar usando `condition` em values para enable/disable templates (e.g., `rbac.create: false` no bootstrap)
   
   **Impacto:** transforma CH-G11 de "diferença não documentada" para "diferença com contrato escrito". **Risco:** zero (decisão arquitetural, não código).

## Post-Launch

1. **Polaris severity=danger gate** → graduar de `continue-on-error: true` (linha ~85 de `helm-charts.yml`) para fail-on-critical. Pré-requisito: revisar baseline atual e fechar findings ou suprimir explicitamente. Per MEET-062 condition #1.

2. **Migrar Helm v3.14.4 → v3.18.x** → testar todas as schemas com strict mode (já que Helm 3.18.5 endureceu validação). Pode revelar bugs latentes em schemas do umbrella.

3. **Substituir `file://` deps em `loopzero-platform/Chart.yaml` dev por OCI digest pin** → seguir padrão Crossplane (`@sha256:...`). Hoje CI rewrite faz isso só no publish. Se dev usar OCI, dev = release. Risco: complica onboarding (cada `helm dependency update` precisa de OCI auth).

4. **Conftest policies para charts** → adicionar regras Rego que validam:
   - Todo Deployment usa `loopzero-common.containerSecurityContext`
   - Todo Service tem `port name` matching ServiceMonitor port
   - Todo template referenciando Secret usa `loopzero-common.secrets.name`
   - `priorityClassName` é definido para platform-critical, ausente para user-facing
   
5. **Helm-drift CI cron** → roda `helm drift run <release>` a cada 6h em produção, alerta quando manifest deployado diverge do chart instalado. Pareia com `helm_release_info{status!="deployed"}` Prometheus rule.

6. **Multi-namespace policy testing com `--policy-namespace-selector`** → para `loopzero-admission-policies`, validar que ClusterImagePolicy aplica corretamente em namespaces opt-in (`policy.sigstore.dev/include: "true"`).

## New SOTA Gates Proposed

### Gate CH-G15: No Ghost Charts In Umbrella

**Objective:** Garantir que toda dependência declarada em `loopzero-platform/Chart.yaml` resolva para um chart com `Chart.yaml` válido e templates renderizáveis.

**Approval criteria:**
- Para cada entrada em `loopzero-platform/dependencies`, existe `charts/<name>/Chart.yaml` válido
- Para cada entrada, `helm template charts/<name>` produz pelo menos um manifesto não-vazio
- CI step `audit-ghost-charts` falha se algum dep é fantasma

**Expected evidence:**
- Job `audit-ghost-charts` em `.github/workflows/helm-charts.yml`
- `loopzero-controller` e `loopzero-tenant-controller` removidos (ou reconstruídos) — hoje violam o gate

**FAANG reference:** Bitnami nunca publica subchart com `Chart.yaml` ausente; é parte do CI de release.

### Gate CH-G16: Drift Detector Between Bootstrap And Published

**Objective:** Operacionalizar CH-G11 com automação. Toda mudança em `infra/helm/<X>/` ou `charts/loopzero-<X>/` que afete os 2 overlaps reais (loopzero-api, loopzero-cloud) ou conceituais (admission-policies, monitoring) emite report obrigatório.

**Approval criteria:**
- Existe `infra/scripts/audit-helm-drift.sh` que detecta:
  - Templates presentes em um lado mas não no outro
  - Diferenças de defaults críticos (`securityContext`, `resources`, `priorityClassName`, `image.repository`)
- CI step `audit-drift` emite warning (não-bloqueante inicialmente, bloqueante após Sprint 9 GA)
- Drift detectado vira issue no PR via comment

**Expected evidence:**
- Script + step CI + 1 ADR documentando "diferença intencional" para cada drift que **deve** persistir
- Hoje: drift entre `loopzero-admission-policies` (6 templates) e `infra/helm/admission-policies` (11 templates) é não-documentado e violador → quick-win #2 fecha.

**FAANG reference:** Crossplane usa `helm-drift` em CI para confirmar que provider chart matches operator state.

### Gate CH-G17: Library Chart Helper Coverage

**Objective:** `loopzero-common` deve fornecer helpers para os 8 padrões duplicados detectados nos templates atuais, eliminando copy-paste cross-chart.

**Approval criteria:**
- `_helpers.tpl` define ao menos: `name`, `fullname`, `labels`, `selectorLabels`, `image`, `podSecurityContext`, `containerSecurityContext`, `serviceAccountName`, `priorityClassName`, `startupProbe`, `affinity`, `topologySpreadConstraints`, `servicemonitor`, `pdb`, `networkpolicy`, **`tplvalues.render`**, **`kubeVersion`**, **`checksumAnnotations`**, **`imagePullSecrets`**, **`secrets.name`**, **`tmpVolumeMount`** (em negrito = novos)
- Cada helper tem teste em `loopzero-common/tests/<helper>_test.yaml`
- Nenhum chart cliente reimplementa lógica equivalente em template próprio

**Expected evidence:**
- 21 helpers documentados em `loopzero-common/README.md`
- Suite helm-unittest cobrindo cada helper (snapshot + targeted asserts)
- `grep -rn 'checksum/config:' charts/loopzero-*/templates/` retorna apenas `{{ include "loopzero-common.checksumAnnotations" . }}` (sem reimplementação inline)

**FAANG reference:** Bitnami common 2.x tem 40+ helpers; cada chart consumer chama via `include` puro, zero lógica duplicada.

### Gate CH-G18: Multi-K8s Version Matrix In Chart-Testing

**Objective:** Charts publicados devem renderizar e instalar em N-1, N, N+1 versões K8s (alinhado com janela de suporte upstream).

**Approval criteria:**
- `ct.yaml` define `kubernetes-versions: [v1.30.0, v1.31.0, v1.32.0]` (atualizar a cada release K8s)
- CI job `install-test` roda `ct install --config ct.yaml` em matriz, falha em qualquer versão
- `kubeconform --kubernetes-version` é aplicado para cada uma das 3+ versões (hoje só 1.31.0)

**Expected evidence:**
- Workflow `helm-charts.yml` job `install-test` com `strategy.matrix.k8s-version`
- Status badge "Charts | K8s 1.30 | 1.31 | 1.32" no README

**FAANG reference:** Elastic helm-charts (`helpers/matrix.yml`); Bitnami testa em GKE/TKG/AKS simultaneamente.

### Gate CH-G19: SBOM Attestation Parity With Container Images

**Objective:** Cada chart publicado tem SBOM CycloneDX attached como cosign attestation, igual aos container images do Sprint 6.

**Approval criteria:**
- CI step `attest-charts-sbom` gera `syft <chart>.tgz -o cyclonedx-json` e roda `cosign attest --predicate sbom.json --type cyclonedx <ref>`
- Attestation é verificável: `cosign verify-attestation --type cyclonedx <ref> --certificate-identity-regexp ...` retorna 0
- Sprint 9 ClusterImagePolicy (admission policy) aceita ou requer attestation

**Expected evidence:**
- Step "Generate + attest SBOM" em `helm-charts.yml`
- 1 chart como amostra com SBOM verificado em DOCR

**FAANG reference:** Sigstore project signs every release with cosign + Syft + Rekor; Bitnami Secure Images shipping com SBOM in-toto.

## Anti-Patterns To Eliminate

1. **Ghost charts em `Chart.lock`** — `loopzero-platform/Chart.lock` linhas 23-25 (loopzero-controller) referenciam dir sem `Chart.yaml`. → DELETE deps + lock + values entries (quick-win #1). Ref: ADR-0023.

2. **Drift silencioso bootstrap → published** — `infra/helm/admission-policies/` tem 5 templates a mais que `charts/loopzero-admission-policies/`. Inversão da expectativa (bootstrap=dev, published=prod). → Portar 5 templates faltantes (quick-win #2).

3. **Padrão `checksum/config` duplicado em N deployments** — 5 deployments de operator + loopzero-api reimplementam linhas 16-17 idênticas: `{{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}`. → Extrair para `loopzero-common.checksumAnnotations` (Sprint 9 #1).

4. **`tmp` emptyDir + volumeMount duplicado em 5 operadores** — `charts/loopzero-tenant-operator/templates/deployment.yaml:96-98 + 111-114` e equivalentes em 4 outros. Padrão obrigatório por `readOnlyRootFilesystem: true` mas não está em helper. → Extrair `loopzero-common.tmpVolumeMount`.

5. **`existingSecret` toggle reimplementado em cada operador** — `charts/loopzero-tenant-operator/templates/deployment.yaml:69-73` e equivalentes. → `loopzero-common.secrets.name` (padrão Bitnami).

6. **`global.imagePullSecrets` accesso defensivo manual** — toda deployment template faz `{{- with .Values.global.imagePullSecrets }}...{{- end }}` sem usar helper. → `loopzero-common.imagePullSecrets`.

7. **`additionalProperties: true` em todos os schemas** — quick-win-safe contra Helm 3.18.5 mas reduz força do schema. Typos em values passam silenciosamente. → Avaliar `additionalProperties: false` em sub-objetos onde compatibilidade não é crítica (ex: `service.type`, `securityContext`).

8. **CI roda helm-unittest apenas em 5 charts** — `.github/workflows/helm-charts.yml` linha 65 hardcodes lista (`loopzero-api loopzero-tenant-operator loopzero-app-operator loopzero-preview-operator loopzero-platform-operator`). → Trocar para loop sobre `charts/*/tests/` automático + adicionar tests em 4 charts faltantes (quick-win #5).

9. **`loopzero-monitoring` naming colide conceitualmente** — published `loopzero-monitoring` é apenas PrometheusRules + dashboards; stack Prometheus real está em `infra/helm/monitoring/` (não existe — provavelmente em outro chart bootstrap). Renomear para `loopzero-monitoring-rules` reduz ambiguidade. → Quick-win opcional ou adiar.

10. **`loopzero-platform` Chart.yaml dev usa file://, CI rewrite para OCI no publish** — divergência dev/release. Reproduce-locally do umbrella só funciona com `helm dependency update` que resolve file://. Onboarding implícito. → Documentar em README do umbrella (XS) ou migrar para OCI em dev (M, opcional Post-Launch #3).

## Sources

- [Bitnami Common Library Chart](https://github.com/bitnami/charts/tree/main/bitnami/common) — referência para helpers gold-standard (40+ helpers, `tplvalues.render`, `kubeVersion`, apiVersion resolvers).
- [Bitnami Chart Architecture - DeepWiki](https://deepwiki.com/bitnami/charts/2-chart-architecture-and-common-patterns) — síntese dos padrões compartilhados (config via ConfigMap + checksum auto-restart, image config, health probes, resource presets).
- [Best Practices for Hardening Helm Charts (Broadcom)](https://techdocs.broadcom.com/us/en/vmware-tanzu/bitnami-secure-images/bitnami-secure-images/services/bsi-doc/apps-tutorials-best-practices-hardening-charts-index.html) — runAsUser/fsGroup/PSA pattern; FIPS/STIG compliance.
- [helm-unittest GitHub](https://github.com/helm-unittest/helm-unittest) — BDD framework, snapshot via `matchSnapshot`, JUnit output para CI.
- [Helm Chart Testing in Production (Vazquez, Jan 2026)](https://alexandre-vazquez.com/helm-chart-testing-best-practices/) — pipeline order: lint → kubeconform → unittest → ct → Conftest → Trivy → Kyverno.
- [How to Validate Helm Charts (OneUptime, Feb 2026)](https://oneuptime.com/blog/post/2026-02-09-validate-helm-charts-ci/view) — schema strict + ct lint + matrix.
- [Helm 3.18.5 Schema Validation Changes (Replicated Community)](https://community.replicated.com/t/helm-3-18-5-upgrade-impact-schema-validation-changes/1577) — nova lib `santhosh-tekuri/jsonschema/v6` quebra `additionalProperties: false` com `global` em umbrella; `--skip-dependencies-schema-validation` mitiga.
- [helm/helm#10392 — JSON Schema validation not friendly with chart deps](https://github.com/helm/helm/issues/10392) — issue histórica que ainda explica decisões de schema design em umbrella charts.
- [Sigstore Cosign Signing Other Types](https://docs.sigstore.dev/cosign/signing/other_types/) — mecanismo OCI artifact + referrers para Helm chart sign.
- [Sigstore Policy Controller](https://docs.sigstore.dev/policy-controller/overview/) — ClusterImagePolicy, namespace opt-in via `policy.sigstore.dev/include: "true"`, no-match-policy: warn|allow|deny.
- [How to Sign and Verify Helm Charts (OneUptime, Jan 2026)](https://oneuptime.com/blog/post/2026-01-17-helm-chart-signing-verification/view) — workflow padrão `helm package → helm push oci:// → cosign sign`.
- [Use OCI-based registries (Helm docs)](https://helm.sh/docs/topics/registries/) — Helm 3.8 GA, OCI deps em Chart.yaml, autenticação.
- [Helm 3 OCI dependency PR — fluxcd/source-controller#770](https://github.com/fluxcd/source-controller/pull/770) — pattern de umbrella com deps OCI.
- [chart-testing GitHub](https://github.com/helm/chart-testing) — `ct lint`, `ct install --upgrade`, K8s matrix.
- [helm-diff GitHub](https://github.com/databus23/helm-diff) — `helm diff upgrade --detailed-exitcode` para CI gate; `HELM_DIFF_THREE_WAY_MERGE=true` evita falsos positivos.
- [helm-drift GitHub](https://github.com/nikhilsbhat/helm-drift) — drift detection puro, complementar a helm-diff.
- [helm-chartsnap GitHub](https://github.com/jlandowner/helm-chartsnap) — snapshot whole-chart, `__snapshot__/v2/*` cached.
- [Crossplane Configuration Packages (OCI)](https://docs.crossplane.io/latest/packages/configurations/) — OCI-only distribution; digest pinning `@sha256:...` para deterministic install.
- [What Package Registries Could Borrow from OCI (Nesbitt, Feb 2026)](https://nesbitt.io/2026/02/18/what-package-registries-could-borrow-from-oci.html) — OCI 1.1 `artifactType` + `subject` + referrers API; Helm/Flux/Crossplane/Sigstore convergindo via ORAS.
