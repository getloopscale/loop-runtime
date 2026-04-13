# API Shell Hardening — Eliminar Scripts Inline Frageis

> **Status:** PLANNED
> **Classificacao:** T1 (refactor local, sem mudanca de contratos)
> **Data:** 2026-04-13
> **Autor:** paulohenriquevn

---

## Problema

O pacote `api/` gera shell scripts via `fmt.Sprintf` e duplica codigo de execucao git em 3 arquivos.
Isso cria 6 gaps concretos:

| # | Gap | Arquivo(s) | Severidade |
|---|-----|-----------|------------|
| G1 | Shell-as-string-template (TOML+shell) | `argo/buildkit.go:206-253` | Media |
| G2 | Shell-as-string-template (420 linhas) | `argo/scripts.go` (15 funcoes) | Media |
| G3 | DRY: gitCmd/gitOutput/sanitize duplicado | `source/gitea_store.go` + `gitops/gitea_pusher.go` | Baixa |
| G4 | DRY: ensureRepo triplicado | `source/gitea_store.go` + `gitops/gitea_pusher.go` + `gitops/gogit_pusher.go` | Baixa |
| G5 | autodeploy.go: exec.Command sem context, tar extraction insegura | `github/autodeploy.go:217-319` | Alta |
| G6 | Migracao go-git incompleta | `source/gitea_store.go` (CLI) vs `gitops/gogit_pusher.go` (go-git) | Baixa |

### G5 e critico

`tarballToGitBundle()` faz extracao de tar.gz **sem nenhuma protecao**:
- Sem validacao de symlinks (exfiltracao de credenciais)
- Sem validacao de path traversal (`../../../etc/passwd`)
- Sem limite de tamanho descomprimido (zip bomb)
- Erros silenciados: `_ = os.MkdirAll`, `_, _ = io.Copy`, `continue`

Ja existe `source/safe_tar.go` com TODAS essas protecoes. O codigo do autodeploy simplesmente nao o usa.

---

## Solucao

Dois novos pacotes internos + migracao dos consumidores:

```
api/internal/
  gitclient/          <- NOVO: git runner compartilhado
    runner.go         <- GitRunner (exec.CommandContext + sanitize)
    gitea.go          <- GiteaRepoManager (ensureRepo + repoURL)
    runner_test.go
    gitea_test.go
  shellgen/           <- NOVO: script builder type-safe
    builder.go        <- ScriptBuilder (fluent API)
    quote.go          <- ShellQuote (POSIX-safe quoting)
    builder_test.go
    quote_test.go
  archive/            <- NOVO: safe_tar movido de source/ para ser importavel
    tar.go            <- SafeExtractTarGz (com opcao StripComponents)
    tar_test.go
```

---

## Fases e Microtasks

### Fase 1 — Extrair `internal/gitclient` (DRY: git runner compartilhado)

**Pode rodar em paralelo com Fase 2.**

#### Task 1.1 — Criar `api/internal/gitclient/runner.go`

**O que:** Extrair o padrao duplicado de `gitCmd`, `gitOutput`, `gitCmdWithAuth`, `sanitizeOutput`, `sanitizeArgs`.

**Arquivos fonte (duplicacao):**
- `api/internal/source/gitea_store.go` linhas 371-436
- `api/internal/gitops/gitea_pusher.go` linhas 380-441

**API:**
```go
type GitRunner struct {
    token   string
    extraEnv []string  // e.g., GIT_AUTHOR_NAME
}

func NewGitRunner(token string, opts ...RunnerOption) *GitRunner
func (r *GitRunner) Run(ctx context.Context, dir string, args ...string) error
func (r *GitRunner) Output(ctx context.Context, dir string, args ...string) (string, error)
func (r *GitRunner) RunWithAuth(ctx context.Context, dir string, gitArgs ...string) error
```

**Criterios de aceite:**
- [ ] `GitRunner` encapsula exec.CommandContext + GIT_TERMINAL_PROMPT=0 + token sanitization
- [ ] Zero duplicacao de gitCmd/gitOutput/sanitize entre source e gitops
- [ ] Testes: Run com args, Output captura stdout, sanitize remove token de output e args
- [ ] Token vazio = operacoes locais (sem auth header)

**DoD:** `go test ./internal/gitclient/...` passa.

---

#### Task 1.2 — Extrair `ensureRepo` para `api/internal/gitclient/gitea.go`

**O que:** Unificar `ensureRepo` triplicado.

**Arquivos fonte:**
- `api/internal/source/gitea_store.go` linhas 313-362
- `api/internal/gitops/gitea_pusher.go` linhas 321-369
- `api/internal/gitops/gogit_pusher.go` linhas 435-483

