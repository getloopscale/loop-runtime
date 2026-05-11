# Module 02 — API (`api/internal/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 90% (Claude-only, single-model residual)
**Maturity:** **3.5 / 4.0**

## TL;DR

API densa e bem-estruturada (113K LOC, 35 subpacotes, 49 rotas HTTP) com disciplina exemplar de locks (zero session-scoped) e state machines (zero mutações diretas). Bloqueia 4.0 por 72 ocorrências de `errors.New()` fora do `pkg/errors` (anti-pattern de consistência) e STUB confirmado em `scale.go:90` (config validada mas não persistida).

---

## 1. Inventário Real

| Métrica | Valor |
|---|---|
| Arquivos `.go` totais | 485 |
| Produção (não-`_test.go`) | 233 |
| Test files (`_test.go`) | 252 |
| LOC total | 113,576 |
| Subpacotes em `api/internal/` | 35 |
| Rotas HTTP registradas | 49 |
| Test functions (`func Test*`) | 2,283 |

**Top subpacotes por LOC:** services (9,070), routes (6,343), db (4,965), argo (3,182), manifests (2,127), middleware (1,801), github (1,499), argocd (1,259).

**Subpacotes que CLAUDE.md não menciona** (encontrados no código real): adapters, admission, archive, cellmgr, cfkv, cfzone, correlation, domain, gitclient, leader, loki, processing, shellgen, tracing, workqueue.

**Top deps:** chi/v5, pgx/v5, go-git/v5, golang-jwt/v5, k8s.io/api, k8s.io/client-go, otelhttp, prometheus/client_golang.

---

## 2. Arquitetura Observada

**Fluxo `POST /api/v1/builds`** (deploy) trace:
1. `routes/builds.go:185-318` — BuildHandler.Create
2. Validação env regex (DA-10), app name/path (SEC-3, CODE-001), linha 187+
3. Cell resolution per-request: `cellResolver.GetTenantCellID()` (linha 272-280, MEET-053 A-06)
4. Admission gate opcional: 429 com Retry-After (linha 269-303)
5. `h.orch.SubmitBuild()` em `services/build_orchestrator_*.go` (linha 305-314)
6. `mapBuildError()` traduz domínio → HTTP (linha 316)

**Lock discipline:** `lock/environment_lock.go:32-96` — `WithEnvironmentLock` usa **apenas** `pg_advisory_xact_lock` (linha 77, `SET LOCAL lock_timeout='5s'`, isolation `READ COMMITTED`). Grep produção: **0 ocorrências** de `pg_advisory_lock` session-scoped. ✅

**Idempotency-Key:** `middleware/idempotency.go:56-135` — SHA-256 do body para hash matching; 400 MISSING_IDEMPOTENCY_KEY, 422 CONFLICT_REQUEST_MISMATCH, replay cached. Scoped por tenant (linha 88-92).

**SSE flow:** `sse/stream_handler.go:23-80` — EventStore polling, heartbeat configurável, terminal events (`build.completed`, `deploy.completed`, `deploy.sync_failed`, `rollout.aborted`), safety timeout 5min após terminal, cap 200 conexões concurrentes.

**Multi-cell:** `placement/dynamic_router.go` + `placement/engine.go:77-150` (RoundRobinEngine, bin-packing) + `cellmgr/client_manager.go` (lazy K8s clients per cell). Single-cell mode removido (CHG-2026-04-30, engine.go:15-19).

**Correlation-ID:** propagado em `middleware.CorrelationID` (router.go:46), persistido em builds/releases/environment_targets, trailer em git commits via `gitea_pusher.go`.

---

## 3. Contratos Públicos

**49 rotas HTTP** (router.go:79-358). Sample:

| Método | Path | Handler |
|---|---|---|
| POST | `/api/v1/builds` | BuildHandler.Create (builds.go:185) |
| GET | `/api/v1/builds/{id}/stream` | streamHandler.ServeHTTP (router.go:240) |
| POST | `/api/v1/projects/{name}/scale` | ScaleHandler.Scale (scale.go) |
| POST | `/api/v1/projects/{name}/rollback` | RollbackHandler.Create |
| POST | `/api/v1/projects/{name}/promote` | PromoteHandler.Create |
| POST | `/api/v1/admin/tenants/pin` | AdminPin |
| POST | `/internal/argocd-callback` | cfg.ArgoCallbackHandler |

