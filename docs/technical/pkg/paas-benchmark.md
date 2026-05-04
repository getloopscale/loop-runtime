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

## Benchmark FAANG / OSS Reference (SOTA Research 2026-05-04)

Comparação por dimensão entre `pkg/` do LoopZero e bibliotecas referência. Escala 1-5 (5 = best-in-class observado em código público).

| Dimensão | LoopZero | google/go-cloud | crossplane-runtime | cockroachdb/errors | sony/gobreaker v2 | sigs.k8s.io/cli-utils/kstatus |
|---|---:|---:|---:|---:|---:|---:|
| Module structure (separate go.mod) | 5 | 5 | 5 | 5 | 5 | 5 |
| Single-runtime-dep discipline | 5 | 4 | 4 | 4 | 5 | 5 |
| Test-to-code ratio | 4 | 4 | 5 | 5 | 4 | 5 |
| Error model (typed + Is/As) | 3 | 4 | 4 | 5 | n/a | n/a |
| Network-portable errors | 1 | n/a | n/a | 5 | n/a | n/a |
| State machine clarity | 4 | n/a | n/a | n/a | n/a | n/a |
| Determinism guard (panic on bad transition) | 5 | n/a | n/a | n/a | n/a | n/a |
| State machine visualization | 1 | n/a | n/a | n/a | n/a | n/a |
| Circuit breaker generics | 3 | n/a | n/a | n/a | 5 | n/a |
| Circuit breaker context-aware | 2 | n/a | n/a | n/a | 4 | n/a |
| Conditions API (kstatus standard) | 0 | n/a | 4 | n/a | n/a | 5 |
| observedGeneration discipline | 0 | n/a | 5 | n/a | n/a | 5 |
| JWT validation hardening (RFC 8725bis) | 3 | n/a | n/a | n/a | n/a | n/a |
| Naming determinism + RFC 1123 | 5 | n/a | 4 | n/a | n/a | n/a |
| ID generation (crypto/rand + prefix) | 4 | n/a | n/a | n/a | n/a | n/a |
| ID validators exported | 0 | n/a | n/a | n/a | n/a | n/a |
| Logging trace extraction | 4 | n/a | n/a | n/a | n/a | n/a |
| No global state (Go CDK principle) | 3 | 5 | 5 | 5 | 5 | 5 |
| Enforcement DIP (mockable storage) | 2 | n/a | n/a | n/a | n/a | n/a |
| API surface minimalism | 4 | 5 | 4 | 4 | 5 | 5 |
| Documentation/code alignment | 3 | 5 | 4 | 5 | 5 | 5 |
| Property-based tests | 2 | 3 | 5 | 5 | 3 | 4 |

### Por dimensão competitiva

**Onde LoopZero já é competitivo (4-5):**
- module hygiene, dep discipline (1 runtime dep);
- state machine determinism (panic-on-invalid + TryTransition for external sources);
- naming RFC 1123 com validação determinística;
- API surface mínima alinhada com Effective Go.

**Onde há gap claro contra best-in-class (≤ 3):**
- **Network-portable errors** (1 vs cockroachdb 5) — não esperado para 1.0, mas relevante quando multi-cell errors trafegam entre planos.
- **Conditions API** (0 vs kstatus 5) — gap arquitetural; `pkg/conditions` ainda não existe.
- **observedGeneration discipline** (0 vs kstatus 5) — operators reconciliam sem disciplina kstatus.
- **State machine visualization** (1 vs n/a) — looplab/fsm Visualize() é trivial; falta no LoopZero.
- **Enforcement DIP** (2 vs operadores comparáveis 5) — direto em `*sql.DB`.
- **CB context-aware** (2 vs sony 4) — sem `context.Context` propagation.

### Benchmark vs PaaS Concorrentes

Análise de shared lib pattern em PaaS comparáveis:

| PaaS | Shared lib equivalente | Padrão observado |
|---|---|---|
| **Vercel** | `@vercel/build-utils`, `@vercel/error-utils` | Lib publicada em npm; tipos compartilhados; mantém SemVer estrito. |
| **Fly.io** | `flyutil` (Elixir) + `superfly/macaroon` | Macaroon-based auth lib; minimal API surface; high-test ratio. |
| **Korifi** | `controllers/api/v1alpha1` + shared utils | CF-on-K8s; conditions seguem kstatus polaridade; CRD types separados. |
| **Crossplane** | `crossplane-runtime` | reference: errors, naming, conditions, resource interfaces — **forte modelo a seguir** |
| **Backstage** | `@backstage/core-*` | Plugin architecture; mas não-Go, menos relevante. |
| **Epinio** | `internal/api`, `internal/cli` shared types | Simpler scale; menos shared layer formal. |

**Aprendizado principal:** O modelo LoopZero está mais próximo de **Crossplane** (mesma escala: PaaS com operators, contracts compartilhados, multi-cell). Crossplane-runtime é a referência mais aplicável e onde LoopZero tem mais a evoluir (especially `pkg/conditions`).

### Conclusão competitiva

`pkg/` do LoopZero está em **bom patamar** comparado com libs Go médias do ecossistema. Para alcançar nível **crossplane-runtime / google/go-cloud**, os gaps prioritários são:

1. Eliminar dead code documentado (`pkg/health` sem importadores).
2. Introduzir `pkg/conditions` kstatus-compatible.
3. Hardenizar `pkg/jwt` para RFC 8725bis (2026).
4. Migrar `pkg/circuitbreaker` para generics + context.
5. Desacoplar `pkg/errors.HTTPStatus` do modelo de erro (separar `pkg/errors/http`).

Detalhes em [improvement-roadmap.md](./improvement-roadmap.md).
