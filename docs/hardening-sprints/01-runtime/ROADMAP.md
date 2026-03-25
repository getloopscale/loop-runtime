# Sprint 1 — RUNTIME REAL

**Objetivo:** Todo estado passa por reconcile loop. 7 controllers running.
**Confiança:** ALTA (1.1-1.4), MÉDIA (1.5)
**Esforço:** ~3h incluindo validação
**Rollback:** Remover chamada Setup do main.go, re-deploy.

---

## Pré-requisito: GATE-0 (Sistema base saudável)

Antes de tocar qualquer controller:

- [ ] ArgoCD server Running: `kubectl -n platform-system get pod -l app.kubernetes.io/name=argocd-server`
- [ ] Gitea acessível: `curl -sf <gitea-url>/api/v1/version` (via port-forward)
- [ ] API respondendo: `curl -sf https://api.dev.loopzero.example/api/v1/health`
- [ ] Runtime cluster acessível: `kubectl --context <runtime> get nodes`
- [ ] Build cluster acessível: `kubectl --context <build> get nodes`
- [ ] TenantController reconciliando: `kubectl -n platform-system get tenant tn-loopzero` → Active

**Se qualquer item falhar → PARAR. Corrigir primeiro.**

---

## Tasks (serializadas — uma por vez)

### Task 1.1 — Wire EnvironmentController

**Por que primeiro:** Controller mais independente. Só precisa de `mgr + runtimeClient` (ambos existem). TenantController já cria Environment CRs — este é o próximo na cadeia.

**Arquivo:** `operators/cmd/main.go`
**Após linha 150** (depois de `SetupClusterCredentialController`):
```go
if err := controller.SetupEnvironmentController(mgr, runtimeClient); err != nil {
    setupLog.Error(err, "unable to setup EnvironmentController")
    os.Exit(1)
}
```

**Dependência:** `runtimeClient` — já existe (main.go:112)

**Gate 1.1:**
- [ ] `go test ./internal/controller/ -run Environment -v -count=1` → PASS
- [ ] Deploy operator
- [ ] Logs: `"starting controller" controller="environment"`
- [ ] `kubectl -n platform-system get environment` → existe
- [ ] `kubectl --context <runtime> get ns loopzero-tn-loopzero-production` → criado em <60s
- [ ] Esperar 5 minutos — zero crash loops

---

### Task 1.2 — Wire ApplicationController

**Por que segundo:** Cria ExposedRoute CRs que o ExposureController (próximo) vai reconciliar.

**Arquivo:** `operators/cmd/main.go`:
```go
baseDomain := os.Getenv("BASE_DOMAIN")
if baseDomain == "" {
    baseDomain = "dev.loopzero.example"
}
if err := controller.SetupApplicationController(mgr, baseDomain); err != nil {
    setupLog.Error(err, "unable to setup ApplicationController")
    os.Exit(1)
}
```

**Ação adicional:** Adicionar env var `BASE_DOMAIN` no deployment do operator (Helm/Tilt).

**Gate 1.2:**
- [ ] `go test ./internal/controller/ -run Application -v -count=1` → PASS
- [ ] Deploy operator
- [ ] Logs: `"starting controller" controller="application"`
- [ ] Criar LoopZeroApplication CR → `status.URL` populado
- [ ] ExposedRoute CR criado com ownerRef → fase Pending (esperado)
- [ ] Esperar 5 minutos — zero crash loops

---

### Task 1.3 — Wire ExposureController

**Por que terceiro:** Consome ExposedRoute CRs criados pelo ApplicationController.

**Arquivo:** `operators/cmd/main.go`:
```go
if err := controller.SetupExposureController(mgr); err != nil {
    setupLog.Error(err, "unable to setup ExposureController")
    os.Exit(1)
}
```

**Dependência:** Apenas `mgr`. Assinatura mais simples possível.

**Gate 1.3:**
- [ ] `go test ./internal/controller/ -run Exposure -v -count=1` → PASS
- [ ] Deploy operator
- [ ] ExposedRoute CR → transiciona (CertIssued se cert Ready, ou Pending se não)
- [ ] Esperar 5 minutos — zero crash loops

---

### Task 1.4 — Wire GitRepositoryBindingController

**Por que quarto (antes de PlatformBootstrap):** Mais simples que PlatformBootstrap. PlatformBootstrap pode criar GitRepositoryBinding CRs.

**Arquivo:** `operators/cmd/main.go`:
```go
httpClient := &http.Client{Timeout: 30 * time.Second}

if err := controller.SetupGitRepositoryBindingController(mgr, httpClient); err != nil {
    setupLog.Error(err, "unable to setup GitRepositoryBindingController")
    os.Exit(1)
}
```

**Imports necessários:** `"net/http"` e `"time"`

**Gate 1.4:**
- [ ] `go test ./internal/controller/ -run GitRepositoryBinding -v -count=1` → PASS
- [ ] Deploy operator
- [ ] Criar GitRepositoryBinding CR → ArgoCD Secret criado
- [ ] Esperar 5 minutos — zero crash loops

---

### Task 1.5 — Wire PlatformBootstrapController

**Por que último:** Mais dependências externas. Se URLs/credenciais estiverem erradas, retry loop.

**Arquivo:** `operators/cmd/main.go` (reutiliza `httpClient` de 1.4):
```go
if err := controller.SetupPlatformBootstrapController(mgr, httpClient); err != nil {
    setupLog.Error(err, "unable to setup PlatformBootstrapController")
    os.Exit(1)
}
```

**Gate 1.5 (MAIS RIGOROSA):**
- [ ] `go test ./internal/controller/ -run PlatformBootstrap -v -count=1` → PASS
- [ ] Deploy operator
- [ ] NÃO criar PlatformBootstrap CR ainda — apenas validar startup sem erros
- [ ] Criar PlatformBootstrap CR com URLs corretos
- [ ] Observar transição: Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete
- [ ] Se stuck >2min em qualquer fase → verificar logs e conditions
- [ ] Re-criar CR → idempotente (não duplica tokens)
- [ ] Esperar 10 minutos — zero retry storms

---

### Task 1.6 — Validação E2E

**GATE-SPRINT-1 (não avançar sem isto):**

- [ ] 7 controllers nos logs do operator: tenant, clustercredential, environment, application, exposure, gitrepositorybinding, platformbootstrap
- [ ] `kubectl -n platform-system get tenant tn-loopzero` → Active
- [ ] `kubectl -n platform-system get environment` → Active
- [ ] Zero crash loops nos últimos 30 minutos
- [ ] API health deep → 200
- [ ] **3 deploys consecutivos estáveis:**

| Deploy | Resultado | Build Status | Release Status |
|---|---|---|---|
| 1 | ___ | ___ | ___ |
| 2 | ___ | ___ | ___ |
| 3 | ___ | ___ | ___ |

- [ ] Todos os 3 → build SUCCEEDED, release ACTIVE
- [ ] ArgoCD app synced + healthy após cada deploy

**SE QUALQUER ITEM FALHAR → NÃO AVANÇAR PARA SPRINT 2.**
