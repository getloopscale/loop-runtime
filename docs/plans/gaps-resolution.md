# Gaps Resolution Plan — Post Custom Domains v2

> 10 gaps identificados em 2026-04-08 após validação E2E com aquim.ai.
> Ordenados por severidade. Cada gap é uma fase independente.

---

## Fase 1: loopzero-stacks Templates (CRÍTICO — primeiro contato do usuário)

### Contexto
`npx create-loopzero` usa templates de `getloopscale/loopzero-stacks`. Os templates atuais geram loopzero.yaml que o validator agora rejeita.

### 1.1 — Fix monorepo-turbo template

**Arquivo:** `templates/monorepo-turbo/loopzero.yaml`

**Mudanças:**
- Remover `render: server` do app web (frontend auto-infer static)
- Remover `commands` block (não reconhecido pelo CLI — gera warning)
- Adicionar `type: frontend` explícito no web (clareza)

**Antes:**
```yaml
web:
  path: apps/web
  framework: nextjs
  type: frontend
  render: server
  commands:
    dev: "npm run dev"
    build: "npm run build"
```

**Depois:**
```yaml
web:
  path: apps/web
  framework: nextjs
  type: frontend
```

**DoD:**
- [ ] `loopzero validate` no template → 0 errors, 0 warnings
- [ ] `loopzero deploy` simulado → passa validação pré-upload
- [ ] PR no loopzero-stacks com CI green

### 1.2 — Fix monorepo-python template

**Mudanças:**
- Remover `port: 8001` do worker
- Adicionar `type: worker` explícito (clareza)
- Remover `commands` block
- Adicionar Dockerfile no worker (framework: custom precisa)

**DoD:**
- [ ] `loopzero validate` → 0 errors
- [ ] Worker não tem porta
- [ ] Worker tem Dockerfile funcional

### 1.3 — Fix monorepo-go template

**Mudanças:**
- Adicionar Dockerfiles para api e worker (framework: custom)
- Remover `commands` block

**DoD:**
- [ ] `loopzero validate` → 0 errors
- [ ] Ambos os Dockerfiles buildam com sucesso
- [ ] Go binary funcional no container

### 1.4 — Fix monorepo-ruby template

**Mudanças:**
- Adicionar Dockerfiles para api e worker
- Remover `commands` block

**DoD:**
- [ ] `loopzero validate` → 0 errors
- [ ] Sinatra app funcional no container

### 1.5 — Fix monorepo-java template

**Mudanças:**
- Adicionar Dockerfiles (Maven/Gradle build)
- Remover `commands` block

**DoD:**
- [ ] Spring Boot app funcional no container

### 1.6 — Fix monorepo-php template

**Mudanças:**
- Adicionar Dockerfiles (PHP-FPM ou built-in server)
- Remover `commands` block

**DoD:**
- [ ] PHP Slim app funcional no container

### 1.7 — Fix monorepo-rust template

**Mudanças:**
- Adicionar Dockerfiles (multi-stage com cargo build)
- Remover `commands` block

**DoD:**
- [ ] Axum app funcional no container

### 1.8 — Fix fullstack-nextjs template

**Mudanças:**
- Remover `render: server` (auto-infer static)
- Remover `commands` block

**DoD:**
- [ ] `loopzero validate` → 0 errors

### 1.9 — Fix single-app templates (node-express, python-fastapi, etc.)

**Mudanças:**
- Remover `commands` block de todos
- Garantir que cada template passa `loopzero validate`

**DoD:**
- [ ] Todos os 19 templates passam `loopzero validate` com 0 errors, 0 warnings

### 1.10 — CI: validação automática de templates

**Arquivo:** `.github/workflows/validate-templates.yml` no loopzero-stacks

**O que:** GitHub Action que roda `loopzero validate` em cada template em todo PR.

**DoD:**
- [ ] CI roda `loopzero validate` para cada template
- [ ] PR que gera template inválido é bloqueado

---

