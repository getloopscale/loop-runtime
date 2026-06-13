# Steady-state baseline — pre M2 runtime envs per-project refactor

**Captured:** 2026-06-07
**Context:** T0.1 of plan `2026-06-07-runtime-envs-per-project-m2` v1.2.
**Purpose:** Bisect anchor BEFORE the runtime environment refactor that
aligns `AddEnvironment` signature with the canonical 3-segment namespace
`loopzero-<tenant>-<project>-<env>` (post J-23 + preview-as-category chain).

Per `.claude/rules/steady-state-baseline.md` — 4 layers mandatory.

---

## Layer 1 — Code state

```
git HEAD        : 01e46f28c33b3f31d797975f3dde1902c87abc39
branch          : worktree-glowing-growing-babbage
ahead of develop: 210 commits
working tree    : 3 untracked dirs (M2 SEPA scaffolding pre-commit)
                  - .claude/agents/implement-2026-06-07-runtime-envs-per-project-m2-2026-06-07/
                  - .claude/knowledge-base/implementations/
                  - .claude/skills/implement-2026-06-07-runtime-envs-per-project-m2-sepa-knowledge/
```

Last 5 commits leading into T0.1 baseline:

```
01e46f28 fix(implement): restore 6 implement-skill artifacts + code-quality stub
20301cd5 fix(plan-confidence): restore 7 missing scripts + 2 bugs + M2 placeholders
30d08e22 plan(m2): v1.2 — 7 task TDD shapes formalized (Path B per skill gate)
a80f0a9b docs(roadmap): add canonical ROADMAP.md for auto-plan skill compat
aa895b27 fix(migrations): renumber memory 000027-000029 → 000029-000031
```

Recent chain on the same surface area (preview-as-category + J-23 — the
two refactors that M2 builds on top of):

```
49381efe feat(preview): per-project slot model — migration 000028 (B-now)
371d0bb2 feat(preview): LIVE dogfood scenarios F+G + CHANGELOG phase 8
5154fb5f feat(preview): API contract evolution + dashboard refactor + tests phase 5-7
5ccc2b17 feat(preview): single-slot substitute + cleanup chains phase 3-4
8d148eda feat(preview): migration 000027 + single-slot model phase 1-2
b21e9446 fix(j23): prune orphan preview ArgoCD Apps (#71)
e682e4d4 fix(j23): DeletePreviewCR by label selector (CR name was always wrong)
a9d6c431 fix(j23): loopzero-api RBAC for previewenvironments + tenant watcher
```

---

## Layer 2 — Runtime state

### Clusters
4 k3d clusters, `loopzero-mgmt` is current context:
- k3d-loopzero-mgmt (control plane)
- k3d-loopzero-build (build pipeline)
- k3d-loopzero-workload (tenant workloads)
- k3d-loopzero-runtime-2 (cell-1)

### loopzero-mgmt pods (control plane operators + datastores)

```
app-operator-8694b6db66-77lxn       1/1 Running 179m
gitea-6fc9696d4c-gjfpk              1/1 Running 29h
harbor-* (8 pods)                   all Running
minio-5759b486cc-9f4tb              1/1 Running 30h
opencost-577ddc7764-qkzsz           2/2 Running 30h
platform-operator-cf7df94dd-jxztc   1/1 Running 7h57m
postgresql-0                        2/2 Running 30h
preview-operator-5c88fd4bdb-9fmb5   1/1 Running 179m
tenant-operator-578756d498-d9cnk    1/1 Running 179m
loopzero-api-6779499548-{22m4m,4zlzd}   2/2 Running 49m
loopzero-cloud-69d4999844-snlqb         1/1 Running 5h24m
```

### Tenant + Environment CRs (mgmt cluster)

```
Tenant        tn-08fa2ecedc3c7e77                        Active  (sponsor)
Tenant        tn-e2etest                                 Active  (bootstrap user)
Environment   tn-08fa2ecedc3c7e77-dev                    Active   PROJECT=<empty>
Environment   tn-08fa2ecedc3c7e77-production             Active   PROJECT=<empty>
Environment   tn-e2etest-j10-burst-j10-test-...-1        <none>   PROJECT=<empty>
Environment   tn-e2etest-production                      Active   PROJECT=<empty>
```

