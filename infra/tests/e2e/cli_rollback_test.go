// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero rollback` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/rollback.ts (AUDIT-25).
//
// Behavior:
//   - Exit 0: success OR cancelled
//   - Exit 1: MISSING_CONFIG, INVALID_CONFIG, CONFIRMATION_REQUIRED in CI
//   - Exit 3: DEPLOY_IN_PROGRESS (409), 404 (no active release / no previous),
//     API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {status, rolled_back_release, active_release,
//     environment, duration_seconds, url?}
//   - --json error: {error, exit_code, code}
//
// SAFETY INVARIANT: migration warning is ALWAYS printed in non-JSON mode
// (even with --yes). This signals that rollback does NOT revert database
// migrations — incompatible schemas are user responsibility.
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero rollback", Ordered, func() {
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
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"rollback", "--yes", "--json")

		expectAllowedExitCode(exitCode, "rollback (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: missing loopzero.yaml ---

	It("Contract 2+5b: missing loopzero.yaml yields exit 1 MISSING_CONFIG", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-rollback-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir,
			"rollback", "--yes", "--json")

		expectAllowedExitCode(exitCode, "rollback (no yaml)")
		Expect(exitCode).To(Equal(1),
			"missing yaml should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 2 + 5: CI without --yes ---

	It("Contract 2+5c: CI=true without --yes yields exit 1 CONFIRMATION_REQUIRED", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		oldCI := os.Getenv("CI")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		os.Setenv("CI", "true")
		defer func() {
			os.Setenv("LOOPZERO_TOKEN", oldToken)
			os.Setenv("CI", oldCI)
		}()

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir,
			"rollback", "--json")

		expectAllowedExitCode(exitCode, "rollback (CI no --yes)")
		Expect(exitCode).To(Equal(1),
			"CI without --yes should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("CONFIRMATION_REQUIRED"))
	})

	// --- SAFETY INVARIANT: migration warning ---

	It("SAFETY: migration warning ALWAYS printed in non-JSON mode (even with --yes)", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Run rollback without --json so the human-readable warning fires.
		// Will likely fail (no active release) but warning prints first.
		stdout, _, _ := runLoopZeroInDir(fixtureDir, "rollback", "--yes")

		// Warning text from rollback.ts line 59-61.
		// "Warning: Rollback does not revert database migrations."
		Expect(stdout).To(ContainSubstring("Rollback does not revert database migrations"),
			"SAFETY violation: migration warning missing — refactor that removed it caught here")

		// Even on warning path, no internal terms.
		expectNoInternalTerms(stdout, "rollback safety warning")
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		tmpDir, _ := os.MkdirTemp("", "loopzero-e2e-rollback-leak-")
		defer os.RemoveAll(tmpDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		oldCI := os.Getenv("CI")
		os.Setenv("CI", "true")
		defer os.Setenv("CI", oldCI)

		for _, args := range [][]string{
			{"rollback", "--yes"},
			{"rollback", "--yes", "--json"},
			{"rollback"},
		} {
			stdout, stderr, _ := runLoopZeroInDir(tmpDir, args...)
			expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
			expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
		}
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires deployed app with previous release", func() {
		Skip("happy path requires (a) at least 2 deployed releases (current + previous) " +
			"to roll back to. State setup beyond Phase 5 scope. F1.5 from " +
			"e2e-validation-final-2026-04-23.md validated this manually.")
	})
})