## Fase 2: Server-Side Framework Validation (CRÍTICO)

### 2.1 — API rejeita frameworks não reconhecidos

**Arquivo:** `api/internal/routes/builds.go`

**Mudança:** No handler `CreateBuild`, validar que cada app.framework é mapeável pelo `resolveLanguageStrategy()` antes de submeter ao pipeline. Se `strategy == "custom"` e `build != "dockerfile"`, retornar 422.

```go
for _, app := range req.Apps {
    strategy := resolveLanguageStrategy(app.Framework)
    if strategy == "custom" && app.Build != "dockerfile" {
        writeJSON(w, 422, map[string]string{
            "error": fmt.Sprintf("App '%s': framework '%s' is not supported with buildpack. Use 'build: dockerfile' or a supported framework.", app.Name, app.Framework),
            "code": "UNSUPPORTED_FRAMEWORK",
        })
        return
    }
}
```

**DoD:**
- [ ] API retorna 422 para `framework: sinatra` + `build: buildpack`
- [ ] API aceita `framework: sinatra` + `build: dockerfile`
- [ ] API aceita todos os 18 frameworks mapeados
- [ ] Teste unitário em `builds_test.go`
- [ ] Mensagem de erro sugere `build: dockerfile`

---

## Fase 3: Pulumi Re-deploy (CRÍTICO)

### 3.1 — Deploy ClusterIssuer dual solver para todos os clusters

**Arquivo:** `infra/pulumi/src/components/bootstrap-shared.ts` (já commitado)

**Ação:** Rodar `pulumi up` para aplicar o dual solver (DNS-01 + HTTP-01) nos clusters mgmt e runtime.

**DoD:**
- [ ] `kubectl get clusterissuer letsencrypt-prod -o yaml` mostra 2 solvers (DNS-01 com dnsZones selector + HTTP-01 catch-all)
- [ ] Cluster mgmt: dual solver ativo
- [ ] Cluster runtime: dual solver ativo
- [ ] Certificados existentes (*.dev.loopzero.example) continuam válidos

### 3.2 — Deploy CF_ZONE_MANAGEMENT em produção

**Ação:** Garantir que `CF_ZONE_MANAGEMENT=true` está no deployment da API via Pulumi (não só via `kubectl set env`).

**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts` ou `infra/tilt/api-deployment.yaml`

**DoD:**
- [ ] Env var persistida no Pulumi stack (não se perde em redeploy)
- [ ] `pulumi preview` mostra a env var
- [ ] API logs: "custom domains: CF Zone Management enabled (v2)"

---

## Fase 4: CF API Rate Limiting (MÉDIO)

### 4.1 — Rate limiter no cfzone client

**Arquivo:** `api/internal/cfzone/client.go`

**Mudança:** Adicionar rate limiter (token bucket) ao client. CF permite 1200 req/5min = 4 req/s.

```go
type Client struct {
    // ...
    rateLimiter *rate.Limiter // golang.org/x/time/rate
}

