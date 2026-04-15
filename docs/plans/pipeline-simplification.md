# Pipeline Simplification Plan

> Plano executável para reescrever a pipeline de infra de 1539 linhas para 4 workflows limpos.
> Baseado em MEET-2026-04-15-049 e MEET-2026-04-15-050.

**Status:** In Progress
**Owner:** Platform Team
**Priority:** P0 — bloqueia todas as operações de infra
**Estimativa:** 2-3 dias

---

## Pré-condições (validadas)

- [x] State limpo (nuclear reset feito 2026-04-15)
- [x] Infra real destruída (clusters, DB, tunnel deletados)
- [x] CI pipeline green (15/15 jobs)
- [x] Build Images pipeline green (11/11 jobs)
- [x] Meeting records aprovados (MEET-049 + MEET-050)

---

## Fase 1: infra-deploy.yml (~150 linhas)

**Objetivo:** `pulumi up` puro, testável local e CI de forma idêntica.

### Task 1.1: Escrever novo infra-deploy.yml
**Arquivo:** `.github/workflows/infra-deploy.yml`
**Mudança:** Reescrever do zero. Estrutura:
```yaml
jobs:
  resolve:     # ~30 linhas: branch → stack/env/backend
  validate:    # ~20 linhas: npm ci + tsc + npm test
  deploy:      # ~60 linhas: pulumi login + config set + pulumi up
  bootstrap:   # ~10 linhas: calls infra-bootstrap.yml (reusable)
```
**Critérios de aceite:**
- [ ] Arquivo tem ≤ 150 linhas (sem contar comentários/blank lines)
- [ ] Zero `pulumi cancel` no workflow
- [ ] Zero `pulumi state delete` no workflow
- [ ] Zero `PULUMI_K8S_DELETE_UNREACHABLE` no workflow
- [ ] Zero `pulumi stack export | python3` no workflow
- [ ] Máximo 5 `|| true` (cada um com comentário justificando)
- [ ] Zero `2>/dev/null` em operações de state
- [ ] `helm dependency update` roda antes do `pulumi up`
- [ ] `cloudflare:apiToken` setado no config antes do `pulumi up`
- [ ] Trigger: push to develop/homolog/main em `infra/**`
- [ ] Trigger: workflow_dispatch com stack + command inputs
- [ ] Environment resolvido por branch (infra-dev, infra-homolog, infra-prod)
- [ ] Concurrency: `cancel-in-progress: false` por stack
- [ ] Deploy health output em `$GITHUB_STEP_SUMMARY`:
  - Pulumi exit code
  - Duration
  - Resources created/updated/deleted/same

**DoD Task 1.1:**
- Workflow YAML válido (`python3 -c "import yaml; yaml.safe_load(open(...))"`)
- Não contém nenhuma das strings proibidas: `state delete`, `stack export.*python`, `DELETE_UNREACHABLE`, `helm uninstall`, `2-pass`, `Pass 2`

### Task 1.2: Remover adopt-or-create.ts do caminho normal
**Arquivo:** `infra/pulumi/src/core/adopt-or-create.ts`, `infra/pulumi/src/index.ts`
**Mudança:** O `resolveOrphans()` no index.ts faz lookups HTTP sync no boot do programa. Isso é aceitável como safety net mas não deve ser o mecanismo primário.
**Critérios de aceite:**
- [ ] `resolveOrphans()` continua existindo (safety net)
- [ ] Lookups que falham logam warning em vez de silenciar (`catch { return undefined }` → `catch(e) { pulumi.log.warn(...); return undefined }`)
- [ ] Zero `execSync` de `curl` sem timeout (atual: 10s — OK)

**DoD Task 1.2:**
- `npx tsc --noEmit` passa
- Warnings de adopt-or-create aparecem no Pulumi output quando executado localmente

