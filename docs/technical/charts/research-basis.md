# Charts Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA da pasta `charts/`.

Ele combina:

- documentação oficial do Helm e de recursos Kubernetes relevantes;
- referência aprofundada da indústria (Bitnami common, Sigstore, Crossplane, Elastic, chart-testing);
- evidências do código atual dos charts do LoopZero;
- leitura da camada `charts/` como contrato de packaging de uma PaaS.

## Fontes Externas Principais

### 1. Helm Best Practices (oficial)

**Fontes:**
- https://helm.sh/docs/chart_best_practices/
- https://helm.sh/docs/topics/charts/

**Pontos relevantes:**

- Charts devem ter estrutura clara (Chart.yaml + templates/ + values.yaml + opcional schema).
- Templates, values, labels, dependencies e RBAC precisam seguir convenções previsíveis.
- Charts são contratos operacionais, não só atalhos de template.
- `apiVersion: v2` para Helm 3-only; `type: library` para shared helpers; `type: application` para deployable.

**Gates fundamentados:** CH-G01, CH-G03, CH-G05, CH-G12, CH-G13.

### 2. Helm Values Best Practices

**Fonte:** https://docs.helm.sh/docs/chart_best_practices/values/

**Pontos relevantes:**

- `values.yaml` é API do chart.
- Nomes e estrutura de values influenciam usabilidade e estabilidade.
- Flat vs nested deve ser escolha deliberada.
- Schema (`values.schema.json`) Draft-07 é a forma oficial de validar.

**Gates fundamentados:** CH-G03, CH-G14.

### 3. Helm Library Charts

**Fonte:** https://v3-1-0.helm.sh/docs/topics/library_charts/

**Pontos relevantes:**

- Library charts formalizam compartilhamento de helpers e padrões.
- Devem fornecer primitives reutilizáveis, não virar aplicação (`type: library` impede deploy).
- Chart consumidor declara dep com `version: ">=0.1.0 <1.0.0"` ou similar.

**Gates fundamentados:** CH-G07, CH-G17 (NEW).

### 4. Bitnami Common Library Chart (referência prática)

**Fontes:**
- https://github.com/bitnami/charts/tree/main/bitnami/common
- https://deepwiki.com/bitnami/charts/2-chart-architecture-and-common-patterns
- https://techdocs.broadcom.com/us/en/vmware-tanzu/bitnami-secure-images/bitnami-secure-images/services/bsi-app-doc/apps-charts-common-index.html

**Pontos relevantes:**

- Bitnami common 2.x tem 40+ helpers organizados por categoria: `names.*`, `labels.*`, `secrets.*`, `images.*`, `tplvalues.render`, `capabilities.*`, `compatibility.*`, `errors.*`, `resources.*`, `affinities.*`, `tolerations.*`.
- `common.tplvalues.render` permite renderizar values com templating Go template (útil em annotations dinâmicas, labels custom).
- `common.kubeVersion` retorna a versão K8s do alvo (default ou `.Values.kubeVersion`) — habilita branching de apiVersion.
- `common.capabilities.deployment.apiVersion` resolve `apps/v1` ou versão antiga conforme cluster.
- `common.secrets.name` resolve `existingSecret` toggle padronizado.
- `common.images.image` constrói image ref com global registry override + digest.
- ConfigMap pattern com `checksum/config` annotation que dispara restart automático em mudança de config.
- Resource presets (`small`, `medium`, `large`, `xlarge`) — values.yaml escolhe via key, helper expande para requests/limits.

**Gates fundamentados:** CH-G07, CH-G17 (NEW).

### 5. Helm Chart Testing — chart-testing (ct)

**Fontes:**
- https://github.com/helm/chart-testing
- https://github.com/helm/chart-testing/blob/main/doc/ct_install.md
- https://alexandre-vazquez.com/helm-chart-testing-best-practices/
- https://oneuptime.com/blog/post/2026-01-17-helm-chart-testing-ct-helm-test/view

**Pontos relevantes:**

