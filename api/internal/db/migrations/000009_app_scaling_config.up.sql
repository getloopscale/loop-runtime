-- CHG-2026-05-03-SCALE-REAL — Plan T2.1.
-- Persist per-app scaling configuration so `loopscale scale` becomes real (no longer 202 stub).
-- Replaces "edit loopzero.yaml + redeploy" workflow for replica + autoscaling changes.
--
-- Forward-only migration (LoopRuntime convention). No .down.sql.

CREATE TABLE app_scaling_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment IN ('production', 'staging', 'preview')),
    app_name TEXT NOT NULL,
    -- Replica bounds. min == max → fixed replicas. min < max → HPA scenario.
    min_replicas INTEGER NOT NULL CHECK (min_replicas >= 0),
    max_replicas INTEGER NOT NULL CHECK (max_replicas >= min_replicas),
    -- HPA target. Only meaningful when max > min.
    target_cpu_pct INTEGER CHECK (target_cpu_pct IS NULL OR target_cpu_pct BETWEEN 1 AND 100),
    -- KEDA HTTP scale-to-zero toggle. Requires min_replicas == 0.
    scale_to_zero BOOLEAN NOT NULL DEFAULT false,
    -- Audit metadata.
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL DEFAULT '',
    -- Idempotency hash: sha256 of canonical config body. Used to short-circuit
    -- unchanged requests with `applied: false, reason: "no change"`.
    config_hash TEXT NOT NULL DEFAULT '',

    -- One config row per (tenant, project, env, app).
    UNIQUE (tenant_id, project_id, environment, app_name)
);

-- Lookup index for the hot path: GetByApp (called on every loopruntime status / scale).
CREATE INDEX idx_app_scaling_config_lookup
    ON app_scaling_config (tenant_id, project_id, environment);

-- Cross-cutting check: scale_to_zero requires min_replicas == 0.
ALTER TABLE app_scaling_config
    ADD CONSTRAINT chk_scale_to_zero_requires_min_zero
    CHECK (NOT scale_to_zero OR min_replicas = 0);

COMMENT ON TABLE app_scaling_config IS
    'CHG-2026-05-03-SCALE-REAL — scaling config per app. Replaces loopzero.yaml-only path. Plan T2.1 of pre-launch-gaps-plan.md.';

COMMENT ON COLUMN app_scaling_config.config_hash IS
    'sha256 of canonical (min,max,target_cpu_pct,scale_to_zero) for idempotency short-circuit.';
