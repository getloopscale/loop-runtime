# Roadmap: Dogfood Fixes — 2026-03-27

> Roadmap executavel para resolver os 8 achados do dogfood de 2026-03-27.
> Fonte: auditoria dogfood completa (login → init → deploy → delete → edge cases → observabilidade).
> Cada task e implementavel em uma unica sessao de trabalho.

---

## Indice

| Sprint | Foco | Tasks | Bloqueio | Status |
|--------|------|-------|----------|--------|
| **DF-1** | Build Pipeline (S0 + dependencias) | DF-1.1 a DF-1.3 | **Desbloqueia TODOS os deploys** | DF-1.1 DONE (code+test+E2E validated: build succeeds, image in registry). DF-1.2 DONE. DF-1.3 blocked by ArgoCD sync. |
| **DF-2** | Lifecycle & Integracao (S1-S2) | DF-2.1 a DF-2.3 | Corrige delete + logs | DF-2.1 DONE (code+test, 19 tests pass). DF-2.2/2.3 pending infra (Loki not deployed). |
| **DF-3** | CLI Polish (S2-S3) | DF-3.1 a DF-3.4 | UX e consistencia | DF-3.1 DONE, DF-3.2 DONE, DF-3.3 DONE. DF-3.4 pending. 492 CLI tests pass. |
| **DF-4** | Observabilidade & Cleanup (S2-S3) | DF-4.1 a DF-4.2 | Ambiente saudavel | DF-4.1 PARTIAL (16 projects deleted via CLI, ArgoCD prune pending). DF-4.2 pending. |

---

## Grafo de Dependencias

```
DF-1.1 (BuildKit fix) ──────────────────────> DF-1.2 (test BuildKit)
                                                 └──> DF-1.3 (deploy E2E validation)

DF-2.1 (delete cancela workflows) ──> DF-2.2 (Loki+Promtail deploy)
                                              └──> DF-2.3 (Loki integration test)

DF-3.1 (logs sem loopzero.yaml) ─┐
DF-3.2 (Commander JSON)     ─┼──> DF-3.4 (CLI edge case regression suite)
DF-3.3 (logs default build) ─┘

DF-4.1 (cleanup orfaos) ── nenhuma dependencia
DF-4.2 (operator conflict) ── nenhuma dependencia
```

**Ordem de execucao recomendada:**
```
Paralelo 1:  DF-1.1 → DF-1.2 → DF-1.3
Paralelo 2:  DF-2.1
Paralelo 3:  DF-3.1, DF-3.2, DF-3.3 (independentes)
Apos DF-1.3: DF-2.2 → DF-2.3
Apos DF-3.*: DF-3.4
Qualquer momento: DF-4.1, DF-4.2
```

---

## Sprint DF-1 — Build Pipeline (BLOQUEANTE)

> Prioridade: **P0**. Nenhum deploy funciona ate que DF-1.1 seja resolvido.

### Task DF-1.1: Fix BuildKit entitlement no daemon

**Componente:** api
**Agent:** api-engineer
**Spec:** §10.1 — Build Flow, §7.4 — Build Engine
**Depende de:** nenhuma
**Estimativa:** P (uma sessao)

#### O que implementar

O `buildkitd` e iniciado sem `--allow-insecure-entitlement security.insecure`, mas o `buildctl` usa `--allow security.insecure`. O daemon precisa da flag de entitlement para conceder a permissao ao client.

**Arquivo:** `api/internal/argo/buildkit.go`, funcao `buildkitEntrypointScript()` (linha 193)

**Fix:** Quando `insecure=true`, adicionar `--allow-insecure-entitlement security.insecure` aos args do `buildkitd`:

```go
func buildkitEntrypointScript(socketPath string, insecure bool) string {
    buildkitdArgs := fmt.Sprintf("--oci-worker-no-process-sandbox --addr '%s'", socketPath)
    if insecure {
        buildkitdArgs += " --oci-worker-snapshotter native"
        buildkitdArgs += " --allow-insecure-entitlement security.insecure"  // <-- FIX
    }
    // ... resto sem alteracao
}
```