**LoopZeroError typed codes (12 named):** ReplicasExceedPlan, LockTimeout, InvalidTransition, TenantNotFound, BuildNotFound, ReleaseNotFound, ProjectNotFound, EnvironmentNotFound, DomainNotFound, RepositoryNotLinked, IdempotencyConflict, NewLoopZeroError (genérica). HTTP status mapping em `pkg/errors/http/mapping.go:35-86` (26 códigos registrados).

**Response shapes** (POST /api/v1/builds):
```json
{"build_id": "uuid", "stream_url": "/api/v1/builds/{id}/stream"}
```

---

## 4. Cobertura de Testes

- **252 `_test.go` files** com **2,283 `func Test*`**
- Distribuição: 73 em `api/tests/`, 179 inline em `internal/*/`
- **Testify uso mínimo** (1 referência), preferência por plain Go testing
- **Sem mockery/gomock** — fakes hand-written
- Unit tests + integration tests (api/tests/integration/ com PG + K8s reais)
- Lock/contention tests + SSE terminal-detect tests + build_orchestrator_*.go com múltiplos test files

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação |
|---|---|---|
| `errors.New()` fora de `pkg/errors` | **72 ocorrências** (anti-pattern Rule 8) | múltiplos: argocd, db/*, placement, admission, etc. |
| STUB confirmado em scale handler | MÉDIA | `routes/scale.go:90` (config validada, **NÃO persistida**, retorna 202) |
| TODO: switching webhook to ArgoCD labels | BAIXA | `services/sync_callback_activator.go` |
| TODO: OTel trace propagation | BAIXA | idem |
| TODO: rule constructor will eventually fail | BAIXA | `placement/router.go:38` |
| `cellmgr/` sem test files dedicados | BAIXA | testes co-locados ou ausentes |
| Direct status mutations | **0** ✅ | grep `.Status.State =` retorna vazio |
| Session-scoped locks | **0** ✅ | grep `pg_advisory_lock\b` retorna vazio |

---

## 6. Avaliação de Maturidade

**Score: 3.5 / 4.0** (confidence 90%)

### Evidências

1. **T1 (commit+test)** — `lock/environment_lock.go:77` (`pg_advisory_xact_lock` + 5s timeout); `middleware/idempotency.go:56-135` (idempotency + SHA-256); 2,283 test functions em 252 `_test.go`.
2. **T2 (LIVE log)** — Cleanroom 12/12 GREEN (2026-05-05), scaling B40 fix LIVE (2026-05-06), multi-cell 3/3 cells GREEN (2026-05-02), rollback P50=648ms / P95=705ms.
3. **T3 (CI run)** — `ci`, `chaos-suite`, `multi-cell` workflows em `.github/workflows/`, go test ./... + integration suite.

### Raciocínio

**Sustenta 3.5+:**
- Lock discipline impecável (0 session-scoped, 100% xact)
- State machines respeitadas (0 mutações diretas)
- 49 rotas com Idempotency-Key middleware + Correlation-ID propagation
- SSE com heartbeat + safety timeout + connection cap
- Multi-cell engine + dynamic router LIVE
- Circuit breaker em PG @ 90% saturation
- Test:LOC saudável (2,283 funcs)

**Bloqueia 4.0:**
- 72 `errors.New()` anti-pattern (consistency, baixo impacto operacional mas drift de contrato)
- scale.go STUB (config validada mas não persiste — confirmado pelo agente)
- 3 TODOs roadmap não-fechados
- `cellmgr/` sem test files dedicados
- SSE safety timeout hardcoded (5min, não configurável)
- Build request timeout 150s tight para uploads grandes

### Caminho para 3.8-4.0

1. Remediar 72 `errors.New()` → typed LoopZeroError (~2-3 sprints, baixo risco)
2. Fechar STUB em `scale.go:90` — persist scaling config em DB
3. Adicionar tests dedicados para `cellmgr/`
4. Configurabilidade do SSE safety timeout via Helm values
5. Pre-flight size check para uploads grandes antes do build handler timeout

---

**Próximas auditorias relacionadas:** Operators (M03), Multi-Cell (M09), State Machines (M11).
