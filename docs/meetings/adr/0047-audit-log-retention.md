# ADR-0047: audit_log retention policy

**Date:** 2026-05-09
**Status:** Accepted
**Plan:** docs/plans/deep-review-remediation-plan.md (T7.3 + EC-15)
**Closes:** find_data_005 MEDIUM (partial — index landed in migration 000013;
prune job is operational work)

---

## Context

The `audit_log` table is declared append-only via
`trg_audit_log_immutable` trigger (DELETE/UPDATE raise EXCEPTION). Migration
000001 creates the table but provides no retention strategy:

- No prune job
- No time-based index (until migration 000013)
- No documented retention period

Without retention, the table grows unbounded. At ~10k events/day across
all tenants (Sprint 11 estimate), the table reaches 4M rows / ~2GB after
1 year. Sequential scans for time-range queries become unacceptable.

EC-15 of edge-case review surfaced an ADJACENT compliance question:
financial events (billing, payment, subscription changes) may be required
by regulators (PCI Level 1, SOC 2 Type II) to be retained for 7 years.

## Decision

**Retention policy: 1 year for security/operational events, with explicit
delegation of financial events to Stripe.**

1. **LoopZero audit_log retention: 1 year.** All rows older than 1 year are
   archived to S3 (DO Spaces) and deleted from the active table. Quarterly
   archival snapshots keep the active table bounded.

2. **Financial events delegated to Stripe.** Subscription changes,
   payment events, and refunds are NOT logged to audit_log — they flow
   through Stripe webhooks → subscriptions table. Stripe retains these
   events per their SLA (typically 7+ years). LoopZero audit_log captures
   the **intent** (who triggered the change) but NOT the financial
   details that compliance regulators require.

3. **Index in migration 000013** — BRIN index on `created_at` enables
   efficient time-range scans for the prune job + ad-hoc queries.

4. **Prune job operational work** — implementation tracked separately
   (api/internal/services/audit_log_pruner.go is the planned home).
   This ADR documents the policy; the implementation lands in a future
   PR after operations review.

## Rationale

| Alternative | Rejected because |
|---|---|
| 7-year retention for ALL events | Storage cost; operational complexity; not required since financial events flow through Stripe |
| 90-day retention | Too short for security incident investigation (typical postmortem window is 6mo) |
| Indefinite retention | Storage scaling problem; index degradation; no compliance benefit |
| BTREE index instead of BRIN | BRIN is ~1000x smaller for append-only time-series data — preferred |

## Consequences

**Enables:**
- Bounded table size at ~10k events/day × 365 days = 3.65M rows (~2GB).
- Compliance answer: financial retention is Stripe's responsibility
  (cited in compliance audits with link to this ADR).

**Constrains:**
- Prune job MUST be operational before Sprint 9 launch — failing to ship
  it leaves the unbounded growth.
- Operators investigating incidents older than 1 year MUST query the
  S3 archive (separate procedure documented in
  docs/runbooks/audit-log-archive-query.md — pending).

## Re-evaluation triggers

This ADR should be revisited if:
- LoopZero onboards a regulator (HIPAA, PCI-Level-1) requiring on-platform
  retention of operational events.
- Active table size exceeds 10GB despite the prune job.
- A real security incident postmortem needs >1 year of audit data and
  the S3 archive proves operationally inadequate.

## References

- find_data_005 (deep-review-remediation T7.3)
- EC-15 of edge-case-plan review
- migration 000013 (BRIN time-index)