#### Criterios de aceite

- [ ] `buildkitEntrypointScript(path, true)` inclui `--allow-insecure-entitlement security.insecure` no script gerado
- [ ] `buildkitEntrypointScript(path, false)` NAO inclui a flag
- [ ] Teste unitario existente atualizado para validar o script (nao apenas os args do buildctl)
- [ ] Deploy local (`loopzero deploy`) progride alem do step `build-image`

#### Teste primeiro (TDD)

**Arquivo:** `api/internal/argo/workflow_builder_test.go`

```go
func TestBuildkitEntrypointScript_InsecureIncludesEntitlement(t *testing.T) {
    script := buildkitEntrypointScript("unix:///sock", true)
    assert.Contains(t, script, "--allow-insecure-entitlement security.insecure")
}

func TestBuildkitEntrypointScript_SecureExcludesEntitlement(t *testing.T) {
    script := buildkitEntrypointScript("unix:///sock", false)
    assert.NotContains(t, script, "--allow-insecure-entitlement")
}
```

#### Riscos conhecidos

- Nenhum risco significativo. A flag e necessaria para que `--allow security.insecure` funcione no buildctl. Sem ela, TODOS os builds falham.

---

### Task DF-1.2: Validar build-image step completo localmente

**Componente:** cross (api + infra)
**Agent:** orchestrator
**Spec:** §10.1 — Build Flow
**Depende de:** DF-1.1
**Estimativa:** P (uma sessao)

#### O que implementar

Apos DF-1.1, redeploy a API e executar um deploy completo para validar que o step `build-image` agora sucede. Verificar que a imagem e pushada para o registry local.

1. Rebuild API: `tilt up` ou `task dev:up` (hot reload aplica o fix)
2. Criar app de teste com health endpoint
3. `loopzero deploy --json` e validar exit code 0
4. Verificar imagem no registry: `curl http://localhost:5050/v2/_catalog`
5. Verificar que o workflow completa com status `Succeeded`

#### Criterios de aceite

- [ ] Workflow step `build-image-*` tem fase `Succeeded`
- [ ] Imagem existe no registry (`k3d-loopzero-registry:5000/acme/<project>/<app>:<sha>`)
- [ ] `loopzero deploy --json` retorna `status: SUCCEEDED` (ou progride para releasing)
- [ ] Nenhum erro no log do pod de build

#### Teste primeiro (TDD)

Nao requer teste de codigo novo. E uma validacao de integracao manual que confirma DF-1.1.

#### Riscos conhecidos

- A registry local pode nao estar acessivel (pre-flight mostrou WARN). Se a imagem nao pusha, o problema e de networking k3d, nao de BuildKit.
- Outros steps podem falhar (gitops push, ArgoCD sync) — tratar separadamente.

---

### Task DF-1.3: Validar deploy E2E com Prova de Vida

**Componente:** cross
**Agent:** orchestrator
**Spec:** §10.1 — Build Flow, §10.2 — Release Flow
**Depende de:** DF-1.2
**Estimativa:** M (meio dia — inclui troubleshooting se falhar)

#### O que implementar

Deploy completo de um app Node.js e validar que esta rodando no workload cluster:

1. `loopzero deploy --json` → exit 0, `status: SUCCEEDED`
2. Pod Running no namespace `loopzero-acme-production`
3. Container Ready (1/1)
4. Health endpoint respondendo (`kubectl exec ... wget localhost:3000/health`)
5. ArgoCD application `acme-production` mostra `Synced + Healthy`
6. `loopzero logs --build <id>` retorna entries com conteudo (depende de DF-2.2 para logs via Loki)

#### Criterios de aceite

- [ ] `loopzero deploy --json` exit 0
- [ ] Pod Running + Container Ready no workload cluster
- [ ] Health endpoint responde 200 de dentro do pod
- [ ] ArgoCD mostra Synced
- [ ] Exit code semanticamente correto em todas as fases

#### Teste primeiro (TDD)

Nao e teste de codigo — e validacao de integracao vertical.

#### Riscos conhecidos

