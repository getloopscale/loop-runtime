# Coverage 80% Plan

> Objetivo: elevar TODOS os packages com cobertura "Baixa" (<50%) para pelo menos "Média" (≥60%), atingindo target geral de ~80%.
> 16 packages precisam subir. Organizados em 6 sprints por prioridade de risco.
> Estimativa total: ~250 novos testes.

---

## Estado Atual vs Target

| Package | Atual | Target | Gap | LOC prod | Testes necessários (est.) |
|---------|-------|--------|-----|----------|--------------------------|
| `api/internal/db/` | 4.4% | 60% | +55.6% | 3,364 | ~40 |
| `api/internal/routes/` | 3.0% | 60% | +57.0% | 4,664 | ~50 |
| `api/internal/services/` | 20.9% | 60% | +39.1% | 5,961 | ~40 |
| `api/internal/source/` | 16.5% | 60% | +43.5% | 785 | ~10 |
| `api/internal/cfkv/` | 16.5% | 60% | +43.5% | 248 | ~5 |
| `pkg/enforcement/` | 19.0% | 80% | +61.0% | 297 | ~8 |
| `api/internal/argocd/` | 25.4% | 60% | +34.6% | 615 | ~10 |
| `api/internal/middleware/` | 26.5% | 60% | +33.5% | 1,151 | ~15 |
| `api/internal/cfzone/` | 27.7% | 60% | +32.3% | 292 | ~5 |
| `pkg/jwt/` | 30.9% | 80% | +49.1% | 303 | ~8 |
| `api/internal/adapters/` | 40.2% | 60% | +19.8% | 483 | ~5 |
| `api/internal/placement/` | 41.5% | 60% | +18.5% | 949 | ~10 |
| `api/internal/metrics/` | 41.7% | 60% | +18.3% | 568 | ~5 |
| `api/internal/gitops/` | 43.1% | 60% | +16.9% | 878 | ~10 |
| `api/internal/lock/` | 0.0% | 60% | +60.0% | 152 | ~5 |
| `api/internal/workqueue/` | 0.0% | 60% | +60.0% | 169 | ~5 |
| **Total** | | | | **~21K** | **~230** |

---

## Sprint C1 — Persistence Layer (db/) — 4.4% → 60%

> **Prioridade máxima:** Cada query SQL errada corrompe dados silenciosamente.
> **Abordagem:** Testes contra interface fake (in-memory), não PG real (integration tests separados).

### Task C1.1: Store CRUD tests — build_store, release_store, project_store

**Criar:** `api/internal/db/build_store_test.go`

| # | Teste | O que verifica |
|---|-------|---------------|
| 1 | `TestBuildStore_Create_ReturnsID` | Insert retorna build ID gerado |
| 2 | `TestBuildStore_GetByID_Found` | Get por ID retorna build correto |
| 3 | `TestBuildStore_GetByID_NotFound` | Get inexistente retorna ErrBuildNotFound |
| 4 | `TestBuildStore_HasActiveBuild_True` | Build QUEUED/RUNNING/RELEASING → true |
| 5 | `TestBuildStore_HasActiveBuild_False` | Sem build ativo → false |
| 6 | `TestBuildStore_UpdateStatus_ValidTransition` | QUEUED→RUNNING seta started_at |
| 7 | `TestBuildStore_UpdateStatus_InvalidTransition` | SUCCEEDED→RUNNING → error |
| 8 | `TestBuildStore_SetWorkflowID` | Persiste workflow ID |
| 9 | `TestBuildStore_SetAppsSpec_GetBuildApps` | JSON round-trip apps |
| 10 | `TestBuildStore_ListByProject_Pagination` | Limit/offset funciona |
| 11 | `TestBuildStore_FindActiveBuilds_CellScoped` | Filtra por cell_id |
| 12 | `TestBuildStore_CancelActiveBuilds_Batch` | Cancela múltiplos, retorna workflow IDs |

**Criar:** `api/internal/db/release_store_test.go`

| # | Teste | O que verifica |
|---|-------|---------------|
| 13 | `TestReleaseStore_Create_ReturnsID` | Insert retorna release ID |
| 14 | `TestReleaseStore_GetByID_Found` | Get por ID |
| 15 | `TestReleaseStore_GetActiveByEnv_ReturnsLatest` | ORDER BY created_at para supersede correto |
| 16 | `TestReleaseStore_UpdateStatus_ValidTransition` | PENDING→ACTIVE |
| 17 | `TestReleaseStore_FindPendingReleases` | Filtra por status PENDING |

**Criar:** `api/internal/db/project_store_test.go`

