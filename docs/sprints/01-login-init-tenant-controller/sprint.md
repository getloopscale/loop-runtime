# Sprint 1 — `loopzero login` + `loopzero init` + TenantController

> **Weeks:** 3-5
> **System Design Reference:** §23 (Sprint 1)
> **Status:** COMPLETE (validated 2026-03-13)

---

## Objective

The CLI exists. A user can authenticate and initialize a project. `loopzero init` detects a monorepo, generates `loopzero.yaml`, and triggers TenantController to provision infrastructure.

## Product Exit Gate

- `loopzero login` → `loopzero init` works end-to-end. **VALIDATED** — real GitHub PAT, real PostgreSQL, real API
- User sees generated `loopzero.yaml`. **VALIDATED** — correct YAML with project name, apps, frameworks
- Gitea repo, namespaces, and RBAC exist in cluster. **VALIDATED** — 65 envtest specs pass (controller not deployed yet — Tilt in Sprint 2)

## CLI Commands Delivered

`login`, `logout`, `whoami`, `init`

## Tasks

### CLI scaffold

```
[x] CLI project structure (Commander)
[x] loopzero login: GitHub OAuth flow (browser open + token store)
[x] loopzero login --token <key>: CI/CD auth (no browser)
[x] loopzero logout, loopzero whoami
[x] loopzero init: monorepo detection (Turborepo, Nx, Lerna, pnpm/npm/yarn workspaces, flat)
[x] loopzero init: framework detection (10 frameworks from section 26.5)
[x] loopzero init: interactive wizard (app discovery → confirmation → loopzero.yaml generation)
[x] loopzero init --yes: non-interactive mode
[x] loopzero init: adds .env.local to .gitignore
[x] Unit tests: monorepo detection, framework detection, loopzero.yaml generation (137 tests)
```

### API — auth + project creation

```
[x] Go + net/http API server with DI (changed from Fastify — see ADR)
[x] GitHub OAuth token validation middleware (calls api.github.com/user)
[x] POST /api/v1/projects (triggered by loopzero init)
[x] API health check endpoints (/health + /health/deep with DB check)
```

### TenantController

```
[x] Ginkgo specs written BEFORE implementation (65 specs)
[x] Reconcile: Gitea repo, namespaces, ResourceQuota, LimitRange, RBAC
[x] Reconcile: NetworkPolicy (deny-all + allow-list + DNS + metadata block)
[x] Reconcile: PriorityClass, build namespace in build cluster
[x] Reconcile: ArgoCD Application
[x] Crash recovery: controller restart mid-reconcile resumes (condition-gated by ObservedGeneration)
[x] Integration tests: TenantController against envtest (65/65 PASS)
```

## Test Results

| Component | Tests | Status |
|-----------|-------|--------|
| CLI (Jest) | 137 | ALL PASS |
| API (Go) | ~30 | ALL PASS |
| Operators (Ginkgo/envtest) | 65 | ALL PASS |
| pkg (Go) | ~15 | ALL PASS |
| **Total** | **~247** | **ALL PASS** |

## Real Integration Tests (2026-03-13)

| # | Test | Expected | Result |
|---|------|----------|--------|
| 1 | `GET /api/v1/health` | 200 | PASS |
| 2 | `GET /api/v1/health/deep` | 200 database:connected | PASS |
| 3 | Correlation-ID auto-generated | UUID v4 header | PASS |
| 4 | Correlation-ID preserved | Client ID returned | PASS |
| 5 | `GET /whoami` no auth | 401 UNAUTHORIZED | PASS |
| 6 | `GET /whoami` invalid token | 401 | PASS |
| 7 | `GET /whoami` real GitHub PAT | 200 user+team+role | PASS |
| 8 | `POST /projects` create | 201 proj-* | PASS |
| 9 | `POST /projects` duplicate | 409 PROJECT_EXISTS | PASS |
| 10 | `POST /projects` invalid name | 400 RFC 1123 | PASS |
| 11 | `POST /projects` no auth | 401 | PASS |
| 12 | `loopzero login --token` real | exit 0, credentials saved | PASS |
| 13 | `loopzero whoami` | exit 0, email+team | PASS |
| 14 | `loopzero init --yes --name` | exit 0, loopzero.yaml+.gitignore+DB | PASS |
| 15 | `loopzero init` without --force | exit 1 | PASS |
| 16 | `loopzero init --force` | exit 0, overwrite | PASS |
| 17 | `loopzero init` no auth | exit 4 | PASS |
| 18 | `loopzero logout` | exit 0, config removed | PASS |
| 19 | Tenant CRD accepted by cluster | kubectl apply OK | PASS |

## Notes

- API technology changed from TypeScript+Fastify to Go+net/http (only CLI remains TypeScript)
- `dev:seed` Taskfile task created for seeding test user/tenant/membership
- CLI `validateToken` was fixed during integration testing: changed from `POST /auth/validate` (nonexistent) to `GET /api/v1/auth/whoami`
- TenantController operator is not deployed to cluster yet (will be via Tilt in Sprint 2) — all reconciliation logic validated via envtest

## Key References in System Design

| Topic | Section |
|-------|---------|
| CLI as primary interface | §0.2 |
| CLI Reference — Authentication | §25.1 |
| CLI Reference — Project initialization | §25.2 |
| Monorepo tool detection | §25.2 |
| loopzero.yaml complete specification | §26 |
| Supported frameworks (v1) | §26.5 |
| TenantController CRD + reconcile loop | §7.3.1 |
| Network isolation (workload + build) | §14.4 |
| Multi-tenancy & isolation | §14 |
| User onboarding flow | §24.2 |
