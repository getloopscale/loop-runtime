# ADR: `services/platform` admission criterion — divide by concern

**Status:** PROPOSED
**Date:** 2026-09-06
**Item:** B-058 Bala 3
**Change Tier:** T2 (contracts / package boundaries at the services layer)
**Meeting:** async — specialists convened via subagents (see § Participants)
**Related halt:** `.claude/records/maintenance-runs/B-058-2026-09-06-BLOCKED.md`

## Participants

- **engine-go** (subagent, Go package boundary specialist) — argued for Path (a) importer-scope.
- **review-deep-loop:architecture-analyst** — argued for Path (b) divide-by-concern; surfaced the structural blocker on Path (a).
- **review-deep-loop:dependency-analyzer** — supplied the measured import graph and file counts.
- **Decision-Record Moderator** (this ADR) — synthesised the trade-off and dictates the mandatory follow-up.

## Context

B-058 Bala 3 halted when `services/platform` failed the Bala 3 admission-criterion test. The package holds 11 production files (env provisioning, project deletion, background janitors, circuit-breaker adapters). Its `doc.go` — if any — admits **by nothing**: any new file mechanically ends up here because there is no rule refusing it. This is the same defect shape Bala 1 (`adapters`, "shape-based") and Bala 2 (`clients`, "protocol-based") were killed for. Left in place, the next background job written by the next contributor lands under `platform` by gravity, not by decision.

### Measured facts (dependency-analyzer)

- **Importers:** 5 total, all in `api/cmd/*` (wiring roots); ZERO under `api/internal/`.
- **Cross-services edges:** 4 of 11 files import `services/deploycontracts` (a leaf per `servicesacyclic` frozen topology); the other 7 import no `services/*` sibling.
- **Group independence:** the 4 natural concerns (env-provisioning, project-deletion, background-janitors, circuit-breakers) have **zero cross-references** among themselves.
- **Filename lie:** `pg_circuit_breaker.go` contains an ArgoCD sync breaker, not a Postgres one.

### Governance constraint

`cmd/loopzero-ops/internal/quality/servicesacyclic/check.go` is fail-closed on unknown subpackages ("a new subpackage not registered here forces a conscious decision instead of silently widening the graph"). Any topology change to `services/*` requires an explicit allowlist amendment in the same slice.

## Problem

**How should `services/platform` acquire an admission criterion that a reviewer can use to REFUSE a file, without recreating the "mode-of-execution", "shape-based", or "pattern-based" defect shapes already rejected by Balas 1 and 2?**

## Options considered

### Option (a) — Importer-scope gate

**Description:** Keep the 11 files together under `services/platform`. Admit files by *who is allowed to construct/import them* (only wiring roots in `api/cmd/*`). Enforced by a companion gate (~200 LoC) similar to `packageadmission`.

**Pros:**
- Cheap: no file movement, no importer path changes across `api/cmd/*`.
- Empirically already true — dependency-analyzer measured every current importer is in wiring.
- One admission mechanism, one place to reason about.

**Cons — the blocker:**
- `servicesacyclic` fail-closed invariant is incompatible with a package whose members are "admitted by importer scope alone" without regard to inter-services edges. To keep `platform` as-is under Path (a) requires EITHER (i) physically relocating 22 files (11 prod + 11 test) into a NON-`services/*` tree (updating 8 importer paths in `api/cmd/*`), OR (ii) carving a skip-list exception in `servicesacyclic` for `platform`. Option (i) is mechanically expensive AND changes public import paths across the wiring layer; option (ii) erodes the very fail-closed invariant that already caught this class of defect at the topology level.

**Production fitness:** Acceptable IF the file-movement cost is paid; unacceptable if a skip-list carve-out is chosen.

**Effort:** Medium (with relocation) to Low (with carve-out — rejected on principle).

### Option (b) — Divide by concern (CHOSEN with conditions)

**Description:** Split `services/platform` into N subpackages, each with its own `doc.go` admission criterion. Amend `servicesacyclic`'s allowlist in the same slice, adding one entry per new subpackage with its justification. Preliminary N = 4:

| Subpackage | Files (indicative) | Admission-criterion shape |
|---|---|---|
| `services/envprovisioning` | `environment_ensurer{,_adapter,_test}.go`, `environment_cr_reader{,_test}.go` | Domain: constructs / reads Environment CRs on behalf of the API. |
| `services/projectdeletion` | `project_deleter{,_test}.go`, `orphan_detector{,_test}.go` | Domain: tears down project-scoped state; refuses anything that CREATES it. |
| `services/janitors` | `audit_log_pruner`, `idempotency_cleaner`, `kv_reconciler`, `secret_resyncer` (+ tests) | Importer-scope: only wiring may construct me; every member is a background reconciliation loop over a specific store. |
| `services/syncbreakers` | `workflow_circuit_breaker`, `pg_circuit_breaker` (renamed to `argocd_sync_circuit_breaker`) + tests | Contract: every member is a `CircuitBreaker` adapter that gates a downstream synchronous IO call and refuses to admit non-breakers. |

The 4 groups have measured zero cross-references, so the split is mechanical: `git mv` + import-path fixups in 5 wiring sites.

**Pros:**
- Zero design cost between groups (no re-architecture — the split just names what is already true).
- Preserves `servicesacyclic` fail-closed invariant: each new context is an explicit registered edge, `platform` is removed.
- Every new subpackage carries its own criterion — the item's underlying rule ("admission criterion must refuse something") is honoured per-subpackage rather than diluted into a global one.
- Filename lie (`pg_circuit_breaker.go`) is fixed in the same slice — the rename is not a separate item.
- Future janitors and future circuit breakers get an obvious home; a NEW concern that fits neither forces a NEW subpackage decision (which is the point).

**Cons:**
- File movement cost: 22 files across 4 subdirs (mechanical, low-risk).
- Allowlist expansion: 1 entry removed (`platform`), 4 entries added — each requires a written justification in `servicesacyclic`.
- Semantic risk: 2 of the 4 groups (`janitors`, `syncbreakers`) are close to the defect shapes Balas 1/2 rejected ("mode-of-execution" and "pattern-based" respectively). This risk is real and is addressed by the conditions in § Follow-up work — the criterion for each of those groups must be written as narrow importer-scope + honest contract, not dressed as false domain.

**Production fitness:** Good. No blast radius outside `api/internal/services/` + 5 wiring sites in `api/cmd/*`. `servicesacyclic` amendment is the guarded step and is enforced by the same gate that caught the defect.

**Effort:** Medium (mechanical `git mv` + import fixups + 4 `doc.go` files + servicesacyclic amendment + rename).

### Option (c) — Hybrid: partial split + importer-scope gate on residual

**Description:** Extract only the clearly-domain groups (envprovisioning, projectdeletion) into subpackages, leave janitors + circuit breakers together under a slimmed `platform`, and apply an importer-scope gate to that residual.

**Pros:** Fewer subpackages; keeps the residual small.

**Cons:**
- Fragments the enforcement mechanism: `servicesacyclic` for the split-out contexts + a second gate for the residual = two mechanisms answering the same question for one directory. This is precisely the "two truths, one target" failure the item was raised against.
- The residual `platform` still has the "no criterion refuses anything" problem the item exists to fix; the importer-scope gate on it must then bear all of the reviewer discipline that Bala 3 was meant to encode structurally.

**Production fitness:** Weaker than (b) because it institutionalises two admission mechanisms for one layer.

**Effort:** Medium (same movement cost as (b), plus the extra gate).

**Rejected.**

### Option (d) — Status quo

**Description:** Item stays HALTED. `services/platform` continues admitting-by-nothing.

**Pros:** Zero engineering cost this week.

**Cons:** The next background job (a new janitor, a new breaker, a new anything the reviewer cannot name a rule against) lands under `platform` by gravity. Bala 3 remains unmet forever; the Balas 1/2 pattern (admit-by-shape / admit-by-protocol) is preserved under a new alias (admit-by-directory-name).

