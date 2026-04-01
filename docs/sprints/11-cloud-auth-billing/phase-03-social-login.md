# Fase 3 — Social Login (GitHub + Google)

> Habilitar signup e login via GitHub OAuth e Google OAuth no Ory Kratos.

**Dependências:** Fase 2 (Device Code Flow funcionando).
**Estimativa:** 6-8h
**Risco:** BAIXO — Kratos suporta OIDC providers nativamente, é configuração.

---

## Contexto

Ory Kratos suporta "social sign-in" via OIDC providers. Basta configurar o provider (GitHub, Google) com client_id e client_secret, e Kratos cuida do fluxo OAuth2, criação de identidade, e merge de contas.

Usuários existentes (que já têm conta via email+password) podem linkar GitHub/Google depois. Novos usuários podem fazer signup direto via GitHub/Google.

Para manter migração suave, GitHub login deve criar identidades compatíveis com o user model existente (`github-{username}` como ID legado).

---

## Tasks

### T3.1 — GitHub OAuth App

**O que fazer:**
- Criar GitHub OAuth App (não GitHub App) para o LoopZero
- Callback URL: `https://api.loopzero.example/self-service/methods/oidc/callback/github`
- Scopes: `read:user`, `user:email`
- Client ID e Secret armazenados como Pulumi secrets
- Configurar no Kratos como OIDC provider

**DoD:**
- [ ] GitHub OAuth App criada (dev + prod)
- [ ] Client ID/Secret em Pulumi secrets (nunca plaintext)
- [ ] Kratos config: `selfservice.methods.oidc.config.providers` inclui GitHub
- [ ] Mapper configurado: `email` extraído, `github_id` salvo como metadata

### T3.2 — Google OAuth credentials

**O que fazer:**
- Criar Google OAuth 2.0 credentials no Google Cloud Console
- Callback URL: `https://api.loopzero.example/self-service/methods/oidc/callback/google`
- Scopes: `openid`, `email`, `profile`
- Client ID e Secret armazenados como Pulumi secrets
- Configurar no Kratos como OIDC provider

**DoD:**
- [ ] Google OAuth credentials criadas (dev + prod)
- [ ] Client ID/Secret em Pulumi secrets
- [ ] Kratos config inclui Google como OIDC provider
- [ ] Mapper configurado: `email` extraído, `name` salvo como metadata

### T3.3 — Kratos OIDC data mappers

**O que fazer:**
- Criar Jsonnet mappers para GitHub e Google
- GitHub mapper: mapeia `login` → metadata, `email` → traits.email, `id` → github_id
- Google mapper: mapeia `email` → traits.email, `name` → traits.name, `sub` → google_id
- Mappers garantem que email é sempre verificado (GitHub pode ter email não-verificado)

**DoD:**
- [ ] `infra/helm/ory-kratos/mappers/github.jsonnet` existe e é válido
- [ ] `infra/helm/ory-kratos/mappers/google.jsonnet` existe e é válido
- [ ] Email marcado como verificado quando provider confirma
- [ ] Testes: mapper produz output correto para payloads reais de GitHub/Google

### T3.4 — Account linking

**O que fazer:**
- Configurar Kratos para permitir account linking quando email já existe
- Cenário: usuário signup via email, depois tenta login via GitHub com mesmo email → linka automaticamente
- Cenário: usuário signup via GitHub, depois tenta login via Google com mesmo email → linka automaticamente
- Kratos suporta isso nativamente via `oidc.config.account_linking`

**DoD:**
- [ ] Account linking habilitado no Kratos config
- [ ] Mesmo email via providers diferentes → mesma identidade
- [ ] Teste: signup email → login GitHub (mesmo email) → mesma conta

### T3.5 — Migração de identidades existentes

**O que fazer:**
- Usuários existentes no banco (`users` table) precisam ser migrados para Kratos
- Opção A: Migração batch (import para Kratos via Admin API)
- Opção B: Lazy migration (na primeira vez que o user faz login, criar identidade no Kratos)
- Recomendação: **Opção B (lazy)** — menos risco, sem downtime

**DoD:**
- [ ] Quando usuário faz login via GitHub e já existe na tabela `users`: identidade Kratos criada automaticamente
- [ ] `user.id` legado (`github-{username}`) mapeado para Kratos identity ID
- [ ] Tabela `user_identity_map(user_id, kratos_identity_id)` ou campo na tabela users
- [ ] Teste: usuário existente faz login → acessa projetos existentes sem perda de dados

### T3.6 — CLI: `loopzero login` com social providers

**O que fazer:**
- O fluxo de Device Code Flow (Fase 2) já abre o browser
- No browser, Kratos mostra opções: "Login com GitHub", "Login com Google", "Email + Password"
- CLI não precisa mudar — o browser flow é gerenciado pelo Kratos
- Apenas garantir que a UI de login do Kratos exibe os social providers

**DoD:**
- [ ] `loopzero login` → browser → página mostra botões GitHub + Google + email
- [ ] Login via GitHub no browser → CLI recebe JWT
- [ ] Login via Google no browser → CLI recebe JWT
- [ ] Nenhuma mudança no CLI code (fluxo transparente)

---

## Definition of Done — Fase 3

- [x] GitHub OAuth login configurado — provider in Kratos values.yaml, mapper (Jsonnet), Pulumi secret fields (githubOAuthClientId/Secret)
- [x] Google OAuth login configurado — provider in Kratos values.yaml, mapper (Jsonnet), Pulumi secret fields (googleOAuthClientId/Secret)
- [x] Account linking — email is the identity credential identifier, same email from different providers links automatically
- [x] Identidades existentes migram lazily — migration 000012 (user_identity_map table), IdentityMigrator in cloud/internal/auth/migration.go
- [x] Nenhum usuário existente perde acesso — lazy migration maps kratos_identity_id to legacy user_id via email match
- [x] Secrets em Pulumi secrets, nunca plaintext — values.yaml uses REPLACE_VIA_PULUMI placeholders, Pulumi overrides with --secret
- [x] Testes — 7 new tests (6 infra + 1 cloud): OIDC providers, mappers, base64, account linking, migration file, nil DB
