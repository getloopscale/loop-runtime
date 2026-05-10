# Plan: Deep Review 2026-05-10 Remediation

> **Version 1.1** — Corrige todos os 32 achados do Deep Review sistêmico de 2026-05-10 (`review-output/final_report.md`), organizados em 6 fases por prioridade (P0→P3). O objetivo é levar a plataforma de "NOT ready for customer traffic" para "launch-ready" eliminando 5 criticals, 15 highs, e 12 mediums. Os findings são bugs localizados, não falhas sistêmicas — a maioria requer <30 min cada. Estimativa total: 5 dias de trabalho focado.
>
> **v1.1 changelog (post `/edge-case-plan` review):** 2 MUST FIX incorporated: EC-1 (nil-factory fallback test added to T0.1 TDD); EC-2 (GetByID renamed to GetByIDInternal for explicit intent in T0.4). 4 SHOULD TEST cases documented in edge-cases file. 2 DOCUMENT items noted (T0.3 severity downgraded to dead env var; T3.4 orphan mitigation via cascade=false). See `docs/plans/deep-review-2026-05-10-remediation-edge-cases.md`.

## Context

O Deep Review de 2026-05-10 (8 fases, 7 agentes especialistas, SQLite DB com 32 findings) identificou:
- **5 Critical:** pgDedup context orphan, preview wrong client (multi-cell), port conflict, IDOR+revocation toxic combo, supply chain toxic combo
- **15 High:** Missing locks (secrets+preview), dual-ACTIVE release, SM panic, reaper 409, admin secrets divergent, no bundle integrity, BuildStore IDOR, callback false success, placement no timeout, deploy.delivery_failed unhandled, 17 dead alerts, ArgoCD uses bootstrap charts, service URL divergence, missing FK index
- **12 Medium:** Error shape mismatch, retrigger race, metric premature, RLS bypass, rate limiter per-tenant, admission dual-owner, app controller double write, billing raw SQL, cell pinner K8s direct, framework allowlist DRY, integration tests thin, CF KV silent failure

O padrão sistêmico dominante é **"implemented but not wired"** — helpers/events/abstrações existem mas call-sites não foram conectados.

**Baseline:** Maturity 3.9/5.0. 12/12 cleanroom dogfood GREEN. 83/83 journey suite PASS.
**Target:** Maturity 4.1/5.0 após P0+P1. 4.3 após P2+P3.

## Objective

**Done = todos os 32 findings corrigidos com testes, `/dogfood full` GREEN, zero regressions.**

Goals mensuráveis:
- Zero criticals open
- Zero highs open
- 3 violated invariants restaurados: environment lock on all mutations, cell-aware routing, tenant isolation at DB layer
- 2 toxic combinations eliminadas (TC-1 IDOR, TC-2 supply chain)
- 17 dead alerts removidos

## ADRs

### D1 — Fix Localized Bugs, Not Redesign
**Decision:** Cada fix é cirúrgico no local identificado. Nenhum redesign arquitetural.
**Rationale:** Deep Review confirmou que findings são bugs localizados, não falhas sistêmicas de design. Arquitetura (4 planos, state machines, advisory locks) está correta.
**Consequences:** Fixes são pequenos e isolados. Risco baixo de regressão. Cada um pode ser merged independentemente.

### D2 — Bootstrap Charts Are Production
**Decision:** Aceitar que `infra/helm/` (bootstrap) é o path de produção. Enhancear com startup probes + seccomp. Deprecar `charts/loopzero-api` e `charts/loopzero-cloud` como dead code.
**Rationale:** ArgoCD L3 já aponta para bootstrap. Migrar requer redesign do overlay inteiro sem benefício pre-release.
**Consequences:** Uma fonte de verdade para deployment. Published charts ficam como referência futura.

### D3 — Single Admin Secret Variable
**Decision:** Consolidar para `LOOPZERO_ADMIN_SERVICE_AUTH_SECRET` como único secret para todas rotas admin.
**Rationale:** 3 variáveis impossível de auditar/rotacionar consistentemente.
**Consequences:** Breaking change em env vars. Requer atualização em Pulumi + Helmfile + Tilt.

