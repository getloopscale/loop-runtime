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

Every change is classified first.

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
decision.

**Two things this section used to claim, both false as of 2026-09-11:**

- The full contract was said to live in `.claude/rules/pre-release-governance.md`. That file **does
  not exist**, so the table above is now the whole of it rather than a summary of it.
- A hook, `.claude/hooks/governance-gate.py`, was said to block edits to production files lacking an
  approved gate record. That hook **does not exist** either. The hooks actually installed are
  `boundary-check`, `english-only-check`, `post-edit-check`, `precompact-preserve`,
  `public-copy-lint`, `sessionstart-context`, `stop-validation`, `userpromptsubmit-inject` and
  `validate-command`.

So the tier discipline is honoured by the implementer, not enforced by a gate. Note also that
`.claude/` is **gitignored** — it is an installed kit, and a fresh clone contains none of it.

## Decision records

| Where | What |
|---|---|
| [`docs/decisions.md`](docs/decisions.md) | Product- and platform-level ADRs — 28 records consolidated into one document on 2026-09-11, each with its status and what it turns on. Full bodies: `git show 4f728eb7a:docs/decisions/<name>.md`. |
| git history | The 16 `MEET-*` technical meeting records, and the engineering ADRs produced by the cycle pipeline. `docs/meetings/records/` was removed in the same consolidation. |

This table used to credit `.claude/knowledge-base/decisions/` with 61 engineering ADRs. **That
directory does not exist.**

An ADR must compare alternatives and state the trade-off explicitly. A decision whose rationale is
only "it is how we already do it" is not a decision record.

Cross-repo and workspace-level decisions live in the sibling repo
`../loopzero-workspace/docs/decisions/` — for example the monorepo decomposition (2026-06-16) and the
AWS/Terraform infrastructure re-platform (2026-06-17). Do not duplicate them here. (`../decisions/`
at the umbrella root, which this line named until 2026-09-11, does not exist; that root holds a
single `README.md`.)

## Plan amendments

A registered plan cannot be amended silently mid-execution. `docs/plans/` was removed in the
2026-09-11 documentation consolidation — the completed plans are at `git show 4f728eb7a:docs/plans/`
— so this contract applies to wherever a plan is currently registered.

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
- Forbidden everywhere: `git checkout`, `git revert`, force-push on a shared branch, and hard reset.
  Use `git switch` / `git restore`. Enforced by `.claude/hooks/validate-command.py` — this line said
  `.sh` until 2026-09-11, and the file is Python. Full contract in
  [`.claude/rules/git-safety.md`](.claude/rules/git-safety.md), which also forbids `git stash` when
  the repository has more than one worktree, because the stash stack is shared across them.
- Releases follow [Semantic Versioning](https://semver.org/) and are cut through the release cycle
  (`develop → main` PR + annotated tag). Changes are tracked in [CHANGELOG.md](CHANGELOG.md) — the
  `[Unreleased]` section is updated by **every** change, not at release time.

[`.github/CODEOWNERS`](.github/CODEOWNERS) assigns review, and the PR checklist lives in
[`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md). Both are present.

**Branch protection is NOT confirmed.** This section said review was enforced by it; querying the
API on 2026-09-11 returned `403 — Upgrade to GitHub Pro or make this repository public to enable
this feature`, so the mechanism that would make the PR mandatory on the remote is unavailable on the
current plan. The local hook guarantees work *originates* on `workspace`; it cannot make the PR
itself mandatory. These are different guarantees and only the first one holds today.

## Quality bar

- **Tests before code.** No production code without a failing test written first.
- `task test` · `task lint` · `task validate:manifests` · `task quality:gates` must pass.
- Any claim that affects a maturity score needs **three independent evidence sources** (commit+test,
  LIVE log, pipeline run). Confidence in such claims is capped at 90-95%.
- Tool adoption is verified by grep against the filesystem, never asserted from memory.

Engineering conventions live in [`.claude/rules/`](.claude/rules/) — `testing.md`,
`error-handling.md`, `git-safety.md`, `architecture.md`, `parsimony-ladder.md`, `public-copy.md`.

Five names this line carried until 2026-09-11 — `workflow.md`, `tdd-workflow.md`, `loopzero-design.md`,
`design-principles.md`, `cli-output.md` — were deleted together in `9a4a29755` and are not coming
back; `CLAUDE.md` § Locked rules records where each subject moved.

## Security

Vulnerability reporting and the disclosure process are in [SECURITY.md](SECURITY.md). Security and
blast-radius review carry an absolute veto on T2/T3 changes.

## Amendments to this document

This document is amended by the project owner. Material changes to the tier table, the branching
model, or the quality bar require an ADR recorded in [`docs/decisions.md`](docs/decisions.md).
