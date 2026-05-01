# Cloud PaaS Benchmark

## Objetivo

Este documento analisa o módulo `cloud/` a partir da perspectiva de uma PaaS como o LoopZero.

O benchmark aqui é a função que esse módulo desempenha no produto:

- ser a camada de conta e experiência web;
- mediar login, billing e dashboard;
- sem competir com a engine como control plane operacional.

## O Que Uma Camada Cloud De PaaS Faz Bem

### 1. Mantém identidade e cobrança próximas, mas não misturadas

Em uma PaaS madura, login e billing se encontram no produto, mas não são a mesma coisa.

Por que isso é forte:

- evita que falha de cobrança quebre autenticação básica;
- permite governar capabilities comerciais separadamente da identidade.

Lição para LoopZero:

- `auth/` e `billing/` precisam continuar distintos e bem definidos.

### 2. Atua como borda de produto, não como novo control plane

O cloud service deve simplificar a experiência do dashboard, mas não deve duplicar a semântica do engine.

Por que isso é forte:

- reduz drift entre dashboard e CLI;
- preserva autoridade do control plane operacional.

Lição para LoopZero:

- o proxy para `/engine` e o uso de session token local mostram a direção correta.

### 3. Trata billing como capability layer

SaaS/PaaS madura usa billing para governar:

- limites;
- planos;
- eligibility de capacidades;
- fluxo comercial do produto.

Por que isso é forte:

- conecta negócio e plataforma sem acoplar tudo;
- permite enforcement consistente.

Lição para LoopZero:

- o módulo cloud precisa continuar sendo o lugar do estado comercial, não o único lugar do enforcement operacional.

### 4. Reconhece que medição é assíncrona

Uso, faturamento e reconciliação quase nunca são perfeitamente síncronos.

Por que isso é forte:

- evita design frágil e sobreconfiante;
- melhora auditabilidade e correção eventual.

Lição para LoopZero:

- a separação `tracker/reporter/scheduler/reconciler` é sinal de maturidade e deve ser preservada.

### 5. Dashboard consome o mesmo produto que a CLI

Em PaaS boa, dashboard e CLI não são dois produtos diferentes.

Por que isso é forte:

- reduz inconsistência de modelo mental;
- facilita suporte e documentação.

Lição para LoopZero:

- dashboard deve continuar refletindo projects, environments, billing, domains e team do mesmo jeito que a CLI e a engine os entendem.

## O Que Não Devemos Copiar Cegamente

### 1. Dashboard como fonte primária de regra

Muitas plataformas web-first deixam a UI decidir demais.

Risco para LoopZero:

- drift com CLI;
- regras críticas só no frontend;
- segurança e enforcement frágeis.

Direção correta:

- a UI deve consumir contratos, não defini-los.

### 2. Billing fortemente acoplado ao runtime operacional

Quando cobrança entra demais na cadeia operacional, falha comercial pode bloquear sistemas demais.

Risco para LoopZero:

- blast radius excessivo;
- comportamento opaco em incidentes.

Direção correta:

- billing governa capability, mas a disponibilidade do control plane precisa de isolamento razoável.

### 3. Sessão web tratada como extensão direta do token do provedor

Misturar tokens externos e sessão interna costuma aumentar acoplamento.

Risco para LoopZero:

- semântica confusa;
- rotação e audience mal definidas.

Direção correta:

- session token do dashboard com contrato próprio é a abordagem certa.

## Benchmark Aplicado Ao LoopZero

### Adoções corretas já visíveis

#### A. Cloud service separado da engine

Isso ajuda a proteger o boundary do produto.

#### B. Device flow alinhado à natureza CLI-first

Isso reforça coerência de produto entre CLI e dashboard.

#### C. Session token próprio para dashboard

Isso reduz acoplamento direto com o token do provedor.

#### D. Billing e metering explicitamente modelados

Isso trata comercial como domínio real, não como callback solto.

## Gaps E Riscos Que O Benchmark Destaca

### 1. O cloud service é ponto sensível de boundary

Se começar a absorver lógica do engine, o módulo rapidamente perde foco e aumenta drift.

### 2. Metering e billing sempre tendem a crescer em complexidade

Isso exige disciplina contínua em contratos, replay e auditabilidade.

### 3. O dashboard precisa continuar subordinado ao modelo de produto da CLI

Quanto mais a UI crescer, maior o risco de criar ontologia paralela.

## Decisão Estratégica

Para o LoopZero, o módulo `cloud/` deve continuar servindo a cinco princípios:

1. identidade e sessão seguras;
2. billing como capability layer;
3. metering reconciliável;
4. dashboard como consumidor do produto;
5. boundary rígido entre cloud e engine.

## Conclusão Prática

Se precisarmos resumir o benchmark em uma única diretriz:

- `cloud/` deve simplificar conta, billing e dashboard para o usuário;
- nunca deve competir com a engine como centro da lógica operacional.

Esse é o uso correto do módulo `cloud/` em uma PaaS como o LoopZero.
