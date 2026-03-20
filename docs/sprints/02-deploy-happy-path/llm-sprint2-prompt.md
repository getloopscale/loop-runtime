> **Voce e um engenheiro de software senior. Sua missao e fazer o Sprint 2 do LoopZero funcionar de verdade.**
> **Voce NAO termina ate que o exit gate passe. Sem excecoes.**

---

## CONTEXTO

LoopZero e um CLI-first Kubernetes PaaS. O usuario roda `loopzero deploy` e recebe uma URL com a aplicacao rodando. O codigo ja foi escrito, mas NUNCA foi validado end-to-end contra o projeto de referencia real.

**Seu trabalho:** Fazer o dogfood-monorepo deployar com sucesso. Nao importa quantas iteracoes leve. Voce implementa, testa, corrige, testa, ate passar.

---

## DOCUMENTOS OBRIGATORIOS — LEIA ANTES DE QUALQUER ACAO

Leia estes arquivos NA ORDEM antes de tocar qualquer codigo:

1. `docs/sprints/02-deploy-happy-path/north-star.md` — O QUE deve funcionar, regras inquebraveis
2. `docs/sprints/02-deploy-happy-path/session-report.md` — Erros ja cometidos (NAO repita)
3. `docs/sprints/02-deploy-happy-path/fix-plan.md` — Gaps conhecidos e correcoes planejadas
4. `docs/sprints/02-deploy-happy-path/dogfood-fix-plan.md` — Problemas do dogfooding
5. `CLAUDE.md` — Regras do projeto (locks, state machines, exit codes, termos proibidos)
6. `docs/technical.md` — Spec autoritativo (quando conflitar com qualquer outro doc, o spec vence)

---

## O PROJETO DE REFERENCIA

O exit gate e validado EXCLUSIVAMENTE contra:

```
infra/tests/e2e/fixtures/dogfood-monorepo/
```

Este e um monorepo Turborepo com npm workspaces:

| App | Tipo | Framework | Port | Health endpoint |
|-----|------|-----------|------|-----------------|
| `api` | server | NestJS | 3000 | `GET /health` → `{status: "ok"}` |
| `gateway` | server | Express | 4000 | `GET /health` → `{status: "ok"}` |
| `worker` | worker | Node.js | — | NENHUM (loop infinito) |
| `web` | frontend | Next.js | — | — |

Shared packages: `@dogfood/shared-config`, `@dogfood/shared-utils`

**Regra absoluta:** NUNCA teste com um app hello-world inventado na hora. SEMPRE use o dogfood-monorepo.

---

## EXIT GATE — O QUE DEVE PASSAR

O sprint SO esta completo quando TODOS estes passos funcionam em sequencia sem intervencao manual:

```bash
# 0. Infra rodando
task dev:up    # clusters k3d + dependencias
tilt up        # dev loop

# 1. Login
loopzero login --token $GITHUB_TOKEN

# 2. Preparar o dogfood-monorepo
cp -r infra/tests/e2e/fixtures/dogfood-monorepo /tmp/dogfood-test
cd /tmp/dogfood-test
git init && git add -A && git commit -m "initial"

# 3. Deploy REAL
loopzero deploy

# 4. Health checks passam
curl -f <URL_API>/health       # HTTP 200
curl -f <URL_GATEWAY>/health   # HTTP 200

# 5. JSON output correto
loopzero deploy --json
# 4 apps: api (deployed, url), gateway (deployed, url), worker (deployed, sem url), web (deployed)

# 6. Deploy parcial funciona
loopzero deploy --app api          # exit 0
loopzero deploy --app worker       # exit 0, sem health probe

# 7. Concurrent deploy rejeitado
# Terminal A: loopzero deploy --no-wait   → exit 0
# Terminal B: loopzero deploy --no-wait   → exit 3 (409)

# 8. Testes passam
task test:unit
task lint

# 9. Zero termos internos
grep -rn 'ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko' cli/src/ --include='*.ts' | grep -v test | grep -v node_modules
# Resultado: 0 matches
```

**Se QUALQUER passo falha, voce NAO terminou.**

---

## METODO DE TRABALHO — O LOOP

Voce trabalha em um loop ate o exit gate passar:

```
LOOP {
    1. DIAGNOSTICAR  → Qual e o proximo bloqueio?
    2. LOCALIZAR     → Onde no codigo esta o problema?
    3. SPEC CHECK    → O que o docs/technical.md diz sobre isso?
    4. TEST FIRST    → Escrever teste que falha (red)
    5. IMPLEMENTAR   → Corrigir o minimo necessario
    6. VALIDAR       → Rodar o teste (green)
    7. E2E CHECK     → Tentar o exit gate de novo

    Se exit gate passa → PARAR
    Se exit gate falha → Voltar ao passo 1 com o PROXIMO bloqueio
}
```

### Ordem de ataque recomendada

Cada fase desbloqueia a proxima. NAO pule fases.

```
FASE 1 — COMPILACAO
    O codigo compila? go build ./... e npx tsc --noEmit passam?
    Se nao: corrigir erros de compilacao primeiro.

FASE 2 — TESTES UNITARIOS
    task test:unit passa?
    Se nao: corrigir testes quebrados.
    NAO escreva codigo novo ate que os testes existentes passem.

FASE 3 — INFRA BOOT
    task dev:up funciona? tilt up mostra tudo verde?
    Se nao: corrigir configs de infra.
    Verificar: PostgreSQL, Gitea, MinIO, ArgoCD, Argo Workflows todos rodando.

FASE 4 — LOGIN + INIT
    loopzero login --token $TOKEN funciona?
    loopzero whoami mostra usuario?
    Se nao: corrigir auth flow.

FASE 5 — SOURCE UPLOAD
    cd /tmp/dogfood-test && loopzero deploy
    O source bundle e criado e uploadado?
    Se nao: corrigir git bundle creation ou upload endpoint.
    ATENCAO: git bundle deve usar --all (nao -1 HEAD).

FASE 6 — BUILD PIPELINE
    O Argo Workflow e submetido e executa?
    Os steps clone-repo, install-deps, build-apps, build-image passam?
    Se nao: corrigir WorkflowTemplate ou parametros.
    ATENCAO: volumeClaimTemplates (nao emptyDir) para compartilhar dados entre tasks.
    ATENCAO: Kaniko precisa de --insecure para registry HTTP e runAsUser: 0.

FASE 7 — CALLBACK
    O step notify-api chama POST /builds/:id/callback?
    A API processa o callback corretamente?
    Se nao: verificar URL de callback, secret, payload format.
    ATENCAO: callback apps deve ter formato {name, image_digest}, nao {name, type, port}.

FASE 8 — GITOPS + ARGOCD SYNC
    O workflow faz gitops-push para o Gitea?
    O ArgoCD sincroniza os manifests?
    Se nao: verificar credenciais do Gitea no ArgoCD, directory.recurse: true.
    ATENCAO: nomes de ArgoCD Application devem alinhar entre controller e API.

FASE 9 — HEALTH PROBE
    A API faz HTTP GET no /health das apps server?
    Workers sao skipped?
    Se nao: verificar HealthProber, URL construida corretamente.

FASE 10 — URL ACESSIVEL
    curl <URL>/health retorna 200?
    Se nao: verificar Traefik Ingress, DNS/hosts, namespace correto.

FASE 11 — CLI OUTPUT
    loopzero deploy --json retorna schema correto com 4 apps?
    Exit codes corretos?
    Zero termos internos?
    Se nao: corrigir rendering, exit code mapping, filtrar termos.
```

---

## REGRAS INQUEBRAVEIS

### O QUE FAZER

1. **SEMPRE testar contra `infra/tests/e2e/fixtures/dogfood-monorepo/`** — nunca um hello-world
2. **SEMPRE ler `docs/technical.md` antes de implementar** — o spec e a verdade
3. **SEMPRE escrever teste ANTES do codigo** — red → green → refactor
4. **SEMPRE usar implementacoes reais** — contra Gitea real, Argo real, ArgoCD real, PostgreSQL real
5. **SEMPRE usar `pg_advisory_xact_lock`** (transaction-scoped) — nunca session-scoped
6. **SEMPRE fazer concurrent-build check DENTRO do lock** — nunca fora
7. **SEMPRE tratar erros** — `_ = funcao()` e PROIBIDO
8. **SEMPRE emitir eventos SSE em cada transicao de estado**
9. **SEMPRE health probe ANTES de marcar release ACTIVE**
10. **SEMPRE usar linguagem de produto no CLI** — "deploy", "build", "team"

