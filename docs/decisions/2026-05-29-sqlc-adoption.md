# ADR — Adopt sqlc as canonical database access layer (replaces hand-rolled pgx)

**Date:** 2026-05-29
**Status:** Accepted (APPROVED WITH CONDITIONS — MEET-2026-05-29-001)
**Tier:** T3 (architecture, stack, critical flow)
**Authors:** Paulo (sponsor + operator) · Ralph (AI executor)
**Meeting:** MEET-2026-05-29-001 (12 specialists; both VETO holders APPROVE conditional)
**Coupled ADRs:** [`2026-05-29-schema-gen-sql-render.md`](./2026-05-29-schema-gen-sql-render.md) · [`2026-05-29-sqlc-migration-rollback.md`](./2026-05-29-sqlc-migration-rollback.md)

> **Path note (added 2026-07-30, body unchanged).** This ADR refers to quality gates under
> `pkg/quality/…`. That package was relocated to **`cmd/loopzero-ops/internal/quality/…`** at M9
> (ROADMAP steps 3+4) — the gate names are unchanged, only the import path moved. The body below is
> preserved as written on its date; do not rewrite it to match the present tree.


---

## 1. Context

### 1.1 The structural problem

Hand-rolled SQL in `api/internal/db/` admits a 3-list synchronization fragility that is **invisible to compilation, invisible to unit tests, and only catchable at runtime or via a defensive mirror test that runs at test time, not compile time**.

The three lists that must agree:

1. The **column list** in `INSERT INTO releases (a, b, c, ...) VALUES ($1, $2, $3, ...)`.
2. The **column list** in `SELECT a, b, c, ... FROM releases`.
3. The **field list** consumed by `rows.Scan(&r.A, &r.B, &r.C, ...)` (a.k.a. `scanReleaseFields`).

Drop any one column from any one of those three lists — independently — and the code still compiles, still passes most unit tests, and silently produces wrong data at runtime.

Inventory verified via filesystem read on 2026-05-29:

| Surface | Count | Notes |
|---|---:|---|
| Store files under `api/internal/db/` | 44 | mix of `_store.go`, transactional helpers, sub-files |
| Forward-only `.up.sql` migrations | 20 (numbered 000001..000020; 000003 retired) | golang-migrate canonical |
| Manual `.Scan(&fields)` sites (estimated baseline) | 26 | the source of M07-class drift |
| pgx callsites (estimated baseline) | 17 | direct `Query` / `QueryRow` / `Exec` usage |

(Counts above reflect what the meeting reviewed; the live numbers will move during the migration and are subject to a render-time recount in ADR-1 appendix M-1.)

### 1.2 The motivating concrete incident: M07 Rekor URL persistence

Commit `b3375ed0` (2026-05-28) fixed a 3-layer drift bug. Migration 000019 added the column `image_attestation_rekor_url`. The `Release` struct was updated. The `cosign_attest` enrichment writer was updated. All three were independently correct.

But `api/internal/db/release_store.go` — the **SQL layer** — had:

- 1 × `INSERT INTO releases` that did not list the new column.
- 13 × `SELECT … FROM releases` queries that did not project the new column.
- 1 × `scanReleaseFields` helper that did not scan the new column.

All three of those omissions were silent. The defensive `schema_mirror_test.go` (T2.2 of the maturity plan) was specifically built to catch this — and it did, at **test time**, not at compile time. The fix landed only after LIVE-pod evidence (release `rel-506b861c71594269`) showed the column being populated by the enrichment writer but immediately discarded on the next read.

This is not a one-off. It is a **bug class** intrinsic to hand-rolled column-list / scan-list duplication. See memory `reference_m07_rekor_url_3layer_bug_2026_05_28` for the full forensic record.

### 1.3 Why T3 (architecture / stack / critical flow)

This change replaces the canonical database access layer for the entire control-plane API. It modifies:

- **Stack:** new tool dependency (sqlc CLI) + new generated package + new CI gate.
- **Architecture:** how state is read and written across every business-logic boundary.
- **Critical flow:** every `withEnvironmentLock` + `pg_advisory_xact_lock` + `Transition()` invocation routes through `db/`.

