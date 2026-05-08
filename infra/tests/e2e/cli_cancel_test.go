// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero cancel <build-id>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/cancel.ts (AUDIT-25).
// Backend: POST /api/v1/builds/{id}/cancel → 202 Accepted.
//
// Behavior:
//   - Exit 0: build cancelled
//   - Exit 1: MISSING_ARGUMENT
//   - Exit 3: BUILD_NOT_FOUND (404), BUILD_ALREADY_COMPLETED (409),
//     API_UNREACHABLE, UNEXPECTED_ERROR
//   - Exit 4: AUTH_REQUIRED
//   - --json success: {ok: true, build_id, status: "cancelled"}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero cancel", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 2 + 5: build not found ---

	It("Contract 2+5a: nonexistent build yields exit 3 BUILD_NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("cancel",
			"build-does-not-exist-"+uniqueSuffix(), "--json")

		expectAllowedExitCode(exitCode, "cancel (404)")
		Expect(exitCode).To(Equal(3),
			"missing build should exit 3, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("BUILD_NOT_FOUND"))
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5b: missing build-id yields exit 1 MISSING_ARGUMENT", func() {
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", "dummy")
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		_, stderr, exitCode := runLoopZero("cancel", "", "--json")
		expectAllowedExitCode(exitCode, "cancel (no id)")
		Expect(exitCode).To(Equal(1),
			"missing build-id should exit 1, got %d (stderr=%s)", exitCode, stderr)
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5c: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("cancel", "build-anything", "--json")

		expectAllowedExitCode(exitCode, "cancel (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("cancel", "build-x")
		expectNoInternalTerms(stdout, "cancel (no token) stdout")
		expectNoInternalTerms(stderr, "cancel (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("cancel", "build-x", "--json")
		expectNoInternalTerms(stdout, "cancel --json (no token) stdout")
		expectNoInternalTerms(stderr, "cancel --json (no token) stderr")

		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			for _, args := range [][]string{
				{"cancel", "absent-" + uniqueSuffix()},
				{"cancel", "absent-" + uniqueSuffix(), "--json"},
			} {
				stdout, stderr, _ := runLoopZero(args...)
				expectNoInternalTerms(stdout, strings.Join(args, " ")+" stdout")
				expectNoInternalTerms(stderr, strings.Join(args, " ")+" stderr")
			}
		}
	})

	// --- Happy path SKIP ---

	It("Contract 1+3: SKIP — happy path requires running build", func() {
		Skip("happy path requires a build in QUEUED/RUNNING/RELEASING state. " +
			"This needs `loopzero deploy` to start a build then immediately cancel " +
			"it — orchestration beyond Phase 5 scope. Tracked.")
	})
})
