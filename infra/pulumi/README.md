# loopzero-infra

Infrastructure-as-Code para a plataforma LoopZero. Provisiona uma topologia de produção completa na DigitalOcean (com portabilidade para AWS) usando Pulumi.

## Topologia

```
                    ┌──────────────────────────────┐
                    │   Management Cluster (DOKS)  │
                    │                              │
                    │  ArgoCD       ← GitOps       │
                    │  Traefik      ← ingress      │
                    │  cert-manager ← TLS          │
                    │  API (Go)     ← platform     │
                    └──────────────┬───────────────┘
                                   │
         ┌─────────────────────────┼──────────────────────────┐
         │                         │                          │
┌────────▼──────────┐   ┌─────────▼──────────┐   ┌───────────▼──────────┐
│   Gitea VM        │   │   Runner VMs       │   │   PostgreSQL (DO)    │
│   (git state)     │   │   (CI/CD builds)   │   │   Managed            │
└────────┬──────────┘   └─────────┬──────────┘   └───────────┬──────────┘
         │                        │                          │
         └────────────┬───────────┴────────────┬─────────────┘
                      │                        │
                ┌─────▼──────┐          ┌──────▼──────────┐
                │  Spaces    │          │ Runtime Cluster  │
                │  (S3-like) │          │ (DOKS)           │
                │  artifacts │          │ user apps only   │
                └────────────┘          └─────────────────┘
```

### Separação de responsabilidades

| Componente | Onde roda | Por quê |
|------------|-----------|---------|
| ArgoCD, API, Traefik | Management cluster | Platform tooling isolado do produto |
| User applications | Runtime cluster | Blast radius separado — mgmt down ≠ apps down |
| Gitea | VM dedicada | Stateful + critical path — fora do K8s |
| Runners | VMs separadas | Builds consomem recursos de forma imprevisível |
| PostgreSQL | Managed (DO) | Backups automáticos, failover, patching |
| Artifacts | Spaces (S3) | Object storage escalável e barato |

## Pipeline de 3 fases

```
Phase 1: createInfra()        Phase 2: bootstrapCluster()     Phase 3: exposePlatform()
┌────────────────────┐        ┌───────────────────────┐       ┌──────────────────────┐
│ VPC                │        │ Mgmt: cert-manager    │       │ Mgmt DNS:            │
│ Mgmt DOKS          │        │ Mgmt: ArgoCD          │       │   api.{domain}       │
│ Runtime DOKS       │──────> │ Mgmt: Traefik         │─────> │   argocd.{domain}    │
│ Gitea VM           │        │ Runtime: Traefik      │       │   grafana.{domain}   │
│ Runner VMs         │        └───────────────────────┘       │ Runtime DNS:         │
│ PostgreSQL         │                                        │   *.{domain}         │
│ Spaces             │                                        └──────────────────────┘
└────────────────────┘
     InfraOutputs      ──>      BootstrapOutputs       ──>     ExposeOutputs
```

## Estrutura

```
src/
├── index.ts                    Orquestrador: 3 fases + execution state + wiring
├── config/
│   └── stack.ts                Config por ambiente (fail-fast)
├── core/
│   ├── types.ts                CloudProvider, Environment, NodeClass
│   ├── naming.ts               Nomes determinísticos
│   ├── tags.ts                 Tags padrão
│   ├── errors.ts               InfraError tipado
│   └── execution.ts            Tracking de estado por fase e componente
├── contracts/
│   ├── provider.ts             Factory + 6 interfaces ISP
│   ├── network.ts              NetworkSpec / Output
│   ├── kubernetes.ts           KubernetesClusterSpec / Output
│   ├── storage.ts              ObjectStorageSpec / Output
│   ├── dns.ts                  DnsRecordSpec / Output
│   ├── compute.ts              ComputeInstanceSpec / Output (VMs)
│   └── database.ts             DatabaseClusterSpec / Output
├── components/
│   ├── infra.ts                Phase 1: topologia completa
│   ├── bootstrap.ts            Phase 2: dual-cluster (mgmt + runtime)
│   └── expose.ts               Phase 3: DNS (mgmt + runtime wildcard)
├── __tests__/
│   ├── naming.test.ts          resourceName, bucketName
│   ├── tags.test.ts            standardTags, tagsToStringArray
│   ├── errors.test.ts          InfraError formatting + structured log
│   ├── nodes.test.ts           resolveNodeSize, resolveDbSize
│   └── dns-detection.test.ts   A vs CNAME vs AAAA auto-detection
└── providers/
    ├── digitalocean/            Implementação completa (8 arquivos)
    │   ├── index.ts             Factory
    │   ├── nodes.ts             NodeClass → droplet/DB sizes
    │   ├── network.ts           VPC
    │   ├── kubernetes.ts        DOKS cluster
    │   ├── storage.ts           Spaces bucket
    │   ├── dns.ts               DNS records (A/CNAME/AAAA auto-detect)
    │   ├── compute.ts           Droplets (VMs)
    │   └── database.ts          Managed PostgreSQL
    └── aws/
        └── index.ts             Stub com guia de implementação
```

