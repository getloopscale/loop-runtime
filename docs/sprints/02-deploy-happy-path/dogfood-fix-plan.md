# Sprint 2 — Plano de Correção Pós-Dogfooding

> **Data:** 2026-03-14
> **Origem:** Dogfooding audit (6 achados, 2 S0 bloqueantes)
> **Objetivo:** Desbloquear o exit gate da Sprint 2 — `loopzero login → loopzero init → loopzero deploy` produz app live com URL real
> **Critério de aceite:** Deploy end-to-end funciona de qualquer repo local, CLI instalável via `bin/loopzero`

---

## Sequência de Execução

Ordem estrita de dependência — cada grupo desbloqueia o próximo.

---

## Grupo 1 — S0 Bloqueantes (Sem esses, nada funciona)

### D1 — Fix: `bin/loopzero` não executa programa (F-02)

**Arquivo:** `cli/bin/loopzero`
**Problema:** O entrypoint faz `require('../dist/index.js')` que carrega o módulo mas não executa. O guard `require.main === module` em `dist/index.js:279` falha porque `require.main` aponta para `bin/loopzero`, não para `dist/index.js`.
**Impacto:** Nenhum usuário que instala via npm consegue usar o CLI. `loopzero --help` → silêncio total.

**Correção:**
1. Alterar `cli/bin/loopzero` de:
   ```js
   #!/usr/bin/env node
   require('../dist/index.js');
   ```
   Para:
   ```js
   #!/usr/bin/env node
   require('../dist/index.js').program.parseAsync(process.argv).catch((err) => {
     process.stderr.write(`Error: ${err.message}\n`);
     process.exitCode = 1;
   });
   ```
2. Alternativa (mais limpa): remover o guard `require.main === module` do `index.ts` e fazer o `parseAsync` rodar sempre que o módulo é importado. Porém isso quebraria os testes unitários que importam `createProgram()` sem executar. Portanto, opção 1 é preferível.

**Validação:**
```bash
./cli/bin/loopzero --help        # deve mostrar help text
./cli/bin/loopzero whoami         # deve mostrar user autenticado
./cli/bin/loopzero deploy --help  # deve mostrar flags do deploy
cd cli && npm test            # todos os 298 testes passam
```

---

### D2 — Fix: Git bundle shallow impede todo deploy (F-01)

**Arquivo CLI:** `cli/src/adapters/git.ts:37`
**Arquivo API:** `api/internal/source/gitea_store.go:84-102`
**Problema:** `git bundle create -1 HEAD` cria bundle com apenas 1 commit. O Gitea repo está vazio (ou com histórico diferente). `git fetch/pull` do bundle falha porque os prerequisite commits (parent do HEAD) não existem no destino.

**Análise da causa raiz:**

O fluxo atual é:
1. CLI: `git bundle create -1 HEAD` → bundle com 1 commit + 1 prerequisite
2. API: `git clone` do Gitea (vazio) → `git pull bundle HEAD` → FALHA (prerequisite ausente)

O problema tem **dois pontos de correção possíveis**:

**Opção A — Fix no CLI (recomendada, mais simples):**

Trocar `git bundle create -1 HEAD` por `git bundle create --all HEAD`. Isso inclui TODOS os commits acessíveis de HEAD, sem prerequisite. O bundle é maior, mas funciona sempre.

```typescript
// cli/src/adapters/git.ts:37
// ANTES:
execSync(`git bundle create ${tmpFile} -1 HEAD`, { stdio: 'pipe' });

// DEPOIS:
execSync(`git bundle create ${tmpFile} --all HEAD`, { stdio: 'pipe' });
```

**Trade-off:** Bundles maiores para repos com muita história. Para um deploy típico de app Node.js (poucos MB de código), o bundle será de dezenas de MB no máximo. Aceitável para v1.

**Opção B — Fix no servidor (fallback):**

Se o `git fetch` do bundle falhar por prerequisite ausente, fazer `git bundle unbundle` diretamente. Isso é mais robusto mas mais complexo.

```go
// api/internal/source/gitea_store.go — dentro do bloco de erro do pull/fetch
// Fallback: unbundle diretamente (ignora história do repo destino)
if err2 := s.gitCmd(ctx, repoDir, "git", "bundle", "unbundle", bundlePath); err2 != nil {
    return "", fmt.Errorf("gitea source: git unbundle: %w", err2)
}
// Checkout the unbundled HEAD
if err2 := s.gitCmd(ctx, repoDir, "git", "checkout", "FETCH_HEAD"); err2 != nil {
    return "", fmt.Errorf("gitea source: checkout after unbundle: %w", err2)
}
```