- GitOps push para Gitea pode falhar (cross-cluster networking)
- ArgoCD sync pode nao converger (repo credentials, RBAC)
- Se falhar apos build suceder, o problema e em outra camada (release, gitops, ArgoCD)

---

## Sprint DF-2 — Lifecycle & Integracao

> Prioridade: **P1**. Corrige dados orfaos e logs vazios.

### Task DF-2.1: Project delete deve cancelar Argo Workflows ativos

**Componente:** api
**Agent:** api-engineer
**Spec:** §10.5 — Project Deletion
**Depende de:** nenhuma
**Estimativa:** M (meio dia)

#### O que implementar

`ProjectDeleter.deleteEnvironment()` em `api/internal/services/project_deleter.go` faz `CancelActiveBuilds()` que apenas atualiza o status no banco. Nao cancela os workflows no Argo.

**Solucao:** Antes de atualizar o DB, buscar os `workflow_id` dos builds ativos e chamar `WorkflowSubmitter.Cancel()` para cada um.

**Arquivo:** `api/internal/services/project_deleter.go`, funcao `deleteEnvironment()` (linha 106)

**Mudancas necessarias:**

1. Adicionar `WorkflowCanceller` como dependencia do `ProjectDeleter`:
   ```go
   type WorkflowCanceller interface {
       Cancel(ctx context.Context, namespace, workflowID string) error
   }
   ```

2. Na funcao `deleteEnvironment()`, ANTES do `CancelActiveBuilds()`, buscar builds ativos com seus workflow IDs:
   ```go
   // Dentro do lock, ANTES de CancelActiveBuilds:
   activeBuilds, err := d.buildStore.ListActiveBuilds(ctx, tx, req.TenantID, req.ProjectID, env)
   // ... cancel workflows ...
   for _, build := range activeBuilds {
       if build.WorkflowID != "" {
           if cancelErr := d.workflowCanceller.Cancel(ctx, "argo", build.WorkflowID); cancelErr != nil {
               slog.Warn("workflow cancel failed (best-effort)", "workflow_id", build.WorkflowID, "error", cancelErr)
           }
       }
   }
   // Depois: CancelActiveBuilds (DB update)
   ```

3. Adicionar `ListActiveBuilds` ao `BuildStore` (query builds QUEUED/RUNNING com workflow_id):
   ```sql
   SELECT id, workflow_id FROM builds
   WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
   AND status IN ('QUEUED', 'RUNNING') AND workflow_id IS NOT NULL
   ```

**Nota:** O cancel do workflow e best-effort (o workflow pode ja ter terminado). Nao bloqueia a operacao de delete.

#### Criterios de aceite

- [ ] `ProjectDeleter` tem dependencia de `WorkflowCanceller`
- [ ] `deleteEnvironment()` chama `Cancel()` para cada build ativo com `workflow_id`
- [ ] `cancelled_builds` no response reflete o numero real de builds cancelados
- [ ] Teste unitario: delete com 2 builds RUNNING → ambos workflows cancelados
- [ ] Teste unitario: delete com build sem workflow_id → skip sem erro
- [ ] Teste unitario: workflow cancel falha → operacao de delete continua (best-effort)

#### Teste primeiro (TDD)

**Arquivo:** `api/tests/unit/project_deleter_test.go`

```go
func TestProjectDeleter_CancelsActiveWorkflows(t *testing.T) {
    // Arrange: 2 builds RUNNING com workflow_id
    // Act: Delete project
    // Assert: WorkflowCanceller.Cancel chamado 2x com IDs corretos
}

func TestProjectDeleter_SkipsBuildWithoutWorkflowID(t *testing.T) {
    // Arrange: 1 build QUEUED sem workflow_id
    // Act: Delete project
    // Assert: WorkflowCanceller.Cancel NAO chamado
}

func TestProjectDeleter_ContinuesOnWorkflowCancelFailure(t *testing.T) {
    // Arrange: 1 build RUNNING, Cancel retorna erro
    // Act: Delete project
    // Assert: delete completa com sucesso, cancelled_builds = 1
}
```

#### Riscos conhecidos