- `ct lint` é mais robusto que `helm lint` puro: detecta charts alterados via git, valida changes.
- `ct install` instala em Kind cluster real, valida render + actual install.
- `ct install --upgrade` testa upgrade de versão N-1 → N (catches breaking changes).
- `ct.yaml` define `chart-dirs`, `chart-repos`, `kubernetes-versions: [v1.30, v1.31, v1.32]`, `validate-chart-schema: true`, `validate-maintainers: true`, `check-version-increment: true`.
- Pipeline order recomendado: helm lint → kubeconform → helm-unittest → ct lint → ct install (--upgrade) → Conftest → Trivy.
- Bitnami testa em GKE/TKG/AKS simultaneamente; Elastic em matriz K8s definida em `helpers/matrix.yml`.

**Gates fundamentados:** CH-G05, CH-G12, CH-G18 (NEW).

### 6. helm-unittest (BDD framework)

**Fontes:**
- https://github.com/helm-unittest/helm-unittest
- https://oneuptime.com/blog/post/2026-01-17-helm-chart-testing-unittest-conftest/view

**Pontos relevantes:**

- BDD-styled, plugin Helm: `helm plugin install https://github.com/helm-unittest/helm-unittest`.
- Suites em `tests/*_test.yaml`, padrão AAA (Arrange-Act-Assert).
- Asserts: `equal`, `notEqual`, `contains`, `notContains`, `matchRegex`, `isKind`, `hasDocuments`, `matchSnapshot`.
- `matchSnapshot` cria baseline em `__snapshot__/v2/*_test.yaml.snap`, falha em diff (revisar antes de `-u, --update-snapshot`).
- Adicionar `__snapshot__/v2/*` ao `.helmignore` (essencial — caso contrário snapshots vão para o package).
- `helm unittest --output-type junit > junit.xml` para CI dashboards.
- Limitação per-file: tests rodam em 1 template por vez; conditional inclusion entre templates pode escapar — complementar com `helm-chartsnap` para whole-chart regression.

**Gates fundamentados:** CH-G12, CH-G17 (NEW).

### 7. Helm 3.18.5 Schema Validation Hardening

**Fontes:**
- https://community.replicated.com/t/helm-3-18-5-upgrade-impact-schema-validation-changes/1577
- https://github.com/helm/helm/issues/10392 (long-standing — `global` em schema strict)
- https://github.com/helm/helm/issues/12100 (nested subcharts + aliases)
- https://github.com/helm/helm/issues/31260 (jsonschema/v6 lib regression)
- https://www.arthurkoziel.com/validate-helm-chart-values-with-json-schemas/

**Pontos relevantes:**

- Helm 3.18.5 trocou lib para `github.com/santhosh-tekuri/jsonschema/v6` — endurece validação Draft-07.
- Charts com `additionalProperties: false` quebram quando umbrella passa `global` ou keys de subchart não declaradas.
- Mitigação: usar `additionalProperties: true` no top-level OU declarar `global` + nomes dos sub-charts no schema do parent.
- Flag `-m, --skip-dependencies-schema-validation` (newer Helm) seta `additionalProperties: true` nas deps automaticamente — útil em umbrella.
- LoopZero usa `additionalProperties: true` (escolha defensiva). Risco residual: typos passam silenciosamente.

**Gates fundamentados:** CH-G03, CH-G05.

### 8. Sigstore — Cosign + Policy Controller

**Fontes:**
- https://docs.sigstore.dev/cosign/signing/other_types/
- https://docs.sigstore.dev/policy-controller/overview/
- https://github.com/sigstore/cosign
- https://oneuptime.com/blog/post/2026-01-17-helm-chart-signing-verification/view

**Pontos relevantes:**

