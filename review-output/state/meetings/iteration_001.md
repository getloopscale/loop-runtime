# Meeting Minutes — Iteration 1 (Phase 1: Baseline)

## Date: 2026-03-29
## Attendees: chief-reviewer, architecture-analyst, dependency-analyzer, flow-tracer

---

## Status

- **Phase:** 1 (Baseline — Discovery and Mapping)
- **Iteration:** 1 (global: 1)
- **Findings registered:** 0 (first iteration, no previous work)
- **Previous work:** None — this is the inaugural meeting
- **Database:** Initialized, empty

---

## Specialist Briefings

### Chief Reviewer — Initial Observations

A structural survey of the LoopZero repository has been completed to frame the work for the team. Key observations:

**Codebase footprint:**
- 6 primary Go modules: `api/`, `operators/`, `pkg/`, `infra/scripts/`, `infra/tests/`, and a TypeScript CLI (`cli/`)
- 2 additional Go modules outside the workspace: `loopzero-forge/` (Argo Workflow SDK) and `loopzero-packs/` (language detection)
- ~4,739 lines across API services alone; `build_orchestrator.go` is the heaviest file at 736 lines
- 9 CRD types, 7 controllers, 7 state machines, 13 Helm charts
- 498 CLI tests (Jest), 60+ API test files, Ginkgo-based operator and E2E tests

**Architecture summary confirmed (4-plane design):**
1. Developer Plane: TypeScript CLI -> Go API (chi + pgx)
2. Control Plane: mgmt cluster (operators, Gitea, ArgoCD, PostgreSQL, MinIO, Traefik)
3. Build Plane: build cluster (Argo Workflows + BuildKit, rootless)
4. Workload Plane: runtime cluster (user apps, ArgoCD-managed, CF Tunnel)

**Cross-plane communication model:**
- API -> build cluster: direct Kubernetes client (polling Workflow CRDs — C-01/C-03 fix, no callback HTTP from build to API)
- API -> mgmt cluster: Kubernetes client (Tenant CR operations)
- API -> gitops: Gitea HTTP (push manifests)
- API -> ArgoCD: HTTP client (sync trigger)
- Static apps: S3 (DO Spaces) + Cloudflare Workers KV for routing

**Key design decisions already in place (ADRs observed):**
- `pg_advisory_xact_lock` for environment serialization (not session-scoped — correct)
- 5-second lock timeout enforced at `SET LOCAL lock_timeout`
- `hashtext()` for key derivation — collision acknowledged (~0.001% with 2000 envs)
- RELEASING intermediate state added to build state machine (MEET-2026-03-26-008)
- Reconcile loop (polling) replaces callback architecture (C-01/C-03)
- Delivery abstraction: `DeliveryActivator` interface allowing both container and static delivery paths

**Incomplete/placeholder items spotted:**
- `AppSecret` CRD: no controller implemented
- `PreviewEnvironment` CRD: no controller implemented
- `ReconciliationReport` CRD: no controller
- `loopzero status` endpoint handler exists but per CLAUDE.md, 1 line missing in `server.go`
- `startup_reconciler.go` still present alongside `build_reconcile_loop.go` — potential overlap
- Sprint 5 (preview environments) not started

---

## Decisions

1. **Decision: Prioritize the 4-plane boundary as the primary structural risk axis.**
   Rationale: LoopZero's architecture spans 3 Kubernetes clusters. Every cross-plane call is a potential failure point. The architecture analyst must map all cross-plane API calls, authentication paths, and failure modes between planes — not just within each component.

2. **Decision: Treat the build → release → ArgoCD sync pipeline as the highest-priority flow to trace.**
   Rationale: This is the core user-facing value path (`loopzero deploy` → live URL). It involves the most components, the most state transitions, and has the most observed historical bugs (C-01, C-03, MEET-008 patches). Any gap here is a critical product defect.

3. **Decision: All 7 state machines must be mapped against their spec definitions before Phase 3 (Architecture Review).**
   Rationale: State machine correctness is a spec-level contract. The `pkg/statemachine/` package has both implementation and tests; the reviewer must verify no state transition gaps exist between the state machine definition and the actual event callers in services.