- `WorkflowSubmitter.Cancel()` ja existe e usa `spec.shutdown: Stop`. O pattern e comprovado no `BuildOrchestrator.CancelBuild()`.
- O cancel do workflow faz I/O cross-cluster (mgmt → build). Se o build cluster estiver inacessivel, o cancel falha mas o delete do DB continua.

---

### Task DF-2.2: Deploy Loki + Promtail no build cluster

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §15 — Observability, §7.8 — Log Aggregation
**Depende de:** nenhuma (pode rodar paralelo a DF-1)
**Estimativa:** M (meio dia)

#### O que implementar

Loki e Promtail NAO estao deployados em nenhum cluster. Os Helm values existem em `infra/helm/loki/` e `infra/helm/promtail/` mas nunca foram instalados no ambiente local.

1. **Instalar Loki** no cluster de build (ou mgmt, conforme design):
   - Verificar `infra/helm/loki/values.yaml` — MinIO endpoint e bucket
   - Helm install no namespace `monitoring` do cluster onde os logs serao armazenados

2. **Instalar Promtail** no cluster de build:
   - Verificar `infra/helm/promtail/values.yaml` — push endpoint para Loki
   - Promtail deve rodar como DaemonSet no build cluster (onde os pods de Argo Workflows rodam)
   - Confirmar que filtra por label `workflows.argoproj.io/workflow`

3. **Configurar LOKI_URL na API**:
   - Adicionar env var `LOKI_URL` no deploy da API (`infra/tilt/api.yaml` ou Tiltfile)
   - Valor: `http://loki.monitoring.svc.cluster.local:3100` (se Loki no mesmo cluster)
   - Ou URL cross-cluster se Loki estiver no build cluster

4. **Adicionar ao Taskfile** (idempotente):
   - `task setup` deve incluir Loki e Promtail no bootstrap
   - Ou task separada: `task setup:loki`

#### Criterios de aceite

- [ ] Pod `loki-*` Running no cluster alvo
- [ ] Pod `promtail-*` Running como DaemonSet no build cluster
- [ ] `LOKI_URL` configurado como env var no deploy da API
- [ ] Loki health check: `curl http://<loki-svc>:3100/ready` → 200
- [ ] Apos um build, `loopzero logs --build <id>` retorna entries nao-vazias
- [ ] `task setup` instala Loki e Promtail idempotentemente

#### Teste primeiro (TDD)

Nao e teste de codigo. Validacao de infraestrutura:
```bash
# Verificar Promtail coleta logs de build pods
kubectl logs -n monitoring -l app=promtail --tail=20 | grep "workflow"

# Verificar Loki recebe logs
curl -G "http://<loki-svc>:3100/loki/api/v1/query" \
  --data-urlencode 'query={namespace="argo"}' --data-urlencode 'limit=5'
```

#### Riscos conhecidos

- MinIO bucket `loopzero-logs` pode nao existir. Precisa ser criado antes do deploy do Loki.
- Cross-cluster networking: se Loki esta no mgmt e Promtail no build, precisa de service cross-cluster ou NodePort.
- Retention de 7 dias pode consumir storage significativo no MinIO local.

---

### Task DF-2.3: Validar integracao Loki end-to-end

**Componente:** cross (api + infra)
**Agent:** orchestrator
**Spec:** §15.3 — Log Aggregation
**Depende de:** DF-2.2, DF-1.2 (precisa de um build que complete)
**Estimativa:** P (uma sessao)

#### O que implementar

Validar que a cadeia completa funciona: build pod → Promtail → Loki → API → CLI.

1. Executar `loopzero deploy` (build deve suceder apos DF-1.*)
2. `loopzero logs --build <id> --json` → entries nao-vazias
3. Verificar que cada step do build tem logs (clone-repo, install-deps, build-image, etc.)
4. Verificar que o filtro `--step` funciona

#### Criterios de aceite

- [ ] `loopzero logs --build <id> --json` retorna `total_lines > 0`
- [ ] Entries contem steps reais do workflow (clone-repo, install-deps, etc.)
- [ ] `loopzero logs --build <id> --step clone-repo` filtra corretamente
- [ ] Logs de build FAILED tambem sao retornados (nao apenas builds que sucedem)

