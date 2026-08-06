# SEPA pre-COMMIT — iter 5 / T2.2 / migration 000047
MODE=TIGHT. usage_records.quantity INTEGER->BIGINT (overflow guard for compute counters).
## Findings
- INFO — non-lossy widen; down is lossy-after-data + documented. PG-REAL test asserts data_type=bigint. Round-trip clean. No 402 path.
## Recommended action
- Commit. Wiring a=defer(T2.3 UpsertUsageRecord) b=pass c=n/a. No CHANGELOG (internal).
