# Sprint 1 — `loopzero login` + `loopzero init` + TenantController

> **Weeks:** 3-5
> **System Design Reference:** §23 (Sprint 1)

---

## Objective

The CLI exists. A user can authenticate and initialize a project. `loopzero init` detects a monorepo, generates `loopzero.yaml`, and triggers TenantController to provision infrastructure.

## Product Exit Gate

- `loopzero login` → `loopzero init` works end-to-end.
- User sees generated `loopzero.yaml`.
- Gitea repo, namespaces, and RBAC exist in cluster.

## CLI Commands Delivered

`login`, `logout`, `whoami`, `init`

## Tasks

### CLI scaffold

```
[ ] CLI project structure (Commander/oclif)
[ ] loopzero login: GitHub OAuth flow (browser open + token store)
[ ] loopzero login --token <key>: CI/CD auth (no browser)
[ ] loopzero logout, loopzero whoami
[ ] loopzero init: monorepo detection (Turborepo, Nx, Lerna, pnpm/npm/yarn workspaces, flat)
[ ] loopzero init: framework detection (10 frameworks from section 26.5)
[ ] loopzero init: interactive wizard (app discovery → confirmation → loopzero.yaml generation)
[ ] loopzero init --yes: non-interactive mode
[ ] loopzero init: adds .env.local to .gitignore
[ ] Unit tests: monorepo detection, framework detection, loopzero.yaml generation
```

### API — auth + project creation

```
[ ] Fastify app with plugins
[ ] JWT authentication middleware (GitHub OAuth token validation)
[ ] POST /projects (triggered by loopzero init)
[ ] API health check endpoint
```

### TenantController

```
[ ] Ginkgo specs written BEFORE implementation
[ ] Reconcile: Gitea repo, namespaces, ResourceQuota, LimitRange, RBAC
[ ] Reconcile: NetworkPolicy (deny-all + allow-list + DNS + metadata block)
[ ] Reconcile: PriorityClass, build namespace in build cluster
[ ] Reconcile: ArgoCD Application
[ ] Crash recovery: controller restart mid-reconcile resumes
[ ] Integration tests: TenantController against envtest
```

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
