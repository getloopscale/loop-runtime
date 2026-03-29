# CI/CD Pipeline Review

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero
**Reviewer:** cicd-reviewer

## Executive Summary

- Overall CI/CD health: NEEDS ATTENTION
- Total findings: 10 (critical: 2, high: 4, medium: 3, low: 1)
- Key concern: Container images are built and pushed to the production registry without verifying that CI tests have passed first, and no action in any workflow is pinned to an immutable SHA — both supply chain and test integrity guarantees are absent.

## CI/CD Platform

- Platform: GitHub Actions
- Workflows found: 4 (ci.yml, build-images.yml, infra-deploy.yml, infra-preview.yml)
- Deployment targets: infra-dev (DO SFO3), infra-staging, infra-prod (via GitHub Environments)

## Pipeline Structure

### ci.yml
Full quality gate: lint (Go × 3 components, TypeScript), go mod tidy, unit tests (pkg, operators, api, infra, CLI, Pulumi), manifest validation via kubeconform, security scanning (govulncheck × 3, npm audit, Trivy fs/IaC/Dockerfile × 2, TruffleHog), binary builds. All 16 jobs funneled into a single `ci-gate` job. Concurrency cancel-in-progress for same branch/PR. Triggers: push to all branches, PR to main.

### build-images.yml
Resolves build targets, builds loopzero-api and tenant-controller Docker images, pushes to DO Container Registry with :dev/:sha or :latest/:sha tags, integrates registry with DOKS clusters, updates CWT ConfigmapTemplates on the build cluster via Helm. Triggers: push to develop/main on api/operators/pkg paths, workflow_dispatch. No dependency on ci.yml.

### infra-deploy.yml
3-phase Pulumi deploy (infra → images → converge) with pre-destroy cleanup logic. Maps develop→infra-dev (auto), main→infra-prod (requires approval). Concurrency cancel-in-progress: false for deploy jobs (correct — prevents orphaned resources). Requires validate (typecheck + Pulumi tests) before deploy. workflow_dispatch allows manual destroy of any stack.

### infra-preview.yml
Runs pulumi preview on PRs to develop/main touching infra/pulumi/, posts result as PR comment. Requires validate before preview. Correct scoping: pull-requests: write for PR comment, contents: read otherwise.

## Test Gate Assessment

| Gate | Present | Enforced | Notes |
|------|---------|----------|-------|
| Unit tests | YES | YES | pkg, operators, api, infra, CLI — all in ci-gate |
| Integration tests | NO | NO | No integration tests in CI — envtest covers operator unit tests only |
| Lint/Format | YES | YES | golangci-lint + ESLint + tsc --noEmit, all gated |
| Type check | YES | YES | TypeScript strict, Pulumi TypeScript |
| Security scan | YES | YES | govulncheck, npm audit --audit-level=high, Trivy, TruffleHog |
| E2E tests | NO | NO | infra/tests/e2e/ (Ginkgo) exists but not in any workflow |
| Coverage threshold | NO | NO | --coverage flag present, no coverageThreshold in jest.config.js |

## Build Reproducibility Assessment

- Lock files committed: YES — package-lock.json for cli/ and infra/pulumi/, go.sum for all Go modules
- Lock file usage: YES — all npm jobs use `npm ci`, Go jobs use `go test ./...` (respects go.sum)
- Actions/plugins pinned: TAG ONLY — zero SHA-pinned action references across all 4 workflows
- Tool installs: PARTIALLY PINNED — doctl v1.104.0 pinned (downloaded via curl), kubeconform v0.6.7 pinned, but setup-envtest and govulncheck use @latest
- Base images: Not inspected here (evaluated in container review)
- Assessment: PARTIALLY REPRODUCIBLE — lockfiles and most tools are pinned, but action tags and two @latest tool installs create non-determinism

## Artifact Management Assessment

- Versioning strategy: SHA (7-char short SHA) + branch-derived mutable tag (:dev or :latest)
- Promotion: Build once per branch push; SHA tag is immutable; mutable tag overwritten on each push
- Immutability: PARTIAL — SHA tag is immutable; :dev and :latest tags are mutable and overwritten on each push
- Registry: DigitalOcean Container Registry (registry.digitalocean.com/loopzero-infra-registry)

## Secret Management Assessment

