# Plano: LoopZero Self-Hosted — Do Installer CLI ao Marketplace

> **Status:** Proposto
> **Data:** 2026-04-24
> **Classificacao:** T3 (Arquitetura + Produto)
> **Pre-requisitos:** Helm Charts OCI no DOCR (concluido), CI/CD padronizado (concluido)

## Contexto

O LoopZero PaaS precisa ser instalavel em clusters Kubernetes de clientes (self-hosted). Hoje, instalar o LoopZero requer clonar o monorepo e conhecer a arquitetura interna. Com 20 clientes na fila de espera para self-hosting, precisamos de um caminho de instalacao que escale de 20 para 1000+ clientes.

**Benchmark de mercado:**

| Empresa | Produto | Abordagem | Escala |
|---|---|---|---|
| Confluent | Confluent Platform | `ConfluentPlatform` CRD + operator | 1000+ clientes |
| Elastic | ECK | `Elasticsearch` CRD + operator | 5000+ |
| MongoDB | MongoDB Enterprise | `MongoDBCommunity` CRD + operator | 2000+ |
| GitLab | GitLab Operator | `GitLab` CRD + operator | 500+ |
| Red Hat | OpenShift | Cluster Version Operator | Enterprise |

**Padrao CNCF:** Cliente instala UM operator → cria UM Custom Resource → operator instala e gerencia toda a plataforma.

---

## Principios

1. **Um comando para instalar** — O cliente nunca precisa saber que existe Gitea, ArgoCD, ou MinIO por tras.
2. **Declarativo** — Toda config e um YAML. Sem steps manuais, sem scripts interativos.
3. **Self-healing** — Se um componente cai, o operator restaura. Se o config drifta, o operator reconcilia.
4. **Upgrade atomico** — `kubectl edit loopzeroplatform` com nova version → operator faz rolling upgrade.
5. **Observavel** — `kubectl get loopzeroplatform` mostra status real-time de todos os componentes.
6. **Bring Your Own** — Cliente pode trazer seu proprio PostgreSQL, registry, ingress, TLS.

---

## Fase 1 — Installer CLI (Semana 1-2)

> **Objetivo:** Entregar self-hosting para os primeiros 20 clientes com um comando.
> **Entrega:** `loopzero platform install -f values.yaml`

### O que e

Um comando no CLI do LoopZero (`cli/`) que:

1. Valida prerequisites (kubectl, helm, cluster acessivel, versoes minimas)
2. Instala dependencias na ordem correta (Helm releases de terceiros)
3. Instala CRDs do LoopZero
4. Instala componentes LoopZero (umbrella chart)
5. Aguarda todos os pods ficarem Ready
6. Roda `helm test` para validacao
7. Imprime URLs de acesso e next steps

### Profiles

```
profiles/
├── minimal.yaml        ← 1 replica, sem HA, sem monitoring, para trial/dev
├── production.yaml     ← HA, monitoring, TLS, NetworkPolicy, PDB
└── enterprise.yaml     ← Multi-cell, multi-cluster (futuro)
```

### Fluxo de instalacao

```
loopzero platform install -f my-values.yaml --profile production

Step 1/7: Validating cluster...
  ✓ kubectl connected (v1.31.2)
  ✓ Helm v3.14+ (v3.14.4)
  ✓ Cluster has 3 nodes, 12 CPU, 48Gi RAM
  ✓ Default StorageClass: do-block-storage

Step 2/7: Installing prerequisites...
  ✓ cert-manager v1.14.5 (namespace: cert-manager)
  ✓ Traefik v28.3.0 (namespace: traefik)
  ✓ PostgreSQL v16 (namespace: loopzero-system) [or: using external PG]
  ✓ Gitea v10.6.0 (namespace: gitea)
  ✓ ArgoCD v2.13.3 (namespace: argocd)
  ✓ Ory Kratos + Hydra (namespace: ory)

Step 3/7: Installing CRDs...
  ✓ 10 LoopZero CRDs installed

Step 4/7: Installing LoopZero platform...
  ✓ loopzero-api (2 replicas)
  ✓ loopzero-tenant-controller (1 replica, leader-elected)
  ✓ loopzero-controller (1 replica)
  ✓ loopzero-cloud (2 replicas)
  ✓ loopzero-dashboard (2 replicas)

Step 5/7: Waiting for pods...
  ✓ All 8 pods Ready (47s)

Step 6/7: Running health checks...
  ✓ API health: /api/v1/health → 200
  ✓ Cloud health: /health → 200
  ✓ Dashboard: / → 200
  ✓ CRDs accessible: 10/10

Step 7/7: Done!

  Platform URL:    https://paas.customer.com
  Dashboard:       https://cloud.paas.customer.com
  API:             https://api.paas.customer.com

  Next steps:
  1. Configure DNS: *.paas.customer.com → <ingress-ip>
  2. Create first user: loopzero admin create-user --email admin@customer.com
  3. Login: loopzero login --api-url https://api.paas.customer.com
```

