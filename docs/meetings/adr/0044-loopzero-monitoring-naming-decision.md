# ADR-0044 — loopzero-monitoring chart naming kept (rename deferred Post-Launch)

## Status

Accepted — 2026-05-05 (charts-sota-implementation-plan T6.2)

## Context

`charts/loopzero-monitoring/` contains 10 PrometheusRule templates + Grafana dashboards. It is NOT the Prometheus stack itself (which lives in `infra/helm/monitoring/`, separate chart). The naming `loopzero-monitoring` suggests the full stack but ships only rules + dashboards.

This naming ambiguity was identified as anti-pattern #9 in `docs/technical/charts/improvement-roadmap.md`. Two paths:

1. **Rename** `loopzero-monitoring` → `loopzero-monitoring-rules`. Disambiguates but breaks consumers (ArgoCD apps, loopzero-platform umbrella, Helm releases in production).
2. **Keep + document**. Status quo with explicit naming explanation in chart README and architecture doc.

## Decision

**Keep** the name `loopzero-monitoring`. Rename deferred to Post-Launch (after GA). Document the naming via:

- `charts/loopzero-monitoring/README.md` (NEW): explains chart scope (rules + dashboards only).
- `docs/technical/charts/architecture.md`: tabela já marca anomalia.

Pre-GA effort budget is better spent on supply chain (SBOM attestation) and drift detection (Phase 1 quick-wins) than on rename that breaks deployed customers.

## Alternatives Considered

1. **Rename pre-GA.** REJECTED:
   - Breaking change for any consumer using the OCI ref.
   - Chart.yaml `version` cannot be bumped to a new major in pre-release without governance. ArgoCD apps would need manual repointing.
   - Cost > benefit at this stage.

2. **Add an alias chart `loopzero-monitoring-rules` that empty-imports `loopzero-monitoring`.** REJECTED:
   - Creates 2 charts with the same content. Doubles publish + sign + SBOM cost.
   - Chart deduplication is hard via Helm (no direct symlink).

## Consequences

- **+** No breaking change for existing consumers.
- **+** Pre-GA effort budget preserved.
- **−** Naming ambiguity persists until Post-Launch.
- **−** Adds documentation surface (README + architecture note).

## Future Action

Post-Launch (after GA stable for ≥ 30 days), revisit this ADR. If consumer count is small enough OR a major version bump is otherwise required, execute the rename. Update Chart.yaml + loopzero-platform deps + CI workflow + this ADR status to "Superseded".

## Related

- Plan: charts-sota-implementation-plan T6.2 + Phase 7 #5 (Post-Launch backlog)
- Anti-pattern #9 in `docs/technical/charts/improvement-roadmap.md`
- Naming convention: `app.kubernetes.io/name=loopzero-monitoring` em manifests already; rename would change selector.
