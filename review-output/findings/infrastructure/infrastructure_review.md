# Infrastructure Review

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero
**Reviewer:** infrastructure-reviewer
**Phase:** 5

---

## Executive Summary

- **Overall infrastructure health: NEEDS ATTENTION**
- **Total findings: 16** (critical: 0, high: 4, medium: 7, low: 5)
- **Key concern:** The build-images pipeline can publish container images to the production registry without CI tests passing, no image signing exists, and all GitHub Actions use mutable tag references rather than immutable SHA pins.

---

## Artifacts Discovered

- Dockerfiles: 2 (api/Dockerfile, operators/Dockerfile)
- Kubernetes manifests: 2 Tilt deployment manifests + 1 NetworkPolicy manifest + 4 PDB templates
- Docker Compose files: 0
- Terraform files: 0 (Pulumi TypeScript used instead)
- Helm charts: 12 (argocd, argo-workflows, cert-manager, external-secrets, gitea, loki, minio, monitoring, network-policies, otel, postgresql, promtail, traefik)
- GitHub Actions workflows: 4 (ci.yml, build-images.yml, infra-deploy.yml, infra-preview.yml)

---

## Dockerfile Assessment

### api/Dockerfile

| Check | Result |
|-------|--------|
| Multi-stage build | PASS — golang:alpine builder + alpine:3.21 runtime |
| Base image pinned | PARTIAL — runtime alpine:3.21 ok, builder golang:1.25-alpine missing patch+Alpine version |
| Base image minimal | PASS — Alpine for both stages |
| Non-root user | PASS — adduser -D -u 1000 loopzero; USER 1000 |
| .dockerignore exists | FAIL — no .dockerignore at repo root |
| No secrets in build args/ENV | PASS |
| COPY before RUN (layer caching) | PASS — go.mod copied before source |
| HEALTHCHECK directive | FAIL — no HEALTHCHECK present |
| EXPOSE matches actual port | PASS — EXPOSE 3001 matches deployment |
| No unnecessary packages | PARTIAL — installs git (needed for migrations, acceptable) |
| ENTRYPOINT exec form | PASS — CMD ["/loopzero-api"] |
| OCI image labels | FAIL — no LABEL directives |
| Temp cleanup | N/A |

### operators/Dockerfile

| Check | Result |
|-------|--------|
| Multi-stage build | PASS |
| Base image pinned | PASS — golang:1.25.3-alpine3.21 builder + alpine:3.21.3 runtime |
| Base image minimal | PASS |
| Non-root user | PASS — adduser -D -u 1000 loopzero; USER 1000 |
| .dockerignore exists | FAIL — no .dockerignore at repo root |
| No secrets in build args/ENV | PASS |
| HEALTHCHECK directive | PASS — wget-based healthcheck on :8081/healthz |
| EXPOSE matches actual ports | PASS — EXPOSE 8080 8081 matches deployment ports |
| No unnecessary packages | PASS — only ca-certificates |
| ENTRYPOINT exec form | PASS — ENTRYPOINT ["tenant-controller"] |
| OCI image labels | PASS — three OCI labels present |

---

## Kubernetes / Tilt Manifest Assessment

### infra/tilt/api-deployment.yaml

| Check | Result |
|-------|--------|
| Resource requests AND limits | PASS — cpu: 100m/500m, memory: 64Mi/256Mi |
| Liveness probe | PASS — GET /api/v1/health :3001 |
| Readiness probe | PASS — GET /api/v1/health :3001 |
| Startup probe | ABSENT — acceptable (fast startup) |
| Security context: runAsNonRoot | PASS — pod and container level |
| Security context: readOnlyRootFilesystem | PASS |
| Security context: drop capabilities | ABSENT — capabilities.drop not configured |
| PodDisruptionBudget | ABSENT — no PDB for loopzero-api in dev manifests |
| HPA | ABSENT |
| Service account | PASS — dedicated loopzero-api SA with minimal RBAC |
| Credentials in env vars | FAIL — DATABASE_URL, MINIO_SECRET_KEY, LOOPZERO_ARGO_CALLBACK_SECRET hardcoded |
| Replicas | 1 — single instance, no HA |

### infra/tilt/operator-deployment.yaml

