# Sprint 2 — North Star: O Que Importa e Nada Mais

> **Para:** Todo o time (CLI, API, Operators, Infra)
> **Motivo:** O time se perdeu múltiplas vezes durante a Sprint 2. Este documento existe para que isso não aconteça de novo.
> **Regra de uso:** Antes de abrir qualquer arquivo de código, leia este documento. Se o que você vai fazer não está aqui, **não faça**.

---

## 1. O OBJETIVO (Uma Frase)

**Um usuário roda `loopzero deploy` e recebe uma URL com a aplicação rodando.**

Não é "a API aceita requests". Não é "o build roda". Não é "o pod existe". É:

```
loopzero login → loopzero init → loopzero deploy → curl URL → HTTP 200
```

Se o `curl` no final não retorna 200, o sprint NÃO está completo. Tudo o mais é meio, não fim.

---

## 2. O QUE "PRONTO" SIGNIFICA (Exit Gate)

O sprint SÓ está completo quando TODOS estes passos funcionam em sequência, sem intervenção manual:

```bash
# 1. Login com token GitHub real
loopzero login --token $GITHUB_TOKEN

# 2. Init em projeto Node.js
mkdir /tmp/test-app && cd /tmp/test-app
cat > package.json << 'EOF'
{"name":"app","scripts":{"start":"node server.js"}}
EOF
cat > server.js << 'EOF'
require("http").createServer((req,res) => {
  if (req.url === "/health") { res.writeHead(200); res.end("ok"); return; }
  res.writeHead(200); res.end("hello from loopzero");
}).listen(process.env.PORT || 3000);
EOF
npm install --package-lock-only
git init && git add -A && git commit -m "init"
loopzero init --yes --name test-app

# 3. Deploy REAL (não simulado, não fake)
loopzero deploy

# 4. URL funciona
curl -f <URL_RETORNADA>/health   # HTTP 200
```

**Se o passo 4 falha, nada mais importa.**

---

## 3. APRENDIZADOS (Erros Que Já Cometemos — Não Repita)

### 3.1. Fakes em código de produção — O ERRO MAIS GRAVE

O deploy inteiro foi "implementado" com 9 Fake* structs em `main.go` e no operator. O exit gate nunca foi satisfeito. O time achou que estava pronto. Não estava.

**Fato:** Nenhum dos 10 problemas reais (cross-cluster networking, PVC sharing, Kaniko root, callback payload, ArgoCD credentials, namespace alignment) teria sido encontrado com fakes.

**Consequência:** ~3 horas de retrabalho para substituir cada fake por implementação real.

### 3.2. Implementar sem ler o spec

O `SourceStore` foi implementado 3 vezes:
1. MinIO (errado — spec §7.1 diz Gitea)
2. Gitea Contents API (errado — commitava blob binário, não clonável)
3. Git push via CLI (correto)

O `GitOpsPusher` foi implementado na API quando o spec §10.1 diz que o workflow faz o gitops-push.

**Custo:** 3 implementações descartadas.

### 3.3. Nomes desalinhados entre componentes

Três bugs causados por naming inconsistente:
- ArgoCD Application: controller `{tenant}-{env}`, API `{tenant}-{project}-{env}`
- Gitea repo URL: controller `/{tenant}/`, deveria ser `/loopzero/`
- Manifest namespace: workflow `{tenant}-{project}-{env}`, controller `{tenant}-{env}`

### 3.4. Cross-cluster networking em k3d

Clusters k3d rodam em Docker networks separadas. Pods de um cluster NÃO alcançam serviços de outro via DNS. Soluções aplicadas:
- `docker network connect` entre nodes
- NodePort nos serviços cross-cluster
- IPs de containers Docker nas env vars

### 3.5. Argo Workflows DAG tasks NÃO compartilham emptyDir

Cada DAG task roda em pod separado. `emptyDir` morre com o pod. Solução: `volumeClaimTemplates` (PVCs reais).

### 3.6. DIP funcionou perfeitamente

A troca de fakes por implementações reais no `main.go` não alterou NENHUMA linha do `BuildOrchestrator`. As interfaces isolaram a lógica de negócio da infraestrutura.

---

## 4. TESTES OBRIGATÓRIOS

Estes testes DEVEM existir e DEVEM passar. Sem exceção.

### 4.1. Testes Unitários — API

