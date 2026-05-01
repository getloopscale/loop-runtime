# Shared Packages PaaS Benchmark

## Objetivo

Este documento analisa `pkg/` a partir da perspectiva de uma PaaS como o LoopZero.

Aqui o benchmark não é uma ferramenta única de mercado, e sim a combinação de uma verdade arquitetural:

- plataformas como o LoopZero possuem múltiplos binaries, múltiplos planos e múltiplos boundaries;
- isso exige uma contract layer compartilhada, pequena e extremamente confiável.

O objetivo é responder:

- que tipo de código deve existir em uma shared layer de PaaS;
- que tipo de código não deve existir;
- como saber se `pkg/` está ajudando ou corroendo a arquitetura.

## O Que Uma PaaS Precisa De Uma Shared Layer

### 1. Invariantes sistêmicos únicos

Em uma PaaS, certos contratos não podem divergir entre componentes:

- naming;
- IDs;
- lifecycle;
- erros tipados;
- health phases;
- policy enforcement.

Por que isso é forte:

- reduz classes inteiras de bugs de integração;
- evita duplicação semântica entre API, operators e serviços auxiliares.

Lição para LoopZero:

- `pkg/` deve continuar sendo o lugar desses invariantes.

### 2. Abstrações pequenas e concretas

A shared layer de uma PaaS boa não é um framework interno gigante.

Por que isso é forte:

- reduz acoplamento;
- facilita adoção gradual;
- evita que cada pacote “puxe” a arquitetura inteira junto.

Lição para LoopZero:

- `circuitbreaker`, `naming`, `id`, `health` e `logging` são bons exemplos de abstrações pequenas e úteis.

### 3. Desacoplamento de stack operacional

Uma PaaS madura precisa compartilhar contexto observável sem obrigar todos os processos a importar exatamente a mesma instrumentação concreta.

Por que isso é forte:

- API e operators podem usar o mesmo contrato de contexto;
- a implementação de tracing/logging pode variar sem quebrar a shared layer.

Lição para LoopZero:

- a estratégia de extractor registrável em `pkg/logging` é arquiteturalmente saudável.

## O Que Uma PaaS Não Deve Colocar Na Shared Layer

### 1. Caso de uso específico de um único módulo

Se só a API usa, provavelmente não pertence a `pkg/`.

Risco:

- shared layer vira depósito de conveniência;
- dependências e semânticas começam a vazar na direção errada.

### 2. Framework interno abstrato demais

Pacotes com nomes e APIs genéricos demais tendem a sobreviver sem ter dono claro.

Risco:

- menor legibilidade;
- maior custo de evolução;
- falsa reutilização.

### 3. Semântica de transporte acoplada demais

Uma PaaS precisa de erros e health compartilhados, mas isso não significa que tudo deva nascer em função de HTTP, CLI ou controller-runtime.

Risco:

- pacotes deixam de ser compartilháveis de verdade;
- contratos ficam presos a um boundary específico.

## Benchmark Aplicado Ao LoopZero

### Adoções corretas já visíveis

#### A. State machine como contrato de plataforma

Isso é exatamente o tipo de shared logic que uma PaaS precisa centralizar.

#### B. Naming centralizado

Em plataforma multi-cluster e multi-resource, nomes inconsistentes são bug de arquitetura. Centralizar isso em `pkg/naming` é a decisão certa.

#### C. Circuit breaker compartilhado

Integrações externas em API e operators compartilham a mesma categoria de risco. Uma primitive comum pequena faz sentido.

#### D. Health aggregation comum

Isso permite que componentes diferentes falem a mesma linguagem de disponibilidade e degradação.

### Riscos que o benchmark destaca

#### A. `pkg/` crescer por conveniência

Esse é o risco estrutural mais comum e mais perigoso.

#### B. Misturar “shared policy” com “policy de um produto específico”

`enforcement` faz sentido hoje porque já cruza domínios relevantes. Mas essa área precisa revisão contínua para não virar extensão do backend de billing.

#### C. Expor contratos compartilhados sem governança forte de compatibilidade

Quanto mais módulos usam `pkg/`, mais cara fica cada mudança.

## Decisão Estratégica

Para o LoopZero, `pkg/` deve continuar sendo:

- pequeno;
- concreto;
- centrado em invariantes;
- testado por contrato;
- desacoplado de consumidores específicos.

Ele não deve virar:

- framework interno;
- bucket de helpers;
- camada de “common code” sem semântica.

## Conclusão Prática

Se precisarmos resumir o benchmark em uma única diretriz:

- tudo que entra em `pkg/` deve reduzir risco sistêmico de divergência;
- nada deve entrar em `pkg/` apenas para economizar código local.

Esse é o uso correto de uma shared layer em uma PaaS como o LoopZero.