Per `.claude/rules/pre-release-governance.md`, this MUST pass through the full T3 meeting flow with mandatory ADR. MEET-2026-05-29-001 satisfied that gate with 12 specialists, both VETO holders (Security & Blast Radius, Production Design) APPROVING conditional, and the operator (Paulo) ratifying.

### 1.4 Why sqlc (and why now)

- **Compile-time correctness.** sqlc generates the exact `INSERT` + `SELECT` + `Scan` triple from a single source of truth (`queries.sql` + `schema.gen.sql`). Drift is impossible — the three lists are derived from one declaration.
- **pgx-native.** sqlc dropped pgx/v4 in v1.25.0 (January 2024); v1.30.0 (September 2025) and v1.31.0 (April 2026) ratchet up pgx/v5 as the canonical 2026 path. LoopZero's existing pgx connection pool is preserved end-to-end.
- **No runtime reflection, no ORM tax, no N+1 by default.** The generated code looks exactly like the code a careful engineer would write by hand — except it cannot drift.
- **Greenfield in LoopZero.** CLAUDE.md Rule 12 grep precheck (executed 2026-05-29 against `api/`, `pkg/`, `infra/`) returned zero existing sqlc adoption. No silent prior commitment to reverse.
- **Pre-release.** Per `.claude/rules/pre-release-governance.md`, backward compatibility is not required and clean redesigns are preferred. sqlc is the clean redesign for this fragility class.

---

## 2. Decision

**Adopt sqlc v1.31.1+ (April 2026 release) with `sql_package: pgx/v5` as the canonical and ONLY database access layer in `api/internal/db/`. Big-bang migration; no incremental phasing; no backward-compatibility shims.**

### 2.1 What this means concretely

