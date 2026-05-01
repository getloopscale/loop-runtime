# Infra Architecture

## Escopo

`infra/` é a camada que cria, bootstrapa, valida e opera a fundação do LoopZero.

Em uma PaaS como a nossa, a infraestrutura não é um detalhe periférico. Ela é parte do produto porque determina:

- a topologia real dos planos do sistema;
- a forma de entrega declarativa;
- o grau de paridade entre dev e produção;
- a capacidade de operar upgrades, incidentes e expansão multi-cell.

## Mapa Por Pasta

| Pasta | Responsabilidade principal | Domínio |
|---|---|---|
| `infra/pulumi*` | provisionamento cloud e contratos entre stacks | L1 cloud foundation |
| `infra/helmfile` | orquestração declarativa do bootstrap base | L2 bootstrap orchestration |
| `infra/helm` | charts de bootstrap leves e idempotentes | L2 bootstrap packages |
| `infra/argocd-bootstrap` | App-of-Apps e entrega declarativa day-2 | L3 GitOps |
| `infra/manifests` | manifests complementares e overlays de operação | operational manifests |
| `infra/k3d` | topologia local de clusters | local parity |
| `infra/tilt` | dev loop e feedback local | developer loop |
| `infra/images` | imagens utilitárias do plano de build e supply chain | build/supply chain |
| `infra/scripts` | automação operacional, bootstrap, repair e migração | ops automation |
| `infra/kubeconform-schemas` | schemas para validação estrita | conformance/validation |
| `infra/tests` | e2e, chaos, load, parity, capacity, dogfood | verification |
| `infra/sloth` | SLOs e geração de regras | reliability |
| `infra/cloudflare` | edge worker e roteamento estático | edge/static delivery |

## Modelo Em Camadas

O LoopZero adota uma arquitetura em três camadas:

### `L1`: Cloud Foundation

Objetivo:

- provisionar recursos base de cloud e edge;
- criar os contratos mínimos que permitem bootstrapar clusters e serviços;
- manter estado separado por stack e ambiente.

Pastas principais:

- `infra/pulumi`
- `infra/pulumi-infra`
- `infra/pulumi-control-plane`
- `infra/pulumi-platform`
- `infra/pulumi-shared`

Características:

- infraestrutura declarada em código;
- referência cruzada entre stacks;
- fronteira explícita entre infraestrutura base e plataforma.

### `L2`: Cluster Bootstrap

Objetivo:

- instalar componentes fundacionais em cada cluster;
- criar wiring mínimo para o plano de controle operar;
- estabelecer rede, secrets, observabilidade e dependências base.

Pastas principais:

- `infra/helmfile`
- `infra/helm`
- `infra/manifests`

Características:

- idempotência operacional;
- charts leves e focados em bootstrap;
- scripts de wiring e secret generation.

### `L3`: Day-2 GitOps

Objetivo:

- entregar a plataforma continuamente via ArgoCD;
- instalar e atualizar aplicações e control planes declarativamente;
- manter o cluster convergindo via GitOps após o bootstrap inicial.

Pastas principais:

- `infra/argocd-bootstrap`

Características:

- App-of-Apps;
- sincronização por ondas;
- raiz declarativa para componentes de plataforma.

## Planos Da Plataforma E Infra Correspondente

### `management plane`

Responsável por:

- API;
- cloud service;
- ArgoCD, Gitea, Harbor, MinIO, observabilidade, Ory;
- operators de management.

Infra associada:

- `infra/argocd-bootstrap/apps/mgmt-*`
- `infra/helm/*` de serviços de gestão
- `infra/pulumi-control-plane/*`

### `build plane`

Responsável por:

- Argo Workflows;
- BuildKit/packs/cosign/syft;
- logging e artefatos do plano de build.

Infra associada:

- `infra/argocd-bootstrap/apps/build-*`
- `infra/images/*`
- `infra/helm/argo-workflows`

### `runtime plane`

Responsável por:

- tráfego de apps;
- cert-manager;
- KEDA;
- external-secrets;
- policies de admissão;
- promtail/runtime metrics.

Infra associada:

- `infra/argocd-bootstrap/apps/runtime-*`
- `infra/helm/keda*`
- `infra/helm/policy-controller`
- `infra/helm/admission-policies`

## Topologias Reais

### Local

O ambiente local representa a plataforma com três clusters k3d:

- `infra/k3d/mgmt-cluster.yaml`
- `infra/k3d/build-cluster.yaml`
- `infra/k3d/workload-cluster.yaml`

Objetivo:

- preservar semântica multi-plano;
- validar wiring entre clusters;
- permitir parity testing sem reduzir tudo a um único cluster.

### Produção

No estado atual do projeto, a produção é composta por múltiplos clusters e já aponta para operação multi-cell/multi-stack.

Implicação arquitetural:

- a infra precisa descrever não só criação inicial, mas também evolução coordenada entre ambientes e cells.

## Domínios Funcionais

### `layered-bootstrap`

- separa responsabilidades entre fundação cloud, bootstrap de cluster e entrega GitOps;
- evita acoplamento circular entre IaC, Helm e ArgoCD.

### `parity-and-validation`

- garante que local, CI e produção usem o mesmo modelo conceitual;
- valida manifests, wiring e capacidade mínima.

### `operability-and-repair`

- inclui runbooks, scripts de state repair, incident response e rotação de segredos;
- trata recuperação como parte do design.

### `supply-chain-and-security`

- inclui imagens utilitárias, políticas, admissão e artefatos de segurança;
- faz da cadeia de build/deploy um domínio explícito da infra.

### `edge-and-routing`

- cobre Cloudflare worker, túnel, DNS/TLS e roteamento de assets/static delivery;
- liga plataforma ao edge sem expor complexidade ao produto.

## Fronteiras Arquiteturais

### O que pertence à infra

- fundação cloud e cluster;
- bootstrap e wiring;
- políticas base de segurança e observabilidade;
- ferramentas de validação, repair e rollout;
- contratos entre stacks e clusters.

### O que não pertence à infra

- lógica de produto síncrona da API;
- semântica de CLI;
- reconciliação declarativa dos CRDs de produto;
- regras de billing e account logic.

## Regra De Ouro

Em uma PaaS como o LoopZero, a infra deve ser declarativa o suficiente para ser reproduzida, e operacional o suficiente para ser consertada sem improviso.
