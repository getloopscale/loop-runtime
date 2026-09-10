# RFC — Extracting the composition

- **Status:** **Slice 1 APPROVED** (external architecture review, 2026-09-10) · slices 2–6 pending G1b · slice 7 pending G2
- **Revision:** 5 — 2026-09-10
- **Tier:** T3 — topology
- **Repos affected:** `loopzero` · `loopzero-cluster-management`
- **Stage gates:** G1 ✅ · G1b open · G2 partial · G3 open
- **Measurements against:** `e031f173a` on branch `workspace`

> **On this file.** The rendered version reached the first reviewer as extracted text — table headers
> concatenated, the diagram flattened. This Markdown source exists so the document survives copy/paste
> and any pipeline that strips markup. When the two disagree, this file is the source.

> **On revisions.** Revisions 1–3 were amended in place and the third review found the result
> internally inconsistent — nine contradictions between header and body. Revision 4 was a full rewrite;
> reconciling its numbers uncovered a measurement error of my own (§ 00). Revision 5 fixes a **logic
> defect** the fourth review found: G2 depended on evidence that slices 2–6 produce, so requiring it to
> start them was circular. G1b now separates them.

---

## 00 · Corrections in this revision

**The release count was wrong in every previous revision.** Revisions 1–3 said 101 releases. Measured
correctly — counting only `- name:` entries under the `releases:` key, at release indentation — the
figure is **59**. The 101 came from a regex that also caught `- name:` inside `set:` blocks
(`installCRDs`, `rootPassword`, `auth.existingSecret`) and the `repositories:` section. It propagated
into the diagrams, the changelog and three commits.

| | Previous claim | Measured |
|---|---|---|
| core | 55 | **31** releases (+ 17 repositories) |
| build | 10 | **7** (+ 2) |
| cell | 28 | **15** (+ 9) |
| secrets | 8 | **6** (+ 2) |
| **Total** | 101 | **59** |

Also corrected: `nfs-ganesha` was counted among the components leaving the offering. It is a
repository, not a release. Nine releases leave, not ten.

**Three further corrections, from the third review:**

1. **Air-gapped installation is not free**, and revision 3 claimed it was. A bundle of resolved charts
   does not contain container images, tooling, chart hooks, or offline trust material. Corrected in
   § 05 and § 10.
2. **Digest is two different things** and the document conflated them: the OCI manifest digest that the
   registry addresses, and the content hash of the resolved `.tgz` inside the bundle. Both belong in the
   manifest, for different reasons.
3. **Publication cannot be asserted before ownership is classified.** Revision 3 stated that the engine
   publishes all infrastructure charts. That is a candidate answer, not a decision — the classification
   of the ~10 topology-adjacent charts precedes it.

---

## 01 · Scope, decided

| Question | Decision | Closed |
|---|---|---|
| Build plane | **Included.** The customer builds on their own infrastructure | C1 |
| Identity | **Ory embedded, federated by OIDC** to the customer's IdP | C1 |
| Postgres, object storage | **The customer's** managed services | C1 |
| Distribution registry (our images) | **Ours, central** — shipped as AM6 | C1 |
| The customer's own images | **Theirs** | C1 |
| Gitea | **In-cluster on the customer** — GitOps machinery, not a choice | C1 |
| Observability | **Included, in-cluster** | C1 |
| Our operational instrumentation | **Excluded** — `chaos-mesh`, `opencost`, `cloudflared`, `smoke-test` | C1 |
| Cells | **One now**, N as evolution | C1 |
| Owning repository | **`loopzero-cluster-management`** | C2 |
| Operating model | **One person, strict sequencing** — risk accepted, see § 11 | C7 |
| Bundle concept and channel | **Signed OCI artifact via ORAS, resolved locally** | O3a |

**Traceability.** Each decision below was taken by the sponsor in session on the date given, and is
recorded in this repository — not ratified in a T3 meeting, which for C1/C2/C7 has not occurred:

| Decision | Approver | Date | Record |
|---|---|---|---|
| C1 — commercial scope | Sponsor | 2026-09-10 | § 01 here · `CHANGELOG.md` [Unreleased] · commit `1ad69f1f7` |
| C2 — owning repository | Sponsor | 2026-09-10 | § 11 here · commit `e031f173a` |
| C7 — operating model | Sponsor | 2026-09-10 | § 11 here · commit `e031f173a` |
| O3a — bundle concept | Proposed by engineering, accepted by sponsor | 2026-09-10 | § 05 here · commit `e031f173a` |