### Valores que o cliente fornece

```yaml
# customer-values.yaml
domain: "paas.customer.com"
clusterIssuer: "letsencrypt-prod"   # ou custom

# Database — bring your own ou instalar
database:
  external: true                     # false = instalar PostgreSQL via Helm
  url: "postgres://user:pass@pg.customer.com:5432/loopzero?sslmode=require"

# Registry — DOCR, Harbor, ou ECR
registry:
  type: "harbor"                     # docr | harbor | ecr | gcr
  endpoint: "harbor.customer.com"
  username: "loopzero-robot"
  password: "..."

# Auth — Ory (bundled) ou external OIDC
auth:
  type: "bundled"                    # bundled | external
  # external:
  #   issuer: "https://auth.customer.com"
  #   clientId: "loopzero"

# Storage — S3-compatible
storage:
  endpoint: "s3.amazonaws.com"
  bucket: "loopzero-artifacts"
  accessKey: "..."
  secretKey: "..."

# Sizing
profile: "production"                # minimal | production
```

### Implementacao

| Componente | Localizacao | Descricao |
|---|---|---|
| `cli/src/commands/platform.ts` | CLI | Comando `loopzero platform install/upgrade/status/uninstall` |
| `cli/src/installer/prerequisites.ts` | CLI | Instalacao de Helm releases de terceiros |
| `cli/src/installer/validator.ts` | CLI | Validacao de cluster e prerequisites |
| `cli/src/installer/profiles/` | CLI | YAML profiles (minimal, production) |
| Charts OCI no DOCR | Registry | Ja existem (v0.1.3) |

### Criterios de aceite

- [ ] `loopzero platform install -f values.yaml` instala plataforma completa em cluster limpo
- [ ] `loopzero platform status` mostra health de todos os componentes
- [ ] `loopzero platform upgrade --version 0.2.0` faz rolling upgrade
- [ ] `loopzero platform uninstall` remove tudo (exceto CRDs e dados)
- [ ] Profile `minimal` funciona em cluster de 2 CPU / 4Gi RAM
- [ ] Profile `production` configura HA, monitoring, NetworkPolicy, PDB
- [ ] Instalacao completa em < 5 minutos (excluindo download de imagens)
- [ ] Zero knowledge de K8s necessario para o cliente (nao precisa saber kubectl)

---

## Fase 2 — Platform Operator (Semana 3-6)

> **Objetivo:** Self-healing, upgrades automaticos, observabilidade nativa.
> **Entrega:** `LoopZeroPlatform` CRD + controller no operator existente.

### O que e

Um controller no `operators/` que reconcilia um CRD `LoopZeroPlatform`. O cliente cria um unico recurso declarativo e o operator gerencia todo o lifecycle.

### CRD: LoopZeroPlatform

