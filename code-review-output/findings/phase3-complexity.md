# Phase 3 — Complexity Scanner Report

**Date:** 2026-06-11
**Agent:** complexity-scanner
**Phase:** 3 (code_review)
**Target:** `/home/paulo/loopzero/.claude/worktrees/glowing-growing-babbage` (9 delivered dashboard journeys)
**Tools used:** `gocyclo` (Go), `lizard --CCN 15` (TypeScript + TSX)
**Severity source:** `consensus` (McCabe NIST 1976 + SonarQube defaults)

## Severity Rubric (McCabe NIST 1976)

| CC range | Severity | Risk band |
|---------:|----------|-----------|
| 11-15 | medium | moderate |
| 16-25 | high | high risk |
| 26-50 | critical | very high (untestable approaching) |
| > 50 | critical | untestable |

## Top 25 Hotspots (sorted by CC desc)

| Rank | CC | Severity | File:Line | Function | Lang | Notes |
|---:|---:|----------|-----------|----------|------|-------|
| 1 | **46** | critical | `api/internal/config/config.go:510` | `(Config).Validate` | go | Validation god-function |
| 2 | **44** | critical | `api/cmd/main.go:34` | `main` | go | Pre-existing P0 (CLAUDE.md) |
| 3 | **41** | critical | `api/internal/server/router.go:262` | `registerProtectedRoutes` | go | Pre-existing P0 (CLAUDE.md) |
| 4 | **40** | critical | `api/internal/sse/stream_handler.go:167` | `(*StreamHandler).ServeHTTP` | go | SSE hot path |
| 5 | **39** | critical | `cli/src/commands/scale.ts:65` | `execute` | ts | Multi-form CLI dispatch |
| 6 | **37** | critical | `api/internal/routes/secrets.go:290` | `(*SecretHandler).Bulk` | go | J-22 batch path |
| 7 | **35** | critical | `api/internal/middleware/audit_actions.go:22` | `deriveAction` | go | Mega switch |
| 8 | **34** | critical | `operators/internal/tenant/tenant_deletion.go:32` | `(*TenantReconciler).reconcileDelete` | go | Multi-stage finalizer |
| 9 | **34** | critical | `api/internal/routes/source.go:92` | `(*SourceHandler).Upload` | go | Upload pipeline |
| 10 | **34** | critical | `api/internal/routes/repositories.go:122` | `(*RepositoryHandler).Connect` | go | J-09 connect |
| 11 | **32** | critical | `operators/internal/app/exposure_controller.go:80` | `(*ExposureReconciler).Reconcile` | go | Reconcile loop |
| 12 | **32** | critical | `cli/src/commands/platform.ts:110` | `platformInstall` | ts | Install dispatch |
| 13 | **32** | critical | `cloud/dashboard/src/pages/projects.tsx:25` | `ProjectsPage` | tsx | J-07 hot page |
| 14 | **29** | critical | `api/internal/services/container_delivery.go:196` | `(*ContainerDeliveryActivator).phaseACreateRelease` | go | Release activation |
| 15 | **29** | critical | `api/internal/routes/builds.go:264` | `(*BuildHandler).Create` | go | Build submit |
| 16 | **29** | critical | `cli/src/commands/preview.ts:194` | `execute` | ts | Preview CLI |
| 17 | **27** | critical | `api/internal/services/build_orchestrator_complete.go:30` | `(*BuildOrchestrator).OnBuildCompleted` | go | Build completion |
| 18 | **27** | critical | `api/internal/archive/tar.go:62` | `SafeExtractTarGzReader` | go | Tar extract safety |
| 19 | **27** | critical | `api/cmd/setup_github.go:49` | `setupGitHub` | go | GH App setup |
| 20 | **26** | critical | `api/internal/routes/scale.go:120` | `(*ScaleHandler).Scale` | go | Scale dispatch |
| 21 | **25** | high | `api/internal/services/rollback_orchestrator.go:98` | `(*RollbackOrchestrator).Rollback` | go | Rollback state |
| 22 | **25** | high | `cloud/dashboard/src/pages/project-detail.tsx:29` | `ProjectDetailPage` | tsx | J-08 detail page |
| 23 | **24** | high | `cloud/dashboard/src/pages/register.tsx:27` | `RegisterPage` | tsx | J-02 register flow |
| 24 | **23** | high | `cloud/dashboard/src/pages/overview.tsx:50` | `OverviewPage` | tsx | J-06 overview |
| 25 | **22** | high | `cloud/dashboard/src/pages/memory/memories.tsx:50` | `MemoryListPage` | tsx | Memory section |

## Hot-Path Concentration

The CRITICAL (CC≥26) findings cluster in three architectural seams:

1. **Bootstrap/config** (4 findings — `Validate`, `main`, `setupGitHub`, `setup_server`): Long sequential setup chains.
2. **HTTP request handlers** (8 findings — `Bulk`, `Upload`, `Connect`, `Create`, `Scale`, `ServeHTTP`, `registerProtectedRoutes`, etc.): Each does its own validation + DB + side-effect, no dispatch table.
3. **Controllers/Orchestrators** (5 findings — `reconcileDelete`, `Reconcile`, `OnBuildCompleted`, `phaseACreateRelease`, `Rollback`): State-machine plus side-effect orchestration in single methods.

## Heavy Files (LOC > 500, scope filtered)

| LOC | File | Pre-existing? |
|---:|------|---------------|
| 1158 | `api/internal/db/generated/domains.sql.go` | sqlc-generated (OK) |
| 790 | `cli/src/commands/deploy.ts` | hot CLI |
| 768 | `api/internal/metrics/metrics.go` | metric registry |
| 768 | `api/internal/config/config.go` | god-config |
| 758 | `api/internal/routes/secrets.go` | J-22 handler |
| 755 | `cli/src/commands/platform.ts` | platform CLI |
| 714 | `cloud/internal/routes/api_keys.go` | api keys |
| 689 | `operators/internal/preview/preview_controller.go` | preview controller |
| 650 | `api/internal/argocd/kube_minting_token_provider.go` | ArgoCD provider |
| 620 | `api/internal/sse/stream_handler.go` | SSE handler |
| 612 | `api/cmd/setup_server.go` | bootstrap |
| 597 | `api/internal/routes/builds.go` | builds handler |
| 597 | `cloud/dashboard/src/pages/api-keys.tsx` | api keys page |

## Decomposition Suggestions (top 5 most impactful)

### 1. `(Config).Validate` (CC=46) — `api/internal/config/config.go:510`
Long if/else cascade validating ~30+ fields. **Refactor:** Define a `validator` slice `[]func(c *Config) error` and iterate. Each rule = single helper with CC≤3. Drops aggregate CC to ≤10.

### 2. `main` (CC=44) — `api/cmd/main.go:34` [Pre-existing P0]
Already flagged in CLAUDE.md. Bootstrap concatenates ~15 init phases. **Refactor:** extract `initDB`, `initAuth`, `initMetrics`, `initRouter`, `initOperators` helpers. Add a `runPhases(phases []bootstrapPhase) error` dispatcher.

### 3. `registerProtectedRoutes` (CC=41) — `api/internal/server/router.go:262` [Pre-existing P0]
Huge route table. **Refactor:** Move route declarations into `[]routeDef{ Method, Path, Handler, Middleware }` and loop. The CC drops to ~5 (one branch per category).

### 4. `(*StreamHandler).ServeHTTP` (CC=40) — `api/internal/sse/stream_handler.go:167`
SSE multiplexer in single method. **Refactor:** Extract per-event-type handlers + an event-router map keyed by event kind.

### 5. `(*SecretHandler).Bulk` (CC=37) — `api/internal/routes/secrets.go:290`
J-22 batch endpoint. **Refactor:** Split into (a) request-shape validation, (b) per-item validation loop, (c) atomic transaction phase, (d) audit-log emission. Each phase CC≤8.

## Cross-Mandate (hardcoded literals correlation)

Functions with CC > 25 in routes/services SHOULD also be grepped for embedded magic numbers/strings — per mandate. High-priority dual-finding candidates:

- `(Config).Validate` — likely embeds version constraints, regex patterns, allowed enums
- `deriveAction` — switch on string action names (audit labels are stable contracts but check for typos)
- `SafeExtractTarGzReader` — size limits, file-mode literals
- `setupGitHub` — webhook event names, scope names

(Flag for `pattern-suggester` to follow up — out of scope for this agent.)

## Tools Status

| Tool | Status | Path |
|------|--------|------|
| gocyclo | OK (installed at `/home/paulo/go/bin/gocyclo`) | scanned all Go dirs |
| lizard | OK (installed at `/home/paulo/.local/bin/lizard`) | scanned TS + TSX |
| complexipy | not used (no Python in scope) | N/A |
| radon | not used (no Python in scope) | N/A |

All 3 tool-runs persisted in DB (tool_run ids 1-3). Raw outputs at:
- `code-review-output/audit/gocyclo_raw.txt` (82 functions CC≥15)
- `code-review-output/audit/lizard_ts.txt` (13 TS warnings)
- `code-review-output/audit/lizard_tsx_v2.txt` (9 TSX warnings)

## DB findings inserted: 25 (ids 1-25)

All with `category=complexity`, `severity_source=consensus`, `cc_value` populated as float.
