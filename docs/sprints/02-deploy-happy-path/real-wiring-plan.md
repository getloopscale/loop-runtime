# Sprint 2 — Plano de Wiring Real (Zero Fakes)

> **Data:** 2026-03-14
> **Problema:** O deploy inteiro é simulado. 5 interfaces em `main.go` usam Fake*.
> **Objetivo:** Substituir TODOS os fakes por implementações reais. Zero simulação.
> **Exit gate:** `loopzero deploy` produz um pod rodando no cluster workload com URL acessível.

---

## Estado Atual

### Infraestrutura (tudo instalado e rodando)

| Componente | Cluster | Status | Acesso |
|-----------|---------|--------|--------|
| PostgreSQL | mgmt | Instalado | `loopzero-system` namespace |
| Gitea | mgmt | Instalado | NodePort 30000, admin: `loopzero-admin` |
| MinIO | mgmt | Instalado | S3-compatible, bucket: `loopzero-artifacts` |
| ArgoCD | mgmt | Instalado | NodePort 30082, v2.13.3, manual sync |
| Argo Workflows | build | Instalado | WorkflowTemplate `loopzero-build-nodejs-v1` existe |
| Traefik | workload | Instalado | Ingress controller |
| k3d clusters | host | 3 clusters | mgmt, build, workload |

### Interfaces a implementar (5 fakes → 5 reais)

| # | Interface | Fake atual | Implementação real |
|---|-----------|-----------|-------------------|
| W1 | `SourceStore` | `FakeSourceStore` (in-memory) | Push git bundle para Gitea source repo |
| W2 | `WorkflowSubmitter` | `FakeWorkflowSubmitter` (sleep 500ms) | Submit Argo Workflow no build cluster |
| W3 | `GitOpsPusher` | `FakeGitOpsPusher` (retorna "commit-fake") | Push manifests K8s para Gitea gitops repo |
| W4 | `SyncClient` | `FakeSyncClient` (retorna nil) | Trigger ArgoCD sync + poll status |
| W5 | `HealthProber` | `FakeHealthProber` (retorna nil) | HTTP GET no health endpoint do app |

---

## Ordem de Execução

```
W5 (HealthProber) ─────────────────── Sem dependências externas, HTTP puro
    │
W1 (SourceStore) ──────────────────── Depende de Gitea estar acessível
    │
W2 (WorkflowSubmitter) ────────────── Depende de W1 (source no Gitea) + Argo Workflows
    │
W3 (GitOpsPusher) ─────────────────── Depende de W2 (build produz image digests)
    │
W4 (SyncClient) ──────────────────── Depende de W3 (manifests no Gitea)
    │
W6 (Wiring main.go) ──────────────── Substituir TODOS os fakes. Remover adapters/Fake*
    │
W7 (E2E: deploy real) ────────────── Validar pod rodando, URL acessível
```

W5 primeiro porque é o mais simples (HTTP puro) e valida o padrão.
W1 antes de W2 porque o Argo Workflow precisa clonar o source do Gitea.
W2 antes de W3 porque o build produz image digests que o gitops push precisa.
W3 antes de W4 porque o ArgoCD sync precisa dos manifests no Gitea.

---

## W5: HealthProber Real

**Interface:** `api/internal/services/build_orchestrator.go`
```go
type HealthProber interface {
    Probe(ctx context.Context, url string, path string) error
}
```

**Spec:** §10.1 step 8 — DA-6
- HTTP GET em `{url}{path}`
- Aceita: 200-399
- Retries: 3x com backoff (2s, 4s, 8s)
- Timeout total: 30s
- Workers: nunca probed (sem URL)

### Arquivo: `api/internal/health/prober.go`

```go
type HTTPProber struct {
    Client *http.Client
}

func (p *HTTPProber) Probe(ctx context.Context, url string, path string) error {
    endpoint := url + path
    var lastErr error
    backoff := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

    for attempt, wait := range backoff {
        resp, err := p.Client.Get(endpoint)
        if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 400 {
            resp.Body.Close()
            return nil
        }
        if err != nil {
            lastErr = err
        } else {
            resp.Body.Close()
            lastErr = fmt.Errorf("health check returned %d", resp.StatusCode)
        }
        if attempt < len(backoff)-1 {
            time.Sleep(wait)
        }
    }
    return fmt.Errorf("health probe failed after 3 attempts: %w", lastErr)
}
```

