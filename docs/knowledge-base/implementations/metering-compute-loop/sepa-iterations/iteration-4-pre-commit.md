---
type: sepa-iteration
title: "SEPA pre-COMMIT — iter 4 / T2.1 (+ T1.2 folded) / migration 000046"
---

# SEPA pre-COMMIT — iter 4 / T2.1 (+ T1.2 folded) / migration 000046
MODE=VERBOSE. High-value schema task (SEPA initial brief flagged [MAJOR]: cursor PK no-period=SC-4, config seed false, RLS off).
## Findings (verified against the applied schema)
- INFO — metering_cursor PK=(tenant_id,metric,cell_id), ZERO period column → SC-4 period-independence lock structurally true (PG-REAL asserted).
- INFO — metering_config seeded enabled=false → EC-5 observe-only ships disabled (PG-REAL asserted).
- INFO — RLS DISABLED on both metering tables (relrowsecurity=f) → control-plane, by design + documented in the migration header.
- INFO — CHECK widened admits cpu_core_seconds/mem_gb_seconds; folded T1.2 drift guard GREEN (parses effective CHECK vs const set); down/up round-trip clean.
- INFO — GRANT loopzero_worker on the 2 new tables only (cost_counters/usage_records already granted in 000023). No 402/enforcement path. Observe-only intact.
## Recommended action
- Commit. Wiring a=defer(T2.3 consumes the tables) b=pass(TestMigration000046_Schema PG-REAL) c=n/a. No CHANGELOG (internal schema, not consumer-visible yet).