**Recomendação:** Implementar Opção A (fix no CLI) como correção primária. É a solução mais simples, resolve o problema na raiz, e funciona para 100% dos cenários. Opção B pode ser adicionada depois como defesa em profundidade.

**Validação:**
```bash
# 1. Criar projeto fresh e fazer deploy
cd /tmp && rm -rf test-deploy && mkdir test-deploy && cd test-deploy
git init && mkdir api
echo '{"name":"api","scripts":{"start":"node index.js"}}' > api/package.json
echo 'require("http").createServer((q,s)=>s.end("ok")).listen(3000)' > api/index.js
cat > loopzero.yaml << 'EOF'
project: test-deploy
apps:
  api:
    type: server
    port: 3000
    path: ./api
EOF
git add -A && git commit -m "initial"
LOOPZERO_API_URL=http://localhost:3001 loopzero deploy --json
# Deve retornar build_id, não "failed to push source bundle"

# 2. Segundo deploy (incremental) também deve funcionar
echo '// v2' >> api/index.js
git add -A && git commit -m "v2"
LOOPZERO_API_URL=http://localhost:3001 loopzero deploy --json
# Deve funcionar (bundle --all inclui tudo, server force-pushes)
```

---

## Grupo 2 — S1 Grave

### D3 — Fix: Deploy sem auth trava indefinidamente (F-03)

**Arquivo:** `cli/src/commands/deploy.ts` + `cli/src/index.ts`
**Problema:** Sem `~/.loopzero/config.json`, o deploy trava por mais de 30 segundos sem mostrar nada. O auth check em `deploy.ts:108` deveria pegar isso, mas algo na wiring do `index.ts` impede.

**Hipótese de causa:**
O `index.ts:198-200` cria `token = configStore.getToken()` e `ApiClient` ANTES de chamar `deployCmd.execute()`. `getToken()` retorna `null`. A questão é se o problema está no `DeployCommand.execute()` ou na construção das deps. Precisa de investigação:

1. Verificar se `configStore.getToken()` está retornando `null` corretamente quando config não existe
2. Verificar se a condição `if (!this.deps.getToken())` em deploy.ts:108 está sendo avaliada
3. Verificar se `uploadSource` ou `connectSSE` são chamados antes do auth check por algum code path
4. Reproduzir: remover `~/.loopzero/config.json`, rodar `loopzero deploy`, observar com `--json`

**Correção (após investigação):**

Se o problema é que `getToken()` retorna string vazia (não null):
```typescript
// deploy.ts:108
if (!this.deps.getToken()?.trim()) {
```

Se o problema é um code path que bypassa o check:
- Adicionar guard no `index.ts` action handler, antes de construir as deps:
```typescript
.action(async (cmdOpts) => {
  const token = configStore.getToken();
  if (!token) {
    writeError('Not authenticated. Run: loopzero login\n');
    process.exitCode = 4;
    return;
  }
  // ... rest of deploy wiring
});
```

**Validação:**
```bash
mv ~/.loopzero/config.json ~/.loopzero/config.json.bak
loopzero deploy 2>&1         # deve mostrar "Not authenticated. Run: loopzero login" e sair com exit 4
loopzero deploy --json 2>&1  # deve mostrar {"error":"Not authenticated...","exit_code":4}
mv ~/.loopzero/config.json.bak ~/.loopzero/config.json
```

---

## Grupo 3 — S2 Moderados

### D4 — Fix: `loopzero init` não detecta apps em subdiretórios (F-04)

**Arquivo:** `cli/src/detection/framework.ts` e `cli/src/commands/init.ts`
**Problema:** Init gera `framework: custom` sem port para projetos com `package.json` em subdiretórios. Deploy subsequente falha com "must have a port".

**Correção:**
1. Investigar `detectFramework()` — verificar se ele só olha no diretório raiz
2. Se sim: estender para examinar subdiretórios de primeiro nível (1 nível de profundidade)
3. Se detectar `package.json` com `scripts.start` em `./api/`, gerar:
   ```yaml
   apps:
     api:
       path: ./api
       type: server
       port: 3000
   ```
4. Se não conseguir detectar port: pelo menos definir `type: server` e avisar o usuário: "Could not detect port for app 'api'. Edit loopzero.yaml to set it."