## Ambientes

| Stack | Domain | Mgmt Nodes | Runtime Nodes | Runners | DB |
|-------|--------|------------|---------------|---------|-----|
| `dev` | dev.loopzero.example | small 2–3 | small 2–3 | 1× small | small ×1 |
| `staging` | staging.loopzero.example | small 2–3 | small 2–4 | 2× small | small ×1 |
| `prod` | loopzero.example | medium 3–5 | medium 3–6 | 2× medium | medium ×2 |

## State Backend

O state do Pulumi é armazenado no **DigitalOcean Spaces** (S3-compatible). Sem dependência do Pulumi Cloud.

```
┌──────────────────────────────────┐
│  DigitalOcean Spaces             │
│  s3://loopzero-infra-pulumi-state          │
│  ├── .pulumi/stacks/dev.json     │  ← state do ambiente dev
│  ├── .pulumi/stacks/staging.json │  ← state do ambiente staging
│  └── .pulumi/stacks/prod.json    │  ← state do ambiente prod
└──────────────────────────────────┘
```

### Bootstrap (uma vez, antes do primeiro deploy)

O bucket de state é "infra de dia zero" — precisa existir antes do Pulumi rodar.

```bash
# 1. Criar Spaces access keys no painel DO:
#    https://cloud.digitalocean.com/account/api/spaces

# 2. Criar o bucket de state
doctl serverless functions init  # (ou via painel DO)
doctl compute cdn create --origin loopzero-infra-pulumi-state.nyc3.digitaloceanspaces.com 2>/dev/null || true

# Ou via s3cmd:
export AWS_ACCESS_KEY_ID="<spaces-key>"
export AWS_SECRET_ACCESS_KEY="<spaces-secret>"
aws s3 mb s3://loopzero-infra-pulumi-state --endpoint-url https://nyc3.digitaloceanspaces.com

# 3. Configurar variáveis locais
export AWS_ACCESS_KEY_ID="<spaces-key>"
export AWS_SECRET_ACCESS_KEY="<spaces-secret>"
export PULUMI_CONFIG_PASSPHRASE="<strong-passphrase>"

# 4. Login no backend S3
pulumi login 's3://loopzero-infra-pulumi-state?endpoint=nyc3.digitaloceanspaces.com&region=us-east-1'
```

## Uso

```bash
npm install
npm run typecheck
npm test

# Login no state backend (S3/Spaces)
export AWS_ACCESS_KEY_ID="<spaces-key>"
export AWS_SECRET_ACCESS_KEY="<spaces-secret>"
export PULUMI_CONFIG_PASSPHRASE="<passphrase>"
pulumi login 's3://loopzero-infra-pulumi-state?endpoint=nyc3.digitaloceanspaces.com&region=us-east-1'

# Deploy
pulumi up -s dev

# Outputs
pulumi stack output --json
pulumi stack output execution --json
pulumi stack output wiring --json
pulumi stack output mgmtKubeconfig --show-secrets > core.kubeconfig
pulumi stack output runtimeKubeconfig --show-secrets > runtime.kubeconfig
```

## Stack Outputs

### Phase 1 — Infra

