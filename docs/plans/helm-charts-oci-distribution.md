# Plano: LoopZero Platform — Helm Charts + OCI Registry Distribution

> **Status:** Aprovado
> **Data:** 2026-04-23
> **Revisado:** 2026-04-23 (CNCF/FAANG best practices review — 14 correções aplicadas)
> **Classificacao:** T3 (Arquitetura + Pipeline)

## Contexto

O LoopZero PaaS hoje e deployado a partir do filesystem do monorepo — manifests Tilt para dev, Pulumi inline para producao. Nao existe um artefato distribuivel. Para instalar ou atualizar a plataforma, e preciso clonar o repositorio.

**Objetivo:** Tornar o LoopZero instalavel em qualquer cluster Kubernetes via `helm install` a partir do DOCR (DigitalOcean Container Registry), que ja hospeda as imagens Docker e suporta OCI Helm charts nativamente. O resultado final:

```bash
helm install loopzero-platform \
  oci://registry.digitalocean.com/loopzero-infra-registry/helm/loopzero-platform \
  --version 0.1.0 \
  --namespace loopzero-system --create-namespace \
  -f my-values.yaml
```

**Escopo:** Apenas componentes LoopZero. Charts de terceiros (PostgreSQL, Gitea, ArgoCD, etc.) continuam nos repos publicos — nao fazem parte deste plano.

---

## Principios de Design (CNCF Best Practices)

Estes principios guiam toda decisao de design dos charts:

1. **Chart auto-contido** — Cada chart e instalavel sozinho, com suas proprias NetworkPolicies, ServiceMonitors, PDBs, e tests. O umbrella orquestra mas nao e obrigatorio.
2. **CRDs como chart separado** — CRDs tem lifecycle proprio (install + upgrade, nunca delete). Chart dedicado `loopzero-crds` evita a limitacao do Helm que nao faz upgrade de CRDs em `crds/`. Versao atual: `v1alpha1` unica stored version. Conversion webhook adiado para GA — sera necessario quando houver `v1beta1`/`v1` migration.
3. **Security by default** — `seccompProfile: RuntimeDefault`, `automountServiceAccountToken: false`, PSA `restricted`, `drop ALL`, `priorityClassName` para componentes de platform. Defense in depth.
4. **Schema validation** — `values.schema.json` valida inputs antes do render. Typos em values sao erros de compilacao, nao bugs silenciosos.
5. **Test hooks** — `helm test` em cada chart para validacao pos-install. Test hooks devem validar funcionalidade real (deep health check), nao apenas conectividade de rede.
6. **Signing** — Charts OCI assinados com cosign (key-based, mesma chave do image signing em `COSIGN_KEY` GitHub Secret). Consistente com image signing existente.
7. **Versionamento lockstep** — `version` (chart) e `appVersion` (imagem) sao independentes e seguem semver. Dentro do monorepo, todos os charts compartilham a mesma `version` para simplificar release (padrao Istio). `appVersion` continua independente por componente.

---

## Namespace Strategy (CNCF Standard)

### Principio: separacao por trust boundary

**Padrao CNCF:** Cada projeto tem seu namespace `{project}-system`. Projetos de terceiros mantem seus namespaces upstream. NUNCA renomear namespace de terceiro com seu prefixo — quebra documentacao, troubleshooting, e expectativas de operadores.

**Exemplos de referencia:**

| Projeto CNCF | Namespace | Tipo |
|-------------|-----------|------|
| Istio | `istio-system` | Proprio |
| Crossplane | `crossplane-system` | Proprio |
| Linkerd | `linkerd` | Proprio |
| ArgoCD | `argocd` | Proprio (terceiro para nos) |
| cert-manager | `cert-manager` | Proprio (terceiro para nos) |

### Situacao atual (inconsistente — pre-producao)

O sistema ainda NAO esta em producao. Estas inconsistencias devem ser corrigidas ANTES do primeiro deploy.

| Componente | Dev local (Tilt) | Prod (Pulumi) | Correto CNCF |
|-----------|-----------------|---------------|-------------|
| LoopZero API, Cloud, Dashboard | `loopzero-system` | `platform-system` | `loopzero-system` |
| Tenant Controller | `loopzero-system` | `platform-system` | `loopzero-system` |
| PostgreSQL, MinIO | `loopzero-system` | `platform-system` | `loopzero-system` (data store privado) |
| ArgoCD | `loopzero-system` (via Taskfile) | `platform-system` | **`argocd`** (upstream) |
| Gitea | `loopzero-system` | `platform-system` | **`gitea`** (upstream) |
| Ory Kratos + Hydra | `loopzero-system` | `platform-system` | **`ory`** (agrupado por funcao) |
| Traefik | `traefik` (local) | `platform-system` (prod) | **`traefik`** (upstream) |
| cert-manager | `cert-manager` | `cert-manager` | **`cert-manager`** (ja correto) |
| Prometheus + Grafana | `monitoring` (via Taskfile) | `platform-system` | **`monitoring`** (upstream kube-prometheus-stack) |
| Loki + Promtail | `monitoring` | `monitoring` | **`monitoring`** (mesma stack) |
| Argo Workflows | `argo` | `argo` | **`argo`** (upstream, ja correto) |
| ESO | `external-secrets` | `external-secrets` | **`external-secrets`** (ja correto) |

### Decisao: mapa de namespaces por cluster

#### mgmt cluster

| Namespace | Componentes | Dono | Justificativa |
|-----------|------------|------|---------------|
| **`loopzero-system`** | loopzero-api, loopzero-cloud, loopzero-dashboard, loopzero-tenant-controller, loopzero-controller, PostgreSQL, MinIO | **LoopZero (Helm charts)** | Componentes LoopZero + data stores privados que so LoopZero usa. PG e MinIO aqui porque sao internal implementation detail, nao servicos compartilhados. |
| **`argocd`** | ArgoCD server, application-controller, repo-server | **Upstream chart** | ArgoCD tem seu proprio RBAC, dashboard, e documentacao que assume `argocd`. Namespace upstream. |
| **`gitea`** | Gitea server | **Upstream chart** | Gitea e um servico independente com admin UI e API propria. Namespace upstream facilita troubleshooting. |
| **`ory`** | Ory Kratos, Ory Hydra, Redis (sessoes) | **Upstream charts** | Stack de auth agrupada. Lifecycle independente da platform (pode upgradar Kratos sem tocar LoopZero). |
| **`traefik`** | Traefik ingress controller | **Upstream chart** | Ingress e cross-namespace por natureza. Namespace upstream. |
| **`cert-manager`** | cert-manager controller | **Upstream chart** | Ja usa namespace correto. |
| **`monitoring`** | Prometheus, Grafana, AlertManager, kube-state-metrics, Tempo, OTEL | **Upstream kube-prometheus-stack** | Stack de observability padrao. `loopzero-monitoring` chart instala PrometheusRules e dashboards NESTE namespace (nao cria namespace proprio). |

#### build cluster

| Namespace | Componentes | Dono | Justificativa |
|-----------|------------|------|---------------|
| **`argo`** | Argo Workflows controller, ServiceAccount `loopzero-workflow-submitter` | **Upstream chart** | Namespace upstream do Argo Workflows. |
| **`loopzero-{tenant}-build`** | Per-tenant Argo Workflows, BuildKit, resource quotas | **TenantController** | Isolacao de build por tenant. Trust boundary diferente. |
| **`monitoring`** | Loki, Promtail | **Upstream charts** | Log aggregation no build cluster. |

#### workload cluster

| Namespace | Componentes | Dono | Justificativa |
|-----------|------------|------|---------------|
| **`loopzero-{tenant}-{env}`** | User apps, Services, Ingress, NetworkPolicies | **TenantController** | Namespace dinamico por tenant+environment. |
| **`traefik`** | Traefik ingress controller | **Upstream chart** | Ingress no workload cluster. |
| **`cert-manager`** | cert-manager controller | **Upstream chart** | TLS no workload cluster. |
| **`external-secrets`** | ESO controller, ClusterSecretStore | **Upstream chart** | Secret sync cross-cluster. |

### Service discovery (FQDNs)

Com componentes em namespaces separados, service URLs DEVEM usar FQDN (nao short names). Os defaults no codigo devem refletir isso:

```yaml
# Antes (tudo no mesmo namespace — short names):
GITEA_URL: "http://gitea-http:3000"
ARGOCD_URL: "http://argocd-server:80"
MINIO_ENDPOINT: "minio:9000"

# Depois (namespaces separados — FQDN obrigatorio):
GITEA_URL: "http://gitea-http.gitea.svc.cluster.local:3000"
ARGOCD_URL: "http://argocd-server.argocd.svc.cluster.local:80"
MINIO_ENDPOINT: "minio.loopzero-system.svc.cluster.local:9000"
JWKS_ENDPOINT: "http://ory-hydra-public.ory.svc.cluster.local:4444/.well-known/jwks.json"
JWT_ISSUER: "http://ory-hydra-public.ory.svc.cluster.local:4444/"
```

> MinIO fica em `loopzero-system` (mesmo namespace que a API), entao short name funciona. Mas usar FQDN para todos e mais robusto e consistente.

### Convergencia necessaria antes dos Helm charts (Fase 0)

O sistema NAO esta em producao. Todas estas mudancas devem ser aplicadas ANTES do primeiro deploy.

