# Stripe Production Setup — Operator Runbook

> **Last verified against Stripe dashboard:** 2026-06-24
> **Plan:** `stripe-l2-wiring-canonical-fix` (T4.1)
> **Recommended re-validation cadence:** every 6 months (Stripe redesigns UI periodically)

This runbook documents the sponsor-bound steps required to complete the Stripe wiring on dev-public after the engineering-side fixes (`stripe-l2-wiring-canonical-fix` Phases 1-3) ship. Engineering side materializes 2 K8s Secrets (`loopzero-stripe-webhook` + `loopzero-stripe-api-keys`) from `~/.loopzero/dev-credentials.json` and wires them into the `loopzero-cloud` Deployment via envFrom. The sponsor side creates Pro + Team products in the Stripe dashboard and adds the resulting price IDs to the same SoT JSON.

## Overview — what's wired vs what's pending

```
~/.loopzero/dev-credentials.json (mode 0600 SoT)
  └─ stripe.api_key           → wired by seed-stripe-api-keys-canonical.sh    → Secret loopzero-stripe-api-keys.STRIPE_API_KEY
  └─ stripe.webhook_secret    → wired by seed-stripe-secret-canonical.sh     → Secret loopzero-stripe-webhook.STRIPE_WEBHOOK_SECRET
  └─ stripe.price_pro    ⚠️    → SPONSOR-PENDING (create Pro product first)
  └─ stripe.price_team   ⚠️    → SPONSOR-PENDING (create Team product first)

loopzero-cloud Deployment envFrom (canonical):
  ├─ secretRef: loopzero-stripe-webhook    optional:true   → exposes STRIPE_WEBHOOK_SECRET
  └─ secretRef: loopzero-stripe-api-keys   optional:true   → exposes STRIPE_API_KEY
```

`live-proof-dev-public.sh --check=stripe` L2 emits PASS for the mandatory pair + SKIP for the pricing pair when this runbook has not yet been executed. SKIP is honest signal, not failure (Unbreakable Rule 3).

## Sponsor setup — Stripe dashboard

Sponsor performs these steps once per dev-public bootstrap (and again whenever pricing changes):

1. **Log in to Stripe dashboard** at `https://dashboard.stripe.com/` (use the **test mode** toggle in the top-right for dev-public; production cutover uses live mode separately).

2. **Create the Pro product:**
   - Navigate to **Products → Add product**
   - Name: `LoopZero Pro`
   - Description: `Per-tier subscription for LoopZero Pro plan (10 projects, progressive delivery, custom domains)`
   - Pricing: recurring, monthly, `$29.00 USD`
   - Click **Save product**
   - On the product detail page, copy the **Price ID** (format: `price_*`, e.g., `price_1AbCdEfGhIjKlMnOp...`)

3. **Create the Team product:**
   - Same flow as step 2 but with:
     - Name: `LoopZero Team`
     - Description: `Per-tier subscription for LoopZero Team plan (unlimited projects, RBAC, progressive delivery config)`
     - Pricing: recurring, monthly, `$99.00 USD`
   - Copy the **Price ID** (`price_*`).

4. **Update `~/.loopzero/dev-credentials.json`** under the `stripe` block:

   ```bash
   # Open the canonical SoT (mode 0600 enforced)
   $EDITOR ~/.loopzero/dev-credentials.json
   ```

   Add 2 new keys under the existing `stripe` object:

   ```json
   {
     "stripe": {
       "api_key": "rk_test_...",
       "publishable_key": "pk_test_...",
       "webhook_secret": "whsec_...",
       "price_pro":  "price_<paste from step 2>",
       "price_team": "price_<paste from step 3>"
     }
   }
   ```

   Save the file. Mode 0600 is preserved (do not run `chmod`).

## Verification

After updating the SoT JSON, run the canonical chain end-to-end:

```bash
# 1. Re-run the seed (idempotent; picks up new price_pro + price_team fields)
bash infra/scripts/local/seed-stripe-api-keys-canonical.sh --verify
bash infra/scripts/local/seed-stripe-secret-canonical.sh --verify

# 2. Restart loopzero-cloud to pick up env from updated Secrets
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout restart deploy/loopzero-cloud
kubectl --context k3d-loopzero-mgmt -n loopzero-system rollout status  deploy/loopzero-cloud --timeout=120s

# 3. Refresh admin JWT (15-min TTL)
bash infra/scripts/local/seed-admin-loopzero-dev.sh --refresh-only

# 4. Final acceptance probe
bash archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh --check=stripe
# expect:
#   PASS: L1 .env has STRIPE secret+publishable+webhook_secret_dev
#   PASS: L2 mandatory Stripe env vars (STRIPE_API_KEY + STRIPE_WEBHOOK_SECRET)
#   PASS: L2 advisory Stripe env vars (STRIPE_PRICE_PRO + STRIPE_PRICE_TEAM)
#   PASS: L3 /v1/billing/portal returns Stripe Customer Portal URL
#   [live-proof] summary PASS=4 SKIP=0 FAIL=0
```

When all 4 L1/L2/L3 lines report PASS and SKIP=0, the Stripe wiring is canonically complete.

## Rotation

Secrets rotate on different triggers per Stripe's runbook — that's why this plan separates them into 2 K8s Secrets (ADR D1):

| Secret | Triggers rotation | Action |
|---|---|---|
| `STRIPE_API_KEY` | Suspected compromise; scheduled key rotation | Generate new key in Stripe dashboard (Developers → API keys → Roll key) → update `~/.loopzero/dev-credentials.json.stripe.api_key` → re-run `seed-stripe-api-keys-canonical.sh --verify` → restart loopzero-cloud |
| `STRIPE_WEBHOOK_SECRET` | Endpoint re-creation; suspected webhook leak | Re-create endpoint in Stripe dashboard (Developers → Webhooks → reveal new signing secret) → update `~/.loopzero/dev-credentials.json.stripe.webhook_secret` → re-run `seed-stripe-secret-canonical.sh --verify` → restart loopzero-cloud |
| `STRIPE_PRICE_*` | Plan price change | Create new product version with updated price in Stripe dashboard → update IDs in `~/.loopzero/dev-credentials.json` → re-run both seeds + restart loopzero-cloud |

Both seeds are idempotent — re-running is always safe.

## Cross-references

- Plan: `.claude/knowledge-base/plans/stripe-l2-wiring-canonical-fix-plan.md`
- ADR D1 (separate Secrets): plan § ADRs
- ADR D3 (live-proof SKIP semantics): plan § ADRs
- Seed scripts: `infra/scripts/local/seed-stripe-api-keys-canonical.sh` + `infra/scripts/local/seed-stripe-secret-canonical.sh`
- LIVE-proof check: `archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh` § `check_stripe`
- SoT discipline: memory `feedback_secret_canonical_no_hardcode` (mode 0600 + no inline secrets)