#### Teste primeiro (TDD)

Validacao de integracao, nao requer teste novo.

#### Riscos conhecidos

- Labels do Promtail podem nao bater com a query LogQL da API (mismatch `workflow=~".*buildID.*"`)

---

## Sprint DF-3 — CLI Polish

> Prioridade: **P2**. Melhora UX e consistencia do CLI.

### Task DF-3.1: `loopzero logs` nao deve exigir loopzero.yaml quando --build fornecido

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.6 — loopzero logs
**Depende de:** nenhuma
**Estimativa:** P (uma sessao)

#### O que implementar

**Arquivo:** `cli/src/commands/logs.ts`, metodo `execute()` (linhas 60-69)

O check de `findLoopZeroYaml()` na linha 60 acontece ANTES do check de `opts.build` na linha 67. Quando `--build` e fornecido, o loopzero.yaml nao e necessario — o build ID e suficiente para consultar a API.

**Fix:** Inverter a ordem — checar `--build` antes de `findLoopZeroYaml()`. Se `--build` esta presente, pular o check de loopzero.yaml:

```typescript
// 2. Validate build ID
if (!opts.build) {
    return this.exitWithError(opts, 'Build ID is required. Usage: loopzero logs --build <id>', 1, 'MISSING_BUILD_ID');
}

// 3. Find loopzero.yaml (only needed if we were to derive project context)
// When --build is provided, we skip loopzero.yaml — build ID is globally unique
```

#### Criterios de aceite

- [ ] `loopzero logs --build <id> --json` funciona de qualquer diretorio (sem loopzero.yaml)
- [ ] `loopzero logs` (sem --build) continua exigindo loopzero.yaml
- [ ] Teste unitario: execute com build ID, sem findLoopZeroYaml → sucesso
- [ ] Teste unitario: execute sem build ID, sem findLoopZeroYaml → MISSING_CONFIG

#### Teste primeiro (TDD)

**Arquivo:** `cli/tests/unit/commands/logs.test.ts`

```typescript
test('succeeds with --build even without loopzero.yaml', async () => {
    const deps = makeDeps({ findLoopZeroYaml: () => null, getLogs: () => mockResponse });
    const cmd = new LogsCommand(deps);
    const exit = await cmd.execute({ build: 'build-abc', json: true });
    expect(exit).toBe(0);
});

test('fails without --build and without loopzero.yaml', async () => {
    const deps = makeDeps({ findLoopZeroYaml: () => null });
    const cmd = new LogsCommand(deps);
    const exit = await cmd.execute({ json: true });
    expect(exit).toBe(1);
});
```

#### Riscos conhecidos

- Se a API exigir tenant context para autorizacao de logs, pode precisar de fallback. Verificar se a rota `GET /api/v1/builds/{id}/logs` usa o token para inferir o tenant.

---

### Task DF-3.2: Commander errors devem respeitar --json

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.3 — JSON Output Contract
**Depende de:** nenhuma
**Estimativa:** P (uma sessao)

#### O que implementar

**Arquivo:** `cli/src/index.ts` (linhas 772-776)

Quando Commander.js detecta um erro de validacao (ex: required option ausente, argumento faltando), o erro e lancado ANTES do `.action()` handler executar. O catch block na linha 773 escreve texto puro para stderr, ignorando a flag `--json`.

**Fix:** Usar `program.exitOverride()` e interceptar erros de Commander para formatar como JSON:

```typescript
// No createProgram(), antes de retornar program:
program.exitOverride();

// No catch block (linha 773):
program.parseAsync(process.argv).catch((err: any) => {
    const globalOpts = program.opts();
    if (globalOpts.json) {
        const output = {
            error: err.message,
            exit_code: 1,
            code: 'COMMAND_VALIDATION_ERROR',
        };
        process.stdout.write(JSON.stringify(output) + '\n');
    } else {
        process.stderr.write(`Error: ${err.message}\n`);
    }
    process.exitCode = err.exitCode || 1;
});
```

