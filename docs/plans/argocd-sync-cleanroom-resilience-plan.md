# Plan: ArgoCD Sync Cleanroom Resilience — Fixing F-01 from dogfood 2026-05-08

> **Version 1.1** (2026-05-08): T2.1 amendment — T2.1 is NO-OP because CLI already handles `deploy.sync_failed` SSE event with exit code 3 (cli/src/commands/deploy.ts:570) and API already emits this event when sync trigger fails non-recoverable (api/internal/services/container_delivery.go:131 → phaseEFinalize verifyFailed=true → eventStore.Append "deploy.sync_failed" line 431). Plan reduced to T1.1 (Helm timeout) + T1.2 (heal H18 extension) + T3.1 (cleanroom + dogfood gate).
>
> **Version 1.0** — Corrigir o achado F-01 (S0 — Bloqueante) revelado pelo `/dogfood full` em 2026-05-08: builds Argo Workflow `Succeeded` mas DB build status `FAILED` com `Release rel-... failed: SYNC_FAILED` e API log `[PLATFORM_DEGRADED] deploy service is temporarily unavailable`. Root cause: ArgoCD `application refresh deadline exceeded` (15s default) sob carga concorrente de cleanroom + repo-server cold-cache → circuit breaker abre. Cascade quebra ~70% dos journeys (10 fails na journey suite + 6 no edge). Plus 1 fix infra cosmético (F-02 — `argocd-repo-gitea-internal` Helm stub recriado pós-H18).

## Context

### O que existe hoje (AS-IS)

- **`infra/helm/argocd/values.yaml`**: zero override para `controller.repoServerTimeoutSeconds`. Default ArgoCD = **15 segundos** — insuficiente para cold-cache cleanroom com 10+ deploys concorrentes.
- **`api/internal/argocd/sync_client.go:118 TriggerSync`**: hard-refresh (B32) + POST /sync. 409 returns `ErrSyncOperationInProgress` (NOT counted toward circuit breaker per MEET-2026-03-29-019). Circuit breaker pattern presumível em camada acima (container_delivery.go).
- **`api/internal/services/container_delivery.go:128`**: log `argocd trigger sync failed after retries` → finalize release como FAILED.
- **`infra/scripts/k3d-cross-cluster-heal.sh:H18`**: deleta `gitea-credentials-template` (added in dogfood-2026-05-08-fixes plan). NÃO cobre `argocd-repo-gitea-internal` (Opaque stub `type=git + url`, repository label) que ArgoCD Helm chart recria automaticamente.
- **CLI `loopzero deploy`**: retorna exit 0 quando build é submitted, mesmo se Release downstream falha SYNC_FAILED. Falso positivo: usuário acha que app está live.

### Evidência

- **JUnit XMLs gerados em /dogfood full**:
  - `/tmp/dogfood-ginkgo-journey-1778264777.xml` — 25 PASS / **10 FAIL** / 290 SKIP em 45m28s
  - `/tmp/dogfood-ginkgo-edge-1778267540.xml` — 14 PASS / **6 FAIL** / 305 SKIP em 26m02s
- **DB**: 8 builds recentes em `FAILED` com `error_message='Release rel-... failed: SYNC_FAILED'`
- **Argo Workflows (build cluster)**: 5/5 builds recentes `Succeeded` (build pipeline OK; sync downstream broken)
- **API ERROR**: `argocd trigger sync failed after retries — [PLATFORM_DEGRADED] deploy service is temporarily unavailable`
- **argocd-server logs**:
  - `application refresh deadline exceeded` (15s timeout)
  - `another operation is already in progress` (FailedPrecondition)
- **CLAUDE.md**: documenta B24 candidato — "API build reconciler not propagating Argo Workflow Succeeded → DB build.status=SUCCEEDED" do real-paas-cleanroom-dogfood-loop. F-01 é variante deste.

## Objective

**Done =** Cleanroom + journey suite returns ≥80/82 PASS (warm-infra baseline restaurado), zero `[PLATFORM_DEGRADED]` log lines durante deploy normal, CLI sinaliza exit 3 quando Release SYNC_FAILED.

Metas mensuráveis:

1. ArgoCD repo-server tem timeout ajustado para suportar cold-cache cleanroom (≥60s)
2. Heal H18 estendido para incluir `argocd-repo-gitea-internal` (idempotente)
3. CLI exit code 3 quando Release status=FAILED dentro do timeout do `--wait` (não exit 0 com falso positivo)
4. Cleanroom + dogfood final gate: zero PLATFORM_DEGRADED em deploys normais

## ADRs

### D1 — Aumentar `controller.repoServerTimeoutSeconds` para 60s