**API:**
```go
type GiteaRepoManager struct {
    baseURL string
    token   string
    orgName string
    client  *http.Client
}

func NewGiteaRepoManager(baseURL, token, orgName string, client *http.Client) *GiteaRepoManager
func (m *GiteaRepoManager) EnsureRepo(ctx context.Context, repoName string, autoInit bool) error
func (m *GiteaRepoManager) RepoURL(repoName string) string
```

**Criterios de aceite:**
- [ ] Uma unica implementacao com parametro `autoInit` (source usa false, gitops usa true)
- [ ] Injetado em GiteaSourceStore, GiteaGitOpsPusher, GoGitGitOpsPusher
- [ ] Teste com httptest: repo existe (200), repo nao existe (404->201), erro (500), conflict (409)
- [ ] Nenhum codigo de ensureRepo permanece nos 3 arquivos consumidores

**DoD:** `go test ./internal/gitclient/...` passa.

---

#### Task 1.3 — Migrar consumers para GitRunner + GiteaRepoManager

**O que:** Rewire os 3 arquivos.

**Arquivos a modificar:**
- `api/internal/source/gitea_store.go`: remover metodos privados, injetar dependencias
- `api/internal/gitops/gitea_pusher.go`: idem
- `api/internal/gitops/gogit_pusher.go`: remover ensureRepo, usar GiteaRepoManager

**Criterios de aceite:**
- [ ] Construtores atualizados para receber GitRunner e GiteaRepoManager
- [ ] Zero metodo gitCmd/gitOutput/sanitize*/ensureRepo nos 3 arquivos
- [ ] Testes existentes ajustados para novo construtor
- [ ] Call sites em `main.go`/`server.go` ajustados

**DoD:** `go test ./internal/source/... ./internal/gitops/...` passa. `go vet ./...` limpo.

---

### Fase 2 — Criar `internal/shellgen` (script builder type-safe)

**Pode rodar em paralelo com Fase 1.**

#### Task 2.1 — Criar `api/internal/shellgen/builder.go`

**O que:** Builder fluente para gerar shell scripts sem `fmt.Sprintf`.

**API:**
```go
func New() *ScriptBuilder
func (b *ScriptBuilder) SetE() *ScriptBuilder                     // set -euo pipefail
func (b *ScriptBuilder) Cd(dir string) *ScriptBuilder             // cd 'quoted-dir'
func (b *ScriptBuilder) Echo(msg string) *ScriptBuilder           // echo 'quoted-msg'
func (b *ScriptBuilder) Env(key, val string) *ScriptBuilder       // export KEY='val'
func (b *ScriptBuilder) MkdirP(dir string) *ScriptBuilder         // mkdir -p 'dir'
func (b *ScriptBuilder) IfFileExists(path string) *CondBuilder    // if [ -f 'path' ]; then
func (b *ScriptBuilder) IfFileNotExists(path string) *CondBuilder // if [ ! -f 'path' ]; then
func (b *ScriptBuilder) Run(cmd string, args ...string) *ScriptBuilder
func (b *ScriptBuilder) Raw(line string) *ScriptBuilder           // escape hatch
func (b *ScriptBuilder) Comment(text string) *ScriptBuilder       // # text
func (b *ScriptBuilder) Append(other *ScriptBuilder) *ScriptBuilder
func (b *ScriptBuilder) String() string

func ShellQuote(s string) string  // POSIX single-quote escaping
```

`ShellQuote` e a funcao central de seguranca. Regra: wrap em single quotes, escapar `'` internas como `'\''`.

**Criterios de aceite:**
- [ ] `ShellQuote` escapa: espacos, aspas simples, aspas duplas, `$()`, backticks, newlines
- [ ] Testes de propriedade: qualquer string Go -> token shell seguro
- [ ] Testes: path com `$(whoami)`, path com `'; rm -rf /`, path com newline
- [ ] `New().SetE().Cd("/workspace").Run("npm", "ci").String()` produz script correto
- [ ] Zero dependencias externas (so stdlib)

**DoD:** `go test ./internal/shellgen/...` com 100% edge cases passando.

---

#### Task 2.2 — Migrar `argo/scripts.go` para ScriptBuilder

**O que:** Reescrever 15 funcoes de script generation.

**Funcoes a migrar:**
1. `scriptValidateNode` / `scriptInstallNode` / `scriptBuildNode` / `scriptScanNode`
2. `scriptValidateGo` / `scriptInstallGo` / `scriptBuildGo` / `scriptScanGo`
3. `scriptValidatePython` / `scriptInstallPython` / `scriptBuildPython` / `scriptScanPython`
4. `scriptValidateCustom` / `scriptInstallRuby`
5. Helpers: `validatePathsScript` / `checkAppLockfiles` / `scriptValidateFile` / `scriptValidateFileAny` / `validatePythonDeps`