### D4 — Bundle Integrity via SHA256
**Decision:** SHA256 no upload → `builds.bundle_digest` → verificação no primeiro step do Argo Workflow.
**Rationale:** Sem digest, S3 substitution é indetectável. cosign assina output, não input.
**Consequences:** Nova migration (000015). Workflow template ganha step de verificação.

### D5 — Rate Limiter Composite Key
**Decision:** Key = `tenantID:userID` para per-user isolation.
**Rationale:** Team/Enterprise tenants compartilham bucket — single user pode DoS todos.
**Consequences:** Mais granular (never less restrictive). Backwards compatible.

## Dependency Graph

```
Phase 0 (P0 Critical) ──▶ Phase 1 (P1 Concurrency) ──▶ Phase 3 (P2 Infra)
                                     │                         │
Phase 2 (P1 Security) ──────────────┘ (parallel with P1)      ▼
                                                         Phase 4 (P2 Completeness)
                                                               │
                                                               ▼
                                                         Phase 5 (P3 Polish)
```

- Phase 0 blocks everything (P0 must ship first)
- Phases 1 and 2 are parallel (independent file sets)
- Phases 3, 4, 5 sequential after 1+2 complete

---

## Phase 0: P0 — Critical Fixes (Block Customer Traffic)

**Objective:** Eliminar os 5 findings critical que tornam a plataforma insegura ou broken.

### T0.1 — Preview Controller: Use Cell-Aware Client

#### Objective
Substituir `r.RuntimeClient` por `r.runtimeClientForPreview(ctx, preview)` em `reconcileDelete` e `reconcileCreating`.

#### Evidence
`preview_controller.go:516,520,253` usa client estático. Helper existe na linha 93 mas nunca é chamado.

#### Files to edit
```
operators/internal/preview/preview_controller.go — 3 line changes (253, 516, 520)
operators/internal/preview/preview_controller_unit_test.go — new test with dual-client
```

#### Deep file dependency analysis
- **preview_controller.go:** Controller de preview environments. 636 LOC. Gerencia lifecycle Creating→Active→Expiring→Deleted.
- **Mudança:** 3 substituições de `r.RuntimeClient` por `r.runtimeClientForPreview(ctx, preview)`.
- **Downstream:** envtest tests (`preview_controller_envtest_test.go`) — rodam com single API server, continuam passando. Novo unit test com dual-client.

#### Tasks
1. Linha 253: `controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, ns, ...)` → `controllerutil.CreateOrUpdate(ctx, r.runtimeClientForPreview(ctx, preview), ns, ...)`
2. Linha 516: `r.RuntimeClient.Get(ctx, ...)` → `rtClient := r.runtimeClientForPreview(ctx, preview); rtClient.Get(ctx, ...)`
3. Linha 520: `r.RuntimeClient.Delete(ctx, ...)` → `rtClient.Delete(ctx, ...)`
4. Novo test com 2 fake clients verificando que namespace ops vão para runtime client

#### TDD
```
RED:     TestReconcileDelete_UsesRuntimeClientForPreview — inject mgmt+runtime clients, assert Delete targets runtime
RED:     TestReconcileCreating_UsesRuntimeClientForPreview — assert CreateOrUpdate targets runtime
RED:     TestRuntimeClientForPreview_NilFactory_ReturnsFallback — factory=nil → returns r.RuntimeClient (not nil) [EC-1]
GREEN:   Apply 3-line substitution
REFACTOR: Extract rtClient variable to avoid repeated helper call in reconcileDelete
VERIFY:  cd operators && go test ./internal/preview/ -v
```

#### Acceptance Criteria
- [ ] `r.RuntimeClient` not referenced directly in reconcileDelete/reconcileCreating
- [ ] New tests pass with dual-client setup
- [ ] Existing envtest suite passes
- [ ] `go vet ./...` passes

#### DoD
- [ ] All tasks completed
- [ ] All tests passing
- [ ] `cd operators && go test ./...` green
- [ ] Zero lint warnings

---

### T0.2 — pgDedup.Release: Use Background Context in Defer

#### Objective
Garantir que pgDedup cleanup funciona mesmo durante graceful shutdown.

#### Evidence
`release_convergence_loop.go:196-198` — parent ctx usado no defer. On shutdown, ctx cancelled → Release fails → orphan claim.