| Output | Tipo | Descrição |
|--------|------|-----------|
| `networkId` | `string` | VPC ID |
| `mgmtClusterName` | `string` | Nome do cluster de management |
| `mgmtClusterEndpoint` | `string` | API endpoint do mgmt cluster |
| `mgmtKubeconfig` | `string` (secret) | Kubeconfig do mgmt cluster |
| `runtimeClusterName` | `string` | Nome do cluster runtime |
| `runtimeClusterEndpoint` | `string` | API endpoint do runtime cluster |
| `runtimeKubeconfig` | `string` (secret) | Kubeconfig do runtime cluster |
| `giteaIp` | `string` | IP público do Gitea |
| `giteaPrivateIp` | `string` | IP privado do Gitea (VPC) |
| `runnerIps` | `string[]` | IPs dos runner VMs |
| `dbHost` | `string` | Host privado do PostgreSQL |
| `dbPort` | `number` | Porta do PostgreSQL |
| `dbUri` | `string` (secret) | Connection string completa |
| `dbPassword` | `string` (secret) | Senha do PostgreSQL |
| `bucketName` | `string` | Nome do bucket |
| `bucketEndpoint` | `string` | Endpoint do bucket |

### Phase 2 — Bootstrap

| Output | Tipo | Descrição |
|--------|------|-----------|
| `mgmtIngressReady` | `boolean` | Traefik deployed no mgmt |
| `mgmtIngressEndpoint` | `string` | LB IP/hostname do mgmt |
| `certManagerReady` | `boolean` | cert-manager deployed |
| `argoCdReady` | `boolean` | ArgoCD deployed |
| `runtimeIngressReady` | `boolean` | Traefik deployed no runtime |
| `runtimeIngressEndpoint` | `string` | LB IP/hostname do runtime |

### Wiring (conexão entre componentes)

```bash
pulumi stack output wiring --json
```

```json
{
  "gitea": {
    "publicUrl": "http://1.2.3.4:3000",
    "privateUrl": "http://10.10.0.5:3000"
  },
  "database": {
    "privateHost": "db-host.db.ondigitalocean.com",
    "port": 25060,
    "database": "defaultdb",
    "user": "doadmin"
  },
  "storage": {
    "bucket": "loopzero-infra-dev-nyc3-artifacts",
    "endpoint": "nyc3.digitaloceanspaces.com"
  },
  "mgmt": {
    "ingressEndpoint": "1.2.3.5",
    "clusterEndpoint": "https://..."
  },
  "runtime": {
    "ingressEndpoint": "1.2.3.6",
    "clusterEndpoint": "https://..."
  }
}
```

### Execution State

```bash
pulumi stack output execution --json
```

```json
{
  "installationId": "dev-2026-03-21T10-00-00-000Z",
  "phases": [
    {
      "phase": "infra",
      "status": "completed",
      "components": [
        { "component": "network", "status": "completed" },
        { "component": "mgmtCluster", "status": "completed" },
        { "component": "runtimeCluster", "status": "completed" },
        { "component": "gitea", "status": "completed" },
        { "component": "runners", "status": "completed" },
        { "component": "database", "status": "completed" },
        { "component": "storage", "status": "completed" }
      ]
    },
    {
      "phase": "bootstrap",
      "status": "completed",
      "components": [
        { "component": "mgmtIngress", "status": "completed" },
        { "component": "mgmtCertManager", "status": "completed" },
        { "component": "mgmtArgocd", "status": "completed" },
        { "component": "runtimeIngress", "status": "completed" }
      ]
    },
    {
      "phase": "expose",
      "status": "pending",
      "components": [
        { "component": "mgmtDns", "status": "pending" },
        { "component": "runtimeDns", "status": "pending" }
      ]
    }
  ]
}
```

## Helm Charts Pinados

| Chart | Versão | Cluster | Repo |
|-------|--------|---------|------|
| cert-manager | v1.14.5 | mgmt | https://charts.jetstack.io |
| ArgoCD | 7.3.11 | mgmt | https://argoproj.github.io/argo-helm |
| Traefik | 28.3.0 | mgmt + runtime | https://traefik.github.io/charts |

## Provider contracts (ISP)

```
CloudProviderFactory
├── IProviderNetwork          VPC / rede
├── IProviderKubernetes       cluster K8s
├── IProviderStorage          object storage (S3-compatible)
├── IProviderDns              registros DNS
├── IProviderCompute          VMs (Droplet / EC2)
└── IProviderDatabase         managed database (PostgreSQL)
```

| Provider | Status |
|----------|--------|
| DigitalOcean | Implementado (6 capabilities) |
| AWS | Stub com guia de implementação |

## Adicionando um novo provider

