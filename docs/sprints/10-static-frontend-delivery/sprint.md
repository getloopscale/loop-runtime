# Sprint 10 — Static Frontend Delivery + Security

> **Decisions:** MEET-009 + MEET-011 + Cloudflare Tunnel First

**Objetivo:** Proteger toda a plataforma com Cloudflare Tunnel (DDoS/WAF/cache), depois servir frontends estáticos diretamente de DO Spaces via Cloudflare Edge — zero pods para frontend, zero nginx intermediário, zero tráfego de assets no cluster.

**Definition of Done:** Todo tráfego da plataforma protegido pelo Cloudflare Tunnel (cluster sem IP público). Monorepo (api + web `render: static` + worker) deploya no DO com frontend servido de DO Spaces via Cloudflare Edge, URL unificada, build SUCCEEDED, alertas ativos.

---

## Fase 0 — Cloudflare Tunnel (PRIORIDADE)

**Objetivo:** Todo tráfego passa pelo Cloudflare Edge antes de chegar ao cluster. Proteção L3-L7 ativa. Base para static delivery.

### 0.1 — Cloudflare Account + Zone Setup
- **Ação:** Criar conta Cloudflare (free tier). Adicionar zone `dev.loopzero.example`. Migrar DNS para Cloudflare nameservers (ou CNAME setup).
- **Done:** `dig dev.loopzero.example` resolve via Cloudflare nameservers.

### 0.2 — Criar Tunnel
- **Ação:** `cloudflared tunnel create loopzero-dev`. Salvar credentials como K8s Secret no mgmt cluster.
- **Done:** Tunnel ID criado, credentials file gerada.

### 0.3 — Deploy cloudflared no cluster
- **Ação:** Deployment `cloudflared` no namespace `platform-system` (runtime cluster).
- **Config:** `config.yaml` com ingress rules:
  ```yaml
  tunnel: <tunnel-id>
  ingress:
    - hostname: "*.dev.loopzero.example"
      service: http://runtime-traefik.platform-system.svc:80
    - service: http_status:404
  ```
- **Done:** cloudflared pod running, tunnel connected.

### 0.4 — DNS: apontar wildcard para Tunnel
- **Ação:** `cloudflared tunnel route dns loopzero-dev "*.dev.loopzero.example"`. Cria CNAME `*.dev.loopzero.example → <tunnel-id>.cfargotunnel.com`.
- **Done:** `curl https://api-production.e2e-project.tn-loopzero.example.loopzero.example/health` funciona via CF.

### 0.5 — Cloudflare Security Rules
- **Ação:**
  - WAF Managed Ruleset (OWASP) — ativado
  - Bot Fight Mode — ativado
  - Rate Limiting Rules — 100 req/10s por IP
  - SSL mode: Full (strict)
- **Done:** Dashboard CF mostra tráfego filtrado. Requests maliciosos bloqueados.

### 0.6 — Remover IP público do Load Balancer
- **Ação:** Após confirmar que todo tráfego chega via tunnel:
  - DO Cloud Firewall: bloquear TODO tráfego inbound nos worker nodes exceto tunnel
  - Ou: mudar Traefik Service de `LoadBalancer` para `ClusterIP` (tunnel faz o ingress)
- **Done:** Cluster invisível da internet. `nmap` no IP antigo do LB não responde.

### 0.7 — Validação
- **Checklist:**
  - [ ] API acessível via CF: `curl https://api-production.e2e-project.tn-loopzero.example.loopzero.example/health`
  - [ ] Worker pods running
  - [ ] CF dashboard mostra requests
  - [ ] WAF blocking test: `curl` com SQLi payload → bloqueado
  - [ ] Rate limit test: 200 requests em 10s → últimos bloqueados
  - [ ] Deploy E2E funciona (`loopzero deploy` → build → release → app acessível)

**Exit Gate Fase 0:** Plataforma 100% atrás do Cloudflare. Zero IP público. WAF ativo.

---

## Fase 1 — Static Frontend via Cloudflare + DO Spaces

**Objetivo:** `render: static` serve assets de DO Spaces via Cloudflare Edge. Zero pods. Zero tráfego de assets no cluster. Cache global.

### 1.1 — Cloudflare Cache Rules para static assets
- **Ação:** Cloudflare Cache Rules na zone `dev.loopzero.example`:
  - `*.js`, `*.css`, `*.png`, `*.svg`, `*.woff2` → Edge Cache TTL 1 year
  - `index.html` → Edge Cache TTL 60s com revalidation
  - `/api/*` paths → bypass cache
- **Done:** Static assets servidos do CF edge (0ms TTFB global).

