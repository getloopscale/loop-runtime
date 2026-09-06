# Package admission: the criterion has to be able to say no

**Item:** B-058 — *Três pacotes admitem membro pela tecnologia ou pelo padrão, não pela
responsabilidade.* Audit of 2026-08-11, re-measured 2026-09-04.
**Gate:** `loopzero-ops quality package-admission` (`cmd/loopzero-ops/internal/quality/packageadmission`).
**Sibling gate:** `loopzero-ops quality adapter-ports`.

## The defect this page exists for

A package has an admission criterion whether or not anybody wrote it down. When nobody
writes it down, the criterion becomes whatever the package name suggests — and three of
ours suggested something that is not a responsibility:

| Package | Admitted by | Why it cannot refuse |
|---|---|---|
| `api/internal/k8s` | **technology** — "it uses client-go" | every domain that speaks to the API server qualifies |
| `api/internal/adapters` | **pattern** — "it is an adapter" | anything bridging two types qualifies |
| `api/internal/services/platform` | **nothing stated** | see the open question below |

None of the three is a bug. That is the point, and it is why this sat at severity
*média* with "nenhum defeito de comportamento". The cost is not a broken behaviour; it
is that the next file lands wherever it fits, and nobody has to decide.

## The rule

> **A package's admission criterion must be a sentence that refuses something.**

A criterion that admits every candidate is not a criterion, it is a description. The
test of a proposed criterion is not "does it describe what is in here" but "name a file
this sentence turns away". If nothing can be named, the sentence is decoration.

## `api/internal/k8s` — plumbing names no domain

**Measured before (parent commit `d61f22699`):** 7 non-test files, **1,520 non-test
lines** (2,945 including tests), holding three unrelated responsibilities that shared a
client library and nothing else:

- the Tenant CR and tenant eviction — `tenant_cr_manager.go`, `tenant_evictor.go`
- the LoopZeroApplication and PreviewEnvironment CRs — `application_cr_manager.go`, `preview_cr_manager.go`
- secret sync — `secret_k8s_manager.go`

**The criterion:** *plumbing names no domain.* A top-level file of `api/internal/k8s`
may not carry a responsibility in its file name, nor export an identifier that carries
one. Whoever cannot state their code in terms of scheme, config, client and namespace
belongs in one of the homes.

This is the sentence the old one could not say. "It uses client-go" admits
`tenant_cr_manager.go`, `application_cr_manager.go` and `secret_k8s_manager.go` alike.
"Plumbing names no domain" refuses all three, and refuses the next one too.

**Measured after:** 3 non-test files, **111 non-test lines** — `doc.go` (the criterion),
`namespace.go` (`ControlPlaneNamespace`, the address both CR homes write to), and
`typed_client.go` (`NewScheme`, `RestConfig`, `NewTypedClient`).

The homes, with the exported types that make each one populated:

| Home | Exported types |
|---|---|
| `k8s/tenancy` | `TenantCRManager`, `TenantK8sEvictor`, `K8sEvictionCounts` |
| `k8s/workload` | `LoopZeroApplicationInput`, `ApplicationCRManager`, `PreviewCRSpec`, `PreviewCRManager` |
| `k8s/secretsync` | `SecretK8sManager` |

### A note on `env_resolve.go`

The DoD names the surviving plumbing as "cliente tipado, resolução de env", and
`env_resolve.go` moved to `k8s/tenancy` rather than staying. That is deliberate, and it
is not a reinterpretation of the bullet: **`RestConfig` in `typed_client.go` is the env
resolution the bullet means** — in-cluster first, then kubeconfig. `env_resolve.go`
resolves an entry of `apiv1.TenantEnvironmentEntry` on the Tenant CR, which is tenancy
domain logic wearing a plumbing-sounding file name. It is also unexported
(`resolveEnvEntry`) and called only from `tenant_cr_manager.go:265`, so it could not have
stayed behind without being exported for no reason.

### What the gate checks, and what it does not

It fails in three distinguishable ways, because the fixes differ:

1. **`ErrForeignDomain`** — a top-level file names a responsibility (in its file name or
   in an exported identifier). Fix: move it to a home.
2. **`ErrHomeMissing`** — a declared home is gone or holds no exported type. Refusing
   three responsibilities *into nowhere* would leave this gate green over a dissolved
   package, which is how a guard outlives its own object. A home that empties out fails.
3. **`ErrNothingScanned`** — the governed package is absent. Finding nothing is not the
   same as finding nothing wrong.

**The limit, stated so nobody reads more into a PASS than it carries:** the gate checks
that plumbing carries no domain vocabulary, not that each home holds the *right* domain.
A file moved from `tenancy` to `workload` compiles and passes here. The import graph and
the reviewer cover that half.

Unexported domain helpers are deliberately allowed: they cannot be read from outside, so
they do not make the package *admit* a responsibility, and refusing them would make the
gate noisy without making the criterion sharper.

