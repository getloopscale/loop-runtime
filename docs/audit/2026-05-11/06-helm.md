# Module 06 — Helm Charts (`infra/helm/` + `charts/` + `infra/helmfile/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.7 (1M context), code-validated
**Confidence:** 90% (Claude-only, single-model residual)
**Maturity:** **3.5 / 4.0**

## TL;DR

Sistema dual maduro (Helmfile L2 bootstrap + OCI-published charts via ArgoCD L3) com **9 gates blocking + 4 advisory** na CI, dual cosign signing (keyless+key-based), SBOM via syft, matriz K8s 1.30/1.31/1.32, e `audit-helm-drift.sh` detectando drift sistematicamente. `runAsUser: 1000` verificado consistente. Bloqueia 4.0 por 4 gates ainda em advisory mode (drift, conftest, chartsnap, helm-diff) e desalinhamento numérico vs CLAUDE.md.

---

## 1. Inventário Real

| Sistema | Path | Charts reais | Overlays/Values-only | Total dirs |
|---|---|---|---|---|
| Bootstrap (Helmfile L2) | `infra/helm/` | **16** (com Chart.yaml) | 18 (overlays para upstream) | 34 |
| Published (ArgoCD L3) | `charts/` | **13** (todos com Chart.yaml) | — | 13 |

**CLAUDE.md afirma "32 bootstrap charts"** — análise real revela 16 charts próprios + 18 value overlays para charts upstream (Jetstack cert-manager, Traefik Community, Argo Project, Prometheus Community, Bitnami, etc.). Clarification needed em CLAUDE.md.

**16 charts em `infra/helm/`** (com Chart.yaml):
admission-policies, argo-rollouts, argo-workflows, chaos-mesh, cloudflared, cluster-issuer, harbor, k6-operator, keda, keda-http-addon, network-policies, smoke-test, static-assets, loopzero-api, loopzero-cloud, velero.

**13 charts em `charts/`** (OCI-publishable):
loopzero-admission-policies, loopzero-api, loopzero-app-operator, loopzero-build-cwts, loopzero-cloud, loopzero-common (**library**), loopzero-crds, loopzero-dashboard, loopzero-monitoring, loopzero-platform (**umbrella**), loopzero-platform-operator, loopzero-preview-operator, loopzero-tenant-operator.

**4 Helmfile environments:** dev, prod, standalone, standalone-multi.

**4 Helmfile templates:** mgmt.helmfile.yaml.gotmpl, build.helmfile.yaml.gotmpl, runtime.helmfile.yaml.gotmpl + helpers em `infra/helmfile/scripts/`.

---

## 2. Arquitetura Observada

**Dual system com responsabilidades distintas (ADR D1):**

| Aspect | `infra/helm/` (Bootstrap) | `charts/` (Published) |
|---|---|---|
| Templates | 5-7 minimal | 10-15 full (RBAC, NetworkPolicy, ConfigMap, Secret) |
| Library | Self-contained | Depends on `loopzero-common` 0.3.0 |
| Distribution | `helmfile sync` em k3d/DOKS | OCI push para `registry.digitalocean.com/loopzero-system/helm/` |
| Validação values | Inline defaults | `values.schema.json` (helm lint --strict) |
| Tests | task-level via `task setup` | `templates/tests/test-connection.yaml` + chart-testing |
| Signing | — | cosign keyless (Fulcio) + key-based (fallback) |

**loopzero-common library** (`charts/loopzero-common/templates/_helpers.tpl`, 499 LOC, 20+ helpers):
- Naming: `loopzero-common.name`, `loopzero-common.fullname`
- Labels: `loopzero-common.labels` (CNCF standard) + `loopzero-common.selectorLabels` + `loopzero-common.operatorLabels`
- Security: `loopzero-common.podSecurityContext` (`runAsNonRoot=true, runAsUser=1000, fsGroup=1000, seccompProfile=RuntimeDefault`) + `loopzero-common.containerSecurityContext` (`allowPrivilegeEscalation=false, readOnlyRootFilesystem=true, capabilities.drop=[ALL]`)
- Probes: `loopzero-common.startupProbe` (30×2s grace post-B149)
- Observabilidade: `loopzero-common.servicemonitor` (conditional Prometheus Operator)
- Resilience: `loopzero-common.pdb` (minAvailable=1 if replicas>1)
- Wiring: `loopzero-common.checksumAnnotations`, `loopzero-common.tplvalues.render`, `loopzero-common.kubeVersion`