#### Files to edit
```
api/internal/services/release_convergence_loop.go — lines 196-198 (defer block)
api/internal/services/release_convergence_loop_test.go — new test
```

#### Deep file dependency analysis
- **release_convergence_loop.go:** 530 LOC. Loop assíncrono que reconcilia PENDING→ACTIVE. Goroutine spawned at line 192.
- **Mudança:** 3 linhas no defer block — novo context com timeout.
- **Downstream:** Testes em `release_convergence_loop_test.go` usam fakes. Nenhuma interface muda.

#### Tasks
1. No defer block: `cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)`
2. `defer cleanupCancel()`
3. `r.pgDedup.Release(cleanupCtx, "releases", release.ID)`

#### TDD
```
RED:     TestPgDedupRelease_WorksWithCancelledParentCtx — cancel parent ctx before goroutine finishes, verify Release still called with non-cancelled context
GREEN:   Apply 3-line fix
REFACTOR: None
VERIFY:  cd api && go test ./internal/services/ -run TestPgDedup -v
```

#### Acceptance Criteria
- [ ] pgDedup.Release uses independent context
- [ ] Timeout = 5s
- [ ] Parent ctx cancellation does not affect cleanup

#### DoD
- [ ] All tests passing
- [ ] `cd api && go test ./...` green

---

### T0.3 — Port Conflict: Align LOOPZERO_API_PORT with service.port

#### Objective
Eliminar mismatch entre `LOOPZERO_API_PORT=8080` e `service.port=3001`.

#### Evidence
`infra/helm/loopzero-api/values.yaml:65` vs `:27`.

#### Files to edit
```
infra/helm/loopzero-api/values.yaml — fix or remove LOOPZERO_API_PORT
```

#### Deep file dependency analysis
- **values.yaml:** Configura bootstrap deployment. Se binário lê env var para porta, probes vão para porta errada.
- **Mudança:** 1 linha.
- **Downstream:** Tilt, Helmfile mgmt.helmfile.yaml.gotmpl podem override. Verificar.

#### Tasks
1. Verificar `api/internal/config/config.go` para `LOOPZERO_API_PORT`
2. Se lido pelo binário: mudar para `"3001"`
3. Se NÃO lido: remover a linha inteira (binary defaults to 3001)
4. `task validate:manifests`

#### TDD
```
RED:     helm template infra/helm/loopzero-api | grep LOOPZERO_API_PORT → should not show 8080
GREEN:   Fix values.yaml
VERIFY:  task validate:manifests
```

#### Acceptance Criteria
- [ ] No port mismatch between env var and service.port
- [ ] `task validate:manifests` passes

#### DoD
- [ ] Chart renders correctly

---

### T0.4 — BuildStore.GetByID: Add Tenant Filter

#### Objective
Fechar IDOR gap adicionando `AND tenant_id = $2` ao GetByID.

#### Evidence
`build_store.go:128-136` — `WHERE id = $1` sem tenant_id. Handler-level check é single point of failure.

#### Files to edit
```
api/internal/db/build_store.go — add GetByIDForTenant method
api/internal/routes/logs.go — migrate to GetByIDForTenant
api/internal/routes/logs_stream.go — same
api/internal/routes/builds.go — same (any user-facing GetByID caller)
api/tests/unit/builds_route_test.go — update fakes
```

#### Deep file dependency analysis
- **build_store.go:** Repository para builds. 28 stores total. GetByID usado por log streaming e status checks.
- **Mudança:** Novo método + migração de callers HTTP para versão scoped.
- **Downstream:** Interface BuildStore (se existir) precisa do novo método. Fakes nos testes.

#### Tasks
1. `GetByIDForTenant(ctx, tenantID, buildID string) (*Build, error)` com `AND tenant_id = $2`
2. Migrar `logs.go`, `logs_stream.go`, `builds.go` (callers HTTP) para `GetByIDForTenant`
3. Remover checks manuais `build.TenantID != uc.TenantID`
4. Renomear `GetByID` → `GetByIDInternal` com godoc: `// Deprecated: Use GetByIDForTenant for HTTP handlers. Required by convergence loops, reaper, and domain_provisioner.` [EC-2]
5. Atualizar todos os 10+ internal callers (domain_provisioner, build_reconcile_complete, build_reconcile_retry, build_orchestrator_cancel, release_reaper) para usar o novo nome
6. Atualizar fakes/interfaces nos testes

