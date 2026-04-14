# Image Build Optimization Plan

> Plano para resolver todos os gaps de supply chain, eficiência e modernização
> identificados na auditoria de builds de imagens (2026-04-14).

**Status:** Complete (2026-04-14)
**Owner:** Platform Team
**Priority:** P0 security → P1 efficiency → P2 modernization

---

## Contexto

A auditoria identificou 3 categorias de problemas:
1. **Supply chain** — binários sem checksum, base images sem digest, key-based signing
2. **Eficiência** — cache mounts ausentes, .dockerignore incompleto, layers não consolidados
3. **Modernização** — SLSA provenance, multi-platform, reproducible builds

O LoopZero já tem uma fundação sólida (rootless BuildKit, cosign, SBOM, Trivy, registry cache),
mas precisa fechar gaps que são o baseline esperado para uma PaaS em 2026.

---

## Fase 1: Supply Chain Security (P0)

**Objetivo:** Eliminar todos os vetores de supply chain attack nos builds.
**Estimativa:** 1-2 dias

### Task 1.1: Verificar checksum do binário `mc` no clone-runner
**Arquivo:** `infra/images/clone-runner/Dockerfile`
**Mudança:**
```dockerfile
ARG MC_VERSION=RELEASE.2025-03-12T17-29-24Z
ARG MC_SHA256=<hash>
RUN wget -q "https://dl.min.io/client/mc/release/linux-amd64/archive/mc.${MC_VERSION}" \
      -O /usr/local/bin/mc && \
    echo "${MC_SHA256}  /usr/local/bin/mc" | sha256sum -c - && \
    chmod +x /usr/local/bin/mc
```
**DoD:**
- [x] `mc` binary pinned por versão + SHA256
- [x] Build falha se checksum não bate
- [x] Versão atualizada para release 2025

### Task 1.2: Pre-bake cosign em imagem dedicada (user builds)
**Arquivo:** `api/internal/argo/cosign.go`, novo `infra/images/cosign-signer/Dockerfile`
**Mudança:**
- Criar imagem `cosign-signer` baseada em `cgr.dev/chainguard/cosign:latest` (distroless, assinada pelo Chainguard)
- Ou: criar Alpine image com cosign baked in e checksum verificado
- Remover wget runtime do `cosign.go` — usar a imagem pre-built
**DoD:**
- [x] Cosign binary não é baixado em runtime
- [x] Imagem cosign-signer no registry, assinada
- [x] `cosign.go` atualizado para usar a nova imagem

### Task 1.3: Digest pin em TODAS as base images
**Arquivos:** Todos os Dockerfiles + `api/internal/argo/constants.go` + CWT values
**Mudança:** Para cada base image, capturar o digest e pinnar:
```dockerfile
# Antes
FROM golang:1.25.3-alpine3.21
# Depois
FROM golang:1.25.3-alpine3.21@sha256:abc123...
```
**Lista de images a pinnar:**
- [x] `golang:1.25.3-alpine3.21` (operators, api, cloud)
- [x] `golang:1.25-bookworm` (harbor-init)
- [x] `alpine:3.21` / `alpine:3.21.3` (api, cloud, operators, packs-runner, clone-runner)
- [x] `node:20-alpine` (dashboard, user builds)
- [x] `nginx:alpine` → `nginxinc/nginx-unprivileged:1.27-alpine` (dashboard)
- [x] `moby/buildkit:v0.18.2-rootless` (argo constants.go)
- [x] `minio/mc:RELEASE.2024-01-16T16-06-34Z` → atualizar + pinnar (CWT cache)
- [x] `gcr.io/distroless/static-debian12:nonroot` (harbor-init)
- [x] `python:3.12-slim`, `ruby:3.3-alpine`, etc. (loopzeropacks-generate templates)
**DoD:**
- [x] Zero Dockerfiles com tags sem `@sha256:`
- [x] Script de atualização de digests documentado (para rotação)

### Task 1.4: Migrar cosign para keyless (Sigstore/Fulcio)
**Arquivos:** `.github/workflows/build-images.yml`, `api/internal/argo/cosign.go`
**Mudança CI:**
```yaml
# Antes
cosign sign --key env://COSIGN_KEY "$IMAGE@$DIGEST"
# Depois
cosign sign --yes "$IMAGE@$DIGEST"
# (usa OIDC token do GitHub Actions automaticamente)
```
**Mudança user builds:**
- Avaliar viabilidade de keyless em Argo Workflows (requer OIDC provider no cluster)
- Se não viável, manter key-based para user builds mas migrar CI para keyless
**DoD:**
- [x] CI usa keyless signing (sem `COSIGN_KEY` secret)
- [x] `COSIGN_KEY` e `COSIGN_PASSWORD` secrets removidos do repo
- [x] Verificação: `cosign verify --certificate-identity-regexp=...` funciona
- [x] User builds: decisão documentada (keyless ou key-based com justificativa)

