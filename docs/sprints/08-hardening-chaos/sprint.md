# Sprint 8 — Hardening + Chaos

> **Weeks:** 24-25
> **System Design Reference:** §23 (Sprint 8)

---

## Objective

The platform survives everything we can throw at it. Every failure mode from section 12.1 has a passing test.

## Product Exit Gate

- Load test with 50 concurrent deploys passes.
- Every chaos scenario recovers automatically.

## CLI Commands Delivered

None — this sprint is hardening.

## Tasks

### Load testing

```
[ ] Load test: 50 concurrent deploys across 10 tenants
[ ] Load test: verify advisory locks under contention (no deadlocks)
[ ] Load test: verify ArgoCD under 200+ Applications
[ ] Load test: verify Gitea throughput under concurrent git operations
```

### Chaos testing

```
[ ] Chaos: every failure mode in section 12.1 has a passing test
[ ] Chaos: concurrent operations on same env (deploy + rollback + promote)
[ ] Chaos: compensation sweeper handles all orphan types
[ ] Chaos: API crash mid-deploy → build completes → release recoverable
[ ] Chaos: Gitea down during deploy → fallback path activates → mirror catches up
[ ] Chaos: ArgoCD degraded → circuit breaker opens → releases queue as READY
[ ] Chaos: PostgreSQL connection pool exhaustion → graceful degradation
[ ] Chaos: build node OOM during Kaniko → build fails cleanly, no orphans
```

### Error message compliance

```
[ ] Audit: all CLI error messages follow section 25.12 format
[ ] Audit: no internal terms (ArgoCD, advisory lock, gitops-push, CRD, tenant_id) in CLI output
[ ] Audit: all errors suggest next action
[ ] Audit: all CLI output uses product terminology (deploy, team, revert)
[ ] Audit: --json output matches schema defined in section 0.3
```

### Operational readiness

```
[ ] Break-glass procedure documented and tested
[ ] Runbooks per failure mode (PostgreSQL, Gitea, ArgoCD, MinIO, etcd)
[ ] On-call rotation and alert routing configured
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Failure modes & recovery (full matrix) | §12.1 |
| Recovery invariants | §12.2 |
| Cancellation & compensation | §11 |
| Conflict matrix | §4.4 |
| CLI Output & Error Contract | §0.3 |
| Error output examples | §25.12 |
| Forbidden internal terms | CLAUDE.md Rule 3 |
| Capacity planning thresholds | §14.5 |
| ArgoCD capacity (preview load) | §5.3 |
| Gitea throughput analysis | §7.5 |
| Break-glass procedure | §16.1 |
| TDD/BDD chaos tests | §18.4 |
| Test contracts by layer | §20 |