| Check | Result |
|-------|--------|
| Resource requests AND limits | PASS — cpu: 100m/500m, memory: 64Mi/256Mi |
| Liveness probe | PASS — GET /healthz :8081 |
| Readiness probe | PASS — GET /readyz :8081 |
| Security context: runAsNonRoot | PASS — pod and container level |
| Security context: readOnlyRootFilesystem | PASS |
| Security context: drop capabilities | ABSENT |
| PodDisruptionBudget | ABSENT in Tilt manifests |
| Leader election | FAIL — --leader-elect=false hardcoded in args |
| Replicas | 1 — single instance |

---

## Helm Charts Assessment

### infra/helm/traefik/values.yaml

| Check | Result |
|-------|--------|
| Resource limits | PASS — cpu 100m/500m, memory 128Mi/256Mi |
| Security context | PASS — runAsNonRoot: true |
| Liveness/readiness probes | PASS — chart defaults (/ping on :9000) |
| Replicas | PASS — 2 replicas |
| PodDisruptionBudget | PASS — enabled, minAvailable: 1 |
| Pod anti-affinity | PASS — preferredDuringScheduling on hostname |
| HTTP to HTTPS redirect | PASS — web.redirectTo.port: websecure |
| Access logging | PASS — JSON format, error codes only |

### infra/helm/argocd/values.yaml

| Check | Result |
|-------|--------|
| Resource limits | PASS — all four components have limits set |
| Security context | PASS — runAsNonRoot on all components |
| Liveness/readiness probes | PASS — enabled on all components |
| Image tag | PASS — global.image.tag: v2.13.3 (pinned) |
| RBAC | PASS — least privilege (sync+get only for loopzero-api account) |
| TLS | FAIL — server.insecure: true (plain HTTP within cluster) |
| Auto-sync | PASS — disabled (manual sync only) |
| Notifications | PASS — webhook to loopzero-api callback endpoint |

### infra/helm/postgresql/values.yaml

| Check | Result |
|-------|--------|
| Resource limits | PASS — cpu 100m/500m, memory 128Mi/512Mi |
| Security context | PASS — runAsNonRoot on pod and container |
| Liveness/readiness probes | PASS — enabled |
| Replication | FAIL — single primary, no replicas |
| Backup | FAIL — no backup schedule configured |
| Password | FAIL — loopzero-dev-password committed in values |
| PVC size | WARNING — 1Gi (insufficient for production) |

### infra/helm/minio/values.yaml

| Check | Result |
|-------|--------|
| Resource limits | PASS |
| Security context | PASS — runAsNonRoot |
| Probes | PASS |
| HA mode | FAIL — standalone (no replication) |
| Admin password | FAIL — loopzero-dev-password committed |
| Console exposure | PASS — ClusterIP only |

### infra/helm/argo-workflows/values.yaml

| Check | Result |
|-------|--------|
| Resource limits | PASS — controller 500m-2cpu/512Mi-1Gi, server 50m-200m/64Mi-128Mi |
| Security context | PASS — runAsNonRoot on controller and server |
| Probes | PASS — liveness and readiness on both |
| Parallelism limits | PASS — workflowWorkers: 32, podWorkers: 32 |
| MinIO insecure | FAIL — minio.insecure: true (dev only; must be false in prod) |
| Pod GC | PASS — deleteDelayDuration: 4h for log collection |

### infra/helm/external-secrets/values.yaml

| Check | Result |
|-------|--------|
| Resource limits | PASS |
| Security context | FAIL — not configured |
| Probes | FAIL — not configured (relies on chart defaults only) |
| Replicas | ABSENT — no replicaCount override |

---

## CI/CD Pipeline Assessment

### ci.yml (22+ jobs)

**Strengths:**
- Comprehensive coverage: lint (Go + TS), mod tidy, tests (pkg, operators, api, infra, CLI, Pulumi), manifest validation, govulncheck, npm audit, Trivy (fs + IaC + Dockerfile), TruffleHog, build verification
- ci-gate job aggregates all results as a single required status check
- Race detector enabled on all Go tests (-race)
- Concurrency groups cancel in-progress runs per branch
- Matrix strategy with fail-fast: false avoids one failure masking others

