# ADR-0043 — loopzero-api / loopzero-cloud dual chart drift is intentional

## Status

Accepted — 2026-05-05 (charts-sota-implementation-plan T6.1)

## Context

`charts/loopzero-api/templates/` has 10 templates (deployment, service, configmap, secret, networkpolicy, pdb, servicemonitor, serviceaccount, clusterrole, clusterrolebinding, NOTES, tests). `infra/helm/loopzero-api/templates/` has 4 templates (deployment, service, ingressroute, possibly servicemonitor).

Same drift pattern in `charts/loopzero-cloud` (7 templates) vs `infra/helm/loopzero-cloud` (2 templates).

The drift detector (`infra/scripts/audit-helm-drift.sh`, T1.3) flags both pairs. They were among the 22 points identified in the SOTA research (`docs/technical/charts/improvement-roadmap.md`).

## Decision

This drift is **intentional and approved**. The two chart systems serve different purposes:

- `infra/helm/<X>`: bootstrap-time, k3d local-dev minimal. IngressRoute for k3d Traefik, no PDB (single replica), no PrometheusRule (Prometheus stack is bootstrapped separately), inline RBAC if needed via Helmfile values overlay.
- `charts/loopzero-<X>`: Day-2 ArgoCD-distributable, production-ready. Full RBAC (ClusterRole + ClusterRoleBinding + ServiceAccount), PDB (replicaCount > 1), NetworkPolicy (DNS + extraEgress), ServiceMonitor (loopzero-monitoring stack), ConfigMap + Secret + checksum auto-restart annotations.

The drift detector marks these pairs as intentional via `APPROVED_DRIFTS` map referencing this ADR.

## Alternatives Considered

1. **Consolidate via `condition: <value>` in shared chart.** REJECTED — would force a single chart to support both bootstrap and production overlays via complex `if/else`. Violates CH-G04 (no product logic in templates). Bootstrap should remain dumb-simple.

2. **Make `infra/helm/loopzero-api` reference `charts/loopzero-api` via OCI dep.** REJECTED — bootstrap runs locally without OCI auth (ADR-0038); air-gapped develop would break.

3. **Remove `infra/helm/loopzero-api` entirely, ArgoCD also handles bootstrap.** REJECTED — bootstrap precedes ArgoCD installation by definition (chicken-and-egg).

## Consequences

- **+** Drift detector exits 0 with these pairs marked intentional.
- **+** Both chart systems remain optimized for their purpose.
- **+** CH-G11 transitions from "paper gate" to "automated with documented exceptions".
- **−** Maintainers must remember to propagate security-critical changes (e.g., new PSA defaults in loopzero-common) to both sides.
- **−** Adding a new chart in `charts/` requires conscious decision: also add to `infra/helm/`?

## Maintenance Rule

Any change to security defaults, image refs, or RBAC in `charts/loopzero-{api,cloud}` MUST be evaluated for propagation to `infra/helm/loopzero-{api,cloud}`. The CI drift detector flags any new template appearing on one side; reviewer classifies via this ADR or fixes regression.

## Related

- Plan: charts-sota-implementation-plan T1.3 + T6.1
- ADR-0041: drift detector advisory→blocking strategy
- Script: `infra/scripts/audit-helm-drift.sh` (APPROVED_DRIFTS map)
- Runbook: `docs/runbooks/charts-drift-audit.md`
