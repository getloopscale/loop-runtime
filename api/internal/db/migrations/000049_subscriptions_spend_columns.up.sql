-- J-06 Overview backend (MEET-2026-07-03-003 / ADR 2026-07-03-j06-overview-backend-read-model-boundaries).
-- Persist the Stripe-observed recurring plan price on the subscription row so
-- GET /v1/billing can OBSERVE it (no Go price constant → no #157 drift; no live
-- Stripe call on the hot Overview read path). Written by the loop-console Stripe
-- webhook at customer.subscription.{created,updated} time.
--
-- Additive + nullable by design: pre-existing rows keep NULL, and GET /v1/billing
-- OMITS `plan_price` when the column is NULL (never a fabricated $0 for a paid
-- plan) — this protects the cross-repo window where the engine migration lands
-- before the loop-console writer/reader (ADR condition 8).
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS amount_minor BIGINT,
    ADD COLUMN IF NOT EXISTS currency TEXT;