| # | Teste | O que verifica |
|---|-------|---------------|
| 18 | `TestProjectStore_Ensure_CreatesNew` | Insert on conflict |
| 19 | `TestProjectStore_Ensure_Idempotent` | Segunda chamada não duplica |
| 20 | `TestProjectStore_GetByName_Found` | Get por nome |
| 21 | `TestProjectStore_GetByName_NotFound` | Retorna nil |

**Abordagem de teste:** Usar `pgx` com `pgxpool` mockado via interface `DBTX`. Os stores já dependem de `DBTX` interface — criar fake que retorna rows pré-configurados.

**Critérios de aceite:**
- [ ] 21 testes passando
- [ ] Testes NÃO precisam de PG real (usam fake DBTX)
- [ ] `go test ./api/internal/db/... -v` passa

---

### Task C1.2: Store tests — domain_store, tenant_store, secret_store, other stores

| # | Teste | Package |
|---|-------|---------|
| 22 | `TestDomainStore_Create_UniqueConstraint` | domain_store |
| 23 | `TestDomainStore_TransitionStatus_CAS` | domain_store |
| 24 | `TestDomainStore_CountByTenant` | domain_store |
| 25 | `TestDomainStore_ListByTenant_Filters` | domain_store |
| 26 | `TestTenantStore_Create` | tenant_store |
| 27 | `TestTenantStore_GetByID` | tenant_store |
| 28 | `TestSecretStore_Set_Encrypted` | secret_store |
| 29 | `TestSecretStore_Get_Decrypts` | secret_store |
| 30 | `TestSecretStore_Delete` | secret_store |
| 31 | `TestIdempotencyStore_Check_NewKey` | idempotency_store |
| 32 | `TestIdempotencyStore_Check_ExistingKey` | idempotency_store |
| 33 | `TestEnvironmentTargetStore_SetActive` | environment_target_store |
| 34 | `TestAuditStore_Append` | audit_store |
| 35 | `TestTeamStore_AddMember` | team_store |
| 36 | `TestTeamStore_ListMembers` | team_store |
| 37 | `TestBootstrapStore_MarkComplete` | bootstrap_store |
| 38 | `TestCellStore_List` | cell_store |
| 39 | `TestRepositoryStore_Connect` | repository_store |
| 40 | `TestMembershipStore_IsMember` | membership_store |

**Critérios de aceite:**
- [ ] 19 testes passando
- [ ] `go test ./api/internal/db/... -v` passa
- [ ] Coverage db/ ≥ 50%

---

## Sprint C2 — HTTP Route Handlers (routes/) — 3.0% → 60%

> **Abordagem:** HTTP handler tests com `httptest.NewRecorder` + fakes para services.
> Cada handler recebe interfaces — criar fakes simples.

### Task C2.1: Build + Source + Logs handlers

**Criar:** `api/internal/routes/builds_test.go`

| # | Teste | O que verifica |
|---|-------|---------------|
| 1 | `TestBuilds_Create_HappyPath` | POST /builds → 202 Accepted |
| 2 | `TestBuilds_Create_MissingFields` | POST sem project → 400 |
| 3 | `TestBuilds_Create_Idempotency` | Same key → same response |
| 4 | `TestBuilds_Get_Found` | GET /builds/{id} → 200 + JSON |
| 5 | `TestBuilds_Get_NotFound` | GET inexistente → 404 |
| 6 | `TestBuilds_Cancel_Success` | POST /builds/{id}/cancel → 200 |
| 7 | `TestBuilds_Cancel_AlreadyTerminal` | Build SUCCEEDED → 409 |

**Criar:** `api/internal/routes/source_test.go`

| # | Teste | O que verifica |
|---|-------|---------------|
| 8 | `TestSource_Upload_HappyPath` | POST multipart → 200 |
| 9 | `TestSource_Upload_TooLarge` | Body > limit → 413 |
| 10 | `TestSource_Upload_InvalidFormat` | Non-tarball → 400 |

**Criar:** `api/internal/routes/logs_test.go`

| # | Teste | O que verifica |
|---|-------|---------------|
| 11 | `TestLogs_Get_HappyPath` | GET /builds/{id}/logs → 200 |
| 12 | `TestLogs_Get_BuildNotFound` | Build inexistente → 404 |

---

### Task C2.2: Project + Team + Environment + Domain handlers

**Criar:** `api/internal/routes/projects_test.go`

| # | Teste |
|---|-------|
| 13 | `TestProjects_List` |
| 14 | `TestProjects_Get_Found` |
| 15 | `TestProjects_Get_NotFound` |
| 16 | `TestProjects_Create_HappyPath` |
| 17 | `TestProjects_Delete_Success` |

