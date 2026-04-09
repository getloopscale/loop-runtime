# Launch Readiness — Resolver Todos os Gaps

> Status real em 2026-04-09. O sistema funciona (git push → deploy → live).
> Este plano resolve tudo que falta para launch público.

---

## Prioridade 0: Fixes Críticos (bloqueiam UX básica)

### P0-1: GitHub commit status 403

**Problema:** GitHub App não tem permissão `statuses:write`. Commit status retorna 403.
**Impacto:** User não vê ✓/✗ no GitHub após deploy.

**Fix:**
1. GitHub Dashboard → Settings → Developer Settings → GitHub Apps → LoopZero Platform
2. Permissions → Repository → Commit statuses → **Read and write** (atualmente só read?)
3. Save → Users will be prompted to approve new permissions

**DoD:**
- [ ] `SetCommitStatus()` retorna 201
- [ ] Push → commit mostra "loopzero/deploy — pending" → "success" após build

**Esforço:** 5 minutos (config change)

---

### P0-2: Init container migration crashes

**Problema:** `migrate` tool crash quando `schema_migrations.version` aponta para migration sem `.down.sql`. O recovery script tenta `force` que precisa do down file.
**Impacto:** Cada deploy da API precisa de fix manual (`UPDATE schema_migrations SET version = 25`).

**Fix:** Mudar o init container script para não usar `force` (que requer down file). Usar `version` check + skip se "no change":

```bash
# Current (broken):
DIRTY=$(migrate ... version 2>&1 || true)
if echo "$DIRTY" | grep -q "dirty"; then
  VER=$(echo "$DIRTY" | grep -oE "[0-9]+")
  migrate ... force "$VER"  # ← CRASH: needs .down.sql
fi
migrate ... up

# Fix:
VER=$(migrate -path=/migrations -database "$DATABASE_URL" version 2>&1 || true)
if echo "$VER" | grep -q "dirty"; then
  DIRTY_VER=$(echo "$VER" | grep -oE "[0-9]+")
  # Force only clears dirty flag — no rollback needed
  migrate -path=/migrations -database "$DATABASE_URL" force "$DIRTY_VER" 2>&1 || true
fi
# Always try up — "no change" if already current
migrate -path=/migrations -database "$DATABASE_URL" up 2>&1 || {
  # If up fails, check if it's a "no migration found" error (expected for manual migrations)
  echo "migrate up returned non-zero — checking if version is current"
  migrate -path=/migrations -database "$DATABASE_URL" version 2>&1
}
```

**Alternativa mais simples:** Já temos os `.down.sql` files (vazios). O problema real é que o init container image é `migrate/migrate:v4.18.1` que NÃO contém nossos migration files — eles estão no API image. O init container precisa montar os files do API image.

**Fix real:** Mudar o init container para usar a API image com um entrypoint diferente:

```yaml
initContainers:
  - name: db-migrate
    image: registry.digitalocean.com/loopzero-infra-registry/loopzero-api:dev  # SAME image as app
    command: ["sh", "-c"]
    args: ["migrate -path=/migrations -database $DATABASE_URL up"]
```

**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts` ou `infra/tilt/api-deployment.yaml`

**DoD:**
- [ ] Init container usa API image (tem migration files)
- [ ] `kubectl rollout restart` funciona sem manual psql fix
- [ ] Migration 26+27 aplicam automaticamente
- [ ] Testado: destroy + recreate → migrations aplicam do zero

**Esforço:** 30 minutos

---

### P0-3: CLI `loopzero connect` command

**Problema:** User precisa chamar API diretamente via curl para linkar repo ao projeto.
**Impacto:** Sem isso, auto-deploy não pode ser configurado pelo user.

**Arquivo:** `cli/src/commands/connect.ts`

**Flow:**
```
$ loopzero connect

  Connecting to GitHub...
  Repository detected from git remote: getloopscale/my-app

  ✓ Connected getloopscale/my-app → project my-app
    Auto-deploy: enabled (push to main → production)

  Install the LoopZero GitHub App if not already:
    https://github.com/apps/loopzero-platform
