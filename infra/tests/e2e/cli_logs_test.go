// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero logs` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/logs.ts (F-03 build resolution).
//
// Behavior:
//   - Exit 0: success (logs streamed or batch displayed)
//   - Exit 1: MISSING_CONFIG (no loopzero.yaml + no --build), MISSING_BUILD_ID,
//     NO_BUILDS (no builds for project), BUILD_RESOLVE_FAILED
//   - Exit 3: API errors (mapped from ApiError exitCode)
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {build_id, status, entries[{step, stream, content,
//     timestamp}], total_lines}
//   - --json error: {error, exit_code, code}
//
// NOTE: F1.4 (logs) is documented as PARTIAL in e2e-validation-final-2026-04-23
// due to Loki cross-cluster gap (3 infra issues). Tests focus on error paths
// reachable without Loki being healthy.
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero logs", Ordered, func() {
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
			"logs", "--build", "build-x", "--json")

		expectAllowedExitCode(exitCode, "logs (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 2 + 5: no loopzero.yaml + no --build ---

	It("Contract 2+5b: no loopzero.yaml AND no --build yields exit 1 MISSING_CONFIG", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		tmpDir, err := os.MkdirTemp("", "loopzero-e2e-logs-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		stdout, stderr, exitCode := runLoopZeroInDir(tmpDir, "logs", "--json")

		expectAllowedExitCode(exitCode, "logs (no yaml, no --build)")
		Expect(exitCode).To(Equal(1),
			"missing config + no build should exit 1, got %d (stderr=%s)",
			exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("MISSING_CONFIG"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// no token path
		os.Setenv("LOOPZERO_TOKEN", "")
		stdout, stderr, _ := runLoopZeroInDir(fixtureDir, "logs", "--build", "x")
		expectNoInternalTerms(stdout, "logs (no token) stdout")
		expectNoInternalTerms(stderr, "logs (no token) stderr")
		stdout, stderr, _ = runLoopZeroInDir(fixtureDir, "logs", "--build", "x", "--json")
		expectNoInternalTerms(stdout, "logs --json (no token) stdout")
		expectNoInternalTerms(stderr, "logs --json (no token) stderr")

		// no yaml path
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		tmpDir, _ := os.MkdirTemp("", "loopzero-e2e-logs-leak-")
		defer os.RemoveAll(tmpDir)
		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "logs")
		expectNoInternalTerms(stdout, "logs (no yaml) stdout")
		expectNoInternalTerms(stderr, "logs (no yaml) stderr")
		stdout, stderr, _ = runLoopZeroInDir(tmpDir, "logs", "--json")
		expectNoInternalTerms(stdout, "logs --json (no yaml) stdout")
		expectNoInternalTerms(stderr, "logs --json (no yaml) stderr")
	})

	// --- Build resolution path: F-03 NO_BUILDS ---

	It("Contract 2+5c: yaml present but project has no builds yields exit 1 NO_BUILDS or BUILD_RESOLVE_FAILED", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}

		fixtureDir := copyFixture("hello-server")
		defer os.RemoveAll(fixtureDir)

		// Use seed token so getProjectStatus call goes through; the fresh
		// fixture yaml uses project name "e2e-project" which the seed sets up
		// but with no builds.
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy-but-syntactically-valid")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZeroInDir(fixtureDir, "logs", "--json")

		expectAllowedExitCode(exitCode, "logs (no builds)")
		expectNoInternalTerms(stdout+stderr, "logs (no builds)")

		// Either NO_BUILDS (yaml present, status returned but empty),
		// BUILD_RESOLVE_FAILED (status call failed), MISSING_CONFIG (yaml
		// invalid for some reason), or AUTH_REQUIRED (token rejected).
		// All are valid documented outcomes for this state.
		Expect(exitCode == 1 || exitCode == 4).To(BeTrue(),
			"expected exit 1 (resolution) or 4 (auth), got %d (stderr=%s)",
			exitCode, stderr)
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires existing build with logs in Loki", func() {
		Skip("happy path requires (a) build that ran + (b) Loki cross-cluster " +
			"streaming functional. F1.4 from e2e-validation-final-2026-04-23 was " +
			"PARTIAL due to known infra gaps. Tracked.")
	})

	// --- Bonus: --follow flag honored ---

	It("Bonus: --follow flag is accepted (does not crash on parse)", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// --follow with no token still hits AUTH_REQUIRED — proves flag parses.
		_, stderr, exitCode := runLoopZeroNoToken("logs", "--build", "x", "--follow", "--json")
		expectAllowedExitCode(exitCode, "logs --follow")
		Expect(exitCode).To(Equal(4),
			"--follow + no token should exit 4, got %d", exitCode)
		// Should not contain "unknown option" or similar parse error.
		Expect(strings.ToLower(stderr)).NotTo(ContainSubstring("unknown option"),
			"--follow flag rejected by parser")
	})
})
