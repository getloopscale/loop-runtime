# ADR — Rename `api/internal/domain` → `api/internal/ports` + coupled DIP-layer cleanup

**Status:** ACCEPTED (ratified by sponsor 2026-09-06 via chat "TUDO APROVADOS")
**Date:** 2026-09-06
**Item:** B-057
**Meeting:** `.claude/records/meetings/B-057-2026-09-06-meeting.md`
**Plan:** `.claude/records/plans/b057-domain-rename-to-ports-plan.md` (v1.0; **v1.1 amendment required per Mandatory Actions**)

## Context

Three Go packages named `domain` mean three different things in the `loopzero` codebase:

| Package | Role | Package declaration |
|---|---|---|
| `pkg/domain` | product vocabulary (constants, plan tiers, env refs) | `package domain` |
| `api/internal/domain` | DIP port layer (stdlib-only interface declarations) | `package domain` |
| `api/internal/services/domain` | DNS domain provisioner (Cloudflare-backed) | `package domainsvc` (forced by collision) |

The forced divergence between directory name `domain` and package name `domainsvc` is one of only two such divergences in the entire Go tree (the other, `placementsvc`, was already resolved to `placement` on 2026-08-19 as the first half of B-057 bala 3).

Additionally:

- `api/internal/domain/types.go` (483 LoC) declares six unrelated bounded contexts (DomainStore, TeamStore, AuditStore, SecretStore, ProjectStore, TenantCRManager) in one file — measured. Architecture-analyst notes seven contexts if Bootstrap is counted separately from Projects.
- `api/internal/routes/{tenants,bootstrap,domains,projects,teams}/*.go` carry alias re-exports (`type X = domain.X`) that nullify the DIP boundary in practice. Alias count is measurement-disputed (plan 49, engine-go 46, dependency-analyzer 3-11 depending on regex strictness) — resolving this is a blocker.
- Import blast radius: **87 files** (measured; plan says ~90), all inside the `api` module.
- The `stdlib-only` gate at `cmd/loopzero-ops/internal/quality/stdlibonly/check.go:52` (const `guardado = "api/internal/domain"`) protects the DIP-layer property (bala 4, shipped 2026-08-18) and must travel with the rename.

## Decision

Approve a coupled 4-part refactor, phased sequentially, subject to **10 mandatory conditions** enumerated below (5 blockers + 3 majors + 2 minors):

