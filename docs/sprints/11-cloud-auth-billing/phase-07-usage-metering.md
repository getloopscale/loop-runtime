# Fase 7 — Usage Metering

> Reportar uso real (builds, storage, build minutes) ao Stripe para cobrança metered.

**Dependências:** Fase 6 (Plan enforcement — usage_records table e tracking existem).
**Estimativa:** 8-10h
**Risco:** MEDIO — Metering impreciso = cobrança errada. Precisa de reconciliação.

---

## Contexto

Planos Team e Enterprise têm soft limits com overage billing. O LoopZero reporta uso ao Stripe via Usage Records API. Stripe calcula o overage e cobra na invoice mensal.

Métricas metered:
- **Builds**: quantidade de builds no período
- **Build minutes**: minutos totais de build no período
- **Storage**: GB de artefatos no Spaces/Registry

---

## Tasks

### T7.1 — Usage tracking hooks

**O que fazer:**
- Incrementar `usage_records` quando eventos acontecem:
  - Build completado (SUCCEEDED ou FAILED) → incrementa `builds` e `build_minutes`
  - Source bundle uploaded → incrementa `storage_gb` (snapshot mensal)
- Hook no `BuildOrchestrator` ou no callback de build completion
- Operação atômica com o update de status do build (mesma transação)

**DoD:**
- [ ] Cada build completado incrementa `builds` count em `usage_records`
- [ ] Duration do build registrada como `build_minutes`
- [ ] Dentro da mesma transação que o status update
- [ ] Testes: build completa → usage_records incrementado

### T7.2 — Stripe usage reporting (cron)

**O que fazer:**
- Background job que roda a cada hora:
  1. Busca `usage_records` onde `reported_to_stripe = false`
  2. Para cada registro, chama Stripe Usage Records API
  3. Marca como `reported_to_stripe = true`
- Idempotente: se falhar no meio, re-run reporta apenas os não-reportados
- Stripe Usage Records: `POST /v1/subscription_items/{si}/usage_records` com `action: increment`

**DoD:**
- [ ] Background job roda a cada hora
- [ ] Reporta builds e build_minutes ao Stripe
- [ ] Idempotente (re-run seguro)
- [ ] Marca registros como reportados
- [ ] Teste: registros pendentes → Stripe API chamada → marcados
- [ ] Teste: registros já reportados → noop

### T7.3 — Storage metering (cron)

**O que fazer:**
- Background job mensal (ou diário com snapshot):
  1. Calcula storage total por tenant (source bundles + container images)
  2. Registra em `usage_records` como `storage_gb`
  3. Reporta ao Stripe
- Storage = tamanho dos bundles no Spaces + images no Registry

**DoD:**
- [ ] Storage calculado por tenant
- [ ] Registrado em `usage_records`
- [ ] Reportado ao Stripe mensalmente
- [ ] Teste: tenant com 2 GB → usage_records mostra 2

### T7.4 — CLI: `loopzero billing` com uso detalhado

**O que fazer:**
- Atualizar `loopzero billing` para mostrar uso metered:
  ```
  Plan: Pro
  Period: Apr 1 - Apr 30, 2026

  Usage:
    Builds:       47 / 1,000
    Build minutes: 128 min
    Storage:       1.2 GB

  Next invoice: ~$19.00
  ```
- `--json` com breakdown completo

**DoD:**
- [ ] `loopzero billing` mostra uso metered por métrica
- [ ] Mostra estimativa de próxima invoice
- [ ] `--json` inclui breakdown: `{ usage: { builds: { used, limit, overage }, ... } }`
- [ ] Testes unitários

### T7.5 — Reconciliação

**O que fazer:**
- Job semanal que compara `usage_records` com realidade:
  - Conta builds reais no DB vs `usage_records.builds`
  - Se divergência > 5%: alerta (log + metric)
  - Não corrige automaticamente (humano decide)
- Métrica Prometheus: `loopzero_billing_reconciliation_drift`

**DoD:**
- [ ] Job de reconciliação implementado
- [ ] Drift detectado e logado
- [ ] Métrica Prometheus emitida
- [ ] Alerta configurado para drift > 5%
- [ ] Teste: simula drift → métrica emitida

---

## Definition of Done — Fase 7

- [ ] Builds e build minutes rastreados automaticamente
- [ ] Storage calculado por tenant
- [ ] Uso reportado ao Stripe via Usage Records API (hourly)
- [ ] `loopzero billing` mostra uso detalhado
- [ ] Reconciliação semanal detecta drift
- [ ] Idempotência em todo o pipeline de metering
- [ ] Testes para cada hook, job, e report