**Helmfile wave ordering** (~32 releases):
- **Wave -3:** cert-manager, policy-controller, external-secrets, prometheus-operator, loki, tempo, otel (no deps)
- **Wave -2:** PostgreSQL, MinIO, Redis, Ory Kratos/Hydra, Gitea
- **Wave -1:** Traefik, Cloudflare, Argo Rollouts
- **Wave 0:** loopzero-api, loopzero-cloud, 4 split operators, ArgoCD
- **Wave 1+:** chaos-mesh, k6-operator, Velero

**Secrets pré-geradas** (`infra/helmfile/scripts/generate-secrets.sh`) ANTES de helmfile sync — evita deadlock de in-cluster secret creation.

---

## 3. Contratos Públicos

**3 overlapping charts (ADR-0043 approved drifts):**

| Pair | Bootstrap | Published | Drift OK? | Alignment crítico |
|---|---|---|---|---|
| loopzero-api | 5 templates | 12 templates | YES (RBAC só em published) | runAsUser: 1000 ✅ idêntico |
| loopzero-cloud | 5 templates | 12 templates | YES (mesma razão) | runAsUser: 1000 ✅ idêntico |
| admission-policies / loopzero-admission-policies | 7+ VAPs | 7+ VAPs | NO drift (paridade pós-T1.2) | registries + VAPs unified ✅ |

**Admission policies — registry allowlist:**
```yaml
registries:
  docrPrefix: "registry.digitalocean.com/loopzero-system/"
  harborPrefix: "harbor.dev.loopzero.example/loopzero-platform/"
  additionalPrefixes: []
```
`standalone-multi` overlay adiciona `k3d-loopzero-registry:5000/<tenant>/`.

**7+ VAPs configuráveis:** restrict-registries, disallow-latest-tag, no-runtime-image-mutations, ClusterImagePolicy loopzero-images-warn, ClusterImagePolicy loopzero-images-keyless (disabled), network policies deny-all, PDB minAvailable.

**monitoring/loopzero-monitoring são complementares** (verificado):
- `infra/helm/monitoring/` — value overlay para prometheus-operator upstream (deploys infra)
- `charts/loopzero-monitoring/` — PrometheusRules + Grafana dashboards (config layer)

---

## 4. Cobertura de Testes

**`.github/workflows/helm-charts.yml`** — 9 BLOCKING + 4 ADVISORY gates:

| Gate | Status | Tool |
|---|---|---|
| helm lint --strict | ✅ BLOCKING | helm v3.14.4 |
| helm-unittest | ✅ BLOCKING | helm-unittest plugin |
| Polaris audit | ✅ BLOCKING (graduado T7.1) | Polaris 9.6.0 severity=danger |
| Audit-helm-drift | ⚠️ ADVISORY (30-day window) | audit-helm-drift.sh |
| Conftest policies | ⚠️ ADVISORY | Conftest v0.55.0 + Rego |
| Manifest validation | ✅ BLOCKING (matrix 1.30/1.31/1.32) | kubeconform v0.6.6 |
| chart-testing lint | ✅ BLOCKING | ct v2.6.1 |
| chart-testing install | ✅ BLOCKING (Kind cluster) | ct v2.6.1 + Kind |
| chartsnap snapshot | ⚠️ ADVISORY | helm-chartsnap |
| helm-diff PR gate | ⚠️ ADVISORY | helm-diff |
| CRD sync check | ✅ BLOCKING | yq count + semantic diff |
| Package & push | ✅ BLOCKING | helm package + OCI push |
| cosign sign (dual) | ✅ BLOCKING | cosign v3.10.0 (keyless + key-based) |
| SBOM generate + attest | ✅ BLOCKING | syft v1.10.0 + CycloneDX |

