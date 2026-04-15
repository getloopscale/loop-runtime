# Pipeline Simplification Plan (v2)

> Plano executável para reescrever a pipeline de infra de 1539 linhas para 5 workflows limpos.
> Baseado em MEET-049, MEET-050 e revisão de segurança operacional.

**Status:** Code Complete (Phases 1-4 implemented, Phase 5 E2E pending validation on DO, Phase 6 cleanup done)
**Owner:** Platform Team
**Priority:** P0 — bloqueia todas as operações de infra
**Estimativa:** 2-3 dias
**Revisão:** v2 — incorpora 8 ajustes da revisão de segurança

---

## Pré-condições (validadas)

- [x] State limpo (nuclear reset feito 2026-04-15)
- [x] Infra real destruída (clusters, DB, tunnel deletados)
- [x] CI pipeline green (15/15 jobs)
- [x] Build Images pipeline green (11/11 jobs)
- [x] Meeting records aprovados (MEET-049 + MEET-050)
- [x] `protect: isProd` condicional implementado (clusters + DB)

---

## Princípios (da revisão de segurança)

1. **infra-deploy.yml NÃO aceita destroy** — commands permitidos: `up`, `preview`, `refresh`
2. **Nuclear state reset NÃO faz parte do destroy normal** — vive em workflow break-glass separado
3. **Zero erro mascarado** — em vez de `|| true`, usar helpers que tratam códigos esperados (e.g., "already exists") e falham em todo o resto
4. **4 estados distintos:** infra provisionada → imagens publicadas → bootstrap concluído → plataforma saudável
5. **CI gate obrigatório** antes de restart de pods (imagens só reiniciam se CI do mesmo SHA passou)
6. **Limites de linhas são alvo**, não critério obrigatório — critérios funcionais são obrigatórios

---

## Inventário de Workflows (5)