#### TDD
```
RED:     TestGetByIDForTenant_WrongTenant_ReturnsNotFound
RED:     TestGetByIDForTenant_CorrectTenant_Returns
GREEN:   Implement method + migrate callers
REFACTOR: Remove manual tenant checks in handlers
VERIFY:  cd api && go test ./internal/db/ -run TestGetByID -v && go test ./tests/unit/ -run TestLog -v
```

#### Acceptance Criteria
- [ ] `GetByIDForTenant` exists with tenant filter
- [ ] HTTP handlers use scoped version
- [ ] Cross-tenant access blocked at DB level
- [ ] Tests verify rejection

#### DoD
- [ ] All tests passing
- [ ] `cd api && go test ./...` green

---

### T0.5 — Policy-Controller: Flip to Deny Mode

#### Objective
Fechar supply chain gap mudando ClusterImagePolicy de warn para enforce.

#### Evidence
`infra/helm/admission-policies/` — policy em warn mode. Imagens não assinadas admitidas.

#### Files to edit
```
infra/helm/admission-policies/values.yaml — flip mode
infra/helm/admission-policies/templates/cluster-image-policy.yaml — verify mode rendering
```

#### Deep file dependency analysis
- **admission-policies chart:** Instala policy-controller + ClusterImagePolicy.
- **Mudança:** 1 campo (mode: warn → enforce).
- **Downstream:** Todo deploy precisa imagem assinada. Build pipeline já assina. k3d local precisa exception.

#### Tasks
1. Mudar mode para `enforce` em values.yaml
2. Garantir exception para `k3d-loopzero-registry:5000` (local unsigned images)
3. Verificar que build pipeline cosign step cobre TODAS imagens
4. `task validate:manifests`

#### TDD
```
RED:     Deploy unsigned image → webhook rejects (test via kubectl run with unsigned image)
GREEN:   Flip mode + add k3d exception
VERIFY:  task validate:manifests
```

#### Acceptance Criteria
- [ ] ClusterImagePolicy mode = enforce
- [ ] k3d local dev still works
- [ ] Signed images admitted
- [ ] Unsigned rejected

#### DoD
- [ ] Policy enforcing in cluster

---

## Phase 1: P1 — Concurrency & State Fixes

**Objective:** Restaurar invariant "environment lock on all mutations" e corrigir state machine bugs.

### T1.1 — SecretHandler: Add Environment Lock

#### Objective
Wrappear `Set` e `DeleteSecret` com `WithLock`.

#### Files to edit
```
api/internal/routes/secrets.go — add locker field, wrap methods
api/internal/server/setup_orchestrator.go — wire locker
api/tests/unit/secrets_route_test.go — add lock tests
```

#### Tasks
1. Adicionar `locker lock.EnvironmentLocker` ao struct (linha 31)
2. Wrap `Set` (linha 110) com `h.locker.WithLock(ctx, tenantID, projectID, env, fn)`
3. Wrap `DeleteSecret` (linha 199) com mesmo pattern
4. Atualizar constructor e wiring
5. Testes: lock acquired, lock timeout → 409

#### TDD
```
RED:     TestSecretSet_AcquiresLock
RED:     TestSecretSet_LockTimeout_Returns409
RED:     TestSecretDelete_AcquiresLock
GREEN:   Implement
VERIFY:  cd api && go test ./tests/unit/ -run TestSecret -v
```

#### Acceptance Criteria
- [ ] Both mutations protected by environment lock
- [ ] Lock timeout returns 409

#### DoD
- [ ] All tests passing

---

### T1.2 — PreviewService: Add Environment Lock

#### Files to edit
```
api/internal/services/preview_service.go — add Locker to deps, wrap CreatePreview
```

#### Tasks
1. Adicionar `Locker lock.EnvironmentLocker` ao `PreviewServiceDeps` (linha 60)
2. Wrap `CreatePreview` store operations com lock (key: tenantID, projectID, branch)

#### TDD
```
RED:     TestCreatePreview_AcquiresLock
GREEN:   Implement
VERIFY:  cd api && go test ./internal/services/ -run TestCreatePreview -v
```

