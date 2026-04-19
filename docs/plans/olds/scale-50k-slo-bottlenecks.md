# Scale 50k — SLO Bottlenecks Analysis

**Date:** 2026-04-16
**Context:** [`scale-50k-plan.md`](scale-50k-plan.md) · [`scale-50k-technical-debt.md`](scale-50k-technical-debt.md)
**Scope:** Análise de todos os gargalos identificados que impedem atingir SLOs do scale-50k-plan, ordenados por impacto e com propostas de remoção.

---

## TL;DR

**L2 SLO nominal:** 5 deploys/min sustained por 30 min (para 50 tenants).
**L2 atingido (v4):** ~1.15 deploys/min. **4.3x abaixo do SLO.**

**Funcional ≠ performance:**
- ✅ 50 tenants live com HTTP 200 é atingível
- ❌ Atingir em 10 min (SLO de 5/min) não é

Os gargalos são acumulativos em múltiplas camadas. Este doc lista **11 gargalos distintos** com fix proposto e ganho esperado.

---

## Métricas Medidas (L2 v4)

| Métrica | Valor observado | SLO L2 |
|---------|-----------------|--------|
| Tempo total L2 | ~45 min | ≤30 min |
| Ritmo médio | 1.15/min | 5/min |
| Ritmo pico (primeiros 5 min) | 2.0/min | 5/min |
| Avg build time | 3m26s | <6min |
| Concurrency simultânea real | 5 workflows | ≥15 |
| Taxa de sucesso (que completam) | 100% | >95% |

**Ao SLO real do plano não foi atingido em performance, mas foi em funcionalidade.**

---

## Gargalos Identificados (11)

### 🔴 CAMADA 1 — Storage (infra)

#### Gargalo 1 — DO CSI provisioning rate-limited

**Impacto medido:** ~3 PVCs/min no burst inicial. 15 workflows concorrentes → fila de 7-10 min antes de clone-repo começar.

**Causa:** DigitalOcean Block Storage API tem rate limit de criação (~1 volume/min sustained, com burst inicial de ~5-6).

**Consequência no SLO:**
- Com concurrency=5, budget de 15s por PVC → OK
- Com concurrency=15 (target SLO), budget de 5s por PVC → **falha**

**Fix proposto:** Workspace em `emptyDir` em vez de PVC (ver Bug 21 no technical-debt doc).

**Ganho esperado:** Throughput 5x+ (remove CSI do hot path).

**Esforço:** 4h. **Decisão:** T2 meeting (muda contrato storage).

---

#### Gargalo 2 — Workspace de 5Gi por build (custo + latência)

**Impacto medido:**
- Custo: ~$0.10/build em DO block storage
- Latência: ~30s cold PVC mount vs ~instant emptyDir

**Causa:** `volumeClaimTemplate` com `storage: 5Gi` e access mode `ReadWriteOnce`.

**Consequência SLO:** 30s × 50 builds = 25 min de overhead só em mount.

**Fix proposto:** Igual ao Gargalo 1 (emptyDir).

**Ganho esperado:** 25 min → ~30s total.

---

### 🔴 CAMADA 2 — Compute (infra)

#### Gargalo 3 — `runtimeMaxNodes: 3` (fixado no Pulumi)

**Impacto medido:** 9 pods Pending em L2 v2 (nodes esgotados), ~10 min de plateau.

**Causa:** `runtimeMaxNodes: 3` no `Pulumi.dev.yaml`. Autoscaler `auto_scale: true` habilitado mas respeita teto.

**Status:** PR #43 (`77c57cf`) merged mudando para 6. Workaround via `doctl` aplicado (bug #15 e #20 no technical-debt).

**Consequência SLO:** 10 min de plateau em L2 v2 custou throughput de 50 deploys.

**Fix proposto:** Aplicar via Pulumi (bloqueado pelo drift — Bug 22).

**Ganho esperado:** Dobra capacidade, cobre L2/L3.

---

#### Gargalo 4 — `mgmtMaxNodes: 3` (identificado, não resolvido)

**Impacto previsto:** Mgmt cluster hoje 3/3 nodes. Teto 3. Para L3+ (100 tenants), mgmt vai virar gargalo de operator reconciliação + Gitea + ArgoCD.

**Causa:** Mesma classe de Gargalo 3 — teto artificial Pulumi.

**Consequência SLO:** Bloqueia L3 (100 tenants requerem mais reconcile throughput).

**Fix proposto:** Bump `mgmtMaxNodes` → 5-6 via Pulumi (ou doctl fallback temporário).

**Ganho esperado:** Destrava L3.

**Esforço:** 30min (Pulumi) ou 10min (doctl). **Prioridade:** ALTA para L3.

---

#### Gargalo 5 — Node size `s-4vcpu-8gb` pode ser pequeno para build cluster

**Impacto medido:** 2 nodes build × 3890m CPU = 7780m CPU total. BuildKit pod pede 500m request, 2000m limit. Com 15 concorrentes: 7500m request mínimo (sem margem).

