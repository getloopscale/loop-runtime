# Shared Packages Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `pkg/` e os gates objetivos para considerar as bibliotecas compartilhadas do LoopZero em padrão SOTA no contexto de uma PaaS como a nossa.

No LoopZero, `pkg/` não é um diretório utilitário genérico. Ele funciona como camada de contratos compartilhados entre:

- API;
- operators;
- cloud services;
- infra e tooling.

Isso significa que um erro de desenho em `pkg/` se replica pelo sistema inteiro.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa dos pacotes, responsabilidades e fronteiras da shared layer |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark de shared libraries e contratos transversais em uma PaaS |

## Resumo Executivo

No LoopZero, `pkg/` só deve ser considerado SOTA quando:

1. Expõe contratos pequenos, estáveis e realmente transversais.
2. Centraliza invariantes sistêmicos que não podem divergir entre componentes.
3. Evita depender de frameworks, runtimes ou detalhes de um consumidor específico.
4. É testado por contrato, não apenas por implementação incidental.
5. Trata naming, IDs, state machines, errors e health como ativos de plataforma.
6. Mantém observabilidade e resiliência desacopladas da stack de cada processo.
7. Não vira um “miscellaneous bucket” de helpers sem dono.

## Leitura Recomendada

- Para entender a decomposição atual dos shared packages: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças em `pkg/`: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de Go e dos padrões usados aqui: [research-basis.md](./research-basis.md)
- Para manter a discussão no contexto de uma PaaS como o LoopZero: [paas-benchmark.md](./paas-benchmark.md)
