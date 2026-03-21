# loopzero-infra

Infrastructure-as-Code para a plataforma LoopZero. Provisiona clusters Kubernetes multi-cloud com um pipeline de três fases usando Pulumi.

## Arquitetura

```
Phase 1: Infra              Phase 2: Bootstrap           Phase 3: Expose
┌─────────────────┐        ┌──────────────────┐        ┌──────────────┐
│ VPC / Network   │        │ platform-system  │        │ DNS Records  │
│ K8s Cluster     │──────> │ cert-manager     │──────> │ (stub)       │
│ Object Storage  │        │ Traefik Ingress  │        └──────────────┘
└─────────────────┘        └──────────────────┘
     InfraOutputs      ──>    BootstrapOutputs     ──>   ExposeOutputs
```

Cada fase produz outputs tipados consumidos pela próxima. Dependências são explícitas no grafo Pulumi — não há ordenação implícita.

### Abstração de provider

Contratos provider-agnostic via Interface Segregation:

```
contracts/
  IProviderNetwork        →  VPC / rede
  IProviderKubernetes     →  cluster K8s
  IProviderStorage        →  object storage (S3-compatible)
  IProviderDns            →  registros DNS
  CloudProviderFactory    →  composição das 4 interfaces
```

| Provider | Status |
|----------|--------|
| DigitalOcean | Implementado |
| AWS | Stub com guia de implementação |

**NodeClass** abstrai tamanhos de instância (`small` / `medium` / `large` / `xlarge`). Cada provider resolve para seu SKU específico sem vazar strings de provider na configuração.

## Estrutura

```
src/
├── index.ts                    Orquestrador: 3 fases + execution state
├── config/
│   └── stack.ts                Carrega config do Pulumi (fail-fast)
├── core/
│   ├── types.ts                CloudProvider, Environment, NodeClass
│   ├── naming.ts               Nomes determinísticos de recursos
│   ├── tags.ts                 Tags padrão para todos os recursos
│   ├── errors.ts               InfraError tipado com contexto
│   └── execution.ts            Tracking de estado por fase
├── contracts/
│   ├── provider.ts             Factory + 4 interfaces ISP
│   ├── network.ts              NetworkSpec / NetworkOutput
│   ├── kubernetes.ts           KubernetesClusterSpec / Output
│   ├── storage.ts              ObjectStorageSpec / Output
│   └── dns.ts                  DnsRecordSpec / Output
├── components/
│   ├── infra.ts                Phase 1: infra cloud
│   ├── bootstrap.ts            Phase 2: serviços de plataforma
│   └── expose.ts               Phase 3: DNS (stub)
└── providers/
    ├── digitalocean/            Implementação completa (6 arquivos)
    └── aws/                     Stub com instruções
```

## Ambientes

| Stack | Domain | NodeClass | Nodes | K8s |
|-------|--------|-----------|-------|-----|
| `dev` | dev.loopzero.example | small | 2–3 | 1.30.2-do.0 |
| `staging` | staging.loopzero.example | small | 2–4 | 1.30.2-do.0 |
| `prod` | loopzero.example | medium | 3–6 | 1.30.2-do.0 |

## Uso

```bash
# Instalar dependências
npm install

# Verificar tipos
npm run typecheck

# Deploy
pulumi up -s dev

# Consultar outputs
pulumi stack output --json
pulumi stack output execution --json
pulumi stack output kubeconfig --show-secrets > kubeconfig.yaml
```

## Stack Outputs

### Phase 1 — Infra
| Output | Tipo | Descrição |
|--------|------|-----------|
| `networkId` | `string` | ID da VPC |
| `clusterName` | `string` | Nome do cluster K8s |
| `endpoint` | `string` | API endpoint do cluster |
| `kubeconfig` | `string` (secret) | Kubeconfig do cluster |
| `bucketName` | `string` | Nome do bucket de artefatos |
| `bucketEndpoint` | `string` | Endpoint do bucket |