#### Acceptance Criteria
- [ ] `CreatePreview` protected by lock
- [ ] Lock key uses branch as env equivalent

#### DoD
- [ ] All tests passing

---

### T1.3 — Supersession Error: Return to Roll Back Transaction

#### Files to edit
```
api/internal/services/release_activator.go — line 169: return error instead of log-only
api/tests/unit/release_activator_test.go — add test
```

#### Tasks
1. Substituir log-only por `return fmt.Errorf("activate release: supersede previous %s: %w", prevRelease.ID, supersedeErr)`

#### TDD
```
RED:     TestActivateReleaseKernel_SupersessionFails_RollsBack — fake returns error on supersede → new release stays PENDING
GREEN:   Return the error
VERIFY:  cd api && go test ./tests/unit/ -run TestActivateReleaseKernel -v
```

#### Acceptance Criteria
- [ ] Supersession failure rolls back entire transaction
- [ ] No dual-ACTIVE possible

#### DoD
- [ ] All tests passing

---

### T1.4 — Preview SM: Use Actual Phase + TryTransition

#### Files to edit
```
operators/internal/preview/preview_controller.go — line 531
```

#### Tasks
1. Substituir `statemachine.PreviewExpiring` por `statemachine.State(preview.Status.Phase)`
2. Usar `TryTransition` em vez de `Transition`

#### TDD
```
RED:     TestReconcileDelete_DeletingPhase_TransitionsToCleanupFailed — no panic
GREEN:   Fix source state + TryTransition
VERIFY:  cd operators && go test ./internal/preview/ -run TestReconcileDelete -v
```

#### Acceptance Criteria
- [ ] No panic on Deleting→CleanupFailed path
- [ ] Both Expiring and Deleting handled

#### DoD
- [ ] All tests passing

---

### T1.5 — Reaper MarkAsCleaned: Re-fetch Before Finalizer

#### Files to edit
```
operators/internal/app/application_reaper.go — lines 148-161
```

#### Tasks
1. Add `c.Get(ctx, client.ObjectKeyFromObject(app), app)` after Status().Update
2. Remove dead code (lines 154, 161)

#### TDD
```
RED:     TestMarkAsCleaned_NoConflictOnFinalizerRemoval
GREEN:   Add re-fetch
VERIFY:  cd operators && go test ./internal/app/ -run TestMarkAsCleaned -v
```

#### Acceptance Criteria
- [ ] No 409 on finalizer removal
- [ ] Dead code removed

#### DoD
- [ ] All tests passing

---

### T1.6 — Placement Serializer: Add lock_timeout

#### Files to edit
```
api/internal/placement/serializer.go — add SET LOCAL lock_timeout after BeginTx
```

#### Tasks
1. Add `tx.Exec(ctx, "SET LOCAL lock_timeout = '30s'")` after line 59
2. Handle SQLSTATE 55P03 → `ErrPlacementLockTimeout`

#### TDD
```
RED:     TestPlacementSerializer_ReturnsTimeoutError
GREEN:   Add lock_timeout
VERIFY:  cd api && go test ./internal/placement/ -run TestPlacement -v
```

#### Acceptance Criteria
- [ ] `lock_timeout = 30s` set in transaction
- [ ] Typed error for timeout

#### DoD
- [ ] All tests passing

---

### T1.7 — Callback Activator: Return Error for HTTP 500

#### Files to edit
```
api/internal/services/sync_callback_activator.go — collect errors, return aggregate
api/internal/routes/argocd_callback.go — handle error → 500
```

#### Tasks
1. Collect activation errors in slice
2. Return `fmt.Errorf(...)` if any failed
3. Route handler: error → HTTP 500

#### TDD
```
RED:     TestOnSyncCompleted_PartialFailure_ReturnsError
GREEN:   Implement
VERIFY:  cd api && go test ./internal/services/ -run TestOnSyncCompleted -v
```

#### Acceptance Criteria
- [ ] Partial failures return error
- [ ] ArgoCD gets 500 → retries

#### DoD
- [ ] All tests passing

---

## Phase 2: P1 — Security Fixes

**Objective:** Fechar gaps de segurança (admin secrets, bundle integrity, rate limiting).

### T2.1 — Admin Secrets: Consolidate (per ADR D3)

