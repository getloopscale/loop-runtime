# Helm chart-testing (ct) Operational Runbook

> **Plan reference:** T2.5 of `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Closes:** M06 Helm MEDIUM gap — "Multi-K8s-version testing missing. Sem chart-testing matrix integration. 16 grep hits v1beta1/v1alpha API versions (deprecation risk K8s 1.30+)."

## Purpose

chart-testing (ct) validates published Helm charts (`charts/` — OCI registry) against multiple Kubernetes API versions via ephemeral kind clusters. Detects:

- Deprecated APIs (e.g., `extensions/v1beta1` removed em K8s 1.22+)
- Invalid templates per K8s OpenAPI schema
- Missing required fields (e.g., `apiVersion`, `kind`, `metadata.name`)
- Chart.yaml schema violations

## Scope

- **Targets:** `charts/` (13 published OCI charts)
- **Excluded:** `loopzero-common` (library chart — no installable resources)
- **NOT scoped:** `infra/helm/` bootstrap charts (validated via helmfile dry-run + kubeconform em `task validate:manifests`)

## Pre-conditions

Install both binaries (operator action — not auto):

```bash
# chart-testing v3.11.0+
go install github.com/helm/chart-testing/v3/ct@v3.11.0

# kind (Kubernetes-in-Docker) for `ct install` ephemeral clusters
go install sigs.k8s.io/kind@v0.24.0

# Verify
ct version
kind version
```

## Quick start

```bash
# Lint all charts (fast — no cluster)
task helm:ct-lint

# Install all charts em ephemeral kind cluster (slow — provisions K8s)
task helm:ct-install -- --kubernetes-version 1.32.0
```

Both commands SKIP cleanly if binary absent — `task quality:gates` does not break em dev workstations without ct.

## Multi-K8s-version matrix

Per `.chart-testing/ct.yaml`, supported K8s versions are documented per release. For full matrix execution (e.g., monthly cron via T0.2 CI platform):

```bash
for v in 1.30.0 1.31.0 1.32.0; do
  echo "=== K8s $v ==="
  task helm:ct-install -- --kubernetes-version "$v"
done
```

Time budget per matrix slot: ~3min lint + ~15min install per chart × 12 charts = ~40min per K8s version. Full matrix (3 versions): ~120min.

**Recommendation per EC-18 of edge-case-review:** Run full matrix only nightly OR pre-release weekly. PR-time lint = lint only (~3min total).

## Configuration

See `.chart-testing/ct.yaml`:
- `chart-dirs: [charts]` — only published charts (not infra/helm/*)
- `target-branch: main` — diff-based incremental testing
- `excluded-charts: [loopzero-common]` — library chart
- `helm-extra-args: --strict` — matches `task helm:lint` invariant
- `validate-chart-schema: true` — enforces values.schema.json conformance

## Operator workflow

### Pre-PR (local dev)

```bash
# Quick check — only changed charts vs main
git push my-branch
ct lint --config .chart-testing/ct.yaml --target-branch main
```

### PR-time (T0.2 CI)

```bash
# CI workflow runs:
task helm:ct-lint
# Blocks merge if any chart fails lint
```

### Pre-release (weekly soak)

```bash
# Operator-driven (or CI nightly cron):
for v in 1.30.0 1.31.0 1.32.0; do
  task helm:ct-install -- --kubernetes-version "$v" --target-branch v0.1.0
done
```

### Pre-K8s-upgrade (DOKS minor bump)

```bash
# Before upgrading DOKS from 1.32 to 1.33:
task helm:ct-install -- --kubernetes-version 1.33.0 --all
# If any chart fails: fix BEFORE upgrade
```

## Failure modes

| Sintoma | Causa | Recovery |
|---------|-------|----------|
| `Error: chart has deprecated API: extensions/v1beta1` | Chart uses removed API | Migrate to apps/v1 (Deployment), networking.k8s.io/v1 (Ingress), etc. |
| `Error: required field "apiVersion" missing` | Template generates invalid YAML | Inspect `helm template <chart>` output; fix template |
| `Error: kind cluster creation failed` | Docker not running OR insufficient resources | Restart Docker; ensure ≥4GB RAM available |
| `ct lint OK but install fails` | Chart requires CRDs not yet installed em kind | Pre-install CRDs em values OR document chart deps |
| `Chart.yaml schema validation failed` | `version` field invalid semver | Use `MAJOR.MINOR.PATCH` strict format |

## SLO

| Metric | Target |
|--------|--------|
| `task helm:ct-lint` runtime | < 3min for all charts |
| `task helm:ct-install` per chart per K8s version | < 90s |
| Full matrix (3 versions × 12 charts) | < 120min |
| Failure rate (chart drift) | 0% (any failure blocks merge) |

## Integration with task quality:gates

T2.5 of plan defers full matrix to T0.2 (CI platform choice). For now:
- `task quality:gates` does NOT include ct-lint (skip-on-missing would always SKIP em dev — noise)
- Operator runs `task helm:ct-lint` manually when chart change touches templates
- Real CI (T0.2) will wire `ct lint --all --target-branch main` em PR pipeline

## Related

- ADR Helm chart dual-system (CLAUDE.md § Helm Chart Dual System)
- `infra/tests/` (render-time tests — complementary, NOT same as ct)
- `task helm:test` (Microsoft-style render-time invariants — 18 charts)
- T0.2 of plan (real CI platform choice — wires ct enforcement)
- Future: pluto deprecated-API detector (revisit per CLAUDE.md tool inventory)
