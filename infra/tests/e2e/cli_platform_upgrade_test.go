// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero platform upgrade --version <ver>` covering applicable
// contracts.
//
// Source-of-truth: cli/src/commands/platform.ts (platformUpgrade) +
//   cli/src/register/platform.ts (CLI flags).
//
// Behavior:
//   - Exit 0: successful upgrade
//   - Exit 1: missing --version, file not readable, helm upgrade failure,
//     pods not ready
//   - Required: --version <version>
//   - Optional: -f / --values <file>
//   - NO --json mode
package e2e

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero platform upgrade", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: missing required --version ---

	It("Contract 2+5a: missing --version yields exit 1", func() {
		_, stderr, exitCode := runLoopZero("platform", "upgrade")

		expectAllowedExitCode(exitCode, "platform upgrade (no --version)")
		Expect(exitCode).To(Equal(1),
			"missing --version should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: nonexistent values file ---

	It("Contract 2+5b: --values pointing to nonexistent file yields exit 1", func() {
		_, stderr, exitCode := runLoopZero("platform", "upgrade",
			"--version", "0.99.0",
			"-f", "/tmp/missing-"+uniqueSuffix()+".yaml")

		expectAllowedExitCode(exitCode, "platform upgrade (no file)")
		Expect(exitCode).To(Equal(1),
			"missing file should exit 1, got %d (stderr=%s)", exitCode, stderr)

		expectNoInternalTerms(stderr, "platform upgrade (no file)")
	})

	// --- Contract 2 + 5: malformed YAML ---

	It("Contract 2+5c: malformed YAML in --values yields exit 1", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-platform-upgrade-bad-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		valuesPath := filepath.Join(tmpDir, "values.yaml")
		err = os.WriteFile(valuesPath,
			[]byte(":::not::yaml:::["), 0o644)
		Expect(err).NotTo(HaveOccurred())

		_, stderr, exitCode := runLoopZero("platform", "upgrade",
			"--version", "0.99.0", "-f", valuesPath)

		expectAllowedExitCode(exitCode, "platform upgrade (bad yaml)")
		Expect(exitCode).To(Equal(1),
			"malformed yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZero("platform", "upgrade")
		expectNoInternalTerms(stdout, "platform upgrade (no --version) stdout")
		expectNoInternalTerms(stderr, "platform upgrade (no --version) stderr")

		stdout, stderr, _ = runLoopZero("platform", "upgrade",
			"--version", "0.0.1", "-f", "/tmp/nope-"+uniqueSuffix()+".yaml")
		expectNoInternalTerms(stdout, "platform upgrade (bad file) stdout")
		expectNoInternalTerms(stderr, "platform upgrade (bad file) stderr")
	})

	// --- Happy path SKIP ---

	It("Contract 1: SKIP — upgrade requires installed platform + new version", func() {
		Skip("happy path requires existing installation + valid target version " +
			"chart. Mutation against shared platform breaks subsequent tests. " +
			"Outside Phase 5 unit-of-work.")
	})
})