**Gaps:**
- All `uses:` references are tag-pinned, not SHA-pinned (find_infra_001)
- govulncheck and setup-envtest installed at @latest (find_infra_014)
- ci-gate does not gate build-images.yml (find_infra_004)

### build-images.yml

**Strengths:**
- SHA tag published alongside mutable tag (provides immutable reference)
- Registry integration with DOKS clusters automated post-build
- buildkit-registry-config secret maintenance automated

**Gaps:**
- Triggers independently of ci.yml — no gate on CI passing (find_infra_004)
- main branch produces :latest tag (find_infra_002)
- No cosign signing after push (find_infra_003)
- No SBOM generation
- digitalocean/action-doctl@v2 — no patch version specified (find_infra_001)

### infra-deploy.yml

**Strengths:**
- Production deploy requires GitHub Environment approval (infra-prod environment)
- concurrency.cancel-in-progress: false prevents orphaned infra resources
- 3-step deploy (infra -> images -> converge) is well-structured
- Cloudflare and DO resource cleanup is thorough in destroy path
- VPC, bucket, and CDN import logic handles state divergence

**Gaps:**
- No dependency on CI workflow passing before deploy
- All actions tag-pinned only (find_infra_001)
- destroy command available via workflow_dispatch without additional approval

### infra-preview.yml

**Strengths:**
- Runs Pulumi validate (typecheck + tests) before preview
- Posts PR comment with execution plan
- Concurrency group prevents parallel previews for same PR

**Gaps:**
- All actions tag-pinned only (find_infra_001)
- pulumi/actions@v6 without SHA pin

---

## Supply Chain Assessment

| Item | Status |
|------|--------|
| GitHub Actions SHA pinning | FAIL — all actions use mutable tags |
| Container image signing (cosign) | NOT IMPLEMENTED — planned Sprint 6 |
| SBOM generation (syft) | NOT IMPLEMENTED — planned Sprint 6 |
| Image provenance (OCI labels) | PARTIAL — operators/Dockerfile has labels, api/Dockerfile does not |
| Dependency scanning (govulncheck) | PASS — runs on every CI push |
| Dependency scanning (npm audit) | PASS — audit-level: high |
| Filesystem vulnerability scanning (Trivy) | PASS — CRITICAL,HIGH severity, exit-code: 1 |
| IaC scanning (Trivy config) | PASS — runs alongside fs scan |
| Secret detection (TruffleHog) | PASS — --only-verified with full git history |
| Lockfile enforcement | PASS — npm ci used everywhere, go.sum verified by tidy check |
| Tool version pinning (CI) | PARTIAL — Go/Node versions pinned via env vars, govulncheck at @latest |

---

## Single Points of Failure Analysis

| Component | Failure Impact | Mitigation Present |
|-----------|---------------|-------------------|
| PostgreSQL (primary) | All API state unavailable — 100% outage | PDB template; production uses managed DO DB (external HA); dev single-node |
| Gitea | Git push fails; ArgoCD cannot sync | PDB template; single replica in dev |
| MinIO (dev) | Build pipeline fails | None in dev; production uses DO Spaces |
| ArgoCD | Deployments cannot sync to workload cluster | PDB template; notifications callback breaks |
| loopzero-api | All CLI commands fail | Single replica in dev; no HPA |
| tenant-controller | Tenant provisioning fails; no new environments | Single replica, leader-elect=false in dev |
| Cloudflare Tunnel | All user traffic to workload cluster blocked | Two connections configured in production |

**Assessment:** The dev/Tilt configuration is appropriately simplified for local development. The production path (via Pulumi + DO managed services) addresses most SPOFs, but the Helm chart defaults committed to the repo do not reflect the production configuration, creating a gap between what is visible in the repo and what actually runs in production.

---

## Runtime Configuration Assessment

The API configuration (api/internal/config/config.go) is well-structured:
- 50+ config fields with explicit production validation
- Database pool configuration complete (max/min conns, idle time, lifetime, health check)
- No credentials in the config package itself — all sourced from env vars or K8s secrets
- HTTP timeouts properly configured

**Gap:** No `.env.example` file exists at repository root. Developers must discover required variables from config.go or the Tilt deployment manifests.

---

## Database Migration Safety Assessment

