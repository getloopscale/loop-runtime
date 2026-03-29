# Dependency Analysis Report

**Project:** LoopZero — CLI-first Kubernetes PaaS
**Analysis date:** 2026-03-29
**Phase:** 1 (Baseline)

---

## Summary

| Metric | Count |
|--------|-------|
| Go workspace modules | 5 (api, operators, pkg, infra/scripts, infra/tests) |
| Internal packages referenced outside their module | 3 (pkg used by api, operators, infra/tests) |
| External Go direct dependencies (api) | 12 |
| External Go direct dependencies (operators) | 7 |
| Node.js packages — CLI (direct) | 2 prod + 9 dev |
| Node.js packages — Pulumi (direct) | 5 prod + 3 dev |
| Node.js total (approx, incl. transitive) | ~197 (CLI), ~similar (Pulumi) |
| Helm charts managed | 13 |
| Circular dependencies (inter-module) | 0 |
| Intra-module coupling concern | 1 (services → routes type-sharing) |
| Unpinned version specifiers (Node.js) | ALL (^semver range, lock file present) |
| Unpinned Helm chart versions in Taskfile | 11 of 11 (no --version flags) |
| Missing go.sum files | 1 (infra/scripts) |
| Go version inconsistency | 1 (pkg at go 1.23, rest at 1.25.0) |
| Known CLAUDE.md documentation inaccuracy | 1 (go.work missing loopzero-forge, loopzero-packs) |

---

## Internal Dependency Graph

### Go Module Graph

```
pkg (github.com/loopzero/loopzero/pkg)
  └── no external deps; go 1.23
  └── provides: errors, logging, id, naming, health, statemachine

api (github.com/loopzero/loopzero/api)
  ├── → pkg (replace directive: ../pkg)
  ├── → github.com/getloopscale/loopzero-forge v0.2.1  [EXTERNAL — not in workspace]
  └── direct external: chi, pgx, prometheus, uuid, yaml, k8s.io

operators (github.com/loopzero/loopzero/operators)
  ├── → pkg (replace directive: ../pkg)
  └── direct external: ginkgo, gomega, k8s.io, controller-runtime

infra/tests (github.com/loopzero/loopzero/infra/tests)
  ├── → api (replace directive: ../../api)
  ├── → operators (replace directive: ../../operators)
  └── → pkg (replace directive: ../../pkg)

infra/scripts (github.com/loopzero/loopzero/infra/scripts)
  └── → infra/tests (replace directive: ../tests)
```

**Note:** `loopzero-forge` and `loopzero-packs` are referenced in `CLAUDE.md` as workspace modules, but they are NOT in `go.work`. `loopzero-forge` is consumed as an external published package (`github.com/getloopscale/loopzero-forge v0.2.1`). No `loopzero-packs` or `loopzero-forge` directory exists at the repository root.

### CLI TypeScript Module Graph (cli/src/)

```
index.ts
  ├── config/store.ts
  ├── config/constants.ts
  ├── api/client.ts → config/constants.ts
  ├── api/sse-client.ts → api/client.ts, config/constants.ts
  ├── output/formatter.ts
  ├── output/deploy-renderer.ts
  ├── detection/monorepo.ts  [Node.js fs/path only]
  ├── detection/framework.ts [Node.js fs/path only]
  ├── adapters/git.ts        [Node.js child_process/fs/path/os]
  ├── adapters/loopzero-yaml.ts  [Node.js fs/path; exports types]
  ├── adapters/app-unit.ts   → adapters/loopzero-yaml.ts (type import)
  ├── adapters/source-bundle.ts [Node.js fs]
  ├── adapters/source-upload.ts → api/client.ts, config/constants.ts
  ├── adapters/validate-config.ts → adapters/loopzero-yaml.ts (type import)
  └── commands/ → api/client.ts, output/*, adapters/*
```

No circular imports detected. The graph is a DAG. `api/client.ts` only imports from `config/constants.ts` (no reverse dependency).

### Intra-Module Coupling Concern in API

`api/internal/services/project_deleter.go` imports `api/internal/routes` to consume `ProjectDeleteInput` and `ProjectDeleteOutput` types. Go's `internal` package rules prevent external access, and there is no cycle (routes does not import services), but this is an architectural smell: a service layer consuming request/response types from the handler layer violates layered architecture. The comment in the file acknowledges this: "Separate from routes.ProjectStore to avoid circular dependency." The risk is currently contained.

