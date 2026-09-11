---
type: requirements
document: trd
scope: loopzero
owner: iris-product-designer
agreed_on: 2026-09-10
review_on: 2026-09-14
derives_from: .squad/wiki/product/objectives.md
session: .squad/records/brainstorms/2026-09-10-session.md
---

# Technical requirements — LoopZero

Each `REQ-N` states **what must be true**, cites the objective that justifies it, and says
how someone checks it holds. Every citation resolves to an `OBJ-N` in `objectives.md`; a
dangling one caps the cascade at `INVALID`, because no edit to this document fixes a
referent that does not exist.

## The line this document does not cross

**What must be true — not how it is made true.** The test is mechanical: *could two
competent teams satisfy this with different technology?* If not, it is a design, and design
belongs to `cycle-plan`, where it gets an edge-case pass, a dependency audit with CVE
checking and a confidence score. Made here, the same decision arrives downstream carrying
the authority of an agreed document and none of those gates.

So no requirement below names a framework, a library, a schema or a file. REQ-3 says *a
certificate the browser accepts*, not how it is issued. REQ-18 says *no dependency edge*,
not the name of the seam file — the property is checkable, the name would only be a choice.

## Every acceptance is ours to run

Consistent with `objectives.md`: no acceptance below needs a person outside this team.
Where the honest instrument was a customer, we are the customer.

---

**OBJ-1 — She reaches a live agent without taking an infrastructure decision**

## REQ-1 — One path, no forks
serves: OBJ-1
statement: The published quickstart presents no choice whose wrong answer blocks her.
acceptance: Execute the whole path in a clean container and record every point where
progress requires an infrastructure DECISION — choosing, creating or configuring a store, a
domain, a registry, a certificate. Count must be 0. Counting words like *choose* was the
earlier criterion and it proves nothing: "set DATABASE_URL to your Postgres" contains none of
them and is a blocking infrastructure decision.

## REQ-2 — Nothing to provision
serves: OBJ-1
statement: Reaching a public URL requires her to create no external resource beyond a model
provider key.
acceptance: In a clean container, list every external account or resource created before the
agent answers. The only permitted one is the model key.

## REQ-3 — The URL arrives ready and trusted
serves: OBJ-1
statement: The deploy returns an address answering over HTTPS with a certificate her browser
accepts, with no further step.
acceptance: Send the agent a prompt with a known expected answer over the returned HTTPS
URL, **without `-k`**, and assert the agent's response. A 200 alone can be a default page or a
health check — it proves TLS and reachability, never that the thing she wrote is answering.

## REQ-4 — A failure names the fix
serves: OBJ-1
statement: Every failure the quickstart path can produce names what she must change.
acceptance: Inject the three most likely failures — invalid model key, name already taken,
code that does not compile — and check that each message names the thing to change.

**Why REQ-4 is here at all:** a 30-minute median dies on one confusing error, not on a slow
step. It is the requirement most easily left out, and the one that most explains the
difference between 30 minutes and giving up.

**Why the model key is the one exception in REQ-2:** *not a model provider* is a declared
non-goal in the vision. A requirement demanding zero keys would contradict it.

---

**OBJ-2 — The four capabilities work without her building them**

## REQ-5 — Memory survives a restart without her modelling it
serves: OBJ-2, OBJ-4
statement: She writes and reads agent memory without declaring a schema, choosing a store or
writing a migration.
acceptance: In the project the quickstart generates, count lines of DDL, migration or
connection string authored by her — 0. Kill the process, start it again, read what was
written before.

## REQ-6 — RAG over her documents without building an index pipeline
serves: OBJ-2
statement: She points at documents and queries them; she writes no chunking, embedding or
index-maintenance code.
acceptance: Index a controlled document carrying a fact present nowhere else in the corpus,
ask a question whose answer is that fact, and assert the answer contains it. Lines of
chunk/embed/index code authored by her — 0. Adding a document and getting a response back does
not show that retrieval happened.

## REQ-7 — Every model call is traced without her instrumenting it
serves: OBJ-2
statement: She can answer *what did the agent do* about a past run without having added
logging.
acceptance: Run an agent from the quickstart with zero instrumentation code, then retrieve
the sequence of model calls, tools and inputs for that run.

