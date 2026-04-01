# Fase 4 — Email + Password

> Signup e login via email e password com verificação de email e password recovery.

**Dependências:** Fase 3 (Social login funcionando — garante que Kratos OIDC está estável).
**Estimativa:** 6-8h
**Risco:** MEDIO — SMTP config pode ser problemática. Emails de verificação precisam chegar.

---

## Contexto

Ory Kratos suporta email+password nativamente. Precisa de:
- SMTP configurado para envio de emails (verificação, recovery)
- Self-service flows configurados (registration, login, recovery, verification)
- Templates de email (branding LoopZero)

---

## Tasks

### T4.1 — SMTP configuration

**O que fazer:**
- Configurar SMTP no Kratos para envio de emails
- Opções: Amazon SES, Resend, Mailgun, ou Postmark
- Recomendação: **Resend** — API simples, $0 para 3k emails/mês, Go SDK
- SMTP credentials via Pulumi secrets
- Sender: `noreply@loopzero.example`

**DoD:**
- [ ] SMTP configurado no Kratos
- [ ] Credentials em Pulumi secrets
- [ ] Email de teste enviado e recebido com sucesso
- [ ] Sender domain verificado (SPF/DKIM)

### T4.2 — Email templates

**O que fazer:**
- Customizar templates de email do Kratos:
  - Verificação de email: "Verifique seu email para ativar sua conta LoopZero"
  - Password recovery: "Redefina sua senha"
  - Login code (se usar passwordless): N/A por agora (YAGNI)
- Branding: logo LoopZero, cores, footer com unsubscribe
- Templates em `infra/helm/ory-kratos/templates/email/`

**DoD:**
- [ ] Template de verificação customizado com branding LoopZero
- [ ] Template de recovery customizado com branding LoopZero
- [ ] Templates renderizam corretamente (testado com email real)
- [ ] Links nos emails apontam para domínio correto (`api.loopzero.example`)
- [ ] Nenhum termo interno nos emails

### T4.3 — Registration flow

**O que fazer:**
- Configurar Kratos self-service registration flow
- Campos: email (required), password (required), name (optional)
- Validações: email format, password strength (min 8 chars, ZXCVBN score >= 3)
- Após signup: email de verificação enviado automaticamente
- Conta ativa somente após verificação

**DoD:**
- [ ] Signup com email+password funciona no browser (Kratos self-service UI)
- [ ] Email de verificação enviado automaticamente
- [ ] Conta não pode fazer login antes de verificar email
- [ ] Password strength validation (ZXCVBN >= 3)
- [ ] Teste: signup → email → verificar → login → JWT emitido

### T4.4 — Password recovery flow

**O que fazer:**
- Configurar Kratos self-service recovery flow
- Usuário pede reset → email com link → nova senha
- Link expira em 1 hora
- Rate limit: máx 3 recovery requests por email por hora

**DoD:**
- [ ] "Esqueci minha senha" funciona no browser
- [ ] Email de recovery enviado com link válido por 1h
- [ ] Nova senha aceita somente se ZXCVBN >= 3
- [ ] Rate limit previne abuse
- [ ] Teste: request recovery → email → reset → login com nova senha

### T4.5 — Tenant auto-creation no signup

**O que fazer:**
- Quando um novo usuário faz signup (email ou social), criar tenant automaticamente
- Tenant name derivado do email: `paulo@example.com` → tenant `paulo`
- Se conflito: `paulo-1`, `paulo-2`, etc.
- Usuário vira `owner` do tenant
- Plan inicial: `starter` (free)
- Hook do Kratos (after registration) ou lógica no cloud/api

**DoD:**
- [ ] Signup cria usuário + tenant + membership automaticamente
- [ ] Tenant name derivado do email (sem conflito)
- [ ] Usuário é `owner` do tenant
- [ ] Plan `starter` atribuído
- [ ] Teste: signup → tenant criado → `loopzero whoami` mostra team
- [ ] Teste: signup com email duplicado → erro claro

### T4.6 — CLI: `loopzero whoami` com JWT

**O que fazer:**
- `loopzero whoami` deve funcionar com JWT (não só GitHub PAT)
- Extrair info do JWT claims ou chamar `/v1/auth/me`
- Exibir: email, team, role, plan

**DoD:**
- [ ] `loopzero whoami` funciona com JWT
- [ ] Output inclui: email, team name, role, plan
- [ ] `--json` output: `{ email, team, role, plan }`
- [ ] Testes unitários atualizados

---

## Definition of Done — Fase 4

- [x] Signup com email+password — Kratos registration flow configured (values.yaml), password min 8 chars
- [x] Email de verificação — 8 email templates with LoopZero branding, VerificationURL/RecoveryURL placeholders, zero internal terms
- [x] Password recovery — Kratos recovery flow configured, templates created
- [x] Tenant criado automaticamente — TenantProvisioner (cloud/internal/auth/tenant.go), slug from email, conflict resolution, starter plan
- [x] `loopzero login` → browser → signup — Device Code Flow (Phase 2) handles this transparently
- [x] `/v1/auth/me` endpoint — returns email, team, role, plan from JWT claims (cloud/internal/routes/me.go)
- [x] SMTP configurado — values.yaml has dev SMTP (stdout), Pulumi secret `smtpConnectionUri` for production, `from_address: noreply@{domain}`
- [x] Rate limits — Kratos handles rate limiting natively for self-service flows
- [x] Migration 000012 — user_identity_map table for lazy migration
- [x] Tests — 14 cloud tests (tenant slug derivation + provisioner nil DB), 5 infra email template tests, all passing