### Task 1.3: Validar `pulumi up` localmente
**Pré-requisito:** Tasks 1.1, 1.2
**Ação:** Rodar `pulumi up` localmente contra o stack dev limpo.
**Critérios de aceite:**
- [ ] `pulumi up` do zero cria toda a infra (3 clusters, DB, Spaces, Helm releases)
- [ ] Zero erros no output
- [ ] Duration < 25 min
- [ ] Segundo `pulumi up` produz zero changes (idempotente)

**DoD Task 1.3:**
- Screenshot/log do `pulumi up` com `0 changes` na segunda execução
- Todos os pods em Running (exceto ImagePullBackOff esperado para platform images)

### Task 1.4: Validar `pulumi up` no CI
**Pré-requisito:** Task 1.3
**Ação:** Push para develop e verificar que o workflow funciona.
**Critérios de aceite:**
- [ ] Pipeline completa em < 25 min
- [ ] Zero erros
- [ ] `$GITHUB_STEP_SUMMARY` contém deploy health JSON
- [ ] Segundo push (sem mudanças infra) produz `0 changes`

**DoD Task 1.4:**
- Link para GitHub Actions run com status ✅
- Deploy health mostra `status: CLEAN`

---

## Fase 2: infra-bootstrap.yml (~100 linhas)

**Objetivo:** Bootstrap idempotente, testável, com pre/postconditions explícitas.

### Task 2.1: Extrair bootstrap para script
**Arquivo:** `infra/scripts/bootstrap-platform.sh` (novo)
**Mudança:** Extrair linhas 1033-1378 do infra-deploy.yml atual para script standalone.
**Critérios de aceite:**
- [ ] Script aceita `STACK` como input (env var)
- [ ] Cada seção é uma função nomeada:
  - `bootstrap_gitea()` — cria admin token + orgs
  - `bootstrap_argocd()` — cria loopzero-api account + apiKey
  - `bootstrap_secrets()` — cria K8s secrets (platform-tokens, repo-creds)
  - `bootstrap_database()` — seed via ephemeral pod
  - `bootstrap_tenant()` — aplica Tenant CR
  - `bootstrap_cloudflare()` — cria CF credentials secret + deploya Worker
- [ ] Cada função tem precondition check (e.g., "Gitea pod running?")
- [ ] Cada função tem postcondition check (e.g., "token gerado e funcional?")
- [ ] Zero `|| true` — cada operação sucede ou falha o script
- [ ] Script é idempotente — rodar 2x em sistema healthy não causa side effects
- [ ] Script funciona localmente (`STACK=dev bash infra/scripts/bootstrap-platform.sh`)

**DoD Task 2.1:**
- Script roda localmente contra o cluster dev
- Todos os tokens/secrets existem após execução
- Segunda execução é no-op (logs "already exists" para cada recurso)

### Task 2.2: Criar workflow infra-bootstrap.yml
**Arquivo:** `.github/workflows/infra-bootstrap.yml` (novo)
**Mudança:** Workflow reusable que chama o script.
**Critérios de aceite:**
- [ ] Arquivo tem ≤ 100 linhas
- [ ] Trigger: `workflow_call` (chamado por infra-deploy) + `workflow_dispatch` (manual)
- [ ] Input: `stack` (required)
- [ ] Steps:
  1. Checkout
  2. Install tools (doctl, kubectl, helm)
  3. Get kubeconfig from Pulumi stack output
  4. Run `bootstrap-platform.sh`
  5. Validate health (API reachable, Gitea reachable, controller running)
- [ ] Health check falha o pipeline se qualquer serviço não responde

**DoD Task 2.2:**
- Workflow YAML válido
- `workflow_dispatch` manual funciona (testado no GH Actions)

---

## Fase 3: infra-destroy.yml (~80 linhas)

**Objetivo:** Destroy seguro, stack-scoped, com approval.

### Task 3.1: Fix pre-destroy-cleanup.sh
**Arquivo:** `infra/scripts/pre-destroy-cleanup.sh`
**Mudança:** Substituir `'loopzero' in c['name']` por `c['name'].startswith('$PREFIX')` em todas as ocorrências.
**Critérios de aceite:**
- [ ] Todas as filtragens de clusters usam `startswith(prefix)` não `in`
- [ ] Todas as filtragens de databases usam `startswith(prefix)` não `in`
- [ ] PREFIX é derivado de `$STACK` + `$REGION` (e.g., `loopzero-infra-dev-sfo3`)
- [ ] Script aceita STACK e REGION como inputs

