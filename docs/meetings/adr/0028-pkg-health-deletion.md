# ADR-0028 — Delete pkg/health (dead code)

## Status

Accepted (async, pending human ratification) — 2026-05-04 (MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT)

## Context

Audit `docs/technical/pkg/improvement-roadmap.md` revealed:

- `pkg/health/aggregator.go`, `phase.go`, `component.go` (253 LOC + 480 LOC tests) had **0 importadores em produção** quando auditado em 2026-05-04 via `grep -r "github.com/loopzero/loopzero/pkg/health" --include="*.go"`.
- Documentação anterior (`architecture.md` pre-update) descrevia o pacote como "shared health contract" — drift documental.
- `api/internal/health/prober.go` é implementação paralela com escopo diferente (HTTP probing, não Phase aggregation). Retém-se.
- Phase aggregation conceptualmente correto vai migrar para `pkg/conditions.AggregateToPhase` (ADR-0032) usando `metav1.Condition` model — a Kubernetes-native discipline (kstatus polaridade abnormal-true + observedGeneration).

## Decision

`pkg/health/` é DELETADO completamente. Documentação relevante atualizada (architecture.md remove linha; research-basis.md remove seção "Health contracts"; CHANGELOG entry [Removed]).

Substituto canônico: `pkg/conditions` introduzido por ADR-0032 cobre o caso de uso conceitual (aggregate health) usando o modelo Kubernetes-native em vez de Phase enum standalone.

## Alternatives Considered

1. **Wire pkg/health into platformbootstrap controller.** REJECTED — duplicação semântica entre Phase enum (`pkg/health`) e `metav1.Condition` (`pkg/conditions` per ADR-0032). Single source of truth wins.

2. **Mark pkg/health experimental and keep.** REJECTED — 0 imports significa nenhum usuário fornece feedback de experimentação. "Experimental" sem use case é dead code com label.

3. **Adopt sigs.k8s.io/cli-utils/pkg/kstatus directly.** REJECTED em ADR-0032 (consultar). Resumo: kstatus tem dep heavy; ~50 LOC wrappers cobrem 95%.

## Consequences

### Enables

- gate PKG-G14 (docs reflect real usage) GREEN para o pacote em questão.
- Single canonical aggregator: `pkg/conditions.AggregateToPhase`.
- Reduz LOC manutenção em 733 LOC (253 prod + 480 test).
- Pre-release governance pre-1.0 alinha com KISS: não shipping flagship que não tem usuário.

### Constrains

- Futura phase aggregation deve usar Condition model, não Phase enum standalone.
- Caller potencial que wanted Phase enum precisa wrapper local. (Histórico mostra: nenhum caller real durante 2+ sprints.)

## Implementation Reference

- Plan task: T1.1 em `docs/plans/pkg-sota-faang-grade-plan.md`.
- Files affected: `pkg/health/*` (delete), `docs/technical/pkg/architecture.md`, `docs/technical/pkg/research-basis.md`, `CHANGELOG.md`.

## References

- `docs/technical/pkg/improvement-roadmap.md` § Anti-Patterns to Eliminate #1
- `docs/technical/pkg/sota-gates.md` PKG-G14
- ADR-0032 (substituto canônico)
