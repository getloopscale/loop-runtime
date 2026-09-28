-- name: GetStripeCustomerIDForTenant :one
-- The billing customer a tenant's portal session is opened for. Not filtered
-- by subscription status: a tenant whose subscription was canceled still has
-- a customer, and the portal is where it resubscribes. A tenant may carry
-- several subscription rows over time; the most recently updated one with a
-- customer id wins, so the answer is deterministic.
SELECT stripe_customer_id::text AS stripe_customer_id
FROM subscriptions
WHERE tenant_id = @tenant_id::text AND stripe_customer_id IS NOT NULL
ORDER BY updated_at DESC
LIMIT 1;
