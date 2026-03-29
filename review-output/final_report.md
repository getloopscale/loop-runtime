# Deep Review Report — LoopZero Platform

**Target:** /home/paulo/loopzero
**Date:** 2026-03-29
**Reviewer:** Autonomous Deep Review Agent (8-phase pipeline)
**Mode:** Full review, severity threshold: low

---

## Executive Summary

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments with a well-architected 4-plane design (Developer, Control, Build, Workload). The codebase demonstrates strong engineering discipline: strict state machines, consistent advisory locking, tenant isolation at the data layer, and parameterized queries throughout.

### C4 Health Score

| Dimension | Score | Assessment |
|-----------|-------|------------|
| **Correto** (Correct) | 7.5/10 | Strong state machines, good patterns, but 7 silent error paths |
| **Completo** (Complete) | 6.5/10 | Core flows work, but 3 CRDs without controllers, circuit breaker not wired, Sprint 4-5 features missing |
| **Confiavel** (Reliable) | 7.0/10 | Good locking + idempotency, but missing Argo circuit breaker and rollback lock bug |
| **Controlavel** (Observable) | 8.0/10 | Excellent: structured logging, correlation IDs, Prometheus, Grafana, Loki, OTel |

**Overall: 7.25/10** — Production-grade core with known gaps in completeness and resilience.

### Finding Summary

| Severity | Count | Key Areas |
|----------|-------|-----------|
| **Critical** | 2 | CI/CD: no SHA pinning, deploy without test gate |
| **High** | 23 | Silent failures, circuit breaker not wired, coupling issues |
| **Medium** | 38 | Supply chain, auth cache, documentation drift |
| **Low** | 28 | Dead code, planned gaps, minor issues |
| **Total** | **91** | Across all phases |

---

## System Overview

### Architecture
- **4 planes:** Developer (CLI+API), Control (mgmt cluster), Build (Argo+BuildKit), Workload (ArgoCD)
- **3 DOKS clusters** on DigitalOcean (SFO3)
- **20 components** mapped, **10 critical flows** traced
- **9 CRDs**, **7 controllers**, **7 state machines**
- **498 CLI tests** + **42 API test files** + **18 operator test files** + **15 pkg test files**

### Technology Stack
| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander |
| API | Go 1.25 + chi + pgx |
| Operators | Go + kubebuilder + controller-runtime |
| Build | Argo Workflows + BuildKit (rootless) |
| GitOps | Gitea 1.22.6 + ArgoCD 2.13.3 |
| Database | PostgreSQL 16 (managed DO) |
| Edge | Cloudflare Tunnel + Worker + KV |
| IaC | Pulumi (TypeScript) |

---

## Critical Findings (P0 — Fix Immediately)

### CRIT-1: build-images.yml deploys without requiring CI tests to pass
- **ID:** cicd_002
- **Impact:** Container images can be built and pushed to production registry without passing lint, test, or security scans
- **Fix:** Add `needs: [ci-complete]` to the build-images workflow

### CRIT-2: All GitHub Actions use tag pinning, not SHA pinning
- **ID:** cicd_001
- **Impact:** Supply chain attack vector — a compromised action tag could inject malicious code into CI
- **Fix:** Pin all 10 actions to SHA digests

---

## High Findings (P1 — Fix This Sprint)

### HIGH-1: Secrets stored without encryption (SEC-001)
- **File:** api/internal/db/secret_store.go
- **Impact:** DB column named `encrypted_value` but NO encryption applied. Raw plaintext stored. A database breach exposes all user secrets.
- **Fix:** Implement AES-256-GCM envelope encryption before DB write. CWE-312, OWASP A02.

### HIGH-2: POST /internal/bootstrap has no authentication (SEC-003)
- **File:** api/internal/server/server.go:113-116
- **Impact:** Any pod in the cluster can create a rogue admin tenant. `BOOTSTRAP_SECRET` config exists but is never wired.
- **Fix:** Add secret validation to the bootstrap handler.

### HIGH-3: Gitea token leaked in source store error logs (SEC-002)
- **File:** api/internal/source/gitea_store.go:233
- **Impact:** When git commands fail, the token appears in error strings. The gitops pusher sanitizes correctly but the source store does not.
- **Fix:** Apply the same `sanitizeArgs`/`sanitizeOutput` pattern from gitea_pusher.go.

### HIGH-4: CircuitBreakerWorkflowSubmitter NOT wired in main.go
- **ID:** find_cb_not_wired
- **File:** api/internal/services/workflow_circuit_breaker.go
- **Impact:** No circuit breaker on Argo Workflow submissions. 10 concurrent deploys against a down Argo controller waste ~70s total.
- **Fix:** Wrap WorkflowSubmitter in main.go (1 line).

### HIGH-5: failRollback acquires advisory lock with empty coordinates
- **ID:** find_arch_rollback_lock
- **File:** api/internal/services/rollback_orchestrator.go:240
- **Impact:** All rollback failures globally serialized. Release stuck in ROLLING_BACK with no recovery path.
- **Fix:** Pass correct tenant/project/env to failRollback.

### HIGH-6: Release supersession query races with own activation (CODE-0001)
- **File:** api/internal/services/container_delivery.go:320
- **Impact:** `GetActiveByEnvTx` runs AFTER the new release is set ACTIVE, potentially returning it instead of the previous one. Old release stays permanently ACTIVE (split-brain).
- **Fix:** Move `GetActiveByEnvTx` to BEFORE `UpdateStatus(ReleaseSyncCompleted)`.