| Teste | Arquivo | O que valida |
|-------|---------|-------------|
| Submit happy path | `build_orchestrator_test.go` | QUEUED → RUNNING → SUCCEEDED → ACTIVE |
| Concurrent build → 409 | `build_orchestrator_test.go` | Check DENTRO do lock, não fora |
| Callback com Build QUEUED (EC-N) | `build_orchestrator_test.go` | QUEUED→RUNNING→SUCCEEDED atômico |
| Callback duplicado → no-op | `build_orchestrator_test.go` | Build já terminal, sem duplicação |
| Health probe falha → SYNC_FAILED | `build_orchestrator_test.go` | Reason: HEALTH_PROBE_FAILED |
| Worker skip probe | `build_orchestrator_test.go` | Workers sem URL, sem probe |
| Build timeout → FAILED | `build_orchestrator_test.go` | Reason: BUILD_TIMEOUT |
| Activation timeout → SYNC_FAILED | `build_orchestrator_test.go` | Reason: ACTIVATION_TIMEOUT |
| Callback endpoint auth | `builds_route_test.go` | Shared secret, não OAuth |
| Callback apps vazio → 400 | `builds_route_test.go` | Succeeded + zero apps = inválido |
| Environment uppercase → 400 | `builds_route_test.go` | `PRODUCTION` rejeitado |
| Health prober retries | `health_prober_test.go` | 500, 500, 200 → sucesso na 3a |
| Build reaper (se existir) | `build_reaper_test.go` | Builds stuck são limpos |
| Source upload válido | `source_upload_test.go` | Bundle aceito, SHA retornado |
| Source upload sem loopzero.yaml → 422 | `source_upload_test.go` | Rejeição com mensagem clara |
| IsBuildTerminal centralizado | `pkg/statemachine/build_test.go` | SUCCEEDED/FAILED/CANCELLED = true |

### 4.2. Testes Unitários — CLI

| Teste | Arquivo | O que valida |
|-------|---------|-------------|
| Deploy happy path | `deploy.test.ts` | Submit + stream + URLs |
| Build failure → exit 2 | `deploy.test.ts` | Exit code correto |
| Deploy failure → exit 3 | `deploy.test.ts` | Exit code correto |
| Auth error → exit 4 | `deploy.test.ts` | Exit code correto |
| Partial failure (2+ apps) → exit 5 | `deploy.test.ts` | DA-3: somente com 2+ apps |
| Single app failure → exit 2 (não 5) | `deploy.test.ts` | DA-3 |
| `--no-wait` → exit 0 | `deploy.test.ts` | DA-2: retorna após submit |
| `--json` schema match | `deploy.test.ts` | Spec §0.3 |
| No `.git` → exit 1 | `deploy.test.ts` | Mensagem clara |
| No `loopzero.yaml` → exit 1 | `deploy.test.ts` | Mensagem sugere `loopzero init` |
| `--env PRODUCTION` → exit 1 | `deploy.test.ts` | DA-10: lowercase only |
| SSE reconnect com Last-Event-ID | `sse-client.test.ts` | Transparente para o usuário |
| SSE fallback polling | `sse-client.test.ts` | JOURNAL_EXPIRED → polling |
| Zero termos internos | `deploy.test.ts` | Rule 3 |

### 4.3. Testes de Integração — API

| Teste | Arquivo | O que valida |
|-------|---------|-------------|
| Dois deploys concorrentes | `concurrent_builds_test.go` | Exatamente um 202, um 409 |
| Idempotency replay | `concurrent_builds_test.go` | Mesma key = mesma response |
| Idempotency mismatch → 422 | `concurrent_builds_test.go` | Key + payload diferente |
| Crash do holder → lock liberado | `lock_crash_recovery_test.go` (se existir) | pg_advisory_xact_lock auto-release |

### 4.4. Teste E2E (o que prova que o sprint está completo)

| Teste | O que valida |
|-------|-------------|
| Deploy single app | Pod rodando, URL retorna 200 |
| Segundo deploy usa cache | Build mais rápido ou log indica cache hit |
| Dois deploys simultâneos | Um 202, um rejeitado com exit 3 |

### 4.5. Verificações Automáticas

```bash
# Todos DEVEM retornar sucesso/zero:
task test:unit
task lint
task validate:manifests

# Zero termos internos no CLI:
grep -rn 'ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko' cli/src/ --include='*.ts' | grep -v test | grep -v node_modules
# Resultado: 0 matches

# Zero fakes em código de produção:
grep -rn 'Fake\|fake' api/cmd/main.go api/internal/adapters/
# Resultado: 0 matches

# Zero isBuildTerminal local:
grep -rn 'func isBuildTerminal' api/
# Resultado: 0 matches
```

---

## 5. REGRAS INQUEBRÁVEIS

### O QUE FAZER