### Teste: `api/tests/unit/health_prober_test.go`

```
- Probe com servidor retornando 200 → sucesso
- Probe com servidor retornando 500 3x → erro após 3 tentativas
- Probe com servidor retornando 500, 500, 200 → sucesso na 3a tentativa
- Probe com servidor inacessível → erro com contexto
- Probe com status 301 (redirect) → sucesso (200-399)
```

### Critérios de aceite

- [ ] Teste escrito e falhando antes da implementação
- [ ] HTTP GET real contra httptest.Server (não mock)
- [ ] Retries com backoff
- [ ] Timeout total ≤ 30s
- [ ] Testes passam

---

## W1: SourceStore Real (Gitea)

**Interface:** `api/internal/source/store.go`
```go
type SourceStore interface {
    PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (commitSHA string, err error)
}
```

**Spec:** §10.1 step 5 — Source upload pushes git bundle to Gitea source repo

### Pré-requisito: Gitea repos criados pelo TenantController

O TenantController já cria repos Gitea para cada tenant. Verificar que o repo de source existe:
- Repo format: `{tenant-slug}/{project-slug}` ou `loopzero-source-{tenant}-{project}`
- Verificar a interface do provisioner Gitea existente

### Arquivo: `api/internal/source/gitea_store.go`

```go
type GiteaSourceStore struct {
    BaseURL  string // http://gitea-http.loopzero-system.svc.cluster.local:3000
    Token    string // Gitea admin token
    Client   *http.Client
}

func (s *GiteaSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (string, error) {
    // 1. Usar Gitea API para push do bundle
    //    POST /api/v1/repos/{owner}/{repo}/raw (ou git HTTP protocol)
    // 2. Retornar commit SHA do push
}
```

### Decisão de design: Git HTTP Smart Protocol vs Gitea API

**Opção A:** Git HTTP Smart Protocol — `git push` via HTTP diretamente
- Pro: Nativo, qualquer git server
- Con: Precisa de git binary, mais complexo

**Opção B:** Gitea API — Upload de arquivo via REST
- Pro: Simples, SDK disponível (code.gitea.io/sdk)
- Con: Específico para Gitea

**Recomendação:** Gitea API (SDK). É mais simples e o spec já assumem Gitea como componente fixo.

### Teste: `api/tests/integration/source_store_test.go`

```
- Push bundle para repo existente → retorna commit SHA válido
- Push bundle para repo inexistente → erro claro
- Push bundle vazio → erro de validação
- Push segundo bundle → novo commit SHA (append, não overwrite)
```

**Estes testes rodam contra Gitea real** (port-forward para cluster mgmt).

### Critérios de aceite

- [ ] Testes de integração contra Gitea real (não mock)
- [ ] Bundle visível no Gitea UI após push
- [ ] Commit SHA retornado é válido
- [ ] Gitea SDK importado e encapsulado atrás da interface

---

## W2: WorkflowSubmitter Real (Argo Workflows)

**Interface:** `api/internal/services/build_orchestrator.go`
```go
type WorkflowSubmitter interface {
    Submit(ctx context.Context, spec WorkflowSpec) (workflowID string, err error)
    Cancel(ctx context.Context, workflowID string) error
}
```

**WorkflowSpec existente:**
```go
type WorkflowSpec struct {
    TenantID    string
    ProjectID   string
    Environment string
    BuildID     string
    CommitSHA   string
    Apps        []AppSpec
    CallbackURL string
}
```

**Spec:** §7.4, §10.1 steps 6-7

### Pré-requisito: WorkflowTemplate

O template `loopzero-build-nodejs-v1` já existe em `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml`. Verificar seus parâmetros esperados.

### Arquivo: `api/internal/argo/workflow_submitter.go`

