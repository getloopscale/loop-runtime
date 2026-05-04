# Knip Baseline (CLI orphan-exports)

> **Plan T0.3 reference.** Baseline of findings from `npx knip` in `cli/`.
> Goal: drive baseline to 0; flip CI gate from warn-mode to fail-mode when reached.

## Status (2026-05-03 — initial baseline post Plan T0.3)

**Total findings:** 9

### Unused exports (7)

| Symbol | File | Action proposal |
|---|---|---|
| `isUsingDefaultApiUrl` | `src/config/constants.ts:48` | DELETE — unused since CLI v0.1 |
| `OAUTH_CALLBACK_URL` | `src/config/constants.ts:83` | DELETE — superseded by Hydra device-code flow |
| `DOCS_URL_SECRETS` | `src/config/constants.ts:111` | DELETE — replaced by inline help text |
| `installOrUpgrade` | `src/installer/helm.ts:35` | INVESTIGATE — may be public API for `loopzero platform install` |
| `uninstall` | `src/installer/helm.ts:74` | INVESTIGATE — same |
| `test` | `src/installer/helm.ts:85` | INVESTIGATE — same |
| `context` | `src/tracing/index.ts:79` | DELETE — OTel integration partial |

### Unused exported types (2)

| Symbol | File | Action proposal |
|---|---|---|
| `HelmRelease` | `src/installer/types.ts:71` | KEEP — public API surface for installer commands |
| `StepResult` | `src/installer/types.ts:87` | KEEP — public API surface |

## Closure plan

1. **Phase 1 (this plan iter):** baseline documented (this doc) + knip CI gate active in warn-mode.
2. **Phase 2 (follow-up iter):** triage each finding via mini-PR — DELETE, KEEP-with-knip-ignore, OR EXPORT-to-public-API.
3. **Phase 3:** flip `continue-on-error: false` in `infra-gates.yml`. Knip becomes blocking gate.

## How to update this doc

```bash
cd cli && npx knip --no-progress > /tmp/knip-snap.txt
# Review delta vs current matrix; update table accordingly.
```

CI runs in warn-mode so new findings don't block PRs immediately, but they appear in PR job summary for reviewer awareness.

## Why baseline-first vs delete-now

The 9 findings fall in 3 categories:
1. **Genuinely dead** (deletable) — no risk; just hygiene.
2. **Maybe-API** (`installOrUpgrade`, `HelmRelease`) — deleting could break `loopzero platform install` in ways tests don't catch (missing E2E coverage).
3. **Tracing infra** (`context` from OTel) — partial integration; deletion would orphan OTel further.

Triage requires per-symbol investigation that should not be bundled with this plan. Baseline-first lets us ratchet over time without one giant cleanup PR.

## References

- `cli/knip.json` — config
- `.github/workflows/infra-gates.yml` job `ts-orphan-exports`
- ADR-0022 (amplifies "implemented but not wired" gate to TS)
- Plan `pre-launch-gaps-plan.md` T0.3
