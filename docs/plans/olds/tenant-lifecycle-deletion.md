# Tenant Lifecycle & Deletion — Control Plane

> **Status:** Approved (MEET-2026-04-17-052)
> **Branch:** `feat/deletetenant`
> **Date:** 2026-04-17
> **Governance:** T3 — Meeting conducted, 6 agents, 3 security vetoes resolved
> **Meeting Record:** `docs/meetings/records/MEET-2026-04-17-052.md`

---

## Architecture Decision

The original TenantReaper proposal was **REJECTED** in MEET-052. The approved architecture is **controller-native with API intent gateway**.

**Rationale:**
- TenantReaper violated Four Planes architecture (API driving Control Plane lifecycle)
- Circuit breakers were at the wrong layer (Reaper, not provisioner clients)
- PG as work queue reimplements K8s controller-runtime primitives
- Dual source of truth (PG queue + K8s etcd) creates split-brain scenarios

---

## AS-IS — Sistema de Deletion de Tenant

Dois caminhos desconectados:

| Camada   | Entry point                   | Arquivo                                                | Tamanho    |
|----------|-------------------------------|--------------------------------------------------------|------------|
| API      | DELETE /api/v1/projects/:name | api/internal/services/project_deleter.go               | 269 linhas |
| Operator | Finalizer loopzero.io/tenant      | operators/internal/controller/tenant_controller.go:228 | 489 linhas |

Sem `DELETE /api/v1/tenants/:id`. O teste L3 fica stuck: 160 finalizers concorrentes sem backpressure.

---

## Problemas Sistumicos Identificados (6)

| #  | Problema                       | Status     |
|----|--------------------------------|------------|
| 1  | Sem backpressure               | FIXED: MaxConcurrentReconciles=5 |
| 2  | Sem visibilidade granular      | TODO: per-step metrics (Phase 5) |
| 3  | Sem circuit breaker            | TODO: provisioner HTTP clients (Phase 2) |
| 4  | Sem bulk ops                   | TODO: API bulk-delete (Phase 3) |
| 5  | Sem TTL/reaper                 | TODO: grace expiry goroutine (Phase 4) |
| 6  | Force-null hack recorrente     | TODO: proper soft-delete flow (Phase 3) |

---

## Approved Architecture — 3 Components

```
+-----------------------------------------------------------------+
|  1. API: DELETE /api/v1/tenants/:id                              |
|     - Writes intent to PG (tenants_pending_deletion)             |
|     - Sets annotation loopzero.io/deletion-requested on Tenant CR    |
|     - Returns 202 with deletion record                           |
|     - ?force=true for immediate (admin only, LOOPZERO_ADMIN_API_KEY) |
|     - POST /api/v1/tenants/:id/restore within grace window       |
|     - POST /api/v1/tenants/bulk-delete (admin, two-step HMAC)    |
|     - Grace expiry goroutine (leader-elected, heartbeat metric)  |
|       calls kubectl delete tenant when grace_until < NOW()       |
+-----------+-----------------------------------------------------+
            | kubectl delete tenant (rate-limited by grace expiry)
            v
+-----------------------------------------------------------------+
|  2. TenantController.reconcileDelete (hardened)                  |
|     - MaxConcurrentReconciles = 5 (configurable, DONE)           |
|     - Exponential backoff (5s base, 5min cap, max 10, DONE)     |
|     - Circuit breakers in provisioner HTTP clients (Phase 2)     |
|     - Per-step metrics (Phase 5)                                 |
|     - Callback to API on completion (authenticated)              |
+-----------+-----------------------------------------------------+
            | on finalizer removed (or max attempts alert)
            v
+-----------------------------------------------------------------+
|  3. API writes tombstone to PG + emits audit event               |
|     - tenant_tombstones table (permanent, PII anonymized at 2y)  |
|     - Reconciliation job detects missing tombstones (5min)       |
+-----------------------------------------------------------------+
```

---

## State Machine (Updated)

```
"" (empty)        --Create--> Provisioning
Provisioning      --AllConditionsReady--> Active
Provisioning      --Delete--> Deleting
Active            --SpecChanged--> Provisioning
Active            --Suspend--> Suspended
Active            --Delete--> Deleting           (force, no grace)
Active            --RequestDeletion--> PendingDeletion  (soft delete)
Suspended         --Unsuspend--> Active
Suspended         --Delete--> Deleting
Suspended         --RequestDeletion--> PendingDeletion
PendingDeletion   --GraceExpired--> Deleting     (grace elapsed)
PendingDeletion   --Delete--> Deleting           (force during grace)
PendingDeletion   --Restore--> Active            (cancel deletion)

Terminal: Deleting
```

