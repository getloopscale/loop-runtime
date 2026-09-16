# Decision: dispose of `api/internal/health` — B-022

| | |
|---|---|
| **Item** | B-022 |
| **Decided** | 2026-09-12 |
| **Decided by** | the alignment reviewer, at `.squad/records/alignment/B-022-alignment.md` CHK004 |
| **Disposition** | **A — delete** |
| **Implemented** | 2026-09-14, from `6c22c4ecf` |

## What was deleted

540 lines across four files:

| File | LoC |
|---|---|
| `api/internal/health/prober.go` | 100 |
| `api/internal/health/prober_test.go` | 126 |
| `api/internal/health/prober_coverage_test.go` | 179 |
| `api/tests/unit/health_prober_test.go` | 135 |

The fourth was the package's sole importer in the whole repository, and its removal
is forced rather than elective: `api/tests/unit` is a single Go package, so an import
of a package that no longer exists stops the whole package compiling. That was
observed failing before the second deletion, not assumed.

The row naming the port was also removed from the doc comment in
`api/internal/adapters/adapters.go`, and a guard test was added so the block cannot
again name a port that is declared nowhere — `api/internal/adapters/doc_contract_test.go`.

## Why the caller went in March

Commit `ad6de675f`, *"Refactor tests and remove unused health prober"* (2026-03-19),
removed the `HealthProber` interface, its construction in `api/cmd/main.go` and its
invocation in the release-activation path, in one commit. Its message records only a
list of refactorings; **the rationale is in the diff**, as a comment added to the
release activator:

```
// A-03 fix: Health validation is delegated entirely to ArgoCD instead of direct HTTP probes.
// ArgoCD's WaitForSync already verifies both sync status (manifests applied) AND health status
// (Deployments have ready pods, Services have endpoints, etc.). Direct HTTP probing from the
// control plane to workload cluster apps was:
//   - Redundant with ArgoCD's health assessment
//   - A cross-cluster network dependency (control plane → workload plane)
//   - Fragile: probes could hit old pods during rollout, apps without /health, etc.
```

Read it with `git show ad6de675f`. That decision is settled and was not reopened here.

The package then sat unreachable for six months. The doc comment went on requiring it
the whole time, and survived two later edits to the same block still carrying the row —
which is why the replacement is a test rather than more care.

## Why it was not kept unwired (disposition B)

B and A are indistinguishable in everything a developer observes: zero rows in
`go list -deps ./cmd/...` either way, zero behaviour change either way. B's only value
is a head start on C, and C is blocked on a product decision with no owner —
`.squad/wiki/product/` holds zero files, so there is no objective to trace to.

Against that, B keeps 405 lines of green, fully-tested, unreachable code. Green and
unreachable is precisely the shape that survives review, because it looks maintained.
The head start is also smaller than it looks: `HTTPProber.Probe` takes an app URL and
a path and returns `error`, while the aggregator's port takes a deploy ID and must
report status code and latency. The reusable part is the HTTP GET, not the adapter.

## Why it was not reused for the aggregator's probe port (disposition C)

Excluded on the state machine, not on taste. The probe adapter emits a failed status
for the `live` phase once its attempt budget is spent, and the deploy timeline gives
that status no way back:

| Measurement | Consequence |
|---|---|
| `pkg/statemachine/deploy_timeline.go:137-138` — `case DTStatusFailed, DTStatusCanceled: s.terminal = true` | `failed` is terminal in **every** phase, not only the last |
| `pkg/statemachine/deploy_timeline.go:159-160` — `case DTStatusDegraded: return phase == DTPhaseDeploy` | `degraded` is reachable only from the deploy phase, so the live phase has no degraded exit |
| `pkg/statemachine/deploy_timeline.go:155-158` — from `DTStatusInProgress`, `to` in `{completed, failed, canceled}` | three exits from in_progress, two of them terminal |
| `validStatusTransition` has no `case DTStatusFailed:` | no recovery edge exists from failed, by construction |

So a probe wired as a **report** has no non-terminal vocabulary for *"the URL has not
answered yet"* beyond looping until its context is cancelled, which hangs the phase
instead of reporting it. An emitted `live/failed` renders a failed deploy to the
developer while the release is ACTIVE and ArgoCD is Healthy.

Nor would it be a second opinion beside ArgoCD:

| Measurement | Consequence |
|---|---|
| `api/internal/aggregator/aggregator.go:55` — `state *statemachine.DeployTimelineState` | one FSM, shared by every source reader |
| `api/internal/aggregator/aggregator.go:152-154` — `if a.state.IsTerminal() { return }` | `Run` returns on the first terminal status observed |

One shared FSM plus first-terminal-wins means a second authority races the first with
no arbitration: the developer-visible outcome would be decided by whether the probe
ticker or the release-row translation reached the FSM first. Making C safe requires
changing a state machine, which the alignment brief places out of scope.

**C is excluded because it cannot be done inside this item — not because outbound
probing is undesirable.**

## Where the code is recoverable

```bash
git show 6c22c4ecf:api/internal/health/prober.go     # the implementation
git show ad6de675f~1:api/cmd/main.go                 # the deleted wiring
```

Git holds it; the tree does not have to. That recovery path was exercised three times
while this item was being measured.

## What this decision does NOT say

- It does **not** settle whether outbound HTTP probing should exist. That question is
  open and belongs to a separate item.
- It does **not** extend the March decision beyond what that commit recorded. March
  withdrew probing as an authoritative gate on release activation; it did not rule on
  probing as observability, and reading it that way would make this record the next
  source of an overstatement this item corrected three times.
- It does **not** touch the comments at `api/cmd/deploy_timeline_spawn.go:124` and
  `:127`, which still claim the live URL is probed. They are false today and they are
  registered separately (B-038, B-039, B-040).