```

**Implementação:**
```typescript
class ConnectCommand {
  async execute(opts) {
    // 1. Detect repo from .git/config
    const remote = getGitRemote(); // parse "origin" URL
    // 2. Get installation_id (user must have installed the app)
    // 3. POST /api/v1/projects/{name}/connect
    // 4. Show result
  }
}
```

**Challenge:** Como obter `installation_id`? O user pode não saber. Opções:
- A: CLI pergunta (ruim UX)
- B: API lista installations do user via GitHub API e encontra a correta
- C: CLI abre browser para instalar o app, que redireciona com installation_id

**Solução B é a melhor.** A API usa o token do user (JWT) para buscar installations via GitHub API.

Novo endpoint: `GET /api/v1/github/installations` — lista installations acessíveis ao tenant.

**DoD:**
- [ ] `loopzero connect` detecta repo do git remote
- [ ] Conecta ao projeto automaticamente
- [ ] `loopzero disconnect` remove o link
- [ ] `--json` output
- [ ] Teste unitário

**Esforço:** 1 dia

---

## Prioridade 1: Preview Environments Foundation

### P1-1: PR comment com preview URL

**Problema:** Quando PR é aberto, LoopZero deveria postar comment com link.
**Dependência:** P0-1 (commit status fix) — mesma permission issue.

**Implementação:** No webhook `pull_request` handler (já recebe eventos):
1. PR opened/synchronize → download source da PR branch
2. Build com environment = `preview-pr-{number}`
3. Deploy no namespace preview
4. Comment no PR com URL

**Mas** isso requer o PreviewEnvironment controller (Sprint 5). Para agora, podemos fazer o mínimo:
1. PR opened → comment "LoopZero received this PR. Preview environments coming soon."

**DoD (mínimo):**
- [ ] PR opened → LoopZero comenta no PR
- [ ] Comment inclui link para status page

**Esforço:** 2 horas (comment apenas, sem deploy)

---

### P1-2: Build speed optimization

**Problema:** Build leva ~4min. Vercel faz ~30s.
**Causa:** BuildKit cold start + image pull + cache miss.

**Quick wins:**
1. **Pre-pull base images** no build cluster (python:3.12-alpine, node:20-alpine)
2. **BuildKit cache warming** — manter daemon running (não start/stop por build)
3. **Smaller base images** — alpine-only
4. **Layer cache persistence** — registry cache já funciona, verificar hit rate

**DoD:**
- [ ] Build time < 2min para rebuilds (cache hit)
- [ ] First build < 4min (cache miss)
- [ ] Cache hit rate > 80% medido via métricas

**Esforço:** 1 dia

---

## Prioridade 2: Polish para Launch

### P2-1: Pulumi dual TLS solver deploy

**Problema:** ClusterIssuer dual solver (DNS-01 + HTTP-01) commitado mas não aplicado.
**Fix:** `pulumi up` nos stacks dev/prod.

**DoD:**
- [ ] `kubectl get clusterissuer -o yaml` mostra 2 solvers em todos os clusters
- [ ] Certificados existentes continuam válidos

**Esforço:** 15 minutos

---

### P2-2: CF_ZONE_MANAGEMENT persistido no Pulumi

**Problema:** `CF_ZONE_MANAGEMENT=true` setado via `kubectl set env` — se perde em redeploy.
**Fix:** Adicionar ao Pulumi deployment spec.

**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts`

**DoD:**
- [ ] Env var no Pulumi stack
- [ ] `pulumi preview` mostra a var
- [ ] Redeploy não perde a config

**Esforço:** 15 minutos

---

### P2-3: GitHub App envFrom no Pulumi

**Problema:** `loopzero-github-app` secret montado via `kubectl patch` — se perde em redeploy.
**Fix:** Adicionar ao Pulumi deployment spec.

**DoD:**
- [ ] envFrom no Pulumi
- [ ] Redeploy mantém GitHub webhook funcional

**Esforço:** 15 minutos

---

### P2-4: loopzero-stacks CI validation

**Problema:** Templates corrigidos mas sem CI que previna regressão.
**Arquivo:** `.github/workflows/validate-templates.yml` no loopzero-stacks repo

**DoD:**
- [ ] GitHub Action roda `loopzero validate` em cada template
- [ ] PR com template inválido é bloqueado

**Esforço:** 1 hora

---

### P2-5: `created_by` tipado para auto-deploys

**Problema:** Build registra `created_by: "github-push:userloopzerodev"` como string livre.
**Fix:** Normalizar para user ID do DB (lookup por GitHub login).

**DoD:**
- [ ] `created_by` é user ID do DB quando push vem de user conhecido
- [ ] Fallback para `github-push:{login}` se user não está no DB

**Esforço:** 2 horas

---

### P2-6: DNS import review flow no CLI

**Problema:** `loopzero domain add` importa DNS records mas não mostra para o user revisar.
**Fix:** Já implementado no response — falta formatar no CLI.

