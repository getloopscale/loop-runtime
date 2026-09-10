# RFC — Extracting the composition

- **Status:** Architecture direction accepted · execution not authorized
- **Revision:** 3 — 2026-09-10 (two rounds of external architecture review absorbed)
- **Tier:** T3 — topology
- **Repos affected:** `loopzero` · `loopzero-cluster-management`
- **Precedent to slice 1:** C1 · C2 · C7 · minimum answer on O3
- **Rendered version:** published artifact (same content, designed for reading)

> **On this file.** The rendered version reached the first reviewer as extracted text — table headers
> concatenated, the diagram flattened to loose words. This Markdown source exists so the document
> survives copy/paste, quoting and any pipeline that strips markup. Both are kept in sync; when they
> disagree, this file is the source.

---

## 01 · What is being asked

We are not asking whether to do this — a paying customer needs the platform on their own
infrastructure, and the current arrangement cannot deliver that. We are asking whether the seam is cut
in the right place.

All measurements are against commit `26ea7a079` on branch `workspace`. Where a figure comes from
reasoning rather than measurement, it says so.

### Review history

| Revision | Verdict | What it changed |
|---|---|---|
| 1 | Approve with conditions | Found two errors of fact: chart publication was missing from the plan and made the design self-contradictory; gates left behind had the same input problem as gates leaving. |
| 2 | Direction accepted, execution not authorized | Six further blocking findings. One — the digest-pinning spike — was answered by measurement and **changed the design**. |

### The finding that changed the design

The reviewer asked for executable proof that digest pinning works end to end. It does not.

| Path | Result |
|---|---|
| `helm pull oci://…@sha256:…` | **Works** — artifact retrieved, named by digest |
| `helm pull --version sha256:…` | Fails — `improper constraint`; the flag is semver-only |
| `helmfile chart: oci://…@sha256:…` | **Rejected** — *"the version for OCI charts should be semver compliant"* |
| `helmfile version: "sha256:…"` | Fails — becomes the literal tag `nginx:sha256:…` |
| `helmfile chart: ./chart.tgz` | **Works** — renders from a pre-resolved local artifact |

Measured against Helm 3.19.0 and Helmfile 1.1.6. **Helm supports digests; Helmfile does not.** So
"every chart by pinned digest, consumed directly by the composition" is not executable with the current
toolchain. The installer must resolve and verify first, then hand local artifacts to Helm — which is
what § 05 is now built around.

### Blocking conditions

| # | Condition | Owner | State |
|---|---|---|---|
| C1 | Resolve the commercial scope of self-host | Product | Open — precedent to slice 1 |
| C2 | Formalise the owning repository of the platform release | Sponsor | Open — precedent to slice 1 |
| C3 | Define a bill of materials with immutable digests | Engineering | Shape defined (§ 05), mechanism open |
| C4 | Close the classification of all 43 gates | Engineering | Open — neutral prep, may start now |
| C5 | Add publication and signing of infrastructure charts | Engineering | Corrected in § 05 |
| C6 | Prove clean install, upgrade and rollback | Engineering | Matrix defined (§ 09) |
| C7 | Name an operational owner and reserved capacity | Sponsor | Open — precedent to slice 1 |

---

## 02 · Context

LoopZero is a CLI-first managed cloud on Kubernetes. Three repositories carry it: `loopzero` (the engine — API,
operators, shared libraries, published charts), `loopzero-cluster-management` (the shared substrate —
cluster lifecycle, tenant placement, cell composition), and `loopzero-data-cells` (the data plane). The
graph between them is acyclic by rule.

Self-host is a published commercial promise. Two of the three enabling layers already shipped:
artifacts distributed through a private OCI registry with cosign signatures and per-customer revocable
tokens, and published charts carrying fail-fast schemas with secrets by reference.

What never shipped is the layer between them: whoever turns 101 Helm releases into a version of the
platform someone can install, upgrade and be supported on. Revision 1 called that an installer. The
reviews' central point is that the installer is the smaller half — the larger half is a **release
contract**.