## REQ-8 — Prompts and skills are versioned without her building versioning
serves: OBJ-2
statement: She changes a prompt or a skill and can say which version produced a past run.
acceptance: Change **a prompt AND a skill**, run again, and resolve an earlier run to the
**exact text** of both as they were then. The title promised both and the earlier criterion
tested only prompts — an inconsistency introduced when this was written.

## REQ-9 — The four compose without glue
serves: OBJ-2
statement: Using the four capabilities together requires no adapter authored by her.
acceptance: One journey combining **all four** — memory, RAG, execution record and versioned
prompts — completing with no adaptation code between them. Two was the earlier bar and it does
not test the claim the title makes.

**Why REQ-9 exists:** six separate repositories — `loopzero-memory`, `loopzero-rag`, `loopzero-lens`,
`loopzero-skills`, `loopzero-promptly`, `loopzero-trust`. Each working alone while requiring glue between
them fails OBJ-2 without failing REQ-5 through REQ-8. A distinct failure needs a distinct
requirement.

**REQ-5's evidence lives in a repository this one does not build — found 2026-09-11 by the
design drawings.** The state survives a restart because it was never in the pod: it is in
`loopzero-memory`'s Postgres, 34 migrations and 17 tables at head. Measured in `loopzero`, the renderer
emits `Deployment` or `Rollout`, never `StatefulSet`, and places no `PersistentVolumeClaim` on the
user's path.

**So the promise holds only while the `loopzero` ↔ `loopzero-memory` edge holds, and no gate here watches
it.** A migration in `loopzero-memory` that drops a table takes this requirement with it, silently,
while every test in this repository stays green. The acceptance criterion above — kill the process,
read back what was written — would catch it, and only if somebody runs it.

**Why REQ-5 serves two objectives:** if memory only survives in a store we host, she needs an
account and OBJ-4 falls. Writing the second citation is what makes that tension visible now
rather than in phase 4.

**Why REQ-8 asks for the text, not the version number:** a pointer to "v3" only helps if v3
still exists and was never rewritten, and her question is *what did the agent receive*, not
*what label did it carry*.

---

**OBJ-3 — Self-host is an install, not a project**

## REQ-10 — WITHDRAWN 2026-09-11 · the install runs on published artifacts alone
serves: OBJ-3
statement: **Withdrawn.** No step of the install requires a file, credential or access that only we
have — written when a CUSTOMER performed the install.
acceptance: **None. This requirement no longer has a subject.**

**Why it is struck rather than deleted.** The id is cited by `technical-pieces.md` and by two issues;
deleting it would silently repoint every citation, which is the same defect the backlog's
never-reuse-an-id rule exists to prevent. It keeps REQ-10 forever.

**Why its premise dissolved.** Self-host was dropped on 2026-09-11 and BYOC is the whole of it. Under
BYOC the install is performed by CAPA using the authority CAPI holds at cell birth, in an account we
assume a role into. **No customer ever fetches an artifact**, so "runs on published artifacts alone"
describes an act nobody performs.

**What it takes with it:** `getloopscale/loopzero#358` — a customer cannot pull the published charts (401,
no entitlement). That was this requirement's blocker, and it stops being one. The entitlement is
still worth having the day a customer needs to fetch anything; it is no longer on the critical path.

## REQ-11 — The customer's code and build artifacts do not leave their infrastructure
serves: OBJ-3
statement: During a deploy, the source bundle and the build artifact stay inside the
customer's infrastructure. Artifacts of **ours** come in; what is **theirs** does not go out.
acceptance: Declare the permitted perimeter first — the customer's AWS account, plus the inbound
endpoints from which OUR artifacts are fetched into the cell we operate there. Then network capture during a full
deploy shows **0 bytes of source bundle or build artifact to any destination outside that
perimeter** — ours included, third parties included. Watching only for egress to our hosts was
the earlier criterion, and a build shipping the bundle to a third-party registry or mirror
would have passed it while violating the statement.