| # | Arquivo | Mudanca | Tipo |
|---|---------|---------|------|
| 1 | `infra/pulumi/src/components/bootstrap-shared.ts` | `PLATFORM_NAMESPACE = "platform-system"` → `"loopzero-system"` | Constante |
| 2 | `operators/internal/constants/ports.go` | `NamespacePlatformSystem = "platform-system"` → `"loopzero-system"` | Constante |
| 3 | `infra/pulumi-control-plane/src/mgmt-control-plane-services.ts` | `PLATFORM_NAMESPACE` → importar de bootstrap-shared (DRY) | DRY |
| 4 | `api/internal/config/config.go:255` | `gitea-http.loopzero-system` → `gitea-http.gitea` | FQDN |
| 5 | `api/internal/config/config.go:259` | `minio.loopzero-system` → `minio.loopzero-system` (ja correto, mesmo ns) | OK |
| 6 | `api/internal/config/config.go:139` | `ory-hydra-public.loopzero-system` → `ory-hydra-public.ory` | FQDN |
| 7 | `api/internal/config/config.go:142` | `ory-hydra-public.loopzero-system` → `ory-hydra-public.ory` | FQDN |
| 8 | `operators/api/v1/platformbootstrap_types.go:100` | `gitea-http.platform-system` → `gitea-http.gitea` | CRD default |
| 9 | `operators/api/v1/platformbootstrap_types.go:149,156` | `loopzero-api.platform-system` → `loopzero-api.loopzero-system` | CRD default |
| 10 | `operators/internal/constants/resources.go:148` | `gitea-http.loopzero-system` → `gitea-http.gitea` | Constante |
| 11 | `operators/config/crd/bases/*.yaml` | Regenerar CRDs apos mudancas em types.go (`make generate manifests`) | CRD YAML |
| 12 | `operators/operatortest/setup.go:88` | `gitea.loopzero-system` → `gitea.gitea` | Test fixture |
| 13 | `operators/api/v1/*_test.go` | `platform-system` → namespaces corretos nos testes | Testes |
| 14 | `Taskfile.yml` (gitea install) | `--namespace loopzero-system` → `--namespace gitea` | Namespace |
| 15 | `Taskfile.yml` (ory install) | `--namespace loopzero-system` → `--namespace ory` | Namespace |
| 16 | `Taskfile.yml` (argocd install) | `--namespace loopzero-system` → `--namespace argocd` (se aplicavel) | Namespace |
| 17 | `Tiltfile` | Service probes atualizados para namespaces corretos | Dev loop |
| 18 | `infra/pulumi/` (todos componentes) | Separar deploys de terceiros em seus namespaces | Pulumi |
| 19 | `infra/helm/network-policies/` | Atualizar NetworkPolicies para cross-namespace | NetPol |

### Helm chart values (service URLs configuraveis)

Nos Helm charts, as URLs de servicos de terceiros sao configuraveis via values (NAO hardcoded):

```yaml
# charts/loopzero-api/values.yaml
config:
  giteaUrl: "http://gitea-http.gitea.svc.cluster.local:3000"         # Gitea namespace upstream
  argocdUrl: "http://argocd-server.argocd.svc.cluster.local:80"      # ArgoCD namespace upstream
  minioEndpoint: "minio.loopzero-system.svc.cluster.local:9000"          # MinIO no mesmo namespace
  jwksEndpoint: "http://ory-hydra-public.ory.svc.cluster.local:4444/.well-known/jwks.json"
  jwtIssuer: "http://ory-hydra-public.ory.svc.cluster.local:4444/"
```

Profiles (dev/prod) podem override estes valores se os namespaces forem diferentes.

### Criterios de aceite

- [ ] ZERO ocorrencias de `platform-system` no codebase (exceto git history)
- [ ] Todos os service URLs usam FQDN com namespace correto
- [ ] Componentes LoopZero em `loopzero-system`, terceiros em namespaces upstream
- [ ] `helm template` de todos os charts renderiza `namespace: loopzero-system`
- [ ] NetworkPolicies permitem trafego cross-namespace (loopzero-system ↔ gitea, argocd, ory, monitoring)
- [ ] Tilt dev loop funciona com namespaces separados
- [ ] Namespaces dinamicos mantém padrao `loopzero-{tenant}-{env}`

---

## Estrutura final

```
charts/
  ├── loopzero-common/              <- Library chart (helpers compartilhados)
  ├── loopzero-crds/                <- CRD chart (10 CRDs, lifecycle independente)
  ├── loopzero-api/                 <- API server (Go)
  │   └── templates/
  │       ├── deployment.yaml
  │       ├── service.yaml
  │       ├── networkpolicy.yaml       <- NetworkPolicy auto-contida
  │       ├── servicemonitor.yaml      <- Condicional em metrics.serviceMonitor.enabled
  │       ├── pdb.yaml                 <- Condicional em replicaCount > 1
  │       └── tests/
  │           └── test-connection.yaml <- helm test hook
  ├── loopzero-tenant-controller/    <- Operator kubebuilder (Go), depende de loopzero-crds
  ├── loopzero-controller/          <- Background loops (usa imagem loopzero-api)
  ├── loopzero-cloud/               <- Auth/billing service (Go)
  ├── loopzero-dashboard/           <- Frontend (Vite React)
  ├── loopzero-build-cwts/          <- ClusterWorkflowTemplates (migra de infra/helm/argo-workflows/)
  ├── loopzero-monitoring/          <- PrometheusRules + dashboards (migra de infra/helm/monitoring/)
  ├── loopzero-admission-policies/  <- ValidatingAdmissionPolicies (migra de infra/helm/admission-policies/)
  └── loopzero-platform/            <- Umbrella chart (instala tudo)
      └── profiles/
          ├── dev.yaml          <- k3d, insecure, NodePort
          └── prod.yaml         <- DOCR, ClusterIP, TLS, HA
```

**Mudancas vs versao anterior:**
- `loopzero-crds/` adicionado — chart separado para CRDs (resolve upgrade lifecycle)
- `loopzero-network-policies/` REMOVIDO — NetworkPolicies movidas para dentro de cada chart (principio auto-contido)
- `ServiceMonitor` em cada chart, nao apenas no controller
- `PodDisruptionBudget` em cada chart com replicaCount > 1
- `helm test` hooks em cada chart
- `values.schema.json` em cada chart

**Registry OCI:**
```
oci://registry.digitalocean.com/loopzero-infra-registry/helm/
  ├── loopzero-crds:0.1.0               <- NOVO: CRDs separados
  ├── loopzero-api:0.1.0
  ├── loopzero-tenant-controller:0.1.0
  ├── loopzero-controller:0.1.0
  ├── loopzero-cloud:0.1.0
  ├── loopzero-dashboard:0.1.0
  ├── loopzero-build-cwts:0.1.0
  ├── loopzero-monitoring:0.1.0
  ├── loopzero-admission-policies:0.1.0
  └── loopzero-platform:0.1.0
```

---

## Versioning Strategy

### Chart version vs App version

Cada chart tem duas versoes:

```yaml
# Chart.yaml
version: 0.1.0      # Chart version — LOCKSTEP: todos os charts compartilham a mesma version
appVersion: "dev"    # App version — INDEPENDENTE: tracking da tag da imagem Docker de cada componente
```

### Lockstep versioning (decisao de design)

**Decisao:** Todos os charts do LoopZero compartilham a mesma `version` (lockstep), similar ao modelo Istio.

**Trade-offs considerados:**

| Abordagem | Vantagem | Desvantagem |
|-----------|----------|-------------|
| **Lockstep (escolhido)** | Release simples (um bump, todos publicados), compatibilidade garantida, debugging facil ("tudo 0.3.0") | Publica charts sem mudanca real, ocupa espaco no registry |
| **Independente** | Publica apenas o que mudou, granularidade fina | Complexidade de release (qual version de A funciona com B?), matrix de compatibilidade, debug dificil |

**Em monorepo com file:// refs, lockstep e a escolha pragmatica.** O custo de re-publicar um chart sem mudanca e negligivel vs o custo de manter matrix de compatibilidade.

- `version` segue semver e e bumpada automaticamente pelo CI — todos os charts ao mesmo tempo.
- `appVersion` e independente por componente, tracking a tag da imagem Docker correspondente.
- No umbrella, `version` e identica aos sub-charts.

### Semver Rules

| Tipo de mudanca | Bump |
|----------------|------|
| Fix em template (bug no render) | PATCH (0.1.0 -> 0.1.1) |
| Novo value opcional, novo template condicional | MINOR (0.1.0 -> 0.2.0) |
| Breaking: rename de value, remocao de field, CRD schema breaking | MAJOR (0.1.0 -> 1.0.0) |

### Version constraints no umbrella

```yaml
# loopzero-platform/Chart.yaml — com lockstep, versions sao exatas
dependencies:
  - name: loopzero-crds
    version: "0.1.0"    # Lockstep: mesma version que o umbrella
```

> Nota: O CI preenche a version exata automaticamente antes do package (script yq na Task 4.1).

### Immutability

Cada version publicada no OCI registry e **immutable**. Re-push da mesma version e proibido. CI falha se a version ja existe no registry.

### Chart.lock

O umbrella gera `Chart.lock` via `helm dependency update`. 

**Lifecycle:**
- O `Chart.lock` commitado no git usa `file://` refs (para desenvolvimento local com `helm dep update`)
- O CI regenera o `Chart.lock` apos rewrite `file://` → `oci://` (para o artefato publicado)
- O lock publicado no OCI registry aponta para `oci://` — nunca `file://`

---

## Fase 1 — Foundation: Library + CRDs + Core Components

> Objetivo: criar o library chart, chart de CRDs, e os 5 componentes Go + dashboard, com helpers compartilhados.

### Task 1.1: `loopzero-common` (library chart)

**O que:** Chart tipo `library` com `_helpers.tpl` contendo template functions reutilizaveis.