```yaml
apiVersion: loopzero.io/v1
kind: LoopZeroPlatform
metadata:
  name: my-platform
  namespace: loopzero-system
spec:
  # Version — operator resolve para Helm chart + imagem tags
  version: "0.1.3"

  # Profile — templates de sizing pre-configurados
  profile: production      # minimal | production | enterprise

  # Domain — base domain para todos os servicos
  domain: "paas.customer.com"
  clusterIssuer: "letsencrypt-prod"

  # Database — bring your own
  database:
    secretRef:
      name: loopzero-database-credentials
      key: url

  # Registry — para imagens de tenant (Harbor, ECR, etc.)
  registry:
    type: harbor
    endpoint: "harbor.customer.com"
    secretRef:
      name: loopzero-registry-credentials

  # Storage — S3-compatible para artifacts
  storage:
    endpoint: "s3.amazonaws.com"
    bucket: "loopzero-artifacts"
    secretRef:
      name: loopzero-storage-credentials

  # Auth
  auth:
    type: bundled            # bundled (Ory) | external (OIDC)

  # Component overrides (optional)
  components:
    api:
      replicas: 3
      resources:
        requests: { cpu: 500m, memory: 256Mi }
    dashboard:
      enabled: true
    monitoring:
      enabled: true
      prometheusRules: true
      dashboards: true

status:
  phase: Ready               # Installing | Upgrading | Ready | Degraded | Failed
  version: "0.1.3"
  observedGeneration: 3

  components:
    - name: loopzero-api
      status: Running
      replicas: 3/3
      version: "0.1.3"
    - name: loopzero-tenant-controller
      status: Running
      replicas: 1/1
      version: "0.1.3"
    - name: loopzero-cloud
      status: Running
      replicas: 2/2
      version: "0.1.3"
    - name: loopzero-dashboard
      status: Running
      replicas: 2/2
      version: "0.1.3"
    - name: loopzero-crds
      status: Installed
      count: 10

  prerequisites:
    - name: postgresql
      status: Connected
      version: "16.4"
    - name: gitea
      status: Running
      version: "1.22.6"
    - name: argocd
      status: Running
      version: "2.13.3"
    - name: cert-manager
      status: Running
    - name: traefik
      status: Running

  conditions:
    - type: DatabaseReady
      status: "True"
      lastTransitionTime: "2026-04-24T10:00:00Z"
    - type: CRDsInstalled
      status: "True"
    - type: PrerequisitesReady
      status: "True"
    - type: ComponentsHealthy
      status: "True"
    - type: IngressConfigured
      status: "True"

  endpoints:
    api: "https://api.paas.customer.com"
    dashboard: "https://cloud.paas.customer.com"
    wildcard: "*.paas.customer.com"
```

### Controller: LoopZeroPlatformReconciler

```
Reconcile Loop:
  1. Validate spec (domain, database, registry)
  2. Ensure prerequisites (Helm releases de terceiros)
     - cert-manager → traefik → PG/Gitea/ArgoCD/Ory (ordem deterministica)
     - Cada um e um sub-reconcile idempotente
  3. Ensure CRDs (loopzero-crds chart)
  4. Ensure components (individual Helm releases ou manifests)
     - loopzero-api, loopzero-tenant-controller, loopzero-controller, loopzero-cloud, loopzero-dashboard
  5. Health check (probes nos endpoints)
  6. Update status (phase, conditions, components)
```

### Upgrade flow

```yaml
# Cliente muda a version:
spec:
  version: "0.2.0"    # era 0.1.3
```

O operator:
1. Detecta `.spec.version` != `.status.version`
2. Seta phase = `Upgrading`
3. Upgrade CRDs primeiro (sem downtime — additive only)
4. Upgrade componentes um a um (rolling)
5. Health check apos cada componente
6. Se falha → rollback automatico
7. Seta phase = `Ready` + version = `0.2.0`

### State Machine

```
Installing → Ready
Installing → Failed
Ready → Upgrading
Ready → Degraded
Upgrading → Ready
Upgrading → Failed
Degraded → Ready (self-heal)
Failed → Installing (retry)
```

### Implementacao

| Componente | Localizacao | Descricao |
|---|---|---|
| `operators/api/v1/loopzeroplatform_types.go` | Operator | CRD types |
| `operators/internal/controller/loopzeroplatform_controller.go` | Operator | Reconcile loop |
| `operators/internal/installer/prerequisites.go` | Operator | Helm release management |
| `operators/internal/installer/components.go` | Operator | Component lifecycle |
| `operators/internal/installer/health.go` | Operator | Health probes |
| `charts/loopzero-operator/` | Helm | Chart para instalar APENAS o operator |

### Diferenca: loopzero-platform (umbrella) vs loopzero-operator

