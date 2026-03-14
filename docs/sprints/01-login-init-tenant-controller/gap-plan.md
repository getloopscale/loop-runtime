# Sprint 1 — Plano de Implementacao dos Gaps

> **Data:** 2026-03-13
> **Baseline:** ~80% do sprint implementado. 6 gaps identificados.
> **Objetivo:** Fechar todos os gaps para atingir o exit gate do Sprint 1.

---

## Resumo dos Gaps

| # | Gap | Componente | Prioridade | Estimativa |
|---|-----|-----------|------------|------------|
| G1 | ArgoCD Application reconciliation | operators | Alta | Media |
| G2 | PriorityClass reconciliation | operators | Alta | Pequena |
| G3 | Browser OAuth flow (`loopzero login`) | cli | Alta | Media |
| G4 | Interactive wizard (`loopzero init`) | cli | Media | Media |
| G5 | Framework detection — 10o framework | cli | Baixa | Ja resolvido |
| G6 | API wiring (main.go → implementacoes concretas) | api | Alta | Grande |

**G5 esta resolvido:** O spec §26.5 lista 10 frameworks. A implementacao atual tem 9 nomeados + `node` como fallback (linha 118-120 de `framework.ts`). O `node` **e** o 10o framework do spec. Total: 10/10.

---

## Ordem de Execucao

```
G2 (PriorityClass) ─────────────────────┐
G1 (ArgoCD Application) ────────────────┤── Operator (paralelo)
                                         │
G3 (Browser OAuth) ─────────────────────┤── CLI (paralelo)
G4 (Interactive wizard) ────────────────┤
                                         │
G6 (API wiring) ────────────────────────┘── API (depende de G1-G4 para e2e)
```

G1 e G2 sao independentes entre si e dos gaps de CLI. G6 (API wiring) e o ultimo porque depende de ter os contratos estabilizados.

---

## G1: ArgoCD Application Reconciliation

**Spec:** §7.3.1, §1484
**Arquivo:** `operators/internal/controller/tenant_controller.go`
**Teste:** `operators/internal/controller/tenant_controller_test.go`

### O que o spec diz

```
→ Ensure ArgoCD Application per environment (idempotent)
```

O TenantController deve criar um `Application` (CRD do ArgoCD) para cada environment do tenant. Isso conecta o Git (Gitea) ao cluster (workload).

### Implementacao

**1. Definir interface `ArgoCDProvisioner` (DIP)**

```go
// operators/internal/argocd/provisioner.go
type ApplicationSpec struct {
    Name            string
    Namespace       string // "argocd"
    TenantID        string
    ProjectName     string
    RepoURL         string // Gitea repo URL
    TargetRevision  string // "main"
    Path            string // "environments/production/manifests/"
    DestinationNS   string // tenant namespace no workload cluster
    DestinationURL  string // workload cluster API URL
}

type Provisioner interface {
    EnsureApplication(ctx context.Context, spec ApplicationSpec) error
    DeleteApplication(ctx context.Context, name, namespace string) error
}
```

**2. Implementar `FakeProvisioner` para testes**

```go
// operators/internal/argocd/fake.go
type FakeProvisioner struct {
    Applications map[string]ApplicationSpec
    EnsureErr    error
    DeleteErr    error
}
```

**3. Adicionar step `reconcileArgoCDApp` no controller**

- Posicao: apos `reconcileNetworkPolicies`, antes de `reconcileBuildNamespace`
- Condition: `ArgoCDAppReady`
- Para cada environment (por enquanto `production` hardcoded — environments vem do Tenant CR)
- Idempotente: create-or-update
- Annotations: `loopzero.io/tenant`, `loopzero.io/managed-by: tenant-controller`
- Sync policy: `automated: {prune: true, selfHeal: true}`

**4. Adicionar cleanup no `reconcileDelete`**

- Antes de deletar namespaces, deletar ArgoCD Applications

**5. Testes Ginkgo (TDD — teste primeiro)**

```
- ArgoCD Application criada com spec correto
- ArgoCD Application atualizada quando spec muda (idempotente)
- ArgoCD Application deletada no reconcileDelete
- Condition ArgoCDAppReady setada apos sucesso
- Erro no provisioner → condition ArgoCDAppReady = False, requeue
```

### Criterios de aceite

- [ ] Teste Ginkgo escrito e falhando antes da implementacao
- [ ] `reconcileArgoCDApp` cria Application com repoURL, path, destination corretos
- [ ] Idempotente: segunda chamada nao cria duplicata
- [ ] Spec change: Application atualizada quando tenant spec muda
- [ ] Delete: Application removida no cleanup
- [ ] Condition `ArgoCDAppReady` reflete estado