**Helpers a criar:**
- `loopzero-common.labels` — labels Kubernetes padrao (app.kubernetes.io/name, instance, version, component, part-of: loopzero-platform, managed-by: Helm)
- `loopzero-common.selectorLabels` — subset para matchLabels (name + instance)
- `loopzero-common.image` — monta image ref a partir de `.Values.global.registry` + `.Values.image.repository` + `.Values.image.tag`
- `loopzero-common.podSecurityContext` — runAsNonRoot: true, runAsUser: 1000, fsGroup: 1000, seccompProfile: RuntimeDefault
- `loopzero-common.containerSecurityContext` — readOnlyRootFilesystem, drop ALL, no privilege escalation, seccompProfile: RuntimeDefault
- `loopzero-common.serviceAccountName` — resolve nome com toggle create/name
- `loopzero-common.automountServiceAccountToken` — default false, override para componentes que precisam de K8s API (operator, api)
- `loopzero-common.servicemonitor` — template condicional para ServiceMonitor (requer `metrics.serviceMonitor.enabled`)
- `loopzero-common.pdb` — template condicional para PodDisruptionBudget (requer `replicaCount > 1`, default `minAvailable: 1`)
- `loopzero-common.networkpolicy` — template base para NetworkPolicy com merge de egress rules: egress base (PostgreSQL, K8s API) definido no helper, cada chart adiciona egress custom (gitea, minio, argocd) via `.Values.networkPolicy.extraEgress`. Ingress e sempre chart-specific.
- `loopzero-common.topologySpreadConstraints` — template condicional para HA (spread across zones/nodes em prod)
- `loopzero-common.affinity` — podAntiAffinity padrao (`preferredDuringSchedulingIgnoredDuringExecution`, weight 100, topologyKey `kubernetes.io/hostname`). Habilitado por default quando `replicaCount > 1`.
- `loopzero-common.priorityClassName` — resolve `priorityClassName` a partir de `.Values.priorityClassName` (default: `system-cluster-critical` para componentes de platform)
- `loopzero-common.startupProbe` — template para startupProbe (failureThreshold: 30, periodSeconds: 2) para componentes com startup lento (leader election, DB connection)
- `loopzero-common.chartMetadata` — gera `Chart.yaml` annotations padrao (home, sources, maintainers)

**Arquivos:**
- `charts/loopzero-common/Chart.yaml` (type: library, version: 0.1.0)
- `charts/loopzero-common/templates/_helpers.tpl`
- `charts/loopzero-common/.helmignore`

**Chart.yaml padrao (todos os charts devem seguir):**
```yaml
apiVersion: v2
name: loopzero-common
description: Shared Helm helpers for the LoopZero PaaS platform
type: library
version: 0.1.0
home: https://github.com/loopzero/loopzero
sources:
  - https://github.com/loopzero/loopzero
maintainers:
  - name: LoopZero Team
    url: https://loopzero.example
icon: https://loopzero.example/icon.png
```

**Criterios de aceite:**
- [ ] `helm lint charts/loopzero-common` passa
- [ ] Helpers renderizam corretamente quando usados por um chart dependente
- [ ] `seccompProfile: RuntimeDefault` presente em ambos security context helpers
- [ ] `automountServiceAccountToken: false` e o default
- [ ] PDB helper renderiza apenas quando `replicaCount > 1`
- [ ] ServiceMonitor helper renderiza apenas quando `metrics.serviceMonitor.enabled: true`
- [ ] NetworkPolicy helper mergeia egress base + extraEgress do chart
- [ ] Affinity helper gera podAntiAffinity quando `replicaCount > 1`
- [ ] PriorityClassName helper default `system-cluster-critical`
- [ ] StartupProbe helper renderiza com failureThreshold/periodSeconds configuraveis

**DoD:** chart lint passa, nenhum helper produz output vazio, security hardening completo.

---

### Task 1.2: `loopzero-crds` (CRD chart)

**O que:** Chart dedicado para os 10 CRDs do LoopZero. Instalado antes de qualquer outro chart. CRDs vao em `templates/` (NAO em `crds/`) com resource-policy keep para sobreviver a `helm uninstall`.

**Por que chart separado (padrao cert-manager/Crossplane):**
- Helm NAO faz upgrade de CRDs no diretorio `crds/` — so instala na primeira vez
- Chart separado permite `helm upgrade loopzero-crds` para atualizar CRDs independentemente
- `resource-policy: keep` garante que `helm uninstall` NAO deleta CRDs (protege dados)
- Permite versionamento independente do operator

**CRDs (10 total, copiar de `operators/config/crd/bases/`):**
1. `loopzero.io_tenants.yaml`
2. `loopzero.io_loopzeroapplications.yaml`
3. `loopzero.io_environments.yaml`
4. `loopzero.io_previewenvironments.yaml`
5. `loopzero.io_exposedroutes.yaml`
6. `loopzero.io_gitrepositorybindings.yaml`
7. `loopzero.io_platformbootstraps.yaml`
8. `loopzero.io_reconciliationreports.yaml`
9. `loopzero.io_appsecrets.yaml`
10. `loopzero.io_clustercredentials.yaml`

**Templates:**
Cada CRD e um template separado com annotations:

```yaml
# charts/loopzero-crds/templates/loopzero.io_tenants.yaml
{{- if .Values.crds.tenants.enabled }}
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
  labels:
    {{- include "loopzero-common.labels" . | nindent 4 }}
  annotations:
    "helm.sh/resource-policy": keep
    controller-gen.kubebuilder.io/version: {{ .Values.controllerGenVersion | default "v0.17.2" }}
# ... CRD spec copiada de operators/config/crd/bases/
{{- end }}
```

**values.yaml:**
```yaml
crds:
  tenants:
    enabled: true
  loopzeroapplications:
    enabled: true
  environments:
    enabled: true
  previewenvironments:
    enabled: true
  exposedroutes:
    enabled: true
  gitrepositorybindings:
    enabled: true
  platformbootstraps:
    enabled: true
  reconciliationreports:
    enabled: true
  appsecrets:
    enabled: true
  clustercredentials:
    enabled: true

controllerGenVersion: "v0.17.2"
```

**CRD versioning policy:**
- Versao atual: `v1alpha1` unica stored version. Sem conversion webhook (adiado para GA).
- NUNCA remover campos de CRD spec — apenas deprecar com annotation `loopzero.io/deprecated-since: "0.x.0"`
- Novos campos sao sempre opcionais (nao quebra CRs existentes)
- Mudancas breaking em CRD requerem major version bump do chart
- Quando houver `v1beta1`/`v1`, conversion webhook sera necessario (chart separado `loopzero-crds-webhook` ou embutido)
- CRDs DEVEM ter `spec.versions[].schema.openAPIV3Schema` completo (structural schemas obrigatorios desde K8s 1.16)

**Criterios de aceite:**
- [ ] 10 CRDs em `templates/`, NAO em `crds/`
- [ ] Todas as CRDs tem `"helm.sh/resource-policy": keep`
- [ ] `helm lint charts/loopzero-crds` passa
- [ ] `helm template loopzero-crds charts/loopzero-crds | kubeconform --strict` passa
- [ ] `helm upgrade loopzero-crds` atualiza CRDs existentes (validar com k3d)
- [ ] `helm uninstall loopzero-crds` NAO deleta CRDs (resource-policy keep)
- [ ] Cada CRD e condicional via `crds.<name>.enabled`

**DoD:** chart renderiza 10 CRDs, upgrade funciona, uninstall preserva CRDs.

**Fonte de extracao:** `operators/config/crd/bases/*.yaml`

---

### Task 1.3: `loopzero-api` chart

**O que:** Chart para o API server. Extrai de `infra/tilt/api-deployment.yaml` (dev) e `infra/pulumi/src/components/mgmt-api.ts` (prod).

**Templates:**
- `deployment.yaml` — Deployment com env vars via ConfigMap + Secret refs, volume mounts para build-kubeconfig, probes (readiness + liveness + startup), resources, topologySpreadConstraints, podAntiAffinity, priorityClassName, revisionHistoryLimit
- `service.yaml` — Service com tipo configuravel (NodePort dev / ClusterIP prod), ports 3001 + 9090
- `serviceaccount.yaml` — conditional, `automountServiceAccountToken: true` (API precisa de K8s API)
- `clusterrole.yaml` — get/create/update Tenant CRs (minimo)
- `clusterrolebinding.yaml`
- `configmap.yaml` — env vars nao-secretas (PORT, BASE_DOMAIN, GITEA_URL, ARGOCD_URL, etc.)
- `secret.yaml` — DATABASE_URL, MINIO keys, tokens (com `existingSecret` alternativo)
- `networkpolicy.yaml` — ingress: allow from traefik/`traefik` ns (port 3001), allow from loopzero-controller/`loopzero-system` ns (port 3001); egress: allow to postgresql/`loopzero-system`, gitea/`gitea` ns, minio/`loopzero-system`, argocd/`argocd` ns, ory/`ory` ns (cross-namespace, FQDNs)
- `servicemonitor.yaml` — condicional em `metrics.serviceMonitor.enabled`, scrape port 9090
- `pdb.yaml` — condicional em `replicaCount > 1`, `minAvailable: 1`
- `tests/test-connection.yaml` — helm test hook: wget health endpoint
- `NOTES.txt` — instrucoes pos-install

**Fonte de verdade para env vars:** `api/internal/config/config.go` (50+ campos configuraveis)

**values.yaml key structure:**
```yaml
global:
  registry: registry.digitalocean.com/loopzero-infra-registry
  imagePullSecrets: []

image:
  repository: loopzero-api
  tag: dev
  pullPolicy: IfNotPresent

replicaCount: 1
revisionHistoryLimit: 3

priorityClassName: system-cluster-critical

podLabels: {}
podAnnotations: {}

service:
  type: ClusterIP
  port: 3001
  metricsPort: 9090

config:
  baseDomain: ""          # REQUIRED
  giteaUrl: ""            # REQUIRED
  argocdUrl: ""           # REQUIRED
  minioEndpoint: ""       # REQUIRED
  registryUrl: ""         # REQUIRED
  registryInsecure: "false"
  logLevel: "info"
  disableLegacyLoops: "false"
  jwksUrl: ""
  jwtIssuer: ""
  clusterIssuer: "letsencrypt-prod"

secrets:
  create: true
  existingSecret: ""
  databaseUrl: ""         # REQUIRED (quando create: true)
  giteaToken: ""
  argocdToken: ""
  minioAccessKey: ""
  minioSecretKey: ""
  argoCallbackSecret: ""

kubeconfig:
  build:
    existingConfigMap: ""  # nome do ConfigMap com build kubeconfig
  runtime:
    existingConfigMap: ""  # nome do ConfigMap com runtime kubeconfig

resources:
  requests: { cpu: 100m, memory: 64Mi }
  limits: { cpu: 500m, memory: 256Mi }

serviceAccount:
  create: true
  automountServiceAccountToken: true  # API precisa de K8s API access

rbac:
  create: true

metrics:
  serviceMonitor:
    enabled: false
    interval: 30s
    scrapeTimeout: 10s

networkPolicy:
  enabled: true

startupProbe:
  enabled: true
  failureThreshold: 30
  periodSeconds: 2

topologySpreadConstraints:
  enabled: false  # Habilitado via profile prod

affinity:
  podAntiAffinity:
    enabled: true  # Auto-habilitado quando replicaCount > 1 via helper

tolerations: []

nodeSelector: {}
```