```go
type ArgoWorkflowSubmitter struct {
    Client     wfclientset.Interface // Argo Workflows Go SDK
    Namespace  string               // Namespace no build cluster
    TemplateName string             // "loopzero-build-nodejs-v1"
}

func (s *ArgoWorkflowSubmitter) Submit(ctx context.Context, spec WorkflowSpec) (string, error) {
    // 1. Construir Workflow CR a partir do WorkflowTemplate
    // 2. Setar parâmetros: tenant, project, env, build_id, commit, apps (JSON), callback_url
    // 3. Criar Workflow no build cluster via client-go
    // 4. Retornar workflow.Name como workflowID
}

func (s *ArgoWorkflowSubmitter) Cancel(ctx context.Context, workflowID string) error {
    // 1. Patch Workflow com shutdown: "Stop"
}
```

### Dependência: Argo Workflows Go SDK

```
go get github.com/argoproj/argo-workflows/v3@latest
```

Importar:
- `github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned` (typed client)
- `github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1` (Workflow types)

### Multi-cluster: API roda no mgmt, Argo Workflows roda no build

O `ArgoWorkflowSubmitter` precisa de um kubeconfig/context para o build cluster, não o mgmt. Verificar como o `ClusterCredentialController` disponibiliza as credenciais do build cluster.

### Teste: `api/tests/integration/workflow_submitter_test.go`

```
- Submit workflow com parâmetros válidos → Workflow CR criado no build cluster
- Submit workflow → workflowID retornado é o name do CR
- Cancel workflow → Workflow status = shutdown
- Submit com template inexistente → erro claro
```

**Estes testes rodam contra Argo Workflows real no build cluster.**

### Critérios de aceite

- [ ] Testes de integração contra Argo Workflows real (não mock)
- [ ] Workflow CR visível via `kubectl get wf -n loopzero-build`
- [ ] Parâmetros passados corretamente ao template
- [ ] Build executa steps reais (clone, install, build, publish)
- [ ] Callback chega ao API endpoint quando build completa

---

## W3: GitOpsPusher Real (Gitea)

**Interface:** `api/internal/services/build_orchestrator.go`
```go
type GitOpsPusher interface {
    Push(ctx context.Context, req GitOpsPushRequest) (commitSHA string, err error)
}
```

**GitOpsPushRequest existente:**
```go
type GitOpsPushRequest struct {
    TenantID    string
    ProjectID   string
    Environment string
    ReleaseID   string
    Apps        []AppRelease
}
```

**Spec:** §7.5, §10.1 step 9

### NOTA: Quem gera os manifests?

O WorkflowTemplate tem um step `generate-manifests` que gera os YAML de Deployment, Service, Ingress. Mas o `GitOpsPusher` no lado da API pode precisar de uma abordagem diferente dependendo de quem faz o push:

**Opção A (spec):** O workflow faz o gitops-push como step `gitops-push` no DAG
**Opção B:** A API faz o push após receber o callback

Verificar o WorkflowTemplate — se ele já tem o step `gitops-push`, então o `GitOpsPusher` na API pode ser um **no-op** ou apenas verificar que o commit existe. Se o workflow NÃO faz o push, a API precisa fazer.

Ler o template para decidir. Se o template já faz tudo, o `GitOpsPusher` na API vira:

```go
func (p *GiteaGitOpsPusher) Push(ctx context.Context, req GitOpsPushRequest) (string, error) {
    // O workflow já fez o push. Verificar que o commit existe no Gitea.
    // Retornar o commit SHA do último commit no branch do environment.
}
```

### Arquivo: `api/internal/gitea/gitops_pusher.go`

Implementação depende da análise acima.

### Teste: `api/tests/integration/gitops_pusher_test.go`

```
- Push manifests para repo Gitea → commit criado com manifests corretos
- Push para environment production → manifests no path correto
- Push com image digests → deployment.yaml referencia imagem correta
```

### Critérios de aceite

- [ ] Testes de integração contra Gitea real
- [ ] Manifests visíveis no Gitea UI
- [ ] Commit tem trailers: Release-ID, Correlation-ID, Build-ID
- [ ] Formato de manifests: Deployment + Service + Ingress por app

---

## W4: SyncClient Real (ArgoCD)

