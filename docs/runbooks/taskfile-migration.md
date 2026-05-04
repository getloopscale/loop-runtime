# Taskfile Migration Runbook

## Status

- **Plan:** `docs/plans/infra-sota-hardening-plan.md` T1.3
- **Meeting:** MEET-2026-05-03-062 (MEET-A)
- **Foundation prepared (iter 1):** 6 stub files in `taskfiles/`, interface check script, this runbook.
- **Pending:** mechanical migration of 2348 lines from `Taskfile.yml` to `taskfiles/*.yml`.

## Why this is multi-iteration work

The `Taskfile.yml` is 2348 lines including embedded shell scripts, jinja-templated commands, and YAML anchors. A naive sed-based split is high-risk because:

1. **Inter-task dependencies.** `task setup` calls `task clusters:create` which calls `task registry:create`. The migration must preserve resolution.
2. **Vars cascade.** Top-level `vars:` block (PG_PORT, contexts, etc.) is referenced by all tasks. Includes must inherit vars correctly.
3. **Embedded shell scripts.** Many tasks have multi-line shell heredocs. Splitting must preserve indentation and quoting.
4. **CI dependencies.** Workflows reference `task lint`, `task test`, `task validate:manifests`. These MUST keep resolving.

## Migration phases

### Phase 1: Foundation (✅ DONE iter 1)

- [x] Create `taskfiles/` directory.
- [x] Create 6 stub files: `k3d.yml`, `helm.yml`, `test.yml`, `infra.yml`, `db.yml`, `dev.yml`.
- [x] Write `scripts/check-taskfile-interface.sh` (gate).
- [x] Document migration plan (this file).

### Phase 2: Add includes block (PENDING)

```yaml
# Top of Taskfile.yml, before vars:
includes:
  k3d:   ./taskfiles/k3d.yml
  helm:  ./taskfiles/helm.yml
  test:  ./taskfiles/test.yml
  infra: ./taskfiles/infra.yml
  db:    ./taskfiles/db.yml
  dev:   ./taskfiles/dev.yml
```

After this commit:

```bash
bash scripts/check-taskfile-interface.sh
# Expected: GREEN — all public commands still resolve (they're in Taskfile.yml).
```

### Phase 3: Migrate one domain at a time (multi-PR)

Recommended order (smallest blast radius first):

1. **`db:*` tasks** — isolated, well-defined boundary.
2. **`registry:*` + `clusters:*` → `taskfiles/k3d.yml`** — small, stable.
3. **`infra:install:*` + `cloud:*` + `tokens:*` + `networking:*` → `taskfiles/infra.yml`** — large but cohesive.
4. **`helm:*` + `crds:*` + `dashboards:*` → `taskfiles/helm.yml`** — large.
5. **`test:*` + `parity:*` + `validate:matrix*` + `test:report:*` → `taskfiles/test.yml`** — many tasks.
6. **`dev:up` + `doctor` + `status` + `logs:*` + `cli:*` + `images:*` + `setup` + `clean*` → `taskfiles/dev.yml`** — final cleanup.

After EACH step:

```bash
# Validate interface preserved.
bash scripts/check-taskfile-interface.sh

# Validate task resolution.
task --list-all | wc -l   # count must be ≥ pre-migration count

# Validate CI commands.
task lint
task test
task validate:manifests
```

### Phase 4: Commit-by-commit

One PR per domain. Each PR:

- Removes target tasks from `Taskfile.yml`.
- Adds them to corresponding `taskfiles/<domain>.yml`.
- Preserves vars references (Task v3 inherits from root automatically).
- Preserves dependencies (cross-namespace `task: ns:name` works).
- Updates `scripts/check-taskfile-interface.sh` if a public command's namespace changes.

### Phase 5: Final cleanup

- `Taskfile.yml` reduces to ≤ 500 lines (orchestrator only).
- `taskfiles/*.yml` total ≤ 2400 lines (all domain tasks).
- Public command resolution unchanged.

## Risk mitigations

1. **CI gate.** `scripts/check-taskfile-interface.sh` runs in `infra-gates.yml` (or to be added to `ci.yml`). Any PR that breaks the contract fails CI.
2. **Per-domain PRs.** Don't bundle multiple domain migrations — bisect-friendly.
3. **Dry-run first.** `task --dry-run <command>` before merging.
4. **Rollback.** Each PR is reversible because we never deleted, just moved.

## Validation checklist (per PR)

- [ ] `bash scripts/check-taskfile-interface.sh` GREEN.
- [ ] `task --list-all | wc -l` ≥ pre-migration count.
- [ ] `task lint` GREEN.
- [ ] `task test:unit` GREEN.
- [ ] `task validate:manifests` GREEN.
- [ ] `task <migrated:command>` GREEN for every migrated task.
- [ ] CI workflow consumers still resolve.

## Why we didn't auto-migrate in iter 1

Auto-migrating 2348 lines via sed risks:

- YAML indentation errors in multi-line scripts.
- Vars resolution breakage if anchors moved across files.
- Cross-namespace dependency breakage.

These are caught only by RUNNING the tasks against k3d/CI, which is multi-minute per task. Running each task as part of a single iteration would exceed the loop's wall-clock budget.

The mechanical migration is best done as a focused PR session with k3d running locally.

## When to mark P1.3 complete

P1.3 is COMPLETE when:

- `Taskfile.yml` ≤ 500 lines.
- All 6 `taskfiles/*.yml` populated.
- `scripts/check-taskfile-interface.sh` GREEN.
- `task --list-all` resolves identically to pre-migration count.
- `task lint`, `task test`, `task validate:manifests` GREEN.
- CHANGELOG.md updated.

Until then, status is **PARTIAL: foundation prepared, mechanical migration pending**.