---

## G2: PriorityClass Reconciliation

**Spec:** §14.3
**Arquivo:** `operators/internal/controller/tenant_controller.go`
**Teste:** `operators/internal/controller/tenant_controller_test.go`

### O que o spec diz

3 PriorityClasses baseadas no plano do tenant:

| Plano | PriorityClass name | Value | Preemption |
|-------|-------------------|-------|------------|
| starter | `loopzero-tenant-starter` | 100 | Never |
| growth | `loopzero-tenant-growth` | 500 | PreemptLowerPriority |
| enterprise | `loopzero-tenant-enterprise` | 1000 | PreemptLowerPriority |

PriorityClasses sao **cluster-scoped** (nao namespaced).

### Implementacao

**1. Adicionar step `reconcilePriorityClass` no controller**

- Posicao: apos `reconcileBuildNamespace`
- Condition: `PriorityClassReady`
- Logica: create-or-update `PriorityClass` com nome `loopzero-tenant-{plan}`
- Mapear `tenant.Spec.Plan` → name, value, preemptionPolicy

**2. Mapping**

```go
func priorityClassForPlan(plan string) (name string, value int32, preemption string) {
    switch plan {
    case "enterprise":
        return "loopzero-tenant-enterprise", 1000, "PreemptLowerPriority"
    case "growth":
        return "loopzero-tenant-growth", 500, "PreemptLowerPriority"
    default: // starter
        return "loopzero-tenant-starter", 100, "Never"
    }
}
```

**3. Cleanup**

PriorityClasses sao compartilhadas entre tenants do mesmo plano. **NAO deletar no reconcileDelete** — outros tenants podem usar a mesma PriorityClass.

**4. Testes Ginkgo (TDD)**

```
- PriorityClass criada com valor e preemption corretos para cada plano
- Idempotente: segunda reconciliacao nao falha
- Spec change (plano upgrade): nova PriorityClass criada
- Delete: PriorityClass NAO e deletada (compartilhada)
```

### Criterios de aceite

- [ ] Teste Ginkgo escrito e falhando antes da implementacao
- [ ] PriorityClass criada com nome, valor e preemption do spec §14.3
- [ ] Idempotente
- [ ] Plan upgrade cria nova PriorityClass (nao deleta a antiga)
- [ ] reconcileDelete nao remove PriorityClass

---

## G3: Browser OAuth Flow (`loopzero login`)

**Spec:** §25.1 — `loopzero login` abre browser para GitHub OAuth
**Arquivo:** `cli/src/commands/login.ts`
**Teste:** `cli/tests/unit/commands/login.test.ts`

### O que o spec diz

```
loopzero login                        # Opens browser for GitHub OAuth
loopzero login --token <api-key>      # For CI/CD, no browser
```

Saida esperada (§0.1): `Authenticated as lucas@acme.dev (team: acme)`

### Implementacao

**1. Adicionar deps para browser OAuth**

```typescript
// Novas deps em LoginDeps:
interface LoginDeps {
    // ... existentes ...
    openBrowser: (url: string) => Promise<void>;
    startCallbackServer: (port: number) => Promise<{ code: string; shutdown: () => void }>;
    exchangeCode: (code: string) => Promise<{ token: string }>;
}
```

**2. Fluxo sem `--token` (interativo)**

```
1. Gerar state + code_verifier (PKCE)
2. Construir URL de autorizacao GitHub:
   https://github.com/login/oauth/authorize?
     client_id={GITHUB_CLIENT_ID}
     &redirect_uri=http://localhost:{PORT}/callback
     &scope=read:user,read:org
     &state={state}
     &code_challenge={challenge}
     &code_challenge_method=S256
3. Abrir browser com a URL
4. Iniciar servidor HTTP local na porta {PORT} (e.g., 9876)
5. Esperar callback com ?code={code}&state={state}
6. Validar state
7. Trocar code por access token via API do LoopZero (ou direto GitHub)
8. Validar token (mesmo fluxo do --token)
9. Salvar token + fechar servidor
```

**3. Fallback CI**

Se `isCI === true` e `--token` nao fornecido → exit 1 com mensagem:
`"In CI mode, use: loopzero login --token <token>"`

**4. Testes (TDD)**

```
- Sem --token em modo interativo → chama openBrowser + startCallbackServer
- Sem --token em CI → exit 1 com mensagem
- Callback recebido com state invalido → exit 4
- Callback recebido com state valido → troca code, valida, salva
- Browser OAuth error → exit 4 com mensagem clara
- Timeout (servidor nao recebe callback) → exit 4
```