### 1.2 — Cloudflare Worker: Static Asset Router
- **Ação:** Cloudflare Worker que intercepta requests para frontends static:
  ```js
  // Pseudocode
  addEventListener('fetch', event => {
    const url = new URL(event.request.url);
    const hostname = url.hostname;
    // Parse: {app}-{env}.{project}.{tenant}.dev.loopzero.example
    const { tenant, project, app, env } = parseHostname(hostname);

    // Lookup active build-id (cached in KV, refreshed by LoopZero API on deploy)
    const buildId = await LOOPZERO_ROUTING.get(`${tenant}/${project}/${app}/${env}`);

    // Fetch from DO Spaces
    const s3Url = `https://loopzero-static-assets.sfo3.digitaloceanspaces.com/${tenant}/${project}/${app}/${buildId}${url.pathname}`;
    return fetch(s3Url);
  });
  ```
- **Cloudflare KV:** routing table `tenant/project/app/env → build-id`
- **Updated on deploy:** `StaticDeliveryActivator.Activate()` writes to KV via CF API
- **Done:** Static frontend request → CF Worker → DO Spaces. Zero cluster involvement.

### 1.3 — StaticDeliveryActivator: update CF KV on deploy
- **Ação:** `StaticDeliveryActivator.Activate()` faz:
  - Phase A: create release (READY) — idêntico
  - Phase B: upload routing entry to Cloudflare KV (`PUT /accounts/{id}/storage/kv/namespaces/{ns}/values/{key}`)
  - Phase C: record result (PENDING) — idêntico
  - Phase D: verify — HTTP GET na URL do frontend, verificar content
  - Phase E: finalize (ACTIVE) — idêntico
- **Sem gitops push para static apps.** Sem ArgoCD. Sem IngressRoute. A entrega é um KV update.
- **Done:** Deploy de static app atualiza KV, CF Worker serve nova versão imediatamente.

### 1.4 — renderStaticFrontendApp(): zero K8s resources
- **Ação:** Para `render: static`, o manifest renderer gera **ZERO recursos K8s**. Nenhum IngressRoute, nenhum Service, nenhum Deployment. O TLS é do Cloudflare (não cert-manager).
- **Done:** `Render()` para static app retorna string vazia. ArgoCD não aplica nada.

### 1.5 — SPA Routing no CF Worker
- **Ação:** Worker detecta paths sem extensão e serve `index.html`:
  ```js
  if (!url.pathname.match(/\.\w+$/)) {
    // SPA fallback: serve index.html
    return fetch(s3Url.replace(url.pathname, '/index.html'));
  }
  ```
- **Done:** Deep link `/dashboard/settings` retorna `index.html`.

### 1.6 — Security Headers no CF Worker
- **Ação:** Worker adiciona headers em todas responses:
  - `X-Frame-Options: DENY`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: strict-origin-when-cross-origin`
  - `Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'`
- **Done:** Headers presentes.

### 1.7 — Rollback
- **Ação:** `loopzero rollback` para static app:
  1. Lê release anterior do banco
  2. Atualiza CF KV com build-id anterior
  3. CF Worker serve assets antigos imediatamente (S3 assets são imutáveis)
- **Done:** Rollback em <2s. Cache-Control `no-cache` no `index.html` garante que browsers buscam a nova versão.

### 1.8 — URL Unificada
- **Ação:** Mesmo hostname format: `{app}-{env}.{project}.{tenant}.dev.loopzero.example`
- CF Worker intercepta requests de static apps (lookup no KV).
- Requests para container apps passam direto para o tunnel → Traefik → pods.
- **Done:** Zero divergência de URL entre container e static.

### 1.9 — Validação DO
- **Checklist:**
  - [ ] API: pod, `{"status":"ok"}`, via CF tunnel
  - [ ] Web: ZERO pods, HTML servido de DO Spaces via CF Worker
  - [ ] Worker: pod running
  - [ ] Build: SUCCEEDED
  - [ ] `curl -I` no web URL mostra security headers
  - [ ] SPA deep link funciona
  - [ ] Rollback funciona (KV update → new content served)
  - [ ] Cache: segundo request ao mesmo asset é CF HIT (0ms TTFB)
  - [ ] Deploy de nova versão: CF Worker serve nova versão em <5s

**Exit Gate Fase 1:** Frontend estático 100% via Cloudflare Edge. Zero pods. Zero tráfego de assets no cluster. Cache global. Rollback instant.

---

## Fase 2 — In-Cluster Security (complementar)

**Objetivo:** Proteção adicional dentro do cluster para tráfego que chega via tunnel.

### 2.1 — Traefik RateLimit Middleware
- Rate limit por IP no Traefik (segunda camada após CF)
- Protege contra abuse que passa pelo CF free tier

### 2.2 — CrowdSec Agent + Bouncer
- Fail2ban colaborativo lendo Traefik access logs
- Blocklists comunitárias
- Bouncer como Traefik middleware

### 2.3 — Alerting
- `LoopZeroRateLimitTriggered`, `LoopZeroCrowdSecBan`, `LoopZeroHighErrorRate`

---

## Fase 3 — Custom Domains (futuro)

**Trigger:** Primeiro tenant que pede custom domain.

### 3.1 — Cloudflare for SaaS
- $0.10/hostname/mês
- Tenants CNAME seu domínio → CF provê TLS automático

---

## Decisões Arquiteturais

| Decisão | Justificativa |
|---------|---------------|
| **Cloudflare PRIMEIRO, não depois** | CF Tunnel protege tudo E é a base para static delivery. Sem CF, precisamos nginx-s3-gateway + Traefik proxy (complexidade desnecessária). |
| **CF Worker para routing de static** | Worker roda no CF edge, não no cluster. Routing por hostname, lookup em KV. Zero compute no cluster para assets. |
| **CF KV para routing table** | Build-id ativo por tenant/app/env. Atualizado no deploy pela API. Lido pelo Worker em cada request (<1ms leitura). |
| **Zero K8s resources para static** | Sem IngressRoute, sem Service, sem Deployment. TLS é do CF. Cache é do CF. Routing é do CF Worker. O cluster não sabe que o frontend existe. |
| **URLs unificadas** | Mesmo hostname. CF Worker decide: KV tem entry → serve de S3. KV não tem → passa para tunnel (container app). |
| **nginx-s3-gateway eliminado** | Era solução para "sem CF". Com CF, é peça intermediária desnecessária. |