---

## 03 · As-is

| Figure | Value |
|---|---|
| Helm releases across four compositions | 101 |
| Relative `../helm/*` references | 75 |
| Quality gates reading this tree | 43 of 150 |
| Artifacts describing a platform version | **0** |

### Four compositions, not one

| Composition | Kube context | Releases | Multiplicity | In destination's scope list? |
|---|---|---|---|---|
| `core` | `k3d-loopzero-core` | 55 | singleton | No |
| `build` | `LOOPZERO_BUILD_CONTEXT` | 10 | singleton | No |
| `cell` | `requiredEnv LOOPZERO_CELL_CONTEXT` | 28 | **once per cell** | Yes — the only one |
| `secrets` | `k3d-loopzero-secrets` | 8 | singleton, gated off | No |

`cell` is the only one whose context is `requiredEnv` — the only one that is multiplied, and the only
one the destination claims.

### The coupling that blocks delivery

| Kind | Count | Consequence |
|---|---|---|
| `values:` — configuration | 55 | Composition. Travels with the Helmfiles, preserving the relative path rather than breaking it. |
| `chart:` — local chart directories | 20 (18 unique) | The real work. Must become resolved artifacts or cease to exist. |

While those references exist, the composition only runs from inside the engine's source tree. A
customer who bought *"same runtime, on your infrastructure"* would have to clone the engine to install.
The destination repository already forbids exactly this shape between repositories — *"a source tree of
another repository is a dependency, and that one is prohibited"*. Here the other repository is the
customer.

Measured irony: third-party charts already arrive by pinned version; only the house's own arrive by
path. The outside components are less coupled than the inside ones.

### What does not exist at all

No artifact names a version of the platform. `Chart.lock` files exist per chart — Helm dependency
resolution — and nothing above them. A search for `platformVersion` returns nothing.

---

## 04 · To-be

```
  loopzero                    private registry           cluster-management            topology (×2)
┌──────────────┐        ┌──────────────────┐      ┌────────────────────┐      ┌──────────────────┐
│ api          │ PUBLISH│ cosign · digests │RESOLVE│ platform.lock      │ APPLY│ core   55 rel    │
│ operators    ├───────►│ entitled pull    ├──────►│ compose/ values/   ├─────►│ build  10 rel    │
│ pkg          │        │                  │      │ → signed bundle    │      │ cells  28 rel ×N │
│ ~22 charts   │        │                  │      │                    │      │ secrets 8 (cond) │
└──────────────┘        └──────────────────┘      └────────────────────┘      └──────────────────┘
```

The engine publishes every chart it owns, including infrastructure charts it does not itself install.
The destination never publishes charts — it resolves them into one signed bundle. The topology is
instantiated twice: on our account and on the customer's.

| Dimension | As-is | To-be |
|---|---|---|
| Ownership | Closed scope list names one composition; three have no owner in the record | Destination owns the **platform release**: BOM, compatibility, install order, upgrade, rollback, supported topologies, end-to-end tests, operator docs |
| Versioning | No artifact names a platform version | Signed release manifest pins every component by digest; reproducible months later; verified by the installer |
| Coupling | 75 relative paths; runs only inside the engine tree | Resolved artifacts in a signed bundle; boundary gate asserts no relative path crosses |
| Validation | 43 gates read the tree from the engine's binary and cannot follow it out | Six gate classes with declared owners **and declared inputs**; no gate reads across a repository boundary |
| Operations | Clean install on k3d is the only exercised path | Acceptance matrix per profile, passed before a version is offered |
| Self-host install | Impossible without cloning the engine | Verify signature, resolve bundle, apply profile. Engine source never ships |

---

## 05 · Platform release contract (C3, C5)

Pinning charts individually does not produce an installable platform — it produces 101 independently
versioned things and no statement about which combination is supported.

### Correction: who publishes what

