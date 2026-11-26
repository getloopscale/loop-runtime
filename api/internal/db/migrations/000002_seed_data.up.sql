-- LoopRuntime PaaS — Seed data for plan limits and circuit breakers.
-- These are platform configuration, not user data.

-- Circuit breaker defaults
INSERT INTO circuit_breakers (key, state) VALUES ('argocd', 'closed') ON CONFLICT (key) DO NOTHING;

-- Plan limits: Starter (Free, 1 project)
INSERT INTO plan_limits VALUES ('starter', 'projects', 1, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'environments', 1, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'builds_per_month', 50, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'seats', 3, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'services_per_project', 3, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'secrets_per_env', 10, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'ram_mb', 512, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'vcpu_millicores', 500, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'build_timeout_seconds', 600, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'api_requests_per_min', 60, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'custom_domains_per_env', 0, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('starter', 'max_replicas_per_service', 2, NULL) ON CONFLICT DO NOTHING;

-- Plan limits: Pro ($29/mo, 3 projects)
INSERT INTO plan_limits VALUES ('pro', 'projects', 3, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'environments', 3, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'builds_per_month', 500, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'seats', 10, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'services_per_project', 10, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'secrets_per_env', 50, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'ram_mb', 2048, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'vcpu_millicores', 2000, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'build_timeout_seconds', 1800, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'api_requests_per_min', 300, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'custom_domains_per_env', 3, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('pro', 'max_replicas_per_service', 5, NULL) ON CONFLICT DO NOTHING;

-- Plan limits: Team ($79/mo, 10 projects)
INSERT INTO plan_limits VALUES ('team', 'projects', 10, 15) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'environments', 10, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'builds_per_month', NULL, 5000) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'seats', 25, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'services_per_project', NULL, 20) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'secrets_per_env', 200, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'ram_mb', 8192, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'vcpu_millicores', 8000, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'build_timeout_seconds', 3600, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'api_requests_per_min', 1000, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'custom_domains_per_env', 10, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('team', 'max_replicas_per_service', 10, NULL) ON CONFLICT DO NOTHING;

-- Plan limits: Enterprise (custom)
INSERT INTO plan_limits VALUES ('enterprise', 'projects', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'environments', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'builds_per_month', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'seats', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'services_per_project', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'secrets_per_env', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'ram_mb', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'vcpu_millicores', NULL, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'build_timeout_seconds', 7200, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'api_requests_per_min', 5000, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'custom_domains_per_env', 999999, NULL) ON CONFLICT DO NOTHING;
INSERT INTO plan_limits VALUES ('enterprise', 'max_replicas_per_service', NULL, NULL) ON CONFLICT DO NOTHING;