4. **Decision: The dependency graph must include both direct and transitive relationships across all 5 Go modules and the TypeScript CLI.**
   Rationale: The Go workspace (`go.work`) ties together 5 modules with shared types via `pkg/`. Import cycle risks and coupling between `api/` and `operators/` must be mapped before Phase 3.

5. **Decision: Flag all "implemented but not wired" patterns as a first-class finding category.**
   Rationale: CLAUDE.md documents a known systemic pattern called "Implemented but not wired" — code exists in the correct location but is not integrated in `main.go` or runtime paths. This is a completeness risk that must be detected early.

6. **Decision: Record the `loopzero status` missing wire as a verified completeness gap, pending confirmation.**
   Rationale: CLAUDE.md explicitly states "`loopzero status` pending (handler exists, 1 line missing in server.go)." The flow tracer must confirm whether this is truly unregistered in the server routing.

---

## Task Assignments

### Architecture Analyst

**Goal:** Produce a component map of all significant modules, their responsibilities, their inter-dependencies, and the cross-plane API surface.

**Specific deliverables:**
1. List all 6+ primary Go modules and the CLI module with their public API surfaces (exported interfaces/types used across modules)
2. Map all cross-cluster clients: which component uses which kubeconfig, for what purpose
3. Map all external service integrations (Gitea, ArgoCD, Argo Workflows, MinIO/S3, GitHub, Cloudflare KV, Loki) and which API package owns each
4. Identify the `startup_reconciler.go` vs `build_reconcile_loop.go` overlap — are both active, is one deprecated?
5. Identify any structural anti-patterns: God objects, circular dependency risks, missing abstractions

**Files to examine:**
- `/home/paulo/loopzero/api/cmd/main.go` (full file — entrypoint wiring)
- `/home/paulo/loopzero/api/internal/server/server.go` (routing table)
- `/home/paulo/loopzero/api/internal/adapters/` (DIP bridges)
- `/home/paulo/loopzero/operators/cmd/` (operator entrypoint — multi-cluster client setup)
- `/home/paulo/loopzero/infra/pulumi/src/index.ts` (3-phase infra orchestration)

---

### Dependency Analyzer

**Goal:** Map the dependency graph across all modules, identify coupling risks, and detect supply chain vulnerabilities.

**Specific deliverables:**
1. Go module dependency graph: which modules import which, what are the shared packages
2. Key third-party Go dependencies per module with version pins (pgx, chi, controller-runtime, client-go, argo, etc.)
3. TypeScript CLI dependency tree: key packages, lock file health
4. Any dependency that is: unpinned, pulling from a non-stable source, or known-vulnerable
5. Identify if `loopzero-forge/` and `loopzero-packs/` are in the Go workspace — they are NOT per `go.work`, which means they may have diverged dependency versions

**Files to examine:**
- `/home/paulo/loopzero/go.work` and all `go.mod` files
- `/home/paulo/loopzero/cli/package.json` and `package-lock.json`
- `/home/paulo/loopzero/loopzero-forge/go.mod` (if it exists)
- `/home/paulo/loopzero/loopzero-packs/go.mod` (if it exists)
- `/home/paulo/loopzero/.github/workflows/ci.yml` (govulncheck + npm audit jobs)

---

### Flow Tracer

**Goal:** Map all critical business flows end-to-end, identifying the sequence of components, state transitions, and failure modes at each step.

**Critical flows to trace (in priority order):**

1. **Deploy flow** (`loopzero deploy` → live URL):
   CLI uploads bundle → API POST /api/v1/builds → `withEnvironmentLock` → Argo Workflow submit → BuildKit → image push → manifest generation → Gitea gitops push → ArgoCD sync → SSE event → CLI renders URL
   - Which state machine events fire at each step?
   - Where does RELEASING state get set and cleared?
   - What happens on ArgoCD sync failure?

2. **Auth flow** (`loopzero login` → authenticated requests):
   CLI token storage → API Bearer token → GitHub validator → token cache → membership lookup → UserContext injection
   - Is the token cache TTL correct?
   - What happens on GitHub API rate limit?

