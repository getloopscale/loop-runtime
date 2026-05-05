# Charts Architecture

## Escopo

`charts/` contém os charts Helm publicados do LoopZero.

Em uma PaaS como a nossa, esses charts são a camada de empacotamento operacional do day-2. Eles materializam a plataforma em forma distribuível, versionável, assinada (cosign) e instalável por ArgoCD, CI e operadores da plataforma.

## Mapa Por Chart

| Chart | Responsabilidade principal | Domínio | Status |
|---|---|---|---|
| `loopzero-common` | library chart com helpers, labels, security contexts e padrões | shared chart primitives | ATIVO (16 helpers; 6 a adicionar Sprint 9) |
| `loopzero-api` | empacotamento da API | control plane | ATIVO |
| `loopzero-cloud` | empacotamento do cloud service (auth + billing) | cloud/product | ATIVO |
| `loopzero-dashboard` | empacotamento do dashboard | product UI | ATIVO |
| `loopzero-tenant-operator` | operator de tenant/environment | operators (split ADR-0023) | ATIVO |
| `loopzero-app-operator` | operator de app/exposure | operators (split ADR-0023) | ATIVO |
| `loopzero-preview-operator` | operator de preview environments | operators (split ADR-0023) | ATIVO |
| `loopzero-platform-operator` | operator de bootstrap (PlatformBootstrap, ClusterCredential, GitRepositoryBinding) | operators (split ADR-0023) | ATIVO |
| `loopzero-crds` | CRDs publicadas e lifecycle independente | API declarativa | ATIVO (10 CRDs) |
| `loopzero-platform` | umbrella chart que compõe a plataforma | platform composition | ATIVO (12 deps incluindo 2 ghost) |
| `loopzero-build-cwts` | cluster workflow templates e recursos de build | build plane | ATIVO |
| `loopzero-monitoring` | dashboards Grafana e PrometheusRules | observability | ATIVO (10 rules) |
| `loopzero-admission-policies` | ValidatingAdmissionPolicies e guardrails declarativos | policy/security | ATIVO mas com **drift regressivo** vs `infra/helm/admission-policies/` (faltam 5 templates incluindo cosign warn-mode) |

## Tipos De Chart Na Pasta

### `library chart`

O `loopzero-common` (Chart.yaml `type: library`) centraliza:

- naming de release (`name`, `fullname`);
- labels e selector labels (CNCF compliant — `app.kubernetes.io/{name,instance,version,part-of,managed-by}`);
- `operatorLabels` específico para fan-out (`app.kubernetes.io/component: loopzero-operator`) cobrindo os 4 binários split;
- image reference building (`loopzero-common.image` com `global.registry` + `image.tag` + fallback `Chart.AppVersion`);
- security contexts hardcoded PSA-restricted (não-configurável via values, intencional);
- service account defaults com `automountServiceAccountToken: false` por padrão;
- affinity preferida (podAntiAffinity soft, weight 100, topologyKey hostname);
- topology spread opcional (zone + hostname, `whenUnsatisfiable: ScheduleAnyway`);
- ServiceMonitor template;
- PDB automático quando `replicaCount > 1`;
- NetworkPolicy base com egress DNS sempre aberto + extraEgress por chart;
- startup probe configurable (failureThreshold 30 × periodSeconds 2 = 60s grace).

