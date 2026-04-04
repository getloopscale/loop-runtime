# Design: Content Hash — Git-free Deploy

**Data:** 2026-04-04
**Status:** Design
**Classificacao:** T2 — afeta CLI, API, source upload, idempotencia, image tagging
**Objetivo:** `loopzero deploy` funciona sem Git. Projetos com Git continuam usando commit SHA.

---

## Problema

O LoopZero exige Git para deploy:
- `loopzero init` rejeita com `NOT_GIT_REPO`
- `loopzero deploy` rejeita com `NOT_GIT_REPO`
- Source bundle usa `git bundle create`
- Versioning usa `git rev-parse HEAD`

Um desenvolvedor que baixou um ZIP, editou um template, ou usa outro VCS nao consegue deployar.

---

## Design

### Principio: Git quando disponivel, content hash quando nao

| Situacao | Identificador | Bundle format |
|----------|--------------|---------------|
| Projeto com `.git` + commits | Git commit SHA (40 chars) | `git bundle` (atual) |
| Projeto sem `.git` | Content hash SHA256 (64 chars) | `tar.gz` |

### Content Hash

SHA256 de todos os arquivos do projeto (exceto `.git/`, `node_modules/`, patterns do `.gitignore`):

```
SHA256(sorted_file_paths + file_contents + file_modes)
```

Deterministico: mesmo conteudo = mesmo hash, independente de ordem de FS.

### Prefixo para distinguir

- Git SHA: `a1b2c3d4e5f6...` (40 hex chars, sem prefixo)
- Content hash: `ch-a1b2c3d4e5f6...` (prefixo `ch-` + 64 hex chars)

O prefixo evita colisao e permite que qualquer componente saiba a origem.

---

## Pontos de mudanca

### 1. CLI: `cli/src/adapters/git.ts`

**Atual:**
```typescript
export async function getHeadCommit(): Promise<string> {
  return execGit(['rev-parse', 'HEAD']); // FAILS without .git
}

export async function createGitBundle(outputPath: string): Promise<void> {
  await execGit(['bundle', 'create', outputPath, 'HEAD']);
}
```

**Novo:**
```typescript
export async function getSourceIdentifier(): Promise<string> {
  if (await hasGitRepo()) {
    return execGit(['rev-parse', 'HEAD']);
  }
  return 'ch-' + await computeContentHash(process.cwd());
}

export async function createSourceBundle(outputPath: string): Promise<void> {
  if (await hasGitRepo()) {
    await execGit(['bundle', 'create', outputPath, 'HEAD']);
  } else {
    await createTarBundle(outputPath); // tar.gz excluding node_modules, .gitignore patterns
  }
}
```

**Nova funcao** `computeContentHash(dir)`:
```typescript
import { createHash } from 'crypto';
import { glob } from 'glob';

async function computeContentHash(dir: string): Promise<string> {
  const hash = createHash('sha256');
  const ignorePatterns = loadIgnorePatterns(dir); // .gitignore + .loopzeroignore + defaults
  const files = await glob('**/*', { cwd: dir, ignore: ignorePatterns, dot: true, nodir: true });
  files.sort(); // deterministic order
  for (const file of files) {
    hash.update(file); // path
    hash.update(await readFile(join(dir, file))); // content
  }
  return hash.digest('hex');
}
```

**Nova funcao** `createTarBundle(outputPath)`:
```typescript
import tar from 'tar';

async function createTarBundle(outputPath: string): Promise<void> {
  const ignorePatterns = loadIgnorePatterns(process.cwd());
  await tar.create(
    { gzip: true, file: outputPath, cwd: process.cwd(), filter: (path) => !shouldIgnore(path, ignorePatterns) },
    ['.']
  );
}
```

### 2. CLI: `cli/src/commands/deploy.ts`

**Atual:** Chama `getHeadCommit()` que falha sem git.

**Novo:** Chama `getSourceIdentifier()` que funciona sempre.

**Idempotency key:** Sem mudanca — ja usa SHA256 do input, funciona com qualquer hash.

### 3. CLI: `cli/src/commands/init.ts`

**Atual:** Rejeita sem `.git`.

**Novo:** Warning ao inves de erro:
```typescript
if (!await hasGitRepo()) {
  warnings.push('No git repository detected. Deploy will use content hash instead of commit SHA.');
}
```

### 4. API: `api/internal/routes/source.go`

**Atual:** Recebe git bundle, chama `store.PushBundle()`.

**Novo:** Detecta formato pelo Content-Type ou magic bytes:
- Git bundle: `# v2 git bundle\n` (header)
- Tar.gz: `\x1f\x8b` (gzip magic bytes)

```go
func detectBundleFormat(r io.Reader) (string, error) {
    header := make([]byte, 2)
    if _, err := io.ReadFull(r, header); err != nil { return "", err }
    if header[0] == 0x1f && header[1] == 0x8b { return "tar", nil }
    return "git", nil
}
```

### 5. API: `api/internal/source/gitea_store.go`

**Atual:** `PushBundle()` usa `git pull` + `git rev-parse HEAD`.

**Novo:** Dois code paths:

```go
func (s *GiteaStore) PushBundle(ctx context.Context, tenantID, project string, bundle io.Reader) (string, error) {
    format := detectBundleFormat(bundle)
    switch format {
    case "git":
        return s.pushGitBundle(ctx, tenantID, project, bundle)  // existing code
    case "tar":
        return s.pushTarBundle(ctx, tenantID, project, bundle)  // new code
    }
}

func (s *GiteaStore) pushTarBundle(ctx context.Context, tenantID, project string, bundle io.Reader) (string, error) {
    // 1. Extract tar.gz to temp dir
    // 2. Compute content hash from extracted files
    // 3. git init + git add + git commit (create git repo for Gitea)
    // 4. git push to Gitea
    // 5. Return "ch-" + contentHash
}
```

**Nota:** O Gitea repo e o gitops flow continuam usando Git internamente.
A diferenca e que o **source code do usuario** pode nao ter Git.

### 6. Image tagging: `pkg/naming/naming.go`

**Atual:** `{registry}/{tenant}/{project}-{app}:{commitSHA}`

**Novo:** Sem mudanca. Content hash funciona como tag:
- Git: `registry/tenant/project-app:a1b2c3d4`
- Content: `registry/tenant/project-app:ch-a1b2c3d4e5f6`

Docker image tags aceitam qualquer string alphanumeric + hyphens.

---

## O que NAO muda

- **Gitea repos:** Sempre usam Git internamente (API cria commit no push)
- **ArgoCD:** Continua lendo do Git (Gitea)
- **Argo Workflows:** Recebe source do Gitea (que e Git)
- **Rollback:** Funciona — usa release ID, nao commit SHA
- **Idempotencia:** Funciona — usa hash do input (commit ou content hash)
- **Build list/inspect:** Mostra o identificador (commit ou content hash)
- **GitOps push:** Continua com Git (manifests no Gitea)

---

## Ignore patterns

Para content hash e tar bundle, excluir:

```
node_modules/
.git/
.DS_Store
*.log
dist/
build/
.next/
__pycache__/
*.pyc
.env
.env.*
```

Mais patterns do `.gitignore` (se existir) e `.loopzeroignore` (novo, opcional).

---

## Migration

- **Zero breaking changes.** Projetos com Git continuam identicos.
- **Novo campo optional:** `builds.source_type` (`git` | `content`) — para UI/API saber qual tipo.
- Campo `commit_sha` renomeado conceptualmente para `source_id` mas mantido como `commit_sha` no DB por backward compat (ou aliased na API response).

---

## Sequencia de implementacao

### Phase 1: CLI — Content hash + tar bundle (2h)

**Arquivos:**
- `cli/src/adapters/git.ts` — `hasGitRepo()`, `getSourceIdentifier()`, `computeContentHash()`, `createTarBundle()`
- `cli/src/adapters/source-upload.ts` — usar `createSourceBundle()` ao inves de `createGitBundle()`
- `cli/src/commands/deploy.ts` — usar `getSourceIdentifier()` ao inves de `getHeadCommit()`
- `cli/src/commands/init.ts` — warning ao inves de erro para projetos sem `.git`

**DoDs:**
- [ ] `hasGitRepo()` retorna `false` em diretorio sem `.git` e `true` com `.git`
- [ ] `computeContentHash('/tmp/test-dir')` retorna SHA256 hex de 64 chars
- [ ] Mesmo diretorio com mesmos arquivos = mesmo hash (deterministico)
- [ ] Diretorio com arquivo modificado = hash diferente
- [ ] `node_modules/`, `.env`, `dist/`, `.git/` sao excluidos do hash
- [ ] `.gitignore` patterns sao respeitados quando arquivo existe
- [ ] `createTarBundle()` cria `.tar.gz` valido que exclui patterns ignorados
- [ ] `tar tzf bundle.tar.gz` nao contem `node_modules/` nem `.git/`
- [ ] `getSourceIdentifier()` retorna commit SHA (40 chars) com `.git`
- [ ] `getSourceIdentifier()` retorna `ch-{sha256}` (67 chars) sem `.git`
- [ ] `loopzero init --yes --name test --json` em dir sem `.git`: exit 0, JSON contem `warning` sobre content hash
- [ ] `loopzero init --yes --name test --json` em dir com `.git`: exit 0, sem warning (comportamento atual)
- [ ] `loopzero deploy --json --no-wait` em dir sem `.git`: exit 0, `build_id` retornado
- [ ] `loopzero deploy --json --no-wait` em dir com `.git`: exit 0 (comportamento atual inalterado)
- [ ] Idempotency key: dois deploys consecutivos sem mudanca de arquivos = mesmo key
- [ ] Idempotency key: deploy apos modificar arquivo = key diferente
- [ ] `npm test` (CLI): todos os 498+ testes passam (nenhuma regressao)
- [ ] Zero termos internos (`content hash`, `tar`, `bundle format`) em output do usuario — apenas `"version"` ou `"source"`

### Phase 2: API — Aceitar tar bundle + content hash (1h)

