# Module 04 — Shared Libraries (`pkg/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 92% (Claude-only, single-model residual)
**Maturity:** **3.8 / 4.0**

## TL;DR

Foundation layer de **maior qualidade** observada até aqui: 388 test functions, property-based tests, zero TODOs/FIXMEs, apenas 1 dependência externa de produção (`golang-jwt/v5`), e refatoração ARCH-001 já consolidada. Hardening JWT RFC 8725bis com whitelist de algoritmos + circuit breaker é state-of-the-art. Bloqueia 4.0 por 3 símbolos deprecated ainda presentes em `logging/` e pacotes magrinhos sem cobertura adversarial (id sem teste de colisão, healthcheck minimal).

---

## 1. Inventário Real

| Pacote | LOC | Files | Test Files | Test Funcs |
|---|---|---|---|---|
| statemachine | 2,989 | 27 | 14 | 117 |
| enforcement | 1,750 | 7 | 3 | 53 |
| jwt | 1,614 | 2 | 1 | 45 |
| naming | 1,151 | 3 | 2 | 73 |
| errors (+ errors/http) | 911 | 2 | 1 | 36 |
| conditions | 838 | 5 | 3 | 26 |
| circuitbreaker | 528 | 2 | 1 | 16 |
| logging | 500 | 4 | 2 | 14 |
| id | 234 | 2 | 1 | 6 |
| healthcheck | 107 | 2 | 1 | 2 |
| **TOTAL** | **10,722** | **57** | **31** | **388** |

**Dependência externa de produção:** apenas `github.com/golang-jwt/jwt/v5 v5.3.1` (necessária para RFC 8725bis hardening). Test-only: `DATA-DOG/go-sqlmock v1.5.2`.

---

## 2. Arquitetura Observada

**11 state machines verificadas (CLAUDE.md alinha):**

| Machine | File | Estados | Terminal | Timeouts |
|---|---|---|---|---|
| Build | build.go | QUEUED→RUNNING→RELEASING→SUCCEEDED + FAILED/CANCELLING/CANCELLED | 3 | 4 (5/30/10min + 60s) |
| Release | release.go | READY→PENDING→ACTIVE/SUPERSEDED/ROLLED_BACK/CANCELLED | 4 | 2 |
| Promotion | promotion.go | VALIDATING→COMMITTING→SYNCING→COMPLETED + 3 failure | 4 | — |
| Tenant | tenant.go | Provisioning→Active→Suspended→Deleting | varia | — |
| Environment, Domain, Application, ExposedRoute, Preview, Rollback, PlatformBootstrap | (existem) | varia | varia | — |

**Construtor genérico:** `NewMachine(cfg MachineConfig)` valida na criação — panic se terminal tem outgoing transition (build.go:70-74), panic se duplicate (From, Event) pair (linhas 79-84). `TryTransition()` retorna erro em vez de panic (para state vindo de CR status).

**LoopZeroError** (`errors/errors.go:29-48`):
- `{Code, Message, Cause, stack}` — 33 factory functions + 33 sentinels (`ErrLockTimeout`, `ErrInvalidTransition`, etc.)
- `Wrap(ctx)` preserve Code, `WrapWithStack(ctx)` captura uma única vez
- `errors.Is()` compara Code (não identidade ponteiro)
- HTTP status separado em `errors/http/mapping.go:35-86` (26 códigos registrados, default 500)

**JWT hardening** (`jwt/validator.go:34-41`):
```go
var AllowedAccessAlgs = []string{"RS256"}     // RSA-only per ADR-0029
var AllowedSessionAlgs = []string{"HS256"}    // HMAC for session
var allowedTypValues = map[string]struct{}{"": {}, "JWT": {}, "at+jwt": {}} // RFC 8725bis §2.6
```

Defesa em profundidade na `Validate()` (linha 163-192):
1. `goJWT.WithValidMethods` (whitelist no parser)
2. `validateTypHeader` (cross-JWT confusion block)
3. Type assertion `*goJWT.SigningMethodRSA` (belt-and-suspenders)
4. JWKS lookup por kid
5. Issuer + audience + exp checks

Circuit breaker integration (`WithCircuitBreaker`, linha 131-145): quando aberto, serve JWKS stale; quando fecha, refetch normal — graceful degradation.

**Session token rotation** (`SessionTokenIssuer`, linhas 222-289): `WithPreviousSecret()` para zero-downtime rotation window.

**Naming functions críticas:**
- `Sanitize(s)` — lowercase, alphanumeric+hyphen, trim, max 63 chars (RFC 1123)
- `PreviewNamespace(tenant, project, prNumber)` — validação prNumber>0
- `Hostname(project, env, baseDomain)` — Sanitize antes de compor
- `ImageTag` / `ImageDigestRef` — **ARCH-001 fix**: centralizado (antes duplicado em 4 sites)

**Conditions** (`conditions/conditions.go:88-126`):
- `SetCondition()` preserva LastTransitionTime quando Status unchanged
- ObservedGeneration tracking para kstatus compat
- `AggregateToPhase()` → Pending/Provisioning/Ready/Stalled

