# API Architecture

## Escopo

`api/` é o núcleo do plano de controle síncrono do LoopZero.

Em uma PaaS como a nossa, a API precisa fazer duas coisas ao mesmo tempo:

- dar ao usuário uma experiência simples e orientada a produto;
- coordenar uma cadeia operacional complexa envolvendo banco, locks, workflows, GitOps, cells, clusters e edge.

## Mapa Por Pasta

| Pasta | Responsabilidade principal | Domínio |
|---|---|---|
| `api/cmd` | bootstrap do processo, wiring de dependências e loops | process/runtime |
| `api/internal/server` | configuração do servidor e router | transport/runtime |
| `api/internal/routes` | handlers HTTP e contrato externo | HTTP surface |
| `api/internal/middleware` | auth, idempotência, correlation, tracing, manutenção, rate limit | transport policy |
| `api/internal/services` | orquestração de casos de uso | application orchestration |
| `api/internal/db` | stores, queries, migrações e adaptação pgx | persistence |
| `api/internal/lock` | serialização por environment com advisory locks | concurrency control |
| `api/internal/domain` | tipos e validações centrais | domain model |
| `api/internal/admission` | gates de admissão e backpressure | admission/control |
| `api/internal/argo` | geração/submissão de workflows e supply chain | build plane |
| `api/internal/argocd` | sync e tokens para ArgoCD | GitOps/runtime |
| `api/internal/gitops` | push e gating de manifests | release delivery |
| `api/internal/manifests` | renderização de manifests de workload | runtime desired state |
| `api/internal/source` / `archive` / `s3` | bundle ingestion e storage | source ingestion |
| `api/internal/github` | GitHub app, webhook, autodeploy, preview hooks | source automation |
| `api/internal/cfzone` / `cfkv` | custom domains, DNS e edge routing | edge |
| `api/internal/k8s` | managers de CRs e bridges com operators | K8s integration |
| `api/internal/placement` / `cellmgr` | roteamento de cells e clientes multi-cluster | multi-cell |
| `api/internal/metrics` / `health` / `correlation` | sinais operacionais | observability |
| `api/tests` / `api/apitest` | testes unitários, integração e servidor de apoio | verification |

## Domínios Funcionais

### `transport-and-contracts`

Responsável por:

- surface HTTP;
- middleware chain;
- contratos JSON;
- semântica de status code;
- proteção de endpoints.

Arquivos âncora:

- `api/internal/server/router.go`
- `api/internal/routes/*`
- `api/internal/middleware/*`

### `orchestration`

Responsável por:

- transformar intenção do usuário em ações coordenadas;
- persistir intenção mínima;
- disparar workflows, release activation, sync e callbacks.

Arquivos âncora:

- `api/internal/services/build_orchestrator*.go`
- `api/internal/services/delivery*.go`
- `api/internal/services/*`

### `persistence-and-state`

Responsável por:

- stores transacionais;
- migrações forward-only;
- idempotency storage;
- leitura e escrita consistente do estado operacional.

Arquivos âncora:

- `api/internal/db/*`

### `concurrency-and-admission`

Responsável por:

- lock por `(tenant, project, environment)`;
- backpressure em pool e build admission;
- proteção contra operações concorrentes inconsistentes.

Arquivos âncora:

- `api/internal/lock/environment_lock.go`
- `api/internal/middleware/idempotency.go`
- `api/internal/admission/*`

### `build-and-release`

Responsável por:

- planejar build;
- submeter workflows;
- acompanhar callback/reconcile;
- ativar release e sincronizar desired state.

Arquivos âncora:

- `api/internal/argo/*`
- `api/internal/gitops/*`
- `api/internal/argocd/*`
- `api/internal/manifests/*`

### `edge-and-domains`

Responsável por:

- domínios customizados;
- DNS and zone management;
- roteamento de static delivery.

Arquivos âncora:

- `api/internal/cfzone/*`
- `api/internal/cfkv/*`
- `api/internal/routes/domains*.go`

### `multi-cell-and-placement`

Responsável por:

- roteamento e descoberta de cells;
- clientes por cell;
- expansão além do single-cell mental model.

Arquivos âncora:

- `api/internal/placement/*`
- `api/internal/cellmgr/*`

## Fronteiras Arquiteturais

### O que pertence à API

- contratos síncronos de produto;
- validação e autenticação de request;
- persistência mínima de intenção;
- orquestração entre subsistemas;
- leitura e agregação do estado operacional;
- proteção de concorrência no boundary de mutação.

### O que não pertence à API

- lógica de renderização de CLI;
- reconciliação declarativa contínua dos resources do cluster;
- packaging de workloads;
- bootstrap de fundação cloud;
- operação manual de componentes externos.

## Regra De Ouro

A API do LoopZero pode orquestrar complexidade. Ela não pode exigir que o usuário pense como ArgoCD, PostgreSQL advisory locks ou multi-cell routing.
