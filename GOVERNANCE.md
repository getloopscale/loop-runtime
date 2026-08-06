# LoopZero Project Governance

How changes are proposed, reviewed, and landed in this repository.

> **This is a proprietary, commercially-operated project** — see [LICENSE](LICENSE). LoopZero is the
> managed runtime operated by loopzero.example. It is **not** a vendor-neutral foundation project: there
> is no CNCF affiliation, no external TOC to escalate to, and no open contributor ladder. This
> document describes the governance that actually applies, not an open-source template.

## Phase: pre-release

The platform runs in production for loopzero customers, but the product is **not released to
external users**. Two consequences drive every rule below:

- **Backward compatibility is NOT required.** Refactoring, substitution and redesign are preferred
  when they yield a cleaner system.
- **Legacy code is an error — delete it.** There is no obligation to maintain, adapt, or work around
  code from a previous approach. Replace it; never patch it. The tests are the safety net, not the
  old code.

"Keep it as-is" requires justification. Inertia is not a valid reason to preserve a bad design.

## Change tiers — classify BEFORE writing code

Every change is classified first. The full contract is
[`.claude/rules/pre-release-governance.md`](.claude/rules/pre-release-governance.md).

| Tier | Description | Meeting required? | ADR required? |
|------|-------------|-------------------|---------------|
| **T0** | Trivial — typo, comment, doc, test-only | No | No |
| **T1** | Local implementation — single-component bug fix, minor refactor | No | No |
| **T2** | Contracts, state, pipeline, or operations impact | **Yes** | Recommended |
| **T3** | Architecture, stack, or critical-flow change | **Yes** | **Yes** |

**Always T2 or higher, regardless of diff size:** database schema/migrations · state-machine
transitions (`pkg/statemachine/`) · API contracts and `--json` shapes · CLI-visible output ·
controller reconcile logic · Helm charts · infrastructure-as-code · shared libraries (`pkg/`) ·
multi-cluster communication · credential and secret handling.

For T2/T3 the implementer **stops before coding**, produces a diagnosis (what, why, blast radius,
alternatives), and convenes a technical meeting. Implementation begins only after an APPROVED
decision. `.claude/hooks/governance-gate.py` blocks edits to production files that lack an approved
gate record.

## Decision records

| Where | What | Count |
|---|---|---|
| [`docs/decisions/`](docs/decisions/) | Product- and platform-level ADRs | 15 |
| [`.claude/knowledge-base/decisions/`](.claude/knowledge-base/decisions/) | Engineering ADRs produced by the cycle pipeline | 61 |
| [`docs/meetings/records/`](docs/meetings/records/) | `MEET-*` technical meeting records | 15 |

An ADR must compare alternatives and state the trade-off explicitly. A decision whose rationale is
only "it is how we already do it" is not a decision record.

Cross-repo and workspace-level decisions live in the umbrella at
[`../decisions/`](../decisions/) — for example the monorepo decomposition (2026-06-16) and the
AWS/Terraform infrastructure re-platform (2026-06-17). Do not duplicate them here.

## Plan amendments

Plans registered under `docs/plans/` cannot be amended silently mid-execution.

| Amendment | Trigger | Approval |
|---|---|---|
| Trivial | Typo / comment / clarification inside a task | Direct edit + commit |
| Scope refinement | Task split into sub-tasks; same DoD | New lightweight `MEET` record |
| DoD redefinition | Acceptance criteria change within a task | Meeting + ADR + plan version bump |
| Task deferral | Task moved to a future plan | Meeting + ADR explaining the blocker |
| Plan abandon | Plan declared infeasible | Full T3 meeting + sponsor revoke |

Forbidden: self-amendment justified only in a commit message; silent task removal; deleting a DoD
bullet without a version bump.

## Branching and releases

```
workspace ──PR──> develop ──PR + semver tag──> main
 (work)          (integration)                (release)
```

- All work is born on `workspace`. `develop` integrates but never originates. `main` is
  release-only and receives merges from `develop` alone.
- Forbidden everywhere: `git checkout`, `git revert`, `git push --force` on a shared branch,
  `git reset --hard`. Use `git switch` / `git restore` / `git stash`. Enforced by
  `.claude/hooks/validate-command.sh`; full contract in
  [`.claude/rules/git-safety.md`](.claude/rules/git-safety.md).
- Releases follow [Semantic Versioning](https://semver.org/) and are cut through the release cycle
  (`develop → main` PR + annotated tag). Changes are tracked in [CHANGELOG.md](CHANGELOG.md) — the
  `[Unreleased]` section is updated by **every** change, not at release time.

Review is enforced by branch protection plus [`.github/CODEOWNERS`](.github/CODEOWNERS); the PR
checklist lives in [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md).

## Quality bar

- **Tests before code.** No production code without a failing test written first.
- `task test` · `task lint` · `task validate:manifests` · `task quality:gates` must pass.
- Any claim that affects a maturity score needs **three independent evidence sources** (commit+test,
  LIVE log, pipeline run). Confidence in such claims is capped at 90-95%.
- Tool adoption is verified by grep against the filesystem, never asserted from memory.

Engineering conventions live in [`.claude/rules/`](.claude/rules/) —
`workflow.md`, `tdd-workflow.md`, `loopzero-design.md`, `design-principles.md`, `testing.md`,
`error-handling.md`, `cli-output.md`, `git-safety.md`.

## Security

Vulnerability reporting and the disclosure process are in [SECURITY.md](SECURITY.md). Security and
blast-radius review carry an absolute veto on T2/T3 changes.

## Amendments to this document

This document is amended by the project owner. Material changes to the tier table, the branching
model, or the quality bar require an ADR under `docs/decisions/`.
