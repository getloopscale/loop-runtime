# LoopZero Platform — Infrastructure & CI/CD Documentation

> Documento técnico completo da infraestrutura de produção do LoopZero.
> Cobre: arquitetura de clusters, Pulumi IaC, pipeline CI/CD, bootstrap, TLS, DNS, e operações.

---

## Visão Geral da Arquitetura

O LoopZero é provisionado como uma plataforma **multi-cluster** na DigitalOcean via Pulumi (TypeScript). A infraestrutura é dividida em 3 fases sequenciais, cada uma com seus componentes e contratos.

```
┌─────────────────────────────────────────────────────────────────┐
│                     DigitalOcean VPC (10.20.0.0/16)             │
│                                                                 │
│  ┌──────────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │  Management (mgmt)│  │ Build        │  │ Runtime          │  │
│  │                   │  │              │  │                  │  │
│  │  ArgoCD           │  │ Argo         │  │ User Apps        │  │
│  │  Gitea            │  │ Workflows    │  │ (managed by      │  │
│  │  Traefik          │  │ + Kaniko     │  │  ArgoCD)         │  │
│  │  cert-manager     │  │              │  │                  │  │
│  │  Prometheus       │  │              │  │ Traefik          │  │
│  │  Grafana          │  │              │  │ cert-manager     │  │
│  │  LoopZero API         │  │              │  │                  │  │
│  │  TenantController │  │              │  │                  │  │
│  └──────────────────┘  └──────────────┘  └──────────────────┘  │
│                                                                 │
│  ┌──────────────────────┐  ┌────────────────────────────────┐  │
│  │  Managed PostgreSQL   │  │  Spaces (S3-compatible)        │  │
│  │  DBs: loopzero, gitea     │  │  Buckets: artifacts, builds    │  │
│  └──────────────────────┘  └────────────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Container Registry (global, shared by all clusters)      │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Por que 3 clusters?

| Cluster | Responsabilidade | Isolamento |
|---------|-----------------|-----------|
| **Management** | ArgoCD, API, operadores, observabilidade | Controle de acesso restrito; single point of coordination |
| **Build** | Argo Workflows + Kaniko | Consumo imprevisível de CPU/RAM; isolamento previne impacto no control plane |
| **Runtime** | Aplicações de usuário | Workloads de tenant isolados; ArgoCD sincroniza manifests do mgmt |

---

## Stack Tecnológico

| Componente | Tecnologia | Versão | Função |
|-----------|------------|--------|--------|
| IaC | Pulumi (TypeScript) | 3.x | Provisiona toda infraestrutura declarativamente |
| CI/CD | GitHub Actions | — | Pipeline de deploy automatizado |
| State backend | DigitalOcean Spaces (S3) | — | Armazena Pulumi state (não depende do Pulumi Cloud) |
| Kubernetes | DOKS (DigitalOcean) | 1.32.13 | 3 clusters gerenciados |
| Database | Managed PostgreSQL | 16 | State da plataforma (loopzero) + Gitea (gitea) |
| Ingress | Traefik | 28.3.0 | Load balancer + TLS termination + routing |
| TLS | cert-manager + Let's Encrypt | 1.14.5 | Certificados automáticos via HTTP-01 |
| GitOps | ArgoCD | 7.3.11 | Sincroniza manifests Gitea → runtime cluster |
| Git server | Gitea | 10.6.0 | Desired state mirror (repos de tenant) |
| Build engine | Argo Workflows + Kaniko | 1.0.5 | Container builds isolados |
| Observabilidade | Prometheus + Grafana | 25.27.0 / 8.5.2 | Métricas + dashboards |
| Storage | DigitalOcean Spaces | — | Artifacts (bundles, cache) |
| Registry | DO Container Registry | — | Imagens de container |

---

## Estrutura do Código Pulumi

```
infra/pulumi/
├── Pulumi.yaml                     # Projeto: loopzero-infra, runtime: nodejs
├── Pulumi.dev.yaml                 # Config do stack dev (com secrets encriptados)
├── Pulumi.staging.yaml             # Config do stack staging
├── Pulumi.prod.yaml                # Config do stack prod
├── package.json                    # Deps: @pulumi/pulumi, @pulumi/digitalocean, @pulumi/kubernetes
├── tsconfig.json
│
├── src/
│   ├── index.ts                    # Entry point — orquestra 3 fases + exporta outputs
│   │
│   ├── config/
│   │   └── stack.ts                # loadStackConfig() — carrega config do Pulumi, fail-fast
│   │
│   ├── core/
│   │   ├── types.ts                # CloudProvider, Environment, NodeClass
│   │   ├── naming.ts               # resourceName(), bucketName() — convenção de nomes
│   │   ├── tags.ts                 # standardTags(), tagsToStringArray()
│   │   ├── errors.ts               # InfraError — códigos tipados com contexto
│   │   └── execution.ts            # ExecutionState — rastreamento de fases/componentes
│   │
│   ├── contracts/                  # Interfaces ISP (Interface Segregation Principle)
│   │   ├── provider.ts             # CloudProviderFactory (compõe todas interfaces)
│   │   ├── network.ts              # IProviderNetwork
│   │   ├── kubernetes.ts           # IProviderKubernetes
│   │   ├── database.ts             # IProviderDatabase
│   │   ├── storage.ts              # IProviderStorage
│   │   ├── dns.ts                  # IProviderDns
│   │   └── compute.ts              # IProviderCompute
│   │
│   ├── providers/
│   │   └── digitalocean/           # Implementação concreta (única ativa)
│   │       ├── index.ts            # DigitalOceanProviderFactory
│   │       ├── network.ts          # VPC (10.20.0.0/16)
│   │       ├── kubernetes.ts       # DOKS clusters
│   │       ├── database.ts         # Managed PostgreSQL
│   │       ├── storage.ts          # Spaces buckets
│   │       ├── dns.ts              # DNS records (auto-detect A/AAAA/CNAME)
│   │       ├── compute.ts          # Droplets
│   │       └── nodes.ts            # NodeClass → instance size mapping
│   │
│   ├── components/                 # Orquestração de alto nível
│   │   ├── infra.ts                # Fase 1: Network + Clusters + DB + Storage + Registry
│   │   ├── bootstrap.ts            # Fase 2: Helm charts + Deployments + TLS
│   │   └── expose.ts               # Fase 3: DNS records
│   │
│   └── __tests__/                  # Vitest unit tests
│       ├── naming.test.ts
│       ├── nodes.test.ts
│       ├── dns-detection.test.ts
│       ├── tags.test.ts
│       └── errors.test.ts
```

---

## Design Pattern: ISP Contracts

O sistema usa **Interface Segregation Principle** para abstrair providers de cloud. Cada capacidade é uma interface separada:

```typescript
// Cada provider implementa APENAS as interfaces que suporta
interface IProviderNetwork {
  createNetwork(spec: NetworkSpec): NetworkOutput;
}