**Key finding — confirms M2 gap**: ALL Environment CRs have empty
`spec.environments[].project` field. `AddEnvironment(tenantID, name)`
on the Tenant CR Manager never received a project, so it never wrote
the field. Namespace inference fell back to 2-segment legacy.

### PreviewEnvironment CRs

Currently 0. Clean state post J-23 + preview-as-category cleanup.

### ArgoCD Apps (mgmt cluster, namespace `argocd`)

```
test-add-env-idem-production     Unknown   Healthy
test-add-env-production          Unknown   Healthy
test-cr-idempotent-production    Unknown   Healthy
test-cr-update-plan-production   Unknown   Healthy
test-get-env-detail-production   Unknown   Healthy
test-list-env-production         Unknown   Healthy
test-rm-env-idem-production      Unknown   Healthy
test-rm-env-last-production      Unknown   Healthy
test-rm-env-production           Unknown   Healthy
tn-08fa2ecedc3c7e77-dev          Unknown   Healthy
tn-08fa2ecedc3c7e77-production   Synced    Healthy
tn-e2etest-production            Unknown   Healthy
```

The 9 `test-*` Apps are e2e fixtures (residual from test runs);
`Unknown` sync = expected (repos likely garbage-collected).
Only `tn-08fa2ecedc3c7e77-production` is fully Synced — that's
sponsor's currently-running production.

### Workload cluster namespaces — **the canonical evidence**

```
loopzero-tn-08fa2ecedc3c7e77-dev                Active 140m  legacy 2-segment
loopzero-tn-08fa2ecedc3c7e77-production         Active 8h    legacy 2-segment
loopzero-tn-08fa2ecedc3c7e77-teste-production   Active 7h43m canonical 3-segment, project=teste
loopzero-tn-e2etest-production                  Active 8h    legacy 2-segment
```

**Critical baseline observation**: the workload cluster has BOTH
legacy 2-segment and canonical 3-segment namespaces SIMULTANEOUSLY
on the same tenant. The 3-segment `loopzero-tn-08fa2ecedc3c7e77-teste-production`
exists because the preview-canonical chain (J-23) and the
preview-as-category lifecycle DO carry project through their code path.
The 2-segment ones come from the legacy `AddEnvironment(tenantID, name)`
path that the M2 plan refactors. Post-M2, only NEW envs adopt
3-segment; existing legacy is preserved per ADR D2 (backward compat).

---

## Layer 3 — Config state

### Schema migrations applied

```
version | dirty
--------|------
     28 | f         preview_slot_per_project (most recent)
     10 | f         baseline
```

Migrations on disk up to 000031:

```
000024_loopzero_api_keys.up.sql
000025_memory_webhooks.up.sql
000026_memory_exports.up.sql
000027_preview_as_category.up.sql       (post-J-23 single-slot preview)
000028_preview_slot_per_project.up.sql  (B-now per-project slot table)
000029_memory_webhooks_add_secret.up.sql       (renumbered from 000027)
000030_memory_webhook_deliveries.up.sql        (renumbered from 000028)
000031_loopzero_api_keys_rotated_from.up.sql       (renumbered from 000029)
```

**M2 plan migration (T7.1)** MUST take next free number = **000032**
(plan body still cites 000029 — UPDATE during T7.1).

### environment_targets table state

```
tenant_id            | environment | kind
---------------------|-------------|----------
tn-08fa2ecedc3c7e77  | production  | permanent
```

Only 1 row LIVE — sponsor's tenant has a single `production`
permanent env. No `project_id` populated on this row even though
the column EXISTS (post-J-23/preview chain). M2 plan adds the
constraint that future writes MUST include project.

### environment_targets schema

```
tenant_id          text NOT NULL
project_id         text NOT NULL   <-- exists; legacy rows have ''
environment        text NOT NULL
desired_release_id text NOT NULL
active_release_id  text
updated_by         text NOT NULL
correlation_id     text NOT NULL
updated_at         timestamptz NOT NULL DEFAULT now()
kind               text NOT NULL DEFAULT 'permanent'
```

### preview_slot table (post-J-23, B-now model)

```
tenant_id         text NOT NULL
project_id        text NOT NULL
active_pr_number  integer
active_branch     text
active_release_id text
url               text
updated_at        timestamptz NOT NULL DEFAULT now()
```

Currently empty (no active previews).

### plan_limits current schema (M2 will refactor caps)