Revision 1 proposed that ~10 house infrastructure charts move into the subtree. **Withdrawn.** It
contradicted pinned-coordinate consumption and would have made the destination a chart publisher, which
its own dependency ruler forbids.

Corrected: **the engine publishes every chart it owns**, including infrastructure charts it does not
install. The release pipeline today iterates `charts/*/` and pushes to `oci://ghcr.io/<ns>/charts`; the
~10 are promoted into that tree and inherit the existing packaging, signing and push path. Published
chart count goes from **12** to roughly **22**.

### Chart ownership is classified, not inherited from the pipeline

Revision 2 placed those charts in the engine because the engine already has a pipeline. That reasoning
was rejected, correctly: **publishing convenience does not establish ownership.** `network-policies`,
`cluster-issuer`, `cloudflared` and the `openbao-*` charts sit closer to topology than to any capability
of the engine.

Each gets a classification before slice 4a: capability implemented, who answers for its behaviour, who
publishes, who approves changes, which consumers exist. Publication may still land in the engine's
pipeline; that becomes a mechanism decision downstream of ownership rather than a substitute for it.

The BOM also reaches past the engine's ~22 — charts produced by `loopzero-data-cells` and pinned
third-party charts are installed, so they are pinned.

### The distribution unit is a bundle, and it is the resolved set

Revision 2 said *"the destination never publishes"*. Too broad: charts pinned by digest do not make an
installation reproducible if Helmfiles, values, schemas, installer and lockfile arrive by cloning a
branch.

Corrected: **the destination publishes exactly one artifact — the platform bundle.** It never publishes
charts. The measured Helmfile constraint decides what the bundle contains, because the installer must
resolve locally anyway:

```
platform bundle — OCI artifact, versioned and signed
├── platform.lock          resolved components, by digest
├── charts/                the resolved .tgz set, verified at resolve time
├── compose/               helmfiles, consuming ./charts/ by path
├── values/
├── schemas/
├── installer/
└── provenance/            SBOM, signatures, attestations
```

**One constraint, three findings answered:**

- **Digest pinning becomes real** — resolution happens once, at bundle build time, where digest and
  cosign signature are both verified. Helmfile then sees local paths, which it accepts.
- **The distribution unit becomes immutable** — one signed OCI artifact, not a branch.
- **Air-gapped installation falls out for free** — a resolved bundle has no registry dependency at
  apply time. It moves from an open question to a property.

Trade-off, stated: the bundle carries chart bytes, so it is large and duplicates what the registry
already holds. We consider that the price of a reproducible, verifiable unit — but it is a real cost
and a reviewer may weigh it differently.

### Two artifacts, not one

Revision 2's lockfile mixed what the platform *supports* with what a customer *chose*, leaving it
ambiguous whether every combination produces a different lockfile or whether the file is a catalogue.
Split:

```yaml
# platform.lock — RELEASE MANIFEST. One per platform version.
# What is supported. Identical for every customer on this version.
platformVersion: 2026.9.0
components:
  loopzero-api:           { version: 4.7.2, digest: "sha256:…" }
  admission-policies: { version: 2.3.1, digest: "sha256:…" }
  # includes loopzero-data-cells charts and pinned third-party charts,
  # not only the ~22 the engine publishes
supportedProfiles:
  secrets: [openbao, external]
  build:   [included, external]     # pending C1
compatibility:
  kubernetes: ">=1.31 <1.34"
  upgradeFrom: ["2026.8.x"]         # enumerated, not "any"
  reversible: false                 # this release contains a one-way migration
```

```yaml
# deployment.yaml — DEPLOYMENT SPECIFICATION. One per installation.
# What this customer chose. Never signed by us; owned by them.
platformVersion: 2026.9.0
profile:
  secrets: external
  build:   included
endpoints:    { … }
storageClass: …
domains:      [ … ]
secretRefs:   { … }
```

Five properties the manifest must have:

1. **Digest, not tag, is the reference.** A tag can be moved; a digest cannot. Semantic versions remain
   human labels alongside it.
