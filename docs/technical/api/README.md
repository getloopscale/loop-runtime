# API Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `api/` e os gates objetivos para considerar a API do LoopZero em padrão SOTA no contexto de uma PaaS como a nossa.

No LoopZero, `api/` não é apenas um backend HTTP. Ela é o núcleo síncrono do control plane e a ponte entre:

- CLI e dashboard;
- banco, locks e stores;
- workflows de build;
- GitOps e runtime;
- operators, placement e edge integrations.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa da arquitetura da API, domínios e fronteiras |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark da API no contexto de uma PaaS como o LoopZero |

## Resumo Executivo

No LoopZero, `api/` só deve ser considerado SOTA quando:

1. Mantém contratos HTTP estáveis e orientados a produto.
2. Separa claramente transporte, orquestração, persistência e integrações externas.
3. Garante idempotência, concorrência segura e retry semantics previsíveis.
4. Persiste intenção mínima antes de disparar efeitos externos.
5. Tolera falhas parciais entre banco, workflows, GitOps e runtime.
6. Propaga correlation e tracing de ponta a ponta.
7. Sustenta o modelo multi-cluster e multi-cell da plataforma sem vazar complexidade para o usuário.

## Leitura Recomendada

- Para entender a decomposição atual da pasta: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças na API: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de HTTP, locking, tracing e problem details: [research-basis.md](./research-basis.md)
- Para manter a discussão no contexto de uma PaaS como o LoopZero: [paas-benchmark.md](./paas-benchmark.md)