func (c *Client) do(...) error {
    if err := c.rateLimiter.Wait(ctx); err != nil {
        return fmt.Errorf("cf rate limit: %w", err)
    }
    // ... rest of request
}
```

**DoD:**
- [ ] Client respeita 4 req/s (configurável)
- [ ] Com 100 domains pending, não excede rate limit
- [ ] Teste: 10 requests em 1s → 4 executam, 6 aguardam
- [ ] Sem dependência externa (usar `golang.org/x/time/rate` que já está no go.mod)

### 4.2 — Batch polling no provisioner

**Arquivo:** `api/internal/services/domain_provisioner.go`

**Mudança:** Em vez de chamar `GetZoneStatus` por domain, listar todas as zones em batch:
```go
// Batch: GET /zones?name=domain1.com,domain2.com
// Reduz N requests para 1
```

Alternativa: o `processPendingNS` já limita a 50 domains por tick (via `findByStatus` LIMIT 50). Com rate limiter, 50 requests/30s = 1.6 req/s. Dentro do limite.

**Decisão:** Rate limiter (4.1) é suficiente. Batch é otimização futura.

**DoD:**
- [ ] Rate limiter implementado (4.1)
- [ ] Verificado que 50 domains/tick não excede 4 req/s

---

## Fase 5: DNS Import Review Flow (MÉDIO)

### 5.1 — CLI mostra records importados com opção de editar

**Arquivo:** `cli/src/commands/domain.ts`

**Mudança:** Após `loopzero domain add`, mostrar os records importados e oferecer opção de revisar:

```
✓ Domain registered

  12 DNS records imported:
  ┌────────┬──────────────────────────┬───────────────────────────────┐
  │ Type   │ Name                     │ Value                         │
  ├────────┼──────────────────────────┼───────────────────────────────┤
  │ MX     │ @                        │ mx1.hostinger.com (pri 5)     │
  │ MX     │ @                        │ mx2.hostinger.com (pri 10)    │
  │ TXT    │ @                        │ v=spf1 include:_spf.hos...    │
  │ CNAME  │ hostingermail-a._domain  │ hostingermail-a.dkim.ma...    │
  │ ...    │                          │                               │
  └────────┴──────────────────────────┴───────────────────────────────┘

  ⚠ Review the records above. If any are missing, add them:
    loopzero domain dns add aquim.ai --type TXT --name @ --value "..."

  To activate, update your nameservers:
    chance.ns.cloudflare.com
    mina.ns.cloudflare.com
```

**DoD:**
- [ ] Records listados em tabela formatada
- [ ] `--json` inclui `dns_records` array
- [ ] Mensagem sugere `loopzero domain dns add` para records faltantes
- [ ] Teste: output inclui records importados

### 5.2 — Deduplicar SPF records no import

**Arquivo:** `api/internal/routes/domains.go` (`createWithCFZone`)

**Mudança:** Após scan, detectar e remover SPF duplicados (com e sem aspas escapadas).

```go
// Deduplicate: CF scan sometimes imports "v=spf1..." and v=spf1... (same content, different quoting)
seen := map[string]bool{}
for _, rec := range records {
    key := rec.Type + ":" + rec.Name + ":" + strings.Trim(rec.Content, "\"")
    if seen[key] {
        _ = h.cfZone.DeleteDNSRecord(ctx, zone.ZoneID, rec.ID)
        continue
    }
    seen[key] = true
}
```

**DoD:**
- [ ] SPF não aparece duplicado no `loopzero domain dns`
- [ ] Teste: scan com SPF duplicado → apenas 1 mantido

---

## Fase 6: Domain Provisioning Monitoring (MÉDIO)

### 6.1 — Métricas Prometheus para domain lifecycle

**Arquivo:** `api/internal/metrics/metrics.go`

**Novas métricas:**
```
loopzero_domain_pending_ns_total       # Domains aguardando NS propagation
loopzero_domain_ns_active_total        # Domains com NS ativo aguardando deploy
loopzero_domain_provisioning_duration  # Histogram: tempo de pending_ns → active
loopzero_domain_cf_api_errors_total    # Erros na CF API
```

**DoD:**
- [ ] Métricas expostas no `/metrics`
- [ ] Grafana dashboard atualizado (ou query manual funciona)

### 6.2 — Alerta para domains stuck

**Arquivo:** `infra/helm/monitoring/` ou Alertmanager rules

**Regra:**
```yaml
- alert: DomainProvisioningStuck
  expr: loopzero_domain_pending_ns_total > 0 and time() - loopzero_domain_oldest_pending_ns > 3600
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Domain stuck in pending_ns for > 1 hour"
```

**DoD:**
- [ ] Alerta dispara quando domain fica >1h em pending_ns
- [ ] Alerta resolve quando domain transiciona
- [ ] Testável com domain de teste

---

## Fase 7: loopzero-packs Suporte a Linguagens Adicionais (MÉDIO)

### 7.1 — Ruby Dockerfile generation

**Arquivo:** `infra/images/packs-runner/loopzeropacks-generate`

**Mudança:** Adicionar detecção de `Gemfile` e geração de Dockerfile para Ruby (Sinatra, Rails).

```bash
elif [ -f "$FULL_PATH/Gemfile" ]; then
  FRAMEWORK="ruby"