| # | Regra | Por quê |
|---|-------|---------|
| F1 | **Ler o spec (`docs/technical.md`) ANTES de implementar qualquer coisa** | Implementamos SourceStore 3 vezes porque não lemos o spec. Custo: ~3h de retrabalho. |
| F2 | **Usar implementações reais contra infraestrutura real** | Fakes escondem 100% dos problemas reais. Os 10 problemas de infra que resolvemos NUNCA teriam aparecido com fakes. |
| F3 | **Escrever o teste ANTES do código** | Red → Green → Refactor. Sem exceção. Se o teste não falha primeiro, ele não prova nada. |
| F4 | **Rodar `loopzero deploy` de verdade antes de considerar qualquer coisa "pronta"** | O exit gate é `curl URL → 200`. Se não testou isso, não está pronto. |
| F5 | **Usar `pg_advisory_xact_lock` (transaction-scoped)** | Session-scoped persiste em connection pool reuse → deadlocks. Spec §4.2. |
| F6 | **Fazer o concurrent-build check DENTRO do lock** | Fora do lock = race condition entre check e insert. Spec §4.2. |
| F7 | **Alinhar nomes entre componentes ANTES de implementar** | 3 bugs por naming inconsistente. Se CLI, API e operator precisam concordar sobre um nome, defina em UM lugar. |
| F8 | **Validar environment como lowercase no CLI E na API** | Defense in depth. Regex: `^[a-z][a-z0-9-]{0,62}$`. |
| F9 | **Emitir eventos SSE em cada transição de estado** | O CLI depende deles para mostrar progresso. Sem SSE, o usuário vê silêncio. |
| F10 | **Tratar erro no callback goroutine** | `_ = h.orch.OnBuildCompleted(...)` é proibido. Se falhar, o build fica preso sem nenhum log. |
| F11 | **Retornar 202 no callback do Argo imediatamente** | O `OnBuildCompleted` roda em goroutine separada. Se bloquear, o step `finalize` do Argo faz timeout. |
| F12 | **Health probe na URL ANTES de marcar release ACTIVE** | Sem probe, a URL pode estar no ingress mas o container em CrashLoopBackOff. |
| F13 | **Usar linguagem de produto no CLI** | "deploy", "team", "build" — nunca ArgoCD, Gitea, CRD, advisory lock. |

### O QUE NÃO FAZER

| # | Regra | Consequência se violar |
|---|-------|-----------------------|
| N1 | **NUNCA usar Fake* em código de produção (`main.go`, `adapters.go`)** | O deploy inteiro vira simulação. O exit gate nunca é satisfeito. Fakes SÓ existem em `_test.go`. |
| N2 | **NUNCA implementar sem ler a seção relevante do spec** | Implementação errada. Retrabalho. Já aconteceu 3 vezes neste sprint. |
| N3 | **NUNCA usar `pg_advisory_lock` (session-scoped)** | Deadlocks em connection pool. Use SOMENTE `pg_advisory_xact_lock`. |
| N4 | **NUNCA fazer check de build concorrente FORA do advisory lock** | Race condition: dois requests passam o check, ambos criam build. |
| N5 | **NUNCA fazer update direto de status (`build.Status = "ACTIVE"`)** | Bypassa validação. Use `statemachine.Transition(event)`. Transição inválida deve panic. |
| N6 | **NUNCA engolir erro com `_ =`** | Erro silencioso. Build preso. Debugging impossível. SEMPRE logar com contexto. |
| N7 | **NUNCA expor termos internos no CLI** | ArgoCD, Gitea, MinIO, CRD, tenant_id, advisory lock — PROIBIDOS em output do usuário. |
| N8 | **NUNCA usar `emptyDir` para compartilhar dados entre DAG tasks no Argo** | Cada task roda em pod separado. `emptyDir` morre com o pod. Use `volumeClaimTemplates`. |
| N9 | **NUNCA assumir que clusters k3d se enxergam por DNS** | Docker networks separadas. Use NodePort + IPs de containers. |
| N10 | **NUNCA marcar o sprint como "pronto" sem rodar o exit gate completo** | "Os testes passam" ≠ "funciona". O exit gate é `curl URL → 200`. |
| N11 | **NUNCA duplicar `isBuildTerminal` ou qualquer lógica de state machine** | Se as constantes mudarem, uma cópia atualiza e a outra não. Uma fonte de verdade em `pkg/statemachine/`. |
| N12 | **NUNCA commitar binários compilados no git** | `api/main` (25MB), `operators/main` (43MB). Infla o clone. Não é source code. |
| N13 | **NUNCA criar `--force` sem semântica definida no spec** | Flags sem comportamento definido não entram no stability contract. |
| N14 | **NUNCA sugerir `loopzero secret set` na Sprint 2** | O comando não existe. Mensagem: "Set secrets before deploying. See: https://docs.loopzero.example/secrets" |
| N15 | **NUNCA fazer `git checkout` ou `git revert`** | Regra global do time. Sem exceção. |

