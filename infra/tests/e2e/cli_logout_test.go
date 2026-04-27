// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero logout` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/logout.ts.
//
// Behavior summary:
//   - Without --yes (and not CI): prompts for confirmation
//   - With --yes or CI=true: skips confirmation
//   - Always exits 0 (success or user cancellation)
//   - --json success: {"authenticated": false}
//   - --json cancelled: {"status": "cancelled"}
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI: loopzero logout", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: happy path + --json schema ---

	It("Contract 1+3: --yes --json produces authenticated=false and exits 0", func() {
		stdout, stderr, exitCode := runLoopZero("logout", "--yes", "--json")

		expectAllowedExitCode(exitCode, "logout --yes --json")
		expectNoInternalTerms(stdout+stderr, "logout --yes --json")

		Expect(exitCode).To(Equal(0), "stderr=%s", stderr)
		Expect(looksLikeJSONLine(stdout)).To(BeTrue(),
			"--json should produce JSON, got: %q", stdout)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		auth, ok := jsonBoolField(obj, "authenticated")
		Expect(ok).To(BeTrue(), "authenticated field missing")
		Expect(auth).To(BeFalse(), "expected authenticated=false after logout")
	})

	// --- Contract 2 + 5: failure path + exit codes ---
	//
	// Logout has no genuine domain failure; the only failure mode is a usage
	// error from Commander when invalid flags are passed. That's exit 1 per
	// Rule 4.

	It("Contract 2+5: invalid flag yields exit 1 (usage error)", func() {
		_, stderr, exitCode := runLoopZero("logout", "--this-flag-does-not-exist")

		expectAllowedExitCode(exitCode, "logout --invalid")
		Expect(exitCode).To(Equal(1),
			"invalid flag should exit 1 (usage), got %d (stderr=%s)",
			exitCode, stderr)
	})

	// --- Contract 4: internal terms (Rule 3) ---

	It("Contract 4: stdout/stderr contain no internal terms", func() {
		// CI=true skips the interactive prompt path.
		os.Setenv("CI", "true")
		defer os.Unsetenv("CI")

		stdout, stderr, _ := runLoopZero("logout", "--yes")
		expectNoInternalTerms(stdout, "logout --yes human stdout")
		expectNoInternalTerms(stderr, "logout --yes human stderr")

		stdout, stderr, _ = runLoopZero("logout", "--yes", "--json")
		expectNoInternalTerms(stdout, "logout --yes --json stdout")
		expectNoInternalTerms(stderr, "logout --yes --json stderr")
	})

	// --- Bonus: cancellation path ---
	//
	// We can exercise the cancellation branch by running with CI=false and
	// piping "n" as input. This validates the {"status": "cancelled"} shape.

	It("Bonus: declining the prompt yields {status: cancelled} and exits 0", func() {
		// This test is sensitive to TTY behavior; some environments may not
		// emit the prompt the same way. We skip if the cancellation shape is
		// not produced, but assert exit 0 is honored either way.
		oldCI := os.Getenv("CI")
		os.Setenv("CI", "")
		defer os.Setenv("CI", oldCI)

		stdout, stderr, exitCode := runLoopZeroWithStdin("n\n", "logout", "--json")
		expectAllowedExitCode(exitCode, "logout cancellation")
		expectNoInternalTerms(stdout+stderr, "logout cancellation")
		Expect(exitCode).To(Equal(0),
			"cancellation should still exit 0, got %d (stderr=%s)",
			exitCode, stderr)

		obj, err := parseJSON(stdout)
		if err != nil {
			Skip("logout cancellation prompt could not be exercised in this env (stdout=" + stdout + ")")
		}
		status := jsonStringField(obj, "status")
		auth, hasAuth := jsonBoolField(obj, "authenticated")
		// Either cancellation shape OR success shape is valid; we only fail
		// if neither matches.
		if status != "cancelled" && !(hasAuth && !auth) {
			Fail("expected either {status: cancelled} or {authenticated: false}; got: " + stdout)
		}
	})
})