### Criterios de aceite

- [ ] Teste escrito e falhando antes da implementacao
- [ ] `loopzero login` (sem --token) abre browser e inicia servidor local
- [ ] Callback server aceita code, valida state, troca por token
- [ ] Token validado e salvo no config store
- [ ] CI mode sem --token → exit 1
- [ ] Erros de OAuth → exit 4 com mensagem sugerindo acao
- [ ] Nenhum termo interno na saida

---

## G4: Interactive Wizard (`loopzero init`)

**Spec:** §25.2, §0.1
**Arquivo:** `cli/src/commands/init.ts`
**Teste:** `cli/tests/unit/commands/init.test.ts`

### O que o spec diz

```
Detected: Turborepo monorepo
  apps/api    → NestJS (port 3000)
  apps/web    → Vite (port 5173)
  apps/worker → Node.js worker (no port)

Generated loopzero.yaml with 3 apps.
Review and edit if needed, then run: loopzero deploy
```

O wizard deve:
1. Detectar monorepo
2. Escanear apps com framework detection
3. Mostrar o que foi detectado
4. Pedir confirmacao (a menos que `--yes`)
5. Gerar loopzero.yaml

### Implementacao

**1. Adicionar deps para prompts interativos**

```typescript
// Novas deps em InitDeps:
interface InitDeps {
    // ... existentes ...
    prompt: (question: string, defaultValue?: string) => Promise<string>;
    confirm: (question: string) => Promise<boolean>;
}
```

**2. Fluxo interativo (sem --yes)**

```
1. Detectar monorepo (ja implementado)
2. Detectar frameworks por app (ja implementado)
3. Exibir resumo:
   "Detected: {tool} monorepo"
   "  {path} → {framework} (port {port})"
4. Pedir confirmacao: "Create project? (Y/n)"
   - Se nao → exit 0 sem gerar
5. Pedir nome do projeto se nao fornecido:
   "Project name (default: {dirname}):"
6. Gerar loopzero.yaml (ja implementado)
7. Chamar API createProject (ja implementado)
8. Exibir mensagem final:
   "Generated loopzero.yaml with {n} apps."
   "Review and edit if needed, then run: loopzero deploy"
```

**3. --yes pula todos os prompts (ja funciona)**

**4. Testes (TDD)**

```
- Modo interativo: exibe resumo de deteccao antes de gerar
- Modo interativo: pede confirmacao, se "n" → exit 0 sem gerar
- Modo interativo: pede nome do projeto se nao fornecido via --name
- --yes: pula prompts e gera direto (ja testado)
- Resumo mostra framework e porta corretos para cada app
```

### Criterios de aceite

- [ ] Teste escrito e falhando antes da implementacao
- [ ] Sem --yes: exibe resumo de deteccao
- [ ] Sem --yes: pede confirmacao antes de gerar
- [ ] Confirmacao negada → exit 0, nenhum arquivo criado
- [ ] Pede nome do projeto interativamente se nao fornecido
- [ ] Mensagem final sugere `loopzero deploy`
- [ ] --yes pula tudo (ja funciona, manter)

---

## G6: API Wiring (main.go → implementacoes concretas)

**Spec:** §7.2
**Arquivo:** `api/cmd/main.go`
**Novos arquivos:**
- `api/internal/github/validator.go` — TokenValidator via GitHub API
- `api/internal/db/membership_store.go` — MembershipStore via PostgreSQL
- `api/internal/db/project_store.go` — ProjectStore via PostgreSQL
- `api/internal/k8s/tenant_cr_manager.go` — TenantCRManager via Kubernetes API

### O que o spec diz

O `main.go` deve conectar implementacoes reais:
- **TokenValidator:** Valida GitHub OAuth tokens via `GET https://api.github.com/user` com header `Authorization: Bearer {token}`
- **MembershipStore:** Consulta `tenant_members` JOIN `tenants` no PostgreSQL
- **ProjectStore:** INSERT/SELECT na tabela `projects` no PostgreSQL
- **TenantCRMgr:** Cria/atualiza Tenant CR no cluster mgmt via client-go

### Implementacao

**1. GitHubTokenValidator**

```go
// api/internal/github/validator.go
type GitHubTokenValidator struct {
    httpClient *http.Client
}

func (v *GitHubTokenValidator) ValidateToken(ctx context.Context, token string) (*middleware.TokenValidationResult, error) {
    // GET https://api.github.com/user
    // Header: Authorization: Bearer {token}
    // Parse response: login, email, id
    // Return TokenValidationResult{UserID: "github-{login}", Email: email, GitHubID: id}
}
```