2. **The installer verifies the signature** — not merely the pipeline that produced it. A signature
   produced and never checked is a receipt, not a control.
3. **Reproducible after the fact.** Given a platform version, the artifact set is recoverable months
   later without the pipeline that built it.
4. **Upgrade sources enumerated.** `upgradeFrom` makes "which versions can a customer come from" a
   declared fact rather than an accident of testing.
5. **Profiles declarative.** The conditionality of the secrets composition is a value, not an `if`
   discovered at install time.

---

## 06 · Ownership (C2)

Moving files does not create a boundary. If the destination does not take on these responsibilities, it
becomes a new location for the same files rather than an architectural seam.

| Responsibility | loopzero | cluster-management | data-cells |
|---|---|---|---|
| Deployable components and their contracts | **Owns** | Consumes | Consumes |
| Chart schema, values contract, chart behaviour | **Owns** | — | — |
| Chart publication and signing | **Owns** | — | Owns its own |
| Bill of materials / platform version | — | **Owns** | — |
| Cross-chart compatibility | — | **Owns** | — |
| Install order and topology profiles | — | **Owns** | — |
| Upgrade, rollback, migration sequencing | Provides per-chart | **Owns end-to-end** | Owns data migrations |
| Supported Kubernetes versions | — | **Owns** | — |
| End-to-end platform tests | — | **Owns** | — |
| Self-host operator documentation | — | **Owns** | — |
| Tenant placement, cell lifecycle | — | **Owns** | — |

### The membership test, narrowed

Revision 1 proposed *"what defines and fills a cluster of the topology"*. Too broad — it absorbs almost
any infrastructure. Adopted instead, and it is better than the criterion it replaces:

> What belongs to the composition repository is everything that determines an installable and supported
> version of the platform, without implementing the internal capabilities of the components that
> compose it.

| Composition | Verdict |
|---|---|
| `cell` | Belongs. Unambiguous. |
| `core` | Belongs, if it is a requirement of the supported platform — which it is under any reading of C1. |
| `build` | Belongs **if it is part of the contracted self-host offering**. The question is commercial; how many planes consume it is the wrong test. |
| `secrets` | Belongs as an **optional profile**, not a mandatory cluster. |

This makes `build` downstream of C1 rather than settled.

### The alternative on the record

A dedicated repository — `loopzero-distribution` or `loopzero-platform` — is legitimate if cluster lifecycle and
product composition have different change cadences, teams or operational responsibilities. Our position
is that the destination already contains `compose/` and `onboarding/`, the two natures this work needs,
and separating them duplicates both. **But if C7 cannot be answered, that is evidence *for* the
dedicated repository, not against it.**

---

## 07 · Migration

### Precedent decisions — before slice 1, not after it

Revision 2 said nothing past slice 1 should start until C1 has an answer. The flaw: **slice 1 already
moves all four compositions.** If C1 excludes the build plane, or C2 chooses a dedicated repository,
that movement was made on a decision nobody had taken.

Corrected: **C1, C2, C7 and a minimum answer on O3 are precedent to slice 1 itself.** What may start
immediately is neutral preparation, useful under every outcome — the gate classification, the chart
ownership classification, the de-pathing inventory.

### Slices

| # | Slice | Behaviour change |
|---|---|---|
| 1 | Subtree born under its final name. `git mv` of Helmfiles, environments, values; Taskfile, Tiltfile, gates and tests repointed in the same commit. Boundary gate born in warn mode. | None |
| 2 | Kill the three duplicated pairs. | Chart origin |
| 3 | Kill the pure wrappers — upstream chart plus local values. | Chart origin |
| 4a | Classify ownership of the ~10 infrastructure charts; prepare for publication. | None |
| 4b | Publish them and prove equivalence — rendered output identical to the local-chart path. | None (published, not consumed) |
| 4c | Switch the composition to the resolved artifacts. | Chart origin |
| 4d | Delete the local copies. | None |
| 5 | Classify all 43 gates; move the composition ones with their inputs. | None |
| 6 | Boundary gate flips to deny and goes green. | None |
| 7 | The cut — extract, graft, repoint, delete. | Yes |