**DoD Task 3.1:**
- Grep por `'loopzero' in` retorna zero resultados no script
- Script testado com `STACK=dev REGION=sfo3` (dry-run mode)

### Task 3.2: Criar workflow infra-destroy.yml
**Arquivo:** `.github/workflows/infra-destroy.yml` (novo)
**Mudança:** Workflow dedicado para destroy.
**Critérios de aceite:**
- [ ] Arquivo tem ≤ 80 linhas
- [ ] Trigger: `workflow_dispatch` only
- [ ] Input: `stack` (required, choice: dev/homolog/prod)
- [ ] Environment: resolvido por stack (infra-dev, infra-homolog, infra-prod)
- [ ] Environment protection: reviewers required para TODOS os stacks (não só prod)
- [ ] Steps:
  1. Checkout
  2. Install tools
  3. Pulumi login
  4. List what will be deleted (summary)
  5. Pre-destroy cleanup (stack-scoped)
  6. Pulumi destroy
  7. Nuclear state reset (export → keep stack only → import)
- [ ] Output: lista de recursos deletados em `$GITHUB_STEP_SUMMARY`

**DoD Task 3.2:**
- Workflow YAML válido
- Environment protection configurado no GitHub (reviewers para infra-dev)

---

## Fase 4: infra-restart.yml (~50 linhas)

**Objetivo:** Restart pods após build de imagens.

### Task 4.1: Criar workflow infra-restart.yml
**Arquivo:** `.github/workflows/infra-restart.yml` (novo)
**Mudança:** Workflow trigger automático após build-images.
**Critérios de aceite:**
- [ ] Arquivo tem ≤ 50 linhas
- [ ] Trigger: `workflow_run` de "Build: Container Images" em branches develop/homolog/main
- [ ] Condição: só roda se build-images teve `conclusion: success`
- [ ] Steps:
  1. Install doctl + kubectl
  2. Auth + get kubeconfig do mgmt cluster
  3. `kubectl rollout restart` em: loopzero-api, tenant-controller, loopzero-controller, loopzero-cloud, loopzero-dashboard
  4. `kubectl rollout status --timeout=120s` para cada
- [ ] Falha se algum rollout não completa em 120s

**DoD Task 4.1:**
- Workflow YAML válido
- Testado: push de código app → build-images → restart → pods com nova imagem

---

## Fase 5: Validação end-to-end

### Task 5.1: Destroy + Up completo via CI
**Pré-requisito:** Todas as tasks anteriores
**Ação:**
1. Disparar `infra-destroy.yml` (stack=dev)
2. Aguardar completion
3. Disparar `infra-deploy.yml` (stack=dev, command=up) via push
4. Aguardar completion
5. Disparar `infra-bootstrap.yml` (stack=dev)
6. Aguardar completion
7. Verificar: todos os serviços running, API reachable, Gitea reachable

**Critérios de aceite:**
- [ ] Destroy: completa sem erros cross-environment
- [ ] Deploy: `pulumi up` completa na primeira tentativa (sem Pass 2)
- [ ] Bootstrap: todos os tokens/secrets criados
- [ ] Health: API, Gitea, ArgoCD, Grafana todos reachable
- [ ] Total time: < 30 min (destroy + up + bootstrap)

**DoD Task 5.1:**
- 3 workflow runs green consecutivos (destroy, deploy, bootstrap)
- Todos os pods Running (ou ImagePullBackOff para platform images — esperado)

### Task 5.2: Idempotência do deploy
**Ação:** Re-rodar `infra-deploy.yml` sem mudanças.
**Critérios de aceite:**
- [ ] `pulumi up` retorna `0 changes`
- [ ] Duration < 3 min
- [ ] Bootstrap é no-op (tokens já existem)

