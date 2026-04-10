# Mensagem Dura ao Time: 100.000 Pods Não É Opcional no Desenho

**Data:** 2026-04-10  
**Objetivo:** deixar explícito, sem suavização, o padrão de exigência para decisões de arquitetura e implementação do LoopZero

## O ponto

O LoopZero **não precisa suportar 100.000 pods hoje em produção**.

Mas o LoopZero **precisa, a partir de agora, ser construído de forma que 100.000 pods continuem possíveis**.

Isso muda completamente o padrão de decisão.

Não estamos autorizados a tomar decisões que:

- resolvem o problema de hoje;
- deixam o sistema aparentemente funcional no curto prazo;
- e empurram uma reescrita arquitetural inevitável para depois.

Se a solução só funciona enquanto temos poucos tenants, poucos apps, pouca concorrência e operadores babysittando o sistema, então essa solução **não serve** como base do LoopZero.

## O erro que não podemos mais cometer

O erro é confundir:

- `não precisamos operar 100.000 pods agora`

com

- `podemos continuar implementando como se nunca fôssemos chegar lá`

Isso é inaceitável.

Infra pode crescer depois.

O que **não pode** ficar para depois é o desenho da solução.

Se deixarmos:

- controle central singleton,
- gargalos globais,
- queries sem isolamento por célula,
- acoplamento invisível entre API e reconciliação,
- fluxos sem backpressure,
- migração de tenant sem consistência,
- e dependências operacionais que só funcionam em ambiente pequeno,

então não estaremos adiando custo. Estaremos **construindo dívida estrutural** com vencimento garantido.

## O estado atual do LoopZero exige honestidade

Hoje o LoopZero:

- já separa `management`, `build` e `runtime` por cluster;
- já aponta corretamente para `cell-based architecture`;
- já tem `cell_id` no modelo;
- já tem algumas abstrações certas;
- mas **ainda não fechou as implementações críticas que transformam essa visão em realidade**.

Também é fato que:

- `cell.ts` ainda está em placeholder;
- `PlacementEngine` ainda é default;
- loops centrais ainda vivem acoplados ao processo da API;
- o sistema ainda tem defeitos reais expostos em load test recente;
- o roadmap de escala foi justamente criticado por ser mais otimista do que o estado real permite.

Ou seja:

**o LoopZero ainda não está pronto para escalar**

e justamente por isso **cada decisão nova precisa ser julgada pela trajetória até 100.000 pods**.

## O padrão correto de decisão

Daqui para frente, toda solução precisa responder:

1. Isso continua válido quando houver muitas células?
2. Isso continua válido quando a API tiver várias réplicas?
3. Isso continua válido quando houver contenção real de banco, filas e reconciliação?
4. Isso continua válido quando uma célula falhar?
5. Isso continua válido quando houver milhares de tenants, não dois?
6. Isso continua válido sem intervenção manual frequente?

Se a resposta for “não sabemos”, a solução não está pronta.

Se a resposta for “não, mas depois a gente refatora”, a solução está errada.

## O que é aceitável adiar

É aceitável adiar:

- tamanho final de nodes;
- tamanho final de clusters;
- tier final do banco;
- número de células por região;
- custo final de infraestrutura;
- tuning fino dependente de carga real.

Tudo isso é infraestrutura, capacidade e otimização.

## O que não é aceitável adiar

Não é aceitável adiar:

- desacoplamento interno do control plane;
- eliminação de gargalos singleton como base estrutural;
- isolamento por célula no desenho da solução;
- modelo de consistência entre componentes;
- estratégia de backpressure e limites;
- desenho de migração e falha parcial;
- contratos verificáveis de escala e operação.

Se isso for adiado, o time não estará ganhando velocidade.

Estará só trocando progresso aparente por rework futuro.

## A régua precisa subir agora

O LoopZero não está mais num estágio em que “funciona localmente” ou “passa no happy path” é critério suficiente.

Esse padrão é baixo demais para a ambição do produto.

Se queremos construir uma plataforma séria, a régua precisa ser:

- solução correta antes de solução conveniente;
- arquitetura evolutiva antes de remendo local;
- consistência e isolamento antes de improviso;
- evidência antes de narrativa.

## O que o time precisa ouvir sem filtro

Se continuarmos aceitando soluções pequenas demais para a arquitetura que escolhemos, vamos chegar a um ponto em que:

- multi-cell vira reescrita;
- API vira gargalo estrutural;
- banco vira funil global;
- GitOps vira fila incontrolável;
- migração de tenant vira operação de alto risco;
- e cada ganho de cliente aumenta o medo em vez da confiança.

Isso é fracasso de engenharia, não falta de máquina.

Infra cara se compra depois.

Arquitetura errada se paga duas vezes:

1. quando implementamos rápido do jeito errado;
2. quando precisamos reconstruir tudo depois.

## Exigência objetiva a partir de agora

A partir de agora, nenhuma solução relevante para o LoopZero deveria ser aprovada se:

- depende de estado local implícito sem estratégia de durabilidade;
- depende de coordenação singleton como fundamento permanente;
- ignora `cell_id` ou futuro isolamento por célula;
- assume queries globais onde o futuro exige escopo por célula;
- não define limites, admissão ou backpressure;
- não tem estratégia de falha ou rollback;
- só funciona porque a escala atual é pequena.

## A mensagem final

Não precisamos suportar 100.000 pods hoje.

Mas **precisamos parar imediatamente de escrever software que nos impede de chegar lá**.

Esse é o padrão.

Não é um ideal.
Não é “visão de longo prazo”.
Não é “talvez um dia”.

É exigência de arquitetura a partir de agora.

Se a solução só serve para o LoopZero pequeno, então ela não serve para o LoopZero.

Se a solução empurra o problema estrutural para frente, ela não é pragmática.

Ela é cara.

E quanto mais tempo aceitarmos isso, mais caro vai ficar.