| | loopzero-platform (Fase 1) | loopzero-operator (Fase 2) |
|---|---|---|
| Instala via | `helm install` ou `loopzero platform install` | `helm install loopzero-operator` + criar CR |
| Gerencia prerequisites | Nao (cliente instala antes) | Sim (operator instala/gerencia) |
| Self-healing | Nao | Sim (reconcile loop) |
| Upgrades | Manual (`helm upgrade`) | Declarativo (`spec.version: "0.2.0"`) |
| Status | `helm status` | `kubectl get loopzeroplatform` (real-time) |
| Rollback | `helm rollback` | Automatico em falha |
| Escala | 20 clientes (manual) | 1000+ (automatizado) |

### Criterios de aceite

- [ ] `LoopZeroPlatform` CRD registrado e validado
- [ ] Operator instala prerequisites na ordem correta
- [ ] Operator instala componentes LoopZero via Helm releases internos
- [ ] `status.phase` reflete estado real (Installing → Ready)
- [ ] `status.components` lista todos os componentes com health
- [ ] `status.conditions` segue KEP-1623 (conditions standard)
- [ ] Upgrade: mudar `spec.version` triggera rolling upgrade
- [ ] Rollback automatico se upgrade falha health check
- [ ] State machine validada (testes com envtest)
- [ ] `kubectl describe loopzeroplatform` mostra toda informacao necessaria para debug
- [ ] Operator e instalavel via: `helm install loopzero-operator oci://registry.digitalocean.com/loopzero-system/helm/loopzero-operator`

---

## Fase 3 — Marketplace + Self-Service (Semana 7-12)

> **Objetivo:** 1-click install em cloud marketplaces. Onboarding < 5 min.
> **Entrega:** Listings no DO Marketplace, AWS Marketplace, e portal self-service.

### 3.1: DigitalOcean Marketplace

**O que:** 1-Click App no DO Marketplace. Cliente clica "Install" → DOKS cluster criado + LoopZero instalado.

**Como:**
- DO Marketplace aceita Helm charts como formato de submission
- O `loopzero-operator` chart e submetido ao marketplace
- Post-install: operator cria `LoopZeroPlatform` CR automaticamente com defaults
- Cliente acessa `https://<cluster-ip>/setup` para wizard de configuracao

**Arquivo de submission:**
```yaml
# marketplace/digitalocean/app.yaml
name: loopzero-platform
slug: loopzero-paas
description: "Kubernetes PaaS for monorepo deployments. Deploy with git push."
categories: [developer-tools, kubernetes]
version: "0.2.0"
helm:
  chart: oci://registry.digitalocean.com/loopzero-system/helm/loopzero-operator
  namespace: loopzero-system
  values:
    autoInstall: true
    profile: production
requirements:
  kubernetes:
    minVersion: "1.28"
    nodes: 3
    minCPU: 4
    minRAM: 16Gi
```

### 3.2: AWS Marketplace

**O que:** Listing no AWS Marketplace como container product.

**Como:**
- EKS Anywhere ou EKS managed
- AMI ou Helm chart delivery
- Metering API para billing per-node ou per-tenant
- Integration com AWS License Manager

### 3.3: Self-Service Portal

**O que:** Portal web onde o cliente faz sign-up, gera o values.yaml, e instala.

**Fluxo:**
```
1. Cliente acessa https://install.loopzero.example
2. Preenche formulario:
   - Domain: paas.meucliente.com
   - Cloud: AWS / DO / GCP / On-prem
   - Database: Managed (RDS/DO) ou Self-hosted
   - Profile: Minimal / Production
3. Portal gera:
   - values.yaml customizado
   - Comando de install (`loopzero platform install` ou `helm install`)
   - Instrucoes DNS
4. Cliente roda o comando
5. Portal recebe heartbeat (opt-in telemetry) e mostra status
```

### 3.4: Telemetria e Licensing

**Para 1000+ clientes, precisa de:**

| Feature | Descricao |
|---|---|
| Heartbeat | Operator envia health status periodicamente (opt-in) |
| License validation | Operator valida license key no startup (offline grace period) |
| Usage metrics | Numero de tenants, builds, deploys (para billing) |
| Remote upgrade | Operator checa novas versoes e sugere upgrade |
| Support tunnel | `loopzero support connect` abre tunnel reverso para suporte |