**values.schema.json (campos REQUIRED):**
```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["config"],
  "properties": {
    "config": {
      "type": "object",
      "required": ["baseDomain", "giteaUrl", "argocdUrl", "minioEndpoint", "registryUrl"],
      "properties": {
        "baseDomain": { "type": "string", "minLength": 1, "description": "Base domain for the platform (e.g., dev.loopzero.example)" },
        "giteaUrl": { "type": "string", "minLength": 1, "format": "uri" },
        "argocdUrl": { "type": "string", "minLength": 1, "format": "uri" },
        "minioEndpoint": { "type": "string", "minLength": 1 },
        "registryUrl": { "type": "string", "minLength": 1 }
      }
    },
    "replicaCount": { "type": "integer", "minimum": 1 },
    "service": {
      "type": "object",
      "properties": {
        "type": { "type": "string", "enum": ["ClusterIP", "NodePort", "LoadBalancer"] },
        "port": { "type": "integer", "minimum": 1, "maximum": 65535 }
      }
    }
  }
}
```

**helm test hook:**

> Test hooks devem validar funcionalidade real, nao apenas conectividade. O `loopzero-api` usa `/api/v1/health` (deep check: DB + dependencias). Demais charts usam `/healthz` (readiness check).

```yaml
# charts/loopzero-api/templates/tests/test-connection.yaml
apiVersion: v1
kind: Pod
metadata:
  name: "{{ include "loopzero-api.fullname" . }}-test-connection"
  labels:
    {{- include "loopzero-common.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": test
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  containers:
    - name: curl
      image: curlimages/curl:8.7.1
      command:
        - sh
        - -c
        - |
          # Deep health check — valida DB connectivity, nao apenas rede
          HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --timeout 10 \
            "http://{{ include "loopzero-api.fullname" . }}:{{ .Values.service.port }}/api/v1/health")
          if [ "$HTTP_CODE" != "200" ]; then
            echo "FAIL: /api/v1/health returned $HTTP_CODE (expected 200)"
            curl -s "http://{{ include "loopzero-api.fullname" . }}:{{ .Values.service.port }}/api/v1/health"
            exit 1
          fi
          echo "PASS: deep health check returned 200"
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
        seccompProfile:
          type: RuntimeDefault
  restartPolicy: Never
  automountServiceAccountToken: false
```

**Criterios de aceite:**
- [ ] `helm lint charts/loopzero-api` passa
- [ ] `helm template loopzero-api charts/loopzero-api -f profiles/dev-values.yaml | kubeconform --strict` passa
- [ ] `values.schema.json` rejeita render sem campos REQUIRED
- [ ] Todos os env vars de `config.go` sao configuraveis via values
- [ ] Security context: non-root, read-only FS, drop ALL, seccompProfile RuntimeDefault
- [ ] `automountServiceAccountToken: true` (API precisa de K8s API)
- [ ] Probes configuram readiness+liveness+startup em `/api/v1/health:3001`
- [ ] `startupProbe` com failureThreshold=30, periodSeconds=2 (permite 60s de startup)
- [ ] NetworkPolicy: ingress de traefik e controller, egress via helper merge (base + extraEgress)
- [ ] ServiceMonitor condicional em `metrics.serviceMonitor.enabled`
- [ ] PDB condicional quando `replicaCount > 1`
- [ ] podAntiAffinity habilitado automaticamente quando `replicaCount > 1`
- [ ] `priorityClassName: system-cluster-critical` no Deployment
- [ ] `revisionHistoryLimit: 3` no Deployment
- [ ] `podLabels` e `podAnnotations` propagados ao pod template
- [ ] `helm test` valida deep health check (`/api/v1/health` retorna 200, nao apenas connectivity)
- [ ] Chart.yaml tem home, sources, maintainers, icon

**DoD:** chart renderiza manifests equivalentes ao `api-deployment.yaml` existente, com NetworkPolicy, ServiceMonitor, PDB, startupProbe, podAntiAffinity, priorityClassName, e test hook com deep health. lint + kubeconform + schema validation passam.

**Arquivos a modificar:** Nenhum existente. Todos novos em `charts/loopzero-api/`.

**Fontes de extracao:**
- `infra/tilt/api-deployment.yaml` (manifests dev)
- `api/internal/config/config.go` (env vars)

---

### Task 1.4: `loopzero-tenant-controller` chart

**O que:** Chart para o operator kubebuilder. NAO contem CRDs (estao no `loopzero-crds`). Declara dependencia no `loopzero-crds`.

> **Nota sobre naming:** O chart se chama `loopzero-tenant-controller` (consistencia com `loopzero-*`). Os recursos Kubernetes internos (ServiceAccount, ClusterRole, Deployment) podem usar `fullnameOverride: tenant-controller` se necessario para compatibilidade com manifests Tilt/Pulumi existentes.

**Chart.yaml:**
```yaml
apiVersion: v2
name: loopzero-tenant-controller
description: LoopZero tenant controller operator (kubebuilder)
type: application
version: 0.1.0
appVersion: "dev"
home: https://github.com/loopzero/loopzero
sources:
  - https://github.com/loopzero/loopzero
maintainers:
  - name: LoopZero Team
    url: https://loopzero.example
dependencies:
  - name: loopzero-common
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-common"
```

> CRDs NAO estao neste chart. Estao no `loopzero-crds`. O umbrella garante que `loopzero-crds` e instalado antes do `loopzero-tenant-controller` via hook-weight.

**Templates:**
- `deployment.yaml` — args: --leader-elect, --health-probe-bind-address, --metrics-bind-address. Inclui startupProbe (leader election pode demorar), priorityClassName, revisionHistoryLimit, podAntiAffinity, podLabels/podAnnotations.
- `serviceaccount.yaml` — `automountServiceAccountToken: true` (operator precisa de K8s API)
- `clusterrole.yaml` — regras extensivas de `operator-deployment.yaml` (CRDs, namespaces, RBAC, secrets, ArgoCD, leases, etc.)
- `clusterrolebinding.yaml`
- `configmap.yaml` — GITEA_URL, ARGOCD_NAMESPACE, BASE_DOMAIN, WORKLOAD_CLUSTER_URL, INGRESS_NAMESPACE
- `secret.yaml` — GITEA_TOKEN (com existingSecret)
- `networkpolicy.yaml` — ingress: allow metrics scrape (port 8443); egress: allow to K8s API, gitea, argocd, minio
- `servicemonitor.yaml` — condicional em `metrics.serviceMonitor.enabled`
- `pdb.yaml` — condicional (leader-elected, so 1 replica ativa, mas PDB protege contra eviction)
- `tests/test-health.yaml` — helm test: health probe endpoint
- `NOTES.txt`

**values.schema.json:** campos REQUIRED: giteaUrl, baseDomain, argocdNamespace

**Criterios de aceite:**
- [ ] ZERO CRDs neste chart (estao no loopzero-crds)
- [ ] `helm lint charts/loopzero-tenant-controller` passa
- [ ] `values.schema.json` valida campos obrigatorios
- [ ] ClusterRole tem TODAS as regras de `infra/tilt/operator-deployment.yaml`
- [ ] Leader election configuravel via values
- [ ] Volume mounts para build-kubeconfig e runtime-kubeconfig
- [ ] NetworkPolicy: egress via helper merge (base + extraEgress para gitea, argocd, minio)
- [ ] ServiceMonitor condicional
- [ ] `helm test` valida health probe (deep check, nao apenas connectivity)
- [ ] `automountServiceAccountToken: true`
- [ ] `seccompProfile: RuntimeDefault`
- [ ] `priorityClassName: system-cluster-critical`
- [ ] `startupProbe` habilitado (leader election startup pode demorar)
- [ ] `revisionHistoryLimit: 3`
- [ ] `podLabels` e `podAnnotations` configuraveis

**DoD:** chart renderiza corretamente sem CRDs, lint + kubeconform + schema passam.

**Fontes de extracao:**
- `infra/tilt/operator-deployment.yaml`
- `operators/cmd/main.go` (env vars)

---

### Task 1.5: `loopzero-controller` chart

**O que:** Chart para o controller de background loops. Usa a MESMA imagem do loopzero-api com command override `/loopzero-controller`.

**Templates:**
- `deployment.yaml` — `command: ["/loopzero-controller"]`, terminationGracePeriodSeconds: 60, startupProbe, priorityClassName, revisionHistoryLimit, podLabels/podAnnotations
- `service.yaml` — ClusterIP port 9091 (metrics + health)
- `serviceaccount.yaml` — `automountServiceAccountToken: true` (precisa de K8s API)
- `clusterrole.yaml` — read-only em Tenant + ClusterCredentials
- `clusterrolebinding.yaml`
- `configmap.yaml` + `secret.yaml`
- `networkpolicy.yaml` — ingress: metrics scrape; egress: postgresql, K8s API
- `servicemonitor.yaml` — condicional em `metrics.serviceMonitor.enabled`
- `tests/test-health.yaml`

**values.schema.json:** campos REQUIRED iguais ao loopzero-api (mesmo config)

