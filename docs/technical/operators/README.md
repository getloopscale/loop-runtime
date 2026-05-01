# Operators Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `operators/` e os gates objetivos para considerar os operators do LoopZero em padrão SOTA no contexto de uma PaaS Kubernetes como a nossa.

O foco aqui não é apenas "escrever bons controllers". O foco é garantir que os operators sustentem a promessa central do LoopZero:

- esconder a complexidade operacional do Kubernetes;
- manter isolamento entre tenants e ambientes;
- materializar deploys, previews e bootstrap de forma declarativa;
- sobreviver a retry, drift, falhas parciais e indisponibilidade de dependências.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa por pastas, binários, domínios e fronteiras dos operators |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark de operadores em PaaS usando Korifi e Epinio como referência |

## Resumo Executivo

No LoopZero, os operators só devem ser considerados SOTA quando:

1. Reconciliam de forma idempotente e crash-safe.
2. Traduzem intenção de produto em recursos Kubernetes sem expor a infraestrutura ao usuário final.
3. Modelam estado com conditions e `observedGeneration`, não com flags soltas.
4. Tratam finalizers e cleanup como contrato operacional explícito.
5. Preservam isolamento entre tenant, environment, preview e platform system.
6. Continuam úteis em uma arquitetura PaaS multi-cluster e multi-cell.
7. São verificáveis por envtest, integração e sinais operacionais.

## Leitura Recomendada

- Para entender a decomposição atual do módulo: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de princípios de operator/controller: [research-basis.md](./research-basis.md)
- Para manter o documento no contexto de uma PaaS como a nossa: [paas-benchmark.md](./paas-benchmark.md)