Movement and behaviour change never share a commit: when the dev loop breaks, bisection must
distinguish *the file moved* from *the chart now comes from somewhere else*.

### Reversibility, in three separate kinds

Revision 1 called slice 7b "irreversible". Git operations are ordinarily reversible; what is
irreversible is state.

| Kind | Reversible? | Mechanism |
|---|---|---|
| Repository — move, graft, deletion | Yes | Forward commit restoring the tree; history preserved on both sides |
| Publication — a version offered to customers | Partly | A digest cannot be unpublished from someone who pulled it. Recovery is a superseding version plus an advisory, never deletion |
| Installation — a real cluster upgraded | **Often not** | CRD schema changes, policy activation and database migrations are one-way |

### Freeze window

- **Authorisation:** the cut proceeds on the sponsor's explicit go, recorded — not on a green pipeline.
- **Maximum duration:** bounded; exceeding the bound aborts rather than extends.
- **Abort:** any composition change required during the window aborts 7a. Editing either copy is
  prohibited — that is the divergence the freeze exists to prevent.
- **Hotfix:** a production hotfix aborts the cut. Shipping a fix outranks the migration.
- **Concurrency:** both repositories' composition paths are protected for the duration.
- **Equivalence proof:** rendered-manifest comparison, pre-cut versus post-cut, byte-identical modulo
  declared differences — automated, not reviewed by eye.
- **Tags and releases** are carried and audited, not just commits.

### Precedent, and where it stops applying

Three prior extractions preserved history — the CLI (319 commits), shared contracts (48), the control
plane (2091), each verifiable by identical commit subjects under different SHAs. All three grafted into
**empty** repositories. This destination has 16 commits, a founding ADR and a 33-concept wiki, so the
graft is a union of unrelated histories. Proposed: `filter-repo` in a throwaway clone, then a merge
allowing unrelated histories, via pull request — not a subtree add, which buries the provenance the
filter preserved.

---

## 08 · Quality gates (C4)

Subject decides the owner; **input decides whether the split is even possible.** Revision 1 reasoned
about composition gates following the tree, then left product-through-values gates in the engine reading
values that had moved — the same forbidden coupling, in the other direction.

| Gate class | Owner | Input |
|---|---|---|
| Helmfile and values integrity | Composition | Its own tree |
| Policy over the topology | Composition | Its own tree |
| Chart schema and behaviour | Engine | **Minimal fixtures owned by the engine** — reference values, not the real composition |
| Chart × composition compatibility | Composition pipeline | Published chart + candidate release manifest |
| Transversal conventions | Shared, versioned tooling | Whichever tree invokes it |
| End-to-end platform test | Composition | A resolved bundle |

The principle behind row three: a gate needing the real composition to validate the product is not a
unit gate — it is an integration test, and integration belongs to the composition pipeline. The engine
keeps fixtures encoding *the contract it promises*, not *the configuration someone chose*.

**"Approximately eight gates travel" is withdrawn as a precondition.** An estimate cannot stand in for a
classification. The precondition for slice 6 is a reviewable artifact listing **all 43** gates with:
current subject, new owner, new input, and whether it survives the split at all. Some will not — a gate
whose only possible input is the other repository's tree must be redesigned or deleted, and finding
those is the point of classifying rather than estimating.

---

## 09 · Operational acceptance (C6)

`task setup` green is necessary and insufficient. A platform version is offered to a customer only after
this matrix passes against a representative environment.