```
routes (handler types + interfaces)
  ↑
  └── imported by: services/project_deleter.go [COUPLING CONCERN — service importing handler types]

adapters (wiring layer)
  ├── imports routes
  ├── imports services
  └── bridges both — this is the intended pattern
```

---

## External Dependency Inventory

### Go — API Module (github.com/loopzero/loopzero/api)

| Package | Version | Pinned? | Role |
|---------|---------|---------|------|
| github.com/go-chi/chi/v5 | v5.2.5 | Yes (exact) | HTTP router |
| github.com/google/uuid | v1.6.0 | Yes (exact) | UUID generation |
| github.com/jackc/pgx/v5 | v5.8.0 | Yes (exact) | PostgreSQL driver + pool |
| github.com/prometheus/client_golang | v1.23.2 | Yes (exact) | Prometheus metrics |
| github.com/loopzero/loopzero/pkg | v0.0.0 | replace directive | Internal shared lib |
| github.com/getloopscale/loopzero-forge | v0.2.1 | Yes (exact) | Argo Workflow builder SDK |
| gopkg.in/yaml.v3 | v3.0.1 | Yes (exact) | YAML parsing |
| k8s.io/api | v0.35.2 | Yes (exact) | Kubernetes API types |
| k8s.io/apimachinery | v0.35.2 | Yes (exact) | Kubernetes meta types |
| k8s.io/client-go | v0.35.2 | Yes (exact) | Kubernetes client |
| k8s.io/klog/v2 | v2.130.1 | Yes (exact) | Kubernetes logging |
| sigs.k8s.io/yaml | v1.6.0 | Yes (exact) | K8s YAML marshalling |

### Go — Operators Module

| Package | Version | Pinned? | Role |
|---------|---------|---------|------|
| github.com/onsi/ginkgo/v2 | v2.28.1 | Yes (exact) | BDD test framework |
| github.com/onsi/gomega | v1.39.1 | Yes (exact) | Matcher library |
| github.com/loopzero/loopzero/pkg | v0.0.0 | replace directive | Internal shared lib |
| k8s.io/api | v0.35.2 | Yes (exact) | K8s API types |
| k8s.io/apimachinery | v0.35.2 | Yes (exact) | K8s meta types |
| k8s.io/client-go | v0.35.2 | Yes (exact) | K8s client |
| sigs.k8s.io/controller-runtime | v0.23.3 | Yes (exact) | Operator framework |

### Node.js — CLI (@loopzero/cli)

| Package | Version Spec | Pinned? | Role |
|---------|-------------|---------|------|
| commander | ^12.1.0 | Semver range (lock file resolves) | CLI argument parsing |
| js-yaml | ^4.1.0 | Semver range (lock file resolves) | YAML parsing |
| typescript | ^5.5.0 | Dev — semver range | Compiler |
| jest | ^29.7.0 | Dev — semver range | Test runner |
| ts-jest | ^29.2.0 | Dev — semver range | Jest TypeScript transformer |
| eslint | ^9.39.4 | Dev — semver range | Linter |

Lock file (`package-lock.json`) present: reproducible builds guaranteed at install time.

### Node.js — Pulumi IaC (loopzero-infra)

| Package | Version Spec | Pinned? | Role |
|---------|-------------|---------|------|
| @pulumi/cloudflare | ^6.13.0 | Semver range (lock file resolves) | Cloudflare resources |
| @pulumi/command | ^1.2.1 | Semver range (lock file resolves) | Shell commands in IaC |
| @pulumi/digitalocean | ^4.40.0 | Semver range (lock file resolves) | DO infrastructure |
| @pulumi/kubernetes | ^4.18.0 | Semver range (lock file resolves) | K8s resources |
| @pulumi/pulumi | ^3.153.1 | Semver range (lock file resolves) | Pulumi SDK core |
| tar (override) | ^7.5.11 | Semver override (resolves to 7.5.13) | Security override — CVE mitigation |