interface IProviderKubernetes {
  createKubernetesCluster(spec: KubernetesClusterSpec): KubernetesClusterOutput;
}

interface IProviderDatabase {
  createDatabaseCluster(spec: DatabaseClusterSpec): DatabaseClusterOutput;
}

// Factory compõe todas interfaces
interface CloudProviderFactory
  extends IProviderNetwork, IProviderKubernetes, IProviderStorage,
          IProviderDns, IProviderCompute, IProviderDatabase {}
```

**Benefício:** Para adicionar AWS, implementa-se cada interface separadamente sem tocar no DigitalOcean. O `resolveProvider()` no `index.ts` faz o switch:

```typescript
function resolveProvider(provider: CloudProvider): CloudProviderFactory {
  switch (provider) {
    case "digitalocean": return new DigitalOceanProviderFactory();
    case "aws": return new AwsProviderFactory(); // throws PROVIDER_NOT_IMPLEMENTED
  }
}
```

---

## Configuração do Stack

Toda configuração vem do `Pulumi.{stack}.yaml`. Valores obrigatórios falham imediatamente (`config.require()`).

```typescript
interface PlatformStackConfig {
  project: string;          // "loopzero-infra" (do Pulumi.yaml)
  environment: Environment; // "dev" | "staging" | "prod"
  provider: CloudProvider;  // "digitalocean" | "aws"
  region: string;           // "sfo3"
  domain: string;           // "dev.loopzero.example"
  dnsZone: string;          // "loopzero.example"
  infraVersion: string;     // "v1"