All 8 migrations are forward-only with no destructive DDL statements. Each migration uses:
- `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` — fully idempotent
- `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` — safe for re-runs
- Migration 000003 uses a partial unique index for concurrent build enforcement (spec §4.2 compliance)
- Migration 000007 adds covering indexes for the most common query patterns

**Assessment:** Migration strategy is sound. All migrations are safe to re-run. Index coverage is adequate for the declared query patterns.

---

## Findings (Detailed)

### INFRA-001: No SHA pinning for GitHub Actions — all actions use mutable tag references

- **Severity:** high
- **Area:** CI/CD supply chain
- **Location:** .github/workflows/ci.yml:37, build-images.yml:112, infra-preview.yml:121
- **Description:** All four workflows use mutable semver tags (e.g., @v4, @v5, @0.35.0, @v6) for every `uses:` reference. A compromised or force-pushed tag injects code into CI runs that build and push to the production container registry.
- **Evidence:** No SHA digests found in any workflow file. `digitalocean/action-doctl@v2` has no patch version at all.
- **Recommendation:** Pin all third-party actions to full commit SHAs. Use Dependabot or StepSecurity harden-runner to automate and maintain SHA pins. Example: `actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683`

---

### INFRA-002: build-images.yml pushes :latest tag from main branch

- **Severity:** high
- **Area:** CI/CD, container registry
- **Location:** .github/workflows/build-images.yml:66-69
- **Description:** Production images are tagged `:latest` alongside a SHA tag. If production deployments reference `:latest`, rollback is impossible and builds cannot be reproduced deterministically.
- **Evidence:** `case main) TAG="latest" ;;` — both `loopzero-api:latest` and `tenant-controller:latest` are pushed on every main branch push.
- **Recommendation:** Remove `:latest` from production builds. Use only the immutable SHA tag. If a human-readable tag is needed, use an explicit semver tag (`v1.2.3`) set intentionally.

---

### INFRA-003: No image signing (cosign) implemented

- **Severity:** high
- **Area:** Supply chain
- **Location:** .github/workflows/build-images.yml:119-135
- **Description:** No `cosign sign` step exists after `docker push`. No signature verification before deployment. No SBOM attached. A registry MITM or compromised push credential cannot be detected.
- **Evidence:** No cosign references in any workflow file. CLAUDE.md: "Image signing: cosign + syft (Sprint 6 — not yet implemented)."
- **Recommendation:** Add `cosign sign` immediately after each `docker push`. Add `cosign verify` in infra-deploy.yml before any deployment step. Generate SBOM with `syft` and attach as attestation.

---

### INFRA-004: build-images.yml not gated on CI passing

- **Severity:** high
- **Area:** CI/CD pipeline gate
- **Location:** .github/workflows/build-images.yml:3-24
- **Description:** build-images.yml triggers on push to develop/main independently of ci.yml. A commit with failing tests or security scan failures can still produce and push container images to the production registry.
- **Evidence:** No `workflow_run` dependency on CI workflow. No `needs: ci-gate` reference.
- **Recommendation:** Change trigger to `on: workflow_run: workflows: ["CI"] types: [completed]` with an initial step that checks `github.event.workflow_run.conclusion == 'success'`.

---

### INFRA-005: api/Dockerfile builder stage uses unpinned golang:1.25-alpine

- **Severity:** medium
- **Area:** Dockerfile
- **Location:** api/Dockerfile:1
- **Description:** Builder stage missing patch version and Alpine version. Inconsistent with operators/Dockerfile which correctly pins to `golang:1.25.3-alpine3.21`.
- **Evidence:** `FROM golang:1.25-alpine AS builder` vs `FROM golang:1.25.3-alpine3.21 AS builder` in operators.
- **Recommendation:** Change to `FROM golang:1.25.3-alpine3.21 AS builder`.

---

### INFRA-006: api/Dockerfile missing HEALTHCHECK directive

- **Severity:** medium
- **Area:** Dockerfile
- **Location:** api/Dockerfile (entire file, 33 lines)
- **Description:** No HEALTHCHECK means `docker ps` reports the container as healthy regardless of API state. operators/Dockerfile has a correct HEALTHCHECK.
- **Evidence:** No HEALTHCHECK directive in api/Dockerfile. operators/Dockerfile:35 has a wget-based HEALTHCHECK.
- **Recommendation:** Add `HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -qO- http://localhost:3001/api/v1/health || exit 1`

