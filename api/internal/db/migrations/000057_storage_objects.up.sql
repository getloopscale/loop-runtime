-- M7 tenant blob storage: object metadata (bytes live in S3-compatible storage).
-- Engine-owned schema; loop-console reads/writes this table via the storage vertical.
-- Tenant isolation at the metadata layer is UNIQUE(tenant_id, name) — the bytes-
-- layer counterpart is the server-derived S3 key {tenant_id}/{name} (plan EC-1).
CREATE TABLE storage_objects (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   TEXT NOT NULL,
  name        TEXT NOT NULL,                        -- full path; virtual folders by '/'
  size_bytes  BIGINT NOT NULL CHECK (size_bytes >= 0),
  mime_type   TEXT NOT NULL DEFAULT 'application/octet-stream',
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'committed')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT storage_objects_tenant_name_uniq UNIQUE (tenant_id, name)
);

-- Prefix listing (folder drill-in) via text_pattern_ops.
CREATE INDEX storage_objects_tenant_prefix ON storage_objects (tenant_id, name text_pattern_ops);

-- Quota SUM(size_bytes) per tenant — index-only via INCLUDE.
CREATE INDEX storage_objects_tenant_usage ON storage_objects (tenant_id) INCLUDE (size_bytes);

-- NOTE (M7 metering scope): storage usage is metered via the Prometheus counter
-- loop_runtime_storage_bytes_written_total ONLY in the MVP. It is deliberately NOT written
-- to cost_counters yet — that would require a `storage_bytes` value in the engine's
-- contract-guarded cost_counters.metric CHECK (asserted == loop-runtime-contracts
-- plan.CostMetric constants by metric_check_drift_test.go). Coupling a storage
-- metric into the shared billing contract before storage billing exists is
-- premature (YAGNI + blueprint ADR D4). Deferred: when storage billing lands, add
-- MetricStorageBytes to loop-runtime-contracts + widen this CHECK in a follow-up migration.
