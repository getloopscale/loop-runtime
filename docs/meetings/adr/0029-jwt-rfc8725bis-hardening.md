# ADR-0029 — JWT RFC 8725bis hardening em pkg/jwt

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

`pkg/jwt/validator.go:78-99` (Validator.Validate) e `validator.go:198-218` (validateWithSecret) fazem validação ad-hoc do método de assinatura via type-assertion `*goJWT.SigningMethodRSA` / `*goJWT.SigningMethodHMAC`. Funciona, mas:

1. Não usa `goJWT.WithValidMethods([]string{...})` whitelist explícito — se outro path emergir (subclasse de SigningMethodRSA), validação pode passar com método inesperado.
2. Não valida o `typ` header — RFC 8725bis (`draft-ietf-oauth-rfc8725bis-04`, May 2026) §2.6 OBRIGA explicit JWT typing para mitigar cross-JWT confusion attack.
3. Não usa case-sensitive verification de `alg` — RFC 8725bis §2.1 (defense em depth contra `aLg=hS256` injection).
4. JWKS fetch (linha 249-287) não passa por circuit breaker — Hydra outage cascateia em todas chamadas auth para 5s timeout antes de falhar.

Algorithm confusion attacks são exploitable em deployments com `alg=HS256` injected; type-assertion atual bloqueia, mas é defense single-layer. RFC 8725bis estabelece defense-in-depth.

## Decision

Implementar 4 mudanças coordenadas em `pkg/jwt/validator.go`:

1. **Algorithm whitelist explícito:** `goJWT.WithValidMethods(AllowedAccessAlgs)` em `Validate()`; `WithValidMethods(AllowedSessionAlgs)` em `validateWithSecret()`. Constants:
   ```go
   var AllowedAccessAlgs = []string{"RS256"}
   var AllowedSessionAlgs = []string{"HS256"}
   ```
2. **`typ` header validation:** após parse, `if typ, ok := token.Header["typ"].(string); ok && typ != "" && typ != "JWT" && typ != "at+jwt" { return error }`.
3. **JWKS fetch via `pkg/circuitbreaker`:** wrap `fetchJWKS` em circuit breaker; durante CB open serve cached keys (max 1h grace); after grace + cache empty → `ErrJWKSUnavailable`.
4. **Case-sensitive alg:** golang-jwt v5 já trata case-sensitive; whitelist explícito reforça.

## Alternatives Considered

1. **Defer to Sprint 9.** REJECTED — gap de segurança hoje em produção. Algorithm confusion attack é exploitable se single-layer defense falhar.
2. **Migrar para `lestrrat-go/jwx/v2`.** REJECTED — adicionaria deps; `golang-jwt/jwt/v5` já suporta as practices via options.
3. **Skip JWKS via circuit breaker (defer to ADR-0031).** REJECTED — ADR-0031 introduz CB v2 generic; este ADR usa CB v1 (T1.4) e migra para v2 em T2.1. Layered approach evita CB outage durante deploy de RFC 8725bis hardening.

## Consequences

### Enables

- Gate PKG-G11 reforçado.
- Gate PKG-G16 GREEN (eat-your-own-dogfood: JWKS via pkg/circuitbreaker).
- Conformance documentável com RFC 8725bis para audits de segurança.
- Tolerância a Hydra outage (cached keys served durante grace window).

### Constrains

- Tokens com `alg` fora do whitelist passam a ser rejeitados — aceitável (LoopZero emite RS256/HS256 only).
- Tokens com `typ` fora `{"", "JWT", "at+jwt"}` rejeitados — aceitável (Hydra emite `JWT`/`at+jwt`).
- Test fixtures com `alg=HS256` em access token tests precisam atualização.

## Implementation Reference

- Plan tasks: T1.2 (whitelist + typ), T1.4 (JWKS via CB).
- Files affected: `pkg/jwt/validator.go`, `pkg/jwt/validator_test.go`, `api/cmd/main.go` (CB wireup).

## References

- [RFC 8725 — JWT Best Current Practices](https://datatracker.ietf.org/doc/html/rfc8725)
- [draft-ietf-oauth-rfc8725bis-04](https://datatracker.ietf.org/doc/draft-ietf-oauth-rfc8725bis/)
- [PortSwigger — Algorithm Confusion Attacks](https://portswigger.net/web-security/jwt/algorithm-confusion)
- `docs/technical/pkg/improvement-roadmap.md` § Quick Wins QW-2 + QW-4
