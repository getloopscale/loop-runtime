# Module 11 — State Machines (`pkg/statemachine/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 92%
**Maturity:** **3.8 / 4.0**

## TL;DR

Módulo de disciplina arquitetural mais limpa auditado: 11 máquinas + 117 tests (4 property-based com `quick.Check` MaxCount=1000), zero TODOs/FIXMEs, panic invariants codificados no construtor, helpers de terminal documentando regras de negócio sutis (Release pode reactivar), e Visualize() determinístico para PR drift detection. Bloqueia 4.0 por State/Event serem string aliases (sem compile-time safety), dual terminal semantics em Release, e inconsistência em helpers exportados (3 de 11).

---

## 1. Inventário Real

| Machine | LOC | Transitions | Terminal States | Timeouts |
|---|---|---|---|---|
| Build | 101 | 14 | 3 (SUCCEEDED, FAILED, CANCELLED) | 4 (5/30/10min + 60s) |
| Release | 143 | 33 | 1 (CANCELLED real) | 2 (PENDING>10min, READY>5min) |
| Tenant | 70 | 14 | 1 (Deleting) | — |
| Preview | 67 | 10 | 1 (Deleted) | TTL externo |
| Promotion | 45 | 6 | 4 (COMPLETED + 3 failure) | controller-side |
| Rollback | 48 | 7 | 5 (COMPLETED + 4 failure/blocked) | controller-side |
| Domain | 109 | 22 | 1 (removing) | dual v1/v2 flows |
| Environment | 49 | 7 | 1 (DELETING) | — |
| Application | 105 | 19 | 1 (Cleaned) | cyclic Reachable↔Degraded |
| ExposedRoute | 63 | 7 | **0** (GC via ownerRef) | — |
| PlatformBootstrap | 56 | 8 | 2 (BootstrapComplete, Failed) | — |
| **Machine (core)** | **177** | — | — | — |
| **TOTAL** | **988** | **147** | — | — |

**Tests:** 117 test funcs em 14 _test.go files. 4 property-based em `properties_test.go`. **11/11 machines** validadas via `properties_test.go:allMachines()`.

---

## 2. Arquitetura Observada

**Construtor genérico** (`machine.go:57-89`):
- **Panic on duplicate transition** (linha 79-83) — programming error
- **Panic if terminal state has outgoing transition** (linha 70-74) — programming error
- "Invalid transitions are programming errors and cause a panic" (machine.go:9)

**API pública:**
- `Transition(state, event) State` — panics on invalid (use when state is known-safe)
- `TryTransition(state, event) (State, error)` — never panics, returns error (use when state from external source)
- `IsTerminal()`, `ValidEvents()`, `Timeouts()`, `Name()`, `Visualize()`

**Per-machine highlights:**

- **Build:** QUEUED → RUNNING → **RELEASING** (non-terminal!) → SUCCEEDED. `IsBuildTerminal()` exclui RELEASING (MEET-2026-03-26-008). Permite reconcile recovery em crash.
- **Release:** Apenas CANCELLED é **truly terminal** (Machine.Terminal); SUPERSEDED + ROLLED_BACK podem reactivar via `ReleaseReactivated`. `IsReleaseTerminal()` helper documenta isso (`release.go:59-61`). Timeout hierarchy MEET-2026-04-04-029 P2: PENDING>10min > convergence SyncTimeout 300s > reaper 600s.
- **Tenant:** Soft delete grace period MEET-052: RequestDeletion → PendingDeletion (grace), depois GraceExpired → Deleting ou Restore → Active.
- **Preview:** TTL handled em ExpiresAt field, **não em machine**. CleanupFailed → GarbageCollected.
- **Promotion/Rollback:** Sem machine timeouts (controller-side, 2min sync). Rollback adiciona BLOCKED state (no-previous-release).
- **Domain:** Dual flow v1 (TXT verification) + v2 (CF Zone). 22 transitions cobrindo provisioning, degraded (CNAME lost, 14d grace), retry.
- **Application:** **CYCLIC** — Reachable ↔ Degraded (probe flapping). Cleaned terminal (added 2026-05-03 reaper). ApplicationOrphaned aceito de QUALQUER non-terminal (orphan pode acontecer em qualquer ponto).
- **ExposedRoute:** **Zero terminal states**. Lifecycle tied to parent LoopZeroApplication ownerRef (Kubernetes GC).
- **PlatformBootstrap:** Sequential: Empty → PENDING → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete | Failed.

**Cascade exemplo (Release):** Project deletion → ALL non-terminal states transitan para CANCELLED (lines 123-132 release.go). Previne zombie ACTIVE/PENDING rows após project delete.

---

## 3. Contratos Públicos

**11 constructors exportados:** `NewBuildMachine`, `NewReleaseMachine`, `NewTenantMachine`, `NewDomainMachine`, `NewEnvironmentMachine`, `NewPlatformBootstrapMachine`, `NewPromotionMachine`, `NewRollbackMachine`, `NewApplicationMachine`, `NewExposedRouteMachine`, `NewPreviewMachine`.

**Tipos:** `State string`, `Event string` (string aliases — type opacity por convenção, sem compile-time enforcement).

**Helpers domain-specific (3 de 11):**
- `IsBuildTerminal()` — exclui RELEASING
- `IsReleaseTerminal()` — apenas CANCELLED (vs Machine.Terminal slice)
- `IsDomainTerminal()`, `IsReleaseReapable()` — domain-specific filtering

**Visualize():** Determinístico → Graphviz DOT → SVG. Usado em CI para detectar transition drift em PRs (`pkg/statemachine/visualize.go`).

---