#### Criterios de aceite

- [ ] `loopzero logs --json` (sem --build) retorna JSON valido com `error`, `exit_code`, `code`
- [ ] `loopzero project delete --yes --json` (sem nome) retorna JSON valido
- [ ] `loopzero logs` (sem --json, sem --build) continua mostrando texto plain
- [ ] Exit code 1 para todos os erros de validacao Commander
- [ ] Teste: Commander error + --json → JSON output
- [ ] Teste: Commander error sem --json → texto stderr

#### Teste primeiro (TDD)

**Arquivo:** `cli/tests/unit/commander-json-errors.test.ts`

```typescript
test('missing required option with --json returns JSON', async () => {
    const { stdout } = await runCLI(['logs', '--json']);
    const parsed = JSON.parse(stdout);
    expect(parsed.code).toBe('COMMAND_VALIDATION_ERROR');
    expect(parsed.exit_code).toBe(1);
});

test('missing required arg with --json returns JSON', async () => {
    const { stdout } = await runCLI(['project', 'delete', '--yes', '--json']);
    const parsed = JSON.parse(stdout);
    expect(parsed.code).toBe('COMMAND_VALIDATION_ERROR');
});
```

#### Riscos conhecidos

- `program.exitOverride()` muda o comportamento de Commander: erros lancam excecao em vez de chamar `process.exit()`. Precisa garantir que o catch block trata TODOS os tipos de erro.
- Cuidado com `err.exitCode` vs `err.code` — Commander usa ambos com semanticas diferentes.

---

### Task DF-3.3: `loopzero logs` deve ter default para ultimo build do projeto

**Componente:** cli + api
**Agent:** cli-engineer (CLI) + api-engineer (endpoint)
**Spec:** §25.6 — loopzero logs
**Depende de:** nenhuma
**Estimativa:** M (meio dia)

#### O que implementar

Atualmente `--build` e obrigatorio. Um dev que esqueceu o build ID nao tem como descobri-lo (nao existe endpoint de lista de builds).

**Opcao A (recomendada):** Tornar `--build` opcional. Quando omitido, pegar o ultimo build do projeto:

1. **API:** Adicionar query param `latest=true` no endpoint `GET /api/v1/builds/{id}/logs`, ou novo endpoint `GET /api/v1/projects/{name}/builds/latest/logs`
2. **CLI:** Se `--build` nao fornecido E loopzero.yaml existe, chamar API com project name para obter logs do ultimo build

**Opcao B (minima):** Melhorar a mensagem de erro sugerindo onde achar o build ID:
```
Build ID is required. Find it in the output of 'loopzero deploy' or 'loopzero deploy --no-wait'.
Usage: loopzero logs --build <id>
```

**Recomendacao:** Implementar Opcao B agora (rapido, sem novo endpoint) e Opcao A como melhoria futura.

#### Criterios de aceite (Opcao B)

- [ ] Mensagem de erro inclui sugestao: "Find it in the output of 'loopzero deploy'"
- [ ] Teste unitario: erro sem --build contem "Find it in the output"

#### Criterios de aceite (Opcao A — se implementar)

- [ ] `loopzero logs` (sem --build, com loopzero.yaml) retorna logs do ultimo build
- [ ] `loopzero logs --json` (sem --build) retorna JSON do ultimo build
- [ ] Teste unitario: sem --build, com projeto → chama API latest
- [ ] API endpoint novo com teste de integracao

#### Teste primeiro (TDD)

**Arquivo:** `cli/tests/unit/commands/logs.test.ts`

```typescript
test('error without --build suggests where to find ID', async () => {
    const deps = makeDeps({ findLoopZeroYaml: () => mockYaml });
    const cmd = new LogsCommand(deps);
    const captured = captureOutput();
    await cmd.execute({ json: false });
    expect(captured.stderr).toContain('loopzero deploy');
});
```

#### Riscos conhecidos

- Opcao A exige novo endpoint na API e nova query no build store. Adiciona complexidade.
- Opcao B e segura mas nao resolve o problema fundamental (dev sem o ID).