- Secret store: GitHub Environments (infra-dev, infra-staging, infra-prod)
- OIDC: NO — long-lived DIGITALOCEAN_TOKEN used everywhere; no OIDC/workload identity
- Secret references: Correct — all secrets accessed via ${{ secrets.X }} syntax
- Hardcoded secrets: NOT FOUND — no hardcoded credentials in workflow files
- Secret scoping: GOOD — build-images jobs scoped to infra-dev environment; infra-deploy uses per-stack environments; secrets not available to validate/resolve jobs without environment
- Token granularity: WEAK — single DIGITALOCEAN_TOKEN used for both registry push (low privilege) and cluster kubeconfig operations (high privilege) in build-images.yml

## Deployment Safety Assessment

- Branch protection: CODEOWNERS configured (requires @paulosergio-jnr review) — actual branch protection rules not visible in code but CODEOWNERS is in place
- Required reviews: CODEOWNERS enforcement depends on repository branch protection settings being enabled
- Manual approval for prod: YES — infra-prod GitHub Environment is configured with required reviewers
- Rollback capability: YES — SHA-tagged images enable rollback via gitops manifest revert; Pulumi state backend on DO Spaces enables infra rollback
- Deployment strategy: Rolling (Kubernetes default via ArgoCD sync)
- Smoke tests after deploy: NO — no post-deploy verification step in any workflow
- Deploy notifications: NO — no Slack/Discord/Teams notification step

## Supply Chain Assessment

- Dependency automation: NONE — no dependabot.yml, no renovate.json
- Vulnerability scanning: govulncheck (Go), npm audit (Node), Trivy (fs + IaC + Dockerfile), TruffleHog (secrets)
- SBOM: NO
- Image signing: NO — no cosign or notation
- Build-test-deploy integrity: BROKEN — build-images.yml runs in parallel with ci.yml on the same push; tested artifact is not guaranteed to equal built artifact

---

## Findings (Detailed)

### CICD-001: All GitHub Actions pinned to tag only — no SHA pinning anywhere
- **Severity:** critical
- **Area:** supply_chain
- **Location:** .github/workflows/ci.yml:37 (and all 4 workflow files)
- **Description:** Every `uses:` reference in all 4 workflows uses a mutable version tag rather than an immutable commit SHA. An attacker who compromises any of the referenced action repositories or force-pushes a tag can inject arbitrary code into CI with full access to all repository secrets including DIGITALOCEAN_TOKEN, CLOUDFLARE_API_TOKEN, PULUMI_CONFIG_PASSPHRASE, and cloud infrastructure credentials.
- **Evidence:** ci.yml:37 `uses: actions/checkout@v4`, ci.yml:43 `uses: golangci/golangci-lint-action@v7`, infra-preview.yml:121 `uses: pulumi/actions@v6`, build-images.yml:112 `uses: digitalocean/action-doctl@v2`, build-images.yml:225 `uses: azure/setup-helm@v4`, ci.yml:330 `uses: trufflesecurity/trufflehog@v3.88.27`
- **Recommendation:** Pin every action to its full 40-character commit SHA. Use Dependabot with `package-ecosystem: github-actions` to keep SHAs current. Example: `uses: actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4.1.1`

### CICD-002: build-images.yml deploys to production registry without requiring CI tests to pass
- **Severity:** critical
- **Area:** test_gate
- **Location:** .github/workflows/build-images.yml:16
- **Description:** The build-images workflow runs on the same `push` event as ci.yml and has no `workflow_run` dependency on CI completing successfully. On any push to develop or main, both workflows start simultaneously. The registry receives a new image whether or not tests pass. The fundamental invariant "what was tested equals what was deployed" is violated.
- **Evidence:** build-images.yml:16-24 `on: push: branches: [develop, main]` with no `workflow_run` or cross-workflow dependency. ci.yml runs independently on the same event.
- **Recommendation:** Add `workflow_run` trigger: `on: workflow_run: workflows: ["CI"] types: [completed]` with condition `if: github.event.workflow_run.conclusion == 'success'`. Alternatively, move the build jobs into ci.yml after the `ci-gate` job.