| Scenario | Acceptance criterion |
|---|---|
| Clean install — **per profile** | Matrixed, not single-path: build internal vs external · OpenBao vs external secret store · one cell vs several · supported Kubernetes versions and distributions · supported CNI, CSI, ingress, storage classes · connected vs air-gapped. Each combination converges and completes a deploy end to end. |
| Upgrade N → N+1 | From every version in `upgradeFrom`; no data loss; availability against a **declared SLO with a permitted interruption budget** — not the unmeasurable "workloads stay served". |
| Rollback N+1 → N (reversible release) | Executed *after* CRD, policy and schema changes have landed. Required only where `compatibility.reversible` is true. |
| Forward recovery (one-way release) | Where a migration cannot be reversed: expand/contract applied, backward compatibility held for a declared window, backup validated, roll-forward tested, and the release **marked non-reversible in the manifest**. |
| Backup and restore | Restore into a fresh cluster; declared RTO met and measured, not asserted. |
| Registry token rotation | Rotation without downtime; a revoked token fails closed with an actionable message. |
| Registry unavailable | A running installation keeps serving; the failure surfaces as a diagnosable state. |
| Secret backend change | OpenBao ↔ external store without reinstalling the platform. |
| Support diagnostics | A single documented command collects the evidence support would ask for. |

---

## 10 · Installer security model

Signed artifacts and revocable tokens already ship. What a distribution security model additionally
requires — none of it decided:

- **Verification is mandatory at install**, not optional and not merely produced upstream. An
  unverifiable artifact fails closed.
- **Digest pinning end to end** — the manifest references digests; the installer refuses a tag-only
  reference.
- **Credential lifecycle** — issuance, storage, rotation, revocation of the customer's registry token,
  with revocation observable.
- **Least privilege** — the RBAC the installer requires, stated and minimised. It is the most privileged
  thing a customer runs on our behalf.
- **Secrets in values** — the existing `existingSecret` floor extended to a rule the composition
  enforces rather than documents.
- **Root of trust and key rotation** — which key signs a platform version, how it rotates, how a
  customer learns the new one.
- **Key compromise response** — revocation path and customer notification, decided before it is needed.
- **Detached signatures** via OCI referrers. A signature must not sit self-referentially inside the
  content it signs.
- **Registry retention** — garbage collection must not delete a digest a supported version still pins.
- **Upstream mirroring** — third-party charts mirrored into our registry, so an upstream deletion cannot
  break a reproducible install.
- **SBOM at both levels** — aggregate for the platform version *and* per component, since a CVE lands on
  a component.
- **The bundle itself is signed**, not only the charts and manifest inside it.
- **Privileged hooks** — what a chart may execute during install, and what the composition refuses.
- **Air-gapped installation** — now a property of the resolved bundle rather than an open question.

---

## 11 · Governance and capacity (C7)

The destination has **zero lines of production code**, all decisions in `PROPOSED` state, and already
owes a transactional service that blocks the data-plane repository entirely. This proposal adds
ownership of the installable product on top of a commitment it has not yet delivered.

A boundary can be conceptually right and operationally unviable. Without answers below, that is the
likely outcome:

- Owning team and approvers.
- Release policy — cadence, versioning scheme, deprecation window.
- Pipeline SLO — a broken composition pipeline blocks every customer install.
- Incident support — who is called when a self-host installation fails.
- Reserved capacity, and explicit **precedence** between the transactional service and self-host. That
  inversion has been decided in favour of self-host; it needs recording rather than implying.

---

## 12 · Open decisions and risks

| # | Open decision | Why it matters |
|---|---|---|
| O1 | Contractual scope of self-host | Read literally, 101 releases across four or five clusters; read as "run my apps on my Kubernetes", ~13 charts on one. A precedent decision, not a risk — it changes sizing, networking, identity, DNS, storage, backup, observability, Kubernetes requirements and duration. |
| O2 | Final ownership of the composition | The destination, or a dedicated distribution repository. Hinges on C7. |
| O3 | Bundle format, channel and signing chain | § 05 gives the shape; format, generator, storage and chain are undecided. Minimum answer is precedent to slice 1. |
| O4 | Cross-repository gate execution | § 08 gives a taxonomy; the mechanism — triggered workflows, published fixtures, a compatibility suite — is undecided. |

