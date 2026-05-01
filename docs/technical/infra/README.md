# Infra Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `infra/` e os gates objetivos para considerar a infraestrutura do LoopZero em padrão SOTA no contexto de uma PaaS multi-plano como a nossa.

Aqui, "infra" não significa apenas provisionar recursos. No LoopZero, `infra/` precisa sustentar simultaneamente:

- criação e evolução da fundação cloud;
- bootstrap controlado do management, build e runtime planes;
- paridade operacional entre local, CI e produção;
- validação, rollback, observabilidade e recuperação.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa por pastas, camadas e fronteiras da infraestrutura |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark de infraestrutura de PaaS e GitOps no contexto do LoopZero |

## Resumo Executivo

No LoopZero, a infra só deve ser considerada SOTA quando:

1. É declarativa, reproduzível e promovível por camadas.
2. Mantém separação clara entre L1 cloud, L2 bootstrap e L3 day-2 GitOps.
3. Garante paridade conceitual entre local, CI e produção.
4. Não depende de passos manuais invisíveis para convergir.
5. É validável por schema, smoke, parity, chaos e observabilidade.
6. Tolera drift, rollback e recuperação de estado sem improviso.
7. Sustenta o modelo multi-cluster e multi-cell da plataforma.

## Leitura Recomendada

- Para entender a decomposição atual da pasta: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças de infra: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de IaC, GitOps e operação em Kubernetes: [research-basis.md](./research-basis.md)
- Para manter a discussão no contexto de uma PaaS como o LoopZero: [paas-benchmark.md](./paas-benchmark.md)