### O QUE NAO FAZER

1. **NUNCA usar Fake*, Mock*, Stub* em codigo de producao** — fakes SO em `_test.go`
2. **NUNCA validar com hello-world** — o exit gate e o dogfood-monorepo
3. **NUNCA implementar sem ler o spec** — ja custou 3 implementacoes descartadas
4. **NUNCA usar `pg_advisory_lock`** (session-scoped) — causa deadlock
5. **NUNCA fazer update direto de status** — usar `statemachine.Transition(event)`
6. **NUNCA engolir erro** — logar com contexto (build_id, tenant_id, etc)
7. **NUNCA expor termos internos no CLI** — ArgoCD, Gitea, MinIO, CRD, tenant_id, advisory lock PROIBIDOS
8. **NUNCA usar `emptyDir` entre DAG tasks do Argo** — usar `volumeClaimTemplates`
9. **NUNCA assumir que clusters k3d se enxergam por DNS** — usar NodePort + IPs
10. **NUNCA marcar como pronto sem rodar o exit gate completo**
11. **NUNCA fazer `git checkout` ou `git revert`**
12. **NUNCA commitar binarios compilados**
13. **NUNCA duplicar logica de state machine** — fonte unica em `pkg/statemachine/`

---

## DIAGNOSTICO RAPIDO — QUANDO ALGO FALHA

### Deploy trava sem output

```bash
# API esta rodando?
kubectl --context k3d-loopzero-mgmt logs -n loopzero-system deploy/loopzero-api --tail=50

# Port-forward ativo?
curl -s http://localhost:3001/api/v1/health
```

### Build falha no Argo

```bash
# Ver workflows
kubectl --context k3d-loopzero-build -n argo get wf

# Ver logs do workflow
kubectl --context k3d-loopzero-build -n argo logs <workflow-pod> -c main

# Problemas comuns:
# - clone-repo falha → Gitea URL errada ou cross-cluster networking
# - install-deps falha → package-lock.json ausente no bundle
# - build-image falha → Kaniko sem --insecure ou sem runAsUser: 0
# - gitops-push falha → Gitea credentials no workflow
# - notify-api falha → API URL errada ou callback secret
```

### ArgoCD nao sincroniza

```bash
# Ver applications
kubectl --context k3d-loopzero-mgmt -n argocd get applications

# Ver status de sync
kubectl --context k3d-loopzero-mgmt -n argocd get app <name> -o jsonpath='{.status.sync.status}'

# Problemas comuns:
# - "0 resources" → falta directory.recurse: true
# - "repository not found" → falta repocreds no ArgoCD para Gitea
# - "namespace not found" → TenantController nao reconciliou
```

### Pod nao inicia no workload cluster

```bash
# Ver pods
kubectl --context k3d-loopzero-workload get pods -A

# Ver eventos
kubectl --context k3d-loopzero-workload describe pod <pod> -n <namespace>

# Problemas comuns:
# - ErrImagePull → registry URL errada no manifest ou registry nao acessivel
# - CrashLoopBackOff → app crasha (ver logs)
# - Pending → sem resources (verificar resource limits)
```

### Health probe falha

```bash
# Testar manualmente de dentro do cluster
kubectl --context k3d-loopzero-workload exec -it deploy/<deploy> -n <ns> -- wget -qO- localhost:3000/health

# De fora via port-forward
kubectl --context k3d-loopzero-workload port-forward -n <ns> svc/<svc> 13000:3000
curl localhost:13000/health

# Problemas comuns:
# - App nao escuta na PORT do env → verificar que PORT e injetada no manifest
# - /health retorna 404 → path errado no probe
# - Connection refused → container ainda iniciando (backoff insuficiente)
```

---

## ESTADO ATUAL DO CODIGO

O codigo ja foi escrito. Todas as implementacoes reais existem:

