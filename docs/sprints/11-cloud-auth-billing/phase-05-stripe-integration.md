# Fase 5 — Stripe Integration

> Integrar Stripe Billing para gerenciamento de subscriptions, customers, e payment methods.

**Dependências:** Fase 8 (Engine Migration — JWT aceito pelo engine). Na prática, pode começar em paralelo com Fase 8.
**Estimativa:** 12-16h
**Risco:** MEDIO — Stripe webhooks precisam de retry/idempotency. Sincronização de estado Stripe↔DB.

---

## Contexto

Stripe gerencia tudo de billing: customers, subscriptions, invoices, payment methods, e portal. O LoopZero apenas sincroniza estado via webhooks e enforça limites baseados no plano.

O flow: signup → Stripe Customer criado → subscription `starter` (free) criada automaticamente → upgrade via Stripe Checkout ou Customer Portal.

---

## Tasks

### T5.1 — Stripe products e prices

**O que fazer:**
- Criar no Stripe Dashboard (ou via API):
  - Product: "LoopZero Starter" (free)
  - Product: "LoopZero Pro" ($19/mo per seat)
  - Product: "LoopZero Team" ($49/mo per seat)
  - Metered price items: "Builds" (per unit), "Storage" (per GB/mo), "Build Minutes" (per min)
- Price IDs salvos como config (não hardcoded)

**DoD:**
- [ ] 3 products criados no Stripe (dev + prod environments)
- [ ] Prices configurados (flat rate + metered components)
- [ ] Price IDs em config/env vars (não hardcoded no código)
- [ ] Documentação: qual Price ID corresponde a qual plano

### T5.2 — Database: tabelas de billing

**O que fazer:**
- Migration com tabelas:
  - `subscriptions` — mirror do Stripe subscription (tenant_id, stripe_customer, stripe_sub, plan, status, period)
  - `usage_records` — tracking de uso por tenant por período (metric, quantity, reported_to_stripe)
  - `plan_limits` — limites por plano e métrica (hard_limit, soft_limit)
- Seed: inserir limites default para cada plano

**DoD:**
- [ ] Migration criada e aplicada
- [ ] Tabelas: `subscriptions`, `usage_records`, `plan_limits`
- [ ] Seed com limites de cada plano (starter, pro, team)
- [ ] Indexes adequados (tenant_id, period)
- [ ] Testes: store CRUD funciona

### T5.3 — Stripe customer creation no signup

**O que fazer:**
- Quando tenant é criado (Fase 4, T4.5): criar Stripe Customer automaticamente
- Customer metadata: `tenant_id`, `email`, `plan`
- Criar subscription `starter` (free) automaticamente
- Salvar `stripe_customer_id` e `stripe_subscription_id` na tabela `subscriptions`

**DoD:**
- [ ] Signup → tenant criado → Stripe Customer criado → subscription starter ativa
- [ ] `subscriptions` table populada
- [ ] Idempotente: re-run não duplica customer
- [ ] Teste: signup → verificar Stripe Customer existe (via API)

### T5.4 — Stripe webhook handler

**O que fazer:**
- `POST /v1/webhooks/stripe` — recebe eventos do Stripe
- Eventos a tratar:
  - `customer.subscription.updated` — plano mudou
  - `customer.subscription.deleted` — subscription cancelada
  - `invoice.paid` — pagamento confirmado
  - `invoice.payment_failed` — pagamento falhou
- Verificação de assinatura do webhook (Stripe-Signature header)
- Idempotência: processar mesmo evento N vezes sem efeito duplicado

**DoD:**
- [ ] Webhook endpoint implementado e acessível
- [ ] Signature validation (rejeita webhooks forjados)
- [ ] 4 eventos tratados corretamente
- [ ] Idempotente (same event ID → noop)
- [ ] Atualiza tabela `subscriptions` no DB
- [ ] Testes unitários para cada evento
- [ ] Teste: signature inválida → 400

### T5.5 — Stripe Customer Portal

**O que fazer:**
- `GET /v1/billing/portal` — retorna URL do Stripe Customer Portal
- O portal permite: upgrade/downgrade, atualizar payment method, ver invoices, cancelar
- Redirect URL: `app.loopzero.example/billing` (ou CLI message)

**DoD:**
- [ ] Endpoint retorna URL válida do Stripe Customer Portal
- [ ] Portal acessível e funcional
- [ ] Teste: request → URL retornada → acessível no browser

### T5.6 — CLI: `loopzero billing`

**O que fazer:**
- `loopzero billing` — mostra plano atual, uso, e link para portal
- `loopzero billing portal` — abre browser com Stripe Customer Portal
- Output: plan name, período, builds usados/limite, seats usados/limite

**DoD:**
- [ ] `loopzero billing` mostra plano e uso atual
- [ ] `loopzero billing portal` abre browser
- [ ] `--json` output: `{ plan, period_start, period_end, usage: { builds: { used, limit }, ... } }`
- [ ] Exit codes: 0 success, 4 auth error
- [ ] Testes unitários
- [ ] Zero termos internos (sem "Stripe", "subscription", "invoice")

---

## Definition of Done — Fase 5

- [x] Stripe Customer creation — StripeClient.CreateCustomer() + CreateSubscription() in cloud/internal/billing/stripe.go
- [x] Subscription starter — CreateSubscription with configurable price ID via env vars
- [x] Webhooks — 4 events handled: subscription.updated, subscription.deleted, invoice.paid, payment_failed
- [x] Webhook signature — VerifySignature() with HMAC-SHA256, 5min timestamp tolerance, 4 tests (valid, invalid, missing, expired)
- [x] Customer Portal — BillingHandler.Portal() returns portal session URL, `loopzero billing portal` opens browser
- [x] `loopzero billing` — shows plan + usage, --json output, 6 CLI tests
- [x] Billing tables — migration 000013: subscriptions, usage_records, plan_limits (40 seed rows), webhook_events
- [x] Idempotency — webhook_events table tracks processed event IDs, duplicate events are no-ops
- [x] Secrets — Pulumi config: stripeApiKey, stripeWebhookSecret (getSecret), env vars STRIPE_API_KEY, STRIPE_WEBHOOK_SECRET
- [x] Zero internal terms — "subscription"→"plan", "invoice"→"billing history" in all user-facing output
