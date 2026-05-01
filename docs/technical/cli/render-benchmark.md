# Render CLI Benchmark

## Objetivo

Este documento analisa [RENDER_CLI.md](./RENDER_CLI.md) como benchmark de experiência para a LoopZero CLI.

O foco não é copiar o produto Render. O foco é extrair padrões de UX e desenho de CLI que fazem sentido para uma PaaS madura, e decidir o que devemos adotar, adaptar ou rejeitar no contexto do LoopZero.

## O Que O Render Faz Bem Em Experiência

### 1. Modo dual explícito: humano e automação

O Render deixa claro que a CLI opera em dois modos:

- interativo, quando há TTY;
- estruturado, quando há `--output json|yaml|text` ou non-TTY.

Por que isso é forte:

- reduz surpresa entre uso humano e uso em script;
- evita que comportamento visual corrompa automação;
- assume que a CLI é tanto produto quanto interface programável.

Lição para LoopZero:

- nossa CLI já tem uma boa base com `--json` e guardas de `CI`/TTY;
- a referência do Render confirma que tratar non-TTY como first-class behavior é padrão de mercado, não detalhe opcional.

### 2. Descobribilidade alta por help e por referência gerada

O documento do Render é gerado a partir da fonte da CLI e segue um formato muito consistente:

- descrição curta;
- usage;
- examples;
- options.

Por que isso é forte:

- reduz drift entre implementação e documentação;
- melhora onboarding;
- transforma o help da CLI em ativo de produto, não só fallback técnico.

Lição para LoopZero:

- `docs/cli-reference.md` é forte, mas hoje é mais manual e mais denso;
- a experiência ideal é help local consistente + referência gerada ou semigerada.

### 3. Comandos orientados a tarefas reais

O Render modela comandos em cima do que o operador quer fazer:

- ver logs;
- reiniciar serviço;
- abrir sessão de banco;
- listar projetos e ambientes;
- disparar deploy.

Por que isso é forte:

- a CLI conversa com intenção do usuário;
- não obriga o usuário a entender a arquitetura interna primeiro.

Lição para LoopZero:

- essa direção já é compatível com as regras do repo;
- precisamos continuar falando em deploy, app, environment, project, team e release, não em mecanismos internos.

### 4. Progressão boa do básico para o avançado

No Render, o iniciante consegue começar com comandos simples, e o usuário avançado encontra profundidade com filtros, subcomandos e passthrough.

Exemplos visíveis no documento:

- `logs` simples ou altamente filtrado;
- `psql` interativo ou com `--command`;
- deploy simples ou apontando commit específico.

Por que isso é forte:

- evita duas CLIs: uma "fácil" e outra "de verdade";
- o mesmo comando cresce com o usuário.

Lição para LoopZero:

- `loopzero deploy`, `loopzero logs`, `loopzero status`, `loopzero rollback` e `loopzero preview` devem seguir esse padrão;
- o caminho feliz precisa ser mínimo, mas o caminho avançado precisa existir sem expor a infra interna.

### 5. Interatividade útil, não ornamental

O Render usa interatividade principalmente para browsing e sessões:

- escolher projeto ou ambiente;
- navegar logs;
- abrir `ssh`, `psql`, `pgcli`, `kv-cli`.

Por que isso é forte:

- interatividade entra onde reduz fricção real;
- não vira dependência para automação.

Lição para LoopZero:

- prompts de confirmação no LoopZero são úteis, mas ainda são superficiais perto desse padrão;
- onde houver ganho real, browsing interativo pode ser melhor do que exigir flags verbosas.

## O Que Não Devemos Copiar Cegamente

### 1. Modelo centrado em inventário de recursos

No Render, muitos comandos partem de resource IDs e tipos de recursos:

- `serviceID`
- `resourceID`
- `postgresID`
- `projectID`

Isso faz sentido para o produto deles, mas para LoopZero é perigoso.

Risco para LoopZero:

- empurrar o usuário para linguagem de inventário interno;
- enfraquecer o modelo mental principal de monorepo + environment + app;
- aumentar vazamento de termos e IDs internos.

Direção correta para LoopZero:

- o centro da experiência deve continuar sendo projeto, ambiente, app e deploy;
- IDs internos só aparecem quando agregam valor operacional claro e já fazem parte de contrato intencional.

### 2. CLI muito larga antes da hora