**Enforcement plan caps:**
- Enterprise: sempre Allowed (hardLimit < 0)
- Starter/Pro/Team: hardLimit check; Team com soft overage + SoftOver=true
- Missing row: fail-closed (hardLimit=0 → deny)

---

## 3. Contratos Públicos

**Cross-package coupling interno:**
- `pkg/jwt` → `pkg/circuitbreaker` (JWKS fetch graceful degradation)
- `pkg/errors/http` → `pkg/errors` (transport-agnostic separation, ADR-0030)
- Zero cycles. errors não importa errors/http.

**Consumidores externos:**
- `api/` importa: errors, naming, id, statemachine, conditions, enforcement, jwt, logging, circuitbreaker
- `operators/` importa: naming, conditions, id, healthcheck
- `cli/` importa: errors, naming, id, logging

**Deprecated symbols (3):**
- `naming.Labels` (line 291) — prefer `StandardLabels{...}.AsMap()`
- `logging.RegisterTraceExtractor` — prefer explicit ContextLogger injection
- `logging.FromContext` — prefer `(*ContextLogger).FromContext`

---

## 4. Cobertura de Testes

- **388 test functions** em **31 _test.go files** (todos os 10 pacotes têm tests)
- **Property-based tests** com `quick.Check`: ~6 instâncias em naming (idempotência Sanitize) e statemachine (transition sequencing)
- **Caminhos críticos cobertos:**
  - statemachine: invalid transitions panic + TryTransition retorna error
  - jwt: rejeição de "none" alg, HS256 com chave pública (confusion), typ header inválido
  - naming: `Sanitize(Sanitize(s)) == Sanitize(s)` (idempotência property)
  - session token: validação com secret errado, rotation com previous secret

**Possíveis gaps:**
- healthcheck: apenas 2 test funcs (timeout/rede sem cobertura)
- id: 6 test funcs (sem teste de colisão/entropy stress)
- logging: 14 test funcs (sem teste explícito de `LOG_LEVEL` env parsing)

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação |
|---|---|---|
| TODO/FIXME count | **0** ✅ | — |
| Deprecated funcs ainda usadas | BAIXA | `naming.Labels`, `logging.RegisterTraceExtractor`, `logging.FromContext` |
| `id/` sem teste de colisão/entropy | BAIXA | `pkg/id/id_test.go` (6 testes apenas) |
| `healthcheck/` minimal | BAIXA | 2 testes |
| `logging/` sem teste de env parsing | BAIXA | 14 testes |
| Code duplication | **0** ✅ (ARCH-001 já fixado) | — |

**External dependency count:** 1 produção (golang-jwt). Excelente minimalismo para foundation layer.

---

## 6. Avaliação de Maturidade

**Score: 3.8 / 4.0** (confidence 92%)

### Evidências

1. **T1 (commit+test)** — `pkg/jwt/validator.go:34-41` (RSA whitelist + RFC 8725bis); `pkg/statemachine/machine.go` (construtor com panic on invalid + 117 test funcs); `pkg/naming/naming.go:309-333` (ImageTag/ImageDigestRef centralizado, ARCH-001).
2. **T2 (LIVE log)** — pkg/ é foundation usada por api+operators+cli, exercitada em 12/12 cleanroom GREEN (2026-05-05), journey 76/76 GREEN (2026-05-06), canary 80/80 GREEN (2026-05-07), cell-2 multi-cell (2026-05-02).
3. **T3 (CI run)** — `pkg-soak` workflow em `.github/workflows/`, go test ./... a cada PR.

### Raciocínio

**Sustenta 3.5+:**
- 388 test functions cobrem 100% dos 10 pacotes
- Property-based tests (`quick.Check`) em naming + statemachine
- Zero TODO/FIXME (limpeza exemplar)
- JWT RFC 8725bis hardening (alg whitelist + typ header + circuit breaker)
- ARCH-001 already fixed (ImageTag centralizado)
- Cross-package coupling mínimo, zero cycles
- 1 única dependência externa de produção

**Bloqueia 4.0:**
- 3 deprecated symbols ainda no código (`logging.*`, `naming.Labels`)
- 3 pacotes com cobertura adversarial fraca (id sem colisão, healthcheck minimal, logging sem env parsing test)
- Sem fuzz tests explícitos (apenas property-based)
- HTTP status mapping registry: códigos não-registrados defaultam para 500 (fail safe ok, mas sem teste CI que falha se alguém adiciona código não-mapeado)

### Caminho para 4.0

1. Remover/migrar 3 deprecated symbols com plano de cutover
2. Adicionar fuzz test em `pkg/jwt` (input fuzzing de token strings)
3. Adicionar teste CI que falha se `NewLoopZeroError(code, ...)` é chamado com código não em `statusByCode`
4. Property-based test para `pkg/id` (1M IDs sem colisão)
5. Coverage para `healthcheck` timeout paths (mock servidor lento)

---

**Próximas auditorias relacionadas:** State Machines (M11, deep-dive das 11 máquinas), Auth/Billing (M10, consumidor de jwt).