O CLI `DomainAddCommand` já mostra os records importados na tabela. O que falta:
- Mensagem sugerindo `loopzero domain dns add` para records faltantes
- `loopzero domain dns` command já existe

**DoD:**
- [ ] Verificar que o output atual é claro
- [ ] Adicionar hint se records < 3 (pode ter faltado email)

**Esforço:** 1 hora

---

## Prioridade 3: Novas Linguagens

### P3-1: loopzero-packs Ruby Dockerfile generation

**Arquivo:** `infra/images/packs-runner/loopzeropacks-generate`

**Adicionar:**
```bash
elif [ -f "$FULL_PATH/Gemfile" ]; then
  FRAMEWORK="ruby"
```

**DoD:**
- [ ] Detecta Ruby (Gemfile)
- [ ] Gera Dockerfile funcional (Sinatra + Rails)
- [ ] Teste: Gemfile → Dockerfile válido

**Esforço:** 2 horas

### P3-2: loopzero-packs Java Dockerfile generation

**Detecta:** pom.xml ou build.gradle
**DoD:** Spring Boot app funcional

**Esforço:** 2 horas

### P3-3: loopzero-packs PHP Dockerfile generation

**Detecta:** composer.json
**DoD:** Slim/Laravel app funcional

**Esforço:** 2 horas

### P3-4: loopzero-packs Rust Dockerfile generation

**Detecta:** Cargo.toml
**DoD:** Axum app funcional (multi-stage build)

**Esforço:** 2 horas

### P3-5: Language strategy scripts (validate/install/build/scan)

**Arquivo:** `api/internal/argo/languages.go`

Novas strategies: Ruby, Java, PHP, Rust (validate workspace + install deps + build + security scan)

**DoD:**
- [ ] Cada linguagem tem strategy completa
- [ ] Build pipeline funciona end-to-end
- [ ] Testes unitários

**Esforço:** 1 dia

---

## Prioridade 4: Preview Environments (Sprint 5)

### P4-1: PreviewEnvironment Controller

**O feature que justifica o Pro plan ($29/mo).**

Precisa de:
- Controller que reconcilia PreviewEnvironment CRs
- Namespace creation no runtime cluster
- Build from PR branch
- IngressRoute para preview URL
- TTL-based cleanup
- PR comment com URL

**Prerequisitos:** P0-1 (commit status), P0-3 (loopzero connect), P1-1 (PR comment)

**Plano detalhado:** `docs/plans/phase-2-differentiators/01-preview-environments.md`

**Esforço:** 2-3 semanas

---

## Sequenciamento

```
Semana 1 (prioridade 0 — fixes críticos):
├── P0-1: Fix commit status permission (5min)
├── P0-2: Fix init container migration (30min)
└── P0-3: CLI loopzero connect (1 dia)

Semana 1-2 (prioridade 1-2 — polish):
├── P1-2: Build speed optimization (1 dia)
├── P2-1: Pulumi dual TLS solver (15min)
├── P2-2: CF_ZONE_MANAGEMENT no Pulumi (15min)
├── P2-3: GitHub App envFrom no Pulumi (15min)
├── P2-4: loopzero-stacks CI (1h)
├── P2-5: created_by tipado (2h)
└── P2-6: DNS review hint (1h)

Semana 2-3 (prioridade 3 — linguagens):
├── P3-1: Ruby Dockerfile (2h)
├── P3-2: Java Dockerfile (2h)
├── P3-3: PHP Dockerfile (2h)
├── P3-4: Rust Dockerfile (2h)
└── P3-5: Language strategies (1 dia)

Semana 3-5 (prioridade 4 — preview envs):
└── P4-1: PreviewEnvironment Controller (2-3 semanas)
```

## Resumo

| Prioridade | Tasks | Esforço total | Bloqueador? |
|-----------|-------|---------------|-------------|
| **P0** | 3 | 1.5 dias | Sim — UX básica |
| **P1** | 2 | 1.5 dias | Não — polish |
| **P2** | 6 | 0.5 dia | Não — infra + polish |
| **P3** | 5 | 2 dias | Não — novas linguagens |
| **P4** | 1 | 2-3 semanas | Não — killer feature |
| **Total** | **17 tasks** | **~5-6 semanas** | |

**Para launch mínimo (1 semana):** P0 + P2 = sistema robusto com auto-deploy funcional.
**Para launch competitivo (3 semanas):** + P1 + P3 = velocidade + todas as linguagens.
**Para Vercel parity (6 semanas):** + P4 = preview environments.
