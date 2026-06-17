# canonical-tests-v1 — QUARANTINE (READ-ONLY consultable)

> **Status:** QUARANTINED 2026-06-17 per ADR-2026-06-17-T3 D5.
> **Exit date:** TBD — DELETE after 3 consecutive chain-of-6-etapas GREEN + sponsor verbatim CP3.
> **Edit policy:** READ-ONLY enforced by `loopzero-ops quality quarantine-protect` gate.

## What's here

The v1 canonical test infrastructure (cleanroom + dogfood + chaos-test + live-proof + contra-prova) moved from `.claude/skills/`, `infra/scripts/local/`, and `infra/scripts/drill/` during T0.1 of plan `canonical-tests-rewrite-v2-plan.md` (per ADR-2026-06-17-T3-canonical-tests-rewrite Amendment 1).

| Subdirectory | Contents |
|---|---|
| `skills/` | 8 v1 SKILL.md directories (cleanroom, cleanroom-drill, dogfood, dogfood-drill, chaos-test, chaos-test-drill, live-proof, contra-prova) |
| `scripts/local/` | 16 v1 bash wrappers (dogfood-*, cleanroom-canonical-chain-runner, live-proof-dev-public) |
| `scripts/drill/` | 3 v1 drill orchestrators (cleanroom, dogfood, chaos) |
| `scripts/lib/` | `drill-dogfood-journeys.sh` shared lib |

## Why

Per ADR T3 D5 ("quarentena consultável"):
- v1 is preserved here so institutional memory (allowlist justificativas, EC absorptions, lessons learned per journey) remains readable during the v2 rewrite janela.
- v1 scripts remain **executable** during the janela (direct `bash archive/canonical-tests-v1/scripts/local/...` invocation OR via `task test:dogfood:*` targets which were path-updated in T0.1).
- This honors mandate INQUEBRÁVEL `cleanroom-up-live-test-mandate.md` "chain antiga continua executável durante a janela" (per ADR D9: mandate does NOT apply to refactor of own gate but DOES apply to feature deliveries in parallel).

## How to invoke v1 during the janela

```bash
# Cleanroom chain (full 7-step v1)
bash archive/canonical-tests-v1/scripts/local/cleanroom-canonical-chain-runner.sh

# Dogfood all v1 wrappers
bash archive/canonical-tests-v1/scripts/local/dogfood-all.sh --live-only

# Live-proof v1
bash archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh
```

These commands continue to work — taskfiles/test.yml `test:dogfood:*` targets were path-updated in the same T0.1 commit.

## Edit policy (enforced by CI)

`pkg/quality/quarantineprotect` gate blocks any PR that ADDs or MODIFIES files under `archive/canonical-tests-v1/`. The gate allows DELETE only when the PR title contains the literal marker `[ARCHIVE-DELETE]` (anti-accidental-delete safeguard per ADR critério de exit).

## When does this disappear

Per Phase 6 of the plan (T6.1):
- Sponsor CP3 (T5.3) emits verbatim "DELETE quarentena autorizado"
- Operator opens a PR titled `[ARCHIVE-DELETE] close ADR T3` containing `git rm -r archive/canonical-tests-v1/`
- Quarantine-protect gate ALLOWS the deletion (marker matches)
- After merge: archive removed; ROADMAP precondition unlocked (M0-M8 may begin); ADR T3 status → IMPLEMENTED.

## Related

- ADR T3: `.claude/knowledge-base/audits/2026-06-17-canonical-tests-deep-review/ADR-T3-bigbang-rewrite.md`
- Plan: `.claude/knowledge-base/plans/canonical-tests-rewrite-v2-plan.md`
- Quarantine-protect gate: `pkg/quality/quarantineprotect/check.go`
