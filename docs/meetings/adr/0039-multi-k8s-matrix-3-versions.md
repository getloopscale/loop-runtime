# ADR-0039 — Multi-K8s matrix policy (3 versions: N-1, N, N+1)

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

CI today runs `kubeconform --kubernetes-version 1.31.0` only (single version). DOKS production is on 1.32.13. Charts não são testados em K8s 1.30 (DOKS minus 1) ou 1.33 (next).

Elastic Helm Charts (`helpers/matrix.yml`), Bitnami (GKE/TKG/AKS multi-cloud), CloudNativePG all run multi-K8s matrix. K8s upstream support window é ~3 versions.

## Decision

CI matrix tests K8s 1.30, 1.31, 1.32 simultaneously via:
- `chart-testing (ct) install` with `helm/kind-action` matrix.
- `kubeconform --strict --kubernetes-version` matrix per version.
- `ct.yaml` declares `kubernetes-versions: [v1.30.0, v1.31.0, v1.32.0]`.

Update cadence: cada release K8s upstream (~3-4 months). DOKS production version is N. Mantemos N-1, N, N+1.

Sprint 9 implementation reflects DOKS 1.32 → matrix 1.31/1.32/1.33 OR keep 1.30/1.31/1.32 if DOKS 1.32 stable. Decision: keep 1.30/1.31/1.32 for initial implementation; bump after T0.1 governance ratification + first matrix CI green.

## Alternatives Considered

1. **Single version (current).** REJECTED — leaves upgrade fragility undetected; quebras só viram evidentes em prod canary.
2. **Full matrix 1.28-1.34 (7 versions).** REJECTED — ~30min CI overhead; LoopZero nunca rodou em <1.30; reaches beyond reasonable upgrade window.
3. **N-1 / N only (2 versions).** REJECTED — not enough to detect forward compat issues; DOKS upgrades to N+1 viram surprise.

## Conditions

- **CONDITION-2 (sre-engineer):** Matrix CI MUST cache Kind images to keep total CI ≤ 20min budget. Implementation must use `helm/kind-action@v1.10.0` with image caching enabled.

## Consequences

- **+** Detect compat issues 3 months before prod upgrade.
- **+** CH-G18 NEW gate MET.
- **+** Bump K8s in production becomes routine (already validated in CI).
- **−** CI ~10-15min slower (mitigado por matrix paralelismo + Kind cache).
- **−** Each K8s upstream release requires CI matrix update.

## Related

- Plan: T3.1, T3.2, T3.3, T3.4
- Reference: Elastic helpers/matrix.yml
- DOKS current version: 1.32.13
