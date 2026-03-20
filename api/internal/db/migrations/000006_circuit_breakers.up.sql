-- Circuit breakers table for shared state across API replicas.
-- Each key represents an external dependency (e.g., "argocd", "gitea").
-- State transitions are managed by the application, not DB triggers.
CREATE TABLE IF NOT EXISTS circuit_breakers (
    key            TEXT PRIMARY KEY,
    state          TEXT NOT NULL DEFAULT 'closed' CHECK (state IN ('closed', 'open')),
    failure_count  INTEGER NOT NULL DEFAULT 0,
    success_count  INTEGER NOT NULL DEFAULT 0,
    last_failure   TIMESTAMPTZ,
    last_success   TIMESTAMPTZ,
    opened_at      TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed the ArgoCD circuit breaker key (idempotent)
INSERT INTO circuit_breakers (key, state) VALUES ('argocd', 'closed')
ON CONFLICT (key) DO NOTHING;
