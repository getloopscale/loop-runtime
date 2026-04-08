# Preview Environments — The Killer Feature

> PR creates full-stack preview: API + frontend + database + secrets + DNS.
> This is what makes Pro worth $29/mo. Without it, Pro = "Starter with more RAM."
> Estimated effort: 2-3 weeks.

---

## Problem

`PreviewEnvironment` CRD is defined (`operators/api/v1/preview_types.go`). The YAML schema exists (`operators/config/crd/bases/loopzero.io_previewenvironments.yaml`). The state machine exists (`pkg/statemachine/` — Preview: Creating → Active → Expiring/Deleting → Deleted).

**No controller exists.** There is no `preview_controller.go` in `operators/internal/controller/`. Sprint 5 was never started.

## What "Preview Environment" Means

When a developer opens a PR:
1. LoopZero detects the PR (via GitHub webhook or CLI trigger)
2. Creates an isolated environment: namespace on runtime cluster
3. Deploys all apps from `loopzero.yaml` (API + Web + Worker)
4. Creates a preview database (or clones from production schema)
5. Syncs secrets for the preview environment
6. Assigns a preview URL: `pr-42.preview.{domain}`
7. Posts the URL as a GitHub PR comment
8. When PR is merged/closed: auto-cleanup everything

## Tasks

### 1. PreviewEnvironment Controller

**Files:**
- New: `operators/internal/controller/preview_controller.go`

**Reconcile loop:**
- Watch `PreviewEnvironment` CRs
- On create: provision namespace + deploy apps + create IngressRoute + post URL
- On delete: cleanup namespace + IngressRoute + secrets
- TTL-based: auto-expire after configurable duration (default: 72h)

**DoD:**
- [ ] Controller reconciles `PreviewEnvironment` CRs
- [ ] Creates namespace `preview-{pr-number}` on runtime cluster
- [ ] Deploys all apps from loopzero.yaml into preview namespace
- [ ] Creates IngressRoute for preview URL
- [ ] Sets status conditions: `NamespaceReady`, `AppsDeployed`, `IngressReady`
- [ ] Handles delete: cleans up namespace and all resources
- [ ] TTL expiry: auto-deletes after 72h (configurable)
- [ ] Ginkgo tests with envtest

### 2. API Endpoint — Create/List/Delete Previews

**Files:**
- New: `api/internal/routes/previews.go`

**Endpoints:**
- `POST /api/v1/projects/{name}/previews` — create preview for a PR
- `GET /api/v1/projects/{name}/previews` — list active previews
- `DELETE /api/v1/projects/{name}/previews/{id}` — delete preview

**DoD:**
- [ ] Create preview from PR number or branch name
- [ ] List previews with URL, status, age
- [ ] Delete preview manually (before TTL)
- [ ] Requires Pro plan or higher (enforcement wired)

### 3. CLI Command — `loopzero preview`

**Files:**
- New: `cli/src/commands/preview.ts`

**Commands:**
- `loopzero preview create --pr 42` or `loopzero preview create --branch feature-x`
- `loopzero preview list`
- `loopzero preview delete <id>`
- `loopzero preview open` — opens the preview URL in browser

**DoD:**
- [ ] `loopzero preview create` triggers preview environment creation
- [ ] `loopzero preview list` shows active previews with URLs
- [ ] `loopzero preview open` opens browser
- [ ] `--json` output for all commands

### 4. GitHub Integration — PR Comment

**Files:**
- New: `api/internal/github/pr_comment.go`

**DoD:**
- [ ] When preview is ready, post comment on PR with preview URL
- [ ] When preview is deleted, update comment
- [ ] Comment includes: preview URL, services deployed, expiry time

### 5. Preview Database

**DoD:**
- [ ] Preview environment gets its own database (or schema)
- [ ] Schema cloned from production (structure only, no data)
- [ ] Migrations applied automatically
- [ ] Database cleaned up on preview deletion