### Phase 2 — Bootstrap
| Output | Tipo | Descrição |
|--------|------|-----------|
| `ingressReady` | `boolean` | Traefik Helm deployed |
| `ingressEndpoint` | `string` | IP (DO) ou hostname (AWS) do LB |
| `certManagerReady` | `boolean` | cert-manager Helm deployed |
| `platformNamespace` | `string` | `platform-system` |

### Phase 3 — Expose
| Output | Tipo | Descrição |
|--------|------|-----------|
| `dnsConfigured` | `boolean` | Sempre `false` (stub) |

### Execution State
```bash
pulumi stack output execution --json
```
```json
{
  "installationId": "dev-2026-03-21T10-00-00-000Z",
  "stack": "dev",
  "environment": "dev",
  "provider": "digitalocean",
  "declaredAt": "2026-03-21T10:00:00.000Z",
  "phases": [
    { "phase": "infra",     "status": "completed" },
    { "phase": "bootstrap", "status": "completed" },
    { "phase": "expose",    "status": "pending"   }
  ]
}
```

Status derivado de readiness real dos recursos, não de controle de fluxo do programa.

## Helm Charts Pinados

| Chart | Versão | Repo |
|-------|--------|------|
| cert-manager | v1.14.5 | https://charts.jetstack.io |
| Traefik | 28.3.0 | https://traefik.github.io/charts |

Versões fixas para provisionamento determinístico. Atualizar explicitamente — nunca usar floating versions.

## Error Handling

Todos os erros usam `InfraError` tipado:

```typescript
throw new InfraError({
  message: "Node class not found",
  code: "NODE_CLASS_NOT_SUPPORTED",    // código tipado
  context: { provider: "digitalocean", nodeClass: "xlarge" },
  cause: originalError,                // causa raiz preservada
});
```

Códigos disponíveis: `PROVIDER_NOT_IMPLEMENTED` · `INVALID_CONFIG` · `RESOURCE_CREATION_FAILED` · `NODE_CLASS_NOT_SUPPORTED` · `BOOTSTRAP_FAILED`

`InfraError.toStructuredLog()` retorna um objeto plano para logging em CI/pipelines.

## Adicionando um novo provider

1. Criar `src/providers/{nome}/` com:
   - `nodes.ts` — mapa `NodeClass → instance type`
   - `network.ts` — implementa `IProviderNetwork`
   - `kubernetes.ts` — implementa `IProviderKubernetes`
   - `storage.ts` — implementa `IProviderStorage`
   - `dns.ts` — implementa `IProviderDns`
   - `index.ts` — factory que compõe as 5 capabilities

2. Registrar em `src/components/infra.ts`:
   ```typescript
   case "novo-provider":
     return new NovoProviderFactory();
   ```

3. Adicionar tipo em `src/core/types.ts`:
   ```typescript
   export type CloudProvider = "digitalocean" | "aws" | "novo-provider";
   ```

4. Criar stack configs: `Pulumi.{env}.yaml`

## Dependências

| Pacote | Versão | Propósito |
|--------|--------|-----------|
| `@pulumi/pulumi` | ^3.153.1 | Core SDK |
| `@pulumi/digitalocean` | ^4.40.0 | Provider DigitalOcean |
| `@pulumi/kubernetes` | ^4.18.0 | Provider Kubernetes (Phase 2) |
| `typescript` | ^5.6.3 | (dev) |

## Decisões de design

**Por que 3 fases?** Permite falha parcial controlada, retry por fase, e isolamento de debug. Phase 1 pode completar sem que Phase 2 exista.

**Por que ISP nos contratos?** Cada provider pode implementar capabilities incrementalmente. AWS pode começar só com Network + K8s sem precisar de DNS.

**Por que NodeClass abstrato?** Impede que `s-2vcpu-4gb` ou `t3.medium` vazem para configs e contratos. O domínio pede capacidade; o provider resolve SKU.

**Por que Helm Release e não Chart?** `Release` executa `helm install` real, respeita hooks, mantém status server-side. `Chart` renderiza templates localmente sem tracking de release.

**Por que execution state derivado de Outputs?** Em Pulumi, o programa declara um grafo — a criação de recursos acontece depois. Medir "qual linha de código rodou" não reflete estado real. Derivar de `Output<boolean>` garante que o status reflete infraestrutura provisionada.