- The 44 hand-rolled `*_store.go` files are deleted as part of the migration.
- The generated sqlc package (working name `api/internal/db/sqlcgen/`) becomes the only typed access path.
- All `withEnvironmentLock` + `pg_advisory_xact_lock` + state-machine `Transition()` invariants are preserved via the `Queries.WithTx(tx)` pattern (sqlc's native transactional handoff).
- A small documented escape hatch (`api/internal/db/raw/`) is reserved for the ~5% of queries that sqlc cannot statically type (LATERAL joins beyond sqlc's planner, recursive CTEs, dynamic `WHERE` builders). The escape hatch is AST-gated per M-9 below to prevent it from becoming a slow leak back into hand-rolled patterns.
- `schema_mirror_test.go` (T2.2 of the maturity plan, 2026-05-26-roadmap) is deleted in a coordinated two-step process per M-10 below — its detection role is subsumed by sqlc compile-time guarantees.
- A new CI gate `pkg/quality/sqlcsync/` is registered in `task quality:gates` (counter moves from 10 → 11).

### 2.2 What this does NOT change

- **golang-migrate stays canonical.** Migrations remain forward-only `.up.sql` files. The schema source for sqlc is rendered FROM those migrations (see ADR-2), not authored separately.
- **pgx connection pool stays canonical.** sqlc's pgx/v5 emission consumes the same `*pgxpool.Pool` LoopZero already constructs.
- **`withEnvironmentLock` stays canonical.** Wrapping is unchanged; only the inner store calls swap to sqlc-generated functions.
- **`pkg/statemachine/` `Transition()` stays canonical.** No business logic moves into sqlc.
- **CLI / API public contracts are untouched.** No exit code, JSON field, or HTTP route changes (Rules 3, 4, 5 invariant).

### 2.3 Branch / merge model (operator-locked)

- One feature branch (`feat/sqlc-adoption`) off `develop`.
- **14-day hard cap on branch lifetime** (D3 of the meeting). Daily rebase against `develop` is required.
- If <5%/day measured progress for 2 consecutive days → Paulo invokes a ratification meeting to decide between (a) extend timeline, (b) phase remaining work, (c) abandon and the squash-merge per ADR-3.
- Merge to `develop` via squash-commit (clean reversion target — see ADR-3).
- The owner committee is Paulo (operator) + Ralph (AI executor, the same model that successfully completed the MEET-001 engineering closure Final Phase this same session). Sponsor checkpoint mechanism is Paulo's conscious self-check per M-15 daily diary.

---

## 3. Alternatives Considered

| Option | Rationale | Why rejected |
|---|---|---|
| **GORM** | Most popular Go ORM | Runtime reflection collides with Rule 2 (code is authoritative — generated reflection-decoded behavior is not greppable). Auto-migrate collides with Rule 7 (forward-only golang-migrate). N+1 by default. Reverses our "no ORM" stance from prior `2026-05-21-external-tooling-evaluations.md`. |
| **ent (Ariga)** | Graph model + code-gen, FAANG-grade | Would replace golang-migrate (ent owns migration). That's a T3 cascade we did not commit to in this meeting. Graph schema is overkill for LoopZero's mostly-relational shape. |
| **bun (uptrace)** | Active, pgx-aware, struct-first | Struct-first model is the same direction that produced the M07 bug (struct + writer + SQL still independent). Solves a different problem (ergonomics) than the one we have (drift). |
| **jet** | Type-safe DSL for queries | Replaces grep-of-textual-SQL with Go-DSL grep — losing one of the strongest debugging tools we have. Extra learning curve for marginal benefit. |
| **squirrel** | Query builder only | Doesn't solve schema↔struct drift. Solves a problem we don't have (string concatenation safety). |
| **Phased migration (one store at a time)** | Lower-risk per-PR | Operator rejected: creates the "implemented but not wired" pattern explicitly called out in CLAUDE.md's "Known Systemic Patterns" table. Two access layers coexisting double the surface area for bugs during the transition. |
| **Hand-rolled + extended `schema_mirror_test`** | No new tool dependency | Half-measure. Detection at test time, not compile time. The M07 bug was caught precisely this way — *after* it shipped to LIVE — proving the model is reactive not preventive. Rejected by Security & Blast Radius reviewer as failing fail-fast principle (`.claude/rules/loopzero-design.md` Error Philosophy). |
| **Defer until post-GA** | Lower disruption now | Rejected by sponsor: pre-release is precisely the window where backward compatibility is not required and the clean redesign is cheapest. Deferring locks in M07-class fragility for every store touched in the next 90 days. |

---

## 4. Trade-offs Accepted

| Trade-off | Why accepted | Mitigation |
|---|---|---|
| Multi-day RED feature branch | Big-bang is operationally cleaner than carrying two layers | 14-day cap (D3); daily rebase; <5%/day-for-2-days triggers ratification meeting; ADR-3 rollback playbook pre-authorized |
| ~5% of queries need raw escape hatch | sqlc's static planner cannot type all dynamic SQL | `api/internal/db/raw/` subpackage; AST gate (M-9) blocks raw access to sensitive tables (releases, builds, tenants, audit_log) without an explicit allowlist comment |
| 10+ store-test files require reshape | Tests currently assert against hand-rolled store APIs | Reshape matrix in Appendix A (M-1) catalogs each test; reshape happens within the same feature branch; net test count must not decrease |
| New CI gate `pkg/quality/sqlcsync/` | Increases CI surface (10 → 11 quality gates) | Gate is pure-logic Go (per `.claude/rules/shell-scripts-allowlist.md` D5 + Anti-Pattern #8); hermetic; <2s execution budget; budget delta measured in M-20 |
| `schema_mirror_test.go` becomes redundant | Compile-time correctness subsumes test-time detection | Two-step deletion per M-10: (step 1) sqlc adoption merged + 7d clean → (step 2) deletion PR. Step 2 is operator-witnessed, not automated. |
| New tool dependency (sqlc CLI) | Don't-reinvent-the-wheel (Rule 9) | Pinned version in CI + documented in `docs/operations/dev-loop.md`; published Helm-like via `go install` + version-pinned `task db:render-schema` |
| `schema.gen.sql` artifact lives in repo | Adds a second representation of schema | Generated, not hand-authored; CI gate (ADR-2) fails on drift; clearly labeled in headers as `// Code generated by golang-migrate replay against ephemeral PG 16. DO NOT EDIT.` |

---

## 5. Consequences

### 5.1 Positive

- **Bug class eliminated at compile time.** sqlc proves SELECT/INSERT/Scan match against a single declared schema. The M07 incident type cannot recur for any column managed by sqlc.
- **DRY at the knowledge level.** The 3-list duplication is collapsed to a single declared shape. Knowledge has one canonical representation, per `.claude/rules/design-principles.md` DRY guidance.
- **pgx-native, zero runtime overhead.** No reflection; generated code reads like careful hand-written pgx.
- **LoopZero invariants preserved.** `withEnvironmentLock` + `pg_advisory_xact_lock` + `Transition()` continue to work natively via `Queries.WithTx(tx)`.
- **Architectural alignment with KISS + Don't Reinvent (Rule 9).** Solves the actual problem with a tool the ecosystem has battle-tested.
- **Sponsor + principal-architect alignment.** First time sponsor explicitly endorses a tool-class shift since cosign-keyless decision.

### 5.2 Negative (accepted)

- **Multi-day RED feature branch** during migration. Operator absorbs this via D3 cap.
- **Escape hatch must be policed.** Without M-9 AST gate, `raw/` can slowly accrete back into hand-rolled territory.
- **CI execution-time budget grows.** Render-schema job (ADR-2) + sqlcsync gate add seconds; measured in M-20.
- **Developer onboarding adds one tool.** sqlc CLI install + `task db:render-schema` familiarization. Documented in `docs/operations/dev-loop.md`.
- **schema_mirror_test.go deletion is a two-step.** Cannot delete in the same PR as adoption (per M-10) — must wait for 7-day clean window.

### 5.3 Risks (explicit, with mitigations)

| Risk | Severity | Mitigation |
|---|---|---|
| Migration takes >14 days | High | D3 cap + ratification meeting if <5%/day for 2 consecutive days; ADR-3 rollback playbook |
| Hidden hand-rolled SQL discovered mid-migration | Medium | Initial grep audit in Appendix A; reshape budget includes 20% contingency |
| sqlc emits surprising code for a LoopZero query pattern | Medium | Pair-review per migrated store (M-2); escape to `raw/` is allowed with allowlist comment |
| `schema.gen.sql` render becomes flaky in CI | Medium | ADR-2 pins PG 16 + golang-migrate version + sqlc CLI version |
| Post-merge P0 in `db/` | High | ADR-3 rollback pre-authorized; 7-day observation window; rollback mechanism (see ADR-3) is atomic |
| Drift between `infra/scripts/production/run-migrations.sh` golang-migrate version and CI render version | Medium | Version pin in both places; CI gate fails on mismatch (ADR-2) |

---

## 6. Conflict Resolution (from MEET-2026-05-29-001)

The meeting record captured 6 conflicts, all resolved before APPROVE. Recording here for canonical traceability:

| # | Conflict | Resolution |
|---|---|---|
| C1 | Phased vs big-bang migration | Operator decision: big-bang. Phased rejected as creating "implemented but not wired" pattern. |
| C2 | Whether to keep `schema_mirror_test.go` indefinitely as belt-and-suspenders | Two-step deletion per M-10. Compile-time correctness subsumes the mirror; redundancy would create false confidence. |
| C3 | Whether the raw escape hatch should be forbidden or AST-gated | AST-gated. Forbidding it would push edge-case queries into the wrong patterns (e.g., string concatenation in services). |
| C4 | Whether to adopt pgx/v4 (current) or pgx/v5 (new) | pgx/v5. sqlc v1.25.0+ dropped pgx/v4. v5 is the canonical 2026 path per upstream cadence. |
| C5 | Sprint 9 timing deconflict (avoid customer-trial overlap) | IRRELEVANT — no customer trial is scheduled. If a trial is later scheduled, sponsor re-evaluates timing. Captured in ADR-2 cross-references. |
| C6 | Whether deletion of `schema_mirror_test.go` should be in the same PR | NO. Two-step per M-10. Same-PR deletion would compromise rollback atomicity (ADR-3). |

---

## 7. Validation Plan (V1–V11)

The migration is gated by 11 validation phases. ALL must be GREEN before merge to `develop`. The order is partial — V1..V3 must precede V4..V9; V10..V11 are post-merge.

| ID | Validation phase | Gate type | Owner |
|---|---|---|---|
| V1 | Grep audit complete: every `*_store.go` mapped to sqlc query OR escape hatch with allowlist comment | Manual + script | Ralph |
| V2 | `schema.gen.sql` renders reproducibly: byte-identical output across 3 fresh CI runs | CI (ADR-2) | CI |
| V3 | `pkg/quality/sqlcsync/` gate registered in `task quality:gates`; gate passes on the migrated tree | CI | CI |
| V4 | `go build ./...` GREEN across `api/`, `pkg/`, `operators/` | CI | CI |
| V5 | `task test:unit` GREEN; net test count not decreased; reshape matrix (Appendix A) fully ticked | CI | Ralph |
| V6 | `task test` (incl. integration / envtest) GREEN | CI | CI |
| V7 | `task lint` GREEN (golangci-lint + ESLint) | CI | CI |
| V8 | `task validate:manifests` GREEN (kubeconform `--strict`) | CI | CI |
| V9 | `task dogfood:full` GREEN on cleanroom-fresh infra (pre-merge gate, not just post-merge) | LIVE | Paulo (operator) |
| V10 | Drill iter ≥56 GREEN post-merge (M07 LIVE keyless GREEN baseline at iter 55) | LIVE | Paulo (operator) |
| V11 | 7-day post-merge observation window: zero P0 + ≤1 P1 traceable to `api/internal/db/` | LIVE | Paulo (operator) + ADR-3 trigger |

V9, V10, V11 are operator HARD STOPs per `feedback_ralph_loop_engineering_completion_pattern` — Ralph cannot self-satisfy LIVE gates.

---

## 8. Mandatory Actions (M-1 .. M-24)

Enumerated below in dependency order. Each is owned by Ralph unless noted; Paulo is owner-of-record for HARD STOP items.

| ID | Action | Owner | Gate it satisfies |
|---|---|---|---|
| M-1 | Produce reshape matrix in Appendix A: every `*_store.go` → target sqlc function(s) OR `raw/` escape with rationale | Ralph | V1 |
| M-2 | Pair-review per migrated store (Paulo reviews, Ralph executes) | Paulo + Ralph | C3 |
| M-3 | Pin sqlc CLI version (v1.31.1+) in CI workflow and in `docs/operations/dev-loop.md` | Ralph | ADR-2 |
| M-4 | Pin golang-migrate version in BOTH `infra/scripts/production/run-migrations.sh` AND the new CI render job | Ralph | ADR-2 |
| M-5 | Pin PostgreSQL 16 in CI render container | Ralph | ADR-2 |
| M-6 | Pre-author ADR-3 (`2026-05-29-sqlc-migration-rollback.md`) before any code lands | Ralph | C6 / ADR-3 |
| M-7 | Confirm drill iter 55 is GREEN baseline before starting migration; iter ≥56 GREEN post-merge | Paulo | V10 |
| M-8 | Squash-merge enforced (no merge commit) to keep ADR-3 rollback atomic | Paulo | ADR-3 |
| M-9 | AST gate over `api/internal/db/raw/` blocking reads/writes to sensitive tables (releases, builds, tenants, audit_log) without an explicit allowlist comment `// sqlc-escape: <rationale> (reviewed: <name>, <date>)` | Ralph | Trade-off table |
| M-10 | Two-step deletion of `schema_mirror_test.go`: (step 1) sqlc adoption merged + 7d clean → (step 2) separate deletion PR, operator-witnessed | Paulo + Ralph | C2 / C6 |
| M-11 | Daily rebase of `feat/sqlc-adoption` against `develop` | Ralph | D3 |
| M-12 | Add `task db:render-schema` target wrapping the render script | Ralph | ADR-2 |
| M-13 | Add `task db:sqlc-generate` target wrapping `sqlc generate` | Ralph | V3 |
| M-14 | Register `pkg/quality/sqlcsync/` in `taskfiles/quality.yml` (gate counter 10 → 11) | Ralph | V3 |
| M-15 | Daily diary entry by Paulo capturing progress %, blockers, decisions; this is the sponsor checkpoint mechanism (D2) | Paulo | D3 trigger |
| M-16 | Net unit test count must not decrease; reshape matrix tracks per-store delta | Ralph | V5 |
| M-17 | All `withEnvironmentLock` callsites verified post-migration to wrap sqlc queries inside the same transaction | Ralph | `.claude/rules/loopzero-design.md` Rule 6 |
| M-18 | All `pkg/statemachine` `Transition()` callers verified to remain the only state-writers; sqlc queries must not bypass them | Ralph | `.claude/rules/loopzero-design.md` Rule 7 / 8 |
| M-19 | CLAUDE.md updated with new "Stack" entry for sqlc + updated db/ structure note | Ralph | Documentation drift hook |
| M-20 | Pre/post CI execution-time budget measurement for `task quality:gates`; delta ≤ +20s acceptable | Ralph | Trade-off table |
| M-21 | `task quality:gates` advisory-lock gate must stay GREEN across migration; any RED triggers ADR-3 rollback evaluation per ADR-3 § Rollback trigger criteria | CI / Paulo | ADR-3 |
| M-22 | Document the raw escape allowlist in `docs/operations/dev-loop.md` so future contributors understand when escape is acceptable | Ralph | M-9 |
| M-23 | Generated package header comment must include sqlc version, render timestamp, schema git-sha, and `DO NOT EDIT` marker | Ralph | Auditability |
| M-24 | Memory entry recording the migration (`reference_sqlc_adoption_2026-05-29` or successor name) once V11 closes | Ralph (post-V11) | Knowledge retention |

---

## 9. Cross-references

- **Meeting record:** MEET-2026-05-29-001 (12 specialists; both VETO holders APPROVE conditional)
- **Coupled ADRs:**
  - [`2026-05-29-schema-gen-sql-render.md`](./2026-05-29-schema-gen-sql-render.md) (ADR-2 — CI render pipeline)
  - [`2026-05-29-sqlc-migration-rollback.md`](./2026-05-29-sqlc-migration-rollback.md) (ADR-3 — rollback playbook)
- **Governance rules:**
  - `.claude/rules/pre-release-governance.md` (T3 workflow that classified this; mandatory ADR per T3)
  - `.claude/rules/loopzero-design.md` (Rule 6 `withEnvironmentLock`, Rule 7 `pg_advisory_xact_lock`, Rule 8 `Transition()` invariants preserved)
  - `.claude/rules/design-principles.md` (KISS + DRY + Don't Reinvent — basis for sqlc selection over hand-rolled)
  - `.claude/rules/shell-scripts-allowlist.md` (D5 + Anti-Pattern #8 — sqlcsync gate is pure-logic Go, not bash)
  - CLAUDE.md Rule 12 (sqlc grep precheck verified greenfield)
- **Motivating evidence:**
  - `api/internal/db/release_store.go` (canonical M07 bug site; column `image_attestation_rekor_url` referenced 14× post-fix)
  - `api/internal/db/schema_mirror_test.go` (T2.2 of maturity plan — detection-time mirror that became redundant)
  - Memory `reference_m07_rekor_url_3layer_bug_2026_05_28`
- **Prior tool-evaluation ADR:**
  - `docs/decisions/2026-05-21-external-tooling-evaluations.md` (set the precedent for tool-class adoption discipline)
- **Sponsor-direct pattern precedents:**
  - MEET-2026-05-13 orphan-crd-deletion (sponsor-direct-approval pattern)
  - MEET-2026-05-13 ed25519-sponsor-approval (same pattern, second invocation)
- **Migration counterparts:**
  - `infra/scripts/production/run-migrations.sh` (production migrator; golang-migrate version-pinned counterpart per M-4)
  - `api/internal/db/migrations/000001..000020` (the 20 forward-only files; schema source of truth)

---

## 10. Decision Record metadata

| Field | Value |
|---|---|
| Sponsor | Paulo (operator) |
| Challenger | sre-engineer (Security & Blast Radius VETO holder); production-design reviewer (Production Design VETO holder) |
| Both VETO holders | APPROVE WITH CONDITIONS — conditions are M-1..M-24 + ADR-3 pre-authorization |
| Decision date | 2026-05-29 |
| Ratification status | RATIFIED (operator-locked at meeting; this ADR is the binding artifact) |
| Owners during migration | Paulo (operator) + Ralph (AI executor) — same committee that completed MEET-001 engineering closure this session |
| Sponsor checkpoint mechanism | M-15 daily diary (Paulo self-check; conscious daily snapshot of progress / blockers / decisions) |
| Branch lifetime cap | 14 days (D3) |
| Rollback authorization | Pre-authorized per ADR-3; triggers enumerated there |

---

## Appendix A — Reshape matrix (skeleton; M-1 fills before V1 closes)

| Source `*_store.go` | Approx. LOC (TBD by M-1) | Target sqlc query name(s) | Reshape risk | Test files to update | Notes |
|---|---:|---|---|---|---|
| `release_store.go` | TBD | `releases.sql`: `InsertRelease`, `SelectReleaseByID`, … | HIGH (M07 site; 14× column refs) | release-related tests | First store migrated; sets the pattern |
| `build_store.go` + `build_store_list.go` + `build_store_reconcile.go` | TBD | `builds.sql`: ~12 queries | HIGH | build-related tests | Three source files collapse |
| `tenant_store.go` + `tenant_deletion_store.go` | TBD | `tenants.sql`: ~8 queries | MEDIUM | tenant tests | Audit-log writes must remain inside `withEnvironmentLock` |
| `project_store.go` | TBD | `projects.sql` | LOW | project tests | Project name vs UUID convention (CLAUDE.md "Known Systemic Patterns") preserved |
| `team_store.go` + `active_team_store.go` + `membership_store.go` | TBD | `teams.sql` | MEDIUM | team tests | Three source files collapse |
| `environment_target_store.go` | TBD | `environment_targets.sql` | HIGH | environment tests | SoT pointer per `.claude/rules/loopzero-design.md` § State Source of Truth |
| `idempotency_store.go` | TBD | `idempotency.sql` | MEDIUM | idempotency tests | Concurrent-deploy invariant must be preserved |
| `audit_store.go` + `audit.go` | TBD | `audit.sql` | LOW | audit tests | Append-only; simplest migration |
| `cost_store.go` | TBD | `costs.sql` | LOW | cost tests | T7.2 of 2026-05-22 plan (OpenCost wiring) — careful |
| `domain_store.go` + 3 sub-files | TBD | `domains.sql` | MEDIUM | domain tests | v1/v2 domain state machine (CLAUDE.md) preserved |
| `cell_store.go` | TBD | `cells.sql` | MEDIUM | cell tests | Multi-cell SoT — careful |
| `attestation_store.go` + `app_attestation_store.go` | TBD | `attestations.sql` | MEDIUM | attestation tests | M07-adjacent; pair-review mandatory |
| `bootstrap_store.go` | TBD | `bootstrap.sql` | LOW | bootstrap tests | |
| `build_gc_store.go` | TBD | `build_gc.sql` | LOW | GC tests | |
| `secret_store.go` | TBD | `secrets.sql` | MEDIUM | secret tests | ESO sync path; do not change semantics |
| `repository_store.go` | TBD | `repositories.sql` | LOW | repo tests | |
| `scaling_store.go` | TBD | `scaling.sql` | LOW | scaling tests | `ScalingResolver` injection preserved |
| `preview_store.go` | TBD | `previews.sql` | MEDIUM | preview tests | Sprint 5 preview environments |
| Adapter / helper files: `pgx_adapter.go`, `pool.go`, `connection.go`, `query_executor.go`, `schema_mirror_helpers.go` | TBD | RETAIN or DELETE per M-1 | LOW | helper tests | `schema_mirror_helpers.go` deletion follows M-10 two-step |

(Rows above enumerate the 44 store files from the 2026-05-29 filesystem read. Subdivisions and exact query names will be filled by M-1.)