---

## 6. FLUXO REAL DO DEPLOY (O que acontece de verdade)

```
CLI                          API                         Build Cluster              Workload Cluster
 │                            │                            │                          │
 ├─ git bundle create ────────┤                            │                          │
 │                            ├─ git push → Gitea          │                          │
 │                            │                            │                          │
 ├─ POST /builds ─────────────┤                            │                          │
 │   (Idempotency-Key)        ├─ Lock 1: advisory lock     │                          │
 │                            ├─ check concurrent build    │                          │
 │                            ├─ create Build (QUEUED)     │                          │
 │                            ├─ submit Argo Workflow ─────┤                          │
 │                            ├─ Build → RUNNING           ├─ clone-repo (Gitea)      │
 │                            ├─ libera Lock 1             ├─ install-deps (npm ci)   │
 │  ◄── 202 {build_id} ──────┤                            ├─ build-apps              │
 │                            │                            ├─ build-image (Kaniko)    │
 ├─ GET /builds/:id/stream    │                            ├─ generate-manifests      │
 │   (SSE)                    │                            ├─ gitops-push → Gitea     │
 │  ◄── build.started         │                            ├─ save-cache → MinIO      │
 │  ◄── build.step.*          │                            │                          │
 │  ◄── build.succeeded       │  ◄── POST /callback ──────┤                          │
 │                            │   (202 imediato)           │                          │
 │                            ├─ Lock 2: advisory lock     │                          │
 │                            ├─ create Release (READY)    │                          │
 │  ◄── release.pending       ├─ Release → PENDING         │                          │
 │                            ├─ ArgoCD sync ──────────────┼──────────────────────────┤
 │                            │                            │                          ├─ Deployment
 │                            │                            │                          ├─ Service
 │  ◄── release.health_check  ├─ health probe HTTP GET ────┼──────────────────────────┤
 │      {status: "checking"}  │                            │                          ├─ /health → 200
 │  ◄── release.health_check  │                            │                          │
 │      {status: "ok"}        ├─ Release → ACTIVE          │                          │
 │                            ├─ previous → SUPERSEDED     │                          │
 │                            ├─ libera Lock 2             │                          │
 │  ◄── deploy.completed      │                            │                          │
 │      {deploy_id, urls}     │                            │                          │
 │                            │                            │                          │
 ├─ Mostra URLs ao usuário    │                            │                          │
 ├─ exit 0                    │                            │                          │
```

**Dois locks separados (DA-17):**
- **Lock 1:** Submit (rápido, libera antes do build). Protege check + create.
- **Lock 2:** Activation (até 150s). Protege release + sync + probe.

Entre Lock 1 e Lock 2, o build roda SEM lock. Isso é by-design.

---

## 7. IDs — QUEM É QUEM

| Interno | User-facing | Formato | Onde aparece |
|---------|-------------|---------|-------------|
| `build-{8hex}` | `b-{4hex}` | `build-4a2c8f1b` → `b-4a2c` | SSE stream, `loopzero logs --build` |
| `rel-{8hex}` | `d-{4hex}` | `rel-8f3a2b1c` → `d-8f3a` | `--json`, `loopzero inspect`, `loopzero rollback` |

**IDs internos NUNCA aparecem em output do usuário.**

---

## 8. EXIT CODES — PERMANENTES

| Código | Significado | Quando |
|--------|------------|--------|
| 0 | Sucesso | Deploy ok, `--no-wait` aceito |
| 1 | Erro de uso | Args inválidos, sem `.git`, sem `loopzero.yaml`, env uppercase |
| 2 | Falha de build | npm install falhou, Dockerfile erro |
| 3 | Falha de deploy | Sync falhou, probe falhou, 409 concurrent, PLATFORM_DEGRADED |
| 4 | Erro de auth | Não logado, token inválido |
| 5 | Falha parcial | 2+ apps solicitadas, pelo menos uma falhou |

**Estes códigos NUNCA mudam. Nenhum outro código é permitido.**

---

## 9. CHECKLIST ANTES DE ABRIR PR

```
[ ] Rodei o exit gate completo (seção 2) e o curl retornou 200
[ ] Zero Fake* em código de produção (main.go, adapters.go)
[ ] Zero termos internos no CLI (grep da seção 4.5)
[ ] Zero isBuildTerminal duplicado
[ ] Zero binários compilados no git
[ ] task test:unit passa
[ ] task lint passa
[ ] task validate:manifests passa
[ ] Concurrent deploy: um 202, um 409
[ ] Todos os exit codes corretos (0/1/2/3/4/5)
[ ] --json match spec §0.3
[ ] Changelog atualizado
```

**Se qualquer item falha, o PR não está pronto.**
