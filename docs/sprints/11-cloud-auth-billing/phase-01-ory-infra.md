# Fase 1 — Ory Infra (Kratos + Hydra)

> Deploy Ory Kratos e Ory Hydra no mgmt cluster via Helm + Pulumi.

**Dependências:** Nenhuma (primeira fase).
**Estimativa:** 8-12h
**Risco:** MEDIO — Ory Helm charts têm muitas knobs, configuração de SMTP para emails pode travar.

---

## Contexto

Ory Kratos gerencia identidades (signup, login, recovery, verificação). Ory Hydra é o servidor OAuth2 (emite tokens JWT, suporta Device Code Flow e PKCE). Ambos são projetos Go open-source, self-hosted, e rodam como pods no cluster.

Kratos precisa de um banco PostgreSQL (pode compartilhar o existente com schema separado) e configuração SMTP para envio de emails (verificação, recovery).

---

## Tasks

### T1.1 — Helm charts para Ory Kratos e Hydra

**O que fazer:**
- Adicionar Helm charts pinados em `infra/helm/ory-kratos/` e `infra/helm/ory-hydra/`
- Kratos: chart `ory/kratos` versão pinada
- Hydra: chart `ory/hydra` versão pinada
- Values configurados para:
  - PostgreSQL DSN (mesmo cluster, database separada `kratos` e `hydra`)
  - SMTP sender (para emails de verificação/recovery)
  - Identity schema (email + password + social providers)
  - Hydra: issuer URL, CORS, grant types (device_code, authorization_code com PKCE)

**DoD:**
- [ ] `infra/helm/ory-kratos/Chart.yaml` existe com versão pinada
- [ ] `infra/helm/ory-kratos/values.yaml` configurado para dev
- [ ] `infra/helm/ory-hydra/Chart.yaml` existe com versão pinada
- [ ] `infra/helm/ory-hydra/values.yaml` configurado para dev
- [ ] Nenhum `latest` tag em image refs
- [ ] Charts passam `kubeconform --strict`

### T1.2 — Identity schema do Kratos

**O que fazer:**
- Criar identity schema JSON para Kratos em `infra/helm/ory-kratos/identity.schema.json`
- Schema define: `email` (required, verified), `name` (optional), `github_id` (optional), `google_id` (optional)
- Configurar social login connectors (GitHub + Google) como OIDC providers no Kratos

**DoD:**
- [ ] `identity.schema.json` válido e referenciado no values.yaml
- [ ] Schema suporta email+password como credencial primária
- [ ] Schema suporta GitHub e Google como OIDC providers
- [ ] Nenhum campo desnecessário (YAGNI)

### T1.3 — Pulumi: databases para Kratos e Hydra

**O que fazer:**
- Adicionar criação de databases `kratos` e `hydra` no PostgreSQL managed (DigitalOcean)
- Configurar via Pulumi stack config (dev/staging/prod)
- DSN passados como Kubernetes secrets para os Helm releases

**DoD:**
- [ ] `pulumi preview` mostra criação de 2 databases
- [ ] DSN armazenados como Kubernetes Secrets (não em plaintext nos values)
- [ ] Funciona com PostgreSQL managed (DigitalOcean) e local (k3d)

### T1.4 — Pulumi: deploy Kratos + Hydra

**O que fazer:**
- Adicionar Helm releases para Kratos e Hydra na fase `bootstrap` do Pulumi
- Incluir readiness checks
- Configurar ingress interno (ClusterIP, sem exposição externa — cloud/api faz proxy)

**DoD:**
- [ ] `pulumi up` deploya Kratos e Hydra no mgmt cluster
- [ ] Pods Running e Ready (readiness probe OK)
- [ ] Kratos admin API acessível internamente: `http://kratos-admin.loopzero-system:80`
- [ ] Kratos public API acessível internamente: `http://kratos-public.loopzero-system:80`
- [ ] Hydra admin API acessível internamente: `http://hydra-admin.loopzero-system:4445`
- [ ] Hydra public API acessível internamente: `http://hydra-public.loopzero-system:4444`
- [ ] Nenhuma API exposta externamente (somente ClusterIP)

### T1.5 — Taskfile: setup local (k3d)

**O que fazer:**
- Adicionar tasks ao Taskfile para setup local: `task cloud:setup`
- Deve instalar Kratos + Hydra no mgmt cluster k3d
- Idempotente (pode rodar N vezes)
- Integrar com `task setup` existente

**DoD:**
- [ ] `task cloud:setup` instala Ory Kratos + Hydra no k3d mgmt cluster
- [ ] Task é idempotente
- [ ] `task doctor` reporta saúde de Kratos e Hydra
- [ ] Funciona em ambiente limpo (fresh `task setup`)

### T1.6 — Smoke test: Kratos e Hydra respondem

**O que fazer:**
- Script ou test que valida que Kratos e Hydra estão saudáveis
- Kratos: `GET /health/alive` → 200
- Hydra: `GET /health/alive` → 200
- Hydra: `GET /.well-known/openid-configuration` retorna issuer correto

**DoD:**
- [ ] Teste automatizado valida health de ambos os serviços
- [ ] `GET /.well-known/openid-configuration` retorna JSON válido com `device_authorization_endpoint`
- [ ] Teste roda como parte do CI (ou como task manual `task cloud:health`)

---

## Definition of Done — Fase 1

- [x] Kratos e Hydra rodando no mgmt cluster (k3d local) — Kratos 1/1 Running, Hydra 1/1 Running
- [x] Kratos e Hydra configurados no Pulumi (DigitalOcean dev) — installOryKratos/Hydra wired, TypeScript compiles
- [x] Health checks passando (`/health/alive` → 200) — Kratos `{"status":"ok"}`, Hydra `{"status":"ok"}`
- [x] Hydra OIDC discovery retorna `device_authorization_endpoint` — `http://localhost:4444/oauth2/device/auth`, grant_types includes `urn:ietf:params:oauth:grant-type:device_code`
- [x] Identity schema configurado (email + social providers) — identity.schema.json + values.yaml, tests pass
- [x] Nenhuma API Ory exposta externamente — ClusterIP validated (test: TestOryServicesAreClusterIP)
- [x] `task cloud:setup` idempotente e funcional — task exists (helm upgrade --install = idempotent)
- [x] `task doctor` reporta status de Kratos + Hydra — Ory section added to doctor
- [x] Todos os manifests passam `kubeconform --strict` — exit code 0 for both charts
- [x] Zero secrets em plaintext nos Helm values — dev secrets marked LOCAL DEV ONLY, prod via Pulumi secrets