- **Decisão:** Override Helm value `controller.repoServerTimeoutSeconds: "60"` em `infra/helm/argocd/values.yaml`. Mantém published chart `charts/argocd` UNCHANGED se não overlaps.
- **Rationale:** Default 15s é razoável para warm-cache prod com poucos apps. Em cleanroom cold-cache + 10+ concurrent deploys, repo-server precisa fazer git clone fresh por App, demoram ≥30s cada. 60s dá margem confortável.
- **Alternatives rejected:** Reduzir concorrência via API queue local (movido para Decision 3 abaixo); aumentar para 300s (overkill — operations devem completar em segundos, não minutos).
- **Consequências:** **Habilita** cleanroom dogfood resiliente. **Constrange** que se repo-server estiver realmente travado, falha demora 60s (vs 15s) — aceitável trade-off.

### D2 — Estender H18 para cleanup de `argocd-repo-gitea-internal`

- **Decisão:** H18 do `k3d-cross-cluster-heal.sh` adiciona delete de `argocd-repo-gitea-internal` (Opaque stub criado por Helm chart). Idempotente.
- **Rationale:** ArgoCD Helm chart cria automaticamente esse Secret stub mesmo quando `argocd-creds-gitea` (canonical, password-bearing) já existe. ArgoCD repo URL matching encontra ambos e log "Found multiple credentials" — comportamento não-determinístico.
- **Alternatives rejected:** Disable a Helm value que cria o stub (não há flag clean para isso). Modificar Helm template post-render (frágil).
- **Consequências:** **Habilita** cleanroom auth deterministic. **Constrange** que H18 precisa ser invocado pós-Helm sync (já está no fluxo de heal).

### D3 — CLI deve detectar Release FAILED e retornar exit 3

- **Decisão:** Quando `loopzero deploy --wait` (default), CLI faz polling de release status via SSE. Se evento `release.failed` chega OU build status transiciona de RELEASING → FAILED, CLI imprime erro user-friendly e exits com código 3 (deploy failure per Rule 4).
- **Rationale:** Atualmente CLI exits 0 quando build foi submitted, sem aguardar Release ACTIVE. False positive: user acha que app está live mas pod não existe. Exit 3 (deploy failure) sinaliza corretamente.
- **Alternatives rejected:** Retornar exit 5 (partial). Manter exit 0 porque "build foi submetido com sucesso" — viola contrato semântico do CLI.
- **Consequências:** **Habilita** CI/CD pipeline detect failure correctly. **Constrange** que CLI agora aguarda Release status — tempo médio do `loopzero deploy` aumenta ~30s (já era esperado em --wait mode).

### D4 — Defer "API sync queue" + "circuit breaker reset semantics" para plano futuro

- **Decisão:** Não tocar em sync_client.go ou container_delivery.go nesta sessão. F-01 root cause é primariamente timeout config (D1) — eliminar isso provavelmente resolve 90% dos symptoms sem precisar refactorar concurrency.
- **Rationale:** D1 é low-risk Helm config change. API queue + breaker reset semantics é T3-tier change (concurrency, state machine). Pre-release governance exige meeting + ADR formal para isso. Defer para `argocd-sync-concurrency-hardening-plan.md` separado se F-01 persistir após D1+D2+D3.
- **Consequências:** **Habilita** ship rápido. **Constrange** que se cleanroom AINDA falhar pós-D1, próxima iteração precisa novo plan.

## Dependency Graph

```
Phase 0 (Pre-flight)
   └── T0.1: Verify current state — list current ArgoCD timeouts + heal H18 + CLI deploy flow

Phase 1 (Quick wins, paralelizáveis)
   ├── T1.1: D1 — argocd values.yaml repoServerTimeoutSeconds=60
   └── T1.2: D2 — H18 estender argocd-repo-gitea-internal

Phase 2 (CLI hardening, sequential after 1)
   └── T2.1: D3 — CLI exit 3 on Release FAILED detection

Phase 3 (Final validation)
   └── T3.1: Cleanroom + journey suite (warm-infra OK gate ≥80/82)
```

| Phase | Sequencial após | Estimativa |
|-------|-----------------|------------|
| 0 | — | 5min |
| 1 | — | 30min |
| 2 | 1 | 60min |
| 3 | 1+2 | 90min |

**Total runtime estimate:** ~3h.

---

## Phase 0: Pre-flight verification

### T0.1 — Inspect current state

#### Objective
Confirmar AS-IS antes de mudar nada.

#### Tasks
1. `grep -n "repoServer\|timeout" infra/helm/argocd/values.yaml` — verify zero override existe hoje
2. `grep -n "argocd-repo-gitea-internal" infra/scripts/k3d-cross-cluster-heal.sh` — verify zero handling
3. `grep -rn "exit_code.*3\|deploy.*FAILED" cli/src/commands/deploy.ts` — verify CLI path atual

#### Acceptance Criteria
- [ ] AS-IS documentado em commit message
- [ ] Diferenças vs PROPOSED estado visíveis em diff

