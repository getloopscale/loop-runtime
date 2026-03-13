# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> This file is the contract between Claude and the LoopZero codebase. Read it entirely before touching any file.

---

## What This Project Is

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments. The goal is Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative specification is **`docs/technical.md`**. Every state machine, failure mode, concurrency decision, CLI output format, and exit code is documented there. If this file and the system design conflict, the system design wins.

Sprint details are in **`docs/sprints/`** — one folder per sprint with tasks, exit gates, and spec references.

**Current status:** Greenfield — no application code exists yet. Only `docs/` and configuration files. Implementation starts with Sprint 0.

---

## Development Commands

```bash
# Local environment
task dev:up              # Boot k3d clusters + dependencies
tilt up                  # Start dev loop with hot reload

# Tests
task test                # Run all tests
task test:unit           # Run unit tests only
task test:e2e            # Run E2E tests (Ginkgo)
task lint                # Lint all components
task validate:manifests  # kubeconform --strict on all manifests

# Per-component (when code exists)
cd cli  && npm test      # CLI unit tests (Jest)
cd api  && npm test      # API component tests (Jest + Supertest)
cd operators && go test ./...  # Operator tests (Ginkgo + envtest)
```

**Prerequisites:** Go 1.22+, Node.js 20+, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev), [Task](https://taskfile.dev)

---

## The Rules (Non-Negotiable)

### Rule 1: Tests before code
No production code without a failing test. Write the test. Run it. Confirm it fails. Then implement. This is not a guideline — it is the development process.

### Rule 2: Read the spec first
Before implementing anything, find the relevant section in `docs/technical.md`. The spec is the design. Do not invent behavior the spec doesn't describe.

### Rule 3: No internal terms in user-facing output
CLI output, error messages, and `--json` responses must never contain:
`ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`

Use product language: deploy, team, account, service, revert, build.

### Rule 4: Exit codes are permanent
`0` success · `1` usage error · `2` build failure · `3` deploy failure · `4` auth error · `5` partial failure. These never change. No other codes.

### Rule 5: `--json` output is a stability contract
Field names cannot change without a major version bump. New fields can be added. See spec §0.3 for required schemas.

### Rule 6: Every mutation uses the environment lock
Any operation that mutates `(tenant, project, environment)` state MUST use `withEnvironmentLock`. Never bypass it. Never perform the concurrent-build check OUTSIDE the lock (race condition).

### Rule 7: `pg_advisory_xact_lock` only
Never use `pg_advisory_lock` (session-scoped). It breaks with connection pools. The transaction-scoped variant releases automatically on commit/rollback/disconnect. See spec §4.2.

### Rule 8: State machines are strict
Transitions go through the state machine functions. Direct status updates are forbidden. Invalid transitions panic — they are programming errors, not runtime errors.

### Rule 9: CLI is the primary interface
Every feature lives in the CLI before the dashboard. A feature that requires the dashboard but has no CLI equivalent is a product bug.

### Rule 10: Local equals production
Same Helm charts, same operators, same build pipeline. No Docker Compose shortcuts. No "development mode" with different behavior.

---

## Stack

| Component | Technology |
|-----------|-----------|
| CLI | TypeScript + Commander · `@loopzero/cli` |
| API | TypeScript + Fastify + Drizzle ORM · `@loopzero/api` |
| Operators | Go + kubebuilder |
| Database | PostgreSQL · advisory locks for serialization |
| Build engine | Argo Workflows + Kaniko |
| GitOps store | Gitea (desired state mirror) |
| GitOps sync | ArgoCD (cluster convergence) |
| Ingress | Traefik + cert-manager |
| Artifact store | MinIO (S3-compatible) |
| Observability | Prometheus + OpenTelemetry + Grafana |
| Local clusters | k3d × 3 (mgmt, build, workload) |
| Dev loop | Tilt |
| Task runner | Taskfile |
| Tests (Go) | Ginkgo + Gomega + envtest |
| Tests (TS) | Jest + Supertest |
| Image signing | cosign + syft |

---

## Repository Structure

```
loopzero/
├── CLAUDE.md                          ← you are here
├── docs/
│   ├── technical.md               ← single source of truth
│   └── sprints/                       ← sprint details (extracted from §23)
│       ├── README.md                  ← sprint index + overview table
│       ├── 00-foundation/
│       ├── 01-login-init-tenant-controller/
│       ├── 02-deploy-happy-path/
│       ├── 03-logs-status-secrets/
│       ├── 04-rollback-promote-cancel/
│       ├── 05-preview-environments/
│       ├── 06-security-supply-chain/
│       ├── 07-observability/
│       ├── 08-hardening-chaos/
│       └── 09-polish-launch/
├── cli/                               ← @loopzero/cli (TypeScript)
│   ├── src/
│   │   ├── commands/                  ← one file per command
│   │   ├── api/                       ← API client + idempotency key generation
│   │   └── output/                    ← formatters: human() + json()
│   └── tests/unit/
├── api/                               ← @loopzero/api (TypeScript + Fastify)
│   ├── src/
│   │   ├── routes/                    ← Fastify route handlers
│   │   ├── services/                  ← business logic
│   │   ├── db/
│   │   │   ├── schema/                ← Drizzle schema definitions
│   │   │   └── migrations/            ← forward-only migrations
│   │   ├── lock/                      ← withEnvironmentLock
│   │   ├── sse/                       ← SSE streaming + event journal
│   │   ├── middleware/                ← idempotency, auth, correlation-id
│   │   └── errors/                    ← LoopZeroError + typed error codes
│   └── tests/component/
├── operators/                         ← Go operators (kubebuilder)
│   ├── api/v1/                        ← CRD type definitions
│   ├── internal/
│   │   ├── controller/                ← reconcile loops + tests
│   │   └── statemachine/              ← state machines (spec §5)
│   └── config/crd/bases/
├── infra/
│   ├── k3d/                           ← cluster configs (mgmt, build, workload)
│   ├── helm/                          ← Helm charts for all components
│   ├── tilt/                          ← Tiltfile
│   └── tests/
│       ├── e2e/                       ← Ginkgo E2E tests
│       └── load/                      ← load tests
└── Taskfile.yml
```

---

## Agents

Claude operates as a team of specialists. When a task spans multiple components, sequence the work across agents.

### Orchestrator
Entry point for multi-component tasks. Reads the spec, breaks work into subtasks, delegates, validates.

**Activates when:** The task touches more than one component (CLI + API, API + operator, etc.)

**Process:**
1. Identify which components are touched
2. Find the authoritative spec section
3. Sequence subtasks: test spec → implementation → integration
4. Validate: tests pass, no internal terms, output matches spec

### CLI Engineer (`agent:cli`)
Owns `cli/`. TypeScript + Commander. DX quality bar is the Vercel CLI.

**Before any command is done:**
- `--json` flag with schema matching spec §0.3
- Correct exit code
- CI mode works (`CI=true`)
- No internal terms in any string
- Error messages suggest next action
- Unit tests cover success + all failure codes + JSON shape

### API Engineer (`agent:api`)
Owns `api/`. TypeScript + Fastify + Drizzle.

**Before any endpoint is done:**
- `withEnvironmentLock` for state mutations
- `Idempotency-Key` middleware active
- Typed errors (no `"Something went wrong"`)
- `Correlation-ID` forwarded
- Component test: success + 409 conflict + idempotency replay + concurrent rejection

### Operator Engineer (`agent:operator`)
Owns `operators/`. Go + kubebuilder.

**Before any controller is done:**
- Ginkgo spec written before implementation
- Runs against `envtest` (no real cluster)
- Reconcile is idempotent
- Crash mid-reconcile → resume correctly
- State transitions via state machine only
- Leader election enabled

### Infrastructure Engineer (`agent:infra`)
Owns `infra/`. k3d + Helm + Tilt + Taskfile.

**Standards:**
- All Taskfile tasks are idempotent
- All manifests pass `kubeconform --strict`
- All containers have resource limits, readiness/liveness probes, non-root security context
- No `latest` image tags

### Test Engineer (`agent:test`)
Owns test quality across all layers. Writes specs before implementation.

**Critical tests that must exist:**
- Concurrent deploys → exactly one 202, one 409
- Same idempotency key → same response
- Different payload same key → 422
- Lock released immediately on API crash
- Invalid state transition → panic
- CLI exit codes for all error paths
- No internal terms in any CLI output
- `--json` shape matches spec schema

---

## Slash Commands

Use these in Claude Code to trigger structured workflows:

| Command | Effect |
|---------|--------|
| `/sprint` | Show current sprint status, completed tasks, next task, exit gate |
| `/implement <feature>` | TDD workflow: spec → fail → implement → pass → validate |
| `/test <component>` | Write or run tests for a component |
| `/review <path>` | Compliance review: lock, idempotency, terms, exit codes |
| `/design-check <description>` | Validate approach against system design before coding |
| `/failure-check <scenario>` | Verify a failure mode from spec §12 has a passing test |
| `/schema-check <command>` | Validate CLI `--json` output against spec §0.3 schema |
| `/state-machine <name>` | Inspect implementation vs spec §5 |

---

## Hooks

### Before writing a production file
Ask: does a failing test for this already exist? If not, write the test first.

### Before any bash command
Block: `kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`, `git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

### After writing any CLI file
Grep for internal terms in string literals. Report violations before proceeding.

### After writing any operator file
Remind: run the test, confirm it fails (if new), then implement.

### Before marking a task complete
Run the pre-completion checklist:
```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] No internal terms in CLI output
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] CLI --json matches spec §0.3 schema
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes match spec §5
```

---

## Sprint Plan

> Full details: `docs/sprints/README.md` and each `docs/sprints/XX-name/sprint.md`

| Sprint | Weeks | Focus | Exit gate | CLI Commands |
|--------|-------|-------|-----------|--------------|
| **[0](docs/sprints/00-foundation/sprint.md)** | 1-2 | Foundation | `task setup` boots, Grafana running | — |
| **[1](docs/sprints/01-login-init-tenant-controller/sprint.md)** | 3-5 | Login + Init + TenantController | `loopzero login` → `loopzero init` → `loopzero.yaml` | `login`, `logout`, `whoami`, `init` |
| **[2](docs/sprints/02-deploy-happy-path/sprint.md)** | 6-9 | Deploy happy path | CLI → live URL | `deploy` |
| **[3](docs/sprints/03-logs-status-secrets/sprint.md)** | 10-12 | Logs + Status + Secrets | All daily workflows functional | `logs`, `status`, `inspect`, `secret` |
| **[4](docs/sprints/04-rollback-promote-cancel/sprint.md)** | 13-15 | Rollback + Promote + Cancel | Recovery in < 30s | `rollback`, `promote`, `build`, `env`, `team`, `project` |
| **[5](docs/sprints/05-preview-environments/sprint.md)** | 16-18 | Preview environments | PR → preview URL → cleanup | `preview` |
| **[6](docs/sprints/06-security-supply-chain/sprint.md)** | 19-21 | Security + Supply chain | Signed images, encrypted secrets, RBAC | — |
| **[7](docs/sprints/07-observability/sprint.md)** | 22-23 | Observability complete | Incident resolved via dashboards in < 15 min | — |
| **[8](docs/sprints/08-hardening-chaos/sprint.md)** | 24-25 | Hardening + Chaos | 50 concurrent deploys, all chaos passes | — |
| **[9](docs/sprints/09-polish-launch/sprint.md)** | 26-27 | Polish + Launch | Non-team-member onboards in < 10 min | — |

Check the current sprint before starting any implementation. Only implement what belongs to the current sprint.

---

## Common Mistakes — Read Before Coding

| Mistake | Consequence | Fix |
|---------|------------|-----|
| `pg_advisory_lock` instead of `pg_advisory_xact_lock` | Lock persists on connection pool reuse → deadlocks | Always use `xact` variant |
| Concurrent build check OUTSIDE advisory lock | Race condition between check and insert | Move check inside `withEnvironmentLock` |
| `errors.New("something failed")` | Untraceable, no error code | Use `LoopZeroError` with typed code |
| Direct state update (`release.Status.State = "active"`) | Bypasses validation, invalid transitions silently succeed | Use `release.Transition(event)` |
| `kubectl` in user-facing workflow | Breaks the "no Kubernetes" promise | Proxy through API, never expose kubectl |
| Marking release ACTIVE before ArgoCD confirms sync | False positive — app may not be running | Wait for ArgoCD sync confirmation |
| Writing prod code before failing test | Unverifiable correctness | Test first, always |
| Internal terms in CLI output | Developer experience failure | Use product language, see Rule 3 |
| Forward-only migration violations (`DROP`) | Irreversible data loss | Explicit sign-off required |

---

## Key References in System Design

| Topic | Section |
|-------|---------|
| Core CLI workflows (terminal output) | §0.1 |
| CLI stability contract | §0.2 |
| Exit codes + JSON schemas + CI mode | §0.3 |
| North Star + target persona | §1 |
| Design philosophy (8 principles) | §2 |
| Source of truth hierarchy | §3 |
| Serialization + idempotency | §4 |
| State machines (all 5) | §5 |
| Architecture (4 planes) | §6 |
| System components | §7 |
| Data model | §9 |
| Core flows (deploy, promote, rollback, preview) | §10 |
| Failure modes + recovery | §12 |
| Multi-tenancy + isolation | §14 |
| Observability (metrics, dashboards, alerts) | §15 |
| Security model | §16 |
| TDD/BDD strategy | §18 |
| Local infrastructure | §19 |
| Test contracts by layer | §20 |
| CLI reference (all commands) | §25 |
| `loopzero.yaml` complete spec | §26 |