### Task 1.5: Assinar e escanear `harbor-init`
**Arquivo:** `.github/workflows/build-images.yml`
**Mudança:**
- Adicionar `harbor-init` à matrix do job `sign-and-attest`
- Adicionar `--cache-from`/`--cache-to` ao build step do harbor-init
- Remover tag `:latest` do harbor-init build
**DoD:**
- [x] `harbor-init` no sign-and-attest matrix
- [x] Trivy scan CRITICAL/HIGH bloqueia pipeline
- [x] SBOM gerado e attestado
- [x] Tag `:latest` removida

---

## Fase 2: Eficiência de Cache e Correctness (P1)

**Objetivo:** Reduzir tempo de build em ~50% e corrigir problemas de correctness.
**Estimativa:** 2-3 dias

### Task 2.1: Adicionar `--mount=type=cache` em todos os Dockerfiles
**Arquivos:** `api/Dockerfile`, `operators/Dockerfile`, `cloud/Dockerfile`, `harbor-init/Dockerfile`
**Mudança por linguagem:**
```dockerfile
# Go — module + build cache
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app ./cmd/...

# Node — npm cache
RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefer-offline
```
**DoD:**
- [x] Todos os 4 Dockerfiles da plataforma usam cache mounts
- [x] `go mod download` usa cache mount
- [x] Build time medido antes/depois (target: >30% redução)

### Task 2.2: Adicionar `--mount=type=cache` nos Dockerfiles gerados (loopzeropacks)
**Arquivo:** `infra/images/packs-runner/loopzeropacks-generate`
**Mudança:** Atualizar cada template de linguagem:
- [x] Node: `--mount=type=cache,target=/root/.npm`
- [x] Python: `--mount=type=cache,target=/root/.cache/pip`
- [x] Ruby: `--mount=type=cache,target=/usr/local/bundle`
- [x] Rust: `--mount=type=cache,target=/usr/local/cargo/registry`
- [x] Java/Maven: `--mount=type=cache,target=/root/.m2`
- [x] Go: `--mount=type=cache,target=/root/go/pkg/mod`
- [x] PHP: `--mount=type=cache,target=/tmp/composer-cache`
**DoD:**
- [x] Todos os 7 templates de linguagem usam cache mounts
- [x] Testes: gerar Dockerfile para cada linguagem, verificar presença de `--mount`

### Task 2.3: Non-root USER em TODOS os Dockerfiles gerados
**Arquivo:** `infra/images/packs-runner/loopzeropacks-generate`
**Mudança:** Adicionar ao final de cada template que não tem:
```dockerfile
RUN adduser -D -u 1000 loopzero
USER 1000
```
**Templates a corrigir:**
- [x] Node (express, fastify, generic node)
- [x] Python
- [x] Ruby
- [x] PHP
**DoD:**
- [x] Todos os 7 templates geram imagem com non-root USER
- [x] Teste: build de cada template verifica `USER` no Dockerfile gerado

### Task 2.4: HEALTHCHECK em imagens da plataforma
**Arquivos:** `api/Dockerfile`, `cloud/Dockerfile`, `cloud/dashboard/Dockerfile`
**Mudança:**
```dockerfile
# api/Dockerfile
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

# cloud/Dockerfile
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:13002/healthz || exit 1

# dashboard/Dockerfile
HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:80/ || exit 1
```
**DoD:**
- [x] 3 Dockerfiles da plataforma com HEALTHCHECK
- [x] Health endpoint confirmado para cada serviço

### Task 2.5: Consolidar layers e adicionar build flags
**Arquivos:** `api/Dockerfile`, `operators/Dockerfile`, `cloud/Dockerfile`
**Mudanças em cada Dockerfile:**
1. Consolidar `RUN apk add` + `adduser` em um único `RUN`
2. Adicionar `-trimpath -ldflags="-s -w"` ao `go build`
3. Adicionar OCI labels (`org.opencontainers.image.*`)
4. Unificar versão do Go builder (`golang:1.25.3-alpine3.21`)
**DoD:**
- [x] Zero Dockerfiles com split `RUN apk` + `RUN adduser`
- [x] Todos os Go builds usam `-trimpath -ldflags="-s -w"`
- [x] Todos os Dockerfiles da plataforma têm OCI labels
- [x] Mesma versão de Go builder em todos

