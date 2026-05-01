# Charts Architecture

## Escopo

`charts/` contém os charts Helm publicados do LoopZero.

Em uma PaaS como a nossa, esses charts são a camada de empacotamento operacional do day-2. Eles materializam a plataforma em forma distribuível, versionável e instalável por ArgoCD, CI e operadores da plataforma.

## Mapa Por Chart

| Chart | Responsabilidade principal | Domínio |
|---|---|---|
| `loopzero-common` | library chart com helpers, labels, security contexts e padrões | shared chart primitives |
| `loopzero-api` | empacotamento da API | control plane |
| `loopzero-cloud` | empacotamento do cloud service | cloud/product |
| `loopzero-dashboard` | empacotamento do dashboard | product UI |
| `loopzero-tenant-operator` | operator de tenant/environment | operators |
| `loopzero-app-operator` | operator de app/exposure | operators |
| `loopzero-preview-operator` | operator de preview | operators |
| `loopzero-platform-operator` | operator de bootstrap/platform resources | operators |
| `loopzero-controller` / `loopzero-tenant-controller` | empacotamentos legados/compatibilidade | compatibility |
| `loopzero-crds` | CRDs publicadas e lifecycle independente | API declarativa |
| `loopzero-platform` | umbrella chart que compõe a plataforma | platform composition |
| `loopzero-build-cwts` | cluster workflow templates e recursos de build | build plane |
| `loopzero-monitoring` | dashboards, alert rules e integração observável | observability |
| `loopzero-admission-policies` | ValidatingAdmissionPolicies e guardrails declarativos | policy/security |

## Tipos De Chart Na Pasta

### `library chart`

O `loopzero-common` centraliza:

- naming de release;
- labels e selector labels;
- image reference building;
- security contexts;
- service account defaults;
- affinity/topology spread;
- startup probe.

Esse chart existe para reduzir divergência de padrões operacionais entre componentes.

### `application charts`

São os charts que empacotam componentes executáveis da plataforma:

- `loopzero-api`
- `loopzero-cloud`
- `loopzero-dashboard`
- charts de operator

Eles precisam traduzir cada componente em workload Kubernetes instalável, seguro e observável.

### `CRD chart`

`loopzero-crds` separa o lifecycle das CRDs do lifecycle dos controladores e workloads.

Isso é arquiteturalmente importante porque:

- CRD upgrade nem sempre deve seguir o mesmo ritmo do controller;
- instalação e remoção de CRDs têm riscos próprios;
- GitOps e bootstrap precisam poder orquestrar esse ciclo explicitamente.

### `umbrella chart`

`loopzero-platform` compõe dependências da plataforma e funciona como agregador de instalação.

Ele expressa:

- composição de subcharts;
- gates de enablement por componente;
- profiles/values por ambiente.

### `policy and monitoring charts`

Charts como `loopzero-admission-policies` e `loopzero-monitoring` fazem parte da plataforma, mas não são “apps” tradicionais.

Eles existem para empacotar:

- políticas de admissão;
- dashboards;
- alert rules;
- sinais operacionais do sistema.

## Domínios Funcionais

### `shared-operational-primitives`

Responsável por:

- labels consistentes;
- contexts de segurança;
- naming previsível;
- helpers para deployment templates.

Arquivo âncora:

- `charts/loopzero-common/templates/_helpers.tpl`

### `component-packaging`

Responsável por:

- transformar API, cloud, dashboard e operators em deployments instaláveis;
- expor services, serviceaccounts, RBAC, probes, resources e monitoramento.

Arquivos âncora:

- `charts/loopzero-api/templates/*`
- `charts/loopzero-cloud/templates/*`
- `charts/loopzero-*-operator/templates/*`

### `platform-composition`

Responsável por:

- compor subcharts em uma visão de plataforma;
- ligar enablement, dependências e profiles.

Arquivos âncora:

- `charts/loopzero-platform/Chart.yaml`
- `charts/loopzero-platform/profiles/*`

### `api-declarative-surface`

Responsável por:

- publicar CRDs como artefatos instaláveis;
- separar lifecycle de tipos declarativos e controladores.

Arquivos âncora:

- `charts/loopzero-crds/templates/*`

### `security-guardrails`

Responsável por:

- aplicar políticas declarativas que protegem a plataforma por default.

Arquivos âncora:

- `charts/loopzero-admission-policies/templates/*`

### `observability-packaging`

Responsável por:

- dashboards e alertas como parte da release da plataforma;
- integração observável consistente entre ambientes.

Arquivos âncora:

- `charts/loopzero-monitoring/templates/*`
- `charts/loopzero-monitoring/dashboards/*`

## Fronteiras Arquiteturais

### O que pertence aos charts

- empacotamento operacional;
- defaults de deploy e runtime;
- RBAC, ServiceMonitor, policy manifests;
- composição de plataforma via dependências Helm.

### O que não pertence aos charts

- regra de negócio da API;
- lógica de lifecycle do operator;
- semântica de produto do CLI;
- automação de bootstrap L2 fora do escopo do artefato publicado.

## Relação Com `infra/helm`

No LoopZero coexistem duas famílias de chart:

- `charts/`: publicadas, versionáveis, voltadas a day-2 e ArgoCD;
- `infra/helm/`: bootstrap charts leves, focados em L2.

Essa duplicidade não é acidental. É uma escolha de arquitetura.

Implicação:

- mudanças em helpers, defaults e superfícies compartilhadas podem exigir propagação deliberada para ambos os sistemas;
- divergência entre eles precisa ser explícita, nunca silenciosa.

## Regra De Ouro

Um chart do LoopZero deve empacotar um componente ou contrato operacional da plataforma. Ele não deve compensar defeitos de design do componente nem esconder complexidade perigosa nos templates.
