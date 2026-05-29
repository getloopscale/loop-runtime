// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// infra_restart_workflow_test.go — DELETED 2026-05-29 as part of T1.1c2c
// follow-up cleanup of plan 2026-05-29-sqlc-migration v1.3.
//
// The 3 sub-tests previously here (TestInfraRestart_UsesKubectlSetImage
// NotRolloutRestart, TestInfraRestart_ResolvesShortSHAFromWorkflowRun,
// TestInfraRestart_DocumentsT42AndAdr0049) asserted structural invariants
// of `.github/workflows/infra-restart.yml`. That file (and the entire
// `.github/workflows/` tree) was deleted 2026-05-23 by the operator
// decision "GitHub Actions is not the CI/CD platform" documented in
// CLAUDE.md § Repository Structure.
//
// The test guarded a file that no longer exists and now hard-fails every
// `go test ./...` run. Per no-workaround discipline of plan v1.3
// (sponsor mandate "QUANDO EXECUTAR CORRIGIR SEM WORAROUNDS POSSIVEIS
// PROBLEMAS QUE ENCONTRAR"), the cleanroom-friendly fix is to delete the
// stale assertion rather than gate it on a missing file.
//
// If/when the equivalent deploy-promotion logic is re-homed (e.g., into
// a `task deploy:promote` target or operator runbook), a new regression
// guard against that artifact should be written at that time.
//
// Cross-refs:
//   - CLAUDE.md § Repository Structure (.github/ NO workflows note)
//   - Memory `feedback_no_tribal_knowledge.md` (no workaround discipline)

package unit