- Cosign assina **qualquer artefato OCI** (containers, Helm charts, SBOMs, blobs) — chart packaged é um OCI artifact.
- Workflow: `helm package → helm push oci:// → cosign sign --yes <REF>` (keyless via OIDC) ou `cosign sign --key cosign.key <REF>` (key-based).
- Keyless: GitHub OIDC token → Fulcio cert curto (10 min) → Rekor transparency log entry → signature como referrer no OCI.
- Verify: `cosign verify --certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-identity-regexp '^https://github\.com/<repo>/'`.
- Policy Controller (Sigstore): admission webhook K8s que valida signatures via `ClusterImagePolicy`. Namespace opt-in via `policy.sigstore.dev/include: "true"`.
- `no-match-policy: warn|allow|deny` controla comportamento default. **LoopZero usa warn-mode** (`loopzero-images-warn` ClusterImagePolicy via Helm chart `infra/helm/admission-policies/`) — Sprint 6 ship.
- Cosign v2.4+ + Policy Controller v0.13+ suportam novo bundle format (OCI 1.1 referrers API).
- SBOM attestation: `cosign attest --predicate sbom.json --type cyclonedx <REF>` — adiciona attestation ao mesmo OCI artifact, verificável via `cosign verify-attestation`.

**Gates fundamentados:** CH-G05, CH-G09, CH-G19 (NEW).

### 9. OCI Registry For Helm Distribution

**Fontes:**
- https://helm.sh/docs/topics/registries/
- https://helm.sh/blog/storing-charts-in-oci/
- https://github.com/fluxcd/source-controller/pull/770 (umbrella OCI deps)
- https://github.com/helm/helm/issues/12062 (failure modes)

**Pontos relevantes:**

- Helm 3.8+ suporta OCI nativamente (GA, deprecou ChartMuseum como recommended path).
- `dependencies[*].repository: "oci://..."` em Chart.yaml.
- Vantagens vs `file://`: digest pinning content-addressable, audit, RBAC + auth via container registry, scanning unified.
- Limitações: sem `helm search` em registries OCI puros (sem index.yaml); auth manual em CI; nem todos registries suportam Helm OCI artifactType corretamente.
- Hybrid pattern: `file://` para subcharts internos do mesmo monorepo, `oci://` para third-party. LoopZero usa `file://` em dev e CI rewrite para OCI no publish.

**Gates fundamentados:** CH-G05, CH-G10.

### 10. Crossplane Packages — OCI digest pinning

**Fontes:**
- https://docs.crossplane.io/latest/packages/configurations/
- https://nesbitt.io/2026/02/18/what-package-registries-could-borrow-from-oci.html

**Pontos relevantes:**

- Crossplane Configuration packages são OCI artifacts.
- `crossplane.io/v1.Configuration` aceita `package: <ref>@sha256:<digest>` para deterministic install.
- Helm, Flux, Crossplane, Sigstore convergem em ORAS + OCI 1.1 (`artifactType` + `subject` + referrers API).
- Padrão emergente: deps de Helm umbrella deveriam usar `oci://...@sha256:...` para fixação imutável.

**Gates fundamentados:** CH-G05, CH-G19 (NEW).

### 11. Kubernetes Validating Admission Policy

**Fontes:**
- https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/
- https://kubernetes.io/docs/concepts/policy

**Pontos relevantes:**

- Policies declarativas em CEL são artefatos nativos K8s 1.30+ (GA na 1.30).
- `failurePolicy` (Fail / Ignore) é o controle de comportamento em runtime errors do CEL.
- `parameterized` policies via `paramKind` permitem reuso de policy com diferentes params.
- VAP é cidadão first-class de chart de policy — não precisa de Kyverno/Gatekeeper para baseline.

**Gates fundamentados:** CH-G09.

### 12. Prometheus Operator / ServiceMonitor

**Fonte:** https://prometheus-operator.dev/docs/developer/getting-started/

**Pontos relevantes:**

- `ServiceMonitor` é parte do contrato declarativo de observabilidade para workloads monitorados.
- Packaging observável precisa descrever targets e rules explicitamente.
- `PrometheusRule` distribuído via chart é o padrão para alert delivery acoplado a release.

**Gates fundamentados:** CH-G08.

### 13. helm-diff + helm-drift (drift detection)

**Fontes:**
- https://github.com/databus23/helm-diff
- https://github.com/nikhilsbhat/helm-drift
- https://oneuptime.com/blog/post/2026-02-09-helm-diff-plugin-preview-changes/view

**Pontos relevantes:**