### HIGH-7: 7 silent error swallowing patterns in production code
- **IDs:** find_silent_0001 through find_silent_0007
- **Impact:** Operations appear to succeed but have partial failures invisible to the user.
- **Key instances:**
  - Release supersession error silently discarded (split-brain risk)
  - RELEASING→SUCCEEDED transition error only logged (build stuck forever)
  - Git config errors silently discarded during gitops push

### HIGH-8: CF KV subdomain generation bug — inline differs from cfkv.Subdomain()
- **ID:** ARCH-004
- **File:** api/internal/services/build_orchestrator.go:710-712
- **Impact:** Inline subdomain generation missing `TrimRight(subdomain, "-")` that `cfkv.Subdomain()` applies. Causes silent static frontend routing failures when truncated key ends with hyphen.
- **Fix:** Replace inline block with `cfkv.Subdomain()` call.

---

## Medium Findings (P2 — Fix Next Sprint)

| Finding | Category | Component |
|---------|----------|-----------|
| 3 CRDs without controllers | Completeness | Operators |
| Token cache 5-min revocation window | Security | API auth |
| Rate limiter goroutine leak on shutdown | Code | API middleware |
| No image signing/SBOM | Infrastructure | CI/CD |
| Documentation drift (loopzero status) | Completeness | CLAUDE.md |
| + 33 more | Various | Various |

---

## Architecture Assessment

### Strengths
- **DIP everywhere:** All external deps behind interfaces (15+ interfaces)
- **Strict state machines:** 7 machines, zero direct mutations, panic on invalid transitions
- **Consistent locking:** EnvironmentLocker used by all 10 mutating services
- **Clean module boundaries:** No circular deps, pkg/ has zero external deps
- **Good test pyramid:** 103+ test files, 890+ tests across 4 layers

### Weaknesses
- **build_orchestrator.go at 736 lines** — borderline but cohesive
- **"Implemented but not wired" systemic pattern** — circuit breaker and startup reconciler found
- **3 orphaned CRD types** without controllers

---

## Security Assessment

### OWASP Top 10 Status
- **A01 (Access Control):** ⚠️ — tenant_id on all queries, BUT /internal/bootstrap and /internal/argocd-callback have NO auth
- **A02 (Crypto Failures):** ❌ — Secrets column named encrypted_value but stored as PLAINTEXT. PG sslmode=disable.
- **A03 (Injection):** ⚠️ — SQL parameterized, BUT LogQL `step` parameter allows injection
- **A07 (Auth Failures):** ⚠️ — 5-min cache window, no server-side revocation
- **A08 (Data Integrity):** ⚠️ — no image signing
- **A09 (Logging):** ⚠️ — Gitea token leaked in source store error logs

### Threat Models
- 13 threat models registered
- Key threats: credential stuffing, supply chain compromise via CI, internal endpoint abuse, secret exfiltration via DB breach

---

## Operational Readiness

| Check | Status |
|-------|--------|
| Structured logging | ✅ slog JSON |
| Correlation IDs | ✅ 78 refs across 22 files |
| Prometheus metrics | ✅ Lock, build, HTTP metrics |
| Grafana dashboards | ✅ kube-prometheus-stack |
| Loki log aggregation | ✅ Build logs via LogQL |
| Rate limiting | ✅ Per-tenant, in-memory |
| Graceful shutdown | ✅ ShutdownCtx pattern |
| Health checks | ✅ /api/v1/health + /api/v1/health/deep |

---

## System Invariants

| # | Invariant | Status |
|---|-----------|--------|
| 1 | Tenant data isolation (all queries scoped) | ✅ Validated |
| 2 | State machine strictness (no bypasses) | ✅ Validated |
| 3 | Advisory lock on all mutations | ✅ Validated (with failRollback exception) |
| 4 | No internal terms in CLI output | ✅ Validated |
| 5 | Parameterized SQL queries only | ✅ Validated |

---

## Remediation Plan

### P0 — Fix Immediately (4 findings)
1. **Encrypt secrets at rest** — implement AES-256-GCM before DB storage (SEC-001)
2. **Add auth to /internal/bootstrap** — wire BOOTSTRAP_SECRET validation (SEC-003)
3. **Add test gate to build-images.yml** — `workflow_run` dependency on CI (CICD-002)
4. **Pin all GitHub Actions to SHA digests** (CICD-001)

### P1 — Fix This Sprint (23 findings)
1. **Fix Gitea token leak in source store** — apply sanitizer pattern (SEC-002)
2. Wire CircuitBreakerWorkflowSubmitter in main.go
3. Fix failRollback empty lock coordinates
4. Fix release supersession query race (CODE-0001)
5. Fix CF KV subdomain generation bug (ARCH-004)
6. Fix 7 silent error swallowing patterns
7. Add auth to /internal/argocd-callback (SEC-004)
8. Fix LogQL step parameter injection (SEC-005)
9. Enable PostgreSQL sslmode in production (SEC-006)

### P2 — Fix Next Sprint (38 findings)
1. Implement AppSecret controller or add admission webhook
2. Add token cache invalidation endpoint
3. Fix rate limiter goroutine leak
4. Add image signing (cosign) and SBOM generation (syft)
5. Update stale CLAUDE.md documentation

### P3 — Backlog (28 findings)
1. Remove dead code (startup_reconciler.go)
2. Implement Sprint 4-5 CLI commands (promote, cancel, etc.)
3. Address remaining low-severity findings

---

## Review Metadata

| Metric | Value |
|--------|-------|
| Phases completed | 8/8 |
| Quality gates passed | 7/7 |
| Components mapped | 20 |
| Flows traced | 10 |
| Findings registered | 91 |
| Invariants validated | 5 |
| Threat models | 13 |
| Specialist agents used | 14 |
| Total iterations | 1 (no loop-back needed) |
