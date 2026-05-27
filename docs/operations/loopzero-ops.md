# loopzero-ops — invocation convention

`loopzero-ops` is the Go binary (`cmd/loopzero-ops`, a standalone Go module) that hosts
the migrated quality gates and operational subcommands. It is the target of the
incremental bash→Go migration (plan `docs/plans/completed/2026-05-23-incremental-bash-to-go-migration-plan.md`).

## Why a single canonical invocation (ADR D6)

The abandoned `develop-bkp` big-bang scattered the build-on-demand idiom

```bash
(cd cmd/loopzero-ops && go build -o /tmp/loopzero-ops .) && /tmp/loopzero-ops <gate>
```

across ~15 call sites — fragile (rebuilds every invocation), DRY-violating, and
untestable. D6 mandates **one** build path.

## Canonical invocations

### Local / Taskfile

```bash
task ops:build            # builds bin/loopzero-ops (gitignored)
./bin/loopzero-ops quality <gate> [flags]
```

`task ops:build` is the single source of truth for producing the binary. Other
Taskfile targets that need a gate depend on it and call `bin/loopzero-ops`.

### CI workflows

For one-shot CI use where caching a binary is not worth it, prefer `go run`:

```bash
go run ./cmd/loopzero-ops quality <gate> [flags]
```

This is already the established convention for migrated gates (e.g.
`cd pkg && go run ./statemachine/cmd/gen-dots`). For gates that run in a
dedicated workflow, the `actions/setup-go` + `go build -o /tmp/loopzero-ops`
single-step pattern (see `no-new-bash-gate.yml`) is acceptable — the key
constraint is ONE build step per job, never duplicated.

## Anti-patterns (forbidden — D6)

- `(cd cmd/loopzero-ops && go build -o /tmp/loopzero-ops .)` repeated inside multiple
  steps/targets. Build once.
- Assuming a pre-built `/tmp/loopzero-ops` exists without building it.

## Available gates

Run `go run ./cmd/loopzero-ops quality --help` for the current list. Gates relevant
to the migration backstop:

- `no-new-bash` — fail if a PR adds a `.sh` outside the allowlist (D3).
- `dangling-script-refs` — fail if a deleted `.sh` is still referenced by a
  live caller (T0.1 backstop). Requires `fetch-depth: 0`.
