-- The memory utilization target of an autoscaled app, beside target_cpu_pct.
-- NULL means the HPA scales on CPU alone.
ALTER TABLE app_scaling_config
    ADD COLUMN target_memory_pct INTEGER
    CHECK (target_memory_pct IS NULL OR target_memory_pct BETWEEN 1 AND 100);
