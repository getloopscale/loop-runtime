# GitHub App Integration — Auto-Deploy on Push

> Modelo Vercel: `git push` → deploy automático. Zero CLI necessário após setup.
> Prerequisito para Preview Environments (Phase 2).

---

## Arquitetura

```
Developer                   GitHub                     LoopZero API                    Build Pipeline
   │                          │                           │                            │
   │  git push main           │                           │                            │
   │─────────────────────────►│                           │                            │
   │                          │  webhook: push event      │                            │
   │                          │──────────────────────────►│                            │
   │                          │                           │  1. Validate HMAC          │
   │                          │                           │  2. Find project by repo   │
   │                          │  GET /tarball/{sha}       │                            │
   │                          │◄──────────────────────────│  3. Download source        │
   │                          │──────────────────────────►│                            │
   │                          │                           │  4. Upload to MinIO        │
   │                          │                           │  5. Submit build           │
   │                          │                           │─────────────────────────►  │
   │                          │  POST /statuses/{sha}     │                            │
   │                          │◄──────────────────────────│  6. Set commit: "pending"  │
   │                          │                           │                            │
   │                          │                           │  ... build runs ...        │
   │                          │                           │                            │
   │                          │  POST /statuses/{sha}     │                            │
   │                          │◄──────────────────────────│  7. Set commit: "success"  │
   │                          │  POST /issues/{pr}/comment│                            │
   │                          │◄──────────────────────────│  8. PR comment with URL    │
```

## O que muda vs hoje

| Aspecto | Hoje (CLI-only) | Com GitHub App |
|---------|----------------|----------------|
| Deploy trigger | `loopzero deploy` (manual) | `git push` (automático) |
| Source upload | CLI bundles + uploads | API downloads do GitHub |
| Auth para source | User token | GitHub App installation token |
| Commit feedback | Nenhum | Status checks (pending/success/error) |
| PR feedback | Nenhum | Comment com URL + status |
| Setup | `loopzero login` + `loopzero init` + `loopzero deploy` | `loopzero connect` (one-time) |

## DB Schema

### Migration: project_repositories

```sql
CREATE TABLE IF NOT EXISTS project_repositories (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_name    TEXT NOT NULL,
    provider        TEXT NOT NULL DEFAULT 'github',  -- future: gitlab, bitbucket
    repo_full_name  TEXT NOT NULL,                    -- "owner/repo"
    repo_id         BIGINT,                           -- GitHub repo ID
    installation_id BIGINT NOT NULL,                  -- GitHub App installation ID
    default_branch  TEXT NOT NULL DEFAULT 'main',
    auto_deploy     BOOLEAN NOT NULL DEFAULT true,    -- deploy on push to default branch
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(repo_full_name)  -- one project per repo
);

CREATE INDEX idx_project_repos_tenant
    ON project_repositories(tenant_id, project_name);
CREATE INDEX idx_project_repos_installation
    ON project_repositories(installation_id);
```

### Migration: github_installations

```sql
CREATE TABLE IF NOT EXISTS github_installations (
    id              BIGINT PRIMARY KEY,               -- GitHub installation ID
    account_login   TEXT NOT NULL,                      -- GitHub user/org login
    account_type    TEXT NOT NULL,                      -- "User" or "Organization"
    tenant_id       TEXT REFERENCES tenants(id),        -- linked LoopZero tenant (nullable until linked)
    access_token    TEXT,                               -- encrypted installation access token
    token_expires_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## Fases e Microtasks

### Fase 0: GitHub App Registration
> Setup no GitHub, não é código

#### 0.1 — Criar GitHub App

**Onde:** github.com/settings/apps (ou org settings)

**Configuração:**
- Name: `LoopZero Platform`
- Homepage: `https://loopzero.example`
- Webhook URL: `https://api.dev.loopzero.example/api/v1/webhooks/github`
- Webhook secret: generate + store in Pulumi secrets
- Permissions:
  - Repository: Contents (read), Metadata (read), Pull requests (read+write), Commit statuses (read+write), Checks (read+write)
  - Organization: Members (read)
- Events: `push`, `pull_request`, `installation`, `installation_repositories`

**DoD:**
- [ ] GitHub App criado com App ID + private key
- [ ] Webhook secret gerado e armazenado
- [ ] Private key armazenada como secret no cluster (K8s Secret)
- [ ] App publicada (visível para instalação)

---

### Fase 1: Webhook Handler (Foundation)
> Recebe eventos do GitHub, valida signature, despacha

#### 1.1 — Webhook endpoint + HMAC validation

