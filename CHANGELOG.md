# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/)
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- k3d cluster configurations for 3-cluster topology: mgmt, build, workload (#S0.1)
- Taskfile with full cluster lifecycle, Helm install, DB migration, and test tasks (#S0.2)
- Helm value files for all platform components: PostgreSQL, Gitea, MinIO, ArgoCD, cert-manager, Argo Workflows, Traefik, Prometheus, Grafana, OpenTelemetry Collector (#S0.3)
- CRD type definitions in Go: Tenant, PreviewEnvironment, AppSecret, ClusterCredential, ReconciliationReport (#S0.4)
- Drizzle ORM schema with 6 tables: builds, releases, environment_targets, audit_log, idempotency_keys, sse_events (#S0.5)
- State machines in Go and TypeScript for Build, Release, Preview, Promotion, Rollback — strict transitions, invalid transitions panic/throw (#S0.6, #S0.7)
- 274 unit tests: 55 Go (naming, IDs, errors, state machines, CRD types) + 219 TypeScript (schema, state machines, errors, correlation, logging) (#S0.8, #S0.9)
- Prometheus + Grafana monitoring stack with cluster health dashboard (#S0.10)
- OpenTelemetry Collector with OTLP receivers and Prometheus exporter (#S0.11)
- Structured JSON logging: Pino (TypeScript) + slog (Go) with correlation_id support (#S0.12)
- GitHub Actions CI pipeline with 4 parallel jobs: test-go, test-ts, lint-go, validate-manifests (#S0.13)
- Drizzle config for local PostgreSQL connection via NodePort (#S0.14)