#### Files to edit
```
api/internal/middleware/auth.go — add RequireAdminService middleware
api/internal/routes/admin_pin_cell.go — remove inline comparison
api/internal/routes/admin_evict.go — remove inline comparison
api/internal/server/router.go — apply middleware to /api/v1/admin/*
infra/helm/loopzero-api/values.yaml — consolidate env vars
```

#### Tasks
1. Create `RequireAdminService()` middleware validating `LOOPZERO_ADMIN_SERVICE_AUTH_SECRET` with dual-key rotation
2. Apply to `/api/v1/admin/*` group in router
3. Remove inline `subtle.ConstantTimeCompare` from both handlers
4. Remove `LOOPZERO_SERVICE_AUTH_SECRET` from configs (keep only `LOOPZERO_ADMIN_SERVICE_AUTH_SECRET`)

#### TDD
```
RED:     TestAdminPin_RequiresAdminServiceAuth
RED:     TestAdminEvict_RequiresAdminServiceAuth
RED:     TestAdminService_DualKeyRotation
GREEN:   Implement middleware
VERIFY:  cd api && go test ./tests/unit/ -run TestAdmin -v
```

#### Acceptance Criteria
- [ ] Single env var for all admin ops
- [ ] Dual-key rotation supported
- [ ] No inline secret comparison in handlers

#### DoD
- [ ] All tests passing
- [ ] Config files updated

---

### T2.2 — Bundle SHA256 Digest (per ADR D4)

#### Files to edit
```
api/internal/db/migrations/000015_bundle_digest.up.sql — (NEW)
api/internal/db/build_store.go — store digest
api/internal/routes/source.go — compute SHA256
api/internal/argo/workflow_builder.go — add verify step
```

#### Tasks
1. Migration: `ALTER TABLE builds ADD COLUMN bundle_digest TEXT`
2. `source.go`: compute `sha256.Sum256`, store hex in build record
3. `workflow_builder.go`: add init container that downloads bundle, verifies digest, fails if mismatch

#### TDD
```
RED:     TestUploadSource_StoresDigest
RED:     TestWorkflowBuilder_HasDigestVerifyStep
GREEN:   Implement
VERIFY:  cd api && go test ./internal/routes/ -run TestUpload -v && go test ./internal/argo/ -run TestWorkflow -v
```

#### Acceptance Criteria
- [ ] SHA256 stored on upload
- [ ] Workflow verifies before build
- [ ] Tampered bundle → build failure

#### DoD
- [ ] All tests passing
- [ ] `task db:migrate` succeeds

---

### T2.3 — Rate Limiter: Per-User Key (per ADR D5)

#### Files to edit
```
api/internal/middleware/ratelimit.go — change key at line 99
```

#### Tasks
1. Change `key = uc.TenantID` → `key = uc.TenantID + ":" + uc.UserID`

#### TDD
```
RED:     TestRateLimiter_PerUserIsolation — user-A at limit, user-B unaffected
GREEN:   Change key
VERIFY:  cd api && go test ./internal/middleware/ -run TestRateLimiter -v
```

#### Acceptance Criteria
- [ ] Per-user isolation within tenant

#### DoD
- [ ] All tests passing

---

## Phase 3: P2 — Infrastructure & Observability

**Objective:** Dead alerts, missing index, chart hardening.

### T3.1 — Remove 17 Dead Prometheus Alerts

#### Files to edit
```
infra/helm/monitoring/manifests/prometheusrule-critical.yaml
infra/helm/monitoring/manifests/prometheusrule-database.yaml
infra/helm/monitoring/manifests/prometheusrule-warning.yaml
infra/helm/monitoring/manifests/prometheusrule-registry.yaml
```

#### Tasks
1. Remove all rules with `expr: vector(0) > 1`
2. `task validate:manifests`

#### Acceptance Criteria
- [ ] Zero `vector(0) > 1` in monitoring manifests

---

### T3.2 — Add Index on build_apps.build_id

#### Files to edit
```
api/internal/db/migrations/000016_build_apps_index.up.sql — (NEW)
```

#### Tasks
1. `CREATE INDEX IF NOT EXISTS idx_build_apps_build_id ON build_apps (build_id);`

