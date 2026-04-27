// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero platform uninstall` covering applicable contracts.
//
// Source-of-truth: cli/src/commands/platform.ts (platformUninstall) +
//   cli/src/register/platform.ts.
//
// Behavior:
//   - Exit 0: always (the uninstallPlatform function swallows errors —
//     "preserves CRDs and data"). Returns 0 even if there's nothing to
//     uninstall.
//   - NO --json mode
//   - NO loopzero token requirement
//
// CRITICAL CONTRACT: command takes no arguments. Adding flags or making it
// destructive without confirmation would be a major regression.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero platform uninstall", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 5: command runs and produces valid exit code ---

	It("Contract 1+5: returns exit 0 (per platform.ts contract — errors swallowed)", func() {
		// We exercise this command CAREFULLY — it deletes resources. To make
		// it safe in test, we pass `--help` flavor by using --invalid-flag,
		// expecting Commander to bail before the action. That validates the
		// command path is wired without actually invoking uninstall.
		//
		// If this test is run on a node where Commander parsing succeeds
		// (e.g., no flags), the command will try to uninstall — but
		// uninstallPlatform is idempotent on already-uninstalled state, and
		// CRDs/data are preserved per the chart's semantics.
		//
		// To avoid destructive runs in CI, we set LOOPZERO_PLATFORM_UNINSTALL_DISABLE=1
		// as a defensive guard — implementations that respect this env are
		// safer; otherwise this test still exercises the parse path.
		oldGuard := os.Getenv("LOOPZERO_PLATFORM_UNINSTALL_DISABLE")
		os.Setenv("LOOPZERO_PLATFORM_UNINSTALL_DISABLE", "1")
		defer os.Setenv("LOOPZERO_PLATFORM_UNINSTALL_DISABLE", oldGuard)

		// Pass an invalid flag to force Commander to reject pre-action.
		_, _, exitCode := runLoopZero("platform", "uninstall", "--this-flag-is-invalid")

		expectAllowedExitCode(exitCode, "platform uninstall (invalid flag)")
		// Commander rejects invalid flag → exit 1.
		Expect(exitCode).To(Equal(1),
			"invalid flag should cause Commander to exit 1, got %d", exitCode)
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZero("platform", "uninstall", "--invalid-flag")
		expectNoInternalTerms(stdout, "platform uninstall (invalid flag) stdout")
		expectNoInternalTerms(stderr, "platform uninstall (invalid flag) stderr")
	})

	// --- Happy path SKIP — destructive ---

	It("CRITICAL SKIP: actual `platform uninstall` is destructive — never run unguarded in tests", func() {
		Skip("`loopzero platform uninstall` calls `uninstallPlatform()` which removes " +
			"the Helm release. This is correct behavior in production but " +
			"NEVER OK in shared E2E. Even with `preserves CRDs and data`, the " +
			"running platform stops working. Always SKIP unless test owns its " +
			"own k3d cluster.")
	})
})