---

## Phase 1: Quick wins

### T1.1 — D1 — Aumentar repoServerTimeoutSeconds para 60s

#### Objective
Overrride Helm value `controller.repoServerTimeoutSeconds: "60"` no chart bootstrap `infra/helm/argocd`.

#### Files to edit
```
infra/helm/argocd/values.yaml — adicionar override sob controller:
```

#### Deep file dependency analysis
- `infra/helm/argocd/values.yaml`: bootstrap chart values usado por Helmfile L2. Production deploy via Pulumi/ArgoCD L3 NÃO é afetado (chart published em `charts/argocd` é separado).

#### Tasks
1. Editar `infra/helm/argocd/values.yaml` adicionando bloco controller com override:
   ```yaml
   controller:
     args:
       repoServerTimeoutSeconds: "60"
   ```
2. Validar render: `helm template argocd argo/argo-cd --version <pinned> -f infra/helm/argocd/values.yaml | grep -A 2 reposerver-timeout`

#### TDD
```
RED:    grep "repoServerTimeoutSeconds" template render → not present (default 15s implied)
GREEN:  Apply edit; re-render shows --reposerver-timeout-seconds=60 in argocd-application-controller args
REFACTOR: None
VERIFY:  helm template argocd argo/argo-cd -f infra/helm/argocd/values.yaml | grep "reposerver-timeout"
```

#### Acceptance Criteria
- [ ] Helm template renderiza `--reposerver-timeout-seconds=60` no application-controller container args
- [ ] Production chart `charts/argocd` UNCHANGED (não modifica)

#### DoD
- [ ] After Helm sync (cleanroom Phase 3), argocd-application-controller pod has `--reposerver-timeout-seconds=60` arg
- [ ] argocd-server logs no longer show "application refresh deadline exceeded" sob carga normal

---

### T1.2 — D2 — H18 estender para argocd-repo-gitea-internal

#### Objective
Heal script `k3d-cross-cluster-heal.sh` H18 deve idempotently deletar `argocd-repo-gitea-internal` (além de `gitea-credentials-template`) + restart argocd-repo-server.

#### Files to edit
```
infra/scripts/k3d-cross-cluster-heal.sh — extend H18 block
```

#### Tasks
1. Edit H18 block para incluir delete idempotente de `argocd-repo-gitea-internal`
2. Test idempotência: run heal 2x, second invocation no-op

#### TDD
```
RED:    Re-create argocd-repo-gitea-internal manually → run heal → verify DELETED
GREEN:  Heal removes it idempotently
REFACTOR: None
VERIFY:  bash infra/scripts/k3d-cross-cluster-heal.sh; kubectl -n argocd get secret argocd-repo-gitea-internal → NotFound
```

#### Acceptance Criteria
- [ ] H18 deletes both stale Secrets idempotently
- [ ] Re-running heal pós-deletion is no-op (already absent)

#### DoD
- [ ] Pós-cleanroom, `kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type=repository` shows zero stub Secrets

---

## Phase 2: CLI hardening (D3) — STATUS: NO-OP (ALREADY IMPLEMENTED)

### T2.1 — CLI exit 3 on Release FAILED — RESOLVED via existing code

**Verification (2026-05-08):** Reading the code path confirmed:
1. `api/internal/services/container_delivery.go:131` — when `argocd trigger sync failed after retries`, calls `phaseEFinalize(verifyFailed=true)`.
2. `phaseEFinalize` at line 425 — `UpdateStatus(release, SYNC_FAILED)` + line 431 `eventStore.Append(build.ID, "deploy.sync_failed", failedData)`.
3. `cli/src/commands/deploy.ts:554-572` — handler for `event.type === 'deploy.sync_failed'` → emits product-language error, finishes with exit code 3.

CLI ALREADY exits 3 on sync_failed and API ALREADY emits the event. The dogfood F-01 root cause is purely the ArgoCD timeout (T1.1) + auth conflict (T1.2). No code change needed in CLI deploy or API release path.

**Marked NO-OP per ADR-0022 trivial amendment.**

#### Objective (HISTORICAL — what original T2.1 wanted to do)
CLI `loopzero deploy` (modo `--wait`, default) deve exit code 3 quando Release transiciona para FAILED, em vez do atual exit 0 (false positive).

#### Files to edit
```
cli/src/commands/deploy.ts — handle Release FAILED event from SSE stream
```

#### Deep file dependency analysis
- CLI deploy faz POST /api/v1/builds, recebe build_id, abre SSE stream em /api/v1/builds/{id}/stream.
- SSE events: build.queued, build.started, build.completed, deploy.verifying, deploy.completed, **release.failed**.
- Atualmente CLI provavelmente trata `deploy.completed` como sucesso e ignora `release.failed`.