**Criterios de aceite:**
- [ ] `image.repository` default = `loopzero-api` (mesma imagem), com comment explicativo no values.yaml
- [ ] `command: ["/loopzero-controller"]` renderizado no deployment
- [ ] ServiceMonitor condicional em `metrics.serviceMonitor.enabled`
- [ ] NetworkPolicy auto-contida
- [ ] POD_NAME via downward API (fieldRef: metadata.name)
- [ ] `helm test` valida health endpoint (deep check)
- [ ] `seccompProfile: RuntimeDefault`
- [ ] `automountServiceAccountToken: true`
- [ ] `priorityClassName: system-cluster-critical`
- [ ] `startupProbe` habilitado (DB connection startup)
- [ ] `revisionHistoryLimit: 3`
- [ ] `podLabels` e `podAnnotations` configuraveis

**DoD:** chart renderiza deployment com command override correto, todos os guards passam.

**Fonte de extracao:** `infra/tilt/controller-deployment.yaml`

---

### Task 1.6: `loopzero-cloud` chart

**O que:** Chart para o auth/billing service. Sem RBAC, sem volume mounts para kubeconfig.

**Templates:**
- `deployment.yaml` — container simples, probes (readiness + liveness + startup) em `/health:13002`, priorityClassName, revisionHistoryLimit, podLabels/podAnnotations
- `service.yaml` — tipo configuravel (NodePort dev / ClusterIP prod)
- `configmap.yaml` — 8 env vars
- `serviceaccount.yaml` — `automountServiceAccountToken: false` (nao precisa de K8s API)
- `networkpolicy.yaml` — ingress: allow from traefik (port 13002); egress: allow to kratos, hydra, loopzero-api
- `servicemonitor.yaml` — condicional
- `pdb.yaml` — condicional
- `tests/test-health.yaml`

**values.schema.json:** campos REQUIRED: kratosPulicUrl, hydraPublicUrl, engineApiUrl

**Env vars:** CLOUD_LISTEN_ADDR, KRATOS_PUBLIC_URL, KRATOS_ADMIN_URL, HYDRA_PUBLIC_URL, HYDRA_ADMIN_URL, HYDRA_CLIENT_ID, ENGINE_API_URL, LOOPZERO_ENV

**Criterios de aceite:**
- [ ] Service type configuravel (NodePort dev / ClusterIP prod)
- [ ] Port 13002 configuravel
- [ ] 8 env vars configuraveis via values
- [ ] Health check em `/health:13002`
- [ ] `automountServiceAccountToken: false`
- [ ] `seccompProfile: RuntimeDefault`
- [ ] NetworkPolicy via helper merge (base + extraEgress para kratos, hydra, loopzero-api)
- [ ] `values.schema.json` valida campos obrigatorios
- [ ] `helm test` valida health endpoint (deep check `/health`)
- [ ] `priorityClassName: system-cluster-critical`
- [ ] `startupProbe` habilitado
- [ ] `revisionHistoryLimit: 3`
- [ ] `podLabels` e `podAnnotations` configuraveis

**DoD:** lint + kubeconform + schema passam, helm test hook funciona.

**Fonte de extracao:** `infra/tilt/cloud-deployment.yaml`

---

### Task 1.7: `loopzero-dashboard` chart

**O que:** Chart para o frontend Vite React servido via nginx/serve.

**Templates:**
- `deployment.yaml` — container nginx/serve, probes (readiness + liveness) em `/`, revisionHistoryLimit, podLabels/podAnnotations. Sem startupProbe (static assets, startup e instantaneo).
- `service.yaml` — tipo configuravel, port 8080
- `configmap.yaml` — VITE_* env vars para runtime injection
- `serviceaccount.yaml` — `automountServiceAccountToken: false`
- `networkpolicy.yaml` — ingress: allow from traefik (port 8080); egress: deny all (static assets, nao precisa de egress)
- `tests/test-connection.yaml`

**values.schema.json:** campos REQUIRED: nenhum (dashboard funciona com defaults)

**Criterios de aceite:**
- [ ] Env vars VITE_* configuraveis (runtime config via docker-entrypoint.sh)
- [ ] Port configuravel (default 8080)
- [ ] Image separada do loopzero-cloud
- [ ] `automountServiceAccountToken: false`
- [ ] `seccompProfile: RuntimeDefault`
- [ ] NetworkPolicy: ingress from traefik, zero egress
- [ ] `helm test` valida que o port responde (HTTP 200 em `/`)
- [ ] `revisionHistoryLimit: 3`
- [ ] `podLabels` e `podAnnotations` configuraveis
- [ ] `priorityClassName` configuravel (default vazio para dashboard — nao e critical path)

**DoD:** lint + kubeconform + schema passam.

**Fonte de extracao:** Novo deployment (nao existe manifest Tilt dedicado para dashboard — extrair de `infra/pulumi/src/components/` se houver, ou criar from scratch baseado no Dockerfile do dashboard)

---

## Fase 2 — Migracao de Charts Existentes

> Objetivo: mover charts que ja existem em `infra/helm/` para `charts/` com templating Helm adequado.

### Task 2.1: Migrar `loopzero-build-cwts`

**O que:** Mover `infra/helm/argo-workflows/` para `charts/loopzero-build-cwts/`. Chart ja existe (v0.1.0).

**Mudancas:**
- Adicionar dependencia do `loopzero-common` para labels padronizados
- Atualizar referencias no Tiltfile e Taskfile.yml
- Adicionar `values.schema.json`
- Adicionar `.helmignore`
- Adicionar Chart.yaml metadata (home, sources, maintainers)

**Criterios de aceite:**
- [ ] Chart no novo local passa `helm lint`
- [ ] CWTs (clone, cache-save, cache-restore) renderizam corretamente
- [ ] NetworkPolicy e ResourceQuota incluidos
- [ ] Referencias em Tiltfile/Taskfile atualizadas
- [ ] `values.schema.json` presente
- [ ] Chart.yaml tem metadata completa

**DoD:** chart migrado, referencias atualizadas, lint + schema passam.

**Arquivos a modificar:**
- `Tiltfile` — atualizar path do chart
- `Taskfile.yml` — atualizar path do chart

---

### Task 2.2: Migrar `loopzero-monitoring`

**O que:** Converter `infra/helm/monitoring/` (manifests raw + dashboards JSON) em chart Helm.

> Nota: ServiceMonitors individuais de cada componente agora vivem dentro de cada chart (principio auto-contido). Este chart contem apenas PrometheusRules (alertas) e Grafana dashboards — recursos que sao cross-component por natureza.

**Templates a criar a partir dos manifests existentes:**
- PrometheusRules (8): capacity, critical, warning, database, domains, registry, otel-pipeline, probes
- Dashboards (20 JSONs) como ConfigMaps com label `grafana_dashboard: "1"`

**Criterios de aceite:**
- [ ] PrometheusRules templated e condicionais
- [ ] Dashboards como ConfigMaps com sidecar label
- [ ] Cada recurso condicional via values (ex: `prometheusRules.capacity.enabled`)
- [ ] Funciona sem prometheus-operator (conditional on CRDs)
- [ ] `values.schema.json` presente
- [ ] Chart.yaml metadata completa

**DoD:** chart com todos os recursos renderizaveis, lint + schema passam.

**Fonte de extracao:** `infra/helm/monitoring/manifests/` e `infra/helm/monitoring/dashboards/`

---

### Task 2.3: Migrar `loopzero-admission-policies`

**O que:** Converter `infra/helm/admission-policies/manifests/` em chart Helm.

**Templates (6 VAPs + bindings):**
1. `vap-require-resource-limits.yaml`
2. `vap-disallow-latest-tag.yaml`
3. `vap-restrict-registries.yaml`
4. `vap-require-image-signature.yaml`
5. `vap-argocd-managed-by.yaml`
6. `vap-protect-platform-networkpolicies.yaml`

**Criterios de aceite:**
- [ ] Cada policy condicional via values
- [ ] Lista de registries permitidos configuravel em `vap-restrict-registries`
- [ ] namespace selector configuravel
- [ ] `values.schema.json` presente
- [ ] Chart.yaml metadata completa

**DoD:** 6 policies templated, lint + schema passam.

**Fonte de extracao:** `infra/helm/admission-policies/manifests/`

---

## Fase 3 — Umbrella Chart + Profiles

> Objetivo: chart que instala toda a plataforma com um comando. Fases 1 e 2 devem estar completas.

### Task 3.1: `loopzero-platform` umbrella chart

**O que:** Chart que declara todos os sub-charts como dependencias condicionais.

**Chart.yaml dependencies:**
```yaml
dependencies:
  - name: loopzero-common
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-common"
  - name: loopzero-crds
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-crds"
    # Sem condition — CRDs sempre instalados
  - name: loopzero-tenant-controller
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-tenant-controller"
    condition: loopzero-tenant-controller.enabled
  - name: loopzero-api
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-api"
    condition: loopzero-api.enabled
  - name: loopzero-controller
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-controller"
    condition: loopzero-controller.enabled
  - name: loopzero-cloud
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-cloud"
    condition: loopzero-cloud.enabled
  - name: loopzero-dashboard
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-dashboard"
    condition: loopzero-dashboard.enabled
  - name: loopzero-build-cwts
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-build-cwts"
    condition: loopzero-build-cwts.enabled
  - name: loopzero-monitoring
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-monitoring"
    condition: loopzero-monitoring.enabled
  - name: loopzero-admission-policies
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-admission-policies"
    condition: loopzero-admission-policies.enabled
```

> Nota: `file://` refs sao usadas apenas para desenvolvimento local. O CI reescreve para `oci://` antes do `helm package` do umbrella (Task 4.1).

**Templates:** `namespace.yaml`, `NOTES.txt` (instrucoes pos-install)

**Chart.lock:** Gerado por `helm dependency update`, commitado no git.

**`file://` -> `oci://` rewrite (CI):**
O CI substitui `repository: "file://../<name>"` por `repository: "oci://registry.digitalocean.com/loopzero-infra-registry/helm"` e version ranges por versions exatas antes de empacotar o umbrella. Isso garante que o chart publicado no OCI registry funciona fora do monorepo.