### Task 2.6: Fix `.dockerignore`
**Arquivos:** `/.dockerignore`, novo `cloud/dashboard/.dockerignore`
**Mudanças:**
```gitignore
# Adicionar ao root .dockerignore:
infra/pulumi-infra/node_modules
infra/pulumi-platform/node_modules
infra/pulumi-control-plane/node_modules
**/dist/
**/build/
**/coverage/
**/*.log
**/.DS_Store

# Mudar de:
*.md
# Para:
**/*.md
!**/README.md
```
Criar `cloud/dashboard/.dockerignore`:
```gitignore
node_modules
dist
coverage
.env*
*.md
```
**DoD:**
- [x] Build context medido antes/depois (target: >50% redução)
- [x] `cloud/dashboard/.dockerignore` existe
- [x] Zero `node_modules` de Pulumi no context

### Task 2.7: Fix bugs nos Dockerfiles gerados (loopzeropacks)
**Arquivo:** `infra/images/packs-runner/loopzeropacks-generate`
**Bugs a corrigir:**
1. **Rust binary name** (line ~271): `$(basename $(pwd))` avaliado no gerador, não no build. Fix: usar `cargo metadata --format-version 1 | jq -r '.packages[0].name'` ou hardcode via build arg
2. **PHP composer**: substituir `curl | php` por multi-stage `FROM composer:2.7`
3. **Dashboard**: `npm install` → `npm ci`
4. **Next.js**: adicionar verificação de `output: 'standalone'` no validate
**DoD:**
- [x] Template Rust gera binary name correto
- [x] PHP usa composer image, não curl
- [x] Dashboard usa `npm ci`

### Task 2.8: Dashboard nginx non-root
**Arquivo:** `cloud/dashboard/Dockerfile`
**Mudança:**
```dockerfile
# Antes
FROM nginx:alpine
# Depois
FROM nginxinc/nginx-unprivileged:1.27-alpine@sha256:<digest>
COPY --from=builder /app/dist /usr/share/nginx/html
```
**DoD:**
- [x] Dashboard roda como non-root (UID 101)
- [x] Health check funciona na porta 8080 (default do unprivileged)

---

## Fase 3: Modernização (P2)

**Objetivo:** Alcançar o baseline esperado para PaaS 2026.
**Estimativa:** 1 sprint

### Task 3.1: SLSA Provenance Attestation
**Arquivos:** `.github/workflows/build-images.yml`, `api/internal/argo/buildkit.go`
**Mudança CI:**
```yaml
docker buildx build --provenance=true --sbom=true ...
```
**Mudança user builds:**
```go
// buildkit.go — adicionar ao buildctlArgs
"--opt", "attest:provenance=mode=max",
```
**DoD:**
- [x] `docker buildx imagetools inspect <image>` mostra provenance attestation
- [x] User images têm SLSA level 1 attestation

### Task 3.2: Reproducible Builds (SOURCE_DATE_EPOCH)
**Arquivos:** CI workflow, todos os Dockerfiles
**Mudança:**
```yaml
# CI: set epoch from git commit
- name: Set SOURCE_DATE_EPOCH
  run: echo "SOURCE_DATE_EPOCH=$(git log -1 --pretty=%ct)" >> $GITHUB_ENV
```
```dockerfile
ARG SOURCE_DATE_EPOCH
ENV SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH}
```
**DoD:**
- [x] Builds com mesmo commit produzem mesmo hash de layer
- [x] `SOURCE_DATE_EPOCH` propagado para todos os build stages

### Task 3.3: `docker buildx bake` para CI
**Novo arquivo:** `docker-bake.hcl`
**Mudança:**
```hcl
group "default" {
  targets = ["api", "operator", "cloud", "dashboard", "harbor-init"]
}

target "api" {
  dockerfile = "api/Dockerfile"
  tags = ["${REGISTRY}/loopzero-api:${TAG}"]
  cache-from = ["type=registry,ref=${REGISTRY}/loopzero-api:cache"]
  cache-to   = ["type=registry,ref=${REGISTRY}/loopzero-api:cache,mode=max"]
  platforms  = ["linux/amd64"]
}
// ... demais targets
```
**DoD:**
- [x] `docker-bake.hcl` no root do repo
- [x] CI usa `docker buildx bake --push` em vez de 5 build steps separados
- [x] Cache compartilhado entre targets funciona

### Task 3.4: Multi-platform builds (amd64 + arm64)
**Pré-requisito:** Task 3.3 (bake)
**Mudança:** Adicionar `platforms = ["linux/amd64", "linux/arm64"]` ao bake file
**Considerações:**
- Go cross-compile é nativo (CGO_ENABLED=0)
- Node/nginx são multi-arch no Docker Hub
- BuildKit QEMU emulation é lento (~3-5x) — considerar runners ARM64 nativos
**DoD:**
- [x] `docker manifest inspect <image>` mostra amd64 + arm64
- [x] Apple Silicon dev team pode rodar imagens nativamente