#### Tasks
1. Read `cli/src/commands/deploy.ts` para entender SSE handling atual
2. Adicionar handler para `release.failed` event que:
   - Imprime user-friendly error message (e.g., "Deploy failed: deploy service unavailable")
   - Exits com código 3 (deploy failure per Rule 4)
3. Garantir mode `--no-wait` continua exit 0 (apenas submetido)

#### TDD
```
RED:    Mock release.failed SSE event → CLI must currently exit 0 (the bug)
GREEN:  Apply handler; CLI exits 3 with clear message
REFACTOR: Extract event-to-exit-code mapping if growing
VERIFY:  cd cli && npm test -- --testPathPattern=deploy
```

#### Acceptance Criteria
- [ ] `loopzero deploy --wait` (default) exits 3 quando release.failed
- [ ] `loopzero deploy --no-wait` continues exit 0 (apenas submission)
- [ ] CLI message NÃO contém termos internos (ArgoCD, sync, etc.)

#### DoD
- [ ] Unit test for SSE release.failed handling passes
- [ ] Existing CLI tests still pass

---

## Phase 3: Final validation gate

### T3.1 — Cleanroom + dogfood gate

#### Objective
Validar end-to-end que F-01 está corrigido após D1+D2+D3.

#### Tasks
1. `task clean:all` (destroy)
2. `task setup` (recreate)
3. `tilt up` (deploy)
4. `task k3d:heal` (apply H18 incluindo new D2 path)
5. `task doctor` (gate 38/0)
6. Run `ginkgo --label-filter=journey` LIVE com fresh JUnit
7. Compare: ≥80/82 PASS (vs 25/35 baseline pre-fix)

#### Acceptance Criteria
- [ ] `task doctor` 38/0 PASS pós-setup
- [ ] Journey suite ≥ 80 PASS / ≤ 2 FAIL (orthogonal flakes ok)
- [ ] Zero `[PLATFORM_DEGRADED]` log lines durante normal deploys
- [ ] DB builds recentes: ≥80% SUCCEEDED (não FAILED com SYNC_FAILED)
- [ ] argocd-server log zero `application refresh deadline exceeded`

#### DoD
- [ ] Final report posted with JUnit XML reference
- [ ] CHANGELOG entry in Unreleased / Fixed section

---

## Coverage Matrix

| # | Achado | Severidade | Resolution Tasks | Notas |
|---|--------|-----------|------------------|-------|
| F-01 | ArgoCD sync timeout em cleanroom | S0 | T1.1 (D1) + T2.1 (D3) | T1.1 fixa root cause; T2.1 fixa false positive observability |
| F-02 | argocd-repo-gitea-internal stub recriado | S2 | T1.2 (D2) | H18 extension |

**Coverage: 2/2 findings covered (100%).**

## Global Definition of Done

- [ ] All phases (0-3) completed
- [ ] All Go/TS tests passing
- [ ] Zero lint warnings
- [ ] CHANGELOG.md updated
- [ ] **Cleanroom + journey suite ≥ 80/82 PASS** (T3.1)
- [ ] Plan-specific criteria met
- [ ] **`/dogfood full` final gate** (operator-driven post-implementation)

---

## Dogfood Gate (MANDATORY)

After ALL phases complete:

```bash
# 1. Fresh infra
task clean:all && task setup && tilt up && task k3d:heal && task doctor

# 2. Run ginkgo journey LIVE
LOOPZERO_E2E=1 LOOPZERO_TOKEN=... ginkgo -v --label-filter=journey --timeout=60m ./infra/tests/e2e/
```

### Pass Criteria

- [ ] `task doctor` 38/0 PASS
- [ ] Journey suite ≥ 80 PASS, ≤ 2 FAIL (orthogonal cleanroom flakes acceptable)
- [ ] Zero `[PLATFORM_DEGRADED]` log lines
- [ ] DB builds: ≥ 80% SUCCEEDED
- [ ] No regression vs warm-infra 83/0 baseline (Ralph loop session 2026-05-08)

### On Failure

If F-01 symptoms persist after D1+D2+D3:
- File follow-up plan: `argocd-sync-concurrency-hardening-plan.md` (D4 deferred work)
- Cover: API sync queue + circuit breaker reset semantics + retry backoff tuning

---

## Notes on Architecture Docs + Edge-Case Plan

Skill spec compliance:

- **Pre-Plan AS-IS** (`/architecture-docs` Mode 2): NOT INVOKED in this iteration — affected domains (`gitops`, `observability`) already have docs from dogfood-2026-05-08-fixes plan; minimal additional architectural change here.
- **Edge-Case review** (`/edge-case-plan`): Will be invoked after this plan saved, mandatory per skill spec.
- **Architecture Docs PROPOSED**: skipped — D1-D3 are config + CLI changes; no domain boundary shift.
