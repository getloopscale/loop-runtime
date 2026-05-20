# Render-Test Env Stubs

**Plan:** `2026-05-20-helm-render-time-testing-terratest-plan.md` T1.3 / ADR D10

This directory holds **plain YAML stubs** of env-specific values
overrides for Helm chart render-time tests.

## Why Plain YAML (not `.gotmpl`)

The helmfile values at `infra/helmfile/values/<env>/<chart>.yaml.gotmpl`
are templated by helmfile (contain `{{ env "..." }}`, `{{ .Values.X }}`
directives). `helm.RenderTemplate` (Terratest wrapper) calls `helm
template` directly — which does NOT process helmfile directives. Passing
a `.gotmpl` file fails with `error converting YAML to JSON: yaml:
invalid map key`.

Per ADR D10: test stubs are plain YAML. Manual sync with helmfile values
when they change. Phase 3 render tests catch drift via env-specific
assertions.

## Structure

```
infra/tests/helm/values/
├── README.md          (this file)
├── dev/               (k3d local dev overrides)
│   └── <chart>.yaml
├── drill/             (DOKS drill iter overrides)
│   └── <chart>.yaml
└── prod/              (Production overrides)
    └── <chart>.yaml
```

## Sync Protocol

When helmfile values at `infra/helmfile/values/<env>/<chart>.yaml.gotmpl`
change:

1. **Operator (human)** identifies which fields changed that AFFECT render
   output (e.g., image registry, replica count, domain — NOT comments).
2. **Operator** manually updates corresponding `infra/tests/helm/values/
   <env>/<chart>.yaml` plain stub.
3. **Phase 3 render tests** catch drift via env-specific assertions
   (e.g., `require.Equal(t, "registry.digitalocean.com/loopzero-system/...",
   deploy.Spec.Template.Spec.Containers[0].Image)`).

## What to Include

**INCLUDE** fields that affect manifests assertable in tests:
- `image.repository` / `image.tag` / `image.pullPolicy`
- `imagePullSecrets`
- `replicaCount`
- `service.port` / `service.type`
- `domain` (used in IngressRoute Host)
- Security context overrides
- `env:` vars consumed by render conditionals
- Resource limits/requests

**SKIP** fields irrelevant to render:
- `priorityClassName` defaults (unless asserted)
- Comments explaining business decisions (kept in source `.gotmpl`)
- Fields covered by chart defaults (only override what diverges)

## Gotchas

- **`{{` in stubs is rejected** — `helpers.NewOptions` enforces this via
  string-contains check. If you need a templated value, use a literal
  default (e.g., `tag: drill-phase2-9ad223a5` instead of
  `{{ env "LOOPZERO_API_IMAGE_TAG" | default "drill-phase2-9ad223a5" }}`).
- **Tests should assert FIELD-LEVEL invariants** (e.g., "image starts
  with `registry.digitalocean.com/`"), NOT exact values (regression
  noise). Per EC-6 from edge case review.
- **Not every chart needs every env stub** — `loopzero-crds` for example
  has identical render across envs (CRDs don't differ per env). Phase
  3 tests `t.Skip("no <env> override for <chart>")` when not present.

## Library Charts EXEMPT (D12)

Library charts (`Chart.yaml type: library`, e.g., `loopzero-common`) emit
zero manifests. No render-time tests applicable; no env stubs needed.
`helpers.IsLibraryChart()` detects + skips.

## Initial Scope (T1.3)

T1.3 creates stubs only for **loopzero-api pilot** (Phase 2):
- `dev/loopzero-api.yaml`
- `drill/loopzero-api.yaml`
- `prod/loopzero-api.yaml`

Other charts get their stubs as part of Phase 3 (T3.1, T3.2) per
implementation cadence.