Lock file present. The `tar` override in `package.json#overrides` forces top-level `tar` to v7.x, mitigating known vulnerabilities in tar v6.x. However, nested dependencies still declare `tar@^6.x` in their own package.json requirements; the override only affects the resolved version at install.

---

## Helm Chart Version Pinning Analysis

### CRITICAL FINDING: No `--version` flags in Taskfile Helm installs

All 11 `helm upgrade --install` commands in `Taskfile.yml` lack `--version` flags:

```
helm upgrade --install postgresql bitnami/postgresql ...      # no --version
helm upgrade --install gitea gitea-charts/gitea ...           # no --version
helm upgrade --install minio minio/minio ...                  # no --version
helm upgrade --install argocd argo/argo-cd ...                # no --version
helm upgrade --install argo-workflows argo/argo-workflows ... # no --version
helm upgrade --install cert-manager jetstack/cert-manager ... # no --version
helm upgrade --install traefik traefik/traefik ...            # no --version
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack ...  # no --version
helm upgrade --install otel-collector open-telemetry/opentelemetry-collector ... # no --version
helm upgrade --install loki grafana/loki ...                  # no --version
helm upgrade --install promtail grafana/promtail ...          # no --version
```

The `values.yaml` files pin application image tags for some charts (ArgoCD: `v2.13.3`, Gitea: `1.22.6`) but without `--version` in the Helm command the CHART VERSION itself is floating. A chart can change its default values, introduce new required fields, or change behavior between releases even when the application image tag is pinned.

**Impact:** Two separate environments running `task infra:install:mgmt` at different times may install different chart versions with different defaults. This breaks the "Local equals production" principle (Rule 10).

**Charts with application image pinned in values:**
- ArgoCD: `global.image.tag: "v2.13.3"`
- Gitea: `image.tag: "1.22.6"`

**Charts with NO image version in values:**
- PostgreSQL, MinIO, cert-manager, Traefik, monitoring, OTel, Loki, Promtail, external-secrets — fully floating chart and app versions

---

## Circular Dependency Analysis

### Go Modules — No Circular Dependencies
The module dependency graph is acyclic:
- `pkg` has no imports of other internal modules
- `api` imports `pkg` only
- `operators` imports `pkg` only
- `infra/tests` imports `api`, `operators`, `pkg`
- `infra/scripts` imports `infra/tests`

### Go Intra-Module — No True Cycles, One Smell
Within `api/internal`:
- `routes` imports: `middleware`, `validation`, `source` (all one-directional)
- `services` imports: `db`, `metrics`, `manifests`, `cfkv`, `routes` [SMELL]
- `adapters` imports: `routes`, `services`, `db`, `lock`, `sse` (wiring layer)

No actual import cycle exists. Go would fail to compile if there were one. The `services → routes` import is a layering violation (service consuming handler-layer types), not a circular import. It is documented in the source code with a comment.

### TypeScript — No Circular Dependencies
The CLI module graph is a clean DAG. No module imports from a module that depends on it.

---

## Version and Health Assessment

### Go Direct Dependencies

