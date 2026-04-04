# Fase 2 — Device Code Flow + JWT

> Implementar `loopzero login` via browser usando RFC 8628 Device Authorization Grant.

**Dependências:** Fase 1 (Ory Infra rodando).
**Estimativa:** 12-16h
**Risco:** MEDIO — Device Code Flow tem polling loop no CLI que precisa de UX cuidadosa.

---

## Contexto

O Device Code Flow (RFC 8628) é o padrão da indústria para CLI auth:
1. CLI pede device code ao Hydra
2. CLI exibe URL + código para o usuário
3. Usuário abre browser, faz login, autoriza
4. CLI polls Hydra até receber o JWT
5. CLI salva JWT localmente

Isso substitui `loopzero login --token ghp_xxx` como fluxo primário. O `--token` continua como fallback para CI.

---

## Tasks

### T2.1 — Criar módulo `cloud/` com go.mod

**O que fazer:**
- Criar estrutura `cloud/` conforme arquitetura aprovada
- `cloud/go.mod` como módulo Go separado
- Adicionar ao `go.work` do workspace
- Estrutura: `cmd/`, `internal/auth/`, `internal/routes/`, `internal/config/`

**DoD:**
- [ ] `cloud/go.mod` existe e compila
- [ ] Adicionado ao `go.work`
- [ ] `go build ./cloud/...` passa
- [ ] Estrutura de diretórios criada (mesmo padrão que `api/`)

### T2.2 — Config do cloud service

**O que fazer:**
- `cloud/internal/config/config.go` com campos:
  - `KratosAdminURL`, `KratosPublicURL`
  - `HydraAdminURL`, `HydraPublicURL`
  - `HydraClientID` (OAuth2 client ID para o CLI)
  - `ListenAddr` (padrão `:13002`)
  - `EngineAPIURL` (URL do engine API para proxy)
  - `JWTIssuer` (Hydra issuer URL para validação)
- Carregamento via env vars (mesmo padrão do `api/internal/config/`)
- Validação: todos os campos obrigatórios em produção

**DoD:**
- [ ] Config struct com todos os campos
- [ ] Validação de campos obrigatórios
- [ ] Testes unitários para loading e validação
- [ ] Env var names documentadas no struct (tags ou comentários)

### T2.3 — Hydra OAuth2 client registration

**O que fazer:**
- Registrar OAuth2 client no Hydra para o CLI (via Hydra Admin API)
- Grant types: `urn:ietf:params:oauth:grant-type:device_code`, `refresh_token`
- Scope: `openid`, `offline_access`
- Client tipo: `public` (CLI não pode guardar client secret)
- Script ou Pulumi config que registra o client automaticamente no setup

**DoD:**
- [ ] OAuth2 client registrado no Hydra com device_code grant
- [ ] Client é `public` (sem client_secret)
- [ ] Registro é idempotente (pode rodar N vezes)
- [ ] Client ID configurável via Pulumi/env

### T2.4 — Device Code Flow endpoints (cloud/api)

**O que fazer:**
- `POST /v1/auth/device/authorize` — CLI envia request, cloud/api faz proxy para Hydra device authorize endpoint. Retorna `{ device_code, user_code, verification_uri, expires_in, interval }`.
- `POST /v1/auth/device/token` — CLI polls com `device_code`. Cloud/api faz proxy para Hydra token endpoint. Retorna `{ access_token, refresh_token, token_type, expires_in }` quando autorizado, ou `{ error: "authorization_pending" }` enquanto espera.
- `POST /v1/auth/token/refresh` — Renova access_token com refresh_token.

**DoD:**
- [ ] 3 endpoints implementados e testados
- [ ] `/device/authorize` retorna device_code + verification_uri
- [ ] `/device/token` retorna `authorization_pending` antes de autorizar
- [ ] `/device/token` retorna JWT após autorização
- [ ] `/token/refresh` renova access_token com refresh_token
- [ ] Testes unitários para cada endpoint (success + error cases)
- [ ] Nenhum internal term exposto (sem "Hydra", "Kratos", "OIDC" no output)

### T2.5 — JWT validation middleware

**O que fazer:**
- `cloud/internal/auth/jwt.go` — valida JWT emitidos pelo Hydra
- Busca JWKS do Hydra (`GET /.well-known/jwks.json`), cacheia com TTL
- Extrai claims: `sub` (user ID), `email`, `tenant_id` (custom claim)
- Constrói `UserContext` compatível com o formato do engine API
- Middleware chi que injeta `UserContext` no request context