**helm-unittest auto-discovery** ativo em `charts/*/tests/` (CH-T3.5+T1.5).

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| 4 gates ainda advisory (drift, conftest, chartsnap, helm-diff) | MÉDIA | `.github/workflows/helm-charts.yml` |
| CLAUDE.md numerical drift (32 charts → 16 + 18 overlays) | BAIXA | Documentação |
| helm-unittest descobre dirs mas pode haver gap de tests substantivos | MÉDIA | `charts/*/tests/` (test-connection.yaml apenas) |
| `Pulumi.staging.yaml` stale + sem loopzero-platform/control-plane staging | BAIXA | Cross-module finding (vide IaC) |
| TODO/FIXME nos Chart.yaml/values.yaml | **0** ✅ | grep retorna vazio |
| `latest` image tags | **0** ✅ | VAP `disallow-latest-tag` enforce |
| `runAsUser` inconsistency | **0** ✅ | Verified 1000 em ambos sistemas |
| Resource limits missing | **0** ✅ | Todos charts com requests+limits |
| Readiness/liveness probes missing | **0** ✅ | Todas application charts |
| Drift unapproved | **0** ✅ | 3 approved (ADR-0043), 0 unapproved |

---

## 6. Avaliação de Maturidade

**Score: 3.5 / 4.0** (confidence 90%)

### Evidências

1. **T1 (commit+test)** — `charts/loopzero-common/templates/_helpers.tpl` (20+ helpers + `runAsUser: 1000`); `.github/workflows/helm-charts.yml` (9 blocking gates + matrix K8s); `infra/scripts/audit-helm-drift.sh` + ADR-0043 (drift registry).
2. **T2 (LIVE log)** — Cleanroom 12/12 GREEN com helmfile sync completo (2026-05-05); loopzero-common 0.3.0 deploy GREEN; cell-2 ativação com 3 cells LIVE via helmfile (2026-05-02); journey 76/76 GREEN com charts publicados (2026-05-06).
3. **T3 (CI run)** — `helm-charts.yml` workflow rodando em cada PR + push; CRD sync check, OCI publish, cosign verify all gated.

### Raciocínio

**Sustenta 3.0-3.5+:**
- Sistema dual bem-arquitetado (bootstrap minimal vs published full)
- loopzero-common library robusta (20+ helpers, PSA restricted enforced)
- 9 gates CI BLOCKING + matrix K8s 3 versões
- Drift detection sistemático (audit-helm-drift.sh + ADR-0043)
- Dual signing (cosign keyless + key-based) + SBOM
- runAsUser/securityContext/resources/probes consistentes
- 0 `:latest` tags (VAP enforce)
- 0 TODO/FIXME stale
- monitoring/loopzero-monitoring layering correto

**Bloqueia 4.0:**
- **4 gates advisory** ainda não-blocking (drift, conftest, chartsnap, helm-diff)
- helm-unittest discovery ativo, mas pode haver gap de unit tests substantivos (não validamos conteúdo profundo)
- chartsnap sem baseline committed (depende de 3 GREEN consecutivos)
- Conftest baseline pendente review
- Sem E2E cross-system test (bootstrap → published transition validation)

### Caminho para 3.8-4.0

1. Graduar `audit-helm-drift.sh` → BLOCKING pós-2026-06-11 (30-day window fecha)
2. Aprovar Conftest baseline policies + graduar para BLOCKING
3. Commitar chartsnap `__snapshot__/` initial após 3 GREEN consecutivos
4. Adicionar helm-unittest YAML tests substantivos:
   - Deployment replicas from values
   - SecurityContext inherits from loopzero-common
   - ServiceMonitor label matching
5. Criar E2E test cross-system: deploy bootstrap → assert pod state → deploy published (ArgoCD Day-2) → assert no disruption
6. Atualizar CLAUDE.md: "16 bootstrap charts + 18 value overlays" (clarification)

---

**Próximas auditorias relacionadas:** Build Pipeline (M07), Observability (M08), CI/CD (M12).