| Package | Version | Status | Notes |
|---------|---------|--------|-------|
| chi/v5 | v5.2.5 | Current | Active project, regularly maintained |
| pgx/v5 | v5.8.0 | Current | Well-maintained, major PostgreSQL driver for Go |
| prometheus/client_golang | v1.23.2 | Current | Official Prometheus client |
| loopzero-forge | v0.2.1 | Unknown | External package on different org (getloopscale vs loopzero) — single-maintainer risk |
| k8s.io/* | v0.35.2 | Current | Tracks Kubernetes 1.35 release |
| controller-runtime | v0.23.3 | Current | Tracks k8s 0.35.2 |
| ginkgo/v2 | v2.28.1 | Current | Actively maintained |

### Node.js Dependencies

| Package | Version Spec | Status | Notes |
|---------|-------------|--------|-------|
| commander | ^12.1.0 | Current | Stable, widely used |
| js-yaml | ^4.1.0 | Current | Maintained, no known recent CVEs |
| @pulumi/* | ^3-6.x range | Current | Actively developed |
| tar (override) | ^7.5.11 | Mitigated | Override applied to address v6 CVEs |

### loopzero-forge External Package Risk

`github.com/getloopscale/loopzero-forge v0.2.1` is consumed as a published external package from a different GitHub org (`getloopscale` vs `loopzero`). This package is used extensively in `api/internal/argo/` (6 files) to build Argo Workflow definitions — it is on the critical build path. Concerns:

1. The package is not in the Go workspace — local changes require a published version
2. Low version number (v0.2.1) suggests early-stage development
3. Different org name (getloopscale) vs project org (loopzero) — may be the same team, but unclear maintenance boundary
4. No documentation in the codebase of what happens if this package becomes unavailable

---

## Missing go.sum Analysis

`infra/scripts/go.sum` is absent. This module has one dependency: `infra/tests`. In a workspace context the workspace sum file (`go.work.sum`) at the root provides the checksums, so this is not a blocking issue. However, without `go.sum`, builds outside the workspace context (e.g., `cd infra/scripts && go build ./...`) would require re-fetching checksums.

---

## Documentation Discrepancy

`CLAUDE.md` lists the Go workspace as including modules: `api, operators, pkg, infra/scripts, infra/tests, loopzero-forge, loopzero-packs`. The actual `go.work` file contains only 5 modules: `api, infra/scripts, infra/tests, operators, pkg`. Neither `loopzero-forge` nor `loopzero-packs` exist as directories or are listed in `go.work`. This is a documentation drift that could mislead developers.

---

## License Analysis

| Component | License | Compatible with proprietary? |
|-----------|---------|------------------------------|
| CLI (package.json) | Apache-2.0 | Yes — own code |
| Pulumi IaC | Apache-2.0 | Yes — own code |
| commander | MIT | Yes |
| js-yaml | MIT | Yes |
| @pulumi/* | Apache-2.0 | Yes |
| go-chi/chi | MIT | Yes |
| pgx/v5 | MIT | Yes |
| prometheus/client_golang | Apache-2.0 | Yes |
| k8s.io/* | Apache-2.0 | Yes |
| controller-runtime | Apache-2.0 | Yes |
| ginkgo/gomega | MIT | Yes |
| tar (Node override) | BlueOak-1.0.0 | Yes — permissive |

No GPL/AGPL/SSPL dependencies detected. All licenses are permissive (MIT, Apache-2.0, BlueOak).

---

## Recommendations

### Priority 1 — Helm Chart Version Pinning (HIGH RISK)

Add `--version` flags to all 11 `helm upgrade --install` commands in `Taskfile.yml`. Example:

```
helm upgrade --install argocd argo/argo-cd --version 7.8.x ...
helm upgrade --install cert-manager jetstack/cert-manager --version 1.14.x ...
```

This is required for the "Local equals production" guarantee and reproducible infrastructure provisioning.

### Priority 2 — pkg Module Go Version Alignment (LOW RISK)

`pkg/go.mod` declares `go 1.23` while all consuming modules require `go 1.25.0`. While backwards compatible, this inconsistency can cause confusion and should be aligned:

```bash
cd /home/paulo/loopzero/pkg && go mod tidy  # or manually update go directive to 1.25.0
```

### Priority 3 — services → routes Layering Violation (MEDIUM RISK)

`api/internal/services/project_deleter.go` imports handler-layer types from `api/internal/routes`. This should be resolved by moving the shared types (`ProjectDeleteInput`, `ProjectDeleteOutput`) to a neutral package (e.g., `api/internal/domain` or `api/internal/types`). Currently harmless but will create friction during refactoring.

### Priority 4 — loopzero-forge External Dependency Risk Assessment (MEDIUM RISK)

Document the health and maintenance status of `github.com/getloopscale/loopzero-forge`. If this is a first-party package maintained by the same team, consider vendoring it or including it in the workspace. If it is truly external, establish a documented policy for what version to upgrade to and when.

### Priority 5 — CLAUDE.md Documentation Correction (LOW RISK)

Update `CLAUDE.md` to remove `loopzero-forge` and `loopzero-packs` from the Go workspace module list, as they do not exist as local modules. The current documentation misleads developers about the workspace structure.

### Priority 6 — infra/scripts go.sum (LOW RISK)

Generate a `go.sum` for `infra/scripts` to allow module-isolated builds outside the workspace:

```bash
cd /home/paulo/loopzero/infra/scripts && go mod tidy
```