```
plan       | metric                 | hard_limit
-----------|------------------------|-----------
starter    | environments           | 1
starter    | projects               | 1
pro        | environments           | 3
pro        | projects               | 3
team       | environments           | 10
team       | projects               | 10
enterprise | environments           | NULL (unlimited)
enterprise | projects               | NULL (unlimited)
```

**M2 plan ADR D4 target (caps interpretation per (project, env) tuple)**:

```
starter    : 1 project  × 1 env-per-project  =   1 env total
pro        : 10 projects × 5 envs-per-project = 50 envs aggregate
team       : 10 projects × 20 envs-per-project = 200 envs aggregate
enterprise : unlimited × unlimited
```

T7.1 SQL migration semantic: `environments` row becomes **per-project
cap**, not aggregate cap. T7.2 enforcement updates aggregate count
to enforce per-project tuple instead of flat scan.

### Tenants (sponsor + bootstrap)

```
id                  | slug       | cell_id      | created
--------------------|------------|--------------|--------
tn-08fa2ecedc3c7e77 | admin      | cell-default | 30h ago
tn-e2etest          | tn-e2etest | cell-default | 30h ago
```

### Projects per tenant

```
tenant_id            | name
---------------------|------
tn-08fa2ecedc3c7e77  | teste     (3-segment NS observed in workload)
tn-08fa2ecedc3c7e77  | web
```

Sponsor has 2 projects — bootstrap user has 0 (per L4.3 probe).

---

## Layer 4 — LIVE functional probes (the SLO floor)

### Probe 1 — API health
```
GET /api/v1/health      → HTTP 200
GET /api/v1/health/deep → HTTP 200
GET /healthz            → HTTP 404 (intentionally absent)
GET /health             → HTTP 404 (intentionally absent)
```

Canonical health route is `/api/v1/health*`. Other paths are 404 by
design (no accidental health surface).

### Probe 2 — GET /api/v1/environments (current shape, bootstrap JWT)

```json
{
  "environments": [{"name": "production", "status": "active"}],
  "permanent": ["production"],
  "previews": []
}
```

**Current shape (post-preview-refactor Phase 5)**:
- Top-level `environments[]` — flat list (legacy compatibility)
- `permanent[]` — names of permanent envs
- `previews[]` — preview slots (empty here)

**Post-M2 target shape (ADR D6 — dashboard groups by project)**:
each entry of `environments[]` MUST carry `project` field. UI groups
client-side. Existing legacy entries (no project) render under a
"legacy" group OR alongside the project picker per T5.2.

### Probe 3 — GET /api/v1/projects (bootstrap JWT)

```json
{"projects": []}
```

Bootstrap user (`tn-e2etest`) has 0 projects. This will drive the
empty-state EC-2 scenario in T5.1 (CreateEnvironmentSheet must show
`<Alert>` + `<Button>` CTA when project list is empty, not crash).

### Probe 4 — sponsor tenant has the 3-segment proof

The workload-cluster namespace `loopzero-tn-08fa2ecedc3c7e77-teste-production`
proves the canonical 3-segment naming WORKS today for paths that
already carry project (preview canonical chain). M2 unifies the
runtime env creation path to also carry project, eliminating the
2-segment legacy fallback for new envs.

---

## Cross-link to plan

- Plan: `.claude/knowledge-base/plans/2026-06-07-runtime-envs-per-project-m2-plan.md` v1.2
- Implementation contract: `.claude/knowledge-base/implementations/2026-06-07-runtime-envs-per-project-m2-implementation.md`
- ROADMAP milestone: `M2 — Environments & Domains`
- Anti-pattern guarded against: drift between legacy 2-segment and canonical 3-segment namespaces accumulating cross-project naming collisions when `production` exists in `web` and `teste` simultaneously (EC-8 of plan).

## Rollback target

If M2 implementation introduces a regression that requires reverting
to pre-refactor state, this baseline is the bisect anchor. Reset to
commit `01e46f28` and reapply the LIVE state captured here.

## Operator next actions (none for T0.1)

T0.1 is fully engineering-side. The next operator gate is T0.2
("Operator approval caps + grandfather strategy") which requires
sponsor sign-off on the Free=1 / Pro=5 / Team=20 per-(project, env)
cap interpretation and the grandfather rule for tenants currently
above the new caps.