## 4. Cobertura de Testes

**Per-machine unit tests** (113 total):
- build_test.go (18), release_test.go (18), domain_test.go (11), application_test.go (10), machine_test.go (10), rollback_test.go (8), preview_test.go (7), promotion_test.go (7), exposedroute_test.go (6), tenant_test.go (5), environment_test.go (5), platformbootstrap_test.go (3)

**4 property-based tests** (`properties_test.go`, quick.Check MaxCount=1000):
- `TestProperty_TerminalStatesRejectAllEvents` — no event aceita de terminal
- `TestProperty_TransitionsAreDeterministic` — (state, event) consistente 100 iters
- `TestProperty_RandomEventsFromValidStateNeverPanic` — TryTransition robustness
- `TestProperty_ValidEventsListMatchesTransitionMap` — ValidEvents == transitions keys

**Cross-cuts validados:**
- `api/internal/db/build_store.go:21` — buildMachine instance
- `api/internal/db/release_store.go:21` — releaseMachine instance
- `api/internal/routes/domains.go:50` — domainMachine instance
- `operators/internal/tenant/tenant_controller.go:37` — tenantSM
- `operators/internal/preview/preview_controller.go:34` — previewSM
- `safeTransition` helper (build_store.go:344) usa TryTransition para DB reads

**Critical paths cobertos:**
- ✅ Invalid transitions panic
- ✅ Timeouts configurados corretamente (MEET-2026-04-04-029 reviewed em release_test.go)
- ✅ Terminal states immutable
- ✅ TryTransition nunca panics

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| TODO/FIXME count | **0** ✅ | grep retorna vazio |
| Direct status mutations bypassing | **0** ✅ | M02 + M03 audits confirmam |
| State/Event como string aliases (sem compile-time safety) | BAIXA | machine.go:18-21 — documented |
| Dual terminal semantics em Release (Machine.Terminal vs IsReleaseTerminal helper) | BAIXA (by design) | release.go:55-58 comment |
| ExposedRoute sem Terminal explícito ({} vs ausente) | BAIXA | exposedroute.go:62 — comment only |
| Helpers de terminal inconsistentes (3 de 11 exportam) | BAIXA | by design — só onde business diverge |
| 4 machines sem timeouts (Promotion, Rollback, Preview, ExposedRoute) | BAIXA | by design — controller/reaper externo |

**Análise de "domain status mutation" no domain_provisioner.go:251:** SAFE — local struct sync após `TransitionStatus` DB call succeeded (linha 246). Não é bypass.

**Análise de PreviewStatusCreating em preview_store.go:164:** SAFE — default-setting on CREATE, consistent with Tenant e Environment patterns.

---

## 6. Avaliação de Maturidade

**Score: 3.8 / 4.0** (confidence 92%)

### Evidências

1. **T1 (commit+test)** — `pkg/statemachine/machine.go:57-89` (NewMachine valida duplicate + terminal outgoing); 11 machines em separate files; 117 test funcs + 4 property tests com `quick.Check` MaxCount=1000.
2. **T2 (LIVE log)** — Usado por api+operators em todos os 11 fluxos LIVE: build/release/promote/rollback/tenant/preview/domain/env/app/exposedroute/platformbootstrap; cleanroom 12/12 GREEN (2026-05-05); rollback P50=648ms/P95=705ms LIVE (cell-2, 2026-05-02).
3. **T3 (CI run)** — `pkg-soak.yml` workflow + `go test ./pkg/...` a cada PR; Visualize() determinístico → SVG diff em PR review.

### Raciocínio

**Sustenta 3.5-3.8+:**
- 11/11 machines + 147 transitions implementadas
- 117 test funcs + 4 property-based tests (quick.Check 1000 iters/property)
- Panic invariants codificados no construtor (duplicate transitions, terminal outgoing)
- Helpers de terminal documentam regras de negócio sutis (Release reactivation, Build RELEASING non-terminal)
- TryTransition robustness contract (never panics em external state)
- 0 TODOs/FIXMEs
- 0 direct status mutations bypassing (verificado cross-module M02 + M03)
- Visualize() determinístico → PR drift detection automatizado
- Cascade explícito (Release on project delete → CANCELLED)
- Timeout hierarchy MEET-2026-04-04-029 P2 respeitado
- MEET-052 soft delete grace period codified (Tenant)
- Application cyclic state pattern (Reachable ↔ Degraded probe flapping) explícito

**Bloqueia 4.0:**
- State/Event são string aliases (sem compile-time safety) — `state := statemachine.State(rawString)` é trivialmente cast
- Dual terminal semantics em Release pode confundir maintainers (IsReleaseTerminal ≠ Machine.IsTerminal por design, mas sutil)
- ExposedRoute sem `Terminal: []State{}` explícito (apenas comment) — pode ser missed em diffs
- Inconsistência em helpers exportados (3 de 11) — por design mas adicional documentation inline em todas as 11 seria preferível

### Caminho para 4.0

1. Migrar `State`/`Event` para distinct types com compile-time enforcement (breaking change — Stage A coexistence pattern)
2. Adicionar `Terminal: []State{}` explícito em ExposedRoute (mesmo que vazio)
3. Documentar inline em todas 11 machines: "this machine has/does not have terminal helpers; rationale: ..."
4. Unificar nomenclatura: ou todas as 11 machines têm helper `IsXxxTerminal`, ou nenhuma (consistency over flexibility)
5. Adicionar fuzz test para `TryTransition` com bytes aleatórios como State input

---

**Próximas auditorias relacionadas:** pkg/ overall (M04), Operators (M03 — consumers), API (M02 — consumers).