Esse chart existe para reduzir divergência de padrões operacionais entre componentes. **Lacunas atuais (Sprint 9 #1):** sem `tplvalues.render`, `kubeVersion` guard, `apiVersion` resolvers, `checksumAnnotations` (duplicado em 5 deployments), `imagePullSecrets` global merger, `secrets.name` (existingSecret pattern), `tmpVolumeMount` (readOnlyRootFilesystem compat).

### `application charts`

Charts que empacotam componentes executáveis da plataforma:

- `loopzero-api`
- `loopzero-cloud`
- `loopzero-dashboard`
- `loopzero-{tenant,app,preview,platform}-operator` (4 binários split — ADR-0023)

Cada um traduz um componente em workload Kubernetes instalável, seguro e observável. Exemplo `charts/loopzero-tenant-operator/templates/deployment.yaml`:

- usa `loopzero-common.fullname`, `loopzero-common.labels`, `loopzero-common.selectorLabels`;
- aplica `checksum/config` + `checksum/secret` annotations para auto-restart em mudança de ConfigMap/Secret (padrão Bitnami);
- monta `tmp` emptyDir 64Mi para `readOnlyRootFilesystem: true` compat;
- adiciona label fixa `loopzero.io/component: control-plane` + `app.kubernetes.io/component: loopzero-operator`;
- envFrom para ConfigMap + Secret (nome via `existingSecret` toggle);
- env condicional para kubeconfigs montados via ConfigMap (`existingConfigMap` pattern para build/runtime separation);
- ports `health:8081` + `metrics:8443` consistentes com helper `loopzero-common.servicemonitor`.

### `CRD chart`

`loopzero-crds` separa o lifecycle das CRDs do lifecycle dos controladores e workloads.

Isso é arquiteturalmente importante porque:

- CRD upgrade nem sempre deve seguir o mesmo ritmo do controller;
- instalação e remoção de CRDs têm riscos próprios (data persistence em CRs existentes);
- GitOps e bootstrap precisam poder orquestrar esse ciclo explicitamente;
- CI valida sync entre `operators/config/crd/bases/loopzero.io_*.yaml` e `charts/loopzero-crds/templates/*.yaml` via job `crd-sync` (helm-charts.yml linhas 215-247) — hoje compara count + name match; quick-win #4 do roadmap adiciona diff semântico de spec.

### `umbrella chart`

`loopzero-platform` compõe dependências da plataforma e funciona como agregador de instalação.

- 12 deps em `Chart.yaml` (loopzero-common + 11 sub-charts; 2 são ghost: loopzero-controller, loopzero-tenant-controller — quick-win #1).
- `condition: <name>.enabled` para cada sub-chart, controlado em `values.yaml`.
- `Chart.lock` commitado com versions pinadas (sha digest no header da lock).
- `profiles/` para overrides por ambiente.
- CI rewrite: durante `package-push`, `Chart.yaml` é mutado de `file://` para `oci://...` via `yq` (helm-charts.yml linhas 252-267) — solving standalone `helm pull` problem.

### `policy and monitoring charts`

Charts como `loopzero-admission-policies` e `loopzero-monitoring` fazem parte da plataforma, mas não são "apps" tradicionais.

`loopzero-admission-policies` empacota:
- VAPs (`vap-disallow-latest-tag`, `vap-protect-platform-networkpolicies`, `vap-require-image-signature`, `vap-require-resource-limits`, `vap-restrict-registries`, `vap-argocd-managed-by`).
- **Drift regressivo conhecido** vs `infra/helm/admission-policies/`: faltam `cluster-image-policy.yaml` (cosign warn-mode), `policy-controller-alerts.yaml`, `tenant-namespace-alerts.yaml`, `vap-no-runtime-image-mutations.yaml`, `vap-tenant-namespace.yaml`. Quick-win #2 do roadmap fecha.

`loopzero-monitoring` empacota:
- 10 PrometheusRules (capacity, critical, database, domains, otel-pipeline, probes, registry, tracing, warning).
- Dashboards Grafana.
- **Naming ambíguo**: este chart é APENAS rules + dashboards; o stack Prometheus real vive em outro chart bootstrap. Considerar renomear `loopzero-monitoring-rules` (anti-pattern #9 do roadmap).

## Domínios Funcionais

### `shared-operational-primitives`

Responsável por:

- labels consistentes (CNCF + LoopZero-specific);
- security contexts PSA-restricted hardcoded;
- naming previsível;
- helpers para deployment templates;
- ServiceMonitor + PDB + NetworkPolicy padronizados.

Arquivo âncora:

- `charts/loopzero-common/templates/_helpers.tpl` (16 helpers atuais, 21 alvo Sprint 9).

### `component-packaging`

Responsável por:

- transformar API, cloud, dashboard e operators em deployments instaláveis;
- expor services, serviceaccounts, RBAC, probes, resources, monitoramento, network policy.

Arquivos âncora:

- `charts/loopzero-api/templates/*` (12 templates: deployment, service, rbac, networkpolicy, configmap, secret, pdb, servicemonitor, NOTES, tests/*).
- `charts/loopzero-cloud/templates/*`.
- `charts/loopzero-dashboard/templates/*`.
- `charts/loopzero-{tenant,app,preview,platform}-operator/templates/*` (cada um com 11 templates: deployment, service, sa, clusterrole, clusterrolebinding, configmap, secret, networkpolicy, pdb, servicemonitor, NOTES).

### `platform-composition`

Responsável por:

- compor subcharts em uma visão de plataforma;
- ligar enablement, dependências e profiles;
- rewrite file:// → OCI no publish.

Arquivos âncora:

- `charts/loopzero-platform/Chart.yaml` (12 deps).
- `charts/loopzero-platform/Chart.lock` (digest sha256, OCI refs em release).
- `charts/loopzero-platform/profiles/*`.

### `api-declarative-surface`

Responsável por:

- publicar CRDs como artefatos instaláveis;
- separar lifecycle de tipos declarativos e controladores.

Arquivos âncora:

- `charts/loopzero-crds/Chart.yaml`.
- `charts/loopzero-crds/templates/{appsecrets,clustercredentials,environments,exposedroutes,gitrepositorybindings,platformbootstraps,previewenvironments,reconciliationreports,tenants,loopzeroapplications}.yaml`.

### `security-guardrails`

Responsável por:

- aplicar políticas declarativas que protegem a plataforma por default.

Arquivos âncora:

- `charts/loopzero-admission-policies/templates/*` (6 VAPs hoje; alvo 11 após quick-win #2).

### `observability-packaging`

Responsável por:

- dashboards e alertas como parte da release da plataforma;
- integração observável consistente entre ambientes.

Arquivos âncora:

- `charts/loopzero-monitoring/templates/{dashboards,prometheusrule-*}.yaml`.

## Fronteiras Arquiteturais

### O que pertence aos charts

- empacotamento operacional;
- defaults de deploy e runtime;
- RBAC, ServiceMonitor, policy manifests;
- composição de plataforma via dependências Helm;
- assinatura cosign (keyless via Fulcio + Rekor + key-based fallback) — embutida no CI `helm-charts.yml`.

### O que não pertence aos charts

- regra de negócio da API;
- lógica de lifecycle do operator (vive em `operators/internal/controllers/`);
- semântica de produto do CLI;
- automação de bootstrap L2 fora do escopo do artefato publicado.

## Relação Com `infra/helm`

No LoopZero coexistem duas famílias de chart:

- `charts/`: publicadas em OCI (DOCR `registry.digitalocean.com/loopzero-system/helm`), versionáveis, assinadas, voltadas a day-2 e ArgoCD.
- `infra/helm/`: bootstrap charts leves, focados em L2 (Helmfile sync), tipicamente wrappers thin sobre charts upstream + values.

**Overlap real (mesmo nome em ambos):**

- `loopzero-api` — published tem 12 templates (full RBAC + PDB + NetworkPolicy + ConfigMap + Secret + tests); bootstrap tem ~3 (ingressroute + minimal). Drift intencional (dev local vs prod) mas não documentado em ADR.
- `loopzero-cloud` — overlap presente, drift não inspecionado.

**Overlap conceitual (nomes diferentes mas mesmo domínio):**

- `loopzero-admission-policies/` ↔ `infra/helm/admission-policies/` — drift regressivo: published tem 6 templates, bootstrap tem 11.
- `loopzero-monitoring/` ↔ ??? (`infra/helm/monitoring/` não existe; stack Prometheus em outro lugar). Naming colisivo.

**Implicação:**

- Mudanças em helpers, defaults e superfícies compartilhadas podem exigir propagação deliberada para ambos os sistemas.
- Divergência entre eles precisa ser **detectada por automação**, nunca silenciosa.
- Hoje, apenas CRDs têm drift detector (`crd-sync` job). Admission-policies, loopzero-api, loopzero-cloud não. CH-G16 (NEW gate) propõe `infra/scripts/audit-helm-drift.sh` (quick-win #3 do roadmap).

## Distribuição (OCI + cosign)

CI `helm-charts.yml` (job `package-push`):

1. **Login DOCR** via `helm registry login`.
2. **Update deps** com `helm dependency update`.
3. **Package + push sub-charts** (com immutability guard `helm pull` antes do push).
4. **Rewrite umbrella refs** (`yq` muta `file://` → `oci://...` em `loopzero-platform/Chart.yaml`).
5. **Package + push umbrella**.
6. **Sign keyless** com cosign via OIDC GitHub → Fulcio cert → Rekor entry (MEET-2026-05-03-063 condition #2).
7. **Sign key-based** como fallback transitório (removido após 7 dias de keyless 100% per cutover plan).
8. **Verify keyless signature** com `cosign verify --certificate-oidc-issuer ...` (closes loop).
9. **Pull verification** — `helm pull` final para confirmar publish.

Esta é uma das partes mais maduras da camada `charts/` e está em paridade FAANG. CH-G19 (NEW) estende com SBOM attestation (paridade com container images Sprint 6).

## Regra De Ouro

Um chart do LoopZero deve empacotar um componente ou contrato operacional da plataforma. Ele não deve compensar defeitos de design do componente nem esconder complexidade perigosa nos templates.

Corolários derivados desta régua:

1. Se o chart precisa de `if/else` profundo para funcionar, o componente está mal desenhado — corrigir o componente, não o template.
2. Se valores em `values.yaml` viram receitas que requerem leitura do template para entender, o chart está vazando complexidade — extrair defaults fortes e variação real.
3. Se uma mudança em `infra/helm/<X>/` deveria refletir em `charts/loopzero-<X>/` mas não reflete, **isso é bug**, não estado normal — automação de drift é mandatório (CH-G16).
4. Se o umbrella aceita dep que não existe (`loopzero-controller`, `loopzero-tenant-controller`), o `Chart.lock` está mentindo — automação de ghost detection é mandatório (CH-G15).
