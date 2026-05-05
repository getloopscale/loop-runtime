# loopzero-platform — Umbrella Chart

LoopZero PaaS platform umbrella chart. Composes all platform components (CRDs, API, operators, cloud, dashboard, build templates, monitoring, admission policies) for ArgoCD-driven Day-2 GitOps installation.

## Quick Start

```bash
# Install from OCI registry (production)
helm install loopzero-platform oci://registry.digitalocean.com/loopzero-system/helm/loopzero-platform \
  --namespace loopzero-system \
  --create-namespace \
  --version 0.2.0 \
  --values values-prod.yaml
```

## Dual Code Path: file:// (dev) vs oci:// (release)

Per ADR-0038 (charts-sota-implementation-plan T6.3), this chart deliberately uses two different dependency resolution paths between development and release:

### Development (`Chart.yaml` source)

Sub-chart dependencies use `file://` paths:

```yaml
dependencies:
  - name: loopzero-common
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-common"
  - name: loopzero-api
    version: ">=0.1.0 <1.0.0"
    repository: "file://../loopzero-api"
    condition: loopzero-api.enabled
```

**Rationale (ADR-0038):**
- Onboarding stays simple (no `helm registry login` required).
- Air-gapped develop works natively.
- `helm dependency update` resolves locally, fast.

### Release (CI mutates `Chart.yaml` before publish)

`.github/workflows/helm-charts.yml` job `package-push` rewrites all `file://` refs to `oci://...` via `yq` before publishing the umbrella to DOCR (`registry.digitalocean.com/loopzero-system/helm`). After CI:

```yaml
dependencies:
  - name: loopzero-common
    repository: "oci://registry.digitalocean.com/loopzero-system/helm"
    version: "0.3.0"  # pinned exact version
  - name: loopzero-api
    repository: "oci://registry.digitalocean.com/loopzero-system/helm"
    version: "0.2.0"
    condition: loopzero-api.enabled
```

**Rationale:**
- Standalone `helm pull oci://.../loopzero-platform` works for ArgoCD without local file:// resolution.
- Sub-charts are signed and SBOM-attested individually (Phase 5 of charts-sota plan).
- Each chart has immutable digest in OCI registry.

### Implications

- **Dev workflow**: `helm dependency update charts/loopzero-platform` works without OCI auth.
- **CI workflow**: `package-push` job MUST run AFTER all sub-charts are published OCI-side first.
- **Local install of published umbrella**: `helm pull oci://.../loopzero-platform` then install — does NOT use file://; works standalone.

## Sub-charts

| Chart | Required | Purpose |
|---|---|---|
| `loopzero-crds` | Yes | 10 CRDs (lifecycle independent) |
| `loopzero-api` | Yes | API server (control plane) |
| `loopzero-tenant-operator` | Yes | Tenant + Environment reconcile |
| `loopzero-app-operator` | Yes | LoopZeroApplication + ExposedRoute reconcile |
| `loopzero-preview-operator` | Optional | Preview environments |
| `loopzero-platform-operator` | Yes | PlatformBootstrap, ClusterCredential, GitRepositoryBinding |
| `loopzero-cloud` | Yes | Auth + billing service |
| `loopzero-dashboard` | Optional | Web UI |
| `loopzero-build-cwts` | Yes | ClusterWorkflowTemplates for build pipeline |
| `loopzero-monitoring` | Yes | PrometheusRules + Grafana dashboards (NOT the Prometheus stack — see ADR-0044) |
| `loopzero-admission-policies` | Yes | ValidatingAdmissionPolicies + ClusterImagePolicy + alerts |

Toggle individual sub-charts via `<name>.enabled` in values.

## Schema

Values are validated via `values.schema.json` (Draft-07). Key constraints:
- `global.registry` must match FQDN regex `^[a-z0-9.-]+(/[a-z0-9._-]+)*$`.
- Each `<sub-chart>.enabled` must be boolean.
- `global.environment` must be enum `[dev, staging, prod]`.

`helm lint --strict charts/loopzero-platform` enforces.

## Related

- ADR-0038: file:// + OCI rewrite strategy
- ADR-0042: ghost chart deletion (loopzero-controller, loopzero-tenant-controller removed)
- ADR-0043: loopzero-api / loopzero-cloud dual chart drift intentional
- ADR-0044: loopzero-monitoring naming (rename deferred Post-Launch)
- charts-sota-implementation-plan: full Sprint 9 chart improvements
- `docs/technical/charts/`: SOTA research + gates + benchmark
