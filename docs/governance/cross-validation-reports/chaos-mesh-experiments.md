# Cross-Validation Report — chaos-mesh-experiments

**Date:** 2026-05-11
**Tasks scanned:** 9

## Summary

| Severity | Count |
|----------|------:|
| CRITICO | 2 |
| GRAVE | 3 |
| MEDIO | 1 |
| OK | 3 |

## CRITICO — 2 tasks

### T0.2 — PrometheusRule for chaos SLO validation

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `infra/helm/monitoring/manifests/prometheusrule-chaos.yaml`

### T2.1 — Create chaos-suite.yml workflow

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `github/workflows/chaos-suite.yml`

## GRAVE — 3 tasks

### T1.1 — Cell isolation experiment (cross-cluster partition)

- **Files total:** 2
- **Existing:** 1
- **Missing:** 1

  Missing files:
  - `infra/tests/chaos/manifests/cell-isolation-partition.yaml`

### T1.2 — Steady-state random pod kill in tenant namespaces

- **Files total:** 2
- **Existing:** 1
- **Missing:** 1

  Missing files:
  - `infra/tests/chaos/manifests/steady-state-pod-kill.yaml`

### T1.3 — IOChaos disk pressure on BuildKit

- **Files total:** 2
- **Existing:** 1
- **Missing:** 1

  Missing files:
  - `infra/tests/chaos/manifests/buildkit-disk-pressure.yaml`

## MEDIO — 1 tasks

### T0.1 — Helmfile release for chaos-mesh

- **Files total:** 3
- **Existing:** 2
- **Missing:** 1

  Missing files:
  - `infra/helmfile/workload.helmfile.yaml.gotmpl`

## OK — 3 tasks

### T1.4 — Deploy-under-chaos integration test

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T3.1 — End-to-end validation against k3d

- **Files total:** 0
- **Existing:** 0
- **Missing:** 0

### T3.2 — Regression matrix entries

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

