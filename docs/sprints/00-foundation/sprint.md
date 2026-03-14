# Sprint 0 — Foundation + Minimal Observability

> **Weeks:** 1-2
> **System Design Reference:** §23 (Sprint 0)
> **Status:** COMPLETE (validated 2026-03-13)

---

## Objective

Local environment boots with `task setup`. Three clusters running. CRDs installed. Structured logging and basic Prometheus operational from day one. Unit tests pass.

## Product Exit Gate

- `task setup` succeeds. **VALIDATED** — 3 k3d clusters + PostgreSQL + Gitea + MinIO + ArgoCD + cert-manager + Traefik + Prometheus + OTel
- `task test:unit` passes. **VALIDATED** — all unit tests across pkg, operators, api, cli pass
- Grafana accessible at localhost with basic cluster health dashboard. **VALIDATED** — kube-prometheus-stack installed in mgmt cluster

## CLI Commands Delivered

None — this sprint is infrastructure foundation.

## Tasks

```
[x] Taskfile: clusters:create, infra:install, test:unit
[x] k3d configs: mgmt, build, workload
[x] CRDs: Tenant, PreviewEnvironment, AppSecret, ClusterCredential, ReconciliationReport
[x] PostgreSQL schema: builds, releases, audit_log, idempotency_keys, sse_events, environment_targets
[x] State machine types + transition validation (Go)
[x] Unit tests: namespace naming, release ID, error types, state machine transitions
[x] CI: unit tests + lint (.github/workflows/ci.yml)
[x] Prometheus + Grafana installed in mgmt cluster (kube-prometheus-stack)
[x] Structured logging (JSON) configured for all platform components (pkg/logging)
[x] OpenTelemetry Collector installed (infra/helm/otel/values.yaml)
```

## Notes

- State machines implemented in Go only (not TypeScript) — CLI does not need state machine logic, only the API and operators do
- Migration 000002 added in Sprint 1 for users, tenants, tenant_members, projects tables
- All 5 CRDs installed: tenants.loopzero.io, previewenvironments.loopzero.io, appsecrets.loopzero.io, clustercredentials.loopzero.io, reconciliationreports.loopzero.io

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
