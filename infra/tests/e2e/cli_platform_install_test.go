// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero platform install -f <values>` covering applicable
// contracts.
//
// Source-of-truth: cli/src/commands/platform.ts (platformInstall) +
//   cli/src/register/platform.ts (CLI flags).
//
// Behavior:
//   - Exit 0: successful install (10-step flow completes)
//   - Exit 1: file not readable, missing domain field, validation failure,
//     prerequisite install failure, etc.
//   - Required: -f / --values <file>
//   - Optional: --profile <minimal|production> (default: minimal)
//   - NO --json mode (operator-facing, human-readable output).
//   - NO loopzero token requirement (uses kubectl directly).
package e2e

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero platform install", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: missing required --values ---

	It("Contract 2+5a: missing -f / --values yields exit 1", func() {
		_, stderr, exitCode := runLoopZero("platform", "install")

		expectAllowedExitCode(exitCode, "platform install (no -f)")
		Expect(exitCode).To(Equal(1),
			"missing -f should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: nonexistent values file ---

	It("Contract 2+5b: nonexistent values file yields exit 1", func() {
		_, stderr, exitCode := runLoopZero("platform", "install",
			"-f", "/tmp/this-file-definitely-does-not-exist-"+uniqueSuffix()+".yaml")

		expectAllowedExitCode(exitCode, "platform install (no file)")
		Expect(exitCode).To(Equal(1),
			"missing file should exit 1, got %d (stderr=%s)", exitCode, stderr)

		// Error message should reference the file or "Failed to read".
		expectNoInternalTerms(stderr, "platform install (no file)")
	})

	// --- Contract 2 + 5: values file without domain ---

	It("Contract 2+5c: values file without 'domain' field yields exit 1", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-platform-install-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		// Write minimal YAML without domain.
		valuesPath := filepath.Join(tmpDir, "values.yaml")
		err = os.WriteFile(valuesPath,
			[]byte("profile: minimal\n# domain intentionally missing\n"), 0o644)
		Expect(err).NotTo(HaveOccurred())

		_, stderr, exitCode := runLoopZero("platform", "install", "-f", valuesPath)

		expectAllowedExitCode(exitCode, "platform install (no domain)")
		Expect(exitCode).To(Equal(1),
			"missing domain should exit 1, got %d (stderr=%s)", exitCode, stderr)

		// Error message should mention "domain".
		Expect(stderr).To(ContainSubstring("domain"),
			"error should reference missing domain field")
	})

	// --- Contract 2 + 5: malformed YAML ---

	It("Contract 2+5d: malformed YAML yields exit 1", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-platform-install-bad-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		valuesPath := filepath.Join(tmpDir, "values.yaml")
		err = os.WriteFile(valuesPath,
			[]byte("this: is: not: valid: yaml::: ["), 0o644)
		Expect(err).NotTo(HaveOccurred())

		_, stderr, exitCode := runLoopZero("platform", "install", "-f", valuesPath)
		expectAllowedExitCode(exitCode, "platform install (bad yaml)")
		Expect(exitCode).To(Equal(1),
			"malformed yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		// no -f flag
		stdout, stderr, _ := runLoopZero("platform", "install")
		expectNoInternalTerms(stdout, "platform install (no -f) stdout")
		expectNoInternalTerms(stderr, "platform install (no -f) stderr")

		// nonexistent file
		stdout, stderr, _ = runLoopZero("platform", "install",
			"-f", "/tmp/nonexistent-"+uniqueSuffix()+".yaml")
		expectNoInternalTerms(stdout, "platform install (no file) stdout")
		expectNoInternalTerms(stderr, "platform install (no file) stderr")
	})

	// --- Contract 1 + 3: SKIP — full install requires fresh cluster ---

	It("Contract 1: SKIP — full install requires fresh k3d cluster + 10-step flow", func() {
		Skip("happy path is a 10-step install: cluster validation, prerequisites " +
			"(cert-manager, traefik, PG, Gitea, ArgoCD, Argo Workflows, Ory, MinIO), " +
			"CRDs, bootstrap, runtime secrets, platform install, pod wait, validation. " +
			"Takes 10-15 minutes against fresh k3d. Outside Phase 5 unit-of-work.")
	})
})