| Workflow | Trigger | Alvo linhas | Responsabilidade |
|----------|---------|-------------|------------------|
| `infra-deploy.yml` | push infra/**, dispatch(up/preview/refresh) | ~150 | `pulumi up` — nada mais |
| `infra-bootstrap.yml` | workflow_call + dispatch | ~120 | Tokens, secrets, seed, Tenant CR |
| `infra-destroy.yml` | dispatch only, approval required | ~100 | Destroy stack-scoped, sem state reset |
| `infra-restart.yml` | workflow_run (build-images success + CI success) | ~60 | Restart pods com nova imagem |
| `infra-state-reset.yml` | dispatch only, break-glass | ~40 | Nuclear state reset (emergência) |

---

## Fase 1: infra-deploy.yml

**Objetivo:** `pulumi up` puro, testável local e CI de forma idêntica.

### Task 1.1: Escrever novo infra-deploy.yml
**Arquivo:** `.github/workflows/infra-deploy.yml`
**Mudança:** Reescrever do zero.
```yaml
jobs:
  resolve:     # branch → stack/env/backend
  validate:    # npm ci + tsc + npm test
  deploy:      # pulumi login + config set + pulumi up
  bootstrap:   # job uses: ./.github/workflows/infra-bootstrap.yml
               # needs: [resolve, deploy]
               # if: needs.resolve.outputs.command == 'up'
               # secrets: inherit
```
**Critérios de aceite:**
- [x] Commands aceitos: `up`, `preview`, `refresh` — **NÃO aceita `destroy`**
- [x] Zero `pulumi cancel` no workflow
- [x] Zero `pulumi state delete` no workflow
- [x] Zero `PULUMI_K8S_DELETE_UNREACHABLE` no workflow
- [x] Zero `pulumi stack export | python3` no workflow
- [x] Zero `|| true` em operações de state — erros esperados tratados via `if ! cmd; then echo "expected: reason"; fi`
- [x] `helm dependency update` roda antes do `pulumi up`
- [x] `cloudflare:apiToken` setado no config antes do `pulumi up`
- [x] Trigger: push to develop/homolog/main em `infra/**`
- [x] Trigger: workflow_dispatch com `command: choice [up, preview, refresh]` (sem destroy)
- [x] Environment resolvido por branch (infra-dev, infra-homolog, infra-prod)
- [x] Concurrency: `cancel-in-progress: false` por stack
- [x] Bootstrap como **job** (não step) usando `uses: ./.github/workflows/infra-bootstrap.yml`
  - `needs: [resolve, deploy]`
  - `if: needs.resolve.outputs.command == 'up'`
  - `with: { stack: ${{ needs.resolve.outputs.stack }} }`
  - `secrets: inherit`
- [x] Deploy health output em `$GITHUB_STEP_SUMMARY`:
  - Pulumi exit code, duration, resources created/updated/deleted/same

**DoD:**
- YAML válido
- Não contém: `state delete`, `stack export.*python`, `DELETE_UNREACHABLE`, `helm uninstall`, `Pass 2`, `destroy`

### Task 1.2: Melhorar adopt-or-create.ts warnings ✅
**Arquivo:** `infra/pulumi/src/core/adopt-or-create.ts`
**Mudança:** Lookups que falham logam warning em vez de silenciar.
**Critérios de aceite:**
- [x] `catch(e) { pulumi.log.warn(...); return undefined }` em todas as funções
- [x] `npx tsc --noEmit` passa

**DoD:** Warnings visíveis no Pulumi output quando lookup falha.

### Task 1.3: Validar `pulumi up` localmente (state limpo → infra completa)
**Critérios de aceite:**
- [ ] `pulumi up` do zero cria toda a infra sem erros
- [ ] Segundo `pulumi up` = `0 changes`
- [ ] Estado: **infra provisionada** (clusters Running, DB online, Helm releases deployed)
- [ ] Pods platform em ImagePullBackOff (esperado — imagens não existem ainda)

**DoD:** Log de `0 changes` na segunda execução.

### Task 1.4: Validar `pulumi up` no CI
**Critérios de aceite:**
- [ ] Pipeline completa sem erros
- [ ] `$GITHUB_STEP_SUMMARY` contém deploy health
- [ ] Estado: **infra provisionada**

**DoD:** Link para GitHub Actions run ✅.

---

## Fase 2: infra-bootstrap.yml

**Objetivo:** Bootstrap idempotente, testável, com pre/postconditions.

### Task 2.1: Extrair bootstrap para script ✅
**Arquivo:** `infra/scripts/bootstrap-platform.sh` (novo)
**Mudança:** Extrair linhas 1033-1378 do infra-deploy.yml antigo para script standalone.
**Critérios de aceite:**
- [x] Script aceita `STACK` como input (env var)
- [x] Cada seção é uma função nomeada:
  - `bootstrap_gitea()`, `bootstrap_argocd()`, `bootstrap_secrets()`,
  - `bootstrap_database()`, `bootstrap_tenant()`, `bootstrap_cloudflare()`
- [x] Cada função tem precondition check (e.g., "Gitea pod running?")
- [x] Cada função tem postcondition check (e.g., "token funcional?")
- [x] Zero erro mascarado — comandos idempotentes usam:
  - `kubectl apply` (idempotent by design)
  - Checks pré/pós com helper: `ensure_exists "resource" "check_cmd" "create_cmd"`
  - Códigos esperados tratados (e.g., `409 Conflict` → "already exists", continua)
  - Qualquer outro erro → script falha
- [x] Script é idempotente — 2x em sistema healthy = no side effects
- [x] Funciona localmente: `STACK=dev bash infra/scripts/bootstrap-platform.sh`

**DoD:** Script roda localmente. Segunda execução é no-op.

### Task 2.2: Criar workflow infra-bootstrap.yml ✅
**Arquivo:** `.github/workflows/infra-bootstrap.yml` (novo)
**Critérios de aceite:**
- [x] Trigger: `workflow_call` (inputs: stack) + `workflow_dispatch` (manual)
- [x] Steps: checkout → tools → kubeconfig → script → health check
- [x] Health check FALHA o pipeline se serviço não responde
- [x] Zero `continue-on-error: true`
- [x] Estado final: **bootstrap concluído** (tokens existem, seed aplicado, Tenant CR criado)

**DoD:** Workflow funciona via `workflow_dispatch` manual.

---

## Fase 3: infra-destroy.yml

**Objetivo:** Destroy seguro, stack-scoped, com approval, SEM state reset.

### Task 3.1: Fix pre-destroy-cleanup.sh ✅
**Arquivo:** `infra/scripts/pre-destroy-cleanup.sh`
**Critérios de aceite:**
- [x] Todas as filtragens usam `startswith(prefix)` não `'loopzero' in name`
- [x] PREFIX derivado de `$STACK` + `$REGION`
- [x] Zero risco cross-environment

**DoD:** `grep "'loopzero' in" infra/scripts/pre-destroy-cleanup.sh` retorna zero.

### Task 3.2: Criar workflow infra-destroy.yml ✅
**Arquivo:** `.github/workflows/infra-destroy.yml` (novo)
**Critérios de aceite:**
- [x] Trigger: `workflow_dispatch` only
- [x] Input: `stack` (required, choice: dev/homolog/prod)
- [x] Environment protection: reviewers required para TODOS os stacks
- [x] `protect: isProd` nos clusters/DB — destroy em prod requer `pulumi state unprotect` explícito (step documentado, não automático)
- [x] Steps:
  1. List what will be deleted (summary) — **DRY RUN FIRST**
  2. Pre-destroy cleanup (stack-scoped)
  3. `pulumi destroy --yes`
  4. Validação pós-destroy (listar recursos cloud que sobreviveram)
- [x] **NÃO faz nuclear state reset** — state fica como `pulumi destroy` deixou
- [x] Output: lista de recursos deletados + remanescentes em `$GITHUB_STEP_SUMMARY`

**DoD:** Workflow YAML válido. Environment protection configurado.

### Task 3.3: Criar workflow infra-state-reset.yml (break-glass) ✅
**Arquivo:** `.github/workflows/infra-state-reset.yml` (novo)
**Critérios de aceite:**
- [x] Trigger: `workflow_dispatch` only
- [x] Input: `stack` + `confirm_text` (deve digitar "RESET STATE FOR {stack}")
- [x] Steps: export → keep stack only → import
- [x] Usado APENAS quando state está corrompido e não há outra opção
- [x] Log: "State reset performed by {actor} at {time} for reason: {confirm_text}"

**DoD:** Workflow requer input textual de confirmação.

---

## Fase 4: infra-restart.yml

**Objetivo:** Restart pods após build de imagens, COM gate de CI.

### Task 4.1: Criar workflow infra-restart.yml ✅
**Arquivo:** `.github/workflows/infra-restart.yml` (novo)
**Critérios de aceite:**
- [x] Trigger: `workflow_run` de "Build: Container Images" com `conclusion: success`
- [x] **Gate de CI:** antes do restart, verificar que CI workflow do mesmo SHA está green:
  ```yaml
  - name: Verify CI passed for this SHA
    run: |
      STATUS=$(gh api repos/$REPO/commits/$SHA/check-suites --jq '.check_suites[] | select(.app.slug=="github-actions") | .conclusion' | head -1)
      if [ "$STATUS" != "success" ]; then
        echo "::error::CI not green for $SHA — skipping restart"
        exit 1
      fi
  ```
- [x] Steps: auth → kubeconfig → rollout restart → rollout status
- [x] Falha se rollout não completa em 120s
- [x] Estado final: **plataforma saudável** (pods running com nova imagem)

**DoD:** Testado: push código → build → CI green → restart → pods novos.

---

## Fase 5: Validação end-to-end

### Task 5.1: Destroy + Up + Bootstrap E2E
**Ação:**
1. Disparar `infra-destroy.yml` (stack=dev) — se infra existir
2. Disparar `infra-deploy.yml` (command=up) via push
3. Aguardar: estado = **infra provisionada** (pods ImagePullBackOff OK)
4. Disparar `infra-bootstrap.yml` (stack=dev)
5. Aguardar: estado = **bootstrap concluído**
6. Disparar `build-images.yml` (component=all)
7. `infra-restart.yml` dispara automaticamente
8. Aguardar: estado = **plataforma saudável**

**Critérios de aceite:**
- [ ] Deploy: `pulumi up` primeira tentativa, zero retries
- [ ] Bootstrap: todos tokens/secrets criados
- [ ] Health: API, Gitea, ArgoCD, Grafana reachable
- [ ] Total: < 35 min (deploy + bootstrap + build + restart)

**DoD:** 4 workflow runs green consecutivos.

### Task 5.2: Idempotência
- [ ] `pulumi up` = `0 changes`
- [ ] Bootstrap = no-op (tokens já existem)

### Task 5.3: Build + Restart flow (sem infra change)
- [ ] Push em `api/` → build-images ✅ → CI green → restart ✅
- [ ] `infra-deploy.yml` NÃO dispara (path filter)

---

## Fase 6: Cleanup

### Task 6.1: Deletar código morto ✅
- [x] Zero referências a `Pass 2`, `2-pass`, `recovery sequence` no codebase (only in docs/plans and meeting records — no code refs)
- [x] CHANGELOG atualizado
- [x] `docs/plans/pipeline-simplification.md` status = Code Complete

---

## Ordem de Execução

```
Fase 1 (deploy) ─── ~4h
  ├── 1.1: Novo infra-deploy.yml
  ├── 1.2: adopt-or-create warnings
  ├── 1.3: Validar local
  └── 1.4: Validar CI
            │
Fase 2 (bootstrap) ─── ~3h
  ├── 2.1: Script bootstrap-platform.sh
  └── 2.2: Workflow infra-bootstrap.yml
            │
Fase 3 (destroy) ─── ~2h
  ├── 3.1: Fix pre-destroy-cleanup.sh
  ├── 3.2: Workflow infra-destroy.yml
  └── 3.3: Workflow infra-state-reset.yml
            │
Fase 4 (restart) ─── ~1h
  └── 4.1: Workflow infra-restart.yml (com CI gate)
            │
Fase 5 (validação) ─── ~2h
  ├── 5.1: E2E (destroy→up→bootstrap→build→restart)
  ├── 5.2: Idempotência
  └── 5.3: Build+Restart flow
            │
Fase 6 (cleanup) ─── ~30min
  └── 6.1: Código morto
```

---

## Métricas de Sucesso

| Métrica | Antes | Depois |
|---------|-------|--------|
| Linhas infra-deploy.yml | 1539 | ~150 |
| Erros mascarados (`\|\| true` + `2>/dev/null`) | 170 | 0 |
| State surgery ops no deploy | ~20 | 0 |
| `pulumi up` passes | 2 (Pass 1 + 2) | 1 |
| Deploy time (from-scratch) | 45+ min | ~20 min |
| Deploy time (no-change) | ~10 min | ~2 min |
| Local = CI | ❌ | ✅ |
| Workflows | 1 monolito | 5 focados |
| Destroy cross-env risk | ❌ CRÍTICO | ✅ stack-scoped |
| CI gate antes de restart | ❌ | ✅ |

---

## Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|--------------|---------|-----------|
| `pulumi up` falha por recurso existente | Média | Médio | `resolveOrphans()` como safety net; operator importa se falhar |
| Bootstrap falha mid-way | Média | Baixo | Funções idempotentes; re-rodar completa o que faltou |
| CF DNS workaround para de funcionar | Baixa | Médio | Monitorar; migrar quando CF provider for corrigido |
| First deploy ImagePullBackOff >10min | Alta | Baixo | Aceito; build-images + restart convergem |
| Destroy cross-environment | Baixa | Crítico | Prefix filter + approval + env protection |
| protect:true bloqueia destroy em prod | Alta (intencional) | Baixo | Documentado; `pulumi state unprotect` como step manual |
| State homolog/prod com DNS antigos (cloudflare.DnsRecord) | Média | Médio | Homolog/prod: state reset antes de migrar. Dev: já limpo |
