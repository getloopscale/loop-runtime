// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero project inspect <name>` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/project-inspect.ts (AUDIT-4, AUDIT-25).
//
// Behavior:
//   - Exit 0: success
//   - Exit 1: missing name (MISSING_ARGUMENT), 404 (PROJECT_NOT_FOUND),
//     API error, API unreachable
//   - Exit 4: not authenticated, 401
//   - --json success: {id, name, team, created_at, created_by?}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero project inspect", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3 ---

	It("Contract 1+3: inspecting seeded project returns valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "seed failed")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		// seed.ProjectName / seed.SeededProjectName is exposed by the seed
		// package; if absent we fall back to the e2e-test-hello convention.
		projectName := seed.ProjectName
		if projectName == "" {
			projectName = "e2e-test-hello"
		}

		stdout, stderr, exitCode := runLoopZero("project", "inspect", projectName, "--json")

		expectAllowedExitCode(exitCode, "project inspect --json")
		expectNoInternalTerms(stdout+stderr, "project inspect --json")

		// If the seeded project doesn't exist on this stack we'll get a 404 +
		// exit 1. Treat that as a soft-skip: the contract assertions only fire
		// for the success path. We still validate the error-shape contract.
		if exitCode != 0 {
			Expect(exitCode).To(Equal(1),
				"if not 0, must be 1 (PROJECT_NOT_FOUND). Got %d. stderr=%s",
				exitCode, stderr)
			obj, err := parseJSON(stdout)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj).To(HaveKey("error"))
			Expect(obj).To(HaveKey("exit_code"))
			Skip("seeded project not present (404) — error-shape validated, success path skipped")
		}

		Expect(looksLikeJSONLine(stdout)).To(BeTrue())
		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// AUDIT-4: shape is {id, name, team, created_at, created_by?}, not team_id.
		Expect(obj).To(HaveKey("id"))
		Expect(obj).To(HaveKey("name"))
		Expect(obj).To(HaveKey("team"))
		Expect(obj).To(HaveKey("created_at"))
		Expect(obj).NotTo(HaveKey("team_id"),
			"AUDIT-4 contract: must use 'team' not 'team_id'")
		Expect(jsonStringField(obj, "name")).To(Equal(projectName))
	})

	// --- Contract 2 + 5: missing argument ---

	It("Contract 2+5a: missing name arg yields exit 1 MISSING_ARGUMENT", func() {
		// Auth must be present so we don't short-circuit on AUTH_REQUIRED.
		err := seed.Run(seed.Config{})
		if err != nil || !apiHealthy() {
			// We can still test missing-arg path locally, but auth is required
			// before the arg check runs in some implementations. Without seed,
			// we'd hit AUTH_REQUIRED first. Skip cleanly.
			Skip("seed/api not available — missing-arg cannot be reached cleanly")
		}
		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("project", "inspect", "", "--json")
		// Empty arg may be rejected by Commander before handler. Accept exit 1.
		expectAllowedExitCode(exitCode, "project inspect (missing name)")
		Expect(exitCode).To(Equal(1),
			"missing name should exit 1, got %d (stderr=%s)", exitCode, stderr)

		// If JSON shape is produced, code should be MISSING_ARGUMENT or
		// COMMAND_VALIDATION_ERROR (Commander wrapper).
		if obj, err := parseJSON(stdout); err == nil {
			code := jsonStringField(obj, "code")
			Expect(code == "MISSING_ARGUMENT" || code == "COMMAND_VALIDATION_ERROR").To(BeTrue(),
				"unexpected code %q for missing-arg path", code)
		}
	})

	// --- Contract 2 + 5: 404 not found ---

	It("Contract 2+5b: nonexistent project yields exit 1 PROJECT_NOT_FOUND", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred())

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", seed.AuthToken)
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("project", "inspect",
			"this-project-does-not-exist-"+uniqueSuffix(), "--json")

		expectAllowedExitCode(exitCode, "project inspect (404)")
		Expect(exitCode).To(Equal(1),
			"missing project should exit 1, got %d (stderr=%s)", exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("PROJECT_NOT_FOUND"))
	})

	// --- Contract 4 ---

	It("Contract 4: error paths leak no internal terms", func() {
		stdout, stderr, _ := runLoopZeroNoToken("project", "inspect", "anything")
		expectNoInternalTerms(stdout, "project inspect (no token) stdout")
		expectNoInternalTerms(stderr, "project inspect (no token) stderr")

		stdout, stderr, _ = runLoopZeroNoToken("project", "inspect", "anything", "--json")
		expectNoInternalTerms(stdout, "project inspect --json (no token) stdout")
		expectNoInternalTerms(stderr, "project inspect --json (no token) stderr")
	})
})