**Criar:** `api/internal/routes/teams_test.go`

| # | Teste |
|---|-------|
| 18 | `TestTeams_ListMembers` |
| 19 | `TestTeams_AddMember_HappyPath` |
| 20 | `TestTeams_AddMember_Duplicate` |
| 21 | `TestTeams_RemoveMember` |
| 22 | `TestTeams_UpdateRole` |

**Criar:** `api/internal/routes/environments_test.go`

| # | Teste |
|---|-------|
| 23 | `TestEnvs_List` |
| 24 | `TestEnvs_Create_HappyPath` |
| 25 | `TestEnvs_Delete` |

**Criar:** `api/internal/routes/domains_test.go`

| # | Teste |
|---|-------|
| 26 | `TestDomains_Create_HappyPath` |
| 27 | `TestDomains_Verify_Success` |
| 28 | `TestDomains_List` |
| 29 | `TestDomains_Delete` |

### Task C2.3: Remaining handlers (rollback, status, audit, bootstrap, repos, admin)

| # | Teste | Handler |
|---|-------|---------|
| 30 | `TestRollback_HappyPath` | rollback |
| 31 | `TestRollback_NoRelease` | rollback |
| 32 | `TestStatus_HappyPath` | status |
| 33 | `TestAudit_List` | audit |
| 34 | `TestBootstrap_AlreadyComplete` | bootstrap |
| 35 | `TestRepos_Connect` | repositories |
| 36 | `TestRepos_Disconnect` | repositories |
| 37 | `TestAdminEvict_Success` | admin_evict |

**Critérios de aceite:**
- [ ] 37 testes de route handlers passando
- [ ] Coverage routes/ ≥ 50%
- [ ] Cada teste usa `httptest.NewRecorder` + fake service

---

## Sprint C3 — Services Layer (services/) — 20.9% → 60%

> Os build reconcile + orchestrator + activator já têm testes. Faltam: domain, delivery, reapers, project deleter.

### Task C3.1: Domain services + Delivery

| # | Teste | File |
|---|-------|------|
| 1 | `TestDomainProvisioner_ProvisionCFZone` | domain_provisioner |
| 2 | `TestDomainProvisioner_HandleDNSConflict` | domain_provisioner |
| 3 | `TestDomainProvisioner_VerifyDomain` | domain_provisioner |
| 4 | `TestDomainHealthChecker_DetectsUnhealthy` | domain_health_checker |
| 5 | `TestDomainHealthChecker_SkipsRecent` | domain_health_checker |
| 6 | `TestContainerDelivery_Activate_HappyPath` | container_delivery |
| 7 | `TestContainerDelivery_Activate_GitOpsFails` | container_delivery |
| 8 | `TestStaticDelivery_Activate_WritesKV` | static_delivery |
| 9 | `TestStaticDelivery_Activate_KVFails` | static_delivery |

### Task C3.2: Reapers + GC + Project deleter + Rollback

| # | Teste | File |
|---|-------|------|
| 10 | `TestBuildReaper_CleansOldBuilds` | build_reaper |
| 11 | `TestBuildReaper_KeepsRecent` | build_reaper |
| 12 | `TestBuildReaper_KeepsLastN` | build_reaper |
| 13 | `TestReleaseReaper_CleansOldReleases` | release_reaper |
| 14 | `TestReleaseReaper_KeepsActive` | release_reaper |
| 15 | `TestBuildGC_DeletesOrphanedWorkflows` | build_gc |
| 16 | `TestProjectDeleter_FullCascade` | project_deleter |
| 17 | `TestProjectDeleter_PartialFailure` | project_deleter |
| 18 | `TestRollbackOrchestrator_HappyPath` | rollback_orchestrator |
| 19 | `TestRollbackOrchestrator_NoRelease` | rollback_orchestrator |
| 20 | `TestRollbackOrchestrator_DuringActiveDeploy` | rollback_orchestrator |

### Task C3.3: Convergence loop + remaining

| # | Teste | File |
|---|-------|------|
| 21 | `TestReleaseConvergenceLoop_DetectsStale` | release_convergence_loop |
| 22 | `TestReleaseConvergenceLoop_TriggersRetry` | release_convergence_loop |
| 23 | `TestOrphanDetector_FindsOrphans` | orphan_detector |
| 24 | `TestIdempotencyCleaner_PurgesOld` | idempotency_cleaner |

**Critérios de aceite:**
- [ ] 24 testes passando
- [ ] Coverage services/ ≥ 55%

---

## Sprint C4 — Middleware + ArgoCD + Source (26.5%, 25.4%, 16.5%) → 60%