---

### Task DF-3.4: Suite de regressao para edge cases CLI

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.3 — JSON schemas, §25 — CLI Reference
**Depende de:** DF-3.1, DF-3.2, DF-3.3
**Estimativa:** M (meio dia)

#### O que implementar

Criar uma suite de testes que cobre TODOS os edge cases testados no dogfood. Estes devem ser testes unitarios que validam comportamento sem depender de infra:

1. **init edge cases:** nome especial (sanitizacao), nome longo (truncamento a 63), nome vazio (fallback dir), sem .git, 2x sem --force
2. **deploy edge cases:** --env UPPERCASE, --env especiais, --app inexistente, sem auth, sem loopzero.yaml, sem .git
3. **auth edge cases:** whoami sem token, deploy sem auth, login invalido nao destroi sessao
4. **delete edge cases:** projeto inexistente, sem nome, sem --yes em CI, idempotente
5. **flags:** --json em todos os erros acima, CI=true, --no-wait
6. **termos internos:** scan de TODOS os outputs por termos proibidos (Rule 3)

#### Criterios de aceite

- [ ] 30+ testes de edge case adicionados
- [ ] Cada teste valida exit code + formato de output (JSON vs human)
- [ ] Nenhum teste usa mock de infra (apenas deps do CLI)
- [ ] Teste de varredura de termos internos em todas as mensagens de erro
- [ ] `npm test` passa com todos os novos testes

#### Teste primeiro (TDD)

Esta task E a criacao dos testes. Arquivos:
- `cli/tests/unit/edge-cases/init-validation.test.ts`
- `cli/tests/unit/edge-cases/deploy-validation.test.ts`
- `cli/tests/unit/edge-cases/auth-flow.test.ts`
- `cli/tests/unit/edge-cases/delete-validation.test.ts`
- `cli/tests/unit/edge-cases/internal-terms.test.ts`

#### Riscos conhecidos

- Alguns testes podem depender de comportamento do Commander que muda entre versoes.

---

## Sprint DF-4 — Observabilidade & Cleanup

> Prioridade: **P3**. Ambiente saudavel e monitoramento.

### Task DF-4.1: Cleanup de pods orfaos e documentar procedimento

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §15 — Observability
**Depende de:** nenhuma
**Estimativa:** P (uma sessao)

#### O que implementar

19 pods orfaos no namespace `loopzero-acme-production` (3 em CrashLoopBackOff) causam ArgoCD `Degraded`. Precisamos:

1. **Identificar e remover projetos orfaos** via `loopzero project delete` para cada projeto antigo
2. **Verificar ArgoCD health** apos cleanup: `acme-production` deve voltar a `Healthy`
3. **Documentar procedimento de cleanup** em `docs/runbooks/cleanup-orphan-apps.md`:
   - Como identificar pods orfaos
   - Como deletar via `loopzero project delete`
   - Como verificar ArgoCD health apos cleanup
   - Adicionar ao pre-gate checklist de dogfood

4. **Considerar TTL automatico** (scope futuro): app sem acesso por N dias → alerta → auto-cleanup

#### Criterios de aceite

- [ ] 0 pods em CrashLoopBackOff no workload cluster
- [ ] ArgoCD `acme-production` mostra `Healthy` (nao `Degraded`)
- [ ] Runbook documentado em `docs/runbooks/cleanup-orphan-apps.md`
- [ ] Pre-gate checklist de dogfood atualizado com step de cleanup

#### Teste primeiro (TDD)

Nao e teste de codigo. Validacao operacional.

#### Riscos conhecidos

- Alguns pods podem nao ter projeto correspondente na API (se foram criados por dogfood anterior e o projeto ja foi deletado). Nesse caso, precisa de cleanup manual via `kubectl delete` dos deployments orfaos.

---

### Task DF-4.2: Reduzir conflitos de optimistic concurrency no Tenant controller

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.1 — Tenant Controller
**Depende de:** nenhuma
**Estimativa:** P (uma sessao)

#### O que implementar