**DoD Task 5.2:**
- Screenshot do `0 changes` no GitHub Actions

### Task 5.3: Build + Restart flow
**Ação:** Push de mudança em `api/` (não infra).
**Critérios de aceite:**
- [ ] `build-images.yml` dispara e completa
- [ ] `infra-restart.yml` dispara automaticamente
- [ ] Pods reiniciam com nova imagem
- [ ] `infra-deploy.yml` NÃO dispara (path filter correto)

**DoD Task 5.3:**
- 2 workflow runs: build-images ✅ + infra-restart ✅
- `infra-deploy.yml` não aparece na lista de runs

---

## Fase 6: Cleanup

### Task 6.1: Deletar código morto
**Ação:** Remover artefatos do pipeline antigo.
**Critérios de aceite:**
- [ ] `infra-deploy.yml` antigo não existe (substituído pelo novo)
- [ ] Zero referências a `2-pass deploy`, `Pass 2`, `recovery sequence` no codebase
- [ ] `adopt-or-create.ts` warnings implementados (Task 1.2)
- [ ] CHANGELOG atualizado com a mudança

**DoD Task 6.1:**
- `grep -r "Pass 2\|2-pass\|recovery sequence" .github/` retorna zero resultados

---

## Ordem de Execução

```
Fase 1 (deploy limpo) ─── ~4 horas
  ├── Task 1.1: Novo infra-deploy.yml
  ├── Task 1.2: Warn adopt-or-create
  ├── Task 1.3: Validar localmente
  └── Task 1.4: Validar no CI
            │
Fase 2 (bootstrap) ─── ~3 horas
  ├── Task 2.1: Script bootstrap-platform.sh
  └── Task 2.2: Workflow infra-bootstrap.yml
            │
Fase 3 (destroy) ─── ~2 horas
  ├── Task 3.1: Fix pre-destroy-cleanup.sh
  └── Task 3.2: Workflow infra-destroy.yml
            │
Fase 4 (restart) ─── ~1 hora
  └── Task 4.1: Workflow infra-restart.yml
            │
Fase 5 (validação) ─── ~2 horas
  ├── Task 5.1: Destroy + Up + Bootstrap E2E
  ├── Task 5.2: Idempotência
  └── Task 5.3: Build + Restart flow
            │
Fase 6 (cleanup) ─── ~30 min
  └── Task 6.1: Deletar código morto
```

**Total estimado: 12-15 horas de trabalho + ~2 horas de tempo de espera (deploys).**

---

## Métricas de Sucesso

| Métrica | Antes | Depois | Meta |
|---------|-------|--------|------|
| Linhas infra-deploy.yml | 1539 | ~150 | ≤ 150 |
| `|| true` / `2>/dev/null` | 170 | ≤ 5 | ≤ 5 |
| State surgery ops no deploy | ~20 | 0 | 0 |
| `pulumi up` passes necessários | 2 (Pass 1 + Pass 2) | 1 | 1 |
| Deploy time (from-scratch) | 45+ min | ~20 min | < 25 min |
| Deploy time (no-change) | ~10 min | ~2 min | < 3 min |
| Local = CI | ❌ (200 linhas de diferença) | ✅ | ✅ |
| Workflows | 1 monolito | 4 focados | 4 |

---

## Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|--------------|---------|-----------|
| `pulumi up` falha por recurso que já existe (sem adopt-or-create) | Média | Médio | `resolveOrphans()` continua como safety net; se falhar, operator importa manualmente |
| Bootstrap falha mid-way | Média | Baixo | Cada função é idempotente; re-rodar o script completa o que faltou |
| CF DNS workaround (Command provider) para de funcionar | Baixa | Médio | Monitorar; quando CF provider for corrigido, migrar de volta |
| First deploy tem ImagePullBackOff por 10+ min | Alta | Baixo | Aceito; `build-images.yml` roda em paralelo e `infra-restart.yml` converge |
| Destroy cross-environment | Baixa (fix em Task 3.1) | Crítico | Filtro por prefix + approval gate + environment protection |