  // Management cluster
  mgmtK8sVersion?: string;  // "1.32.13-do.0"
  mgmtNodeClass: NodeClass;
  mgmtMinNodes: number;
  mgmtMaxNodes?: number;

  // Runtime cluster
  runtimeNodeClass: NodeClass;
  runtimeMinNodes: number;
  runtimeMaxNodes?: number;

  // Build cluster
  buildNodeClass: NodeClass;
  buildMinNodes: number;
  buildMaxNodes?: number;

  // Database
  dbNodeClass: NodeClass;
  dbVersion: string;        // "16"
  dbNodeCount: number;

  // Secrets (Pulumi encrypted)
  spacesAccessKeyId: pulumi.Output<string>;
  spacesSecretAccessKey: pulumi.Output<string>;
  digitaloceanToken: pulumi.Output<string>;
}
```

### NodeClass → DigitalOcean Mapping

| NodeClass | Compute (K8s/Droplets) | Database |
|-----------|----------------------|----------|
| `small` | `s-2vcpu-4gb` | `db-s-1vcpu-2gb` |
| `medium` | `s-4vcpu-8gb` | `db-s-2vcpu-4gb` |
| `large` | `s-8vcpu-16gb` | `db-s-4vcpu-8gb` |
| `xlarge` | `s-16vcpu-32gb` | `db-s-8vcpu-16gb` |

---

## Fases de Provisionamento

### Fase 1: Infraestrutura Base (`infra.ts`)

Cria todos os recursos cloud fundamentais:

```
createInfra(config)
  ├── createNetwork()           → VPC 10.20.0.0/16
  ├── createKubernetesCluster() → mgmt (2-3 nodes, small)
  ├── createKubernetesCluster() → build (2-3 nodes, small)
  ├── createKubernetesCluster() → runtime (2-3 nodes, small)
  ├── createDatabaseCluster()   → PostgreSQL 16 (DBs: loopzero, gitea)
  ├── createObjectStorage()     → Spaces bucket (artifacts)
  └── createContainerRegistry() → DO Container Registry (basic tier)
