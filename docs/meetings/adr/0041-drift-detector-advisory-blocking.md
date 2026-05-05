# ADR-0041 — Drift detector strategy (advisory → blocking gradual)

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

CH-G11 (Published charts and bootstrap charts stay deliberately aligned) was paper gate — sem ferramenta. Drifts conhecidos não documentados:
- `charts/loopzero-api/templates/` (12 templates) vs `infra/helm/loopzero-api/templates/` (~3) — intentional dev minimal vs prod full, sem ADR.
- `charts/loopzero-admission-policies/templates/` (6) vs `infra/helm/admission-policies/templates/` (11) — REGRESSION (T1.2 fixes).

Crossplane uses `helm-drift` plugin in CI. Polaris in LoopZero (`MEET-062 condition #1`) graduated `continue-on-error: true` baseline-first → severity=danger.

## Decision

Build `infra/scripts/audit-helm-drift.sh` + CI step in `.github/workflows/helm-charts.yml` with **advisory mode** initially (`continue-on-error: true`).

Graduation timeline:
- **D+0**: Advisory mode begins. CI emits GitHub annotations on drift, doesn't fail.
- **D+30** (or before GA, whichever first): Each detected drift classified:
  - "Intentional" → ADR documenting justification (e.g., ADR-0043 for loopzero-api dual chart).
  - "Regression" → fix via PR (e.g., T1.2 admission-policies port).
- **After D+30 / pre-GA**: Remove `continue-on-error: true`. Drift detector becomes blocking. New drifts require ADR before merge.

Same governance pattern as Polaris audit graduation (MEET-062 condition #1).

## Alternatives Considered

1. **Block immediately.** REJECTED — travaria PRs em massa; team needs runway to classify existing drifts; same mistake as forcing Polaris severity=danger from day 1.
2. **Manual review only.** REJECTED — paper gate atual provou que convenção sem automação não funciona. CRD drift detector existe e funciona; admission/charts merece o mesmo.
3. **Block from day 1, pre-classify all known drifts via ADR before launch.** REJECTED — bottleneck de ADR creation; conditions 4-5 specialists demandam classification durante usage real.

## Conditions

- **CONDITION-3 (platform-security-engineer):** Drift detector MUST graduate from advisory to blocking after D+30 days OR before GA, whichever comes first.

## Consequences

- **+** CH-G16 NEW gate MET (advisory mode counts as MET-with-condition).
- **+** Habilita classification de drifts existentes sem block.
- **+** Forces team to write ADRs for intentional drifts (auditability).
- **−** 30 dias de runway antes do gate ser efetivo — risk de novos drifts entrarem.
- **−** Requires explicit graduation step (T7.1 referenced).

## Related

- Plan: T1.3 (advisory mode), T6.1 (ADR-0043 for loopzero-api), T7.1 (graduation)
- Polaris baseline-first reference: MEET-062 condition #1