**Rejected.** The item exists precisely because status quo is the failure mode.

## Decision

**Chosen: Option (b) — DIVIDE BY CONCERN, with the conditions in § Follow-up work.**

### Reasoning

Path (a) has a **hard structural blocker**: `servicesacyclic`'s fail-closed invariant either forces expensive physical relocation OR a skip-list carve-out that erodes the very gate that made this class of defect visible in the first place. Path (b) has NO structural blocker — the 4 groups are measured to have zero cross-references, so the split is mechanical, and each new subpackage strengthens (rather than erodes) `servicesacyclic` by being an explicit registered context.

The semantic objection to Path (b) — that `janitors` reproduces "mode-of-execution" grouping and `syncbreakers` reproduces "pattern-based" grouping — is real and addressed head-on: **each of those subpackages must carry an honest importer-scope + contract criterion, named as such**, rather than dressed as false domain. This is the mandatory condition in § Follow-up work item #2.

Path (b) trades a mechanical movement cost for a structural gain: the fail-closed invariant is preserved, the item is closed under a rule that refuses something in every subpackage, and the filename lie is fixed in the same slice.

## Trade-offs consciously accepted

1. **22 files redistributed across 4 subdirs.** Mechanical, low-risk; verifiable by `go build ./...` per module.
2. **`servicesacyclic` allowlist amendment in the same slice.** 1 entry removed (`platform`), 4 entries added. Each new entry carries an explicit justification (subpackage purpose + allowed edges — expected: each new context edges only to `deploycontracts`, preserving the leaf invariant).
3. **Two of the four new subpackages (`janitors`, `syncbreakers`) will carry importer-scope + contract criteria, not domain criteria.** This is explicit: pretending otherwise (dressing them as false domains — e.g. "operational-hygiene" or "resilience") would recreate the defect Bala 3 exists to fight. Honest labelling is part of the decision.
4. **The `packageadmission`-style companion gate MAY be reused** for the importer-scope criterion of `janitors` and `syncbreakers`, but is optional if each subpackage's `doc.go` carries the criterion and reviewer discipline enforces it. The choice is left to the slice.

## Consequences

### Positive

- `services/platform` disappears; four subpackages take its place, each with a criterion a reviewer can point at to refuse a file.
- `servicesacyclic` fail-closed invariant is strengthened (four registered contexts instead of one under-specified one).
- Filename lie (`pg_circuit_breaker.go` for an ArgoCD breaker) is fixed in the same slice.
- Future contributions face a decision: which of the four groups does this belong to, and if the answer is "none", a fifth subpackage decision is forced.
- Bala 3 closes under the same rule Balas 1 and 2 closed under: admission by something that can refuse.

### Negative

- Import paths change for 5 wiring sites under `api/cmd/*` (mechanical fix in the same slice).
- `janitors` and `syncbreakers` are close in shape to previously-rejected defect classes. Named honestly, they are acceptable; misnamed as false domains, they would re-open the item under a new alias. This risk is real and rests on reviewer discipline at slice-time (see Risk R1 + R2 below).
- The number `4` is preliminary. The plan slice may adjust N if a group becomes empty or a fifth concern is identified during physical relocation.

### Risks (accepted, with mitigation + owner)

- **R1 — `janitors` subpackage recreates the "mode-of-execution" defect.**
  *Mitigation:* Its `doc.go` states admission as importer-scope ("only wiring may construct me") + a narrow contract ("every member is a background reconciliation loop over a named store; a new member must name its store"). The criterion refuses "a new domain type wrapped in a `go func()`".
  *Owner:* engine-go specialist at slice-time; reviewer at PR.
- **R2 — `syncbreakers` subpackage recreates the "pattern-based" defect (Bala 1 shape).**
  *Mitigation:* Its `doc.go` states admission as a contract: "every member is a `CircuitBreaker` adapter that gates exactly one downstream synchronous IO call and exposes an `Allow()`/`RecordSuccess()`/`RecordFailure()` triple". Refuses non-breakers; refuses breakers over async / event-loop IO.
  *Owner:* engine-go specialist at slice-time; reviewer at PR.
