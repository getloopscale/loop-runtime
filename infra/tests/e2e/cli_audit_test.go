// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E tests for `loopzero audit` covering the 5 CLI contracts.
//
// Source-of-truth: cli/src/commands/audit.ts (AUDIT-14, AUDIT-23 history).
//
// Behavior:
//   - Exit 0: success (with or without entries)
//   - Exit 1: API unreachable, unexpected error, FORBIDDEN with null data
//   - Exit 4: not authenticated, 401, 403 from server
//   - --json success: {entries, total, limit, offset}
//   - --json error: {error, exit_code, code}
package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

var _ = Describe("CLI: loopzero audit", Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("LOOPZERO_E2E") == "" {
			Skip("Set LOOPZERO_E2E=1 to run E2E tests")
		}
		if !loopzeroCLIAvailable() {
			Skip("loopzero binary not on PATH")
		}
	})

	// --- Contract 1 + 3: happy path + --json schema ---

	It("Contract 1+3: authenticated audit returns exit 0 + valid JSON shape", func() {
		if !apiHealthy() {
			Skip("API not reachable")
		}
		err := seed.Run(seed.Config{})
		Expect(err).NotTo(HaveOccurred(), "seed failed")

		oldToken := os.Getenv("LOOPZERO_TOKEN")
		os.Setenv("LOOPZERO_TOKEN", resolveToken())
		defer os.Setenv("LOOPZERO_TOKEN", oldToken)

		stdout, stderr, exitCode := runLoopZero("audit", "--json")

		expectAllowedExitCode(exitCode, "audit --json (authenticated)")
		expectNoInternalTerms(stdout+stderr, "audit --json")

		// AUDIT-23: every error branch routes through writeFailure with --json
		// shape. Success returns a different shape (entries/total/limit/offset).
		// Both are valid JSON.
		Expect(looksLikeJSONLine(stdout)).To(BeTrue(),
			"--json should produce JSON, got: %q\nstderr: %s", stdout, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())

		// If exit code 0: success shape.
		// If exit code 4: error shape (e.g. seed token may not have audit perms).
		if exitCode == 0 {
			Expect(obj).To(HaveKey("entries"), "success shape requires entries: %v", obj)
			Expect(obj).To(HaveKey("total"), "success shape requires total: %v", obj)
		} else {
			// FORBIDDEN is a known outcome if the seed user is not admin/owner.
			// We accept exit 4 here but assert error-shape contract.
			Expect(exitCode).To(Equal(4),
				"if not 0, must be 4 (forbidden). Got %d. stderr=%s", exitCode, stderr)
			Expect(obj).To(HaveKey("error"))
			Expect(obj).To(HaveKey("exit_code"))
			Expect(obj).To(HaveKey("code"))
		}
	})

	// --- Contract 2 + 5: failure path + exit codes ---

	It("Contract 2+5: no token yields exit 4 with AUTH_REQUIRED", func() {
		stdout, stderr, exitCode := runLoopZeroNoToken("audit", "--json")

		expectAllowedExitCode(exitCode, "audit --json (no token)")
		Expect(exitCode).To(Equal(4),
			"unauthenticated audit should exit 4, got %d (stderr=%s)",
			exitCode, stderr)

		obj, err := parseJSON(stdout)
		Expect(err).NotTo(HaveOccurred())
		Expect(jsonStringField(obj, "code")).To(Equal("AUTH_REQUIRED"),
			"unauthenticated audit should set code=AUTH_REQUIRED")
	})

	// --- Contract 4: internal terms (Rule 3) ---

	It("Contract 4: error and success paths leak no internal terms", func() {
		// No-token path (exercises auth error rendering).
		stdout, stderr, _ := runLoopZeroNoToken("audit")
		expectNoInternalTerms(stdout, "audit human (no token) stdout")
		expectNoInternalTerms(stderr, "audit human (no token) stderr")
		stdout, stderr, _ = runLoopZeroNoToken("audit", "--json")
		expectNoInternalTerms(stdout, "audit --json (no token) stdout")
		expectNoInternalTerms(stderr, "audit --json (no token) stderr")

		// Authenticated path (success or forbidden — both rendered to user).
		err := seed.Run(seed.Config{})
		if err == nil && apiHealthy() {
			oldToken := os.Getenv("LOOPZERO_TOKEN")
			os.Setenv("LOOPZERO_TOKEN", resolveToken())
			defer os.Setenv("LOOPZERO_TOKEN", oldToken)

			stdout, stderr, _ := runLoopZero("audit")
			expectNoInternalTerms(stdout, "audit human (authenticated) stdout")
			expectNoInternalTerms(stderr, "audit human (authenticated) stderr")
		}
	})
})