```

**Dockerfile gerado:**
```dockerfile
FROM ruby:3.3-slim
WORKDIR /app
COPY Gemfile Gemfile.lock ./
RUN bundle install --without development test
COPY . .
EXPOSE 4567
CMD ["bundle", "exec", "ruby", "app.rb"]
```

**DoD:**
- [ ] `loopzeropacks-generate` detecta Ruby apps
- [ ] Dockerfile gerado funciona com Sinatra e Rails
- [ ] Teste: Gemfile + Sinatra → Dockerfile válido

### 7.2 — Java/Spring Dockerfile generation

**Detecção:** `pom.xml` ou `build.gradle`

```dockerfile
FROM maven:3.9-eclipse-temurin-21 AS builder
WORKDIR /app
COPY pom.xml .
RUN mvn dependency:go-offline
COPY src ./src
RUN mvn package -DskipTests

FROM eclipse-temurin:21-jre-alpine
COPY --from=builder /app/target/*.jar /app.jar
EXPOSE 8080
CMD ["java", "-jar", "/app.jar"]
```

**DoD:**
- [ ] Detecta Maven e Gradle
- [ ] Spring Boot app funcional no container

### 7.3 — PHP Dockerfile generation

**Detecção:** `composer.json`

**DoD:**
- [ ] Slim/Laravel apps funcionais

### 7.4 — Rust Dockerfile generation

**Detecção:** `Cargo.toml`

**DoD:**
- [ ] Multi-stage build (cargo build → alpine)
- [ ] Axum app funcional

### 7.5 — Atualizar resolveLanguageStrategy para novas linguagens

**Arquivo:** `api/internal/services/build_plan.go`

**Mudanças:**
```go
case "sinatra", "rails", "ruby":
    return "ruby"
case "spring", "java":
    return "java"
case "laravel", "slim", "php":
    return "php"
case "axum", "actix", "rust":
    return "rust"
```

**Arquivo:** `api/internal/argo/languages.go`

**Mudanças:** Novas strategies: RubyStrategy, JavaStrategy, PHPStrategy, RustStrategy com validate/install/build/scan scripts.

**DoD:**
- [ ] Cada linguagem tem strategy com validate + install + build + scan
- [ ] Testes unitários para cada strategy
- [ ] `resolveLanguageStrategy("sinatra")` retorna `"ruby"`
- [ ] Build pipeline funciona end-to-end para cada linguagem

---

## Fase 8: Domain v1→v2 Migration + Verify Deprecation (BAIXO)

### 8.1 — Deprecar `POST /domains/{domain}/verify` (v1)

**Arquivo:** `api/internal/routes/domains.go`

**Mudança:** Quando `CF_ZONE_MANAGEMENT=true`, retornar 410 Gone com mensagem:

```json
{
    "code": "DEPRECATED",
    "message": "TXT verification is deprecated. Domains now use nameserver delegation. Use 'loopzero domain add' to register a new domain."
}
```

**DoD:**
- [ ] Endpoint retorna 410 quando v2 ativo
- [ ] Endpoint funciona normalmente quando v2 desativado (backward compat)
- [ ] CLI `loopzero domain verify` mostra mensagem clara se 410

### 8.2 — Migration tool: v1 domains → v2

**Arquivo:** Novo script ou CLI command `loopzero domain migrate <domain>`

**Fluxo:**
1. Fetch existing v1 domain from DB
2. Create CF zone para o domínio
3. Scan + import DNS records
4. Update DB row com CF zone fields
5. Transicionar status para `pending_ns`
6. Retornar nameservers

**DoD:**
- [ ] Domínios v1 existentes podem ser migrados sem downtime
- [ ] Email records preservados
- [ ] Teste: domain v1 active → migrate → pending_ns → ns_active → active

---

## Fase 9: `commands` Field no loopzero.yaml (BAIXO)

### 9.1 — Suportar `commands` field no parser

**Arquivo:** `cli/src/adapters/loopzero-yaml.ts`

**Mudança:** Adicionar `commands` ao `LoopZeroYamlApp` interface (opcional, ignorado pelo deploy mas usado por `loopzero dev`):

```typescript
interface LoopZeroYamlApp {
    // ... existing fields
    commands?: {
        dev?: string;
        build?: string;
        test?: string;
        lint?: string;
        format?: string;
        security?: string;
    };
}
```

**DoD:**
- [ ] `commands` field não gera warning
- [ ] `loopzero validate` aceita `commands` sem warning
- [ ] `commands` é ignorado pelo deploy (não enviado ao server)
- [ ] Testes atualizados

### 9.2 — `loopzero dev` usa commands field

**Futuro:** Usar `commands.dev` para `loopzero dev` (dev server local). Não implementar agora — apenas aceitar o campo.

---

## Fase 10: Path Traversal Validation no CLI (BAIXO — SEC)

### 10.1 — Rejeitar path traversal no validator

**Arquivo:** `cli/src/adapters/validate-config.ts`

**Mudança:**
```typescript
// SEC: reject path traversal
const resolved = path.resolve(projectRoot, app.path);
if (!resolved.startsWith(path.resolve(projectRoot))) {
    errors.push({
        field: `apps.${name}.path`,
        message: `App '${name}': path '${app.path}' escapes project root. Use a relative path within the project.`,
    });
}
```

**DoD:**
- [ ] `../../etc` → error
- [ ] `apps/api` → pass
- [ ] `./apps/../apps/api` → pass (resolves within project)
- [ ] Teste unitário

---

## Sequenciamento

```
Fase 1 (loopzero-stacks) ─────── Paralela com tudo (repo separado)
Fase 2 (server-side validation) ──→ Deploy junto com Fase 3
Fase 3 (Pulumi deploy) ──→ Prerequisito para produção
Fase 4 (rate limiter) ──→ Antes de >50 custom domains
Fase 5 (DNS review) ──→ DX improvement
Fase 6 (monitoring) ──→ Antes de produção real
Fase 7 (novas linguagens) ──→ Após loopzero-stacks fix
Fase 8 (v1 deprecation) ──→ Após validação v2 em produção
Fase 9 (commands field) ──→ Baixa prioridade
Fase 10 (path traversal) ──→ SEC fix, baixa prioridade (server tem SEC-3)
```

**Prioridade imediata:** Fases 1 + 2 + 3 (bloqueiam produção)
**Antes de launch:** Fases 4 + 5 + 6
**Pós-launch:** Fases 7 + 8 + 9 + 10

## Resumo

| Fase | Gap | Tasks | Severidade | Estimativa |
|------|-----|-------|------------|------------|
| 1 | loopzero-stacks templates | 10 | Crítico | Médio |
| 2 | Server-side framework validation | 1 | Crítico | Pequeno |
| 3 | Pulumi re-deploy | 2 | Crítico | Pequeno |
| 4 | CF API rate limiting | 2 | Médio | Pequeno |
| 5 | DNS import review | 2 | Médio | Médio |
| 6 | Domain monitoring | 2 | Médio | Médio |
| 7 | Novas linguagens (Ruby/Java/PHP/Rust) | 5 | Médio | Grande |
| 8 | v1 deprecation + migration | 2 | Baixo | Médio |
| 9 | `commands` field | 2 | Baixo | Pequeno |
| 10 | Path traversal SEC fix | 1 | Baixo | Pequeno |
| **Total** | **10 gaps** | **29 tasks** | | |