1. Criar `src/providers/{nome}/` com:
   - `nodes.ts` — `NodeClass → instance type` + `NodeClass → DB size`
   - `network.ts` — `IProviderNetwork`
   - `kubernetes.ts` — `IProviderKubernetes`
   - `storage.ts` — `IProviderStorage`
   - `dns.ts` — `IProviderDns`
   - `compute.ts` — `IProviderCompute`
   - `database.ts` — `IProviderDatabase`
   - `index.ts` — factory

2. Registrar em `src/components/infra.ts` → `resolveProvider()`

3. Adicionar tipo em `src/core/types.ts` → `CloudProvider`

## Decisões de design

**Por que 2 clusters?** Management cluster pode cair sem afetar apps em produção. ArgoCD down = deploys param, mas apps continuam rodando. Blast radius controlado.

**Por que Gitea em VM?** Stateful + critical path. Pod restart em K8s pode corromper dados. VM dedicada é mais simples de operar, fazer backup e restaurar.

**Por que runners em VMs?** Builds consomem CPU/memória de forma imprevisível. Dentro do cluster, um build pesado causa eviction de pods do control plane.

**Por que managed PostgreSQL?** Backups automáticos, failover, patching sem overhead operacional. Estado da plataforma não deveria depender de um StatefulSet.

**Por que ArgoCD só no mgmt?** ArgoCD é Kubernetes-native e gerencia o runtime cluster via kubeconfig. Não faz sentido colocá-lo fora do K8s.

**Portabilidade AWS:** Gitea VM → EC2, Spaces → S3, DOKS → EKS, DB → RDS. O modelo não muda — só as implementações dos providers.

## Dependências

| Pacote | Versão | Propósito |
|--------|--------|-----------|
| `@pulumi/pulumi` | ^3.153.1 | Core SDK |
| `@pulumi/digitalocean` | ^4.40.0 | Provider DigitalOcean |
| `@pulumi/kubernetes` | ^4.18.0 | Provider Kubernetes (Phase 2) |
| `vitest` | ^3.2.4 | Test runner (dev) |

## CI/CD Pipeline

### Workflows

| Workflow | Trigger | Ação |
|----------|---------|------|
| `ci.yml` (test-pulumi) | Push em qualquer branch | Typecheck + testes unitários |
| `infra-preview.yml` | PR para develop/main | `pulumi preview` → comentário no PR |
| `infra-deploy.yml` | Push em develop | `pulumi up` → stack dev (automático) |
| `infra-deploy.yml` | Push em main | `pulumi up` → stack prod (requer aprovação) |
| `infra-deploy.yml` | Manual (workflow_dispatch) | Qualquer comando em qualquer stack |

### Fluxo

```
PR → develop                    Push → develop              Push → main
┌─────────────────┐            ┌─────────────────┐         ┌─────────────────┐
│ typecheck        │            │ typecheck        │         │ typecheck        │
│ vitest           │            │ vitest           │         │ vitest           │
│ pulumi preview   │            │ pulumi up (dev)  │         │ approval gate    │
│ → PR comment     │            │ → execution state│         │ pulumi up (prod) │
└─────────────────┘            └─────────────────┘         └─────────────────┘
```

### Secrets necessários

Configure nos GitHub Environments (`infra-dev`, `infra-prod`):

| Secret | Descrição |
|--------|-----------|
| `DIGITALOCEAN_TOKEN` | Token da API DigitalOcean (full access para deploy) |
| `SPACES_ACCESS_KEY_ID` | Spaces access key (state backend S3) |
| `SPACES_SECRET_ACCESS_KEY` | Spaces secret key (state backend S3) |
| `PULUMI_CONFIG_PASSPHRASE` | Passphrase para encriptação de secrets no state |

### GitHub Environments

| Environment | Protection |
|-------------|-----------|
| `infra-dev` | Nenhuma — deploy automático |
| `infra-staging` | Opcional — deploy manual via workflow_dispatch |
| `infra-prod` | Required reviewers + deployment branch = main |

### Operações manuais

```bash
# Via GitHub Actions UI → workflow_dispatch:
#   Stack: dev | staging | prod
#   Command: up | preview | refresh | destroy
#   Message: razão da operação

# Exemplos:
# - refresh: sincroniza state com recursos reais (drift detection)
# - destroy: teardown completo do ambiente (requer aprovação para prod)
# - preview: dry-run antes de deploy manual
```

### Concurrency

- **Preview (PRs):** cancel-in-progress: true — safe, é read-only
- **Deploy:** cancel-in-progress: false — NUNCA cancelar um `pulumi up` em andamento