**Arquivo:** `api/internal/github/webhook.go`

**O que:**
```go
type WebhookHandler struct {
    secret []byte  // HMAC-SHA256 webhook secret
    // ... handlers for each event type
}

func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
    // 1. Read body
    // 2. Validate X-Hub-Signature-256 via HMAC-SHA256
    // 3. Parse X-GitHub-Event header
    // 4. Dispatch to event handler
    // 5. Return 200 immediately (async processing)
}
```

**Security:**
- HMAC-SHA256 validation obrigatória (rejeita requests sem signature)
- Body size limit (10MB)
- Event type whitelist (push, pull_request, installation)

**DoD:**
- [ ] `POST /api/v1/webhooks/github` endpoint registrado
- [ ] HMAC-SHA256 valida contra webhook secret
- [ ] Request sem signature → 401
- [ ] Request com signature inválida → 403
- [ ] Event type desconhecido → 200 (ignore, don't fail)
- [ ] Teste: valid signature → 200
- [ ] Teste: invalid signature → 403
- [ ] Teste: missing signature → 401

#### 1.2 — Event parser + dispatcher

**Arquivo:** `api/internal/github/events.go`

**Types:**
```go
type PushEvent struct {
    Ref        string     `json:"ref"`        // "refs/heads/main"
    After      string     `json:"after"`      // commit SHA
    Before     string     `json:"before"`     // previous SHA
    Repository Repository `json:"repository"`
    Sender     User       `json:"sender"`
    HeadCommit *Commit    `json:"head_commit"`
    Installation struct {
        ID int64 `json:"id"`
    } `json:"installation"`
}

type PullRequestEvent struct {
    Action      string      `json:"action"`      // "opened", "synchronize", "closed"
    Number      int         `json:"number"`
    PullRequest PullRequest `json:"pull_request"`
    Repository  Repository  `json:"repository"`
    Installation struct {
        ID int64 `json:"id"`
    } `json:"installation"`
}

type InstallationEvent struct {
    Action       string       `json:"action"`  // "created", "deleted"
    Installation Installation `json:"installation"`
    Repositories []Repository `json:"repositories"`
}
```

**DoD:**
- [ ] Parse `push`, `pull_request`, `installation` events
- [ ] Dispatch to typed handler functions
- [ ] Ignore unknown events gracefully
- [ ] Teste: parse real GitHub push payload
- [ ] Teste: parse real GitHub PR payload

---

### Fase 2: GitHub API Client
> Token management + API calls

#### 2.1 — Installation token management

**Arquivo:** `api/internal/github/app_client.go`

**O que:** Manage GitHub App authentication:
1. Generate JWT from App ID + private key
2. Exchange JWT for installation access token (`POST /app/installations/{id}/access_tokens`)
3. Cache token until expiry (1 hour)
4. Auto-refresh on expiry

```go
type AppClient struct {
    appID      int64
    privateKey *rsa.PrivateKey
    httpClient *http.Client
    tokenCache map[int64]*installationToken  // installation_id → token
    mu         sync.RWMutex
}

func (c *AppClient) GetInstallationToken(ctx context.Context, installationID int64) (string, error)
```

**DoD:**
- [ ] JWT generation from App ID + private key (RS256)
- [ ] Installation token exchange via GitHub API
- [ ] Token caching with TTL (expires_at from response)
- [ ] Auto-refresh on expiry
- [ ] Thread-safe (mutex on cache)
- [ ] Teste: JWT generation produces valid token
- [ ] Teste: token refresh after expiry

#### 2.2 — Source download via GitHub API

**Arquivo:** `api/internal/github/source.go`

**O que:** Download source tarball for a commit:
```go
func (c *AppClient) DownloadSource(ctx context.Context, installationID int64, owner, repo, sha string) (io.ReadCloser, error) {
    // GET /repos/{owner}/{repo}/tarball/{sha}
    // Returns redirect to tarball URL
    // Download tarball (follow redirect)
}
```

**DoD:**
- [ ] Download tarball for any commit SHA
- [ ] Follows GitHub redirect to actual tarball
- [ ] Streams response (no full buffer in memory)
- [ ] Size limit (500MB, matches MAX_BUNDLE_SIZE_BYTES)
- [ ] Teste: download tarball from public repo

#### 2.3 — Commit status updates

**Arquivo:** `api/internal/github/status.go`

```go
func (c *AppClient) SetCommitStatus(ctx context.Context, installationID int64, owner, repo, sha string, status CommitStatus) error {
    // POST /repos/{owner}/{repo}/statuses/{sha}
    // status: pending, success, failure, error
    // target_url: deploy URL
    // context: "loopzero/deploy"
}

type CommitStatus struct {
    State       string  // "pending", "success", "failure", "error"
    TargetURL   string  // URL to LoopZero deploy/logs
    Description string  // "Deploying..." / "Deployed to production"
    Context     string  // "loopzero/deploy" or "loopzero/preview"
}
```

**DoD:**
- [ ] Set commit status on any SHA
- [ ] States: pending, success, failure, error
- [ ] Target URL points to deploy/preview URL
- [ ] Context: "loopzero/deploy" (production) or "loopzero/preview" (branch)
- [ ] Teste: set status returns 201

#### 2.4 — PR comment

**Arquivo:** `api/internal/github/comment.go`

```go
func (c *AppClient) CommentOnPR(ctx context.Context, installationID int64, owner, repo string, prNumber int, body string) error {
    // POST /repos/{owner}/{repo}/issues/{pr_number}/comments
}

func (c *AppClient) UpdateComment(ctx context.Context, installationID int64, owner, repo string, commentID int64, body string) error {
    // PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}
}

func FormatDeployComment(projectName, env, url string, apps []string, duration time.Duration) string {
    // Returns markdown formatted comment
}
```

**Comentário exemplo:**
```markdown
### LoopZero Deploy ✓

| App | URL | Status |
|-----|-----|--------|
| api | https://my-app-api-production.dev.loopzero.example | ✓ Live |
| web | https://my-app-web-production.dev.loopzero.example | ✓ Live (CDN) |

Deployed in 42s from commit `abc1234`.
```

**DoD:**
- [ ] Post comment on PR
- [ ] Update existing comment (edit, not duplicate)
- [ ] Markdown formatted with app URLs
- [ ] Includes deploy duration and commit SHA
- [ ] Teste: format function produces valid markdown

---

### Fase 3: Repository Linking
> Conectar project LoopZero ↔ repo GitHub

#### 3.1 — DB migration: project_repositories + github_installations

**Arquivo:** `api/internal/db/migrations/000027_github_integration.up.sql`

**DoD:**
- [ ] Tables criadas sem erro
- [ ] Unique constraint em repo_full_name
- [ ] FK cascade correto
- [ ] Indexes para lookup por installation_id e tenant

#### 3.2 — Repository store

**Arquivo:** `api/internal/db/repository_store.go`

**Métodos:**
```go
type RepositoryStore interface {
    LinkRepository(ctx, tenantID, projectName, repoFullName string, installationID int64) error
    UnlinkRepository(ctx, tenantID, projectName string) error
    GetByRepo(ctx, repoFullName string) (*ProjectRepository, error)
    GetByProject(ctx, tenantID, projectName string) (*ProjectRepository, error)
    ListByInstallation(ctx, installationID int64) ([]ProjectRepository, error)
    SaveInstallation(ctx, installation *GitHubInstallation) error
    GetInstallation(ctx, installationID int64) (*GitHubInstallation, error)
}
```

**DoD:**
- [ ] Todos os métodos implementados
- [ ] Link/unlink idempotente
- [ ] GetByRepo retorna nil se não linkado

#### 3.3 — API endpoints: connect/disconnect

**Arquivo:** `api/internal/routes/repositories.go`

**Endpoints:**
- `POST /api/v1/projects/{name}/connect` — link repo to project
  - Body: `{"repo": "owner/repo", "installation_id": 12345}`
- `DELETE /api/v1/projects/{name}/connect` — unlink repo
- `GET /api/v1/projects/{name}/connect` — get linked repo

**DoD:**
- [ ] Connect rejeita se repo já linkado a outro project
- [ ] Connect requer owner/admin role
- [ ] Disconnect cleanup: remove webhook trigger
- [ ] Teste: connect + disconnect cycle

#### 3.4 — CLI: `loopzero connect`

**Arquivo:** `cli/src/commands/connect.ts`

**Flow:**
```
$ loopzero connect

  Select a GitHub repository to connect:
  > owner/my-app          (detected from git remote)
    owner/other-repo
    owner/another-repo

  ✓ Connected owner/my-app to project my-app
    Auto-deploy: enabled (push to main → production deploy)

  To disable auto-deploy: loopzero connect --no-auto-deploy
```

**DoD:**
- [ ] Detects GitHub remote from `.git/config`
- [ ] Lists repos accessible via installation
- [ ] Connects project to repo
- [ ] Shows auto-deploy status
- [ ] `--json` output

---

### Fase 4: Auto-Deploy on Push
> The core feature — git push triggers deploy

#### 4.1 — Push event handler

**Arquivo:** `api/internal/github/webhook_push.go`

**Flow:**
1. Parse push event → extract `ref`, `after` (SHA), `repository.full_name`, `installation.id`
2. Check: is this the default branch? (ignore other branches for now)
3. Lookup `project_repositories` by `repo_full_name` → get project + tenant
4. Check: `auto_deploy` enabled?
5. Download source tarball via GitHub API (2.2)
6. Parse loopzero.yaml from tarball
7. Upload to MinIO (reuse existing source upload path)
8. Submit build (reuse existing build orchestrator)
9. Set commit status: "pending"
10. Return 200 (build runs async)

**DoD:**
- [ ] Push to main → auto-deploy triggered
- [ ] Push to other branch → ignored (for now, previews later)
- [ ] Repo not linked → 200 (ignore, no error)
- [ ] Auto-deploy disabled → 200 (ignore)
- [ ] Commit status set to "pending" before build starts
- [ ] Build uses same pipeline as `loopzero deploy`
- [ ] Concurrent push protection (environment lock)
- [ ] Teste: push event to linked repo → build submitted
- [ ] Teste: push to non-linked repo → no build

#### 4.2 — Build completion callback → commit status

**Arquivo:** `api/internal/github/deploy_status.go`

**O que:** When build completes (via existing ArgoCD callback or convergence loop), update GitHub commit status.

**Integration point:** Hook into `ActivateReleaseKernel` or build status change:
```go
// After release becomes ACTIVE:
if repo := getLinkedRepo(tenantID, projectName); repo != nil {
    go githubClient.SetCommitStatus(ctx, repo.InstallationID, owner, repoName, commitSHA, CommitStatus{
        State:       "success",
        TargetURL:   deployURL,
        Description: "Deployed to production",
        Context:     "loopzero/deploy",
    })
}

// On build failure:
go githubClient.SetCommitStatus(ctx, ..., CommitStatus{
    State:       "failure",
    Description: "Build failed",
    Context:     "loopzero/deploy",
})
```

**DoD:**
- [ ] Build success → commit status "success" with deploy URL
- [ ] Build failure → commit status "failure" with logs URL
- [ ] Deploy timeout → commit status "error"
- [ ] Non-blocking (goroutine, failure doesn't affect deploy)
- [ ] Teste: mock GitHub API receives status update

#### 4.3 — Installation event handler

**Arquivo:** `api/internal/github/webhook_installation.go`

**Events:**
- `installation.created` → save installation to DB
- `installation.deleted` → remove installation + unlink all repos
- `installation_repositories.added` → log only (repos linked explicitly)
- `installation_repositories.removed` → unlink removed repos

**DoD:**
- [ ] New installation saved to DB
- [ ] Deleted installation cleans up all linked repos
- [ ] Teste: installation created + deleted cycle

---

### Fase 5: Config + Wiring

#### 5.1 — Config fields

**Arquivo:** `api/internal/config/config.go`

```go
// GitHub App Integration
GitHubAppID         int64  // GitHub App ID
GitHubAppPrivateKey string // PEM-encoded RSA private key
GitHubWebhookSecret string // HMAC-SHA256 webhook secret
```

**DoD:**
- [ ] Config loaded from env vars: `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY`, `GITHUB_WEBHOOK_SECRET`
- [ ] Graceful: if not set, GitHub integration disabled (CLI-only mode)

#### 5.2 — Wire in main.go + server.go

**Wiring:**
- Create `AppClient` with private key
- Create `WebhookHandler` with secret + handlers
- Register `POST /api/v1/webhooks/github` route (NO auth middleware — GitHub can't authenticate)
- Register repository routes under auth middleware
- Inject `AppClient` into build orchestrator for status updates

**DoD:**
- [ ] Webhook endpoint accessible without auth
- [ ] Repository endpoints require auth
- [ ] AppClient created when config present
- [ ] Graceful degradation when config missing

#### 5.3 — K8s Secrets for GitHub App credentials

**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts`

**DoD:**
- [ ] GitHub App ID, private key, webhook secret stored as K8s Secret
- [ ] Mounted as env vars in loopzero-api deployment
- [ ] Pulumi manages rotation

---

### Fase 6: CLI Updates

#### 6.1 — `loopzero connect` command

(Defined in 3.4)

#### 6.2 — `loopzero disconnect` command

```
$ loopzero disconnect

  ⚠ This will:
    - Stop auto-deploys from GitHub pushes
    - Remove commit status updates

  ✓ Disconnected owner/my-app from project my-app
```

**DoD:**
- [ ] Unlinks repo from project
- [ ] Confirmation required
- [ ] `--json` output

#### 6.3 — Update `loopzero status` to show GitHub connection

```
$ loopzero status

  Project:    my-app
  GitHub:     owner/my-app (auto-deploy: main)
  Last deploy: 2m ago (commit abc1234 by @user)
  ...
```

**DoD:**
- [ ] Shows connected repo + branch
- [ ] Shows last deploy source (push vs CLI)

---

### Fase 7: E2E Test

#### 7.1 — Full flow test

**Steps:**
1. Create GitHub App (test instance) or use smee.io for webhook forwarding
2. Install app on test repo
3. `loopzero connect` to link project
4. `git push` to test repo
5. Verify: build triggered, commit status set, deploy live
6. `git push` again → verify: old build superseded
7. `loopzero disconnect` → verify: push doesn't trigger deploy

**DoD:**
- [ ] Full push-to-deploy cycle works
- [ ] Commit status visible on GitHub
- [ ] Disconnect stops auto-deploy

---

## Sequenciamento

```
Fase 0 (GitHub App registration) — prerequisito manual
    ↓
Fase 1 (webhook handler) ──→ Fase 4 (auto-deploy)
    ↓                              ↓
Fase 2 (GitHub API client) ──→ Fase 4.2 (commit status)
    ↓
Fase 3 (repo linking) ──→ Fase 6 (CLI)
    ↓
Fase 5 (config + wiring)
    ↓
Fase 7 (E2E)
```

**Dependência crítica:** Fase 0 (manual) → Fase 1 + 2 (paralelo) → Fase 3 + 4 → Fase 5 → E2E

## Git Environment Variables

Quando o deploy é triggerado por push (não CLI), o build recebe env vars extras:

| Variable | Example | Source |
|----------|---------|--------|
| `LOOPZERO_GIT_PROVIDER` | `github` | Hardcoded |
| `LOOPZERO_GIT_REPO_SLUG` | `my-app` | Push event |
| `LOOPZERO_GIT_REPO_OWNER` | `acme` | Push event |
| `LOOPZERO_GIT_REPO_ID` | `117716146` | Push event |
| `LOOPZERO_GIT_COMMIT_REF` | `main` | Push event ref |
| `LOOPZERO_GIT_COMMIT_SHA` | `fa1eade4...` | Push event after |
| `LOOPZERO_GIT_COMMIT_MESSAGE` | `Update API` | Push event head_commit.message |
| `LOOPZERO_GIT_COMMIT_AUTHOR_LOGIN` | `user` | Push event sender.login |
| `LOOPZERO_GIT_PULL_REQUEST_ID` | `23` | PR event (empty for push) |

## UX Final

### Setup (one-time)

```bash
# 1. Install LoopZero GitHub App on your org/repo
# (click "Install" on github.com/apps/loopzero-platform)

# 2. Connect your project
loopzero connect
  ✓ Connected owner/my-app → project my-app (auto-deploy: main)
```

### Daily workflow

```bash
# Developer workflow — zero LoopZero commands needed
git checkout -b feature-x
# ... make changes ...
git push origin feature-x
# → GitHub shows: "loopzero/deploy — Pending" (commit status)
# → (future: PR comment with preview URL)

# Merge to main
git checkout main && git merge feature-x && git push
# → GitHub shows: "loopzero/deploy — Deployed to production ✓"
# → https://my-app-api-production.dev.loopzero.example is updated

# Check status (optional)
loopzero status
  Last deploy: 15s ago (push by @developer, commit abc1234)
  api → https://my-app-api-production.dev.loopzero.example ✓
```

## Resumo

| Fase | Tasks | Complexidade |
|------|-------|-------------|
| 0 | GitHub App setup | Manual (30min) |
| 1 | Webhook handler + HMAC | 2 tasks, Baixa |
| 2 | GitHub API client (tokens, source, status, comments) | 4 tasks, Média |
| 3 | Repo linking (DB, API, CLI) | 4 tasks, Média |
| 4 | Auto-deploy on push + status callback | 3 tasks, Média |
| 5 | Config + wiring | 3 tasks, Baixa |
| 6 | CLI updates | 3 tasks, Baixa |
| 7 | E2E test | 1 task, Média |
| **Total** | **20 tasks** | **Média-Alta** |
