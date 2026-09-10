# Gate classification for the composition extraction (C4)

- **Status:** Draft for review — the artifact C4 requires
- **Date:** 2026-09-10
- **Scope:** the 43 of 150 quality gates in `cmd/loopzero-ops/internal/quality/` that read `infra/helm` or `helmfile`
- **Supersedes:** the "approximately eight gates travel" estimate, withdrawn under review

> **How this was produced, and its limit.** Each gate was classified from its package doc-comment and
> the paths it reads — not from reading every `check.go` in full. Rows marked **`verify`** in the
> confidence column are the ones whose classification could flip on a closer read; they are named rather
> than averaged into the totals silently. This is a reviewable draft, not a finished contract.

## Taxonomy

| Class | Owner | Input |
|---|---|---|
| **A** — Helmfile and values integrity | Composition | Its own tree |
| **B** — Policy over the topology | Composition | Its own tree |
| **C** — Chart schema and behaviour | Engine | Minimal fixtures owned by the engine |
| **D** — Chart × composition compatibility | Composition pipeline | Published chart + candidate manifest |
| **E** — Transversal conventions | Shared, versioned tooling | Whichever tree invokes it |
| **F** — End-to-end platform test | Composition | A resolved bundle |
| **X** — Ceases to exist | — | — |

## Summary

| Class | Count | Destination |
|---|---|---|
| A — Helmfile/values integrity | 7 | **Travels** |
| B — Policy over topology | 8 | **Travels** |
| C — Chart schema and behaviour | 14 | **Stays** in the engine |
| E — Transversal conventions | 11 | **Stays** |
| X — Ceases to exist | 3 | Deleted by slice 2 |
| **Total** | **43** | |

**15 travel, 25 stay, 3 die.** The withdrawn estimate said "approximately eight" — it was low by roughly half, which is the reason an estimate could not stand in for this.

## X — Gates that cease to exist

These exist to police the duplicated chart pairs. Slice 2 kills the pairs, so the gates lose their subject.

| Gate | Why it dies |
|---|---|
| `chartpairs` | Maintains the registry of charts present in both trees under the same name. With no pairs, the registry is empty and the gate has nothing to assert. |
| `helmdrift` | Compares template-file sets between `infra/helm/` and `charts/`. One tree after slice 2. |
| `helmoverlapparity` | Enforces parity of version, resources and rendered securityContext across the pair. Same. |