**Teeth, measured 2026-09-04 against the real tree, not a fixture:** restoring
`tenant_evictor.go` into `api/internal/k8s` makes the gate exit 1 with three findings —
the file name, `TenantK8sEvictor` and `NewTenantK8sEvictor`. Removing it again returns
PASS with 3 files scanned.

## `api/internal/adapters` — declare the port, do not describe it

Closed 2026-08-18 by the second alternative of its DoD bullet: the package kept its name
and gained a criterion that can refuse. Every adapter states, **at compile time**, which
port it satisfies — `var _ Port = (*Type)(nil)`.

Of the nine adapters, five named their port in prose, four named nothing, and one
asserted it to the compiler. Prose does not compile: a comment saying *"implements
routes.X"* survives the interface being renamed, reshaped or moved, and the next reader
believes it.

`adapter-ports` enforces the declaration, and the distinction that makes it useful is
that `var _ = pkg.Type{}` is an assertion of **use**, not of **port** — conflating the
two would pass exactly the defect the gate exists for.

**Re-measured 2026-09-04:** `adapter-ports: PASS — 9 adaptador(es), todos declarando a
porta que satisfazem`.

The one member that could not be given a port was deleted instead:
`vault_transit_adapter.go`, whose own doc said *"Signatures match byte-for-byte; only the
method names differ"* — a bridge from internal code to internal code. It went in
`2f8910db3` on 2026-08-16. Its citation in `secrets.go` outlived it by 19 days, because
nothing checks a comment; the claim is now
`var _ secrets.SecretTransit = (vault.Transit)(nil)` in `api/cmd/setup_transit.go`.

## `api/internal/services/platform` — OPEN, and the DoD's own answer is refuted

**This is the one bullet of B-058 that is not closed, and it is not closed because
measuring it contradicted the item.**

The DoD offers two alternatives: *gain a name that says the real property (roda no
controller)*, or *be divided by concern*. The parenthetical is the item's own answer for
what the real property is. **It is false.**

Measured 2026-09-04 by reading every construction site of a `platform.*` symbol:

| Wiring site | Binary | Constructs |
|---|---|---|
| `api/cmd/main.go` | **api** | `EnvManagerAdapter`, `EnvironmentEnsurer`, `EnvironmentCRReader`, `SecretResyncer` |
| `api/cmd/setup_background.go` | **api** | `IdempotencyCleaner`, `AuditLogPruner`, `OrphanDetector`, `KVReconciler` |
| `api/cmd/setup_orchestrator.go` | **api** | `ProjectDeleter` |
| `api/cmd/setup_workflows.go` | **api** | `CircuitBreakerWorkflowSubmitter`, `CircuitBreakerSyncClient` |
| `api/cmd/controller/setup_services.go` | **controller** | `CircuitBreakerWorkflowSubmitter`, `CircuitBreakerSyncClient`, `IdempotencyCleaner` |

The janitor loops are not controller-side either: `main.go:266` →
`startLeaderLoops` (`setup_background.go:424`) → `createCoreLoops` (`:227`), all inside
the **api** binary under leader election.

So the property is the reverse of the one the DoD names. **Every member runs in the api
binary; three of them also run in the controller; not one runs only in the controller.**
Renaming the package to say "runs in the controller" would encode a measured falsehood
in a package name — a worse outcome than the unstated criterion it replaced, because a
name is believed.

**What is true of the package, and is a criterion that can refuse:** nothing inside
`api/internal` imports `services/platform`. Its only importers are the process wiring
(`api/cmd`, `api/cmd/controller`) and tests. A route, a service or a middleware that
needed to import it would be refused by that sentence.

**What this needs:** a decision between (a) amending the bullet to the measured property
and renaming or documenting against *that*, and (b) the split by concern — 11 non-test
files, **1,922 non-test lines**, across at least four groups (background janitors;
circuit breakers; environment provisioning; project deletion) whose boundaries the item
does not name. Choosing the boundaries is the decision, and it is not one this page
should make on its own. `platform` is a registered context in the `services-acyclic`
allow-list, so either route touches that gate too.

One finding to carry into that decision: `pg_circuit_breaker.go` contains
`CircuitBreakerSyncClient` — the ArgoCD sync breaker, not a Postgres one. The Postgres
breaker is `api/internal/middleware/policy/pgcircuitbreaker`. The file names inside
`platform` do not describe their contents either.

## Adding a package to this rule

1. Write the criterion as a sentence that refuses. Name the file it turns away.
2. Name the homes the refused members go to, and require each to stay populated —
   otherwise the gate goes green over a vacuum.
3. Prove the teeth with a mutant against the **real tree**, not only a fixture.
4. Register the gate (`cmd/loopzero-ops/quality_registry.go`), wire it
   (`taskfiles/quality.yml`), and re-freeze `cmd/loopzero-ops/testdata/gateset.frozen`. The
   `TestEveryGateIsWiredOrExempted` guard fails a registered-but-unwired gate on purpose.
