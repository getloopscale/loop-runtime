# ADR 0001: Transaction-scoped advisory locks

## Status
Accepted

## Context
LoopZero needs serialization for concurrent builds on the same (tenant, project, environment). PostgreSQL offers two advisory lock types: session-scoped (`pg_advisory_lock`) and transaction-scoped (`pg_advisory_xact_lock`).

## Decision
Use `pg_advisory_xact_lock` exclusively. Session-scoped locks persist across transactions and break with connection pooling (pgbouncer, pgxpool) — the lock outlives the intent.

## Consequences
- Locks release automatically on commit, rollback, or disconnect
- No deadlock risk from forgotten locks
- 5-second lock timeout prevents contention cascading
- Cannot hold locks across multiple transactions (acceptable — our mutations are single-transaction)