**This must be sequenced deliberately, not discovered.** Deleting them before slice 2 removes the only
checks on a divergence that is still live — and one of those divergences is already filed (#356). They
are deleted **in** slice 2, in the same commit that removes the second copy.

## A — Helmfile and values integrity → travels

| Gate | What it asserts | Input after the split |
|---|---|---|
| `helmfilecontextparity` | The cluster a Helmfile targets is the one it declares | Own tree |
| `helmtopology` | Which chart belongs to which cluster's composition | Own tree |
| `devpublicenv` | Shape of `environments/dev-public.yaml` | Own tree |
| `esooverlay` | The ESO values overlay per environment | Own tree |
| `optionbeso` | ESO deployed through the composition, per the documented option | Own tree |
| `prodimagetags` | No mutable `:tag` in production manifests | Own tree |
| `prodimagedigests` | Production manifests reference `image@sha256:` | Own tree — **strengthened** by the resolved bundle, which makes digests the norm rather than the exception |

## B — Policy over the topology → travels

| Gate | What it asserts | Note |
|---|---|---|
| `openbaobootstrapincore` | OpenBao is never installed into the core cluster | The topology rule this repository just gave a home to (`2026-09-09-the-secrets-cluster-is-separate-and-conditional.md`) |
| `clusterimagepolicymode` | `ClusterImagePolicy.mode` may only be `enforce` under a recorded ADR | |
| `clusterrolenosecretsstar` | No ClusterRole grants `secrets *` | |
| `nolegacysatoken` | No legacy ServiceAccount token Secrets | |
| `noenvvaultsecrets` | No vault secrets passed through env vars | |
| `credentialssot` | Every Secret created by bootstrap is enumerated | `verify` — reads bootstrap scripts as well as the composition |
| `pgpdb` | PodDisruptionBudget on the platform Postgres | `verify` — Postgres leaves the self-host offering per C1, but stays in ours |
| `securitycontext` | `runAsNonRoot` and friends across `infra/helm/*` | `verify` — subject is the chart, but the sweep is over the composition tree; may be C after slice 4 |

## C — Chart schema and behaviour → stays in the engine

Input becomes **minimal fixtures owned by the engine** — reference values encoding the contract the
chart promises, not the configuration someone chose. A gate needing the real composition to validate
the product is an integration test, and integration belongs to the composition pipeline (class D).

| Gate | Subject |
|---|---|
| `billingsecrets` | A literal where a value reference belongs, in the cloud chart |
| `orykratoslifespan` | Session and flow lifespans in the Kratos values |
| `orykratospublicurls` | Public base URL, CORS, cookie domain, allowed return URLs |
| `loopzeroapiopenbao` | The `loopzero-api` chart's OpenBao wiring |
| `esochartsanity` | Sanity of the `loopzero-external-secrets` chart |
| `esocreationpolicy` | `creationPolicy` in that chart |
| `openbaoinitauth` | Per-cluster K8s auth mounts, per-binary roles |
| `lokiretentionschema` | The Loki retention stream schema |
| `lokivaultretention` | Retention of the vault audit stream |
| `vaultpromalerts` | The vault Prometheus alerts ship |
| `dashboardpromtool` | Dashboard JSON validity |
| `alertrunbooks` | Every alert carries `runbook_url` |
| `orphanalerts` | No alert staged with a `vector(0) > 1` placeholder |
| `webhookcanonicalpath` | The canonical GitHub webhook path |
| `chartdefaulttags` | No loopzero-owned chart defaults its image tag to a mutable value |
| `tenantcsppassthrough` | CSP middleware never attached to tenant routes |

*(16 listed; `chartdefaulttags` and `tenantcsppassthrough` are the two that could argue for class A —
counted in C, flagged here rather than hidden.)*

## E — Transversal conventions → stays

These are repository hygiene, not composition. Each repository runs its own copy, ideally from shared
versioned tooling rather than a fork.

| Gate | Subject |
|---|---|
| `nonewbash` | No new `.sh` file without allowlist justification |
| `shellhygiene` | Shell style and safety |
| `scriptsaudit` | Inventory of every `.sh` — path, LOC, callers |
| `trufflehogscan` | Secret scanning |
| `unhomedlogic` | Operational logic that has no home module |
| `danglingdata` | A deleted script still referenced by a live caller |
| `testskipabsentpath` | Tests skipping on an absent path |
| `advisorylock` | `pg_advisory_xact_lock` scope in the API |
| `scheduledimage` | Scheduled image references |
| `clusterimagepolicymode` | *(also listed in B — the policy half travels, the convention half stays; needs splitting or a single owner)* |

**A live problem this classification exposes:** four of these (`nonewbash`, `shellhygiene`,
`scriptsaudit`, and one more) read `.claude/rules/shell-scripts-allowlist.md` **by literal path**, and
that file does not exist in this repository. They are red today, before any extraction. Tracked in
#281; the split cannot fix them.

## What the composition repository inherits

15 gates travelling means `loopzero-cluster-management` acquires a Go quality-gate binary its design never
planned for — it has no `cmd/`, and the design anticipated `go-arch-lint`, not a gate suite. This is
one of the two amendments the T3 meeting must approve, and this table is its size.

## Open questions in this draft

1. **Three `verify` rows in class B** (`credentialssot`, `pgpdb`, `securitycontext`) read both trees today. Each needs a decision on which side owns it once the trees separate.
2. **Class D is empty.** No gate today tests chart × composition compatibility, because there was no seam to test. That is the gate class the split *creates* the need for, and it does not exist yet — arguably the most important row in this document.
3. **`clusterimagepolicymode` is double-counted** between B and E; it needs splitting or one owner.
4. **Shared tooling has no mechanism.** Class E says "shared, versioned tooling"; today it is one binary in one repository. Extracting it is a third repository's worth of work, or each side forks and drifts.