**Causa:** Escolha de instance type no Pulumi. Não houve avaliação para load L2+.

**Consequência SLO:** Em L2 v3 observamos scheduling pressure mesmo com PVCs resolvidos. L3 vai ser pior.

**Fix proposto:** Avaliar `s-8vcpu-16gb` ou aumentar `buildMaxNodes` → 4-6.

**Ganho esperado:** 2x+ concurrency de build por cluster.

**Esforço:** 1h. **Decisão:** T2 (custo vs performance).

---

### 🟡 CAMADA 3 — Network (infra)

#### Gargalo 6 — Docker Hub rate limit (resolvido)

**Impacto medido:** 50 builds × 3 pulls/build = 150 pulls em 10 min. Rate limit unauth Docker Hub: 100 pulls/6h.

**Causa:** BuildKit puxava `node:20-bookworm-slim` diretamente de `docker.io` sem auth.

**Status:** ✅ **RESOLVIDO** — commit `0c9aafc` adiciona `mirror.gcr.io` ao buildkitd.toml.

**Consequência SLO (antes do fix):** 6 workflows Failed em L2 v2. Zero após fix.

**Observação:** Para L3+ (100 builds), vale considerar pull-through cache no DOCR.

---

#### Gargalo 7 — CF Tunnel single-point-of-failure

**Impacto medido:** Harbor 404 quando cloudflared pod cai (Bug 24).

**Causa:** Subdomínios "funcionais" têm A record para Traefik; serviços menos prioritários caem no wildcard → CF Tunnel.

**Status:** Bug 24 resolvido via A record explícito para Harbor (runtime-only).

**Consequência SLO:** Harbor é dependência do `sign-image` pipeline. Se cair em L3+, todos builds quebram.

**Fix proposto:**
- Lista explícita de "infra crítica" com A records
- Alerta quando cloudflared pod cair

**Esforço:** 1h (Pulumi CF record) + 30min (alerta).

---

#### Gargalo 8 — Let's Encrypt rate limit

**Impacto identificado (A-03):** 5 certs/168h por identifier set. Atingido em L2 v3.

**Causa:** Cada tenant gera cert independente; re-deploys em curta janela esgotam limite.

**Consequência SLO:** L3+ (100 tenants) com re-deploys frequentes **vai quebrar regularmente**.