**Criterios de aceite:**
- [ ] `helm dependency build charts/loopzero-platform` sucede
- [ ] `helm template loopzero charts/loopzero-platform` renderiza todos os sub-charts
- [ ] Install seletivo funciona: `--set loopzero-dashboard.enabled=false`
- [ ] `global.registry` propagado para todos os sub-charts
- [ ] `Chart.lock` presente e commitado
- [ ] `values.schema.json` valida global config
- [ ] Chart.yaml metadata completa (home, sources, maintainers)

**DoD:** dependency build + template + lint + schema passam. Chart.lock commitado.

---

### Task 3.2: Profiles dev e prod

**O que:** Arquivos de values pre-configurados para dev (k3d) e prod (DOKS).

**`profiles/dev.yaml`:**
```yaml
global:
  registry: k3d-loopzero-registry:5000
  imagePullSecrets: []

# Service types
loopzero-api:
  service:
    type: NodePort
  replicaCount: 1
  config:
    registryInsecure: "true"
  networkPolicy:
    enabled: false        # k3d nao tem CNI com NetworkPolicy por default
  metrics:
    serviceMonitor:
      enabled: false      # prometheus pode nao existir em dev

loopzero-controller:
  replicaCount: 1
  metrics:
    serviceMonitor:
      enabled: false

loopzero-tenant-controller:
  replicaCount: 1
  metrics:
    serviceMonitor:
      enabled: false

loopzero-cloud:
  service:
    type: NodePort
  replicaCount: 1

loopzero-dashboard:
  service:
    type: NodePort
  replicaCount: 1

loopzero-monitoring:
  enabled: false          # prometheus pode nao existir

loopzero-admission-policies:
  enabled: false          # VAPs nao necessarias em dev
```

> Nota: Secrets NAO sao incluidos no profile. Devem ser fornecidos via `--set` ou arquivo separado nao commitado (`secrets-dev.yaml` em `.gitignore`).

**`profiles/prod.yaml`:**
```yaml
global:
  registry: registry.digitalocean.com/loopzero-infra-registry
  imagePullSecrets:
    - name: docr-registry-secret

# HA configuration
loopzero-api:
  replicaCount: 2
  service:
    type: ClusterIP
  config:
    registryInsecure: "false"
  secrets:
    create: false
    existingSecret: "loopzero-api-secrets"  # Managed by ESO
  networkPolicy:
    enabled: true
  metrics:
    serviceMonitor:
      enabled: true
      interval: 15s
  topologySpreadConstraints:
    enabled: true
  affinity:
    podAntiAffinity:
      enabled: true       # Spread across nodes
  resources:
    requests: { cpu: 200m, memory: 128Mi }
    limits: { cpu: "1", memory: 512Mi }

loopzero-controller:
  replicaCount: 1         # Leader-elected, 1 ativo
  secrets:
    create: false
    existingSecret: "loopzero-controller-secrets"
  metrics:
    serviceMonitor:
      enabled: true

loopzero-tenant-controller:
  replicaCount: 1         # Leader-elected
  secrets:
    create: false
    existingSecret: "loopzero-tenant-controller-secrets"
  metrics:
    serviceMonitor:
      enabled: true

loopzero-cloud:
  replicaCount: 2
  service:
    type: ClusterIP
  secrets:
    create: false
    existingSecret: "loopzero-cloud-secrets"
  networkPolicy:
    enabled: true
  metrics:
    serviceMonitor:
      enabled: true
  topologySpreadConstraints:
    enabled: true
  affinity:
    podAntiAffinity:
      enabled: true

loopzero-dashboard:
  replicaCount: 2
  service:
    type: ClusterIP
  networkPolicy:
    enabled: true
  topologySpreadConstraints:
    enabled: true
  affinity:
    podAntiAffinity:
      enabled: true

loopzero-monitoring:
  enabled: true

loopzero-admission-policies:
  enabled: true
```

**Criterios de aceite:**
- [ ] `helm template -f profiles/dev.yaml` produz manifests equivalentes aos Tilt atuais
- [ ] `helm template -f profiles/prod.yaml` produz manifests equivalentes ao Pulumi atual
- [ ] Ambos passam `kubeconform --strict`
- [ ] ZERO secrets inline nos profiles (fornecidos externamente)
- [ ] Prod tem: topologySpreadConstraints, PDB, ServiceMonitor, NetworkPolicy, existingSecret
- [ ] Dev tem: NodePort, monitoring disabled, networkPolicy disabled

**DoD:** ambos profiles renderizam e validam. Secrets nao commitados.

---

### Task 3.3: Validacao end-to-end no k3d

**O que:** Instalar o umbrella chart num cluster k3d limpo e verificar que todos os pods sobem.

**Passos:**
1. `k3d cluster create loopzero-helm-test`
2. `helm install loopzero-crds charts/loopzero-crds -n loopzero-system --create-namespace`
3. `helm install loopzero charts/loopzero-platform -f profiles/dev.yaml -n loopzero-system`
4. Verificar que CRDs estao instalados (10 CRDs)
5. Verificar que todos os pods atingem Ready
6. `helm test loopzero -n loopzero-system` — rodar todos os test hooks
7. `helm upgrade loopzero charts/loopzero-platform -f profiles/dev.yaml -n loopzero-system` — validar upgrade
8. `helm uninstall loopzero -n loopzero-system`
9. Verificar que CRDs sobrevivem ao uninstall (resource-policy keep)
10. `k3d cluster delete loopzero-helm-test`

**Criterios de aceite:**
- [ ] Todos os pods Ready em < 2 minutos
- [ ] 10 CRDs acessiveis via `kubectl get tenants`, `kubectl get clustercredentials`, etc.
- [ ] Services respondendo nos ports corretos
- [ ] `helm test` passa para todos os charts com test hooks
- [ ] `helm upgrade` funciona sem downtime
- [ ] `helm uninstall loopzero` limpa tudo exceto CRDs
- [ ] CRDs permanecem apos uninstall

**DoD:** install + test hooks + upgrade + uninstall num cluster limpo.

---

## Fase 4 — CI Pipeline + OCI Publishing

> Objetivo: automatizar packaging, signing e push dos charts para DOCR. Pode iniciar em paralelo com Fase 3.

### Task 4.1: Workflow `helm-charts.yml`

**O que:** GitHub Actions workflow para lint, test, package, sign e push de charts OCI.

**Trigger:** push para develop/homolog/main com mudancas em `charts/**`

**Jobs:**

1. **dep-update** — `helm dependency update` para cada chart com `dependencies` no Chart.yaml (obrigatorio antes de lint/template/package — sem isso, charts que dependem de `loopzero-common` falham com "missing in charts/ directory")
2. **lint** — `helm lint charts/*/` (depende de dep-update)
3. **schema-validate** — Verificar que cada chart tem `values.schema.json` e que `helm template` com values invalidos falha
4. **validate** — `helm template` + `kubeconform --strict` para cada chart
5. **unit-test** — `helm unittest charts/*/` (plugin helm-unittest)
6. **package** — Para cada chart:
   - `helm dependency update charts/<name>` (garantir deps atualizadas)
   - `helm package charts/<name>`
   - Para o umbrella: reescrever `file://` -> `oci://` no Chart.yaml via `yq` (NAO `sed`) antes do package, e regenerar `Chart.lock` apos rewrite
7. **sign** — `cosign sign` em cada chart OCI artifact (key-based, usando `COSIGN_KEY` GitHub Secret — mesma chave do image signing)
8. **push** — `helm push <name>-<version>.tgz oci://registry.digitalocean.com/loopzero-infra-registry/helm`
9. **verify** — `helm pull` + `cosign verify` do chart publicado

**Immutability guard:**
```bash
# Antes do push, verificar se a version ja existe
if helm pull oci://registry.../helm/${CHART_NAME} --version ${VERSION} 2>/dev/null; then
  echo "ERROR: ${CHART_NAME}:${VERSION} already exists. Bump the version."
  exit 1
fi
```

**Umbrella `file://` -> `oci://` rewrite script:**

> IMPORTANTE: Usar `yq` para manipulacao de YAML, NUNCA `sed`. `sed` em YAML e fragil — um espaco ou aspas diferente quebra o regex silenciosamente.

```bash
# Executado antes do helm package do umbrella
OCI_REGISTRY="oci://registry.digitalocean.com/loopzero-infra-registry/helm"

# 1. Reescrever todas as refs file:// para oci:// via yq (nao sed)
yq -i "(.dependencies[] | select(.repository == \"file://*\")).repository = \"$OCI_REGISTRY\"" \
  charts/loopzero-platform/Chart.yaml

# 2. Substituir version ranges por versions exatas dos sub-charts
for chart in charts/*/Chart.yaml; do
  name=$(yq '.name' "$chart")
  version=$(yq '.version' "$chart")
  yq -i "(.dependencies[] | select(.name == \"$name\")).version = \"$version\"" \
    charts/loopzero-platform/Chart.yaml
done

# 3. Regenerar Chart.lock apos rewrite (o lock commitado no git e para file:// local)
helm dependency update charts/loopzero-platform
```

**Chart.lock lifecycle:**
- O `Chart.lock` commitado no git e gerado com `file://` refs (para desenvolvimento local)
- O CI regenera o `Chart.lock` apos o rewrite para `oci://` (para o artefato publicado)
- O `Chart.lock` no artefato OCI publicado aponta para `oci://` — nao para `file://`

