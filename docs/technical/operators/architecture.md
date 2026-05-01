# Operators Architecture

## Escopo

`operators/` materializa o plano de controle declarativo do LoopZero. É a camada que pega CRDs do produto e as transforma em:

- namespaces e isolamento;
- integrações com build, runtime e GitOps;
- bootstrap de plataforma;
- previews efêmeros;
- sinais de status observáveis para API, CLI e operação.

Em uma PaaS como o LoopZero, os operators não são apenas "automação Kubernetes". Eles são a camada que sustenta a abstração do produto.

## Mapa Por Pasta

| Pasta | Responsabilidade principal | Domínio |
|---|---|---|
| `operators/api/v1` | tipos das CRDs e contrato da API declarativa | API declarativa |
| `operators/cmd/tenant-operator` | binário para tenancy/environment, provisioners e multi-cluster clients | tenant/control plane |
| `operators/cmd/app-operator` | binário para aplicações e exposure | app/runtime |
| `operators/cmd/preview-operator` | binário para previews efêmeros | preview |
| `operators/cmd/platform-operator` | binário para bootstrap e CRDs de plataforma | platform/bootstrap |
| `operators/internal/controller` | controllers históricos/monolíticos e lógica central compartilhada | core reconcile |
| `operators/internal/tenant` | controllers e helpers do domínio tenant/environment | tenant/isolation |
| `operators/internal/app` | controllers do domínio application/exposure | app/runtime |
| `operators/internal/preview` | controller e recursos do domínio preview | preview |
| `operators/internal/platform` | controllers do domínio bootstrap/platform infra | platform/bootstrap |
| `operators/internal/conditions` | helpers e convenções de conditions | status/observability |
| `operators/internal/constants` | constantes operacionais, labels, finalizers, timeouts | control-plane invariants |
| `operators/internal/shared` | tracing, pprof, cluster client, utilitários comuns | runtime/shared |
| `operators/internal/argocd` | provisioners para sync/declarative delivery | GitOps/runtime |
| `operators/internal/gitea` | provisioners de repositório | source/gitops |
| `operators/internal/minio` | provisioners de credenciais/storage | build/storage |
| `operators/internal/harbor` | provisioners de registry/projeto/robot accounts | registry/supply chain |
| `operators/internal/metrics` / `tracing` | sinais operacionais e tracing | observability |
| `operators/config/crd` | CRDs geradas e versionadas | API surface |
| `operators/config/rbac` | RBAC do módulo | security |
| `operators/operatortest` | testes sistêmicos e de concorrência | verification |

## Binários E Particionamento Atual

O LoopZero já evoluiu para uma topologia multi-binário:

- `tenant-operator`
- `app-operator`
- `preview-operator`
- `platform-operator`

Esse particionamento é importante para uma PaaS porque separa:

- frequência de reconcile;
- blast radius operacional;
- dependências externas;
- escopo de cache e permissões.

### `tenant-operator`

Domínio:

- `Tenant`
- `Environment`
- integração com Gitea, ArgoCD, Harbor, MinIO
- clientes build/runtime cross-cluster

Características:

- binário mais pesado;
- maior superfície de side effects externos;
- núcleo do isolamento multi-tenant.

### `app-operator`

Domínio:

- `LoopZeroApplication`
- `ExposedRoute`

Características:

- mais próximo do runtime de app;
- menos dependências externas;
- foco em desired state de aplicação e exposure.

### `preview-operator`

Domínio:

- `PreviewEnvironment`

Características:

- efemeridade, TTL, cleanup e garbage collection;
- cliente runtime cross-cluster;
- requeue periódico por natureza do lifecycle.

### `platform-operator`

Domínio:

- `PlatformBootstrap`
- `GitRepositoryBinding`
- `ClusterCredential`

Características:

- baixa frequência e alto impacto;
- bootstrap incremental;
- controle de recursos de plataforma em namespace restrito.

## CRDs Do Produto

Com base em `operators/config/crd/bases/`, os CRDs centrais do módulo hoje são:

- `Tenant`
- `Environment`
- `LoopZeroApplication`
- `ExposedRoute`
- `PlatformBootstrap`
- `GitRepositoryBinding`
- `ClusterCredential`
- `PreviewEnvironment`
- `AppSecret`
- `ReconciliationReport`

Para uma PaaS como o LoopZero, isso importa porque cada CRD representa um boundary de produto, não apenas um objeto técnico.

## Domínios Funcionais

### `tenant-and-isolation`

Responsável por:

- provisionar e manter tenancy;
- criar recursos base por tenant;
- garantir isolamento entre tenants e environments;
- integrar credenciais, quotas, network policy e prioridades.

Arquivos âncora:

- `operators/internal/controller/tenant_controller.go`
- `operators/internal/tenant/*`

### `environment-runtime`

Responsável por:

- reconciliar ambientes derivados do tenant;
- namespace, RBAC, quotas e network policy;
- refletir o lifecycle do environment na malha de execução.

Arquivos âncora:

- `operators/internal/controller/environment_controller.go`
- `operators/internal/tenant/environment_*`

### `application-and-exposure`

Responsável por:

- reconciliar CRDs de aplicação;
- gerar/runtime wiring para exposição;
- manter interface estável entre intenção de app e recursos downstream.

Arquivos âncora:

- `operators/internal/app/application_controller.go`
- `operators/internal/app/exposure_controller.go`

### `preview-lifecycle`

Responsável por:

- namespace efêmero;
- políticas restritivas;
- resource quota e limit range;
- TTL, cleanup e marcação de saúde do preview.

Arquivos âncora:

- `operators/internal/controller/preview_controller.go`
- `operators/internal/preview/*`

### `platform-bootstrap`

Responsável por:

- bootstrap progressivo da plataforma;
- integração com Gitea, ArgoCD e API;
- finalizer e limpeza de recursos próprios.

Arquivos âncora:

- `operators/internal/controller/platformbootstrap_controller.go`
- `operators/internal/platform/*`

### `cross-cluster-provisioning`

Responsável por:

- operar a partir do management cluster sobre build cluster e runtime cluster;
- encapsular falhas e retries de dependências externas;
- manter boundaries claros entre clients e domínios.

Arquivos âncora:

- `operators/cmd/tenant-operator/main.go`
- `operators/cmd/preview-operator/main.go`
- `operators/internal/shared/cluster.go`

### `status-and-observability`

Responsável por:

- conditions consistentes;
- observed generation;
- métricas e tracing;
- healthz/readyz e debug controlado.

Arquivos âncora:

- `operators/internal/conditions/conditions.go`
- `operators/internal/metrics/metrics.go`
- `operators/internal/shared/tracing.go`

## Fronteiras Arquiteturais

### O que pertence aos operators

- convergência declarativa;
- reconciliação de recursos de cluster;
- integração controlada com dependências externas;
- status operacional observável;
- cleanup assíncrono e finalização.

### O que não pertence aos operators

- API síncrona de produto para usuário final;
- validação rica de UX de entrada;
- orquestração imperativa de longa duração que deveria estar na API/workflow;
- linguagem de produto para CLI/UI;
- verdade de billing/account fora do cluster.

## Regra De Ouro

Em uma PaaS como o LoopZero, o operator deve traduzir intenção declarativa de produto em realidade Kubernetes. Ele não deve forçar o produto a falar a linguagem do cluster.
