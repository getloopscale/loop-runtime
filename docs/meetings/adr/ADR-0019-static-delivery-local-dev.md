# ADR-0019 — Static Frontend Delivery em Local Dev (sem CF Worker)

| | |
|---|---|
| **Status** | APPROVED |
| **Date** | 2026-04-29 |
| **Plan** | docs/plans/dogfood-real-e2e-faang-plan.md (T1.1) |
| **Discovery** | F7 do triage de bugs em CHG-2026-04-29-DOGFOOD-FB |
| **Reviewers** | platform-architect, production-design-reviewer, dx-engineer |

## Problem

Em `api/internal/services/static_delivery.go:phaseBDeliver`, quando `s.kvClient == nil` (caso típico em standalone-multi sem credenciais Cloudflare), a função retorna `nil` silenciosamente:

```go
func (s *StaticDeliveryActivator) phaseBDeliver(ctx, build, release, result) error {
    if s.kvClient == nil {
        slog.Warn("static delivery: CF KV client not configured, skipping KV write (local dev)")
        return nil
    }
    // ... write to CF KV
}
```

Consequência:
- Release transita para ACTIVE como se delivery tivesse sucesso
- Mas **não há entry CF KV** → CF Worker não tem rota → frontend nunca é servido
- User curl no host do app retorna 404 ou backend default

Isso viola **Rule 10 (Local must equal production)** explicitamente: o produto "funciona" em prod (CF KV configurado) e "funciona aparentemente" em local (silent-success), mas o frontend não é acessível em nenhum dos casos sem CF Worker rodando.

Discovered live durante CHG-2026-04-29-DOGFOOD-FB Phase 1: build pipeline com `deliver-artifact-web` Succeeded, mas web app nunca respondeu em standalone-multi.

## Constraints

1. **Não introduzir dependência externa pesada** — wrangler dev/cloudflared adiciona complexidade considerável ao bootstrap.
2. **Não criar branches "if local-dev"** — Rule 10. Se o caminho de delivery static depende do ambiente, o teste não detecta drift entre local e prod.
3. **Compatibilidade com Sprint 10 já shipped** — não quebrar contrato com CF Worker em produção real.
4. **Bootstrap reproduzível** — qualquer dev externo deve conseguir `task setup -e standalone-multi` e ter frontend funcional sem credenciais Cloudflare.

## Options

### Option A — Traefik fallback servindo direto de MinIO/S3

Implementação: quando `kvClient == nil`, gerar IngressRoute Traefik em vez de CF KV entry. Rota aponta para MinIO via path-based routing usando o nome canônico `<subdomain>.loopzero.local → minio.loopzero-system:9000/loopzero-static-assets/<s3prefix>/`.

**Prós**:
- Reusa Traefik + MinIO já presentes no stack
- Nenhuma dependência nova
- Comportamento idêntico ao container path (Traefik é fonte de verdade)

**Cons**:
- Asset path layout: MinIO retorna XML index quando GET em prefix → precisa de `default.html` ou middleware que reescreve `/` → `/index.html`
- MinIO não tem auth public-read por default; expor sem auth requer policy bucket explícita
- Branch `if kvClient == nil` continua existindo no código — apenas com comportamento diferente do silent-success. Rule 10 melhor honrada, mas dois caminhos vivos
- Domain/host routing precisa duplicar a lógica do CF Worker (subdomain extraction + S3 prefix lookup)

### Option B — CF Worker rodando em container local (Wrangler/cloudflared)

Implementação: adicionar release Helm com `cloudflare/wrangler:latest` rodando o `worker.mjs` em modo `wrangler dev`, escutando em :8787, registrado como Service em `loopzero-system`. Traefik IngressRoute encaminha `*.loopzero.local` para esse Service.

**Prós**:
- Comportamento bit-exact igual a produção — mesmo binário (`worker.mjs`)
- Detecta bugs no Worker code que mock não detectaria
- ADR D5 do plano dogfood-completo previa essa opção

**Cons**:
- Wrangler em container precisa de KV API mock (anel de dependências cíclicas)
- 200+ MB de imagem
- Wrangler dev local CHAMA `https://api.cloudflare.com` para namespace lookup → exige token CF mesmo para "modo local"
- Bootstrap pesado — não cabe em "task setup standalone"

### Option C — `cfkv.Client` ganha endpoint customizável + Mock HTTP server in-cluster

Implementação:
1. Refatorar `cfkv.NewClient` para aceitar `BaseURL` opcional (default: `https://api.cloudflare.com/client/v4`)
2. Criar `infra/helm/cfkv-mock/` com Helm chart (Go binary minimal — net/http server compatível com 4 endpoints CF KV: PUT/GET/DELETE values + GET namespaces)
3. Em `values-standalone-multi.yaml`, declarar `CF_KV_BASE_URL=http://cfkv-mock.loopzero-system:8080` + tokens dummy não-vazios
4. Worker code (`worker.mjs`) NÃO precisa rodar em local — LoopZero apenas escreve nas keys; serve assets é problema separado
5. Para serve assets em standalone-multi: combinar com Option A (Traefik+MinIO), mas a "delivery write" usa path real (mock CF KV), eliminando o branch silent-success

