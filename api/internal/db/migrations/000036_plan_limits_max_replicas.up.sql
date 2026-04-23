-- Migration 000036: Add max_replicas_per_service to plan_limits
-- B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): cap per-service replica count
-- Forward-only: additive INSERT only, no schema change to plan_limits table.
--
-- Gate originally named this "000032_plan_limits_max_replicas.up.sql" but numbering
-- advanced to 000035 before this landed; using next-free index 000036.
--
-- Caps approved in MEET-2026-04-23-059:
--   starter     → 2  (Quota math: 2 replicas × 3 services × per-pod CPU/mem ≤ H7 ResourceQuota)
--   pro         → 5
--   team        → 10
--   enterprise  → NULL (unlimited, handled as short-circuit in PlanLimiter.CheckValue)

INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES
    ('starter',    'max_replicas_per_service', 2,    NULL),
    ('pro',        'max_replicas_per_service', 5,    NULL),
    ('team',       'max_replicas_per_service', 10,   NULL),
    ('enterprise', 'max_replicas_per_service', NULL, NULL)
ON CONFLICT (plan, metric) DO UPDATE
    SET hard_limit = EXCLUDED.hard_limit,
        soft_limit = EXCLUDED.soft_limit;
