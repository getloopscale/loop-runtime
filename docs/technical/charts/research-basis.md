# Charts Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA da pasta `charts/`.

Ele combina:

- documentação oficial do Helm e de recursos Kubernetes relevantes;
- evidências do código atual dos charts do LoopZero;
- leitura da camada `charts/` como contrato de packaging de uma PaaS.

## Fontes Externas Principais

### 1. Helm Best Practices

Fonte:

- https://helm.sh/docs/chart_best_practices/

Pontos relevantes:

- charts devem ter estrutura clara;
- templates, values, labels, dependencies e RBAC precisam seguir convenções previsíveis;
- charts são contratos operacionais, não só atalhos de template.

Gates fundamentados:

- `CH-G01`
- `CH-G03`
- `CH-G05`
- `CH-G12`
- `CH-G13`

### 2. Helm Values Best Practices

Fonte:

- https://docs.helm.sh/docs/chart_best_practices/values/

Pontos relevantes:

- `values.yaml` é API do chart;
- nomes e estrutura de values influenciam usabilidade e estabilidade;
- flat vs nested deve ser escolha deliberada.

Gates fundamentados:

- `CH-G03`
- `CH-G14`

### 3. Helm Library Charts

Fonte:

- https://v3-1-0.helm.sh/docs/topics/library_charts/

Pontos relevantes:

- library charts formalizam compartilhamento de helpers e padrões;
- devem fornecer primitives reutilizáveis, não virar aplicação.

Gates fundamentados:

- `CH-G07`

### 4. Helm Chart Structure

Fonte:

- https://helm.sh/docs/topics/charts/

Pontos relevantes:

- estrutura de chart, templates e dependências segue contrato conhecido;
- charts são renderizados a partir de templates + values override.

Gates fundamentados:

- `CH-G01`
- `CH-G10`
- `CH-G12`

### 5. Kubernetes Validating Admission Policy

Fontes:

- https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/
- https://kubernetes.io/docs/concepts/policy

Pontos relevantes:

- policies declarativas em CEL são artefatos nativos e versionáveis;
- failure policy e parameterization importam para segurança operacional.

Gates fundamentados:

- `CH-G09`

### 6. Prometheus Operator / ServiceMonitor

Fonte:

- https://prometheus-operator.dev/docs/developer/getting-started/

Pontos relevantes:

- `ServiceMonitor` é parte do contrato declarativo de observabilidade para workloads monitorados;
- packaging observável precisa descrever targets e rules explicitamente.

Gates fundamentados:

- `CH-G08`

## Evidências Internas Do LoopZero

### Library chart compartilhado

Arquivos:

- `charts/loopzero-common/templates/_helpers.tpl`

O que sustentam:

- labels consistentes;
- image helper;
- security contexts endurecidos;
- service account defaults;
- affinity/topology spread;
- startup probe helper.

### Component charts operacionais

Arquivos:

- `charts/loopzero-api/templates/*`
- `charts/loopzero-cloud/templates/*`
- `charts/loopzero-*-operator/templates/*`

O que sustentam:

- packaging consistente de deployment, RBAC, service, monitoramento e network policy;
- uso de library helpers.

### CRD separation

Arquivos:

- `charts/loopzero-crds/Chart.yaml`
- `charts/loopzero-crds/templates/*`

O que sustentam:

- lifecycle independente das CRDs;
- separação explícita entre API declarativa e workloads.

### Umbrella composition

Arquivos:

- `charts/loopzero-platform/Chart.yaml`
- `charts/loopzero-platform/profiles/*`

O que sustentam:

- composição explícita de subcharts;
- conditions por dependência;
- profiles por ambiente.

### Policy packaging

Arquivos:

- `charts/loopzero-admission-policies/templates/*`

O que sustentam:

- postura de segurança da plataforma publicada como chart;
- uso de ValidatingAdmissionPolicy para guardrails declarativos.

### Monitoring packaging

Arquivos:

- `charts/loopzero-monitoring/templates/*`
- `charts/loopzero-monitoring/dashboards/*`

O que sustentam:

- observabilidade como artefato publicável;
- alertas e dashboards empacotados junto da plataforma.

### Compatibilidade/legado

Arquivos:

- `charts/loopzero-controller/*`
- `charts/loopzero-tenant-controller/*`

O que sustentam:

- existência de camada de compatibilidade durante transição arquitetural;
- necessidade de governança explícita em coexistência de charts antigos e novos.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com o desenho recomendado oficialmente para charts Helm.
2. São necessários para transformar charts em contrato operacional confiável de uma PaaS.
3. Já encontram apoio forte na estrutura e nos padrões presentes em `charts/` hoje.