**Arquivos:**
- `api/internal/routes/source.go` — detectar formato do bundle (git vs tar)
- `api/internal/source/gitea_store.go` — `pushTarBundle()` novo code path
- `api/internal/source/store.go` — interface inalterada (retorna string)

**DoDs:**
- [ ] Upload de git bundle (Content-Type ou magic bytes `# v2 git bundle`): funciona como antes
- [ ] Upload de tar.gz bundle (magic bytes `\x1f\x8b`): aceito, extraido, commitado no Gitea
- [ ] `PushBundle()` com tar.gz retorna `ch-{sha256}` (content hash com prefixo)
- [ ] `PushBundle()` com git bundle retorna commit SHA (40 chars, sem prefixo — atual)
- [ ] Gitea repo contem os arquivos do tar.gz apos push (verificavel via Gitea API)
- [ ] Content hash no response do upload: `{"commit_sha": "ch-a1b2c3..."}`
- [ ] `go test ./...` (API): todos os testes passam
- [ ] Build criado com content hash: `builds.commit_sha = "ch-a1b2c3..."` no DB
- [ ] Image tag com content hash: `registry/tenant/project-app:ch-a1b2c3d4e5` (truncado 12 chars)

### Phase 3: E2E — Deploy sem Git na DigitalOcean (1h)

**Teste manual no ambiente DO dev:**

```bash
# 1. Criar projeto SEM git
mkdir /tmp/no-git-test && cd /tmp/no-git-test
cat > index.js << 'EOF'
const http = require('http');
http.createServer((req, res) => {
  if (req.url === '/health') { res.writeHead(200); res.end('ok'); return; }
  res.writeHead(200); res.end('Hello without Git!');
}).listen(process.env.PORT || 3000);
EOF
echo '{"name":"no-git","version":"1.0.0","scripts":{"start":"node index.js"}}' > package.json
npm install --package-lock-only

# 2. Init (sem .git)
loopzero init --yes --name no-git-test --json
# Esperado: exit 0, warning sobre content hash

# 3. Deploy
loopzero deploy --json --no-wait
# Esperado: exit 0, build_id retornado

# 4. Aguardar build
loopzero status --json
# Esperado: SUCCEEDED

# 5. Prova de Vida
kubectl get pods -n loopzero-tn-loopzero-production -l app=no-git-test
# Esperado: 1/1 Running
kubectl exec <pod> -- wget -qO- http://localhost:3000/health
# Esperado: "ok"
```

**DoDs:**
- [ ] `loopzero init` em dir sem `.git`: exit 0 com warning
- [ ] `loopzero deploy` em dir sem `.git`: exit 0, build submitted
- [ ] Build completa com status SUCCEEDED
- [ ] `loopzero status --json` mostra `commit_sha` com prefixo `ch-`
- [ ] `loopzero build inspect <id> --json` mostra `commit_sha` com prefixo `ch-`
- [ ] Pod Running no runtime cluster (Prova de Vida)
- [ ] Health endpoint responde `ok`
- [ ] `loopzero logs --json` retorna linhas de log do build
- [ ] Deploy idempotente: segundo `loopzero deploy` sem mudancas = mesmo build (idempotency key igual)
- [ ] Deploy apos modificar arquivo: novo build com hash diferente
- [ ] Projeto com `.git` continua funcionando normalmente (nao-regressao)

### Phase 4: Polish (30min)

**Arquivos:**
- `cli/src/adapters/git.ts` — `.loopzeroignore` support
- `cli/src/commands/deploy.ts` — mensagem informativa no modo human
- `cli/src/output/` — `source_type` field no JSON output

**DoDs:**
- [ ] `.loopzeroignore` no projeto: patterns adicionais respeitados no hash e no tar
- [ ] Modo human: `"Packaging source (content hash — no git detected)"` ao inves de silencio
- [ ] Modo human com git: `"Packaging source (commit abc1234)"` (atual, inalterado)
- [ ] `--json` deploy response inclui `"source_type": "content"` ou `"source_type": "git"`
- [ ] `--json` build inspect inclui `"source_type"` field
- [ ] `loopzero validate --json` funciona sem `.git` (exit 0)

---

## Checklist pre-merge

- [ ] `npm test` (CLI): 498+ testes passam
- [ ] `go test ./...` (API): todos passam
- [ ] `task lint` passa
- [ ] Zero termos internos em output CLI
- [ ] Deploy E2E com `.git` funciona (nao-regressao)
- [ ] Deploy E2E sem `.git` funciona (novo)
- [ ] Rollback funciona apos deploy sem `.git`
- [ ] Idempotencia: mesmo conteudo = mesmo build
- [ ] Content hash deterministico: 3 execucoes consecutivas = mesmo hash

**Tempo total: ~4-5h**

---

## Riscos

| Risco | Mitigacao |
|-------|----------|
| Content hash nao deterministico (file permissions, timestamps) | Usar apenas path + content, ignorar metadata |
| Tar bundle grande (sem .gitignore) | Respeitar .gitignore + .loopzeroignore + defaults |
| Dois code paths para manter | Abstrair atras de `SourceBundler` interface |
| Image tag muito longo (`ch-` + 64 chars) | Truncar para 12 chars como Git SHA short |
