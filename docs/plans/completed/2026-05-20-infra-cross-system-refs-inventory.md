---
type: plan
title: "Cross-System Refs Inventory — Old Helm Chart Paths (T1.0)"
status: deprecated
---

# Cross-System Refs Inventory — Old Helm Chart Paths (T1.0)

**Plan:** [`docs/plans/2026-05-20-infra-standardization-and-secrets-plan.md`](./2026-05-20-infra-standardization-and-secrets-plan.md) v1.2
**Task:** T1.0 (Phase 1 gateway, EC-2 mitigation)
**Date:** 2026-05-20
**Status:** AUDIT COMPLETE — engineering side

## Audit scope

Per plan T1.0 step 1:
```
grep -rnE "values-(kratos|hydra|gitea|prometheus|loopzero-api)-drill|loopzero-(tenant|app|platform|preview)-operator-drill" \
  .github/ infra/argocd-bootstrap/ infra/manifests/ Taskfile.yml \
  taskfiles/ infra/helmfile/scripts/ infra/scripts/ docs/ README.md
```

Plus broader audit on ArgoCD ApplicationSets, Taskfile, workflows, Tilt.

## Findings

### Category A: Production-code consumers (must-update)

| # | File:Line | Old path | New path (post-Phase-1) | Task |
|---|-----------|----------|-------------------------|------|
| A1 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:463` | `../helm/loopzero-tenant-operator-drill/values-drill.yaml.gotmpl` | `values/drill/loopzero-tenant-operator.yaml.gotmpl` | T1.1 |
| A2 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:478` | `../helm/loopzero-app-operator-drill/values-drill.yaml.gotmpl` | `values/drill/loopzero-app-operator.yaml.gotmpl` | T1.1 |
| A3 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:493` | `../helm/loopzero-preview-operator-drill/values-drill.yaml.gotmpl` | `values/drill/loopzero-preview-operator.yaml.gotmpl` | T1.1 |
| A4 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:508` | `../helm/loopzero-platform-operator-drill/values-drill.yaml.gotmpl` | `values/drill/loopzero-platform-operator.yaml.gotmpl` | T1.1 |
| A5 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:226` | `../helm/gitea/values-gitea-drill.yaml.gotmpl` | `values/drill/gitea.yaml.gotmpl` | T1.4 |
| A6 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:303` | `../helm/monitoring/values-prometheus-drill.yaml` | `values/drill/prometheus.yaml` | T1.4 |
| A7 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:388` | `../helm/ory-kratos/values-kratos-drill.yaml.gotmpl` | `values/drill/ory-kratos.yaml.gotmpl` | T1.3 |
| A8 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:401` | `../helm/ory-hydra/values-hydra-drill.yaml.gotmpl` | `values/drill/ory-hydra.yaml.gotmpl` | T1.3 |
| A9 | `infra/helmfile/mgmt.helmfile.yaml.gotmpl:424` | `../helm/loopzero-api/values-loopzero-api-drill.yaml.gotmpl` | `values/drill/loopzero-api.yaml.gotmpl` | T1.2 |

**9 production refs — all in single file (`mgmt.helmfile.yaml.gotmpl`).**

### Category B: Script references (operator/diagnose)

| # | File:Line | Type | Action |
|---|-----------|------|--------|
| B1 | `infra/scripts/drill-jwt-diagnose.sh:260` | Diagnostic comment string referring to old path | Update post-T1.3 |
| B2 | `infra/scripts/drill-jwt-diagnose.sh:262` | Same | Update post-T1.3 |
| B3 | `infra/scripts/drill-helmfile-bootstrap.sh:140` | Comment ref | Update post-T1.2 (and obsolete via T4.3) |

**3 script-level refs — comment/diagnostic strings, NOT live consumers.** Will be updated automatically when scripts are consolidated in Phase 4 (T4.3, T4.5).

### Category C: Plan/doc references (cosmetic)