### CICD-003: No Dependabot or Renovate configured
- **Severity:** high
- **Area:** supply_chain
- **Location:** .github/ (file absent)
- **Description:** No automated dependency update tooling is configured. GitHub Actions tags, npm packages, and Go modules will drift indefinitely. Combined with cicd_001 (tag-only pinning), this means the effective version of each action drifts to whatever the tag resolves to when the tag owner pushes a new commit.
- **Evidence:** `find /home/paulo/loopzero/.github` — only CODEOWNERS and 4 workflow files. No dependabot.yml or renovate.json present anywhere in the repository.
- **Recommendation:** Create `.github/dependabot.yml` with entries for `github-actions`, `npm` (cli/, infra/pulumi/), and `gomod` (api/, operators/, pkg/, infra/tests/, infra/scripts/).

### CICD-004: govulncheck and setup-envtest installed with @latest
- **Severity:** high
- **Area:** reproducibility
- **Location:** .github/workflows/ci.yml:132 and ci.yml:252
- **Description:** `go install setup-envtest@latest` and `go install govulncheck@latest` download the most recently released version at execution time. Two CI runs on the same commit can produce different results if a new version ships between runs. Govulncheck version changes affect which CVEs are reported, potentially causing builds to flip between pass and fail on unchanged code.
- **Evidence:** ci.yml:132 `go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest`, ci.yml:252 `go install golang.org/x/vuln/cmd/govulncheck@latest`
- **Recommendation:** Pin to explicit versions: `setup-envtest@v0.19.0` and `govulncheck@v1.1.3`. Add to Taskfile as `task tools:install` so developers use the same versions locally.

### CICD-005: Container images pushed with mutable :latest and :dev tags
- **Severity:** high
- **Area:** artifacts
- **Location:** .github/workflows/build-images.yml:66
- **Description:** In addition to an immutable SHA tag, images are pushed with branch-derived mutable tags (:dev for develop, :latest for main). If any Helm value or Pulumi resource references the mutable tag, rollback by reverting a gitops manifest will fail — the cluster will continue pulling the current :latest image. The mutable tags overwrite on every push, destroying the previous content with no traceability.
- **Evidence:** build-images.yml:66-70 `TAG=dev|latest`, pushed at lines 129-130 `docker push $REGISTRY/loopzero-api:${TAG}` and `docker push $REGISTRY/loopzero-api:${SHA}`.
- **Recommendation:** Enforce SHA-only references in all Helm values and Pulumi configurations. The mutable tags can be retained as aliases but must never appear in deployment manifests. Add a CI check that verifies no active Kubernetes deployment uses a mutable image tag.

### CICD-006: No image signing or SBOM generation
- **Severity:** high
- **Area:** supply_chain
- **Location:** .github/workflows/build-images.yml:119
- **Description:** Images are pushed to the DO registry with no cryptographic signing and no SBOM attestation. There is no way to verify that a running container came from a specific commit, was built by the CI pipeline (not manually), or has not been replaced after the push. This is explicitly acknowledged in CLAUDE.md as planned but not implemented (Sprint 6).
- **Evidence:** build-images.yml:119-134 — no cosign, notation, or attestation step. CLAUDE.md Stack table: `cosign + syft (Sprint 6 — not yet implemented)`.
- **Recommendation:** Add `cosign sign` with keyless OIDC (Sigstore/Fulcio) immediately after each push. Add `syft` SBOM generation and attach via `cosign attest`. Requires adding `id-token: write` to build job permissions. DO Container Registry supports OCI artifacts for storing attestations.

### CICD-007: Single DIGITALOCEAN_TOKEN for both registry push and cluster operations
- **Severity:** medium
- **Area:** secrets
- **Location:** .github/workflows/build-images.yml:112
- **Description:** One token with full DO account access is passed to the third-party `digitalocean/action-doctl@v2` action (which is itself not SHA-pinned). This token is used for both registry login (low privilege needed) and cluster kubeconfig save (higher privilege needed). Least-privilege principle requires separate scoped tokens.
- **Evidence:** build-images.yml:112-114 token passed to doctl action; same token used at line 239 `doctl kubernetes cluster kubeconfig save`.
- **Recommendation:** Create two tokens: DIGITALOCEAN_TOKEN_REGISTRY (registry write only) for build jobs, DIGITALOCEAN_TOKEN_CLUSTER (kubernetes:admin) for integrate-registry and update-cwts jobs.