## REQ-12 — Two installs of the same version produce the same system
serves: OBJ-3
statement: The install is reproducible from the written procedure, with no decision taken in
the moment.
acceptance: Declare which properties must coincide — component inventory, the resolved version of
each, and a digest of the effective configuration — then have CAPA create the same version twice from
the same ClusterClass and compare all three. Follow with the same functional probe on both installs; both
pass. Matching names alone can hide different versions, different configuration and different
behaviour.

## REQ-13 — An upgrade declares its direction before it is performed
serves: OBJ-3
statement: Each published version declares which versions one may come from, and whether the
path is reversible.
acceptance: Read the published version's declaration, perform one of the paths, and check the
behaviour matches what was declared.

## REQ-14 — The install declares what the customer brings, and refuses early when it is absent
serves: OBJ-3
statement: The prerequisites the customer supplies are declared, and cell creation fails before
changing anything when one of them is missing. Under BYOC this is the D10 eight-item bootstrap
contract — including the ACTIVE validation that assumes the role WITHOUT the `ExternalId` and refuses
it if that succeeds.
acceptance: Run the install with a prerequisite missing; it fails naming what is missing, and
nothing in the environment was altered.

## REQ-19 — A cell is created in an account that is not ours, and reaches workload-ready
serves: OBJ-3
statement: CAPA creates a cell in an AWS account this team does not own, and the cell reaches
workload-ready with no step performed by hand after the bootstrap contract of REQ-14 is satisfied.
acceptance: Declare what workload-ready means first — the cluster exists, its GitOps controller is
running, and an application synced into it reports Healthy. Then create a cell in an account that is
not ours and observe all three. A cell that stands up and cannot receive a workload is not ready; a
cell that receives one only after a manual step did not meet the requirement.

**REQ-19 was added on 2026-09-11, and its absence for a day is the reason to state it plainly.**
OBJ-3 carries two halves — a cell is created and reaches workload-ready, and none of the customer's
bytes leave. The second had REQ-11. **The first had nothing.** REQ-12, REQ-13 and REQ-14 describe how
the provisioning BEHAVES — reproducible, reversible, pre-validated — and not one of them asserts that
it happens. The closest was REQ-10, which at least claimed the install ran, and it was withdrawn with
self-host.

This is not the objective restated. The objective carries the NUMBER (≤ 1 day); the requirement
carries the FACT, with an acceptance somebody can fail. Without it the first half of OBJ-3 had
nothing beneath it to fail — and a half that cannot fail anywhere is the half nobody tests.

**Researched 2026-09-11: the perimeter is only half of what a security review asks.** The other
half is what our control plane RETAINS, and *"a scoped cross-account role"* does not survive reading.
Measured from vendors' own permission documents: **Databricks needs ~45 to 70+ AWS actions**,
including `PutRolePolicy` and `AuthorizeSecurityGroupIngress` — escalation-relevant on their own;
**Redpanda** needs IAM role and policy creation, OIDC provider setup and `PassRole`.

**And two vendors argue publicly against our specific mechanism.** Not against BYOC — against the
assumed cross-account role that D5 uses. WarpStream: *"wouldn't it be better if external access
wasn't required at all?"* Nuon, on why least privilege does not hold: *"handling the minimal set of
permissions prohibits most day-to-day debugging"*. Both propose an agent INSIDE the customer account,
**egress-only, with no inbound path**.

This is the finding with the most leverage, because the mechanism is not yet built. Full record in [`../decisions/byoc-as-the-only-mode-has-no-precedent.md`](../decisions/byoc-as-the-only-mode-has-no-precedent.md).

**The asymmetry in REQ-11 is the requirement, not a detail.** A requirement saying "zero
traffic" would be false — our chart and our images have to come in. What must not happen is
**their code going out**. That direction is what the customer bought.

**REQ-14 came from the C1 scope and survives the change to BYOC**, with its subject moved: the
customer brings an AWS account satisfying the D10 bootstrap contract rather than a Postgres and an
IdP. Without a declaration and an early refusal, cell creation becomes "find out what is missing with
half a cluster standing" — `error-handling.md` fail-fast applied to a provisioner instead of an
installer.

