# Infra PaaS Benchmark

## Objetivo

Este documento usa referências de PaaS e de bootstrap declarativo para extrair padrões úteis à infraestrutura do LoopZero.

As referências principais aqui são:

- Argo CD cluster bootstrapping / App-of-Apps
- Pulumi stacks/state
- Korifi architecture
- Epinio installation model

O objetivo é responder:

- o que uma PaaS madura faz bem em bootstrap e operação;
- o que isso implica para a infra do LoopZero;
- o que devemos adotar, adaptar ou evitar.

## O Que Essas Referências Confirmam

### 1. PaaS séria não esconde que precisa de fundação explícita

Epinio deixa claro que sua proposta de "code to URL" ainda depende de:

- cluster;
- ingress;
- cert-manager;
- instalação declarativa da plataforma.

Por que isso é forte:

- evita fantasia de "zero infra";
- força design honesto de pré-requisitos e bootstrap.

Lição para LoopZero:

- nossa infra deve continuar explicitando fundação, camadas e dependências;
- simplicidade de produto não pode significar ambiguidade operacional.

### 2. GitOps é excelente para day-2, mas não substitui bootstrap pensado

A própria documentação do ArgoCD separa o problema de cluster bootstrapping e mostra o App-of-Apps como padrão para instalar muitos componentes em um cluster novo.

Por que isso é forte:

- reconhece que existe um momento anterior ao GitOps contínuo;
- evita ciclo lógico de "usar Argo para instalar o que ainda não existe".

Lição para LoopZero:

- a divisão L1/L2/L3 do LoopZero está arquiteturalmente correta;
- o valor agora é proteger essa separação contra regressão.

### 3. State e contratos entre stacks são parte do produto

Pulumi trata stack e state como primeira classe.

Por que isso é forte:

- infra reproduzível depende de state bem governado;
- multi-stack exige contratos explícitos, não outputs implícitos.

Lição para LoopZero:

- nossos stacks separados e `stack-outputs.ts` apontam na direção certa;
- runbooks de state repair não são sinal de fraqueza, são sinal de maturidade.

### 4. PaaS boa usa Kubernetes como substrate, não como UX final

Korifi e Epinio mostram que a plataforma roda em Kubernetes, mas a experiência de produto deve abstrair a maior parte dessa complexidade.

Por que isso é forte:

- ajuda a decidir o que vai para CLI/API e o que fica na infra;
- impede que o produto force o usuário a operar a plataforma via manifests crus.

Lição para LoopZero:

- a infra deve ser poderosa sem empurrar o usuário para a linguagem do cluster;
- complexidade deve ficar encapsulada em bootstrap, operators e APIs.

## O Que Não Devemos Copiar Cegamente

### 1. Bootstrap simplificado demais

Muitas plataformas menores tratam bootstrap como um único chart ou um script enorme.

Risco para LoopZero:

- colapsar L1, L2 e L3;
- dificultar debug, rollback e migração;
- criar acoplamento oculto entre provider, cluster e aplicações.

Direção correta:

- manter layered bootstrap como princípio permanente.

### 2. Dependência excessiva em passos manuais

Plataformas que parecem simples às vezes escondem muito conhecimento tribal.

Risco para LoopZero:

- onboarding lento;
- incidentes difíceis de recuperar;
- baixa reprodutibilidade.

Direção correta:

- scripts e runbooks devem continuar sendo produto operacional da plataforma.

### 3. GitOps sem governança de drift e repair

GitOps sozinho não resolve:

- state quebrado;
- secrets incoerentes;
- wiring cross-cluster;
- orfandade de recursos cloud.

Risco para LoopZero:

- assumir que "Argo corrige tudo";
- ignorar incidentes reais de state e bootstrap.

Direção correta:

- manter repair, cleanup e break-glass como capacidades oficiais.

## Benchmark Aplicado Ao LoopZero

### Adoções recomendadas

#### A. Preservar a separação entre fundação, bootstrap e day-2

Isso já é uma força do LoopZero e deve virar regra arquitetural estável.

#### B. Tratar state como domínio operacional explícito

O benchmark reforça que state backend, restore e cleanup precisam continuar documentados, testados e automatizados.

#### C. Promover validação a gate obrigatório

Em PaaS, validação de schema, sync waves, alert rules e parity não é overhead. É proteção de produto.

#### D. Continuar investindo em runbooks como parte da arquitetura

O LoopZero já tem um volume relevante de runbooks. Isso é um ativo da plataforma e precisa acompanhar evolução da infra.

#### E. Manter multi-cluster como verdade de projeto

Muitas referências simplificam para um cluster. O LoopZero não deve regredir para esse modelo mental se ele quebrar a abstração real da plataforma.

## Gaps Atuais Do LoopZero Vistos Por Esse Benchmark

### 1. A transição entre stacks Pulumi ainda precisa disciplina contínua

Temos múltiplos projetos Pulumi e documentação de boundaries, mas isso exige atenção constante para não reintroduzir sobreposição.

### 2. Wiring e segredos continuam sendo área sensível

O benchmark reforça que bootstrap "funciona na minha máquina" é insuficiente; wiring cross-cluster e secret generation continuam sendo áreas críticas.

### 3. O LoopZero já opera como plataforma de verdade, então o custo de infra é inevitavelmente maior

Isso não é defeito. Significa apenas que a nossa régua de SOTA precisa incluir repair, rollback, validation e observability desde o início.

## Decisão Estratégica

As referências confirmam cinco princípios para a infra do LoopZero:

1. fundação explícita;
2. bootstrap em camadas;
3. GitOps governando day-2;
4. state tratado como domínio crítico;
5. operação e repair como parte da arquitetura.

## Conclusão Prática

Se precisarmos resumir o benchmark em uma única diretriz:

- simplificar a experiência do produto;
- nunca simplificar demais a verdade operacional da plataforma.

Esse é o uso correto das referências de PaaS para a pasta `infra/`.
