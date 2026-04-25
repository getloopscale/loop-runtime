// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero init` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/init.ts (AUDIT-36).
//
// Behavior:
//   - Exit 0: success OR project already exists (409 idempotent)
//   - Exit 1: CI_REQUIRES_YES (CI without --yes), FILE_EXISTS
//   - Exit 4: AUTH_REQUIRED
//   - Exit 5: AUDIT-36 partial failure — API returned 4xx (not 409),
//     loopzero.yaml still generated for inspection
//   - --json success: {project, apps, id?, detected?, sanitized_from?, warning?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero init", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5a: no token yields exit 4 AUTH_REQUIRED", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-init-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "init", "--yes", "--json")

		expectAllowedExitCode(exitCode, "init (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: CI requires --yes ---

	It("Contract 2+5b: CI=true without --yes yields exit 1 CI_REQUIRES_YES", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-init-ci-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		oldCI := os.Getenv("CI")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		os.Setenv("CI", "true")
		defer func() {
			os.Setenv("LOOPZERO_TOKEN", oldToken)
			os.Setenv("CI", oldCI)
		}()

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "init", "--json")

		expectAllowedExitCode(exitCode, "init (CI no --yes)")
		Expect(exitCode).To(Equal(1),
			"CI without --yes should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CI_REQUIRES_YES"))
	})

	// --- Contract 2 + 5: loopzero.yaml exists ---

	It("Contract 2+5c: existing loopzero.yaml without --force yields exit 1 FILE_EXISTS", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-init-exists-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		// Pre-create a loopzero.yaml.
		err = os.WriteFile(filepath.Join(tmpDir, "loopzero.yaml"),
			[]byte("project: pre-existing\napps: {}\n"), 0o644)
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		oldCI := os.Getenv("CI")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		os.Setenv("CI", "true")
		defer func() {
			os.Setenv("LOOPZERO_TOKEN", oldToken)
			os.Setenv("CI", oldCI)
		}()

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "init", "--yes", "--json")

		expectAllowedExitCode(exitCode, "init (file exists)")
		Expect(exitCode).To(Equal(1),
			"existing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("FILE_EXISTS"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-init-leak-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		oldCI := os.Getenv("CI")
		os.Setenv("CI", "true")
		defer func() {
			os.Setenv("LOOPZERO_TOKEN", oldToken)
			os.Setenv("CI", oldCI)
		}()

		// no token path
		os.Setenv("LOOPZERO_TOKEN", "")
		stdout, stderr, _ := runLoopZeroInDir(tmpDir, "init", "--yes")
		expectNoInternalTerms(stdout, "init (no token) stdout")
		expectNoInternalTerms(stderr, "init (no token) stderr")
		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "init", "--yes", "--json")
		expectNoInternalTerms(stdout, "init --json (no token) stdout")
		expectNoInternalTerms(stderr, "init --json (no token) stderr")

		// CI no --yes path
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "init")
		expectNoInternalTerms(stdout, "init (CI no --yes) stdout")
		expectNoInternalTerms(stderr, "init (CI no --yes) stderr")
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires real API to register project", func() {
		Skip("happy path scans the cwd, generates loopzero.yaml, calls API. Doable but " +
			"requires (a) clean tmp dir + (b) seed-valid auth + (c) no project " +
			"name conflict. AUDIT-36 also adds exit 5 on backend reject. Tracked.")
	})
})
