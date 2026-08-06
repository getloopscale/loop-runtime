---
type: operational-runbook
title: "System Design Playground — Roadmap de implementação"
---

# System Design Playground — Roadmap de implementação

> Do protótipo HTML (`system-design-playground.html`) a um produto com feature paga.
> Opinião de arquiteto, honesta sobre o que é difícil e onde está o *moat*.

## Tese do produto

Existem dezenas de editores de diagrama (Excalidraw, Eraser, draw.io). **Desenhar não é o
moat** — é commodity. O que ninguém entrega bem é o par:

1. **Simulador** — "coloca em produção e vê o que acontece" (latência sob carga, gargalo,
   e o **caos**: cai a AZ / o datacenter → o sistema sobrevive?).
2. **Review com IA** — "seu desenho faz sentido? nota X, e aqui está o porquê".

**A parte grátis é o editor. A parte paga é o simulador + caos + review de IA.** É o que
transforma "mais um whiteboard" em "um coach de system design que roda o teu desenho".

---

## Build vs Buy (não reinventar a roda — Rule 9)

| Peça | Decisão | Porquê |
|---|---|---|
| Canvas / drag-drop / edges | **React Flow** (buy/OSS) | Maduro, portas tipadas, minimap, undo. O HTML MVP provou o conceito; não escreva um canvas do zero. |
| Motor de simulação | **Build** (é o moat) | Ninguém vende isso pronto pro nosso caso. Modelo analítico de filas (M/M/c), não sim discreto no v1. |
| Review de IA | **Claude API** (buy) | Não treinar modelo. Prompt + rubrica + **grounding nos números do simulador**. |
| Persistência / auth / billing | **Supabase + Stripe** (buy) | Padrão. Zero valor em construir. |
| Templates / cenários | **Build** (conteúdo = growth) | "Desenhe um encurtador p/ 10k RPS" — é o loop de engajamento. |

---

## Milestones

### M0 — Protótipo (✅ feito)
`system-design-playground.html`. Valida o modelo de interação + SPOF + caos por
conectividade + score heurístico. **Serve pra decidir build-vs-buy do canvas.** Descartável.

### M1 — Editor de verdade (fundação)
**Goal:** editor persistente, multi-usuário, exportável.
- React + **React Flow**; catálogo de componentes como dado (JSON), não hardcoded.
- Portas tipadas (um DB não conecta em CDN), validação de conexão.
- Save/load (Supabase), export PNG/JSON, undo/redo.
- Auth (designs por usuário).
- **Sai:** editor grátis usável. **Esforço:** ~3 eng-weeks.

### M2 — Simulador v1 (analítico) — 💰 moat parte 1
**Goal:** sair de "conectividade" pra "o que acontece sob carga".
- Input: **RPS** na entrada + fanout por aresta.
- Cada componente: *service time* + *capacity* (instâncias × μ).
- Modelo de filas **M/M/c**: utilização ρ=λ/(c·μ); se ρ≥1 → **gargalo** (satura).
- Output: **p50/p95/p99** ponta-a-ponta, throughput teto, nó que satura primeiro.
- Visual: arestas engrossam com tráfego; nó vermelho quando ρ→1.
- **Por que analítico e não sim-discreto:** roda em ms no browser, determinístico, e é
  exato o bastante pra "achar o gargalo". Sim-discreto (estilo SimPy) é o v2 de precisão.
- **Esforço:** ~3 eng-weeks (a matemática é o trabalho).

### M3 — Chaos engine — 💰 moat parte 2
**Goal:** "e se a AZ/datacenter/região cair?" com número, não achismo.
- Cada componente tem **disponibilidade** (instância única 99.9% → redundante 99.99%).
- Composição série/paralelo ao longo dos caminhos Client→store → **SLA do sistema**.
- Injetar falha: matar nó / AZ / região → recomputar disponibilidade + latência **em modo
  degradado** + **blast radius** (% de capacidade/caminhos perdidos).
- Detecta failover: existe caminho redundante? Em quanto tempo (MTTR estimado)?
- É a hipótese de **steady-state** (Chaos Engineering) computada.
- **Esforço:** ~2 eng-weeks (reusa o grafo do M2).

### M4 — Review com IA (LLM) — 💰 moat parte 3
**Goal:** a nota que "faz sentido".
- Serializa o grafo → prompt com **rubrica de system design**.
- **Grounding:** injeta os outputs determinísticos do M2/M3 (gargalo, SPOF, p99, SLA) no
  prompt, pra IA criticar com **números reais** — não alucina ("teu LB é SPOF; ρ=0.97 no
  App tier; adicione instância em AZ-b").
- Output estruturado: score 0-100 + findings priorizados + **fix sugerido aplicável** (botão
  "aplicar sugestão" que edita o canvas).
- **Esforço:** ~2 eng-weeks.

> **A fatia paga mínima vendável = M2 + M3 + 1 review M4.** Editor (M1) fica grátis.

### M5 — Produto (billing + gating)
- Stripe; free = editor + review heurístico; **paid = simulador + caos + review LLM**.
- Biblioteca de templates, share por link, teams.
- **Esforço:** ~2 eng-weeks.

### M6 — Growth (conteúdo)
- **Cenários-desafio** ("URL shortener 10k RPS", "feed com fanout") com meta de score.
- Leaderboard, modo aprendizado (a IA vira tutor), share do score (loop viral).
- **Esforço:** contínuo.

---

## Riscos & onde vai doer

| Risco | Mitigação |
|---|---|
| Simulador "certo o bastante" é difícil de calibrar | Começar M/M/c simples; validar contra casos conhecidos; ser honesto que é estimativa. |
| IA alucinar crítica | **Grounding** nos números do M2/M3 é obrigatório (não deixar a IA "chutar" métricas). |
| Escopo do M2 explode | Time-box: só latência + gargalo no v1; throughput/backpressure fino no v2. |
| Virar "mais um diagram tool" | Segurar o editor grátis e cobrar **só** simulador+caos+IA — nunca inverter. |

## Sequência recomendada
`M1 → M2 → M3 → M4` é o caminho crítico pra ter a fatia paga. **M2 é o coração** — se o
simulador não convencer, o resto não importa. Faria M2 num spike isolado (2 semanas) **antes**
de investir no M1 completo, pra de-riscar a matemática cedo.

## Total (1 dev, rough)
~12 eng-weeks até a fatia paga vendável (M1-M4) + ~4 pra produto/growth (M5-M6).
