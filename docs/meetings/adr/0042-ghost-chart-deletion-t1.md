# ADR-0042 — Ghost chart deletion classified T1, not T2

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

Pre-release governance (`.claude/rules/pre-release-governance.md`) classifies changes:
- T0: trivial (typo, comment)
- T1: local implementation (single-component bug fix, minor refactor)
- T2: contracts, state, pipeline, ops impact (REQUIRES meeting)
- T3: architecture, stack, critical flow (REQUIRES meeting + ADR)

Helm chart modifications are listed in T2/T3 automatic triggers. Ambiguity: does "Helm chart modification" cover **deletion of ghost charts that have no Chart.yaml and render zero manifests**?

State of ghost charts:
- `charts/loopzero-controller/` — has `charts/loopzero-common-0.2.0.tgz` vendored, NO Chart.yaml, NO templates.
- `charts/loopzero-tenant-controller/` — same.
- `charts/loopzero-platform/Chart.lock` lines 23-25 reference `loopzero-controller`.
- `charts/loopzero-platform/values.yaml` lines 25-26 toggle them on.

Verified: `helm template charts/loopzero-controller` fails (no Chart.yaml). They render NOTHING.

ADR-0023 (operator split) already formalized monolito legado removal in T1.5 (LOCAL phase). These dirs are residue.

## Decision

`git rm -r charts/loopzero-controller charts/loopzero-tenant-controller` is **T1** (local cleanup), not T2.

T1 classification means:
- No meeting required.
- No formal ADR required (this ADR exists to formalize the *classification rule*, not the deletion).
- Standard TDD workflow applies (T1.1 of charts-sota plan has TDD).
- Phase 1 quick-win can proceed without governance gate.

## Alternatives Considered

1. **Treat as T2 + meeting required.** REJECTED — dirs sem Chart.yaml não renderizam, equivalente a remover comentário órfão. Governance overhead disproporcional.
2. **Keep as "compatibility safety net".** REJECTED — não há consumer (zero render); ADR-0023 already shipped split; pre-release não acumula débito legado per CLAUDE.md design-principles "Legacy code is an error. Delete it."
3. **Mark as T2 retroactively when Chart.yaml is added back.** REJECTED — over-engineering; ghost deletion now is simple; if reanimated later, that itself is a new T2/T3 decision.

## Consequences

- **+** Phase 1 T1.1 unblocked sem governance gate adicional.
- **+** Clarifies T1 vs T2 boundary for future ambiguous cases.
- **+** CH-G15 NEW gate MET in 1 commit.
- **−** Establishes precedent that "no Chart.yaml = T1 deletion" — must guard against misuse (can't apply this rationale to charts with Chart.yaml).

## Rule Derived

For future charts cleanup:
- If `<chart>/Chart.yaml` does NOT exist → T1 deletion.
- If `<chart>/Chart.yaml` exists → T2 (impacts published artifact lineage).

## Related

- Plan: T1.1 (Phase 1 quick-win)
- ADR-0023: operator split (parent decision)
- ADR-0036: quick-wins-first sequencing
- Pre-release governance: `.claude/rules/pre-release-governance.md`
- CLAUDE.md design-principles "Legacy code is an error. Delete it."
