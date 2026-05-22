// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package plutogate verifies that the Pluto CI workflow at
// `.github/workflows/pluto-deprecated-apis.yml` is correctly configured.
//
// This package contains NO production code — only tests. The actual gate
// runs as the Fairwinds Pluto Action in the workflow YAML; this package
// hermetically validates the YAML structure (SHA-pin, target-versions,
// only-show-removed: false) so the external Action actually does what
// the plan promises.
//
// Per Anti-Pattern #8 of plan 2026-05-22-shell-script-strategic-plan.md
// v1.3: external Pluto tool is ADOPTED, NOT reimplemented as a Go gate.
//
// Phase 7 T7.1.
package plutogate