### Task C4.1: Middleware tests

| # | Teste | File |
|---|-------|------|
| 1 | `TestAuth_ValidToken_SetsUser` | auth |
| 2 | `TestAuth_ExpiredToken_Returns401` | auth |
| 3 | `TestAuth_MissingToken_Returns401` | auth |
| 4 | `TestIdempotency_SameKey_ReturnsCache` | idempotency |
| 5 | `TestIdempotency_DifferentPayload_Returns422` | idempotency |
| 6 | `TestRateLimit_UnderLimit_Passes` | ratelimit |
| 7 | `TestRateLimit_OverLimit_Returns429` | ratelimit |
| 8 | `TestAudit_LogsAction` | audit |
| 9 | `TestCorrelation_SetsID` | correlation |
| 10 | `TestPGCircuitBreaker_OpenState_Returns503` | pgcircuitbreaker |
| 11 | `TestJWTValidator_ValidRSA` | jwt_validator |
| 12 | `TestRoles_AdminRequired_Passes` | roles |
| 13 | `TestRoles_AdminRequired_NonAdmin_Returns403` | roles |

### Task C4.2: ArgoCD sync client tests

| # | Teste | File |
|---|-------|------|
| 14 | `TestSyncClient_TriggerSync_HappyPath` | sync_client |
| 15 | `TestSyncClient_TriggerSync_AppNotFound` | sync_client |
| 16 | `TestSyncClient_WaitForSync_Succeeds` | sync_client |
| 17 | `TestSyncClient_WaitForSync_Timeout` | sync_client |
| 18 | `TestTokenProvider_ReadsFile` | token_provider |
| 19 | `TestTokenProvider_RefreshesOnChange` | token_provider |

### Task C4.3: Source store tests

| # | Teste | File |
|---|-------|------|
| 20 | `TestGogitStore_PushTarball` | gogit_store |
| 21 | `TestGogitStore_PushTarball_InvalidTar` | gogit_store |
| 22 | `TestGiteaStore_PushTarball` | gitea_store |
| 23 | `TestSourceValidator_ValidProject` | validator |
| 24 | `TestSourceValidator_PathTraversal` | validator |

**Critérios de aceite:**
- [ ] 24 testes passando
- [ ] Coverage middleware/ ≥ 55%, argocd/ ≥ 55%, source/ ≥ 55%

---

## Sprint C5 — Supporting Packages (cfkv, cfzone, gitops, placement, metrics) → 60%

### Task C5.1: Cloudflare + GitOps

| # | Teste | File |
|---|-------|------|
| 1 | `TestCFKV_Put_HappyPath` | cfkv/client |
| 2 | `TestCFKV_Get_NotFound` | cfkv/client |
| 3 | `TestCFKV_Delete` | cfkv/client |
| 4 | `TestCFZone_CreateZone` | cfzone/client |
| 5 | `TestCFZone_ScanDNS` | cfzone/client |
| 6 | `TestGiteaPusher_Push_HappyPath` | gitops/gitea_pusher |
| 7 | `TestGiteaPusher_Push_Conflict` | gitops/gitea_pusher |
| 8 | `TestGogitPusher_Push` | gitops/gogit_pusher |
| 9 | `TestManifestGate_RejectsCertificateCR` | gitops/manifest_gate |
| 10 | `TestManifestGate_AllowsDeployment` | gitops/manifest_gate |

### Task C5.2: Placement + Metrics + Lock + WorkQueue

| # | Teste | File |
|---|-------|------|
| 11 | `TestPlacementEngine_AssignsCell` | placement/engine |
| 12 | `TestPlacementEngine_RespectsConstraints` | placement/constraints |
| 13 | `TestDynamicRouter_RoutesByCell` | placement/dynamic_router |
| 14 | `TestSerializer_MarshalsPlacement` | placement/serializer |
| 15 | `TestMetrics_RegistersAll` | metrics/metrics |
| 16 | `TestMetrics_Middleware_RecordsLatency` | metrics/middleware |
| 17 | `TestLock_AcquiresAdvisoryLock` | lock/environment_lock |
| 18 | `TestLock_Timeout` | lock/environment_lock |
| 19 | `TestWorkQueue_EnqueueDequeue` | workqueue/queue |
| 20 | `TestWorkQueue_Consumer_Processes` | workqueue/consumer |

**Critérios de aceite:**
- [ ] 20 testes passando
- [ ] Todos os packages acima ≥ 55%

---

## Sprint C6 — Shared Libraries (pkg/) → 80%

### Task C6.1: JWT validator

