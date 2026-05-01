# API PaaS Benchmark

## Objetivo

Este documento analisa a API do LoopZero a partir da perspectiva de uma PaaS.

As referências principais são:

- o próprio papel do LoopZero como CLI-first PaaS;
- a arquitetura do Korifi, que explicita uma API de tradução para um control plane declarativo;
- o posicionamento de plataformas como Epinio, que expõem “code to URL” enquanto escondem a maior parte da complexidade operacional.

## O Que Uma API De PaaS Faz Bem

### 1. Fala em intenção de produto, não em componentes internos

Uma boa API de PaaS fala em:

- deploy;
- project;
- environment;
- domain;
- preview;
- release;
- team.

Por que isso é forte:

- protege a experiência da CLI e da UI;
- evita que o usuário precise entender GitOps, workflow engines ou topology interna.

Lição para LoopZero:

- esse precisa continuar sendo o modelo mental da nossa API.

### 2. Orquestra, mas não faz tudo de forma síncrona

Korifi deixa claro que a camada de API traduz intenção para recursos/control plane abaixo dela. A API não executa tudo inline.

Por que isso é forte:

- request path permanece rápido e confiável;
- long-running work fica em subsistemas adequados.

Lição para LoopZero:

- workflows, callbacks, reconcile loops e SSE são a direção correta;
- deploy síncrono “faz tudo na request” seria regressão arquitetural.

### 3. Expõe estado útil, não apenas status bruto de subsistema

Uma PaaS boa agrega realidade operacional suficiente para o cliente agir.

Por que isso é forte:

- a CLI não precisa consultar cinco sistemas diferentes;
- dashboard consegue refletir a plataforma como produto.

Lição para LoopZero:

- endpoints como status, logs, health e listagens precisam continuar priorizando valor de decisão para o usuário.

### 4. Assume falha parcial como normal

Em uma PaaS, banco, workflow engine, GitOps, cluster runtime e edge podem falhar em ritmos diferentes.

Por que isso é forte:

- evita modelos “happy path only”;
- aumenta confiabilidade real da plataforma.

Lição para LoopZero:

- lock, idempotência, admission, pool breaker, retry loops e circuit breakers precisam continuar sendo parte central do desenho.

## O Que Não Devemos Copiar Cegamente

### 1. API excessivamente translacional e pouco orientada ao produto

Algumas plataformas baseadas em CRDs correm o risco de expor demais a ontologia interna.

Risco para LoopZero:

- empurrar complexidade declarativa para o usuário cedo demais;
- degradar a promessa de simplicidade da CLI.

Direção correta:

- a API do LoopZero deve continuar sendo fachada de produto sobre um control plane complexo.

### 2. Superfície síncrona grande demais

Tentar resolver toda a cadeia na request quase sempre piora disponibilidade.

Risco para LoopZero:

- timeouts longos;
- comportamento inconsistente sob pressão;
- baixa tolerância a retry e replay.

Direção correta:

- manter o design de persistir intenção e mover trabalho pesado para fluxos assíncronos.

### 3. Conflitos concorrenciais tratados tarde demais

Plataformas que não definem bem o boundary de serialização acabam aceitando mutações que competem entre si e só falham depois.

Risco para LoopZero:

- builds duplicados;
- drift de release;
- ativação conflitante.

Direção correta:

- proteger cedo com lock ambiental e idempotência.

## Benchmark Aplicado Ao LoopZero

### Adoções corretas já visíveis

#### A. Middleware chain explícita e orientada a operação

Isso ajuda a manter segurança, observabilidade e degradação previsíveis.

#### B. Lock transacional por environment

Esse é comportamento de PaaS madura: concorrência tratada como contrato, não como acaso.

#### C. Build orchestration por interfaces

Isso reduz acoplamento entre API, workflow engine, GitOps e runtime.

#### D. Rotas públicas, internas, admin e protegidas separadas

Isso mostra que a superfície HTTP já é pensada por tipo de consumidor.

### Gaps E Riscos Que O Benchmark Destaca

#### A. A API já é ampla e precisa manter disciplina de fronteiras

Quanto mais domínios entram no módulo, maior o risco de handlers ou services virarem “god object”.

#### B. Multi-cell torna a API mais poderosa e mais sensível

Placement e discovery aumentam bastante a necessidade de contracts e observabilidade claros.

#### C. Erro estruturado ainda pode amadurecer

Mesmo com bom envelope próprio, a API pode evoluir na direção de problem details mais formais ao longo do tempo.

## Decisão Estratégica

Para o LoopZero, a API deve continuar servindo a cinco princípios:

1. fachada de produto;
2. orquestração curta e síncrona;
3. execução longa e assíncrona;
4. concorrência e replay como contrato;
5. estado agregado e útil para CLI/UI.

## Conclusão Prática

Se precisarmos resumir o benchmark em uma única diretriz:

- a API do LoopZero deve esconder a complexidade do control plane;
- nunca fingir que essa complexidade não existe.

Esse é o uso correto da camada `api/` em uma PaaS como o LoopZero.