- `helm diff upgrade --detailed-exitcode` — exit ≠ 0 quando há drift; CI gate-friendly.
- `HELM_DIFF_THREE_WAY_MERGE=true` + `HELM_DIFF_NORMALIZE_MANIFESTS=true` evitam falsos positivos de ordering/defaults.
- helm-drift compara state real do cluster vs chart instalado (admission controllers podem mutar manifests).
- CI cron pattern: a cada 6h iterar `helm list -q` e diff cada release; alertar via webhook em diff não-vazio.
- LoopZero já tem helm-diff como prereq (CLAUDE.md) mas não usa em CI — Sprint 9 #2 do roadmap incorpora.

**Gates fundamentados:** CH-G05, CH-G16 (NEW).

### 14. helm-chartsnap (snapshot whole-chart)

**Fonte:** https://github.com/jlandowner/helm-chartsnap

**Pontos relevantes:**

- Complementar a helm-unittest: snapshot do chart inteiro com todos os values aplicados, não 1 template por vez.
- Cache em `__snapshot__/v2/*` (igual helm-unittest).
- Detecta drift agregado: mudança em `loopzero-common` que afeta 9 charts vira 1 diff revisável.
- Pattern de uso: `helm chartsnap charts/loopzero-platform -f values-prod.yaml` (sem `--update`) em CI; `--update` em PR após review.

**Gates fundamentados:** CH-G12.

## Evidências Internas Do LoopZero

### Library chart compartilhado

**Arquivo:**
- `charts/loopzero-common/templates/_helpers.tpl` (357 linhas, 16 helpers).

**O que sustentam:**
- Labels CNCF-compliant (`app.kubernetes.io/{name,instance,version,part-of=loopzero-platform,managed-by}`).
- `operatorLabels` específico para os 4 binários split (component=loopzero-operator).
- Image helper com `global.registry` + tag/digest fallback.
- Security contexts hardcoded PSA-restricted (não-configurável intencionalmente).
- ServiceAccount + automountSAToken (default false).
- Affinity + topology spread.
- ServiceMonitor + PDB + NetworkPolicy templates.
- Startup probe configurable.