| # | Teste | O que verifica |
|---|-------|---------------|
| 1 | `TestJWTValidator_ValidToken_ReturnsUser` | Token RSA válido → user extraído |
| 2 | `TestJWTValidator_ExpiredToken_ReturnsError` | Token expirado → error |
| 3 | `TestJWTValidator_WrongAudience_ReturnsError` | Audience mismatch → error |
| 4 | `TestJWTValidator_WrongIssuer_ReturnsError` | Issuer mismatch → error |
| 5 | `TestJWTValidator_InvalidSignature_ReturnsError` | Assinatura inválida |
| 6 | `TestJWTValidator_RotatedKey_AcceptsBothKids` | Dual-key rotation |

### Task C6.2: Enforcement limiter

| # | Teste | O que verifica |
|---|-------|---------------|
| 7 | `TestLimiter_ProjectsLimit_Enforced` | Starter 1 project → 2nd blocked |
| 8 | `TestLimiter_ProjectsLimit_Growth` | Growth 3 projects → 4th blocked |
| 9 | `TestLimiter_BuildsPerMonth_Enforced` | Monthly build limit |
| 10 | `TestLimiter_SeatsLimit_Enforced` | Team seat limit |
| 11 | `TestLimiter_EnvironmentsLimit_Enforced` | Environment limit |
| 12 | `TestEnforcementMiddleware_BlocksOverLimit` | HTTP 429 on limit exceeded |
| 13 | `TestEnforcementCounters_Increment` | Counter tracking |

**Critérios de aceite:**
- [ ] 13 testes passando
- [ ] pkg/jwt ≥ 75%, pkg/enforcement ≥ 75%

---

## Ordem de Execução

```
C1 (db/)        ──→ C2 (routes/)     [routes usam db stores como deps]
C3 (services/)  ──→ independente
C4 (middleware+) ──→ independente
C5 (supporting) ──→ independente
C6 (pkg/)       ──→ independente

C1 e C3 podem rodar em paralelo.
C2 depende de C1 (precisa de fakes dos stores).
C4, C5, C6 são totalmente independentes.
```

---

## Projeção de Cobertura

| Package | Atual | Após Plano | Target |
|---------|-------|-----------|--------|
| `api/internal/db/` | 4.4% | ~60% | 60% |
| `api/internal/routes/` | 3.0% | ~55% | 60% |
| `api/internal/services/` | 20.9% | ~60% | 60% |
| `api/internal/middleware/` | 26.5% | ~60% | 60% |
| `api/internal/argocd/` | 25.4% | ~60% | 60% |
| `api/internal/source/` | 16.5% | ~60% | 60% |
| `api/internal/cfkv/` | 16.5% | ~60% | 60% |
| `api/internal/cfzone/` | 27.7% | ~60% | 60% |
| `api/internal/gitops/` | 43.1% | ~60% | 60% |
| `api/internal/placement/` | 41.5% | ~60% | 60% |
| `api/internal/metrics/` | 41.7% | ~60% | 60% |
| `api/internal/adapters/` | 40.2% | ~65% | 60% |
| `api/internal/lock/` | 0.0% | ~60% | 60% |
| `api/internal/workqueue/` | 0.0% | ~60% | 60% |
| `pkg/jwt/` | 30.9% | ~80% | 80% |
| `pkg/enforcement/` | 19.0% | ~80% | 80% |
| **api/ global** | **~45%** | **~65%** | **60%** |
| **pkg/ global** | **~80%** | **~85%** | **80%** |
| **Sistema geral** | **~55%** | **~75%** | **80%** |

### Para atingir 80% global

Após este plano (~230 testes), a cobertura global sobe para ~75%. Para atingir 80%:
- CLI register/ files precisam de testes (atualmente 19%) — +15 testes
- api/internal/sse/ precisa subir de 59% para 75% — +5 testes
- api/internal/config/ precisa subir de 58% para 75% — +3 testes

**Total para 80%: ~250 testes novos (este plano) + ~23 adicionais = ~273 testes.**

---

## Gate de Conclusão

```bash
# Verificação final
go test ./api/... -short -cover 2>&1 | grep "^ok" | awk '{print $5}' | \
  sed 's/%//' | awk '{sum+=$1; n++} END {print "API avg coverage:", sum/n "%"}'

go test ./pkg/... -short -cover 2>&1 | grep "^ok" | awk '{print $5}' | \
  sed 's/%//' | awk '{sum+=$1; n++} END {print "PKG avg coverage:", sum/n "%"}'

# Nenhum package <50%:
go test ./api/... -short -cover 2>&1 | grep "^ok" | awk '{
  split($5, a, "%"); if (a[1]+0 < 50) print "BELOW 50%:", $2, $5
}'
```