⚠️ **These are sponsor-direct decisions, not T3 ratifications.** The governance rule for this repository
requires a meeting for T2/T3 changes, and the two ADRs governing the destination remain `PROPOSED` with
no meeting record since the date set for 2026-08-17. Slice 1 changes no behaviour, so it does not
depend on that; **slices 2–3 and 7 do**, and the meeting is their precondition regardless of G1b.

### Two supported topologies

The OpenBao profile remains commercially supported, so there are two, and the document states both
rather than averaging them.

```text
Self-host baseline            core + build + cell        3 clusters · 44 releases
Optional OpenBao profile      core + build + cell + secrets   4 clusters · 50 releases
```

| | Clusters | core | build | cell | secrets | Total |
|---|---|---|---|---|---|---|
| SaaS today | 5 | 31 | 7 | 15 | 6 | **59** |
| Self-host baseline | 3 | 23 | 7 | 14 | — | **44** |
| Self-host + OpenBao | 4 | 23 | 7 | 14 | 6 | **50** |

**The arithmetic, explicitly:** `59 − 44 = 15`, which is **9 releases excluded by C1** (eight from
`core`, `cloudflared` from `cell`) plus **6 releases of the `secrets` composition**, absent when the
customer brings their own secret store.

Excluded from the target and from the test matrix, because C1 removed them: `build: external` as an
option, multiple cells as a day-one requirement, and every component C1 named.

### Two design constraints that follow

1. **The engine must accept an external identity provider.** It does not today. Ory stays as a broker,
   not as the source of identity. This is product work C1 created.
2. **Source bundles never leave the customer's boundary.** Measured: `loopzero-builds` is where
   `POST /source/upload` deposits customer source and where the build clones from. Centralising object
   storage in our infrastructure would move proprietary source out of their account on every deploy,
   voiding the reason they bought self-host.

*Why not "manage everything to reduce friction":* it moves friction from the sale to operations, and
multi-cloud multiplies it per provider. Two external data points, cited rather than asserted — both
retrieved 2026-09-10:

| Source | What it says |
|---|---|
| [GitLab — Use cloud services for GitLab components](https://docs.gitlab.com/install/cloud-services/) · [Migrate from the bundled charts](https://docs.gitlab.com/charts/installation/migration/bundled_chart_migration/) | The bundled PostgreSQL, Redis and MinIO charts are for **evaluation purposes only**; production requires external services. In the cloud-native architecture, *"external PostgreSQL and Redis/Valkey services are required. These components are not bundled in the chart."* |
| [Gitpod — Self-hosted, not self-managed](https://www.gitpod.io/blog/self-hosted-not-self-managed) · [devclass, 2022-12-09](https://www.devclass.com/devops/2022/12/09/gitpod-abandons-self-hosted-product-in-favor-of-dedicated-cloud/1629631) · [Emms, 2023-02-05](https://simonemms.com/blog/2023/02/05/self-hosted-is-dead-long-live-self-hosted) | Sunset self-hosted after three years: *"increasingly difficult for us to support … a burden for our clients to manage and operate."* The named cause was aiming at every managed Kubernetes provider. Cost reported: seven figures of net-new revenue forfeited and 28% of staff laid off. |

**Read as evidence, not as proof.** Neither company is LoopZero, and both figures come from their own
published accounts. What they establish is that the failure mode is real and has been paid for twice,
not that our outcome is determined.

---

## 02 · Stage gates

Previous revisions grouped seven conditions into one list, which made "unblocked" ambiguous. They are
separated by what they actually gate.

### G1 — Migration may start (slice 1)

| # | Condition | State |
|---|---|---|
| C1 | Commercial scope | **Closed** — § 01 |
| C2 | Owning repository | **Closed** — `loopzero-cluster-management`, § 11 |
| C7 | Operating model | **Closed as an accepted risk** — § 11 |
| O3a | Bundle concept and channel | **Closed** — § 05 |

**G1 is satisfied. Slice 1 may be authorized — and only slice 1.**

### G1b — Slices 2–6 may proceed

Slices 2–6 *produce* the evidence G2 needs. Requiring G2 to start them would be circular, and revision
4 said so by implication when it stated that nothing beyond slice 1 proceeds on G1 alone. Corrected:

| # | Condition | State |
|---|---|---|
| — | Slice 1 merged and the dev loop green against the moved tree | Open |
| — | #356 exemption ported, so slice 2 does not eliminate the correct copy | Open |
| O3b-cut | Manifest schema, generator, internal hashes, deterministic bundle build | Open |

**These slices are reversible** — each is a forward commit away from undone, and none has been
published to anyone. That is why they need an authorization weaker than the cut's.

### G2 — The cut may proceed (slice 7 only)

| # | Condition | State |
|---|---|---|
| C4 | All 43 gates classified, with owner and input | **Draft delivered**; 4 open questions |
| C5 | Infrastructure chart ownership classified, then publication decided | **Open** — ownership precedes publication |
| C3 | Release manifest implemented and produced by a pipeline | **Open** — shape defined, mechanism not |
| — | Boundary gate green (slice 6) | Open |
| — | Rendered-manifest equivalence, pre-cut vs post-cut | Open |

### C1 traced to acceptance criteria

C1 is a commercial decision; G3 needs it as testable criteria. Each row below is a scope decision turned
into something that can pass or fail — **none is written yet**, and writing them is part of C6.

| C1 decision | Acceptance criterion it becomes |
|---|---|
| Ory federated by OIDC | Login succeeds against a customer-operated IdP; the engine accepts an external issuer; token refresh and logout work end to end |
| Postgres external | Declared supported versions and required extensions; connection by reference only; failover behaviour of the customer's managed service does not corrupt platform state |
| Object storage external | Declared compatibility contract — which S3 API surface is required; bucket lifecycle and versioning expectations |
| Gitea in-cluster | Survives node loss; its state is included in backup and restore |
| Observability included | The platform's own health is visible to the customer without our access |
| **Source never leaves the boundary** | **Proven, not asserted**: a network-level test showing no source bundle or build artifact egresses to our infrastructure during a deploy |
| Registry central | Behaviour under registry unavailability — see the acceptance matrix, which now covers pull-dependent paths |

The sixth row is the one that must be demonstrated rather than documented, because it is the promise
the customer bought.

### G3 — A version may be offered to a customer

| # | Condition | State |
|---|---|---|
| C6 | Acceptance matrix passed per supported profile | **Open** |
| O3c-release | Signing identity, retention, provenance, SBOM, trust policy | **Open** |
| — | Upgrade path modelled with its reversibility class | **Open** |
| — | Finite compatibility matrix strategy | **Open** |

---

## 03 · As-is

| Figure | Value |
|---|---|
| Helm releases across four compositions | **59** |
| Relative `../helm/*` references | 75 |
| Quality gates reading this tree | 43 of 150 |
| Artifacts describing a platform version | **0** |

### Four compositions, not one

| Composition | Kube context | Releases | Multiplicity | In destination's scope list? |
|---|---|---|---|---|
| `core` | `k3d-loopzero-core` | 31 | singleton | No |
| `build` | `LOOPZERO_BUILD_CONTEXT` | 7 | singleton | No |
| `cell` | `requiredEnv LOOPZERO_CELL_CONTEXT` | 15 | **once per cell** | Yes — the only one |
| `secrets` | `k3d-loopzero-secrets` | 6 | singleton, gated off | No |

`cell` is the only one whose context is `requiredEnv` — the only one that is multiplied, and the only
one the destination claims.

### The coupling that blocks delivery

| Kind | Count | Consequence |
|---|---|---|
| `values:` — configuration | 55 | Composition. Travels with the Helmfiles, preserving the relative path rather than breaking it. |
| `chart:` — local chart directories | 20 (18 unique) | The real work. Must become resolved artifacts or cease to exist. |

While those references exist, the composition only runs from inside the engine's source tree. A
customer who bought *"same runtime, on your infrastructure"* would have to clone the engine to install.
The destination repository already forbids this shape between repositories — *"a source tree of another
repository is a dependency, and that one is prohibited"*. Here the other repository is the customer.

Measured irony: third-party charts already arrive by pinned version; only the house's own arrive by
path.

### What does not exist at all

No artifact names a version of the platform. `Chart.lock` files exist per chart — Helm dependency
resolution — and nothing above them. A search for `platformVersion` returns nothing.

---

## 04 · To-be

```
  loopzero                  private registry        cluster-management         topology
┌──────────────┐      ┌──────────────────┐    ┌────────────────────┐    ┌────────────────────┐
│ api          │PUBLISH│ cosign · digests │RESOLVE│ platform.lock    │APPLY│ core     23 rel   │
│ operators    ├──────►│ entitled pull    ├────►│ resolved bundle    ├───►│ build     7 rel   │
│ pkg          │      │                  │    │ compose/ values/   │    │ cell     14 rel   │
│ charts       │      │                  │    │                    │    │ [secrets  6 rel]  │
└──────────────┘      └──────────────────┘    └────────────────────┘    └────────────────────┘
                                                                          baseline: 3 clusters
                                                                          + OpenBao: 4 clusters
```

The engine publishes the charts it owns. The destination never publishes charts — it resolves them
into one signed bundle. `[secrets]` is the optional profile.

| Dimension | As-is | To-be |
|---|---|---|
| Ownership | Closed scope list names one composition; three have no owner | Destination owns the **platform release**: BOM, compatibility, install order, upgrade, rollback, supported topologies, end-to-end tests, operator docs |
| Versioning | No artifact names a platform version | Signed release manifest pinning every component; reproducible later; verified by the installer |
| Coupling | 75 relative paths; runs only inside the engine tree | Resolved artifacts in a signed bundle; boundary gate asserts no relative path crosses |
| Validation | 43 gates read the tree from the engine's binary and cannot follow it out | Six gate classes with declared owners **and inputs**; no gate reads across a repository boundary |
| Operations | Clean install on k3d is the only exercised path | Acceptance matrix per supported profile, passed before a version is offered |
| Self-host install | Impossible without cloning the engine | Verify signature, resolve bundle, apply profile. Engine source never ships |

---

## 05 · Platform release contract

### O3a — closed: concept and channel

**The bundle is a signed OCI artifact, published with ORAS, containing charts resolved and verified at
build time.** ORAS is a CNCF subproject and the standard vehicle for non-image OCI artifacts; it reuses
the registry, the entitlement token and the keyless cosign the pipeline already runs on tag `v*`, and
gives an immutable digest. Cost: one new binary in the pipeline — `oras` is not installed today,
`cosign` is.

The reason resolution happens at build time is measured, not stylistic:

| Path | Result |
|---|---|
| `helm pull oci://…@sha256:…` | **Works** — artifact retrieved, named by digest |
| `helm pull --version sha256:…` | Fails — `improper constraint`; the flag is semver-only |
| `helmfile chart: oci://…@sha256:…` | **Rejected** — *"the version for OCI charts should be semver compliant"* |
| `helmfile version: "sha256:…"` | Fails — becomes the literal tag `nginx:sha256:…` |
| `helmfile chart: ./chart.tgz` | **Works** — renders from a pre-resolved local artifact |

Helm 3.19.0, Helmfile 1.1.6. **Helm supports digests; Helmfile does not.** So the installer must
resolve and verify first, then hand local artifacts to Helm.

```
platform bundle — OCI artifact, versioned and signed
├── platform.lock          release manifest
├── charts/                resolved .tgz set, verified at resolve time
├── compose/               helmfiles, consuming ./charts/ by path
├── values/
├── schemas/
├── installer/
└── provenance/            SBOM, signatures, attestations
```

### O3b-cut and O3c-release — open, and they gate different things

Revision 4 put schema, generator and hashes in O3b at G3, while C3 at G2 required a working manifest
and pipeline. That overlapped. Split:

| | Contents | Gate |
|---|---|---|
| **O3b-cut** | Manifest schema, generator, internal hashes, deterministic bundle build | **G1b/G2** — the cut needs a bundle that builds reproducibly |
| **O3c-release** | Signing identity, retention policy, provenance chain, SBOM, trust policy | **G3** — a customer needs to verify it, we do not |

C3 is satisfied by O3b-cut; O3c-release is what turns a buildable bundle into a shippable one.

### Two digests, and they are not interchangeable

Revision 3 used "digest" for both. They answer different questions and the manifest carries both:

| | What it is | What it proves |
|---|---|---|
| **OCI manifest digest** | What the registry addresses — `oci://repo@sha256:…` | Provenance: this is the artifact the publisher signed, in that registry |
| **Resolved `.tgz` content hash** | Hash of the file inside the bundle | Integrity: the bytes the installer applies are the bytes that were resolved |

Without the first, the bundle cannot prove where a chart came from. Without the second, a bundle could
be altered after resolution and still cite a legitimate origin.

### Air-gapped installation — NOT free, and revision 3 was wrong

Revision 3 claimed air-gap "falls out for free" from the resolved bundle. It does not. **A bundle of
charts is not a bundle of software.** What a genuine air-gapped install additionally requires:

- **Container images.** Charts reference images; they do not contain them. This is the bulk of the
  bytes and none of it is in the bundle today.
- **Chart hooks and their images**, which run at install time.
- **Tooling** the installer shells out to.
- **Offline trust material** — cosign keys or the TUF root — since verification cannot reach a
  transparency log or key server.
- **Third-party charts and their images**, mirrored, since upstream is unreachable.

The resolved bundle is a **precondition** for air-gap, not air-gap. Position for v1: **not supported**,
stated explicitly rather than left silent — silence becomes a support commitment by default.

### Chart ownership precedes publication

Revision 3 asserted the engine publishes every chart including the ~10 infrastructure ones. That is a
**candidate answer**, not a decision. `network-policies`, `cluster-issuer`, `cloudflared` and the
`openbao-*` charts sit closer to topology than to any capability of the engine, and publishing
convenience does not establish ownership.

Sequence: classify each of the ~10 — capability implemented, who answers for behaviour, who publishes,
who approves changes, which consumers exist — **then** decide publication. That classification is C5
and gates G2.

The bill of materials also reaches past the engine's charts: charts produced by `loopzero-data-cells` and
pinned third-party charts are installed, so they are pinned.

### Release manifest and deployment specification

```yaml
# platform.lock — RELEASE MANIFEST. One per platform version.
# What is supported. Identical for every customer on this version.
platformVersion: 2026.9.0
components:
  loopzero-api:
    version: 4.7.2
    ociDigest:  "sha256:…"   # provenance — what the registry addressed
    contentHash: "sha256:…"  # integrity — the .tgz inside this bundle
supportedProfiles:
  secrets: [openbao, external]     # the two topologies of § 01
compatibility:
  kubernetes: ">=1.31 <1.34"
  airGapped: false                 # explicit, not inferred from silence
upgradePaths:                      # reversibility belongs to the PATH, not the release
  - from: "2026.8.x"
    strategy: forward-only         # or: reversible
    compatibilityWindow: "24h"     # how long N-1 keeps working alongside N
    backupRequired: true
  - from: "2026.7.x"
    strategy: reversible
    backupRequired: false
```

```yaml
# deployment.yaml — DEPLOYMENT SPECIFICATION. One per installation.
# What this customer chose. Never signed by us; owned by them.
platformVersion: 2026.9.0
profile:
  secrets: external
endpoints:    { … }
storageClass: …
domains:      [ … ]
secretRefs:   { … }
```

Note that `build` no longer appears as a profile axis: C1 included it in the offering, so it is not a
choice.

---

## 06 · Ownership

| Responsibility | loopzero | cluster-management | data-cells |
|---|---|---|---|
| Deployable components and their contracts | **Owns** | Consumes | Consumes |
| Chart schema, values contract, chart behaviour | **Owns** | — | — |
| Chart publication and signing | **Owns** (pending C5 for the ~10) | — | Owns its own |
| Bill of materials / platform version | — | **Owns** | — |
| Cross-chart compatibility | — | **Owns** | — |
| Install order and topology profiles | — | **Owns** | — |
| Upgrade, rollback, migration sequencing | Provides per-chart | **Owns end-to-end** | Owns data migrations |
| Supported Kubernetes versions | — | **Owns** | — |
| End-to-end platform tests | — | **Owns** | — |
| Self-host operator documentation | — | **Owns** | — |
| Tenant placement, cell lifecycle | — | **Owns** | — |

### The membership test

> What belongs to the composition repository is everything that determines an installable and supported
> version of the platform, without implementing the internal capabilities of the components that
> compose it.

| Composition | Verdict |
|---|---|
| `cell` | Belongs. Unambiguous. |
| `core` | Belongs — a requirement of the supported platform. |
| `build` | **Belongs** — C1 included the build plane in the offering, which is the test that decides it. |
| `secrets` | Belongs as an **optional profile**. |

---

## 07 · Migration

### Slices

| # | Slice | Behaviour change | Gate |
|---|---|---|---|
| 1 | Subtree born under its final name. `git mv` of Helmfiles, environments, values; Taskfile, Tiltfile, gates and tests repointed in the same commit. Boundary gate born in warn mode. | None | **G1 ✅ authorized** |
| 2 | Kill the three duplicated pairs — **and delete the three gates that police them, in this same slice** | Chart origin | G1b |
| 3 | Kill the pure wrappers — upstream chart plus local values | Chart origin | G1b |
| 4a | Classify ownership of the ~10 infrastructure charts (C5) | None | G1b |
| 4b | Decide publication from that classification; publish and prove equivalence | None | G1b |
| 4c | Switch the composition to the resolved artifacts | Chart origin | G1b |
| 4d | Delete the local copies | None | G1b |
| 5 | Move the 15 travelling gates with their inputs | None | G1b |
| 6 | Boundary gate flips to deny and goes green | None | G1b |
| 7a | Extract and graft into the destination; the engine still uses its local copy | No | G2 |
| 7b | Repoint the engine, then delete the local subtree | Yes | G2 |

Movement and behaviour change never share a commit: when the dev loop breaks, bisection must
distinguish *the file moved* from *the chart now comes from somewhere else*.

**Slice 2 carries an ordering constraint that inverts the outcome if missed.** Killing the
`admission-policies` pair makes the *published* chart the only copy — and that copy is missing a policy
exemption the bootstrap copy carries (#356). The exemption is ported first.

### Reversibility, by path

| Kind | Reversible? | Mechanism |
|---|---|---|
| Repository — move, graft, deletion | Yes | Forward commit restoring the tree; history preserved on both sides |
| Publication — a version offered to customers | Partly | A digest cannot be unpublished from someone who pulled it. Recovery is a superseding version plus an advisory |
| Installation — a real cluster upgraded | **Depends on the release** | Declared per version in `compatibility.reversible`. Where false, forward recovery is the only path |

**Reversibility belongs to the path, not to the release.** Revision 4 said so and then contradicted
itself with a release-level `reversible: false` boolean — corrected above: each entry in
`upgradePaths` carries its own `strategy`, `compatibilityWindow` and `backupRequired`. A path marked
`forward-only` is tested by roll-forward, never by a rollback that cannot pass; a path marked
`reversible` is tested by an actual rollback after CRD, policy and schema changes have landed.

### Freeze window

- **Authorisation:** the cut proceeds on the sponsor's explicit go, recorded.
- **Maximum duration:** bounded; exceeding the bound aborts rather than extends.
- **Abort:** any composition change required during the window aborts 7a. Editing either copy is
  prohibited.
- **Hotfix:** a production hotfix aborts the cut. Shipping a fix outranks the migration.
- **Concurrency:** both repositories' composition paths are protected for the duration.
- **Equivalence proof:** rendered-manifest comparison, pre-cut versus post-cut, byte-identical modulo
  declared differences — automated.
- **Tags and releases** carried and audited, not just commits.

### Precedent, and where it stops applying

Three prior extractions preserved history — the CLI (319 commits), shared contracts (48), the control
plane (2,091), each verifiable by identical commit subjects under different SHAs. All three grafted
into **empty** repositories. This destination has 16 commits, a founding ADR and a 33-concept wiki, so
the graft is a union of unrelated histories: `filter-repo` in a throwaway clone, then a merge allowing
unrelated histories, via pull request — not a subtree add, which buries the provenance.

### Development flow during the transition

While the composition consumes resolved artifacts, a developer changing a chart needs a path that does
not require publishing first. **Candidate charts:** the composition accepts a local override tree for
development, and the boundary gate distinguishes a declared development override from a leaked
relative path. Without this, every chart change becomes a publish cycle, and the dev loop that
`tilt up` provides today is lost. **This is unresolved and belongs in G2.**

---

## 08 · Quality gates

| Gate class | Owner | Input |
|---|---|---|
| A — Helmfile and values integrity | Composition | Its own tree |
| B — Policy over the topology | Composition | Its own tree |
| C — Chart schema and behaviour | Engine | Minimal fixtures owned by the engine |
| D — Chart × composition compatibility | Composition pipeline | Published chart + candidate manifest |
| E — Transversal conventions | Shared, versioned tooling | Whichever tree invokes it |
| F — End-to-end platform test | Composition | A resolved bundle |

Classification delivered: `cluster-management-extraction-gate-classification.md`. **15 travel, 25 stay,
3 cease to exist.** The withdrawn "approximately eight" estimate was low by nearly half.

Three findings outweigh the counts:

1. The three dying gates exist to police the pairs slice 2 kills, so they are deleted **inside** that
   slice — deleting earlier removes the only checks on a live divergence.
2. **Class D is empty.** Nothing tests chart-versus-composition compatibility today, because there was
   no seam. It is the class the split creates the need for, and it does not exist.
3. Four transversal gates read an allowlist file by literal path that this repository no longer
   contains. They are red **before** any extraction (#281); the split cannot fix them.

---

## 09 · Operational acceptance (G3)

A platform version is offered to a customer only after this passes against a representative
environment, **per supported profile**.

| Scenario | Acceptance criterion |
|---|---|
| Clean install | Per profile — baseline (3 clusters) and OpenBao (4). Supported Kubernetes versions and distributions; supported CNI, CSI, ingress, storage classes. Each converges and completes a deploy end to end. |
| Upgrade N → N+1 | From every version in `upgradeFrom`; no data loss; availability against a **declared SLO with a permitted interruption budget**. |
| Rollback (release with `reversible: true`) | Executed after CRD, policy and schema changes have landed. |
| Forward recovery (release with `reversible: false`) | Expand/contract applied, backward compatibility held for a declared window, backup validated, roll-forward tested. |
| Backup and restore | Restore into a fresh cluster; declared RTO met and measured. |
| Registry token rotation | Rotation without downtime; a revoked token fails closed with an actionable message. |
| Registry unavailable | Not just "keeps serving" — that misses every path that needs a fresh pull. Must cover: **node replacement**, **scale-out**, **pod reschedule**, and any **new image pull**. Acceptance is a declared degradation mode: what still works, what stops, and how long the installation survives without the registry. |
| Secret backend change | **Open — is this contracted?** Supporting two topologies does not imply online migration between them. If migrating is not a commercial requirement, this row leaves G3 and becomes "choose at install time, changeable only by reinstall". Decision needed before G3 closes. |
| Support diagnostics | A single documented command collects the evidence support would ask for. |

**The compatibility matrix must be finite.** Kubernetes versions × distributions × CNI × CSI × ingress
× storage classes is unbounded. Strategy: a **named support tier** — one primary combination fully
exercised per release, a declared secondary set tested at reduced depth, and everything else explicitly
unsupported. Which combinations occupy each tier is open and belongs to G3.

---

## 10 · Installer security model

- **Verification mandatory at install**, not merely produced upstream. An unverifiable artifact fails
  closed.
- **Both digests pinned** — OCI manifest digest for provenance, content hash for integrity.
- **Credential lifecycle** — issuance, storage, rotation, revocation, with revocation observable.
- **Least privilege** — the RBAC the installer requires, stated and minimised.
- **Secrets in values** — the `existingSecret` floor enforced by the composition, not documented.
- **Root of trust and key rotation** — which key signs a version, how it rotates, how a customer learns
  the new one.
- **Key compromise response** — revocation path and customer notification, decided before it is needed.
- **Detached signatures** via OCI referrers; never self-referentially inside the signed content.
- **Registry retention** — garbage collection must not delete a digest a supported version still pins.
- **Upstream mirroring** — third-party charts mirrored, so an upstream deletion cannot break a
  reproducible install.
- **SBOM at both levels** — aggregate for the version and per component.
- **The bundle itself is signed**, not only its contents.
- **Privileged hooks** — what a chart may execute at install, and what the composition refuses.
- **Air-gapped installation: not supported in v1.** Stated explicitly. See § 05 for what it would
  require.

---

## 11 · Governance — C2 and C7 as accepted risk

C2 and C7 are **decided**, not eliminated. Recording them as resolved without recording the risk would
be the same defect this revision exists to fix.

### C2 — `loopzero-cluster-management`

The condition that would justify a dedicated distribution repository — distinct teams, cadences or
operational responsibilities — was measured against six months of history:

| Repository | Commits | Authors |
|---|---|---|
| `loopzero` | 5,248 | 1 |
| `loopzero-cloud` | 2,091 | 1 |
| `loopzero-data-cells` | 23 | 1 |
| `loopzero-cluster-management` | 16 | 1 |

One author across all four. A fourth repository would add a fourth pipeline and a fourth governance to
the same person.

**What this does not demonstrate:** that cluster lifecycle and product composition have the same
cadence or the same operational responsibility. It demonstrates only that no separate team exists to
receive them. The decision is sound today and its premise is time-bound.

### C7 — accepted risk record

| Field | Value |
|---|---|
| **Risk** | Bus factor 1 on a commercially sold product |
| **Risk owner** | The sponsor |
| **Exception duration** | Until the first self-host customer is installed and supported through one upgrade |
| **Continuity plan** | **Not defined** — this is the gap, stated rather than implied |
| **Support during absence** | **Not defined** |
| **Review trigger** | Second self-host customer, OR the first production incident on a customer installation, OR C2's premise changing (a second author) |

**"Strict sequencing" resolves priority contention, not operational capacity.** Self-host runs until
the customer is installed; the data plane follows. That answers which work happens first. It does not
answer who responds when a customer installation fails at 03:00, and this document does not pretend
otherwise.

---

## 12 · Open decisions and risks

| # | Open | Gate |
|---|---|---|
| O3b | Manifest schema, generator, internal hashes, signing chain, retention, provenance | G3 |
| O4 | Cross-repository gate execution mechanism (class D does not exist) | G2 |
| O5 | Development flow with candidate charts during the transition | G2 |
| O6 | Which combinations occupy each support tier of the compatibility matrix | G3 |
| O7 | Continuity plan and incident coverage for bus factor 1 | G3 |

| # | Risk | Note |
|---|---|---|
| R1 | SaaS-only assumptions travel with the files | k3d in the environments, a per-environment database split, a CDN on the TLS and domain path, in-cluster Git. Moving files does not remove these |
| R2 | The governing rules are unreadable from this repository | Four of eleven binding contracts resolve nowhere; six only inside sibling repositories (#281) |
| R3 | Bus factor 1 | § 11 |

### Evidence about the record itself

Measuring for this document produced two defects that were not code: the published admission-policy
chart ships a fail-closed policy without the exemption its bootstrap twin carries (#356), and the
ratified ADR deciding the secrets topology says the opposite of what the repository runs, with a second
topology decision leaning on that practice through a citation to a file that does not exist (#357,
fixed).

Add to that the release-count error corrected in § 00, which was mine and survived three revisions.
**Where this document cites a number, the citation is the claim; where it cites a decision, check that
the decision still says what the citation says it says.**

---

## 13 · Evidence

| Claim | How measured | Result |
|---|---|---|
| 59 releases | `- name:` under the `releases:` key, at release indentation, per Helmfile | 31 + 7 + 15 + 6 |
| 9 releases excluded by C1 | each named component checked against that release list | 8 core + 1 cell |
| 44 / 50 releases in the two topologies | 59 − 9 − 6, and 59 − 9 | see § 01 |
| 75 relative references | `grep -c '\.\./helm' infra/helmfile/*.gotmpl` | 40 + 21 + 9 + 5 |
| 20 `chart:` vs 55 `values:` | field split of the same matches | 20 / 55 |
| 43 of 150 gates | `grep -rl -e 'infra/helm' -e helmfile cmd/loopzero-ops/internal/quality/*/check.go` | 43 / 150 |
| Only `charts/` is published | `release-artifacts.yml` iterates `charts/*/` | 12 dirs |
| No platform version artifact | search for `platformVersion`; no helmfile lockfile | 0 hits |
| Helm accepts digest, Helmfile does not | live pull + `helmfile template` against a public registry | § 05 |
| One author across four repositories | `git log --since='6 months ago' --format='%an' \| sort \| uniq -c` | § 11 |
| Prior extractions preserved history | identical commit subject, different SHA, both repositories | 3 of 3 |
| Secrets composition gated off | `grep 'openbaoEnabled'` across helmfile and environments | 4 gates · 1 of 8 envs |
| Zero submodules in the ecosystem | `find -name .gitmodules` across 21 repositories | 0 |

### Rejected alternatives

| Option | Why rejected |
|---|---|
| Git submodule of the destination inside the engine | Inverts the dependency graph and closes a cycle; central case of the rule that repository states |
| Build the composition fresh in the destination | Loses history and guarantees divergence during transition |
| Strangler — both copies live for a period | Duplicating a composition duplicates knowledge. This repository has the scar: a mirrored document with an "edit one, copy the diff" rule; the mirror is gone and the rule died with it |
| Gates in the engine reading the extracted tree | Requires the destination checked out during validation — the submodule through the back door |
| Move only the cell composition | The installer is assembled from two repositories — the problem the extraction exists to solve, one level down |
| Infrastructure charts travel into the subtree *(rev 1)* | Contradicts resolved-artifact consumption and makes the destination a chart publisher |
| Composition consumes charts directly by digest *(rev 2)* | Measured: Helmfile rejects digest references |
| Air-gap as a free consequence of the bundle *(rev 3)* | Charts are not images. Corrected in § 05; v1 does not support air-gap |
| A dedicated `loopzero-distribution` repository | Its premise — separate teams or cadences — is measurably absent today. Revisit under C7's trigger |
| Manage the customer's stateful dependencies for them | Moves friction from sale to operations, multiplied per cloud. GitLab and Gitpod are the evidence |

---

*Revision 4 — full rewrite after three review rounds. Measurements against `e031f173a` on branch
`workspace`. The nine settled mechanism decisions are in
`docs/wiki/grills/cluster-management-extraction-grill.md`; the gate classification and two diagrams are
under `docs/architecture/`.*