3. **Rollback flow** (`loopzero rollback` → previous release live):
   API POST /rollback → `withEnvironmentLock` → GetPreviousRelease → manifest retrieval from git history → Gitea push → ArgoCD sync → release state transition
   - Does the rollback flow go through the same state machines?
   - Is the convergence loop involved?

4. **Static delivery flow** (static frontend → CF edge):
   Build → S3 upload → CF KV write → CF Worker → DO Spaces CDN
   - Which service writes CF KV? (`cfkv/` package in API)
   - Is this path covered by the reconcile loop?

5. **`loopzero status` wiring gap** (verify the known missing line):
   Check `server.go` routing table — is `StatusHandler` registered?

**Files to examine:**
- `/home/paulo/loopzero/api/internal/services/build_orchestrator.go`
- `/home/paulo/loopzero/api/internal/services/build_reconcile_loop.go`
- `/home/paulo/loopzero/api/internal/services/release_convergence_loop.go`
- `/home/paulo/loopzero/api/internal/services/rollback_orchestrator.go`
- `/home/paulo/loopzero/api/internal/services/static_delivery.go`
- `/home/paulo/loopzero/api/internal/server/server.go` (route registration)
- `/home/paulo/loopzero/cli/src/commands/deploy.ts`
- `/home/paulo/loopzero/cli/src/commands/rollback.ts`

---

## Risk Hypotheses

These hypotheses are not confirmed findings — they are areas that require evidence from specialist agents before being elevated to findings.

### RH-01: State Machine Caller Gaps
**Hypothesis:** Some state transitions defined in `pkg/statemachine/` may not have callers in `api/internal/services/`. The RELEASING state was added recently (MEET-2026-03-26-008); there may be callers that still expect the old RUNNING→SUCCEEDED direct transition.
**Priority:** High
**Owner to investigate:** Flow Tracer + Code Reviewer (Phase 4)

### RH-02: `loopzero-forge` and `loopzero-packs` Outside Go Workspace
**Hypothesis:** `loopzero-forge/` and `loopzero-packs/` are not in `go.work`, meaning they operate as independent modules. If the API or build pipeline imports them, there may be version drift between what is tested locally and what runs in CI.
**Priority:** Medium
**Owner to investigate:** Dependency Analyzer

### RH-03: `startup_reconciler.go` Dead Code Risk
**Hypothesis:** `startup_reconciler.go` still exists in `api/internal/services/` but the reconcile loop (C-01/C-03 fix) was designed to subsume it. If `startup_reconciler.go` is still wired in `main.go`, there may be duplicate processing of builds on startup.
**Priority:** Medium
**Owner to investigate:** Architecture Analyst + Flow Tracer

### RH-04: `loopzero status` Endpoint Not Registered
**Hypothesis:** CLAUDE.md states "handler exists, 1 line missing in server.go." If confirmed, `loopzero status` would silently return 404 in production without any error at startup.
**Priority:** High (user-facing gap)
**Owner to investigate:** Flow Tracer (Phase 1), Completeness Auditor (Phase 2)

### RH-05: Auth Token Cache Invalidation on GitHub Revoke
**Hypothesis:** The auth middleware caches GitHub token validation results for 5 minutes. If a user's GitHub token is revoked, they remain authenticated for up to 5 minutes. This is expected behavior but must be confirmed as a deliberate design decision, not an oversight.
**Priority:** Medium (security design)
**Owner to investigate:** Security Auditor (Phase 6)

### RH-06: hashtext() Lock Key Collision Under Multi-Tenant Scale
**Hypothesis:** The lock key uses `hashtext()` (int32 hash). With many concurrent tenants/projects/environments, hash collisions would cause false serialization (one tenant's deploys waiting for an unrelated tenant's deploy). The code acknowledges ~0.001% collision rate but this may not account for adversarial naming.
**Priority:** Low-Medium (liveness, not safety)
**Owner to investigate:** Security Auditor + Code Reviewer