#### Acceptance Criteria
- [ ] Index exists
- [ ] `task db:migrate` + `task db:reset` succeed

---

### T3.3 — Bootstrap Charts: Add Startup Probes + Seccomp

#### Files to edit
```
infra/helm/loopzero-api/templates/deployment.yaml
infra/helm/loopzero-cloud/templates/deployment.yaml
```

#### Tasks
1. Add startupProbe: httpGet /health, failureThreshold 30, periodSeconds 2
2. Add seccompProfile: type: RuntimeDefault

#### Acceptance Criteria
- [ ] Both charts have startupProbe + seccomp
- [ ] `task validate:manifests` passes

---

### T3.4 — Remove ArgoCD Dual-Owner for Admission Policies

#### Files to edit
```
infra/argocd-bootstrap/apps/runtime-admission-policies.yaml — DELETE
```

#### Acceptance Criteria
- [ ] File deleted
- [ ] Helmfile L2 is sole owner

---

## Phase 4: P2 — Completeness & Contracts

**Objective:** CLI event handler, error shapes, metric placement.

### T4.1 — CLI: Add deploy.delivery_failed Handler

#### Files to edit
```
cli/src/commands/deploy.ts — add case ~line 600
cli/tests/unit/commands/deploy.test.ts — add test
```

#### Tasks
1. Add case `'deploy.delivery_failed'`: message "Deploy failed: could not deliver manifests" + `finish(3)`

#### TDD
```
RED:     test('handles deploy.delivery_failed with exit 3')
GREEN:   Add case
VERIFY:  cd cli && npm test -- --testPathPattern=deploy
```

#### Acceptance Criteria
- [ ] Event handled with exit 3
- [ ] No internal terms in message

---

### T4.2 — Error Shape: Migrate 4 Route Files to LoopZeroError

#### Files to edit
```
api/internal/routes/domains_create.go
api/internal/routes/domains_dns.go
api/internal/routes/domains_verify.go
api/internal/routes/repositories.go
```

#### Tasks
1. Replace all `writeJSON(w, code, map[string]string{"error": "..."})` with `writeLoopZeroError`
2. Add error codes to `pkg/errors` as needed

#### TDD
```
RED:     TestDomainsCreate_ErrorsAreLoopZeroError — verify JSON shape {code, message}
GREEN:   Replace all 34 call sites
VERIFY:  cd api && go test ./tests/unit/ -run TestDomain -v
```

#### Acceptance Criteria
- [ ] Zero raw error maps in 4 files
- [ ] All use LoopZeroError shape

---

### T4.3 — Metric: Move SetToCurrentTime After Query

#### Files to edit
```
api/internal/services/release_convergence_loop.go — move line 166
```

#### Tasks
1. Move `SetToCurrentTime()` to after successful `FindPendingReleases` (before early return on error)

#### TDD
```
RED:     TestTick_DBError_DoesNotUpdateMetric
GREEN:   Move line
VERIFY:  cd api && go test ./internal/services/ -run TestTick -v
```

#### Acceptance Criteria
- [ ] Metric only set after success

---

## Phase 5: P3 — Polish & Architecture Cleanup

**Objective:** Refatorações menores para qualidade de código.

### T5.1 — App Controller: Remove Deferred Status().Update
Remove defer block (lines 91-101), fold `SetReadinessSummaryViaPkg` before explicit Patch.

### T5.2 — Billing.go: Extract to billing_store.go
Create `api/internal/db/billing_store.go` with `FetchPlan` and `FetchStripeCustomerID`.

### T5.3 — Cell Pinner: Extract TenantCRPatcher Interface
Replace `dynamic.Interface` in `cell_pinner.go` with injected `TenantCRPatcher` interface.

### T5.4 — Framework Allowlist: Single Source
Export `SupportedFrameworks()` from `argo/languages.go`, call from `routes/builds.go`.

### T5.5 — Sync Retrigger: Atomic Increment
Replace `LoadOrStore`/`Store` with `sync.Map` of `*atomic.Int32`.

### T5.6 — deploy.completed: Add Retry
3-attempt retry with 100ms backoff for `eventStore.Append("deploy.completed")`.

### T5.7 — Move Error Sentinels to pkg/errors
Move `ErrTenantNotFound` etc from `services/` to `pkg/errors`. Update import in `routes/admin_pin_cell.go`.

