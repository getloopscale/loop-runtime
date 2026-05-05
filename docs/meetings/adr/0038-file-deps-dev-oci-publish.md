# ADR-0038 — Manter file:// deps em dev + CI rewrite OCI no publish

## Status

Accepted (async, pending human ratification) — 2026-05-05 (MEET-2026-05-05-CHARTS-SOTA-ASYNC-AMENDMENT)

## Context

`charts/loopzero-platform/Chart.yaml` declares 12 deps with `repository: "file://../loopzero-X"`. Pattern de monorepo umbrella.

CI workflow `.github/workflows/helm-charts.yml` rewrites `file://` → `oci://...` in `loopzero-platform/Chart.yaml` no `package-push` job (linhas 252-267) before publishing umbrella to DOCR. Pattern shipping today.

Crossplane reference uses OCI digest pinning (`@sha256:...`) for deterministic install everywhere. Bitnami uses traditional Helm repo + classic deps. Korifi uses local file:// for monorepo.

## Decision

NÃO migrar `loopzero-platform/Chart.yaml` para `oci://...@sha256:...` em dev. Manter `file://` para subcharts internos. CI rewrite continua exatamente como está.

Specifically:
- Dev `helm dependency update charts/loopzero-platform` resolves via local `file://`.
- CI `package-push` rewrites to OCI before publishing umbrella.
- Published umbrella consumer (ArgoCD) gets OCI deps with pinned versions (não digests yet — possível Post-Launch).
- Documentation in `charts/loopzero-platform/README.md` (T6.3) explains dual code path.

## Alternatives Considered

1. **Migrate to OCI digest pin in dev (Crossplane pattern).** REJECTED:
   - Requires `helm registry login` em cada onboarding novo dev.
   - Breaks air-gapped develop (sem mirror OCI).
   - Complicates `helm dependency update` local (auth + network).
   - Adds CI complexity (must publish individual subcharts BEFORE running umbrella tests, breaking current parallelism).
   - Crossplane pattern serves provider distribution to external consumers; LoopZero subcharts são internal-only.

2. **Drop CI rewrite + use file:// at publish too.** REJECTED:
   - Breaks standalone `helm pull loopzero-platform` para consumers (ArgoCD).
   - Não há repository OCI consumível para deps no umbrella publicado.
   - Status quo solves this elegantly.

3. **Hybrid: file:// for internal, OCI for vendored libraries.** REJECTED:
   - LoopZero doesn't vendor external library charts em `loopzero-platform`. loopzero-common is internal.
   - Adds zero value over current pattern.

## Consequences

- **+** Onboarding permanece simples (sem OCI auth).
- **+** Air-gapped develop preserved.
- **+** `helm pull oci://.../loopzero-platform` funciona pós-CI rewrite.
- **+** No CI complexity increase.
- **−** Dual code path (dev = file://, release = OCI) precisa documentação clara (T6.3).
- **−** Não há digest-level immutability em deps internas (nominal version pinning only).

## Related

- Plan: T6.3 (README umbrella documenting file://+OCI rewrite)
- CI workflow: `.github/workflows/helm-charts.yml` linhas 252-267
- Crossplane reference: https://docs.crossplane.io/latest/packages/configurations/
