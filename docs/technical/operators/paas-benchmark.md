# Operators PaaS Benchmark

## Objetivo

Este documento analisa referências de operadores em PaaS para extrair padrões úteis ao LoopZero.

As referências principais aqui são:

- `referencias/korifi/docs/architecture.md`
- `referencias/epinio/README.md`

O objetivo não é copiar Korifi ou Epinio. O objetivo é responder:

- o que operators de PaaS maduros fazem bem;
- o que isso significa para uma PaaS monorepo/multi-plane como o LoopZero;
- o que devemos adotar, adaptar ou rejeitar.

## O Que Korifi Ensina Bem

### 1. A verdadeira API da plataforma pode ser declarativa

Korifi deixa claro que seus CRDs são a API real do sistema, mesmo quando existe uma API compatível com outro ecossistema acima deles.

Por que isso é forte:

- reforça CRD como boundary de arquitetura;
- faz controllers trabalharem sobre conceitos de produto;
- permite integração por ferramentas nativas de Kubernetes sem reimplementar tudo.

Lição para LoopZero:

- nossos CRDs precisam ser tratados como contrato interno de plataforma, não como structs incidentais;
- `Tenant`, `Environment`, `PreviewEnvironment`, `PlatformBootstrap` e `LoopZeroApplication` precisam continuar representando conceitos de produto.

### 2. Controllers devem ser tradutores, não a experiência final do usuário

Korifi usa controllers para transformar recursos de alto nível em recursos de execução de nível mais baixo.

Por que isso é forte:

- separa intenção declarativa de implementação concreta;
- permite trocar subsistemas abaixo do boundary do CRD.

Lição para LoopZero:

- operators do LoopZero devem continuar traduzindo produto em Kubernetes, não ensinando Kubernetes ao usuário;
- detalhes como ArgoCD, Harbor, namespaces ou policies devem ficar encapsulados.

### 3. Extensibilidade por recursos intermediários é valiosa

Korifi usa recursos como `BuildWorkload`, `AppWorkload` e `TaskWorkload` para desacoplar subsistemas.

Por que isso é forte:

- reduz acoplamento com um único runtime ou builder;
- deixa claro onde termina o contrato do core e começa a implementação substituível.

Lição para LoopZero:

- onde houver pressão futura por múltiplos runtimes, builders ou cells, vale pensar em boundaries declarativos equivalentes;
- isso é especialmente relevante para build/runtime integration e preview orchestration.

## O Que Epinio Ensina Bem

### 1. PaaS precisa abstrair Kubernetes de verdade

Epinio se posiciona explicitamente como camada para levar de code to URL sem exigir conhecimento de Kubernetes.

Por que isso é forte:

- deixa claro o critério de produto;
- evita o erro de expor recursos de cluster como modelo mental principal.

Lição para LoopZero:

- os operators devem sustentar essa promessa;
- qualquer CRD ou reconcile que exija raciocínio de workload raw demais está puxando o produto para baixo.

### 2. Instalação e operação precisam ser coerentes entre dev e produção

Epinio enfatiza deploy em desenvolvimento e produção com o mesmo modelo operacional.

Por que isso é forte:

- uma PaaS quebra rápido quando local e produção têm semânticas diferentes;
- operators precisam refletir isso em runtime, bootstrap e test strategy.

Lição para LoopZero:

- preview, tenant isolation, bootstrap e environment lifecycle precisam manter paridade conceitual entre k3d e produção.

### 3. Configurabilidade deve seguir a forma nativa do Kubernetes

O trecho de breaking changes do Epinio mostra uma evolução de env vars para configuração mais estruturada e nativa do Kubernetes.

Por que isso é forte:

- reduz debt operacional;
- melhora expressividade para afinidade, toleration, resources e storage.

Lição para LoopZero:

- nossos operators devem preferir config declarativa estruturada a acúmulo de flags/env vars implícitas onde o domínio já amadureceu.