**Lacunas identificadas (Sprint 9 #1):**
- `tplvalues.render` (Bitnami pattern para values templated).
- `kubeVersion` guard (apiVersion compat).
- `checksumAnnotations` (duplicado em 5 deployments).
- `imagePullSecrets` (acesso defensivo manual em cada deployment).
- `secrets.name` (`existingSecret` toggle reimplementado).
- `tmpVolumeMount` (readOnlyRootFilesystem compat duplicado).

### Component charts operacionais

**Arquivos:**
- `charts/loopzero-api/templates/*` (12 templates incluindo tests/).
- `charts/loopzero-cloud/templates/*`.
- `charts/loopzero-{tenant,app,preview,platform}-operator/templates/*` (11 templates por chart).
- `charts/loopzero-dashboard/templates/*`.

**O que sustentam:**
- Packaging consistente de deployment, RBAC, service, monitoring, network policy, PDB.
- Uso de library helpers pervasivo.
- Deployment template ricos: checksum auto-restart, tmp emptyDir para ROFS, kubeconfig dual mount, envFrom CM+Secret pattern.

### CRD separation

**Arquivos:**
- `charts/loopzero-crds/Chart.yaml`.
- `charts/loopzero-crds/templates/{appsecrets,clustercredentials,environments,exposedroutes,gitrepositorybindings,platformbootstraps,previewenvironments,reconciliationreports,tenants,loopzeroapplications}.yaml` (10 CRDs).

**O que sustentam:**
- Lifecycle independente das CRDs.
- Separação explícita entre API declarativa e workloads.
- CI `crd-sync` job (linhas 215-247 de `helm-charts.yml`) compara count + name match com `operators/config/crd/bases/`.

### Umbrella composition

**Arquivos:**
- `charts/loopzero-platform/Chart.yaml` (12 deps).
- `charts/loopzero-platform/Chart.lock` (sha256 digest, versions pinadas).
- `charts/loopzero-platform/values.yaml` (toggles + global.registry).
- `charts/loopzero-platform/profiles/*`.

**O que sustentam:**
- Composição explícita de subcharts via `condition: <name>.enabled`.
- Conditions por dependência ativadas via values.
- Profiles por ambiente.

**Anomalia detectada:**
- Linhas 23-25 do Chart.lock referenciam `loopzero-controller` e values.yaml linhas 25-26 mantêm `loopzero-controller.enabled: true` + `loopzero-tenant-controller.enabled: true` — esses dirs em `charts/` não têm `Chart.yaml` (apenas `charts/loopzero-common-0.2.0.tgz` vendorado). Ghost charts. Quick-win #1.

### Policy packaging

**Arquivos:**
- `charts/loopzero-admission-policies/templates/{vap-argocd-managed-by,vap-disallow-latest-tag,vap-protect-platform-networkpolicies,vap-require-image-signature,vap-require-resource-limits,vap-restrict-registries}.yaml` (6 VAPs).

**O que sustentam:**
- Postura de segurança da plataforma publicada como chart.
- Uso de ValidatingAdmissionPolicy para guardrails declarativos.

**Drift detectado vs `infra/helm/admission-policies/`:**
- Bootstrap tem 11 templates (5 a mais): `cluster-image-policy.yaml`, `policy-controller-alerts.yaml`, `tenant-namespace-alerts.yaml`, `vap-no-runtime-image-mutations.yaml`, `vap-tenant-namespace.yaml`.
- Inversão regressiva: published deveria ser superset de bootstrap, está sendo subset. Quick-win #2.

### Monitoring packaging

**Arquivos:**
- `charts/loopzero-monitoring/templates/{dashboards,prometheusrule-capacity,prometheusrule-critical,prometheusrule-database,prometheusrule-domains,prometheusrule-otel-pipeline,prometheusrule-probes,prometheusrule-registry,prometheusrule-tracing,prometheusrule-warning}.yaml` (10 rules + dashboards).

**O que sustentam:**
- Observabilidade como artefato publicável.
- Alertas e dashboards empacotados junto da plataforma.

**Anomalia de naming:**
- `loopzero-monitoring` é apenas rules + dashboards; o stack Prometheus real está em outro chart bootstrap. Considerar renomear `loopzero-monitoring-rules`.

### Compatibilidade/legado

**Arquivos:**
- `charts/loopzero-controller/charts/loopzero-common-0.2.0.tgz` (vendorado, sem Chart.yaml próprio).
- `charts/loopzero-tenant-controller/charts/loopzero-common-0.2.0.tgz` (idem).

**O que sustentam:**
- ZERO. São ghost charts pós-split de operadores (ADR-0023).
- Persistem em `loopzero-platform/Chart.lock` mas não renderizam nada.

**Ação requerida:** delete (quick-win #1).

### CI workflow

**Arquivo:**
- `.github/workflows/helm-charts.yml` (~340 linhas, 4 jobs: lint, validate, crd-sync, package-push).

**O que sustenta:**
- `helm lint --strict` em todos charts.
- `helm-unittest` em 5 charts (api + 4 operators).
- Polaris audit (continue-on-error baseline-first per MEET-062).
- `kubeconform --strict --kubernetes-version 1.31.0` em todos charts não-library.
- CRD drift count + name match.
- Package + push OCI com immutability guard.
- Cosign keyless (Fulcio + Rekor) + key-based dual-signing (MEET-063 condition #2).
- Verify keyless signature step.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com o desenho recomendado oficialmente para charts Helm.
2. São necessários para transformar charts em contrato operacional confiável de uma PaaS.
3. Já encontram apoio forte na estrutura e nos padrões presentes em `charts/` hoje.
4. Os 5 NEW gates (CH-G15 a G19) endereçam lacunas concretas identificadas no código atual: ghost charts, drift sem automação, library coverage incompleta, falta de matriz K8s, SBOM attestation pendente.

A direção é convergir com o padrão Bitnami common (library) + Sigstore (signing) + chart-testing (validation) + Crossplane (digest pinning) — todas referências FAANG-bar com adoção comprovada na indústria.