**Criterios de aceite:**
- [ ] Workflow triggera em mudancas sob `charts/`
- [ ] `helm dependency update` executado para cada chart com dependencies ANTES de lint/template
- [ ] `values.schema.json` validado para cada chart
- [ ] `helm unittest` roda para charts com testes
- [ ] Cada chart e empacotado e pushado como OCI artifact
- [ ] Cada chart OCI e assinado com cosign (key-based, `COSIGN_KEY` GitHub Secret)
- [ ] Umbrella e publicado com refs OCI (nao file://) — rewrite via `yq`, NUNCA `sed`
- [ ] `Chart.lock` regenerado apos rewrite no umbrella
- [ ] `helm pull oci://registry.../helm/loopzero-platform --version 0.1.0` funciona
- [ ] `cosign verify oci://registry.../helm/loopzero-platform:0.1.0` funciona
- [ ] Falha em lint/schema/unittest bloqueia push
- [ ] Re-push da mesma version e bloqueado (immutability)

**DoD:** workflow roda com sucesso, charts acessiveis e verificaveis via OCI.

**Arquivo a criar:** `.github/workflows/helm-charts.yml`

---

### Task 4.2: CI lint para PRs

**O que:** Adicionar validacao de charts no `ci.yml` existente.

**Mudancas no `ci.yml`:**
- Novo job `lint-charts` com change detection para `charts/**`
- `helm dependency update` para cada chart com dependencies (obrigatorio antes de lint/template — `ct lint` executa internamente mas precisa de acesso aos sibling charts via `--chart-dirs=charts`)
- `helm lint` + `helm unittest` + `kubeconform` no output renderizado
- `values.schema.json` validation (render com values invalidos deve falhar)
- Adicionado ao `ci-gate` needs
- Usar `ct lint --chart-dirs=charts` (chart-testing) para lint padronizado CNCF
- Usar `ct install --chart-dirs=charts` com cluster k3d efemero para validacao de install+test (padrao CNCF, requer `kind`/`k3d` no runner)

**Criterios de aceite:**
- [ ] PRs que quebram charts falham CI
- [ ] Job skipped quando charts nao mudam (path filter)
- [ ] `helm dependency update` executado antes de lint para charts com deps
- [ ] `ct lint` roda para charts modificados com `--chart-dirs=charts`
- [ ] `ct install` roda para charts modificados (install + helm test num cluster efemero)
- [ ] `helm unittest` roda para charts com testes
- [ ] Schema validation: render com campo REQUIRED faltando deve falhar
- [ ] Executa em < 5 minutos (ct install com k3d inclui cluster bootstrap)

**DoD:** job integrado no CI gate, path filter funciona, ct install valida install real.

**Arquivo a modificar:** `.github/workflows/ci.yml`

---

### Task 4.3: Taskfile commands

**O que:** Comandos locais para operacoes com charts.

**Tasks a adicionar no Taskfile.yml:**
```yaml
helm:lint           # helm lint charts/*/
helm:unittest       # helm unittest charts/*/ (requer plugin)
helm:template       # helm template de todos os charts (dry-run)
helm:template:dev   # helm template com profile dev
helm:template:prod  # helm template com profile prod
helm:schema:validate # Validar values.schema.json de cada chart
helm:package        # helm package de todos os charts
helm:sign           # cosign sign dos charts empacotados
helm:push           # helm push para DOCR (requer doctl login)
helm:install:dev    # helm install umbrella com profile dev no k3d
helm:install:test   # helm test pos-install (roda test hooks)
helm:sync-crds      # copia CRDs de operators/config/crd/bases/ para charts/loopzero-crds/templates/
helm:dep:update     # helm dependency update do umbrella (regenera Chart.lock)
```

**Criterios de aceite:**
- [ ] `task helm:lint` passa em todos os charts
- [ ] `task helm:unittest` passa (quando testes existem)
- [ ] `task helm:sync-crds` mantém CRDs sincronizados (10 CRDs)
- [ ] `task helm:schema:validate` verifica schema de cada chart
- [ ] `task helm:dep:update` regenera Chart.lock

**DoD:** todos os tasks executam com sucesso.

**Arquivo a modificar:** `Taskfile.yml`

---

### Task 4.4: CRD sync safety (CI guard)

**O que:** Step no CI que verifica se os CRDs em `charts/loopzero-crds/templates/` estao sincronizados com `operators/config/crd/bases/`.

**Verificacoes:**
1. **Drift source -> chart:** CRDs em `operators/config/crd/bases/` devem estar identicos aos em `charts/loopzero-crds/templates/` (exceto annotations Helm adicionadas)
2. **Contagem:** Exatamente 10 CRDs em ambos os diretorios
3. **Nenhum CRD novo em source sem correspondente no chart**

**Criterios de aceite:**
- [ ] CI falha se CRDs divergirem
- [ ] CI falha se contagem de CRDs for diferente
- [ ] Mensagem de erro clara: "Run `task helm:sync-crds` to fix"

**DoD:** guard integrado no CI, detecta drift.

**Arquivo a modificar:** `.github/workflows/ci.yml`

---

### Task 4.5: Helm unittest snapshots

**O que:** Criar testes unitarios para templates Helm usando o plugin `helm-unittest`.

**Testes por chart (minimo):**

```yaml
# charts/loopzero-api/tests/deployment_test.yaml
suite: deployment
templates:
  - deployment.yaml
tests:
  - it: should render deployment with correct image
    set:
      global.registry: registry.example.com
      image.repository: loopzero-api
      image.tag: "1.0.0"
    asserts:
      - isKind:
          of: Deployment
      - equal:
          path: spec.template.spec.containers[0].image
          value: registry.example.com/loopzero-api:1.0.0

  - it: should have security context with seccompProfile
    asserts:
      - equal:
          path: spec.template.spec.securityContext.seccompProfile.type
          value: RuntimeDefault
      - equal:
          path: spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation
          value: false

  - it: should not render PDB when replicaCount is 1
    set:
      replicaCount: 1
    template: pdb.yaml
    asserts:
      - hasDocuments:
          count: 0

  - it: should render PDB when replicaCount > 1
    set:
      replicaCount: 3
    template: pdb.yaml
    asserts:
      - isKind:
          of: PodDisruptionBudget

  - it: should render ServiceMonitor when enabled
    set:
      metrics.serviceMonitor.enabled: true
    template: servicemonitor.yaml
    asserts:
      - isKind:
          of: ServiceMonitor

  - it: should NOT render ServiceMonitor when disabled
    set:
      metrics.serviceMonitor.enabled: false
    template: servicemonitor.yaml
    asserts:
      - hasDocuments:
          count: 0

  - it: should render NetworkPolicy when enabled
    set:
      networkPolicy.enabled: true
    template: networkpolicy.yaml
    asserts:
      - isKind:
          of: NetworkPolicy
```

**Cobertura minima por chart:**
- Deployment renderiza com image correta
- Security context completo (seccompProfile, drop ALL, non-root)
- PDB condicional em replicaCount
- ServiceMonitor condicional
- NetworkPolicy condicional
- automountServiceAccountToken correto (true para operator/api, false para dashboard/cloud)
- Recursos condicionais nao renderizam quando disabled
- `priorityClassName` presente no Deployment (platform components)
- `revisionHistoryLimit: 3` no Deployment
- `startupProbe` presente quando habilitado
- `podAntiAffinity` renderiza quando `replicaCount > 1`
- `podLabels` e `podAnnotations` propagados ao pod template
- NetworkPolicy egress merge: base rules + extraEgress

**Criterios de aceite:**
- [ ] Cada chart tem pelo menos 5 testes unitarios
- [ ] Testes cobrem todos os recursos condicionais (on/off)
- [ ] Testes validam security context hardening
- [ ] `helm unittest charts/*/` passa 100%
- [ ] Snapshot tests para output completo de cada chart

**DoD:** testes unitarios passam para todos os charts.

---

### Task 4.6: Upgrade strategy e hooks

**O que:** Definir estrategia de upgrade para cada componente e implementar lifecycle hooks.

**Pre-upgrade hook para DB migrations:**
```yaml
# charts/loopzero-api/templates/hooks/pre-upgrade-migration.yaml
{{- if .Values.migrations.enabled }}
apiVersion: batch/v1
kind: Job
metadata:
  name: "{{ include "loopzero-api.fullname" . }}-migrate"
  labels:
    {{- include "loopzero-common.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-5"
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  template:
    spec:
      containers:
        - name: migrate
          image: "{{ include "loopzero-common.image" . }}"
          command: ["/loopzero-migrate", "--direction=up"]
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: {{ include "loopzero-api.secretName" . }}
                  key: databaseUrl
      restartPolicy: Never
      automountServiceAccountToken: false
  backoffLimit: 3
{{- end }}
```

**Upgrade ordering (via hook-weight):**
```
-10: loopzero-crds (CRDs primeiro)
 -5: DB migrations (pre-upgrade)
  0: Deployments (default)
```

**Rollback safety:**

| Componente | Rollback seguro? | Notas |
|-----------|-----------------|-------|
| loopzero-crds | PARCIAL | Novos campos OK (ignorados por pods antigos). Campos removidos = BREAKING. Policy: nunca remover. |
| loopzero-api | SIM | Deployment rollback padrao |
| loopzero-tenant-controller | SIM | Stateless, leader-elected |
| loopzero-controller | SIM | Stateless |
| loopzero-cloud | SIM | Stateless |
| loopzero-dashboard | SIM | Stateless |
| DB migration | NAO | Forward-only. Rollback do chart NAO reverte migration. |

**Criterios de aceite:**
- [ ] Pre-upgrade migration Job renderiza quando `migrations.enabled: true`
- [ ] Hook weights garantem ordem: CRDs -> migrations -> deployments
- [ ] `helm rollback` funciona para components stateless
- [ ] Documentacao clara sobre forward-only migrations
- [ ] CRD policy: nunca remover campos, so deprecar

**DoD:** hooks implementados, estrategia documentada.

---

### Task 4.7: Documentacao

**O que:** Atualizar CLAUDE.md com nova estrutura, comandos Helm, e instrucoes de install. Gerar README por chart.

**Mudancas:**
- Adicionar `charts/` ao tree de Repository Structure
- Adicionar commands `task helm:*` a secao Development Commands
- Adicionar secao "Installing LoopZero via Helm" com exemplos
- Gerar README.md por chart usando `helm-docs` (tabela de values automatica)

**Chart README (gerado por helm-docs):**

Cada chart deve ter um `README.md` gerado automaticamente contendo:
- Descricao do chart
- Tabela de values com tipos, defaults e descricoes
- Exemplos de instalacao
- Notas de upgrade

**Template para helm-docs (`README.md.gotmpl`):**
```markdown
{{ template "chart.header" . }}

{{ template "chart.description" . }}

## Installing

\```bash
helm install {{ template "chart.name" . }} oci://registry.digitalocean.com/loopzero-infra-registry/helm/{{ template "chart.name" . }} --version {{ template "chart.version" . }}
\```

{{ template "chart.requirementsTable" . }}

{{ template "chart.valuesTable" . }}
```

**Criterios de aceite:**
- [ ] Tree atualizado com todos os charts (incluindo loopzero-crds)
- [ ] Comandos helm documentados
- [ ] Instrucoes de install para cluster novo (CRDs primeiro, depois umbrella)
- [ ] README.md por chart com tabela de values
- [ ] helm-docs configurado para regeneracao automatica

**DoD:** CLAUDE.md atualizado, READMEs gerados.

**Arquivos a modificar:** `CLAUDE.md`, `charts/*/README.md` (novos)

---

## Dependencias entre tasks

```
Fase 0 (namespace convergence) ──── PRE-REQUISITO para tudo
  platform-system → loopzero-system (Pulumi + operators/constants)
  Gitea → namespace `gitea` (Taskfile + Pulumi + service URLs)
  Ory → namespace `ory` (Taskfile + Pulumi + service URLs)
  ArgoCD → namespace `argocd` (ja correto na maioria dos refs)
  Service URLs → FQDN com namespaces corretos (api/config, operators/types)

Fase 0 completa ─┐
                  ├──► Task 1.1 (loopzero-common) ─────────┐
                  │                                      ├──► Task 1.3 (loopzero-api)
                  │                                      ├──► Task 1.4 (loopzero-tenant-controller)
                  │                                      ├──► Task 1.5 (loopzero-controller)
                  │                                      ├──► Task 1.6 (loopzero-cloud)
                  │                                      └──► Task 1.7 (loopzero-dashboard)
                  │
                  └──► Task 1.2 (loopzero-crds) ──── independente de 1.1, pode rodar em paralelo

Tasks 2.1-2.3 (migracoes) ──────── independentes entre si, independentes da Fase 1

Fase 1 completa ─┐
Fase 2 completa ─┤
Task 1.2 ────────┴──► Task 3.1 (umbrella) ──► Task 3.2 (profiles) ──► Task 3.3 (validacao e2e)

Task 4.1 (workflow) ──────── pode iniciar quando pelo menos 1 chart da Fase 1 existe
Task 4.2 (CI lint) ────────── pode iniciar junto com 4.1
Task 4.3 (Taskfile) ──────── pode iniciar junto com Fase 1
Task 4.4 (CRD sync) ──────── requer Task 1.2
Task 4.5 (unittest) ──────── requer pelo menos 1 chart da Fase 1
Task 4.6 (upgrade) ────────── requer Task 1.3 (loopzero-api, para migration hook)
Task 4.7 (docs) ──────────── apos todas as outras
```

---

## Riscos mitigados

| Risco | Mitigacao |
|-------|-----------|
| CRD drift entre `operators/config/crd/bases/` e `charts/loopzero-crds/templates/` | Task 4.4 — CI guard que falha se divergir + `task helm:sync-crds` |
| CRD upgrade ignorado pelo Helm | Task 1.2 — CRDs em `templates/` (nao `crds/`), chart separado com lifecycle proprio |
| CRD breaking change em upgrade | Policy: nunca remover campos, so deprecar. Major version bump obrigatorio. |
| Env var drift entre config.go / Tilt / Helm values | Task 3.2 — profiles dev/prod validados contra manifests existentes |
| Umbrella publicado com file:// refs | Task 4.1 — CI rewrite script substitui file:// por oci:// antes do package |
| DOCR nao suportar OCI Helm | DOCR e OCI-compliant. Testar com chart dummy na Task 4.1 antes de investir mais |
| Quebrar dev loop Tilt | Manifests Tilt (`infra/tilt/*.yaml`) ficam intactos. Charts sao aditivos |
| Bootstrap circular | Nao existe — DOCR e externo ao cluster sendo instalado |
| Typo em values silencioso | `values.schema.json` em cada chart rejeita inputs invalidos antes do render |
| Template regression silenciosa | Task 4.5 — helm unittest com snapshots detecta mudancas inesperadas |
| Secrets commitados em profiles | Profiles nao contem secrets. Fornecidos via `--set` ou arquivo em `.gitignore` |
| Chart OCI tampering | Task 4.1 — cosign sign + verify em cada chart publicado |
| Upgrade quebra pods (migration ordering) | Task 4.6 — pre-upgrade hooks com weight ordering: CRDs -> migrations -> deployments |
| Version collision no registry | Task 4.1 — immutability guard bloqueia re-push da mesma version |
| `helm dep update` esquecido no CI | Task 4.1/4.2 — dep-update e step obrigatorio antes de lint/template/package. Sem isso, charts com dependencies falham |
| `sed` rewrite quebra YAML silenciosamente | Task 4.1 — rewrite via `yq` (parser YAML real), NUNCA `sed` (regex fragil) |
| Chart.lock diverge entre local e publicado | Task 4.1 — Chart.lock no git e para `file://` (dev). CI regenera apos rewrite para `oci://`. Documentado. |
| CRD stored version migration | Task 1.2 — v1alpha1 unica stored version. Conversion webhook adiado para GA. Documentado na policy. |
| Platform pods evicted por pressure | Todos os charts — `priorityClassName: system-cluster-critical` garante scheduling priority sobre workloads de usuario |
| Pod killed durante startup lento | Charts com startup lento — `startupProbe` com failureThreshold=30 evita liveness kill prematuro |
| Namespace diverge entre dev e prod | Fase 0: convergencia para `loopzero-system` (LoopZero) + namespaces upstream (terceiros). `platform-system` eliminado. Padrao CNCF `{project}-system`. |
| Service discovery com namespaces separados | Todos os service URLs usam FQDN (`svc.cluster.local`). NetworkPolicies cross-namespace explicitamente permitidas. |
| Terceiros em namespaces upstream | ArgoCD em `argocd`, Gitea em `gitea`, Ory em `ory`. Respeita upstream, facilita troubleshooting com documentacao oficial. |

---

## Quality gates (CNCF checklist)

Cada chart DEVE atender a todos estes criterios antes de ser publicado:

| # | Criterio | Ferramenta | Obrigatorio? |
|---|---------|------------|-------------|
| 1 | `helm lint` passa | helm lint | SIM |
| 2 | `helm template` + `kubeconform --strict` | kubeconform | SIM |
| 3 | `values.schema.json` presente e valida | helm template | SIM |
| 4 | `helm unittest` passa (quando testes existem) | helm-unittest | SIM |
| 5 | `helm test` hook presente | helm test | SIM |
| 6 | Chart.yaml tem home, sources, maintainers | ct lint | SIM |
| 7 | `.helmignore` presente | manual | SIM |
| 8 | README.md gerado (helm-docs) | helm-docs | SIM |
| 9 | Security: seccompProfile RuntimeDefault | helm unittest | SIM |
| 10 | Security: automountServiceAccountToken explicitamente definido | helm unittest | SIM |
| 11 | Security: drop ALL capabilities | helm unittest | SIM |
| 12 | Security: runAsNonRoot: true | helm unittest | SIM |
| 13 | NetworkPolicy template (condicional) | helm unittest | SIM |
| 14 | ServiceMonitor template (condicional) | helm unittest | SIM |
| 15 | PDB template (condicional quando replicaCount > 1) | helm unittest | SIM |
| 16 | priorityClassName presente (platform components) | helm unittest | SIM |
| 17 | startupProbe presente (componentes com startup lento) | helm unittest | SIM |
| 18 | revisionHistoryLimit explicitamente definido | helm unittest | SIM |
| 19 | podAntiAffinity auto-habilitado quando replicaCount > 1 | helm unittest | SIM |
| 20 | podLabels e podAnnotations propagados | helm unittest | SIM |
| 21 | NetworkPolicy egress merge (base + extraEgress) | helm unittest | SIM |
| 22 | helm test hook com deep health check (nao apenas connectivity) | helm test | SIM |
| 23 | `ct install` passa em cluster efemero | ct install | SIM |
| 24 | `helm dependency update` executado antes de lint/package | CI workflow | SIM |
| 25 | No deprecated K8s APIs | pluto | RECOMENDADO |
| 26 | Best practice score >= 8/10 | kube-score | RECOMENDADO |
| 27 | OCI artifact assinado com cosign (key-based, COSIGN_KEY) | cosign verify | SIM |

---

## Verificacao final

Quando todas as 4 fases estiverem completas:

```bash
# 1. Charts publicados no DOCR
helm pull oci://registry.digitalocean.com/loopzero-infra-registry/helm/loopzero-platform --version 0.1.0

# 2. Verificar assinatura
cosign verify oci://registry.digitalocean.com/loopzero-infra-registry/helm/loopzero-platform:0.1.0

# 3. Install em cluster limpo
k3d cluster create test-install

# 4. CRDs primeiro (chart separado)
helm install loopzero-crds \
  oci://registry.digitalocean.com/loopzero-infra-registry/helm/loopzero-crds \
  --version 0.1.0 \
  -n loopzero-system --create-namespace

# 5. Platform
helm install loopzero \
  oci://registry.digitalocean.com/loopzero-infra-registry/helm/loopzero-platform \
  --version 0.1.0 \
  -n loopzero-system \
  -f profiles/dev.yaml

# 6. Test hooks
helm test loopzero -n loopzero-system

# 7. Pods running
kubectl get pods -n loopzero-system  # todos Ready

# 8. CRDs instalados
kubectl get crd | grep loopzero.io   # 10 CRDs

# 9. Upgrade
helm upgrade loopzero \
  oci://registry.digitalocean.com/loopzero-infra-registry/helm/loopzero-platform \
  --version 0.1.1 \
  -n loopzero-system \
  -f profiles/dev.yaml

# 10. Cleanup
helm uninstall loopzero -n loopzero-system
kubectl get crd | grep loopzero.io   # 10 CRDs still present (resource-policy keep)
helm uninstall loopzero-crds -n loopzero-system  # Explicit CRD removal only when intentional
k3d cluster delete test-install
```