O Render cobre muitos domínios laterais: jobs, skills, workflows, docs, SQL sessions, SSH, Redis, workspaces.

Isso é maturidade de produto, mas também aumenta peso cognitivo.

Risco para LoopZero:

- espalhar a superfície da CLI cedo demais;
- criar subcomandos antes de consolidar os fluxos principais;
- degradar consistência entre comandos core e comandos periféricos.

Direção correta para LoopZero:

- fortalecer primeiro os loops centrais: login, init, validate, deploy, status, logs, rollback, promote, preview, domain, env;
- só depois expandir capacidades anexas.

### 3. Excesso de affordance interativa fora do core

Render pode sustentar comandos como `psql`, `ssh` e `kv-cli` porque o produto já assume esses recursos como objetos de primeira classe.

Risco para LoopZero:

- reintroduzir detalhes de infra que o produto promete esconder;
- criar atalhos que bypassam o modelo operacional canônico.

Direção correta para LoopZero:

- acesso operacional avançado só deve existir quando preserva a promessa do produto;
- comandos de "escape hatch" precisam ser cuidadosamente delimitados.

## Benchmark De UX Aplicado Ao LoopZero

### Adoções recomendadas

#### A. Índice de help mais uniforme por comando

Cada comando LoopZero deveria expor consistentemente:

- descrição curta;
- usage;
- examples;
- behavior em `--json`;
- notas de CI/non-TTY, quando relevante.

Impacto:

- melhora onboarding;
- reduz dependência de documentação externa;
- aproxima a CLI do padrão de produto observável no Render.

#### B. Convenção global de modo de saída

O Render usa uma flag global única de saída. No LoopZero, a versão atual é `--json`.

Recomendação:

- manter `--json` como contrato atual;
- avaliar evolução futura para `--output text|json`, sem quebrar compatibilidade.

Impacto:

- preserva contrato;
- abre espaço para modo estruturado mais uniforme no longo prazo.

#### C. Browsing guiado para recursos de alta frequência

Candidatos naturais no LoopZero:

- `loopzero project list` com filtros melhores;
- `loopzero env list` e `loopzero env inspect`;
- `loopzero logs`;
- `loopzero build list`.

Recomendação:

- usar interatividade apenas onde reduz custo de descoberta;
- nunca como dependência obrigatória para automação.

#### D. Examples como parte do contrato de comando

O Render trata exemplos como parte da experiência do comando.

Recomendação:

- todo comando core do LoopZero deve ter exemplos reais de uso no help e na referência.

#### E. Referência gerada ou validada por fonte

`RENDER_CLI.md` é forte principalmente porque a documentação parece derivada do código.

Recomendação:

- caminhar para referência gerada ou ao menos validada automaticamente a partir dos comandos reais.

## Gaps Atuais Da LoopZero CLI Vistos Por Esse Benchmark

### 1. Nossa experiência de output ainda é correta, mas mais estreita

Hoje temos:

- `--json`;
- detecção de CI;
- tratamento de TTY.

O benchmark sugere que ainda podemos melhorar:

- consistência de help;
- examples por comando;
- eventualmente uma abstração global de output mode.

### 2. O help da LoopZero ainda não transmite a mesma profundidade de affordance

Temos boa cobertura funcional, mas a experiência de descoberta ainda depende mais de documentação externa e conhecimento prévio do operador.

### 3. Ainda podemos amadurecer browsing e operação assistida

Especialmente em:

- logs;
- builds;
- ambientes;
- recursos de plataforma.

## Decisão Estratégica

O Render é uma boa referência de experiência para LoopZero em cinco princípios:

1. dual-mode humano + automação;
2. help altamente descobrível;
3. comandos orientados à intenção;
4. progressão do simples para o avançado;
5. interatividade útil e opcional.

Mas não deve ser referência para o nosso modelo mental principal de produto.

No LoopZero, a experiência deve continuar centrada em:

- monorepo;
- app units;
- environment lifecycle;
- deploy and release flow;
- abstração de Kubernetes e da infraestrutura subjacente.

## Conclusão Prática

Se tivermos que resumir o benchmark em uma diretriz:

- copiar a disciplina de UX do Render;
- não copiar a ontologia de recursos do Render.

Esse é o uso correto do `RENDER_CLI.md` como referência para uma PaaS como a nossa.
