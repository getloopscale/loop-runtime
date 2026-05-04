# ADR-0032 — Introduce pkg/conditions kstatus-compatible package

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

8 controllers em `operators/internal/controller/` reconciliam `metav1.Condition` ad-hoc — sem helper compartilhado. Resultado:

- Drift entre controllers em condition Reasons (alguns CamelCase, outros UPPER_SNAKE).
- `LastTransitionTime` updated em todos os reconciles, não apenas em status changes (race condition em conditions sort).
- Sem polaridade abnormal-true (kstatus convention).
- Sem `observedGeneration` discipline — controllers não distinguem "not yet reconciled" de "fully reconciled".

ADR-0025 (Operators kstatus universal) estabeleceu a intenção de adotar kstatus, mas o pacote shared não existe em `pkg/`.

`sigs.k8s.io/cli-utils/pkg/kstatus` é a referência canônica:
- `Reconciling` + `Stalled` standard conditions.
- Polaridade abnormal-true: ausência de Stalled = OK; Stalled=True = problema.
- `observedGeneration` mandatório no status.
- Fallback em `Ready` para CRDs sem standard conditions.

Operator SDK + Kubebuilder oficialmente recomendam `meta.SetStatusCondition` (apimachinery) com kstatus convention.

## Decision

Criar `pkg/conditions/` package com:

1. **`conditions.go`:** wrappers around `metav1.Condition`:
   ```go
   const (
       ConditionReady       = "Ready"
       ConditionReconciling = "Reconciling"
       ConditionStalled     = "Stalled"
   )
   func SetCondition(conds *[]metav1.Condition, name, reason, message string, status metav1.ConditionStatus) bool
   func IsReady(conds []metav1.Condition) bool
   func IsStalled(conds []metav1.Condition) bool
   func IsReconciling(conds []metav1.Condition) bool
   ```

2. **`aggregate.go`:** `AggregateToPhase(conds []metav1.Condition, observedGen, currentGen int64) Phase` — substitui o conceito conceitual de `pkg/health.AggregatePlatform` usando Condition model. Phase enum local.

3. **`pkg/conditions` é o substituto canônico de `pkg/health`** (deletado em ADR-0028).

4. **Migration ordering:** começar com `tenant_controller.go` + `environment_controller.go`; restantes documentados como adoption pipeline em `docs/technical/pkg/conditions.md`.

## Alternatives Considered

1. **Import `sigs.k8s.io/cli-utils/pkg/kstatus` directly.** REJECTED — dep heavy (kstatus traz controller-runtime + apimachinery deeply). LoopZero wrappers cobrem 95% do uso em ~50 LOC.
2. **Continue ad-hoc per controller.** REJECTED — 8 controllers reescrevem mesma lógica; drift cumulativo.
3. **Use generics `Conditioned[T]` interface.** REJECTED — over-engineering; metav1.Condition é o standard K8s.

## Consequences

### Enables

- Gate PKG-G16 (eat-your-own-dogfood) GREEN — operators usam pkg/.
- Single source of truth para condition logic em controllers.
- `kubectl get tenant <X> -o yaml` retorna conditions consistentes.
- ArgoCD health sync usa kstatus convention.
- Substituto conceitual para `pkg/health` (deletado em ADR-0028).

### Constrains

- 8 controllers precisam refactor — programado em T2.2 inicial (2 controllers); restantes em follow-ups separados.
- `pkg/conditions` carrega dependência em `k8s.io/apimachinery` — aceitável (1 dep K8s alinha com mission do package).

## Implementation Reference

- Plan task: T2.2 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files: `pkg/conditions/conditions.go` (NEW), `pkg/conditions/aggregate.go` (NEW), `operators/internal/controller/tenant_controller.go` + `environment_controller.go` migrate.
- Documentation: `docs/technical/pkg/conditions.md` (NEW) com migration template.

## References

- ADR-0025 (Operators kstatus universal — intent ratificado)
- [kstatus README](https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/README.md)
- [What the heck are Conditions in Kubernetes controllers?](https://maelvls.dev/kubernetes-conditions/)
- `docs/technical/pkg/improvement-roadmap.md` § S9-2