---

## DB Schema (PostgreSQL)

```sql
CREATE TABLE tenants_pending_deletion (
  tenant_id       UUID PRIMARY KEY REFERENCES tenants(id),
  requested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  requested_by    TEXT NOT NULL,
  reason          TEXT NOT NULL,
  grace_until     TIMESTAMPTZ,
  force           BOOLEAN NOT NULL DEFAULT false,
  attempt_count   INT NOT NULL DEFAULT 0,
  last_error      TEXT,
  last_error_at   TIMESTAMPTZ
);
CREATE INDEX ON tenants_pending_deletion (grace_until) WHERE grace_until IS NOT NULL;

CREATE TABLE tenant_tombstones (
  tenant_id        UUID PRIMARY KEY,
  tenant_slug      TEXT NOT NULL,
  deleted_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  requested_by     TEXT NOT NULL,
  reason           TEXT NOT NULL,
  resources_cleaned JSONB NOT NULL,
  resources_failed  JSONB,
  anonymized_at    TIMESTAMPTZ  -- GDPR: PII hashed after 2 years
);
```

---

## API Surface

```
DELETE /api/v1/tenants/:id           -> 202 (soft delete, 24h grace)
DELETE /api/v1/tenants/:id?force=true -> 202 (admin only, immediate)
POST   /api/v1/tenants/:id/restore   -> 200 (cancel within grace)
POST   /api/v1/tenants/bulk-delete   -> two-step HMAC confirmation
GET    /api/v1/tenants/:id            -> includes deletion object when pending
POST   /internal/tenants/:id/deletion-complete -> authenticated callback
```

**Security requirements (MEET-052 vetoes):**
- `LOOPZERO_ADMIN_API_KEY` with dual-key rotation for force/bulk/admin ops
- Bulk-delete: HMAC-SHA256 confirmation token, 5min TTL, single-use, max batch 50
- Callback authenticated with `X-Service-Auth` (ServiceAuthSecret)
- IDOR protection: non-admin callers can only delete their own tenant

---

## CLI Surface

```bash
loopzero team delete           # self-service (24h grace)
loopzero team delete --force   # immediate (admin only)
loopzero team restore          # cancel within grace window

loopzero admin tenant evict    # force delete (admin)
loopzero admin tenant evict --selector "label=loadtest-l3"  # bulk
loopzero admin tenant list --pending-deletion
```

---

## Execution Phases

| Phase | Scope | Status |
|-------|-------|--------|
| 0 | Fix DeleteApplications NotFound idempotency | DONE |
| 1a | MaxConcurrentReconciles=5 for TenantController | DONE |
| 1b | Exponential backoff (5s-5min, max 10 attempts) | DONE |
| 1c | PendingDeletion state in tenant state machine | DONE |
| 2 | Circuit breakers in provisioner HTTP clients | TODO |
| 3 | API intent gateway (admin auth, endpoints, bulk) | TODO |
| 4 | Grace expiry goroutine + tombstone + reconciliation | TODO |
| 5 | Per-step metrics + 10 PrometheusRules | TODO |
| 6 | Audit events (9 lifecycle events) | TODO |
| 7 | CLI commands | TODO |
| 8 | Documentation update | DONE |
| 9 | 160-tenant envtest validation | TODO |

---

## Open Decisions (Resolved in MEET-052)

| # | Decision | Resolution |
|---|----------|-----------|
| 1 | Grace period default | 24h self-service, 0 for force/admin |
| 2 | GDPR tombstone | Hash requested_by after 2 years |
| 3 | TenantReaper deployment | REJECTED. Grace expiry goroutine in API (leader-elected) |
| 4 | Bulk authn/authz | LOOPZERO_ADMIN_API_KEY with dual-key rotation |
| 5 | Worker pool size | N/A — replaced by MaxConcurrentReconciles=5 |

---

## Phase 0 Note (Deprecated)

Phase 0 (`loopzero admin tenant evict` via direct kubectl) is a **temporary workaround**.
It is deprecated upon Phase 3 delivery (API intent gateway).
Do not build permanent workflows on Phase 0.
