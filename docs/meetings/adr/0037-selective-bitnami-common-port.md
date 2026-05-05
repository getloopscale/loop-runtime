# ADR-0037 — Selective Bitnami common port (5-6 helpers, not 40)

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

`charts/loopzero-common/templates/_helpers.tpl` has 16 helpers (covers naming, labels, image, security context, ServiceMonitor, PDB, NetworkPolicy). Bitnami `common` 2.x library has 40+ helpers across 12 categories (`names.*`, `labels.*`, `secrets.*`, `images.*`, `tplvalues.*`, `capabilities.*`, `compatibility.*`, etc).

Code audit revealed 5 patterns duplicated across 5 deployment templates (~80 LOC duplication):
- `checksum/config: {{ include ... | sha256sum }}` (5x)
- `tmp` emptyDir + volumeMount for `readOnlyRootFilesystem: true` (5x)
- `existingSecret` toggle for Secret name (5x)
- `imagePullSecrets` defensive access (5x)
- Templated values in annotations (multiple cases)
- `kubeVersion` guard pattern (needed once matrix testing K8s 1.30/1.31/1.32 reveals API divergence)

CH-G07 (Library chart consistency) PARTIAL today; CH-G17 (NEW, library coverage) NOT_MET.

## Decision

`loopzero-common` grows from 16 to 22 helpers. Port ONLY these 6:

1. `loopzero-common.tplvalues.render` — Bitnami pattern for value templating
2. `loopzero-common.kubeVersion` — guard for apiVersion compat
3. `loopzero-common.checksumAnnotations` — eliminate 5x duplication
4. `loopzero-common.imagePullSecrets` — global merger
5. `loopzero-common.secrets.name` — `existingSecret` pattern
6. `loopzero-common.tmpVolumeMount` (+ `loopzero-common.tmpVolume`) — ROFS compat

Hard cap: ≤ 25 helpers total in loopzero-common. Any future addition requires evidence of ≥ 2 caso de uso real (grep across `charts/`).

## Alternatives Considered

1. **Port full Bitnami common (40+ helpers).** REJECTED — framework opaco; YAGNI; 40+ helpers without ≥2 use cases each; inflates onboarding; Bitnami has different scale (300+ charts vs LoopZero's 13).
2. **Keep current 16 helpers, no expansion.** REJECTED — 5 deployments duplicam ~80 LOC; CH-G17 NEW gate requires coverage; review burden de cada deployment é alta.
3. **Inline expansion per chart (no helper).** REJECTED — anti-pattern explicit; viola DRY for genuine knowledge duplication (5x same checksum pattern).

## Consequences

- **+** ~80 LOC removed from 5 deployment templates after migration (T2.7).
- **+** New helpers each have ≥ 2 caso de uso provado.
- **+** CH-G17 status: NOT_MET → MET; CH-G07 PARTIAL → MET.
- **+** loopzero-common stays legível (16 → 22, cap 25).
- **−** loopzero-common minor version bump (0.2.0 → 0.3.0).
- **−** Migration de 5 deployments (T2.7) requires render diff verification.

## Related

- Plan: T2.1-T2.7
- Bitnami common reference: https://github.com/bitnami/charts/tree/main/bitnami/common