**REQ-13 corrects a defect an architecture review already caught once:** the RFC said
reversibility belongs to the *path* while its YAML used a release-level boolean; it became
`upgradePaths` with a `strategy`. This requirement speaks of paths, not flags.

**REQ-12 is what separates an install from a project.** If every install needs a decision
taken on the spot, it is consulting under another name — which is exactly what C7 recorded as
an accepted risk, with **bus factor 1**.

---

**OBJ-4 — LoopZeroKit is adoptable with no account**

## REQ-15 — The packages install from the public registry with no credential of ours
serves: OBJ-4
statement: Installing LoopZeroKit requires no token, account or authentication configuration of
ours.
acceptance: Clean container, no auth configuration at all: `npm create loopzerokit` and the
install complete.

## REQ-16 — The local agent runs without contacting us
serves: OBJ-4
statement: From install to the agent's first response, nothing is sent to a domain of ours —
telemetry included.
acceptance: Network capture covering install and first run — 0 requests to a domain of ours.

## REQ-17 — What needs the platform is stated, not discovered
serves: OBJ-4
statement: The documentation declares which capabilities work locally and which require the
platform; invoking one that requires it says so rather than failing obscurely.
acceptance: Running locally, invoke a capability that needs the platform. The message names
what is missing and why.

## REQ-18 — The framework does not depend on the commercial product
serves: OBJ-4
statement: LoopZeroKit packages have no dependency on platform packages; the platform reads a
declaration LoopZeroKit produces.
acceptance: Inspect the dependency tree of the published LoopZeroKit packages — no edge to a
platform package.

**REQ-16 has a favourable static inspection and NO acceptance yet — corrected 2026-09-11.**
This paragraph claimed the requirement was *measured true*, and that claim was stronger than
the instrument behind it. The requirement's own acceptance is a **network capture during
install and first run**; what was actually run was a source grep for `fetch`/`axios` and for
domain literals. Asserting a requirement satisfied by a weaker instrument than the one it
names is the failure this whole document is built to prevent, and it was done here while
congratulating the same paragraph for catching a regex false positive.

What the static inspection genuinely shows: no `fetch`/`axios` to an absolute host in any
package, no lifecycle scripts (`preinstall` / `install` / `postinstall` / `prepare`) in any
published package, and the eight occurrences of `loopzerokit.dev` are strings printed in migration
and documentation messages.

What it cannot see, and one of these turned out to be real: **`create-loopzerokit` shells out to
`git clone`** (`clone-example.ts:1`, `execFileSync` from `node:child_process`) — a network
call no `fetch`/`axios` grep could ever find. It is opt-in via `--example=<URL>` and goes to
GitHub rather than to us, so REQ-16 may still hold; that is a different claim from the one
that was made, and only the capture answers it. Also invisible to a grep: transitive
dependencies, DNS prefetch, `sendBeacon`, `new Image()`, WebSocket, and a dynamic import that
fetches at module load.

**Status: static inspection favourable, acceptance pending.** And it remains a product
decision rather than a technical
detail.**REQ-17 is the honest requirement the objective alone would hide.** Some things *do* need the
platform — public URL, TLS, deploy. Pretending otherwise breaks on the first attempt. What the
requirement demands is that she be **told**, not that everything work offline.

---

## Coverage

| Objective | Requirements |
|---|---|
| OBJ-1 | REQ-1, REQ-2, REQ-3, REQ-4 |
| OBJ-2 | REQ-5, REQ-6, REQ-7, REQ-8, REQ-9 |
| OBJ-3 | ~~REQ-10~~ *(withdrawn)*, REQ-11, REQ-12, REQ-13, REQ-14, REQ-19 |
| OBJ-4 | REQ-5, REQ-15, REQ-16, REQ-17, REQ-18 |

Every objective carries requirements. An objective with none would mean it is unreachable or
nobody knows how to reach it, and both are worth finding here rather than in phase 4.

## What OBJ-3's requirements will do on 2026-09-13

OBJ-3 cannot close on its horizon — it installs artifacts the extraction has not produced.
The attempted install will stop somewhere, and **where it stops names which of REQ-10 to
REQ-14 fails first**. That is a better baseline than any date we could have estimated.
