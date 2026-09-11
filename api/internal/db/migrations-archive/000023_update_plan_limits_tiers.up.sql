-- Migration 000023: Update plan_limits to match pricing tiers
-- competitive-analysis-and-pricing
--
-- Starter (Free): 1 project, 1 env, 50 builds, 3 seats
-- Pro ($29/mo): 3 projects, 3 envs, 500 builds, 10 seats
-- Team ($79/mo): 10 projects, 10 envs, unlimited builds, 25 seats
-- Enterprise: all unlimited

-- Allow NULL in hard_limit (NULL = unlimited)
ALTER TABLE plan_limits ALTER COLUMN hard_limit DROP NOT NULL;

DELETE FROM plan_limits;

INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES
  -- Starter (Free)
  ('starter', 'projects', 1, NULL),
  ('starter', 'environments', 1, NULL),
  ('starter', 'builds_per_month', 50, NULL),
  ('starter', 'seats', 3, NULL),
  ('starter', 'services_per_project', 3, NULL),
  ('starter', 'secrets_per_env', 10, NULL),
  ('starter', 'ram_mb', 512, NULL),
  ('starter', 'vcpu_millicores', 500, NULL),
  ('starter', 'build_timeout_seconds', 600, NULL),
  ('starter', 'api_requests_per_min', 60, NULL),

  -- Pro ($29/mo)
  ('pro', 'projects', 3, NULL),
  ('pro', 'environments', 3, NULL),
  ('pro', 'builds_per_month', 500, NULL),
  ('pro', 'seats', 10, NULL),
  ('pro', 'services_per_project', 10, NULL),
  ('pro', 'secrets_per_env', 50, NULL),
  ('pro', 'ram_mb', 2048, NULL),
  ('pro', 'vcpu_millicores', 2000, NULL),
  ('pro', 'build_timeout_seconds', 1800, NULL),
  ('pro', 'api_requests_per_min', 300, NULL),

  -- Team ($79/mo)
  ('team', 'projects', 10, 15),
  ('team', 'environments', 10, NULL),
  ('team', 'builds_per_month', NULL, 5000),
  ('team', 'seats', 25, NULL),
  ('team', 'services_per_project', NULL, 20),
  ('team', 'secrets_per_env', 200, NULL),
  ('team', 'ram_mb', 8192, NULL),
  ('team', 'vcpu_millicores', 8000, NULL),
  ('team', 'build_timeout_seconds', 3600, NULL),
  ('team', 'api_requests_per_min', 1000, NULL),

  -- Enterprise (all unlimited)
  ('enterprise', 'projects', NULL, NULL),
  ('enterprise', 'environments', NULL, NULL),
  ('enterprise', 'builds_per_month', NULL, NULL),
  ('enterprise', 'seats', NULL, NULL),
  ('enterprise', 'services_per_project', NULL, NULL),
  ('enterprise', 'secrets_per_env', NULL, NULL),
  ('enterprise', 'ram_mb', NULL, NULL),
  ('enterprise', 'vcpu_millicores', NULL, NULL),
  ('enterprise', 'build_timeout_seconds', 7200, NULL),
  ('enterprise', 'api_requests_per_min', 5000, NULL);
