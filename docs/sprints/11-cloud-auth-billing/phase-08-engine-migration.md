# Fase 8 — Engine Migration (JWT)

> Migrar a API engine de GitHub PAT validation para JWT validation. Backward compatible.

**Dependências:** Fase 2 (JWT validation middleware existe no cloud/).
**Estimativa:** 6-8h
**Risco:** BAIXO — middleware de auth da API já usa interface `TokenValidator`. Troca de implementação.

---

## Contexto

A API engine (`api/`) hoje valida tokens chamando a GitHub API. Com o cloud/ emitindo JWTs via Hydra, o engine precisa aceitar JWTs.

A migração é suave porque o middleware de auth (`api/internal/middleware/auth.go`) já usa a interface `TokenValidator`. Basta criar uma implementação `JWTValidator` e configurar dual validation (aceita JWT e PAT durante a transição).

---

## Tasks

### T8.1 — JWTValidator para o engine

**O que fazer:**
- Nova implementação de `TokenValidator` em `api/internal/middleware/jwt_validator.go`
- Valida JWT contra JWKS do Hydra (mesmo código do `cloud/internal/auth/jwt.go`, extrair para `pkg/`)
- Extrai claims → constrói `UserContext`
- JWKS cacheado com refresh automático

**DoD:**
- [ ] `JWTValidator` implementa `TokenValidator`
- [ ] Valida JWT contra JWKS do Hydra
- [ ] Claims extraídos: sub, email, tenant_id, role
- [ ] `UserContext` construído corretamente
- [ ] JWKS cache com refresh
- [ ] Testes unitários com JWTs de teste

### T8.2 — Dual validation (JWT + PAT)

**O que fazer:**
- Criar `DualValidator` que tenta JWT primeiro, depois PAT (GitHub) se JWT falhar
- Configurar como padrão no `api/cmd/main.go`
- Se token começa com `ghp_` ou `gho_` → route para GitHub validator
- Senão → route para JWT validator

**DoD:**
- [ ] `DualValidator` implementa `TokenValidator`
- [ ] Token JWT → validado via JWKS
- [ ] Token GitHub PAT → validado via GitHub API (como hoje)
- [ ] Seleção automática por formato do token
- [ ] Testes: JWT válido → OK, PAT válido → OK, inválido → 401

### T8.3 — JWT shared lib (pkg/jwt)

**O que fazer:**
- Extrair JWT validation para `pkg/jwt/` (shared entre cloud/ e api/)
- JWKS fetcher, token parser, claims extractor
- Evita duplicação (DRY) entre cloud/ e engine

**DoD:**
- [ ] `pkg/jwt/` existe com: `Validator`, `JWKSFetcher`, `Claims`
- [ ] Usado por `cloud/internal/auth/` e `api/internal/middleware/`
- [ ] Zero duplicação de lógica de validação
- [ ] Testes unitários no pkg/

### T8.4 — Config: JWKS URL no engine

**O que fazer:**
- Adicionar `JWKSEndpoint` ao config do engine (`api/internal/config/config.go`)
- Valor: URL do Hydra JWKS endpoint
- Em dev: `http://hydra-public.loopzero-system:4444/.well-known/jwks.json`
- Em prod: URL interna do Hydra
- Quando vazio: dual validation desabilitado, só PAT funciona (backward compatible)

**DoD:**
- [ ] `JWKSEndpoint` na config
- [ ] Quando set: dual validation habilitado
- [ ] Quando vazio: só PAT (comportamento atual)
- [ ] Testes: config com/sem JWKS → validator correto instanciado

### T8.5 — Deprecation warning no PAT login

**O que fazer:**
- Quando `loopzero login --token ghp_xxx` é usado: exibir warning
  ```
  Warning: GitHub token authentication is deprecated.
  Run `loopzero login` (without --token) to switch to the new login flow.
  GitHub token support will be removed in a future version.
  ```
- Warning vai para stderr (não interfere com `--json`)
- Não bloqueia — funciona normalmente, só avisa
- `LOOPZERO_SUPPRESS_DEPRECATION=1` silencia (para CI scripts em migração)

**DoD:**
- [ ] Warning exibido no stderr ao usar `--token` com PAT
- [ ] `LOOPZERO_SUPPRESS_DEPRECATION=1` silencia
- [ ] Warning não aparece com JWT token
- [ ] `--json` output não afetado
- [ ] Testes: PAT → warning stderr, JWT → sem warning

### T8.6 — Testes de integração: dual auth

**O que fazer:**
- Testes que validam o fluxo completo:
  1. Request com JWT válido → 200 + UserContext correto
  2. Request com PAT válido → 200 + UserContext correto + deprecation header
  3. Request com JWT expirado → 401
  4. Request com PAT inválido → 401
  5. Request sem token → 401
  6. Request com JWT → mesmo resultado que PAT para a mesma operação

**DoD:**
- [ ] 6 cenários de teste passando
- [ ] JWT e PAT produzem o mesmo UserContext para o mesmo usuário
- [ ] Nenhuma regressão nos testes existentes

---

## Definition of Done — Fase 8

- [x] Engine API dual validation — DualValidator routes JWT→JWKSValidator, PAT→GitHubValidator by token format
- [x] JWT validation via JWKS — pkg/jwt/Validator with JWKS fetch + cache (5min TTL) + RSA key parsing
- [x] PAT validation (deprecated) — existing GitHubValidator untouched, DualValidator falls back to it
- [x] Shared lib pkg/jwt/ — Validator, IsJWTToken, IsGitHubPAT. Used by cloud/ and api/ (zero duplication)
- [x] Deprecation warning — stderr warning for ghp_/gho_ tokens, suppressed in JSON mode + LOOPZERO_SUPPRESS_DEPRECATION=1
- [x] Zero regression — 32/32 login tests pass, 22 API packages pass, middleware tests pass
- [x] Config backward compatible — JWKS_ENDPOINT env var: when empty, DualValidator uses PAT-only (nil JWT validator)
- [x] Tests — pkg/jwt: 13 subtests (IsJWTToken, IsGitHubPAT), middleware: 4 tests (dual routing, fallback, format detection)
