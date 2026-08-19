// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package phase3 validates Phase 3 (deprecate task setup; canonicalize
// Tilt) of plan 2026-05-22-shell-script-strategic-plan.md v1.3:
//
//   - T3.1: docs/operations/dev-loop.md exists + CLAUDE.md/README canonicalized
//   - T3.2: Taskfile.yml setup target has deprecation notice pointing to tilt
//
// Phase 3 is doc-only — no production Go code. Tests verify the docs are
// committed and align with the plan's canonical dev loop narrative.
package phase3
