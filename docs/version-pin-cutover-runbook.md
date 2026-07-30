# Version-Pin Cutover Runbook — loopzero-contracts

How to cut the consumers (`loopzero` engine + `loopzero-cloud`) over from the local
`replace => ../loopzero-contracts` directive to a **pinned published version** of
`github.com/getloopscale/loopzero-contracts`. Written at M5 of the monorepo decomposition.

> **Owner-gated.** This cutover is a release-grade change to the dependency graph.
> Do NOT run it autonomously — it requires the repo owner to publish a loopzero-contracts
> tag and to approve the consumer bumps. Until then, the local `replace` is the
> canonical wiring (it keeps every milestone GREEN without a published registry).

## Current state (M3/M4 → M5)

Both consumers depend on loopzero-contracts via the Go workspace convention:

```go
// go.mod (loopzero engine AND loopzero-cloud)
require github.com/getloopscale/loopzero-contracts v0.0.0
replace github.com/getloopscale/loopzero-contracts => ../loopzero-contracts
```

`v0.0.0` + `replace` means: "resolve this module from the sibling working tree, not
from a registry." This is deliberate — it lets the three repos evolve together while
the decomposition is in flight, with no publish step between edits. The acyclic
invariant is still enforced (the engine's `loopzero-ops quality acyclic-graph` gate scans
real imports, independent of how the module is resolved).

## When to cut over

Cut over once loopzero-contracts is **stable enough to version** — i.e., its public API
(`jwt`, `serviceauth`, `plan`) is not changing every milestone, and the team wants
consumers to pin a known-good revision rather than track the sibling tree.

Pre-conditions:

- loopzero-contracts builds + tests GREEN on its own (`cd loopzero-contracts && go build ./... && go test ./... -race`).
- The acyclic gate is GREEN on the real layout (`cd loopzero && task quality:gates`).
- The cross-repo contract runner is GREEN (`cd loopzero && task quality:contract:cross-repo`).
- Owner approval to publish a tag.

## Cutover steps

### 1. Publish a loopzero-contracts tag

```bash
cd loopzero-contracts
# Pick the next semver. First stable cut is typically v0.1.0.
git tag -a v0.1.0 -m "loopzero-contracts v0.1.0 — first pinned cut"
git push origin v0.1.0
```

The module path is already `github.com/getloopscale/loopzero-contracts`, so `go get` can
resolve the tag once it is pushed (the repo must be reachable by the consumers' Go
toolchain — public, or via `GOPRIVATE` + auth for a private repo).

### 2. Bump each consumer, one at a time

For **each** consumer repo (`loopzero` engine, then `loopzero-cloud`):

```bash
cd <consumer>
# Remove the replace directive and pin the published version.
go mod edit -dropreplace github.com/getloopscale/loopzero-contracts
go get github.com/getloopscale/loopzero-contracts@v0.1.0
go mod tidy
go build ./...
go test ./... -race
```

`go get @v0.1.0` rewrites the `require` line from `v0.0.0` to `v0.1.0`; `-dropreplace`
removes the sibling redirect. `go mod tidy` reconciles `go.sum`.

### 3. Verify the graph is still acyclic

```bash
cd loopzero && task quality:gates                 # includes loopzero-ops quality acyclic-graph
cd loopzero && task quality:contract:cross-repo   # both seams still GREEN
```

The acyclic gate result MUST be unchanged — pinning a version does not change which
packages import which. If the gate flips, STOP: a transitive dependency leaked in
with the published module and must be reverted.

### 4. Commit (on develop, never main)

```bash
# in each consumer
git add go.mod go.sum
git commit -m "build(deps): pin loopzero-contracts v0.1.0 (drop local replace)"
```

Add a `CHANGELOG.md` entry under `[Unreleased] § Changed` in each consumer.

## Rollback

If a consumer breaks after the bump, restore the local wiring without losing work:

```bash
cd <consumer>
go mod edit -replace github.com/getloopscale/loopzero-contracts=../loopzero-contracts
go mod tidy
```

This re-points at the sibling tree (the M3/M4 state). No `git reset --hard` needed —
`go mod edit` is a forward change.

## Why one consumer at a time

The engine and loopzero-cloud are independent modules. Bumping them separately means a
bad cut in one does not block the other, and the blast radius of a rollback is one
repo. Never bundle both bumps into a single change set.

## Cross-references

- Decomposition graph + acyclic invariant: `loopzero-contracts ◄── loopzero (engine) + loopzero-cloud + loopzero-cli`.
- Acyclic gate: `loopzero/cmd/loopzero-ops/internal/quality/acyclicgraph/` (`loopzero-ops quality acyclic-graph`).
- Cross-repo contract runner: `loopzero/docs/cross-repo-contract-matrix.md` (`task quality:contract:cross-repo`).
- Per-repo contracts: `loopzero-contracts/CLAUDE.md`, `loopzero-cloud/CLAUDE.md`, `loopzero-cli/CLAUDE.md`.