### T5.8 — RLS: Separate Internal Role (Deferred to Sprint 9)
Document as backlog. Requires Pulumi + managed PG role provisioning changes.

---

## Coverage Matrix

| # | Finding | Task | Resolution |
|---|---------|------|-----------|
| 1 | Preview wrong client (CRITICAL) | T0.1 | Use runtimeClientForPreview |
| 2 | pgDedup cancelled ctx (CRITICAL) | T0.2 | context.Background() in defer |
| 3 | Port conflict (CRITICAL) | T0.3 | Align/remove LOOPZERO_API_PORT |
| 4 | IDOR GetByID (CRITICAL combo) | T0.4 | GetByIDForTenant |
| 5 | Supply chain warn-mode (CRITICAL combo) | T0.5 | Flip to deny |
| 6 | SecretHandler no lock (HIGH) | T1.1 | Add EnvironmentLocker |
| 7 | PreviewService no lock (HIGH) | T1.2 | Add EnvironmentLocker |
| 8 | Dual-ACTIVE release (HIGH) | T1.3 | Return supersession error |
| 9 | Preview SM panic (HIGH) | T1.4 | TryTransition + actual phase |
| 10 | Reaper 409 (HIGH) | T1.5 | Re-fetch before finalizer |
| 11 | Placement no timeout (HIGH) | T1.6 | lock_timeout = 30s |
| 12 | Callback false success (HIGH) | T1.7 | Return aggregate error |
| 13 | Admin secrets (HIGH) | T2.1 | Single variable + middleware |
| 14 | No bundle integrity (HIGH) | T2.2 | SHA256 digest |
| 15 | Rate limiter per-tenant (MEDIUM) | T2.3 | Composite key |
| 16 | Dead alerts (HIGH) | T3.1 | Remove 17 rules |
| 17 | Missing FK index (HIGH) | T3.2 | CREATE INDEX migration |
| 18 | ArgoCD bootstrap (HIGH) | D2 + T3.3 | Enhance bootstrap, deprecate published |
| 19 | Admission dual-owner (MEDIUM) | T3.4 | Remove ArgoCD app |
| 20 | CLI delivery_failed (HIGH) | T4.1 | Add handler |
| 21 | Error shape (MEDIUM) | T4.2 | LoopZeroError migration |
| 22 | Metric premature (MEDIUM) | T4.3 | Move after query |
| 23 | App controller double write (MEDIUM) | T5.1 | Remove deferred |
| 24 | Billing raw SQL (MEDIUM) | T5.2 | Extract store |
| 25 | Cell pinner K8s direct (MEDIUM) | T5.3 | Extract interface |
| 26 | Framework allowlist DRY (MEDIUM) | T5.4 | Single source |
| 27 | Sync retrigger race (MEDIUM) | T5.5 | Atomic increment |
| 28 | deploy.completed swallowed (MEDIUM) | T5.6 | Add retry |
| 29 | Routes import services (MEDIUM) | T5.7 | Move to pkg/errors |
| 30 | RLS bypass (MEDIUM) | T5.8 | Deferred Sprint 9 |
| 31 | Service URL divergence (HIGH) | D2 | Deprecate published charts |
| 32 | CF KV silent failure (MEDIUM) | Note | Accepted risk (in-cluster fallback documented) |

**Coverage: 32/32 findings addressed (100%)**

## Global Definition of Done

- [ ] All phases completed (Phase 0-5)
- [ ] All tests passing (Go + TypeScript)
- [ ] Zero lint warnings (golangci-lint + ESLint)
- [ ] `task validate:manifests` passes
- [ ] `task db:migrate` succeeds
- [ ] 3 violated invariants restored
- [ ] Zero critical findings open
- [ ] Zero high findings open
- [ ] **`/cross-validation deep-review-2026-05-10-remediation` PASSED**
- [ ] **`/dogfood full` PASSED**

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

### Execution

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up
task doctor

# 2. Full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] Prova de Vida passes for every deploy
- [ ] Prova de Morte passes for every delete
- [ ] No regressions in previously passing flows
- [ ] Observability check passes
- [ ] Rule 3 clean: zero internal terms

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run on fresh infrastructure
3. Repeat until zero S0/S1