**Prós**:
- `kvClient` deixa de ser opcional — sempre não-nil. Branch `if s.kvClient == nil` deletado. Rule 10 honrada strict.
- Mock é binary < 5MB (Go static)
- API contrato estável (CF KV API v1 público)
- Independente de Worker — testar persistence layer separadamente do compute layer

**Cons**:
- Implementar mock = 1-2 dias trabalho
- Ainda não entrega "frontend HTTP 200" sozinho — precisa Option A combinada para servir assets

### Option D — Decision híbrida: A + C

Implementação:
1. **Curto prazo (T1.2)**: implementar Option A (Traefik+MinIO IngressRoute) — entrega Prova de Vida do frontend em <1 dia, MAS mantém branch `if kvClient == nil` com comportamento diferente do silent-success (gera IngressRoute em vez de retornar nil)
2. **Médio prazo (follow-up issue)**: implementar Option C (mock CF KV) e remover o branch `if kvClient == nil` completamente

## Decision

**Option D — híbrido.** Aplicar Option A imediatamente para destravar dogfood Phase 4 (multi-app misto), abrir issue para Option C como follow-up (não bloqueia release).

### Rationale

- Option A entrega valor imediato (frontend funcional em local) sem mock pesado
- O branch `if kvClient == nil` sai do silent-success e passa a ter **comportamento positivo verificável** (IngressRoute criada → curl funciona)
- Mock CF KV (Option C) é o caminho ideal mas custa 1-2 dias e não afeta Rule 10 já que comportamento agora é determinístico
- Worker code (Option B) fica fora de escopo — adiciona complexidade sem benefício comparável

### Implementation contract (T1.2)

1. Em `static_delivery.go:phaseBDeliver`, substituir o branch `if s.kvClient == nil { return nil }` por:
   - Se `kvClient != nil`: comportamento atual (write CF KV)
   - Se `kvClient == nil` AND `s.localStaticDelivery != nil`: invocar `localStaticDelivery.Provision(ctx, subdomain, s3Prefix)` que cria IngressRoute Traefik
   - Se ambos nil: **fail-loud** (retorna error explicando configuração incompleta)
2. Novo type `LocalStaticDelivery` em `services/local_static_delivery.go`:
   - Recebe RuntimeClient (controller-runtime)
   - Cria/atualiza Traefik IngressRoute apontando para Service `minio.loopzero-system:9000`
   - Middleware `path-strip` + `add-prefix` para mapear `<host>/` → `<host>/index.html` e prefixar com S3 path
3. `cmd/setup_workflows.go`: quando `cfg.CFAccountID == ""` mas `cfg.LocalStaticDeliveryEnabled == true`, instanciar `LocalStaticDelivery` e injetar no `StaticDeliveryActivator`
4. `values-standalone-multi.yaml`: setar `LOCAL_STATIC_DELIVERY=true`
5. **Bucket public-read**: bootstrap helmfile script aplica policy MinIO em `loopzero-static-assets` permitindo `s3:GetObject` anônimo (apenas em standalone — produção usa CF KV path por default)

### Acceptance criteria for T1.2

- [ ] `static_delivery.go:phaseBDeliver` não retorna mais nil silenciosamente — sempre retorna sucesso real ou error fatal
- [ ] Em standalone-multi com `LOCAL_STATIC_DELIVERY=true`, `loopzero deploy` de app static gera IngressRoute Traefik
- [ ] `curl --connect-to <host>:443:127.0.0.1:8443 -k https://<host>/` retorna `index.html` da app Vite
- [ ] Em produção (DOKS) com CF credentials, comportamento inalterado (CF KV write)
- [ ] Test unit cobrindo 3 cenários: kvClient set / localStaticDelivery set / ambos nil (fail-loud)

### Rollback plan

Se LocalStaticDelivery introduzir regression:
1. `helm upgrade loopzero-api --set env.LOCAL_STATIC_DELIVERY=false` desliga branch local
2. Sistema volta ao silent-success (estado atual). Branch original preservado por feature flag enquanto Option D não está totalmente confiável.

## Consequences

### Positive
- Phase 4 do dogfood (multi-app misto) destravada
- Rule 10 honrada (frontend funciona em local idêntico ao prod)
- Refactoring oferece base para Option C (mock CF KV) no futuro

### Negative
- Dois caminhos de delivery static no código (CF KV + Local Traefik) — mais surface área
- Bucket MinIO precisa policy public-read em standalone — atenção operacional
- IngressRoute Traefik adicionada por static deploy precisa cleanup em Prova de Morte