### RH-07: SSE Stream Leak on Long-Running Builds
**Hypothesis:** The SSE stream has a safety timeout (`SSESafetyTimeout`), but if builds enter RELEASING state and stall there for up to 10 minutes, CLI connections may drop before the stream delivers the final URL, leaving the user with no feedback.
**Priority:** Medium (DX regression)
**Owner to investigate:** Flow Tracer + Code Reviewer (Phase 4)

### RH-08: AppSecret CRD Unimplemented — ESO Bridge Gap
**Hypothesis:** `AppSecret` CRD exists with no controller. Secrets currently managed via External Secrets Operator (ESO). The gap between ESO sync and the `AppSecret` abstraction may leave secret management in an inconsistent state for new tenants.
**Priority:** High (completeness)
**Owner to investigate:** Completeness Auditor (Phase 2)

### RH-09: Cross-Cluster Client Failure Isolation
**Hypothesis:** The TenantController uses 3 separate Kubernetes clients (mgmt, build, runtime). If the runtime client becomes unreachable, reconcile loops may block or fail in ways that affect unrelated tenants on the same controller instance.
**Priority:** High (operational)
**Owner to investigate:** Architecture Analyst + Infrastructure Reviewer (Phase 5)

### RH-10: Cloudflare KV Consistency with S3 Upload
**Hypothesis:** In the static delivery flow, S3 upload and CF KV write are sequential operations. If CF KV write fails after S3 upload succeeds, the build is marked as completed but traffic is not routed to the new version. The rollback path for this failure mode is unclear.
**Priority:** High (data consistency)
**Owner to investigate:** Flow Tracer + Data Reviewer (Phase 5)

---

## Strategic Assessment

**What we know:**
- The codebase is feature-rich and architecturally ambitious. Four separate planes with 3 Kubernetes clusters is a significant operational complexity surface.
- Multiple recent bug fixes (C-01, C-03, MEET-008) indicate the build-to-release pipeline has been unstable and has received emergency patches. This is the area most likely to harbor residual issues.
- The DIP (Dependency Inversion Principle) is consistently applied via interface abstractions. This is a positive signal for testability.
- The `pg_advisory_xact_lock` implementation is correct and well-commented — it demonstrates deliberate engineering.
- The `RELEASING` intermediate state (non-terminal, retryable) is a sound design for crash recovery.

**Key structural risks:**
- Multi-cluster complexity: failure isolation between the 3 clusters is not yet reviewed
- Cross-cutting state: builds span 3 clusters and 2 state machines (Build + Release) — ensuring they stay consistent under crash scenarios is critical
- "Implemented but not wired" pattern documented in CLAUDE.md — this must be systematically checked, not assumed

**What we do NOT yet know:**
- Whether all 7 state machine implementations match their spec definitions precisely
- Whether the reconcile loop correctly handles all RELEASING→SUCCEEDED edge cases
- Whether there are any missing route registrations beyond the known `loopzero status` gap
- Whether `loopzero-forge` and `loopzero-packs` have integration into the main build pipeline and what their dependency health looks like

---

## Loop-Back Assessment

- **Should we loop back?** N/A — this is Phase 1, Iteration 1
- **Next phase criteria:** Phase 1 is complete when:
  1. All primary components are mapped with their responsibilities and interfaces
  2. The dependency graph across all Go modules and the CLI is documented
  3. The 5 critical flows (deploy, auth, rollback, static delivery, status wiring) are traced end-to-end
  4. All 10 risk hypotheses have a status (confirmed, refuted, or escalated to later phase)

---

## Next Steps (Before Phase 1 Complete)

1. Architecture Analyst: complete component map with cross-plane dependency graph — deliver to `review-output/baseline/` within this iteration
2. Dependency Analyzer: produce Go module and npm dependency analysis — flag any modules outside `go.work`
3. Flow Tracer: trace deploy flow and verify `loopzero status` route registration — confirm or refute RH-04 immediately
4. Chief Reviewer: synthesize all 3 reports into Phase 1 findings summary before advancing to Phase 2

---

## Next Meeting

- **Expected at:** Iteration 2 (start of Phase 2: Completeness Audit)
- **Focus:** Review specialist reports from Iteration 1, decide which risk hypotheses are confirmed findings, assign Phase 2 tasks to Completeness Auditor and Code Reviewer
