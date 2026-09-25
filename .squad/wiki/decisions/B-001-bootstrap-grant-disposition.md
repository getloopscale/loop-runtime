# B-001 — disposition of the two bootstrap-only security grants

**Date:** 2026-09-15 · **Item:** B-001 · **Status:** recorded, not decided

## What this record is, and what it is not

`helm-drift` now compares what each side of a declared chart pair GRANTS, cell by
cell. On the `loopzero-api` pair it finds **174 divergent `(apiGroup, resource, verb)`
cells** of a 200-cell union, and every one of them is declared in
`infra/quality/helm-drift-registry.yaml` (in Go literals in
`cmd/loopzero-ops/internal/quality/helmdrift/rbac_approvals.go` until 2026-09-25).

Two of those declarations are **not approvals**. They carry `Status:
statusUndecided`, the gate prints them by name on every run, and this record
exists so that "the gate is green" can never be read as "somebody reviewed these".

**This item enforces the contract. It does not decide the grants.** The decision
belongs to **B-048**.

## The two cells

### `(core)/secrets/delete`

| | |
|---|---|
| bootstrap | `infra/helm/loopzero-api/templates/serviceaccount.yaml:35-37` — `apiGroups: [""]`, `resources: [namespaces, configmaps, secrets, services, pods, events]`, `verbs: [get, list, watch, create, update, patch, delete]`, with no enclosing conditional |
| published | `charts/loopzero-api/templates/clusterrole.yaml:40-43` — `secrets` is gated behind `.Values.rbac.legacyClusterWideSecrets`, and the verb list omits `delete` under **every** value of that flag |
| measured | bootstrap-only under **both** entries of `rbacValuesMatrix` — not an artefact of a flag state |

The published chart's own comment at `clusterrole.yaml:29-39` calls the
cluster-wide `secrets`/`configmaps` grant a *"catastrophic blast radius"* and says
the flag exists so the grant can be removed after the per-tenant Role rollout. The
bootstrap chart grants strictly more than that, including `delete`, unconditionally.

### `(core)/pods/exec/create`

| | |
|---|---|
| bootstrap | `infra/helm/loopzero-api/templates/serviceaccount.yaml:38-40` — `resources: [pods/log, pods/exec]`, `verbs: [get, list, create]`, cluster-wide |
| published | absent entirely; the published chart grants `services`, `pods` and `pods/log` for `get`, `list`, `watch` only |
| measured | bootstrap-only under **both** entries of the matrix |

`create` on `pods/exec` is the verb that opens a shell in any pod in the cluster.
Nothing measured in B-001 establishes a caller for it in `loopzero-api`.

## Why they are recorded rather than converged

Narrowing the bootstrap side changes the RBAC that Helmfile installs into the core
cluster. Nothing in B-001 measured what depends on the 149 bootstrap-only cells, and
removing a grant on the strength of "the other chart does not have it" is a change
made from the absence of evidence rather than from evidence.

Nor are they declared as intended. A gate asserting these two are fine would be
asserting a security judgement nobody made — which is the failure mode the whole
item is about, one level up.

So the third option: **record them, print them, and name who decides.** The gate
stays useful for the next divergence, which is what a permanently red gate would
not be, and green never claims more than "every divergence is recorded".

## What the gate says after this item

> RBAC is compared. 174 divergences are recorded. 2 are undecided.

It does **not** say the pair is in parity, and it does not say the grants are
appropriate.

## Follow-ups this record leaves open

1. **B-048** decides the two cells above: converge the bootstrap side, or declare
   them intended with a caller named. Either turns `statusUndecided` into
   `statusDeclared` or removes the entry.
2. **The pair sources are not unified.** `helm-drift` discovers pairs from disk
   (three today); `chart-pairs` reads `infra/quality/chart-pairs.txt` (two). The
   divergence is recorded in the `helmdrift` package doc and guarded by
   `TestThePairSourcesDivergeAndTheDivergenceIsRecorded`. It was NOT filed as a
   backlog item, because `BACKLOG.md` is gitignored here (`.gitignore:295`) and an
   item filed there reaches nobody who clones — so it is written where it travels.
3. **Wildcards are compared literally.** `loopzero.io */*` on the bootstrap side and
   the seven enumerated verbs on the published side read as eleven divergent cells
   although the first covers the second. Resolving a wildcard would need the API
   server's resource list, which a static gate cannot see; the cost is declared in
   `expandRules` and in the three affected registry entries.

## Cross-references

- Registry: `infra/quality/helm-drift-registry.yaml`, loaded by `cmd/loopzero-ops/internal/quality/helmdrift/registry.go`
- Comparison: `cmd/loopzero-ops/internal/quality/helmdrift/rbac.go`
- Item that decides the two cells: **B-048**