1. **Rename** `api/internal/domain` → `api/internal/ports` (9 files, 87 importers, 577 identifier call-sites, 1 gate constant, 2 quality-gate literals).
2. **Split** `types.go` (483 LoC) into per-context files inside `ports/` (target: 6 files; must resolve Bootstrap-tangled-in-Projects question first).
3. **Delete** alias re-exports in `api/internal/routes/*` (exact set derived by `gopls rename` dry-run pre-execution).
4. **Restore** `api/internal/services/domain` from `package domainsvc` back to `package domain` (now unblocked by change #1).

Name `ports` chosen per Hexagonal Architecture / Ports & Adapters (Cockburn 2005). Alternatives (`dip`, `interfaces`, `contracts`, `boundaries`) rejected in plan ADR D1; ratified here.

## Alternatives considered (and rejected)

| Alternative | Rejected because |
|---|---|
| **Do nothing** | `domainsvc` divergence is a permanent onboarding tax; `types.go` at 483 LoC guarantees merge conflicts across unrelated teams; 46+ aliases nullify the DIP boundary they exist to expose. Precedent (`placementsvc → placement`, 2026-08-19) proved the pattern works — deferring compounds debt against a proven playbook. |
| **Split into 2 PRs** (rename-only, then split + aliases) | Sub-change 4 (`domainsvc → domain`) is genuinely blocked on sub-change 1 (removes collision). Splitting doubles MEET-governance overhead without reducing risk — the risky part (identifier rewrites) is in sub-change 1 regardless. Phases within one plan can still be committed separately for reviewability. |
| **`ports` → `interfaces`** | Anti-idiomatic in Go (packages should name responsibility, not pattern — cf. `io`, `sort`). |
| **`ports` → `dip`** | Obscure abbreviation; sigla for SOLID unrecognizable to newcomers. |
| **`ports` → `contracts`** | Semantic collision with sibling repo `loopzero-contracts`. |
| **`ports` → `boundaries`** | Longer, less canonical than "port" in the Hexagonal literature. |
| **`gopls rename` INSTEAD OF sed** | Partially accepted (mandatory for identifier rewrites) — but sed remains OK for import-path scoping combined with EC-3 hand-check. |
| **Decompose `ports` into 6 separate packages by bounded context** | Out of scope. This ADR authorizes rename + file split; bounded-context repartition is a distinct architectural decision. Tracked as future item, not gated by this one. |
| **Preserve alias re-exports in `routes/*` renamed to `type X = ports.X`** | Rejected: only relocates the anti-pattern under a new name; consumers of routes still get transitive DIP declarations they should import directly. |

## Trade-offs consciously accepted

- **Per-context file split is a lesser fix than bounded-context repartition.** Six files in one package `ports` is an improvement over one 483-LoC monolith, but the package still houses interfaces for 6-7 unrelated concerns. Ratifying this ADR does not preclude a future bounded-context repartition.
- **Post-Phase-4 fan-out into `routes/*`.** Deleting the aliases means every route handler package imports `ports` directly with no intermediate abstraction. This is architecturally cleaner than today's alias indirection (aliases nullify DIP entirely) but does increase the fan-out surface. Any intermediate abstraction that emerges organically later can be introduced without another rename.
- **loopzero-cli cross-repo status is deferred to ratification** (see Blocker/Condition list below).

## Risks accepted (with mitigation and owner)

| Risk | Severity | Mitigation | Owner |
|---|---|---|---|
| `sed` corruption of comments/testdata/docstrings (EC-3) | Medium | Mandatory `git grep -n "api/internal/domain"` diff review pre-commit; hand-edit the 2 quality-gate literals (`stdlibonly/check.go:52`, `contractconsumers/check.go:115`) | implementer |
| Hook `validate-command.sh` (B-168) false-positive blocks sed/git-mv commands | Low | Use Write tool for edits blocked by hook; report new false-positives against B-168 | implementer |
| loopzero-cli sibling repo consumes deleted alias re-exports | Low (partial measurement) | Clone-and-verify pre-Phase-4 OR document explicit accepted risk here with rollback plan | sponsor |

## Risks NOT accepted (blocker conditions on this ADR)

The MEET identified 5 blockers that must close before Phase 1 code executes. Ratifying this ADR obligates the implementer to close all 5.

**Blockers (must resolve pre-Phase-1):**

1. **Phase 1 as written breaks compilation.** The plan's sed script rewrites `package domain` and import paths but does **not** rewrite the 577 unaliased call-sites `domain.X` across 102 files. Required: `gopls rename` (workspace-aware) OR add a third sed pass `s/\bdomain\./ports./g` scoped to the 87 importers with false-positive review.
2. **Workspace module count wrong.** `go.work` declares **9 modules**, not 8; `tools/gen-service-auth-ed25519` missing from plan DoD, build loop, and CLAUDE.md § Multi-module. All three must be corrected.
3. **Alias count undefined.** Three different measurements exist (plan 49, engine-go 46, dep-analyzer 3-11). Pin the exact set with `gopls rename` dry-run before Phase 4 scoping.
4. **Atomicity of Phase 1 commit.** Const `guardado` update + `git mv` must land in ONE commit; splitting them leaves the `stdlib-only` gate red between commits.
5. **Citation drift.** Plan Baseline Context cites `api/cmd/setup_server.go` as disambiguation site; actual file is `setup_background.go`. Small but exactly the class `check_evidence_citations.py` exists to catch.

**Majors (must resolve pre-Phase-2 or pre-Phase-4 as noted):**

6. **types.go 6-vs-7 context ambiguity.** Bootstrap symbols (BootstrapRequest/BootstrapStore/CellPlacementEngine) are physically nested inside the Projects block at line 353. The plan must name explicitly where these land in the Phase 2 split (own `ports/bootstrap.go`, or folded into `ports/tenant.go`, or `ports/project.go`) BEFORE Phase 2 executes.
7. **Quality-gate string literals hand-edited.** `stdlibonly/check.go:52` and `contractconsumers/check.go:115` carry `"api/internal/domain"` as tracked-path registry keys, not import paths. A blind sed would touch them semantically. Hand-edit with review of consumption logic.
8. **EC-1 closure.** loopzero-cli is not cloned at `~/dev/loopzero-cli` — either clone-and-verify OR document explicit accepted-risk in this ADR with rollback plan.

**Minors (implementer discretion):**

9. Consider Phase 4 ↔ Phase 3 swap (delete aliases first — exercises new path across whole workspace before the isolated `services/domain` rename).
10. Add DoD assertion `git grep -n "api/internal/domain" -- '*.go'` returns 0 hits.

## Verification

Every condition above is verifiable by direct measurement (grep, `cat go.work`, `go build` per module). No self-report; no proxy metric.

## Consequences

- Rename atomically fixes the two-directory-package divergence (`domainsvc` → `domain`) and restores meaningful DIP boundary layer name (`ports`).
- Deleting alias re-exports means every consumer imports `ports.X` directly — the port layer's declared purpose (adapter-swap without touching consumers) becomes operative rather than performative.
- Every future port declaration lands in a per-context file rather than a monolithic `types.go`.
- CLAUDE.md § Multi-module table must be corrected to 9 modules (defensive: an incorrect docs entry is how a defect this size hides in plain sight).
- Post-execution, no further refactor is needed to close B-057 — item transitions to `shipped`.

## Sign-off

- [ ] Sponsor (paulohenriquevn) — ratifies this ADR and the 10 conditions
- [ ] Implementer (engine-go specialist) — commits to closing 5 blockers + 3 majors before Phase 1

Until both boxes are ticked, this ADR is **PROPOSED**. Phase 1 code MUST NOT execute.

## References

- Meeting record: `.claude/records/meetings/B-057-2026-09-06-meeting.md`
- Plan: `.claude/records/plans/b057-domain-rename-to-ports-plan.md`
- Backlog item: `BACKLOG.md § B-057`
- Governance rule: `.claude/rules/pre-release-governance.md § Change Classification` (T3)
- Precedent: `placementsvc → placement` (shipped 2026-08-19, first half of B-057 bala 3)
- Architecture rule: `.claude/rules/architecture.md § Layered boundaries`
- Hexagonal Architecture reference: Cockburn, Alistair. "Hexagonal Architecture", 2005.