**Interface:** `api/internal/services/build_orchestrator.go`
```go
type SyncClient interface {
    TriggerSync(ctx context.Context, appName string) error
    WaitForSync(ctx context.Context, appName string, timeout time.Duration) error
}
```

**Spec:** §7.3, §10.1 steps 11-12

### Nota: ArgoCD API

ArgoCD expõe REST API. Endpoints necessários:
- `POST /api/v1/applications/{name}/sync` — trigger sync
- `GET /api/v1/applications/{name}` — check status

### Arquivo: `api/internal/argocd/sync_client.go`

Verificar se `api/internal/services/argocd_client.go` já existe (mencionado na revisão como tendo circuit breaker). Se sim, estender. Se não, criar.

```go
type ArgoCDSyncClient struct {
    BaseURL string // http://argocd-server.argocd.svc.cluster.local
    Token   string // ArgoCD API token
    Client  *http.Client
}

func (c *ArgoCDSyncClient) TriggerSync(ctx context.Context, appName string) error {
    // POST /api/v1/applications/{appName}/sync
}

func (c *ArgoCDSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration) error {
    // Poll GET /api/v1/applications/{appName} a cada 10s
    // Até status.sync.status == "Synced" && status.health.status == "Healthy"
    // Ou timeout (2 minutos)
}
```

### Teste: `api/tests/integration/argocd_sync_test.go`

```
- TriggerSync em app existente → sucesso (ou sync já em andamento)
- WaitForSync em app synced → retorna imediatamente
- WaitForSync com timeout → erro após timeout
- TriggerSync em app inexistente → erro claro
```

**Estes testes rodam contra ArgoCD real no mgmt cluster.**

### Critérios de aceite

- [ ] Testes de integração contra ArgoCD real
- [ ] Sync visível no ArgoCD UI (NodePort 30082)
- [ ] Timeout de 2 minutos respeitado
- [ ] Circuit breaker integrado (DA-7, já implementado em argocd_client.go)

---

## W6: Wiring Real no main.go

**Arquivo:** `api/cmd/main.go`

### Substituições

```go
// ANTES (fake):
sourceStore = source.NewFakeSourceStore("dev-local")
fakeWorkflow := &adapters.FakeWorkflowSubmitter{}
orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
    Workflow: fakeWorkflow,
    GitOps:   &adapters.FakeGitOpsPusher{},
    Health:   &adapters.FakeHealthProber{},
    Sync:     &adapters.FakeSyncClient{},
    ...
})

// DEPOIS (real):
sourceStore = &source.GiteaSourceStore{
    BaseURL: os.Getenv("GITEA_URL"),       // http://gitea-http.loopzero-system.svc:3000
    Token:   os.Getenv("GITEA_TOKEN"),
    Client:  &http.Client{Timeout: 30 * time.Second},
}

argoWfClient := /* build cluster client from kubeconfig */
workflowSubmitter := &argo.ArgoWorkflowSubmitter{
    Client:       argoWfClient,
    Namespace:    "loopzero-build",
    TemplateName: "loopzero-build-nodejs-v1",
}

gitOpsPusher := &gitea.GitOpsPusher{
    BaseURL: os.Getenv("GITEA_URL"),
    Token:   os.Getenv("GITEA_TOKEN"),
    Client:  &http.Client{Timeout: 30 * time.Second},
}

syncClient := &argocd.SyncClient{
    BaseURL: os.Getenv("ARGOCD_URL"),      // http://argocd-server.argocd.svc:443
    Token:   os.Getenv("ARGOCD_TOKEN"),
    Client:  &http.Client{Timeout: 10 * time.Second},
}

healthProber := &health.HTTPProber{
    Client: &http.Client{Timeout: 10 * time.Second},
}

orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
    Workflow: workflowSubmitter,
    GitOps:   gitOpsPusher,
    Health:   healthProber,
    Sync:     syncClient,
    ...
})
```

### Variáveis de ambiente necessárias