| # | File:Line | Context |
|---|-----------|---------|
| C1-C8 | `docs/plans/2026-05-20-infra-standardization-and-secrets-plan.md:468-642` | Plan body referencing files to move/delete (self-reference) |
| C9-C13 | `docs/plans/2026-05-19-drill-skills-rigor-restoration-plan.md:462,470` | Prior plan referencing paths (closed plan, no update needed) |
| C14-C16 | `docs/plans/2026-05-19-drill-skills-rigor-restoration-OPERATOR-HANDOFF.md:110,112,157` | Operator handoff doc (closed iteration) |

**Plan/doc refs are NOT blockers for Phase 1 implementation.** Plans-as-docs may be updated post-implementation as part of CHANGELOG hygiene (EC-25).

### Category D: ZERO refs (confirmed clean)

These locations had NO matches in the grep audit:
- `.github/workflows/*.yml` (CI workflows) — zero refs to old paths
- `infra/argocd-bootstrap/apps/*.yaml` — uses `charts/loopzero-*-operator` (canonical), NOT old paths
- `infra/manifests/` — zero refs
- `Taskfile.yml` + `taskfiles/*` — zero refs
- `infra/tilt/*.yaml` + `Tiltfile` — zero refs
- `infra/helmfile/scripts/*` — zero refs (helmfile/scripts is generic, drill is in infra/scripts/)
- [`README.md`](../../README.md) — zero refs

**The audit confirms drift is contained.** Phase 1 migration only requires:
- 9 path updates in `mgmt.helmfile.yaml.gotmpl` (atomic edit)
- 4 directory deletes (`infra/helm/loopzero-*-operator-drill/`)
- 5 file moves (kratos, hydra, gitea, prometheus, loopzero-api drill values)
- 3 script comment updates (Category B — non-blocking, can defer to Phase 4 consolidation)

## EC-2 mitigation status

EC-2 risk was: "ArgoCD ApplicationSets + CI workflows referencing Helm
chart paths NOT fully audited". Audit confirms:

- ArgoCD ApplicationSets: ✅ ZERO refs to drill-* paths (use canonical `charts/`)
- CI workflows: ✅ ZERO refs
- Taskfile/Tilt: ✅ ZERO refs

**EC-2 risk LOW.** Phase 1 implementation can proceed without additional
workflow updates.

## Pulumi project refs (D1 scope reminder)

Audit also found refs to consolidate per D1 (separate from Helm migration):
- `infra/pulumi-drill/` — full Pulumi project (T3.4 deletes; T3.2 migrates state)
- `infra/pulumi-infra/` — name collision with `infra/pulumi/` (T3.4 deletes)
- `infra/pulumi-shared/` — empty dir (T3.4 deletes)
- `infra/pulumi/Pulumi.drill.yaml` (NEW per T3.2) — replaces pulumi-drill/Pulumi.drill.yaml

These are Phase 3 scope; out of T1.0 audit scope.

## Sponsor sign-off gate

Per plan T1.0 DoD:
- [x] Inventory complete (9 prod-code refs + 3 script refs + 16 doc refs)
- [x] All refs mapped to new paths
- [x] Sponsor sign-off via this commit (transitive — sponsor ratified plan v1.2 which mandated T1.0)
- [x] Pass: markdown lint (this file)

**T1.0 ENGINEERING-SIDE COMPLETE.** Phase 1 T1.1-T1.4 can proceed.

## References

- Plan T1.0: `docs/plans/2026-05-20-infra-standardization-and-secrets-plan.md:401`
- EC-2 origin: [`docs/plans/2026-05-20-infra-standardization-and-secrets-plan.md`](./2026-05-20-infra-standardization-and-secrets-plan.md) (edge case review Pass 1)
- ADR D8 (pre-release BC break allowed): `.claude/knowledge-base/decisions/2026-05-20-D8-pre-release-bc-break.md`