**DoD:**
- [ ] JWT validado contra JWKS do Hydra
- [ ] JWKS cacheado (refresh a cada 5 min ou on-failure)
- [ ] Claims extraídos: sub, email, tenant_id, role
- [ ] `UserContext` compatível com formato do engine API
- [ ] 401 para token inválido/expirado
- [ ] 403 para token sem tenant
- [ ] Testes unitários com JWTs gerados (test fixtures)

### T2.6 — Consent flow (Hydra)

**O que fazer:**
- Implementar consent e login handlers que Hydra chama para aprovar/rejeitar auth
- Login handler: redireciona para Kratos login flow
- Consent handler: auto-aprova para CLI (first-party client, trusted)
- Pode ser um handler simples em `cloud/api` ou usar Kratos self-service UI

**DoD:**
- [ ] Login handler redireciona para Kratos self-service login
- [ ] Consent handler auto-aprova para o CLI client
- [ ] Fluxo completo funciona: device code → browser login → consent → token emitido
- [ ] Testes de integração cobrindo o fluxo end-to-end

### T2.7 — CLI: `loopzero login` com Device Code Flow

**O que fazer:**
- Modificar `cli/src/commands/login.ts`:
  - Sem `--token`: inicia Device Code Flow
    1. `POST /v1/auth/device/authorize`
    2. Exibe: "Abra {verification_uri} e insira o código: {user_code}"
    3. Tenta abrir browser automaticamente (`open` / `xdg-open`)
    4. Poll `POST /v1/auth/device/token` a cada `interval` segundos
    5. Ao receber token: salva em `~/.loopzero/config.json`
    6. Exibe: "Autenticado como {email}"
  - Com `--token`: mantém fluxo legado (GitHub PAT ou JWT direto)
- Timeout: 5 minutos (device code expira)
- CI mode: `--token` obrigatório (sem browser)

**DoD:**
- [ ] `loopzero login` sem args abre browser e completa auth
- [ ] `loopzero login --token xxx` continua funcionando (backward compatible)
- [ ] Token JWT salvo em `~/.loopzero/config.json`
- [ ] Refresh token salvo junto
- [ ] Timeout de 5 minutos com mensagem clara
- [ ] CI mode (`CI=true`) requer `--token`
- [ ] `--json` output: `{ ok: true, email: "...", team: "..." }`
- [ ] Exit codes corretos: 0 success, 4 auth error, 1 usage error
- [ ] Testes unitários: success flow, timeout, CI mode, --token fallback
- [ ] Zero termos internos na output (sem "Hydra", "device_code", "JWT")

### T2.8 — CLI: token refresh automático

**O que fazer:**
- Antes de qualquer API call, verificar se o access_token está expirado
- Se expirado + refresh_token disponível: chamar `/v1/auth/token/refresh`
- Se refresh também falhou: pedir re-login
- Transparente para o usuário

**DoD:**
- [ ] Access token renovado automaticamente antes de expirar
- [ ] Usuário não percebe renovação (silenciosa)
- [ ] Se refresh falha: mensagem "Sessão expirada. Execute `loopzero login`" + exit code 4
- [ ] Testes unitários para: token válido (noop), token expirado (refresh), refresh expirado (re-login)

---

## Definition of Done — Fase 2

- [x] Módulo `cloud/` existe e compila no Go workspace — go build exit 0, go.work updated
- [x] `loopzero login` abre browser, usuário autentica, CLI recebe JWT — Device Code Flow implemented (device-flow.ts), LoginCommand updated
- [x] JWT validado pelo cloud/api middleware — JWKSValidator (jwt.go) + Claims extraction
- [x] Token refresh automático e transparente — token-refresh.ts + ensureFreshToken() + ConfigStore.isTokenExpired()
- [x] `loopzero login --token xxx` continua funcionando (backward compatible) — 44 existing login tests pass (zero regression)
- [x] CI mode requer `--token` — LoginCommand returns MISSING_TOKEN in CI mode
- [ ] 3 deploys estáveis após integração — requires end-to-end validation with running cloud service
- [x] Testes unitários para todos os endpoints e fluxos CLI — 10 Go tests + 11 CLI tests + 44 existing login tests
- [x] Zero termos internos em qualquer output do CLI — validated in TestDeviceAuthorize + device-flow.test.ts
- [x] `--json` output segue spec §0.3 — LoginCommand JSON output: { authenticated, email, team }