| Variável | Valor no dev | Descrição |
|----------|-------------|-----------|
| `GITEA_URL` | `http://gitea-http.loopzero-system.svc.cluster.local:3000` | Gitea HTTP API |
| `GITEA_TOKEN` | Token admin do Gitea | Autenticação para push |
| `ARGOCD_URL` | `http://argocd-server.argocd.svc.cluster.local:443` | ArgoCD API |
| `ARGOCD_TOKEN` | Token admin do ArgoCD | Autenticação para sync |
| `BUILD_CLUSTER_KUBECONFIG` | Path para kubeconfig do build cluster | Acesso ao Argo Workflows |
| `LOOPZERO_ARGO_CALLBACK_SECRET` | Secret compartilhado | Auth do callback |

### Configurar no Tiltfile

Adicionar env vars no deployment do loopzero-api:

```python
k8s_yaml(/* loopzero-api deployment com env vars */)
```

### Remover fakes do código de produção

Após wiring real:
1. Remover `adapters.FakeWorkflowSubmitter` de `adapters.go`
2. Remover `adapters.FakeGitOpsPusher` de `adapters.go`
3. Remover `adapters.FakeSyncClient` de `adapters.go`
4. Remover `adapters.FakeHealthProber` de `adapters.go`
5. Remover `source.FakeSourceStore` de `source/fake_store.go`
6. Remover o bloco `fakeWorkflow.OnComplete` de `main.go`
7. Remover o log `"sprint 2 dependencies wired", "source_store", "fake"...`

**Os fakes podem continuar existindo APENAS em arquivos `_test.go`** para testes unitários que precisam isolar lógica de negócio. Mas NUNCA em código de produção ou wiring.

### Critérios de aceite

- [ ] Zero `Fake*` em `main.go`
- [ ] Zero `Fake*` em `adapters.go` (mover para `_test.go` se necessário)
- [ ] Zero `fake` no log de startup
- [ ] Todas as env vars configuradas no Tiltfile
- [ ] API inicia sem erros com implementações reais

---

## W7: Validação E2E — Deploy Real

### Teste manual

```bash
# 1. Tilt rodando com todas as dependências
tilt up

# 2. Login
loopzero login --token $GITHUB_TOKEN

# 3. Init
mkdir /tmp/hello-world && cd /tmp/hello-world
npm init -y
echo 'const http = require("http"); http.createServer((_, r) => { r.writeHead(200); r.end("ok"); }).listen(3000);' > index.js
loopzero init --yes --name hello-world

# 4. Deploy
loopzero deploy

# 5. Verificar pod rodando
kubectl --context k3d-loopzero-workload get pods -n tenant-*

# 6. Verificar URL acessível
curl -s $(loopzero deploy --json | jq -r '.apps[0].url')

# 7. Verificar no ArgoCD UI
open http://localhost:30082
```

### Teste E2E automatizado: `infra/tests/e2e/deploy_real_test.go`

```
- Deploy de app Node.js simples → pod rodando no workload cluster
- Pod responde no health endpoint → 200
- URL do deploy resolve → app acessível
- Segundo deploy → release anterior SUPERSEDED
- Deploy concurrent → um 202, um 409
```

### Critérios de aceite

- [ ] Pod rodando no workload cluster (`kubectl get pods`)
- [ ] Health endpoint responde 200
- [ ] URL do deploy resolve (DNS ou /etc/hosts)
- [ ] ArgoCD mostra Application synced
- [ ] Argo Workflows mostra Workflow succeeded
- [ ] Gitea mostra commits no repo gitops
- [ ] MinIO mostra cache salvo

---

## Exit Gate Final

```bash
# Todos estes comandos devem PASSAR:

# 1. Testes unitários
task test:unit

# 2. API inicia com implementações reais (zero "fake" no log)
kubectl logs -n loopzero-system deploy/loopzero-api | grep -c "fake"  # deve ser 0

# 3. Deploy real
loopzero login --token $GITHUB_TOKEN
loopzero init --yes --name e2e-test
loopzero deploy

# 4. Pod existe
kubectl --context k3d-loopzero-workload get pods -n tenant-* | grep e2e-test

# 5. App responde
curl -f $(loopzero deploy --json | jq -r '.apps[0].url')/health

# 6. Exit gate do Sprint 2
# "loopzero login → loopzero init → loopzero deploy produces a running app with a real URL"
```

**O Sprint 2 SÓ está completo quando o passo 5 retorna 200.**
