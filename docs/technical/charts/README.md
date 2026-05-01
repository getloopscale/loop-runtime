# Charts Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `charts/` e os gates objetivos para considerar os Helm charts publicados do LoopZero em padrão SOTA no contexto de uma PaaS como a nossa.

No LoopZero, `charts/` não é só uma forma de empacotar YAML. Ele é parte do contrato operacional da plataforma porque define:

- como os componentes são instalados e atualizados;
- quais defaults de segurança e operação entram em produção;
- como CRDs, operators, API, cloud e observabilidade são distribuídos;
- como day-2 é entregue via GitOps.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa dos charts, papéis e fronteiras da camada de packaging |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark de charts/packaging no contexto de uma PaaS como o LoopZero |

## Resumo Executivo

No LoopZero, `charts/` só deve ser considerado SOTA quando:

1. Publica componentes com defaults seguros e comportamento previsível.
2. Mantém separação clara entre library chart, charts operacionais, CRDs, umbrella chart e observabilidade/policies.
3. Preserva compatibilidade de values e segurança de upgrade.
4. Expõe labels, probes, recursos, RBAC e métricas de forma consistente.
5. Não empurra lógica de produto para dentro dos templates.
6. Permite governança GitOps e day-2 sem divergência silenciosa com `infra/helm`.
7. Trata CRDs, policies e monitoring como parte do contrato da plataforma.

## Leitura Recomendada

- Para entender a decomposição atual dos charts: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças em chart: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de Helm e Kubernetes aplicada aqui: [research-basis.md](./research-basis.md)
- Para manter a discussão no contexto de uma PaaS como o LoopZero: [paas-benchmark.md](./paas-benchmark.md)