**Validação:**
```bash
cd /tmp && rm -rf test-init && mkdir test-init && cd test-init
git init
mkdir api && echo '{"name":"api","scripts":{"start":"node index.js"}}' > api/package.json
loopzero init --json
cat loopzero.yaml  # deve ter path: ./api, framework: node, port detectado ou warning
```

---

### D5 — Fix: Mensagem "failed to push source bundle" não é acionável (F-05)

**Arquivo:** `cli/src/commands/deploy.ts`
**Problema:** Quando `uploadSource` falha, a mensagem é genérica: "failed to push source bundle". Não explica causa nem sugere ação.

**Correção:**
1. Localizar onde `uploadSource` é chamado em `deploy.ts`
2. Enriquecer o catch com contexto:
   ```typescript
   } catch (err) {
     if (err instanceof ApiError && err.statusCode === 500) {
       return this.exitWithError(opts,
         'Failed to upload source code. The server could not process your code bundle. ' +
         'Ensure your git history is clean and try again.',
         err.exitCode);
     }
     // ... existing handling
   }
   ```
3. Se o erro vier do servidor com `message` no body, exibi-lo (sem termos internos)

**Validação:** Executar deploy que falha → mensagem deve sugerir próxima ação.

---

## Grupo 4 — S3 Leve

### D6 — Melhoria: `--json` não aparece no help do deploy (F-06)

**Arquivo:** `cli/src/index.ts` (definição do comando deploy)
**Problema:** `--json` é flag global (no programa pai), não aparece no `deploy --help`.

**Correção:** Não é necessária correção. A description do comando já menciona `--json`. Isso é comportamento padrão do Commander.js — flags globais não repetem no help de subcomandos.

**Status:** Won't fix — comportamento correto do framework.

---

## Checklist de Validação Final

Após todas as correções, executar a jornada completa:

```bash
# 0. Rebuild CLI
cd cli && npx tsc && npm test

# 1. bin/loopzero funciona
./cli/bin/loopzero --help          # mostra help
./cli/bin/loopzero --version       # mostra 0.1.0

# 2. Login (com token existente)
./cli/bin/loopzero login --token "$GITHUB_TOKEN"
./cli/bin/loopzero whoami          # mostra user

# 3. Init em projeto novo
cd /tmp && rm -rf e2e-test && mkdir e2e-test && cd e2e-test
git init && echo '{"name":"api","scripts":{"start":"node i.js"}}' > package.json
echo 'require("http").createServer((q,s)=>s.end("ok")).listen(3000)' > i.js
git add -A && git commit -m "init"
LOOPZERO_API_URL=http://localhost:3001 ./cli/bin/loopzero init

# 4. Deploy
LOOPZERO_API_URL=http://localhost:3001 ./cli/bin/loopzero deploy --json
# Deve: enviar source, iniciar build, retornar build_id

# 5. Deploy sem auth
mv ~/.loopzero/config.json ~/.loopzero/config.json.bak
LOOPZERO_API_URL=http://localhost:3001 ./cli/bin/loopzero deploy
# Deve: "Not authenticated. Run: loopzero login" (exit 4, < 1s)
mv ~/.loopzero/config.json.bak ~/.loopzero/config.json

# 6. Testes unitários passam
cd /home/paulo/Projetos/loopzero/loopzero/cli && npm test    # 298 pass
cd /home/paulo/Projetos/loopzero/loopzero/api && go test ./... # all pass

# 7. Zero termos internos
grep -rn 'ArgoCD\|argocd\|GitOps\|Gitea\|MinIO\|Kaniko' cli/src/ --include='*.ts' | grep -v test | wc -l  # 0
```

**Exit gate Sprint 2:** Passo 4 retorna `build_id` e o build progride (ou completa) no cluster.

---

## Resumo

| ID | Severidade | Problema | Correção | Risco |
|----|-----------|---------|---------|-------|
| D1 | S0 | `bin/loopzero` não executa | Fix entrypoint para chamar `parseAsync` | Baixo — change trivial |
| D2 | S0 | Git bundle shallow falha sempre | `--all HEAD` em vez de `-1 HEAD` | Baixo — bundles maiores mas funcional |
| D3 | S1 | Deploy sem auth trava | Auth guard antes de I/O | Médio — precisa investigar causa exata |
| D4 | S2 | Init não detecta subdiretórios | Estender `detectFramework` | Médio — lógica de detecção |
| D5 | S2 | Mensagem de erro genérica | Enriquecer catch com contexto | Baixo — text change |
| D6 | S3 | `--json` não no help | Won't fix | N/A |

**Ordem recomendada:** D1 → D2 → validar deploy E2E → D3 → D4 → D5