**Exemplo de migracao:**
```go
// ANTES:
func scriptInstallGo(apps []services.AppSpec) string {
    script := "set -euo pipefail\ncd /workspace\n"
    for _, p := range paths {
        dir := "/workspace/" + p
        script += fmt.Sprintf(`cd %q\ngo mod download\n`, dir)
    }
    return script
}

// DEPOIS:
func scriptInstallGo(apps []services.AppSpec) string {
    b := shellgen.New().SetE().Cd("/workspace")
    for _, p := range appPaths(apps) {
        b.Echo("Downloading Go modules in " + p).
            Cd("/workspace/" + p).
            Run("go", "mod", "download").
            Echo("Modules downloaded in " + p)
    }
    return b.String()
}
```

**Criterios de aceite:**
- [ ] Todas as 15 funcoes usam ScriptBuilder — zero `fmt.Sprintf` para shell
- [ ] Helpers refatorados ou absorvidos no builder
- [ ] Testes existentes (`scripts_test.go`) continuam passando (output equivalente)
- [ ] Novo teste: path com `$()` nao gera code injection
- [ ] `appPaths()`, `sanitizeBinName()`, `defaultPort()` inalterados

**DoD:** `go test ./internal/argo/...` passa.

---

#### Task 2.3 — Migrar `buildkitEntrypointScript` para ScriptBuilder

**O que:** Funcao `buildkitEntrypointScript()` em `buildkit.go:206-253`.

**Problemas atuais:**
- `socketPath` injetado com aspas simples manuais (quebra se contiver `'`)
- `registryURL` injetado direto em TOML (quebra se contiver `"` ou newlines)

**Solucao:**
- Shell: `shellgen.ScriptBuilder` para toda a geracao
- TOML: validar `registryURL` antes de injecao (reject `"`, `\n`, `\r`)
- `socketPath`: escapar via `ShellQuote()`

**Criterios de aceite:**
- [ ] `buildkitEntrypointScript` usa ScriptBuilder
- [ ] `socketPath` escapado via ShellQuote em todos os pontos
- [ ] `registryURL` validado antes de injecao no TOML
- [ ] Teste: socketPath com espaco -> script valido
- [ ] Teste: registryURL com `"` -> erro explicito
- [ ] Estrutura do script permanece identica (daemon + wait + buildctl + cleanup)

**DoD:** `go test ./internal/argo/...` passa.

---

### Fase 3 — Corrigir `autodeploy.go` (context + safe_tar + GitRunner)

**Depende de:** Fase 1 (GitRunner).

#### Task 3.1 — Migrar `autodeploy.go` run() para GitRunner com context

**O que:** `tarballToGitBundle()` linhas 278-291 tem closure `run()` com:
1. `exec.Command` sem context (nao cancela em timeout)
2. Sem sanitizacao de output
3. Nao reusa GitRunner

**Mudanca:**
- `tarballToGitBundle` recebe `context.Context`
- Substituir closure por `gitclient.GitRunner.Run(ctx, dir, args...)`
- GIT_AUTHOR_NAME/EMAIL via `RunnerOption` (WithEnv)

**Criterios de aceite:**
- [ ] `tarballToGitBundle` recebe `context.Context`
- [ ] Usa `gitclient.GitRunner` (nao closure local)
- [ ] `exec.CommandContext` em vez de `exec.Command`
- [ ] Chamador `HandlePush` passa ctx
- [ ] Teste: context cancelled -> funcao retorna erro

**DoD:** `go test ./internal/github/...` passa.

---

#### Task 3.2 — Substituir tar extraction insegura por SafeExtractTarGz

**O que:** Linhas 224-275 de `tarballToGitBundle()` fazem extracao manual SEM protecoes.

**Mudanca:**
1. Mover `source/safe_tar.go` para `internal/archive/` (evitar import cycle github->source)
2. Adicionar opcao `StripComponents: 1` (GitHub tarballs tem `owner-repo-sha/` prefix)
3. Substituir extracao manual por `archive.SafeExtractTarGz()`

**Criterios de aceite:**
- [ ] `SafeExtractTarGz` em `internal/archive/` importavel por `github/`
- [ ] `StripComponents` opcao funcional (strip N niveis de diretorio)
- [ ] Protecoes ativas: symlink rejection, path traversal, size limit, file count limit
- [ ] Erros NAO silenciados (removido `continue` e `_ =` em erros criticos)
- [ ] `source/` importa de `archive/` (sem duplicacao)
- [ ] Teste: tarball com symlink -> erro
- [ ] Teste: tarball com path traversal -> erro
- [ ] Teste: tarball com StripComponents=1 -> funciona

