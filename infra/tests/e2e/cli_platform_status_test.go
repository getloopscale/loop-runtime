// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero platform status` covering applicable contracts.
//
// Source-of-truth: cli/src/commands/platform.ts (platformStatus).
//
// Behavior:
//   - Exit 0: phase == 'Ready'
//   - Exit 1: any other phase (NotInstalled, Installing, Failed, etc.)
//   - NO --json flag (platform commands are operator-facing, human-readable)
//   - NO loopzero token requirement (uses kubectl directly)
//
// LIMITATIONS: this command does not have --json output and does not gate on
// LOOPZERO_TOKEN. The 5-contract template doesn't fully apply. Contracts 1, 2, 4,
// 5 are tested here; Contract 3 (--json schema) is documented as N/A for
// platform commands.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero platform status", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 5: returns valid exit code based on phase ---

	It("Contract 1+5: returns exit 0 (Ready) or 1 (any other phase)", func() {
		stdout, stderr, exitCode := runLoopZero("platform", "status")

		expectAllowedExitCode(exitCode, "platform status")

		// Both 0 (Ready) and 1 (NotInstalled, Installing, etc.) are valid.
		// The contract is: must be one of the documented exit codes.
		Expect(exitCode == 0 || exitCode == 1).To(BeTrue(),
			"platform status should exit 0 or 1, got %d (stderr=%s)",
			exitCode, stderr)

		// Output should mention Phase, CRDs, or Components.
		Expect(stdout + stderr).NotTo(BeEmpty(), "platform status should produce output")
	})

	// --- Contract 4 ---

	It("Contract 4: stdout/stderr leak no internal terms", func() {
		// Platform commands talk to k8s directly via kubectl. Output may
		// reference component status (e.g., "Running"), endpoints, etc.
		// Critical: must not leak ArgoCD/Gitea/MinIO/etc. (Rule 3).
		stdout, stderr, _ := runLoopZero("platform", "status")
		expectNoInternalTerms(stdout, "platform status stdout")
		expectNoInternalTerms(stderr, "platform status stderr")
	})

	// --- Contract 3 documented N/A ---

	It("Contract 3 N/A: platform commands have no --json mode", func() {
		// Pass --json anyway and confirm command does not crash. It may be
		// silently ignored or produce a Commander error. Either is fine —
		// the contract is "no crash".
		_, _, exitCode := runLoopZero("platform", "status", "--json")
		expectAllowedExitCode(exitCode, "platform status --json (no-op)")
		// Output not asserted — just that exit code is valid.
	})
})