| Componente | Arquivo | Status |
|-----------|---------|--------|
| SourceStore (Gitea) | `api/internal/source/gitea_store.go` | Implementado |
| WorkflowSubmitter (Argo) | `api/internal/argo/workflow_submitter.go` | Implementado |
| SyncClient (ArgoCD) | `api/internal/argocd/sync_client.go` | Implementado |
| HealthProber (HTTP) | `api/internal/health/prober.go` | Implementado |
| BuildOrchestrator | `api/internal/services/build_orchestrator.go` | Implementado |
| BuildReaper | `api/internal/services/build_reaper.go` | Implementado |
| Environment Lock | `api/internal/lock/environment_lock.go` | Implementado |
| SSE Stream | `api/internal/sse/stream_handler.go` | Implementado |
| CLI Deploy | `cli/src/commands/deploy.ts` | Implementado |
| CLI SSE Client | `cli/src/api/sse-client.ts` | Implementado |
| State Machine | `pkg/statemachine/build.go` | Centralizado |
| WorkflowTemplate | `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml` | Implementado |
| Tilt Deployment | `infra/tilt/api-deployment.yaml` | Configurado |

**O problema NAO e falta de codigo. E que NUNCA foi validado end-to-end contra o dogfood-monorepo.**

Problemas reais so aparecem com infraestrutura real. Espere encontrar:
- Cross-cluster networking quebrado
- Parametros de workflow errados
- Nomes desalinhados entre componentes
- Manifests com namespace errado
- Credenciais faltando
- Volumes nao compartilhados
- Callbacks com payload errado

Cada problema destes e uma iteracao do loop. Diagnostique, corrija, valide, repita.

---

## FLUXO REAL DO DEPLOY (para referencia)

```
CLI                          API                         Build Cluster              Workload Cluster
 |                            |                            |                          |
 |-- git bundle create -------|                            |                          |
 |                            |-- git push → Gitea          |                          |
 |                            |                            |                          |
 |-- POST /builds ------------|                            |                          |
 |   (Idempotency-Key)        |-- Lock 1: advisory lock     |                          |
 |                            |-- check concurrent build    |                          |
 |                            |-- create Build (QUEUED)     |                          |
 |                            |-- submit Argo Workflow -----|                          |
 |                            |-- Build → RUNNING           |-- clone-repo (Gitea)      |
 |                            |-- libera Lock 1             |-- install-deps (npm ci)   |
 |  <-- 202 {build_id} -------|                            |-- build-apps              |
 |                            |                            |-- build-image (Kaniko)    |
 |-- GET /builds/:id/stream    |                            |-- generate-manifests      |
 |   (SSE)                    |                            |-- gitops-push → Gitea     |
 |  <-- build.started         |                            |-- save-cache → MinIO      |
 |  <-- build.step.*          |                            |                          |
 |  <-- build.succeeded       |  <-- POST /callback -------|                          |
 |                            |   (202 imediato)           |                          |
 |                            |-- Lock 2: advisory lock     |                          |
 |                            |-- create Release (READY)    |                          |
 |  <-- release.pending       |-- Release → PENDING         |                          |
 |                            |-- ArgoCD sync --------------|--------------------------|
 |                            |                            |                          |-- Deployment
 |                            |                            |                          |-- Service
 |  <-- release.health_check  |-- health probe HTTP GET ----|--------------------------|
 |      {status: "checking"}  |                            |                          |-- /health → 200
 |  <-- release.health_check  |                            |                          |
 |      {status: "ok"}        |-- Release → ACTIVE          |                          |
 |                            |-- previous → SUPERSEDED     |                          |
 |                            |-- libera Lock 2             |                          |
 |  <-- deploy.completed      |                            |                          |
 |      {deploy_id, urls}     |                            |                          |
 |                            |                            |                          |
 |-- Mostra URLs ao usuario    |                            |                          |
 |-- exit 0                    |                            |                          |
```

---

## ENTREGAVEIS FINAIS

Quando o exit gate passar, produza:

1. **Lista de todos os problemas encontrados e corrigidos** (com arquivo e linha)
2. **Evidencia do exit gate passando** (output real dos comandos)
3. **Testes adicionados** (se algum teste novo foi necessario)
4. **Changelog atualizado** (`CHANGELOG.md` secao `[Unreleased]`)

---

## COMECE AGORA

1. Leia `docs/sprints/02-deploy-happy-path/north-star.md`
2. Leia `CLAUDE.md`
3. Verifique se `go build ./...` e `npx tsc --noEmit` passam
4. Verifique se `task test:unit` passa
5. Tente o exit gate (copie dogfood-monorepo, faca deploy)
6. O primeiro erro que aparecer e seu primeiro trabalho
7. Loop ate passar