```

Todos os clusters e DB estão na mesma VPC — comunicação via rede privada.

### Fase 2: Bootstrap dos Clusters (`bootstrap.ts`)

Instala serviços K8s via Helm + Deployments customizados.

#### Management Cluster

Sequência de dependências:
```
namespace (platform-system)
  → cert-manager v1.14.5    (CRDs para TLS)
  → ArgoCD v7.3.11          (GitOps engine)
  → Gitea v10.6.0           (Git server, usa PostgreSQL externo)
  → Prometheus v25.27.0     (métricas + kube-state-metrics + node-exporter)
  → Grafana v8.5.2          (dashboards pré-configurados)
  → gitea-bootstrap Job     (cria admin, orgs, tokens)
  → ArgoCD repo credentials (Secret para autenticar no Gitea)
  → LoopZero API Deployment     (2 replicas, init container para migrations)
  → TenantController        (1 replica, leader election)
  → ArgoCD cluster secret   (registra runtime cluster como deploy target)
  → Traefik v28.3.0         (ingress + LoadBalancer)
  → ClusterIssuer           (Let's Encrypt prod via HTTP-01)
  → TLS IngressRoutes       (api, argocd, git, grafana)
```

#### Build Cluster

```
namespace (argo)
  → Argo Workflows v1.0.5   (controller + CRDs)
  → ClusterWorkflowTemplates (clone, cache-restore, cache-save)
```

#### Runtime Cluster

```
namespace (platform-system)
  → cert-manager v1.14.5    (para TLS em apps de tenant)
  → ClusterIssuer            (Let's Encrypt prod)
  → Traefik v28.3.0         (ingress + LoadBalancer para apps de tenant)
```

### Fase 3: Exposição (`expose.ts`)

Cria DNS records apontando os LoadBalancers para FQDNs públicos.

**Management subdomains** (A records → mgmt LB IP):
- `api.dev.loopzero.example`
- `argocd.dev.loopzero.example`
- `git.dev.loopzero.example`
- `grafana.dev.loopzero.example`

**Runtime wildcard** (A record → runtime LB IP):
- `*.dev.loopzero.example` → qualquer app deployada recebe DNS automático

**Cálculo de prefixo DNS:**
```typescript
// prod: api.loopzero.example (sem prefixo)
// dev:  api.dev.loopzero.example (prefixo "dev")
dnsPrefix("dev.loopzero.example", "loopzero.example") → "dev"
dnsPrefix("loopzero.example", "loopzero.example")     → "" (prod)
```

---

## Pipeline CI/CD — GitHub Actions

### Workflow: `infra-deploy.yml`

**Triggers:**
| Evento | Stack | Comando | Condição |
|--------|-------|---------|----------|
| Push `develop` | dev | up | Automático (paths: `infra/pulumi/**`, `infra/helm/**`, `api/internal/db/migrations/**`) |
| Push `main` | prod | up | Requer aprovação (GitHub Environment `infra-prod`) |
| `workflow_dispatch` | escolha | escolha | Manual (qualquer stack, qualquer comando) |

**Secrets necessários (por GitHub Environment):**
- `DIGITALOCEAN_TOKEN` — API token DO (full access)
- `SPACES_ACCESS_KEY_ID` — Chave Spaces (state backend + artifacts)
- `SPACES_SECRET_ACCESS_KEY` — Secret Spaces
- `PULUMI_CONFIG_PASSPHRASE` — Passphrase para secrets encriptados no state

### Estrutura do Pipeline

```
┌───────────┐     ┌───────────┐     ┌──────────────────────────────────────────┐
│  Resolve  │────▶│ Validate  │────▶│               Deploy                     │
│ (stack,   │     │ (tsc,     │     │                                          │
│  command) │     │  tests)   │     │  ┌─────────────────────────────────────┐  │
└───────────┘     └───────────┘     │  │ Pre-destroy cleanup (if destroy)   │  │
                                    │  │  • Remove K8s finalizers           │  │
                                    │  │  • Delete LBs, clusters, DB, VPC  │  │
                                    │  │  • Clean DNS records              │  │
                                    │  └─────────────────────────────────────┘  │
                                    │                                          │
                                    │  ┌─────────────────────────────────────┐  │
                                    │  │ Step 1/3: Provision infrastructure  │  │
                                    │  │  • pulumi cancel (stale locks)     │  │
                                    │  │  • pulumi refresh (sync state)     │  │
                                    │  │  • pulumi up (all resources)       │  │
                                    │  └───────────────┬─────────────────────┘  │
                                    │                  │ success()              │
                                    │  ┌───────────────▼─────────────────────┐  │
                                    │  │ Step 2/3: Build container images    │  │
                                    │  │  • docker build loopzero-api            │  │
                                    │  │  • docker build tenant-controller   │  │
                                    │  │  • docker push → DO Registry        │  │
                                    │  │  • doctl registry add (clusters)    │  │
                                    │  │  • Create loopzero-builds bucket        │  │
                                    │  └───────────────┬─────────────────────┘  │
                                    │                  │ success()              │
                                    │  ┌───────────────▼─────────────────────┐  │
                                    │  │ Step 3/3: Deploy applications       │  │
                                    │  │  • pulumi refresh (sync state)      │  │
                                    │  │  • pulumi up (converge)             │  │
                                    │  └───────────────┬─────────────────────┘  │
                                    │                  │ success()              │
                                    │  ┌───────────────▼─────────────────────┐  │
                                    │  │ Bootstrap platform                  │  │
                                    │  │  1. Gitea: token + orgs             │  │
                                    │  │  2. ArgoCD: JWT via /session API    │  │
                                    │  │  3. K8s Secrets: platform-tokens    │  │
                                    │  │     + repo credentials (repository) │  │
                                    │  │  4. ArgoCD restart (reload creds)   │  │
                                    │  │  5. DB seed: user + tenant + member │  │
                                    │  │  6. Tenant CR: kubectl apply        │  │
                                    │  │  7. Wait: controller → Active       │  │
                                    │  │  8. Restart: API + controller       │  │
                                    │  └───────────────┬─────────────────────┘  │
                                    │                  │                        │
                                    │  ┌───────────────▼─────────────────────┐  │
                                    │  │ Verify deployment health            │  │
                                    │  │  • pulumi stack output execution    │  │
                                    │  │  • Check all phases == completed    │  │
                                    │  └─────────────────────────────────────┘  │
                                    └──────────────────────────────────────────┘
```

### Controle de Concorrência

```yaml
concurrency:
  group: infra-deploy-${{ needs.resolve.outputs.stack }}
  cancel-in-progress: false  # NUNCA cancela deploy em progresso
```

**Razão:** Cancelar um `pulumi up` no meio cria recursos órfãos sem tracking no state. O `cancel-in-progress: false` garante que deploys são serializados.

### Step 1/3: Provision Infrastructure

```bash
# Cancel locks de runs anteriores que falharam
pulumi cancel -s "$STACK" 2>/dev/null || true

# Sincronizar state com realidade (handles deletes manuais, drift)
pulumi refresh -s "$STACK" --yes --skip-preview --non-interactive 2>/dev/null || true

# Criar todos os recursos
# SEM || true — falhas são reais e devem parar o pipeline
# skipAwait nos Deployments previne bloqueio por ImagePullBackOff
pulumi up --yes --skip-preview --stack "$STACK" --non-interactive
```

**Por que sem `|| true`:** Com `pulumi.com/skipAwait: "true"` nos Deployments e Services que dependem de imagens (loopzero-api, tenant-controller, gitea-bootstrap Job), o `pulumi up` completa sem bloquear. Se um recurso realmente falha (ex: região sem capacidade), o pipeline para imediatamente.

### Step 2/3: Build Container Images

```bash
# Autenticação
doctl registry login --expiry-seconds 600

# Build + push
docker build -f api/Dockerfile -t $REGISTRY/loopzero-api:dev .
docker push $REGISTRY/loopzero-api:dev
docker build -f operators/Dockerfile -t $REGISTRY/tenant-controller:dev .
docker push $REGISTRY/tenant-controller:dev

# Integrar registry com clusters DOKS
for KEY in mgmtClusterName buildClusterName runtimeClusterName; do
  CLUSTER=$(pulumi stack output "$KEY" --stack "$STACK")
  doctl kubernetes cluster registry add "$CLUSTER"
done

# Criar bucket para source bundles (não gerenciado pelo Pulumi)
INFRA_REGION=$(pulumi stack output region --stack "$STACK")
aws s3 mb "s3://loopzero-builds" --endpoint-url "https://${INFRA_REGION}.digitaloceanspaces.com"
```

### Step 3/3: Deploy Applications

```bash
# Refresh: Jobs completados podem ter sido GC'd pelo K8s entre Step 1 e agora
pulumi refresh -s "$STACK" --yes --skip-preview --non-interactive 2>/dev/null || true

# Converge: Deployments agora encontram as imagens no registry
pulumi up --yes --skip-preview --stack "$STACK" --non-interactive
```

### Bootstrap Platform

O step mais crítico — torna o sistema funcional após um deploy from-scratch.

#### 1. Gitea Token (com validação)

```bash
# Validar token existente contra Gitea API
EXISTING_TOKEN=$(kubectl get secret loopzero-platform-tokens -o jsonpath='{.data.GITEA_TOKEN}' | base64 -d)
kubectl port-forward svc/gitea-http 13099:3000 &
VALIDATE_RESP=$(curl -sf "http://localhost:13099/api/v1/user" -H "Authorization: token $EXISTING_TOKEN")

if echo "$VALIDATE_RESP" | grep -q "login"; then
  GITEA_TOKEN="$EXISTING_TOKEN"    # Token válido — reusar
else
  # Token inválido (novo Gitea após destroy+recreate) — gerar novo
  GITEA_TOKEN=$(kubectl exec "$GITEA_POD" -- gitea admin user generate-access-token \
    --username gitea_admin --token-name "loopzero-platform-$(date +%s)" --scopes all)
fi
```

**Por que validar:** Num destroy+recreate, o Secret K8s pode ter um token do deploy anterior. O novo Gitea não conhece esse token.

#### 2. ArgoCD JWT

```bash
# Obter password do admin
ARGOCD_PASS=$(kubectl get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)

# Gerar JWT via API REST (não usar password diretamente)
kubectl port-forward svc/argocd-server 13001:80 &
ARGOCD_JWT=$(curl -sf "http://localhost:13001/api/v1/session" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"'$ARGOCD_PASS'"}' | jq -r '.token')
```

**Por que JWT:** O ArgoCD API REST requer Bearer token JWT, não a password. O JWT tem expiração e é o formato correto para chamadas programáticas.

#### 3. K8s Secrets + ArgoCD Repo Registration

```bash
# Platform tokens (API + Controller consomem via env vars)
kubectl create secret generic loopzero-platform-tokens \
  --from-literal=GITEA_TOKEN="$GITEA_TOKEN" \
  --from-literal=ARGOCD_TOKEN="$ARGOCD_JWT" --dry-run=client -o yaml | kubectl apply -f -

# Credential template (fallback para qualquer repo loopzero/*)
kubectl create secret generic gitea-repo-creds \
  --from-literal=type=git --from-literal=url="$GITEA_BASE" \
  --from-literal=username=gitea_admin --from-literal=password="$GITEA_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl label secret gitea-repo-creds argocd.argoproj.io/secret-type=repo-creds --overwrite

# Registro EXPLÍCITO do repo do tenant bootstrap
# ArgoCD detecta type=repository de forma mais confiável que repo-creds (bug #7673)
kubectl create secret generic argocd-repo-tn-loopzero-gitops \
  --from-literal=type=git \
  --from-literal=url="${GITEA_BASE}/loopzero/tn-loopzero-gitops.git" \
  --from-literal=username=gitea_admin --from-literal=password="$GITEA_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl label secret argocd-repo-tn-loopzero-gitops argocd.argoproj.io/secret-type=repository --overwrite

# Reiniciar ArgoCD para detectar novos credentials imediatamente
kubectl rollout restart deployment argocd-repo-server
kubectl rollout restart statefulset argocd-application-controller
```

**Dois tipos de Secret:**
- `repo-creds` — template por URL prefix (fallback genérico)
- `repository` — registro explícito por repo (confiável, sem cache stale)

#### 4. Database Seed

```bash
# Resolve o GitHub user que triggerou o workflow
GH_LOGIN="${{ github.triggering_actor }}"

psql "$LOOPZERO_DB_URI" <<SQL
INSERT INTO users (id, email, github_id, display_name, created_at, updated_at)
VALUES ('github-${GH_LOGIN}', '${GH_LOGIN}@users.noreply.github.com', 0, '${GH_LOGIN}', NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET updated_at = NOW();

INSERT INTO tenants (id, display_name, plan, owner_id, created_at, updated_at)
VALUES ('tn-loopzero', 'loopzero', 'starter', 'github-${GH_LOGIN}', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_members (tenant_id, user_id, role, created_at, updated_at)
VALUES ('tn-loopzero', 'github-${GH_LOGIN}', 'owner', NOW(), NOW())
ON CONFLICT DO NOTHING;
SQL
```

**Idempotente:** `ON CONFLICT DO NOTHING` garante segurança em re-execuções.

#### 5. Tenant CR

```yaml
apiVersion: loopzero.io/v1
kind: Tenant
metadata:
  name: tn-loopzero
  namespace: platform-system
spec:
  displayName: loopzero
  plan: starter
  environments:
    - name: production
      cluster: runtime
  gitops:
    repoName: tn-loopzero-gitops
    branch: main
  limits:
    maxBuildConcurrency: 2
    maxCPU: "4"
    maxMemory: "8Gi"
    maxStorage: "20Gi"
  preview:
    maxEnvironments: 3
    ttlHours: 24
```

Aplicado via `kubectl apply`. O TenantController reconcilia:
- Namespace `loopzero-tn-loopzero-production` no runtime cluster
- Namespace `build-tn-loopzero` no build cluster
- RBAC (ServiceAccount, Role, RoleBinding)
- NetworkPolicies (deny-all + allow-ingress + allow-egress)
- Gitea repo (`tn-loopzero-gitops`)
- ArgoCD AppProject + Application
- ResourceQuotas + LimitRanges
- PriorityClass

---

## Pre-Destroy Cleanup

O destroy é a operação mais complexa. Kubernetes finalizers e cloud resources precisam ser limpos antes do `pulumi destroy`.

### Ordem de Operações

```
1. Remover finalizers de TODOS os CRDs + recursos cert-manager
2. Deletar Helm release secrets (previne "release not found")
3. Force-delete namespaces em todos os clusters
4. Deletar Load Balancers via DO API (bloqueiam VPC deletion)
5. Deletar clusters via DO API (dangerous=true)
6. Deletar PostgreSQL via DO API
7. Deletar Container Registry via DO API
8. Deletar Spaces buckets (todos que match loopzero-*, preservar pulumi-state)
9. Esperar 2 min (cloud deletions)
10. Deletar VPC (retry se membros restantes)
11. Deletar DNS records

Depois:
12. pulumi refresh (sync state com realidade)
13. pulumi destroy (limpa o que restou no state)
14. Se falhar: retry
15. Se falhar novamente: nuclear (clear state + delete via API)
```

### Nuclear Fallback

Quando tudo falha, limpa o Pulumi state mantendo apenas o Stack resource:

```bash
pulumi stack export | python3 -c "
import sys,json
s=json.load(sys.stdin)
s['deployment']['resources']=[r for r in s['deployment']['resources'] if r.get('type')=='pulumi:pulumi:Stack']
json.dump(s,sys.stdout)
" | pulumi stack import
```

---

## Execution State Tracking

Cada `pulumi up` produz um output `execution` que rastreia readiness:

```json
{
  "installationId": "dev-2026-03-23T17:32:21.235Z",
  "stack": "dev",
  "environment": "dev",
  "provider": "digitalocean",
  "phases": [
    {
      "phase": "infra",
      "status": "completed",
      "components": [
        {"component": "network", "status": "completed"},
        {"component": "mgmtCluster", "status": "completed"},
        {"component": "buildCluster", "status": "completed"},
        {"component": "runtimeCluster", "status": "completed"},
        {"component": "database", "status": "completed"},
        {"component": "storage", "status": "completed"},
        {"component": "registry", "status": "completed"}
      ]
    },
    {
      "phase": "bootstrap",
      "status": "completed",
      "components": [
        {"component": "mgmtIngress", "status": "completed"},
        {"component": "mgmtCertManager", "status": "completed"},
        {"component": "mgmtArgocd", "status": "completed"},
        {"component": "mgmtGitea", "status": "completed"},
        ...
      ]
    },
    {
      "phase": "expose",
      "status": "completed",
      "components": [
        {"component": "mgmtDns", "status": "completed"},
        {"component": "runtimeDns", "status": "completed"}
      ]
    }
  ]
}
```

**Query:** `pulumi stack output execution --json --stack dev`

---

## Stack Outputs (Contrato Estável)

Todos os valores que outros sistemas (API, operators, CI) consomem:

| Output | Tipo | Uso |
|--------|------|-----|
| `provider` | string | Identificação do cloud provider |
| `environment` | string | Environment tier (dev/staging/prod) |
| `region` | string | Região de deploy |
| `mgmtKubeconfig` | secret | Acesso ao cluster mgmt |
| `buildKubeconfig` | secret | Acesso ao cluster build |
| `runtimeKubeconfig` | secret | Acesso ao cluster runtime |
| `dbUri` | secret | Connection string PostgreSQL |
| `dbHost` | string | Host privado do DB |
| `bucketName` | string | Nome do bucket de artifacts |
| `registryEndpoint` | string | Endpoint do container registry |
| `mgmtIngressEndpoint` | string | IP do LB do mgmt |
| `runtimeIngressEndpoint` | string | IP do LB do runtime |
| `execution` | object | Estado de execução (fases/componentes) |
| `wiring` | object | Detalhes de conexão para API/operators |

O output `wiring` contém tudo que a API e operators precisam:

```json
{
  "gitea": { "internalUrl": "http://gitea-http.platform-system.svc:3000", "publicUrl": "https://git.dev.loopzero.example" },
  "grafana": { "internalUrl": "...", "publicUrl": "https://grafana.dev.loopzero.example" },
  "prometheus": { "internalUrl": "http://prometheus-server.platform-system.svc:80" },
  "registry": { "endpoint": "registry.digitalocean.com/loopzero-infra-registry" },
  "database": { "privateHost": "...", "port": 25060, "database": "defaultdb", "user": "doadmin" },
  "storage": { "bucket": "loopzero-infra-dev-sfo3-artifacts", "endpoint": "..." },
  "mgmt": { "ingressEndpoint": "209.38.119.12", "clusterEndpoint": "https://..." },
  "build": { "clusterEndpoint": "https://..." },
  "runtime": { "ingressEndpoint": "134.199.187.139", "clusterEndpoint": "https://..." }
}
```

---

## TLS Configuration

### Management Cluster (platform services)

Cada serviço tem um Certificate CR + IngressRoute:

```
cert-manager → ClusterIssuer (letsencrypt-prod, HTTP-01, class: traefik)
  → Certificate CR (tls-api, tls-argocd, tls-git, tls-grafana)
    → Secret (tls-api-secret, tls-argocd-secret, etc.)
      → IngressRoute (tls.secretName: tls-api-secret)
```

### Runtime Cluster (tenant apps)

O manifest renderer gera Certificate CR por app:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: tls-go-api
  namespace: loopzero-tn-loopzero-production
spec:
  secretName: tls-go-api
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - go-api-production--tn-loopzero.example.loopzero.example
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: go-api
spec:
  entryPoints: [websecure]
  routes:
    - match: Host(`go-api-production--tn-loopzero.example.loopzero.example`)
      kind: Rule
      services:
        - name: go-api
          port: 8080
  tls:
    secretName: tls-go-api
```

---

## Error Handling

### InfraError (Pulumi)

```typescript
class InfraError extends Error {
  code: "PROVIDER_NOT_IMPLEMENTED" | "INVALID_CONFIG" | "RESOURCE_CREATION_FAILED"
        | "NODE_CLASS_NOT_SUPPORTED" | "BOOTSTRAP_FAILED";
  context: { provider?, resource?, operation? };
  cause?: Error;
}
```

### Pipeline Error Handling

| Cenário | Comportamento |
|---------|-------------|
| Step 1 falha | Pipeline para. Step 2 e 3 não rodam (`if: success()`) |
| Step 2 falha (build) | Step 3 não roda |
| Step 3 falha | Execution state capturado, bootstrap não roda |
| Bootstrap falha | `continue-on-error: true` — não falha o pipeline |
| Destroy falha 1x | Retry com refresh |
| Destroy falha 2x | Nuclear: clear state + delete via API |

---

## Naming Conventions

| Recurso | Padrão | Exemplo |
|---------|--------|---------|
| VPC | `{project}-{env}-{region}-net` | `loopzero-infra-dev-sfo3-net` |
| Cluster | `{project}-{env}-{region}-{role}` | `loopzero-infra-dev-sfo3-mgmt` |
| Database | `{project}-{env}-{region}-db` | `loopzero-infra-dev-sfo3-db` |
| Bucket | `{project}-{env}-{region}-artifacts` | `loopzero-infra-dev-sfo3-artifacts` |
| Namespace | `platform-system` (platform) / `loopzero-{tenant}-{env}` (tenant) | `loopzero-tn-loopzero-production` |
| Build namespace | `build-{tenant}` | `build-tn-loopzero` |
| Helm release | `mgmt-{chart}` | `mgmt-traefik`, `mgmt-cert-manager` |
| Pulumi resource | `{role}-{resource}` | `runtime-cert-manager`, `mgmt-traefik` |
| DNS (mgmt) | `{subdomain}.{domain}` | `api.dev.loopzero.example` |
| DNS (runtime) | `*.{domain}` | `*.dev.loopzero.example` |
| App hostname | `{app}-{env}--{tenant}.{domain}` | `go-api-production--tn-loopzero.example.loopzero.example` |

---

## Tempos de Provisionamento

| Operação | Tempo Aproximado |
|----------|-----------------|
| Step 1: Infra (from scratch) | 10-15 min |
| Step 2: Build images | 2-3 min |
| Step 3: Converge | 1-2 min |
| Bootstrap | 3-5 min |
| **Total (from scratch)** | **~20 min** |
| Convergência (infra existente) | 3-5 min |
| Destroy (com cleanup) | 5-10 min |

---

## Testes

```bash
cd infra/pulumi

npm test              # Roda todos os testes (Vitest)
npx tsc --noEmit      # Type check sem emitir
```

| Teste | Cobertura |
|-------|-----------|
| `naming.test.ts` | `resourceName()`, `bucketName()`, validação RFC 1123 |
| `nodes.test.ts` | Mapping NodeClass → DO sizes (compute + DB) |
| `dns-detection.test.ts` | Auto-detecção A/AAAA/CNAME por valor do target |
| `tags.test.ts` | Formatação de tags, conversão para string array |
| `errors.test.ts` | InfraError structure, logging, codes |