| # | Risk | Note |
|---|---|---|
| R1 | SaaS-only assumptions travel with the files | k3d in the environments, a per-environment database split, an embedded identity provider, a CDN on the TLS and domain path, in-cluster registry and Git, a legacy IaC layer. Moving files does not remove these. |
| R2 | The governing rules are unreadable from this repository | Of eleven contracts the onboarding document calls binding, four resolve nowhere and six exist only inside sibling repositories as locally installed plugins — including the one governing 148 shell scripts and the one governing every live validation. |

### Evidence about the record itself

Measuring for this document produced two defects that were not code. The published admission-policy
chart ships a fail-closed policy without the exemption its bootstrap twin carries — latent, since the
policy defaults off, but unrecoverable once enabled, because the policy matches the very update that
would repair the resource. And the ratified ADR deciding the secrets topology says the opposite of what
the repository runs, with a second topology decision leaning on that practice through a citation to a
file that does not exist.

Both are filed; one is fixed. They calibrate how much weight a citation carries here: several did not
survive checking. **Where this document cites a decision, treat the citation as the claim.**

---

## 13 · Evidence

| Claim | How measured | Result |
|---|---|---|
| 101 releases | `grep -cE '^\s+- name:' infra/helmfile/*.gotmpl` | 55 + 28 + 10 + 8 |
| 75 relative references | `grep -c '\.\./helm' infra/helmfile/*.gotmpl` | 40 + 21 + 9 + 5 |
| 20 `chart:` vs 55 `values:` | field split of the same matches | 20 / 55 |
| 43 of 150 gates | `grep -rl -e 'infra/helm' -e helmfile cmd/loopzero-ops/internal/quality/*/check.go` | 43 / 150 |
| Charts vs values-only under `infra/helm/` | presence of `Chart.yaml` | 24 / 19 |
| Only `charts/` is published | `release-artifacts.yml` iterates `charts/*/` | 12 dirs |
| No platform version artifact | search for `platformVersion`; no helmfile lockfile | 0 hits |
| Helm accepts digest, Helmfile does not | live pull + `helmfile template` against a public registry | see § 01 |
| Prior extractions preserved history | identical commit subject, different SHA, both repositories | 3 of 3 |
| Secrets composition gated off | `grep 'openbaoEnabled'` across helmfile and environments | 4 gates · 1 of 8 envs |
| Data plane does not use the build plane | full-text search of that repository | 1 generic hit |
| Zero submodules in the ecosystem | `find -name .gitmodules` across 21 repositories | 0 |

### Rejected alternatives

| Option | Why rejected |
|---|---|
| Git submodule of the destination inside the engine | Inverts the dependency graph — the destination sits above the engine and consumes its charts, so a submodule closes a cycle. Central case of the rule that repository already states. |
| Build the composition fresh in the destination | The material exists in the engine. Rewriting loses history and guarantees divergence. |
| Strangler — both copies live for a period | Duplicating 101 releases duplicates knowledge. This repository has the scar: a mirrored document with an "edit one, copy the diff" rule; the mirror is gone and the rule died with it. |
| Gates in the engine reading the extracted tree | Requires the destination checked out during validation. The submodule returning through the back door. |
| Move only the cell composition, per the closed list as written | The installer is then assembled from two repositories — the problem the extraction exists to solve, one level down. |
| Infrastructure charts travel into the subtree *(revision 1)* | Withdrawn. Contradicts resolved-artifact consumption and makes the destination a chart publisher, which its own ruler forbids. |
| Composition consumes charts directly by digest *(revision 2)* | Withdrawn. Measured: Helmfile rejects digest references. Superseded by the resolved bundle. |
| Amend the ratified secrets ADR in place | Rewriting a ratified decision to match the present destroys the evidence of what was decided then. Superseded instead. |

---

*Revision 3, incorporating two rounds of external architecture review. Measurements against
`26ea7a079` on branch `workspace`. The nine settled decisions and their alternatives are in
`docs/wiki/grills/cluster-management-extraction-grill.md`; two diagrams accompany this document under
`docs/architecture/`.*
