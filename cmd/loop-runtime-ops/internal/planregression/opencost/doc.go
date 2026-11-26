// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package opencost provides hermetic validation that the OpenCost
// adoption (T7.2 of plan 2026-05-22-shell-script-strategic-plan.md v1.3)
// is correctly wired:
//
//   - infra/helm/opencost/ LoopRuntime wrapper chart exists with required files
//   - infra/helmfile/core.helmfile.yaml.gotmpl declares the opencost release
//     with needs: [monitoring/prometheus]
//   - ServiceMonitor at infra/helm/monitoring/manifests/servicemonitor-opencost.yaml
//     scrapes :9003/metrics
//
// Per Anti-Pattern #8: NO production Go code in this package. External
// OpenCost tool adopted via Helm; only YAML structure validation lives here.
//
// LIVE validation (pod Running, metrics queryable in Prometheus) is
// operator HARD STOP — runs in cleanroom or drill cluster, not hermetic CI.
package opencost