### Criterios de aceite

- [ ] DO Marketplace listing aprovado e publicado
- [ ] 1-click install cria cluster + LoopZero em < 10 minutos
- [ ] Self-service portal gera values.yaml valido
- [ ] Telemetria heartbeat funcional (opt-in, GDPR compliant)
- [ ] License key validation com grace period offline de 7 dias
- [ ] AWS Marketplace listing (container product)

---

## Dependencias entre fases

```
Fase 1 (Installer CLI) ──────────────────────────────────────┐
  Task 1.1: Comando loopzero platform install/upgrade/status      │
  Task 1.2: Prerequisites installer (Helm releases)           │
  Task 1.3: Profiles (minimal, production)                    │
  Task 1.4: Validator (cluster, versions, resources)           │
  Task 1.5: Validacao local (k3d) + DO (DOKS)                 │
                                                               │
Fase 2 (Platform Operator) ──── depende de Fase 1 validada ───┤
  Task 2.1: LoopZeroPlatform CRD types                            │
  Task 2.2: Reconciler (prerequisites + components)            │
  Task 2.3: Health check + conditions                          │
  Task 2.4: Upgrade flow (rolling + rollback)                  │
  Task 2.5: State machine + envtest tests                      │
  Task 2.6: loopzero-operator Helm chart                           │
  Task 2.7: Migrar Fase 1 clientes para operator               │
                                                               │
Fase 3 (Marketplace) ──── depende de Fase 2 estavel ──────────┘
  Task 3.1: DO Marketplace submission
  Task 3.2: AWS Marketplace submission
  Task 3.3: Self-service portal (web)
  Task 3.4: Telemetria + licensing
```

---

## Riscos

| Risco | Mitigacao |
|---|---|
| Prerequisites incompativeis (versao K8s, CNI, CSI) | Fase 1: validator rigoroso com mensagens claras |
| Operator loop infinito em prerequisite falho | Fase 2: backoff exponencial + max retries + Degraded state |
| Upgrade quebra estado | Fase 2: pre-upgrade backup + rollback automatico |
| Marketplace rejection (review demorado) | Fase 3: submeter cedo, iterar com feedback |
| Telemetria opt-in recusada pelo cliente | Fase 3: funcionar 100% offline, telemetria e bonus |
| License key pirataria | Fase 3: validation server + grace period + feature gating |
| Cliente com cluster nao-standard (Rancher, OpenShift, k0s) | Fase 1: matrix de compatibilidade testada |

---

## Compatibilidade de cluster (matrix)

| Distribuicao | Fase 1 | Fase 2 | Notas |
|---|---|---|---|
| DOKS (DigitalOcean) | ✓ Primario | ✓ | Target principal |
| EKS (AWS) | ✓ | ✓ | Segundo target |
| GKE (Google) | ✓ | ✓ | Terceiro target |
| AKS (Azure) | ✓ | ✓ | Quarto target |
| k3s | ✓ | ✓ | Edge / on-prem pequeno |
| Rancher (RKE2) | Testado | ✓ | On-prem enterprise |
| OpenShift | Fase 3 | Fase 3 | Requer SecurityContextConstraints |
| Kind | Dev only | Dev only | CI testing |
| k3d | Dev only | Dev only | Local dev |

---

## Verificacao final

Quando as 3 fases estiverem completas:

```bash
# Fase 1: Installer CLI
loopzero platform install -f customer-values.yaml --profile production
loopzero platform status
loopzero platform upgrade --version 0.2.0

# Fase 2: Platform Operator
helm install loopzero-operator oci://registry.digitalocean.com/loopzero-system/helm/loopzero-operator
kubectl apply -f loopzeroplatform.yaml
kubectl get loopzeroplatform    # → Ready
kubectl describe loopzeroplatform my-platform   # → full status

# Fase 3: Marketplace
# DO: 1-click install from marketplace
# AWS: Deploy from AWS Marketplace
# Self-service: https://install.loopzero.example → generates install command
```
