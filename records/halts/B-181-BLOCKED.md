# B-181 — HALTED: bala 1 é decisão de produto

**Date:** 2026-09-06
**Phase halted:** /idea-to-release entrada (before /plan-alignment)
**Verdict:** AWAITING_HUMAN
**Owner of decision:** product / sponsor

## Why

Bala 1 do B-181 é um OU cujos dois ramos precisam de uma decisão de produto que
o agente não pode tomar:

- **Opção A** — emitir `loopzero_egress_bytes_total` e `loopzero_docr_pulls_total`.
  Precisa nomear o *producer point* de cada uma. O plan de 2026-05-12
  (`docs/wiki/plans/completed/2026-05-12-launch-readiness-waves-plan.md:972`)
  listou duas alternativas por métrica, sem decidir:
  - egresso: `loopzero_egress_bytes_total{tenant}` "if exposed by ingress" OR DOCR pull logs
  - puxadas: "DO Spaces API events OR registry access logs"
  Nenhum ADR registra a escolha. Escrever um producer sem ela é inventar
  arquitetura (violaria rules/autonomy-envelope.md — envelope declara que
  decisões de produto pertencem à pessoa).

- **Opção B** — remover as duas consultas do agregador e registrar por escrito
  que essas métricas saem do produto. É o próprio DoD do item: *"ou o item
  registra, com razão escrita, que essas duas métricas de custo saem do
  produto e o agregador deixa de consultá-las"*. A razão escrita é a decisão
  de produto — não é o agente que a produz.

O próprio bloco B-181 diz literalmente:
> Bala 1 é um OU cujo segundo ramo é uma DECISÃO de produto — essas duas
> métricas de custo saem ou não saem do produto — que não é do agente.

## What is measured

- Grep em 7 repositórios (`loopzero`, `loopzero-cloud`, `loopzero-data-plane`, `loopzero-ops`,
  `loopzero-forge`, `loopzero-lens-reconciler`, `loopzero-workspace`) — zero producers.
- Prometheus `k3d-loopzero-core` — 48 séries `loopzero_*`, nenhuma das duas presentes.
- Bala 2 já FECHADA — `loopzero-ops quality cost-series-producers` está wired e
  congelada em gateset.frozen (evidência no próprio bloco B-181).

## What the human needs to decide

Uma das duas, e por escrito:

1. **"Emitir"** — dizer ONDE cada producer vive:
   - egresso: no reverse-proxy (Traefik?) por response bytes? DOCR pull logs?
   - puxadas: hook no registry access log? DO Spaces API events?
2. **"Remover"** — as duas métricas não são do produto; agregador deixa de
   consultá-las (bala 3 então vira "cost_counters recebe linha para
   loopzero_build_duration_seconds", a única sobrevivente).

Com a decisão registrada (ADR curto ou update no BACKLOG), o item volta ao
queue e Daedalus segue.

## Lane released

`LANE FREE: yes` — Hermes pode agendar o próximo item enquanto B-181 aguarda.