### CICD-008: No Jest coverage threshold — coverage is reported but cannot fail
- **Severity:** medium
- **Area:** test_gate
- **Location:** .github/workflows/ci.yml:171 / cli/jest.config.js
- **Description:** Jest is run with `--coverage` but `cli/jest.config.js` has no `coverageThreshold`. The CI step will pass with 0% coverage. The coverage artifact is uploaded but serves only as a vanity metric — it cannot enforce quality regression prevention.
- **Evidence:** ci.yml:171 `npm test -- --ci --coverage`. cli/jest.config.js contains only preset, testEnvironment, roots, testMatch, and moduleFileExtensions — no coverageThreshold.
- **Recommendation:** Add `coverageThreshold: { global: { branches: 70, functions: 80, lines: 80, statements: 80 } }` to cli/jest.config.js, calibrated to current coverage levels.

### CICD-009: workflow_dispatch destroy command has no additional protection beyond environment approval
- **Severity:** medium
- **Area:** deployment
- **Location:** .github/workflows/infra-deploy.yml:33
- **Description:** Any repository collaborator with write access can trigger a full infrastructure destroy of any stack (dev, staging, or prod) via workflow_dispatch. For prod, the GitHub Environment gate provides one layer of protection, but a single reviewer approval is sufficient. No explicit confirmation token or secondary workflow-level gate exists for destructive operations.
- **Evidence:** infra-deploy.yml:33-54 `workflow_dispatch` with `command: destroy` and `stack: prod` as valid inputs.
- **Recommendation:** Add a required `confirm` text input (e.g., must type "DESTROY-PROD") for destroy operations. Consider a separate restricted workflow for destroy that requires two environment approvals. Document the destroy runbook as a required read before approval.

### CICD-010: CI triggers on push to all branches
- **Severity:** low
- **Area:** pipeline
- **Location:** .github/workflows/ci.yml:5
- **Description:** `branches: ['**']` causes the full 22-job CI matrix to run on every push to every branch, including work-in-progress commits. The concurrency group with cancel-in-progress limits the damage but intermediate commits still consume CI minutes.
- **Evidence:** ci.yml:5 `branches: ['**']`
- **Recommendation:** Consider restricting push trigger to `[develop, main]` and relying on the pull_request trigger for feature branch PRs. If per-branch CI is intentional, add `paths-ignore` for docs-only changes to avoid CI runs on README edits.

---

## Recommendations (Prioritized)

1. **Fix the test-before-build integrity gap (cicd_002)** — Add `workflow_run` trigger to build-images.yml so images are only built after `ci-gate` passes. This is the most critical correctness issue: images currently enter the registry regardless of test outcomes.

2. **SHA-pin all GitHub Actions (cicd_001)** — Run `pinact` or manually look up SHAs for the 12 distinct action references across 4 workflows. Configure Dependabot for `github-actions` ecosystem to maintain them. This closes the highest-impact supply chain attack vector.

3. **Add Dependabot configuration (cicd_003)** — Create `.github/dependabot.yml` covering github-actions, npm (2 directories), and gomod (5 modules). This directly enables ongoing maintenance of finding cicd_001.

4. **Implement image signing with cosign (cicd_006)** — Keyless signing via Sigstore is zero-configuration cost and adds provenance to every image. Required for Sprint 6 milestone.

5. **Pin @latest tool installs (cicd_004)** — Two-line change in ci.yml. Pin setup-envtest and govulncheck to specific versions. Low effort, high reproducibility benefit.

6. **Enforce mutable-tag-free deployments (cicd_005)** — Audit all Helm values and Pulumi resource configurations to ensure they reference SHA tags. Add a CI check or admission webhook that rejects deployments referencing :latest or :dev.

7. **Split DIGITALOCEAN_TOKEN by capability (cicd_007)** — Create a registry-scoped token for the build jobs to reduce blast radius if digitalocean/action-doctl (unverified by SHA) is ever compromised.

8. **Add Jest coverage threshold (cicd_008)** — Single line addition to jest.config.js. Start at the current coverage level to prevent regression.

9. **Add destroy confirmation input (cicd_009)** — Add a text confirmation input to the destroy workflow path. Low engineering effort, high operational safety value.

10. **Restrict CI push trigger scope (cicd_010)** — Minor workflow cleanup. Not blocking but reduces noise and CI spend.