---

### INFRA-007: No .dockerignore at repository root

- **Severity:** medium
- **Area:** Dockerfile, build efficiency
- **Location:** Repository root (missing file)
- **Description:** Both Dockerfiles are built with the full repository root as context. This transfers docs, infra, CLI node_modules, test fixtures, and potentially sensitive files to the Docker daemon on every build.
- **Evidence:** No `.dockerignore` found at `/home/paulo/loopzero/`. `docker build -f api/Dockerfile .` in build-images.yml:124.
- **Recommendation:** Create root `.dockerignore` excluding: `.git`, `docs/`, `infra/`, `cli/`, `examples/`, `**/node_modules`, `**/__pycache__`, `.env*`.

---

### INFRA-008: Tilt dev deployment hardcodes credentials in plain environment variables

- **Severity:** medium
- **Area:** Kubernetes manifests, secrets management
- **Location:** infra/tilt/api-deployment.yaml:59, 63, 75
- **Description:** Database URL with password, MinIO secret key, and ArgoCD callback secret are hardcoded as plain env var values in a committed manifest.
- **Evidence:** DATABASE_URL contains loopzero-dev-password. MINIO_SECRET_KEY: loopzero-dev-password. LOOPZERO_ARGO_CALLBACK_SECRET: dev-callback-secret.
- **Recommendation:** Replace with `secretKeyRef` references pointing to Kubernetes secrets provisioned by the Tilt setup task, not committed to the repository.

---

### INFRA-009: Single replica for loopzero-api and tenant-controller; operator leader-elect disabled

- **Severity:** medium
- **Area:** Kubernetes, high availability
- **Location:** infra/tilt/operator-deployment.yaml:128, 146
- **Description:** Both core control plane components run as single instances in the committed Tilt manifests. The operator has leader election explicitly disabled with `--leader-elect=false`, contrary to the HA comment in the same file.
- **Evidence:** `replicas: 1` in both manifests. `--leader-elect=false` in operator args despite line 5 comment stating HA requires `--leader-elect=true`.
- **Recommendation:** Set `--leader-elect=true` and replicas >= 2 for both services in production overlays. Add PodDisruptionBudgets. Verify production Pulumi stack overrides these values.

---

### INFRA-010: Plaintext dev passwords committed in Helm values files

- **Severity:** medium
- **Area:** Helm charts, secrets management
- **Location:** infra/helm/postgresql/values.yaml:6, minio/values.yaml:7, gitea/values.yaml:7+19, loki/values.yaml:23
- **Description:** The password `loopzero-dev-password` appears in 4 Helm values files. TruffleHog with `--only-verified` will not flag these as they are not active API credentials. Accidental production deployment without overrides would expose services with known passwords.
- **Evidence:** `password: loopzero-dev-password` in postgresql, minio, gitea. `secret_access_key: loopzero-dev-password` in loki.
- **Recommendation:** Replace with `CHANGE-ME-IN-PRODUCTION` placeholder values. Add a CI check that fails if `loopzero-dev-password` appears in any values file. Rely on Pulumi secret injection for all actual credentials.

---

### INFRA-011: PostgreSQL single-node with no backup configuration in Helm chart

- **Severity:** medium
- **Area:** Data persistence, high availability
- **Location:** infra/helm/postgresql/values.yaml
- **Description:** Helm chart configures a standalone PostgreSQL primary with 1Gi PVC and no backup schedule. PostgreSQL is the sole state store for the entire platform.
- **Evidence:** No `readReplicaCount`, no backup section, `primary.persistence.size: 1Gi`.
- **Recommendation:** Production must use the postgresql-ha chart (Patroni) or the managed DO database with WAL archiving to DO Spaces. Verify the production Pulumi stack deploys the managed DO database with built-in HA and backups.

---

### INFRA-012: ArgoCD server.insecure: true — no in-cluster TLS