## O Que Não Devemos Copiar Cegamente

### 1. Tornar CRD sinônimo de interface do usuário

Korifi explicitamente admite usuários falando com CRDs por `kubectl`.

Isso pode ser ótimo para eles, mas no LoopZero precisa de cautela.

Risco para LoopZero:

- deslocar o centro da experiência do produto da CLI/API para o cluster;
- aumentar acoplamento operacional do usuário final com conceitos internos.

Direção correta:

- CRDs do LoopZero são API de plataforma e control plane;
- CLI e API continuam sendo a interface primária de produto.

### 2. Generalizar extensibilidade cedo demais

Korifi investe bastante em pontos de extensão. Isso é poderoso, mas tem custo.

Risco para LoopZero:

- criar abstrações antes de termos variação real;
- inflar a superfície do control plane.

Direção correta:

- usar extensibilidade onde a pressão arquitetural já existe:
  build plane, runtime plane, preview runtime, multi-cell placement.

### 3. Expandir a ontologia de recursos além do necessário

Toda PaaS baseada em CRDs corre o risco de multiplicar tipos e controllers demais.

Risco para LoopZero:

- mais recursos para testar, versionar e operar;
- mais pontos de inconsistência de status;
- mais dificuldade para manter linguagem de produto enxuta.

Direção correta:

- cada novo CRD deve justificar claramente o boundary de domínio que ele cria.

## Benchmark Aplicado Ao LoopZero

### Adoções recomendadas

#### A. Tratar CRDs como contratos de produto de control plane

O benchmark reforça que CRD em PaaS não pode ser modelada como detalhe interno qualquer.

Aplicação no LoopZero:

- maior disciplina em spec/status;
- conditions mais uniformes;
- evolução/versionamento com mais cuidado.

#### B. Reforçar controllers como camada tradutora

Aplicação no LoopZero:

- manter resources de produto em alto nível;
- continuar escondendo Gitea/ArgoCD/Harbor/cluster topology do consumidor de produto.

#### C. Consolidar boundaries de extensão onde a plataforma realmente varia

Aplicação no LoopZero:

- build;
- runtime;
- preview runtime;
- placement/multi-cell.

#### D. Fortalecer observabilidade operacional dos CRDs

Korifi mostra o valor de controllers propagarem estado de forma consumível pelos níveis acima.

Aplicação no LoopZero:

- status mais consistente entre resources;
- conditions sumárias mais previsíveis;
- melhor legibilidade para API/CLI.

## Gaps Atuais Do LoopZero Vistos Por Esse Benchmark

### 1. Já temos bom particionamento por binário, mas ainda há duplicação histórica

Há lógica em `internal/controller` e em pacotes split por domínio. Isso é etapa de transição, não estado final ideal.

### 2. Nosso modelo já é de produto, mas ainda pode amadurecer como API declarativa

Especialmente em:

- consistência entre CRDs;
- padrões de conditions;
- relação entre phase machine e readiness sumária.

### 3. O LoopZero já assume multi-cluster de forma mais forte que muitas referências

Isso nos dá mais poder, mas também exige uma régua mais alta para:

- containment de falha externa;
- separação de clients;
- recovery parcial.

## Decisão Estratégica

Korifi e Epinio confirmam três princípios que devem governar os operators do LoopZero:

1. Operators de PaaS existem para preservar abstração de produto.
2. CRDs devem ser contratos declarativos estáveis do control plane.
3. Controllers devem traduzir, observar e corrigir drift, não apenas provisionar uma vez.

## Conclusão Prática

Se precisarmos resumir o benchmark em uma única diretriz:

- adotar a disciplina declarativa e tradutora de PaaS como Korifi e Epinio;
- evitar transformar LoopZero em uma coleção de recursos Kubernetes expostos sem filtro.

Esse é o uso correto dessas referências para os operators do LoopZero.