**DoD:** `go test ./internal/github/... ./internal/archive/...` passa. Zero regressao em `source/`.

---

### Fase 4 — Completar migracao go-git para source store

**Depende de:** Fase 1 (GitRunner + GiteaRepoManager).

#### Task 4.1 — Criar `source/gogit_store.go`

**O que:** `gitops/gogit_pusher.go` ja usa go-git. Criar equivalente para source store.

**Operacoes a migrar:**
- `pushGitBundle`: clone via go-git, pull from bundle, push via go-git
- `pushTarBundle`: init + add + commit via go-git, push via go-git, bundle create via GitRunner
- `gitOutput` (rev-parse HEAD): `repo.Head()` em go-git

**Nota:** go-git nao suporta `git bundle create`. Manter GitRunner APENAS para essa operacao.

**Criterios de aceite:**
- [ ] `GoGitSourceStore` implementa mesma interface que `GiteaSourceStore`
- [ ] PushBundle (git format): clone/commit/push via go-git
- [ ] PushBundle (tar format): init/add/commit/push via go-git + bundle create via GitRunner
- [ ] Zero shell injection possivel
- [ ] Testes com repo local (file://)
- [ ] Config para escolher implementacao (default: go-git)

**DoD:** `go test ./internal/source/...` passa com ambas implementacoes.

---

### Fase 5 — Validacao Final

**Depende de:** Todas as fases anteriores.

#### Task 5.1 — Audit final: grep por padroes residuais

**Verificacoes:**
```bash
# exec.Command deve estar APENAS em:
#   gitclient/runner.go (centralizado)
#   source/gogit_store.go (bundle create only)
grep -rn 'exec\.Command' api/internal/

# fmt.Sprintf gerando shell deve ser ZERO fora de shellgen/
grep -rn 'fmt\.Sprintf.*set -e' api/internal/
grep -rn 'fmt\.Sprintf.*cd ' api/internal/

# sanitize/ensureRepo deve estar APENAS em gitclient/
grep -rn 'sanitizeOutput\|sanitizeArgs\|ensureRepo' api/internal/
```

**Criterios de aceite:**
- [ ] Zero exec.Command fora de gitclient/ e bundle create
- [ ] Zero fmt.Sprintf gerando shell fora de shellgen/
- [ ] Zero duplicacao de gitCmd/sanitize/ensureRepo
- [ ] `go test ./...` passa em todo o modulo api
- [ ] `go vet ./...` limpo
- [ ] `golangci-lint run` sem novos warnings

**DoD:** Relatorio de audit limpo. CI green.

---

#### Task 5.2 — Atualizar CHANGELOG.md

**Entradas em [Unreleased]:**

```markdown
### Changed
- Git operations centralized in `internal/gitclient` — eliminates duplicated gitCmd/sanitize/ensureRepo across source, gitops, and github packages
- Build container scripts generated via type-safe `internal/shellgen` builder — eliminates raw fmt.Sprintf shell injection risk
- Source store migrated to go-git (matches gitops store pattern)

### Security
- Fixed unsafe tar extraction in GitHub auto-deploy — added symlink, path traversal, and zip bomb protection via SafeExtractTarGz
- Shell script generation uses POSIX-safe quoting via ShellQuote for all interpolated values
- BuildKit entrypoint validates registryURL before TOML injection
```

**DoD:** CHANGELOG.md atualizado.

---

## Dependencias entre Fases

```
Fase 1 ─────────────┐
(gitclient)          ├──> Fase 3 (autodeploy) ──> Fase 5 (audit)
                     ├──> Fase 4 (gogit source)
Fase 2 ──────────────┘
(shellgen)
```

**Fases 1 e 2 sao independentes e podem rodar em paralelo.**

---

## O que NAO muda

- API routes, response shapes, --json output
- CLI output, exit codes
- State machines
- CRDs, controller reconcile logic
- Helm charts, Pulumi
- Database schema
- Contratos de interface (SourceStore, GitOpsPusher)

---

## Metricas de Sucesso

| Metrica | Antes | Depois |
|---------|-------|--------|
| Arquivos com exec.Command | 4 | 1 (gitclient/runner.go) + 1 excecao (bundle create) |
| Funcoes ensureRepo | 3 (identicas) | 1 |
| Funcoes gitCmd/sanitize | 6 (3 pares) | 2 (em GitRunner) |
| Linhas de fmt.Sprintf em shell | ~80 | 0 (via ScriptBuilder) |
| Tar extraction sem protecao | 1 (autodeploy) | 0 |
| exec.Command sem context | 1 (autodeploy) | 0 |