- **R3 — `pg_circuit_breaker.go` filename lie survives the split.**
  *Mitigation:* Rename to `argocd_sync_circuit_breaker.go` (or split into two files if it holds both) is a mandatory action in the same slice — see § Follow-up work item #3.
  *Owner:* engine-go.

### Risks NOT accepted (would block)

None. No security regression, no SRE-visible change, no downstream contract breakage (all 5 importers are in this repo's `api/cmd/*` and are updated in the same slice).

## Validation plan

- **`go build ./...` per module** — every wiring site under `api/cmd/*` compiles after the import-path fixups.
- **`task test:unit`** — every relocated `*_test.go` passes at its new location without behavioural change (the split is mechanical).
- **`task quality:gates`** — including `servicesacyclic` (must pass with the amended allowlist naming the 4 new contexts, no `platform` entry, no "unknown context" for the relocated dirs) AND the Bala 3 admission-criterion test (must pass for each of the 4 new subpackages).
- **Reviewer read of each new `doc.go`** — the criterion in `janitors` and `syncbreakers` must be written as importer-scope + narrow contract, NOT as false domain. This is the qualitative gate that catches R1 and R2.
- **Rollback criterion:** if the slice cannot land all four `doc.go` criteria that honestly refuse something, the split is aborted and the item returns to `triaged` with a new opportunity naming the specific subpackage whose criterion could not be written honestly.

## Rollback plan

Pre-release; no external consumers. If the split lands and a fifth concern is later identified, add a fifth subpackage with a new entry in `servicesacyclic` — no migration cost. If a subpackage's criterion turns out unenforceable in practice, merge it back into a sibling (again, only import-path fixups) and record the merger in a follow-up ADR.

## Follow-up work (mandatory — numbered, with owners)

1. **Kairos (backlog owner) — MEET + ADR amending Bala 3 DoD.** New DoD text: *"`services/platform` is divided into N subpackages, each with an explicit admission criterion in its `doc.go`; the `services-acyclic` allowlist is amended in the same slice with each new context and its justification."* Governance path per `.claude/rules/pre-release-governance.md § Plan amendment policy` (DoD redefinition = MEET + ADR + plan version bump). Blocks `/plan-write` for the implementation slice.
2. **Plan slice (Daedalus tech-lead) — must name, before implementation begins:**
   (a) each of the N subpackage names;
   (b) each subpackage's admission criterion (verbatim `doc.go` text), with `janitors` and `syncbreakers` written as importer-scope + narrow contract, NOT dressed as false domains;
   (c) each new subpackage's `servicesacyclic` allowlist entry (allowed edges);
   (d) the exact mapping of the 11 current `platform/*.go` files (and their `_test.go` siblings) to the new subpackages.
3. **engine-go specialist — same slice — rename `pg_circuit_breaker.go`** to `argocd_sync_circuit_breaker.go` (or split into two files if it holds both Postgres and ArgoCD breakers). The filename lie MUST NOT survive the split.
4. **Optional — companion `packageadmission`-style gate reuse** for the importer-scope criterion of `janitors` and `syncbreakers`. Decided at slice-time based on whether reviewer discipline + `doc.go` prose is judged sufficient. If added, must be wired into `task quality:gates` in the same slice.

## References

- Halt record: `.claude/records/maintenance-runs/B-058-2026-09-06-BLOCKED.md`
- Architectural background: `docs/architecture/package-admission.md`
- Fail-closed topology gate: `cmd/loopzero-ops/internal/quality/servicesacyclic/check.go`
- Companion admission-criterion gate (reusable pattern): `cmd/loopzero-ops/internal/quality/packageadmission/`
- Governance for DoD amendment: `.claude/rules/pre-release-governance.md § Plan amendment policy`
- Related closed items (same underlying rule): B-058 Bala 1 (`adapters`, shape-based — killed), Bala 2 (`clients`, protocol-based — killed).
