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
| [architecture.md](./architecture.md) | Mapa dos pacotes, responsabilidades, fronteiras e Real Usage Matrix |
| [sota-gates.md](./sota-gates.md) | 18 gates SOTA com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark de shared libraries (FAANG/OSS) e contratos transversais em uma PaaS |
| [improvement-roadmap.md](./improvement-roadmap.md) | Roadmap de melhorias 2026 — Quick Wins, Sprint 9, Post-Launch (atual: 7.0/10 → alvo 9.0/10) |

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
- Para o plano concreto de melhorias 2026 com prioridades e referências: [improvement-roadmap.md](./improvement-roadmap.md)

## Status Atual (SOTA Research 2026-05-04)

**Score:** 7.0/10 → alvo 9.0/10 (após Sprint 9 + post-launch).

**Gaps prioritários** (detalhes em improvement-roadmap.md):

1. **`pkg/health` é dead code** — 0 imports em produção apesar de ser flagship na doc.
2. **`pkg/jwt` pré-RFC 8725bis** — sem `WithValidMethods` whitelist, sem `typ` validation.
3. **`pkg/circuitbreaker` v1-style** — sem generics, sem `context.Context`.
4. **`LoopZeroError.HTTPStatus`** acopla modelo a transporte (viola PKG-G08/G15).
5. **`pkg/enforcement`** depende direto de `*sql.DB` (viola DIP).
6. **`pkg/logging`** mantém global mutable state (viola Go CDK design).
7. **Falta `pkg/conditions`** kstatus-compatible para operators.

**Pontos fortes a preservar:**

- module separado (`pkg/go.mod`) com 1 única dep runtime;
- test-to-code 1.82:1 agregado (e 3.7:1 em `enforcement`, 3:1 em `jwt`);
- state machine pattern com determinismo via panic-on-invalid + TryTransition;
- naming centralizado e RFC 1123 compliant.
