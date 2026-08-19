// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package phase2 provides hermetic validation that Phase 2 of plan
// docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3 was implemented
// correctly:
//
//   - taskfiles/infra.yml install:* tasks delegate to helmfile sync (T2.3.a)
//   - Zero `helm upgrade --install` calls outside comments
//   - Zero `base64 -d` invocations (replaced by `kubectl -o go-template` OR
//     `openssl base64 -d -A`) — T2.3.b portability fix
//   - Helm wrapper values (ory-kratos, ory-hydra) use single-quoted YAML for
//     gotmpl literal embedding — EC-02 YAML escape fix
//
// Per Anti-Pattern #8: NO production Go code in this package. Tests validate
// the Taskfile + Helm values YAML structure to ensure plan compliance.
//
// Phase 2 T2.1 + T2.2 + T2.3 — engineering side. T2.4 (LIVE cleanroom) is
// operator HARD STOP.
package phase2
