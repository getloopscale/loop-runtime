# Charts PaaS Benchmark

## Objetivo

Este documento analisa a pasta `charts/` a partir da perspectiva de uma PaaS como o LoopZero.

O benchmark aqui não é “qual chart parece bonito”. É:

- como plataformas maduras empacotam componentes, CRDs, políticas e observabilidade;
- como a camada de packaging conversa com GitOps e bootstrap;
- o que isso significa para o LoopZero.

## O Que Uma PaaS Precisa De Seus Charts

### 1. Charts como contrato de instalação, não só template reuse

Em uma PaaS, chart precisa dizer claramente:

- o que instala;
- com quais defaults;
- com quais permissões;
- com quais sinais operacionais.

Por que isso é forte:

- aumenta previsibilidade;
- reduz “tribal knowledge” na operação;
- melhora GitOps e rollback.

Lição para LoopZero:

- charts do LoopZero precisam continuar sendo artefatos operacionais explícitos, não apenas wrappers de deployment.

### 2. Library chart disciplinado

Padrões compartilhados em charts são valiosos quando centralizam:

- labels;
- security context;
- imagens;
- topology;
- probes.

Por que isso é forte:

- reduz divergência entre componentes;
- facilita elevar o baseline de segurança e operação de uma vez.

Lição para LoopZero:

- `loopzero-common` é ativo arquitetural real;
- ele deve continuar pequeno, útil e focado em primitives compartilhadas.

### 3. CRDs e policies como artefatos de primeira classe

Plataformas maduras tratam API declarativa e policies como partes instaláveis da plataforma, não como anexos.

Por que isso é forte:

- melhora governança de upgrade;
- deixa explícita a postura de segurança e o contrato declarativo do sistema.

Lição para LoopZero:

- `loopzero-crds` e `loopzero-admission-policies` estão na direção certa e devem ser tratados como cidadãos de primeira classe.

### 4. Observabilidade empacotada junto da plataforma

Uma PaaS não deve depender de o operador “lembrar depois” de instalar alertas e dashboards.

Por que isso é forte:

- a release da plataforma já nasce operável;
- day-2 fica mais confiável.

Lição para LoopZero:

- `loopzero-monitoring` não é um extra; é parte do contrato de plataforma.

## O Que Não Devemos Copiar Cegamente

### 1. Umbrella chart que esconde topologia real

Umbrella charts são úteis, mas podem virar abstrações enganosas.

Risco para LoopZero:

- esconder dependências entre CRDs, operators, API e monitoring;
- tornar troubleshooting e upgrade mais opacos.

Direção correta:

- usar `loopzero-platform` como composição explícita, não como mágica.

### 2. Values que equivalem a reescrever o template

Charts de baixa qualidade transferem complexidade demais para o usuário de values.

Risco para LoopZero:

- chart difícil de usar;
- aumento de superfície breaking;
- menor previsibilidade.

Direção correta:

- defaults fortes;
- variação real onde necessário;
- sem obrigar o usuário a remontar metade do manifest via values.

### 3. Duplicação silenciosa entre charts publicados e bootstrap charts

Ter duas famílias de chart é aceitável. O problema é deixá-las divergir sem governança.

Risco para LoopZero:

- bootstrap com defaults diferentes do day-2;
- segurança ou observabilidade inconsistentes;
- correção aplicada em uma família e perdida na outra.

Direção correta:

- diferenças intencionais;
- revisão cruzada quando padrões compartilhados mudam.

## Benchmark Aplicado Ao LoopZero

### Adoções corretas já visíveis

#### A. Library chart com primitives úteis

Isso é exatamente o tipo de reutilização que vale a pena em packaging.

#### B. CRD chart separado

Isso é maturidade real de plataforma, não detalhe cosmético.

#### C. Policy chart dedicado

Mostra que segurança da plataforma está entrando como artefato declarativo.

#### D. Monitoring chart próprio

Mostra que observabilidade faz parte da release da plataforma.

#### E. Umbrella chart com dependências explícitas

Isso ajuda a compor a plataforma sem perder a noção dos componentes.

## Gaps E Riscos Que O Benchmark Destaca

### 1. Coexistência entre charts novos e legados

`loopzero-controller` e `loopzero-tenant-controller` convivem com charts split. Isso exige governança deliberada até a convergência final.

### 2. Dualidade `charts/` vs `infra/helm`

Essa é uma escolha válida, mas precisa disciplina contínua para não virar fonte de drift operacional.

### 3. Crescimento do library chart

Toda library chart bem-sucedida corre o risco de virar framework. O LoopZero precisa evitar isso.

## Decisão Estratégica

Para o LoopZero, a pasta `charts/` deve continuar servindo a cinco princípios:

1. packaging explícito;
2. defaults seguros;
3. CRDs e policies como first-class artifacts;
4. observabilidade como parte da release;
5. composição de plataforma sem esconder dependências reais.

## Conclusão Prática

Se precisarmos resumir o benchmark em uma única diretriz:

- charts do LoopZero devem simplificar a instalação e a operação;
- nunca simplificar ao ponto de esconder contratos críticos da plataforma.

Esse é o uso correto da camada `charts/` em uma PaaS como o LoopZero.