**2. PostgresMembershipStore**

```go
// api/internal/db/membership_store.go
type PostgresMembershipStore struct {
    db *pgxpool.Pool
}

func (s *PostgresMembershipStore) GetMemberships(ctx context.Context, userID string) ([]middleware.TenantMembership, error) {
    // SELECT tenant_id, role FROM tenant_members WHERE user_id = $1
}
```

**3. PostgresProjectStore**

```go
// api/internal/db/project_store.go
type PostgresProjectStore struct {
    db *pgxpool.Pool
}

func (s *PostgresProjectStore) CreateProject(ctx context.Context, input routes.CreateProjectInput) (routes.ProjectResult, error) {
    // INSERT INTO projects (id, tenant_id, name, created_by) VALUES (...)
    // ON CONFLICT (tenant_id, name) → return ErrProjectExists
    // Return ProjectResult{ID: id, Name: name, TeamID: tenantID}
}
```

**4. KubeTenantCRManager**

```go
// api/internal/k8s/tenant_cr_manager.go
type KubeTenantCRManager struct {
    client dynamic.Interface // ou client-go typed
}

func (m *KubeTenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec routes.TenantCRSpec) error {
    // Create or Update Tenant CR no cluster mgmt
    // kubectl apply -f equivalente via client-go
}
```

**5. Wiring no main.go**

```go
func main() {
    // Parse env vars
    dbURL := os.Getenv("DATABASE_URL")

    // Connect to PostgreSQL
    pool, err := pgxpool.New(ctx, dbURL)

    // Build config with real implementations
    cfg := server.Config{
        Port:           port,
        Logger:         logger,
        DB:             pool,
        TokenValidator: &github.GitHubTokenValidator{...},
        MemberStore:    &db.PostgresMembershipStore{DB: pool},
        ProjectStore:   &db.PostgresProjectStore{DB: pool},
        TenantCRMgr:    &k8s.KubeTenantCRManager{...},
    }
}
```

**6. Testes (TDD)**

Para cada implementacao concreta, testes de integracao:

```
- GitHubTokenValidator: mock HTTP server simulando GitHub API
  - Token valido → retorna user info
  - Token invalido/expirado → retorna ErrInvalidToken
  - GitHub API indisponivel → retorna erro (nao ErrInvalidToken)

- PostgresMembershipStore: testcontainers com PostgreSQL real
  - User com 1 membership → retorna 1
  - User com N memberships → retorna N
  - User sem membership → retorna lista vazia

- PostgresProjectStore: testcontainers com PostgreSQL real
  - Create → retorna ProjectResult com ID gerado
  - Create duplicado → retorna ErrProjectExists
  - ID gerado segue formato proj-{8hex}
```

### Criterios de aceite

- [ ] Teste escrito e falhando antes da implementacao
- [ ] GitHubTokenValidator valida contra GitHub API real
- [ ] PostgresMembershipStore consulta tenant_members
- [ ] PostgresProjectStore insere com unique constraint handling
- [ ] KubeTenantCRManager cria Tenant CR via client-go
- [ ] main.go conecta tudo com graceful shutdown
- [ ] `PORT`, `DATABASE_URL` configurveis via env vars
- [ ] Health deep retorna 200 com DB conectado

---

## Exit Gate — Validacao Final

Apos todos os gaps implementados, validar:

```bash
# 1. Todos os testes passam
task test:unit

# 2. Lint passa
task lint

# 3. Subir infraestrutura
task setup

# 4. API inicia com stores reais
cd api && DATABASE_URL=postgres://loopzero:loopzero-dev-password@localhost:5432/loopzero go run ./cmd/main.go

# 5. Health deep retorna 200
curl -s http://localhost:3001/api/v1/health/deep | jq .

# 6. Login via token
cd cli && node dist/index.js login --token ghp_xxx

# 7. Init gera loopzero.yaml
cd /tmp/test-project && node /path/to/cli/dist/index.js init --yes --name test-project

# 8. Tenant CR criado no cluster
kubectl --context k3d-loopzero-mgmt get tenant -n loopzero-system

# 9. Controller provisiona recursos
kubectl --context k3d-loopzero-workload get ns | grep test
kubectl --context k3d-loopzero-workload get networkpolicy -n tenant-test-project
kubectl --context k3d-loopzero-mgmt get priorityclass | grep loopzero-tenant

# 10. ArgoCD Application existe
kubectl --context k3d-loopzero-mgmt get application -n argocd | grep test
```

**Sprint 1 esta completo quando todos os 10 passos acima passam.**
