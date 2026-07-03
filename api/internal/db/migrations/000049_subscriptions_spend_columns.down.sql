-- Reverse 000049: drop the Stripe-observed plan-price columns.
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS currency,
    DROP COLUMN IF EXISTS amount_minor;