O Tenant controller tem 5 erros "the object has been modified" em 15 minutos. Isso indica que multiplos reconcile loops competem para atualizar o mesmo Tenant CR.

**Investigar:**
1. Quantos controllers atualizam status do Tenant? (Tenant, Environment, Application, Exposure)
2. Os sub-controllers deveriam atualizar o Tenant CR ou seus proprios CRs?
3. O Tenant controller usa `status.subresource`?

**Possíveis fixes (investigar antes de implementar):**
- Usar `status` subresource separado se nao estiver em uso
- Reduzir frequencia de status updates (debounce)
- Garantir que cada controller so atualiza SEU CR, nao o Tenant

#### Criterios de aceite

- [ ] Investigacao documenta a causa raiz dos conflitos
- [ ] Se fix implementado: < 1 conflito por hora em operacao normal
- [ ] Testes de operator continuam passando (`cd operators && go test ./...`)

#### Teste primeiro (TDD)

Depende da investigacao. Se a causa raiz for identificada, o teste sera escrito antes do fix.

#### Riscos conhecidos

- Conflitos de optimistic concurrency sao normais em Kubernetes quando multiplos controllers tocam o mesmo resource. O controller-runtime faz requeue automatico. O risco e mais de performance/latencia do que de corretude.

---

## Exit Gates

### Exit Gate DF-1: Build Pipeline Funcional

```bash
# 1. Testes unitarios passam (inclui teste de entitlement)
cd api && go test ./internal/argo/... -run TestBuildkit

# 2. Deploy completo sucede
cd /tmp/test-app && loopzero deploy --json
# Resultado: exit 0, status SUCCEEDED

# 3. Prova de Vida
KUBECONFIG=$(k3d kubeconfig write loopzero-workload)
kubectl --kubeconfig="$KUBECONFIG" get pods -n loopzero-acme-production -l app=<app>
# Resultado: 1/1 Running
```

### Exit Gate DF-2: Lifecycle & Logs

```bash
# 1. Delete cancela workflows
loopzero deploy --no-wait  # inicia build
loopzero project delete <name> --yes --json
# Resultado: cancelled_builds > 0
kubectl get workflows -n argo  # workflow nao mais Running

# 2. Loki funcional
loopzero deploy --json  # build completo
loopzero logs --build <id> --json
# Resultado: total_lines > 0, entries nao-vazias
```

### Exit Gate DF-3: CLI Polish

```bash
# 1. Testes unitarios
cd cli && npm test
# Resultado: 530+ testes passando (498 existentes + 30+ novos)

# 2. Commander JSON errors
loopzero logs --json 2>&1 | python3 -c "import json,sys; json.load(sys.stdin)"
# Resultado: JSON valido

# 3. Logs sem loopzero.yaml
cd /tmp && loopzero logs --build build-xxx --json
# Resultado: nao falha com MISSING_CONFIG
```

### Exit Gate DF-4: Ambiente Limpo

```bash
# 1. Nenhum pod em erro
for cluster in loopzero-mgmt loopzero-build loopzero-workload; do
  KUBECONFIG=$(k3d kubeconfig write $cluster)
  kubectl --kubeconfig="$KUBECONFIG" get pods -A | grep -E "CrashLoop|Error|Pending"
done
# Resultado: vazio (nenhum pod em erro)

# 2. ArgoCD healthy
kubectl get applications -n argocd -o custom-columns=NAME:.metadata.name,HEALTH:.status.health.status
# Resultado: todos Healthy
```

---

## Resumo de Esforco

| Sprint | Tasks | Estimativa Total | Bloqueio |
|--------|-------|-----------------|----------|
| DF-1 | 3 | 1 dia | Desbloqueia deploys |
| DF-2 | 3 | 1.5 dias | Corrige lifecycle + logs |
| DF-3 | 4 | 1.5 dias | Polish CLI |
| DF-4 | 2 | 0.5 dia | Ambiente limpo |
| **Total** | **12 tasks** | **~4.5 dias** | |

**Caminho critico:** DF-1.1 → DF-1.2 → DF-1.3 → DF-2.3 (validacao Loki depende de build funcional)