- **Severity:** low
- **Area:** Kubernetes, TLS
- **Location:** infra/helm/argocd/values.yaml:97
- **Description:** ArgoCD serves plain HTTP within the cluster. An attacker with access to any pod in the cluster can intercept ArgoCD API traffic including the loopzero-api service account token, which has application-wide sync permissions.
- **Evidence:** `server.insecure: true` in params config.
- **Recommendation:** Enable TLS on ArgoCD server or configure Traefik TLS passthrough for end-to-end encryption to the ArgoCD pod.

---

### INFRA-013: MinIO standalone — single point of failure for build artifacts

- **Severity:** low
- **Area:** Infrastructure, availability
- **Location:** infra/helm/minio/values.yaml:4
- **Description:** MinIO standalone mode has no redundancy. Build pipeline fails completely if MinIO is unavailable. Source bundles, build cache, and build logs are all at risk.
- **Evidence:** `mode: standalone`
- **Recommendation:** In production, migrate build artifacts and cache to DO Spaces (already used for static assets). Keep local MinIO for development only.

---

### INFRA-014: govulncheck and setup-envtest installed at @latest in CI

- **Severity:** low
- **Area:** CI/CD determinism
- **Location:** .github/workflows/ci.yml:252, 133
- **Description:** Security tooling installed at @latest produces non-reproducible CI results — different runs may use different tool versions with different vulnerability databases.
- **Evidence:** `go install golang.org/x/vuln/cmd/govulncheck@latest` and `go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest`.
- **Recommendation:** Pin to specific versions: `govulncheck@v1.1.3`, `setup-envtest` to a pinned release tag.

---

### INFRA-015: External Secrets Operator values missing security context and probe configuration

- **Severity:** low
- **Area:** Helm charts, security
- **Location:** infra/helm/external-secrets/values.yaml
- **Description:** The values.yaml is 15 lines with only resource limits and webhook flags. No securityContext (runAsNonRoot), no probe configuration, and no replicaCount override. ESO is critical for secret sync to workloads.
- **Evidence:** Minimal values file with no securityContext or probe configuration.
- **Recommendation:** Add `securityContext: runAsNonRoot: true`, explicit liveness/readiness probes, and `replicaCount: 2` with pod anti-affinity for production.

---

### INFRA-016: api/Dockerfile missing OCI image labels

- **Severity:** low
- **Area:** Dockerfile, supply chain
- **Location:** api/Dockerfile
- **Description:** No OCI provenance labels in api/Dockerfile. The image cannot be traced to its source commit from registry metadata, and SBOM tools cannot link the image to its repository.
- **Evidence:** No LABEL directives in api/Dockerfile. operators/Dockerfile has 3 OCI labels.
- **Recommendation:** Add OCI labels matching operators/Dockerfile: `org.opencontainers.image.source`, `.title`, `.description`. Pass `GIT_SHA` as a build ARG to add `.revision`.

---

## Recommendations (Prioritized)

1. [high] Gate build-images.yml on CI passing — implement `workflow_run` trigger on `ci.yml` completing with `success`. Without this, test failures do not prevent production image publication.

2. [high] Implement cosign image signing — add `cosign sign` after every `docker push` in build-images.yml, and `cosign verify` before every deployment in infra-deploy.yml. Already on Sprint 6 roadmap.

3. [high] Pin all GitHub Actions to SHA digests — use StepSecurity or Dependabot to automate SHA pinning across all four workflow files.

4. [high] Remove :latest tag from main branch builds — use only the SHA-based immutable tag; production manifests must reference the SHA tag.

5. [medium] Create root-level .dockerignore — reduces build context size, prevents accidental file leakage, improves layer cache effectiveness.

6. [medium] Replace committed passwords in Helm values with CHANGE-ME placeholders — use Pulumi secret injection for all actual credentials; add CI check that fails if loopzero-dev-password appears in values files.

7. [medium] Add HEALTHCHECK to api/Dockerfile — match the operators/Dockerfile pattern.

8. [medium] Fix operator deployment to enable leader election — change `--leader-elect=false` to `--leader-elect=true` in the production Pulumi overlay; ensure replicas > 1 for production.

9. [low] Pin govulncheck and setup-envtest to specific versions — prevents non-deterministic CI security scanning behavior.

10. [low] Add OCI image labels to api/Dockerfile — enables registry-based provenance tracing, required for cosign attestation.
