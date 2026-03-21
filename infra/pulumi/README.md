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
┌────────────────────┐        ┌───────────────────────┐       ┌──────────────────┐
│ VPC                │        │ Mgmt: cert-manager    │       │ DNS records      │
│ Mgmt DOKS          │        │ Mgmt: ArgoCD          │       │ (stub)           │
│ Runtime DOKS       │──────> │ Mgmt: Traefik         │─────> │                  │
│ Gitea VM           │        │ Runtime: Traefik      │       │                  │
│ Runner VMs         │        └───────────────────────┘       └──────────────────┘
│ PostgreSQL         │
│ Spaces             │
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
│   └── expose.ts               Phase 3: DNS (stub)
└── providers/
    ├── digitalocean/            Implementação completa (8 arquivos)
    │   ├── index.ts             Factory
    │   ├── nodes.ts             NodeClass → droplet/DB sizes
    │   ├── network.ts           VPC
    │   ├── kubernetes.ts        DOKS cluster
    │   ├── storage.ts           Spaces bucket
    │   ├── dns.ts               DNS records
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

## Uso

```bash
npm install
npm run typecheck

# Deploy
pulumi up -s dev

# Outputs
pulumi stack output --json
pulumi stack output execution --json
pulumi stack output wiring --json
pulumi stack output mgmtKubeconfig --show-secrets > mgmt.kubeconfig
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