**Fix proposto:**
- Opção A: Wildcard cert `*.dev.loopzero.example` via DNS-01 challenge (1 cert cobre todos)
- Opção B: Cert-manager com clusterIssuer de DNS-01 (evita rate limit HTTP-01)
- Opção C: Private CA para dev (Let's Encrypt só em prod)

**Esforço:** 4-8h. **Decisão:** T2 (arquitetura TLS).

---

### 🟡 CAMADA 4 — Credenciais (operações)

#### Gargalo 9 — imagePullSecret watcher shell não escala

**Impacto medido:** Em L2 v3, 13 pods ImagePullBackOff simultâneos; watcher shell demora 1-5 min para cobrir todos.

**Causa:** Polling loop bash 10s; não reage a eventos K8s; single process.

**Status:** Bug 16 runtime-only.

**Consequência SLO:** Cada pod em Backoff atrasa HTTP 200 em 1-5 min.

**Fix proposto:** Codificar no TenantController (Opção A do technical-debt) ou ESO ClusterExternalSecret (Opção B).

**Ganho esperado:** Secret disponível <5s após namespace criação.

---

### 🟡 CAMADA 5 — Harness / Código

#### Gargalo 10 — Concurrency fixo em 5 no harness

**Impacto medido:** Limita throughput a 5 × 0.3/min per build = 1.5/min absoluto.

**Causa:** `e8a06a2` limitou concurrency a 3 originalmente (port-forward/CF Tunnel overload). Depois bumped para 5 no L2 v4.

**Consequência SLO:** Máximo teórico 5 deploys/min SE build for 1 min (não é — são 3m26s).

**Fix proposto:**
- In-cluster k6 Job (sem port-forward) pode ter concurrency maior
- Ou harness com pool dinâmico (aumenta se API aceita, recua se 429)

**Esforço:** 4h (k6 mode já existe, precisa wiring).

**Ganho esperado:** Concurrency 15-20 → 5 deploys/min factíveis.

---

#### Gargalo 11 — Admission gate propositivo vs reativo

**Impacto medido:** Admission gate configurado para max 20/cell + 3/tenant. Em L2 atual (50 tenants, 1 projeto cada) nunca chegou a bloquear.

**Causa:** Não é "gargalo" ainda — é ceiling. Em L3+ (100+ tenants + múltiplos projetos), vai bloquear.

**Consequência SLO:**
- L2: sem impacto
- L3+: pode serializar deploys → latência aumenta

**Fix proposto:** Revisar limites para cada nível:
- L2 (50 tenants): max 30/cell, 5/tenant
- L3 (100 tenants): max 50/cell, 5/tenant
- Ajustar via env vars no controller

**Esforço:** 30min + validação. **Prioridade:** antes de L3.

---

## Matriz de Impacto vs Esforço

| # | Gargalo | Impacto SLO | Esforço | ROI |
|---|---------|-------------|---------|-----|
| 1 | DO CSI throughput | 🔴 MUITO ALTO | 4h | ⭐⭐⭐⭐⭐ |
| 2 | Workspace 5Gi latência | 🔴 ALTO | (mesma do 1) | ⭐⭐⭐⭐⭐ |
| 3 | runtimeMaxNodes | 🟡 MÉDIO | 30min (Pulumi) | ⭐⭐⭐⭐ |
| 4 | mgmtMaxNodes | 🔴 ALTO (L3+) | 30min | ⭐⭐⭐⭐ |
| 5 | Node size build | 🟡 MÉDIO | 1h | ⭐⭐⭐ |
| 6 | Docker Hub rate (fixed) | ✅ — | 0 | — |
| 7 | CF Tunnel SPOF | 🟡 MÉDIO | 1.5h | ⭐⭐⭐ |
| 8 | LE rate limit | 🔴 ALTO (L3+) | 4-8h | ⭐⭐⭐ |
| 9 | imagePullSecret watcher | 🟡 MÉDIO | 4h | ⭐⭐⭐ |
| 10 | Harness concurrency | 🔴 ALTO | 4h | ⭐⭐⭐⭐ |
| 11 | Admission gate ceilings | 🟡 BAIXO (L2) | 30min | ⭐⭐ |

---

## Caminho do Cliente para 5 deploys/min sustained

Para atingir SLO L2 real, fazer **em ordem**:

### Fase 1 — Storage (3 horas)
1. Implementar Bug 21 (workspace emptyDir) — **Gargalos 1+2**

Ganho: 3 PVCs/min → ilimitado. Build setup 30s → ~instant.

### Fase 2 — Harness (4 horas)
2. Habilitar in-cluster k6 mode — **Gargalo 10**

Ganho: concurrency 5 → 15+. Sem port-forward bottleneck.

### Fase 3 — Compute (1 hora)
3. Resolver Bug 22 (Pulumi drift)
4. Aplicar `runtimeMaxNodes: 6` + `mgmtMaxNodes: 5` via Pulumi — **Gargalos 3+4**

Ganho: cluster tem headroom para burst.

### Validação
5. Rodar L2 v5 com fixes acima. Meta: ≥5 deploys/min sustained 15 min.

**Esforço total:** ~8h work + meetings.

---

## Para L3 (100 tenants) — Ação Adicional

Além do acima:
- **Gargalo 5** — bump build cluster nodes ou size
- **Gargalo 8** — wildcard cert ou DNS-01 Let's Encrypt
- **Gargalo 9** — imagePullSecret via TenantController
- **Gargalo 11** — admission ceilings para 50/cell

---

## Observações Sistêmicas

### Padrão: "Autoscaling helps only up to the Pulumi ceiling"

Autoscaler DO funciona — observamos node 3→4 em L2 v4. Mas o teto artificial no Pulumi (`maxNodes: 3/6`) sempre segura crescimento.

**Recomendação:** Setar `maxNodes` com folga generosa (3x uso médio previsto). Deixar autoscaler + `minNodes` decidir uso real. Custa nada se os nodes não forem usados.

### Padrão: "Shell watchers não são arquitetura"

Bug 9 (imagePullSecret watcher bash) é o arquétipo. Solução permanente é controller K8s nativo ou ESO. Watchers shell são aceitáveis temporariamente se:
- Documentados em memory/feedback
- Com ticket de follow-up
- Validados em cluster fresh pelo menos 1x/semana

### Padrão: "Rate limits são arquitetura, não operação"

Gargalos 6 (Docker Hub) e 8 (Let's Encrypt) ambos são rate limits externos. Solução não é "pagar mais" — é mudar arquitetura (mirror, wildcard, ou private provider).

---

## Commit & Review

Este documento deve:

1. Ser revisado em meeting técnico antes de priorização formal de L3
2. Virar backlog no issue tracker (GitHub Projects ou similar)
3. Gargalos 1+2+10 formam "SLO epic" — PR único pode fechar todos se workspace+harness andarem juntos

---

## Referências

- [`scale-50k-plan.md`](scale-50k-plan.md) — SLOs oficiais
- [`scale-50k-technical-debt.md`](scale-50k-technical-debt.md) — bugs overlap com alguns gargalos
- [`scale-50k-day2-learnings.md`](scale-50k-day2-learnings.md) — timeline L2
- [`scale-50k-l2-learnings.md`](scale-50k-l2-learnings.md) — L2 operational
- Memória: `feedback_pulumi_state_drift.md` — bloqueio atual para Gargalos 3, 4
