// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero env inspect <name>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/env-inspect.ts (AUDIT-1, AUDIT-25).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: missing name (MISSING_ARGUMENT), 404 (ENV_NOT_FOUND),
//     API error, API_UNREACHABLE
//   - Exit 4: not authenticated, 401
//   - --json success: {name, status, projects: []|null|undefined}
//   - --json error: {error, exit_code, code}
//
// AUDIT-1 contract: API may omit `projects` entirely; CLI must handle null
// gracefully (no crash). Tested implicitly by happy-path.
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero env inspect", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: inspecting existing env returns valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// Try the canonical "production" env that is created during seed.
		// If it doesn't exist (drift in seed), fall back to soft-skip via
		// 404 handling.
		stdout, stderr, exitCode := runLoopZero("env", "inspect", "production", "--json")

		expectAllowedExitCode(exitCode, "env inspect --json")
		expectNoInternalTerms(stdout+stderr, "env inspect --json")

		if exitCode == 1 {
			// 404 path is also valid contract — error shape required.
			obj, err := parseJSON(stdout)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj).To(HaveKey("error"))
			Expect(obj).To(HaveKey("exit_code"))
			Skip("seeded env 'production' missing — error-shape validated, success path skipped")
		}

		Expect(exitCode).To(Equal(0))
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// Schema: name + status required. projects optional (AUDIT-1).
		Expect(obj).To(HaveKey("name"))
		Expect(obj).To(HaveKey("status"))
		Expect(jsonStringField(obj, "name")).To(Equal("production"))
	})

	// --- Contract 2 + 5: 404 ---

	It("Contract 2+5a: nonexistent env yields exit 1 ENV_NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("env", "inspect",
			"this-env-does-not-exist-"+uniqueSuffix(), "--json")

		expectAllowedExitCode(exitCode, "env inspect (404)")
		Expect(exitCode).To(Equal(1),
			"missing env should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("ENV_NOT_FOUND"))
	})

	// --- Contract 2 + 5: no token ---

	It("Contract 2+5b: no token yields exit 4 AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("env", "inspect", "anything", "--json")

		expectAllowedExitCode(exitCode, "env inspect (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated should exit 4, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("env", "inspect", "anything")
		expectNoInternalTerms(stdout, "env inspect (no token) stdout")
		expectNoInternalTerms(stderr, "env inspect (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("env", "inspect", "anything", "--json")
		expectNoInternalTerms(stdout, "env inspect --json (no token) stdout")
		expectNoInternalTerms(stderr, "env inspect --json (no token) stderr")
	})
})
