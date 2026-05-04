# ADR-0030 — Decouple LoopZeroError from HTTP transport

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

`pkg/errors/errors.go:14-27` define `LoopZeroError` com campos `Code`, `Message`, `HTTPStatus`, `Cause`. Mistura:

- Modelo de domínio (Code, Message, Cause).
- Camada de transporte (HTTPStatus).

Consequências do acoplamento:

- Operators e CLI importam `pkg/errors` mas não precisam de HTTP semantics.
- Audit log e logging estruturado armazenam HTTPStatus inutilmente.
- Quando LoopZero eventualmente expor gRPC ou tRPC, HTTPStatus se torna tech debt.
- Viola gate PKG-G08 ("Error Typing Without Overfitting") — sentinels e factories são forçados a carregar HTTP semantics.

Google AIP precedent: `google.rpc.Status` separa `code int32`, `message string`, `details []google.protobuf.Any`. HTTP mapping é responsabilidade do `grpc-gateway`, não do erro.

RFC 9457 (Problem Details for HTTP APIs, supersede RFC 7807) é o wire format formal para HTTP errors. Belongs em uma camada de transporte, não no error model.

## Decision

Refactor em duas camadas:

1. **`pkg/errors/errors.go` (modelo):** `LoopZeroError` mantém `Code`, `Message`, `Cause`, opcional `Stack []uintptr` (per ADR-0028 future-compatible). Field `HTTPStatus` REMOVIDO.

2. **`pkg/errors/http/` (transporte) — NEW package:**
   - `pkg/errors/http/mapping.go`: `Status(err error) int` faz `errors.As` para extrair Code, lookup table, default 500.
   - `pkg/errors/http/writer.go`: `WriteJSON(w http.ResponseWriter, r *http.Request, err error)` emite RFC 9457 problem+json com fields `type`, `title`, `status`, `detail`, `code`, `instance`.

3. **Sentinels updated:** `var ErrLockTimeout = &LoopZeroError{Code: "LOCK_TIMEOUT", Message: "..."}` (sem HTTPStatus).

4. **Lookup table:** `var statusByCode = map[string]int{ "LOCK_TIMEOUT": 409, "TENANT_NOT_FOUND": 404, ... }` em `pkg/errors/http/mapping.go`.

**BREAKING CHANGE:** callers que faziam `loopzeroErr.HTTPStatus` direto migram para `errorshttp.Status(loopzeroErr)`. Pre-release governance permite (CHANGELOG entry [Changed]/[Removed] mandatória, condition CONDITION-2 staged rollout per MEET).

## Alternatives Considered

1. **Manter HTTPStatus mas marcar deprecated.** REJECTED — deprecation sem migration window real apenas adia o break; pre-release is exactly when to break.
2. **Adicionar gRPC status field paralelo.** REJECTED — YAGNI até LoopZero precisar de gRPC.
3. **Usar `cockroachdb/errors`.** REJECTED por agora — cockroachdb pattern é PL-1 (post-launch); este ADR foca decoupling transport.

## Consequences

### Enables

- Gate PKG-G15 (No transport coupling in domain errors) GREEN.
- RFC 9457 problem+json shape gerado automaticamente via lookup.
- Operators e CLI consumem `pkg/errors` sem importar HTTP semantics.
- Tech debt limpa para futura adoção de gRPC ou outra wire format.

### Constrains

- BREAKING change em pkg/errors API — todos callers que acessavam `.HTTPStatus` direto precisam migrar.
- Migration path explicit per CONDITION-2 (Stages A/B/C com revert):
  - **Stage A:** introduzir `pkg/errors/http/` + helper. Sem remover field.
  - **Stage B:** migrate consumers (api/internal middleware + routes) para usar helper.
  - **Stage C:** remove field + update sentinels.
- CHANGELOG entry mandatória.

## Implementation Reference

- Plan task: T2.3 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files affected: `pkg/errors/errors.go`, `pkg/errors/http/*` (NEW), `api/internal/middleware/error_handler.go`, todos handlers que acessavam HTTPStatus.

## References

- [RFC 9457 — Problem Details for HTTP APIs](https://datatracker.ietf.org/doc/html/rfc9457)
- [Google AIP — Errors](https://google.aip.dev/193)
- `docs/technical/pkg/improvement-roadmap.md` § S9-3 + Anti-Patterns #2
