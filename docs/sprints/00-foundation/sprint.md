# Sprint 0 — Foundation + Minimal Observability

> **Weeks:** 1-2
> **System Design Reference:** §23 (Sprint 0)

---

## Objective

Local environment boots with `task setup`. Three clusters running. CRDs installed. Structured logging and basic Prometheus operational from day one. Unit tests pass.

## Product Exit Gate

- `task setup` succeeds.
- `task test:unit` passes.
- Grafana accessible at localhost with basic cluster health dashboard.

## CLI Commands Delivered

None — this sprint is infrastructure foundation.

## Tasks

```
[ ] Taskfile: clusters:create, infra:install, test:unit
[ ] k3d configs: mgmt, build, workload
[ ] CRDs: Tenant, PreviewEnvironment, AppSecret, ClusterCredential, ReconciliationReport
[ ] PostgreSQL schema: builds, releases, audit_log, idempotency_keys, sse_events, environment_targets
[ ] State machine types + transition validation (Go + TypeScript)
[ ] Unit tests: namespace naming, release ID, error types, state machine transitions
[ ] CI: unit tests + lint
[ ] Prometheus + Grafana installed in mgmt cluster (basic cluster health)
[ ] Structured logging (JSON) configured for all platform components from start
[ ] OpenTelemetry Collector installed (receives traces, even if no dashboards yet)
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| State machines (all 5) | §5 |
| Data model | §9 |
| Local infrastructure (k3d topology) | §19 |
| Taskfile | §19.2 |
| k3d configuration | §19.3 |
| Observability (metrics, dashboards, alerts) | §15 |
| Toolchain decisions | §22 |
