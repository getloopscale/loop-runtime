# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

LoopZero is a **CLI-first managed cloud (Kubernetes-backed)** for monorepo deployments. `loopzero login`, `loopzero init`, `loopzero deploy` — every service live with TLS, preview environments per PR, one-command rollback. No Kubernetes exposed to the user.

**This repo is the ENGINE** — API, operators, shared Go libs, Helm charts, infra. It is **not** the whole product.

| Was here, now extracted | Lives in | Extracted at |
|---|---|---|
| `cli/` (TypeScript CLI) | `../loopzero-cli` | M2 |
| `cloud/` + dashboard (control plane, billing, Ory) | `../loopzero-cloud` | M4 |
| `jwt` · `serviceauth` · `plan` contracts | `../loopzero-contracts` | M3 |

**Consequence:** if you are looking for a CLI command, a dashboard screen, Stripe billing, or a shared contract type, it is **not in this repo**. Do not re-implement it here. The decomposition ADR is `../decisions/2026-06-16-monorepo-decomposition.md` (umbrella root — **not** this repo's own `.claude/knowledge-base/decisions/`); the workspace contract is `../CLAUDE.md` (this used to also name an umbrella-scope rule file; it does not exist in the umbrella either — verified 2026-08-16, B-052).

**Cycle artifacts (plans, reviews, audits, ADRs) and the CHANGELOG live in THIS repo** — `.claude/records/` and `CHANGELOG.md`. `.claude/knowledge-base/` is the same trail up to 2026-08-28, frozen (see below).

This paragraph said the opposite until 2026-08-19, and both halves of it were false (B-159). Measured: `../CHANGELOG.md` **does not exist**, `../.claude/knowledge-base/` **does not exist**, and **all 21 sibling repos keep their own `CHANGELOG.md`** — this one has 4,567 lines and took 227 commits since 2026-08-01 alone. The instruction named a path nothing resolved to, while the repo's overwhelming practice did the opposite.

It also contradicted two neighbours: the paragraph immediately below points at *this* repo's `CHANGELOG.md` and `.claude/knowledge-base/` as the dated history, and the kit's rule file states it outright. That file was `knowledge-base-location.md` when this paragraph was written; the kit has since renamed the convention, and the rule now lives at `.claude/rules/records-location.md` saying *"`<project>/.claude/records/` is canonical. Always"*. The point stands unchanged — the canonical tree is THIS repo's, and consumers do **not** share one. WHICH of the two directories on disk is the live one was `B-166`; it is settled: `.claude/records/`. Per this file's own precedence rule, the tree won — every cycle artifact since 2026-09-03 landed there, and `.claude/knowledge-base/` has taken no write since 2026-08-28.

What genuinely lives at the umbrella root is narrower: the decomposition ADR named above, and `../CLAUDE.md`, the workspace contract.

**The codebase is authoritative.** Where this file and the code disagree, the code wins. `CHANGELOG.md` (1 MB, exhaustive), `.claude/records/` (live) and `.claude/knowledge-base/` (frozen archive) hold the dated history — this file deliberately does not restate it.

## Multi-module Go workspace (read before any build command)

`go.work` (Go **1.26.1**) unions **8 modules**, one of which is a **sibling repository**:

```
../loopzero-contracts   ← SIBLING REPO. Absent → the workspace does not build.
./api  ./pkg  ./operators  ./operators/api  ./cmd/loopzero-ops
./infra/scripts  ./infra/tests
```

There is **no `go.mod` at the repo root**. `go build ./...` / `go test ./...` from the root does **not** cover the repo — run them per module, or use the Taskfile.

## Development Commands

```bash
task doctor              # environment health check — run this FIRST
task setup               # ONE-SHOT cold start: 4 k3d clusters + Helmfile sync (~30-45min)
tilt up                  # DEV LOOP: hot reload of API/operators (canonical day-to-day)
                         # ONE-SHOT vs DEV LOOP em detalhe: docs/operations/dev-loop.md
task status              # clusters, pods, DB, API at a glance
```

### Tests

```bash
task test                     # unit + lint + manifest validation
task test:unit                # unit only
task test:e2e                 # Ginkgo E2E (requires running clusters + tilt up)
task test:integration         # requires running clusters
task test:coverage            # HTML coverage per component

# Per module (the root does NOT cover them — see § Multi-module):
cd api        && go test ./...
cd pkg        && go test ./...
cd operators  && go test ./...   # Ginkgo + envtest
cd cmd/loopzero-ops && go test ./...

# A single Go test / package
cd api && go test ./internal/services/... -run TestScalingResolver -v

# A single Ginkgo spec (E2E). NOTE: `go test -timeout` alone does NOT extend the
# Ginkgo suite timeout (its own default is 60min) — pass -ginkgo.timeout too:
cd infra/tests && go test ./e2e/... -v -timeout 120m \
  -args -ginkgo.timeout=2h -ginkgo.focus="deploy happy path"
```

### Quality gates — the CI substitute

GitHub Actions was removed as the pipeline in 2026-05-23. **4 workflows remain** (`build-publish.yml`, `release-artifacts.yml`, `workflow-hygiene.yml`, `ci.yml`) — this line said **3** and named the first three until 2026-08-19, and the fourth had been added deliberately in the meantime. Three of them are not the quality gate; **`ci.yml` is**, and its own header says why: the gates moved to a pre-push hook that runs *on the pusher machine and only there*, so a repository with no GitHub gate has none for anyone else. The gate inventory is **140 wired quality gates** in `cmd/loopzero-ops` (measured 2026-08-19 via `--list`, the oracle), run locally or by whatever CI is wired:

```bash
task quality:gates            # full-repo scan
task quality:diff             # PR-diff scoped (BASE_REF=<ref>, default origin/main)
task quality:all              # hermetic tests + full scan + contract gates
task quality:contract:cross-repo   # decomposition seam contracts across sibling repos
task quality:build            # builds the gate binary to /tmp/loopzero-ops — do this FIRST
/tmp/loopzero-ops quality --list  # gate inventory (NAME / WIRED / SUMMARY) — the oracle
/tmp/loopzero-ops quality <gate>  # run one gate

task lint                     # golangci-lint v2.11+ + ESLint
task validate:manifests       # kubeconform --strict
task db:migrate               # forward-only migrations (golang-migrate)
task db:reset                 # drop + recreate + migrate + seed
task db:generate              # regenerate api/internal/db/generated/ via sqlc
```

A stray `./loopzero-ops` at the repo root is a **gitignored local build artifact**, not the gate binary — it can be arbitrarily stale (one measured on 2026-07-30 reported 73 gates against a source tree carrying 108; the tree carries 122 as of 2026-08-12) and is absent in a fresh clone. Always build first; never read `./loopzero-ops` as the oracle.

**Adding a new `.sh` file is FORBIDDEN by default** (`no-new-bash` gate, D3). New logic belongs in `cmd/loopzero-ops/internal/quality/` (Go — this is where the gates live; the old `pkg/quality/` was relocated out of the shared base at M9), `infra/helmfile/` (declarative), or `infra/tests/e2e/` (Ginkgo). The allowlist + justification contract is `.claude/rules/shell-scripts-allowlist.md`.

**Prerequisites:** Go 1.26+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, **helm-diff plugin**, **Helmfile v1.0+**, kubectl, kubeconform, golangci-lint v2.11+, cosign (optional).

## Architecture Overview

### Four Planes

```
DEVELOPER PLANE   — ../loopzero-cli (TypeScript) ↔ api/ (Go)
CONTROL PLANE     — mgmt cluster: 4 operators · Gitea · PG · MinIO · ArgoCD · Traefik · cert-manager
BUILD PLANE       — build cluster: Argo Workflows + BuildKit + cosign signing
WORKLOAD PLANE    — runtime cluster(s): user apps via ArgoCD · Traefik + cert-manager + KEDA + Argo Rollouts (canary)
SECRETS           — SEPARATE `loopzero-secrets` cluster: OpenBao (Transit) + ESO. CONDITIONAL — see below.
```

`task setup` creates **4** k3d clusters (`infra/k3d/{core,build,cell-0,cell-1}-cluster.yaml` — the names on disk since the B-001 rename; the old `{mgmt,workload,runtime-2}` appear in no filename). **OpenBao runs in a 5th, separate cluster** (`infra/k3d/secrets-cluster.yaml`, `task k3d:secrets:create` + `k3d:secrets:bootstrap`). Installing it into core (`k3d:secrets:bootstrap-in-core`) is **FORBIDDEN in every tier** — it is a topology violation, not a shortcut.

**The 5th cluster is CONDITIONAL, and this used to read as if it were not.** Three things, all measured 2026-09-09:

1. **The composition is gated, default off.** Four releases in `secrets.helmfile.yaml.gotmpl` carry `installed: {{ .Values | get "openbaoEnabled" false }}` (`openbao-cert`, `openbao`, `openbao-init`, `openbao-snapshot-agent`). The flag is set in **1 of 8** environment files — only `vault-local.yaml` — and the only caller is `taskfiles/k3d.yml`, always `-e vault-local`. The helmfile header names the production target as *"DOKS `loopzero-secrets-prod` **in future**"*. OpenBao **is** on the production path via `infra/helm/loopzero-external-secrets` (ESO → loopzero-api broker → OpenBao transit) with its backup bucket in `Pulumi.prod.yaml`; what is gated is the composition that stands the cluster up.
2. **The secret backend is per-installation, by contract.** `charts/loopzero-api/SECRETS.md` and `charts/loopzero-cloud/SECRETS.md` state that secrets are set *"per installation, in your target's native store (AWS Secrets Manager / GCP Secret Manager)"*, and the AM6.5 anti-lock-in ADR makes ESO **recommended, not required**. So OpenBao is this platform's operational choice, **not a product requirement** — a self-hoster bringing their own secret store runs a **4-cluster** topology.
3. **The decision now has a home; the rule this paragraph used to cite does not exist.** The separation and its conditionality are recorded in `docs/decisions/2026-09-09-the-secrets-cluster-is-separate-and-conditional.md`, which supersedes § Q2 of the ratified adoption ADR (that ADR decided *"not dedicated DOKS cluster"* and was never amended — #357). `.claude/rules/cleanroom-up-live-test-mandate.md` resolves nowhere in the workspace (`.claude/` is not in this repository), so the citation was replaced rather than left dangling. The prohibition above holds for **our** topology; what has no readable source is the "every tier" wording.

### Deploy flow (happy path)

```
loopzero deploy → CLI validates → upload bundle → API locks env → submit Argo Workflow
  → build (BuildKit) → cosign sign → push image → generate manifests
    (Rollout + 2 Services + TraefikService + AnalysisTemplate when ProgressiveDelivery=true;
     legacy Deployment when LOOPZERO_PROGRESSIVE_DELIVERY=false)
  → push to Gitea (gitops) → ArgoCD sync → Argo Rollouts orchestrates canary steps
    (per-plan: Starter 50→100, Pro 20→Analysis→50→100, Team/Enterprise 10→Analysis→30→60→100)
  → SSE rollout.step events during canary; rollout.aborted (exit 3) on Degraded/Aborted
  → app Healthy → SSE deploy.completed → CLI shows URL
  (static apps: build → upload S3 → write CF KV → CF Worker serves from DO Spaces)
```

### Components

| Path | What |
|---|---|
| `api/internal/` | ~40 subpackages. Notable: `routes` (43 files), `services`, `lock`, `placement`, `cellmgr`, `gitops`, `argo`, `manifests`, `metering`, `sse`, `source` |
| `operators/` | 4 split binaries (`operators/cmd/{tenant,app,preview,platform}-operator`) — kubebuilder + controller-runtime |
| `operators/api/` | CRD Go types (own module) |
| `pkg/` | Shared libs: `statemachine`, `errors`, `naming`, `conditions`, `enforcement`, `circuitbreaker`, `healthcheck`, `id`, `logging`, `domain`, `events`, `vault`, `shellrun`, `pulumi`, `correlation` |
| `cmd/loopzero-ops/` | Ops + quality binary: **126** wired quality gates (134 subpackages — the difference is helpers and not-yet-wired checks; `loopzero-ops quality --list` is the oracle) + `seed` / `drill` / `pulumi` / `chaos` subcommands |
| `infra/helm/` (44 dirs) | Bootstrap charts — installed by **Helmfile L2** |
| `charts/` (12) | Published OCI charts — consumed by **ArgoCD L3**; use the `loopzero-common` library |
| `infra/tests/` | Ginkgo e2e, chaos, load (k6), unit (own module) |
| `tools/` | `gen-service-auth-ed25519` (era um par; `loopzero-dockerfile-lint` saiu em 2026-08-19, B-157 — validava Dockerfile do usuario, caminho removido na T2.2) |

### CRDs (8, all wired)

Tenant · Environment · LoopZeroApplication · ExposedRoute · PlatformBootstrap · GitRepositoryBinding · ClusterCredential · PreviewEnvironment.

The 2 historical orphan CRDs (AppSecret, ReconciliationReport) were deleted 2026-05-13 per ADR `.claude/knowledge-base/decisions/2026-05-13-orphan-crd-deletion.md` (this repo's own ADR tree).

### State machines (11, `pkg/statemachine/`)

Build · Release · Tenant · PlatformBootstrap · Preview · Promotion · Rollback · Domain · Environment · Application · ExposedRoute.

Each has a `*_typed.go` companion. **The code is the spec** — read it for the full state set (terminal states, failure/recovery edges, timeouts). Never set status directly; use `Transition(event)`. Invalid transitions **panic** (programming error, not runtime error).

### Single-status-writer invariant (operator reconcilers)

A CRD's `.status` has exactly ONE writer, which persists with a **merge patch off a base snapshot** taken right after the initial `Get`:

```go
base := obj.DeepCopy()          // right after r.Get, BEFORE mutating .Status
// ... mutate obj.Status ...
r.Status().Patch(ctx, &obj, client.MergeFrom(base))
```

A merge patch carries no `resourceVersion`, so it never 409s against a concurrent spec/metadata write by another controller. **Never** use the refetch-then-restore `Status().Update` dance (lost update), and **never** `return ctrl.Result{}, nil` on a status-write failure — return the error and requeue. Reference: `operators/internal/environment/environment_controller.go` + its `_conflict_test.go`. **The section above is the full contract** — `loopzero-design.md` was deleted in `1c941b91f` and this text is what survived it (B-052).

### Multi-cell routing — placement once, route many

- **Placement is sticky and one-time.** `loopzero bootstrap` runs `RoundRobinEngine.AssignAndExecute` (`api/internal/placement/engine.go`) inside a SERIALIZABLE txn with `pg_advisory_xact_lock`; bin-packing picks the cell with fewest tenants under `maxPerCell`. Writes `tenants.cell_id` (canonical SoT) + Tenant CR `spec.cellID`.
- **Routing is pure lookup, zero decision.** `DynamicRouter.Resolve(cellID)` (`api/internal/placement/dynamic_router.go`) is a hash lookup returning kubeconfig / ArgoCD URL+token / base domain.
- **`loopzero admin tenants pin` is the only path that moves a tenant** — DB update + Tenant CR MergePatch atomically, writes `audit_log`.
- `ClusterCredential` CRDs are looked up by **`spec.cellID`, NOT by `metadata.name`** (a CR named `workload` with `spec.cellID=cell-0` is valid).
- **No automatic rebalancing by design** (ADR-0008 / ADR-0021 D3): DNS continuity, state affinity, hot-spot = upgrade-tier signal, auditability.

### Helm chart dual system

`infra/helm/` (bootstrap, Helmfile L2) and `charts/` (published OCI, ArgoCD L3) coexist by design (ADR D1). **2 charts exist in BOTH trees under the same `name`** — `loopzero-api` and `loopzero-cloud`: when patching one, apply the same fix to the other. Refer to each by its **path**, never by the bare name — `infra/helm/loopzero-api` is the bootstrap one, `charts/loopzero-api` is the published OCI one.

This used to read **3**, naming `loopzero-admission-policies` as the third. Measured 2026-08-18 (B-056): `infra/helm/loopzero-admission-policies` **does not exist** — the chart lives only in `charts/`, so there is no other side to patch. A parity contract that names a pair with one side dilutes the two that are real. The pair list now lives in `infra/quality/chart-pairs.txt`, and the `chart-pairs` gate refuses a pair that is undeclared **or** a declaration that outlived its pair — because this sentence drifted once already. `monitoring` / `loopzero-monitoring` are complementary (infra vs config), not overlapping.

### Infrastructure — AWS re-platform in progress

> **CTO mandate 2026-06-17** (ADR `../decisions/2026-06-17-infra-state-decomposition-aws.md`, ACCEPTED). The **L1 cloud-primitives layer** is being re-platformed **DigitalOcean/Pulumi → AWS/Terraform**, into two sibling repos: `../loopzero-infra-modules` (versioned blueprints) + `../loopzero-infra-live` (per-env Terragrunt units, per-component state isolation). Tooling is **Terraform**, not OpenTofu, not Pulumi.
>
> `infra/pulumi*` is **AS-IS legacy — live until the AM8 cutover**. Do not cite Pulumi as the forward path for infra construction, and do not add new Pulumi for target infra. The **in-cluster platform layer is unchanged**: Helmfile L2 + ArgoCD L3 + charts + k3d dev all stay. Roadmap: `../ROADMAP_AWS.md` (AM0-AM9, strangler-fig).

Layering today: **L1** cloud primitives (Pulumi/DO legacy → Terraform/AWS target) · **L2** Helmfile (SoT for ~32 platform releases) · **L3** ArgoCD (tenant Day-2 GitOps, manual sync, from Gitea).

## The Rules (Non-Negotiable)

1. **Tests before code** — no production code without a failing test. Write it, run it, confirm red, then implement.
2. **Code is authoritative** — read the code + `CHANGELOG.md` + ADRs. Never invent behavior not present in the code.
3. **No internal terms in user-facing output** — forbidden in CLI output, error messages, `--json`: `ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`. Use product language: deploy, team, account, service, revert, build.
4. **Exit codes are permanent** — `0` success, `1` usage, `2` build failure, `3` deploy failure, `4` auth, `5` partial failure. No others.
5. **`--json` is a stability contract** — field names cannot change without a major bump. New fields may be added.
6. **Every mutation uses the environment lock** — `withEnvironmentLock` for all `(tenant, project, environment)` mutations. The concurrent-build check MUST be **inside** the lock.
7. **`pg_advisory_xact_lock` only** — never session-scoped `pg_advisory_lock` (breaks with connection pools).
8. **State machines are strict** — use the state machine functions, never direct status updates.
9. **CLI is the primary interface** — every feature lands in `../loopzero-cli` before the dashboard. Dashboard-only features are product bugs.
10. **Local equals production** — same Helm charts, same operators, same build pipeline. No Docker Compose shortcuts, no "dev mode" with different behavior.
11. **Claims need evidence** — any claim affecting a maturity score requires **3 evidence sources** (T1 commit+test, T2 LIVE log, T3 CI run). Maturity-claim confidence cap **90-95%**. This is separate from — and looser than — the **95%+ implementation confidence** gate (Unbreakable Principle 1, `~/.claude/CLAUDE.md` Part I § 1 — the `workflow.md` this used to cite was deleted in `1c941b91f`), which applies to all code changes.
12. **Tool adoption claims need grep, not memory** — before classifying any tool as "adopted" or "gap", run `grep -ril "<tool>" infra/ charts/ taskfiles/`. The authoritative inventory is the filesystem: `infra/helm/`, `charts/`, `infra/helmfile/environments/*.yaml`, `taskfiles/*.yml`. **This file is a curated summary, NOT a tool inventory** — classifying from memory produces false positives (this rule exists because Velero/Trivy/Polaris/kubeconform/Renovate/TruffleHog were once all misclassified as gaps while already adopted).

## Locked rules — read before the matching work

| Before you… | Read |
|---|---|
| touch any LoopZeroKit ↔ LoopZeroCloud seam — `api/internal/routes/source.go`, `api/internal/buildplane/source/services.schema.json`, `api/internal/buildplane/source/validator_*.go`, `api/internal/manifests/renderer.go`, or anything touching `services_json` / the multipart upload contract | `docs/architecture/loopzerokit-loopzerocloud-integration.md`. This doc used to be mirrored on the LoopZeroKit side; **that mirror no longer exists anywhere in the workspace**, so the copy here is the only one. If you re-establish a mirror, restore the "edit one, copy the diff in the same commit" rule with it. |
| declare a journey complete, cut a release, or run `/cleanroom` `/dogfood full` `/chaos-test` | `.claude/rules/steady-state-baseline.md` — a 4-layer baseline snapshot is **mandatory** at every milestone boundary (code · runtime · config · LIVE functional probes). Omitting any layer invalidates it. |
| run any LIVE validation | `.claude/rules/cleanroom-up-live-test-mandate.md` — INQUEBRÁVEL: Cleanroom TOTAL → UP → seeds → DOGFOOD FULL → LIVE TEST → QA Red Team. Warm-infra results are not LIVE-valid. |
| implement a T2/T3 change (schema, state machine, API contract, CLI output, reconcile logic, Helm, Pulumi, `pkg/`, credentials, multi-cluster) | `.claude/rules/pre-release-governance.md` — classify the tier first; T2+ requires a meeting **before** code. |
| write public copy (`PITCH.md`, loopzero.example, launch material) | `.claude/rules/public-copy.md`. Cross-Project Rule 9: never expose `Kubernetes`, `cluster`, `ArgoCD`, `Cosign`, `KEDA`, `Gitea`, `MinIO`, `Pulumi`, `Traefik`, `Ory` in user-facing copy — internals belong in a DEEP DIVE / Self-host section only. `PITCH.md` states this contract in its own header. |

Other contracts live in `.claude/rules/` — `testing.md`, `error-handling.md`, `git-safety.md`, `shell-scripts-allowlist.md`, `architecture.md`, `parsimony-ladder.md`, `public-copy.md`, `pre-release-governance.md`.

Five files this list used to name (`workflow.md`, `tdd-workflow.md`, `loopzero-design.md`, `cli-output.md`, `design-principles.md`) were **deleted together** in `1c941b91f` ("Remove docs inconsistent") and are **not coming back** — restoring them would restore documentation someone judged inconsistent. Their subjects did not disappear with them; each is repointed (B-052):

| was | read instead |
|---|---|
| `workflow.md` (95% confidence, task completion, honesty, git) | the Unbreakable Principles in `~/.claude/CLAUDE.md` Part I, plus `git-safety.md` for the git half |
| `tdd-workflow.md` (RED→GREEN→REFACTOR, pyramid, naming) | `testing.md` for the discipline, `cycle-implement.md` for the loop |
| `loopzero-design.md` (four planes, state SoT, single-status-writer) | this file — § Four Planes, § State machines, and § Single-status-writer invariant below **are** the contract, not a summary of one |
| `cli-output.md` (internal terms, exit codes, `--json`) | Rules 3, 4 and 5 of this file; `public-copy.md` for the copy side |
| `design-principles.md` (Don't Reinvent, KISS, YAGNI, DRY, SOLID) | `~/.claude/CLAUDE.md` Part II, operationalised by `parsimony-ladder.md` |

`docs/README.md` is the index for `docs/` — it also states which subtrees are **living reference**
(fix them when they drift) and which are **audit trail** (`plans/completed/`, `operations/baselines/`,
`meetings/records/`, `operations/findings/`, `reviews/`) that must never be edited to match the
present. Rewriting a dated record to look current destroys the only evidence of what was true then.

## Records (`.claude/records/` live, `.claude/knowledge-base/` frozen)

Both store **WHY** — decisions, trade-offs, rationale that cannot be derived from code. Evidence is classified `[MEASURED]` / `[REPORTED]` / `[ESTIMATED]`. Max 3 pages updated per task; never auto-generate as a coding side effect — **this sentence is the rule**; the `auto-docs.md` it used to cite does not exist (B-052).

**Write to `.claude/records/`.** `.claude/rules/records-location.md` states it without a per-project option — *"`<project>/.claude/records/` is canonical. Always."* — and the kit's boundary hook lists `records/` among the paths a consumer owns, which `knowledge-base/` is not.

**`.claude/knowledge-base/` is a read-only archive** of the same trail up to 2026-08-28. It is frozen rather than deleted because it holds 149 records with **zero** overlap with `.claude/records/` — no file in either tree has a byte-identical twin in the other, measured 2026-09-04 — so each is the only copy of its evidence, and 59 citations elsewhere in this repo still resolve into it. Its `.md` set is pinned by `.claude/rules/records-archive-frozen.txt` and enforced by `loopzero-ops quality records-tree-split`, which fails both ways: a record ADDED there means the split reopened, a record REMOVED means evidence was destroyed. `decisions/` (48 ADRs) is the subtree most often cited from it.

This section used to send readers to `index.md`, `concepts/` and `references/` inside the archive. None of the three exists — verified 2026-09-04, B-166, same defect class as the `auto-docs.md` citation above.

## Pre-completion checklist

```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] task quality:gates passes (or quality:diff for a scoped change)
[ ] No internal terms in user-facing output (Rule 3)
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes go through Transition()
[ ] CHANGELOG entry in THIS repo's `CHANGELOG.md`, under `[Unreleased]` (B-159 — this said "in the UMBRELLA (../CHANGELOG.md)" until 2026-08-19, and that file does not exist)
```

**Bash blocked:** `kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`, `git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

---

## Known Systemic Patterns

| Pattern | Mitigation |
|---------|-----------|
| "Implemented but not wired" | Verify call site after implementation; integration test required |
| "Correct code in wrong place" | Single source of truth per behavior; grep duplicates before merge |
| "Project name vs project ID" | CLI sends project name as `project_id` in builds. Use name (not UUID) for build/release queries. Only `projects` table uses UUID. |
| "ArgoCD `notifiers` not `services`" | argo-cd Helm v7.x: use `notifications.notifiers`, `services` silently ignored |
| "Single ArgoCD App per tenant" | One App per environment. Any Degraded resource blocks ALL notifications. `gitops.Remove` failure is FATAL. |
| "Implemented but not documented" | Code is source of truth. Verify before claiming missing. |
| "Custom domains = CF Zone per domain" | v2: CF Zone Mgmt, NS change by user, no cert-manager Certificate (CF handles TLS). Full state machine: see § State Machines (Domain row) — includes v1 (TXT verification) backward-compat flow + degraded/failed/removing states. |
| "CF scan imports conflicting apex" | v2 create flow deletes conflicting A/AAAA/CNAME at apex BEFORE creating routing CNAME |
| "Live vs snapshot credentials" | Prefer fetching the live source at reconcile time over snapshotting at bootstrap. Snapshots go stale; live sources self-heal. See `kube-root-ca.crt` pattern for cluster CAs (`operators/internal/platform/ca_fetcher.go`, plan 2026-05-25-live-ca-fetch-clustercredential). |
| "Bootstrap circular dependency (X-fetches-X)" | A credential fetcher MUST NOT use the credential it fetches. Example: fetcher reading `kube-root-ca.crt` to refresh stale CA must connect with `Insecure: true`, NOT with the stale CA itself — otherwise TLS handshake fails first and fetch never runs (Op B EC-13 trap). |
| "Validation procedure proves nothing" | Procedures that "test" CA rotation by restarting k3s container do NOT actually rotate the CA (it's persisted on volume). Real rotation requires `rm /var/lib/rancher/k3s/server/tls/server-ca.{crt,key}` before restart. Always assert pre/post hash differ to prove the failure mode was actually triggered. |
| "GitHub tarball ≠ git bundle" | autodeploy.go: tarball → extract → git init/add/commit → bundle → S3 |
| "Auto-deploy source = commit SHA" | Key `{tenant}/source/{commitSHA}.bundle`. CommitSHA from webhook must match exactly. |

---

## Common Mistakes — Read Before Coding

> Rows citing `cli/…` or `cloud/…` paths describe lessons whose code now lives in the sibling repos (`../loopzero-cli`, `../loopzero-cloud`). The lesson still applies — the path moved.

| Mistake | Fix |
|---------|-----|
| `pg_advisory_lock` (session-scoped) | Use `pg_advisory_xact_lock` |
| Default lido do fallback do construtor em vez da config | `placement/engine.go:91` tem `if maxPerCell <= 0 { maxPerCell = 500 }` — é o **fallback**, não o valor entregue. O real é `config.go:497` (`envInt("MAX_TENANTS_PER_CELL", 5000)`) + `charts/loopzero-api/values.yaml:121`. O simulador de capacidade leu o 500 e inflou a contagem de cells em 10x, produzindo um gargalo `cell_count` inexistente e uma "Fase 1" que seria um downgrade. Ao citar um default, cite a **config**, não a guarda defensiva. |
| Expressão CEL sobre `object.` numa regra `DELETE` | Em `DELETE` o `object` **não vem populado** como em CREATE/UPDATE. A expressão levanta erro de avaliação e, sob `failurePolicy: Fail`, isso vira **bloqueio geral**. Use `oldObject` quando precisar inspecionar o recurso, ou `request.*` para decidir por identidade. Terceira armadilha CEL da família (as outras duas: `!has(request.subResource)` e as isenções de GC do B33). Gate `vap-delete-guardrail` trava. |
| Lista de isenções vazia num `matchCondition` de negação | A forma `!(request.userInfo.username in [...])` com lista **vazia** é `!(x in [])` — **sempre true**. A política passa a rodar para toda identidade e nega **tudo**, inclusive dos operators. Um piso mínimo de identidades é asserção de gate, não convenção. |
| Testar admission policy com o kubeconfig padrão do e2e | O kubeconfig de k3d é admin, membro de `system:masters` — **grupo exempto** nos VAPs. O teste passa *porque o delete foi permitido*, não porque foi bloqueado. Use `--as=system:serviceaccount:<ns>:<sa>` com uma SA sem isenção, **e conceda RBAC a ela**: sem RBAC o `Forbidden` vem do RBAC e não prova nada sobre a política (a mensagem diz `cannot delete resource ... at the cluster scope`; a da política nomeia a policy). |
| Concurrent build check OUTSIDE lock | Move inside `withEnvironmentLock` |
| `errors.New("...")` | Use `LoopZeroError` with typed code |
| `release.Status.State = "active"` | Use `release.Transition(event)` |
| `kubectl` in user-facing flow | Proxy through API |
| Mark release ACTIVE before ArgoCD confirms | Wait for sync confirmation |
| Internal terms in CLI output | Product language (Rule 3) |
| `notifications.services` in ArgoCD Helm | Use `notifications.notifiers` |
| `project.ID` (UUID) for build/release queries | Use project name from `loopzero.yaml` |
| `gitops.Remove` error treated non-fatal | FATAL — delete must fail |
| cert-manager Certificate for CF-managed domains | Skip Certificate CR (use `CFManaged`) |
| Forward-only migration violations (`DROP`) | Explicit sign-off required |
| Writing prod code before failing test | Test first, always |
| Multi-cell ClusterCredential CR name = cellID assumption | Wrong — `K8sCellStore` lookup is by `spec.cellID`, not metadata.name. CR named `workload` with `spec.cellID=cell-0` works fine (per H12). |
| `wire-clusters.sh` with `cluster-name=cell-0` for k3d | Wrong — local cluster is `loopzero-cell-0`. Use `cluster-name=cell-0` so `scoped-kubeconfig.sh` resolves `k3d-loopzero-cell-0-server-0` docker container (B82 + H12). |
| Cached SA token in ClusterCredential Secret stops working before exp claim | SA UID changed (recreated during rollout). Mint fresh: `kubectl create token loopzero-system/default --duration=24h` + patch Secret data.kubeconfig (per H13). |
| ArgoCD `InvalidSpecError: project X does not exist` (but project EXISTS) | Repo-server cached `app path does not exist` for ~5min. `SyncClient.TriggerSync` calls `?refresh=hard` BEFORE POST /sync (B32). |
| VAP `failurePolicy: Fail` deadlocks namespace deletion | Add `matchConditions` excluding `system:serviceaccount:kube-system:namespace-controller`, `system:kube-controller-manager`, `system:masters` group (B33). |
| `secret K8s manager not available — ESO sync disabled` in api logs | `CELL_0_KUBECONFIG` not mounted. Mount `loopzero-cluster-kubeconfigs` Secret (key=`cell-0.kubeconfig`) + export `CELL_0_KUBE_CONTEXT=in-cluster` (B34). |
| `seed-local-user.sh failed during regen` mid-journey | `HYDRA_NAMESPACE=loopzero-auth` (prod default) ≠ `loopzero-system` (k3d standalone). Set `SEED_HYDRA_NS=loopzero-system` in `_common.sh` (B35). |
| `releases_status_check` constraint violation on rollback | DB CHECK missing `ROLLING_BACK`/`ROLLED_BACK`. Migration `000010_release_rollback_states.up.sql` adds them. State machine declares it but DB CHECK was never updated (B37). |
| Admin endpoints return 404 `Endpoint not available` despite valid X-Service-Auth | `LOOPZERO_ADMIN_API=true` env var missing (defense-in-depth flag). Production sets via Pulumi; standalone Tilt YAML must include it (B38). |
| `kubectl rollout restart` is non-blocking — race with downstream operations | Always pair with `kubectl rollout status` if subsequent commands trigger reconciles in old/new pod. Old pod (env stale) reconciles BEFORE termination, sobrescreve writes (B39). |
| Convergence loop / health checker filters `res.Kind != "Deployment"` only | Wrong — workload primitive is now Deployment OR Rollout (default-on progressive delivery). Accept BOTH (`res.Kind != "Deployment" && res.Kind != "Rollout"`) or every Rollout-backed release stays PENDING forever (F-19). |
| `kubectl get deploy` in test scaffolding to verify workload exists | Wrong post-canary plan — query `deployments.apps,rollouts.argoproj.io` together. ProveDead helpers must include both kinds + TraefikService (F-20). |
| Adding a cell-0-cluster Helm release without updating `task k3d:bootstrap-cell-0` selector | The standalone `task setup` only syncs charts named in the explicit `--selector name=X` list. New runtime-cluster releases (e.g., argo-rollouts) MUST be added to that list, otherwise fresh cleanroom never deploys them (F-21). |
| New tenant-side CRD requires applying it via ArgoCD without granting `loopzero-ctrl-runtime` RBAC | The scoped kubeconfig's ClusterRole `loopzero-ctrl-runtime-rbac-admin` (`infra/helmfile/scripts/lib/scoped-kubeconfig.sh`) lists framework CRD apiGroups; missing entries cause silent `forbidden` failures during ArgoCD sync. Add the new apiGroup explicitly (F-22 added `argoproj.io`). |
| Argo Rollouts 1.7.x defaults Traefik traffic-router queries to legacy `traefik.containo.us` | LoopZero runs Traefik v3 on `traefik.io/v1alpha1`. Set `controller.extraArgs: [--traefik-api-group=traefik.io, --traefik-api-version=v1alpha1]` in `infra/helm/argo-rollouts/values.yaml` (F-16). |
| `Rollout` annotation `argocd.argoproj.io/sync-wave: "-1"` on first deploy | Wave -1 makes ArgoCD apply Rollout BEFORE the stable+canary Services. Rollout immediately Degraded (`InvalidSpec — stableService not found`) and ArgoCD halts. Use default wave 0 for ALL canary resources (F-15). |
| Test fixture `loopzero.yaml project: <name>` must match journey's `project` constant | Fixture gets named via loopzero.yaml; deployed Rollout = `<project>-<app>`. If they diverge, `kubectl get rollout <project>-<app>` returns NotFound and the assertion fails. Create dedicated fixture or align constant (F-17). |
| Renderer omits `Rollout.spec.progressDeadlineSeconds` | Argo Rollouts default 600s exceeds the 8min E2E budget for readiness-driven aborts. Renderer sets `progressDeadlineSeconds: 300` for ALL Rollouts so canary failures (e.g., bad `/health` probe) transition to Degraded within budget (F-18). |
| `if app.Build == "dockerfile"` in workflow_builder orchestration layer | Wrong layer — strategy decisions belong in `LanguageStrategy.DockerfileStep`, not in the orchestrator. Use `ResolveStrategyForApp(AppSpec)` + `g.Strategy.DockerfileStep(...)`. Todas as estrategias emitem `generate-dockerfile` desde a remocao do caminho BYO em T2.2 (B-068). Build mode dispatch plan T1.6. |
| DAG hardcodes `"generate-dockerfile"` as build-image dependency | **Row corrected 2026-08-18 (B-068).** It used to say the DAG must ask the strategy, because `UserDockerfileStrategy` emitted `dockerfile-lint`. That strategy and the bring-your-own-Dockerfile path were **removed in T2.2** — `user_dockerfile_strategy_test.go` asserts the step's absence — so the name no longer varies. Measured 2026-08-18: node, python and the unsupported fallback all emit `generate-dockerfile`. The DAG now uses the `dockerfileStepTemplate` constant (`api/internal/buildplane/argo/languages.go`), and `TestTodaEstrategiaEmiteOMesmoNomeDePassoDeDockerfile` fails if a strategy ever diverges again — telling you to restore the variation point rather than hardcode a second literal. |
| loopzero-packs v0.3.x stale image rejects user Dockerfile | Pin `imageLoopZeroPacks = ":v0.4.0"` (canonical lenient contract). v0.4.0 copies user Dockerfile verbatim to `--output`, exits 0. Cleanroom heal H16 ensures registry has v0.4.0 via `infra/scripts/build/build-and-push-loopzero-packs.sh`. |
| New tenant CRD apiGroup added without RBAC update | `loopzero-ctrl-runtime-rbac-admin` ClusterRole's framework-CRD apiGroups list MUST include the new group. F-22 added `argoproj.io`; build-mode-dispatch added pattern (no new apiGroup, but Strategy refactor reminded us). Defense-in-depth + governance-gate registers scope. |
| Dockerfile linter rejection in user-facing message uses internal terms | Use product language: "Dockerfile validation" not "BuildKit lint", "LoopZero's allow-list" not "ClusterImagePolicy registries". Hard rejection rule names (FROM_REGISTRY_NOT_ALLOWED etc.) are stable contracts but messages must be plain English. |
| Embedding CA snapshot in ClusterCredential reconcile (instead of live fetch) | Use `caFetcher.FetchClusterCA(ctx, restConfig)` (`operators/internal/platform/ca_fetcher.go`) to read `default/kube-root-ca.crt` on every reconcile. Snapshot becomes stale on CA rotation (k3s restart, prod incidents). When fetching, MUST clone `restConfig` with `TLSClientConfig{Insecure: true}` — otherwise stale snapshot blocks the very call that would refresh it. See plan 2026-05-25-live-ca-fetch-clustercredential. |
| `Authorization: "Bearer ${RESEND_API_KEY}"` in Kratos config YAML | Wrong — Ory Kratos v26 does NOT env-interpolate config strings. Header is sent LITERAL byte-for-byte to Resend → HTTP 400 "API key is invalid". Substitute at helmfile-template time instead: `Authorization: "Bearer {{ env "LOOPZERO_RESEND_API_KEY" \| default "REPLACE_VIA_LOOPZERO_RESEND_API_KEY" }}"`. Operator MUST export `LOOPZERO_RESEND_API_KEY` before `helmfile sync`. ConfigMap holds the real key (loopzero-auth ns, RBAC-restricted — same blast radius as a Secret). See `infra/helm/ory-kratos/values.yaml.gotmpl` + commit `662f3469`. |
| Patching `ory-kratos-config` ConfigMap and expecting courier to pick it up | Kubelet refreshes the mounted volume in ~30s, but Kratos reads config ONLY at process startup (no SIGHUP). After any ConfigMap change MUST `kubectl rollout restart statefulset ory-kratos-courier` (NOT deployment — that is the API server; courier worker is the StatefulSet). Container name trap: Deployment container is `kratos`, StatefulSet container is `kratos-courier` — `kubectl exec` defaults to first container, silently wrong for the courier. |
| `time.Hour.Truncate` bucket on Stripe `Idempotency-Key` | Stripe caches Idempotency-Key responses for 24h. A 1h bucket means every `POST /v1/billing/checkout` within the same wall-clock hour returns the SAME `cs_test_*` URL — including after the underlying session was paid or canceled. User sees "Tudo pronto / esta sessão expirou" forever. Use `time.Minute` (60s absorbs double-click latency; legitimate retries unblocked). Multiple Checkout sessions never produce duplicate charges (Stripe only charges on payment), so coarser windows offer no protection. See `cloud/internal/billing/stripe.go` + commit `7f1c630f`. |
| `WHERE stripe_subscription_id = $X` in Stripe webhook handler | Stripe does NOT guarantee webhook delivery order — `customer.subscription.created` can arrive BEFORE `checkout.session.completed` has linked the subscription_id onto the row. The .created handler then UPDATE-zero-rows and plan stays at starter. Use `WHERE stripe_subscription_id = $X OR stripe_customer_id = $Y` + always SET stripe_subscription_id. Idempotent + commutative across arrival orders. Same lesson generalizes: webhook handlers must be **idempotent AND order-independent**. See `cloud/internal/billing/webhook.go` + commit `95c27732`. |
| Enumeration-by-negation in UI: `planTier !== 'enterprise' && planTier !== 'team'` | Silently covers `pro` (intermediate tier) and shows "Upgrade to Pro" to a Pro user. Always enumerate positively: `planTier === 'free' { ... }`, `planTier === 'pro' { ... }`, etc. The compiler/reader cannot forget a case under positive enumeration. Same trap to watch for in `Record<string, X>` lookups — `dict[k] ?? default` masks a key miss. Normalize case on lookup (`(plan ?? 'starter').toLowerCase()`). See commits `bfd1cc8d` + `b72076e1`. |
| e2e suite "SUCCESS! - Suite skipped in BeforeSuite" (0 of 132 specs ran) against dev-public | `apiURL()` defaults to `http://localhost:3001` (Tilt port-forward fallback). In dev-public (4-cluster k3d + helmfile L2), loopzero-api is exposed via NodePort 30001 → host port 13001 (`docker inspect k3d-loopzero-core-serverlb`). Set `LOOPZERO_API_URL=http://localhost:13001` for the test invocation. The BeforeSuite `verifyAPIHealth()` probes `<apiURL>/api/v1/health`; if it fails (default 5s timeout) all 132 specs SKIP without any visible FAIL — looks GREEN but ran nothing. See `infra/tests/e2e/helpers_test.go:37` + `infra/tests/e2e/deploy_suite_test.go:48`. Same trap for `LOOPZERO_API_URL` not exported: localhost:3001 unreachable → suite silently zero-runs. |
| e2e seed `failed SASL auth: password authentication failed for user "loopzero"` | `seed.go:80-87` default DB URL `postgres://loopzero:loopzero@localhost:5432/loopzero` is legacy from in-process PG that no longer exists. Set `LOOPZERO_E2E_DATABASE_URL=postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable` AFTER running `kubectl --context k3d-loopzero-core -n loopzero-system port-forward svc/postgresql 15432:5432` in background. Password is the deterministic standalone override `loopzero-dev-password` (from `infra/helm/postgresql/values.yaml`), NOT the random Secret. Documented mechanism per `infra/scripts/seed/seed.go:76-77`. |
| `parity/preflight.sh` exits 1 silently after "PASS droplet-install:--dry-run" with no FAIL line | Gate 5 (`doks-install.sh --dry-run`) pre-flight requires `infra/pulumi/node_modules` to exist (script line 61: `[[ -d "$PULUMI_DIR/node_modules" ]] \|\| fail "$PULUMI_DIR — run 'npm ci' first" 1`). Without it, the bash `set -e` aborts BEFORE `result $?` prints the FAIL marker. Stdout ends mid-stream + exit 1 + no error message. Fix: `cd infra/pulumi && npm ci`. Setup pre-req for any e2e run that includes the parity suite, NOT documented in the e2e onboarding. |
| `go test -timeout 120m` alone does NOT extend Ginkgo suite-timeout | Ginkgo v2 has its own `--timeout` default of 60min, independent of `go test -timeout`. Suite at 60min emits `[TIMEDOUT] A suite timeout occurred` even though `go test` would allow 120min. Symptom: dogfood --full with deploy-heavy specs hits TIMEDOUT at 60min wall-clock with N of 132 specs unran. Fix: pass `-args -ginkgo.timeout=2h` after the test pattern (e.g., `go test ./e2e/... -v -timeout 120m -args -ginkgo.timeout=2h`). The `-args` separator is required by Go test to forward flags to the test binary. |
| Test assertion `Or(Equal(0), Equal(1), Equal(4))` for exit-code contracts that include exit 5 | CLI doc-comments are the source-of-truth contract. `cli/src/commands/platform.ts:594-596` documents `0 = Ready no drift; 5 = Ready but runtime diverged (drift detected; ADR D3)`. When the test pre-dates the contract expansion, the test asserts an outdated contract. Fix is NOT to suppress exit 5 — it's to align the test with the CLI doc-comment. Check exit-code doc-comments BEFORE writing the matcher. JSON-shape checks should also branch on the contract (e.g., `if exitCode == 0 \|\| exitCode == 5 { validate JSON }`). |
| Orphan ArgoCD `Application` — **the pattern as written was outdated; this is the corrected form** | **Who owns the `Application`:** the **operator** owns `.spec`; the `api` writes the *input* (`Tenant.Spec.Environments`), triggers *operations* (`/sync`, `/operation`), and deletes only on the administrative-eviction path. ADR `docs/wiki/decisions/2026-08-12-argocd-application-ownership.md` (B-050). **What is already covered:** an env removed from `tenant.Spec.Environments` is reaped — `pruneOrphanPreviewApps` (`operators/internal/tenant/tenant_gitops.go`) deletes any Application labelled with the tenant outside `expectedApplicationNames`, since `ecbabf34` (2026-06-12). The paragraph this row replaces landed in `c6fc9671` (2026-06-10), **two days earlier**, and was carried forward unrevised — both of its file:line citations had stopped resolving. **The gap that remained, and is now closed (B-074, 2026-08-19):** `EnvironmentReconciler.reconcileDelete` deleted the Namespace and removed the finalizer while touching neither the Application nor `Tenant.Spec.Environments` — the entry stayed in spec, so the App stayed *expected* and the reaper never reached it. It now calls `pruneTenantEnvironmentEntry` (`operators/internal/environment/environment_controller.go`) **before** releasing the finalizer, removing the `(Project, Environment)` tuple so `pruneOrphanPreviewApps` reaps the App. It removes the tuple rather than deleting the App itself because the tenant operator owns that `.spec` (ADR above) — one owner, one mechanism. If the Tenant is already gone the prune is a no-op, so deletion never wedges. |
| Concurrent build submissions cause `clone-repo: Max duration limit exceeded` + workflow.phase=Succeeded mismatch | Under concurrent build load (idempotency tests, ~4+ builds within ~30s of each other on cold k3d), Argo Workflow `clone-repo` step exhausts its retry budget (`maxDuration: 900s` with exponential backoff 10s/20s/40s) because each clone attempt is slow on contended I/O. Result: `clone-repo=Failed` + `sign-image-app=Omitted` BUT `workflow.status.phase=Succeeded` because the workflow `onExit=Succeeded` handler overrides actual step state. loopzero-api `reconcile_complete.go:90` sees Succeeded → proceeds to manifest stage → `manifest_builder` rejects with `missing image digest for app "X"` because cosign never ran. Build status flips FAILED with reason `Failed to build manifests with custom domains`. Pattern: workflow phase is NOT step-truth — check `wf.status.nodes.*` for actual step phases. Reference: `api/internal/buildplane/argo/buildkit.go:153` "L3 v8 observed 4 workflows Max duration limit exceeded at c=30". |
| Parity test SKIP that references a shell script with `--dry-run` mode | Don't blanket-skip. Port via `exec.Command("bash", scriptPath, "--dry-run")` + assert exit 0 + `[dry-run]` marker in output. The shell's `--dry-run` is canonical first-class affordance, NOT a workaround. Validates preflight + flag parsing + reachable code paths without mutating state. LIVE mutating runs stay operator-only per MEET-ASYNC-AMENDMENT-2026-05-24-001. Pattern: read the script's `--dry-run` semantics first (some require stub env vars like `cell2-rollback-slo.sh` which needs `SYNTH_TENANT_ID=tn-stub`). See `infra/tests/e2e/parity_install_test.go`, `parity_cell2_canary_test.go`, `parity_cell2_test.go:C4` for canonical port examples (commit `a85eb8c9`). |