### Task 3.5: Semver release tags
**Mudança:** Criar workflow `release.yml` que:
1. Lê versão do `package.json` ou `VERSION` file
2. Cria git tag `v1.2.3`
3. Builda com tags `:v1.2.3`, `:1.2`, `:1` (semver convention)
4. Cria GitHub Release com CHANGELOG excerpt
**DoD:**
- [x] `loopzero --version` retorna semver
- [x] Container images taggeadas com semver
- [x] GitHub Releases criados automaticamente

### Task 3.6: Atualizar ADR Kaniko → BuildKit
**Arquivo:** `docs/meetings/adr/0003-kaniko-over-buildkit.md`
**Mudança:** Atualizar status para `Superseded` e documentar a decisão real (BuildKit)
com justificativa (rootless, melhor cache, industry standard desde 2024).
**DoD:**
- [x] ADR reflete a arquitetura real
- [x] Referência cruzada com o `buildkit.go`

### Task 3.7: Atualizar `minio/mc` nos CWTs
**Arquivo:** `infra/helm/argo-workflows/values.yaml`, CWT templates
**Mudança:**
- Atualizar de `RELEASE.2024-01-16` para release 2025 recente
- Pin por digest
**DoD:**
- [x] `minio/mc` com release < 6 meses
- [x] Digest pinned no values.yaml

### Task 3.8: Trivy `--ignore-unfixed` e `.trivyignore`
**Arquivos:** `.github/workflows/build-images.yml`, novo `.trivyignore`
**Mudança:**
```yaml
with:
  ignore-unfixed: true
  trivyignores: .trivyignore
```
**DoD:**
- [x] Pipeline não bloqueia em vulnerabilidades sem fix disponível
- [x] `.trivyignore` existe com formato documentado

---

## Critérios de Aceite Globais

### Fase 1 (P0)
- [x] Zero binários baixados em runtime sem checksum
- [x] 100% das base images pinned por digest
- [x] CI signing é keyless (Sigstore)
- [x] Todas as 5 imagens da plataforma assinadas + escaneadas

### Fase 2 (P1)
- [x] Build time médio reduzido em >30% (medir com `time docker buildx build`)
- [x] 100% dos containers da plataforma rodam non-root
- [x] 100% dos Dockerfiles gerados produzem containers non-root
- [x] Build context < 10MB para cada imagem (medir com `docker buildx build --progress=plain`)

### Fase 3 (P2)
- [x] SLSA provenance em todas as imagens
- [x] Multi-platform (amd64 + arm64)
- [x] Semver release tags
- [x] ADR atualizado

---

## Ordem de Execução

```
Fase 1 (P0 Security) ─── 1-2 dias
  ├── Task 1.1: mc checksum         ← mais rápido, maior risco
  ├── Task 1.2: cosign pre-bake     ← depende de nova imagem
  ├── Task 1.3: digest pin          ← bulk edit, pode ser paralelo
  ├── Task 1.4: keyless signing     ← CI change, precisa de teste
  └── Task 1.5: harbor-init sign    ← CI matrix edit
                    │
Fase 2 (P1 Efficiency) ─── 2-3 dias
  ├── Task 2.1: cache mounts (platform)    ← maior impacto em tempo
  ├── Task 2.2: cache mounts (loopzeropacks)   ← impacto para users
  ├── Task 2.3: non-root USER              ← security + correctness
  ├── Task 2.4: HEALTHCHECK               ← quick win
  ├── Task 2.5: consolidate layers         ← quick win
  ├── Task 2.6: .dockerignore             ← quick win
  ├── Task 2.7: loopzeropacks bugs            ← correctness
  └── Task 2.8: dashboard non-root        ← security
                    │
Fase 3 (P2 Modernization) ─── 1 sprint
  ├── Task 3.1: SLSA provenance
  ├── Task 3.2: reproducible builds
  ├── Task 3.3: docker bake
  ├── Task 3.4: multi-platform
  ├── Task 3.5: semver tags
  ├── Task 3.6: ADR update
  ├── Task 3.7: minio/mc update
  └── Task 3.8: trivy improvements
```

---

## Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|--------------|---------|-----------|
| Digest pin quebra CI quando upstream publica nova versão | Alta | Baixo | Script de atualização automatizado + Renovate/Dependabot |
| Keyless signing requer OIDC no cluster (user builds) | Média | Médio | Manter key-based para user builds, keyless só no CI |
| Multi-platform builds dobram tempo de CI | Alta | Médio | ARM64 native runners + cache compartilhado |
| `--mount=type=cache` não funciona em todos os runners | Baixa | Médio | Testar em GHA runners primeiro |
| AppProject empty destinations bloqueia first deploy | Média | Alto | EnvironmentController deve popular namespaces antes do ArgoCD sync |
